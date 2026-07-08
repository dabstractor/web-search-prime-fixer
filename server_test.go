package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fixedAuthTripper is a test-only http.RoundTripper that sets a fixed Authorization
// header on every outbound request — mirroring how a real agent's MCP client sends
// its API key to OUR server. (It is deliberately NOT authInjector: authInjector
// reads from context for OUR upstream side; the CLIENT side needs a fixed header.)
type fixedAuthTripper struct {
	base http.RoundTripper
	auth string
}

func (f *fixedAuthTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", f.auth)
	return f.base.RoundTrip(req)
}

// newE2E stands up the full client→server→upstream→fake-z.ai stack in-process.
// It reuses newFakeZAI (fake z.ai), builds OUR server with registerTools applied
// (the upstream pointed at the fake), mounts it via StreamableHTTPHandler +
// authMiddleware exactly like main.go, and connects an SDK client that carries a
// fixed Authorization. logw captures logs (for the auth-not-logged test); pass
// io.Discard otherwise. Returns the client session, the fake state, and cfg.
//
// Each call gets a fresh stack (own fake z.ai, own server, own session) for
// isolation; t.Cleanup closes the client session, the httptest servers, and the ctx.
func newE2E(t *testing.T, logw io.Writer) (*mcp.ClientSession, *fakeState, Config) {
	t.Helper()
	zaiSrv, st := newFakeZAI(t) // reuse from upstream_test.go

	cfg := DefaultConfig()
	cfg.Upstream = zaiSrv.URL // delegate to the fake z.ai, NOT the real one
	log := newLogger(logw, "debug")

	// OUR server + the wired dispatch handler (server.go from P1.M5.T2.S1).
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)
	upstream := newUpstreamClient(cfg, log)
	registerTools(server, cfg, upstream, log)

	// Mount OUR server EXACTLY like main.go: SDK handler wrapped in authMiddleware
	// so the inbound Authorization is threaded into the handler ctx.
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ourSrv := httptest.NewServer(authMiddleware(sdkHandler))
	t.Cleanup(ourSrv.Close)

	// SDK client carrying a fixed Authorization to OUR server (like a real agent).
	// DisableStandaloneSSE: our server sends no server-initiated messages (no
	// tool-list-changed, etc.), so the optional persistent SSE GET stream the SDK
	// opens after initialize adds nothing under test — and it keeps a long-lived
	// connection that blocks httptest.Server.Close on teardown. Disabling it is the
	// spec-sanctioned, MCP-optional path; request/response over POST (everything
	// these cases exercise) is identical.
	//
	// A dedicated *http.Transport (not http.DefaultTransport) backs the client so
	// CloseIdleConnections can drain the keep-alive pool in cleanup — otherwise the
	// pooled idle POST connections keep ourSrv's Close waiting (the SDK detaches
	// its connection context from the request ctx, so cancelling the test ctx does
	// not close them).
	tripper := &fixedAuthTripper{
		base: &http.Transport{MaxIdleConns: 10},
		auth: "Bearer e2e-key",
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:             ourSrv.URL,
		HTTPClient:           &http.Client{Transport: tripper},
		DisableStandaloneSSE: true,
	}
	t.Cleanup(tripper.base.(*http.Transport).CloseIdleConnections)
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test-client", Version: "test"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect to our server: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	// Tear down the upstream's LAZY client session to the fake z.ai, BEFORE the
	// fake's httptest.Server.Close runs (newFakeZAI registered srv.Close first, so
	// LIFO cleanup order tears this session down first). Without it, the upstream's
	// standalone SSE stream to the fake keeps an active connection and the fake's
	// Close blocks. Mirrors dispatch_test.go's upstream.session close. Read under
	// u.mu (ensureSession is the writer); nil when no tools/call reached it (e.g.
	// the empty case never opens one).
	t.Cleanup(func() {
		upstream.mu.Lock()
		s := upstream.session
		upstream.mu.Unlock()
		if s != nil {
			_ = s.Close()
		}
	})
	return sess, st, cfg
}

// callWeb invokes sess.CallTool(web_search, arguments) with a timeout. arguments
// may be any JSON-able value (object, bare string, array, ...).
func callWeb(t *testing.T, sess *mcp.ClientSession, arguments any) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sess.CallTool(ctx, &mcp.CallToolParams{Name: "web_search", Arguments: arguments})
}

