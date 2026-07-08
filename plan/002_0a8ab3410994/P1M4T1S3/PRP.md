name: "P1.M4.T1.S3 — Session-expiry re-init + honest error surfacing + result handling (PRD §11.1, §11.2, §11.3)"
description: |

  MAKE `UpstreamClient.callTool` resilient to a z.ai session-expiry signal
  (HTTP 404 / invalid-session), surface upstream failures HONESTLY (never
  synthesize a fake result), and return z.ai's result Content INTACT (the
  stringified-array text payload). PRD §11.1, §11.2, §11.3, §8, §15, FR-5, FR-6.

  The mechanism, FULLY SOURCE-VERIFIED against go-sdk v1.6.1 and PROVEN by a
  throwaway PoC (see research/session-expiry-reinit.md §3), is:

  1. DETECT — the SDK maps a server-side 404 (the MCP Streamable HTTP "session
     terminated → MUST answer 404" rule, spec §2.5.3) to the exported sentinel
     `mcp.ErrSessionMissing`. Detection is `errors.Is(err, mcp.ErrSessionMissing)`
     (transport.go:37; client checkResponse streamable.go:2057-2060 wraps the
     404 as `fmt.Errorf("...: %w", ErrSessionMissing)`; CallTool client.go:990
     surfaces it). A 404 is TERMINAL — after it the connection is dead
     (streamable.go:1730-1770) so the old `*mcp.ClientSession` is unusable and a
     fresh one must be built. Transient 502/503/504/429 are a SEPARATE path
     (`jsonrpc2.ErrRejected`, retried by the transport) — S3 does NOT react to
     those.

  2. RE-INIT ONCE — build a BRAND-NEW `StreamableClientTransport` + `Client.Connect`
     (NOT re-send initialize on the dead session — its sessionID is stale and the
     server would 404 it again). `Client.Connect` does the full handshake
     (initialize → capture NEW id → notifications/initialized) synchronously
     (client.go:255-292). Concurrency-safe via a compare-and-close of the dead
     session pointer under `u.mu` (only one of N concurrent in-flight callers
     that each saw the expiry performs the re-init; the rest reuse its fresh
     session). Auth needs NO code here — the rebuilt transport's `authInjector`
     (from S2) reads the current `callTool`'s auth from context (S2 threading).

  3. RETRY — call `CallTool` ONCE on the fresh session with the same args.

  4. HONEST ERROR — if the retry STILL fails (re-init failed, or the retry's
     CallTool returned any non-nil error), return that error VERBATIM. NEVER
     synthesize a `*mcp.CallToolResult`, never fabricate/empty content, never set
     `IsError`. PRD §11.1: "If it still fails, surface the upstream error."

  5. RESULT PRESERVATION — z.ai's result Content is a JSON-encoded STRING (a
     stringified array), not an object (PRD §8). `callTool` returns the
     `*mcp.CallToolResult` from `session.CallTool` UNCHANGED on the happy path
     and on the retry-success path: it never re-parses, reorders, truncates, or
     sets `IsError`. The warning append happens LATER (M5.T2 + teach.go).

  6. LOG — emit the `upstream_error` log event (PRD §15) on expiry detected and
     on re-init failure, via a NEW nil-safe `log *logger` field. (The re-init
     attempt count is known only inside S3's retry loop, so S3 owns the emission.)

  SCOPE (all in `package main`, no new files):
  1. MODIFY `upstream.go`: add `log *logger` field (6th field, nil-safe); extract
     `connectLocked(ctx) error` from `ensureSession` (behavior-preserving
     refactor); add `reinitSession(ctx, dead) (*mcp.ClientSession, error)`
     (compare-and-close under `u.mu`); rewrite `callTool` into a retry-once-on-
     ErrSessionMissing loop with honest error surfacing; add `logUpstreamError`;
     update Mode-A doc comments (re-init-once, honest-error rule, result-
     preservation note). New imports: `errors`, `fmt` (stdlib).
  2. MODIFY `upstream_test.go`: extend `fakeState` + the `newFakeZAI` recording
     wrapper with a session-expiry layer (404 a SPECIFIC Mcp-Session-Id on
     demand) and a tool-error arm; add three tests —
     `TestUpstreamClient_SessionExpiryReinitSuccess` (the PRD §19.3 case 6
     detect→re-init→retry→success flow), `TestUpstreamClient_ReinitRetryFails-
     HonestError` (retry fails → error surfaced, result is nil = no synthesis),
     `TestUpstreamClient_NonSessionErrorNoReinit` (a non-`ErrSessionMissing`
     error is surfaced verbatim WITHOUT triggering re-init).

  No other file is touched. `go.mod` gains ZERO requires (`errors`/`fmt` are
  stdlib). NO handler wiring (M5.T2's job). NO warning append (M5.T2 + teach.go).
  NO query extraction (M5.T2 + extract.go). `newUpstreamHTTPClient`, the struct
  field set aside from the new `log`, and S1/S2's tests are otherwise UNCHANGED.

---

## Goal

**Feature Goal**: Make `UpstreamClient.callTool` transparently recover from a
z.ai session-expiry (404/invalid-session) by detecting the terminal
`mcp.ErrSessionMissing` sentinel, re-initializing the ONE shared session ONCE,
and retrying the single `tools/call`; surface any further failure honestly
without ever synthesizing a result; and return z.ai's result Content intact
(the stringified-array text payload). Exactly ONE re-init + ONE retry per
inbound call (PRD §11.1). The re-init is concurrency-safe (N concurrent callers
that each saw the expiry produce exactly one fresh session).

**Deliverable**: Two files MODIFIED at the repo root (both `package main`):
1. **MODIFY** `upstream.go` — add `log *logger` field; extract `connectLocked`;
   add `reinitSession`; rewrite `callTool` (retry-once-on-`ErrSessionMissing`,
   honest error surfacing); add `logUpstreamError`; update Mode-A doc comments.
   New imports: `"errors"`, `"fmt"` (stdlib).
2. **MODIFY** `upstream_test.go` — extend `fakeState`/`newFakeZAI` with the
   session-expiry wrapper (404 a specific `Mcp-Session-Id`) + a tool-error arm;
   add the three tests named above.

No new files. `go.mod`/`go.sum` unchanged. `main.go` UNCHANGED (M5.T2 will pass
the logger into the struct when it constructs `UpstreamClient`).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -race -run 'Upstream' ./...`) all exit clean.
Against an expiry-armed fake z.ai: a first `callTool` succeeds; after the fake
expires the live session, a second `callTool` gets `ErrSessionMissing`, re-inits
ONCE, retries, and succeeds with the real z.ai result and a NEW session pointer.
When the retry itself fails, `callTool` returns `(nil, err)` with NO synthesized
result. When the FIRST `CallTool` returns a non-session error, `callTool`
surfaces it verbatim and does NOT re-init. z.ai's result Content is returned
unchanged (no mutation, `IsError` never set by S3). S1/S2 tests stay green
unchanged (the new `log` field is nil-safe). The `upstream_error` log event
fires on expiry + re-init failure.

## Hard Prerequisites

1. **`upstream.go` + `upstream_test.go` from P1.M4.T1.S1 AND S2 EXIST and are
   green** (DONE — verified: `UpstreamClient` with 5 fields; `newUpstreamHTTPClient`
   wrapping `authInjector`; `authInjector`/`authFromContext`; `ensureSession`
   (lazy, mutex-guarded); `callTool` (single attempt); `newFakeZAI` with
   auth-recording; 7 tests). S3 is a SURGICAL edit to these two files.
2. **`authHeaderKey struct{}` + `authMiddleware` EXIST in main.go**
   (P1.M1.T2.S2 — DONE). S3's re-init runs inside a `callTool` whose ctx carries
   `authHeaderKey`; the rebuilt transport's `authInjector` reads current auth
   from context. **S3 needs NO auth-specific code** (re-affirmed by S2's PRP).
3. **`github.com/modelcontextprotocol/go-sdk v1.6.1`** required in go.mod
   (DONE). The expiry/re-init facts S3 relies on were RE-VERIFIED by reading the
   v1.6.1 source AND proven by a throwaway PoC (research §1-§9).
4. **The `*logger` API exists** (`logger.go` — DONE). `(l *logger).log(level,
   msg, fields map[string]any)`. S3 adds a `log *logger` field (nil-safe) and
   calls `u.log.log("warn", "upstream_error", {...})`.
5. **`mcp.ErrSessionMissing` is an exported sentinel in the SDK** (transport.go:37
   — VERIFIED). `errors.Is(err, mcp.ErrSessionMissing)` is the detection idiom.

## User Persona

**Target User**: (1) **P1.M5.T2** (server dispatch) — calls `res, err :=
upstream.callTool(ctx, args)`; on `err != nil` it surfaces the error to the
client (it does NOT fabricate results either); on success it appends the teaching
warning via `teach.appendWarning(res, ...)` to the SAME `*mcp.CallToolResult` S3
returned unchanged. M5.T2 constructs `&UpstreamClient{upstream, targetTool,
targetParam, log: log}` — the ONLY new constructor concern is passing `log`.
(2) **P1.M5.T3** (server_test.go e2e) — case 6 "Upstream session-expiry → one
transparent re-init, then success" (PRD §19.3) reuses EXACTLY the expiry wrapper
S3 ships. (3) **P1.M4.T1.S1/S2 tests** — must stay green unchanged (the new `log`
field defaults to nil and is a no-op).

**Use Case**: The shared z.ai session expires server-side (z.ai evicted it; or a
proxy/load-balancer reset). The agent's next `tools/call` reaches `callTool`;
the first `CallTool` POST gets a 404; the client surfaces `ErrSessionMissing`;
`callTool` detects it, re-initializes the shared session ONCE (transparently),
retries the single `tools/call`, and returns z.ai's real result. The agent never
sees the expiry. If the retry also fails, the agent sees the honest upstream
error (not a fake/empty result).

**User Journey**: inbound `tools/call` → handler (M5.T2) → `upstream.callTool(ctx,
args)` → `ensureSession` (session exists) → first `CallTool` → 404 →
`ErrSessionMissing` → `logUpstreamError("session_expired", 1)` →
`reinitSession` (compare-and-close the dead session under `u.mu`; build a fresh
transport + `Connect` → new session) → retry `CallTool` → success → return real
result UNCHANGED. (Failure branch: re-init fails OR retry fails →
`logUpstreamError` + return the error verbatim, result nil.)

**Pain Points Addressed**: (1) PRD §11.1 "re-run initialize once transparently and
retry. If it still fails, surface the upstream error" — satisfied by a single
retry loop keyed on the terminal sentinel, with honest error return. (2) PRD
§11.1 "surface the upstream error" / FR-5 honesty — satisfied by NEVER
synthesizing a `*mcp.CallToolResult` (the retry-failure path returns `nil`). (3)
PRD §11.3 / §8 result integrity — satisfied by returning `session.CallTool`'s
result unchanged. (4) The shared-session-vs-concurrent-expiry tension — resolved
by compare-and-close under `u.mu`.

## Why

- Implements **PRD §11.1** (re-init once transparently + retry; honest surfacing
  on failure), **§11.3** (preserve the original content in order; never set
  `isError`), **§8** (the result payload is a JSON-encoded STRING — keep it
  intact), and **FR-5** ("Re-initialize the upstream session on session-expiry
  signals").
- Implements **PRD §15** (`upstream_error` event: session-expiry + re-init
  attempts). The re-init attempt count is known ONLY inside S3's retry loop, so
  S3 owns the emission.
- Implements the honest-error half of **FR-6** discipline: results are never
  fabricated. (S3 returns errors verbatim; the WARNING-append is M5.T2.)
- **Completes `upstream.go`**: S1 (lazy shared session) + S2 (auth threading) +
  S3 (expiry resilience, honest errors, result integrity) = a fully functional
  `UpstreamClient` ready for M5.T2 to wire and M5.T3 to test end-to-end.
- **Decouples expiry-handling from auth and from the handler.** S3 adds a field
  (`log`) and three methods; it does NOT touch the transport, the authInjector,
  or any handler. M5.T2 consumes the now-complete `callTool`.

## What

`upstream.go` gains a `log *logger` field, a `connectLocked` refactor, a
`reinitSession` method, a rewritten retry-once `callTool`, and a
`logUpstreamError` helper. `upstream_test.go` gains an expiry-armed fake and
three tests. Visible behavior:

- A `callTool` whose first `CallTool` returns an `ErrSessionMissing`-wrapping
  error: S3 logs `upstream_error{status:"session_expired", reinit_attempts:1}`,
  closes the dead session, builds a fresh transport + session (compare-and-close
  under `u.mu` so concurrent callers share ONE new session), retries `CallTool`
  once, and returns its result unchanged on success.
- If the re-init `Connect` fails: S3 logs `upstream_error{status:"reinit_failed",
  reinit_attempts:1}` and returns `(nil, fmt.Errorf("upstream session expired;
  re-initialize failed: %w", rerr))` — NO synthesis.
- If the retry `CallTool` returns any non-nil error: S3 returns it verbatim —
  NO synthesis, NO second re-init.
- If the FIRST `CallTool` returns a NON-`ErrSessionMissing` error: S3 returns it
  verbatim WITHOUT re-init and WITHOUT logging `upstream_error` (that path is
  left for M5.T2's `delegate` event, which already carries upstream status).
- z.ai's result `Content` is returned UNCHANGED (no re-parse/reorder/truncate;
  `IsError` never set by S3).

### Success Criteria

- [ ] `UpstreamClient` has a 6th field `log *logger` (nil-safe: `logUpstreamError`
      is a no-op when `u.log == nil`, so S1/S2 tests that construct without it
      stay green).
- [ ] `connectLocked(ctx) error` exists, ASSUMES the caller holds `u.mu`, builds
      the transport (`newUpstreamHTTPClient()`) + `mcp.NewClient(...)` +
      `client.Connect(ctx, transport, nil)`, stores the result in `u.session`, and
      leaves `u.session` nil on error. `ensureSession` is refactored to call it
      (behavior-preserving: same lock, same nil-check, same leave-nil-on-error).
- [ ] `reinitSession(ctx, dead *mcp.ClientSession) (*mcp.ClientSession, error)`
      exists, holds `u.mu`, and does a COMPARE-AND-CLOSE: if `u.session != nil &&
      u.session != dead` → return `u.session` (another caller already re-init'd);
      else close the dead session (if any), set `u.session = nil`, call
      `connectLocked`, return the new session (or the error, leaving session nil).
- [ ] `callTool` performs: `ensureSession` → snapshot `sess` under `u.mu` → first
      `sess.CallTool`. On `err == nil` return `(res, nil)` (result UNCHANGED). On
      `!errors.Is(err, mcp.ErrSessionMissing)` return `(nil, err)` VERBATIM (no
      re-init). On `ErrSessionMissing`: `logUpstreamError("session_expired",1)` →
      `sess2, rerr := reinitSession(ctx, sess)` → on `rerr != nil`
      `logUpstreamError("reinit_failed",1)` + return `(nil, fmt.Errorf("upstream
      session expired; re-initialize failed: %w", rerr))` → else retry
      `sess2.CallTool` once; on `err2 != nil` return `(nil, err2)` VERBATIM; else
      return `(res2, nil)` UNCHANGED. Exactly ONE re-init + ONE retry per call.
- [ ] `logUpstreamError(calledTool, status string, attempts int)` exists, is a
      no-op when `u.log == nil`, else calls `u.log.log("warn", "upstream_error",
      map[string]any{"called_tool":..., "status":..., "reinit_attempts":...})`.
- [ ] `callTool` NEVER returns a non-nil `*mcp.CallToolResult` together with a
      non-nil error, and NEVER synthesizes a result on the failure paths (the
      three failure paths all return `(nil, err)`).
- [ ] Against an expiry-armed fake: first call succeeds; after `st.expire()`, a
      second `callTool` re-inits once (session pointer CHANGES), retries, and
      succeeds; the result is the fake's real result; `st.calls` advances by
      exactly 2 total (the failed first attempt of the 2nd call is 404'd at the
      HTTP layer before the tool handler runs, so it does not increment).
- [ ] When the retry's `CallTool` returns an error (armed via the fake's tool
      handler), `callTool` returns `(nil, err)`, `errors.Is(err, mcp.ErrSession-
      Missing)` is FALSE (it is the tool error), and the session pointer still
      changed (re-init happened exactly once). NO synthesis.
- [ ] When the FIRST `CallTool` returns a non-session error (armed BEFORE the
      first call, no expiry), `callTool` returns it verbatim, does NOT re-init
      (`st.calls == 1`), and the session pointer is the one from init.
- [ ] The `upstream_error` log line is emitted (and parseable) on the expiry +
      reinit-failed paths when `u.log` is non-nil; NOT emitted on the
      non-session-error path.
- [ ] `go test -race -run 'Upstream' ./...` is green (no data race on the
      compare-and-close path or the session snapshot).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat go.mod` is empty; `git diff --stat` shows ONLY
      `upstream.go` + `upstream_test.go`; S1/S2 tests still pass unchanged.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone
because: (a) the central SDK facts are given as EXACT, file:line-cited statements
(§1 `ErrSessionMissing` sentinel; §2 the 404→sentinel mapping is terminal while
502/503/504/429 are transient; §2.2 after an `ErrSessionMissing` the connection
is DEAD so a fresh transport+Connect is required); (b) the FULL detect→re-init→
retry cycle was PROVEN by a throwaway PoC whose PASS output is quoted (research
§3) and whose CRITICAL test-harness gotcha (expire a SPECIFIC session-id, not
"any", because `notifications/initialized` carries the NEW id) is explained
(research §3.2); (c) the exact Go for `connectLocked`, the refactored
`ensureSession`, `reinitSession` (compare-and-close), the rewritten `callTool`,
and `logUpstreamError` is given as a reference implementation; (d) the concurrency
correctness trace for `reinitSession` (two/three concurrent callers A/B/C) is
written out (research §4.2); (e) the test-harness expiry wrapper is given as
exact Go (research §3.1/§5) with the exact assertions and the
`st.calls`-does-not-count-the-404 reasoning; (f) the `log *logger` field decision
and its nil-safety (so S1/S2 tests stay green) + its non-conflict with S2's
`TestUpstreamClient_AuthNotRetained` (research §6) are explained; (g) result
preservation (return `session.CallTool`'s result UNCHANGED; never synthesize;
never set `IsError`) is explicit.

### Documentation & References

```yaml
# MUST READ — the rules this item implements.
- file: PRD.md
  section: "§11.1 Session lifecycle" + "§11.3 Result handling" + "§8 Verified transport contract"
  why: §11.1 = "On a session-expiry signal from z.ai (404 / invalid-session), re-run
        initialize once transparently and retry the call. If it still fails, surface
        the upstream error." §11.3 = "Preserve the original content in order. Never
        set isError." §8 = "The result payload is a JSON-encoded STRING (a stringified
        array), not an object. We keep it intact when returning it to the client."
  critical: re-init EXACTLY ONCE per inbound call; on persistent failure surface the
        REAL error (NEVER synthesize a result); return the result Content UNCHANGED.

- file: PRD.md
  section: "§15 Logging" + "§19.3 case 6"
  why: §15 = the upstream_error event (session-expiry / re-init attempts, fields
        called_tool + status + reinit attempts). §19.3 case 6 = "Upstream
        session-expiry → one transparent re-init, then success."
  critical: the re-init attempt count is known only inside S3's retry loop, so S3
        owns the upstream_error emission.

# MUST READ — THIS ITEM'S RESEARCH (source-verified SDK facts + proven PoC + design).
- docfile: plan/002_0a8ab3410994/P1M4T1S3/research/session-expiry-reinit.md
  why: §1 the ErrSessionMissing sentinel; §2 the 404→sentinel mapping (terminal) vs
        transient 5xx/429; §2.2 after ErrSessionMissing the connection is dead; §3
        the PROVEN PoC + its PASS output; §3.1-§3.2 the expiry-wrapper harness +
        the CRITICAL gotcha (expire a SPECIFIC id, not "any"); §4 the concurrency-
        safe compare-and-close design (reinitSession) + the connectLocked refactor +
        the exact callTool retry-once structure; §5 the test-harness (Option A
        preferred: extend fakeState + wrapper); §6 the log *logger field decision;
        §7 result preservation; §8 scope boundaries; §9 verified file:line table.
  section: ALL sections load-bearing; §3 (PoC), §3.2 (harness gotcha), §4 (design +
        exact Go), §5 (harness), §6 (logger) are implementation-critical.

- docfile: plan/002_0a8ab3410994/P1M4T1S3/research/external-mcp-session-research.md
  why: the spec-level background ("MAY terminate → MUST answer 404"; no JSON-RPC
        code for invalid session; transient vs terminal) + a pitfalls checklist.
  section: §1-§4 + "Pitfalls checklist".

# MUST READ — the SDK API surface (verified from v1.6.1 source).
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§2 CLIENT SIDE" + "§3 CONTENT TYPES"
  why: §2 = Connect returns *ClientSession; CallTool(ctx, *CallToolParams)
        (*CallToolResult, error); StreamableHTTPHandler.sessions is unexported so an
        external test must WRAP the handler (not reach into the map). §3 = read the
        result via res.Content[0].(*mcp.TextContent).Text.
  critical: the ONLY SDK symbols S3 adds are errors.Is(err, mcp.ErrSessionMissing);
        Connect/CallTool are reused exactly as S1 uses them.

# MUST READ — the files S3 edits (S1+S2 output). Read them FIRST.
- file: upstream.go
  section: "UpstreamClient struct" + "ensureSession" + "callTool" + doc comments
  why: S3 ADDS a 6th field (log *logger); EXTRACTS connectLocked from ensureSession;
        ADDS reinitSession + logUpstreamError; REWRITES callTool. authInjector /
        authFromContext / newUpstreamHTTPClient are UNCHANGED (S3 reuses them).
  pattern: S1's ensureSession holds u.mu over Connect (serialize the once-only
        shared init). S3 mirrors that for reinitSession (hold u.mu over the
        re-init Connect — session-expiry is rare, so the serialization cost is
        negligible).

- file: upstream_test.go
  section: "fakeState" + "newFakeZAI" + the 7 S1/S2 tests
  why: S3 EXTENDS fakeState (liveID/expiredID/toolErr) + the recording wrapper
        (add expiry + tool-error) and ADDS 3 tests. The S1/S2 tests construct
        UpstreamClient WITHOUT a logger (log defaults nil) and must stay green.

# MUST READ — the logger API (S3 calls u.log.log).
- file: logger.go
  section: "type logger struct" + "func (l *logger) log(level, msg, fields)"
  why: log signature is log(level string, msg string, fields map[string]any). S3
        calls u.log.log("warn", "upstream_error", map[string]any{...}). A
        *bytes.Buffer-backed logger captures the line for assertion.

# Go stdlib (errors.Is sentinel detection).
- url: https://pkg.go.dev/errors#Is
  why: errors.Is(err, mcp.ErrSessionMissing) traverses the %w wrap chain (the SDK
        wraps the 404 as fmt.Errorf("...: %w", ErrSessionMissing), so Is is true).
  critical: use errors.Is, NEVER == or strings.Contains, to detect the sentinel.

# MCP Streamable HTTP spec (the "MAY terminate → MUST answer 404" rule).
- url: https://modelcontextprotocol.io/specification/2025-06-18/basic/transports
  why: §2.5.3 session lifecycle (quoted verbatim in go-sdk streamable.go:2054-2056).
        Background only; the SDK already implements both halves of this rule.
  critical: a 404 (NOT a JSON-RPC error code) is the canonical session-expiry
        signal; the SDK maps it to ErrSessionMissing. (live anchor unverified —
        see research Gaps; semantics are unchanged across spec versions.)
```

### Current Codebase tree (the INPUT state — run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  main.go           # authHeaderKey{} + authMiddleware (P1.M1.T2.S2) — UNTOUCHED by S3
  upstream.go       # S1+S2: UpstreamClient(5 fields) + authInjector + authFromContext
                    #         + newUpstreamHTTPClient(wraps authInjector) + ensureSession
                    #         + callTool(single attempt) — S3 EDITS (adds log field,
                    #         connectLocked, reinitSession, logUpstreamError; rewrites callTool)
  upstream_test.go  # S1+S2: newFakeZAI(auth-recording) + 7 tests — S3 EDITS (expiry
                    #         wrapper + tool-error arm + 3 new tests)
  logger.go         # *logger + log(level,msg,fields) + redactHeaders — UNTOUCHED (consumed)
  config.go, health.go, extract.go, teach.go — UNTOUCHED
  *_test.go (config/resolve/logger/health/extract/teach) — UNTOUCHED
  testdata/*.sse, README.md, config.example.json, PRD.md, doc.go — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
upstream.go        # MODIFY (no new file). Adds:
                   #   field:        log *logger              (nil-safe; 6th field)
                   #   func (u *UpstreamClient) connectLocked(ctx context.Context) error
                   #   func (u *UpstreamClient) reinitSession(ctx, dead *mcp.ClientSession)
                   #                                           (*mcp.ClientSession, error)
                   #   func (u *UpstreamClient) logUpstreamError(calledTool, status string,
                   #                                           attempts int)
                   # Refactors ensureSession to call connectLocked. Rewrites callTool into a
                   #   retry-once-on-ErrSessionMissing loop with honest error surfacing.
                   # Updates Mode-A doc comments. New imports: "errors", "fmt" (stdlib).
                   # authInjector / authFromContext / newUpstreamHTTPClient UNCHANGED.
upstream_test.go   # MODIFY (no new file). fakeState gains liveID/expiredID/toolErr +
                   #   expire()/setToolErr() helpers; the newFakeZAI recording wrapper
                   #   gains the session-expiry (404 a SPECIFIC Mcp-Session-Id) + tool-error
                   #   arms; ADD TestUpstreamClient_SessionExpiryReinitSuccess,
                   #   TestUpstreamClient_ReinitRetryFailsHonestError,
                   #   TestUpstreamClient_NonSessionErrorNoReinit. +import "bytes","errors".
# NO other files created or modified. go.mod/go.sum unchanged.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL — a 404 is TERMINAL; the old session is DEAD. The SDK's checkResponse
// (streamable.go:2057-2060) wraps a 404 as fmt.Errorf("...: %w", ErrSessionMissing),
// then c.fail(err) stores it via failOnce and closes the 'failed' channel, so EVERY
// subsequent Read/Write on that connection returns it (streamable.go:1730-1770). The
// old *mcp.ClientSession is therefore UNUSABLE — re-init MUST build a FRESH
// transport + Client.Connect, NOT re-send initialize on the dead session (its
// sessionID is stale; setMCPHeaders would re-attach it and the server 404s again).

// CRITICAL — detect with errors.Is, never ==. CallTool's returned error wraps the
// sentinel: errors.Is(err, mcp.ErrSessionMissing). A == check or strings.Contains
// is brittle (the message includes a dynamic session id). (research §1, §2.3)

// CRITICAL — do NOT react to transient 5xx/429. checkResponse maps 502/503/504/429
// to jsonrpc2.ErrRejected (streamable.go:2064) which the transport RETRIES without
// breaking the connection. Only the TERMINAL ErrSessionMissing needs re-init. A
// non-ErrSessionMissing error (including a JSON-RPC error from a failing tool
// handler) MUST be surfaced verbatim WITHOUT re-init. (research §2.2, §4.4)

// CRITICAL — expire a SPECIFIC session-id in the test harness, NOT "any". Client.
// Connect is NOT a single POST: after initialize (which carries NO Mcp-Session-Id),
// the server assigns a NEW id, and Connect sends notifications/initialized as a
// SECOND POST that CARRIES the new id (client.go:256-292). A blanket "404 any
// session-id" toggle would 404 that notification and kill the re-init. The harness
// 404s ONLY the expired id; the re-init's initialize (no id) and the new id pass.
// (research §3.2 — the PoC's first attempt FAILED on exactly this; the fix is to
// expire a SPECIFIC id.)

// CRITICAL — the test harness's expired first attempt does NOT invoke the tool
// handler. The wrapper 404s the request at the HTTP layer BEFORE the SDK server
// dispatches the tool handler. So the failed first attempt of the 2nd callTool does
// NOT increment the handler's call counter. After the success test, st.calls ==
// (first call) + (retry) == 2. (research §3)

// CRITICAL — exactly ONE re-init + ONE retry per inbound call. Never loop on a
// 404. If the retry still fails, return its error VERBATIM; do NOT synthesize a
// *mcp.CallToolResult. The three failure paths all return (nil, err). (PRD §11.1)

// CRITICAL — the re-init runs inside the SAME callTool whose ctx carries
// authHeaderKey (S2). The rebuilt transport's authInjector reads the current
// request's auth from context. S3 needs ZERO auth-specific code. Do NOT thread
// auth into reinitSession/connectLocked explicitly. (S2 PRP consumer seam)

// CRITICAL — compare-and-close under u.mu for concurrency. The session is SHARED;
// N concurrent in-flight calls can EACH receive ErrSessionMissing. reinitSession
// must (a) return the existing fresh session if another caller already re-init'd
// (u.session != nil && u.session != dead), else (b) close the dead session, nil
// it, connectLocked ONCE, return the new session. The lock is held across the
// network Connect (mirrors S1's ensureSession; session-expiry is rare so the
// serialization cost is negligible). (research §4.2)

// CRITICAL — the new log field MUST be nil-safe. S1/S2 tests construct
// &UpstreamClient{upstream, targetTool, targetParam} WITHOUT a logger; they must
// stay green unchanged. logUpstreamError returns immediately when u.log == nil.
// (research §6)

// GOTCHA — log is *logger, NOT a string and NOT credential-named, so S2's
// TestUpstreamClient_AuthNotRetained (reflect name-walk over {auth,authheader,
// authorization,key,apikey,credential,token} + string-equality on the string
// fields) still passes. Do NOT name the field anything in that denied set.

// GOTCHA — result preservation. callTool returns session.CallTool's
// *mcp.CallToolResult UNCHANGED on BOTH the happy path and the retry-success path.
// Never re-parse/reorder/truncate Content; never set IsError. The warning append is
// M5.T2's job (teach.appendWarning appends to the SAME result S3 returned). (PRD §8, §11.3)

// GOTCHA — do NOT double-log. A NON-ErrSessionMissing error is surfaced verbatim
// and is left for M5.T2's delegate event (which already carries upstream status).
// S3 emits upstream_error ONLY on: expiry detected (status="session_expired",
// attempts=1) and re-init Connect failure (status="reinit_failed", attempts=1).
// (research §6)

// GOTCHA — connectLocked must leave u.session nil on failure (so a later call
// retries via ensureSession), exactly as S1's ensureSession did. A failed Connect
// closes its partial connection and returns a non-nil error.
```

## Implementation Blueprint

### Data models and structure

No new data models. `UpstreamClient` gains ONE field (`log *logger`, nil-safe).
Two new private methods (`connectLocked`, `reinitSession`) + one helper
(`logUpstreamError`). `callTool` is rewritten. The result type
(`*mcp.CallToolResult`) is UNCHANGED and returned as-is. Deps: stdlib `errors`,
`fmt` (newly imported into upstream.go), plus the already-imported `context`,
`net/http`, `sync`, `time`, and `github.com/modelcontextprotocol/go-sdk/mcp`.

### Reference implementation (the `upstream.go` edits)

**(A) Add the `log` field + import `errors`,`fmt`.** Add `log *logger` as the
6th field. Update the import block (gofmt-sorted) to include `"errors"` and
`"fmt"`:

```go
type UpstreamClient struct {
	mu          sync.Mutex
	session     *mcp.ClientSession
	upstream    string
	targetTool  string
	targetParam string
	log         *logger // nil-safe: if nil, logUpstreamError is a no-op (PRD §15)
}
```

**(B) Extract `connectLocked` + refactor `ensureSession` (behavior-preserving).**
`connectLocked` ASSUMES the caller holds `u.mu`; it leaves `u.session` nil on
failure. `ensureSession` becomes a thin wrapper. The body of `connectLocked` is
S1's exact ensureSession body (transport + client + Connect):

```go
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
// leaves session nil and returns the error (the next callTool retries). The actual
// Connect is in connectLocked (shared with reinitSession).
func (u *UpstreamClient) ensureSession(ctx context.Context) error {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.session != nil {
		return nil
	}
	return u.connectLocked(ctx)
}
```

**(C) Add `reinitSession` (compare-and-close under `u.mu`).**

```go
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
```

**(D) Rewrite `callTool` (retry-once on `ErrSessionMissing`; honest error surfacing).**

```go
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
// VERBATIM. It NEVER synthesizes a *mcp.CallToolResult: the three failure paths
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
```

**(E) Add `logUpstreamError` (nil-safe; PRD §15).**

```go
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
```

> NOTE on imports: upstream.go's final gofmt-sorted import block is `"context"`,
> `"errors"`, `"fmt"`, `"net/http"`, `"sync"`, `"time"`, then
> `"github.com/modelcontextprotocol/go-sdk/mcp"`. (`"net/http"`/`"time"` remain in
> use by `newUpstreamHTTPClient`/`authInjector`.) Do NOT remove any existing import.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify S1+S2 + the SDK sentinel exist
  - RUN: test -f upstream.go && test -f upstream_test.go \
        && grep -q "func (u \*UpstreamClient) callTool" upstream.go \
        && grep -q "func (u \*UpstreamClient) ensureSession" upstream.go \
        && grep -q "authInjector" upstream.go \
        && grep -q "authHeaderKey struct{}" main.go \
        && grep -q "ErrSessionMissing" "$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/transport.go" \
        && grep -q "modelcontextprotocol/go-sdk v1.6.1" go.mod
  - EXPECT: exit 0. IF ANY FAIL: STOP — a prerequisite (S1/S2 or P1.M1.T2.S2) has
        not run, or the SDK version drifted. S3 EDITS upstream.go; it does not
        create it.

Task 1: ADD the log field + imports to upstream.go
  - FILE: upstream.go (MODIFY). Add `log *logger` as the 6th field of UpstreamClient
        (nil-safe — see the doc on logUpstreamError). Add "errors" and "fmt" to the
        import block (gofmt-sorted).
  - CONSTRAINTS: the field is named `log` (NOT auth/key/token/etc. — S2's
        TestUpstreamClient_AuthNotRetained reflect name-walk must stay green).

Task 2: EXTRACT connectLocked + REFACTOR ensureSession (behavior-preserving)
  - FILE: upstream.go (MODIFY). Move S1's ensureSession body (transport + client +
        Connect) into connectLocked(ctx) error that ASSUMES the caller holds u.mu and
        leaves u.session nil on failure. Make ensureSession the thin wrapper from
        "Reference implementation (B)".
  - VERIFY behavior-preservation: same lock, same nil-check, same Connect, same
        leave-nil-on-error. S1's tests (LazyInit/LazyReuse/Concurrent/EnsureSessionError)
        must still pass byte-for-byte unchanged.

Task 3: ADD reinitSession (compare-and-close under u.mu)
  - FILE: upstream.go (MODIFY). Paste reinitSession from "Reference implementation
        (C)". early-return the fresh session when u.session != nil && u.session !=
        dead; else close+nil+connectLocked.
  - CONSTRAINTS: hold u.mu for the whole method (mirrors ensureSession). Close() is
        cheap (skips DELETE on ErrSessionMissing). Return the new session (or the
        error, leaving u.session nil).

Task 4: REWRITE callTool (retry-once on ErrSessionMissing; honest errors)
  - FILE: upstream.go (MODIFY). Replace S1's callTool body with "Reference
        implementation (D)". Order: ensureSession → snapshot sess under u.mu → first
        CallTool → (err==nil → return res UNCHANGED) → (!errors.Is(ErrSessionMissing)
        → return (nil, err) verbatim) → (ErrSessionMissing → log session_expired →
        reinitSession → on rerr log reinit_failed + return wrapped error → else retry
        CallTool once → on err2 return (nil, err2) → else return res2 UNCHANGED).
  - CONSTRAINTS: use errors.Is(err, mcp.ErrSessionMissing) — NEVER == or strings.
        Exactly ONE re-init + ONE retry. The three failure paths return (nil, err).
        Never synthesize a *mcp.CallToolResult; never set IsError.

Task 5: ADD logUpstreamError (nil-safe; PRD §15)
  - FILE: upstream.go (MODIFY). Paste logUpstreamError from "Reference
        implementation (E)". No-op when u.log == nil; else u.log.log("warn",
        "upstream_error", {called_tool, status, reinit_attempts}).
  - CONSTRAINTS: level "warn" (matches logger.go levelNum). Called ONLY with
        status="session_expired"/"reinit_failed", attempts=1.

Task 6: EXTEND fakeState + newFakeZAI with the expiry wrapper + tool-error arm
  - FILE: upstream_test.go (MODIFY). Add to fakeState: liveID string, expiredID
        string, toolErr error. Add helpers expire() (sets expiredID = liveID) and
        setToolErr(e error). Extend the recording http.Handler so it: (a) records
        Authorization (keep S2); (b) on each request remembers the FIRST non-empty
        Mcp-Session-Id as liveID; (c) when expiredID != "" and the request's
        Mcp-Session-Id == expiredID, responds 404 "session not found" WITHOUT
        forwarding (this is BEFORE the tool handler runs, so it does not increment
        st.calls); else forwards to the SDK handler. Extend the tool handler so when
        st.toolErr != nil it returns (nil, st.toolErr) WITHOUT incrementing st.calls.
        (See "Test harness" below.) This is backward-compatible: S1/S2 tests never
        call expire()/setToolErr(), so expiredID/toolErr stay zero-value and the
        wrapper/handler behave exactly as before.

Task 7: ADD the three new tests (upstream_test.go)
  - TestUpstreamClient_SessionExpiryReinitSuccess: the PRD §19.3 case 6 flow. Build
        u against the expiring fake. First callTool → succeeds; snapshot oldSession =
        u.session. st.expire(). Second callTool → succeeds (re-init + retry
        transparent). Assert: u.session != oldSession (a NEW session); result is the
        fake's real result (not synthesized); no error; atomic st.calls == 2 (first
        call + retry; the failed first attempt of the 2nd call was 404'd before the
        tool handler).
  - TestUpstreamClient_ReinitRetryFailsHonestError: arm a sentinel tool error AFTER
        the first call + expiry, so the re-init SUCCEEDS but the retry CallTool
        returns the (non-ErrSessionMissing) tool error. Assert: callTool returns
        (nil, err); errors.Is(err, sentinel) is TRUE (it is the tool error, wrapped
        by the server as "handling \"tools/call\": %w"); errors.Is(err,
        mcp.ErrSessionMissing) is FALSE; u.session != oldSession (re-init happened
        exactly once); NO synthesis (result is nil). Optionally assert the buffer-
        backed logger saw the "session_expired" upstream_error line.
  - TestUpstreamClient_NonSessionErrorNoReinit: arm the tool error BEFORE the first
        call, and arm NO expiry. Assert: callTool returns the tool error verbatim;
        errors.Is(err, errFakeUpstreamTool) is TRUE; errors.Is(err,
        mcp.ErrSessionMissing) is FALSE; and NO upstream_error line appears (its
        absence PROVES the ErrSessionMissing branch was never entered, hence no
        re-init). Note: do NOT use st.calls as the proof here — the tool-error
        invocation is not counted, so it cannot distinguish the cases.

