package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// decodeLines splits a buffer of newline-terminated JSON log lines into decoded
// maps. Fails the test if any line is not valid JSON. Skips a trailing empty line
// (from the final newline). Use this instead of byte/string matching because
// json.Marshal HTML-escapes '<' to "\u003c" (see research/verify-logger-stdlib.md).
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue // trailing newline / blank line
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// (a) redactHeaders: the four sensitive headers → "<redacted>"; others preserved.
func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer hunter2")
	h.Set("Cookie", "session=abc123")
	h.Set("Set-Cookie", "a=1")
	h.Add("Set-Cookie", "b=2") // multiple cookies all collapse to one marker
	h.Set("Proxy-Authorization", "Basic dXNlcjpwYXNz")
	h.Set("Content-Type", "application/json")
	h.Set("Mcp-Session-Id", "sess-42")

	got := redactHeaders(h)

	for _, sensitive := range []string{"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization"} {
		if got[sensitive] != "<redacted>" {
			t.Errorf("%s = %#v, want \"<redacted>\"", sensitive, got[sensitive])
		}
	}
	// Non-sensitive headers keep their []string value.
	if ct, ok := got["Content-Type"].([]string); !ok || len(ct) != 1 || ct[0] != "application/json" {
		t.Errorf("Content-Type = %#v, want []string{\"application/json\"}", got["Content-Type"])
	}
	if sid, ok := got["Mcp-Session-Id"].([]string); !ok || len(sid) != 1 || sid[0] != "sess-42" {
		t.Errorf("Mcp-Session-Id = %#v, want []string{\"sess-42\"}", got["Mcp-Session-Id"])
	}
}

// (a.2) redactHeaders is case-insensitive on the key and does not mutate the input.
func TestRedactHeaders_CaseInsensitiveAndNonMutating(t *testing.T) {
	h := http.Header{"authorization": []string{"secret"}, "SET-COOKIE": []string{"c=v"}}
	got := redactHeaders(h)
	if got["authorization"] != "<redacted>" {
		t.Errorf("lowercase authorization = %#v, want \"<redacted>\"", got["authorization"])
	}
	if got["SET-COOKIE"] != "<redacted>" {
		t.Errorf("SET-COOKIE = %#v, want \"<redacted>\"", got["SET-COOKIE"])
	}
	// Input must be untouched (it's the live request header map in production).
	// NOTE: access the map directly rather than via h.Get, because http.Header.Get
	// canonicalizes its lookup key and would miss a non-canonical stored key
	// (the point of this test is non-mutation of the raw input, not key casing).
	if got := h["authorization"]; len(got) != 1 || got[0] != "secret" {
		t.Errorf("input header[authorization] mutated: %v", got)
	}
	if got := h["SET-COOKIE"]; len(got) != 1 || got[0] != "c=v" {
		t.Errorf("input header[SET-COOKIE] mutated: %v", got)
	}
}

// (b) Level filtering: at level=info, debug is dropped; info/warn/error kept.
func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")

	l.log("debug", "should be dropped", nil)
	if buf.Len() != 0 {
		t.Errorf("debug message written at info level: %q", buf.String())
	}

	for _, lvl := range []string{"info", "warn", "error"} {
		buf.Reset()
		l.log(lvl, "kept", nil)
		if buf.Len() == 0 {
			t.Errorf("%s message dropped at info level", lvl)
		}
	}
}

// (b.2) At level=error, only error is kept.
func TestLogger_LevelFiltering_ErrorOnly(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "error")
	for _, lvl := range []string{"debug", "info", "warn"} {
		buf.Reset()
		l.log(lvl, "dropped", nil)
		if buf.Len() != 0 {
			t.Errorf("%s written at error level: %q", lvl, buf.String())
		}
	}
	buf.Reset()
	l.log("error", "kept", nil)
	if buf.Len() == 0 {
		t.Error("error message dropped at error level")
	}
}

// (c) Output is valid JSON with ts/level/msg + merged fields; fields are added.
func TestLogger_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "debug")
	l.log("info", "forward", map[string]any{
		"req_id":  "r-1",
		"method":  "POST",
		"headers": redactHeaders(http.Header{"Authorization": []string{"Bearer x"}}),
	})

	lines := decodeLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	m := lines[0]
	if m["level"] != "info" {
		t.Errorf("level = %#v, want \"info\"", m["level"])
	}
	if m["msg"] != "forward" {
		t.Errorf("msg = %#v, want \"forward\"", m["msg"])
	}
	if m["req_id"] != "r-1" {
		t.Errorf("req_id = %#v, want \"r-1\"", m["req_id"])
	}
	// Nested redacted headers decode back to "<redacted>".
	hdrs, ok := m["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers = %#v, want a JSON object", m["headers"])
	}
	if hdrs["Authorization"] != "<redacted>" {
		t.Errorf("headers.Authorization = %#v, want \"<redacted>\"", hdrs["Authorization"])
	}
}

// (c.2) One JSON object per log call, each terminated by a newline.
func TestLogger_OneLinePerCall(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	l.log("info", "first", nil)
	l.log("warn", "second", nil)
	l.log("error", "third", nil)

	lines := decodeLines(t, &buf)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	wantMsgs := []string{"first", "second", "third"}
	for i, want := range wantMsgs {
		if lines[i]["msg"] != want {
			t.Errorf("line %d msg = %#v, want %q", i, lines[i]["msg"], want)
		}
	}
	// Buffer must end with a newline (one object per line).
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Error("buffer does not end with a newline")
	}
}

// (d) ts is RFC3339 — round-trips through time.Parse.
func TestLogger_RFC3339Ts(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	l.log("info", "x", nil)
	lines := decodeLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	ts, ok := lines[0]["ts"].(string)
	if !ok {
		t.Fatalf("ts = %#v, want a string", lines[0]["ts"])
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("ts %q is not RFC3339: %v", ts, err)
	}
}

// (e) newLogger tolerates an unknown/empty level (treated as info).
func TestNewLogger_UnknownLevel(t *testing.T) {
	for _, lvl := range []string{"", "trace", "verbose", "INFO"} {
		var buf bytes.Buffer
		l := newLogger(&buf, lvl) // must NOT panic
		// Treated as info: debug dropped, info kept.
		l.log("debug", "dropped", nil)
		if buf.Len() != 0 {
			t.Errorf("level=%q: debug written (expected info-rank behavior): %q", lvl, buf.String())
		}
		l.log("info", "kept", nil)
		if buf.Len() == 0 {
			t.Errorf("level=%q: info dropped (expected info-rank behavior)", lvl)
		}
	}
}
