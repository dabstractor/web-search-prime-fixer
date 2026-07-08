# Research — mount SDK StreamableHTTPHandler, auth middleware, v1 deletion (T2.S2)

## Question
P1.M1.T2.S2 rewrites `main()` to (1) build an MCP SDK server + Streamable HTTP
handler, (2) wrap it in auth-extraction middleware that puts the inbound
`Authorization` header into the request context, (3) mount `/healthz` + `/` on the
mux, keeping graceful shutdown; then (4) DELETE the v1 `proxy.go`/`sse.go`/
`rewrite.go` and six matching test files. This note verifies the SDK API surface
on-disk, characterizes exactly what stays red after the deletion, and pins the
context-key middleware pattern.

## Method
Read the cached SDK source at
`/home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`
(file:line citations) + grepped the surviving test files for references to the
deleted v1 symbols. No network needed (SDK cached).

## Results

### 1. SDK API surface — VERIFIED on-disk (go-sdk v1.6.1)
| Symbol | Location | Signature |
|--------|----------|-----------|
| `NewServer` | `server.go:157` | `func NewServer(impl *Implementation, options *ServerOptions) *Server` |
| `NewStreamableHTTPHandler` | `streamable.go:194` | `func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler` |
| `(*StreamableHTTPHandler).ServeHTTP` | `streamable.go:255` | `func (h *StreamableHTTPHandler) ServeHTTP(w http.ResponseWriter, req *http.Request)` |
| `type Implementation struct` | `protocol.go:1507` | fields `Name string`, `Title string`, `Version string`, `WebsiteURL string`, `Icons []Icon` |
| `type StreamableHTTPOptions struct` | `streamable.go:127` | fields incl. `Stateless`, `JSONResponse`, `Logger`, `EventStore`, `CrossOriginProtection` |

**Conclusions:**
- `*StreamableHTTPHandler` IS an `http.Handler` (it has `ServeHTTP(http.ResponseWriter, *http.Request)`) → it can be passed straight to `mux.Handle("/", sdkHandler)` / wrapped by middleware returning `http.Handler`. No adapter needed.
- `NewServer(impl, nil)` — pass `nil` for `*ServerOptions` (defaults). `impl` must be non-nil. We pass `&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}` (only `Name`+`Version` needed; `version` is the package var from `health.go`).
- `NewStreamableHTTPHandler(getServer, nil)` — pass `nil` for options (defaults: stateful sessions, SSE responses — exactly FR-1). `getServer func(*http.Request) *mcp.Server` is called per request; returning the SAME `*Server` every time is explicitly supported by the API doc.
- With **zero `AddTool` calls**, the SDK still answers `initialize` + `ping` + `notifications/*` + `resources/list`(empty) + `prompts/list`(empty) + `logging/setLevel` + `completion/complete` automatically, and `tools/list` returns an **empty** list. This is the milestone's expected behavior ("empty list since no tools") — no tools are required for `go run` to boot and serve.

### 2. Context propagation for Authorization — VERIFIED at `streamable.go:491-493`
```go
// Pass req.Context() here, to allow middleware to add context values.
// The context is detached in the jsonrpc2 library when handling the
// long-running stream.
session, err := server.Connect(req.Context(), transport, connectOpts)
```
**Conclusion:** The SDK's `ServeHTTP` passes `req.Context()` into `server.Connect`,
so values a middleware injects via `context.WithValue` on the inbound request
propagate through to the tool handler's context. This is the verified basis for
the auth-extraction middleware: read `r.Header.Get("Authorization")`, store it
under an unexported context-key type, then `h.ServeHTTP(w, r.WithContext(ctx))`.
(P1.M4.T1.S2 later reads it back to thread onto the outbound z.ai request.)

### 3. Auth middleware / context-key pattern
Idiomatic Go context key = an **unexported struct type** (zero-cost, collision-free
across packages):
```go
type authHeaderKey struct{}   // unexported; package-private

func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
```
- `r.Header.Get("Authorization")` returns `""` when absent — storing an empty
  string is harmless (downstream only forwards a non-empty value).
- Read-back (P1.M4.T1.S2): `v, _ := r.Context().Value(authHeaderKey{}).(string)`.
- We **never log** the value (PRD §17). The value is only forwarded verbatim to z.ai.

### 4. Breakage map after the v1 deletion (grep of surviving test files)
Deleted: `proxy.go`, `sse.go`, `rewrite.go` + `proxy_test.go`, `proxy_e2e_test.go`,
`proxy_harness_test.go`, `proxy_log_test.go`, `sse_test.go`, `rewrite_test.go`.

