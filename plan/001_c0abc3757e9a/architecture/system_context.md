# System Context — web-search-prime-fixer

## Project state
**Greenfield.** The working directory `/home/dustin/projects/web-search-prime-fixer`
contains only:
- `PRD.md` — authoritative design doc (581 lines)
- `plan/` — planning scaffolding (read-only snapshots + this architecture dir)
- `.git` — single commit `3031xxx initial spec`

There is **no Go source, no `go.mod`/`go.sum`, no vendor dir, no CI, no Dockerfile,
no Makefile**. Every source file listed in PRD §9 must be created from scratch.

## Toolchain
- Installed: `go1.26.4-X:nodwarf5 linux/amd64` (satisfies PRD floor `go 1.22+`).
- `GOPATH=/home/dustin/go`, `GOROOT=/usr/lib/go`, `GOPROXY=proxy.golang.org,direct`.
- **No third-party dependencies are required or allowed** (PRD §6, §9). `go.mod`
  will list zero requires. `CGO_ENABLED=0` is trivially achievable for a static
  binary but is not mandated.

## The problem this proxy solves (validated)
The z.ai `web-search-prime` MCP tool's ONLY required parameter is `search_query`.
Agents routinely send `query` (or `q`, `searchQuery`, etc.) instead, which z.ai
ignores — returning empty/wrong results and burning retries.

**Direct schema confirmation** (via the live `web-search-prime` MCP server):
```
web_search_prime_web_search_prime
  search_query          (string, REQUIRED)   <- the canonical name agents miss
  search_domain_filter  (string)             <- must pass through UNTOUCHED
  search_recency_filter (string)             <- must pass through UNTOUCHED
  content_size          (string)             <- must pass through UNTOUCHED
  location              (string)             <- must pass through UNTOUCHED
```
This validates PRD §1 (root cause) and PRD §3 (non-goals: do NOT normalize
location/content_size/recency — they pass through verbatim).

## Architecture summary (per PRD §8-§9)
A single Go binary, one `http.Server` bound to `127.0.0.1:8787`, two routes:
- `GET /healthz` → local health JSON (never touches upstream).
- everything else → MCP proxy handler.

All non-`/healthz` traffic is forwarded to the single fixed upstream
`https://api.z.ai/api/mcp/web_search_prime/mcp` (Streamable HTTP / SSE,
protocol `2024-11-05`). Identity pass-through session handling: the upstream's
`mcp-session-id` is forwarded unchanged; no session map is needed.

## Core data flow
```
client POST /mcp (JSON-RPC over HTTP, SSE response)
  │
  ▼ proxy.go: read body → decide rewrite
  │   ├─ no alias present  → streamThrough=true, forward ORIGINAL bytes
  │   └─ alias present     → Rewrite(args), re-serialize, remember reqID
  ▼ forward to upstream (POST, ctx-cancelled, ResponseHeaderTimeout 30s)
  ▼ response:
  │   ├─ streamThrough → io.Copy verbatim (zero buffering)
  │   └─ rewritten     → sse.go inject warning into tools/call result, flush
```

## Files to create (PRD §9 file layout)
| File | Responsibility |
|------|----------------|
| `go.mod` | module `web-search-prime-fixer`, `go 1.22`, zero deps |
| `main.go` | config load, server bootstrap, graceful shutdown, version |
| `config.go` | Config struct, defaults, file+env loading, validation |
| `proxy.go` | HTTP handler: passthrough vs rewrite, forward, response write |
| `rewrite.go` | pure `Rewrite(args, aliases, target) RewriteResult` |
| `sse.go` | SSE reader + warning injector |
| `doc.go` | package comment |
| `config.example.json` | documented example config |
| `README.md` | install + run |
| `rewrite_test.go` / `sse_test.go` / `proxy_test.go` | tests |
| `testdata/initialize.sse` / `testdata/tools_call.sse` | golden fixtures |

## Implementation order (PRD §20)
config+main skeleton → rewrite → sse → proxy wiring → e2e tests → docs → `go vet`/`go test`.
