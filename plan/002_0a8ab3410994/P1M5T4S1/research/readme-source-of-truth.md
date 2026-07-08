# README rewrite — source-of-truth facts (verified on disk)

Every value below is copied verbatim from the on-disk source the README must
describe. The README must match these byte-for-byte where it quotes them.

## 1. What it is (the v2 framing — replaces ALL v1 proxy framing)

- A **normalizing MCP server** (NOT a proxy). Built on the **official Go MCP SDK**
  (`github.com/modelcontextprotocol/go-sdk` v1.6.1). One Go binary.
- Goal (PRD §2): make an agent's **first** web-search attempt succeed, regardless
  of how it guessed the tool name or mangled the arguments, while teaching toward
  one canonical form.
- Two-part fix (PRD §5): **Part A** = client unlock (bare tool names via
  `toolPrefix:"none"`); **Part B** = this server (advertise `web_search`, extract
  the query from ANY input, delegate ONE clean call to z.ai, append a warning
  after the results when usage was non-canonical).
- It OWNS the JSON-RPC surface and `tools/list` (advertises essentially one tool),
  and acts as an MCP **client** to z.ai (one lazily-initialized session, reused,
  re-initialized on expiry). It is NOT a byte forwarder, NOT a transparent proxy,
  NOT an open proxy.

## 2. Canonical surface (PRD §9.4 / tools.go)

- **Tool:** `web_search`
- **Parameter:** `query` (string, required)
- **Optional** (passed through to z.ai when provided): `location`,
  `content_size`, `search_recency_filter`
- Canonical is `web_search` / `query` exactly. Anything else triggers the warning.
- The z.ai-branded name `web_search_prime` is **never** advertised to the agent.

### Canonical tool Description (tools.go `canonicalDescription`, default CanonicalParam "query"):
```
Performs a web search via z.ai. Call with { "query": "..." }. Accepts alternative parameter names but canonical is query.
```
### Alias tool Description (tools.go `aliasDescription`, default CanonicalTool "web_search"):
```
Performs a web search; alias of web_search.
```

## 3. What the agent sees (teach.go — VERBATIM, byte-fixed incl. EM DASH `—`)

Canonical call (`web_search` + `query`) → results only, **no warning, no retry**.

Non-canonical (any other tool name / param name / nested / bare-string / array /
inferred / normalized optionals) → results FIRST, then warning appended AFTER:

```
[web-search-prime-fixer] Warning: this call used "<tool>"/"<source>" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```

Empty input (no usable query anywhere) → immediate warning, **NO upstream call**:

```
[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```

`isError` is **never** set for any normalization, guidance, or warning case (FR-6,
PRD §12.2). The warning text contains an **EM DASH** (`—`, U+2014) before
"`e.g.`", NOT a hyphen. The example query `"rust async runtime"` is a fixed
literal.

## 4. Part A client config (PRD §20 — VERBATIM JSON block)

`~/.pi/agent/mcp.json`:

```json
{
  "settings": { "toolPrefix": "none" },
  "mcpServers": {
    "web_search": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${Z_AI_API_KEY}" }
    }
  }
}
```

Notes (PRD §20 / §5.1):
- `toolPrefix: "none"` makes pi expose each tool by its bare name, so `web_search`
  is callable as `web_search` rather than `web_search_web_search_prime`. It is
  GLOBAL across all the operator's MCP servers; safe for the operator's current
  `web_search` + `zread` set; only matters if two servers ever advertise the same
  tool name.
- The `Authorization` header stays on the client; the server forwards it verbatim
  to z.ai and holds no key of its own (PRD §17, FR-7).
- `${Z_AI_API_KEY}` — the operator's own z.ai API key.
- Other clients (Claude Code, Cursor): whatever makes them expose bare tool names
  (document per-client as needed; the URL `http://127.0.0.1:8787/mcp` is the same).

## 5. Build (go.mod + system_context.md)

