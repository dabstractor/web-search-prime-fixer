package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// Canned initialize SSE — PRD §8 wire format, NO space after `data`, trailing
// blank line dispatches the event. (P1.M3.T2 later extracts shared fixtures.)
const initSSE = "id:1\nevent:message\n" +
	`data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",` +
	`"capabilities":{"logging":{},"tools":{"listChanged":true}},` +
	`"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}` + "\n\n"

const testSID = "11111111-1111-1111-1111-111111111111"

// fakeUpstream returns a server that records the last request it received into
// *got and replies with the canned initialize SSE + mcp-session-id. Close with
// defer. (This harness is the seed P1.M5.T1.S1 will formalize.)
func fakeUpstream(t *testing.T, got *http.Request) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r // shallow copy for assertions
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, initSSE)
	}))
}

// newTestProxy wires a proxy at the given upstream URL with a discard logger.
func newTestProxy(upstreamURL string) *httptest.Server {
	cfg := DefaultConfig()
	cfg.Upstream = upstreamURL
	return httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard, "error"), newUpstreamClient()))
}

// (1) Transparent passthrough: client receives the SSE body byte-for-byte + the
// session header, status 200, Content-Type text/event-stream (PRD §11.3/§19.3
// case 3).
func TestPassthrough_InitializeSSE(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want it to contain text/event-stream", ct)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != testSID {
		t.Errorf("Mcp-Session-Id = %q, want %q", sid, testSID)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != initSSE {
		t.Errorf("body not byte-equal to upstream SSE:\n got %q\nwant %q", body, initSSE)
	}
}

// (2) Accept fallback when omitted; passthrough when provided (PRD §8/§11.2).
func TestPassthrough_AcceptFallback(t *testing.T) {
	// omitted -> upstream gets the default
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()
	resp, err := http.Post(proxy.URL, "", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if a := got.Header.Get("Accept"); a != "application/json, text/event-stream" {
		t.Errorf("omitted Accept: upstream got %q, want the default", a)
	}

	// provided -> forwarded unchanged
	var got2 http.Request
	up2 := fakeUpstream(t, &got2)
	defer up2.Close()
	proxy2 := newTestProxy(up2.URL)
	defer proxy2.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy2.URL, strings.NewReader(`{}`))
	req.Header.Set("Accept", "application/json")
	resp2, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp2.Body.Close()
	if a := got2.Header.Get("Accept"); a != "application/json" {
		t.Errorf("provided Accept: upstream got %q, want application/json (unchanged)", a)
	}
}

