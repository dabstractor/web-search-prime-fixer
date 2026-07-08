package main

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// UpstreamClient owns ONE shared MCP client session to the z.ai upstream and
// delegates tools/call requests to it (PRD §11.1, FR-5). It is the upstream half
// of the normalizing server: where the SDK server handler (P1.M5.T2) receives the
// agent's tools/call, this client sends a single normalized tools/call to z.ai's
// web_search_prime and returns z.ai's result verbatim.
//
// LAZY SHARED SESSION (PRD §11.1): the session (*mcp.ClientSession) is created on
// the FIRST callTool via ensureSession, then reused for every subsequent call. z.ai
// sessions are expected to tolerate concurrent use, so concurrent inbound calls
// share the one session. The session is created lazily (not at server boot) so a
// server with no tools/call traffic never opens an upstream connection, and so the
// first request's context (which carries the inbound Authorization in S2) is the
// one used for the initialize handshake.
//
// MUTEX GUARDING: ensureSession holds u.mu while it checks session==nil and runs
// the (network) initialize handshake via mcp.Client.Connect, so two concurrent
// first-calls cannot both Connect. callTool reads u.session UNDER u.mu (the only
// reader; ensureSession is the only writer) and then calls session.CallTool on that
// LOCAL copy OUTSIDE the mutex, so concurrent calls proceed in parallel once the
// session exists. The SDK detaches the connection context from the Connect context
// (xcontext.Detach, streamable.go:223), so the shared session survives the first
// request's context cancellation.
//
// TIMEOUTS (PRD §11.2): the outbound HTTP is governed by the client context (each
// callTool's ctx; cancellation propagates to z.ai) plus a ~30s response-header
// timeout (newUpstreamHTTPClient sets Transport.ResponseHeaderTimeout). There is NO
// http.Client.Timeout: a non-zero Timeout is a whole-exchange deadline that includes
// reading the response body and would cut off z.ai's long SSE result streams
// (verified: ResponseHeaderTimeout "does not include the time to read the response
// body"). The header timeout detects a dead upstream quickly without bounding the
// body read.
//
// AUTH (PRD §17, S2): this struct (S1) builds the HTTP client WITHOUT Authorization.
// P1.M4.T1.S2 wraps newUpstreamHTTPClient's Transport with a RoundTripper that sets
// Authorization from ctx.Value(authHeaderKey{}) (the inbound header stored by
// main.go's authMiddleware) and rebuilds the session when the auth changes. Until S2
// lands, this client connects to a no-auth fake z.ai (tests) and would be rejected
// by real z.ai.
type UpstreamClient struct {
	mu          sync.Mutex
	session     *mcp.ClientSession
	upstream    string
	targetTool  string
	targetParam string
}

// newUpstreamHTTPClient builds the *http.Client used for the upstream z.ai MCP
// transport (PRD §11.2). It clones http.DefaultTransport (sensible dial/TLS/HTTP2
// defaults and idle-connection pooling) and sets ResponseHeaderTimeout to 30s so a
// dead or slow upstream is detected quickly. It deliberately leaves Client.Timeout
// at its zero value: a non-zero Timeout is a whole-exchange deadline that includes
// reading the response body and would truncate z.ai's long streamed SSE result
// responses (verified: net/http documents ResponseHeaderTimeout as not including
// the time to read the body). The returned Transport is the base that S2 wraps with
// an Authorization-injecting RoundTripper.
func newUpstreamHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

// ensureSession initializes the shared z.ai client session on first use (PRD §11.1).
// It is goroutine-safe: it holds u.mu while checking session==nil and performing the
// initialize handshake, so concurrent first-calls produce exactly one session. If
// the session already exists it returns immediately. If the handshake fails it
// leaves session nil and returns the error, so the next callTool retries (a failed
// Connect closes its partial connection — client.go — and returns a non-nil error).
//
// Connect performs the FULL initialize handshake synchronously (initialize request,
// awaits the response, sends the initialized notification). The SDK detaches the
// connection context from ctx, so the session survives this request's cancellation
// and is reused by later calls. P1.M4.T1.S3 will add the session-expiry re-init
// path around this method; P1.M4.T1.S2 will inject Authorization into the transport.
func (u *UpstreamClient) ensureSession(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.session != nil {
		return nil
	}
	transport := &mcp.StreamableClientTransport{
		Endpoint:   u.upstream,
		HTTPClient: newUpstreamHTTPClient(),
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err // session stays nil -> next call retries
	}
	u.session = sess
	return nil
}

// callTool delegates a single tools/call to z.ai's web_search_prime (PRD §11.1,
// FR-5). It lazily ensures the shared session exists, then calls session.CallTool
// with the caller-built args (extract.go's ToUpstreamArgs output: {targetParam:
// query, ...optionals}) and returns z.ai's *mcp.CallToolResult UNCHANGED (the
// server handler in P1.M5.T2 appends any teaching warning via teach.go's
// appendWarning). ctx is the inbound request's context: its cancellation propagates
// to the upstream call, but the shared session persists across requests (the SDK
// detaches the connection context). The CallTool runs OUTSIDE u.mu so concurrent
// calls proceed in parallel; u.session is read under u.mu for race-safety.
func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	if err := u.ensureSession(ctx); err != nil {
		return nil, err
	}
	u.mu.Lock()
	sess := u.session
	u.mu.Unlock()
	return sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      u.targetTool,
		Arguments: args,
	})
}