Surviving test files and their state post-T2.S2:
| File | References to deleted/v1 symbols | State |
|------|----------------------------------|-------|
| `config_test.go` | `def.Aliases` (L37-38), `partial.Aliases` (L68) — v1 `Config.Aliases` no longer exists | **RED** (T3.S1) |
| `resolve_test.go` | `cfg.Aliases`, `DefaultConfig().Aliases` (L212,220,221) | **RED** (T3.S1) |
| `health_test.go` | `newProxyHandler`+`newUpstreamClient` (L151), `cfg.Aliases`+logStartup `aliases` assertions (L114-115) | **RED** (T3.S1) |
| `logger_test.go` | only `newLogger` (survives in `logger.go`) — NO proxy/sse/rewrite/`Aliases` refs | **GREEN** |

**Conclusion:** After T2.S2, `go build ./...` is **GREEN** (production files:
`config.go`, `logger.go`, `health.go`, `main.go`, `doc.go` all compile; deleted
files gone). `go vet ./...` and `go test ./...` stay **RED** on exactly
`config_test.go`/`resolve_test.go`/`health_test.go` — which is **P1.M1.T3.S1's**
explicit scope ("Rewrite config_test.go, resolve_test.go, and health_test.go for
v2 schema"). `logger_test.go` compiles and passes untouched. **`testdata/` is NOT
deleted** (reused by `upstream_test.go` later — system_context.md).

### 5. Build semantics — `go build` does NOT compile `_test.go`
`go build ./...` builds each package's non-test `.go` files only. `_test.go` files
are compiled by `go vet`/`go test`. Therefore `go build ./...` being GREEN is a
clean gate for T2.S2 even while the surviving v1-schema test files are still red
(unlike T2.S1, which needed isolated temp-module gates because production `main.go`
itself referenced deleted symbols). T2.S2 can use `go build ./...` directly.

### 6. SDK localhost / DNS-rebinding protection (FYI, does NOT block this milestone)
`(*StreamableHTTPHandler).ServeHTTP` (`streamable.go:255+`) auto-enables DNS
rebinding protection for localhost servers unless `opts.DisableLocalhostProtection`
is set or `disablelocalhostprotection == "1"`. Since we bind `127.0.0.1` (PRD §17)
and the default is fine for a loopback server, we leave `StreamableHTTPOptions` as
`nil`. This only affects the SDK handler path (POST to `/`), never `/healthz` (our
own handler). The full initialize/tools-list round-trip is exercised by
`server_test.go` in P1.M5.T3; T2.S2 only verifies `go build` + boot + `/healthz`.

## Conclusions for the implementation
1. `main()` builds `server := mcp.NewServer(&mcp.Implementation{Name:"web-search-prime-fixer", Version: version}, nil)`, then `sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request)*mcp.Server{ return server }, nil)`, wraps it in `authMiddleware`, and mounts `/healthz`→`healthHandler`, `/`→`authMiddleware(sdkHandler)`. Graceful-shutdown goroutine unchanged.
2. `main.go` import block = T2.S1's six stdlib imports PLUS `github.com/modelcontextprotocol/go-sdk/mcp`. `version` comes from `health.go` (same package).
3. Define `type authHeaderKey struct{}` (unexported) + `authMiddleware(http.Handler) http.Handler` in `main.go`.
4. Delete the three v1 sources + six v1 test files (exact list above). Keep `testdata/`, `config_test.go`/`resolve_test.go`/`health_test.go`/`logger_test.go`, `doc.go`, `config.go`, `logger.go`, `health.go`, `go.mod`, `go.sum`.
5. Gate on `go build ./...` GREEN (not vet/test — those stay red on the T3.S1 test files). Runtime smoke: `go run . &` + `curl /healthz` → `200 {"ok":true,"version":"dev"}`.
6. Mode A doc comment on the handler mount: "the SDK owns MCP transport framing — initialize, Mcp-Session-Id lifecycle, SSE framing, JSON-RPC dispatch, tools/list, tools/call (PRD §13, FR-1); we supply tool handlers later via AddTool."

## Go SDK references (canonical; verified on-disk above)
- `mcp.NewServer`: server.go:157 — `func NewServer(impl *Implementation, options *ServerOptions) *Server`
- `mcp.NewStreamableHTTPHandler`: streamable.go:194 — returns `*StreamableHTTPHandler`
- `*StreamableHTTPHandler` implements `http.Handler` (ServeHTTP at streamable.go:255)
- `mcp.Implementation{Name,Title,Version,WebsiteURL,Icons}` — protocol.go:1507
- Context threading: streamable.go:491-493 (`server.Connect(req.Context(), …)`)
- Go context keys: https://go.dev/blog/context — "use an unexported type to avoid collisions"
