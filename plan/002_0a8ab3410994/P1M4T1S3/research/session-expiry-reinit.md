# S3 Research — Session-expiry re-init + honest error surfacing + result handling

Source-verified against `github.com/modelcontextprotocol/go-sdk@v1.6.1` (the module
pinned in go.mod). All file:line cites are under
`$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`.
The full detect → re-init → retry cycle was PROVEN by a throwaway PoC (see §3).

---

## 1. The sentinel: `mcp.ErrSessionMissing`

`transport.go:37`:
```go
// ErrSessionMissing is returned when the session is known to not be present on
// the server.
var ErrSessionMissing = errors.New("session not found")
```

It is an **exported package-level sentinel error** in the `mcp` package. Detection
in our code is the standard idiom:

```go
import "errors"
if errors.Is(err, mcp.ErrSessionMissing) { ... }
```

`ErrSessionMissing` is the SDK's ONE canonical signal for "the server terminated
this session". It is the exact thing S3 must react to.

---

## 2. How a server-side session-expiry becomes an `ErrSessionMissing` on the client

### 2.1 The server (z.ai / fake z.ai) MUST answer with HTTP 404 for a dead session

Per the MCP Streamable HTTP transport spec (§2.5.3; quoted in the SDK source at
`streamable.go:2054-2056`):

> "The server MAY terminate the session at any time, after which it MUST respond
> to requests containing that session ID with HTTP 404 Not Found."

The SDK's OWN server (`StreamableHTTPHandler.ServeHTTP`) implements this: when an
inbound request carries an `Mcp-Session-Id` that is no longer in its `sessions`
map, it responds `http.Error(w, "session not found", http.StatusNotFound)`
(`streamable.go:306`). So a REAL z.ai session-expiry is a 404, and the SDK server
produces exactly that 404 for a dropped session.

### 2.2 The client translates a 404 into an `ErrSessionMissing`-wrapping error

The client transport's `checkResponse` (`streamable.go:2050-2073`):
```go
func (c *streamableClientConn) checkResponse(requestSummary string, resp *http.Response) (err error) {
    // §2.5.3: "The server MAY terminate the session ... 404 Not Found."
    if resp.StatusCode == http.StatusNotFound {
        return fmt.Errorf("%s: failed to connect (session ID: %v): %w", requestSummary, c.sessionID, ErrSessionMissing)
    }
    if isTransientHTTPStatus(resp.StatusCode) { // 502/503/504/429
        return fmt.Errorf("%w: %s: %v", jsonrpc2.ErrRejected, requestSummary, http.StatusText(resp.StatusCode))
    }
    if resp.StatusCode < 200 || resp.StatusCode >= 300 {
        return fmt.Errorf("%s: %v", requestSummary, http.StatusText(resp.StatusCode))
    }
    return nil
}
```

Key facts:
- A **404** → error wrapping `ErrSessionMissing`. This is **terminal** (not retried
  by the transport).
- **502/503/504/429** → `jsonrpc2.ErrRejected` (transient) → the transport's
  `MaxRetries` (default 5) retries them. S3 does NOT touch these.
- Other non-2xx → a plain `requestSummary: StatusText` error (no sentinel wrap).

`checkResponse` failing calls `c.fail(err)` (`streamable.go:1730-1749`), which
stores the terminal error via `failOnce` and closes the `failed` channel. Every
subsequent `Read`/`Write` on that connection returns it
(`streamable.go:1752-1770`, `streamable.go:1785`). So **after an ErrSessionMissing,
the connection is dead** — the old `*mcp.ClientSession` is unusable; S3 must build
a fresh one.

### 2.3 The error surfaces from `CallTool`

`(*ClientSession).CallTool` (`client.go:990`) goes through the jsonrpc2 layer
(`conn.Call` → `Write` the request → await the response). When the `tools/call`
POST gets a 404, `checkResponse` fails, the connection fails, and `CallTool`
returns an error that **wraps `ErrSessionMissing`**.

PROVEN empirically (PoC §3). The observed error string is:
```
calling "tools/call": sending "tools/call": failed to connect (session ID: <uuid>): session not found
```
and `errors.Is(err, mcp.ErrSessionMissing)` is **true**. (The SDK's own canonical
test `Test_ExportErrSessionMissing` at `streamable_test.go:2680` proves the same
for `ListTools`; `CallTool` uses the identical transport path.)

---

## 3. PROVEN PoC — the full detect → re-init → retry cycle

A throwaway test (`/tmp/s3poc/poc_test.go`) connected a real
`StreamableClientTransport` to a real `mcp.Server` over `httptest`, expired the
session, and exercised the cycle. RESULT: **PASS**.