- **Sole external dependency:** `github.com/modelcontextprotocol/go-sdk` v1.6.1.
- Module directive in go.mod: `go 1.25.0` (cite go.mod as source of truth for the
  toolchain; do NOT hardcode a version that can drift).
- The v1 README's claims are now **FALSE and must be removed**: "Go standard
  library only", "no external dependencies", "no go.sum", "no network access is
  needed to build". The stdlib-only constraint was explicitly relinquished (PRD §13).
- Build commands:
  - `go mod tidy`  (resolve SDK; SDK is cached locally so this needs no network)
  - `go build -o web-search-prime-fixer .`
  - Version via ldflags: `go build -ldflags "-X main.version=0.2.0" -o web-search-prime-fixer .`
    (a plain build reports `dev`).
- Output binary: `./web-search-prime-fixer`.

## 6. Run / health (main.go + health.go)

- `./web-search-prime-fixer` — runs with built-in defaults (no config file needed):
  listens `127.0.0.1:8787`, delegates to z.ai.
- Binds `127.0.0.1` ONLY (not an open proxy). TLS unnecessary (loopback HTTP to a
  TLS upstream).
- Graceful shutdown on SIGINT/SIGTERM (10s drain; in-flight upstream calls
  cancelled via context).
- `GET /healthz` → `200` `{"ok":true,"version":"<version>"}`, `Content-Type:
  application/json`. Non-GET → 405. Does NOT touch the upstream. `version` default
  `dev`, set via `-ldflags "-X main.version=..."` (health.go `var version`).

## 7. Server configuration (config.go — v2 schema, defaults, env, BREAKING CHANGE)

### Config struct + JSON keys + defaults (config.go `DefaultConfig`, field order = PRD §18.1):

| JSON key | Go field | Default | Notes |
| --- | --- | --- | --- |
| `upstream` | Upstream | `https://api.z.ai/api/mcp/web_search_prime/mcp` | Must be absolute URL. |
| `listen` | Listen | `127.0.0.1:8787` | Must parse host:port. Local only. |
| `path` | Path | `/mcp` | Informational/reserved. |
| `tools` | Tools | `["web_search"]` | Advertised tool names; Tools[0] is canonical. Never `web_search_prime`. |
| `canonical_tool` | CanonicalTool | `web_search` | Must be present in Tools. |
| `canonical_param` | CanonicalParam | `query` | The param we teach. |
| `query_aliases` | QueryAliases | `["query","search_query","q","search","searchQuery","search_term","term","text","input","prompt","question","keywords","topic","searchString"]` | **RENAMED from v1 `aliases`** (BREAKING). Ordered slice (index order = extraction priority). |
| `optional_aliases` | OptionalAliases | `{"location":["country","region"],"content_size":["size","contentSize","detail"],"search_recency_filter":["recency","freshness","time_filter","date_filter"]}` | Map of z.ai canonical optional → alias priority slice. |
| `target_tool` | TargetTool | `web_search_prime` | z.ai tool called; never advertised. |
| `target_param` | TargetParam | `search_query` | z.ai param sent; forced to `search_query` if empty. |
| `log_level` | LogLevel | `info` | `debug\|info\|warn\|error`. |

### BREAKING CHANGE (architecture/codebase_patterns.md §7; config.go doc comment) — MUST be called out prominently:
- v1 JSON key **`aliases`** → v2 JSON key **`query_aliases`**. Same `[]string`
  semantics. **Operators with a v1 config file MUST rename the key
  `aliases` → `query_aliases`** or it is silently ignored (unknown fields are
  dropped), and extraction falls back to the built-in alias list anyway — but the
  operator's customizations are lost. (The current on-disk `config.example.json`
  still uses the stale `aliases` key; it is rewritten in the parallel P1.M5.T4.S2.)
- New v2 fields with no v1 equivalent: `tools`, `canonical_tool`,
  `canonical_param`, `optional_aliases`, `target_tool`.

