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

// The five PRD §19.3 end-to-end cases (P1.M5.T1.S2), built on the P1.M5.T1.S1
// harness (proxy_harness_test.go: newFakeMCP/newFakeMCPInit/postRPC/recorded) and
// the seed helpers (proxy_test.go: newTestProxy/captureProxy/testSID/toolsCallSSE/
// jsonToContent0Text). Each case is a real http.DefaultClient -> proxy -> fakeMCP
// round-trip; no real z.ai call is ever made (the fake IS the mock).
//
// Cases 1-2 re-prove behavior the seed covers (TestForward_RewriteWarningFirst /
// TestForward_PassthroughByteEqual) on the FORMAL harness; case 3 adds the real
// two-request session lifecycle; case 4 is the combined forward+redact proof;
// case 5 proves /healthz isolation via the harness's recorded() zero value.

// (E1) §19.3 case 1 / §21 #1: client sends {"query":"x"} -> upstream receives
// search_query; the client SSE result has the warning text block FIRST then the
// original payload (one-line correction, no retry). isError stays false (FR-3).
func TestE2E_RewriteAndWarningFirst(t *testing.T) {
	up := newFakeMCP(t, toolsCallSSE(t), testSID) // canned id:2 tools/call result
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Alias "query" -> proxy renames to search_query (rewrittenIDs={float64(2)}).
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// UPSTREAM side: the alias reached upstream renamed to search_query.
	rec := up.recorded()
	var req map[string]any
	if err := json.Unmarshal(rec.Body, &req); err != nil {
		t.Fatalf("recorded upstream body not JSON: %v (%q)", err, rec.Body)
	}
	args := req["params"].(map[string]any)["arguments"].(map[string]any)
	if _, ok := args["query"]; ok {
		t.Errorf("upstream received alias 'query' (should be renamed): %#v", args)
	}
	if got := args["search_query"]; got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\"", got)
	}

	// CLIENT side: one SSE event; result.content[0]=warning, [1]=original; isError=false.
	ev, err := NewSSEReader(bytes.NewReader(body)).Next()
	if err != nil {
		t.Fatalf("client response not a decodable SSE event: %v\n%s", err, body)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &res); err != nil {
		t.Fatalf("client event Data not JSON: %v\n%s", err, ev.Data)
	}
	if res["id"] != float64(2) {
		t.Errorf("event id = %#v, want float64(2)", res["id"])
	}
	result := res["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError = true, want false (FR-3: proxy never sets isError)")
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2 (warning + original)", len(content))
	}
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" {
		t.Errorf("content[0].type = %#v, want text", c0["type"])
	}
	warn, _ := c0["text"].(string)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[0].text = %q, want the warning marker first", warn)
	}
	// content[1] preserves the original stringified-array payload (fixture content[0]).
	orig := jsonToContent0Text(t, toolsCallSSE(t))
	if c1 := content[1].(map[string]any)["text"].(string); c1 != orig {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1, orig)
	}
}

// (E2) §19.3 case 2 / §21 #2: client sends {"search_query":"x"} -> upstream
// receives it unchanged; the client body is BYTE-EQUAL to the upstream payload
// (no injected block). Proves zero-overhead passthrough (identical results,
// identical schema, no warning).
func TestE2E_CanonicalPassthrough(t *testing.T) {
	want := toolsCallSSE(t) // the exact bytes the client must receive
	up := newFakeMCP(t, want, testSID)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Canonical param -> decideRewrite streamThrough=true -> io.Copy verbatim.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
		nil)
	defer resp.Body.Close()

	// UPSTREAM: search_query received unchanged.
	rec := up.recorded()
	var req map[string]any
	if err := json.Unmarshal(rec.Body, &req); err != nil {
		t.Fatalf("recorded upstream body not JSON: %v (%q)", err, rec.Body)
	}
	args := req["params"].(map[string]any)["arguments"].(map[string]any)
	if got := args["search_query"]; got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (unchanged)", got)
	}

	// CLIENT: byte-equal to the upstream payload — no injected warning block.
	got, _ := io.ReadAll(resp.Body)
	if string(got) != want {
		t.Errorf("client body not byte-equal to upstream (no block should be added):\n got %q\nwant %q", got, want)
	}
}

