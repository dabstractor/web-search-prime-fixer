# PRP — P1.M1.T4.S2: Minimal transparent passthrough handler + graceful shutdown

## Goal

**Feature Goal**: Deliver the **transparent MCP passthrough** (PRD §5 FR-1, §11.2,
§11.3) and **graceful shutdown** (PRD §16) as the final piece of Milestone 1.
Concretely: a hand-rolled `net/http` handler that forwards every non-`/healthz`
request to `cfg.Upstream` as a streaming `POST`, copies request headers minus the
hop-by-hop set, applies the `Accept` fallback required by z.ai, propagates the
client context upstream, then copies the upstream status + non-hop-by-hop
response headers and `io.Copy`'s the body through with an SSE flush — **leaving
the SSE response byte-for-byte unchanged**. Plus `SIGINT`/`SIGTERM` →
`server.Shutdown(10s)` → `shutdown` log. The forward core is built to be
**consumed and EXTENDED (not rewritten)** by P1.M4.T1/T2.

**Deliverable**: Three changes at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), all `package main`:
1. **CREATE** `proxy.go` — `var hopHeaders` (the exact stdlib 9-entry list),
   `func isHopByHop(string) bool`, `func newUpstreamClient() *http.Client`
   (clones `DefaultTransport`, sets `ResponseHeaderTimeout=30s`, NO `Client.Timeout`),
   `func copyForwardHeaders(dst, src http.Header)`, `func newProxyHandler(cfg
   Config, log *logger, client *http.Client) http.HandlerFunc` (the handler
   factory), and `func forward(client *http.Client, rw http.ResponseWriter,
   outReq *http.Request, log *logger)` (the reusable forward core).
2. **MODIFY** `main.go` (on top of the T4.S1 bootstrap) — build `client :=
   newUpstreamClient()` once; **swap** `mux.HandleFunc("/",
   passthroughHandler)` → `mux.HandleFunc("/", newProxyHandler(cfg, log,
   client))`; **remove** the now-dead `passthroughHandler` stub; **add** the
   graceful-shutdown goroutine (`signal.Notify(SIGINT/SIGTERM)` →
   `srv.Shutdown(ctx 10s)` → `log "shutdown"` → return); add the `context`,
   `os/signal`, `syscall` imports. T4.S1 symbols (`version`, `healthHandler`,
   `logStartup`, the logger) are PRESERVED.
3. **MODIFY** `health_test.go` — `TestRouting_HealthzOnly` referenced
   `passthroughHandler` and asserted `/mcp` → 501; both are now invalid. Rewrite
   it to assert the durable `/healthz`-isolation invariant (PRD §19.3 case 5)
   against a fake `httptest.Server` (hit counter stays 0 for `/healthz`). **CREATE**
   `proxy_test.go` — `httptest.Server` fake upstream returning a canned SSE
   `initialize` (PRD §8); assert the SSE body + `mcp-session-id` reach the client
   byte-for-byte, the `Accept` fallback fires, hop-by-hop request headers are
   stripped, and `Authorization` is forwarded verbatim.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. A booting binary forwards an MCP `initialize`
to a fake upstream and the client receives the exact canned SSE bytes plus the
`mcp-session-id` response header unchanged. `SIGTERM` causes a clean exit with one
`shutdown` JSON line on stderr. `go.mod` gains zero `require`s (every new import
is stdlib). The real-z.ai `initialize` smoke test is a **MANUAL** step (needs an
API key); it is documented but not automated (httptest e2e vs the live endpoint
is P1.M5).

## User Persona

**Target User**: (1) the MCP client (pi/Claude/Cursor) now able to talk to the
proxy end-to-end for the passthrough case; (2) the operator/process-supervisor
who needs clean `SIGTERM` handling; (3) downstream subtask implementers —
P1.M4.T1/T2 EXTEND `newProxyHandler`/`forward`; P1.M5.T1.S1 reuses the fake-upstream
harness; P1.M5.T1.S2 asserts `Authorization` is forwarded+redacted and
`/healthz` is isolated.

**Use Case**: The client `POST`s an MCP `initialize` to `http://127.0.0.1:8787/mcp`;
the proxy forwards it to z.ai verbatim and streams the SSE `initialize` response
back with `mcp-session-id` intact. On `SIGTERM` the proxy drains in-flight
requests within 10s and exits.

**User Journey**: client → `POST /mcp` (with `Authorization: Bearer …`) → proxy
builds upstream POST (headers copied minus hop-by-hop, `Accept` defaulted,
context propagated) → z.ai returns `200` SSE + `mcp-session-id` → proxy copies
status/headers and `io.Copy`'s the SSE body → client receives identical bytes.
Operator sends `kill <pid>` → proxy logs `shutdown` and exits 0 within 10s.

**Pain Points Addressed**: (1) until now every non-`/healthz` request hit a 501
stub — the proxy could not actually proxy; this makes it a working transparent
forwarder. (2) `Ctrl-C`/`kill` previously killed the process hard; now it drains
gracefully. (3) P1.M4 needs a forward core to extend rather than reinvent — this
ships exactly that seam.

## Why

