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
// AUTH (PRD §17, FR-7): the outbound transport (built in newUpstreamHTTPClient)
// is wrapped by an authInjector that copies the inbound client's Authorization
// header from the request context onto every z.ai request, verbatim. The auth
// value reaches the outbound request because the SDK propagates CallTool's ctx to
// the outbound POST's req.Context() (verified; see authInjector's doc comment),
// and xcontext.Detach preserves context values so the SSE GET/DELETE carry it too.
// The credential is NEVER assigned to a field of this struct, NEVER logged, and
// NEVER transformed — it is a transient context value. P1.M4.T1.S3 (re-init) needs
// no auth-specific code: its rebuilt transport reads the current request's auth
// from context for free.
type UpstreamClient struct {
	mu          sync.Mutex
	session     *mcp.ClientSession
	upstream    string
	targetTool  string
	targetParam string
}

// newUpstreamHTTPClient builds the *http.Client used for the upstream z.ai MCP
// transport (PRD §11.2). It clones http.DefaultTransport (sensible dial/TLS/HTTP2
// defaults and idle-connection pooling), sets ResponseHeaderTimeout to 30s so a
// dead or slow upstream is detected quickly, and leaves Client.Timeout at its zero
// value (a non-zero Timeout is a whole-exchange deadline that includes reading the
// response body and would truncate z.ai's long streamed SSE result responses —
// verified: net/http documents ResponseHeaderTimeout as not including the time to
// read the body). It wraps that base transport with an authInjector so every
// outbound z.ai request carries the inbound client's verbatim Authorization
// header (PRD §17, FR-7). The auth value is read from the request context inside
// authInjector.RoundTrip; it is never stored on the client or logged.
func newUpstreamHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: &authInjector{base: tr}}
}

// authInjector is an http.RoundTripper that copies the inbound client's
// Authorization header onto every outbound z.ai HTTP request, VERBATIM (PRD §17,
// FR-7). It is the forwarding half of the auth seam opened by main.go's
// authMiddleware (which stores the inbound Authorization in the request context
// under authHeaderKey) and closed by the server handler passing that same context
// to UpstreamClient.callTool.
//
// CONTEXT THREADING (verified against the Go MCP SDK v1.6.1): the per-call ctx
// passed to ClientSession.CallTool reaches the outbound POST's req.Context()
// unchanged (CallTool client.go:990 → handleSend shared.go:136 → call
// transport.go:222 → jsonrpc2 conn.Call/Write conn.go:273/306/718 → streamable
// Write streamable.go:1788 → http.NewRequestWithContext(ctx) streamable.go:1806).
// The connection context is xcontext.Detach'd (streamable.go:1601), which
// PRESERVES context values (xcontext.go:34 delegates Value to the parent), so the
// standalone SSE GET (streamable.go:2206) and the DELETE-on-close
// (streamable.go:2239) carry authHeaderKey too. Therefore a SINGLE
// context-reading authInjector injects the verbatim Authorization on every
// outbound z.ai request with NO mutable shared state.
//
// NEVER LOGGED / NEVER STORED (PRD §15, §17): the credential is read transiently
// from req.Context() inside RoundTrip and is never assigned to any field of
// UpstreamClient (see TestUpstreamClient_AuthNotRetained). UpstreamClient does
// not log the credential; the delegate log event (emitted by the server handler
// in P1.M5.T2) carries no headers (PRD §15), and redactHeaders (logger.go)
// replaces any Authorization value with "<redacted>" should one ever be logged.
type authInjector struct {
	base http.RoundTripper
}

// RoundTrip implements http.RoundTripper. It forwards the inbound client's
// Authorization header VERBATIM: when authFromContext(req.Context()) is non-empty
// it sets req.Header "Authorization" to that exact value, then delegates to the
// base transport (which owns ResponseHeaderTimeout, TLS, HTTP/2, and connection
// pooling). When the context carries no Authorization, the request is forwarded
// unchanged — we never fabricate or strip a credential. Setting a header directly
// on req is safe here because the SDK builds each outbound request fresh via
// http.NewRequestWithContext (never reused/cached); the SDK's own transports do
// the same (streamable.go:1946).
func (a *authInjector) RoundTrip(req *http.Request) (*http.Response, error) {
	if auth := authFromContext(req.Context()); auth != "" {
		req.Header.Set("Authorization", auth)
	}
	return a.base.RoundTrip(req)
}

// authFromContext returns the inbound Authorization header threaded through the
// request context by main.go's authMiddleware, or "" if none is present. It
// REUSES authHeaderKey (the key the middleware writes under) — it does not define
// a new context key. Returning "" (not redacted, not transformed) is the
// verbatim-forward contract (PRD §17, FR-7).
func authFromContext(ctx context.Context) string {
	v, _ := ctx.Value(authHeaderKey{}).(string)
	return v
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
