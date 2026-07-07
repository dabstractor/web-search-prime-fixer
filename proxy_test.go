package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