// (3) Hop-by-hop request headers stripped; sensitive/useful ones kept (PRD §11.2).
func TestPassthrough_HopByHopStripped(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{}`))
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("Authorization", "Bearer xyz")
	req.Header.Set("Mcp-Session-Id", testSID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	for _, h := range []string{"Connection", "Keep-Alive", "Transfer-Encoding", "Upgrade"} {
		if got.Header.Get(h) != "" {
			t.Errorf("hop-by-hop %q reached upstream", h)
		}
	}
	if got.Header.Get("Authorization") != "Bearer xyz" {
		t.Error("Authorization not forwarded")
	}
	if got.Header.Get("Mcp-Session-Id") != testSID {
		t.Error("Mcp-Session-Id not forwarded")
	}
}

// (4) Authorization is forwarded verbatim to the upstream (PRD §13/§19.3 case 4).
func TestPassthrough_AuthorizationForwarded(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer hunter2-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if v := got.Header.Get("Authorization"); v != "Bearer hunter2-token" {
		t.Errorf("upstream Authorization = %q, want it forwarded verbatim", v)
	}
}

// (5) Non-hop-by-hop response headers forwarded; hop-by-hop response headers
// dropped (PRD §11.3).
func TestPassthrough_ResponseHeadersCopied(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Log-Id", "log-7")
		w.Header().Set("Connection", "keep-alive") // hop-by-hop -> must NOT reach client
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, initSSE)
	}))
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.Header.Get("Mcp-Session-Id") != testSID {
		t.Error("Mcp-Session-Id not copied")
	}
	if resp.Header.Get("Cache-Control") != "no-cache" {
		t.Error("Cache-Control not copied")
	}
	if resp.Header.Get("X-Log-Id") != "log-7" {
		t.Error("X-Log-Id not copied")
	}
	if resp.Header.Get("Connection") != "" {
		t.Error("hop-by-hop Connection leaked to client")
	}
}

// dcCfg is a minimal Config for decideRewrite tests (decoupled from config.go
// defaults so the unit is stable if defaults change). PRD §10 alias order.
var dcCfg = Config{
	Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"},
	TargetParam: "search_query",
}

// dcArgsQueryBody is a canonical tools/call arguments skeleton used across rows.
const dcArgsQueryBody = `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
	`"params":{"name":"web_search_prime","arguments":{"query":"lunar rover"}}}`

// firstArgValue re-decodes dec.body and returns the params.arguments of the
// (first) rewritten tools/call object, so tests assert on the SEMANTIC value,
// not on marshaled key order. Handles both a single object body and a JSON-RPC
// batch array (returns the first tools/call element's arguments).
func firstArgValue(t *testing.T, b []byte) map[string]any {
	t.Helper()
	// Try a single object first (the normal MCP case).
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err == nil {
		params, _ := obj["params"].(map[string]any)
		args, _ := params["arguments"].(map[string]any)
		return args
	}
	// Otherwise it is a JSON-RPC batch array: find the first tools/call element.
	var arr []any
	if err := json.Unmarshal(b, &arr); err != nil {
		t.Fatalf("dec.body not valid JSON object or array: %v\n%s", err, b)
	}
	for _, elem := range arr {
		obj, ok := elem.(map[string]any)
		if !ok || obj["method"] != "tools/call" {
			continue
		}
		params, _ := obj["params"].(map[string]any)
		args, _ := params["arguments"].(map[string]any)
		return args
	}
	return nil
}

func TestDecideRewrite(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStream     bool   // expect streamThrough
		wantBodyIdent  bool   // expect dec.body == input bytes (unchanged path)
		wantArgKey     string // expected canonical arg key after rewrite ("" => unchanged)
		wantIDKey      any    // expected single rewritten id key (nil => no ids)
		wantWarningHas string // substring the warning must contain ("" => empty)
	}{
		// tools/call with alias "query" -> renamed to search_query (PRD §11.1/§10 row 1).
		{
			"toolscall_query_renamed",
			dcArgsQueryBody,
			false, false, "search_query", float64(2), "renamed",
		},
		// tools/call already canonical -> unchanged (PRD §10 row 6). BYTE-IDENTITY.
		{
			"toolscall_canonical_unchanged",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
			true, true, "", nil, "",
		},
		// tools/call with a NON-alias param -> unchanged (PRD §3). BYTE-IDENTITY.
		{
			"toolscall_nonalias_unchanged",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"foo":"bar"}}}`,
			true, true, "", nil, "",
		},
		// non-tools/call (initialize) -> unchanged. BYTE-IDENTITY.
		{
			"initialize_unchanged",
			`{"jsonrpc":"2.0","method":"initialize","id":1}`,
			true, true, "", nil, "",
		},
		// non-tools/call (tools/list) -> unchanged. BYTE-IDENTITY.
		{
			"toolslist_unchanged",
			`{"jsonrpc":"2.0","method":"tools/list","id":3}`,
			true, true, "", nil, "",
		},
		// JSON ARRAY (defensive batch): element[1] tools/call with alias -> rewrite;
		// body re-serialized as an array; one rewritten id (float64(5)) (PRD §11.1 point 2).
		{
			"array_batch_one_rewrite",
			`[{"jsonrpc":"2.0","method":"initialize","id":1},` +
				`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"arguments":{"q":"hi"}}}]`,
			false, false, "search_query", float64(5), "renamed",
		},
		// JSON ARRAY with NO tools/call rewrite -> unchanged, BYTE-IDENTITY.
		{
			"array_batch_no_rewrite",
			`[{"jsonrpc":"2.0","method":"initialize","id":1}]`,
			true, true, "", nil, "",
		},
		// tools/call with params absent -> skip, unchanged.
		{
			"toolscall_no_params",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call"}`,
			true, true, "", nil, "",
		},
		// tools/call with params.arguments absent -> skip, unchanged.
		{
			"toolscall_no_arguments",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_search_prime"}}`,
			true, true, "", nil, "",
		},
		// tools/call with params.arguments a NON-object (array) -> skip, unchanged (PRD §11.1 point 3).
		{
			"toolscall_arguments_array",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":["a","b"]}}`,
			true, true, "", nil, "",
		},
		// tools/call with a STRING id that is rewritten -> id keyed by the string.
		{
			"toolscall_string_id",
			`{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"arguments":{"query":"x"}}}`,
			false, false, "search_query", "abc", "renamed",
		},
		// invalid JSON -> pass through unchanged, BYTE-IDENTITY (never reject).
		{
			"invalid_json_passthrough",
			`{not valid json`,
			true, true, "", nil, "",
		},
		// valid JSON scalar -> pass through unchanged (default switch arm).
		{
			"scalar_json_passthrough",
			`42`,
			true, true, "", nil, "",
		},
		// multiple aliases present -> query promoted, q dropped; combined notes.
		{
			"toolscall_query_and_q",
			`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"arguments":{"query":"x","q":"y"}}}`,
			false, false, "search_query", float64(9), "dropped",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(tc.body)
			dec := decideRewrite(in, dcCfg)

			if dec.streamThrough != tc.wantStream {
				t.Errorf("streamThrough=%v, want %v", dec.streamThrough, tc.wantStream)
			}
			if tc.wantBodyIdent {
				// Unchanged path: body must be byte-for-byte identical to input.
				if !bytes.Equal(dec.body, in) {
					t.Errorf("unchanged body drifted:\n got %q\nwant %q", dec.body, in)
				}
			}
			if tc.wantIDKey != nil {
				if len(dec.rewrittenIDs) != 1 {
					t.Fatalf("rewrittenIDs len=%d, want 1: %#v", len(dec.rewrittenIDs), dec.rewrittenIDs)
				}
				if !dec.rewrittenIDs[tc.wantIDKey] {
					t.Errorf("rewrittenIDs missing key %#v (float64 contract): %#v", tc.wantIDKey, dec.rewrittenIDs)
				}
			} else if len(dec.rewrittenIDs) != 0 {
				t.Errorf("expected no rewritten ids, got %#v", dec.rewrittenIDs)
			}
			if tc.wantArgKey != "" {
				args := firstArgValue(t, dec.body)
				if args[tc.wantArgKey] == nil {
					t.Errorf("dec.body missing params.arguments.%s", tc.wantArgKey)
				}
				// The renamed alias must be GONE (only the canonical key remains).
				if args["query"] != nil || args["q"] != nil {
					t.Errorf("alias key leaked into rewritten args: %#v", args)
				}
			}
			if tc.wantWarningHas != "" {
				if dec.warning == "" {
					t.Errorf("warning empty, want it to contain %q", tc.wantWarningHas)
				} else if !strings.Contains(dec.warning, tc.wantWarningHas) {
					t.Errorf("warning=%q, want it to contain %q", dec.warning, tc.wantWarningHas)
				}
			} else if dec.warning != "" {
				t.Errorf("expected empty warning, got %q", dec.warning)
			}
		})
	}
}

