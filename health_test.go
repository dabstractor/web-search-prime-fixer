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

// (d) logStartup emits one info/startup line with the four config fields and NO
// authorization field (PRD §15 "Never logs credentials").
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
	// aliases is a JSON array matching cfg.Aliases ([]any after decode).
	aliases, ok := m["aliases"].([]any)
	if !ok {
		t.Fatalf("aliases = %#v, want a JSON array", m["aliases"])
	}
	want := make([]any, len(cfg.Aliases))
	for i, a := range cfg.Aliases {
		want[i] = a
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Errorf("aliases = %#v, want %#v", aliases, want)
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

// (e) Routing table (PRD §9): only /healthz is intercepted; it NEVER calls the
// upstream (PRD §16/§19.3 case 5). Built via the SAME mux main() builds. The
// "/mcp forwards" half is covered by proxy_test.go.
func TestRouting_HealthzOnly(t *testing.T) {
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
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
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("/healthz body not valid JSON: %v (raw=%q)", err, body)
	}
	if m["ok"] != true {
		t.Errorf("/healthz body ok = %#v, want true (routed to healthHandler)", m["ok"])
	}
	if hits != 0 {
		t.Errorf("upstream hit %d times for /healthz, want 0 (isolation)", hits)
	}
}
