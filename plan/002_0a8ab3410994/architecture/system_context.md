# System Context — web-search-prime-fixer (v2.0 delta)

## Project state: MAJOR REWRITE of a working v1 codebase

The working directory `/home/dustin/projects/web-search-prime-fixer` contains a
**complete v1 implementation** (the transparent proxy that **failed in production**).
This plan (session `002`) is a full architectural pivot from "transparent proxy"
to "normalizing MCP server."

The v1 code compiles and passes its tests, but the architecture is wrong: the
client rejected wrong tool names locally before any request reached the proxy, so
the proxy's argument-rename logic never ran. v2 inverts the architecture.

## What EXISTS now (v1, to be modified/deleted)

### Reusable infrastructure (KEEP — extract to own files, behavior unchanged)
| v1 location | v2 file | Notes |
|---|---|---|
| `main.go`: `logger`, `newLogger`, `log`, `redactHeaders`, level consts | `logger.go` | Structured JSON logger to stderr. Redacts Authorization/Cookie/Set-Cookie/Proxy-Authorization. Unchanged behavior; move to own file per PRD §14 layout. |
| `main.go`: `healthHandler`, `var version = "dev"` | `health.go` | GET /healthz → `{"ok":true,"version":"<version>"}`. 405 on non-GET. `-ldflags` seam. Pure local (no upstream). |
| `main.go`: graceful shutdown (SIGINT/SIGTERM → srv.Shutdown 10s) | stays in `main.go` | Reused. |
| `config.go`: `resolveConfigPath`, `ResolveConfig` discovery/precedence logic | `config.go` | Discovery (WSPF_CONFIG/CWD/XDG) + env overrides (WSPF_UPSTREAM/LISTEN/LOG_LEVEL) reused as-is. Only the **struct schema + validation** change. |

### To be DELETED (v1 core — replaced by SDK + new logic)
- `proxy.go` (~559 lines) — byte-forwarding transparent handler, `newUpstreamClient`, `forward`, `decideRewrite`, `rewriteObject`, `copyForwardHeaders`, `isHopByHop`, `flushWriter`.
- `sse.go` (~314 lines) — hand-rolled SSE reader/injector (`SSEReader`, `Event`, `Inject`, `emitEvent`, `warningText`). The SDK owns SSE framing now.
- `rewrite.go` (~87 lines) — narrow alias-rename (`Rewrite`, `RewriteResult`). Replaced by `extract.go`.
- **Six test files:** `proxy_test.go`, `proxy_e2e_test.go`, `proxy_harness_test.go`, `proxy_log_test.go`, `sse_test.go`, `rewrite_test.go`.
- The v1 non-goals that forbade normalizing — explicitly dropped.

### Existing tests that need UPDATING (not deleted)
- `config_test.go` — asserts `DefaultConfig()`, `LoadConfig()`. Must be rewritten for the new Config schema (new fields, dropped `Aliases`, new `QueryAliases`/`OptionalAliases`/`Tools`/etc.).
- `resolve_test.go` — config discovery/precedence tests. Update for new validation rules + new schema. The `isolateConfigEnv`/`writeConfig` helpers are reusable.
- `health_test.go` — tests `healthHandler` + `logStartup`. `logStartup` signature changes (new config fields). Update the `TestLogStartup` field assertions.

### Reused test fixtures
- `testdata/initialize.sse`, `testdata/tools_call.sse`, `testdata/tools_call_multiline.sse` — golden fixtures from v1's direct z.ai probe. Reused by the new `upstream_test.go` (PRD §19.4). May need the actual `mcp.` namespace check but are retained as wire-format references.

## What is NEW (v2)
| File | Responsibility |
|---|---|
| `extract.go` | Pure query-extraction from arbitrary JSON structure (PRD §10). Table-tested. |
| `teach.go` | Canonical-pair rule + warning text + append-after-results (PRD §12). |
| `upstream.go` | MCP client to z.ai via SDK: lazy shared session, re-init on expiry, auth threading (PRD §11). |
| `tools.go` | Tool definitions: `web_search` permissive schema + terse alias schemas (PRD §9). |
| `server.go` | Register tools (`AddTool`), dispatch handler: extract → delegate → teach (PRD §5.2/§11.3). |
| `logger.go` | Extracted from main.go (logger/redactHeaders). |
| `health.go` | Extracted from main.go (healthHandler/version). |
| `extract_test.go`, `teach_test.go`, `server_test.go`, `upstream_test.go` | New test suites. |

## New external dependency
- `github.com/modelcontextprotocol/go-sdk` v1.6.1 (official, stable, post-1.0). Verified present in local module cache at `/home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/`. The sole `require` in `go.mod`. The v1 "stdlib-only / no go.sum" constraint is explicitly relinquished (PRD §13).

## Toolchain
- `go1.26.4-X:nodwarf5 linux/amd64` (satisfies PRD floor `go 1.22+`; SDK v1.6.1 works with it).
- `GOPATH=/home/dustin/go`. SDK already cached — `go mod tidy` will not require network.
- Module path: `web-search-prime-fixer`.

## Architecture (v2 data flow)
```
MCP client (pi/Claude/Cursor)  -- toolPrefix:"none" --  POST /mcp  (JSON-RPC, SSE)
   │
   ▼
web-search-prime-fixer (Go binary, Go MCP SDK)
   ├── /healthz → local health JSON (no upstream)
   └── everything else → mcp.NewStreamableHTTPHandler (SDK owns initialize/session/SSE/dispatch)
        └── tools/list → our generated list: ONE tool, web_search (PRD §9)
        └── tools/call → extract.go (query from ANY structure)
                       → if no query: teach.go returns warning immediately, NO upstream call
                       → else: upstream.go delegates ONE tools/call to z.ai
                       → teach.go appends warning AFTER results if non-canonical
   │
   ▼
https://api.z.ai/api/mcp/web_search_prime/mcp  (one shared MCP client session)
```

## Key architectural differences from v1
1. **We own the JSON-RPC surface** (via SDK's `NewStreamableHTTPHandler`), not forward bytes.
2. **We own `tools/list`** — we advertise exactly `web_search` (canonical) plus optional terse aliases. Never `web_search_prime`.
3. **We act as an MCP client to z.ai** (via SDK's `StreamableClientTransport` + `CallTool`), not forward the client's request.
4. **Extraction is broad** (any structure → query string), not narrow (rename one key).
5. **Warning is appended AFTER results**, not prepended/injected into SSE. Results always accompany the warning (except when there's genuinely no query).

## Implementation order (PRD §21, 5 milestones)
1. **M1:** config.go schema extension + SDK dep + extract logger/health to own files + delete v1 proxy/sse/rewrite + mount SDK handler (no-op tools) + boot + /healthz green.
2. **M2:** extract.go + extract_test.go (pure, table-driven, every structure).
3. **M3:** teach.go + teach_test.go (canonical rule, append-after, no-results).
4. **M4:** upstream.go + upstream_test.go (SDK client, lazy session, re-init, auth).
5. **M5:** tools.go + server.go + server_test.go (e2e) + doc sweep (README, config.example.json, doc.go) + quality gate (go vet/test/build).