```
got expected ErrSessionMissing: calling "tools/call": sending "tools/call": failed to connect (session ID: UQPUJDVMMUWISUIQMJJOBDBQLW): session not found
re-init Connect succeeded with new session
re-init + retry succeeded
--- PASS: TestFullReinitCycle (0.0s)
```

### 3.1 How the fake simulates session-expiry (the test-harness design — see §5)

Because the SDK's `StreamableHTTPHandler.sessions` map is **unexported**
(`streamable.go:53`: `sessions map[string]*sessionInfo`), an EXTERNAL test
(package main) CANNOT delete a session the way the SDK's own internal test does
(`streamable_test.go:2701` reaches into `handler.sessions` because it lives IN
package mcp). The faithful external simulation is an `http.Handler` WRAPPER:

```go
type expiryWrapper struct {
    inner        http.Handler
    mu           sync.Mutex
    expiredID    string // once non-empty, 404 any request carrying THIS session-id
    observedInit string // the first Mcp-Session-Id seen on a request
}
func (e *expiryWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    sid := r.Header.Get("Mcp-Session-Id")
    // remember the first session-id we observe (the live session)
    ...e.observedInit = sid...
    if e.expiredID != "" && sid == e.expiredID {
        http.Error(w, "session not found", http.StatusNotFound) // the exact 404
        return
    }
    e.inner.ServeHTTP(w, r)
}
```

### 3.2 CRITICAL test-harness gotcha — expire a SPECIFIC session-id, not "any"

First attempt at the PoC used a blanket toggle (`if expired && sid != "" → 404`).
It FAILED the re-init:
```
re-init Connect: sending "notifications/initialized": failed to connect (session ID: RJRPOAL7TMKG2O3VMHREYY35T4): session not found
```

Why: `client.Connect` is not a single POST. After the `initialize` request (which
has NO `Mcp-Session-Id`), the server assigns a NEW session-id, and `Connect` then
sends the `notifications/initialized` notification as a SECOND POST that CARRIES
the new session-id (`client.go:256-292`: connect → handleSend[InitializeResult]
→ notify(initialized)). A blanket "404 any session-id" toggle 404'd that
notification too, killing the re-init.

FIX (and the harness design for the PRP): expire a SPECIFIC session-id (the old
one). The re-init's `initialize` has no session-id (passes through); the new
session-id differs from the expired one, so `notifications/initialized` and the
retry pass through naturally. This faithfully models real z.ai expiring ONE
session.

---

## 4. The concurrency-safe re-init design (CAS close + single Connect)

### 4.1 The problem

The session is SHARED (PRD §11.1: concurrent calls share one session). When it
expires, N concurrent in-flight calls can EACH receive `ErrSessionMissing`. Naive
re-init would trigger N Connects and/or clobber each other's replacement session.

### 4.2 The design — `reinitSession(ctx, dead)` (compare-and-close under the lock)

S3 adds a private method that, under `u.mu`, performs a **compare-and-close** of
the dead session pointer:

```go
func (u *UpstreamClient) reinitSession(ctx context.Context, dead *mcp.ClientSession) (*mcp.ClientSession, error) {
    u.mu.Lock()
    defer u.mu.Unlock()
    // If some other caller already re-init'd, reuse their fresh session.
    if u.session != nil && u.session != dead {
        return u.session, nil
    }
    // We own the re-init. Close the dead session (Close skips DELETE for
    // ErrSessionMissing — streamable.go:2236), then build a fresh one.
    if u.session != nil {
        _ = u.session.Close()
    }
    u.session = nil
    if err := u.connectLocked(ctx); err != nil {
        return nil, err // session stays nil -> next call retries
    }
    return u.session, nil
}
```

Correctness trace (two concurrent callers A, B, both holding a snapshot of the
dead session `d`):
- A acquires the lock first. `u.session == d` → not the early-return branch → A
  closes `d`, sets nil, `connectLocked` → `session2`. Returns `session2`. A
  retries CallTool on `session2`. ✅
- B blocked on the lock until A finishes. Now `u.session == session2 != d` (and
  `!= nil`) → early-return → B reuses `session2`. B retries on `session2`. ✅
- A third caller C arriving fresh: `ensureSession` finds `session2` non-nil,
  reuses it. ✅
