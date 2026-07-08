package main

import (
	"context"
	"errors"
	"fmt"
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
	log         *logger // nil-safe: if nil, logUpstreamError is a no-op (PRD §15)
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

// connectLocked builds a fresh upstream session: a new StreamableClientTransport
// (whose *http.Client is S1's newUpstreamHTTPClient, wrapping S2's authInjector)
// and a full Client.Connect (initialize → capture NEW Mcp-Session-Id →
// notifications/initialized; client.go:255-292). It ASSUMES the caller holds u.mu
// (so it is safe to set u.session). On failure it leaves u.session nil so the next
// caller retries via ensureSession. Shared by ensureSession (lazy first-init) and
// reinitSession (expiry re-init) so the two paths build the session identically.
func (u *UpstreamClient) connectLocked(ctx context.Context) error {
	transport := &mcp.StreamableClientTransport{
		Endpoint:   u.upstream,
		HTTPClient: newUpstreamHTTPClient(),
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return err // u.session stays nil -> next call retries
	}
	u.session = sess
	return nil
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
// and is reused by later calls. The actual Connect is in connectLocked (shared with
// reinitSession).
func (u *UpstreamClient) ensureSession(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.session != nil {
		return nil
	}
	return u.connectLocked(ctx)
}

// reinitSession rebuilds the shared session after an ErrSessionMissing (PRD §11.1).
// It is concurrency-safe via a COMPARE-AND-CLOSE: under u.mu, if some OTHER caller
// already re-init'd (u.session != nil && u.session != dead), it returns that fresh
// session and does nothing; otherwise it closes the dead session, nils it, and runs
// connectLocked once. `dead` is the session pointer the caller observed fail — it is
// the CAS token that decides who owns the re-init.
//
// CONCURRENCY TRACE (two concurrent callers A, B, each holding snapshot `d` of the
// dead session): A acquires u.mu first; u.session == d → not the early-return branch
// → A closes d, nils u.session, connectLocked → session2; returns session2. B then
// acquires u.mu; u.session == session2 != d (and != nil) → early-return → B reuses
// session2. A fresh caller C: ensureSession finds session2 non-nil → reuses it. If
// A's connectLocked FAILS: u.session is left nil; B then sees u.session == nil (not
// the early-return branch) and retries connectLocked — bounded by the number of
// concurrent callers. The lock is held across the network Connect, mirroring
// ensureSession (session-expiry is rare, so the serialization cost is negligible).
//
// Close() skips the redundant DELETE when the failure wraps ErrSessionMissing
// (streamable.go:2236), so closing the dead session is cheap. Auth needs no code
// here: the rebuilt transport's authInjector (S2) reads the current request's auth
// from ctx, which reaches connectLocked through callTool unchanged.
func (u *UpstreamClient) reinitSession(ctx context.Context, dead *mcp.ClientSession) (*mcp.ClientSession, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	// Another caller already re-init'd: reuse its fresh session.
	if u.session != nil && u.session != dead {
		return u.session, nil
	}
	// We own the re-init. Close the dead session (cheap: Close skips DELETE on
	// ErrSessionMissing), nil it, then build a fresh one.
	if u.session != nil {
		_ = u.session.Close()
	}
	u.session = nil
	if err := u.connectLocked(ctx); err != nil {
		return nil, err // u.session stays nil -> next call retries via ensureSession
	}
	return u.session, nil
}

// callTool delegates a single tools/call to z.ai's web_search_prime (PRD §11.1,
// §11.3, FR-5). It lazily ensures the shared session, then calls session.CallTool
// with the caller-built args (extract.go's ToUpstreamArgs: {targetParam: query,
// ...optionals}) and returns z.ai's *mcp.CallToolResult UNCHANGED.
//
// SESSION-EXPIRY RESILIENCE (PRD §11.1): if the first CallTool returns an error
// wrapping mcp.ErrSessionMissing (a server-side 404 / invalid-session — the SDK
// maps the 404 to that terminal sentinel), callTool re-initializes the shared
// session ONCE transparently (reinitSession: compare-and-close under u.mu, then a
// fresh transport + Client.Connect) and retries the single CallTool. If the retry
// ALSO fails — or the re-init itself fails — it surfaces the upstream error
// VERBATIM. It NEVER synthesizes a *mcp.CallToolResult: the failure paths
// (ensureSession error, non-ErrSessionMissing error, re-init error, retry error)
// all return (nil, err). Exactly ONE re-init + ONE retry per inbound call; never
// loop on a 404.
//
// HONEST ERROR RULE (PRD §11.1, FR-5/FR-6): only ErrSessionMissing triggers a
// re-init. A transient 5xx/429 is retried by the transport itself (no re-init); any
// other error (a JSON-RPC error from a failing tool, a decode error, …) is surfaced
// verbatim WITHOUT re-init and WITHOUT an upstream_error log line (M5.T2's delegate
// event already carries upstream status). When usage was canonical, the result is
// returned with no added warning; when it was not, M5.T2 appends the warning AFTER
// this result via teach.appendWarning.
//
// RESULT PRESERVATION (PRD §8, §11.3): z.ai's result Content is a JSON-encoded
// STRING (a stringified array), not an object. callTool returns session.CallTool's
// *mcp.CallToolResult UNCHANGED on both the happy path (res) and the retry-success
// path (res2): it never re-parses, reorders, or truncates Content, and never sets
// IsError. The warning is appended later (M5.T2 + teach.go) to this same result.
//
// TIMEOUTS / CANCELLATION (PRD §11.2): ctx is the inbound request's context. Its
// cancellation propagates to the upstream CallTool (the SDK propagates CallTool's
// ctx to the outbound POST's req.Context() — verified in S2). The shared session
// itself survives a single request's cancellation (the SDK xcontext.Detach's the
// connection context), so re-init is only triggered by a server-side expiry, not by
// a cancelled client. A ~30s response-header timeout (S1's newUpstreamHTTPClient)
// detects a dead upstream quickly without bounding the SSE body read.
func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
	if err := u.ensureSession(ctx); err != nil {
		return nil, err
	}
	// Snapshot the session under the lock; CallTool runs OUTSIDE the lock so
	// concurrent calls proceed in parallel (u.session is read under u.mu for
	// race-safety; ensureSession/reinitSession are the only writers).
	u.mu.Lock()
	sess := u.session
	u.mu.Unlock()

	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      u.targetTool,
		Arguments: args,
	})
	if err == nil {
		return res, nil // happy path: result UNCHANGED
	}
	if !errors.Is(err, mcp.ErrSessionMissing) {
		// Not a session-expiry signal (transient 5xx/429 already retried by the
		// transport; or a JSON-RPC/decode error). Surface it VERBATIM — no re-init,
		// no synthesis. (PRD §11.1 honest-error rule.)
		return nil, err
	}

	// Session-expiry (404 / invalid-session): re-initialize ONCE and retry. (PRD §11.1)
	u.logUpstreamError(u.targetTool, "session_expired", 1)
	sess2, rerr := u.reinitSession(ctx, sess)
	if rerr != nil {
		u.logUpstreamError(u.targetTool, "reinit_failed", 1)
		return nil, fmt.Errorf("upstream session expired; re-initialize failed: %w", rerr)
	}
	res2, err2 := sess2.CallTool(ctx, &mcp.CallToolParams{
		Name:      u.targetTool,
		Arguments: args,
	})
	if err2 != nil {
		// Retry also failed: surface the upstream error HONESTLY (no synthesis).
		return nil, err2
	}
	return res2, nil // retry success: result UNCHANGED
}

// logUpstreamError emits the PRD §15 "upstream_error" event at warn level. It is
// NIL-SAFE: when u.log == nil (all S1/S2 tests, which construct UpstreamClient
// without a logger) it is a no-op, so those tests stay green unchanged. The fields
// (called_tool, status, reinit_attempts) match PRD §15. S3 owns this emission
// because the re-init attempt count is known only inside callTool's retry loop.
// It is called on: expiry detected (status="session_expired", attempts=1) and
// re-init Connect failure (status="reinit_failed", attempts=1). A plain non-expiry
// upstream error is surfaced verbatim and is NOT logged here (M5.T2's delegate
// event already carries upstream status — S3 does not double-log it).
func (u *UpstreamClient) logUpstreamError(calledTool, status string, attempts int) {
	if u.log == nil {
		return
	}
	u.log.log("warn", "upstream_error", map[string]any{
		"called_tool":     calledTool,
		"status":          status,
		"reinit_attempts": attempts,
	})
}
