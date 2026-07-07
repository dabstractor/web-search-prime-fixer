# Research — P1.M1.T4.S2 stdlib verification (go1.26.4, GOROOT=/usr/lib/go)

Every Go-side claim below was verified by reading the installed toolchain source
on-disk (`go1.26.4-X:nodwarf5 linux/amd64`). Protocol-side claims (MCP/SSE wire
format) come from PRD §8 / `architecture/external_deps.md` (already verified there).

## 1. stdlib hop-by-hop header set — EXACT 9 entries (supersedes PRD §11.2's 8)

Source: `/usr/lib/go/src/net/http/httputil/reverseproxy.go:365`:

```go
// Hop-by-hop headers. These are removed when sent to the backend.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer", // not Trailers per URL above
	"Transfer-Encoding",
	"Upgrade",
}
```

**Conclusion for T4.S2:** use this verbatim 9-entry list. PRD §11.2 lists 8 (omits
`Proxy-Connection`); the work-item contract also lists 8. The stdlib var has 9.
Using the stdlib's 9 is strictly safer (a superset) — it also strips the de-facto
`Proxy-Connection`. `"Te"` vs `"TE"` is irrelevant because `http.Header`
canonicalizes keys via `textproto.CanonicalMIMEHeaderKey`, so comparing
`CanonicalHeaderKey(name) == CanonicalHeaderKey(h)` matches either casing.
This list is UNEXPORTED in `net/http/httputil` (lowercase `hopHeaders`), so we
must define our own package-local copy of the exact bytes.

## 2. DefaultTransport clone + ResponseHeaderTimeout (PRD §17) — VERIFIED

Source: `/usr/lib/go/src/net/http/transport.go`

`DefaultTransport` (line 46) is a `RoundTripper` interface value backed by
`*http.Transport` with: dial 30s/keepalive 30s, `ForceAttemptHTTP2: true`,
`MaxIdleConns: 100`, `IdleConnTimeout: 90s`, `TLSHandshakeTimeout: 10s`,
`ExpectContinueTimeout: 1s` — i.e. the "sensible dial/TLS/h2 defaults" the
contract wants. `func (t *Transport) Clone() *Transport` exists (line 337) and
copies every field (including `ResponseHeaderTimeout`, line 353) so the clone
keeps all defaults.

`ResponseHeaderTimeout` field doc (line 227): *"specifies the amount of time to
wait for a server's response headers after fully writing the request (including
its body, if any). **This time does not include the time to read the response
body.**"* → bounds time-to-first-byte on headers WITHOUT bounding the SSE body.