- Closes out **PRD §20 step 1** ("serve `/healthz` and a no-op passthrough
  handler … verify it boots and proxies `initialize`") — the "proxies
  `initialize`" half is this subtask.
- Implements **PRD §5 FR-1** (transparent proxy), **§8** (verified transport
  contract — the `text/event-stream` `Accept` requirement, `Mcp-Session-Id`
  identity pass-through), **§11.2** (forward to upstream), **§11.3 passthrough
  path** (io.Copy verbatim, flush on SSE), **§13** (Authorization forwarded
  verbatim, never owned), **§16** (graceful shutdown), **§17** (timeouts:
  `ResponseHeaderTimeout=30s`, NO `Client.Timeout`, context cancellation).
- Establishes the **forward core** P1.M4 consumes: `forward(client, rw, outReq,
  log)` does the send + response-header copy + io.Copy + flush; P1.M4.T1 swaps
  the streamed `r.Body` for rewritten bytes and tracks `reqID`; P1.M4.T2.S2
  adds the conditional SSE-injection branch in the body step. Request-header
  building and response-header copying are reused verbatim.
- Wires the **graceful-shutdown seam** T4.S1 deliberately left open (`srv` is
  `*http.Server{Addr, Handler}` with no timeouts; `ErrServerClosed` is already
  guarded) — T4.S2 is a pure *addition* around that `srv`.

## What

`proxy.go` (NEW, `package main`) provides:

- **`var hopHeaders = []string{...}`** — the **exact stdlib 9-entry list** from
  `net/http/httputil/reverseproxy.go:365` (Connection, Proxy-Connection,
  Keep-Alive, Proxy-Authenticate, Proxy-Authorization, Te, Trailer,
  Transfer-Encoding, Upgrade). This is the contract's "exact stdlib hopHeaders
  list per external_deps.md §4"; it supersedes PRD §11.2's 8-entry summary by
  also stripping the de-facto `Proxy-Connection`. It is unexported in stdlib, so
  we reproduce the bytes verbatim.
- **`func isHopByHop(name string) bool`** — canonicalizes both sides and tests
  membership (so `"TE"`/`"Te"` match identically).
- **`func newUpstreamClient() *http.Client`** — `tr :=
  http.DefaultTransport.(*http.Transport).Clone(); tr.ResponseHeaderTimeout =
  30 * time.Second; return &http.Client{Transport: tr}`. DefaultTransport already
  carries dial 30s / TLS 10s / h2-via-ALPN / idle-pooling defaults — the clone
  keeps them. **`Client.Timeout` is left zero** (a hard deadline would cut SSE).
- **`func copyForwardHeaders(dst, src http.Header)`** — copy every `src` header
  into `dst` EXCEPT `isHopByHop` ones (so Authorization, Content-Type, Accept,
  Mcp-Session-Id, Accept-Language, User-Agent all pass through verbatim).
- **`func newProxyHandler(cfg Config, log *logger, client *http.Client)
  http.HandlerFunc`** — builds `outReq` via
  `http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.Upstream, r.Body)`
  (streams `r.Body` without buffering; sets the context in one step ≡ the
  contract's `outReq.WithContext(r.Context())`); `copyForwardHeaders(outReq.Header,
  r.Header)`; **Accept fallback**: `if outReq.Header.Get("Accept") == "" {
  outReq.Header.Set("Accept", "application/json, text/event-stream") }` (the
  `text/event-stream` token is REQUIRED by z.ai per PRD §8 — without it z.ai
  returns empty); then `forward(client, rw, outReq, log)`. **A client-provided
  Accept is passed through unmodified** (PRD §11.2 "do not otherwise modify").
- **`func forward(client, rw, outReq, log)`** — `resp, err := client.Do(outReq)`;
  on error log `"upstream_error"` (PRD §15) and write `502`; else `defer
  resp.Body.Close()`, copy non-hop-by-hop response headers into `rw.Header()`
  BEFORE `rw.WriteHeader(resp.StatusCode)`, then `io.Copy(rw, resp.Body)` and
  `if f, ok := rw.(http.Flusher); ok { f.Flush() }`. This is the SSE-preserving
  passthrough: status + headers verbatim, body byte-for-byte.

`main.go` (MODIFY) adds: `client := newUpstreamClient()`; the registration swap;
the graceful-shutdown goroutine; imports `context`, `os/signal`, `syscall`. The
`passthroughHandler` stub is **deleted**.

`health_test.go` (MODIFY) and `proxy_test.go` (NEW) — see Validation Loop Level 2.

### Success Criteria

- [ ] `proxy.go` ships `hopHeaders` (9 entries, verbatim stdlib), `isHopByHop`,
      `newUpstreamClient` (cloned transport, `ResponseHeaderTimeout=30s`, no
      `Client.Timeout`), `copyForwardHeaders`, `newProxyHandler`, `forward`.
- [ ] A request to a fake upstream is received by upstream as `POST
      cfg.Upstream` with the client's `Authorization`, `Content-Type`,
      `Mcp-Session-Id` (and any `Accept-Language`/`User-Agent`) headers present,
      and NONE of the hop-by-hop headers present.
- [ ] When the client omits `Accept`, the upstream receives `Accept:
      application/json, text/event-stream`; when the client sends `Accept:
      application/json`, that value is forwarded **unchanged** (no overwrite).
- [ ] The canned SSE `initialize` body returned by the fake upstream reaches the
      client **byte-for-byte**, with status `200`, `Content-Type` containing
      `text/event-stream`, and the `mcp-session-id` response header intact
      (identity pass-through, PRD §8).
- [ ] `SIGTERM`/`SIGINT` causes the server to call `Shutdown` (≤10s) and emit
      exactly one `shutdown` JSON line containing `signal`, then exit 0.
- [ ] `/healthz` still returns `200 {"ok":true,"version":"…"}` and **never**
      touches the upstream (isolation invariant, PRD §16/§19.3 case 5).
- [ ] `main.go` no longer references `passthroughHandler`; `grep -rn
      passthroughHandler .` returns nothing; T4.S1 logger/health/version symbols
      are unchanged.
- [ ] `go.mod` gains zero `require`s (new imports: `context`, `os/signal`,
      `syscall` are stdlib; `proxy.go` uses `io`, `net/http`, `time`).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.

## All Needed Context

### Context Completeness Check

_Pass._ The work-item contract fixes: the manual-handler decision + the exact
hop-by-hop list + `req.WithContext(r.Context())` + "no `Client.Timeout`" +
`ResponseHeaderTimeout=30s` + `io.Copy`/`http.Flusher` (all cross-referenced to
`architecture/external_deps.md` §2/§4/§5, which were verified against the
on-disk Go source). The INPUT state — T4.S1's `main.go` (logger + `version` +
`healthHandler` + `logStartup` + `passthroughHandler` stub + real `main()` with
`srv.ListenAndServe()` guarded by `ErrServerClosed`), and the logger/config APIs
— is read on-disk / fixed by the T4.S1/T3.S1/T2.S2 PRP contracts. The five
stdlib behaviors T4.S2 rests on (`hopHeaders` 9-entry var, `DefaultTransport`
type-assert + `Clone` + `ResponseHeaderTimeout`, `NewRequestWithContext(ctx,…,
r.Body)`, `signal.Notify`+`Server.Shutdown`, `io.Copy`+`http.Flusher`) are
**verified on-disk** in `research/verify-proxy-stdlib.md` (go1.26.4). The one
cross-item coordination point (the stub swap forces a `health_test.go` edit) is
fully specified in the Implementation Tasks. The canned SSE `initialize` bytes
are quoted verbatim from PRD §8. An agent with no prior knowledge of this
codebase can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative transport/handler/shutdown contract.
- file: PRD.md
  why: §5 FR-1 (transparent proxy: forward every JSON-RPC method, preserve
        session IDs/headers/SSE framing end-to-end); §8 (Verified transport
        contract — POST to cfg.Upstream; the Accept "application/json,
        text/event-stream" token is REQUIRED or z.ai returns empty; Authorization
        Bearer; Mcp-Session-Id identity pass-through; the exact initialize SSE
        bytes incl. `id:`/`event:`/`data:` and no space after `data`);
        §11.2 (Forward to upstream — copy Content-Type/Accept/Authorization/
        Mcp-Session-Id + Accept-Language/User-Agent; strip hop-by-hop; Accept
        fallback; context-governed client, no hard timeout); §11.3 (Write the
        response — copy Content-Type/Mcp-Session-Id/Cache-Control/Vary/X-Log-Id
        and other non-hop-by-hop; strip hop-by-hop; copy status; passthrough =
        io.Copy verbatim; flush on SSE); §13 (Authorization forwarded verbatim,
        never read/logged/stored; dials ONLY cfg.Upstream, not an open proxy;
        loopback only); §16 (Graceful shutdown: SIGINT/SIGTERM ->
        server.Shutdown(ctx) 10s deadline -> exit); §17 (Transport with sensible
        dial/TLS/h2 defaults; request lifetime = client context via
        req.WithContext; NO Client.Timeout; ResponseHeaderTimeout default 30s);
        §19.3 (e2e test plan — fake upstream returns initialize SSE + asserts
        Accept contains text/event-stream; case 5 /healthz isolated).
  critical: the `text/event-stream` Accept token is MANDATORY or z.ai returns
        empty results — the Accept fallback MUST set exactly
        `application/json, text/event-stream`. Authorization is forwarded
        UNCHANGED and never read (the handler must not log its value — pass
        headers through redactHeaders only if logging). Strip hop-by-hop on BOTH
        request and response. Do NOT set http.Server timeouts (would cut SSE) —
        T4.S1 already built srv with Addr+Handler only; keep it.

# INPUT (HARD DEPENDENCY) — the main/mux this subtask consumes.
- file: plan/001_c0abc3757e9a/P1M1T4S1/PRP.md
  why: T4.S1 ships main.go with: the T3.S1 logger (UNCHANGED); `var version`;
        `func healthHandler`; `func logStartup(l *logger, cfg Config)`;
        `func passthroughHandler(w,r)` (501 STUB, registered at
        `mux.HandleFunc("/", passthroughHandler)`); and `func main()` =
        ResolveConfig -> newLogger -> logStartup -> mux (`/healthz`+`/`) ->
        `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` (NO timeouts) ->
        `srv.ListenAndServe()` guarded by `err != http.ErrServerClosed`. Also
        ships health_test.go with TestRouting_HealthzOnly (inline mux using
        passthroughHandler; asserts /mcp -> 501).
  critical: T4.S2 SWAPS `mux.HandleFunc("/", passthroughHandler)` -> the real
        factory, REMOVES passthroughHandler (now dead), and ADDS the
        shutdown goroutine around `srv`. Because TestRouting_HealthzOnly
        references passthroughHandler, T4.S2 MUST edit health_test.go (Task 3).
        T4.S1's ErrServerClosed guard is exactly what makes Shutdown() a clean
        return (do not remove it). Build srv with Addr+Handler ONLY — do NOT add
        ReadTimeout/WriteTimeout (would truncate SSE).

# INPUT (HARD DEPENDENCY) — the logger API, already on disk (T3.S1).
- file: plan/001_c0abc3757e9a/P1M1T3S1/PRP.md
  why: `type logger struct{w io.Writer; level int}`; `func newLogger(w
        io.Writer, level string) *logger`; `func (l *logger).log(level, msg
        string, fields map[string]any)`; `func redactHeaders(h http.Header)
        map[string]any`. Used by newProxyHandler/forward for upstream_error and
        the shutdown event.
  critical: keep the logger UNCHANGED. `log.log("warn", "upstream_error",
        map[string]any{"err": ...})` matches the PRD §15 event names; if you log
        request headers anywhere, pass them through redactHeaders (Authorization
        -> "<redacted>"). Do NOT import the stdlib "log" package.

# INPUT (HARD DEPENDENCY) — the Config shape, already on disk (T2).
- file: plan/001_c0abc3757e9a/P1M1T2S2/PRP.md
  why: `type Config{ Upstream, Listen, Path string; Aliases []string;
        TargetParam, LogLevel string }`; `func ResolveConfig() (Config, error)`;
        `func DefaultConfig() Config`. cfg.Upstream is the FULL z.ai URL
        (https://api.z.ai/api/mcp/web_search_prime/mcp) — forward to it as-is;
        the incoming path is IGNORED for forwarding (PRD §9).
  critical: cfg.Upstream is absolute and validated by ResolveConfig. There is NO
        credential field on Config (PRD §13) — Authorization comes from the
        request header at runtime, forwarded verbatim. cfg.Aliases/cfg.TargetParam
        are NOT used by T4.S2 (passthrough); they are P1.M2/M4 concerns.

# VERIFIED GOTCHAS — on-disk proof of the five stdlib behaviors.
- file: plan/001_c0abc3757e9a/P1M1T4S2/research/verify-proxy-stdlib.md
  why: go1.26.4 on-disk: the exact hopHeaders var (9 entries, incl
        Proxy-Connection; "Te" canonicalizes with "TE"); DefaultTransport is a
        RoundTripper so `http.DefaultTransport.(*http.Transport).Clone()` is
        required (Clone keeps all defaults, copies ResponseHeaderTimeout);
        ResponseHeaderTimeout doc "does not include the time to read the
        response body"; NewRequestWithContext(ctx,method,url,r.Body) streams +
        sets ctx in one call; signal.Notify+Server.Shutdown compiles clean;
        io.Copy+http.Flusher streaming.
  critical: hopHeaders is the 9-ENTRY stdlib var, NOT PRD §11.2's 8 — using 9
        is a safe superset (also strips Proxy-Connection). Do NOT set
        http.Client.Timeout. NewRequestWithContext(r.Context(),...,r.Body) is
        EQUIVALENT to the contract's req.WithContext(r.Context()) but in one
        step — use it.

# ARCHITECTURE — stdlib-only invariant.
- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: "No third-party dependencies are required or allowed (PRD §6, §9)."
  critical: do NOT use httputil.ReverseProxy (external_deps.md §4: it cannot
        cleanly express conditional SSE injection / request-side body mutation —
        that's WHY a manual handler was chosen). Hand-roll with net/http.

# Go stdlib refs — exact semantics relied upon (stable; verified on-disk).
- url: https://pkg.go.dev/net/http#Transport
  why: ResponseHeaderTimeout field — "the amount of time to wait for a server's
        response headers after fully writing the request … This time does not
        include the time to read the response body." Bounds time-to-first-byte
        without bounding SSE.
  critical: do NOT set Client.Timeout (external_deps.md §5) — it includes body
        read time and would cut SSE.
- url: https://pkg.go.dev/net/http#Client
  why: `&http.Client{Transport: tr}` with Timeout left zero -> no whole-exchange
        deadline; cancellation flows from the per-request context.
- url: https://pkg.go.dev/net/http#NewRequestWithContext
  why: `func NewRequestWithContext(ctx, method, url, body io.Reader) (*Request,
        error)`. Passing r.Body streams it; passing ctx propagates disconnects.
- url: https://pkg.go.dev/net/http#Flusher
  why: `type Flusher interface { Flush() }`; guard `if f, ok := rw.(http.Flusher);
        ok { f.Flush() }` to push SSE events to the client.
- url: https://pkg.go.dev/net/http#Server
  why: `func (srv *Server) Shutdown(ctx context.Context) error` — gracefully
        shuts down without interrupting active connections; causes
        ListenAndServe to return http.ErrServerClosed.
- url: https://pkg.go.dev/os/signal#Notify
  why: `func Notify(c chan<- os.Signal, sig ...os.Signal)` — relay SIGINT/SIGTERM
        to a channel.
- url: https://pkg.go.dev/net/http/httputil#ReverseProxy
  why: the stdlib hopHeaders var lives here (reverseproxy.go:365) — the source of
        the exact list we reproduce. (We do NOT use ReverseProxy itself.)
```

### Current Codebase tree (the INPUT state of this subtask)

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22; NO requires (T1.S1)
  doc.go            # package comment (T1.S1)
  main.go           # T3.S1 logger + T4.S1 bootstrap (version, healthHandler,     ← THIS SUBTASK EXTENDS IT
                    #   logStartup, passthroughHandler STUB, real main() with
                    #   srv.ListenAndServe guarded by ErrServerClosed)
  health_test.go    # T4.S1: TestHealthHandler*, TestLogStartup,                   ← THIS SUBTASK EDITS TestRouting_HealthzOnly
                    #        TestRouting_HealthzOnly (uses passthroughHandler)
  config.go         # Config/DefaultConfig/LoadConfig (S1) + ResolveConfig (S2)     [T2 — DO NOT EDIT]
  config_test.go    # T2.S1 tests                                                  [T2 — DO NOT EDIT]
  resolve_test.go   # T2.S2 tests                                                  [T2 — DO NOT EDIT]
  logger_test.go    # T3.S1 tests                                                  [T3 — DO NOT EDIT]
  testdata/.gitkeep # placeholder (P1.M3.T2 adds *.sse later)
  PRD.md            # unchanged
  plan/...          # unchanged (READ-ONLY)
# NOTE: no proxy.go yet; main()'s "/" route is the 501 stub. This subtask adds the
# real forwarder and shutdown.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod            # UNCHANGED (zero requires; new imports are stdlib)
  doc.go            # UNCHANGED
  main.go           # MODIFIED — import block += "context","os/signal","syscall";  [THIS]
                    #            += client := newUpstreamClient();                  [THIS]
                    #            mux.HandleFunc("/", newProxyHandler(cfg, log, client))  [THIS] (was stub)
                    #            -= func passthroughHandler  (DELETED, now dead)    [THIS]
                    #            += signal/Shutdown goroutine before ListenAndServe [THIS]
                    #            (version/healthHandler/logStartup/logger UNCHANGED)
  proxy.go          # NEW — hopHeaders, isHopByHop, newUpstreamClient,              [THIS]
                    #        copyForwardHeaders, newProxyHandler, forward
  proxy_test.go     # NEW — TestPassthrough_InitializeSSE / _AcceptFallback /       [THIS]
                    #        _HopByHopStripped / _AuthorizationForwarded / _ResponseHeaders
                    #        (httptest.Server fake upstream; canned SSE from PRD §8)
  health_test.go    # MODIFIED — TestRouting_HealthzOnly rewritten to assert        [THIS]
                    #        /healthz isolation vs a fake upstream (no more stub-501)
  config.go         # UNCHANGED
  config_test.go    # UNCHANGED
  resolve_test.go   # UNCHANGED
  logger_test.go    # UNCHANGED
  testdata/.gitkeep # UNCHANGED (canned SSE is inline in proxy_test.go; P1.M3.T2
                    #                later ships testdata/*.sse golden fixtures)
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL (#1): the hop-by-hop list is the stdlib 9-ENTRY var, NOT PRD §11.2's 8.
// Reproduce net/http/httputil/reverseproxy.go:365 verbatim, INCLUDING
// "Proxy-Connection". Using 9 is a safe superset (strips the de-facto header too).
// "Te" vs "TE" is a non-issue because http.Header canonicalizes keys; compare via
// CanonicalHeaderKey on both sides. hopHeaders is UNEXPORTED in stdlib, so we own
// the copy.

// CRITICAL: clone DefaultTransport with a TYPE ASSERTION — it is declared as
// `var DefaultTransport RoundTripper = &Transport{...}`, so
// `http.DefaultTransport.(*http.Transport).Clone()` is required (a bare
// `.Clone()` on the interface value does not compile). Clone keeps all defaults
// (dial/TLS/h2/idle); only override ResponseHeaderTimeout.

// CRITICAL: do NOT set http.Client.Timeout. It is a whole-exchange deadline that
// INCLUDES reading the response body and would cut off long SSE streams
// (external_deps.md §5). Leave it zero; rely on per-request context cancellation
// (r.Context() -> outReq). ResponseHeaderTimeout=30s is the correct dead-upstream
// knob ("does not include the time to read the response body").

// CRITICAL: pass r.Body as the body io.Reader to NewRequestWithContext so it
// STREAMS to upstream WITHOUT buffering (PRD §6 "minimal overhead; common case
// must not buffer or parse"). P1.M4 will replace this with
// bytes.NewReader(rewritten) for the rewrite path — that is M4's change, not here.

// CRITICAL: set response headers BEFORE WriteHeader. In forward():
//   for ... { rw.Header().Add(k, v) }   // copy non-hop-by-hop resp headers
//   rw.WriteHeader(resp.StatusCode)      // THEN status
//   io.Copy(rw, resp.Body)
// WriteHeader locks the header map; later Header mutations are silently ignored
// (same gotcha as T4.S1's healthHandler).

// CRITICAL: strip hop-by-hop on the RESPONSE side too. If upstream sends
// `Transfer-Encoding: chunked` (common for SSE) and you copy it verbatim, you
// double-frame. isHopByHop("Transfer-Encoding") drops it; the proxy's own server
// then frames the io.Copy'd bytes correctly. Content-Length is NOT hop-by-hop and
// is copied (correct for passthrough — body unchanged; M4 rewrite must clear it).

// CRITICAL: the Accept fallback fires ONLY when the client omitted Accept. If the
// client sends `Accept: application/json`, forward it UNCHANGED (PRD §11.2 "do not
// otherwise modify a client-provided Accept"). Overwriting a client value would
// break clients that intentionally request JSON-only.

// CRITICAL: Authorization is forwarded VERBATIM via copyForwardHeaders (it is not
// hop-by-hop). NEVER read, log, or store its value (PRD §13). If you log request
// headers, pass them through redactHeaders first. The forward path must not touch
// the Authorization string.

// CRITICAL: the upstream request goes to cfg.Upstream regardless of the incoming
// path (PRD §9: "only /healthz is intercepted; everything else forwards"). Do NOT
// splice r.URL.Path into the upstream URL — that would make this an open proxy.
// cfg.Upstream is already the full absolute z.ai URL.

// CRITICAL: do NOT add ReadTimeout/WriteTimeout/IdleTimeout to *http.Server. T4.S1
// built `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` with Addr+Handler
// ONLY; keep it that way — a write deadline truncates streamed SSE (PRD §8/§11.3).

// CRITICAL: graceful shutdown must run ListenAndServe on the MAIN goroutine and
// the signal waiter in a SEPARATE goroutine. Shutdown() causes ListenAndServe to
// return http.ErrServerClosed, which T4.S1 already treats as non-fatal (the
// `err != nil && err != http.ErrServerClosed` guard) — so after Shutdown, main()
// falls through and exits 0. Do NOT swallow the guard.

// GOTCHA: signal.Notify's channel must be buffered (size 1) so a signal sent
// before the receiver is ready is not dropped. Use os.Interrupt (= SIGINT/Ctrl-C
// on POSIX) AND syscall.SIGTERM (for `kill`). Pass sig.String() into the shutdown
// log so the operator sees which signal fired.

// GOTCHA: io.Copy + a single final Flush is sufficient for the canned (complete)
// SSE test fixtures. For live event-by-event streaming a per-chunk flushing copy
// loop is a refinement P1.M4 may add; it is NOT required for the passthrough tests
// (the fixtures are fully written bodies).

// GOTCHA: httptest.Server's client receives the body bytes the upstream handler
// wrote, re-framed by Go's server. Because we strip Transfer-Encoding and io.Copy
// the raw bytes, the CLIENT-visible body is byte-equal to what the fake upstream
// wrote — so a `bytes.Equal` / `strings.Contains` assertion on the SSE body is
// valid and strong (PRD §19.3 case 3 "byte-equal to upstream payload").

// GOTCHA: the canned initialize SSE uses NO space after `data` (PRD §8 note:
// "real wire format has no space after data"). Use `data:{...}` not `data: {...}`
// in the test fixture or the byte-equal assertion can drift.

// SCOPE GUARD: do NOT implement request-side body parsing, tools/call detection,
// alias rewrite, reqID tracking, or SSE warning injection. Those are P1.M2 (rule)
// and P1.M4 (integration). Here the body streams through UNCHANGED and the
// response is io.Copy'd UNCHANGED. forward() is built so M4 can EXTEND the body
// step, not rewrite the whole function.

// SCOPE GUARD: do NOT create testdata/*.sse golden fixtures — that is P1.M3.T2.
// Define the canned initialize SSE inline as a `const` in proxy_test.go (quoted
// from PRD §8). P1.M3.T2 later extracts the shared fixtures.

// SCOPE GUARD: the real-z.ai initialize smoke test needs an API key and is a
// MANUAL step — note it in a comment / the Validation Loop, do NOT automate it
// (httptest e2e vs the live endpoint is P1.M5.T1).

// SCOPE GUARD: do NOT touch config.go / config_test.go / resolve_test.go /
// logger_test.go / doc.go / go.mod. Edit ONLY main.go, health_test.go and CREATE
// proxy.go, proxy_test.go.
```

## Implementation Blueprint

### Data models and structure

No new persistent data. `proxy.go` introduces one package-level header set and
five package-level functions; `main.go` gains one client-construction call, one
registration swap, one stub deletion, and the shutdown goroutine.

```go
// hopHeaders — the exact stdlib hop-by-hop set (net/http/httputil/reverseproxy.go).
// Reproduced verbatim (9 entries). Stripped from both the forwarded request and
// the copied response. external_deps.md §4.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized "TE"
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT (read-only, no edits)
  - RUN: `cd /home/dustin/projects/web-search-prime-fixer && grep -n 'func
    passthroughHandler\|func healthHandler\|func main\|var version\|srv :=
    &http.Server\|ListenAndServe' main.go`
  - EXPECT: main.go contains the T4.S1 bootstrap (version, healthHandler,
    passthroughHandler STUB, real main() building srv and calling ListenAndServe
    guarded by ErrServerClosed). If main.go is STILL the bare T3.S1 logger +
    `func main() {}` (no healthHandler/passthroughHandler), STOP — T4.S1 (hard
    dependency) has not landed; do not proceed.
  - RUN: `grep -n 'func ResolveConfig\|type Config' config.go` → MUST show both.
  - RUN: `go build ./... && go test ./...` → MUST pass (T4.S1 + T2 + T3 landed).
  - RUN: `grep -rn 'passthroughHandler' .` → EXPECT hits in main.go (def +
    registration) AND health_test.go (TestRouting_HealthzOnly). These are the
    swap + edit targets. Note the exact lines for Tasks 2 & 3.
  - WHY: This subtask's INPUT is "main/mux from P1.M1.T4.S1" (the stub + srv),
    plus the T2 config and T3 logger. Confirm all three before editing.

Task 1: CREATE proxy.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/proxy.go
  - PACKAGE: `package main`
  - IMPORTS: `"io"`, `"net/http"`, `"time"` (stdlib only).
  - IMPLEMENT (verbatim, per "Implementation Patterns & Key Details"): `var
    hopHeaders` (9 entries), `func isHopByHop(name string) bool`, `func
    newUpstreamClient() *http.Client`, `func copyForwardHeaders(dst, src
    http.Header)`, `func newProxyHandler(cfg Config, log *logger, client
    *http.Client) http.HandlerFunc`, `func forward(client *http.Client, rw
    http.ResponseWriter, outReq *http.Request, log *logger)`.
  - NAMING: hopHeaders (var), isHopByHop / newUpstreamClient / copyForwardHeaders
    / newProxyHandler / forward (funcs). No exported symbols (package main).
  - PLACEMENT: repo root proxy.go (PRD §9 file layout).
  - WHY: this is the forward core P1.M4 consumes (forward + copyForwardHeaders +
    the request-building inside newProxyHandler are reused; M4 swaps the body and
    extends the response body step). hopHeaders/isHopByHop are shared by request
    and response stripping.

Task 2: MODIFY main.go — client, registration swap, delete stub, shutdown
  - FILE: /home/dustin/projects/web-search-prime-fixer/main.go
  - PRESERVE: the T3.S1 logger block AND T4.S1's version/healthHandler/logStartup
    UNCHANGED.
  - IMPORT BLOCK (add, alphabetical): `"context"`, `"os/signal"`, `"syscall"`.
    (os and time already present from T3.S1/T4.S1.)
  - IN main(): after `logStartup(log, cfg)` and BEFORE building the mux, ADD
    `client := newUpstreamClient()`.
  - SWAP: `mux.HandleFunc("/", passthroughHandler)` -> `mux.HandleFunc("/",
    newProxyHandler(cfg, log, client))`.
  - DELETE: the entire `func passthroughHandler(w http.ResponseWriter, r
    *http.Request) { http.Error(...) }` definition (it is now dead code).
  - ADD: the graceful-shutdown goroutine between the `srv := &http.Server{...}`
    line and `srv.ListenAndServe()` (see Implementation Patterns — signal.Notify
    os.Interrupt+syscall.SIGTERM -> log "shutdown" -> srv.Shutdown(10s ctx)).
  - WHY: this realizes PRD §16 (shutdown) and FR-1 (real forwarder). Deleting the
    stub removes dead code (anti-pattern). The ErrServerClosed guard stays so
    Shutdown is a clean return.

Task 3: MODIFY health_test.go — evolve TestRouting_HealthzOnly (stub is gone)
  - FILE: /home/dustin/projects/web-search-prime-fixer/health_test.go
  - FIND: `func TestRouting_HealthzOnly(t *testing.T)` — it builds an inline mux
    with `mux.HandleFunc("/", passthroughHandler)` and asserts `/mcp` -> 501.
  - REWRITE: keep the test name and the `/healthz`-isolation INTENT, but:
      * spin up a fake `httptest.Server` whose handler increments a `hits` counter
        and returns 200 (any body);
      * build `cfg := DefaultConfig(); cfg.Upstream = upstream.URL`;
      * build the mux as `/healthz` -> healthHandler, `/` -> newProxyHandler(cfg,
        newLogger(io.Discard,"error"), newUpstreamClient());
      * assert GET /healthz returns the health body (ok:true) AND `hits == 0`
        (isolation — PRD §19.3 case 5).
      * drop the `/mcp -> 501` assertions (that coverage now lives in
        proxy_test.go, Task 4).
  - REMOVE any other reference to `passthroughHandler` in this file.
  - WHY: T4.S2 removed the stub; the routing test must not reference it. The
    durable invariant is "/healthz is intercepted and never calls the upstream",
    which survives forever (case 5). This is the ONE necessary cross-item test
    edit; it is minimal and well-defined.

Task 4: CREATE proxy_test.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/proxy_test.go
  - PACKAGE: `package main`
  - IMPORTS: `"bytes"`, `"io"`, `"net/http"`, `"net/http/httptest"`, `"strings"`,
    `"testing"` (stdlib only; mirror logger_test.go's decode-via-Unmarshal style
    where JSON is involved).
  - FIXTURE: a `const initSSE` holding the PRD §8 initialize bytes VERBATIM, with
    NO space after `data` and a trailing blank line:
        id:1\n event:message\n data:{"jsonrpc":"2.0","id":1,"result":{...}}\n \n
    and a const session id e.g. `"11111111-1111-1111-1111-111111111111"`.
  - HARNESS: a helper `fakeUpstream(t)` that returns an `*httptest.Server`
    capturing the received request (method, URL, headers, body) and returning
    `200`, `Content-Type: text/event-stream;charset=UTF-8`,
    `Mcp-Session-Id: <sid>`, body `initSSE`. (This harness is the seed P1.M5.T1.S1
    will formalize.)
  - IMPLEMENT (see Validation Loop Level 2 for bodies):
      TestPassthrough_InitializeSSE: client POSTs initialize to a proxy wired at
        the fake upstream; assert client got status 200, Content-Type contains
        text/event-stream, Mcp-Session-Id == <sid>, and the BODY is byte-equal
        to initSSE (transparent passthrough, PRD §11.3/§19.3 case 3).
      TestPassthrough_AcceptFallback: client omits Accept; assert the fake
        upstream received Accept == "application/json, text/event-stream". Then a
        second case where the client sends Accept: application/json and assert it
        is forwarded UNCHANGED (no overwrite).
      TestPassthrough_HopByHopStripped: client sends Connection + Keep-Alive +
        Transfer-Encoding + Upgrade; assert NONE reached the fake upstream, while
        Authorization/Content-Type/Mcp-Session-Id DID reach it.
      TestPassthrough_AuthorizationForwarded: client sends Authorization: Bearer
        xyz; assert the fake upstream received it verbatim (PRD §13/§19.3 case 4).
      TestPassthrough_ResponseHeadersCopied: assert Content-Type, Mcp-Session-Id,
        Cache-Control, Vary, X-Log-Id (set by the fake upstream) are forwarded to
        the client, and hop-by-hop response headers are NOT.
  - NAMING: TestPassthrough_InitializeSSE, _AcceptFallback, _HopByHopStripped,
    _AuthorizationForwarded, _ResponseHeadersCopied.
  - WHY: covers the contract's MOCKING requirements (canned SSE initialize
    reaches client; Accept fallback) + PRD §11.2/§11.3/§13/§19.3 passthrough
    subset. The rewrite/injection cases (§19.3 cases 1-2) are P1.M4/P1.M5.

Task 5: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w main.go proxy.go health_test.go proxy_test.go
        go build ./...
        go vet ./...
        gofmt -l .
        go test ./...
        go test -run 'TestPassthrough|TestRouting' -v ./...
        grep -rn 'passthroughHandler' .   # MUST print nothing
  - EXPECT: gofmt silent; build/vet clean; all tests PASS; grep empty. Re-run
    after any edit.

Task 6: INTEGRATION SMOKE (Level 3 — graceful shutdown + real forwarding proof)
  - RUN (repo root):
        go build -o /tmp/wspf .
        WSPF_LISTEN=127.0.0.1:18787 /tmp/wspf & PID=$!
        sleep 0.3
        curl -sS -i http://127.0.0.1:18787/healthz        # 200 {"ok":true,...}
        kill -TERM $PID; wait $PID 2>/dev/null; echo "exit=$?"  # expect exit=0
        # (stderr captured) — assert exactly one "shutdown" JSON line with "signal"
  - EXPECT: process exits 0 on SIGTERM; stderr has one `{"...","msg":"shutdown",
    "signal":"terminated"}` line. (If testing SIGINT: kill -INT.)
  - MANUAL (needs API key — do NOT automate): point the proxy at the real z.ai
    upstream, POST a real initialize, confirm the SSE initialize + mcp-session-id
    come back. Document the command in a code comment; the live e2e is P1.M5.T1.
```

### Implementation Patterns & Key Details

```go
// proxy.go — the transparent passthrough forwarder + reusable forward core.

package main

import (
	"io"
	"net/http"
	"time"
)

// hopHeaders is the exact stdlib hop-by-hop set, reproduced verbatim from
// net/http/httputil/reverseproxy.go:365 (external_deps.md §4). It is unexported
// in stdlib, so we own this copy. Stripped from BOTH the forwarded request and
// the copied upstream response. NOTE: this is the 9-ENTRY list (incl
// Proxy-Connection), a safe superset of PRD §11.2's 8-entry summary.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",      // canonicalized version of "TE"
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// isHopByHop reports whether name is a hop-by-hop header. Canonicalizes both
// sides so "TE"/"Te" match identically (http.Header already canonicalizes keys,
// but the request header map may carry non-canonical entries).
func isHopByHop(name string) bool {
	c := http.CanonicalHeaderKey(name)
	for _, h := range hopHeaders {
		if c == http.CanonicalHeaderKey(h) {
			return true
		}
	}
	return false
}

// newUpstreamClient builds the single *http.Client used to forward every request
// to cfg.Upstream (PRD §17). It clones http.DefaultTransport (sensible dial/TLS/
// h2 defaults: dial 30s, TLS 10s, ALPN h2, idle pooling) and sets
// Transport.ResponseHeaderTimeout to 30s so a dead upstream is detected quickly
// WITHOUT bounding the response-body read (verified: "does not include the time
// to read the response body").
//
// CRITICAL: http.Client.Timeout is left ZERO. A non-zero Timeout is a
// whole-exchange deadline that includes reading the body and would cut off long
// SSE streams (external_deps.md §5). Rely on per-request context cancellation.
func newUpstreamClient() *http.Client {
	// DefaultTransport is declared as RoundTripper; assert to *Transport to Clone.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

// copyForwardHeaders copies src's headers into dst EXCEPT the hop-by-hop set.
// Authorization, Content-Type, Accept, Mcp-Session-Id, Accept-Language,
// User-Agent and all other non-hop-by-hop headers pass through VERBATIM
// (PRD §11.2, §13). Authorization is forwarded unchanged and is never read here.
func copyForwardHeaders(dst, src http.Header) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// newProxyHandler returns the HTTP handler that transparently forwards every
// non-/healthz request to cfg.Upstream (PRD §9, §11.2). The client body streams
// through UNCHANGED (no buffering — PRD §6); headers are copied minus
// hop-by-hop; the Accept fallback is applied; the client context propagates
// upstream (disconnect cancellation). forward() writes the response.
//
// P1.M4.T1 EXTENDS this factory: it will read the body into bytes, detect
// tools/call, apply the alias rewrite, re-serialize, and set reqID — replacing
// the streamed r.Body with rewritten bytes and tracking ids. P1.M4.T2.S2
// EXTENDS forward() with a conditional SSE-injection response path.
func newProxyHandler(cfg Config, log *logger, client *http.Client) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		// NewRequestWithContext(r.Context(), ..., r.Body) streams r.Body to
		// upstream without buffering AND sets the context in one step (≡ the
		// contract's outReq.WithContext(r.Context())). cfg.Upstream is the full
		// absolute z.ai URL; the incoming path is NOT spliced in (PRD §9).
		outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.Upstream, r.Body)
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"bad upstream request"}`, http.StatusBadGateway)
			return
		}
		copyForwardHeaders(outReq.Header, r.Header)
		// Accept fallback (PRD §8): the text/event-stream token is REQUIRED or
		// z.ai returns empty. ONLY default when the client omitted Accept; a
		// client-provided Accept is passed through unmodified (PRD §11.2).
		if outReq.Header.Get("Accept") == "" {
			outReq.Header.Set("Accept", "application/json, text/event-stream")
		}
		forward(client, rw, outReq, log)
	}
}

// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.3 passthrough path). It is the REUSABLE FORWARD CORE: copy status +
// non-hop-by-hop response headers, io.Copy the body verbatim, flush for SSE.
//
// P1.M4.T2.S2 will EXTEND the body step: io.Copy (passthrough) UNLESS the
// request was rewritten, in which case the body is fed through the SSE injector
// keyed on reqID. The request has already been built by the caller (T4.S2
// streams r.Body; M4 passes rewritten bytes), so forward is body-source agnostic.
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	// Copy non-hop-by-hop response headers BEFORE WriteHeader (WriteHeader locks
	// the header map). Content-Type, Mcp-Session-Id, Cache-Control, Vary, X-Log-Id
	// pass through; Transfer-Encoding/Connection etc. are stripped (hop-by-hop).
	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	// io.Copy streams the body without whole-body buffering (PRD §11.3/§6). A
	// copy error here (client disconnect mid-stream) is logged at warn, not fatal.
	if _, err := io.Copy(rw, resp.Body); err != nil {
		log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
	}
	// Flush so SSE events reach the client immediately (external_deps.md §2).
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
}
```

