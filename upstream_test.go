package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	lastAuth string // S2: the Authorization header the fake z.ai received (verbatim-forward proof)
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
	// S2: record the inbound Authorization the fake z.ai RECEIVES (initialize POST,
	// tools/call POST, and SSE GET all pass through here) so tests can assert the
	// UpstreamClient forwarded the client's header verbatim (PRD §17, FR-7).
	recording := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st.mu.Lock()
		st.lastAuth = r.Header.Get("Authorization")
		st.mu.Unlock()
		h.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(recording)
	t.Cleanup(srv.Close)
	return srv, st
}

func testCtx(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 10*time.Second)
}

// TestNewUpstreamHTTPClient pins PRD §11.2: ResponseHeaderTimeout==30s, Timeout==0.
// S2: the client's Transport is now an *authInjector whose base is the *http.Transport.
func TestNewUpstreamHTTPClient(t *testing.T) {
	c := newUpstreamHTTPClient()
	ai, ok := c.Transport.(*authInjector)
	if !ok {
		t.Fatalf("Transport is %T, want *authInjector", c.Transport)
	}
	tr, ok := ai.base.(*http.Transport)
	if !ok {
		t.Fatalf("authInjector.base is %T, want *http.Transport", ai.base)
	}
	if tr.ResponseHeaderTimeout != 30*time.Second {
		t.Errorf("base ResponseHeaderTimeout = %v, want 30s", tr.ResponseHeaderTimeout)
	}
	if c.Timeout != 0 {
		t.Errorf("Client.Timeout = %v, want 0 (no hard deadline — PRD §11.2)", c.Timeout)
	}
}

// recordingTripper is a base http.RoundTripper for unit-testing authInjector: it
// captures the outbound request (after authInjector has injected the header) and
// returns a trivial 200 with no body.
type recordingTripper struct {
	got *http.Request
}

func (r *recordingTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

// TestAuthInjector_ContextThreading unit-tests the RoundTripper: auth present →
// Authorization set verbatim on the base's request; auth absent → header left unset
// (never fabricate). Proves PRD §17/FR-7 verbatim-forward without the full MCP stack.
func TestAuthInjector_ContextThreading(t *testing.T) {
	t.Run("auth present is set verbatim", func(t *testing.T) {
		rec := &recordingTripper{}
		ai := &authInjector{base: rec}
		req := httptest.NewRequest(http.MethodPost, "https://z.ai/mcp", nil)
		req = req.WithContext(context.WithValue(req.Context(), authHeaderKey{}, "Bearer secret-xyz"))
		if _, err := ai.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if got := rec.got.Header.Get("Authorization"); got != "Bearer secret-xyz" {
			t.Errorf("Authorization = %q, want %q (verbatim)", got, "Bearer secret-xyz")
		}
	})
	t.Run("auth absent leaves header unset", func(t *testing.T) {
		rec := &recordingTripper{}
		ai := &authInjector{base: rec}
		req := httptest.NewRequest(http.MethodPost, "https://z.ai/mcp", nil) // no authHeaderKey
		if _, err := ai.RoundTrip(req); err != nil {
			t.Fatalf("RoundTrip: %v", err)
		}
		if got := rec.got.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization = %q, want empty (never fabricate)", got)
		}
	})
}

// TestUpstreamClient_AuthForwarded: a callTool whose ctx carries authHeaderKey makes
// the auth-recording fake z.ai observe that EXACT Authorization value (PRD §17, FR-7).
func TestUpstreamClient_AuthForwarded(t *testing.T) {
	srv, st := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}

	ctx, cancel := testCtx(t)
	defer cancel()
	const secret = "Bearer test-key-123"
	ctx = context.WithValue(ctx, authHeaderKey{}, secret)

	if _, err := u.callTool(ctx, map[string]any{"search_query": "lunar rover"}); err != nil {
		t.Fatalf("callTool: %v", err)
	}
	defer func() { _ = u.session.Close() }()

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.lastAuth != secret {
		t.Errorf("fake z.ai received Authorization %q, want %q (verbatim forward — PRD §17, FR-7)",
			st.lastAuth, secret)
	}
}

// TestUpstreamClient_AuthNotRetained enforces PRD §17 "never store": after a call
// made with a known secret, NO credential-named field exists on UpstreamClient and
// NO existing string field holds the secret value.
//
// NOTE on reflect: UpstreamClient's fields are ALL unexported (mu/session/upstream/
// targetTool/targetParam), so reflect Value.Interface()/CanInterface() CANNOT read
// their values (CanInterface()==false for all of them; Interface() would panic). A
// naive reflect value-walk therefore skips every field and passes trivially. We
// enforce "never stored" two ways that DO work from the same package:
//
//	(1) reflect over field NAMES only (no value read, no panic) -> assert no field is
//	    named like a credential (catches a future regression that ADDS an auth field).
//	(2) direct same-package access of the known string fields -> assert none holds the
//	    secret value (catches storing it in an existing field).
func TestUpstreamClient_AuthNotRetained(t *testing.T) {
	srv, _ := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}

	ctx, cancel := testCtx(t)
	defer cancel()
	const secret = "Bearer never-stored-456"
	ctx = context.WithValue(ctx, authHeaderKey{}, secret)
	if _, err := u.callTool(ctx, map[string]any{"search_query": "x"}); err != nil {
		t.Fatalf("callTool: %v", err)
	}
	defer func() { _ = u.session.Close() }()

	// (1) No credential-named field exists on UpstreamClient (PRD §17: hold no key).
	denied := map[string]bool{
		"auth": true, "authheader": true, "authorization": true,
		"key": true, "apikey": true, "credential": true, "token": true,
	}
	rt := reflect.TypeOf(UpstreamClient{})
	for i := 0; i < rt.NumField(); i++ {
		if denied[strings.ToLower(rt.Field(i).Name)] {
			t.Errorf("UpstreamClient has credential-named field %q — PRD §17 forbids storing auth",
				rt.Field(i).Name)
		}
	}
	// (2) The existing string fields do not retain the secret value (same-package access).
	if u.upstream == secret || u.targetTool == secret || u.targetParam == secret {
		t.Errorf("a UpstreamClient string field retained the credential (%q) — PRD §17 forbids storing it", secret)
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
