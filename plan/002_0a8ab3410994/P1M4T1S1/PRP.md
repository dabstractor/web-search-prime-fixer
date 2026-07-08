name: "P1.M4.T1.S1 — Upstream client struct + lazy shared session init via SDK client"
description: |

  CREATE `upstream.go` (the upstream-delegation module, PRD §11): `type UpstreamClient
  struct` with EXACTLY five fields (`mu sync.Mutex`, `session *mcp.ClientSession`,
  `upstream string`, `targetTool string`, `targetParam string`), a private
  `func newUpstreamHTTPClient() *http.Client` (clones `http.DefaultTransport`,
  sets `Transport.ResponseHeaderTimeout = 30 * time.Second`, leaves `Client.Timeout`
  at zero — PRD §11.2 / external_deps §5: no hard timeout or long SSE result streams
  are cut off), `func (u *UpstreamClient) ensureSession(ctx context.Context) error`
  (mutex-guarded LAZY init: if `session==nil`, build
  `&mcp.StreamableClientTransport{Endpoint: u.upstream, HTTPClient: newUpstreamHTTPClient()}`,
  `mcp.NewClient(&mcp.Implementation{...}, nil)`, `client.Connect(ctx, transport, nil)`
  which performs the FULL initialize handshake synchronously, store the session;
  on failure leave `session` nil so the next call retries), and
  `func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any)
  (*mcp.CallToolResult, error)` (ensureSession; then read `u.session` UNDER the mutex
  for race-safety and call `session.CallTool(ctx, &mcp.CallToolParams{Name:
  u.targetTool, Arguments: args})` OUTSIDE the mutex so concurrent calls proceed in
  parallel). The session is shared across calls; context VALUES survive the first
  request's cancellation (the SDK detaches the connection context — verified).
  **NO auth injection in S1** — the HTTPClient is built WITHOUT Authorization; S1 is
  tested against a no-auth fake z.ai; S2 (P1.M4.T1.S2) adds the auth RoundTripper
  reading `ctx.Value(authHeaderKey{})` (already in main.go). CREATE `upstream_test.go`
  with a fake-z.ai harness (a REAL `mcp.Server` over `httptest` that records
  `tools/call`) PROVEN against the SDK: tests newUpstreamHTTPClient config, lazy
  init + reuse, callTool wiring (targetTool + args), concurrency (`-race`), and
  ensureSession error propagation. **Mode A docs**: a doc comment on `UpstreamClient`
  documenting the lazy-shared-session pattern, the mutex guarding, and the
  no-hard-timeout + ResponseHeaderTimeout rationale (PRD §11.1, §11.2). go.mod gains
  ZERO requires (the SDK require already exists; only stdlib `net/http`, `sync`,
  `time` + the `mcp` import are added).

---

## Goal

**Feature Goal**: Ship the upstream-delegation skeleton (PRD §11.1, FR-5): a single
shared MCP client session to z.ai, initialized LAZILY on the first `tools/call`,
guarded by a mutex so concurrent inbound calls share one session without
double-initializing, and a `callTool` that delegates to z.ai's `web_search_prime`
with the extracted args and returns z.ai's `*mcp.CallToolResult` verbatim. The
outbound HTTP is governed by the client context (cancellation propagates) plus a
~30s response-header timeout — NEVER a hard body timeout, so long SSE result
streams are not cut off (PRD §11.2). This item produces the tested skeleton; S2
adds auth, S3 adds session-expiry re-init.

**Deliverable**: TWO new files at the repo root, both `package main`:
1. **CREATE** `upstream.go` — `type UpstreamClient struct` (5 fields),
   `func newUpstreamHTTPClient() *http.Client`, `func (u *UpstreamClient)
   ensureSession(ctx context.Context) error`, `func (u *UpstreamClient) callTool(ctx
   context.Context, args map[string]any) (*mcp.CallToolResult, error)`, plus a Mode-A
   doc comment on `UpstreamClient`. Imports: `"net/http"`, `"sync"`, `"time"`, and
   `"github.com/modelcontextprotocol/go-sdk/mcp"`.
2. **CREATE** `upstream_test.go` — the fake-z.ai harness (a real `mcp.Server` over
   `httptest`) + tests: `TestNewUpstreamHTTPClient` (ResponseHeaderTimeout==30s,
   Timeout==0), `TestUpstreamClient_LazyInitAndCallTool` (session nil→non-nil,
   callTool sends targetTool+args, returns the fake's result), `TestUpstreamClient_
   LazyReuse` (second callTool reuses the SAME session pointer — no re-Connect),
   `TestUpstreamClient_Concurrent` (N goroutines, `-race`, all succeed, single
   session), `TestUpstreamClient_EnsureSessionError` (bad upstream → error, session
   stays nil → next call retries).

No other file is touched. `go.mod` gains zero `require`s (the SDK require was added
in P1.M1.T2.S1 and `main.go` already imports `mcp`). No README/config/doc.go change
(Mode A docs = doc comments only). `upstream.go` is NOT wired into any handler in
this item — P1.M5.T2 builds the `UpstreamClient`, calls `callTool`, and threads it
into the `extract→delegate→teach` dispatch.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -race -run Upstream ./...`) all exit clean. Against a
fake z.ai, `callTool` reaches `web_search_prime` with the exact args, returns z.ai's
`Content`/`IsError` untouched; the session is created ONCE and reused; concurrent
calls do not race and do not double-init; a dead upstream yields a non-nil error and
leaves the session nil. `newUpstreamHTTPClient` sets `ResponseHeaderTimeout=30s` and
no `Timeout`. `go doc . UpstreamClient` shows the lazy-session + mutex + timeout
rationale (Mode A).

## Hard Prerequisites

