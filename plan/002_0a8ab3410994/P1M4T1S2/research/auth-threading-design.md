# P1.M4.T1.S2 Research — Authorization header threading (context + RoundTripper)

All SDK facts were VERIFIED by reading the actual Go MCP SDK source at
`/home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`
(SDK v1.6.1 — exactly what go.mod requires) and the Go 1.25 toolchain source.
The `authInjector` + `atomic.Pointer[string]` design was PROVEN race-free by a
stdlib PoC (see §5).

This item MODIFIES `upstream.go` (created by P1.M4.T1.S1). S1 ships the no-auth
skeleton; S2 adds the auth RoundTripper. S2 owns the FINAL `upstream.go` +
`upstream_test.go`. S1's no-auth tests must keep passing.

---

## §1 THE AUTH SEAM IS ALREADY IN main.go (do NOT redefine a context key)

`main.go` (P1.M1.T2.S2, COMPLETE) already defines:

```go
// main.go:38
type authHeaderKey struct{}

// main.go:45
func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

So the inbound `Authorization` header is stored in the request context under
`authHeaderKey{}`. **S2 REUSES this exact key** — it MUST NOT define a new
context-key type (a new type, e.g. `authKey struct{}`, would be a DIFFERENT key;
`ctx.Value(newKey{})` would always return nil). The work-item wording
"Define a package-private context key type (e.g. type authKey struct{})" is the
pattern description; the key already exists. S2 only ADDS the helper:

```go
// reads the inbound Authorization stored by authMiddleware; "" if absent.
func authFromContext(ctx context.Context) string {
	v, _ := ctx.Value(authHeaderKey{}).(string)
	return v
}
```

The SDK passes `req.Context()` (with authHeaderKey) into `server.Connect`
(streamable.go:491 — the SERVER side, how the inbound context reaches the tool
handler). The tool handler (P1.M5.T2) receives that ctx and passes it to
`upstream.callTool(ctx, args)`. So `authFromContext(ctx)` in callTool always sees
the inbound Authorization. ROCK SOLID (does not depend on any outbound-side SDK
behavior).

---

## §2 DOES THE SDK PROPAGATE THE PER-CALL CONTEXT TO THE OUTBOUND REQUEST? (yes — verified)

Full call chain for an upstream `tools/call`, traced through source:

```
ClientSession.CallTool(ctx, params)          // client.go:990
 -> handleSend[*CallToolResult](ctx, ...)    // client.go:998 / shared.go:136
    -> mh(ctx, method, req)                  // sending method handler
       -> call(ctx, conn, method, params)    // transport.go:218
          -> conn.Call(ctx, method, params)  // transport.go:224 (jsonrpc2)
             -> c.write(ctx, call)           // conn.go:306  (ctx passed RAW)
                -> c.writer.Write(ctx, msg)  // conn.go:718  (ctx passed RAW)
                   = streamableClientConn.Write(ctx, msg)        // streamable.go:1781
                      -> http.NewRequestWithContext(ctx, ...)    // streamable.go:1806
                         -> client.Do(req)  -> RoundTripper.RoundTrip(req)
