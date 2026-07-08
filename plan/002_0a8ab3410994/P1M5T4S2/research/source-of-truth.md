# P1.M5.T4.S2 — Source of Truth (config.example.json + doc.go)

Every value the two deliverables must state is copied here from the on-disk source
of truth (`config.go`, `tools.go`, `teach.go`, `go.mod`) and the PRD. Copy values
from this file; do not re-derive.

---

## 1. The two deliverables (item contract)

1. **`config.example.json`** — REWRITE to the full v2 schema matching
   `DefaultConfig()` EXACTLY. Valid JSON. **No comments** (config.go parses with
   `json.Unmarshal`, which rejects `//` and `/* */`; the v1 file was comment-free
   and v2 must stay comment-free). 11 fields.
2. **`doc.go`** — REWRITE the package comment to describe the **normalizing MCP
   server**. Remove ALL transparent-proxy / do-not-normalize language. Keep the
   Go-doc format: a `//` comment block immediately preceding `package main`.

---

## 2. ⚠️ CRITICAL DISCREPANCY — query_aliases has 14 entries, NOT 13

The item description says *"query_aliases (13 entries)"*. That count is WRONG.
Both `DefaultConfig()` (config.go) and PRD §18.2 list **14** entries:

```
query, search_query, q, search, searchQuery, search_term, term,
text, input, prompt, question, keywords, topic, searchString
```

The item contract explicitly says config.example.json must **"match DefaultConfig()
exactly"**. Therefore config.example.json MUST contain 14 entries — NOT 13.
`DefaultConfig()` is the source of truth; the "13" in the item text is a counting
error. **Use 14.** Verify with `python3 -c "import json;print(len(json.load(open('config.example.json'))['query_aliases']))"` → 14.

---

## 3. config.example.json — EXACT target content (matches DefaultConfig())

Field order follows the struct declaration order in config.go (= PRD §18.1):

| JSON key            | Value (from DefaultConfig)                                            |
|---------------------|-----------------------------------------------------------------------|
| `upstream`          | `https://api.z.ai/api/mcp/web_search_prime/mcp`                      |
| `listen`            | `127.0.0.1:8787`                                                      |
| `path`              | `/mcp`                                                                |
| `tools`             | `["web_search"]`                                                      |
| `canonical_tool`    | `web_search`                                                          |
| `canonical_param`   | `query`                                                               |
| `query_aliases`     | the 14-entry list (§2 above)                                          |
| `optional_aliases`  | map: `location`, `content_size`, `search_recency_filter` (3 keys)     |
| `target_tool`       | `web_search_prime`                                                    |
| `target_param`      | `search_query`                                                        |
| `log_level`         | `info`                                                                |

The `optional_aliases` map, expanded:
```json
"optional_aliases": {
  "location": ["country", "region"],
  "content_size": ["size", "contentSize", "detail"],
  "search_recency_filter": ["recency", "freshness", "time_filter", "date_filter"]
}
```

**The exact ready-to-write JSON file body** (copy verbatim — it round-trips through
`json.Unmarshal` into `DefaultConfig()` and reproduces it field-for-field):

```json
{
  "upstream": "https://api.z.ai/api/mcp/web_search_prime/mcp",
  "listen": "127.0.0.1:8787",
  "path": "/mcp",
  "tools": [
    "web_search"
  ],
  "canonical_tool": "web_search",
  "canonical_param": "query",
  "query_aliases": [
    "query",
    "search_query",
    "q",
    "search",
    "searchQuery",
    "search_term",
    "term",
    "text",
    "input",
    "prompt",
    "question",
    "keywords",
    "topic",
    "searchString"
  ],
  "optional_aliases": {
    "location": ["country", "region"],
    "content_size": ["size", "contentSize", "detail"],
    "search_recency_filter": ["recency", "freshness", "time_filter", "date_filter"]
  },
  "target_tool": "web_search_prime",
  "target_param": "search_query",
  "log_level": "info"
}
```

### Why this exact body
- `json.Unmarshal(data, &cfg)` overlays the file onto `DefaultConfig()`. A file with
  these exact values reproduces the defaults, so config.example.json is a TRUE example
  of "the server runs with no config file at all" (PRD §18.2 closing line).