// TestDecideRewrite_Float64IDContract pins the load-bearing contract with
// sse.Inject: numeric ids are keyed by float64(N), and an int(N) key does NOT
// match (the producer and consumer both decode via encoding/json -> float64).
func TestDecideRewrite_Float64IDContract(t *testing.T) {
	dec := decideRewrite([]byte(dcArgsQueryBody), dcCfg) // id:2
	if dec.streamThrough {
		t.Fatal("expected a rewrite, got streamThrough")
	}
	if !dec.rewrittenIDs[float64(2)] {
		t.Errorf("rewrittenIDs[float64(2)]=false, want true (Inject matches this)")
	}
	if dec.rewrittenIDs[int(2)] {
		t.Errorf("rewrittenIDs[int(2)]=true, want false — int keys must NOT match")
	}
}

// TestDecideRewrite_UnchangedBodyIsOriginalBytes guards PRD §11.1 point 4:
// nothing-changed => forward the ORIGINAL bytes (no re-serialization, no drift).
func TestDecideRewrite_UnchangedBodyIsOriginalBytes(t *testing.T) {
	// Non-canonical key order + extra whitespace: a re-serialization would SORT keys
	// and drop this formatting; the original-bytes path must preserve it verbatim.
	in := []byte(`{  "id" : 1 , "method":"initialize" , "jsonrpc":"2.0" }`)
	dec := decideRewrite(in, dcCfg)
	if !dec.streamThrough {
		t.Fatal("expected streamThrough for a non-tools/call body")
	}
	if !bytes.Equal(dec.body, in) {
		t.Errorf("unchanged body drifted (should be byte-identical):\n got %q\nwant %q", dec.body, in)
	}
}

// captureProxy builds a proxy at upstreamURL whose logger writes to buf at the
// given level (so warn-level upstream_error lines are observable, unlike
// newTestProxy's discard-error logger). (P1.M4.T2.S1)
func captureProxy(t *testing.T, upstreamURL string, buf *bytes.Buffer, level string) *httptest.Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Upstream = upstreamURL
	return httptest.NewServer(newProxyHandler(cfg, newLogger(buf, level), newUpstreamClient()))
}