```

KEY FACTS (all source-verified):
- `jsonrpc2.write` (conn.go:708-718) passes `ctx` **RAW** to `c.writer.Write` — it
  does NOT wrap/strip it. So context VALUES survive.
- `streamableClientConn.Write` (streamable.go:1806) builds the outbound POST with
  `http.NewRequestWithContext(ctx, ...)` using that same per-call `ctx`. The SDK
  comment at streamable.go:1918-1922 confirms: "ctx comes from the call".
- The connection (background) context is `connCtx = context.WithCancel(xcontext.Detach(ctx))`
  (streamable.go:1601), used ONLY for the standalone SSE stream, NOT for per-call
  POSTs. The detach comment explicitly says it exists "to preserve context values
  (which may be necessary for auth middleware)".

CONCLUSION: a RoundTripper that does `authFromContext(req.Context())` inside
`RoundTrip` WOULD see the inbound Authorization on every outbound POST. (This is
"Design 1" / option (d) below.) This is verified and is what the SDK is designed
for. However, S2 chooses the field-based design (Design 2) to match the work-item
contract and to depend ONLY on the inbound context (defense against any future
SDK change to outbound-side propagation). See §3.

NOTE: `setMCPHeaders` (streamable.go:1931) sets `Authorization` on the outbound
request ONLY when `oauthHandler != nil` (it pulls a token from the handler's
TokenSource). We leave `OAuthHandler` nil — we forward a static Bearer token, not
OAuth — so the SDK adds NOTHING; the authInjector is the sole source of the
outbound Authorization header.

---

## §3 DESIGN DECISION: authInjector with an atomic auth field (Design 2)

The work item PRESCRIBES:
> an `authInjector` http.RoundTripper that wraps a base Transport: RoundTrip sets
> `req.Header.Set("Authorization", a.auth)` (if non-empty) then delegates to
> `a.base.RoundTrip(req)`. ... The auth value is set from the inbound request's
> Authorization header before each callTool.

So: an `authInjector{base, auth}` RoundTripper; `callTool` sets `auth` from
`authFromContext(ctx)` before delegating to `session.CallTool`. This is Design 2.

CONCURRENCY: the authInjector is SHARED across concurrent callTool calls (one
session, one injector). "Set before each callTool" means concurrent writers. A
bare `string` field would be a DATA RACE under `-race`. Solution: store the auth
in an `atomic.Pointer[string]` (Go 1.19+; verified in
/usr/lib/go/src/sync/atomic/type.go: `Load() *T`, `Store(val *T)`, type-safe, no
panic). `setAuth` does `tmp := s; a.auth.Store(&tmp)`; RoundTrip does
`if p := a.auth.Load(); p != nil && *p != "" { req.Header.Set(...) }`. PoC-proven
race-free (§5).

WHY NOT Design 1 (read context in RoundTrip, no field)? Design 1 is cleaner (no
mutable state, no atomic) and is verified to work (§2). But:
- The work item literally specifies the `auth` field + "set before each callTool".
- Design 2's correctness depends ONLY on the inbound context (§1, rock-solid),
  NOT on the SDK propagating context to the outbound request (§2, an SDK detail).
  Defense-in-depth against future SDK changes.
The single-operator-one-key invariant (PRD §4/§17) means the auth is effectively
constant, so the "last write wins" under concurrent setAuth is harmless. The
architecture doc §6 explicitly endorses this: "store the current auth value on the
upstream client struct ... a struct field updated on each call (before the
CallTool) is the pragmatic choice."

The RoundTripper "don't mutate a request you didn't create" rule
(pkg.go.dev/net/http#RoundTripper): setting a header via `req.Header.Set` on the
request the SDK itself built (via `http.NewRequestWithContext`) is the SAME
pattern the SDK's own `setMCPHeaders` uses (streamable.go:1931 sets
`req.Header.Set(...)` directly on the passed `*http.Request`). The SDK creates a
fresh request per POST, so mutating its header before `client.Do` is safe and
idiomatic. (Cloning is only required when the SAME request object is reused; the
SDK does not reuse it.)

---

## §4 STRUCTURAL CHANGES TO upstream.go (S1 -> S2)

S1's `UpstreamClient` (5 fields): `mu sync.Mutex`, `session *mcp.ClientSession`,
`upstream string`, `targetTool string`, `targetParam string`. S1's
`newUpstreamHTTPClient() *http.Client` returns `&http.Client{Transport: tr}` with
`tr.ResponseHeaderTimeout = 30 * time.Second`, no `Timeout`. S1's `ensureSession`
builds `&mcp.StreamableClientTransport{Endpoint: u.upstream, HTTPClient:
newUpstreamHTTPClient()}`. S1's `callTool` reads `u.session` under `u.mu`, calls
`CallTool` outside the lock.

S2 ADDS (minimal diff, S1's tests keep passing):
1. NEW type `authInjector struct { base http.RoundTripper; auth atomic.Pointer[string] }`
   + `RoundTrip` + `setAuth`.
2. NEW func `authFromContext(ctx context.Context) string`.
3. NEW field `u.injector *authInjector` (the 6th field; S1 explicitly reserved it
   for S2).
4. MODIFY `ensureSession`: wrap `newUpstreamHTTPClient().Transport` with an
   `authInjector`, bake `authFromContext(ctx)` into it, store `u.injector`
   (ALONGSIDE `u.session`, only on Connect success), pass
   `&http.Client{Transport: inj}` as the transport's HTTPClient.
5. MODIFY `callTool`: after ensureSession, read `u.injector` under `u.mu`, then
   `inj.setAuth(authFromContext(ctx))` BEFORE `session.CallTool`.

S1's `newUpstreamHTTPClient` is UNCHANGED (its test stays). S2 only extracts its
`.Transport` to wrap. S1's no-auth tests still pass: their contexts have no
authHeaderKey, so `authFromContext` returns "", `setAuth("")`, RoundTrip adds no
header, the no-auth fake is happy.

New import: `"sync/atomic"` (S1 imported context, net/http, sync, time, mcp).

---

## §5 authInjector PoC (race-free, proven)

A stdlib PoC (`/tmp/poc_auth/main.go`) mirrored the planned authInjector and ran
UNDER `-race`:
- empty auth -> the recording httptest server saw NO Authorization header;
- "Bearer secret123" -> forwarded verbatim;
- 16 concurrent goroutines each setAuth+Get -> no panic, consistent value.

Result (both `go run` and `go run -race`):
```
empty auth -> no header set: OK
non-empty auth -> forwarded verbatim: OK
concurrent set+get: OK (no panic)
```
This proves the atomic.Pointer[string] field is race-free under concurrent
writers/readers and the empty-vs-nonempty header logic is correct.

---

## §6 TEST APPROACH: a recording fake z.ai (captures the Authorization header)

The MCP tool handler does NOT see HTTP headers (it gets `*mcp.CallToolRequest`).
So to assert the authInjector set the header on the OUTBOUND request, wrap the
fake z.ai's httptest.Server with a tiny recording middleware that captures
`r.Header.Get("Authorization")` on every inbound request (initialize POST +
tools/call POST + standalone SSE GET):

```go
srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	rec.mu.Lock(); rec.lastAuth = r.Header.Get("Authorization"); rec.mu.Unlock()
	mcpHandler.ServeHTTP(w, r)
}))
```

S2's `newAuthRecordingFakeZAI(t)` reuses S1's fake-z.ai MCP server (the same
AddTool/InputSchema shape) wrapped with this recorder. Tests:
- `TestUpstreamClient_AuthForwarded`: ctx with `authHeaderKey{} = "Bearer
  secret-123"` -> callTool -> recorder.lastAuth == "Bearer secret-123".