```go
// main.go — the ADDITIONS to the T4.S1 bootstrap (the logger/health/version
// symbols above are UNCHANGED). Only the import block gains context/os/signal/
// syscall, and main() gains the client build, the registration swap, the stub
// deletion, and the shutdown goroutine.
//
// === IMPORT BLOCK (final, after edit) ===
// import (
// 	"context"
// 	"encoding/json"
// 	"io"
// 	"net/http"
// 	"os"
// 	"os/signal"
// 	"syscall"
// 	"time"
// )

// func main() — the diff vs T4.S1 (ResolveConfig/newLogger/logStartup unchanged):
func main() {
	cfg, err := ResolveConfig()
	if err != nil {
		newLogger(os.Stderr, "error").log("error", "config", map[string]any{"err": err.Error()})
		os.Exit(1)
	}
	log := newLogger(os.Stderr, cfg.LogLevel)
	logStartup(log, cfg)

	// T4.S2: build the shared upstream client ONCE (PRD §17).
	client := newUpstreamClient()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	// T4.S2: real transparent forwarder (was the passthroughHandler 501 stub).
	mux.HandleFunc("/", newProxyHandler(cfg, log, client))

	// CRITICAL: Addr+Handler ONLY — no timeouts (a write deadline truncates SSE).
	srv := &http.Server{Addr: cfg.Listen, Handler: mux}

	// T4.S2: graceful shutdown (PRD §16). Runs in its own goroutine so
	// ListenAndServe stays on the main goroutine. SIGINT (Ctrl-C) / SIGTERM
	// (kill) -> log one "shutdown" line -> Shutdown drains within 10s ->
	// ListenAndServe returns http.ErrServerClosed -> main falls through to exit 0.
	go func() {
		sigCh := make(chan os.Signal, 1) // buffered: don't drop a fast signal
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		sig := <-sigCh
		log.log("info", "shutdown", map[string]any{"signal": sig.String()})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.log("error", "listen", map[string]any{"err": err.Error(), "listen": cfg.Listen})
		os.Exit(1)
	}
	// ErrServerClosed (from Shutdown) falls through here -> clean exit 0.
}
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"   # proxy.go, proxy_test.go, health_test.go, main.go all package main

SYMBOLS INTRODUCED (consumed by later subtasks):
  - var hopHeaders                                              # -> isHopByHop (request+response strip)
  - func isHopByHop(name string) bool                           # -> copyForwardHeaders + forward response copy
  - func newUpstreamClient() *http.Client                       # -> built once in main; passed to newProxyHandler
  - func copyForwardHeaders(dst, src http.Header)               # -> newProxyHandler; REUSED by P1.M4 (request building)
  - func newProxyHandler(cfg Config, log *logger, client *http.Client) http.HandlerFunc
        # -> registered at mux.HandleFunc("/", ...) in main();
        #    EXTENDED by P1.M4.T1 (body parse/rewrite/reqID).
  - func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, log *logger)
        # -> called by newProxyHandler; EXTENDED by P1.M4.T2.S2 (conditional SSE injection).

SYMBOLS REMOVED:
  - func passthroughHandler   # T4.S1's 501 stub — deleted (replaced by newProxyHandler).

CONSUMED FROM PRIOR SUBTASKS (read-only, already on disk / fixed by contracts):
  - func ResolveConfig() (Config, error)        # P1.M1.T2.S2 (config.go)
  - type Config{ Upstream, Listen, Path string; Aliases []string; TargetParam, LogLevel string }  # P1.M1.T2
  - func DefaultConfig() Config                  # used by health_test.go/proxy_test.go (P1.M1.T2.S1)
  - func newLogger(w io.Writer, level string) *logger              # P1.M1.T3.S1 (main.go)
  - func (l *logger).log(level, msg string, fields map[string]any) # P1.M1.T3.S1
  - func healthHandler(w http.ResponseWriter, r *http.Request)     # P1.M1.T4.S1 (main.go) — UNCHANGED
  - func logStartup(l *logger, cfg Config)                         # P1.M1.T4.S1 — UNCHANGED
  - var version                                                    # P1.M1.T4.S1 — UNCHANGED
  - srv := &http.Server{Addr, Handler} + ErrServerClosed guard    # P1.M1.T4.S1 — Shutdown target

HANDOFF TO P1.M4 (the forward-core reuse seam):
  - P1.M4.T1 (request side): in newProxyHandler, replace `r.Body` with bytes
        read from r.Body, parse JSON, detect tools/call, call Rewrite, re-serialize,
        build outReq with bytes.NewReader(rewritten), set reqID. copyForwardHeaders
        and the Accept fallback are REUSED unchanged.
  - P1.M4.T2.S1: the request building is ALREADY this subtask's newProxyHandler
        minus the body/rewrite — M4 reuses copyForwardHeaders + Accept fallback +
        ctx verbatim.
  - P1.M4.T2.S2 (response side): in forward, branch the body step — io.Copy
        (passthrough, unchanged) vs SSE injector (when reqID matches a rewritten
        request). The status + response-header copy + flush are REUSED unchanged.
  - P1.M4.T3: log "rewrite" + "debug forward" events from newProxyHandler/forward.

HANDOFF TO P1.M5 (the test-harness seam):
  - P1.M5.T1.S1 formalizes the fakeUpstream harness seeded here in proxy_test.go
        into the shared proxy_test.go e2e suite, adding golden fixtures
        (testdata/*.sse from P1.M3.T2). The canned initSSE const here is the seed.

NO NEW ENV VARS / NO go.mod CHANGES / NO CONFIG SCHEMA CHANGES:
  - New imports are stdlib only: context, os/signal, syscall (main.go); io,
    net/http, time (proxy.go). go.mod gains zero requires.
  - [Mode A] DOCS: no README/config.example.json/doc.go changes (those are
    P1.M5.T3). Inline doc comments on every new symbol are the documentation.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -w main.go proxy.go health_test.go proxy_test.go   # format in place
gofmt -l .                                                # MUST print nothing
go vet ./...                                              # MUST exit 0, no output
go build ./...                                            # MUST exit 0, no output

# Dependency-free invariant (PRD §6/§9) — grep for stray third-party / stdlib-log imports:
grep -nE '^\s*"github\.com|^\s*"gopkg\.in|^\s*"golang\.org/x|"log"|"log/slog"|gorilla|cobra|httputil' \
  main.go proxy.go proxy_test.go health_test.go || true
# Expected: empty. proxy.go imports io/net/http/time; main.go adds context/os/os-signal/syscall.

# Confirm the stub is fully gone (Task 2 + Task 3):
grep -rn 'passthroughHandler' .   # MUST print nothing
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# The new passthrough suite + the evolved routing test:
go test -run 'TestPassthrough|TestRouting|TestHealthHandler|TestLogStartup' -v ./...

# Full suite (config + logger + health + proxy):
go test ./...

# Expected: all PASS. If TestRouting_HealthzOnly fails to compile, you missed a
# passthroughHandler reference in health_test.go (Task 3) — grep and remove it.
```

