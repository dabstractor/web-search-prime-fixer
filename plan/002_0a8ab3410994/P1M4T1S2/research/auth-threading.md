# P1.M4.T1.S2 Research — Authorization header threading (context + RoundTripper)

VERIFIED by reading the actual Go MCP SDK v1.6.1 source at
`$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/`
(go1.25 toolchain, exactly what go.mod requires). This research pins the
mechanism S2 builds on: the inbound `Authorization` header reaches the
outbound z.ai HTTP request through the SDK's context plumbing, so an
`http.RoundTripper` can inject it verbatim with **no mutable shared state**.

---

## §1 THE CENTRAL FINDING — the per-call ctx reaches the outbound POST's req.Context()

`CallTool(ctx, params)`'s `ctx` flows, **unchanged**, all the way to the
outbound z.ai HTTP request's `req.Context()`. The chain (every hop cites source):

```
ClientSession.CallTool(ctx, params)          client.go:990
  -> handleSend(ctx, method, req)            shared.go:136
     -> mh(ctx, method, req)                 (sendingMethodHandler)
        -> call(ctx, conn, method, params)   transport.go:222
           -> conn.Call(ctx, method, params) jsonrpc2/conn.go:273
              -> c.write(ctx, call)          jsonrpc2/conn.go:306
                 -> c.writer.Write(ctx, msg) jsonrpc2/conn.go:718
                    -> streamableClientConn.Write(ctx, msg)  streamable.go:1788
                       -> http.NewRequestWithContext(ctx, POST, url, body)  streamable.go:1806
                          -> c.client.Do(req)                streamable.go:1821
```

Key points (all verified):
- `jsonrpc2/conn.go:306` `c.write(ctx, call)` passes the SAME `ctx` it received
  from `Call` into `c.writer.Write`. **The jsonrpc2 `write` does NOT detach,
  replace, or wrap the context** (conn.go:708-720): it only checks shutdown
  state, then calls `c.writer.Write(ctx, msg)`.
- `streamable.go:1806` builds the outbound POST with `http.NewRequestWithContext(ctx, ...)`.
  So the POST's `req.Context()` IS the per-call `ctx` passed to `CallTool`.

**Consequence:** an `http.RoundTripper` wrapping `StreamableClientTransport.HTTPClient`
can read the inbound `Authorization` from `req.Context().Value(authHeaderKey{})`
at `RoundTrip` time. This is TRUE per-call threading — no struct field, no lock,
no race — and it is race-free because the authInjector holds no mutable state.

## §2 xcontext.Detach PRESERVES VALUES (so SSE GET + DELETE also carry auth)

The connection context is `connCtx = context.WithCancel(xcontext.Detach(ctx))`
(streamable.go:1601). `Detach` is defined (internal/xcontext/xcontext.go):

```go
func Detach(ctx context.Context) context.Context { return detachedContext{ctx} }
type detachedContext struct{ parent context.Context }
func (v detachedContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (v detachedContext) Done() <-chan struct{}       { return nil }
func (v detachedContext) Err() error                  { return nil }
func (v detachedContext) Value(key any) any           { return v.parent.Value(key) }  // VALUES PRESERVED
```

So `connCtx` keeps ALL values of `ctx` (the Connect ctx = the first callTool's
ctx, which carries `authHeaderKey` from `authMiddleware`), while detaching
cancellation. The SDK comment at streamable.go:1595-1607 confirms the intent:
"creating a cancellable context detached from the incoming context allows us to
preserve context values (which may be necessary for auth middleware)."

Outbound requests that use `connCtx` (NOT the per-call ctx) and therefore ALSO
get auth injected by the context-reading authInjector:
- **standalone SSE GET** — `http.NewRequestWithContext(c.ctx, GET, url, nil)`
  at streamable.go:2206 (`c.ctx == connCtx`).
- **DELETE on close** — `http.NewRequestWithContext(c.ctx, DELETE, url, nil)`
  at streamable.go:2239.

Net: a context-reading `authInjector` injects the verbatim `Authorization` on
**every** outbound z.ai request: initialize POST, tools/call POST, standalone
SSE GET, and DELETE. For the PRD's single-operator/single-key model, the value
is identical on all of them.

## §3 `Connect` uses `t.HTTPClient` (our wrapped client) for ALL requests