Task 8: VALIDATE
  - gofmt -w upstream.go upstream_test.go
  - go vet ./...
  - go build ./...
  - go test -run 'Upstream' -v                  # S1+S2 (unchanged) + S3 new tests
  - go test -race -run 'Upstream' -v            # race gate (compare-and-close path)
  - go test ./...                               # full suite green (no regressions)
  - ALL green. git diff --stat must show ONLY upstream.go + upstream_test.go.
  - git diff --stat go.mod must be EMPTY.
  - go doc . UpstreamClient.callTool      # Mode-A: re-init-once, honest-error, result-preservation
  - go doc . UpstreamClient.reinitSession # Mode-A: compare-and-close concurrency trace
  - grep -n 'errors.Is(err, mcp.ErrSessionMissing)' upstream.go   # exactly ONE (in callTool)
```

### Test harness + tests (the `upstream_test.go` edits)

```go
// --- EXTEND fakeState (add liveID, expiredID, toolErr) ---
type fakeState struct {
	mu        sync.Mutex
	calls     int32 // atomic count of tools/call handled
	lastTool  string
	lastArgs  map[string]any
	lastAuth  string // S2: Authorization the fake z.ai received
	liveID    string // S3: first Mcp-Session-Id observed (the live session)
	expiredID string // S3: when non-empty, 404 requests carrying THIS id only
	toolErr   error  // S3: when non-nil, the tool handler returns it (no call count)
}