`proxy_test.go` skeleton (the fake-upstream harness + the five cases):

```go
package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Canned initialize SSE — PRD §8 wire format, NO space after `data`, trailing
// blank line dispatches the event. (P1.M3.T2 later extracts shared fixtures.)
const initSSE = "id:1\nevent:message\n" +
	`data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",` +
	`"capabilities":{"logging":{},"tools":{"listChanged":true}},` +
	`"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}` + "\n\n"

const testSID = "11111111-1111-1111-1111-111111111111"

// fakeUpstream returns a server that records the last request it received and
// replies with the canned initialize SSE + mcp-session-id. close with defer.
func fakeUpstream(t *testing.T, got *http.Request) *httptest.Server {
	t.Helper()
	hits := 0
	_ = hits
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*got = *r // shallow copy for assertions
		hits++
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, initSSE)
	}))
}

// (1) Transparent passthrough: client receives the SSE body byte-for-byte + the
// session header, status 200, Content-Type text/event-stream.
func TestPassthrough_InitializeSSE(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	cfg := DefaultConfig()
	cfg.Upstream = up.URL
	proxy := httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard, "error"), newUpstreamClient()))
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { t.Fatalf("status = %d, want 200", resp.StatusCode) }
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want it to contain text/event-stream", ct)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != testSID {
		t.Errorf("Mcp-Session-Id = %q, want %q", sid, testSID)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != initSSE {
		t.Errorf("body not byte-equal to upstream SSE:\n got %q\nwant %q", body, initSSE)
	}
}