`StreamableClientTransport.Connect` (streamable.go:1574-1601):
```go
client := t.HTTPClient
if client == nil { client = http.DefaultClient }
...
conn := &streamableClientConn{ ..., client: client, ... }
```
And every outbound request goes through `c.client.Do(req)` (POST: 1821; GET:
2217; DELETE: 2245). So wrapping `HTTPClient.Transport` with `authInjector`
covers the entire outbound surface. **No SDK field other than `HTTPClient`
needs to be touched.**

## §4 The authHeaderKey seam ALREADY EXISTS in main.go (P1.M1.T2.S2 — DONE)

```go
// main.go
type authHeaderKey struct{}   // unexported struct type = collision-free ctx key
func authMiddleware(h http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
        h.ServeHTTP(w, r.WithContext(ctx))
    })
}
```
- The SDK server handler passes `req.Context()` into `server.Connect`
  (verified streamable.go:491-494), so `authHeaderKey` reaches the tool handler.
- **S2 MUST reuse `authHeaderKey`** (it is the key the middleware WRITES under).
  The item contract's "e.g. `type authKey struct{}`" is just an example of the
  pattern; defining a NEW key would never find the value the middleware stored.
- S2 ADDS only the reader side: `func authFromContext(ctx context.Context) string`
  returning `v, _ := ctx.Value(authHeaderKey{}).(string)`. **S2 does NOT touch
  main.go** — the middleware + key already exist.

## §5 DESIGN DECISION — context-reading authInjector (vs. a mutable struct field)

The item contract offers two shapes: (A) store `auth` on `UpstreamClient` /
the authInjector as a field, set per-call; (B) "pass auth per-call". This PRP
implements **(B) the per-call-context shape**, for these verified reasons:

1. **Race-free with zero shared state.** The authInjector holds only an
   immutable `base http.RoundTripper`. The existing S1 tests run under `-race`;
   a mutable auth field would need a mutex/atomic and careful ordering. The
   context shape needs none.
2. **True per-call threading** (matches the contract's "the auth value is set
   from the inbound request's Authorization header before each callTool"
   precisely). A field captured at lazy-init would serve the init-time value on
   later calls (correct for single-operator, but not truly per-call). The
   context shape always serves the current request's value.
