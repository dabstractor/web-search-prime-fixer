package main

import (
	"encoding/json"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
)

// initJSON is the initialize event's Data segment: proxy_test.go's initSSE with
// the "id:1\nevent:message\ndata:" prefix and trailing "\n\n" removed. It equals
// the decoded Event.Data of testdata/initialize.sse (PRD §8/§12.1).
var initJSON = strings.TrimSuffix( // drop trailing blank-line "\n"
	strings.TrimPrefix(initSSE, "id:1\nevent:message\ndata:"),
	"\n\n")

// TestSSEReader_DecodeInitializeFixture proves the §8 initialize wire format
// decodes to Event{ID:"1", Type:"message", Data:<init JSON>}, and that the next
// Next() returns io.EOF (single-event stream) (PRD §8 init, §12.1).
func TestSSEReader_DecodeInitializeFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize.sse")
	if err != nil {
		t.Skipf("testdata fixture missing — run the P1.M3.T1.S1 fixtures item first: %v", err)
	}
	rd := NewSSEReader(strings.NewReader(string(raw)))
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("first Next() err=%v, want nil", err)
	}
	if ev.ID != "1" {
		t.Errorf("ID=%q want 1", ev.ID)
	}
	if ev.Type != "message" {
		t.Errorf("Type=%q want message", ev.Type)
	}
	if ev.Data != initJSON {
		t.Errorf("Data mismatch:\n got %q\nwant %q", ev.Data, initJSON)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("second Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_DecodeToolsCallFixture proves a §8 tools/call event (no id:/event:
// line, default Type "message") decodes to valid-JSON Data (PRD §8 tools/call,
// §19.2).
func TestSSEReader_DecodeToolsCallFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata fixture missing — run the P1.M3.T1.S1 fixtures item first: %v", err)
	}
	rd := NewSSEReader(strings.NewReader(string(raw)))
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("first Next() err=%v, want nil", err)
	}
	if ev.ID != "" {
		t.Errorf("ID=%q want \"\"", ev.ID)
	}
	if ev.Type != "message" {
		t.Errorf("Type=%q want message (default)", ev.Type)
	}
	if !json.Valid([]byte(ev.Data)) {
		t.Errorf("Data is not valid JSON: %q", ev.Data)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("second Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_MultilineJoinFixture proves the §8.10 multi-line data: form: the
// per-line values are joined with "\n" and the result is valid JSON (the §19.2
// round-trip case). tools_call_multiline.sse carries 8 data: lines.
func TestSSEReader_MultilineJoinFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call_multiline.sse")
	if err != nil {
		t.Skipf("testdata fixture missing — run the P1.M3.T1.S1 fixtures item first: %v", err)
	}
	rd := NewSSEReader(strings.NewReader(string(raw)))
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("first Next() err=%v, want nil", err)
	}
	if !json.Valid([]byte(ev.Data)) {
		t.Errorf("joined Data is not valid JSON: %q", ev.Data)
	}
	// 8 data: lines join to a Data string with 7 interior "\n".
	if got, want := strings.Count(ev.Data, "\n")+1, 8; got != want {
		t.Errorf("joined line count = %d, want %d (Data=%q)", got, want, ev.Data)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("second Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_LargeLineOver64KB PROVES the bufio.Reader-over-Scanner choice:
// a single data: line whose VALUE exceeds bufio.MaxScanTokenSize (64 KiB) parses
// without error. A bufio.Scanner framer would return bufio.ErrTooLong here
// (architecture/external_deps.md §7).
func TestSSEReader_LargeLineOver64KB(t *testing.T) {
	big := strings.Repeat("x", 70000) // 70000 > 65536 (bufio.MaxScanTokenSize)
	rd := NewSSEReader(strings.NewReader("data:" + big + "\n\n"))
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("Next() on >64KB line err=%v — did you use bufio.Scanner? want nil", err)
	}
	if len(ev.Data) != 70000 {
		t.Errorf("len(Data)=%d want 70000", len(ev.Data))
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("second Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_EOFFlushNoTrailingBlank PROVES the §12.1 EOF-flush rule: a
// pending event with no terminating blank line is still delivered, then io.EOF.
// ReadString returns (partial, io.EOF); the partial data: line must be processed
// BEFORE the pending event is flushed.
func TestSSEReader_EOFFlushNoTrailingBlank(t *testing.T) {
	rd := NewSSEReader(strings.NewReader("data:hello")) // no \n\n
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("Next() err=%v, want nil (EOF flush)", err)
	}
	if ev.Data != "hello" {
		t.Errorf("Data=%q want hello", ev.Data)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("second Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_MultipleEvents proves dispatch+reset across back-to-back events:
// each blank line yields one event and the accumulators clear before the next.
func TestSSEReader_MultipleEvents(t *testing.T) {
	rd := NewSSEReader(strings.NewReader("data:a\n\ndata:b\n\n"))
	ev, err := rd.Next()
	if err != nil || ev.Data != "a" {
		t.Fatalf("first Next() = %+v, %v; want Data:a", ev, err)
	}
	ev, err = rd.Next()
	if err != nil || ev.Data != "b" {
		t.Fatalf("second Next() = %+v, %v; want Data:b", ev, err)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("third Next() err=%v, want io.EOF", err)
	}
}

// TestSSEReader_InitializeFixtureEqualsInitSSE is a belt-and-suspenders byte
// identity check: the on-disk fixture must equal proxy_test.go's inline initSSE
// constant so the future inline->fixture swap is behavior-neutral (fixtures item).
func TestSSEReader_InitializeFixtureEqualsInitSSE(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize.sse")
	if err != nil {
		t.Skipf("testdata fixture missing — run the P1.M3.T1.S1 fixtures item first: %v", err)
	}
	if string(raw) != initSSE {
		t.Errorf("testdata/initialize.sse is NOT byte-identical to proxy_test.go initSSE:\n got %q\nwant %q", raw, initSSE)
	}
}

// TestSSE_Framing_Table exercises the WHATWG framing edge cases (PRD §12.1):
// comment lines, one-leading-space strip, default + explicit event type, id:,
// unknown fields ignored, multi-data join, CRLF tolerance, and the z.ai no-space
// wire form. Each case is a single-event stream; the subsequent Next() must be
// io.EOF.
func TestSSE_Framing_Table(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Event
	}{
		{"one_leading_space_stripped", "data: foo\n\n", Event{Type: "message", Data: "foo"}},
		{"two_leading_spaces_keep_one", "data:  foo\n\n", Event{Type: "message", Data: " foo"}},
		{"default_event_type_message", "data:foo\n\n", Event{Type: "message", Data: "foo"}},
		{"explicit_event_type", "event:custom\ndata:foo\n\n", Event{Type: "custom", Data: "foo"}},
		{"id_field", "id:42\ndata:foo\n\n", Event{ID: "42", Type: "message", Data: "foo"}},
		{"unknown_field_ignored", "retry:10000\ndata:foo\n\n", Event{Type: "message", Data: "foo"}},
		{"multiple_data_joined", "data:a\ndata:b\n\n", Event{Type: "message", Data: "a\nb"}},
		{"crlf_tolerated", "data:foo\r\n\r\n", Event{Type: "message", Data: "foo"}},
		{"no_space_after_colon", "data:foo\n\n", Event{Type: "message", Data: "foo"}}, // z.ai wire form
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rd := NewSSEReader(strings.NewReader(tc.in))
			got, err := rd.Next()
			if err != nil {
				t.Fatalf("Next() err=%v, want nil", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Next() = %+v, want %+v", got, tc.want)
			}
			if _, err := rd.Next(); err != io.EOF {
				t.Errorf("second Next() err=%v, want io.EOF", err)
			}
		})
	}
}

// TestSSE_CommentPreserved guards validation ISSUE 2: WHATWG comment lines
// (leading ':') are surfaced as standalone Events with Comment set, in stream
// order, and re-emitted VERBATIM by emitEvent. A heartbeat like ":keepalive"
// interleaved before a data event therefore survives the rewrite path (a dropped
// heartbeat on a long aliased stream could let an intermediary time the
// connection out).
func TestSSE_CommentPreserved(t *testing.T) {
	rd := NewSSEReader(strings.NewReader(": a comment\ndata:foo\n\n"))
	cmt, err := rd.Next()
	if err != nil {
		t.Fatalf("first Next() err=%v, want nil", err)
	}
	if cmt.Comment != ": a comment" {
		t.Errorf("comment event Comment=%q, want %q", cmt.Comment, ": a comment")
	}
	if cmt.Data != "" || cmt.ID != "" || cmt.Type != "" {
		t.Errorf("comment event leaked data/id/type: %+v", cmt)
	}
	// emitEvent re-emits the comment verbatim (no data: line, no dispatch blank line).
	var sb strings.Builder
	if err := emitEvent(&sb, cmt); err != nil {
		t.Fatalf("emitEvent comment err=%v", err)
	}
	if got := sb.String(); got != ": a comment\n" {
		t.Errorf("emitEvent comment = %q, want %q", got, ": a comment\n")
	}
	// The data event after the comment is still decoded normally.
	ev, err := rd.Next()
	if err != nil {
		t.Fatalf("second Next() err=%v, want nil", err)
	}
	if ev.Type != "message" || ev.Data != "foo" {
		t.Errorf("post-comment event = %+v, want {Type:message Data:foo}", ev)
	}
	if _, err := rd.Next(); err != io.EOF {
		t.Errorf("third Next() err=%v, want io.EOF", err)
	}
}

// TestWarningText_Table proves the PRD §12.3 warning-text format: the envelope,
// the "; " verbatim join of rewrite.go's note strings, and the two-branch
// suffix rule (all-ignored -> "avoid this notice"; otherwise -> "in future// calls"). Each row feeds a literal note slice (matching rewrite.go's PRD §10
// algorithm output) and asserts a byte-exact §12.3 string.
func TestWarningText_Table(t *testing.T) {
	// Note literals come from rewrite.go's PRD §10 algorithm strings (joined
	// verbatim). PRD §12.3 dictates the envelope + suffix.
	tests := []struct {
		name  string
		notes []string
		want  string
	}{
		{
			// PRD §12.3 example 1: {"query":"x"} -> renamed.
			"renamed_only_future_calls",
			[]string{`"query" is not a valid parameter; renamed to "search_query"`},
			`[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`,
		},
		{
			// PRD §12.3 example 2: {"query":"x","q":"y"} -> renamed + dropped.
			"renamed_and_dropped_future_calls",
			[]string{
				`"query" is not a valid parameter; renamed to "search_query"`,
				`dropped redundant "q"`,
			},
			`[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query"; dropped redundant "q". Use "search_query" in future calls.`,
		},
		{
			// {"query":"x","search_query":"y"} -> target wins, query ignored.
			"ignored_only_avoid_notice",
			[]string{`ignored "query" (use only "search_query")`},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"). Use only "search_query" to avoid this notice.`,
		},
		{
			// Multiple ignored aliases (e.g. {"query":x,"q":y,"search_query":z})
			// -> still all-ignored -> avoid-notice suffix.
			"multiple_ignored_avoid_notice",
			[]string{
				`ignored "query" (use only "search_query")`,
				`ignored "q" (use only "search_query")`,
			},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"); ignored "q" (use only "search_query"). Use only "search_query" to avoid this notice.`,
		},
		{
			// Mixed: one ignored + one renamed -> NOT all-ignored -> future-calls.
			"mixed_ignored_and_renamed_future_calls",
			[]string{
				`ignored "query" (use only "search_query")`,
				`"search" is not a valid parameter; renamed to "search_query"`,
			},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"); "search" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`,
		},
		{
			// Defensive: empty notes -> "" (injector only calls with non-empty,
			// but guard against a malformed "[web-search-prime-fixer] . Use…" stub).
			"empty_returns_empty",
			nil,
			``,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := warningText(tc.notes)
			if got != tc.want {
				t.Errorf("warningText(%#v):\n got %q\nwant %q", tc.notes, got, tc.want)
			}
		})
	}
}

// injectWarning is a fixed warning string for Inject tests (decoupled from
// warningText, which is exercised by TestWarningText_*). PRD §19.2.
const injectWarning = `[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`

// injectAll runs Inject over the SSE stream in raw, with the given rewrittenIDs and
// warning, and returns the re-emitted bytes. Helper for the §19.2 cases.
func injectAll(t *testing.T, raw string, rewrittenIDs map[any]bool, warning string) string {
	t.Helper()
	var out strings.Builder
	if err := Inject(&out, strings.NewReader(raw), rewrittenIDs, warning); err != nil {
		t.Fatalf("Inject err=%v, want nil", err)
	}
	return out.String()
}

// firstEventData decodes the first event of an SSE stream and returns it (for
// asserting on the injected/unchanged Data).
func firstEventData(t *testing.T, raw string) Event {
	t.Helper()
	ev, err := NewSSEReader(strings.NewReader(raw)).Next()
	if err != nil {
		t.Fatalf("re-decode first event err=%v, want nil", err)
	}
	return ev
}

// TestSSE_Inject_ToolCallPrependsWarning: inject into testdata/tools_call.sse with
// id 2 in the set -> content[0] is the warning; content[1].text is the ORIGINAL
// stringified array (byte-identical) and still valid JSON; isError untouched
// (PRD §19.2, §12.2, §3/FR-3).
func TestSSE_Inject_ToolCallPrependsWarning(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	// Capture the ORIGINAL content[0].text (the stringified array) BEFORE inject.
	origEv := firstEventData(t, string(raw))
	origResult := origEv.Data // full JSON-RPC object string
	var origObj map[string]any
	json.Unmarshal([]byte(origResult), &origObj)
	origText := origObj["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)

	out := injectAll(t, string(raw), map[any]bool{json.Number("2"): true}, injectWarning)
	ev := firstEventData(t, out)

	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("injected Data is not valid JSON: %v\n%s", err, ev.Data)
	}
	if obj["jsonrpc"] != "2.0" || obj["id"] != float64(2) {
		t.Errorf("envelope changed: jsonrpc=%v id=%v", obj["jsonrpc"], obj["id"])
	}
	result := obj["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError=%v, want false (FR-3: never set isError)", isErr)
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content)=%d, want 2 (warning + original)", len(content))
	}
	// content[0] is the warning block.
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" || c0["text"] != injectWarning {
		t.Errorf("content[0]=%v, want {type:text text:%q}", c0, injectWarning)
	}
	// content[1].text is the ORIGINAL stringified array, byte-identical + valid JSON.
	c1text := content[1].(map[string]any)["text"].(string)
	if c1text != origText {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1text, origText)
	}
	if !json.Valid([]byte(c1text)) {
		t.Errorf("content[1].text is not valid JSON: %q", c1text)
	}
}

