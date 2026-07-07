package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// recordedRequest is a deep snapshot of the request the fake upstream received
// (PRD §19.3). Unlike the seed fakeUpstream's shallow `*got = *r` copy (which cannot
// capture the body — r.Body is a consumed io.ReadCloser), this stores the FULLY-READ
// body plus a CLONED header map, so PRD §19.3 case 1 ("upstream receives search_query")
// and the redaction case can assert on what actually reached upstream.
type recordedRequest struct {
	Method string      // always POST for MCP, but recorded for completeness
	Path   string      // r.URL.Path (the proxy forwards regardless of path; recorded anyway)
	Header http.Header // CLONED (live map races concurrent requests)
	Body   []byte      // io.ReadAll(r.Body) — the bytes the proxy forwarded
}

// fakeMCP is a configurable in-process stand-in for the z.ai MCP upstream
// (PRD §19.3 / §19.4, P1.M5.T1.S1). It records every request it receives (headers +
// body, under a mutex) and replies with a canned SSE body loaded from a testdata
// fixture, plus the headers a real z.ai response carries (Content-Type
// text/event-stream + Mcp-Session-Id). It is the formalized harness P1.M5.T1.S2's
// five PRD §19.3 cases build on; it complements (does NOT replace) proxy_test.go's
// seed fakeUpstream, which the existing unit tests and the parallel proxy_log_test.go
// still use.
//
// MOCKING: the fake upstream IS the mock. Config.Upstream points at f.URL; no real
// z.ai call is ever made.
type fakeMCP struct {
	*httptest.Server // embed so up.URL / up.Close() work like the seed

	mu  sync.Mutex
	rec recordedRequest

	sseBody string // canned SSE to return (loaded from a testdata fixture)
	status  int    // HTTP status (0 -> coerced to 200 in serve)
	session string // Mcp-Session-Id response header value ("" -> omit)
}

// newFakeMCP starts a fake upstream that replies with sse (a canned SSE body,
// typically loaded from a testdata fixture via loadFixture) and the given
// Mcp-Session-Id response header. status defaults to 200 (set f.status before the
// first request to override). Close with defer.
func newFakeMCP(t *testing.T, sse, session string) *fakeMCP {
	t.Helper()
	f := &fakeMCP{sseBody: sse, session: session, status: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// newFakeMCPInit starts a fake upstream that returns the initialize SSE loaded from
// testdata/initialize.sse + the canonical testSID (PRD §8 initialize response). This
// is the default z.ai-initialize stand-in T1.S2's session round-trip case builds on.
func newFakeMCPInit(t *testing.T) *fakeMCP {
	t.Helper()
	return newFakeMCP(t, loadFixture(t, "testdata/initialize.sse"), testSID)
}

// serve is the fake upstream handler: record the full request (headers + body), then
// write the canned SSE response with the configured status + session header.
//
// The body is captured by io.ReadAll(r.Body) — NOT by a shallow `*http.Request` copy
// — because r.Body is an io.ReadCloser that is consumed once read; the seed
// fakeUpstream's `*got = *r` could record headers/URL/method but left Body unreadable
// from the snapshot, so PRD §19.3 case 1 ("upstream receives search_query") was not
// assertable from the recording. The header is CLONED and the swap mutex-guarded
// because r.Header is a live map that races concurrent requests.
func (f *fakeMCP) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.rec = recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   body,
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
	if f.session != "" {
		w.Header().Set("Mcp-Session-Id", f.session)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, f.sseBody)
}

// recorded returns a copy of the last request the fake upstream received. Safe to
// call after postRPC returns (the recording is captured at handler entry, before the
// response is written). PRD §19.3 cases assert on this.
func (f *fakeMCP) recorded() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rec
}