// (E3) §19.3 case 3 / FR-1: the initialize response reaches the client with the
// mcp-session-id header intact, and a FOLLOW-UP request resends that same
// Mcp-Session-Id to the upstream (inspect the upstream-received header). Two
// requests over one fake upstream: the id handed out on initialize is the one
// carried on the follow-up.
func TestE2E_SessionRoundTrip(t *testing.T) {
	up := newFakeMCPInit(t) // initialize.sse + Mcp-Session-Id=testSID on every response
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// (1) initialize: client receives the upstream's Mcp-Session-Id response header.
	resp1 := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","method":"initialize","id":1}`, nil)
	defer resp1.Body.Close()
	sid := resp1.Header.Get("Mcp-Session-Id")
	if sid != testSID {
		t.Fatalf("initialize client Mcp-Session-Id = %q, want %q (header must reach client intact)", sid, testSID)
	}
	_, _ = io.ReadAll(resp1.Body) // drain so the handler fully completes

	// (2) follow-up request resends the session id; upstream RECEIVES it verbatim.
	// recorded() returns the LAST request, so after the follow-up it holds the
	// follow-up's headers — exactly what this case inspects.
	resp2 := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
		func(h http.Header) { h.Set("Mcp-Session-Id", sid) })
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)

	rec := up.recorded() // the LAST request == the follow-up
	if got := rec.Header.Get("Mcp-Session-Id"); got != sid {
		t.Errorf("follow-up upstream Mcp-Session-Id = %q, want %q (resend verbatim)", got, sid)
	}
}

// (E4) §19.3 case 4 / §13/§6/§21: the Authorization header reaches the upstream
// UNCHANGED and is ABSENT from the captured stderr log. Uses captureProxy at debug
// + an aliased tools/call so BOTH the rewrite and forward log lines fire (the
// largest log surface a leaked secret could land in). proxy.go's log events carry
// NO header values by construction; this locks that invariant at the integration
// level — it is a REGRESSION GUARD, not a test of redactHeaders (that is unit-tested
// in logger_test.go).
func TestE2E_AuthForwardedAndRedacted(t *testing.T) {
	const secret = "Bearer test-secret-hunter2-token"
	up := newFakeMCP(t, toolsCallSSE(t), testSID)
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug") // debug -> rewrite + forward lines fire
	defer proxy.Close()

	// Aliased tools/call WITH Authorization -> rewrite path logs; header forwarded.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		func(h http.Header) { h.Set("Authorization", secret) })
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body) // drain so all log calls complete

	// (a) FORWARD: the Authorization header reached upstream verbatim.
	rec := up.recorded()
	if got := rec.Header.Get("Authorization"); got != secret {
		t.Errorf("upstream Authorization = %q, want forwarded verbatim", got)
	}

	// (b) REDACT: the secret value (and the Bearer marker) never appear in stderr.
	if bytes.Contains(buf.Bytes(), []byte(secret)) {
		t.Errorf("secret token leaked into the log:\n%s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("Bearer")) {
		t.Errorf("a Bearer marker leaked into the log (Authorization value logged):\n%s", buf.String())
	}
	// Belt-and-suspenders: no decoded JSONL line carries an Authorization field.
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		for k := range m {
			if http.CanonicalHeaderKey(k) == "Authorization" {
				t.Errorf("log line carries an Authorization field: %s", line)
			}
		}
	}
}

// (E5) §19.3 case 5 / §16: GET /healthz returns 200 {"ok":true,...} and does NOT
// call the upstream. Built on the SAME mux main() builds; the fake upstream's
// recorded() stays the ZERO value (its handler never ran) -> isolation proof via
// the harness, no separate hit counter.
func TestE2E_HealthzIsolated(t *testing.T) {
	up := newFakeMCPInit(t) // would record any request if /healthz leaked through
	defer up.Close()
	cfg := DefaultConfig()
	cfg.Upstream = up.URL
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", newProxyHandler(cfg, newLogger(io.Discard, "error"), newUpstreamClient()))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("/healthz body not JSON: %v (raw=%q)", err, raw)
	}
	if body["ok"] != true {
		t.Errorf("/healthz ok = %#v, want true", body["ok"])
	}

	// ISOLATION: the upstream handler never ran -> recorded() is the zero value.
	rec := up.recorded()
	if rec.Method != "" || rec.Header != nil || rec.Body != nil {
		t.Errorf("/healthz called the upstream (should be local-only): recorded=%+v", rec)
	}
}