- `TestUpstreamClient_AuthEmptyNoHeader`: ctx with NO authHeaderKey -> callTool ->
  recorder.lastAuth == "" (the injector adds no placeholder header).
- `TestAuthInjector_RoundTrip`: unit-test the injector directly against a plain
  httptest server (no MCP) — setAuth("X") -> header set; setAuth("") -> no header;
  -race clean under concurrency.
- `TestAuthFromContext`: returns the stored value; "" when absent / wrong type.
- S1's `TestUpstreamClient_Concurrent` STILL passes (16 goroutines, plain ctx,
  -race) — proves concurrent setAuth("") is race-free.

Every test uses `context.WithTimeout(context.Background(), 10s)` so it cannot
hang; the fake is in-process (no network).

---

## §7 NEVER-LOG INVARIANT (PRD §15, §17, FR-7)

Three layers (all already in place except the S2 code discipline):
1. `Config` carries NO credential field (PRD §13) — `logStartup` structurally
   cannot leak a key.
2. The redacting logger (logger.go:89 `redactHeaders`) replaces the VALUE of any
   header named Authorization/Cookie/Set-Cookie/Proxy-Authorization with
   `<redacted>` (tested in logger_test.go). Defense-in-depth for the handler path.
3. S2 CODE DISCIPLINE: the auth value is a bare string (not a header), so layer 2
   would NOT catch it if it were formatted into a log line. Therefore S2 must
   NEVER pass `authFromContext`'s return or `injector.auth` into any
   `l.log`/`fmt.Sprintf`/`error` string. `UpstreamClient` has NO logger field (S1
   shipped none; S2 adds none), so `callTool` logs nothing. The `delegate` log
   event (PRD §15) is emitted by the handler (M5.T2), which logs called_tool /
   source / canonical / optionals / warning / upstream status / latency — NEVER
   auth. S2 adds no logging.

Validation gate (Level 1 grep): `grep -nE 'authFromContext|\.auth\b|authHeader'
upstream.go` and confirm hits are ONLY in the injector/header path, never in a
`log`/`Sprintf`/`error` context.

---

## §8 S3 (re-init) SEAM

"If the session must be rebuilt (S3), the new transport gets the current auth."
With this design: `ensureSession` ALWAYS bakes `authFromContext(ctx)` into a fresh
injector at creation. When S3 (P1.M4.T1.S3) resets `u.session = nil` (and
`u.injector = nil`) under the mutex and re-calls `ensureSession`, it builds a NEW
injector with the re-init call's current auth. So S3 works with NO special handling
— `ensureSession` is the single auth-baking point. The old injector is GC'd. S2
just needs to set `u.injector = nil` on the failure/ reset path so re-init creates
a fresh one (ensureSession already only sets it on success, so a failed Connect
leaves it nil → next call rebuilds).

---

## §9 REFERENCES (verified)

- pkg.go.dev/net/http#RoundTripper — "must be safe for concurrent use", "should not
  modify the request", "must always close the body". (Verified in
  /usr/lib/go/src/net/http/client.go, Go 1.25.)
- pkg.go.dev/sync/atomic#Pointer — `atomic.Pointer[T]`, Load/Store *T, type-safe.
  (Verified in /usr/lib/go/src/sync/atomic/type.go.)
- pkg.go.dev/net/http#Request.Clone — clone-before-mutate (used by the SDK's own
  setMCPHeaders equivalent pattern; not needed here since the SDK builds a fresh
  request per POST).
- go.dev/blog/context — unexported-type context-key idiom (the project's
  authHeaderKey struct{} already follows it).
- pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#StreamableClientTransport
  — Endpoint + HTTPClient fields; OAuthHandler nil => SDK adds no Authorization.
- MCP SDK v1.6.1 source: streamable.go:1574/1601/1684/1781/1806/1931,
  internal/jsonrpc2/conn.go:273/306/708/718, transport.go:218/224, shared.go:136,
  client.go:990/998. (Cited with line numbers above.)
- OWASP Logging Cheat Sheet (cheatsheetseries.owasp.org/.../Logging_Cheat_Sheet)
  — exclude tokens/credentials from logs (URL from training knowledge; not
  live-verified, but the principle is uncontested).
