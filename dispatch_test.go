package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newDispatchHandler builds the handler against a fresh fake z.ai (reusing
// upstream_test.go's newFakeZAI). It discards logs at info level. Returns the
// handler and the fake's recorded state. The UpstreamClient is constructed exactly
// as upstream_test.go does (struct literal; no credential field).
//
// SESSION CLEANUP: a callTool lazily opens a real MCP client session whose
// standalone SSE connection outlives the request (xcontext.Detach). The test
// binaries would HANG at exit on those leaked goroutines, so a t.Cleanup closes
// the session (if opened) once the test finishes — mirroring upstream_test.go's
// `defer u.session.Close()` without changing each test body.
func newDispatchHandler(t *testing.T) (mcp.ToolHandler, *fakeState) {
	t.Helper()
	srv, st := newFakeZAI(t)
	cfg := DefaultConfig()
	log := newLogger(io.Discard, "info")
	upstream := &UpstreamClient{
		upstream:    srv.URL,
		targetTool:  cfg.TargetTool,
		targetParam: cfg.TargetParam,
	}
	t.Cleanup(func() {
		if upstream.session != nil {
			_ = upstream.session.Close()
		}
	})
	return makeDispatchHandler(cfg, upstream, log), st
}

// callDispatch invokes h with a constructed CallToolRequest (nil Session/Extra — the
// handler reads only Params). argsJSON is the raw arguments payload (a JSON object,
// string, etc.).
func callDispatch(t *testing.T, h mcp.ToolHandler, tool, argsJSON string) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h(ctx, &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: tool, Arguments: json.RawMessage(argsJSON)},
	})
}

// lastArg reads a single recorded upstream argument under the fake's mutex (race-safe).
func lastArg(st *fakeState, key string) any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.lastArgs[key]
}

// callCount atomically reads the fake's tools/call count.
func callCount(st *fakeState) int32 {
	return atomic.LoadInt32(&st.calls)
}

// contentText returns Content[i] as a *mcp.TextContent's Text, failing if the shape is wrong.
func contentText(t *testing.T, res *mcp.CallToolResult, i int) string {
	t.Helper()
	if i >= len(res.Content) {
		t.Fatalf("Content[%d] out of range (len=%d)", i, len(res.Content))
	}
	tc, ok := res.Content[i].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[%d] is %T, want *mcp.TextContent", i, res.Content[i])
	}
	return tc.Text
}

// (1) Canonical call: web_search + {"query":...} -> upstream gets search_query verbatim;
// result is the single canned block with NO appended warning; IsError false.
func TestDispatch_Canonical_NoWarning(t *testing.T) {
	h, st := newDispatchHandler(t)
	res, err := callDispatch(t, h, "web_search", `{"query":"rust async runtime"}`)
	if err != nil {
		t.Fatalf("canonical call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (canonical never errors — FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "rust async runtime" {
		t.Errorf("upstream search_query = %#v, want the verbatim query", got)
	}
	if lastArg(st, "query") != nil {
		t.Errorf("upstream received alias 'query' (should be renamed to search_query)")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1 (no warning for canonical)", len(res.Content))
	}
	if strings.Contains(contentText(t, res, 0), "[web-search-prime-fixer]") {
		t.Errorf("canonical result unexpectedly contains a warning block")
	}
}

// (2) Alias + junk: web_search + {"q":"x","junk":1} -> upstream gets search_query=="x"
// (junk dropped); result is the canned block THEN a warning; IsError false.
func TestDispatch_AliasJunk_WarningAppendedAfter(t *testing.T) {
	h, st := newDispatchHandler(t)
	res, err := callDispatch(t, h, "web_search", `{"q":"x","junk":1}`)
	if err != nil {
		t.Fatalf("alias call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (warnings never set IsError — FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (extracted from alias 'q')", got)
	}
	if lastArg(st, "junk") != nil {
		t.Errorf("upstream received dropped key 'junk' (ToUpstreamArgs must drop it)")
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2 (result THEN warning)", len(res.Content))
	}
	// Results LEAD: content[0] is the canned upstream result, NOT the warning.
	if strings.Contains(contentText(t, res, 0), "[web-search-prime-fixer]") {
		t.Errorf("content[0] is the warning (results must LEAD, warning TRAILS — §11.3)")
	}
	// content[1] is the appended warning; it names the source actually used ("q").
	warn := contentText(t, res, 1)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[1] is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, `"q"`) {
		t.Errorf("warning does not name the used source \"q\": %q", warn)
	}
}

// (3) Bare-string argument: web_search + "x" -> extracted (Source="bare-string"),
// forwarded, result + warning; IsError false.
func TestDispatch_BareString_ExtractedAndWarned(t *testing.T) {
	h, st := newDispatchHandler(t)
	res, err := callDispatch(t, h, "web_search", `"x"`)
	if err != nil {
		t.Fatalf("bare-string call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (bare-string extracted)", got)
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2 (result + warning for non-canonical)", len(res.Content))
	}
	if !strings.Contains(contentText(t, res, 1), "bare-string") {
		t.Errorf("warning does not reflect the bare-string source: %q", contentText(t, res, 1))
	}
}

// (4) Empty object: web_search + {} -> NO upstream call (callCount==0); the result is
// the immediate no-results warning (1 block); IsError false. (PRD §10.1.5, FR-6)
func TestDispatch_Empty_NoUpstreamImmediateWarning(t *testing.T) {
	h, st := newDispatchHandler(t)
	res, err := callDispatch(t, h, "web_search", `{}`)
	if err != nil {
		t.Fatalf("empty call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (FR-6)")
	}
	if n := callCount(st); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (no-query must NOT call upstream — §10.1.5)", n)
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1 (the immediate no-results warning)", len(res.Content))
	}
	warn := contentText(t, res, 0)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("result is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, "could not find a search query") {
		t.Errorf("result is not the no-results warning: %q", warn)
	}
}

// (5) Upstream error: the fake tool returns an error -> the handler returns (nil, err)
// honestly (no synthesized result). PRD §11.1 honest-error rule.
func TestDispatch_UpstreamError_ReturnedHonestly(t *testing.T) {
	h, st := newDispatchHandler(t)
	st.setToolErr(errors.New("z.ai blew up"))
	res, err := callDispatch(t, h, "web_search", `{"query":"x"}`)
	if err == nil {
		t.Fatalf("upstream error not surfaced: err == nil, res=%v", res)
	}
	if res != nil {
		t.Errorf("handler synthesized a result on upstream error (must return nil): %+v", res)
	}
}

// (6) IsError-never-set sweep: across every normalization/warning shape, IsError stays
// false. (The error-return path is excluded — it returns nil, not a result.)
func TestDispatch_IsErrorNeverSet(t *testing.T) {
	shapes := []struct{ name, args string }{
		{"canonical", `{"query":"x"}`},
		{"alias", `{"q":"x"}`},
		{"bare-string", `"x"`},
		{"number", `42`},
		{"nested", `{"input":{"query":"x"}}`},
		{"empty", `{}`},
		{"no-arguments", ``},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			h, _ := newDispatchHandler(t)
			res, err := callDispatch(t, h, "web_search", s.args)
			if err != nil {
				t.Fatalf("%s: unexpected error %v", s.name, err)
			}
			if res.IsError {
				t.Errorf("%s: IsError = true (FR-6: never set for normalization/warning)", s.name)
			}
		})
	}
}