// contentText is REUSED from dispatch_test.go (same package, identical semantics:
// Content[i] as a *mcp.TextContent's Text). It is NOT redeclared here.

// Race-safe fake-state readers (st fields are written under st.mu; calls is int32).
// lastArg/callCount from dispatch_test.go already cover the arg/count reads under
// the same names; these add the tool/auth readers this suite needs.
func zaiCalls(st *fakeState) int32 { return atomic.LoadInt32(&st.calls) }
func zaiTool(st *fakeState) string { st.mu.Lock(); defer st.mu.Unlock(); return st.lastTool }
func zaiAuth(st *fakeState) string { st.mu.Lock(); defer st.mu.Unlock(); return st.lastAuth }
func zaiArg(st *fakeState, k string) any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.lastArgs[k]
}

// (1) Canonical: web_search + {"query":"x"} -> results only, no warning.
func TestE2E_Canonical_NoWarning(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{"query": "rust async runtime"})
	if err != nil {
		t.Fatalf("canonical call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d, want 1 (no warning for canonical)", len(res.Content))
	}
	if got := contentText(t, res, 0); got != `[{"title":"r","url":"u","content":"c"}]` {
		t.Errorf("Content[0]=%q, want the canned z.ai result", got)
	}
	if zaiTool(st) != "web_search_prime" {
		t.Errorf("upstream tool=%q, want web_search_prime", zaiTool(st))
	}
	if got := zaiArg(st, "search_query"); got != "rust async runtime" {
		t.Errorf("upstream search_query=%#v, want the verbatim query", got)
	}
	if zaiArg(st, "query") != nil {
		t.Errorf("upstream received alias 'query' (should be renamed to search_query)")
	}
	if n := zaiCalls(st); n != 1 {
		t.Errorf("upstream calls=%d, want 1", n)
	}
}

// (2) Alias + junk: web_search + {"q":"x","junk":1} -> results THEN warning.
func TestE2E_AliasJunk_WarningAppendedAfter(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{"q": "x", "junk": 1})
	if err != nil {
		t.Fatalf("alias call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content)=%d, want 2 (result THEN warning)", len(res.Content))
	}
	if got := contentText(t, res, 0); got != `[{"title":"r","url":"u","content":"c"}]` {
		t.Errorf("Content[0]=%q, want the canned result LEADING (FR-6 results lead)", got)
	}
	warn := contentText(t, res, 1)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("Content[1] is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, "Results are above.") {
		t.Errorf("warning lacks 'Results are above.' (results-lead invariant): %q", warn)
	}
	if !strings.Contains(warn, `"q"`) {
		t.Errorf("warning does not name the used source \"q\": %q", warn)
	}
	if got := zaiArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query=%#v, want \"x\"", got)
	}
	if zaiArg(st, "junk") != nil {
		t.Errorf("upstream received dropped key 'junk' (ToUpstreamArgs must drop it)")
	}
}

// (3) Bare-string / nested / array argument shapes -> extracted + warned.
func TestE2E_NonCanonical_ArgShapes(t *testing.T) {
	shapes := []struct {
		name      string
		arguments any
		wantSub   string // source label embedded in the warning
	}{
		{"bare-string", "x", "bare-string"},
		{"nested", map[string]any{"input": map[string]any{"query": "x"}}, "nested"},
		{"array", []any{"x"}, "array"},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			sess, st, _ := newE2E(t, io.Discard)
			res, err := callWeb(t, sess, s.arguments)
			if err != nil {
				t.Fatalf("%s call: %v", s.name, err)
			}
			if res.IsError {
				t.Errorf("%s: IsError=true, want false (FR-6)", s.name)
			}
			if got := zaiArg(st, "search_query"); got != "x" {
				t.Errorf("%s: upstream search_query=%#v, want \"x\"", s.name, got)
			}
			if len(res.Content) != 2 {
				t.Fatalf("%s: len(Content)=%d, want 2 (result + warning)", s.name, len(res.Content))
			}
			warn := contentText(t, res, 1)
			if !strings.Contains(warn, s.wantSub) {
				t.Errorf("%s: warning does not reflect source %q: %q", s.name, s.wantSub, warn)
			}
		})
	}
}

