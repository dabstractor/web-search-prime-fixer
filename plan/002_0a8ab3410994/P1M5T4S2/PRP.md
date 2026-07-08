name: "P1.M5.T4.S2 — Update config.example.json to v2 schema + rewrite doc.go package comment (PRD §18, §0, §3, §9)"
description: |

  TWO DOCS-ONLY deliverables, both STALE for v2 and the last un-synced changeset-level
  surfaces (parallel to P1.M5.T4.S1's README):

    1. config.example.json — REWRITE to the full v2 schema matching DefaultConfig()
       EXACTLY. The on-disk file is v1: it has only 6 keys (upstream, listen, path,
       "aliases" [v1 name, 5 entries], target_param, log_level) and is MISSING the 5
       new v2 fields (tools, canonical_tool, canonical_param, optional_aliases,
       target_tool). It must become the 11-field v2 example (PRD §18.1/§18.2). Valid
       JSON, NO comments (config.go parses with json.Unmarshal which rejects // and /* */).
    2. doc.go — REWRITE the package comment. The on-disk comment describes a "local
       reverse proxy" that "uses the Go standard library exclusively" and whose stated
       non-goal is that it "does not normalize" arguments. That framing is STALE and
       contradicts the entire v2 architecture (PRD §0: the proxy-era do-not-normalize
       non-goals are DROPPED; v2's job IS to normalize). Replace it with a comment that
       describes the NORMALIZING MCP SERVER: one tool (web_search), query extraction
       from arbitrary input, delegation to z.ai's web_search_prime, append-after-results
       teaching warning, and the REVISED non-goals (PRD §0/§3/§9). Keep Go-doc format.

  DOCS-ONLY: no Go source edits (doc.go is a comment-only file in package main — its
  ONLY non-comment line is `package main`), no go.mod/go.sum, no PRD.md, no README.md
  (README is the parallel P1.M5.T4.S1).

  ⚠️ ITEM-DESCRIPTION COUNTING ERROR (resolved): the item text says "query_aliases
  (13 entries)". DefaultConfig() and PRD §18.2 both list 14. The contract says
  config.example.json must match DefaultConfig() EXACTLY, so it MUST contain 14
  entries — NOT 13. DefaultConfig() is the source of truth; the "13" is a typo.
  Use 14. (research/source-of-truth.md §2.)

  INPUT (treat as CONTRACT — already on disk):
    config.go  : v2 Config struct (PRD §18.1) + DefaultConfig (PRD §18.2) + json tags.
                 THE source of truth for every key/value in config.example.json.
    doc.go     : the v1 comment being replaced (research/source-of-truth.md §4 has full text).
    tools.go   : canonicalDescription/aliasDescription — the v2 tool surface doc.go names.
    teach.go   : append-after-results warning behavior doc.go describes (warningText etc.).
    go.mod     : confirms the Go MCP SDK dependency (doc.go now correctly names it).
    PRD.md §18/§0/§3/§9/§5.2/§12 : config schema, what-changed, non-goals, tool surface,
                 flow, teaching signal.

  OUTPUT: rewritten config.example.json (11-field v2) + rewritten doc.go package comment.

---

## Goal

**Feature Goal**: Two synchronized changeset-level docs that no longer describe the
  dead v1 proxy. `config.example.json` becomes a complete, valid, comment-free v2
  example that reproduces `DefaultConfig()` field-for-field (11 fields, 14 query
  aliases). `doc.go`'s package comment describes the v2 normalizing MCP server — one
  tool, query extraction from arbitrary input, delegation to z.ai, append-after-results
  teaching signal, and the revised non-goals — with zero remaining proxy/stdlib/
  do-not-normalize language.

**Deliverable**:
  - `config.example.json` — full rewrite (v2 schema, valid JSON, no comments).
  - `doc.go` — full rewrite of the package comment (prose `//` block ending in
    `package main`; the ONLY non-comment line in the file stays `package main`).

**Success Definition**:
  - `python3 -m json.tool config.example.json >/dev/null` succeeds (valid JSON).
  - `json.Unmarshal(config.example.json)` over `DefaultConfig()` reproduces the defaults
    (all 11 keys present; query_aliases has 14 entries; optional_aliases has 3 keys).
  - `gofmt -l doc.go` prints nothing (doc.go is gofmt-clean).
  - `go vet ./...` passes (doc.go is in package main and must parse).
  - Every stale-phrase grep in the Validation Loop returns ZERO matches in doc.go.
  - config.example.json contains NO comments and NO credential/api_key fields.
  - doc.go names the normalizing MCP server, the Go MCP SDK, the single web_search tool,
    query extraction, z.ai delegation, the append-after-results warning, and the revised
    non-goals (PRD §3).

## User Persona

**Target User**: The operator-developer (same as the README). They copy
  config.example.json as the starting point for their own config, and they read
  `doc.go` via `go doc` / an IDE to understand the package at a glance.
**Use Case**: (1) `cp config.example.json web-search-prime-fixer.json && $EDITOR ...`
  to customize; (2) `go doc` to recall what the binary is.
**User Journey**: copy example → tweak (e.g. add the `search` alias) → run; or run
  `go doc .` to see the v2 architecture summary without opening PRD.md.
**Pain Points Addressed**: the v1 example is a footgun (its `aliases` key is silently
  ignored by v2 config loading — unknown fields dropped — so an operator copying it
  loses their alias list). The v1 doc.go actively lies to anyone reading it
  (says "reverse proxy", "standard library only", "does not normalize" — all false).

## Why

- Implements the **Mode B changeset-level documentation** for the two remaining stale
  surfaces (PRD §14 file layout lists both; M5 is the doc sweep). The v2 architecture
  shipped across M1–M5; config.example.json and doc.go are the last files still
  describing the dead v1 proxy.
- config.example.json is the **authoritative example** README.md (P1.M5.T4.S1) points
  operators at ("see config.example.json for a ready-to-copy example"). It MUST reflect
  the v2 schema or the README's reference is a broken link in spirit.
- doc.go is the **`go doc` entry point** for the package. A maintainer running
  `go doc web-search-prime-fixer` today sees the v1 proxy story — actively misleading.
- The `aliases`→`query_aliases` rename is a **breaking config change**
  (architecture/codebase_patterns.md §7, config.go doc comment). config.example.json
  is where an operator SEES the correct v2 key names in context.
- This is the last item before the P1.M5.T4.S3 quality gate (`go vet`/`go test`/`go
  build`). Both deliverables must be gofmt/vet-clean or that gate fails.

## What

### Deliverable 1 — config.example.json (full rewrite)
A valid, comment-free JSON object with exactly these 11 keys, matching
`DefaultConfig()` (config.go) field-for-field (field order follows the struct =
PRD §18.1):

| JSON key            | Value                                                                 |
|---------------------|-----------------------------------------------------------------------|
| `upstream`          | `https://api.z.ai/api/mcp/web_search_prime/mcp`                      |
| `listen`            | `127.0.0.1:8787`                                                      |
| `path`              | `/mcp`                                                                |
| `tools`             | `["web_search"]`                                                      |
| `canonical_tool`    | `web_search`                                                          |
| `canonical_param`   | `query`                                                               |
| `query_aliases`     | the **14**-entry list (NOT 13 — see Known Gotchas)                    |
| `optional_aliases`  | 3-key map: location / content_size / search_recency_filter            |
| `target_tool`       | `web_search_prime`                                                    |
| `target_param`      | `search_query`                                                        |
| `log_level`         | `info`                                                                |

The `query_aliases` array, in order:
`query, search_query, q, search, searchQuery, search_term, term, text, input, prompt, question, keywords, topic, searchString`.

The `optional_aliases` map:
```json
{
  "location": ["country", "region"],
  "content_size": ["size", "contentSize", "detail"],
  "search_recency_filter": ["recency", "freshness", "time_filter", "date_filter"]
}
```

Constraints: 2-space indentation (match v1 style); valid JSON (double-quoted keys/strings,
no trailing commas); NO comments; single trailing newline; NO credential/api_key fields.
The exact ready-to-write body is in research/source-of-truth.md §3 — copy it verbatim.

### Deliverable 2 — doc.go (rewrite package comment)
Replace the entire v1 comment block with a prose `//`-comment block describing the
v2 normalizing MCP server, immediately followed by `package main` (the file's only
non-comment line). Suggested 3-paragraph structure mirroring the v1 comment's shape:

- **Para 1 — what it is**: a local **normalizing MCP server** for the z.ai
  web-search-prime endpoint; built on the **official Go MCP SDK**; binds 127.0.0.1
  (local only); advertises a **single tool, web_search**; acts as an MCP **client**
  to z.ai rather than forwarding the agent's bytes; explicitly NOT a transparent/open proxy.
- **Para 2 — the tools/call flow**: extracts the search query from whatever
  arguments the agent sent (any key, nested object, bare string, or array — PRD §10),
  delegates one clean call to z.ai's `web_search_prime` forwarding the client's
  Authorization header (holding no key of its own), and returns the results; when the
  call was non-canonical it APPENDS a teaching warning (with a correct-usage example)
  AFTER the results (never instead of them, never prepended); the canonical call
  (`web_search` with `{ "query": "..." }`) gets no warning; when no query can be
  extracted it returns the warning immediately with no upstream call.
- **Para 3 — non-goals** (PRD §3): not a general search engine; not a general-purpose
  or open proxy; no credential ownership; no retry/rate-limit/caching of upstream calls;
  no truncation of the query text; no multi-tool semantics (every advertised tool does
  the same one search); `web_search_prime` is never advertised to the agent; not
  responsible for other MCP servers' tool-name collisions when the client runs with
  `toolPrefix: "none"`.

Keep it concise (~16 comment lines, similar to v1). Begin the first line with
`// Package main ...` (Go doc convention). No blank line between the last `//` line
and `package main` (match v1).

### Success Criteria
- [ ] config.example.json: `python3 -m json.tool config.example.json` succeeds.
- [ ] config.example.json: all 11 v2 keys present; query_aliases == 14 entries;
      optional_aliases == 3 keys (location/content_size/search_recency_filter).
- [ ] config.example.json: NO comments; no `aliases` key; no credential fields.
- [ ] config.example.json: byte-for-byte values match DefaultConfig() (research §3).
- [ ] doc.go: `gofmt -l doc.go` prints nothing; `go vet ./...` passes.
- [ ] doc.go: every stale-phrase grep (research §4 / Validation Loop) returns 0.
- [ ] doc.go: names "normalizing MCP server", "Go MCP SDK", single "web_search" tool,
      query extraction, z.ai `web_search_prime` delegation, append-after-results warning,
      and the revised non-goals.
- [ ] doc.go: only non-comment line is `package main`.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can complete both deliverables from this PRP
alone because: (a) the EXACT byte-for-byte target content of config.example.json is
given in research/source-of-truth.md §3 (copy verbatim); (b) the exact v1 text being
removed from doc.go is given in research §4 with enumerated stale-phrase grep targets;
(c) the exact v2 content doc.go must convey is specified paragraph-by-paragraph from
PRD §0/§3/§5.2/§9/§12; (d) the formatting rules (valid JSON/no comments; gofmt-clean
Go-doc) are explicit; (e) the validation gates are concrete executable commands
(json.tool, gofmt -l, go vet, python count checks); (f) the scope boundary (these two
files only; README/go.mod/PRD untouched) is explicit.

### Documentation & References

```yaml
# MUST READ — single source of truth for every value both files state.
- docfile: plan/002_0a8ab3410994/P1M5T4S2/research/source-of-truth.md
  why: the exact ready-to-write config.example.json body (§3, copy verbatim); the full
        v1 doc.go text being replaced (§4); the v2 content doc.go must convey (§5);
        the 14-vs-13 query_aliases resolution (§2); gofmt/json formatting rules (§6/§7).
  critical: query_aliases has 14 entries (NOT 13 as the item text says). config.example.json
        MUST contain 14 to match DefaultConfig() exactly.

# MUST READ — the authoritative v2 config source.
- file: config.go
  section: "Config struct (PRD §18.1) + DefaultConfig (PRD §18.2) + json struct tags"
  why: every JSON key, default value, and field order for config.example.json. Cross-check
        the body in research §3 against DefaultConfig() before writing.

# MUST READ — the file being rewritten (deliverable 2).
- file: doc.go
  why: the current v1 package comment (reverse proxy / stdlib-only / do-not-normalize).
        REPLACE entirely. research §4 has the full text + the stale-phrase grep targets.

# MUST READ — the v2 behavior doc.go describes (verify against these).
- file: tools.go
  section: "buildTools + canonicalDescription + aliasDescription"
  why: the v2 tool surface (one canonical web_search; terse aliases; web_search_prime never
        advertised) that doc.go para 1/2 names.
- file: teach.go
  section: "shouldWarn + warningText + noQueryWarningText + appendWarning"
  why: the append-after-results teaching behavior doc.go para 2 describes (APPEND after
        results, never prepend/inject; canonical call gets no warning).
- file: main.go
  section: "package main + bootstrap + /healthz + graceful shutdown"
  why: confirms package name is `main` and the server is built on the Go MCP SDK
        (`github.com/modelcontextprotocol/go-sdk/mcp`), which doc.go names.
- file: go.mod
  why: confirms the sole dependency is github.com/modelcontextprotocol/go-sdk v1.6.1 —
        the basis for doc.go's "built on the official Go MCP SDK" claim (the v1 "standard
        library exclusively" claim is FALSE).

# MUST READ — the PRD sections both files mirror.
- file: PRD.md
  section: "§18 Configuration (config.go) — §18.1 Schema + §18.2 Defaults"
  why: the v2 config schema and defaults (the config.example.json source of truth).
- file: PRD.md
  section: "§0 What changed since the first revision + §3 Non-goals (revised)"
  why: doc.go's framing + non-goals come from here. §0 explicitly states the
        do-not-normalize non-goals were DROPPED and the new job IS to normalize.
- file: PRD.md
  section: "§5.2 Part B server rewrite + §9 Advertised tools + §12 Teaching signal"
  why: the tools/call flow (extract → delegate → append-warning) doc.go para 2 describes.

# READ — confirms the breaking change and stale framing.
- docfile: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  section: "§7 JSON key mapping (v1 → v2 config breaking change)"
  why: aliases→query_aliases rename + the 5 new v2 fields. config.example.json must use
        only v2 keys.
- docfile: plan/002_0a8ab3410994/architecture/system_context.md
  why: confirms "the v1 ... transparent proxy that failed in production" framing is STALE
        and "the v1 stdlib-only / no go.sum constraint is explicitly relinquished".
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  config.example.json   # v1 (6 keys, "aliases", MISSING 5 v2 fields) — REWRITE THIS (deliverable 1)
  doc.go                # v1 package comment (reverse proxy / stdlib / do-not-normalize) — REWRITE THIS (deliverable 2)
  config.go             # v2 source of truth: Config + DefaultConfig + json tags (READ)
  tools.go              # canonicalDescription / aliasDescription — v2 tool surface (READ)
  teach.go              # append-after-results warning behavior (READ)
  main.go               # package main; built on Go MCP SDK (READ)
  go.mod                # sole dep go-sdk v1.6.1 (READ — doc.go names it)
  README.md             # owned by parallel P1.M5.T4.S1 — DO NOT EDIT
  *.go + *_test.go      # shipped v2 implementation + suites (untouched)
  PRD.md                # READ-ONLY; mirror §18/§0/§3/§9
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
config.example.json   # REWRITE (full replacement): the 11-field v2 example matching
                      #   DefaultConfig() exactly (14 query aliases; 3-key optional_aliases).
                      #   Valid JSON; no comments; no credential fields; 2-space indent.
doc.go                # REWRITE (full comment replacement): a prose // package comment
                      #   describing the normalizing MCP server — one tool (web_search),
                      #   query extraction from arbitrary input, delegation to z.ai's
                      #   web_search_prime, append-after-results teaching warning, revised
                      #   non-goals (PRD §3). Only non-comment line stays `package main`.
# No other file is created or modified by this item.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — query_aliases has 14 ENTRIES, NOT 13. The item description says "13" but
DefaultConfig() and PRD §18.2 both have 14: query, search_query, q, search, searchQuery,
search_term, term, text, input, prompt, question, keywords, topic, searchString. The
contract says config.example.json must match DefaultConfig() EXACTLY, so use 14. Verify:
`python3 -c "import json;assert len(json.load(open('config.example.json'))['query_aliases'])==14"`.

CRITICAL — NO COMMENTS in config.example.json. config.go parses with json.Unmarshal,
which rejects // and /* */. The v1 file was comment-free; v2 MUST stay comment-free.
Do NOT add `// z.ai endpoint` annotations. A stray comment makes LoadConfig error out
and the P1.M5.T4.S3 quality gate (go test ./...) fail.

CRITICAL — config.example.json must NOT carry the v1 "aliases" key. The v2 key is
"query_aliases". Leaving "aliases" in would be silently dropped (unknown fields ignored)
and is exactly the v1 footgun this rewrite removes.

CRITICAL — doc.go is NOT a normal source edit. Its ONLY non-comment line is `package
main`. The deliverable is a COMMENT rewrite. Do not add imports, functions, or any code.
The file must remain a pure package-comment file (it currently is; keep it so).

CRITICAL — the v1 doc.go non-goals list is the OPPOSITE of v2. v1 said "does not normalize
other parameters ... does not warn about or drop unsupported parameters". v2's WHOLE JOB
is to normalize (PRD §0: "The proxy-era non-goals that forbade normalizing arguments are
dropped. The new job is explicitly to normalize."). Every "does not normalize" phrase
must be GONE.

CRITICAL — doc.go must NOT claim "standard library exclusively". v2 depends on the Go
MCP SDK (go.mod: github.com/modelcontextprotocol/go-sdk v1.6.1). The stdlib-only claim
was relinquished (PRD §13, architecture/system_context.md). doc.go must instead NAME the
SDK ("built on the official Go MCP SDK").

CRITICAL — doc.go must NOT say "passes through untouched" or "reverse proxy" or
"transparent proxy". v2 is a real MCP server that owns the JSON-RPC surface and acts as
an MCP CLIENT to z.ai (PRD §5.2). It rewrites/normalizes everything; nothing passes
through verbatim.

CRITICAL — the warning is APPENDED AFTER results, never prepended/injected. v1 doc.go
said "a one-line warning is injected into the tools/call result". v2 APPENDS (teach.go
appendWarning) and only AFTER a successful search (PRD §12.3). State "appends after the
results", never "injects"/"prepends".

CRITICAL — teach the canonical surface web_search / query, NOT search_query. v1 doc.go
called search_query "the canonical" — it is z.ai's INTERNAL param (TargetParam), which
the agent never sees. doc.go must describe the canonical call as web_search with
{ "query": "..." }.

CRITICAL — web_search_prime is NEVER advertised to the agent. doc.go may name it (as the
z.ai upstream tool the server delegates to) but must state it is never advertised
(non-goal, PRD §3/§9.3). It is also forbidden in cfg.Tools (config validation rejects it).

GOTCHA — gofmt is the authority for doc.go. Run `gofmt -l doc.go`; if it prints the
filename, run `gofmt -w doc.go`. A package comment immediately precedes `package main`
with NO blank line between the last // line and the clause (match the v1 file). The first
comment line should start "// Package main ...".

GOTCHA — config.example.json indentation/style. Use 2-space indent (the v1 file did).
Arrays (tools, query_aliases) may be inline or multiline; multiline query_aliases reads
better and matches the struct's line-broken layout. Either parses identically. One
trailing newline.

GOTCHA — no credential fields anywhere. Authorization is a request header forwarded
verbatim (PRD §13, §17, FR-7); Config carries no key. config.example.json must contain
NO api_key/token/authorization/bearer fields (the v1 file correctly had none; keep it so).

GOTCHA — scope. This item is config.example.json + doc.go ONLY. README.md is the parallel
P1.M5.T4.S1 (DO NOT EDIT). No other .go source, no go.mod/go.sum, no PRD.md, no test files.
```

## Implementation Blueprint

### Data models and structure

None. Both deliverables are static text: one JSON file and one Go package comment. The
"data" is the verbatim values in research/source-of-truth.md §3 (config body) and §4/§5
(doc.go v1-out / v2-in), each copied from the on-disk source of truth (config.go,
PRD §18.2/§0/§3).

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) confirm the source of truth is on disk
  - RUN: test -f config.go && test -f doc.go && test -f config.example.json \
        && test -f go.mod && grep -q "func DefaultConfig" config.go \
        && grep -q "QueryAliases" config.go && grep -q "modelcontextprotocol/go-sdk" go.mod \
        && echo OK
  - EXPECT: OK. If any fail, the v2 sources are missing — STOP (prerequisite not met).

Task 1: REWRITE config.example.json to the 11-field v2 schema
  - FILE: config.example.json (overwrite).
  - COPY the exact JSON body from research/source-of-truth.md §3 VERBATIM. It reproduces
        DefaultConfig() field-for-field (config.go). Field order = struct order = PRD §18.1.
  - FIELDS (11): upstream, listen, path, tools (["web_search"]), canonical_tool
        ("web_search"), canonical_param ("query"), query_aliases (14 entries — NOT 13),
        optional_aliases (3-key map), target_tool ("web_search_prime"), target_param
        ("search_query"), log_level ("info").
  - RULES: valid JSON; 2-space indent; NO comments; no trailing commas; no "aliases" key;
        no credential fields; single trailing newline.
  - FOLLOW pattern: the v1 file's 2-space style; the config.go json struct tags for key
        names; DefaultConfig() for values.

Task 2: VALIDATE config.example.json (run before doc.go)
  - RUN: python3 -m json.tool config.example.json >/dev/null && echo "valid JSON"
  - RUN: python3 -c "import json;d=json.load(open('config.example.json'));keys=sorted(d);assert keys==['canonical_param','canonical_tool','listen','log_level','optional_aliases','path','query_aliases','target_param','target_tool','tools','upstream'],keys;assert len(d['query_aliases'])==14,(len(d['query_aliases']),'must be 14 NOT 13');assert len(d['optional_aliases'])==3;assert d['tools']==['web_search'];assert 'aliases' not in d;print('schema OK')"
  - RUN: go run . -h 2>/dev/null; ./web-search-prime-fixer -h 2>/dev/null  # optional smoke
  - EXPECT: "valid JSON" + "schema OK". If the count assertion fails you wrote 13 — write 14.

Task 3: REWRITE doc.go — the package comment (normalizing MCP server)
  - FILE: doc.go (overwrite the comment; keep `package main` as the only non-comment line).
  - STRUCTURE: prose // block (3 paragraphs, ~16 lines) immediately preceding `package main`,
        first line "// Package main implements ...". No blank line before `package main`.
  - PARA 1 (what it is): normalizing MCP server for z.ai web-search-prime; built on the
        official Go MCP SDK; binds 127.0.0.1 (local only); advertises a single tool
        (web_search); acts as an MCP client to z.ai, not a byte-forwarding proxy; NOT a
        transparent/open proxy.
  - PARA 2 (the flow): on tools/call extracts the query from arbitrary input (any key /
        nested object / bare string / array), delegates one clean call to z.ai's
        web_search_prime forwarding the client's Authorization header (holds no key),
        returns the results; when non-canonical APPENDS a teaching warning (with a
        correct-usage example) AFTER the results (never instead of them / never prepended);
        the canonical call web_search with { "query": "..." } gets no warning; when no
        query is found, returns the warning immediately with no upstream call.
  - PARA 3 (non-goals, PRD §3): not a search engine; not a general-purpose/open proxy;
        no credential ownership; no retry/rate-limit/caching; no query truncation; no
        multi-tool semantics; web_search_prime never advertised; not responsible for other
        servers' name collisions under toolPrefix:none.
  - FOLLOW pattern: the v1 doc.go's prose // style + "Package main implements ..." opener
        + closing "package main" with no preceding blank line.

Task 4: VALIDATE doc.go (gofmt + vet + stale-phrase grep)
  - RUN: gofmt -l doc.go   # MUST print nothing
  - RUN: go vet ./...      # MUST pass (doc.go is in package main)
  - RUN: the stale-phrase grep loop in Validation Loop Level 2 — every term MUST return 0.
  - EXPECT: gofmt silent; vet clean; all greps "OK (0)".
```

### Implementation Patterns & Key Details

```go
// PATTERN: doc.go is a pure package-comment file. Its entire body is ONE // comment
//   block followed by `package main`. No imports, no funcs, no vars. The deliverable
//   is the comment text. Keep the file structurally identical to the v1 (a comment +
//   the package clause) so it stays a documentation-only file.

// PATTERN: open with "// Package main implements web-search-prime-fixer, a local
//   normalizing MCP server ...". Go's `go doc` renders the first sentence as the
//   synopsis, so lead with the noun phrase that defines the package. Do NOT open with
//   "this is a rewrite of the proxy" — that keeps stale framing alive in the synopsis.

// PATTERN: describe behavior, not file structure, in the package comment. Name what the
//   server DOES (extract, delegate, append-warn), not which file lives where (that is
//   the README's/PRD's job). Mention web_search_prime ONLY as the z.ai tool delegated
//   to + as a never-advertised non-goal; never as something the agent calls.

// GOTCHA: APPEND, never inject/prepend. v1 said "injected"; v2 appends after results
//   (teach.go appendWarning, PRD §12.3). One wrong verb inverts the whole behavior.

// GOTCHA: the canonical surface is web_search / query. v1 called search_query "canonical"
//   — that is z.ai's internal TargetParam. doc.go must frame web_search/{ "query": "..." }
//   as the canonical call and search_query/web_search_prime as upstream-only internals.

// JSON GOTCHA: config.example.json is parsed by json.Unmarshal over DefaultConfig(). A
//   file that exactly equals the defaults is the cleanest example AND the one that lets
//   "the server runs with no config file" (PRD §18.2) read true. Do not invent values.
```

### Integration Points

```yaml
FILES MODIFIED:
  - config.example.json  (full rewrite; deliverable 1)
  - doc.go               (full comment rewrite; deliverable 2; only non-comment line stays `package main`)
FILES NOT TOUCHED (contract):
  - README.md            : owned by parallel P1.M5.T4.S1. (It REFERENCES config.example.json;
                           this rewrite makes that reference correct — do not edit README here.)
  - any *.go source / *_test.go : untouched. doc.go is comment-only; no behavior change.
  - go.mod / go.sum      : untouched.
  - PRD.md               : READ-ONLY.
CONSUMER SEAM:
  - config.example.json is the ready-to-copy example README.md (P1.M5.T4.S1) points at and
        the input the P1.M5.T4.S3 quality gate's `go test ./...` exercises (config_test /
        resolve_test may load example-shaped JSON). Keep keys/values matching DefaultConfig().
  - doc.go is the `go doc` entry point P1.M5.T4.S3's "documentation synced" check relies on;
        it must be gofmt-clean + vet-clean or the quality gate fails.
DATABASE / ROUTES / ENV / CONFIG: none (documentation only). Both files DESCRIBE/EXAMPLE
  the server's config (config.go); they do not change runtime behavior.
```

## Validation Loop

### Level 1: Well-formedness (Immediate Feedback)

```bash
# config.example.json: valid JSON, no comments (json.Unmarshal rejects comments).
python3 -m json.tool config.example.json >/dev/null && echo "config.example.json: valid JSON" \
  || echo "FAIL: invalid JSON (check for comments / trailing commas)"
grep -nE '^\s*//|/\*|\*/' config.example.json && echo "FAIL: comment found" || echo "OK: no comments"

# doc.go: gofmt-clean (gofmt is the authority for Go doc comments).
gofmt -l doc.go && echo "FAIL: doc.go needs gofmt -w" || echo "OK: doc.go gofmt-clean"
# Expected: config.example.json valid + comment-free; doc.go gofmt-clean.
```

### Level 2: Stale v1 framing is GONE in doc.go (core correctness gate)

```bash
# Every phrase below MUST return ZERO matches in doc.go. They have NO legitimate v2 use
# (research §4). If any matches, doc.go still carries v1 framing — rewrite that paragraph.
for term in \
  'reverse proxy' \
  'transparent proxy' \
  'standard library' \
  'passes through' \
  'passes through untouched' \
  'renamed to the canonical' \
  'the canonical value wins' \
  'a one-line warning is injected' \
  'injected into the tools/call' \
  'does not normalize' \
  'does not warn about or drop' \
  'does not rewrite the tool schema' \
  'the default alias list is' ; do
    n=$(grep -c -F "$term" doc.go)
    [ "$n" -eq 0 ] && echo "OK   (0): $term" || echo "FAIL ($n): $term  <-- remove"
done
# Expected: every line "OK (0):". Any "FAIL" = stale v1 text remains; fix before finishing.
```

### Level 3: Verbatim values + content presence

```bash
# (a) config.example.json reproduces DefaultConfig() exactly.
python3 -c "
import json
d=json.load(open('config.example.json'))
assert d['upstream']=='https://api.z.ai/api/mcp/web_search_prime/mcp', d['upstream']
assert d['listen']=='127.0.0.1:8787'
assert d['path']=='/mcp'
assert d['tools']==['web_search']
assert d['canonical_tool']=='web_search'
assert d['canonical_param']=='query'
assert d['query_aliases']==['query','search_query','q','search','searchQuery','search_term','term','text','input','prompt','question','keywords','topic','searchString'], d['query_aliases']
assert d['optional_aliases']=={'location':['country','region'],'content_size':['size','contentSize','detail'],'search_recency_filter':['recency','freshness','time_filter','date_filter']}, d['optional_aliases']
assert d['target_tool']=='web_search_prime'
assert d['target_param']=='search_query'
assert d['log_level']=='info'
assert len(d)==11, ('expected 11 keys, got', len(d))
assert 'aliases' not in d
print('config.example.json == DefaultConfig()  OK')
"

# (b) doc.go names the v2 architecture.
grep -qi 'normalizing MCP server' doc.go && echo "OK: normalizing MCP server"
grep -qi 'MCP SDK\|modelcontextprotocol/go-sdk' doc.go && echo "OK: names the SDK"
grep -q 'web_search' doc.go && echo "OK: names web_search tool"
grep -qi 'extract' doc.go && echo "OK: names query extraction"
grep -qi 'web_search_prime' doc.go && echo "OK: names z.ai delegation target"
grep -qi 'append' doc.go && echo "OK: names append-after-results warning"
# (c) doc.go package clause intact + only non-comment line is `package main`.
grep -n '^package main$' doc.go && echo "OK: package main present"
[ "$(grep -vc '^\s*//' doc.go)" -eq 1 ] && echo "OK: exactly one non-comment line (package main)" \
  || echo "FAIL: doc.go has non-comment lines beyond package main"
# (d) no credential fields leaked into config.example.json.
grep -niE 'api_key|apikey|token|authorization|bearer' config.example.json && echo "FAIL: credential field" || echo "OK: no credential fields"
# Expected: all OK. config.example.json field-for-field == DefaultConfig(); doc.go has all v2 concepts.
```

### Level 4: Scope & build sanity (no out-of-scope edits; package still builds)

```bash
# Only the two deliverables changed by this item.
git diff --name-only | grep -vE '^(config\.example\.json|doc\.go)$' \
  && echo "FAIL: touched an out-of-scope file" || echo "OK: scope clean (config.example.json + doc.go only)"
git diff --name-only | grep -E 'README\.md|\.go$|^doc\.go$' | grep -vE '^doc\.go$' \
  && echo "FAIL: touched README or a .go source file" || echo "OK: no README/source edits"
# doc.go is in package main — the package must still vet + build (P1.M5.T4.S3 gates on this).
go vet ./... && echo "OK: go vet clean"
go build ./... && echo "OK: go build clean"
# Expected: scope clean; go vet + go build pass (doc.go parses; config.example.json is not compiled).
```

## Final Validation Checklist

### Technical Validation
- [ ] Level 1: `python3 -m json.tool config.example.json` succeeds; no `//` or `/* */`.
- [ ] Level 1: `gofmt -l doc.go` prints nothing.
- [ ] Level 2: every stale-phrase grep returns 0 in doc.go (no "reverse proxy", no
      "standard library", no "passes through", no "injected", no "does not normalize").
- [ ] Level 3: config.example.json == DefaultConfig() field-for-field (11 keys, 14 aliases).
- [ ] Level 3: doc.go names normalizing MCP server / Go MCP SDK / web_search / extract /
      web_search_prime / append-after-results warning.
- [ ] Level 3: doc.go has exactly one non-comment line (`package main`).
- [ ] Level 3: no credential fields in config.example.json.
- [ ] Level 4: `git diff --name-only` == config.example.json + doc.go only.
- [ ] Level 4: `go vet ./...` and `go build ./...` pass.

### Feature Validation (the two deliverables)
- [ ] config.example.json: all 11 v2 keys; query_aliases == 14 entries; optional_aliases
      == 3 keys; no `aliases` key; no comments.
- [ ] config.example.json: byte-for-byte values match DefaultConfig() (research §3).
- [ ] doc.go: para 1 = normalizing MCP server, Go MCP SDK, single web_search tool, not a proxy.
- [ ] doc.go: para 2 = extract from arbitrary input → delegate to z.ai web_search_prime →
      append warning AFTER results (canonical = no warning; no-query = immediate warning).
- [ ] doc.go: para 3 = revised non-goals (PRD §3).

### Code Quality / Scope Validation
- [ ] ONLY config.example.json + doc.go modified; no other file touched.
- [ ] README.md NOT edited (parallel P1.M5.T4.S1).
- [ ] No other .go source / go.mod / go.sum / PRD.md / test files changed.
- [ ] doc.go remains a pure package-comment file (comment block + `package main`).
- [ ] config.example.json stays comment-free (json.Unmarshal-safe).

### Documentation & Deployment
- [ ] config.example.json is a TRUE example of "runs with no config file" (== defaults).
- [ ] doc.go's `go doc` synopsis reads as the v2 normalizing server, not the v1 proxy.
- [ ] Both files are gofmt/json-clean so the P1.M5.T4.S3 quality gate passes.

---

## Anti-Patterns to Avoid

- ❌ Don't write 13 query_aliases. DefaultConfig() has 14; the item text's "13" is a typo.
  Use 14 (research §2). A 13-entry file fails the count assertion.
- ❌ Don't add comments to config.example.json. json.Unmarshal (config.go) rejects them and
  the quality gate's tests will fail. The v1 file was comment-free; keep it so.
- ❌ Don't keep the v1 `aliases` key. The v2 key is `query_aliases`. Leaving `aliases` would
  be silently dropped and is the exact footgun this rewrite removes.
- ❌ Don't keep ANY v1 framing in doc.go — no "reverse proxy", no "standard library
  exclusively", no "passes through untouched", no "injected", no "does not normalize".
  v2 normalizes; that was the whole pivot (PRD §0).
- ❌ Don't call search_query "the canonical" in doc.go. It is z.ai's internal TargetParam.
  The canonical surface we teach is `web_search` / `{ "query": "..." }`.
- ❌ Don't say the warning is "injected"/"prepended" in doc.go. v2 APPENDS it AFTER results
  (teach.go appendWarning, PRD §12.3). The verb matters.
- ❌ Don't add code to doc.go. It is a comment-only file; the only non-comment line is
  `package main`. Imports/funcs/vars are out of scope and break the file's purpose.
- ❌ Don't edit README.md, go.mod, any *.go source, or PRD.md. This item is exactly two
  files (config.example.json + doc.go). README is the parallel P1.M5.T4.S1.
- ❌ Don't invent config values. config.example.json must reproduce DefaultConfig() exactly
  so it is both a valid example and a true "runs with no config file" sample (PRD §18.2).
- ❌ Don't skip gofmt on doc.go. `gofmt -l doc.go` must be silent or the P1.M5.T4.S3 gate
  (go vet/test/build) is compromised.

---

## Confidence Score

**9/10** for one-pass success. The exact byte-for-byte target content of
config.example.json is given verbatim in research/source-of-truth.md §3 (copy it). The
exact v1 doc.go text being removed is in research §4 with enumerated zero-tolerance grep
targets, and the exact v2 content is specified paragraph-by-paragraph from PRD §0/§3/§5.2/
§9/§12. The only judgment call is doc.go prose wording, bounded by the explicit
must-name/must-not-contain lists and the stale-phrase grep gate. The one resolved
ambiguity (13 vs 14 query_aliases) is settled decisively toward DefaultConfig() (14) and
enforced by an assertion in the validation loop. Both deliverables are static text with
no build/test consequences beyond gofmt + vet cleanliness. The residual 1 point reflects
that doc.go prose tone is subjective and that config.example.json style (inline vs
multiline arrays) is cosmetic — both mitigated by the verbatim body in research §3 and
the gofmt gate.
