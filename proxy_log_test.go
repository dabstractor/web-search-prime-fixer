package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// findMsg decodes buf's JSONL and returns the first line whose "msg" == want, or
// fatals if absent. (P1.M4.T3.S1 log-event tests.)
func findMsg(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m["msg"] == want {
			return m
		}
	}
	t.Fatalf("no %q line in log output:\n%s", want, buf.String())
	return nil
}

// (L1) REWRITE event: an aliased tools/call emits exactly ONE info-level "rewrite"
// JSON line carrying req_id, tool (params.name), a non-empty notes array, and a
// per-alias presence map — and NO Authorization field / no Bearer token (PRD §15/§13;
// item MOCKING "a rewrite produces a JSON line with notes and NO Authorization field").
func TestLog_RewriteEvent(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // returns initSSE; we assert on the LOG, not the body
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "info") // info keeps rewrite, drops debug forward
	defer proxy.Close()

	// Alias "query" -> decideRewrite renames to search_query (rewrittenIDs={float64(2)}).
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"name":"web_search_prime","arguments":{"query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Drain so the handler (and its log calls) fully complete.
	_, _ = io.ReadAll(resp.Body)

	rewrite := findMsg(t, &buf, "rewrite")
	// Exactly one rewrite line.
	count := bytes.Count(buf.Bytes(), []byte(`"msg":"rewrite"`))
	if count != 1 {
		t.Fatalf("rewrite line count = %d, want 1:\n%s", count, buf.String())
	}
	if rewrite["level"] != "info" {
		t.Errorf("rewrite level = %#v, want info", rewrite["level"])
	}
	if rewrite["req_id"] != float64(2) {
		t.Errorf("rewrite req_id = %#v, want float64(2)", rewrite["req_id"])
	}
	if rewrite["tool"] != "web_search_prime" {
		t.Errorf("rewrite tool = %#v, want web_search_prime (params.name)", rewrite["tool"])
	}
	notes, ok := rewrite["notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Errorf("rewrite notes = %#v, want a non-empty array", rewrite["notes"])
	} else {
		// The renamed note mentions the alias + target.
		joined := strings.ToLower(fmt.Sprint(notes))
		if !strings.Contains(joined, "query") || !strings.Contains(joined, "search_query") {
			t.Errorf("rewrite notes missing the renamed->target fact: %v", notes)
		}
	}
	present, ok := rewrite["present"].(map[string]any)
	if !ok {
		t.Errorf("rewrite present = %#v, want a map", rewrite["present"])
	} else if present["query"] != true {
		t.Errorf("present[query] = %#v, want true (alias was present)", present["query"])
	}

	// SECURITY (PRD §6/§13): no Authorization field, no Bearer token anywhere.
	if _, ok := rewrite["Authorization"]; ok {
		t.Error("rewrite line carries an Authorization field (must never log secrets)")
	}
	if bytes.Contains(buf.Bytes(), []byte("Bearer")) {
		t.Errorf("a bearer token leaked into the log:\n%s", buf.String())
	}
}

// (L2) NO rewrite event when nothing changed: a canonical-param tools/call emits no
// rewrite line (PRD §15 "Logged whenever FR-2 changes a call" -> only on change).
func TestLog_NoRewriteWhenUnchanged(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "info")
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if bytes.Contains(buf.Bytes(), []byte(`"msg":"rewrite"`)) {
		t.Errorf("rewrite emitted for an unchanged (canonical) request:\n%s", buf.String())
	}
}

// (L3) FORWARD debug event: at debug level every request gets a "forward" line with
// method, id, and mode; a rewrite -> "injected", a passthrough -> "streamed". At INFO
// the forward line is SUPPRESSED (PRD §15 "Levels honored"; item MOCKING "info level
// suppresses debug forward lines").
func TestLog_ForwardDebugEvent(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()

	// (a) injected mode: aliased tools/call at DEBUG.
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug")
	defer proxy.Close()
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	fwd := findMsg(t, &buf, "forward")
	if fwd["method"] != "tools/call" {
		t.Errorf("forward method = %#v, want tools/call", fwd["method"])
	}
	if fwd["id"] != float64(2) {
		t.Errorf("forward id = %#v, want float64(2)", fwd["id"])
	}
	if fwd["mode"] != "injected" {
		t.Errorf("forward mode = %#v, want injected (alias was rewritten)", fwd["mode"])
	}

	// (b) streamed mode: canonical tools/call at DEBUG.
	var buf2 bytes.Buffer
	proxy2 := captureProxy(t, up.URL, &buf2, "debug")
	defer proxy2.Close()
	resp2, err := http.Post(proxy2.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)
	fwd2 := findMsg(t, &buf2, "forward")
	if fwd2["mode"] != "streamed" {
		t.Errorf("forward mode = %#v, want streamed (no rewrite)", fwd2["mode"])
	}
	if fwd2["method"] != "tools/call" {
		t.Errorf("forward method = %#v, want tools/call", fwd2["method"])
	}

	// (c) LEVEL FILTER: at INFO the forward line is dropped (rewrite/upstream_error stay).
	var buf3 bytes.Buffer
	proxy3 := captureProxy(t, up.URL, &buf3, "info")
	defer proxy3.Close()
	resp3, err := http.Post(proxy3.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	_, _ = io.ReadAll(resp3.Body)
	if bytes.Contains(buf3.Bytes(), []byte(`"msg":"forward"`)) {
		t.Errorf("forward debug line emitted at info level (must be suppressed):\n%s", buf3.String())
	}
}