// (2) Accept fallback when omitted; passthrough when provided.
func TestPassthrough_AcceptFallback(t *testing.T) {
	// omitted -> upstream gets the default
	var got http.Request
	up := fakeUpstream(t, &got); defer up.Close()
	cfg := DefaultConfig(); cfg.Upstream = up.URL
	proxy := httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard,"error"), newUpstreamClient()))
	defer proxy.Close()
	resp, _ := http.Post(proxy.URL, "", strings.NewReader(`{}`)); defer resp.Body.Close()
	if a := got.Header.Get("Accept"); a != "application/json, text/event-stream" {
		t.Errorf("omitted Accept: upstream got %q, want the default", a)
	}
	// provided -> forwarded unchanged
	var got2 http.Request
	up2 := fakeUpstream(t, &got2); defer up2.Close()
	cfg2 := DefaultConfig(); cfg2.Upstream = up2.URL
	proxy2 := httptest.NewServer(newProxyHandler(cfg2, newLogger(io.Discard,"error"), newUpstreamClient()))
	defer proxy2.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy2.URL, strings.NewReader(`{}`))
	req.Header.Set("Accept", "application/json")
	resp2, _ := http.DefaultClient.Do(req); defer resp2.Body.Close()
	if a := got2.Header.Get("Accept"); a != "application/json" {
		t.Errorf("provided Accept: upstream got %q, want application/json (unchanged)", a)
	}
}

