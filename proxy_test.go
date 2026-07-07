package main

import (
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
