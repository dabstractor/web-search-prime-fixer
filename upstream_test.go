package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeState records what the fake z.ai received.
type fakeState struct {
	mu       sync.Mutex
	calls    int32 // atomic count of tools/call handled
	lastTool string
	lastArgs map[string]any
}

// newFakeZAI stands up a REAL MCP server ("fake-zai") over httptest that advertises
// web_search_prime, records each tools/call, and returns a canned result. This is
// the in-process substitute for real z.ai: the UpstreamClient's StreamableClientTransport
// connects to srv.URL and performs the REAL initialize handshake + tools/call, so
// the lazy-init, mutex, and callTool-wiring are exercised end-to-end with no network.
// (Proven pattern — see research/upstream-client-design.md §3.)
func newFakeZAI(t *testing.T) (*httptest.Server, *fakeState) {
	t.Helper()
	st := &fakeState{}
	zai := mcp.NewServer(&mcp.Implementation{Name: "fake-zai", Version: "v1"}, nil)
	zai.AddTool(&mcp.Tool{
		Name:        "web_search_prime",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"search_query":{"type":"string"}},"additionalProperties":true}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		atomic.AddInt32(&st.calls, 1)
		st.mu.Lock()
		st.lastTool = req.Params.Name
		_ = json.Unmarshal(req.Params.Arguments, &st.lastArgs)
		st.mu.Unlock()
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: `[{"title":"r","url":"u","content":"c"}]`},
		}}, nil
	})
	h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return zai }, nil)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, st
}

func testCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// TestNewUpstreamHTTPClient pins PRD §11.2: ResponseHeaderTimeout==30s, Timeout==0.
func TestNewUpstreamHTTPClient(t *testing.T) {
	c := newUpstreamHTTPClient()
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport is %T, want *http.Transport", c.Transport)
	}
	if tr.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("ResponseHeaderTimeout = %v, want 30s", tr.ResponseHeaderTimeout)
	}
	if c.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0 (no hard deadline — PRD §11.2)", c.Timeout)
	}
}

// TestUpstreamClient_LazyInitAndCallTool: first callTool lazily creates the session,
// delegates to web_search_prime with the exact args, and returns z.ai's result.
func TestUpstreamClient_LazyInitAndCallTool(t *testing.T) {
	srv, st := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}
	if u.session != nil {
		t.Fatal("session should be nil before first call")
	}

	ctx, cancel := testCtx(t)
	defer cancel()
	args := map[string]any{"search_query": "lunar rover", "location": "US"}
	res, err := u.callTool(ctx, args)
	if err != nil {
		t.Fatalf("callTool: %v", err)
	}
	defer func() { _ = u.session.Close() }()

	if u.session == nil {
		t.Fatal("session should be non-nil after first call (lazy init)")
	}
	if len(res.Content) != 1 {
		t.Fatalf("result Content len = %d, want 1", len(res.Content))
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[0] is %T, want *mcp.TextContent", res.Content[0])
	}
	if tc.Text != `[{"title":"r","url":"u","content":"c"}]` {
		t.Errorf("result text = %q", tc.Text)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (z.ai result returned verbatim)")
	}
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.lastTool != "web_search_prime" {
		t.Errorf("fake z.ai saw tool %q, want web_search_prime", st.lastTool)
	}
	if st.lastArgs["search_query"] != "lunar rover" || st.lastArgs["location"] != "US" {
		t.Errorf("fake z.ai saw args %v, want the exact forwarded args", st.lastArgs)
	}
}

// TestUpstreamClient_LazyReuse: a second callTool reuses the SAME session (no
// re-Connect). Proven by snapshotting the session pointer after the first call.
func TestUpstreamClient_LazyReuse(t *testing.T) {
	srv, st := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}

	ctx, cancel := testCtx(t)
	defer cancel()
	if _, err := u.callTool(ctx, map[string]any{"search_query": "a"}); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = u.session.Close() }()
	first := u.session

	if _, err := u.callTool(ctx, map[string]any{"search_query": "b"}); err != nil {
		t.Fatal(err)
	}
	if u.session != first {
		t.Errorf("session changed after second call (should reuse the lazy session)")
	}
	if got := atomic.LoadInt32(&st.calls); got != 2 {
		t.Errorf("fake z.ai handled %d calls, want 2", got)
	}
}

// TestUpstreamClient_Concurrent: N goroutines call callTool concurrently; under the
// race detector all succeed, no double-init, single shared session. Run with -race.
func TestUpstreamClient_Concurrent(t *testing.T) {
	srv, st := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}

	const n = 16
	var wg sync.WaitGroup
	wg.Add(n)
	ctx, cancel := testCtx(t)
	defer cancel()
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = u.callTool(ctx, map[string]any{"search_query": "x"})
		}()
	}
	close(start)
	wg.Wait()

	if u.session == nil {
		t.Fatal("session nil after concurrent calls")
	}
	defer func() { _ = u.session.Close() }()
	if got := atomic.LoadInt32(&st.calls); got != n {
		t.Errorf("fake z.ai handled %d calls, want %d", got, n)
	}
	// (The race detector verifies no data race on u.session and no double-init.)
}

// TestUpstreamClient_EnsureSessionError: a non-MCP/garbage upstream makes Connect
// fail; callTool propagates the error and leaves u.session nil (retryable).
func TestUpstreamClient_EnsureSessionError(t *testing.T) {
	// A plain HTTP server that is NOT an MCP server -> the initialize handshake fails.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(bad.Close)

	u := &UpstreamClient{upstream: bad.URL, targetTool: "web_search_prime", targetParam: "search_query"}
	ctx, cancel := testCtx(t)
	defer cancel()
	_, err := u.callTool(ctx, map[string]any{"search_query": "x"})
	if err == nil {
		t.Fatal("callTool against a non-MCP upstream should fail")
	}
	if u.session != nil {
		t.Errorf("session should stay nil after a failed init (retryable), got non-nil")
	}
}