- If A's `connectLocked` FAILS: `u.session` is left nil. B then sees `u.session
  == nil` (not the early-return branch), retries `connectLocked`. This is correct
  retry-after-error (bounded by the number of concurrent callers). ✅

The lock is held across the network `Connect`, mirroring S1's `ensureSession`
(which already holds the lock over Connect to serialize the lazy first-init).
This is the deliberate "serialize the once-only shared-resource init" trade-off;
session-expiry is rare, so the serialization cost is negligible.

### 4.3 Refactor: extract `connectLocked` (caller holds `u.mu`)

To share the transport+client+Connect body between `ensureSession` (lazy init)
and `reinitSession` (re-init), S3 extracts it into a `connectLocked(ctx) error`
that ASSUMES the caller holds `u.mu` and leaves `u.session` nil on failure:

```go
func (u *UpstreamClient) connectLocked(ctx context.Context) error {
    transport := &mcp.StreamableClientTransport{
        Endpoint:   u.upstream,
        HTTPClient: newUpstreamHTTPClient(),
    }
    client := mcp.NewClient(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)
    sess, err := client.Connect(ctx, transport, nil)
    if err != nil {
        return err // u.session stays nil
    }
    u.session = sess
    return nil
}
```

`ensureSession` becomes:
```go
func (u *UpstreamClient) ensureSession(ctx context.Context) error {
    u.mu.Lock()
    defer u.mu.Unlock()
    if u.session != nil {
        return nil
    }
    return u.connectLocked(ctx)
}
```

This is a behavior-preserving refactor: S1's tests (LazyInit/LazyReuse/Concurrent/
EnsureSessionError) all still pass (verified by reasoning: same lock, same
nil-check, same Connect, same leave-nil-on-error).

### 4.4 The `callTool` retry-once structure

```go
func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
    if err := u.ensureSession(ctx); err != nil {
        return nil, err
    }
    // First attempt: snapshot session under lock, CallTool outside lock.
    u.mu.Lock()
    sess := u.session
    u.mu.Unlock()
    res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: u.targetTool, Arguments: args})
    if err == nil {
        return res, nil
    }
    if !errors.Is(err, mcp.ErrSessionMissing) {
        // HONEST ERROR SURFACING (PRD §11.1): not a session-expiry signal.
        // Surface the upstream error verbatim. NEVER synthesize a result.
        return nil, err
    }
    // Session-expiry (404 / invalid-session): re-initialize ONCE and retry (PRD §11.1).
    u.logUpstreamError(u.targetTool, "session_expired", 1)
    sess2, rerr := u.reinitSession(ctx, sess)
    if rerr != nil {
        u.logUpstreamError(u.targetTool, "reinit_failed", 1)
        return nil, fmt.Errorf("upstream session expired; re-initialize failed: %w", rerr)
    }
    res2, err2 := sess2.CallTool(ctx, &mcp.CallToolParams{Name: u.targetTool, Arguments: args})
    if err2 != nil {
        // Retry also failed: surface the upstream error honestly (no synthesis).
        return nil, err2
    }
    return res2, nil
}
```

Notes:
- Exactly ONE re-init + ONE retry per inbound call (PRD §11.1 "re-run initialize
  once transparently and retry"). If the retry still fails, the error is returned
  verbatim — never a fabricated/empty `*mcp.CallToolResult`.
- The re-init runs inside the SAME `callTool` whose `ctx` carries `authHeaderKey`
  (S2's threading). The fresh transport's `authInjector` reads the current
  request's auth from context — **S3 needs NO auth-specific code** (confirmed in
  S2's PRP consumer seam).
- A non-`ErrSessionMissing` error (e.g. a 500, or a decode error) is surfaced
  verbatim WITHOUT retry. Only session-expiry triggers re-init. This matches
  §11.1's "on a session-expiry signal".

---

## 5. Test harness for S3 (extends S1/S2's `newFakeZAI`)

S1/S2's `newFakeZAI` builds `httptest.NewServer(mcp.NewStreamableHTTPHandler(...))`
(optionally wrapped by S2's auth-recording handler). S3 adds an `expiryWrapper`
layer that can expire a SPECIFIC session-id on demand. Two clean options:

### Option A (preferred): a toggle on `fakeState` + an outer wrapper
Extend the harness so a test can call `st.expire()` after the first call. The
wrapper captures the first observed `Mcp-Session-Id` and, once expired, returns
404 for requests carrying THAT id only. The re-init (initialize, no id) and the
new session (different id) pass through.

```go
// added to fakeState
mu        sync.Mutex
expiredID string
liveID    string // first Mcp-Session-Id observed