1. **`github.com/modelcontextprotocol/go-sdk v1.6.1` is required in `go.mod`** and
   the module cache is populated (P1.M1.T2.S1 — DONE, verified: `go.mod` has the
   sole `require`; `go test ./...` is green). All SDK signatures below were VERIFIED
   by reading the source at `$(go env GOMODCACHE)/github.com/modelcontextprotocol/
   go-sdk@v1.6.1/mcp/` (see research/upstream-client-design.md §1).
2. **`main.go` already imports `"github.com/modelcontextprotocol/go-sdk/mcp"`** and
   defines `authHeaderKey struct{}` + `authMiddleware` (P1.M1.T2.S2 — DONE,
   verified). S1 does NOT read `authHeaderKey`; S2 does. S1 references it only in
   doc comments to document the seam.
3. **`config.go` has the v2 Config fields** `Upstream`, `TargetTool` (default
   `"web_search_prime"`), `TargetParam` (default `"search_query"`) (P1.M1.T1.S1 —
   DONE, verified). `UpstreamClient` is constructed from these in P1.M5.T2; S1's
   tests build it inline from literals.
4. **`extract.go`'s `ExtractionResult.ToUpstreamArgs(targetParam) map[string]any`**
   exists (P1.M2.T1.S3 — DONE, verified at extract.go:315): it emits
   `{targetParam: Query, ...optionals}`. That is the `args` shape `callTool`
   receives (built by M5.T2); S1 forwards it verbatim.
5. **`teach.go`'s `appendWarning(result *mcp.CallToolResult, text string)`** exists
   or will exist (P1.M3.T1.S2, parallel). S1 does NOT call it; M5.T2 calls it on the
   `*mcp.CallToolResult` S1 returns. S1 returns z.ai's result UNCHANGED.

## User Persona

**Target User**: (1) **P1.M4.T1.S2** (auth threading), which wraps
`newUpstreamHTTPClient()`'s Transport with a `RoundTripper` that sets
`Authorization` from `ctx.Value(authHeaderKey{})` and rebuilds the session when auth
changes — it MODIFIES `ensureSession`/the HTTPClient construction S1 ships. (2)
**P1.M4.T1.S3** (session-expiry re-init), which detects a 404/invalid-session from
`CallTool`'s error, re-runs `initialize` once transparently, and retries — it adds a
re-init path around `ensureSession`/`callTool`. (3) **P1.M5.T2** (server dispatch),
which constructs `&UpstreamClient{upstream: cfg.Upstream, targetTool: cfg.TargetTool,
targetParam: cfg.TargetParam}`, calls `res, err := upstream.callTool(ctx, args)`, and
`appendWarning`s when `shouldWarn`. (4) **P1.M5.T3** (server_test.go e2e), whose
case 6 (session-expiry) and the delegate path assert `callTool`'s contract.

**Use Case**: Agent calls `web_search` with `{"query": "rust async runtime"}`. M5.T2
extracts the query, builds `args := ToUpstreamArgs("search_query")` =
`{"search_query":"rust async runtime"}`, and calls `upstream.callTool(ctx, args)`.
The FIRST call triggers `ensureSession` → `Connect` performs the z.ai initialize
handshake (lazily, once) → `CallTool` POSTs `tools/call` to `web_search_prime` with
`{"search_query":"rust async runtime"}` → z.ai returns the SSE result → `callTool`
returns the `*mcp.CallToolResult`. Subsequent calls reuse the session.

**User Journey**: server boot (no upstream session yet) → first `tools/call` →
`callTool` → `ensureSession` (mutex; `Connect` handshake; store session) → `CallTool`
→ result → M5.T2 appends warning → response. Second `tools/call` → `callTool` →
`ensureSession` (session exists; fast return) → `CallTool` (reused session) → result.

**Pain Points Addressed**: (1) PRD §11.1 "one shared session, initialized lazily,
guarded by a mutex" — without the mutex, concurrent first-calls would both
`Connect`, racing on `u.session`. (2) PRD §11.2 "no hard body timeout" — a naive
`http.Client.Timeout` would cut off long SSE result streams; `ResponseHeaderTimeout`
detects a dead upstream without bounding the body. (3) The SDK's per-request context
vs. shared-session tension — verified the SDK detaches the connection context, so the
lazy session survives the first request's cancellation (research §4).

## Why

- Implements **PRD §11.1 (Session lifecycle)**: one shared `mcp.ClientSession` to
  z.ai, initialized lazily on first `tools/call`, guarded by a mutex; the session is
  reused across calls.
- Implements **PRD §11.2 (Timeouts and cancellation)**: the outbound request
  lifetime is governed by the client context (not a hard body timeout), with a
  ~30s response-header timeout for liveness.
- Implements the delegation core of **FR-5**: maintain one MCP client session via
  the SDK's `StreamableClientTransport` + `client.Connect`; on each `tools/call`,
  send one `tools/call` to z.ai's `web_search_prime`.
- **Decouples the upstream client from auth, re-init, and dispatch.** S1 ships the
  struct + lazy init + callTool; S2 adds auth; S3 adds re-init; M5.T2 wires it into
  the handler. Each stage's contract is fixed so the others don't change.
- **Establishes the fake-z.ai test harness** that S2/S3/M5.T3 will extend (S3's
  session-expiry test injects a 404 into the fake; M5.T3's e2e points the OUR server
  at a fake z.ai).

## What

`upstream.go` (new) with `UpstreamClient`, `newUpstreamHTTPClient`, `ensureSession`,
`callTool`. `upstream_test.go` (new) with a fake-z.ai `httptest` harness. Visible
behavior: the first `callTool` lazily opens a shared z.ai session; `callTool`
delegates `web_search_prime` with the given args and returns z.ai's
`*mcp.CallToolResult` untouched. No auth yet (S2); no re-init yet (S3); not wired
into any handler yet (M5.T2).

