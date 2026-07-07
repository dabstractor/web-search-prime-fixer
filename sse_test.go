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
		{"comment_ignored", ": a comment\ndata:foo\n\n", Event{Type: "message", Data: "foo"}},
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