// (6) Non-2xx upstream: log upstream_error {status, req_id} BUT copy status +
// headers + body through unchanged (PRD §15/§11.3; P1.M4.T2.S1 MOCKING).
func TestForward_Non2xxCopiedThroughAndLogged(t *testing.T) {
	const errBody = `{"error":"rate limited"}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusServiceUnavailable) // 503
		_, _ = io.WriteString(w, errBody)
	}))
	defer up.Close()

	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "warn")
	defer proxy.Close()

	// A tools/call with a numeric id so req_id is non-nil in the log.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Copy-through: the client gets 503 + the original body, not a synthesized 502.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (copy-through, not synthesized)", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != errBody {
		t.Errorf("body = %q, want %q (copied through verbatim)", got, errBody)
	}
	if resp.Header.Get("Mcp-Session-Id") != testSID {
		t.Error("non-hop-by-hop response header not copied on non-2xx")
	}

	// Exactly one upstream_error line, with status 503 and req_id 2.
	var saw bool
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if !bytes.Contains(line, []byte(`"msg":"upstream_error"`)) {
			continue
		}
		if saw {
			t.Fatalf("more than one upstream_error line:\n%s", buf.String())
		}
		saw = true
		if !bytes.Contains(line, []byte(`"status":503`)) {
			t.Errorf("upstream_error line missing status 503:\n%s", line)
		}
		// req_id 2 marshals as a JSON number (json.Marshal of float64(2) -> "2").
		if !bytes.Contains(line, []byte(`"req_id":2`)) {
			t.Errorf("upstream_error line missing req_id 2:\n%s", line)
		}
	}
	if !saw {
		t.Fatalf("no upstream_error line emitted for 503:\n%s", buf.String())
	}
}

// (7) A 2xx response emits NO upstream_error line (the log fires only on non-2xx).
func TestForward_2xxNoUpstreamError(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // replies 200 + initSSE
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug") // debug captures everything
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if bytes.Contains(buf.Bytes(), []byte(`"msg":"upstream_error"`)) {
		t.Errorf("upstream_error emitted on 200:\n%s", buf.String())
	}
}

// (8) Mcp-Session-Id round-trip: client request header -> upstream, and upstream
// response header -> client (PRD §8/§11; item MOCKING "Mcp-Session-Id round-trips").
func TestForward_McpSessionIdRoundTrip(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // echoes mcp-session-id=testSID in the response
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	const clientSID = "22222222-2222-2222-2222-222222222222"
	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", clientSID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Request side: upstream received the client's Mcp-Session-Id verbatim.
	if got.Header.Get("Mcp-Session-Id") != clientSID {
		t.Errorf("upstream Mcp-Session-Id = %q, want %q", got.Header.Get("Mcp-Session-Id"), clientSID)
	}
	// Response side: the upstream's mcp-session-id reached the client.
	if resp.Header.Get("Mcp-Session-Id") != testSID {
		t.Errorf("client Mcp-Session-Id = %q, want %q", resp.Header.Get("Mcp-Session-Id"), testSID)
	}
}

// TestDecideRewrite_ReqID pins that decideRewrite threads the request id for the
// upstream_error log (P1.M4.T2.S1). Numeric -> float64; string -> string; absent
// -> nil. Additive to T1.S1's decision (does not affect rewrittenIDs/warning).
func TestDecideRewrite_ReqID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want any
	}{
		{"numeric_id", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`, float64(2)},
		{"string_id", `{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"arguments":{"query":"x"}}}`, "abc"},
		{"no_id_notification", `{"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{"query":"x"}}}`, nil},
		{"initialize", `{"jsonrpc":"2.0","method":"initialize","id":1}`, float64(1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := decideRewrite([]byte(tc.body), dcCfg)
			if dec.reqID != tc.want {
				t.Errorf("reqID = %#v (want %#v); note numeric must be float64, not int", dec.reqID, tc.want)
			}
		})
	}
}

// toolsCallSSE loads the canned tools/call SSE result (id:2) the e2e tests stream.
// (P1.M4.T2.S2; mirrors sse_test.go's fixture read, but proxy_test.go owns its own.)
func toolsCallSSE(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata/tools_call.sse missing: %v", err)
	}
	return string(b)
}