### Env overrides (applied AFTER file load; highest precedence; empty values ignored):
- `WSPF_CONFIG` — config file path, used **verbatim**. A missing named file is a
  **hard error** (exit non-zero), NOT a silent fallback to defaults.
- `WSPF_UPSTREAM` — overrides `upstream`.
- `WSPF_LISTEN` — overrides `listen`.
- `WSPF_LOG_LEVEL` — overrides `log_level`.
- NO env override for: `path`, `tools`, `canonical_tool`, `canonical_param`,
  `query_aliases`, `optional_aliases`, `target_tool`, `target_param`.

### Config discovery precedence (config.go `resolveConfigPath`):
1. `WSPF_CONFIG` if set (verbatim; missing = hard error).
2. Else first EXISTING of:
   - `./web-search-prime-fixer.json`
   - `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json` (defaults to
     `~/.config/web-search-prime-fixer/config.json` on Linux/macOS; resolved
     portably via `os.UserConfigDir`).
3. Else no file → built-in defaults (server runs with zero configuration).

### Validation (config.go `ResolveConfig`; on failure logs + exits non-zero):
- `listen` parses as host:port (`net.SplitHostPort`).
- `upstream` is an absolute URL (`url.Parse` + `IsAbs`).
- `tools` is non-empty AND contains `canonical_tool`.
- No `tools` entry equals `target_tool` (never advertise z.ai's real name).
- Unknown JSON fields ignored. No credential fields in Config (Authorization is a
  request header, never owned — so startup logging cannot leak a secret).

## 8. Non-goals (PRD §3 — revised; the proxy-era "do not normalize" non-goals are DROPPED)

The new job is explicitly to normalize. Non-goals:
- Not a general search engine (delegates to z.ai `web_search_prime`; no
  synthesize/rank/cache/invent).
- Not a general-purpose or open proxy (binds 127.0.0.1; dials only configured upstream).
- No credential ownership (forwards client Authorization; holds no key).
- No retry / rate-limiting / caching of upstream calls (one delegated call per
  inbound; surface upstream errors honestly).
- No truncation of the query text (extracted query forwarded verbatim).
- No multi-tool semantics (every advertised tool does the same one thing).
- No z.ai-branded names advertised (`web_search_prime` never exposed).
- Not responsible for other MCP servers' tool-name collisions under
  `toolPrefix:"none"` (the operator owns that).

## 9. Stale v1 framing that MUST be entirely gone from the new README

Grep targets that MUST return ZERO matches in the rewritten README (no false
positives — none of these phrases appear legitimately in v2):
- `transparent proxy`
- `renames the` (v1 "renames the query parameter") / `rename a configured alias`
- `Go standard library only` / `no external dependencies` / `no `go.sum`` /
  `no network access is needed to build`
- `byte-for-byte` / `forwarded byte-for-byte`
- `prepends a` / `prepended` (v1 prepended the warning to the result; v2 appends)
- `search_query" is not a valid parameter` (v1 warning text)
- `ignored "query"` (v1 warning text)
- `"web-search-prime":` as the mcpServers key name (v1 config used the server name
  `web-search-prime`; v2 uses `web_search` and is about `toolPrefix:none`)
- `passes through untouched` / `passes through unchanged` (v1 passthrough framing)
- `forwarded verbatim` describing the WHOLE request/response (v2 only forwards the
  Authorization header + one delegated call, not the bytes)

## 10. Tests (unchanged from v1 shape, still accurate)

- `go test ./...`
- Suite uses httptest in-process fakes and golden SSE fixtures. No network calls,
  no credentials. (v2 adds extract/teach/upstream/server E2E suites.)

## 11. Scope boundaries (CRITICAL — what this PRP does NOT touch)

- This PRP rewrites **README.md ONLY**.
- `config.example.json` and `doc.go` are the **parallel P1.M5.T4.S2** item — do
  NOT edit them here. The README may *reference* config.example.json (the updated
  v2 version is produced by T4.S2), but must not duplicate its full contents.
- No Go source edits, no go.mod/go.sum changes, no PRD.md edits.
