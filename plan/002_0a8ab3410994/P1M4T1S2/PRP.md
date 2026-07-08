name: "P1.M4.T1.S2 — Authorization header threading via context + RoundTripper (PRD §11.1, §17, FR-7)"
description: |

  THREAD the inbound client's `Authorization` header verbatim onto every
  outbound z.ai HTTP request, without ever reading/logging/storing the
  credential (PRD §11.1, §17, FR-7). The mechanism is an `http.RoundTripper`
  named `authInjector` that wraps the base `*http.Transport` already built by
  S1's `newUpstreamHTTPClient()`, and a package-private helper
  `authFromContext(ctx) string`.

  VERIFIED DESIGN (see research/auth-threading.md): the SDK propagates the
  per-call `ctx` from `CallTool(ctx, params)` UNCHANGED all the way to the
  outbound POST's `req.Context()` (chain: CallTool client.go:990 → handleSend
  shared.go:136 → call transport.go:222 → conn.Call jsonrpc2/conn.go:273 →
  write conn.go:306/718 → streamable Write streamable.go:1788 →
  http.NewRequestWithContext(ctx) streamable.go:1806). And the connection
  context `xcontext.Detach(ctx)` (streamable.go:1601) PRESERVES context VALUES
  (xcontext.go:34 delegates `Value` to the parent), so the standalone SSE GET
  (streamable.go:2206) and DELETE (streamable.go:2239) ALSO carry
  `authHeaderKey`. Therefore the `authInjector` reads the credential from
  `req.Context().Value(authHeaderKey{})` in `RoundTrip` and sets it verbatim
  onto the outbound request. This is TRUE per-call threading with NO mutable
  shared state (race-free under `-race`), and the credential NEVER becomes a
  struct field (most faithful to PRD §17 "never read, log, or store it").

  The `authHeaderKey struct{}` + `authMiddleware` ALREADY EXIST in main.go
  (P1.M1.T2.S2 — DONE): the middleware stores `r.Header.Get("Authorization")`
  under `authHeaderKey{}` in the request context, and the SDK passes
  `req.Context()` into the session (streamable.go:491). **S2 REUSES
  `authHeaderKey`; it does NOT define a new key** (the item contract's "e.g.
  `type authKey struct{}`" is only an example — a new key would never find the
  value the middleware stored). S2 ADDS the reader side in upstream.go only;
  **S2 does NOT touch main.go.**

  SCOPE (all in `package main`):
  1. CREATE in `upstream.go`: `type authInjector struct { base http.RoundTripper }`
     + `func (a *authInjector) RoundTrip(req *http.Request) (*http.Response, error)`
     (sets `Authorization` from `authFromContext(req.Context())` when non-empty,
     then delegates to `a.base.RoundTrip`); `func authFromContext(ctx
     context.Context) string` (returns `v, _ := ctx.Value(authHeaderKey{}).(string)`).
  2. MODIFY in `upstream.go`: `newUpstreamHTTPClient()` to wrap the S1 base
     transport — `&http.Client{Transport: &authInjector{base: tr}}` (the
     `*http.Transport` with `ResponseHeaderTimeout=30s` becomes the `base`;
     `Timeout` stays 0). The function SIGNATURE is unchanged.
  3. UPDATE Mode-A doc comments: `authInjector`/`authFromContext` (new) document
     the verbatim-forward rule, the never-log invariant, and the
     context-threading pattern; `newUpstreamHTTPClient`'s comment (S1 said "the
     base that S2 wraps") now states it DOES the wrapping; `UpstreamClient`'s
     S1 "AUTH (PRD §17, S2)" comment is updated to "auth is wired via
     authInjector".
  4. MODIFY `upstream_test.go`: update `TestNewUpstreamHTTPClient` to UNWRAP the
     `authInjector` (`c.Transport.(*authInjector).base.(*http.Transport)` →
     `ResponseHeaderTimeout==30s`, `Timeout==0`); EXTEND `newFakeZAI`/`fakeState`
     with a recording wrapper that captures the inbound `Authorization` the fake
     z.ai receives; ADD `TestAuthInjector_ContextThreading` (unit test the
     RoundTripper: auth present → header set; auth absent → header not set),
     `TestUpstreamClient_AuthForwarded` (e2e: ctx carries
     `authHeaderKey`="Bearer ..." → fake records it verbatim), and
     `TestUpstreamClient_AuthNotRetained` (reflect over `UpstreamClient` fields
     after a call with a known secret → NONE equals the secret → §17 "never
     store").

  No other file is touched. `go.mod` gains ZERO requires (only `context` is newly
  imported into upstream.go; it is stdlib). NO handler wiring (M5.T2's job). NO
  session-expiry re-init (S3's job — and S3 needs NO auth field: its re-init
  runs inside a callTool whose ctx already carries `authHeaderKey`, so the new
  transport's authInjector reads current auth for free).

---

## Goal

**Feature Goal**: Make every outbound HTTP request from the `UpstreamClient` to
z.ai carry the inbound client's `Authorization` header VERBATIM (PRD §11.1, FR-7,
§17), so real z.ai accepts the delegated `tools/call`. The credential must be
forwarded, never transformed; never logged; never stored in client state. The
mechanism must be race-free under concurrent `callTool` calls (S1's `-race`
tests stay green) and must require NO changes to S1's `ensureSession`,
`callTool`, or the `UpstreamClient` struct.

**Deliverable**: Two files MODIFIED at the repo root (both `package main`):
1. **MODIFY** `upstream.go` — add `authInjector` (+ `RoundTrip`) and
   `authFromContext`; change `newUpstreamHTTPClient` to wrap its base transport
   with `authInjector`; update the affected Mode-A doc comments. New import:
   `"context"` (already used in signatures; S1's gofmt import block gains it).
2. **MODIFY** `upstream_test.go` — extend `fakeState`/`newFakeZAI` to record the
   received `Authorization`; update `TestNewUpstreamHTTPClient` to unwrap the
   authInjector; add `TestAuthInjector_ContextThreading`,
   `TestUpstreamClient_AuthForwarded`, `TestUpstreamClient_AuthNotRetained`.

No new files. `go.mod`/`go.sum` unchanged. `main.go` UNCHANGED (`authHeaderKey`
+ `authMiddleware` already exist from P1.M1.T2.S2).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -race -run 'Upstream|AuthInjector' ./...`) all exit
clean. Against a fake z.ai that records its inbound `Authorization` header, a
`callTool` made with `context.WithValue(ctx, authHeaderKey{}, "Bearer
test-key-123")` causes the fake to observe `Authorization: Bearer test-key-123`
VERBATIM; a `callTool` with no `authHeaderKey` in ctx causes the fake to observe
NO `Authorization` header (we forward verbatim — never fabricate). After either
call, NO field of `UpstreamClient` is credential-named and NO existing string
field holds the secret (reflect name-walk + same-package access). The base
`*http.Transport` still has `ResponseHeaderTimeout==30s` and the client
`Timeout==0`. `go doc . authInjector` / `authFromContext` show the
verbatim-forward + never-log + context-threading rationale (Mode A).

## Hard Prerequisites

1. **`upstream.go` + `upstream_test.go` from P1.M4.T1.S1 EXIST and are green**
   (DONE — verified: `UpstreamClient` with 5 fields, `newUpstreamHTTPClient`,
   `ensureSession`, `callTool`, and the `newFakeZAI` harness + 5 tests). S2 is a
   SURGICAL edit to these two files; it does not rewrite them.
2. **`authHeaderKey struct{}` + `authMiddleware` EXIST in main.go**
   (P1.M1.T2.S2 — DONE, verified). The middleware writes
   `r.Header.Get("Authorization")` under `authHeaderKey{}` into the request
   context; the SDK passes `req.Context()` into the session (verified
   streamable.go:491-494). S2 reads this key; it does NOT redefine it.
3. **`github.com/modelcontextprotocol/go-sdk v1.6.1`** required in go.mod
   (P1.M1.T2.S1 — DONE). The context-propagation facts S2 relies on were
   RE-VERIFIED by reading the v1.6.1 source (research/auth-threading.md §1-§3).
4. **The redacting logger exists** (`redactHeaders`, logger.go — DONE). It is the
   belt-and-suspenders for the never-log invariant; S2's auth value lives only
   in request context and is never assigned to a logged field.

## User Persona

**Target User**: (1) **P1.M4.T1.S3** (session-expiry re-init) — its re-init path
calls `ensureSession` inside a `callTool` whose ctx carries `authHeaderKey`, so
the rebuilt transport's `authInjector` reads current auth for free; **S3 needs no
auth field.** (2) **P1.M5.T2** (server dispatch) — calls `upstream.callTool(ctx,
args)` where ctx is the inbound request's context (carrying `authHeaderKey` via
`authMiddleware`); the `authInjector` does the rest. M5.T2 changes nothing about
auth. (3) **P1.M5.T3** (server_test.go e2e) — case "delegate" asserts the fake
z.ai saw the verbatim `Authorization`; the `newFakeZAI` auth-recording extension
S2 ships is exactly what M5.T3 reuses.

**Use Case**: Agent sends `tools/call` with `Authorization: Bearer <key>`. The
SDK server handler's context carries `<key>` under `authHeaderKey`. M5.T2 calls
`upstream.callTool(ctx, args)`. The SDK propagates ctx to the outbound z.ai POST;
`authInjector.RoundTrip` reads `<key>` from `req.Context()` and sets
`Authorization: Bearer <key>` on the outbound request; z.ai authenticates the
delegated call and returns results. We never logged or stored `<key>`.

**User Journey**: inbound `tools/call` → `authMiddleware` stores header in ctx
(already done in main.go) → handler reads ctx → `callTool(ctx, args)` → SDK sends
POST to z.ai with `req.Context()==ctx` → `authInjector.RoundTrip` sets
`Authorization` from ctx → z.ai authenticates → result returns. (On session
expiry, S3 re-inits inside the same callTool; the new transport's authInjector
reads the same ctx → same auth.)

**Pain Points Addressed**: (1) PRD §17 / FR-7 "forward Authorization verbatim;
never read, log, or store it" — satisfied by reading transiently from context in
the RoundTripper, never via a struct field. (2) The shared-session vs.
per-request-auth tension — resolved cleanly: the per-call ctx reaches the
outbound request (verified), so the ONE shared transport always injects the
CURRENT request's auth with no mutation. (3) Race-safety — the authInjector holds
no mutable state, so concurrent calls inject concurrently without a lock.

## Why

- Implements **PRD §11.1** ("initialized lazily on first tools/call, using the
  inbound request's Authorization header") and **FR-7** ("we forward it verbatim
  to z.ai and hold no key").
- Implements **PRD §17** ("Forward Authorization verbatim to z.ai; never read,
  log, or store it") at the transport layer: the credential is a transient
  context value injected onto the outbound header, never a field.
- **Completes the auth seam** P1.M1.T2.S2 opened (middleware stores the header)
  and that M5.T2 will close (handler passes ctx to callTool). S2 is the
  forwarding half.
- **Decouples auth from session lifecycle.** Because auth is read per-request
  from context, S3 (re-init) and M5.T2 (dispatch) need ZERO auth-specific code.

## What

`upstream.go` gains `authInjector` + `authFromContext` and `newUpstreamHTTPClient`
is changed to wrap its base transport. `upstream_test.go` gains an
auth-recording fake and three new tests, and `TestNewUpstreamHTTPClient` is
updated to unwrap. Visible behavior: a `callTool` whose ctx carries
`authHeaderKey` causes z.ai to receive that exact `Authorization` header; a call
with no `authHeaderKey` causes z.ai to receive none. The credential is never
retained in `UpstreamClient` state and never logged.

### Success Criteria

- [ ] `type authInjector struct { base http.RoundTripper }` exists in
      `upstream.go` and implements `http.RoundTripper` via
      `func (a *authInjector) RoundTrip(req *http.Request) (*http.Response, error)`.
- [ ] `func authFromContext(ctx context.Context) string` exists and returns
      `v, _ := ctx.Value(authHeaderKey{}).(string)` (REUSES main.go's key; does
      NOT define a new key).
- [ ] `RoundTrip` sets `req.Header.Set("Authorization", <from-context>)` ONLY
      when the context value is non-empty, then delegates to `a.base.RoundTrip(req)`;
      when empty, it leaves the request untouched (no strip, no fabricate).
- [ ] `newUpstreamHTTPClient()` returns a `*http.Client` whose `Transport` is a
      `*authInjector`; the authInjector's `base` is a `*http.Transport` with
      `ResponseHeaderTimeout == 30 * time.Second`; `Client.Timeout == 0`. The
      function signature is unchanged from S1.
- [ ] `UpstreamClient`'s struct is UNCHANGED (still the S1 5 fields — NO auth
      field added). `ensureSession` and `callTool` logic is UNCHANGED (auth
      threading is entirely in the transport layer + the SDK's ctx propagation).
- [ ] Against an auth-recording fake z.ai: `callTool` with
      `context.WithValue(ctx, authHeaderKey{}, "Bearer test-key-123")` makes the
      fake observe `Authorization == "Bearer test-key-123"` VERBATIM; a call with
      a bare `context.Background()` makes the fake observe an EMPTY `Authorization`
      (header absent).
- [ ] After a `callTool` made with a known secret, NO credential-named field
      exists on `UpstreamClient` and NO existing string field
      (`upstream`/`targetTool`/`targetParam`) equals the secret
      (reflect name-walk + same-package access — §17 "never store").
- [ ] `go test -race -run 'Upstream|AuthInjector' ./...` is green (no data race
      on the concurrent path — the authInjector holds no mutable state).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat go.mod` is empty; `git diff --stat` shows ONLY
      `upstream.go` + `upstream_test.go`; `go doc . authInjector` and
      `go doc . authFromContext` show the Mode-A rationale.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone
because: (a) the central SDK fact — that `CallTool(ctx)`'s ctx reaches the
outbound POST's `req.Context()` — is given as an EXACT, cited chain (research §1;
re-verified: client.go:990 → shared.go:136 → transport.go:222 → jsonrpc2
conn.go:273/306/718 → streamable.go:1788/1806), and `xcontext.Detach` preserving
VALUES is quoted verbatim (xcontext.go:34, research §2) — so the implementer
need not re-derive why a context-reading RoundTripper works; (b) the exact Go for
`authInjector`, `authFromContext`, and the `newUpstreamHTTPClient` edit is given
(research §6-§7); (c) the auth key to reuse (`authHeaderKey`, already in main.go)
is named explicitly, with the warning NOT to redefine it; (d) the test-harness
recording wrapper is given as exact Go (research §8); (e) the race-freedom
argument is stated (research §9) so the `-race` gate is understood; (f) the
never-log/never-store enforcement (research §10) is concrete and testable.

### Documentation & References

```yaml
# MUST READ — the rules this item implements.
- file: PRD.md
  section: "§17 Headers, credentials, security" + "FR-7 Credentials forwarded, not owned"
  why: §17 = "Forward Authorization verbatim to z.ai; never read, log, or store it."
        FR-7 = "we forward it verbatim to z.ai and hold no key. It is never logged."
  critical: the credential must be a TRANSIENT context value injected onto the
        outbound header, never a struct field (most faithful to "never store").
        The redacting logger (logger.go redactHeaders) is the never-log safety net.

- file: PRD.md
  section: "§11.1 Session lifecycle" + "§15 Logging"
  why: §11.1 = "initialized lazily on first tools/call, using the inbound request's
        Authorization header." §15 = the delegate log event fields NEVER include
        headers (even at debug: "raw inbound arguments shape (never headers)").
  critical: S2's authInjector injects on EVERY outbound request (initialize POST,
        tools/call POST, SSE GET, DELETE) because the SDK propagates ctx to all of
        them (verified).

# MUST READ — THIS ITEM'S RESEARCH (source-verified SDK facts + the exact design).
- docfile: plan/002_0a8ab3410994/P1M4T1S2/research/auth-threading.md
  why: §1 the cited chain proving per-call ctx reaches outbound req.Context();
        §2 xcontext.Detach preserves VALUES (SSE GET/DELETE carry auth too);
        §3 Connect uses t.HTTPClient for ALL requests; §4 authHeaderKey already
        exists (reuse, don't redefine); §5 the design decision (context shape vs.
        field shape); §6 exact authInjector Go; §7 the newUpstreamHTTPClient edit;
        §8 the test-harness recording wrapper; §9 race-freedom; §10 never-logged.
  section: all sections are load-bearing; §1, §6, §7, §8 are implementation-critical.

# MUST READ — the SDK API surface (verified from v1.6.1 source).
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§2 CLIENT SIDE (StreamableClientTransport)" + "§6 AUTH/CONTEXT"
  why: §2 = StreamableClientTransport has HTTPClient for RoundTripper auth
        injection; §6 = the auth-context seam (middleware → ctx → handler →
        upstream RoundTripper). Re-affirms the context-threading pattern.
  critical: HTTPClient is the ONLY SDK field S2 touches; Connect/CallTool/ensureSession
        are UNCHANGED.

# MUST READ — the file S2 edits (S1's output). Read it FIRST.
- file: upstream.go
  section: "newUpstreamHTTPClient" + "UpstreamClient doc comment (AUTH section)" +
        "ensureSession" + "callTool"
  why: S2 wraps newUpstreamHTTPClient's base transport and updates two doc comments;
        ensureSession/callTool/struct are UNCHANGED. The S1 doc comment's "AUTH
        (PRD §17, S2): ... S2 wraps newUpstreamHTTPClient's Transport" is the line
        to update to "auth is wired via authInjector".
  pattern: S1's newUpstreamHTTPClient clones http.DefaultTransport, sets
        ResponseHeaderTimeout=30s, returns &http.Client{Transport: tr}. S2 changes
        ONLY the return to &http.Client{Transport: &authInjector{base: tr}}.

# MUST READ — the test file S2 extends.
- file: upstream_test.go
  section: "fakeState" + "newFakeZAI" + "TestNewUpstreamHTTPClient"
  why: S2 adds lastAuth to fakeState + a recording wrapper in newFakeZAI;
        updates TestNewUpstreamHTTPClient to unwrap the authInjector; adds 3 tests.
  pattern: newFakeZAI builds httptest.NewServer(mcp.NewStreamableHTTPHandler(...));
        wrap it in an http.HandlerFunc that records r.Header.Get("Authorization").

# MUST READ — the auth key S2 REUSES (do NOT redefine).
- file: main.go
  section: "authHeaderKey struct{}" + "authMiddleware"
  why: authHeaderKey is the context key the middleware WRITES under. authFromContext
        MUST read ctx.Value(authHeaderKey{}). S2 does NOT edit main.go.

# CODEBASE CONVENTIONS — the redacting logger (never-log safety net).
- file: logger.go
  section: "redactHeaders"
  why: replaces Authorization/Cookie/Set-Cookie/Proxy-Authorization with
        "<redacted>". S2's auth value lives only in context and is never a logged
        field; redactHeaders is the belt-and-suspenders if any header is ever logged.

# Go stdlib (RoundTripper contract).
- url: https://pkg.go.dev/net/http#RoundTripper
  why: RoundTripper.RoundTrip(req) (*http.Response, error) signature + the "should
        not modify the request" note.
  critical: setting a header directly in RoundTrip is safe here because each
        outbound request is freshly built by the SDK (NewRequestWithContext) and
        never reused — see research §6 gotcha. The SDK's own transports set headers
        this way (streamable.go:1946).

# Go stdlib (context key convention).
- url: https://go.dev/blog/context
  why: unexported struct type as a collision-free context key (the convention
        authHeaderKey already follows).
```

### Current Codebase tree (the INPUT state — run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  main.go           # authHeaderKey{} + authMiddleware (P1.M1.T2.S2) — UNTOUCHED by S2
  upstream.go       # S1: UpstreamClient(5 fields) + newUpstreamHTTPClient + ensureSession + callTool — S2 EDITS
  upstream_test.go  # S1: newFakeZAI + 5 tests — S2 EDITS (extends harness, updates 1 test, adds 3)
  config.go, logger.go, health.go, extract.go, teach.go — UNTOUCHED
  *_test.go (config/resolve/logger/health/extract/teach) — UNTOUCHED
  testdata/*.sse, README.md, config.example.json, PRD.md, doc.go — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
upstream.go        # MODIFY (no new file). Adds:
                   #   type authInjector struct { base http.RoundTripper }
                   #   func (a *authInjector) RoundTrip(req) (*http.Response, error)
                   #   func authFromContext(ctx context.Context) string
                   # Changes newUpstreamHTTPClient to wrap base with &authInjector{base: tr}.
                   # Updates Mode-A doc comments on authInjector, authFromContext,
                   #   newUpstreamHTTPClient, and UpstreamClient's AUTH section.
                   # New import: "context" (stdlib). struct/ensureSession/callTool UNCHANGED.
upstream_test.go   # MODIFY (no new file). fakeState gains lastAuth; newFakeZAI wraps the
                   #   SDK handler in a recording http.Handler; TestNewUpstreamHTTPClient
                   #   unwraps the authInjector; ADD TestAuthInjector_ContextThreading,
                   #   TestUpstreamClient_AuthForwarded, TestUpstreamClient_AuthNotRetained.
# NO other files created or modified. go.mod/go.sum unchanged.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL — REUSE authHeaderKey from main.go; do NOT define a new context key.
// main.go's authMiddleware writes r.Header.Get("Authorization") under authHeaderKey{}.
// authFromContext MUST read ctx.Value(authHeaderKey{}). A new key (e.g. authKey{})
// would never find the value and silently forward NO auth (real z.ai rejects).

// CRITICAL — the authInjector reads auth from req.Context(), NOT from a struct field.
// VERIFIED (research §1): CallTool(ctx) → ... → http.NewRequestWithContext(ctx) so the
// per-call ctx reaches the outbound POST's req.Context(). And xcontext.Detach preserves
// VALUES (research §2) so the SSE GET (streamable.go:2206) and DELETE (streamable.go:2239)
// carry authHeaderKey too. So one context-reading authInjector covers ALL outbound requests.
// Do NOT add an auth field to UpstreamClient — it would be redundant AND a §17 "store" risk.

// CRITICAL — UpstreamClient struct, ensureSession, callTool are UNCHANGED. S1 already
// builds the transport via newUpstreamHTTPClient() (which S2 makes wrap) and passes ctx
// to CallTool (which the SDK propagates to req.Context()). Auth threading is ENTIRELY in
// the transport layer. Do not touch the lazy-init/mutex logic.

// CRITICAL — newUpstreamHTTPClient's SIGNATURE is unchanged: func newUpstreamHTTPClient()
// *http.Client. Only its RETURN changes: the *http.Transport becomes the authInjector's
// base, and the client's Transport is the authInjector. S1's TestNewUpstreamHTTPClient
// asserted c.Transport.(*http.Transport); S2 updates it to unwrap: assert
// c.Transport is *authInjector, then .base.(*http.Transport) has ResponseHeaderTimeout==30s.

// CRITICAL — ResponseHeaderTimeout stays 30s and Client.Timeout stays 0 (PRD §11.2, S1).
// The authInjector must NOT introduce any Client.Timeout or per-request deadline (a whole-
// exchange Timeout would cut off z.ai's long SSE result streams). The base *http.Transport
// owns ResponseHeaderTimeout; the authInjector only injects the header.

// GOTCHA — setting req.Header.Set inside RoundTrip is safe here. The net/http doc says
// "RoundTrip should not modify the request", but each outbound request is freshly built by
// the SDK (NewRequestWithContext) and never reused/cached, and the SDK's own transports set
// headers this way (streamable.go:1946). Do NOT req.Clone (it cannot always re-read the
// body). Direct Set matches the SDK convention. (research §6)

// GOTCHA — empty auth → leave the header UNTOUCHED. Set Authorization ONLY when
// authFromContext returns non-empty. Never fabricate; never strip an existing header. (The
// base transport sets no Authorization, so an empty context yields no Authorization outbound.)

// GOTCHA — authHeaderKey/authMiddleware already exist in main.go (P1.M1.T2.S2). S2 does NOT
// edit main.go. If `grep authHeaderKey main.go` is empty, a prerequisite failed — STOP.

// GOTCHA — race-free by construction. authInjector has ONE field (base), set once at
// construction, never mutated. RoundTrip reads req.Context() (per-request) and the immutable
// base. The S1 concurrent test stays green under -race WITHOUT any lock/atomic for auth.

// GOTCHA — the never-log invariant. UpstreamClient/callTool do not log in S2 (the delegate
// log event is M5.T2's job; PRD §15 lists its fields — never headers). The auth value is read
// transiently in RoundTrip and never assigned to a field. redactHeaders (logger.go) is the
// never-log safety net. The runnable enforcement is TestUpstreamClient_AuthNotRetained.
```

## Implementation Blueprint

### Data models and structure

No new data models. `UpstreamClient` is UNCHANGED (S1's 5 fields). The only new
type is the transport-layer `authInjector`, which holds an immutable
`base http.RoundTripper`. New helper `authFromContext` is a pure function over
the context. Deps: stdlib `context`, `net/http` (both already in upstream.go's
import set after S1, except `"context"` which S2 adds explicitly).

### Reference implementation (the `upstream.go` additions/edits)

ADD these two declarations to `upstream.go` (placement: near
`newUpstreamHTTPClient`, since they are its collaborators):

```go
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
```

EDIT `newUpstreamHTTPClient` to wrap the base transport (signature unchanged):

```go
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
```

EDIT the `UpstreamClient` doc comment's AUTH paragraph (S1 said S1 builds the
client WITHOUT auth and S2 will wrap it). Replace that paragraph with:

```go
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
```

> NOTE: upstream.go's import block gains `"context"` (used in `authFromContext`'s
> signature). The final gofmt-sorted import block is: `"context"`, `"net/http"`,
> `"sync"`, `"time"`, then `"github.com/modelcontextprotocol/go-sdk/mcp"`.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify S1 + the auth seam exist
  - RUN: test -f upstream.go && test -f upstream_test.go \
        && grep -q "authHeaderKey struct{}" main.go \
        && grep -q "authMiddleware" main.go \
        && grep -q "func newUpstreamHTTPClient" upstream.go \
        && grep -q "type UpstreamClient struct" upstream.go \
        && grep -q "modelcontextprotocol/go-sdk v1.6.1" go.mod
  - EXPECT: exit 0 (S1 shipped, the auth key+middleware exist in main.go, SDK
        required). IF ANY FAIL: STOP — a prerequisite (S1 or P1.M1.T2.S2) has not
        run. S2 does NOT create upstream.go; it EDITS it.

Task 1: ADD authInjector + authFromContext to upstream.go
  - FILE: upstream.go (MODIFY). Paste the two declarations from "Reference
        implementation" above, placed adjacent to newUpstreamHTTPClient.
  - ADD "context" to the import block (gofmt-sorted).
  - CONSTRAINTS: authFromContext reads ctx.Value(authHeaderKey{}) (REUSE main.go's
        key — do NOT define authHeaderKey or any new key in upstream.go). RoundTrip
        sets Authorization ONLY when authFromContext is non-empty, then delegates to
        a.base.RoundTrip. authInjector has EXACTLY one field: base http.RoundTripper.
  - MODE A: the doc comments (verbatim-forward rule, never-log invariant,
        context-threading pattern with the cited SDK chain) are load-bearing —
        paste them verbatim from the Reference implementation.

Task 2: EDIT newUpstreamHTTPClient to wrap the base transport
  - FILE: upstream.go (MODIFY). Change ONLY the return statement from
        `&http.Client{Transport: tr}` to `&http.Client{Transport: &authInjector{base: tr}}`.
  - UPDATE its doc comment (S1 said "the base that S2 wraps") to state it DOES the
        wrapping (paste the new comment from "Reference implementation").
  - CONSTRAINTS: ResponseHeaderTimeout stays 30s; Client.Timeout stays 0; the
        *http.Transport is the authInjector's base. Signature unchanged.

Task 3: EDIT the UpstreamClient doc comment's AUTH paragraph
  - FILE: upstream.go (MODIFY). Replace S1's "AUTH (PRD §17, S2): this struct (S1)
        builds the HTTP client WITHOUT Authorization. P1.M4.T1.S2 wraps ..." paragraph
        with the updated paragraph from "Reference implementation" (auth is now wired
        via authInjector). Leave the rest of the UpstreamClient doc comment intact.

Task 4: EXTEND the fake-z.ai harness to record Authorization (upstream_test.go)
  - FILE: upstream_test.go (MODIFY). Add `lastAuth string` to fakeState.
  - WRAP the SDK handler in newFakeZAI with a recording http.HandlerFunc that, under
        st.mu, sets st.lastAuth = r.Header.Get("Authorization"), then calls
        h.ServeHTTP(w, r). (See "Test harness" below.) This is backward-compatible
        with S1's tests (they ignore lastAuth).

Task 5: UPDATE TestNewUpstreamHTTPClient to unwrap the authInjector (upstream_test.go)
  - FILE: upstream_test.go (MODIFY). The client's Transport is now *authInjector.
        Assert: c.Transport is *authInjector; ai := c.Transport.(*authInjector);
        tr := ai.base.(*http.Transport); tr.ResponseHeaderTimeout == 30*time.Second;
        c.Timeout == 0. (See "Test harness" below.)

Task 6: ADD the three new tests (upstream_test.go)
  - TestAuthInjector_ContextThreading: unit-test the RoundTripper with a recording
        base (see below). Cases: (a) ctx carries authHeaderKey="Bearer k" → base
        sees Authorization=="Bearer k"; (b) bare context.Background() → base sees
        Authorization=="" (not set).
  - TestUpstreamClient_AuthForwarded: e2e. Build u against an auth-recording fake
        z.ai. ctx := context.WithValue(testCtx, authHeaderKey{}, "Bearer
        test-key-123"). callTool. Assert st.lastAuth == "Bearer test-key-123"
        (verbatim). (Also assert the result still returns, i.e. auth didn't break
        the path.)
  - TestUpstreamClient_AuthNotRetained: after a call with a known secret, reflect
        over UpstreamClient's field NAMES (unexported values can't be read via reflect
        from the same package) to assert NO credential-named field exists, AND direct-
        access the string fields to assert none holds the secret (PRD §17 "never store").
        (See below.)

Task 7: VALIDATE
  - gofmt -w upstream.go upstream_test.go
  - go vet ./...
  - go build ./...
  - go test -run 'Upstream|AuthInjector' -v          # new + updated S1 tests
  - go test -race -run 'Upstream|AuthInjector' -v    # race gate (authInjector has no mutable state)
  - go test ./...                                    # full suite green (no regressions)
  - ALL green. git diff --stat must show ONLY upstream.go + upstream_test.go.
  - git diff --stat go.mod must be EMPTY.
  - go doc . authInjector ; go doc . authFromContext  # Mode A prints the rationale.
  - grep -n 'authHeaderKey\|authKey' upstream.go      # ONLY authHeaderKey in authFromContext;
                                                     # NO new key type defined.
```

### Test harness + tests (the `upstream_test.go` edits)

```go
// --- EXTEND fakeState (add the lastAuth field) ---
type fakeState struct {
	mu       sync.Mutex
	calls    int32
	lastTool string
	lastArgs map[string]any
	lastAuth string // S2: the Authorization header the fake z.ai received (verbatim-forward proof)
}

// --- EDIT newFakeZAI: wrap the SDK handler in a recording handler ---
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

// --- EDIT TestNewUpstreamHTTPClient: unwrap the authInjector ---
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

// --- ADD TestAuthInjector_ContextThreading (unit test the RoundTripper) ---
// A recording base RoundTripper that captures the request it received.
type recordingTripper struct {
	got *http.Request
}

func (r *recordingTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
}

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

// --- ADD TestUpstreamClient_AuthForwarded (e2e verbatim forward) ---
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

// --- ADD TestUpstreamClient_AuthNotRetained (PRD §17 "never store") ---
// NOTE on reflect: UpstreamClient's fields are ALL unexported (mu/session/upstream/
// targetTool/targetParam), so reflect Value.Interface() / CanInterface() CANNOT read
// their values (CanInterface()==false for all of them; Interface() would panic). A
// naive reflect value-walk therefore skips every field and passes trivially. We
// enforce "never stored" two ways that DO work from the same package:
//   (1) reflect over field NAMES only (no value read, no panic) -> assert no field is
//       named like a credential (catches a future regression that ADDS an auth field).
//   (2) direct same-package access of the known string fields -> assert none holds the
//       secret value (catches storing it in an existing field).
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
```

> IMPORTS for upstream_test.go gain `"reflect"` and `"strings"` (and `authHeaderKey`
> is already in scope — same package main). `httptest`, `context` are already imported.

### Implementation Patterns & Key Details

```go
// PATTERN: context-reading RoundTripper. The authInjector's only state is the
// immutable base; the per-request credential is read from req.Context() inside
// RoundTrip. This is the canonical Go pattern for injecting request-scoped
// values (auth, tracing) onto outbound HTTP without shared mutable state. The
// SDK's verified ctx propagation (research §1) is what makes it work here.

// PATTERN: surgical wrap of an existing transport. newUpstreamHTTPClient already
// builds the *http.Transport (ResponseHeaderTimeout, no Timeout). S2 wraps it:
// &authInjector{base: tr}. The base retains ALL its behavior (TLS, HTTP/2, idle
// pooling, header timeout). The authInjector adds ONE responsibility: inject
// Authorization. Single Responsibility at the transport layer.

// PATTERN: verbatim forward + never-fabricate. RoundTrip sets Authorization only
// when the context value is non-empty; otherwise it forwards unchanged. This is
// the literal FR-7 contract: forward what the client sent; hold no key of our own.

// GOTCHA (restated): the auth value reaches ALL outbound requests — initialize
// POST (per-call ctx), tools/call POST (per-call ctx), SSE GET (connCtx =
// xcontext.Detach, values preserved), DELETE (connCtx). One authInjector covers
// the whole surface. (research §1, §2)

// GOTCHA (restated): authHeaderKey is REUSED, not redefined. main.go owns it.
// Defining authHeaderKey or any new key in upstream.go is a compile error
// (redeclared) at worst, or a silent no-auth-forward bug at best — avoid both.

// GOTCHA (restated): do NOT add an auth field to UpstreamClient. It is redundant
// (context carries the value per-call) and a §17 "store" risk. S3 (re-init) and
// M5.T2 (dispatch) need no field either — both run inside a callTool whose ctx
// carries authHeaderKey.
```

### Integration Points

```yaml
FILES MODIFIED:
  - upstream.go      (MODIFY): +authInjector +authFromContext; newUpstreamHTTPClient
        wraps base; 3 doc-comment updates; +import "context". struct/ensureSession/
        callTool UNCHANGED.
  - upstream_test.go (MODIFY): fakeState +lastAuth; newFakeZAI recording wrapper;
        TestNewUpstreamHTTPClient unwraps authInjector; +3 tests; +import "reflect".
NO OTHER FILES TOUCHED:
  - main.go: UNCHANGED (authHeaderKey + authMiddleware already exist — P1.M1.T2.S2).
  - go.mod/go.sum: UNCHANGED (context/reflect are stdlib; the SDK require already exists).
  - All other *_test.go, config.go, logger.go, health.go, extract.go, teach.go, doc.go:
        UNCHANGED.
CONSUMER SEAMS (S2 changes NOTHING about UpstreamClient's struct or method signatures;
        auth is entirely transport-layer + SDK ctx propagation):
  - P1.M4.T1.S3 (re-init): on a session-expiry error, re-enter ensureSession after
        resetting u.session=nil under the mutex; the rebuilt transport's authInjector
        reads the current callTool's auth from context — NO auth-specific code needed.
  - P1.M5.T2 (server dispatch): constructs &UpstreamClient{...}; calls
        res, err := upstream.callTool(ctx, args) where ctx is the inbound request's
        context (carrying authHeaderKey via authMiddleware). The authInjector injects
        automatically. M5.T2 adds ZERO auth code.
  - P1.M5.T3 (server_test.go e2e): reuses newFakeZAI's lastAuth recording to assert
        the delegate path forwarded the verbatim Authorization.
DATABASE / ROUTES / ENV: none. S2 is transport-layer only; no handler wiring.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w upstream.go upstream_test.go
go vet ./...
go build ./...                  # must compile; "context"/"reflect" are stdlib

# authFromContext reuses authHeaderKey; NO new key type defined in upstream.go:
grep -n 'authHeaderKey\|authKey\|type.*Key struct' upstream.go
#   expect: authHeaderKey{} ONLY inside authFromContext; NO "type authHeaderKey" and
#   NO "type authKey" declaration in upstream.go (the type lives in main.go).

# authInjector has exactly one field (base); UpstreamClient is UNCHANGED (5 fields):
grep -A2 'type authInjector struct' upstream.go    # one field: base http.RoundTripper
grep -A6 'type UpstreamClient struct' upstream.go  # still mu/session/upstream/targetTool/targetParam

# newUpstreamHTTPClient wraps with authInjector (ResponseHeaderTimeout still on the base):
grep -n 'authInjector{base\|ResponseHeaderTimeout\|Client.Timeout\|&http.Client' upstream.go

# go.mod UNCHANGED:
git diff --stat go.mod          # expect EMPTY

# Only the two files changed:
git status --short              # expect:  M upstream.go   M upstream_test.go   (NO other lines)

# Expected: zero errors; authInjector(1 field); authFromContext reuses authHeaderKey;
# newUpstreamHTTPClient wraps; UpstreamClient unchanged; go.mod clean.
```

### Level 2: Unit Tests (Component Validation)

```bash
# New + updated S1 tests, no race.
go test -run 'Upstream|AuthInjector' -v

# Race gate (the authInjector holds no mutable state -> clean under -race).
go test -race -run 'Upstream|AuthInjector' -v

# MUST PASS:
#   TestNewUpstreamHTTPClient             -> Transport is *authInjector; its base
#                                            *http.Transport has ResponseHeaderTimeout==30s;
#                                            Client.Timeout==0.
#   TestAuthInjector_ContextThreading     -> (a) ctx w/ authHeaderKey sets Authorization
#                                            verbatim on the base's request; (b) bare ctx
#                                            leaves Authorization empty (never fabricate).
#   TestUpstreamClient_AuthForwarded      -> ctx carrying "Bearer test-key-123" makes the
#                                            auth-recording fake z.ai observe that EXACT
#                                            value; result still returns.
#   TestUpstreamClient_AuthNotRetained    -> after the call, NO string field of
#                                            *UpstreamClient equals the secret (§17 never-store).
#   (S1's tests stay green: LazyInitAndCallTool, LazyReuse, Concurrent, EnsureSessionError —
#    the harness change is backward-compatible: they ignore lastAuth.)
# Expected: PASS, exit 0 (with AND without -race). If AuthForwarded sees an empty
# Authorization, the auth key was misread (did you define a new key instead of reusing
# authHeaderKey?) or the SDK ctx did not propagate (re-check research §1). If
# AuthNotRetained fails, an auth value was assigned to a UpstreamClient field — remove it.
```

### Level 3: Integration Testing (System Validation)

```bash
# Full suite stays healthy with the transport-layer change.
go build ./...                 # compiles (UpstreamClient still unused by non-test code -> builds)
go test ./...                  # config/resolve/logger/health/extract/teach/upstream — ALL green

# Mode A doc comments print the rationale.
go doc . authInjector          # verbatim-forward + never-log + context-threading (cited SDK chain)
go doc . authFromContext       # reuses authHeaderKey; "" when absent
go doc . newUpstreamHTTPClient # wraps base with authInjector; ResponseHeaderTimeout=30s, Timeout=0

# Prove the verbatim forward end-to-end against the in-process fake (no real key, no network):
go test -run TestUpstreamClient_AuthForwarded -v

# Expected: build clean; full suite green; go doc shows the Mode-A rationale; AuthForwarded
# passes (the fake observed the verbatim Authorization). NOTE: real-z.ai delegation is NOT
# exercised here (needs a live key + the handler wiring that lands in P1.M5.T2). S2 proves
# the transport injects the verbatim header against an auth-recording fake z.ai.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Security: confirm the credential never appears in any log line the package could emit.
# (UpstreamClient does not log in S2; the delegate log event is M5.T2's. This gate asserts
# the design invariant concretely.)
go test -run 'TestUpstreamClient_AuthNotRetained|TestUpstreamClient_AuthForwarded' -v
# AuthNotRetained reflect-walks UpstreamClient fields for the secret (none may hold it).
# AuthForwarded proves forwarding works. Together: forwarded verbatim, never retained.

# Race robustness under the realistic concurrent path (single shared session, many calls):
go test -race -run TestUpstreamClient_Concurrent -v
# Green => the authInjector (no mutable state) injects concurrently without a data race.

# grep audit: the secret string literal must appear ONLY in test files, never in upstream.go:
grep -rn 'test-key-123\|never-stored-456\|secret-xyz' upstream.go   # expect: no matches
grep -rn 'test-key-123\|never-stored-456\|secret-xyz' upstream_test.go # expect: the test literals

# Expected: no secret literals in production code; the credential is data (from context), not code.
```

## Final Validation Checklist

### Technical Validation

- [ ] All 4 validation levels completed successfully.
- [ ] `go build ./...` clean.
- [ ] `go vet ./...` clean.
- [ ] `gofmt -l .` empty.
- [ ] `go test ./...` green (full suite, no regressions).
- [ ] `go test -race -run 'Upstream|AuthInjector' ./...` green (race gate).

### Feature Validation

- [ ] `authInjector` injects the verbatim `Authorization` from `req.Context()` on
      every outbound z.ai request (unit + e2e against the auth-recording fake).
- [ ] Empty context → no `Authorization` forwarded (never fabricate).
- [ ] `UpstreamClient` struct unchanged; no auth field; `ensureSession`/`callTool`
      unchanged.
- [ ] After a call with a known secret, no `UpstreamClient` field holds it
      (TestUpstreamClient_AuthNotRetained).
- [ ] `ResponseHeaderTimeout` still 30s; `Client.Timeout` still 0 (S1 invariant held).
- [ ] `go doc . authInjector` / `authFromContext` show the Mode-A rationale.

### Code Quality Validation

- [ ] `authFromContext` REUSES `authHeaderKey` (no new key type in upstream.go).
- [ ] `newUpstreamHTTPClient` signature unchanged; only the transport wrap changed.
- [ ] Follows existing conventions (package main; t.Helper builders; doc comments
      cite PRD sections and SDK file:line like S1/S2 siblings).
- [ ] No `Client.Timeout` introduced; no `req.Clone` (direct header Set, SDK convention).
- [ ] Imports: only `"context"` (upstream.go) and `"reflect"` (upstream_test.go) added.

### Documentation & Deployment

- [ ] Mode-A doc comments on `authInjector`/`authFromContext`/`newUpstreamHTTPClient`/
      `UpstreamClient` AUTH paragraph document the verbatim-forward rule, the
      never-log invariant, and the verified context-threading pattern.
- [ ] No new env vars / config / routes (transport-layer only).

---

## Anti-Patterns to Avoid

- ❌ Don't define a new context key (`authKey{}`, etc.) — REUSE `authHeaderKey` from
  main.go. A new key silently forwards no auth.
- ❌ Don't add an `auth`/`authHeader` field to `UpstreamClient` or the authInjector —
  the verified context mechanism makes it redundant and it violates §17 "never store".
- ❌ Don't mutate `ensureSession`/`callTool`/the struct for auth — threading is
  entirely in the transport layer + the SDK's ctx propagation.
- ❌ Don't introduce a `Client.Timeout` or per-request body deadline — it would cut
  off z.ai's long SSE result streams (PRD §11.2, S1 invariant).
- ❌ Don't `req.Clone` in `RoundTrip` — it can't always re-read the body; direct
  `Header.Set` is safe here (fresh per-call requests; SDK does the same).
- ❌ Don't log the credential, store it in a field, or pass it to the logger. It is
  a transient context value read inside `RoundTrip` only.
- ❌ Don't catch all exceptions / swallow the base `RoundTrip` error — return it
  verbatim so session-expiry (S3) and honest error surfacing work.