// (9) PASSTHROUGH byte-equal: a canonical-param tools/call (no alias) streams the
// upstream SSE body back BYTE-FOR-BYTE — no injected warning block (PRD §11.3/§19.3
// case 2; P1.M4.T2.S2 MOCKING "passthrough case asserts client body is BYTE-EQUAL").
func TestForward_PassthroughByteEqual(t *testing.T) {
	want := toolsCallSSE(t) // upstream payload the client must receive verbatim
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, want)
	}))
	defer up.Close()
	proxy := newTestProxy(up.URL) // discard logger is fine — success path logs nothing
	defer proxy.Close()

	// Canonical param -> decideRewrite streamThrough=true -> io.Copy verbatim.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != want {
		t.Errorf("passthrough body not byte-equal to upstream (no block should be added):\n got %q\nwant %q", got, want)
	}
}

// (10) REWRITE + warning first: an aliased tools/call ({"query":...}, id:2) ->
// upstream RECEIVES search_query; client SSE event has the warning block at
// content[0] and the ORIGINAL content at content[1], isError==false (PRD §11.3/§3/
// FR-3/§19.3 case 1; P1.M4.T2.S2 MOCKING "rewrite case asserts warning block first").
func TestForward_RewriteWarningFirst(t *testing.T) {
	upPayload := toolsCallSSE(t) // id:2, content[0].text = stringified array
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)

		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		args, _ := obj["params"].(map[string]any)["arguments"].(map[string]any)
		if _, ok := args["query"]; ok {
			t.Errorf("upstream received alias 'query' (should be renamed): %#v", args)
		}
		if args["search_query"] == nil {
			t.Errorf("upstream did NOT receive 'search_query': %#v", args)
		}

		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, upPayload)
	}))
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Alias "query" -> decideRewrite renames to search_query, rewrittenIDs={float64(2):true}.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Decode the (single) SSE event the client received and assert on the JSON-RPC object.
	ev, err := NewSSEReader(bytes.NewReader(body)).Next()
	if err != nil {
		t.Fatalf("client response is not a decodable SSE event: %v\n%s", err, body)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("client event Data is not valid JSON: %v\n%s", err, ev.Data)
	}
	if obj["id"] != float64(2) {
		t.Errorf("event id = %v, want float64(2)", obj["id"])
	}
	result := obj["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError = true, want false (FR-3: never set isError)")
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2 (warning + original)", len(content))
	}
	// content[0] is the injected warning block.
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" {
		t.Errorf("content[0].type = %v, want text", c0["type"])
	}
	warn, _ := c0["text"].(string)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[0].text = %q, want it to start with the warning marker", warn)
	}
	// content[1].text is the ORIGINAL stringified array (the fixture's content[0].text).
	origContent := jsonToContent0Text(t, upPayload)
	c1text := content[1].(map[string]any)["text"].(string)
	if c1text != origContent {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1text, origContent)
	}
}

// jsonToContent0Text decodes an SSE payload's first event and returns
// result.content[0].text (the original stringified array) so the rewrite test can
// compare pre- vs post-inject.
func jsonToContent0Text(t *testing.T, sse string) string {
	t.Helper()
	ev, err := NewSSEReader(strings.NewReader(sse)).Next()
	if err != nil {
		t.Fatalf("decode upstream payload: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("upstream payload not JSON: %v", err)
	}
	return obj["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
}

// (11) flushWriter flushes once per Write and not on error; f==nil is a pass-through
// (P1.M4.T2.S2; pins "Flush after each emitted event" since emitEvent does one Write
// per event).
func TestFlushWriter_FlushesPerWrite(t *testing.T) {
	// recordingFlusher counts Flush() calls.
	var flushes int
	rc := &recordingFlusher{&flushes}
	fw := flushWriter{w: bytes.NewBuffer(nil), f: rc}
	if _, err := fw.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if flushes != 2 {
		t.Errorf("flushes = %d, want 2 (one per Write)", flushes)
	}

	// f == nil -> pass-through, no Flush.
	buf := bytes.NewBuffer(nil)
	fwNil := flushWriter{w: buf, f: nil}
	if _, err := fwNil.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "c" {
		t.Errorf("nil-flusher pass-through wrote %q, want c", buf.String())
	}

	// erroringWriter -> Write errors, Flush must NOT be called.
	before := flushes
	fwErr := flushWriter{w: erroringWriter{}, f: rc}
	if _, err := fwErr.Write([]byte("x")); err == nil {
		t.Error("erroringWriter Write returned nil error")
	}
	if flushes != before {
		t.Errorf("Flush called after a write error (flushes %d -> %d)", before, flushes)
	}
}

type recordingFlusher struct{ n *int }

func (r *recordingFlusher) Flush() { *r.n++ }

type erroringWriter struct{}

func (erroringWriter) Write(p []byte) (int, error) { return 0, io.ErrShortWrite }