### Success Criteria

- [ ] `type UpstreamClient struct` exists in `upstream.go` with EXACTLY five fields:
      `mu sync.Mutex`, `session *mcp.ClientSession`, `upstream string`,
      `targetTool string`, `targetParam string`.
- [ ] `func newUpstreamHTTPClient() *http.Client` exists; its `Transport` is a
      `*http.Transport` with `ResponseHeaderTimeout == 30 * time.Second`, and the
      `Client.Timeout == 0` (no hard deadline).
- [ ] `func (u *UpstreamClient) ensureSession(ctx context.Context) error` exists;
      when `u.session == nil` it builds the transport + client, calls `Connect`, and
      stores the session; it is guarded by `u.mu`; on `Connect` failure it leaves
      `u.session == nil` (so the next call retries).
- [ ] `func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any)
      (*mcp.CallToolResult, error)` exists; it calls `ensureSession`, then reads
      `u.session` UNDER `u.mu`, then calls `session.CallTool` OUTSIDE `u.mu` with
      `&mcp.CallToolParams{Name: u.targetTool, Arguments: args}`.
- [ ] Against a fake z.ai: the FIRST `callTool` sets `u.session` non-nil; the fake
      recorded `tool == u.targetTool` and the exact `args`; `callTool` returns the
      fake's `*mcp.CallToolResult` with `Content`/`IsError` untouched.
- [ ] A SECOND `callTool` reuses the SAME `u.session` pointer (no re-`Connect`) —
      proven by snapshotting the pointer after the first call.
- [ ] `go test -race -run Upstream ./...` is green: N concurrent `callTool` calls
      all succeed, no data race, `u.session` is a single non-nil session.
- [ ] `callTool` against a dead/garbage upstream returns a non-nil error and leaves
      `u.session == nil` (retryable).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat go.mod` is empty; `go doc . UpstreamClient` shows the
      lazy-session + mutex + timeout rationale (Mode A).

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) every SDK signature is quoted VERBATIM with source file:line cites (NewClient
client.go:44, Connect client.go:255, CallTool client.go:990, StreamableClientTransport
streamable.go:1505, CallToolParams protocol.go, NewStreamableHTTPHandler
streamable.go:194, NewServer server.go:157, AddTool server.go:238) — all
re-verified in this PRP's research (§1, §2); (b) the fake-z.ai test pattern is PROVEN
by a throwaway PoC that connected a real StreamableClientTransport to a real MCP
server over httptest and recorded a tools/call (research §3); (c) the race-safe
mutex design is given as exact Go (research §5) — the only write of `u.session` is
under `u.mu`, the only read is under `u.mu`, and `CallTool` runs on a local copy
outside the lock; (d) the `Connect` is synchronous + context-detached facts (research
§4) justify the lazy shared session surviving the first request's cancellation; (e)
the HTTPClient config is pinned (ResponseHeaderTimeout=30s, Timeout=0) with the
verified rationale; (f) the S1/S2/S3/M5.T2 boundaries are explicit (S1 ships no auth,
no re-init, no wiring); (g) the args shape callTool receives is pinned
(`ToUpstreamArgs` → `{targetParam: Query, ...optionals}`).

### Documentation & References

```yaml
# MUST READ — the lifecycle + timeout rules this item implements.
- file: PRD.md
  section: "§11.1 Session lifecycle" + "§11.2 Timeouts and cancellation"
  why: §11.1 = one shared session, lazily initialized on first tools/call, guarded
        by a mutex, reused across calls. §11.2 = outbound lifetime governed by the
        client context (NOT a hard body timeout); a ~30s response-header timeout
        detects a dead upstream. These ARE the UpstreamClient invariants.
  critical: §11.1 "Guarded by a mutex; concurrent inbound calls may share it."
        §11.2 "not a hard body timeout, so long SSE result streams are not cut off."

- file: PRD.md
  section: "FR-5 Upstream delegation" + "§8 Verified transport contract" + "§17 Headers, credentials, security"
  why: FR-5 = maintain one MCP client session via StreamableClientTransport +
        Connect; send one tools/call to web_search_prime. §8 = the z.ai endpoint +
        the SSE result shape. §17 = forward Authorization verbatim (S2's job; S1
        builds the no-auth skeleton and documents the seam).
  critical: S1 does NOT add Authorization (S2 does). S1 returns z.ai's result
        UNCHANGED (no warning append — M5.T2 + teach.go do that).

# MUST READ — the SDK API, VERIFIED from v1.6.1 source (the authoritative reference).
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§2 CLIENT SIDE" + "§3 CONTENT TYPES" + "§4 TESTING" + "§6 AUTH/CONTEXT"
  why: §2 has NewClient/Connect/CallTool/StreamableClientTransport/CallToolParams
        with exact signatures; §3 the Content/TextContent/CallToolResult types;
        §4 the in-memory + httptest test patterns; §6 the auth-context seam (S2).
  critical: Connect "performs the initialize handshake"; StreamableClientTransport
        has HTTPClient for RoundTripper auth injection (S2); the connection context
        is detached (xcontext.Detach) so context values survive.

# THIS ITEM'S RESEARCH — source-verified facts + the proven fake-z.ai PoC + the race design.
- docfile: plan/002_0a8ab3410994/P1M4T1S1/research/upstream-client-design.md
  why: §1 every client-side signature (file:line); §2 the server-side API for the
        fake z.ai; §3 the PROVEN fake-z.ai PoC (Connect+CallTool against an
        httptest MCP server in ~5ms); §4 Connect is synchronous + context detached
        (lazy session safe); §5 the race-safe mutex design (exact Go); §6 the
        HTTPClient config; §7 the args shape; §8 the consumer+auth seams; §9 the
        standalone-SSE decision.
  section: all sections are load-bearing; §3 + §5 are the implementation-critical ones.