// TestSSE_Inject_IdNotInSetPassthrough: a tools/call result whose id is NOT in the
// set is re-emitted with Data UNCHANGED (no warning) (PRD §19.2).
func TestSSE_Inject_IdNotInSetPassthrough(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	wantData := firstEventData(t, string(raw)).Data
	out := injectAll(t, string(raw), map[any]bool{float64(99): true}, injectWarning)
	gotData := firstEventData(t, out).Data
	if gotData != wantData {
		t.Errorf("id-not-in-set Data changed:\n got %q\nwant %q", gotData, wantData)
	}
}

// TestSSE_Inject_InitializePassthrough: a non-tools/call result (initialize; id 1)
// is re-emitted with Data UNCHANGED (PRD §19.2).
func TestSSE_Inject_InitializePassthrough(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	out := injectAll(t, string(raw), map[any]bool{json.Number("2"): true}, injectWarning)
	ev := firstEventData(t, out)
	if ev.Data != initJSON {
		t.Errorf("initialize Data changed:\n got %q\nwant %q", ev.Data, initJSON)
	}
}

// TestSSE_Inject_ErrorResultPassthrough: a JSON-RPC error envelope (id in set) and
// an MCP isError:true result are re-emitted UNCHANGED (PRD §19.2 "error result (no
// content array) -> verbatim"; §12.2).
func TestSSE_Inject_ErrorResultPassthrough(t *testing.T) {
	cases := []struct {
		name, sse string
	}{
		{
			// JSON-RPC error response: has "error", no "result" (hence no content).
			"jsonrpc_error_envelope",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"error\":{\"code\":-32603,\"message\":\"boom\"}}\n\n",
		},
		{
			// MCP tools/call error: result.isError=true (still has content) -> unchanged.
			"mcp_isError_true",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"err\"}]}}\n\n",
		},
		{
			// result present but no content array -> unchanged.
			"result_no_content",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"isError\":false}}\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantData := firstEventData(t, tc.sse).Data
			out := injectAll(t, tc.sse, map[any]bool{json.Number("2"): true}, injectWarning)
			gotData := firstEventData(t, out).Data
			if gotData != wantData {
				t.Errorf("error-result Data changed:\n got %q\nwant %q", gotData, wantData)
			}
		})
	}
}