- Indentation is cosmetic but 2-space is the existing v1 style (verify: the v1 file
  used 2-space). Keep 2-space for consistency.
- Arrays may be one-line (`["web_search"]`) or multi-line; multi-line for
  `query_aliases` reads better. Either parses identically.
- Trailing newline: the v1 file had one. Keep a single trailing `\n`.

---

## 4. doc.go — the v1 text being replaced (what MUST GO)

Current doc.go (the STALE v1 package comment — every claim is wrong for v2):

```go
// Package main implements web-search-prime-fixer, a local reverse proxy for
// the z.ai web-search-prime MCP endpoint. It binds to 127.0.0.1 (local only)
// and uses the Go standard library exclusively.
//
// On a tools/call, argument keys found in the alias list are renamed to the
// canonical "search_query". When "search_query" is absent, the first present
// alias in config order is promoted and the remaining aliases are dropped.
// When "search_query" is present, all aliases are dropped and the canonical
// value wins. When a rewrite is applied, a one-line warning is injected into
// the tools/call result so the caller learns the correct name. Every other
// argument and request passes through untouched. The default alias list is
// query, q, search, searchQuery, search_term; the target is always
// "search_query".
//
// Non-goals: it does not normalize other parameters (location, content_size,
// search_recency_filter, or any enum), does not warn about or drop unsupported
// parameters, does not rewrite the tool schema, and does not manage API keys,
// retry, rate-limit, or cache.
package main
```