# MUST READ — the file that PRODUCES the args callTool receives (frozen contract).
- file: extract.go
  section: "func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any" (extract.go:315)
  why: emits {targetParam: Query, ...optionals}. M5.T2 calls it; S1's callTool
        receives its output as `args` and forwards it as CallToolParams.Arguments.
  pattern: args is a map[string]any with cfg.TargetParam as the query key.
  gotcha: S1 does NOT call extract or ToUpstreamArgs — the server (M5.T2) does. S1
        treats `args` as an opaque map[string]any and forwards it verbatim.

# MUST READ — the consumer of callTool's result (parallel, frozen contract).
- file: teach.go
  section: "func appendWarning(result *mcp.CallToolResult, text string)" (teach.go:86)
  why: M5.T2 calls appendWarning on the *mcp.CallToolResult S1 returns. S1 does NOT
        append warnings — it returns z.ai's result UNCHANGED. Confirms the return
        type is the SDK's *mcp.CallToolResult (not a custom type).

# MUST READ — the auth seam S1 documents but does NOT implement.
- file: main.go
  section: "authHeaderKey struct{}" + "authMiddleware" (P1.M1.T2.S2)
  why: the inbound Authorization is stored in the request context under
        authHeaderKey{}. S2 reads ctx.Value(authHeaderKey{}) and injects it. S1's
        doc comment names this seam; S1's code does not read it.

# CODEBASE CONVENTIONS — follow these patterns.
- file: extract_test.go
  why: the project's test conventions: package main; table-driven + t.Run; local
        decoupling constants. upstream_test.go follows the same shape (the fake-z.ai
        harness is a t.Helper builder).
- file: logger.go / health.go
  why: the post-pivot file-per-module layout (one .go per concern + its _test.go).
        upstream.go + upstream_test.go follow it (module = upstream delegation).

# CONFIG (read-only) — proves the import path is in go.mod and the fields exist.
- file: go.mod
  why: already requires github.com/modelcontextprotocol/go-sdk v1.6.1 (P1.M1.T2.S1).
        upstream.go adding the mcp import needs NO go.mod change.
- file: config.go
  why: Config.Upstream, Config.TargetTool ("web_search_prime"), Config.TargetParam
        ("search_query") exist (P1.M1.T1.S1). UpstreamClient is built from them in M5.T2.

# Go MCP SDK (stable v1.6.1 — verified from source; pkg.go.dev renders the same).
- url: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#Client.Connect
  why: Connect performs the initialize handshake and returns a *ClientSession.
- url: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#ClientSession.CallTool
  why: CallTool(ctx, *CallToolParams) (*CallToolResult, error) — the delegation call.
- url: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#StreamableClientTransport
  why: the transport with Endpoint + HTTPClient (custom client for auth RoundTripper in S2).
- url: https://pkg.go.dev/net/http#Transport
  why: ResponseHeaderTimeout "does not include the time to read the response body" —
        the rationale for 30s header timeout + no Client.Timeout (PRD §11.2).