3. **Most faithful to PRD §17** ("Forward Authorization verbatim; never read,
   log, or store it"). The credential lives ONLY in the transient request
   context — it never becomes a struct field, so it cannot be accidentally
   logged or persisted. The redacting logger (`redactHeaders`, logger.go)
   remains the belt-and-suspenders for any header ever logged.
4. **Minimally invasive to S1.** S1's `ensureSession` already builds the
   transport via `newUpstreamHTTPClient()` and `callTool` already passes `ctx`
   to `CallTool` (which propagates to the outbound `req.Context()`). So S2's
   only upstream.go logic change is wrapping the base transport inside
   `newUpstreamHTTPClient`. `ensureSession`/`callTool`/the struct are UNCHANGED.

The contract's `authHeader` struct field is therefore NOT added: the verified
context mechanism makes it redundant for injection, and omitting it better
serves §17. (If a future item wants a "current auth" snapshot, it can be added
later; S3's re-init also runs inside a callTool that carries `authHeaderKey`,
so it needs no field either.)

## §6 THE authInjector (exact Go)

```go
// authInjector is an http.RoundTripper that copies the inbound client's
// Authorization header (carried in the request context by main.go's
// authMiddleware) onto every outbound z.ai request. It forwards the value
// VERBATIM and never reads/logs/stores it beyond this header set.
type authInjector struct {
    base http.RoundTripper
}

func (a *authInjector) RoundTrip(req *http.Request) (*http.Response, error) {
    if auth := authFromContext(req.Context()); auth != "" {
        req.Header.Set("Authorization", auth) // verbatim; never redacted/transformed
    }
    return a.base.RoundTrip(req)
}

// authFromContext returns the inbound Authorization header threaded through the
// request context by authMiddleware (main.go), or "" if absent.
func authFromContext(ctx context.Context) string {
    v, _ := ctx.Value(authHeaderKey{}).(string)
    return v
}
```

Gotchas:
- **`req.Header.Set` directly in RoundTrip is safe here.** The net/http
  RoundTripper doc says "RoundTrip should not modify the request", but every
  outbound request is freshly built by the SDK via `http.NewRequestWithContext`
  (streamable.go:1806/2206/2239), never reused/cached, and the SDK's own
  transports set headers this way (e.g. streamable.go:1946). Direct set matches
  the SDK convention and avoids a costly `req.Clone` that can't always re-read
  the body.
- **Empty auth → header left untouched.** We `Set` only when non-empty, so we
  never fabricate or strip an Authorization header (the base transport sets
  none). If the inbound request had no Authorization, z.ai sees none (and would
  reject — but that is correct forwarding behavior, not our concern).

## §7 newUpstreamHTTPClient — wrap the S1 base transport

S1 ships:
```go
func newUpstreamHTTPClient() *http.Client {
    tr := http.DefaultTransport.(*http.Transport).Clone()
    tr.ResponseHeaderTimeout = 30 * time.Second
    return &http.Client{Transport: tr}
}
```
S2 wraps the SAME base transport with the authInjector (signature UNCHANGED):
```go
func newUpstreamHTTPClient() *http.Client {
    tr := http.DefaultTransport.(*http.Transport).Clone()
    tr.ResponseHeaderTimeout = 30 * time.Second
    return &http.Client{Transport: &authInjector{base: tr}}
}
```
- `ResponseHeaderTimeout` still lives on the `*http.Transport` (the `base`),
  unchanged (PRD §11.2). `Client.Timeout` stays 0.
- `TestNewUpstreamHTTPClient` (S1) asserted `c.Transport.(*http.Transport)`;
  after wrapping, `c.Transport` is `*authInjector`, so S2 updates that test to
  unwrap: `c.Transport.(*authInjector).base.(*http.Transport)` and assert
  `ResponseHeaderTimeout == 30s`, `Timeout == 0`.

## §8 TEST HARNESS — record the Authorization header the fake z.ai RECEIVED

The fake z.ai is `httptest.NewServer(mcp.NewStreamableHTTPHandler(...))`. The
`getServer func(*http.Request)` receives the inbound `*http.Request`, but the
cleanest capture is a wrapping `http.Handler` that records the header on EVERY
inbound request (initialize POST, tools/call POST, SSE GET) before delegating
to the SDK handler. Extend `fakeState` with `lastAuth string`:

```go
h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return zai }, nil)
recording := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    st.mu.Lock()
    st.lastAuth = r.Header.Get("Authorization")
    st.mu.Unlock()
    h.ServeHTTP(w, r)
})
srv := httptest.NewServer(recording)
```
This is backward-compatible with S1's tests (they ignore `lastAuth`). The
assertion `st.lastAuth == "Bearer <the-key>"` proves verbatim forwarding.

## §9 WHY THIS IS RACE-FREE (for the -race tests)

`authInjector` has exactly one field, `base`, set once at construction in
`newUpstreamHTTPClient` and never mutated. `RoundTrip` reads `req.Context()`
(per-request, goroutine-local to the SDK's send) and the immutable `base`.
Nothing is shared-mutable, so concurrent `callTool` calls (the S1 concurrent
test) inject auth concurrently with no data race. Contrast with a struct field
holding auth: it would be read in `RoundTrip` (SDK goroutine) and written in
`callTool` (handler goroutine) — a genuine race needing a lock/atomic. The
context shape sidesteps this entirely.

## §10 NEVER-LOGGED INVARIANT (PRD §15 / §17) — how it is enforced

- The credential is read from `req.Context()` only, transiently, inside
  `RoundTrip`. It is never assigned to a `UpstreamClient` field.
- `UpstreamClient` / `callTool` do not log in S2 (the `delegate` log event is
  M5.T2's job, and PRD §15 lists its fields as `called_tool`/`source`/
  `canonical`/`optionals`/`warning`/status/latency — explicitly NOT headers;
  even at `debug`, "raw inbound arguments shape (never headers)").
- `redactHeaders` (logger.go) replaces `Authorization` with `<redacted>` should
  any header ever be logged. Belt-and-suspenders.
- S2's runnable enforcement: a test that, after a `callTool` made with a known
  secret, `reflect`s over `UpstreamClient`'s fields and asserts NONE equals the
  secret — proving the credential is not retained in client state.