// (3) Hop-by-hop request headers stripped; sensitive/useful ones kept.
func TestPassthrough_HopByHopStripped(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got); defer up.Close()
	cfg := DefaultConfig(); cfg.Upstream = up.URL
	proxy := httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard,"error"), newUpstreamClient()))
	defer proxy.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{}`))
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Keep-Alive", "timeout=5")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Upgrade", "h2c")
	req.Header.Set("Authorization", "Bearer xyz")
	req.Header.Set("Mcp-Session-Id", testSID)
	resp, _ := http.DefaultClient.Do(req); defer resp.Body.Close()
	for _, h := range []string{"Connection","Keep-Alive","Transfer-Encoding","Upgrade"} {
		if got.Header.Get(h) != "" { t.Errorf("hop-by-hop %q reached upstream", h) }
	}
	if got.Header.Get("Authorization") != "Bearer xyz" { t.Error("Authorization not forwarded") }
	if got.Header.Get("Mcp-Session-Id") != testSID { t.Error("Mcp-Session-Id not forwarded") }
}

// (4) Authorization is forwarded verbatim to the upstream (PRD §13/§19.3 case 4).
func TestPassthrough_AuthorizationForwarded(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got); defer up.Close()
	cfg := DefaultConfig(); cfg.Upstream = up.URL
	proxy := httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard,"error"), newUpstreamClient()))
	defer proxy.Close()
	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer hunter2-token")
	resp, _ := http.DefaultClient.Do(req); defer resp.Body.Close()
	if got := got.Header.Get("Authorization"); got != "Bearer hunter2-token" {
		t.Errorf("upstream Authorization = %q, want it forwarded verbatim", got)
	}
}

// (5) Non-hop-by-hop response headers forwarded; hop-by-hop response headers dropped.
func TestPassthrough_ResponseHeadersCopied(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Log-Id", "log-7")
		w.Header().Set("Connection", "keep-alive") // hop-by-hop -> must NOT reach client
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, initSSE)
	}))
	defer up.Close()
	cfg := DefaultConfig(); cfg.Upstream = up.URL
	proxy := httptest.NewServer(newProxyHandler(cfg, newLogger(io.Discard,"error"), newUpstreamClient()))
	defer proxy.Close()
	resp, _ := http.Post(proxy.URL, "application/json", strings.NewReader(`{}`))
	defer resp.Body.Close()
	if resp.Header.Get("Mcp-Session-Id") != testSID { t.Error("Mcp-Session-Id not copied") }
	if resp.Header.Get("Cache-Control") != "no-cache" { t.Error("Cache-Control not copied") }
	if resp.Header.Get("X-Log-Id") != "log-7" { t.Error("X-Log-Id not copied") }
	if resp.Header.Get("Connection") != "" { t.Error("hop-by-hop Connection leaked to client") }
}
```