// expire marks the currently-live z.ai session expired: subsequent requests
// carrying THAT Mcp-Session-Id get a 404 "session not found" (the exact signal the
// SDK maps to mcp.ErrSessionMissing). Models real z.ai evicting ONE session.
func (st *fakeState) expire() {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.expiredID = st.liveID
}

// setToolErr arms a non-session error returned by the tool handler (used to drive
// the honest-error / no-reinit tests). When non-nil, the handler returns it WITHOUT
// incrementing st.calls.
func (st *fakeState) setToolErr(e error) {
	st.mu.Lock()
	defer st.mu.Unlock()
	st.toolErr = e
}

// --- EDIT newFakeZAI: wrap the SDK handler with recording + expiry ---
func newFakeZAI(t *testing.T) (*httptest.Server, *fakeState) {
	t.Helper()
	st := &fakeState{}
	zai := mcp.NewServer(&mcp.Implementation{Name: "fake-zai", Version: "v1"}, nil)
	zai.AddTool(&mcp.Tool{
		Name:        "web_search_prime",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"search_query":{"type":"string"}},"additionalProperties":true}`),
	}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		// S3: when armed, the tool handler returns a non-session error (honest-error
		// / no-reinit tests). Do NOT count it (it models a failing upstream tool).
		st.mu.Lock()
		toolErr := st.toolErr
		st.mu.Unlock()
		if toolErr != nil {
			return nil, toolErr
		}
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
	// S2 records Authorization; S3 ALSO simulates session-expiry by 404'ing the
	// expired Mcp-Session-Id. The first observed non-empty session-id is the live
	// one; once expiredID is set, requests carrying THAT id get a 404 (the exact
	// server-side signal the SDK maps to ErrSessionMissing). The re-init's
	// initialize (no id) and the new session's id pass through untouched.
	recording := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sid := r.Header.Get("Mcp-Session-Id")
		st.mu.Lock()
		st.lastAuth = r.Header.Get("Authorization")
		if st.liveID == "" && sid != "" {
			st.liveID = sid // first session-id observed
		}
		expired := st.expiredID != "" && sid == st.expiredID
		st.mu.Unlock()
		if expired {
			// The MCP "MAY terminate → MUST answer 404" rule (spec §2.5.3). This
			// fires BEFORE the SDK handler dispatches the tool, so it does NOT
			// increment st.calls.
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		h.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(recording)
	t.Cleanup(srv.Close)
	return srv, st
}

// errFakeUpstreamTool is the sentinel the honest-error / no-reinit tests arm via
// st.setToolErr. The SDK server wraps a handler error as fmt.Errorf("handling
// \"tools/call\": %w", err) (shared.go:164), so errors.Is(clientErr, errFakeUpstreamTool)
// is TRUE on the client side.
var errFakeUpstreamTool = errors.New("fake z.ai tool failure")

// --- ADD TestUpstreamClient_SessionExpiryReinitSuccess (PRD §11.1, §19.3 case 6) ---
func TestUpstreamClient_SessionExpiryReinitSuccess(t *testing.T) {
	srv, st := newFakeZAI(t)
	u := &UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}

	ctx, cancel := testCtx(t)
	defer cancel()

	// First call: lazily creates the live session and succeeds.
	res, err := u.callTool(ctx, map[string]any{"search_query": "first"})
	if err != nil {
		t.Fatalf("first callTool: %v", err)
	}
	if res == nil || res.IsError {
		t.Fatal("first callTool: expected a real, non-error result")
	}
	oldSession := u.session
	if oldSession == nil {
		t.Fatal("session should be set after the first call")
	}

	// Expire the live session (z.ai evicted it). The next call's first attempt 404s.
	st.expire()

	// Second call: S3 detects ErrSessionMissing, re-inits ONCE, retries, succeeds.
	res2, err := u.callTool(ctx, map[string]any{"search_query": "second"})
	if err != nil {
		t.Fatalf("second callTool (after expiry): %v (expected transparent re-init + retry)", err)
	}
	if res2 == nil || res2.IsError {
		t.Fatal("second callTool: expected a real, non-error result after re-init")
	}
	if u.session == oldSession {
		t.Error("expected a NEW session after re-init; session pointer unchanged")
	}
	// The failed first attempt of the 2nd call was 404'd before the tool handler,
	// so st.calls == (first call) + (retry) == 2.
	if got := atomic.LoadInt32(&st.calls); got != 2 {
		t.Errorf("fake z.ai handled %d tool calls, want 2 (first + retry; the 404'd attempt is not a tool call)", got)
	}
}

// --- ADD TestUpstreamClient_ReinitRetryFailsHonestError (PRD §11.1 honest-error) ---
func TestUpstreamClient_ReinitRetryFailsHonestError(t *testing.T) {
	srv, st := newFakeZAI(t)
	var buf bytes.Buffer
	u := &UpstreamClient{
		upstream:    srv.URL,
		targetTool:  "web_search_prime",
		targetParam: "search_query",
		log:         newLogger(&buf, "debug"),
	}

	ctx, cancel := testCtx(t)
	defer cancel()

	// First call establishes the live session and succeeds.
	if _, err := u.callTool(ctx, map[string]any{"search_query": "first"}); err != nil {
		t.Fatalf("first callTool: %v", err)
	}
	oldSession := u.session

	// Expire, THEN arm a non-session tool error. The re-init's initialize +
	// notifications/initialized do NOT invoke the tool handler, so the re-init
	// succeeds; only the retry's CallTool hits the handler and returns the error.
	st.expire()
	st.setToolErr(errFakeUpstreamTool)

	// Second call: 404 → ErrSessionMissing → re-init (succeeds) → retry (tool
	// error). S3 MUST surface the retry's error honestly and return a nil result.
	res, err := u.callTool(ctx, map[string]any{"search_query": "second"})
	if err == nil {
		t.Fatal("expected an honest error after the retry failed, got nil")
	}
	if res != nil {
		t.Errorf("expected a nil result (no synthesis); got non-nil %v", res)
	}
	if !errors.Is(err, errFakeUpstreamTool) {
		t.Errorf("expected the surfaced error to wrap errFakeUpstreamTool; got %v", err)
	}
	if errors.Is(err, mcp.ErrSessionMissing) {
		t.Error("the surfaced error should be the TOOL error, not a session-missing error")
	}
	if u.session == oldSession {
		t.Error("expected the re-init to have created a new session; pointer unchanged")
	}
	// S3 logged the session_expired event on detecting the expiry.
	if !strings.Contains(buf.String(), `"msg":"upstream_error"`) ||
		!strings.Contains(buf.String(), `"status":"session_expired"`) {
		t.Errorf("expected a session_expired upstream_error log line; got:\n%s", buf.String())
	}
}

// --- ADD TestUpstreamClient_NonSessionErrorNoReinit (only ErrSessionMissing re-inits) ---
func TestUpstreamClient_NonSessionErrorNoReinit(t *testing.T) {
	srv, st := newFakeZAI(t)
	var buf bytes.Buffer
	u := &UpstreamClient{
		upstream:    srv.URL,
		targetTool:  "web_search_prime",
		targetParam: "search_query",
		log:         newLogger(&buf, "debug"),
	}

	ctx, cancel := testCtx(t)
	defer cancel()

	// Arm a tool error BEFORE the first call, and arm NO expiry. The first (and
	// only) CallTool POST carries the live session-id, which is NOT in expiredID
	// (expiredID==""), so the wrapper forwards it -> the handler runs and returns
	// the error. S3 must surface it verbatim and NOT re-init.
	st.setToolErr(errFakeUpstreamTool)

	_, err := u.callTool(ctx, map[string]any{"search_query": "x"})
	if err == nil {
		t.Fatal("expected the tool error to be surfaced, got nil")
	}
	if !errors.Is(err, errFakeUpstreamTool) {
		t.Errorf("expected the surfaced error to wrap errFakeUpstreamTool; got %v", err)
	}
	if errors.Is(err, mcp.ErrSessionMissing) {
		t.Error("a non-session error should not look like ErrSessionMissing")
	}
	// PROOF that no re-init happened: the ONLY place S3 emits upstream_error is
	// after detecting ErrSessionMissing. No expiry was armed, so no 404 was possible,
	// so the ErrSessionMissing branch was never entered, so the `!errors.Is(...)`
	// branch returned verbatim. Assert the log is empty of that event.
	if strings.Contains(buf.String(), `"msg":"upstream_error"`) {
		t.Errorf("non-session errors must NOT emit upstream_error (proves no re-init); got:\n%s", buf.String())
	}
}
```

> IMPORTS for upstream_test.go gain `"bytes"` and `"errors"` (both stdlib). `strings`,
> `context`, `sync`, `sync/atomic`, `testing`, `time`, `net/http`, `net/http/httptest`,
> `encoding/json`, `reflect`, and `mcp` are already imported (S1/S2).
>
> NOTE on the three tests: the distinguishing assertion for "no re-init" in
> `TestUpstreamClient_NonSessionErrorNoReinit` is the ABSENCE of the `upstream_error`
> log line — S3 emits that event ONLY on the `ErrSessionMissing` path, and no expiry was
> armed, so its absence proves the verbatim-return branch was taken. (Counting `st.calls`
> does NOT distinguish the cases, because the tool-error invocation is not counted, so it
> is not used as the proof there.)

### Implementation Patterns & Key Details

```go
// PATTERN: sentinel-based terminal-error detection. errors.Is(err, mcp.ErrSessionMissing)
// is the SDK's ONE canonical "session terminated" signal. A 404 is TERMINAL (the
// connection is dead); transient 5xx/429 are a separate, transport-retried path. S3
// reacts ONLY to the terminal sentinel. (research §1, §2)

// PATTERN: compare-and-close for shared-resource re-init. reinitSession uses the dead
// session POINTER as a CAS token under u.mu: only the caller that still sees u.session
// == dead performs the re-init; later callers reuse the fresh session. This is the
// standard Go pattern for "N goroutines observe a shared-resource failure; only one
// rebuilds it". (research §4.2)

// PATTERN: behavior-preserving refactor (connectLocked). S1's ensureSession body moves
// verbatim into connectLocked; ensureSession becomes a thin wrapper. The lock semantics
// are identical, so S1's tests pass unchanged. (research §4.3)

// PATTERN: honest error surfacing / no synthesis. Every failure path returns (nil, err).
// PRD §11.1 "if it still fails, surface the upstream error" is enforced structurally:
// there is NO code path that constructs a *mcp.CallToolResult on error. A non-nil error
// ALWAYS comes with a nil result. (research §4.4, §7)

// PATTERN: nil-safe optional dependency. log *logger defaults to nil; logUpstreamError
// is a no-op then. This keeps S1/S2 tests (which never set log) green while letting S3's
// tests and M5.T2 pass a real logger. (research §6)

// GOTCHA (restated): expire a SPECIFIC session-id in the harness, never "any". The
// re-init's notifications/initialized POST carries the NEW id; a blanket 404 kills it.
// (research §3.2 — proven by the PoC's first FAILED attempt.)

// GOTCHA (restated): the 404'd first attempt of the 2nd callTool does NOT reach the tool
// handler (the wrapper 404s at the HTTP layer), so it does NOT increment st.calls. After
// the success test st.calls == 2. (research §3)

// GOTCHA (restated): S3 needs ZERO auth code. The re-init's rebuilt transport reuses
// newUpstreamHTTPClient() (S1) which wraps authInjector (S2); the current callTool's ctx
// carries authHeaderKey; the SDK propagates it to the outbound POST. (S2 PRP seam)

// GOTCHA (restated): a tool handler returning error is NOT ErrSessionMissing. The server
// wraps it as fmt.Errorf("handling \"tools/call\": %w", err) (shared.go:164); the client's
// CallTool returns that Go error; errors.Is(err, ErrSessionMissing) is FALSE → S3 surfaces
// it verbatim WITHOUT re-init. This is the basis of the honest-error / no-reinit tests.
```

### Integration Points

```yaml
FILES MODIFIED:
  - upstream.go      (MODIFY): +log *logger field; +connectLocked; +reinitSession;
        +logUpstreamError; ensureSession refactored; callTool rewritten (retry-once);
        Mode-A doc comments; +imports "errors","fmt". authInjector/authFromContext/
        newUpstreamHTTPClient UNCHANGED.
  - upstream_test.go (MODIFY): fakeState +liveID/expiredID/toolErr +expire()/setToolErr();
        newFakeZAI recording wrapper gains expiry(404)+tool-error arms; +3 tests;
        +imports "bytes","errors".
NO OTHER FILES TOUCHED:
  - main.go: UNCHANGED (authHeaderKey + authMiddleware already exist; UpstreamClient is
        not yet constructed there — M5.T2's job).
  - go.mod/go.sum: UNCHANGED (errors/fmt/bytes are stdlib; the SDK require already exists).
  - All other *_test.go, config.go, logger.go, health.go, extract.go, teach.go, doc.go:
        UNCHANGED.
CONSUMER SEAMS:
  - P1.M5.T2 (server dispatch): constructs &UpstreamClient{upstream: cfg.Upstream,
        targetTool: cfg.TargetTool, targetParam: cfg.TargetParam, log: log} — the ONLY
        new constructor concern is passing `log`. Calls res, err := upstream.callTool(ctx,
        args); on err != nil surfaces it (never fabricates); on success appends the
        teaching warning via teach.appendWarning(res, ...) to the SAME *mcp.CallToolResult
        S3 returned unchanged. M5.T2 adds ZERO expiry/auth code.
  - P1.M5.T3 (server_test.go e2e): reuses the S3 expiry wrapper for §19.3 case 6
        ("Upstream session-expiry → one transparent re-init, then success").
DATABASE / ROUTES / ENV: none. S3 is upstream-client logic only; no handler wiring, no
        config change, no new env vars.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w upstream.go upstream_test.go
go vet ./...
go build ./...                  # must compile; "errors"/"fmt"/"bytes" are stdlib

# Exactly ONE ErrSessionMissing check, in callTool (reinitSession must NOT re-check):
grep -n 'errors.Is(err, mcp.ErrSessionMissing)' upstream.go   # expect exactly 1 match

# UpstreamClient now has 6 fields (added log); connectLocked/reinitSession/logUpstreamError exist:
grep -A7 'type UpstreamClient struct' upstream.go             # mu/session/upstream/targetTool/targetParam/log
grep -n 'func (u \*UpstreamClient) connectLocked' upstream.go
grep -n 'func (u \*UpstreamClient) reinitSession' upstream.go
grep -n 'func (u \*UpstreamClient) logUpstreamError' upstream.go

# No synthesized result on error paths: every `return nil, err` in callTool is a nil result:
grep -n 'return nil, err\|return nil, fmt.Errorf' upstream.go

# go.mod UNCHANGED:
git diff --stat go.mod          # expect EMPTY

# Only the two files changed:
git status --short              # expect:  M upstream.go   M upstream_test.go   (NO other lines)

# Expected: zero errors; one ErrSessionMissing check; 6 struct fields; 3 new methods;
# nil-result-on-every-error-path; go.mod clean.
```

### Level 2: Unit Tests (Component Validation)

```bash
# S1+S2 (unchanged) + S3 new tests, no race.
go test -run 'Upstream' -v

# Race gate (compare-and-close path + session snapshot under concurrent expiry).
go test -race -run 'Upstream' -v

# MUST PASS:
#   TestUpstreamClient_SessionExpiryReinitSuccess -> after st.expire(), the 2nd callTool
#        re-inits ONCE (u.session changed), retries, and returns the real result; st.calls==2.
#   TestUpstreamClient_ReinitRetryFailsHonestError -> re-init succeeds but retry returns the
#        tool error; callTool returns (nil, err) wrapping errFakeUpstreamTool, NOT
#        ErrSessionMissing; no synthesis; session changed; session_expired log line present.
#   TestUpstreamClient_NonSessionErrorNoReinit -> a non-ErrSessionMissing error is surfaced
#        verbatim; NO re-init; NO upstream_error line.
#   (S1/S2 tests stay green UNCHANGED: LazyInit/LazyReuse/Concurrent/EnsureSessionError +
#    NewUpstreamHTTPClient/AuthInjector_ContextThreading/AuthForwarded/AuthNotRetained.
#    The harness change is backward-compatible: they never call expire()/setToolErr().)
# Expected: PASS, exit 0 (with AND without -race). If SessionExpiryReinitSuccess sees a
# permanent ErrSessionMissing on the re-init, the harness is 404'ing the NEW session id
# too — re-check the "expire a SPECIFIC id" gotcha (research §3.2). If ReinitRetryFails-
# HonestError returns a non-nil result, a failure path synthesized one — remove it. If
# NonSessionErrorNoReinit triggers a re-init, the detection is too broad (did you use
# strings.Contains instead of errors.Is(ErrSessionMissing)?).
```

### Level 3: Integration Testing (System Validation)

```bash
# Full suite stays healthy with the upstream-client change.
go build ./...                 # compiles (UpstreamClient still unused by non-test code -> builds)
go test ./...                  # config/resolve/logger/health/extract/teach/upstream — ALL green

# Mode A doc comments print the rationale.
go doc . UpstreamClient.callTool       # re-init-once, honest-error rule, result preservation
go doc . UpstreamClient.reinitSession  # compare-and-close concurrency trace
go doc . UpstreamClient.connectLocked  # shared by ensureSession + reinitSession

# Prove the transparent re-init end-to-end against the in-process expiring fake (no network):
go test -run TestUpstreamClient_SessionExpiryReinitSuccess -v

# Expected: build clean; full suite green; go doc shows the Mode-A rationale; the expiry
# test passes (one transparent re-init, then success). NOTE: real-z.ai delegation + the
# warning-append are NOT exercised here (they land in P1.M5.T2/M5.T3). S3 proves the
# detect→re-init→retry→success cycle and the honest-error/no-reinit paths against the
# in-process expiring fake z.ai.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Race robustness on the realistic concurrent-expiry path: many in-flight calls share the
# one session; if the session expires mid-flight, the compare-and-close must produce exactly
# one fresh session under -race. (A bespoke concurrency-expiry test is OPTIONAL; the proven
# PoC + the existing TestUpstreamClient_Concurrent under -race cover the shared-session path.)
go test -race -run TestUpstreamClient_Concurrent -v
go test -race -run TestUpstreamClient_SessionExpiryReinitSuccess -v

# Honesty audit: no failure path may construct a *mcp.CallToolResult. Every error return in
# callTool must be a nil result.
grep -A40 'func (u \*UpstreamClient) callTool' upstream.go | grep -n 'return'   # every error return is `return nil, err`

# Expected: clean under -race; every error return in callTool is a nil result; the
# upstream_error event is emitted on expiry/reinit-failed and NOT on non-expiry errors.
```

## Final Validation Checklist

### Technical Validation

- [ ] All 4 validation levels completed successfully.
- [ ] `go build ./...` clean.
- [ ] `go vet ./...` clean.
- [ ] `gofmt -l .` empty.
- [ ] `go test ./...` green (full suite, no regressions — S1/S2 tests unchanged).
- [ ] `go test -race -run 'Upstream' ./...` green (race gate on compare-and-close).

### Feature Validation

- [ ] On `ErrSessionMissing`, `callTool` re-inits the shared session ONCE and retries the
      single `CallTool`; on success returns the real result with a NEW session pointer
      (TestUpstreamClient_SessionExpiryReinitSuccess).
- [ ] If the retry fails (or re-init fails), `callTool` returns `(nil, err)` VERBATIM — no
      synthesized result (TestUpstreamClient_ReinitRetryFailsHonestError).
- [ ] A non-`ErrSessionMissing` error is surfaced verbatim WITHOUT re-init and WITHOUT an
      `upstream_error` log line (TestUpstreamClient_NonSessionErrorNoReinit).
- [ ] z.ai's result `Content` is returned UNCHANGED (no mutation, no `IsError` set by S3).
- [ ] The `upstream_error` event is emitted (session_expired / reinit_failed) when `u.log`
      is non-nil; it is a no-op when nil.
- [ ] Exactly ONE re-init + ONE retry per inbound call (never loops on a 404).

### Code Quality Validation

- [ ] Detection uses `errors.Is(err, mcp.ErrSessionMissing)` (exactly once, in `callTool`);
      never `==`, never `strings.Contains`.
- [ ] `connectLocked` is a behavior-preserving extraction; `ensureSession`/S1 tests unchanged.
- [ ] `reinitSession` is concurrency-safe (compare-and-close under `u.mu`); the lock spans
      the network `Connect` (mirrors `ensureSession`).
- [ ] The new `log *logger` field is nil-safe and NOT credential-named (S2's AuthNotRetained
      reflect name-walk stays green).
- [ ] Follows existing conventions (package main; t.Helper builders; doc comments cite PRD
      sections and SDK file:line like S1/S2 siblings).
- [ ] Imports: only `"errors"`/`"fmt"` (upstream.go) and `"bytes"`/`"errors"` (upstream_test.go) added.

### Documentation & Deployment

- [ ] Mode-A doc comments on `callTool`, `reinitSession`, `connectLocked`, `logUpstreamError`,
      and the `log` field document the re-init-once rule, the honest-error rule (no
      synthesis), the result-preservation note (stringified-array text kept intact), the
      compare-and-close concurrency trace, and the nil-safety of the logger.
- [ ] No new env vars / config / routes (upstream-client logic only).

---

## Anti-Patterns to Avoid

- ❌ Don't retry a 404 with the SAME session — it is terminal. Build a FRESH transport +
  `Client.Connect` (never re-send `initialize` on the dead session; its sessionID is stale).
- ❌ Don't loop on a 404 — re-init + retry EXACTLY ONCE, then surface the real error.
  Never synthesize a `*mcp.CallToolResult` on failure.
- ❌ Don't react to transient 5xx/429 — those map to `jsonrpc2.ErrRejected` and are retried
  by the transport WITHOUT breaking the session. Only `ErrSessionMissing` needs re-init.
- ❌ Don't use `==` or `strings.Contains` to detect the sentinel — use
  `errors.Is(err, mcp.ErrSessionMissing)` (the error wraps it via `%w` and carries a
  dynamic session id).
- ❌ Don't hold `u.mu` during `CallTool` — only during the session snapshot and during
  `reinitSession`/`ensureSession`'s `Connect`. Concurrent `CallTool`s must proceed in
  parallel once a session exists.
- ❌ Don't let two concurrent expiry observers both `Connect` — use compare-and-close
  (`u.session != dead` → reuse the winner's fresh session).
- ❌ Don't expire "any" session-id in the test harness — expire a SPECIFIC id, or the
  re-init's `notifications/initialized` (which carries the NEW id) is 404'd and the
  re-init dies (research §3.2 — proven failure).
- ❌ Don't add auth code to S3 — the rebuilt transport's `authInjector` (S2) reads the
  current `callTool`'s auth from context for free.
- ❌ Don't transform/truncate z.ai's result `Content` or set `IsError` — return it
  unchanged; the warning is appended later by M5.T2 + teach.go.
- ❌ Don't double-log a non-expiry upstream error — surface it verbatim; M5.T2's `delegate`
  event already carries upstream status. S3 emits `upstream_error` only on expiry /
  reinit-failed.