Correct construction (the work item's "clone DefaultTransport, set
ResponseHeaderTimeout"):

```go
tr := http.DefaultTransport.(*http.Transport).Clone() // type-assert RoundTripper -> *Transport
tr.ResponseHeaderTimeout = 30 * time.Second
client := &http.Client{Transport: tr}
```

**CRITICAL:** do NOT set `http.Client.Timeout` (external_deps.md §5) — it is a
whole-exchange deadline that INCLUDES reading the body and would cut off long
SSE streams. Leave it zero (no client deadline); rely on per-request context
cancellation instead.

## 3. Streaming request body + context propagation — VERIFIED

`func NewRequestWithContext(ctx context.Context, method, url string, body io.Reader) (*Request, error)`
(`/usr/lib/go/src/net/http/request.go:889`).

Passing `r.Body` (an `io.ReadCloser`) as `body` sets `outReq.Body = r.Body` and
streams it to upstream WITHOUT buffering (PRD §6 minimal overhead). The request
is sent with `Transfer-Encoding: chunked` when the length is unknown — z.ai
accepts this for MCP POSTs.

`NewRequestWithContext(r.Context(), ...)` sets the context in the same call, so
client-disconnect cancels the upstream request (external_deps.md §5). This is
EQUIVALENT to the contract's `outReq = outReq.WithContext(r.Context())` but in
one step. Use whichever reads cleaner; the two-step form is only needed if the
request is built with `http.NewRequest` (which uses `context.Background()`).

## 4. Response streaming + Flusher — VERIFIED (external_deps.md §2)

`io.Copy(rw, resp.Body)` streams the upstream body straight through without
whole-body buffering. `/usr/lib/go/src/net/http/server.go` defines
`type Flusher interface { Flush() }`; doc: handlers "should always test for this
ability at runtime" → `if f, ok := rw.(http.Flusher); ok { f.Flush() }`.

For T4.S2 the contract prescribes `io.Copy` + final flush. This delivers the
full body for the canned (complete) SSE test fixtures. Real-time event-by-event
flush (a copy loop flushing per chunk) is a refinement P1.M4 may apply; not
required for the passthrough tests.

## 5. Graceful shutdown pattern — VERIFIED COMPILES CLEAN

`signal.Notify(c chan<- os.Signal, sig ...os.Signal)` (`/usr/lib/go/src/os/signal/signal.go:122`).
`func (srv *Server) Shutdown(ctx context.Context) error` exists in
`/usr/lib/go/src/net/http/server.go`; it blocks until all in-flight requests
finish or the context expires, then causes `ListenAndServe` to return
`http.ErrServerClosed` (which T4.S1's main() already guards as non-fatal).

Verified by `go vet` of a throwaway module containing exactly this pattern:

```go
go func() {
    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM) // Ctrl-C + `kill`
    sig := <-sigCh
    log.log("info", "shutdown", map[string]any{"signal": sig.String()})
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()
    _ = srv.Shutdown(ctx)
}()
if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed { ... }
```

Imports required (all stdlib): `context`, `os/signal`, `syscall`. (`os` and
`time` are already imported by main.go from T3.S1/T4.S1.)

## 6. COORDINATION POINT — T4.S1 ↔ T4.S2 (the stub swap)

T4.S1 (hard dependency, "Implementing" in parallel) ships, in main.go:
- `func passthroughHandler(w, r)` — a 501 PLACEHOLDER registered at
  `mux.HandleFunc("/", passthroughHandler)`.
- `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` (no timeouts).
- `main()` ending in `srv.ListenAndServe()` guarded by `err != http.ErrServerClosed`.
- `health_test.go` with `TestRouting_HealthzOnly` that builds its OWN inline mux
  (`mux.HandleFunc("/", passthroughHandler)`) and asserts `/mcp` → 501 stub.

T4.S2 must therefore:
1. SWAP the registration: `mux.HandleFunc("/", newProxyHandler(cfg, log, client))`.
2. Build `client := newUpstreamClient()` ONCE before the mux (PRD §17).
3. REMOVE the now-dead `passthroughHandler` (anti-pattern: no dead code).
4. ADD the signal/Shutdown goroutine before `ListenAndServe`.
5. UPDATE `health_test.go`: `TestRouting_HealthzOnly` references `passthroughHandler`
   and asserts 501 — both become invalid. Rewrite it to assert `/healthz`
   ISOLATION against a fake `httptest.Server` (hit counter stays 0 for `/healthz`),
   which is the durable PRD §19.3-case-5 invariant. The `/mcp`-forwards coverage
   lives in T4.S2's own `proxy_test.go`. (This is the one necessary cross-item
   test edit; it is minimal and well-defined. Grep `passthroughHandler` must
   return nothing after the change.)

## 7. Forward-helper design — M4 reuse (consumed, not rewritten)

The contract suggests `forwardStream(rw, upstreamReq, body []byte, log)`. The
`body []byte` shape is the P1.M4 rewrite case. For the passthrough path (T4.S2)
we must STREAM `r.Body` without buffering (PRD §6). Resolution: the forward
helper takes the fully-built `*http.Request` (whose `.Body` is `r.Body` for
passthrough / `bytes.NewReader` for M4 rewrite) and does `client.Do` +
non-hop-by-hop response-header copy + `io.Copy` + flush. P1.M4.T2.S2 EXTENDS the
body step with a conditional SSE-injection branch; the request-building
(header/hop-by-hop/Accept/ctx) and response-header-copying are reused verbatim.

## Verification summary

| # | Point | Method | Conf |
|---|-------|--------|------|
| 1 | hopHeaders = 9 entries (incl Proxy-Connection) | GOROOT reverseproxy.go:365 | High |
| 2 | DefaultTransport.Clone + ResponseHeaderTimeout | GOROOT transport.go:46,227,337 | High |
| 3 | NewRequestWithContext streams r.Body + sets ctx | GOROOT request.go:889 | High |
| 4 | io.Copy + http.Flusher streaming | GOROOT server.go + external_deps §2 | High |
| 5 | signal.Notify + Shutdown compiles clean | `go vet` throwaway module | High |
| 6 | T4.S1 stub swap + health_test.go edit | T4.S1 PRP (contract) | High |
| 7 | forward(client,rw,outReq,log) for M4 reuse | Design reconciliation | High |