### Stale phrases that must DISAPPEAR from doc.go (grep targets, 0 matches required)
- "reverse proxy" / "transparent proxy" / "local reverse proxy"
- "uses the Go standard library exclusively" / "standard library"
- "passes through untouched" / "passes through"
- "renamed to the canonical" / "the canonical value wins"
- "the canonical "search_query"" (search_query is z.ai's PARAM, not our canonical surface — our canonical is web_search/query)
- "a one-line warning is injected" (v2 APPENDS after results, never injects/prepends)
- "does not normalize" (v2's WHOLE JOB is to normalize — this non-goal was explicitly DROPPED, PRD §3)
- "does not warn about or drop unsupported parameters"
- "does not rewrite the tool schema"
- "the default alias list is query, q, search, searchQuery, search_term" (v2 has 14, and the key is query_aliases)
- "the target is always "search_query"" (target PARAM is search_query, but framing is proxy-era)

The v1 non-goals list forbade normalization — that is the EXACT thing v2 now does.
PRD §3: "The proxy-era non-goals that forbade normalizing arguments are dropped.
The new job is explicitly to normalize."

---

## 5. doc.go — the v2 content it must convey (PRD §0/§3/§5.2/§9/§12/§18)

The new package comment must describe the **normalizing MCP server**. It should
cover (in Go-doc prose, not bullet points — match the v1 comment's prose style):

1. **What it is**: a normalizing MCP server (NOT a proxy). Built on the official
   Go MCP SDK (`github.com/modelcontextprotocol/go-sdk`, PRD §13). Owns the
   JSON-RPC surface and delegates only the actual search. Binds 127.0.0.1 (local only).
2. **One tool**: advertises essentially one tool, `web_search` (PRD §9). The z.ai
   tool name `web_search_prime` is NEVER advertised to the agent.
3. **Query extraction**: on a tools/call it extracts the intended search query from
   ARBITRARY input (extract.go — scalar/string/array/object-alias/inference, PRD §10)
   rather than requiring a fixed parameter.
4. **Delegation**: delegates ONE clean call to z.ai's `web_search_prime`, forwarding
   the client's Authorization header but holding no key of its own (PRD §11, §17, FR-7).
5. **Teaching signal**: when usage was non-canonical, appends a warning (with a
   correct-usage example) AFTER the results — never instead of them, never prepended
   (PRD §12). The canonical call (`web_search` with `{ "query": "..." }`) gets no warning.
6. **Revised non-goals** (PRD §3, verbatim intent): not a general search engine;
   not a general-purpose/open proxy; no credential ownership; no retry/rate-limit/
   caching of upstream calls; no truncation of the query text; no multi-tool semantics
   (every advertised tool does the same one search); no z.ai-branded names advertised;
   not responsible for other MCP servers' tool-name collisions when the client runs
   with `toolPrefix: "none"`.

### Suggested doc.go structure (prose paragraphs, mirror the v1's 3-paragraph shape)
- **Para 1 — what it is**: Package main implements web-search-prime-fixer, a local
  normalizing MCP server for the z.ai web-search-prime endpoint. It is built on the
  official Go MCP SDK, binds to 127.0.0.1 (local only), advertises a single tool
  (web_search), and acts as an MCP client to z.ai rather than forwarding the agent's
  bytes. It is NOT a transparent/open proxy.
- **Para 2 — the tools/call flow**: on a tools/call it extracts the search query from
  whatever arguments the agent sent (any key, nested object, bare string, or array —
  PRD §10), delegates one clean call to z.ai's web_search_prime forwarding the
  client's Authorization header, and returns the results. When the call was
  non-canonical it APPENDS a teaching warning (with a correct-usage example) AFTER the
  results, never instead of them; the canonical call (web_search with { "query": "..." })
  gets no warning. When no query can be extracted, it returns the warning immediately
  with no upstream call.
- **Para 3 — non-goals**: the revised non-goals (PRD §3): not a search engine; not an
  open proxy; no credential ownership; no retry/rate-limit/cache; no query truncation;
  no multi-tool semantics; web_search_prime never advertised; not responsible for other
  servers' name collisions under toolPrefix:none.

Keep it concise (the v1 was ~16 comment lines; aim similar). End with `package main`
on its own line, immediately after the comment block (no blank line between the last
`//` line and `package main`, per Go doc convention — though a single blank is also
accepted by gofmt; the v1 had NO blank line, so match that).

---

## 6. Go-doc formatting rules for doc.go (gotchas)

- A package comment is a `//`-comment block immediately preceding the `package`
  clause. The FIRST line should begin with `// Package main ...` (Go convention;
  `go doc` renders it).
- `gofmt` is the authority. Run `gofmt -l doc.go` — if it prints the filename, the
  file needs formatting; run `gofmt -w doc.go`. The deliverable MUST be gofmt-clean
  (it is also a gate in the P1.M5.T4.S3 quality gate).
- `go vet ./...` must still pass (doc.go is in `package main`; vet parses it).
- No HTML/Markdown in package comments — it renders as plain text via `go doc`.
- The EM DASH (—) is fine in a comment if used (teach.go uses it), but doc.go need
  not quote the warning verbatim; a plain description is enough. Prefer ASCII in the
  package comment for portability unless quoting.

---

## 7. config.example.json formatting rules (gotchas)

- **No comments.** `json.Unmarshal` (config.go) rejects `//` and `/* */`. The v1 file
  was comment-free; v2 MUST stay comment-free. Do NOT add `// z.ai endpoint` style notes.
- Valid JSON only: double quotes on all keys and string values; no trailing commas.
- Verify after writing: `python3 -m json.tool config.example.json` (or
  `go run .` style — but simplest is `python3 -c "import json;json.load(open('config.example.json'))"`).
- The file should reproduce `DefaultConfig()` exactly, so this is also the "documented
  example" referenced by README.md (P1.M5.T4.S1) and the canonical ready-to-copy sample.
- No credential keys (Authorization is a header, never config — PRD §13). The file must
  contain NO api_key / token / authorization fields.

---

## 8. References (for the PRP's Documentation & References block)

- `config.go` — Config struct (PRD §18.1) + DefaultConfig (PRD §18.2) + json tags.
  THE source of truth for every key and value in config.example.json.
- `doc.go` — the v1 file being replaced (§4 above has full text).
- `tools.go` — canonicalDescription/aliasDescription (the v2 tool surface doc.go names).
- `teach.go` — the append-after-results behavior doc.go describes (warningText etc.).
- `go.mod` — confirms the Go MCP SDK dependency (doc.go now correctly names it).
- `PRD.md` §0/§3/§5.2/§9/§12/§18 — the v2 framing, non-goals, flow, tool surface,
  teaching signal, config schema.
- `plan/002_0a8ab3410994/architecture/codebase_patterns.md` §7 — the v1→v2 key mapping
  (aliases→query_aliases; the 5 new fields).
- `plan/002_0a8ab3410994/architecture/system_context.md` — confirms the v1 "transparent
  proxy / stdlib-only" framing is STALE and must be replaced; SDK is the sole dependency.