```

### Current Codebase tree (the INPUT state — run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap; authHeaderKey{} + authMiddleware (P1.M1.T2.S2); imports mcp — UNTOUCHED
  config.go       # Config{Upstream,TargetTool,TargetParam,...} (P1.M1.T1) — UNTOUCHED
  logger.go, health.go — UNTOUCHED
  extract.go      # ExtractionResult + ToUpstreamArgs (P1.M2.T1, COMPLETE) — UNTOUCHED
  extract_test.go # extract tests — UNTOUCHED (the test-style analog to mirror)
  teach.go        # shouldWarn/warningText/noQueryWarningText/appendWarning/noQueryResult (P1.M3.T1) — UNTOUCHED
  teach_test.go   # teach tests — UNTOUCHED
  config_test.go, resolve_test.go, health_test.go, logger_test.go — green — UNTOUCHED
  testdata/*.sse  # legacy v1 fixtures (unused by v2; kept) — UNTOUCHED
  README.md, config.example.json, PRD.md — UNTOUCHED
  # NO upstream.go / upstream_test.go yet — THIS ITEM CREATES THEM.
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
upstream.go        # CREATE. package main. Imports: net/http, sync, time,
                   #   github.com/modelcontextprotocol/go-sdk/mcp.
                   #   type UpstreamClient struct { mu sync.Mutex; session *mcp.ClientSession;
                   #       upstream, targetTool, targetParam string }
                   #   func newUpstreamHTTPClient() *http.Client   (ResponseHeaderTimeout=30s, no Timeout)
                   #   func (u *UpstreamClient) ensureSession(ctx) error   (mutex-guarded lazy init)
                   #   func (u *UpstreamClient) callTool(ctx, args) (*mcp.CallToolResult, error)
                   #   Mode-A doc comment on UpstreamClient (lazy shared session, mutex, timeout rationale).
upstream_test.go   # CREATE. package main. Imports: context, encoding/json, net/http,
                   #   net/http/httptest, sync, sync/atomic, testing, time, mcp.
                   #   newFakeZAI(t) (*httptest.Server, *atomic.Int32 calls, *fakeState) helper.
                   #   TestNewUpstreamHTTPClient, TestUpstreamClient_LazyInitAndCallTool,
                   #   TestUpstreamClient_LazyReuse, TestUpstreamClient_Concurrent,
                   #   TestUpstreamClient_EnsureSessionError.
# NO other files created or modified. go.mod unchanged.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL — UpstreamClient has EXACTLY five fields (contract): mu sync.Mutex,
// session *mcp.ClientSession, upstream string, targetTool string, targetParam string.
// Do NOT add an auth field, an httpClient field, or a transport field in S1 — those
// are S2/S3's extensions. The HTTPClient is built INSIDE ensureSession via
// newUpstreamHTTPClient(). (S2 will modify newUpstreamHTTPClient/ensureSession to
// wrap the Transport with an auth RoundTripper; S3 will add re-init.)

// CRITICAL — NO http.Client.Timeout. A non-zero Timeout is a whole-exchange deadline
// that INCLUDES reading the response body and would cut off z.ai's long SSE result
// streams (PRD §11.2; external_deps §5; verified: ResponseHeaderTimeout "does not
// include the time to read the response body"). Set Transport.ResponseHeaderTimeout
// = 30s; leave Client.Timeout == 0. The test asserts both.

// CRITICAL — race-safe session access. ensureSession is the ONLY writer of u.session
// (under u.mu). callTool reads u.session UNDER u.mu, then calls CallTool on the LOCAL
// copy OUTSIDE u.mu (so concurrent calls proceed in parallel). Never read u.session
// without the lock — `go test -race` will flag it. The contract's ensureSession
// returns only `error` (not the session), which is why callTool must re-read under
// the lock. See research §5 for the exact pattern.

// CRITICAL — hold the mutex during Connect (network I/O). This is deliberate: it
// serializes concurrent FIRST calls so only ONE Connect happens (preventing
// double-init). Subsequent calls find u.session != nil and return immediately under
// the lock; CallTool runs outside the lock. Holding a lock over I/O is normally an
// anti-pattern, but for a lazy-init-once shared resource it is the correct guard.
// (Alternative: sync.Once — but it does not support retry-after-error; the nil-check
// under the mutex does: a failed Connect leaves session nil → next call retries.)

// CRITICAL — Connect performs the FULL initialize handshake SYNCHRONOUSLY (client.go:
// 256-292: connect -> handleSend[*InitializeResult](initialize) AWAITS the response ->
// notify(initialized)). So ensureSession's Connect call returns only after the
// handshake completes (or fails). On failure Connect calls cs.Close() and returns a
// non-nil error — ensureSession must NOT store the session on error (leave it nil).

// CRITICAL — the SDK DETACHES the connection context (streamable.go:223, xcontext.
// Detach). The first inbound request's ctx is used for Connect, but the session
// SURVIVES that ctx's cancellation (the per-request ctx dies when the response is
// sent; the shared session persists). So the lazy shared session is safe across
// requests. callTool passes each request's own ctx to CallTool (per-request
// cancellation), reusing the shared session.

// CRITICAL — S1 builds the HTTPClient WITHOUT Authorization. S1's UpstreamClient
// connects to a no-auth fake z.ai (tests) but would be REJECTED by real z.ai until
// S2 adds the auth RoundTripper. This is the deliberate S1/S2 split. Do NOT read
// ctx.Value(authHeaderKey{}) in S1 — document the seam in the doc comment only.

// GOTCHA — ClientSession has a Close() method (client.go:341). S1 does NOT add a
// Close() to UpstreamClient (out of scope; M5.T2 owns the lifecycle and will add it
// or call u.session.Close() on shutdown). Tests close u.session directly in a defer
// (same package; unexported field accessible) to avoid leaking the connection.

// GOTCHA — StreamableClientTransport default DisableStandaloneSSE=false sends a
// persistent GET after initialize (server-initiated messages). The PoC used the
// default against an SDK fake and worked. Follow the contract literally (two fields:
// Endpoint + HTTPClient). If real-z.ai testing shows background SSE reconnect churn,
// set DisableStandaloneSSE:true (S3's scope). Documented in research §9.

// GOTCHA — NewStreamableHTTPHandler's getServer takes *http.Request, NOT *mcp.Request
// (streamable.go:194). The fake-z.ai harness uses func(*http.Request) *mcp.Server.

// GOTCHA — AddTool's t.InputSchema MUST be non-nil and type "object" (server.go:238
// panics otherwise). The fake z.ai's web_search_prime tool uses
// json.RawMessage(`{"type":"object","properties":{"search_query":{"type":"string"}},
// "additionalProperties":true}`).

// GOTCHA — keep UpstreamClient/newUpstreamHTTPClient/ensureSession/callTool in
// package main (the project is a single package; extract.go/teach.go/main.go are all
// package main). callTool/ensureSession are UNEXPORTED (lowercase) per the contract;
// M5.T2 (same package) calls them directly.

// GOTCHA — go.mod is UNCHANGED. The SDK require already exists (P1.M1.T2.S1) and
// main.go already imports mcp. Do NOT run `go get` or edit go.mod. Verify with
// `git diff --stat go.mod` == empty.
```

## Implementation Blueprint

### Data models and structure

```go
// UpstreamClient owns ONE shared MCP client session to z.ai (PRD §11.1). It is
// constructed once (P1.M5.T2) from cfg.Upstream/cfg.TargetTool/cfg.TargetParam and
// shared across all inbound tools/call requests.
type UpstreamClient struct {
	mu          sync.Mutex
	session     *mcp.ClientSession
	upstream    string // cfg.Upstream (z.ai MCP endpoint URL)
	targetTool  string // cfg.TargetTool ("web_search_prime"); never advertised
	targetParam string // cfg.TargetParam ("search_query"); informational (args are pre-built by the caller)
}
```

`targetParam` is stored for completeness/future use (S3 logging may cite it); S1's
`callTool` forwards the caller-built `args` verbatim and does not itself key on
`targetParam`. Deps: stdlib `net/http`, `sync`, `time` + SDK `mcp`.

### Reference implementation (CREATE `upstream.go`)

```go
package main

import (
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
```

> NOTE: `"context"` is referenced in the signatures (`context.Context`) — add it to
> the import block. The final import block (gofmt-sorted) is: `"context"`,
> `"net/http"`, `"sync"`, `"time"`, then `"github.com/modelcontextprotocol/go-sdk/mcp"`.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the SDK + config + seam inputs exist
  - RUN: grep -q "modelcontextprotocol/go-sdk v1.6.1" go.mod \
        && grep -q "authHeaderKey struct{}" main.go \
        && grep -q "TargetTool" config.go \
        && grep -q "func (r ExtractionResult) ToUpstreamArgs" extract.go \
        && test ! -f upstream.go
  - EXPECT: exit 0 (SDK required, auth seam + TargetTool + ToUpstreamArgs present,
        upstream.go NOT yet created). IF ANY FAIL: STOP — a prerequisite has not run
        or upstream.go already exists (do not clobber).

Task 1: CREATE upstream.go (paste the Reference implementation above)
  - FILE: upstream.go (NEW). package main. Imports: context, net/http, sync, time,
        github.com/modelcontextprotocol/go-sdk/mcp (gofmt-sorted).
  - PASTE: UpstreamClient (5 fields, EXACTLY), newUpstreamHTTPClient, ensureSession,
        callTool, each WITH its Mode-A doc comment. `version` is the health.go
        package var (already in scope; same package).
  - CONSTRAINTS: ensureSession holds u.mu during Connect and leaves u.session nil on
        error; callTool reads u.session under u.mu and calls CallTool OUTSIDE u.mu;
        newUpstreamHTTPClient sets ResponseHeaderTimeout=30s and Timeout=0. NO auth
        read in S1. NO Close() method in S1. NO wiring into any handler.
  - PLACEMENT: repo root (package main).

Task 2: CREATE upstream_test.go (the fake-z.ai harness + tests)
  - FILE: upstream_test.go (NEW). package main. Imports: context, encoding/json,
        net/http, net/http/httptest, sync, sync/atomic, testing, time,
        github.com/modelcontextprotocol/go-sdk/mcp.
  - ADD newFakeZAI(t) (*httptest.Server, *fakeState): builds a REAL mcp.Server
        ("fake-zai") that registers u.targetTool's name (use "web_search_prime") via
        AddTool with InputSchema json.RawMessage type:object, records each tools/call
        (Name + json.Unmarshal Arguments) into fakeState, and returns a canned
        *mcp.CallToolResult. Wraps it in mcp.NewStreamableHTTPHandler(func(*http.Request)
        *mcp.Server { return zai }, nil) + httptest.NewServer. t.Cleanup(srv.Close).
        See "Test harness" below.
  - ADD the five tests below (table/t.Run where natural). Each builds an
        &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime",
        targetParam: "search_query"} and exercises callTool. defer u.session.Close()
        when a session is created (same package; unexported field accessible).
  - CONSTRAINTS: NO real network (only the in-process httptest fake). Each test uses
        a context.WithTimeout(context.Background(), ...) so it cannot hang.

Task 3: VALIDATE
  - gofmt -w upstream.go upstream_test.go
  - go vet ./...
  - go build ./...
  - go test -run Upstream -v              # the new tests (no race)
  - go test -race -run Upstream -v        # the concurrency test under the race detector
  - go test ./...                         # full suite green (no regressions)
  - ALL green. git diff --stat must show ONLY upstream.go + upstream_test.go (new files).
  - git diff --stat go.mod must be EMPTY.
  - go doc . UpstreamClient                # Mode A: prints lazy-session + mutex + timeout rationale.
```

### Test harness + tests (CREATE `upstream_test.go`)

```go
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
	calls    int32           // atomic count of tools/call handled
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
		Name: "web_search_prime",
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
```

### Implementation Patterns & Key Details

```go
// PATTERN: lazy shared resource, mutex-guarded, retry-after-error. ensureSession
// checks-then-creates under u.mu; a FAILED Connect leaves session nil so the next
// call retries (unlike sync.Once, which permanently fails). Holding the lock over
// the network Connect is deliberate (prevents double-init); only the FIRST call pays.

// PATTERN: read-shared-field-under-lock, use-local-copy-outside-lock. callTool reads
// u.session under u.mu (race-safe) into a local `sess`, then calls sess.CallTool
// WITHOUT the lock so concurrent calls run in parallel. This is the standard Go
// pattern for a lazily-initialized shared, mutable-once resource.

// PATTERN: proven fake-z.ai test harness. A REAL mcp.Server over httptest stands in
// for z.ai; the UpstreamClient's StreamableClientTransport connects to it for real,
// exercising lazy-init + handshake + callTool end-to-end with no network. The fake
// records Name + Arguments so tests assert callTool forwarded the right tool + args.

// GOTCHA (restated): NO http.Client.Timeout. ResponseHeaderTimeout only. A Timeout
// would cut off long SSE result streams (PRD §11.2). Test pins Timeout==0.

// GOTCHA (restated): Connect is synchronous + the context is detached. The lazy
// session built with the first request's ctx SURVIVES that ctx's cancellation (SDK
// xcontext.Detach). So the shared session is safe across requests. callTool passes
// each request's own ctx to CallTool (per-request cancellation on the shared session).

// GOTCHA (restated): S1 ships NO auth. The HTTPClient has no Authorization. S1 is
// tested against the no-auth fake; real z.ai rejects it until S2. Do not read
// authHeaderKey in S1.

// GOTCHA (restated): the SDK fake supports the default standalone SSE, so the default
// StreamableClientTransport (DisableStandaloneSSE=false) works in tests. Follow the
// contract (two fields). DisableStandaloneSSE:true is a S3 robustness lever if real
// z.ai shows reconnect churn.
```

### Integration Points

```yaml
FILES CREATED:
  - upstream.go       (NEW): UpstreamClient + newUpstreamHTTPClient + ensureSession +
        callTool + Mode-A doc comment. Imports context/net/http/sync/time + mcp.
  - upstream_test.go  (NEW): newFakeZAI harness + 5 tests.
NO OTHER FILES TOUCHED:
  - main.go, config.go, extract.go, teach.go, logger.go, health.go, doc.go: UNTOUCHED.
  - go.mod/go.sum: UNCHANGED (SDK require already present; main.go already imports mcp).
  - All *_test.go: UNTOUCHED.
CONSUMER SEAMS (S1 freezes the struct + method signatures; later items build on them):
  - P1.M4.T1.S2 (auth): wraps newUpstreamHTTPClient()'s Transport with an auth
        RoundTripper reading ctx.Value(authHeaderKey{}); rebuilds the session when
        auth changes. May modify ensureSession / add a field. S1's signatures stay.
  - P1.M4.T1.S3 (re-init): on a session-expiry error from CallTool, re-run initialize
        once transparently (re-enter ensureSession after resetting u.session to nil
        under the mutex) and retry. Adds the re-init path around callTool.
  - P1.M5.T2 (server dispatch): constructs &UpstreamClient{upstream: cfg.Upstream,
        targetTool: cfg.TargetTool, targetParam: cfg.TargetParam}; calls
        res, err := upstream.callTool(ctx, extract(...).ToUpstreamArgs(cfg.TargetParam));
        appendWarning(res, ...) when shouldWarn; returns res.
  - P1.M5.T3 (server_test.go e2e): case 6 (session-expiry) and the delegate path
        assert callTool's contract via a fake z.ai (the newFakeZAI harness or a peer).
DATABASE / ROUTES / ENV: none. UpstreamClient is constructed in M5.T2; S1 creates the
        type + tests it in isolation against a fake. No handler wiring in S1.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w upstream.go upstream_test.go
go vet ./...
go build ./...                 # must compile; the mcp import resolves (already in go.mod)

# upstream.go imports the expected set (context, net/http, sync, time, mcp):
grep -A6 '^import' upstream.go

# UpstreamClient has EXACTLY five fields:
grep -A6 'type UpstreamClient struct' upstream.go

# newUpstreamHTTPClient sets ResponseHeaderTimeout and does NOT set Timeout:
grep -n 'ResponseHeaderTimeout\|Timeout =' upstream.go   # one ResponseHeaderTimeout=30s; no Client.Timeout

# No auth read in S1 (the seam is S2's):
grep -n 'authHeaderKey' upstream.go   # expect: ONLY in doc comments, never in code

# go.mod UNCHANGED:
git diff --stat go.mod          # expect EMPTY

# Only the two new files changed (no other edits):
git status --short              # expect: ?? upstream.go  ?? upstream_test.go

# Expected: zero errors; 5-field struct; ResponseHeaderTimeout=30s, no Timeout; no auth
# read; go.mod clean; only the two new files.
```

### Level 2: Unit Tests (Component Validation)

```bash
# New tests, no race.
go test -run Upstream -v

# Concurrency under the race detector (the mutex test).
go test -race -run Upstream -v

# MUST PASS:
#   TestNewUpstreamHTTPClient          -> ResponseHeaderTimeout==30s, Timeout==0
#   TestUpstreamClient_LazyInitAndCallTool -> session nil->non-nil; fake saw web_search_prime
#                                            + exact args; result Content/IsError verbatim
#   TestUpstreamClient_LazyReuse       -> session pointer STABLE across 2 calls; fake saw 2 calls
#   TestUpstreamClient_Concurrent      -> 16 goroutines, all succeed, single session (under -race)
#   TestUpstreamClient_EnsureSessionError -> non-MCP upstream -> error, session stays nil
# Expected: PASS, exit 0 (with AND without -race). If LazyInit fails, the fake-z.ai
# harness is the usual suspect — re-check getServer takes *http.Request and InputSchema
# is a type:object json.RawMessage. If Concurrent fails under -race, u.session is being
# read/written without the lock — re-check callTool reads under u.mu.
```

### Level 3: Integration Testing (System Validation)

```bash
# Confirm the module + full suite stay healthy with the new file + the SDK client use.
go build ./...        # must compile (UpstreamClient is unused by non-test code so far — still builds)
go test ./...         # config/resolve/logger/health/extract/teach/upstream — ALL green
go doc . UpstreamClient   # Mode A: lazy-session + mutex + timeout rationale
go doc . newUpstreamHTTPClient
go doc . callTool

# Expected: build clean; full suite green; go doc shows the Mode-A rationale. NOTE:
# real-z.ai delegation is NOT exercised here (needs an API key + auth, which land in S2)
# and the handler wiring lands in M5.T2. S1 proves the UpstreamClient against an
# in-process fake z.ai end-to-end (lazy init, handshake, callTool, reuse, concurrency,
# error path).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Race-safety + no double-init under concurrency (the load-bearing mutex check).
go test -race -run 'TestUpstreamClient_Concurrent' -v
# If this fails or reports a race, u.session is accessed without the lock — fix before
# proceeding. The single shared session after 16 concurrent calls proves no double-init.

# (b) Retry-after-error: a failed init leaves session nil so the NEXT call retries.
#     (Extend TestUpstreamClient_EnsureSessionError conceptually: after the failed call,
#      pointing u at a GOOD fake and calling again should succeed — S3 formalizes this
#      as the re-init path; S1 just guarantees session is nil after failure.)
go test -run 'TestUpstreamClient_EnsureSessionError' -v

# (c) Confirm the no-hard-timeout invariant by code inspection (the SSE-truncation guard).
grep -n 'Timeout' upstream.go | grep -v 'ResponseHeaderTimeout'   # expect: no Client.Timeout assignment

# Expected: (a) PASS under -race; (b) session nil after failure (retryable); (c) no
# Client.Timeout anywhere (only ResponseHeaderTimeout). These three are the strongest
# production-correctness guarantees S1 gives its consumers (S2/S3/M5.T2).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: gofmt clean; `go vet ./...` and `go build ./...` exit 0;
      upstream.go imports context/net/http/sync/time + mcp; 5-field struct;
      ResponseHeaderTimeout=30s and no Client.Timeout; no authHeaderKey read in code;
      `git diff --stat go.mod` empty; only upstream.go + upstream_test.go added.
- [ ] Level 2 passes: `go test -run Upstream -v` and `go test -race -run Upstream -v`
      both PASS (all 5 tests).
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc .
      UpstreamClient` shows the lazy-session + mutex + timeout rationale (Mode A).

### Feature Validation

- [ ] `UpstreamClient` has exactly `mu sync.Mutex`, `session *mcp.ClientSession`,
      `upstream string`, `targetTool string`, `targetParam string`.
- [ ] `newUpstreamHTTPClient` sets `Transport.ResponseHeaderTimeout == 30s` and
      `Client.Timeout == 0`.
- [ ] First `callTool` lazily creates the session; the fake recorded
      `tool==targetTool` and the exact args; the result is returned with
      `Content`/`IsError` verbatim.
- [ ] Second `callTool` reuses the SAME session pointer; the fake saw 2 calls.
- [ ] 16 concurrent `callTool` calls all succeed under `-race`; single shared session.
- [ ] A non-MCP upstream makes `callTool` return a non-nil error and leaves
      `u.session == nil` (retryable).

### Code Quality Validation

- [ ] upstream.go / upstream_test.go follow the post-pivot file-per-module layout
      (one .go + one _test.go per concern) and the extract_test.go conventions
      (package main; t.Helper builders; context.WithTimeout to prevent hangs).
- [ ] Doc comment on `UpstreamClient` is Mode-A (cites PRD §11.1/§11.2; the lazy
      shared session; the mutex guarding + the read-under-lock/use-outside-lock
      pattern; the no-hard-timeout + ResponseHeaderTimeout rationale; the S2 auth
      seam).
- [ ] No anti-patterns (see below); no auth read in S1; no Close() in S1; no handler
      wiring in S1; no extra struct fields.

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . UpstreamClient` prints the rationale. NO
      README/config.example.json/doc.go changes (the upstream client is internal,
      surfaced only via the M5.T2 handler that calls callTool).
- [ ] No new env vars / config keys / routes / go.mod requires introduced.

---

## Anti-Patterns to Avoid

- ❌ Don't add fields to `UpstreamClient` beyond the five. S2 (auth) and S3 (re-init)
  own their extensions; S1 ships exactly `mu`, `session`, `upstream`, `targetTool`,
  `targetParam`. An auth/httpClient/transport field in S1 pre-empts S2's design.
- ❌ Don't set `http.Client.Timeout`. A non-zero Timeout includes reading the body and
  truncates z.ai's long SSE result streams (PRD §11.2). Use
  `Transport.ResponseHeaderTimeout=30s` only. The test pins `Timeout==0`.
- ❌ Don't read `u.session` without `u.mu`. `ensureSession` writes it under the mutex;
  `callTool` must read it under the mutex too, then call `CallTool` on the LOCAL copy
  outside the lock. An unlocked read is a data race (`-race` fails) and (under
  concurrency) a torn read. See research §5.
- ❌ Don't call `CallTool` while holding `u.mu`. That serializes every call through the
  mutex, defeating "concurrent calls share the session". Read `u.session` under the
  lock into a local, release the lock, then `CallTool`.
- ❌ Don't use `sync.Once` for lazy init. It does not support retry-after-error: a
  failed `Connect` would permanently poison the client. The nil-check-under-mutex
  leaves `session` nil on failure so the next call retries.
- ❌ Don't store the session on a failed `Connect`. On error, return the error and
  leave `u.session == nil` (the SDK's `Connect` already closes its partial connection).
  Storing a dead/half session would break retry.
- ❌ Don't add Authorization in S1. S1 builds the HTTPClient WITHOUT auth and is tested
  against a no-auth fake. Reading `ctx.Value(authHeaderKey{})` or wrapping a RoundTripper
  is S2's scope. S1 documents the seam in the doc comment only. (Real z.ai rejects S1's
  no-auth client until S2 lands — that is the deliberate split.)
- ❌ Don't add a `Close()` method in S1. The lifecycle owner is M5.T2. Tests close
  `u.session` directly in a `defer` (same package; unexported field accessible).
- ❌ Don't wire `UpstreamClient` into any handler or `main.go`. M5.T2 constructs it and
  calls `callTool` from the tool handler. S1 creates the type + tests it in isolation.
- ❌ Don't append warnings or transform z.ai's result. `callTool` returns z.ai's
  `*mcp.CallToolResult` UNCHANGED. The teaching warning is appended by M5.T2 via
  teach.go's `appendWarning`. S1 never touches `Content`/`IsError`.
- ❌ Don't call `extract` or `ToUpstreamArgs` in `upstream.go`. The server (M5.T2) builds
  the args; `callTool` receives an opaque `map[string]any` and forwards it verbatim as
  `CallToolParams.Arguments`. Keeping extraction out keeps upstream.go a pure
  delegation client.
- ❌ Don't edit `go.mod`/`go.sum`. The SDK `require` already exists (P1.M1.T2.S1) and
  `main.go` already imports `mcp`. Adding the import to upstream.go needs no module change.
- ❌ Don't build the fake z.ai's `getServer` with `*mcp.Request`. It takes `*http.Request`
  (streamable.go:194). And don't forget `InputSchema` must be a non-nil `type:"object"`
  JSON or `AddTool` panics (server.go:238).
- ❌ Don't let a test hang. Every test uses `context.WithTimeout(context.Background(),
  ...)`. The fake z.ai is in-process (no real network), so tests are fast, but the
  timeout guarantees no hang if the SDK's background SSE or handshake misbehaves.