// TestSSE_Inject_MultilineRoundTrip: a multi-data:-line event (8 lines) is parsed,
// injected, and re-emitted with content preserved. The joined Data parses to valid
// JSON; content[0] is the warning, content[1].text is valid JSON (PRD §19.2, §8.10).
func TestSSE_Inject_MultilineRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call_multiline.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	out := injectAll(t, string(raw), map[any]bool{json.Number("2"): true}, injectWarning)
	ev := firstEventData(t, out)
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("multiline injected Data not valid JSON: %v\n%s", err, ev.Data)
	}
	content := obj["result"].(map[string]any)["content"].([]any)
	if c0 := content[0].(map[string]any); c0["type"] != "text" || c0["text"] != injectWarning {
		t.Errorf("multiline content[0]=%v, want the warning block", c0)
	}
	c1text := content[1].(map[string]any)["text"].(string)
	if !json.Valid([]byte(c1text)) {
		t.Errorf("multiline content[1].text not valid JSON: %q", c1text)
	}
}

// TestSSE_EmitEvent_Framing: emitEvent produces id:/event:/data: framing per PRD
// §8.10 — id: iff ID!=""; event: iff a non-default type; internal "\n" in Data
// split into multiple data: lines; terminated by a blank line. Round-trips through
// NewSSEReader (PRD §19.2 multi-line round-trip).
func TestSSE_EmitEvent_Framing(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want string
	}{
		{
			// message default + multi-line data -> no event: line; 2 data: lines.
			"message_multiline_data",
			Event{Type: "message", Data: "a\nb"},
			"data:a\ndata:b\n\n",
		},
		{
			// id present, message default -> id: line, no event: line.
			"id_and_message",
			Event{ID: "1", Type: "message", Data: "x"},
			"id:1\ndata:x\n\n",
		},
		{
			// custom event type -> event: line emitted.
			"custom_event_type",
			Event{Type: "ping", Data: "x"},
			"event:ping\ndata:x\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := emitEvent(&b, tc.ev); err != nil {
				t.Fatalf("emitEvent err=%v", err)
			}
			if got := b.String(); got != tc.want {
				t.Errorf("emitEvent framing:\n got %q\nwant %q", got, tc.want)
			}
			// Round-trip: re-decoding the emitted bytes yields the original Event
			// (Data joined back with "\n", Type defaulted to "message" if absent).
			rt, err := NewSSEReader(strings.NewReader(b.String())).Next()
			if err != nil {
				t.Fatalf("round-trip re-decode err=%v", err)
			}
			wantRT := tc.ev
			if wantRT.Type == "" {
				wantRT.Type = "message"
			}
			if !reflect.DeepEqual(rt, wantRT) {
				t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", rt, wantRT)
			}
		})
	}
}