// (4) Empty {}: NO upstream call; immediate no-results warning; IsError false.
func TestE2E_Empty_NoUpstreamImmediateWarning(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{})
	if err != nil {
		t.Fatalf("empty call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if n := zaiCalls(st); n != 0 {
		t.Errorf("upstream calls=%d, want 0 (no-query must NOT call upstream — §10.1.5)", n)
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d, want 1 (the immediate no-results warning)", len(res.Content))
	}
	if got := contentText(t, res, 0); !strings.Contains(got, "could not find a search query") {
		t.Errorf("Content[0] is not the no-results warning: %q", got)
	}
}

// (5) tools/list advertises exactly cfg.Tools; only web_search has a full
// description; never web_search_prime.
func TestE2E_ToolsList(t *testing.T) {
	sess, _, cfg := newE2E(t, io.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != len(cfg.Tools) {
		t.Fatalf("len(Tools)=%d, want %d (exactly cfg.Tools)", len(res.Tools), len(cfg.Tools))
	}
	for i, want := range cfg.Tools {
		if res.Tools[i].Name != want {
			t.Errorf("Tools[%d].Name=%q, want %q", i, res.Tools[i].Name, want)
		}
	}
	// The canonical tool (web_search) carries the full description (names "query").
	var canonical *mcp.Tool
	for _, tl := range res.Tools {
		if tl.Name == cfg.CanonicalTool {
			canonical = tl
			break
		}
	}
	if canonical == nil {
		t.Fatalf("canonical tool %q not advertised", cfg.CanonicalTool)
	}
	if !strings.Contains(canonical.Description, cfg.CanonicalParam) {
		t.Errorf("canonical Description=%q lacks the canonical param %q", canonical.Description, cfg.CanonicalParam)
	}
	// Never advertise z.ai-branded names.
	for _, tl := range res.Tools {
		if tl.Name == cfg.TargetTool {
			t.Errorf("tools/list advertised z.ai-branded name %q (forbidden — PRD §9.3)", cfg.TargetTool)
		}
	}
}

// (6) Upstream session-expiry -> one transparent re-init, then success.
func TestE2E_SessionExpiry_TransparentReinit(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)

	// First call: lazily establishes the upstream session and succeeds.
	if res, err := callWeb(t, sess, map[string]any{"query": "first"}); err != nil {
		t.Fatalf("first call: %v", err)
	} else if res.IsError {
		t.Fatal("first call: IsError=true")
	}

	// Evict the live z.ai session OUR UpstreamClient holds.
	st.expire()

	// Second call: transparently re-inits ONCE and retries; the client sees success.
	res2, err := callWeb(t, sess, map[string]any{"query": "second"})
	if err != nil {
		t.Fatalf("second call (after expiry): %v (expected transparent re-init + retry)", err)
	}
	if res2 == nil || res2.IsError {
		t.Fatal("second call: expected a real, non-error result after re-init")
	}
	// st.calls counts successful tool-handler calls: first + retry == 2.
	// The 404'd attempt is rejected before the fake's handler, so NOT counted.
	if n := zaiCalls(st); n != 2 {
		t.Errorf("upstream calls=%d, want 2 (first + retry; the 404'd attempt is not a tool call)", n)
	}
}

// (7) Auth: the Authorization header reached the fake z.ai verbatim AND was never
// logged. (PRD §17, FR-7; the item contract: "Assert auth header reached the fake
// z.ai and was never logged.")
func TestE2E_AuthForwardedAndNotLogged(t *testing.T) {
	var buf bytes.Buffer
	sess, st, _ := newE2E(t, &buf) // capture logs
	if _, err := callWeb(t, sess, map[string]any{"query": "x"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	// Reached the fake z.ai verbatim.
	if got := zaiAuth(st); got != "Bearer e2e-key" {
		t.Errorf("fake z.ai received Authorization %q, want %q (verbatim — PRD §17, FR-7)", got, "Bearer e2e-key")
	}
	// Never logged: the captured log stream must not contain the credential.
	if strings.Contains(buf.String(), "e2e-key") {
		t.Errorf("credential 'e2e-key' appears in the log (must never be logged — PRD §15/§17):\n%s", buf.String())
	}
}