`health_test.go` evolved `TestRouting_HealthzOnly` (replace the stub-based version):

```go
// (routing) /healthz is intercepted and NEVER calls the upstream (PRD §9/§16/
// §19.3 case 5). The "/mcp forwards" half is covered by proxy_test.go.
func TestRouting_HealthzOnly(t *testing.T) {
	hits := 0
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()
	cfg := DefaultConfig(); cfg.Upstream = up.URL
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", newProxyHandler(cfg, newLogger(io.Discard, "error"), newUpstreamClient()))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil { t.Fatal(err) }
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK { t.Fatalf("status = %d, want 200", resp.StatusCode) }
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil { t.Fatal(err) }
	if body["ok"] != true { t.Errorf("ok = %#v, want true", body["ok"]) }
	if hits != 0 { t.Errorf("upstream hit %d times for /healthz, want 0 (isolation)", hits) }
}
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go build -o /tmp/wspf .

# Graceful shutdown proof (PRD §16):
WSPF_LISTEN=127.0.0.1:18787 /tmp/wspf 2>/tmp/wspf.stderr & PID=$!
sleep 0.3
curl -sS -i http://127.0.0.1:18787/healthz        # 200 {"ok":true,"version":"dev"}
kill -TERM $PID
wait $PID 2>/dev/null; echo "exit=$?"              # expect exit=0
grep -c '"msg":"shutdown"' /tmp/wspf.stderr        # expect 1
grep '"signal"' /tmp/wspf.stderr >/dev/null && echo "signal field present"
rm -f /tmp/wspf /tmp/wspf.stderr

# End-to-end forward proof (proxy <-> fake upstream via the real binary):
# (Optional; proxy_test.go already covers this in-process. The manual real-z.ai
# smoke is documented but NOT automated here — needs an API key; live e2e is P1.M5.)
```