func (st *fakeState) expire() {
    st.mu.Lock()
    defer st.mu.Unlock()
    st.expiredID = st.liveID
}
// the recording/expiry http.Handler wrapper records Authorization (S2) AND,
// for each request, remembers the first Mcp-Session-Id and 404s the expired one.
```

### Option B: a separate `newExpiringFakeZAI(t)` for the one expiry test
Keeps S1/S2's `newFakeZAI` byte-for-byte unchanged; builds a sibling harness with
the `expiryWrapper` for the session-expiry test only. Less reuse, but zero risk
to S1/S2 tests.

The PRP recommends Option A (extend `fakeState` + the wrapper) because M5.T3's e2e
case 6 (session-expiry) reuses exactly this — but S3 ships the harness extension.

---

## 6. The logger field (the `upstream_error` event lives in S3)

PRD §15 defines the `upstream_error` event: "non-2xx / session-expiry / re-init
attempts" with fields `called_tool`, `status`, re-init attempts. The re-init
attempts count is KNOWN ONLY inside S3's retry loop, so S3 owns the emission.

The codebase logger (`logger.go`) is an injected `*logger` with
`log(level, msg, fields)`; it is NEVER package-level (main.go builds it and passes
it to `logStartup`). S1/S2's `UpstreamClient` has exactly 5 fields and NO logger.

DECISION: S3 adds a 6th field `log *logger` to `UpstreamClient`:
```go
type UpstreamClient struct {
    mu          sync.Mutex
    session     *mcp.ClientSession
    upstream    string
    targetTool  string
    targetParam string
    log         *logger // nil-safe: if nil, logUpstreamError is a no-op
}
```
- **Nil-safe** (`if u.log == nil { return }`): all S1/S2 tests construct
  `&UpstreamClient{upstream, targetTool, targetParam}` WITHOUT a logger and stay
  green unchanged. S3's new tests construct WITH a buffer-backed logger.
- **No §17 conflict**: `log` is `*logger`, not a string and not credential-named,
  so S2's `TestUpstreamClient_AuthNotRetained` (reflect name-walk + string-equality
  check) still passes.
- **M5.T2 sets it**: `&UpstreamClient{upstream: cfg.Upstream, targetTool:
  cfg.TargetTool, targetParam: cfg.TargetParam, log: log}`.

The helper:
```go
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
S3 emits it on: session-expiry detected (`status="session_expired"`, attempts=1),
re-init failure (`status="reinit_failed"`). A plain non-expiry upstream error is
surfaced verbatim and left for M5.T2's `delegate` event (which already carries
upstream status) — S3 does NOT double-log it.

---

## 7. Result preservation (PRD §11.3, §8) — S3 does NOT transform the result

z.ai's `tools/call` result Content is a JSON-encoded STRING (a stringified array),
not an object (PRD §8: `"text":"<json string>"`). S3's `callTool` returns the
`*mcp.CallToolResult` from `session.CallTool` UNCHANGED (the happy path returns
`res`; the retry path returns `res2`). S3 never re-parses, reorders, or truncates
`Content`, and never sets `IsError`. The warning append happens AFTER, in M5.T2,
via `teach.go`'s `appendWarning(res, text)` (teach.go:86: `result.Content = append
(result.Content, &mcp.TextContent{Text: text})` — appends, preserves order). S3's
job is the doc-comment note + the discipline of not touching `Content`/`IsError`.

---

## 8. What S3 does NOT do (scope boundaries — do not creep)

- Does NOT add Authorization (S2's job — S3 reuses it for free via context).
- Does NOT append warnings (M5.T2 + teach.go).
- Does NOT extract queries / build args (M5.T2 + extract.go).
- Does NOT wire `UpstreamClient` into the SDK handler (M5.T2).
- Does NOT retry transient 5xx/429 (the SDK transport's `MaxRetries` owns that —
  S3 only reacts to the terminal `ErrSessionMissing`).
- Does NOT change `newUpstreamHTTPClient` (S1 builds it, S2 wraps it; S3 reuses).
- Does NOT change `go.mod` (the `mcp` require already exists; `errors`/`fmt` are
  stdlib).

---

## 9. Verified facts summary (file:line)

| Fact | Source |
|---|---|
| `var ErrSessionMissing = errors.New("session not found")` | transport.go:35-37 |
| server 404s a dead session: `http.Error(w, "session not found", 404)` | streamable.go:306 |
| client: 404 → `fmt.Errorf("...: %w", ErrSessionMissing)` | streamable.go:2057-2060 |
| `checkResponse` 502/503/504/429 → `ErrRejected` (transient, retried) | streamable.go:2064-2066 |
| terminal error stored via `fail`; subsequent Read/Write return it | streamable.go:1730-1770, 1785 |
| `Close()` skips DELETE when `errors.Is(failure, ErrSessionMissing)` | streamable.go:2236-2237 |
| `(*ClientSession).CallTool(ctx, *CallToolParams) (*CallToolResult, error)` | client.go:990 |
| `(*Client).Connect` does initialize + notify(initialized) synchronously | client.go:256-292 |
| `notify(initialized)` POST carries the NEW session-id (harness gotcha) | client.go:256-292 (PoC §3.2) |
| `StreamableHTTPHandler.sessions` is UNEXPORTED (external test wraps) | streamable.go:53 |
| canonical expiry test (inside mcp pkg) | streamable_test.go:2680-2720 |