// loadFixture reads a testdata SSE fixture (relative to the package main test CWD =
// repo root) and returns its contents as a string. Used to feed fakeMCP canned SSE
// from the golden fixtures (PRD §19.4). Generalizes proxy_test.go's toolsCallSSE.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// postRPC posts a JSON-RPC body to the proxy at proxyURL and returns the real client
// response (PRD §19.3 — "send a JSON-RPC request and capture the client-side SSE
// response"). The caller owns resp.Body (close it). setHeaders, if non-nil, mutates
// the outbound request headers AFTER Content-Type is set (use it to add
// Authorization, Mcp-Session-Id, or a custom Accept). The upstream-received request
// is read via the fake's recorded() — postRPC returns ONLY the client response to
// stay decoupled from the fake type.
//
// CRITICAL: Go's http client sets NO default Accept, so a request built with
// Content-Type alone has Accept == "" at the proxy, which triggers the proxy's
// Accept fallback (proxy.go: "application/json, text/event-stream"). That is the
// behavior TestHarness_InitializeAndAccept asserts on. Do NOT add a default Accept
// here.
func postRPC(t *testing.T, proxyURL, body string, setHeaders func(http.Header)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build rpc request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if setHeaders != nil {
		setHeaders(req.Header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post rpc to %s: %v", proxyURL, err)
	}
	return resp
}

// (H1) Initialize round-trip + Accept assertion (PRD §8/§19.3 preamble). A JSON-RPC
// initialize sent with NO Accept reaches the fake upstream with Accept containing
// text/event-stream (the proxy fallback); the fake records the request BODY (proving
// body recording works); the client receives 200, text/event-stream, Mcp-Session-Id,
// and a body byte-equal to testdata/initialize.sse.
func TestHarness_InitializeAndAccept(t *testing.T) {
	up := newFakeMCPInit(t) // initialize.sse + testSID + 200
	defer up.Close()
	proxy := newTestProxy(up.URL) // reuse the seed: real handler + discard logger
	defer proxy.Close()

	// POST an initialize with Content-Type ONLY (no Accept) -> proxy fallback fires.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","method":"initialize","id":1}`, nil)
	defer resp.Body.Close()

	rec := up.recorded()

	// (a) The item's explicit assertion: upstream Accept contains text/event-stream.
	if accept := rec.Header.Get("Accept"); !strings.Contains(accept, "text/event-stream") {
		t.Errorf("upstream Accept = %q, want it to contain text/event-stream (proxy fallback)", accept)
	}
	// (b) The harness records the BODY (the seed's shallow copy could not).
	if !bytes.Contains(rec.Body, []byte(`"method":"initialize"`)) {
		t.Errorf("recorded upstream body missing initialize method: %q", rec.Body)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("recorded Method = %q, want POST", rec.Method)
	}

	// (c) Client received the initialize SSE verbatim + the session header.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("client Content-Type = %q, want text/event-stream", ct)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != testSID {
		t.Errorf("client Mcp-Session-Id = %q, want %q", sid, testSID)
	}
	got, _ := io.ReadAll(resp.Body)
	if want := loadFixture(t, "testdata/initialize.sse"); string(got) != want {
		t.Errorf("client body != initialize.sse fixture:\n got %q\nwant %q", got, want)
	}
}

// (H2) Body + header recording (PRD §19.3 case 1/4 substrate). A tools/call with an
// ALIAS ("query") + Authorization + a client Mcp-Session-Id yields a recorded()
// whose headers carry the forwarded Authorization and the client Mcp-Session-Id, and
// whose body shows the alias RENAMED to search_query before it reached upstream
// (proving the body is captured as the PROXY forwarded it). This is the capability
// T1.S2's rewrite + Auth-forwarded cases assert on.
func TestHarness_RecordsBodyAndHeaders(t *testing.T) {
	up := newFakeMCP(t, toolsCallSSE(t), testSID) // canned tools/call SSE
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		func(h http.Header) {
			h.Set("Authorization", "Bearer secret-token")
			h.Set("Mcp-Session-Id", "client-session-id")
		})
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body) // drain so the handler fully completes

	rec := up.recorded()

	// Header recording: Authorization forwarded verbatim; client Mcp-Session-Id forwarded.
	if got := rec.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Errorf("recorded Authorization = %q, want forwarded verbatim", got)
	}
	if got := rec.Header.Get("Mcp-Session-Id"); got != "client-session-id" {
		t.Errorf("recorded Mcp-Session-Id = %q, want the client value forwarded", got)
	}

	// Body recording: the alias was RENAMED by the proxy before reaching upstream.
	var obj map[string]any
	if err := json.Unmarshal(rec.Body, &obj); err != nil {
		t.Fatalf("recorded body not valid JSON: %v (%q)", err, rec.Body)
	}
	args, ok := obj["params"].(map[string]any)["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("recorded body missing params.arguments: %#v", obj)
	}
	if _, ok := args["query"]; ok {
		t.Errorf("recorded body still has alias 'query' (proxy should have renamed it): %#v", args)
	}
	if args["search_query"] == nil {
		t.Errorf("recorded body missing 'search_query' (rename did not reach upstream): %#v", args)
	}
}
