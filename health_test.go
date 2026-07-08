package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// (a) GET /healthz -> 200, application/json, {"ok":true,"version":"dev"}.
func TestHealthHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	healthHandler(rr, req)

	if got := rr.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (raw=%q)", err, rr.Body.String())
	}
	if body["ok"] != true {
		t.Errorf("ok = %#v, want true", body["ok"])
	}
	if body["version"] != version {
		t.Errorf("version = %#v, want %#v", body["version"], version)
	}
}

// (b) Build-version override: setting the package var flows to /healthz. This
// proves the -ldflags "-X main.version=..." seam (the linker sets this same var).
func TestHealthHandler_VersionOverride(t *testing.T) {
	old := version
	version = "1.2.3"
	defer func() { version = old }() // restore for other tests

	rr := httptest.NewRecorder()
	healthHandler(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body map[string]any
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["version"] != "1.2.3" {
		t.Fatalf("version = %#v, want 1.2.3 (ldflags seam)", body["version"])
	}
}

// (c) healthHandler is pure: it completes without any network. (Structurally
// guaranteed — no http.Client call in the body — but asserted to lock the
// invariant that /healthz "does not touch the upstream", PRD §16.)
func TestHealthHandler_NoUpstream(t *testing.T) {
	rr := httptest.NewRecorder()
	// No upstream server is started; if the handler tried to dial, the test would
	// hang or fail. It must return 200 immediately.
	healthHandler(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (handler must be pure/local)", rr.Code)
	}
}

// (c2) validation NOTE 6: PRD §16 specifies GET /healthz. A non-GET method is
// rejected with 405 Method Not Allowed and an Allow: GET header so a health probe
// that accidentally POSTs cannot masquerade as a liveness check.
func TestHealthHandler_RejectsNonGET(t *testing.T) {
	rr := httptest.NewRecorder()
	healthHandler(rr, httptest.NewRequest(http.MethodPost, "/healthz", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405 (non-GET healthz)", rr.Code)
	}
	if allow := rr.Header().Get("Allow"); allow != "GET" {
		t.Errorf("Allow = %q, want GET", allow)
	}
	if rr.Body.Len() != 0 {
		t.Errorf("non-GET body = %q, want empty", rr.Body.String())
	}
}

// (d) logStartup emits one info/startup line with the v2 config fields and NO
// authorization field (PRD §15 "Never logs credentials"). v2 fields: tools,
// canonical_tool, query_aliases (as JSON arrays / scalar), listen, upstream,
// log_level.
func TestLogStartup(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	cfg := DefaultConfig()

	logStartup(l, cfg)

	// Exactly one line.
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (raw=%q)", len(lines), buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("line not valid JSON: %v (raw=%q)", err, lines[0])
	}
	if m["level"] != "info" {
		t.Errorf("level = %#v, want info", m["level"])
	}
	if m["msg"] != "startup" {
		t.Errorf("msg = %#v, want startup", m["msg"])
	}
	// tools is a JSON array matching cfg.Tools ([]any after decode).
	tools, ok := m["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %#v, want a JSON array", m["tools"])
	}
	wantTools := make([]any, len(cfg.Tools))
	for i, tt := range cfg.Tools {
		wantTools[i] = tt
	}
	if !reflect.DeepEqual(tools, wantTools) {
		t.Errorf("tools = %#v, want %#v", tools, wantTools)
	}
	// canonical_tool is the canonical tool name string.
	if m["canonical_tool"] != cfg.CanonicalTool {
		t.Errorf("canonical_tool = %#v, want %q", m["canonical_tool"], cfg.CanonicalTool)
	}
	// query_aliases is a JSON array matching cfg.QueryAliases ([]any after decode).
	aliases, ok := m["query_aliases"].([]any)
	if !ok {
		t.Fatalf("query_aliases = %#v, want a JSON array", m["query_aliases"])
	}
	wantAliases := make([]any, len(cfg.QueryAliases))
	for i, a := range cfg.QueryAliases {
		wantAliases[i] = a
	}
	if !reflect.DeepEqual(aliases, wantAliases) {
		t.Errorf("query_aliases = %#v, want %#v", aliases, wantAliases)
	}
	if m["listen"] != cfg.Listen {
		t.Errorf("listen = %#v, want %q", m["listen"], cfg.Listen)
	}
	if m["upstream"] != cfg.Upstream {
		t.Errorf("upstream = %#v, want %q", m["upstream"], cfg.Upstream)
	}
	if m["log_level"] != cfg.LogLevel {
		t.Errorf("log_level = %#v, want %q", m["log_level"], cfg.LogLevel)
	}
	// Security invariant: no credential field (Config has none; assert defensively).
	if _, present := m["authorization"]; present {
		t.Errorf("startup line contains an authorization field: %#v", m["authorization"])
	}
}

// (e) Routing (PRD §16/§9): /healthz routes to the local healthHandler and NEVER
// falls through to the catch-all. main() mounts the SDK StreamableHTTPHandler at
// "/"; this test substitutes a SENTINEL catch-all (decoupled from the SDK — the
// SDK integration is exercised by server_test.go in P1.M5.T3) that fails if hit,
// proving /healthz isolation. The /healthz body {"ok":true,...} also proves it
// reached healthHandler, not the catch-all.
func TestRouting_HealthzOnly(t *testing.T) {
	catchAllHit := false
	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catchAllHit = true
		w.WriteHeader(http.StatusNotFound)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/", catchAll) // sentinel stands in for the SDK handler at "/"

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
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("/healthz body not valid JSON: %v (raw=%q)", err, body)
	}
	if m["ok"] != true {
		t.Errorf("/healthz body ok = %#v, want true (routed to healthHandler)", m["ok"])
	}
	if catchAllHit {
		t.Error("/healthz fell through to the catch-all; want isolated routing to healthHandler")
	}
}