```bash
# MANUAL (needs API key) — real z.ai initialize smoke (documented, NOT automated).
# Set WSPF_UPSTREAM to the live endpoint (default already is), run the proxy, then:
#   curl -sS -i -X POST http://127.0.0.1:8787/mcp \
#     -H 'Content-Type: application/json' \
#     -H 'Accept: application/json, text/event-stream' \
#     -H "Authorization: Bearer $WSPF_KEY" \
#     -d '{"jsonrpc":"2.0","method":"initialize","id":1}'
# Expect: 200, content-type text/event-stream, an mcp-session-id header, and the
# initialize SSE body per PRD §8. (P1.M5.T1 automates this against a fake upstream.)
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Hop-by-hop correctness spot-check (read the source you reproduce):
diff <(sed -n '/^var hopHeaders = \[\]string{/,/^}/p' proxy.go) \
     <(sed -n '/^var hopHeaders = \[\]string{/,/^}/p' /usr/lib/go/src/net/http/httputil/reverseproxy.go)
# Expected: identical (the 9 entries). Any diff = you drifted from the stdlib list.

# Streaming-without-buffering reasoning check (PRD §6): confirm forward() calls
# io.Copy on resp.Body directly (no ioutil.ReadAll / io.ReadAll before writing):
grep -n 'io.ReadAll\|ioutil.ReadAll' proxy.go || echo "OK: no whole-body buffering"

# Shutdown-drain reasoning check: signal goroutine + Shutdown + main-goroutine
# ListenAndServe are all present and distinct:
grep -n 'signal.Notify\|srv.Shutdown\|srv.ListenAndServe' main.go
```

## Final Validation Checklist

### Technical Validation

- [ ] All 4 validation levels completed successfully.
- [ ] `go build ./...` exits 0.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` all PASS.
- [ ] `grep -rn 'passthroughHandler' .` prints nothing (stub fully removed).

### Feature Validation

- [ ] Canned SSE `initialize` reaches the client byte-for-byte, with `mcp-session-id`
      and `text/event-stream` Content-Type intact (TestPassthrough_InitializeSSE).
- [ ] Accept fallback fires only when omitted; a client `Accept` is forwarded
      unchanged (TestPassthrough_AcceptFallback).
- [ ] Hop-by-hop request headers stripped; Authorization/Mcp-Session-Id forwarded
      (TestPassthrough_HopByHopStripped, TestPassthrough_AuthorizationForwarded).
- [ ] Non-hop-by-hop response headers copied; hop-by-hop response headers dropped
      (TestPassthrough_ResponseHeadersCopied).
- [ ] `/healthz` isolated — upstream hit counter stays 0 (TestRouting_HealthzOnly).
- [ ] `SIGTERM` → clean exit 0 with exactly one `shutdown` JSON line carrying
      `signal` (Level 3 smoke).
- [ ] Real-z.ai initialize smoke documented as a MANUAL step (not automated).

### Code Quality Validation

- [ ] `proxy.go` follows the existing `package main` + gofmt style; no exported
      symbols; doc comments on every symbol.
- [ ] File placement matches the desired tree (proxy.go, proxy_test.go at repo root).
- [ ] `forward` is structured so P1.M4 can EXTEND the body step (not rewrite it);
      `copyForwardHeaders` + Accept fallback + response-header copy are reusable.
- [ ] `http.Client.Timeout` is zero; `ResponseHeaderTimeout=30s`; no `*http.Server`
      timeouts (SSE-safe).
- [ ] Authorization never read/logged/stored; only forwarded via copyForwardHeaders.
- [ ] `go.mod` gains zero `require`s; only stdlib imports added.

### Documentation & Deployment

- [ ] Inline doc comments on hopHeaders, isHopByHop, newUpstreamClient,
      copyForwardHeaders, newProxyHandler, forward (including the P1.M4 extension notes).
- [ ] The shutdown log event and the manual real-z.ai smoke are documented.
- [ ] No new env vars; no config schema change.

---

## Anti-Patterns to Avoid

- ❌ Don't use `httputil.ReverseProxy` — external_deps.md §4 proves it cannot cleanly
  express request-side body mutation or conditional SSE injection; a manual handler
  is the chosen design.
- ❌ Don't set `http.Client.Timeout` (cuts SSE) or `*http.Server` write timeouts
  (truncates the SSE stream).
- ❌ Don't buffer the whole request body in T4.S2 (`io.ReadAll`) — stream `r.Body`
  via `NewRequestWithContext`; whole-body read is P1.M4's rewrite path only.
- ❌ Don't overwrite a client-provided `Accept` — only default when it is absent.
- ❌ Don't read/log/store `Authorization` — forward it verbatim via copyForwardHeaders.
- ❌ Don't splice `r.URL.Path` into `cfg.Upstream` — forward to the fixed upstream
  URL (PRD §9; otherwise this becomes an open proxy).
- ❌ Don't leave `passthroughHandler` as dead code — delete it when you swap the
  registration, and remove every reference (grep must be empty).
- ❌ Don't run `ListenAndServe` in a goroutine and `Shutdown` on the main one —
  keep ListenAndServe on the main goroutine and the signal waiter in the goroutine
  so process exit ordering is deterministic.
- ❌ Don't catch `io.Copy` errors as fatal — a mid-stream client disconnect is
  expected (context cancellation); log at `warn` and return.
- ❌ Don't create `testdata/*.sse` fixtures here — that is P1.M3.T2; define the
  canned SSE inline in `proxy_test.go`.
- ❌ Don't implement request parsing / alias rewrite / SSE injection — those are
  P1.M2 and P1.M4; `forward`/`newProxyHandler` are built to be EXTENDED, not rewritten.
