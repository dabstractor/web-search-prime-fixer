# PRP — P1.M1.T2.S2: Mount SDK StreamableHTTPHandler in main.go; delete v1 proxy/sse/rewrite + tests

## Goal

**Feature Goal**: Rewrite `main()` so the v2 normalizing MCP server boots on the
official Go MCP SDK: build an `mcp.Server`, mount its `mcp.NewStreamableHTTPHandler`
behind an auth-extraction middleware that threads the inbound `Authorization`
header into the request context (for later upstream forwarding), route `/healthz`
to the local health handler and everything else (`/`) to the wrapped SDK handler,
and keep the existing graceful-shutdown path — then **delete** the entire v1
proxy core (`proxy.go`, `sse.go`, `rewrite.go`) and its six test files. No tools
are registered yet (`AddTool` arrives in P1.M5.T2); the SDK answers
`initialize`/`ping`/`notifications/*`/`resources/list`/`prompts/list`/`tools/list`
(empty list) on its own. The package's production code (`go build ./...`) becomes
green for the first time since the v2 pivot began.

**Deliverable**: Two changes at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), all `package main`:
1. **EDIT** `main.go` — add the `github.com/modelcontextprotocol/go-sdk/mcp`
   import; add an unexported `type authHeaderKey struct{}` and an
   `authMiddleware(http.Handler) http.Handler`; **rewrite `main()`'s body** to
   construct the SDK server + handler, wrap it in `authMiddleware`, and mount
   `/healthz` + `/`. Keep `logStartup` (T2.S1's v2 version) and the graceful-
   shutdown goroutine. Add the `[Mode A]` doc comment on the handler mount.
2. **DELETE** nine files: `proxy.go`, `sse.go`, `rewrite.go` and `proxy_test.go`,
   `proxy_e2e_test.go`, `proxy_harness_test.go`, `proxy_log_test.go`, `sse_test.go`,
   `rewrite_test.go`.

**Success Definition**: `go build ./...` exits 0 (production code green); the binary
boots (`go run .`); `GET /healthz` returns `200 {"ok":true,"version":"dev"}`. The
nine v1 files are gone. `go vet`/`go test ./...` may still be red **only** on the
surviving v1-schema test files (`config_test.go`, `resolve_test.go`,
`health_test.go`) — that is explicitly P1.M1.T3.S1's scope; `logger_test.go` stays
green. No tool is registered; no other file (`config.go`, `logger.go`, `health.go`,
`doc.go`, `go.mod`, `go.sum`, `testdata/`) is modified or deleted.

## Why

- This is the **keystone** of the v1→v2 pivot: it replaces the byte-forwarding
  transparent proxy with an SDK-backed MCP server that *owns* the JSON-RPC surface
  (PRD §5.2, §13, FR-1). Every later subtask (`extract.go`, `teach.go`,
  `upstream.go`, `tools.go`/`server.go`) plugs into the `*mcp.Server` created here.
- The auth-extraction middleware is the **first half of PRD §17 / FR-7** ("Forward
  `Authorization` verbatim to z.ai; never read, log, or store it"): it captures the
  inbound credential into the request context WITHOUT ever logging or storing it in
  `Config`. P1.M4.T1.S2 reads it back from context to inject it onto the outbound
  z.ai request. Threading it via context is required because the SDK calls the tool
  handler with `req.Context()` (verified: `streamable.go:491-493`).
- Deleting the v1 core removes the dead hand-rolled SSE framer, the byte proxy, and
  the narrow alias-renamer — the exact code that carried the bug classes the pivot
  retires (SSE multi-line `data:` rejoining, the >64 KiB scanner limit,
  `Mcp-Session-Id` lifecycle, HTML escaping). Keeping them around would block a
  clean `go build` and invite accidental reuse.
- Establishes the **runtime contract** the rest of M1 builds on: a bootable binary
  that serves `/healthz` locally and the SDK handler on `/`, so M2/M3/M4 can add
  logic above a known-good foundation.

## What

`main()` keeps the existing bootstrap spine — `ResolveConfig()` (fail-fast on
error), `newLogger(os.Stderr, cfg.LogLevel)`, `logStartup(log, cfg)` — then,
**instead of** the v1 `newUpstreamClient()` + `newProxyHandler(...)`, it:
1. Creates `server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)` (`version` is the package var from `health.go`).
2. Creates `sdkHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)` — the SDK owns MCP transport framing (initialize / `Mcp-Session-Id` / SSE / JSON-RPC dispatch / `tools/list` / `tools/call`).
3. Wraps it: `authMiddleware(sdkHandler)` — an `http.HandlerFunc` that does `ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))` then `h.ServeHTTP(w, r.WithContext(ctx))`.
4. Mounts: `mux.HandleFunc("/healthz", healthHandler)` and `mux.Handle("/", authMiddleware(sdkHandler))`.
5. Keeps `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` (NO timeouts — would truncate SSE), the SIGINT/SIGTERM graceful-shutdown goroutine, and the `ListenAndServe`/`ErrServerClosed` tail unchanged.

Then the nine v1 files are removed. `testdata/` is **kept** (golden fixtures
reused by `upstream_test.go` later). The four surviving test files
(`config_test.go`, `resolve_test.go`, `health_test.go`, `logger_test.go`) are
**untouched** — the first three remain red on v1 schema until T3.S1; the fourth
stays green.

### Success Criteria

- [ ] `main.go` imports `github.com/modelcontextprotocol/go-sdk/mcp` plus the six
      stdlib imports (`context`, `net/http`, `os`, `os/signal`, `syscall`, `time`).
- [ ] `main.go` defines an unexported `type authHeaderKey struct{}` and an
      `authMiddleware(h http.Handler) http.Handler` that stores
      `r.Header.Get("Authorization")` under `authHeaderKey{}` in the request
      context and delegates to `h.ServeHTTP(w, r.WithContext(ctx))`.
- [ ] `main()` calls `mcp.NewServer(&mcp.Implementation{Name:"web-search-prime-fixer", Version: version}, nil)` and `mcp.NewStreamableHTTPHandler(func(*http.Request)*mcp.Server{ return server }, nil)`, and registers NO tools (no `AddTool` call).
- [ ] The mux mounts `/healthz` → `healthHandler` and `/` → `authMiddleware(sdkHandler)`.
- [ ] `main()` contains NO reference to `newProxyHandler` or `newUpstreamClient` (the v1 calls are gone).
- [ ] The graceful-shutdown goroutine and the `ListenAndServe`/`ErrServerClosed`
      tail are present and unchanged.
- [ ] `main.go` has a `[Mode A]` comment on the handler mount noting the SDK owns
      transport framing (initialize/Mcp-Session-Id/SSE/dispatch) per PRD §13/FR-1.
- [ ] `proxy.go`, `sse.go`, `rewrite.go`, `proxy_test.go`, `proxy_e2e_test.go`,
      `proxy_harness_test.go`, `proxy_log_test.go`, `sse_test.go`, `rewrite_test.go`
      are deleted; `testdata/` and the four surviving test files remain.
- [ ] `go build ./...` exits 0. `go run .` boots and stays up; `curl -s -o -
      http://127.0.0.1:8787/healthz` returns `200` with body
      `{"ok":true,"version":"dev"}`.
- [ ] `go.mod`/`go.sum` are NOT modified (the SDK dependency is owned by T2.S1).

## All Needed Context

### Context Completeness Check

_Pass._ Every SDK signature is **verified on-disk** against the cached
`go-sdk@v1.6.1` source (file:line citations in
`research/sdk_mount_and_v1_deletion.md`), including that
`*StreamableHTTPHandler` is an `http.Handler` (so it mounts on the mux / wraps in
middleware with no adapter) and that `ServeHTTP` passes `req.Context()` into
`server.Connect` (`streamable.go:491-493`, the verified basis for the auth
middleware). The exact new `main()` body, the `authHeaderKey`/`authMiddleware`
definitions, and the import block are given verbatim below. The deletion list is
confirmed against `ls` (all nine files exist). The post-deletion breakage map
(which surviving test files stay red, owned by T3.S1) is pinned by grepping the
surviving tests for v1 `.Aliases`/`newProxyHandler` references. The predecessor's
deliverables (`logger.go`, `health.go`, trimmed `main.go`, SDK in `go.mod`) are
treated as the T2.S1 contract. An agent with no prior knowledge of this repo can
implement this from the PRP + the quoted source.

### Documentation & References

```yaml
# MUST READ — the SDK adoption decision + transport/security/logging spec.
- file: PRD.md
  why: §13 "MCP SDK" (adopt go-sdk; NewStreamableHTTPHandler owns initialize/
        Mcp-Session-Id/SSE framing/JSON-RPC dispatch/tools/list/tools/call);
        FR-1 (Streamable HTTP on the SDK); §5.2 (we own the JSON-RPC surface,
        delegate only the search); §17 (forward Authorization verbatim, never
        read/log/store); §15 (logging — startup/shutdown events, redaction);
        §16 (GET /healthz, graceful shutdown 10s).
  critical: §13/§17 => the auth middleware stores Authorization in context but
        NEVER logs it; Config has no credential field. §13 => we register NO
        tools here (AddTool is P1.M5.T2); the SDK answers initialize/tools-list
        (empty list) on its own. §16 => keep the existing graceful-shutdown path.

# VERIFIED GOTCHAS — the SDK API surface + breakage map + middleware pattern.
- file: plan/002_0a8ab3410994/P1M1T2S2/research/sdk_mount_and_v1_deletion.md
  why: On-disk proof (go-sdk@v1.6.1) of NewServer/NewStreamableHTTPHandler
        signatures, *StreamableHTTPHandler implements http.Handler,
        Implementation{Name,Version} fields, the streamable.go:491-493 context
        threading, the unexported-struct context-key pattern, the exact nine-file
        deletion list, and the post-deletion breakage map (config_test.go/
        resolve_test.go/health_test.go stay RED = T3.S1; logger_test.go GREEN).
  critical: *StreamableHTTPHandler IS an http.Handler (mount directly, no
        adapter). go build ./... does NOT compile _test.go, so it is GREEN after
        deletion even though vet/test stay red on the three T3.S1 test files.
        testdata/ is NOT deleted (reused by upstream_test.go).

# PREDECESSOR (CONTRACT) — defines exactly what exists when this subtask starts.
- file: plan/002_0a8ab3410994/P1M1T2S1/PRP.md
  why: T2.S1 produces logger.go (logger code moved), health.go (health code moved),
        a trimmed main.go (moved defs removed; logStartup updated to v2 fields
        tools/canonical_tool/query_aliases/listen/upstream/log_level; main() body
        UNCHANGED — still calls newProxyHandler/newUpstreamClient which live in
        proxy.go), and go.mod (go 1.25.0 + SDK v1.6.1) + go.sum.
  critical: This subtask's INPUT is that trimmed main.go. main() still references
        newProxyHandler/newUpstreamClient (proxy.go) — T2.S2 REPLACES those calls
        with the SDK handler and then deletes proxy.go. version is defined in
        health.go (same package) — do NOT redefine it. logStartup is T2.S1's v2
        version — leave it. go.mod/go.sum are T2.S1's — do NOT run `go get`.

# SDK SOURCE — authoritative signatures (read the cached source to confirm).
- file: /home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/server.go
  why: server.go:157 `func NewServer(impl *Implementation, options *ServerOptions) *Server`
        (impl non-nil; pass nil options for defaults).
- file: /home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/streamable.go
  why: streamable.go:194 NewStreamableHTTPHandler signature; streamable.go:255
        `func (h *StreamableHTTPHandler) ServeHTTP(...)` => implements http.Handler;
        streamable.go:491-493 `server.Connect(req.Context(), ...)` => context values
        from middleware reach the tool handler. streamable.go:127 StreamableHTTPOptions.
- file: /home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/protocol.go
  why: protocol.go:1507 `type Implementation struct{Name,Title,Version,WebsiteURL; Icons []Icon}`.

# PATTERNS — the main.go bootstrap conventions to preserve.
- file: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  why: §4 documents the bootstrap sequence (ResolveConfig→newLogger→logStartup→
        route table→http.Server NO timeouts→graceful shutdown goroutine→
        ListenAndServe/ErrServerClosed). §6 maps the v1→v2 deletion impact
        (main.go's newProxyHandler/newUpstreamClient refs MUST be replaced).
  critical: Keep the bootstrap spine intact; only the handler-construction +
        route-table lines change. NO ReadTimeout/WriteTimeout (would truncate SSE).

# Go stdlib refs.
- url: https://go.dev/blog/context
  why: Context-key convention — "define an unexported type for the key to avoid
        collisions across packages." This is the authHeaderKey struct{} pattern.
  critical: Do NOT use a string context key (collision risk). The unexported
        struct type is zero-cost and package-private.

# Current codebase tree (the T2.S1 state — INPUT to this subtask)
```
### Current Codebase tree (after P1.M1.T2.S1, the INPUT state)

```bash
# Repo root: /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # SDK v1.6.1 + go 1.25.0 + // comment (T2.S1)              [UNCHANGED here]
  go.sum            # (T2.S1)                                                            [UNCHANGED]
  config.go         # v2 Config + DefaultConfig + LoadConfig + resolveConfigPath +       [UNCHANGED]
                    #   fileExists + ResolveConfig (T1.S1/T1.S2)
  logger.go         # logger code (T2.S1)                                               [UNCHANGED]
  health.go         # var version + healthHandler (T2.S1)                               [UNCHANGED]
  main.go           # trimmed (T2.S1): main() still calls newProxyHandler/              [EDITED here]
                    #   newUpstreamClient; logStartup=v2; imports 6 stdlib
  doc.go            # package comment                                                   [UNCHANGED]
  proxy.go          # v1 transparent proxy (newProxyHandler, newUpstreamClient, ...)    [DELETED here]
  sse.go            # v1 SSE framer/injector                                            [DELETED here]
  rewrite.go        # v1 alias renamer (Rewrite, RewriteResult)                         [DELETED here]
  config_test.go    # v1 schema (cfg.Aliases) — RED, T3.S1                              [UNCHANGED]
  resolve_test.go   # v1 schema (cfg.Aliases) — RED, T3.S1                              [UNCHANGED]
  health_test.go    # v1 (newProxyHandler, cfg.Aliases) — RED, T3.S1                    [UNCHANGED]
  logger_test.go    # tests newLogger only — GREEN                                      [UNCHANGED]
  proxy_test.go proxy_e2e_test.go proxy_harness_test.go proxy_log_test.go               [DELETED here]
  sse_test.go rewrite_test.go                                                          [DELETED here]
  testdata/*.sse    # golden fixtures — KEPT (reused by upstream_test.go later)         [UNCHANGED]
  config.example.json README.md                                                         [UNCHANGED]
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod go.sum config.go logger.go health.go doc.go config.example.json README.md   # UNCHANGED
  main.go           # EDITED: +mcp import; +authHeaderKey; +authMiddleware; main() body
                    #        rewritten (SDK server+handler+middleware; /healthz + / mount)
  config_test.go resolve_test.go health_test.go logger_test.go                        # UNCHANGED (tests; 3 stay RED = T3.S1)
  testdata/*.sse                                                                       # UNCHANGED
  # DELETED: proxy.go sse.go rewrite.go proxy_test.go proxy_e2e_test.go
  #          proxy_harness_test.go proxy_log_test.go sse_test.go rewrite_test.go
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: *mcp.StreamableHTTPHandler IS an http.Handler (verified: streamable.go:255
// defines ServeHTTP(http.ResponseWriter, *http.Request)). Mount it DIRECTLY on the mux
// (mux.Handle("/", ...)) and/or wrap it in authMiddleware — NO adapter function needed.
// Do NOT call sdkHandler.ServeHTTP from a HandleFunc unnecessarily; pass the handler.

// CRITICAL: register NO tools in this subtask. The SDK answers initialize/ping/
// notifications/resources.list/prompts.list/logging.setLevel/completion.complete
// automatically, and tools/list returns an EMPTY list with zero AddTool calls
// (verified). AddTool (P1.M5.T2) needs a tool handler + InputSchema; do not stub it.

// CRITICAL: the auth middleware stores the Authorization header in context but MUST
// NEVER log it (PRD §17) and Config has NO credential field (PRD §13). The value is
// forwarded verbatim to z.ai later (P1.M4.T1.S2 reads ctx.Value(authHeaderKey{})).
// Use an UNEXPORTED struct-type context key (authHeaderKey struct{}), NOT a string —
// strings collide across packages (https://go.dev/blog/context).

// CRITICAL: 'version' is defined in health.go (T2.S1 moved `var version = "dev"` there).
// main.go references it as serverInfo.Version. Do NOT redeclare version in main.go
// (compile error: redeclared in this block). It is the SAME package.

// CRITICAL: keep the http.Server timeouts ABSENT. The T2.S1 main.go already omits
// ReadTimeout/WriteTimeout (they would truncate SSE responses). Do not add them.

// GOTCHA: go build ./... does NOT compile _test.go files (only go vet/go test do).
// So `go build ./...` is GREEN after the deletion even though config_test.go/
// resolve_test.go/health_test.go still reference the deleted cfg.Aliases/newProxyHandler.
// Gate on `go build ./...` (GREEN); document that vet/test stay red on those three
// files until T3.S1. logger_test.go is GREEN (it only uses newLogger).

// GOTCHA: the v1 main() calls newUpstreamClient() and newProxyHandler(cfg, log, client).
// Those live in proxy.go, which this subtask DELETES. So you MUST remove those calls
// from main() (replace with the SDK handler) BEFORE/with deleting proxy.go, or main.go
// won't compile. The order is: rewrite main() body, then delete the v1 files.

// GOTCHA: do NOT delete testdata/. The golden *.sse fixtures are reused by the later
// upstream_test.go (PRD §19.4). Only the nine listed .go files are deleted.

// GOTCHA: do NOT run `go get`/`go mod tidy`/edit go.mod/go.sum. The SDK dependency is
// T2.S1's deliverable and is already in go.mod (go 1.25.0 + require go-sdk v1.6.1).
// If go.mod lacks the SDK, STOP — T2.S1 has not landed (parallel execution).

// SCOPE GUARD: Do NOT edit config.go (T1.x), logger.go/health.go (T2.S1), doc.go
// (P1.M5.T4), any *_test.go (T3.S1 owns the surviving three; the six are deleted),
// README.md/config.example.json (P1.M5.T4). Edit ONLY main.go; delete ONLY the nine
// listed v1 files.
```

## Implementation Blueprint

### Data models and structure

No new exported types. One unexported context-key type and one unexported
middleware function are added to `main.go`:

```go
// authHeaderKey is an unexported context-key type that carries the inbound
// Authorization header from the HTTP middleware down to the tool handler (and
// onward to the upstream z.ai client in P1.M4.T1.S2). An unexported struct type
// is the collision-free context-key convention (PRD §17; go.dev/blog/context).
type authHeaderKey struct{}

// authMiddleware wraps h so that each request's Authorization header is stored in
// the request context before h handles it. The SDK's StreamableHTTPHandler passes
// req.Context() into server.Connect (verified: streamable.go:491-493), so the
// value reaches the tool handler. The credential is NEVER logged or stored in
// Config (PRD §13/§17); it is only forwarded verbatim to z.ai later.
func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT STATE (read-only)
  - RUN: `test -f logger.go && test -f health.go && grep -F 'go-sdk v1.6.1' go.mod`
    -> all must succeed. go.mod must show `go 1.25.0` (NOT 1.22). `test -f go.sum`
    -> succeeds.
  - RUN: `grep -nE 'func logStartup|func main\(' main.go` -> both present.
        `grep -nE 'cfg\.(Tools|CanonicalTool|QueryAliases)' main.go` -> logStartup
        references v2 fields (T2.S1 landed). `grep -n 'cfg\.Aliases' main.go`
        -> should be EMPTY (T2.S1 removed it).
  - RUN: `ls proxy.go sse.go rewrite.go proxy_test.go proxy_e2e_test.go
        proxy_harness_test.go proxy_log_test.go sse_test.go rewrite_test.go`
        -> all nine exist (the deletion targets).
  - WHY: This subtask's INPUT is "logger.go, health.go, go.mod with SDK (T2.S1) +
        updated Config (T1.x)". If logger.go/health.go are missing or go.mod lacks
        the SDK, STOP — T2.S1 has not landed (parallel execution). Do NOT recreate
        them or run `go get`.

Task 1: EDIT main.go — add the SDK import
  - FIND the existing import block (T2.S1's six stdlib imports):
        import (
            "context"
            "net/http"
            "os"
            "os/signal"
            "syscall"
            "time"
        )
  - REPLACE with (stdlib group + SDK group, gofmt-stable):
        import (
            "context"
            "net/http"
            "os"
            "os/signal"
            "syscall"
            "time"

            "github.com/modelcontextprotocol/go-sdk/mcp"
        )
  - WHY: main() now references mcp.NewServer / mcp.NewStreamableHTTPHandler /
        mcp.Implementation / mcp.Server. The other six are still used by main()
        (context, net/http, os, os/signal, syscall, time).

Task 2: EDIT main.go — add authHeaderKey + authMiddleware (above main)
  - INSERT the authHeaderKey type and authMiddleware function (verbatim from
        "Data models and structure" above) immediately ABOVE the `func main()`
        definition (after logStartup).
  - GOTCHA: authHeaderKey MUST be unexported (lowercase) and a struct type — the
        collision-free context-key convention. Do NOT use a string key.

Task 3: EDIT main.go — rewrite main()'s body (replace the v1 handler wiring)
  - KEEP the opening of main() verbatim:
        cfg, err := ResolveConfig()
        if err != nil { ... newLogger(os.Stderr,"error").log("error","config",...) ; os.Exit(1) }
        log := newLogger(os.Stderr, cfg.LogLevel)
        logStartup(log, cfg)
  - DELETE the v1 line `client := newUpstreamClient()`.
  - INSERT (Mode A doc comment + SDK construction) verbatim from
        "Implementation Patterns & Key Details" below.
  - REPLACE the mux block:
        OLD (v1):
            mux := http.NewServeMux()
            mux.HandleFunc("/healthz", healthHandler)
            mux.HandleFunc("/", newProxyHandler(cfg, log, client)) // subtree catch-all
        NEW (v2):
            mux := http.NewServeMux()
            mux.HandleFunc("/healthz", healthHandler)                 // local health (no upstream); PRD §16
            mux.Handle("/", authMiddleware(sdkHandler))               // SDK owns MCP framing; subtree catch-all
  - KEEP unchanged: `srv := &http.Server{Addr: cfg.Listen, Handler: mux}`,
        the graceful-shutdown goroutine, and the `ListenAndServe`/`ErrServerClosed`
        tail (see "Implementation Patterns & Key Details" for the full target main()).
  - VERIFY: `grep -nE 'newProxyHandler|newUpstreamClient' main.go` -> EMPTY.

Task 4: DELETE the nine v1 files
  - COMMAND (repo root):
        rm -f proxy.go sse.go rewrite.go \
              proxy_test.go proxy_e2e_test.go proxy_harness_test.go \
              proxy_log_test.go sse_test.go rewrite_test.go
  - VERIFY: `ls proxy.go sse.go rewrite.go proxy_test.go 2>&1` -> "No such file".
        `ls testdata/` -> STILL PRESENT (golden fixtures kept).
  - GOTCHA: do NOT delete testdata/, config.go, logger.go, health.go, doc.go, or
        the four surviving test files. Only the nine listed .go files.

Task 5: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w main.go
        go build ./...                                              # PRIMARY GATE: MUST exit 0
        go build -o /tmp/wspf-boot . && ( /tmp/wspf-boot & SV=$! ; \
          sleep 1 ; curl -s -w '\nHTTP %{http_code}\n' http://127.0.0.1:8787/healthz ; \
          kill $SV 2>/dev/null ; wait $SV 2>/dev/null ) ; rm -f /tmp/wspf-boot
  - EXPECT: `go build ./...` exit 0 (no output). The curl line prints
        {"ok":true,"version":"dev"}  and  HTTP 200.
  - NOTE: `go vet ./...` and `go test ./...` are EXPECTED to remain RED on
        config_test.go/resolve_test.go/health_test.go (v1 .Aliases/newProxyHandler
        refs — T3.S1). logger_test.go PASSES. Any error in main.go/config.go/
        logger.go/health.go IS a defect; errors only in those three test files are
        expected and out of scope here.
```

### Implementation Patterns & Key Details

```go
// The full target main() body (the spine is unchanged from T2.S1; only the
// handler-construction + mux lines change). 'version' and 'healthHandler' come
// from health.go; 'logStartup', 'newLogger' from main.go/logger.go (same package).

func main() {
	cfg, err := ResolveConfig()
	if err != nil {
		newLogger(os.Stderr, "error").log("error", "config", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	log := newLogger(os.Stderr, cfg.LogLevel)
	logStartup(log, cfg)

	// MCP SDK server: owns the JSON-RPC surface (initialize, ping, notifications/*,
	// resources/list, prompts/list, tools/list, tools/call). NO tools are registered
	// in this milestone — AddTool calls arrive in P1.M5.T2. Pass nil ServerOptions
	// for defaults. version is the health.go package var ("dev", or set via -ldflags).
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)

	// The SDK's StreamableHTTPHandler OWNS all MCP transport framing — initialize
	// handshake, Mcp-Session-Id lifecycle, SSE framing, JSON-RPC dispatch, and
	// tools/list / tools/call routing (PRD §13, FR-1). We hand it the same *Server
	// for every request (getServer may return a shared server). Pass nil options
	// for stateful sessions + SSE responses (the Streamable HTTP transport). This
	// replaces the v1 byte-forwarding proxy + hand-rolled SSE framer.
	sdkHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)

	// Route table: /healthz is our local handler (no upstream); EVERYTHING else
	// goes to the SDK handler, wrapped so the inbound Authorization header is
	// threaded into the request context for later upstream forwarding (PRD §17).
	// The SDK passes req.Context() into server.Connect (verified streamable.go:491),
	// so the context value reaches the tool handler. We never log the credential.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/", authMiddleware(sdkHandler))

	// NO ReadTimeout/WriteTimeout — a write deadline would truncate streamed SSE
	// responses. Addr + Handler only (the SDK drives the streaming lifecycle).
	srv := &http.Server{Addr: cfg.Listen, Handler: mux}

	// Graceful shutdown (PRD §16): SIGINT/SIGTERM -> one "shutdown" log line ->
	// srv.Shutdown with a 10s deadline -> ListenAndServe returns ErrServerClosed ->
	// clean exit 0. (Unchanged from v1.)
	go func() {
		sigCh := make(chan os.Signal, 1)
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

// PATTERN: context-key middleware. authMiddleware is a pure http.Handler wrapper;
// it does not import the SDK and is independently unit-testable (T3+). The stored
// value is read back downstream as: v, _ := ctx.Value(authHeaderKey{}).(string).
// r.Header.Get("Authorization") returns "" when the header is absent; storing an
// empty string is harmless (downstream only forwards a non-empty value).

// SECURITY: Authorization is the only credential on the wire. It lives ONLY in the
// request context (authHeaderKey), never in Config, never in a log line (PRD §13/§17).
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"           # main.go + config.go + logger.go + health.go + doc.go are package main

SYMBOLS INTRODUCED (consumed by later subtasks):
  - type authHeaderKey struct{}                     # UNEXPORTED context key; read by
        P1.M4.T1.S2 (upstream auth threading) and the P1.M5.T2 dispatch handler via
        ctx.Value(authHeaderKey{}).(string).
  - func authMiddleware(h http.Handler) http.Handler # UNEXPORTED; wraps the SDK handler.
  - (main.go now constructs) *mcp.Server             # consumed by P1.M5.T2 (AddTool) —
        the same server instance is returned by getServer each request.

INTEGRATION WITH THE SDK (PRD §13, FR-1):
  - mcp.NewServer(...nil)            # owns the JSON-RPC surface
  - mcp.NewStreamableHTTPHandler(...nil)  # owns transport framing (initialize/
        Mcp-Session-Id/SSE/dispatch). *StreamableHTTPHandler is an http.Handler.

DOWNSTREAM CONSUMERS:
  - P1.M5.T2: calls server.AddTool(&mcp.Tool{...}, handler) to register web_search.
  - P1.M4.T1.S2: reads ctx.Value(authHeaderKey{}) to inject Authorization upstream.
  - P1.M1.T3.S1: rewrites the three red surviving test files; logger_test.go unchanged.

NO INTEGRATION POINTS TOUCHED BY THIS SUBTASK:
  - config.go, logger.go, health.go, doc.go, go.mod, go.sum, testdata/,
        config.example.json, README.md, and the four surviving *_test.go files.
```

## Validation Loop

> Unlike T2.S1 (which needed isolated temp-module gates because production `main.go`
> referenced deleted symbols), T2.S2 ends with **`go build ./...` GREEN** (the v1
> proxy refs are replaced and the dead files are deleted). `go vet`/`go test` stay
> red ONLY on the three v1-schema test files that T3.S1 owns — that is expected and
> documented, not gated.

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -l main.go                              # MUST print nothing (already formatted)
gofmt -w main.go                              # idempotent format

# main.go must import the SDK and the six stdlib packages, and no others:
grep -A8 '^import (' main.go | grep -E '"context"|"net/http"|"os"|"os/signal"|"syscall"|"time"|"github.com/modelcontextprotocol/go-sdk/mcp"'
# Expected: 7 matches (6 stdlib + 1 SDK).

# v1 handler references are gone from main.go:
! grep -nE 'newProxyHandler|newUpstreamClient' main.go   # MUST succeed (exit 0)
# SDK construction + mount are present:
grep -n 'mcp.NewServer' main.go                          # MUST match
grep -n 'mcp.NewStreamableHTTPHandler' main.go           # MUST match
grep -n 'mux.Handle("/", authMiddleware(sdkHandler))' main.go  # MUST match
grep -n 'authHeaderKey' main.go                          # MUST match (type + middleware use)
# No tools registered this milestone:
! grep -n 'AddTool' main.go                              # MUST succeed (no AddTool yet)

# Expected: all greps behave as noted. If `newProxyHandler`/`newUpstreamClient` still
# appear, main()'s v1 wiring was not fully replaced -> fix before continuing.
```

### Level 2: Build Gate (Component Validation — PRIMARY)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go build ./...          # PRIMARY GATE: MUST exit 0, no output
# Expected: exit 0. This compiles config.go + logger.go + health.go + main.go + doc.go
# (proxy.go/sse.go/rewrite.go are deleted). _test.go files are NOT compiled by `go build`,
# so the surviving v1-schema test files do not affect this gate.

# Confirm the nine v1 files are gone and testdata + surviving tests remain:
for f in proxy.go sse.go rewrite.go proxy_test.go proxy_e2e_test.go \
         proxy_harness_test.go proxy_log_test.go sse_test.go rewrite_test.go; do
  test ! -e "$f" && echo "deleted: $f" || echo "STILL EXISTS: $f"
done
# Expected: nine "deleted:" lines.
ls testdata/ >/dev/null 2>&1 && echo "testdata retained" || echo "testdata MISSING"
# Expected: "testdata retained".
ls config_test.go resolve_test.go health_test.go logger_test.go >/dev/null 2>&1 && echo "surviving tests retained"
# Expected: "surviving tests retained".

# Documented expected-red (NOT a gate to fix): vet/test stay red on the three T3.S1 files.
go vet ./... 2>&1 | grep -E 'config_test|resolve_test|health_test' | head || true
# Expected: errors ONLY in config_test.go / resolve_test.go / health_test.go (cfg.Aliases /
# newProxyHandler refs — T3.S1). Any vet error in main.go/config.go/logger.go/health.go
# is a DEFECT in this subtask — fix it. logger_test.go must NOT appear here.
```

### Level 3: Runtime Smoke (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Build a real binary and exercise the running server.
go build -o /tmp/wspf-boot . && {
  /tmp/wspf-boot & SV=$!
  sleep 1   # allow bind + handler mount
  # Health: local handler, no upstream — the contract's explicit verification.
  curl -s -w '\nHTTP %{http_code}\n' http://127.0.0.1:8787/healthz
  # Expected:
  #   {"ok":true,"version":"dev"}
  #   HTTP 200
  kill "$SV" 2>/dev/null
  wait "$SV" 2>/dev/null
  echo "exit=$?"
}
rm -f /tmp/wspf-boot
# Expected: healthz body {"ok":true,"version":"dev"} + HTTP 200. The process exits 0
# on SIGTERM (graceful shutdown path). This proves main() ran past server+handler
# construction, the mux routes /healthz to healthHandler, and the SDK handler mounts
# without crashing the boot.

# OPTIONAL mount proof (robust to a non-404): a JSON-RPC initialize POST to "/" should
# NOT be a 404 (proves the SDK handler is mounted at "/"). A non-conformant request may
# get 400/406 from the SDK (wrong Accept/headers) — that is fine; 404 would mean the
# handler is NOT mounted. (The full initialize/tools-list round-trip is exercised by
# server_test.go in P1.M5.T3; not required here.)
```

### Level 4: Static & Doc Checks (Domain-Specific)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Mode A doc comment deliverable: the handler mount notes the SDK owns transport framing.
grep -nE 'SDK|Streamable|Mcp-Session-Id|SSE|framing|initialize' main.go | head
# Expected: a comment block above the sdkHandler construction / mux.Handle explaining
# the SDK owns initialize/Mcp-Session-Id/SSE/dispatch (PRD §13, FR-1).

# authHeaderKey is an unexported struct type (collision-free context key):
grep -n 'type authHeaderKey struct{}' main.go    # MUST match
grep -nE 'context\.WithValue\(r\.Context\(\), authHeaderKey\{\}, r\.Header\.Get\("Authorization"\)\)' main.go
# MUST match (the middleware stores the header under the key).

# No credential logging / no Config credential field (PRD §13/§17):
! grep -nE '\.log\(.*[Aa]uthorization' main.go   # MUST succeed (Authorization never logged)

# go.mod untouched by this subtask (SDK is T2.S1's):
grep -F 'require github.com/modelcontextprotocol/go-sdk v1.6.1' go.mod   # STILL present
grep -E '^go 1\.25\.0$' go.mod                                           # STILL 1.25.0
git diff --name-only go.mod go.sum                                       # MUST be empty (unchanged)
# (If git shows go.mod/go.sum changed, you accidentally ran `go get`/tidy — revert.)

# Single SDK require, no other externals:
grep -cE '^\s+github\.com/modelcontextprotocol/go-sdk v1\.6\.1$' go.mod   # exactly 1 (direct)
```

## Final Validation Checklist

### Technical Validation

- [ ] `gofmt -l main.go` prints nothing.
- [ ] **`go build ./...` exits 0** (PRIMARY gate; production code green).
- [ ] The binary boots; `GET /healthz` → `200 {"ok":true,"version":"dev"}`.
- [ ] `go vet`/`go test` red ONLY on `config_test.go`/`resolve_test.go`/`health_test.go` (T3.S1); no error in production files; `logger_test.go` green.
- [ ] `go.mod`/`go.sum` unchanged by this subtask.

### Feature Validation

- [ ] `main.go` imports `github.com/modelcontextprotocol/go-sdk/mcp` (+ 6 stdlib).
- [ ] `main()` calls `mcp.NewServer(&mcp.Implementation{Name:"web-search-prime-fixer", Version: version}, nil)` and `mcp.NewStreamableHTTPHandler(func(*http.Request)*mcp.Server{return server}, nil)`; NO `AddTool`.
- [ ] `authHeaderKey struct{}` + `authMiddleware(http.Handler) http.Handler` defined; middleware stores `r.Header.Get("Authorization")` under `authHeaderKey{}` via `context.WithValue`.
- [ ] Mux: `/healthz` → `healthHandler`; `/` → `authMiddleware(sdkHandler)`.
- [ ] `main()` has no `newProxyHandler`/`newUpstreamClient` references; graceful-shutdown goroutine + `ListenAndServe`/`ErrServerClosed` tail intact; no server timeouts.
- [ ] Nine v1 files deleted; `testdata/` + four surviving test files retained.
- [ ] `[Mode A]` comment on the handler mount (SDK owns transport framing per PRD §13/FR-1).

### Code Quality Validation

- [ ] `*StreamableHTTPHandler` mounted directly (it is an `http.Handler`); no needless adapter.
- [ ] Context key is an unexported struct type (not a string); credential never logged (PRD §17).
- [ ] `version` referenced from `health.go` (not redeclared); `logStartup` left as T2.S1's v2 version.
- [ ] Scope respected: ONLY `main.go` edited; ONLY the nine listed v1 files deleted.

### Documentation & Deployment

- [ ] Handler-mount comment documents that the SDK owns initialize/Mcp-Session-Id/SSE/dispatch (PRD §13, FR-1).
- [ ] No env vars changed; no README/config.example.json/doc.go changes (later subtasks).

---

## Anti-Patterns to Avoid

- ❌ Don't write an adapter to make `*StreamableHTTPHandler` an `http.Handler` — it already is one (`ServeHTTP` at streamable.go:255). Mount it directly / wrap it in `authMiddleware`.
- ❌ Don't register any tool (`AddTool`) in this milestone — tools arrive in P1.M5.T2. The SDK answers initialize/tools-list (empty list) with zero tools (verified).
- ❌ Don't use a string context key for Authorization — use the unexported `authHeaderKey struct{}` (collision-free; go.dev/blog/context). And never log the credential (PRD §17).
- ❌ Don't redeclare `var version` in `main.go` — it lives in `health.go` (T2.S1); reference it in `mcp.Implementation{Version: version}`. Same package.
- ❌ Don't delete `proxy.go`/`sse.go`/`rewrite.go` while `main()` still calls `newProxyHandler`/`newUpstreamClient` — rewrite `main()`'s body FIRST (Task 3), then delete (Task 4), or the package won't build between steps.
- ❌ Don't delete `testdata/` or the four surviving test files — `testdata/` is reused by `upstream_test.go`; the three red test files are T3.S1's; `logger_test.go` is green.
- ❌ Don't run `go get`/`go mod tidy`/edit `go.mod`/`go.sum` — the SDK dependency is T2.S1's and is already present. If it's missing, STOP (T2.S1 hasn't landed).
- ❌ Don't add `ReadTimeout`/`WriteTimeout` to the `http.Server` — they would truncate SSE responses (PRD §8/§11.3). Keep `Addr`+`Handler` only.
- ❌ Don't gate on `go vet`/`go test` being fully green — they stay red on the three v1-schema test files until T3.S1. The gate is `go build ./...` (which excludes `_test.go`).
- ❌ Don't touch `config.go` (T1.x), `logger.go`/`health.go` (T2.S1), `doc.go` (P1.M5.T4), `README.md`/`config.example.json` (P1.M5.T4), or any `*_test.go`.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is a well-bounded edit to `main()` plus a deletion. Every
SDK signature is **verified on-disk** in `research/sdk_mount_and_v1_deletion.md`
(`NewServer`, `NewStreamableHTTPHandler`, `*StreamableHTTPHandler` is an
`http.Handler`, `Implementation{Name,Version}`, the `streamable.go:491-493` context
threading that justifies the auth middleware). The exact target `main()` body, the
`authHeaderKey`/`authMiddleware` definitions, and the import block are given
verbatim. The nine-file deletion list is confirmed against `ls`, and the
post-deletion breakage map is pinned by grepping the surviving tests (so the
implementer knows `go build ./...` is the green gate while vet/test stay red only on
the T3.S1 trio). The gate is achievable directly (`go build ./...` excludes
`_test.go`), and the runtime smoke (`/healthz` → 200) is a crisp behavioral check.
The residual 1/10 risk is an agent either (a) redeclaring `version` in main.go,
(b) deleting the v1 files before rewriting `main()`, (c) running `go get`/editing
go.mod, or (d) gating on `go test` green — all four are pinned explicitly in the
Gotchas/Anti-Patterns with the exact grep checks that catch them.
