name: "P1.M5.T4.S1 — Rewrite README.md for the v2 normalizing MCP server + Part A client config (PRD §20, §14, §3)"
description: |

  FULLY REWRITE `README.md` for the v2 architecture. The on-disk README is the
  **v1 transparent-proxy** description (renames `query`→`search_query`, prepends a
  warning, "Go standard library only", config key `aliases`). That framing is
  STALE and must be REPLACED ENTIRELY. The new README describes a **normalizing
  MCP server** built on the official Go MCP SDK that makes an agent's first
  web-search attempt succeed. DOCS-ONLY: no Go code,
  no go.mod/go.sum, no PRD.md, no config.example.json, no doc.go (those last two
  are the parallel P1.M5.T4.S2 item).

  THE EIGHT THINGS THE NEW README MUST CONTAIN (item contract; PRD §20/§14/§3):
    (a) what it is — a normalizing MCP server (NOT a proxy); makes the first try work.
    (b) the canonical surface: `web_search` / `{ "query": "..." }`.
    (c) the Part A client config — `~/.pi/agent/mcp.json` with `settings.toolPrefix:"none"`
        + the `mcpServers.web_search` entry (PRD §20, verbatim JSON block).
    (d) the append-after-results warning (with example) the agent sees when it strays
        from canonical — verbatim from teach.go, incl. the EM DASH.
    (e) the new config fields + env vars, calling out the `aliases`→`query_aliases`
        BREAKING CHANGE.
    (f) the SDK build (`go mod tidy` / `go build`); the "stdlib only / no go.sum"
        v1 claims are now FALSE and must be GONE.
    (g) `GET /healthz`.
    (h) the revised non-goals (PRD §3).
  Every v1 proxy reference must be gone (see research §9 grep targets).

  INPUT (treat as CONTRACT — the full v2 behavior already shipped):
    README.md (v1, to be replaced — research has the full current text).
    config.go         : v2 Config schema, DefaultConfig, env vars, validation, BREAKING CHANGE.
    main.go           : startup fields, /healthz + / route table, graceful shutdown.
    health.go         : /healthz contract, var version = "dev", -ldflags seam.
    tools.go          : canonicalDescription / aliasDescription verbatim text.
    teach.go          : warningText / noQueryWarningText verbatim (byte-fixed, EM DASH).
    go.mod            : go 1.25.0; sole require go-sdk v1.6.1 (source of truth for toolchain).
    PRD.md §20/§14/§3 : the mcp.json block, file layout, non-goals.
    config.example.json + doc.go are owned by the parallel P1.M5.T4.S2 — REFERENCE
    config.example.json, do NOT edit/duplicate it.

  SCOPE: REWRITE README.md ONLY. If config.example.json or doc.go still carry v1
  text, leave them (T4.S2 owns them). The README must be self-consistent for v2.

---

## Goal

**Feature Goal**: A single `README.md` that, read on its own, tells an operator
  exactly what the v2 server is, how to build/run it, how to configure their MCP
  client (the Part A unlock), what their agent will see (results + appended warning),
  the full v2 config schema + env vars + the `aliases`→`query_aliases` breaking
  change, `/healthz`, and the revised non-goals — with **zero** remaining v1
  proxy/stdlib/passthrough framing.

**Deliverable**: `README.md` (full rewrite, ~same length or slightly longer than
  the 155-line v1). Markdown only. All quoted text (warning strings, tool
  descriptions, the mcp.json block, the `/healthz` body, config defaults) matches
  the on-disk source verbatim.

**Success Definition**:
  - `grep` for every stale v1 phrase in research §9 returns ZERO matches in README.md.
  - The Part A `mcp.json` JSON block matches PRD §20 verbatim (incl. `toolPrefix:"none"`).
  - Both warning strings match teach.go byte-for-byte (incl. the EM DASH `—`, U+2014).
  - The config table matches config.go `DefaultConfig` exactly (field names, keys, defaults).
  - The `aliases`→`query_aliases` BREAKING CHANGE is called out prominently.
  - The Build section says the SDK is a dependency and uses `go mod tidy`/`go build`;
    the v1 "standard library only / no go.sum / no network to build" claims are GONE.
  - A markdown linter / `cat README.md` renders cleanly; no broken code fences.

## User Persona

**Target User**: The single operator-developer who runs one local instance for
  their own MCP clients (pi, Claude Code, Cursor). They install Go, build the
  binary, point their client at it, and want the agent's first web search to work.
**Use Case**: An operator reads the README top-to-bottom once, runs `go build`,
  drops the PRD §20 block into `~/.pi/agent/mcp.json`, starts the binary, hits
  `/healthz`, and is done.
**User Journey**: (1) read "what it is" → (2) build → (3) paste client config →
  (4) run + `/healthz` → (5) optionally tune the server config (renaming any v1
  `aliases` key) → (6) rely on it daily.
**Pain Points Addressed**: The v1 README actively misleads — it describes a proxy
  the code no longer is, claims stdlib-only (false), teaches the wrong canonical
  surface (`search_query` is z.ai's param, not what we teach the agent), and omits
  the Part A unlock entirely. Operators following it would misconfigure the client
  and hit the exact layer-1 failure v2 exists to fix.

## Why

- Implements **PRD §14** ("README.md — install + run + client config (incl.
  toolPrefix:none)") and is the user-facing mirror of **PRD §20** (the Part A
  client config) and **PRD §3** (revised non-goals).
- This IS the **Mode B changeset-level documentation** task for README.md: the v2
  architecture shipped across M1–M5; the README is the last surface still
  describing the dead v1 proxy.
- The `aliases`→`query_aliases` rename is a **breaking config change**
  (architecture/codebase_patterns.md §7, config.go doc comment) — the README is
  where operators learn to migrate their existing config file.
- Without the Part A `toolPrefix:"none"` section, the server alone cannot fix
  layer-1 (wrong tool name) failures (PRD §5, §9.1) — the README is the only place
  that unlock is documented for the operator.

## What

A rewritten `README.md` (markdown). The user-visible content maps 1:1 to the eight
contract points. Suggested section order (mirrors the contract a–h and the v1
README's tested structure, adapted to v2):

```
# web-search-prime-fixer
  [intro paragraph: normalizing MCP server, NOT a proxy; built on the Go MCP SDK;
   makes the agent's first web-search attempt succeed; one developer, one local instance]
## What it solves                          (PRD §1/§2 — the failure mode this fixes)
## What it is (and is not)                 (contract a — normalizing server; owns tools/list;
                                           acts as MCP client to z.ai; NOT a byte proxy/open proxy)
## The canonical surface                   (contract b — web_search / { "query": "..." }; optionals)
## How it works                            (the tools/call flow: extract → delegate → append-warning;
                                           the two-part fix Part A + Part B)
## Configure your MCP client               (contract c — PRD §20 mcp.json verbatim; toolPrefix:none;
                                           why; other clients note)
## What the agent sees                      (contract d — canonical=no warning; non-canonical=results
                                           THEN warning; empty=immediate warning no upstream call;
                                           verbatim warning strings; isError never set)
## Build                                    (contract f — go mod tidy; go build; SDK dep; -ldflags;
                                           cite go.mod for toolchain; REMOVE v1 stdlib/no-go.sum claims)
## Run                                      (defaults; WSPF_LISTEN; WSPF_CONFIG; 127.0.0.1 only; shutdown)
## Health check                             (contract g — GET /healthz; body; version -ldflags; no upstream)
## Configuration                            (contract e — v2 schema table; env vars; discovery; the
                                           aliases→query_aliases BREAKING CHANGE callout; validation;
                                           ref config.example.json)
## Non-goals                                (contract h — PRD §3 verbatim/revised)
## Tests                                    (go test ./...; in-process fakes; no network)
```

### Success Criteria

- [ ] Intro/first paragraph establishes "normalizing MCP server", "not a proxy",
      "built on the official Go MCP SDK".
- [ ] Canonical surface stated as `web_search` / `{ "query": "..." }` with the
      three optionals; `web_search_prime` noted as never-advertised.
- [ ] Part A `mcp.json` block present verbatim (PRD §20), with a sentence on
      `toolPrefix:"none"` and on Claude Code/Cursor parity.
- [ ] Both warning strings present verbatim (teach.go), incl. the EM DASH and the
      fixed `"rust async runtime"` example.
- [ ] Build section: `go mod tidy` + `go build -o web-search-prime-fixer .` +
      the `-ldflags "-X main.version=..."` form; cites go.mod for the toolchain;
      SDK named as the dependency. No "standard library only / no go.sum".
- [ ] `/healthz` section: `GET`, body `{"ok":true,"version":"<version>"}`, version
      default `dev`, no upstream touch.
- [ ] Configuration section: full v2 table (11 rows) matching DefaultConfig; env
      vars (WSPF_CONFIG/WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL) with the
      "WSPF_CONFIG missing file = hard error" note; discovery precedence; the
      `aliases`→`query_aliases` BREAKING CHANGE called out prominently; reference
      config.example.json.
- [ ] Non-goals: the PRD §3 list (not a search engine, not an open proxy, no
      credential ownership, no retry/rate-limit/cache, no query truncation, no
      multi-tool semantics, no z.ai-branded names, not responsible for other
      servers' name collisions under toolPrefix:none).
- [ ] All research §9 stale-phrase grep targets return ZERO matches.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can write this README from this PRP alone
because: (a) every value the README must state verbatim — the two warning strings,
the canonical/alias tool descriptions, the `/healthz` body, the mcp.json block, the
config table, the env vars, the build commands — is quoted in research §1–§8 from
the on-disk source of truth; (b) the exact v1 phrases that must disappear are
enumerated in research §9 with zero false positives; (c) the suggested section
order maps 1:1 to the eight contract points; (d) the scope boundary (README only;
config.example.json/doc.go are T4.S2) is explicit; (e) the validation gates are
concrete greps + a render check.

### Documentation & References

```yaml
# MUST READ — the single source of truth for every value the README states verbatim.
- docfile: plan/002_0a8ab3410994/P1M5T4S1/research/readme-source-of-truth.md
  why: the exact warning strings, tool descriptions, mcp.json block, config table,
        env vars, build commands, /healthz body, non-goals, AND the stale-phrase
        grep targets (§9). Copy values from here; do not re-derive.
  critical: the warning strings contain an EM DASH (—, U+2014), NOT a hyphen —
        byte-match teach.go. The mcp.json block must match PRD §20 verbatim incl.
        toolPrefix:"none" and the ${Z_AI_API_KEY} header.

# MUST READ — the v1 text being replaced (so the writer knows what to remove).
- file: README.md
  why: the current on-disk README (transparent-proxy description). REPLACE entirely.
        Every section is stale: the "How it works" proxy rewrite, the v1 warning
        text, "Go standard library only", the config table's `aliases` row, the
        client config using the `web-search-prime` server name.

# MUST READ — the v2 behavior sources (verify any value against these).
- file: config.go
  section: "Config struct (PRD §18.1) + DefaultConfig + resolveConfigPath + ResolveConfig"
  why: the authoritative v2 schema, defaults, JSON keys, env vars, discovery
        precedence, validation rules, and the documented aliases→query_aliases
        BREAKING CHANGE (read the struct + DefaultConfig doc comments).
- file: teach.go
  section: "warningText + noQueryWarningText"
  why: the exact warning strings the "What the agent sees" section must quote.
        Confirm the EM DASH and the fixed example query.
- file: tools.go
  section: "canonicalDescription + aliasDescription"
  why: the exact canonical/alias tool Description text (if the README shows them).
- file: health.go
  section: "var version + healthHandler"
  why: the /healthz contract (GET only; {"ok":true,"version":<version>}; no upstream;
        version via -ldflags -X main.version=...).
- file: main.go
  section: "main bootstrap + logStartup + authMiddleware"
  why: route table (/healthz + / → SDK handler wrapped in auth middleware), graceful
        shutdown (SIGINT/SIGTERM, 10s), 127.0.0.1-only bind, startup log fields.
- file: go.mod
  why: source of truth for the toolchain (go 1.25.0) and the sole dependency
        (github.com/modelcontextprotocol/go-sdk v1.6.1). CITE go.mod in Build; do not
        hardcode a Go version that can drift.

# MUST READ — the PRD sections the README mirrors.
- file: PRD.md
  section: "§20 Client configuration (the Part A unlock) — verbatim mcp.json block"
  why: the exact JSON block (incl. toolPrefix:"none" and the web_search server entry).
- file: PRD.md
  section: "§3 Non-goals (revised)"
  why: the non-goals list the README must reproduce.
- file: PRD.md
  section: "§14 Architecture and file layout + §9.4 Canonical surface + §5 Solution overview"
  why: the file-layout / data-flow / two-part-fix framing for "How it works".

# READ — confirms the stale framing + the breaking change must be called out.
- docfile: plan/002_0a8ab3410994/architecture/system_context.md
  why: "the v1 README describes a transparent proxy ... that framing is STALE and
        must be replaced entirely" + the stdlib-only constraint is relinquished.
- docfile: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  section: "§7 JSON key mapping (v1 → v2 config breaking change)"
  why: the aliases→query_aliases rename + the new v2 fields table.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  README.md             # v1 PROXY description — REWRITE THIS (the deliverable)
  config.example.json   # v1 (still uses "aliases") — owned by parallel P1.M5.T4.S2; REFERENCE, do NOT edit
  doc.go                # v1 package comment (proxy) — owned by parallel P1.M5.T4.S2; do NOT edit
  config.go             # v2 source of truth: Config + DefaultConfig + ResolveConfig (READ)
  main.go               # bootstrap, /healthz + / routes, graceful shutdown, authMiddleware (READ)
  health.go             # var version="dev"; healthHandler; /healthz contract (READ)
  tools.go              # canonicalDescription / aliasDescription verbatim (READ)
  teach.go              # warningText / noQueryWarningText verbatim (READ)
  go.mod                # go 1.25.0; require go-sdk v1.6.1 (READ — cite in Build)
  *.go + *_test.go      # shipped v2 implementation + suites (untouched)
  PRD.md                # READ-ONLY; mirror §20/§3/§14
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
README.md   # REWRITE (full replacement): the v2 normalizing-MCP-server README.
            #   Sections (see "What" above): intro, What it solves, What it is (and is not),
            #   Canonical surface, How it works, Configure your MCP client (Part A, PRD §20
            #   verbatim), What the agent sees (teach.go warnings verbatim), Build (go mod
            #   tidy / go build; SDK dep; -ldflags; cite go.mod), Run, Health check (/healthz),
            #   Configuration (v2 table + env vars + discovery + aliases→query_aliases
            #   BREAKING CHANGE; ref config.example.json), Non-goals (PRD §3), Tests.
# No other file is created or modified by this item.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — EM DASH, not hyphen. Both warning strings in teach.go use the Unicode
EM DASH (—, U+2014) before "e.g." (e.g. "... — e.g. web_search(..."). A hyphen
(-) or en dash (–) is a WRONG character and breaks the "verbatim" requirement.
Copy the strings from research §3 / teach.go exactly.

CRITICAL — the canonical surface we TEACH is `web_search` / `query`, NOT
`search_query`. `search_query` is z.ai's INTERNAL parameter name (TargetParam),
which the agent never sees and the README must NOT present as the canonical call.
The v1 README taught `search_query` — that is the core framing error to fix.

CRITICAL — `web_search_prime` is NEVER advertised to the agent. The README may
name it (as the z.ai upstream tool / TargetTool / the thing delegated to) but
must never suggest it as a callable tool name. It is also forbidden in cfg.Tools
(validation rejects it).

CRITICAL — the v1 README's build claims are now FALSE: "Go standard library only",
"no external dependencies", "no go.sum", "no network access is needed to build".
The v2 server depends on github.com/modelcontextprotocol/go-sdk (PRD §13; the
stdlib-only constraint was explicitly relinquished). The Build section must name
the SDK and use `go mod tidy` + `go build`. All four stale phrases must be GONE
(research §9 greps for them).

CRITICAL — the Part A client config is the WHOLE other half of the fix. A README
that documents only the server is incomplete: without toolPrefix:"none" the agent
still cannot call web_search by its bare name (PRD §5, §9.1). The mcp.json block
(PRD §20) MUST appear verbatim, including the top-level "settings": {"toolPrefix":"none"}.

CRITICAL — BREAKING CHANGE callout. config.go renamed the v1 JSON key "aliases" →
"query_aliases" (same []string semantics). Operators with a v1 config file MUST
rename the key or their custom alias list is silently dropped (unknown fields are
ignored). Call this out PROMINENTLY in the Configuration section (a "Breaking
change" note). Note the on-disk config.example.json still uses "aliases" and is
fixed by the parallel P1.M5.T4.S2 — do not edit it here, just reference it.

CRITICAL — WSPF_CONFIG semantics differ from the other env vars. WSPF_CONFIG is
used VERBATIM; a missing named file is a HARD ERROR (exit non-zero), not a silent
fallback. The other three (WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL) are plain
overrides where empty values are ignored. State this distinction.

CRITICAL — scope. This item is README.md ONLY. config.example.json and doc.go are
the parallel P1.M5.T4.S2. Do NOT edit them, do NOT paste config.example.json's
full body into the README (reference it instead), do NOT fix doc.go's package
comment. No Go source edits; no go.mod/go.sum; no PRD.md.

GOTCHA — toolchain version. PRD §14 says "go 1.22+", but go.mod's module directive
is `go 1.25.0`. CITE go.mod as the source of truth (e.g. "Requires Go (see the
`go` directive in go.mod; currently 1.25.0)") rather than hardcoding a number
that can drift or contradict go.mod.

GOTCHA — code fences. The mcp.json block and the warning strings are fenced code
blocks. Ensure the fences balance (open ``` and close ```) and that the warning
blocks are plain (no language tag) so the EM DASH renders. A missing closing fence
breaks rendering of everything after it.

GOTCHA — the v1 client-config block used the server NAME "web-search-prime"
({"mcpServers":{"web-search-prime":{...}}}). The v2 block (PRD §20) uses the name
"web_search" and adds the top-level settings.toolPrefix. Do not reuse the v1
block; use PRD §20 verbatim.
```

## Implementation Blueprint

### Data models and structure

None. This is a markdown documentation file. The "data" is the verbatim values in
research §1–§8 (warning strings, descriptions, mcp.json block, config table,
env vars, build/health details, non-goals), each already copied from its on-disk
source of truth.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) confirm the source of truth is on disk
  - RUN: test -f README.md && test -f config.go && test -f teach.go && test -f health.go \
        && test -f tools.go && test -f go.mod && grep -q "func DefaultConfig" config.go \
        && grep -q "func warningText" teach.go && grep -q "var version = \"dev\"" health.go \
        && grep -q "modelcontextprotocol/go-sdk" go.mod && echo OK
  - EXPECT: OK. If any fail, the v2 sources are missing — STOP (prerequisite not met).

Task 1: WRITE the new README.md — intro + "what it is/is not" + canonical surface
        + "how it works" (contract a, b, + flow)
  - FILE: README.md (overwrite). Open with a one-paragraph intro: a normalizing MCP
        server (NOT a proxy), built on the official Go MCP SDK, that makes an agent's
        first web-search attempt succeed; for one developer running a single local
        instance (pi, Claude Code, Cursor).
  - "What it solves": the layer-1 (wrong tool name) + layer-2 (wrong param/schema)
        failures (PRD §1/§2); "the first attempt should just work".
  - "What it is (and is not)": a real MCP server that owns tools/list (one tool,
        web_search) and acts as an MCP client to z.ai; NOT a transparent proxy, NOT
        a byte forwarder, NOT an open proxy (binds 127.0.0.1).
  - "Canonical surface": `web_search` / `{ "query": "..." }`; optionals location,
        content_size, search_recency_filter; web_search_prime never advertised.
  - "How it works": tools/call → extract query from ANY input (extract.go) → if no
        query, immediate warning (no upstream) → else delegate ONE clean call to z.ai
        → return results → append warning AFTER results if non-canonical. Two-part
        fix: Part A (client toolPrefix:none) + Part B (this server).

Task 2: WRITE "Configure your MCP client" (Part A unlock — contract c)
  - PASTE the PRD §20 mcp.json block VERBATIM (research §4): the top-level
        "settings": {"toolPrefix":"none"} + "mcpServers":{"web_search":{...}} pointing
        at http://127.0.0.1:8787/mcp with Authorization Bearer ${Z_AI_API_KEY}.
  - EXPLAIN toolPrefix:"none" (bare tool names; why web_search is callable as
        web_search not web_search_web_search_prime; global across the operator's MCP
        servers; safe for current web_search+zread set). Note Claude Code/Cursor
        parity = "whatever makes them expose bare tool names". State the Authorization
        stays on the client and is forwarded verbatim (no server-held key).

Task 3: WRITE "What the agent sees" (contract d — verbatim warnings)
  - Three cases: (1) canonical web_search/{query} → results only, no warning, no
        retry; (2) non-canonical (any other name/param/shape/inferred/normalized
        optionals) → results FIRST then the warning appended AFTER; (3) empty input
        → immediate warning, NO upstream call.
  - PASTE both warning strings from teach.go VERBATIM (research §3), incl. the EM
        DASH (—) and the fixed example query "rust async runtime".
  - STATE: isError is NEVER set for any normalization/warning/guidance case (FR-6).

Task 4: WRITE "Build" + "Run" + "Health check" (contract f, g)
  - BUILD: `go mod tidy` (SDK is cached locally; needs no network) then
        `go build -o web-search-prime-fixer .`. Version:
        `go build -ldflags "-X main.version=0.2.0" -o web-search-prime-fixer .`
        (plain build → dev). NAME the dependency
        (github.com/modelcontextprotocol/go-sdk v1.6.1). CITE go.mod for the toolchain
        (currently go 1.25.0). DO NOT include "standard library only" / "no go.sum".
  - RUN: `./web-search-prime-fixer` (defaults: 127.0.0.1:8787, delegates to z.ai);
        `WSPF_LISTEN=127.0.0.1:9000 ./...`; `WSPF_CONFIG=./my.json ./...`; binds
        127.0.0.1 only; graceful shutdown on Ctrl-C/SIGTERM (10s drain).
  - HEALTH: `curl http://127.0.0.1:8787/healthz` → 200
        `{"ok":true,"version":"<version>"}`; version default dev (set via -ldflags);
        GET only (non-GET → 405); does NOT touch the upstream.

Task 5: WRITE "Configuration" (contract e — v2 schema + env + BREAKING CHANGE)
  - TABLE: the 11 v2 fields (research §7 table) — JSON key, default, notes — matching
        config.go DefaultConfig exactly. Mark query_aliases as "RENAMED from v1 aliases".
  - ENV VARS: WSPF_CONFIG (verbatim; missing file = HARD ERROR, exit non-zero),
        WSPF_UPSTREAM, WSPF_LISTEN, WSPF_LOG_LEVEL (highest precedence; empty ignored).
        Note the eight fields with NO env override.
  - DISCOVERY: WSPF_CONFIG → ./web-search-prime-fixer.json →
        $XDG_CONFIG_HOME/web-search-prime-fixer/config.json → built-in defaults.
  - BREAKING CHANGE NOTE (prominent): v1 key "aliases" → v2 "query_aliases"; operators
        MUST rename it or their custom alias list is silently dropped. New v2 fields:
        tools, canonical_tool, canonical_param, optional_aliases, target_tool.
  - VALIDATION: listen host:port; upstream absolute URL; tools non-empty + contains
        canonical_tool; no tools entry == target_tool; unknown JSON fields ignored;
        no credential fields in Config. Reference config.example.json (updated by
        P1.M5.T4.S2) for a ready-to-copy example.

Task 6: WRITE "Non-goals" + "Tests" (contract h + tests)
  - NON-GOALS: reproduce PRD §3 (research §8): not a search engine; not an open proxy;
        no credential ownership; no retry/rate-limit/cache; no query truncation; no
        multi-tool semantics; no z.ai-branded names advertised; not responsible for
        other servers' name collisions under toolPrefix:none.
  - TESTS: `go test ./...`; in-process httptest fakes + golden SSE fixtures; no
        network calls, no credentials.

Task 7: VALIDATE (run every gate in the Validation Loop)
  - The §9 stale-phrase greps MUST return zero; the verbatim greps (mcp.json block,
        warning EM DASH, /healthz body, config defaults) MUST match; markdown renders.
```

### Implementation Patterns & Key Details

```markdown
# PATTERN: lead with what it IS, not what it was. The intro paragraph must establish
#   "normalizing MCP server" + "not a proxy" before any other sentence. Do not open
#   with "this is a rewrite" or "this used to be a proxy" — that keeps stale framing
#   alive. (A one-line breaking-change note belongs in the Configuration section, not
#   the intro.)

# PATTERN: quote behavior verbatim in fenced blocks. The warning strings, the mcp.json
#   block, the /healthz body, and the canonical tool description are all things an
#   operator copies. They must match the on-disk source byte-for-byte (research §3/§2/§4/§6).

# PATTERN: separate "what the OPERATOR configures" (the client mcp.json) from "what the
#   SERVER is configured with" (config.go + env + file). These are two different audiences
#   of the same README; keep them in distinct sections so an operator doesn't conflate
#   toolPrefix:none (client) with query_aliases (server).

# GOTCHA: the warning is APPENDED AFTER results, never prepended and never instead of
#   them. State this explicitly — the v1 README said "prepends", which is wrong for v2.

# GOTCHA: teach the agent `query`, not `search_query`. search_query is z.ai's internal
#   param (TargetParam) that the server sends upstream; the agent never types it. The
#   whole point of the server is the agent can call { "query": "..." } and succeed.

# GOTCHA: the mcp.json server NAME is "web_search" (matches the advertised tool), with a
#   top-level "settings": {"toolPrefix":"none"}. The v1 block used the name
#   "web-search-prime" with no settings block — do not reuse it.
```

### Integration Points

```yaml
FILES MODIFIED:
  - README.md  (full rewrite; the only file this item touches)
FILES NOT TOUCHED (contract):
  - config.example.json : owned by parallel P1.M5.T4.S2 (still uses v1 "aliases" key;
        the README REFERENCES it as "see config.example.json for a ready-to-copy example"
        and may note it is updated by T4.S2, but does not edit or inline it).
  - doc.go              : owned by parallel P1.M5.T4.S2 (v1 package comment). Do not edit.
  - any *.go source / *_test.go : untouched.
  - go.mod / go.sum     : untouched.
  - PRD.md              : READ-ONLY.
CONSUMER SEAM:
  - This README is the user-facing artifact that P1.M5.T4.S3 (quality gate) points at
        as "documentation synced" and that operators read to deploy v2. Keep section
        names stable-ish ("Build", "Configure your MCP client", "Health check",
        "Configuration", "Non-goals") so future edits land predictably.
DATABASE / ROUTES / ENV / CONFIG: none (documentation only). The README DESCRIBES the
  server's config (config.go) and env vars; it does not change them.
```

## Validation Loop

### Level 1: Markdown well-formedness (Immediate Feedback)

```bash
# Balanced code fences: odd number of ``` = broken rendering. Count must be EVEN.
test "$(grep -c '```' README.md)" -eq $(( $(grep -c '```' README.md) / 2 * 2 )) \
  && echo "fences balanced" || echo "UNBALANCED FENCES"
# Render sanity (if a markdown viewer is available, optional):
#   glow README.md | head -40   # or just `cat README.md` and eyeball
# Expected: fences balanced; the mcp.json + warning + /healthz blocks render as code.
```

### Level 2: Stale v1 framing is GONE (the core correctness gate)

```bash
# Every phrase below MUST return ZERO matches. These have NO legitimate v2 use
# (research §9). If any matches, that section still carries v1 framing — rewrite it.
for term in \
  'transparent proxy' \
  'Go standard library only' \
  'no external dependencies' \
  'no `go.sum`' \
  'no network access is needed to build' \
  'byte-for-byte' \
  'prepends a' \
  'prepended' \
  'search_query" is not a valid parameter' \
  'ignored "query"' \
  'renames the' \
  'passes through untouched' \
  'passes through unchanged' ; do
    n=$(grep -c -F "$term" README.md)
    [ "$n" -eq 0 ] && echo "OK   (0): $term" || echo "FAIL ($n): $term  <-- remove"
done
# Expected: every line "OK (0):". Any "FAIL" = stale text remains; fix before finishing.

# The v1 client-config server name "web-search-prime": as the mcpServers key must be gone
# (v2 uses "web_search"). (A passing reference to the tool/brand elsewhere is fine, so
# this is a manual eyeball, not a hard grep — confirm the Configure-your-client block
# uses "web_search".)
grep -n 'web-search-prime' README.md   # eyeball: any hit should NOT be the mcpServers key
```

### Level 3: Verbatim values match the on-disk source of truth

```bash
# (a) Part A mcp.json block present verbatim (PRD §20). Must contain BOTH the
#     top-level settings.toolPrefix AND the web_search server at 127.0.0.1:8787/mcp.
grep -F '"toolPrefix": "none"' README.md && \
grep -F '"url": "http://127.0.0.1:8787/mcp"' README.md && \
grep -F '"web_search": {' README.md && echo "mcp.json block OK"

# (b) Both warning strings present, WITH the EM DASH (—, U+2014) — not a hyphen.
#     Use the EM DASH literal in the grep pattern (copy from teach.go).
grep -F 'Results are above. Next time call: web_search with { "query": "..." } — e.g.' README.md \
  && echo "after-results warning OK (EM DASH present)"
grep -F 'could not find a search query in the arguments; no search was run.' README.md \
  && echo "no-query warning OK"
# Belt-and-suspenders: a hyphen where the EM DASH should be is a FAIL.
grep -F 'Next time call: web_search with { "query": "..." } - e.g.' README.md \
  && echo "FAIL: hyphen instead of EM DASH" || echo "OK: no stray hyphen before e.g."

# (c) /healthz body + version default + GET-only.
grep -F '{"ok":true,"version":"<version>"}' README.md   # or {"ok": true, ...}; state the shape
grep -F 'dev' README.md | grep -F version || true       # version defaults to dev
grep -iF 'GET /healthz' README.md || grep -iF 'curl http://127.0.0.1:8787/healthz' README.md

# (d) Build section names the SDK + uses go mod tidy; does NOT claim stdlib-only.
grep -F 'go mod tidy' README.md && \
grep -F 'go build -o web-search-prime-fixer' README.md && \
grep -F 'modelcontextprotocol/go-sdk' README.md && echo "build section OK"

# (e) Breaking change called out.
grep -F 'aliases' README.md | grep -iF 'query_aliases' && grep -iF 'breaking' README.md

# (f) Config defaults match DefaultConfig (spot-check the distinctive ones).
grep -F 'searchString' README.md        # the last query alias in the default list
grep -F 'web_search_prime' README.md    # the target tool / never-advertised note
grep -F 'search_query' README.md        # the target param
# Expected: all of the above match. If a distinctive default is missing, the table is incomplete.
```

### Level 4: Scope & cross-reference sanity

```bash
# Only README.md changed by this item.
git diff --stat                # expect: README.md only
git diff --name-only           # expect exactly: README.md
# config.example.json + doc.go are NOT touched (they are P1.M5.T4.S2).
git diff --name-only | grep -E 'config\.example\.json|^doc\.go$|\.go$|go\.mod|go\.sum|PRD\.md' \
  && echo "FAIL: touched an out-of-scope file" || echo "OK: scope clean (README.md only)"

# README references config.example.json (so an operator can find the example) without
# inlining its full (still-stale) body.
grep -F 'config.example.json' README.md   # expect >=1 reference
# Expected: scope clean; README.md is the sole change; config.example.json referenced, not duplicated.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1: code fences balanced (`grep -c '```'` is even); markdown renders cleanly.
- [ ] Level 2: every stale-phrase grep returns 0 (no "transparent proxy", no
      "standard library only", no "no go.sum", no "prepends", no v1 warning text,
      no "renames the", no "passes through untouched").
- [ ] Level 3: mcp.json block verbatim (toolPrefix:"none" + web_search @ 127.0.0.1:8787/mcp).
- [ ] Level 3: both warning strings present WITH the EM DASH (no hyphen).
- [ ] Level 3: /healthz body + version default dev + GET-only.
- [ ] Level 3: Build names the SDK + uses `go mod tidy`/`go build`; cites go.mod.
- [ ] Level 3: breaking change `aliases`→`query_aliases` called out.
- [ ] Level 3: config defaults present (searchString / web_search_prime / search_query).
- [ ] Level 4: `git diff --name-only` == README.md only.

### Feature Validation (the eight contract points)

- [ ] (a) "what it is" — normalizing MCP server, not a proxy, built on the Go MCP SDK.
- [ ] (b) canonical surface `web_search` / `{ "query": "..." }` + 3 optionals; web_search_prime never advertised.
- [ ] (c) Part A mcp.json block verbatim (PRD §20) + toolPrefix:"none" explanation + other-clients note.
- [ ] (d) "What the agent sees": canonical=no warning; non-canonical=results THEN warning; empty=immediate no-upstream warning; isError never set.
- [ ] (e) v2 config table + env vars + discovery + the aliases→query_aliases BREAKING CHANGE + validation; reference config.example.json.
- [ ] (f) Build: go mod tidy / go build; SDK dep; -ldflags; go.mod cited; NO stdlib/no-go.sum claims.
- [ ] (g) /healthz: GET, body, version default dev, no upstream.
- [ ] (h) Non-goals: the PRD §3 list.

### Code Quality / Scope Validation

- [ ] ONLY README.md modified; no other file touched.
- [ ] config.example.json and doc.go NOT edited (they are P1.M5.T4.S2).
- [ ] No Go source / go.mod / go.sum / PRD.md changes.
- [ ] Section names are operator-friendly and stable ("Build", "Configure your MCP
      client", "Health check", "Configuration", "Non-goals", "Tests").
- [ ] Intro leads with what it IS (normalizing server), not what it was.

### Documentation & Deployment

- [ ] All quoted values (warnings, mcp.json, /healthz, config defaults) match the
      on-disk source byte-for-byte (research §1–§8).
- [ ] No drift-prone hardcoded Go version (cite go.mod).
- [ ] An operator can deploy v2 from this README alone (build → client config → run → health).

---

## Anti-Patterns to Avoid

- ❌ Don't keep ANY v1 framing alive — no "transparent proxy", no "renames query to
  search_query", no "prepends a warning", no "passes through untouched", no
  "standard library only / no go.sum". The whole document is the v2 story.
- ❌ Don't teach `search_query` to the agent. That is z.ai's internal param; the
  canonical surface we teach is `web_search` / `query`. The v1 README's core error.
- ❌ Don't use a hyphen (-) or en dash (–) in the warning strings — they use the EM
  DASH (—, U+2014). Copy verbatim from teach.go / research §3.
- ❌ Don't omit the Part A client config (toolPrefix:"none") — the server alone cannot
  fix layer-1 (wrong tool name) failures. The mcp.json block is half the product.
- ❌ Don't omit the aliases→query_aliases BREAKING CHANGE callout — operators with a
  v1 config file will silently lose their custom alias list otherwise.
- ❌ Don't claim the build is stdlib-only / no-network — the server depends on the
  Go MCP SDK (PRD §13). The Build section must name it and use `go mod tidy`.
- ❌ Don't edit config.example.json or doc.go — they are the parallel P1.M5.T4.S2.
  Reference config.example.json; do not inline its (still-stale) full body.
- ❌ Don't reuse the v1 client-config block (`"web-search-prime": {...}` with no
  settings) — use the PRD §20 block (`"web_search": {...}` + top-level toolPrefix:none).
- ❌ Don't leave unbalanced code fences — a missing ``` breaks every section after it.
- ❌ Don't open with "this is a rewrite of the proxy" — that buries stale framing in
  the intro. Lead with what it IS.

---

## Confidence Score

**9/10** for one-pass success. Every value the README must state verbatim (the two
warning strings, the canonical/alias tool descriptions, the `/healthz` body, the
PRD §20 mcp.json block, the full v2 config table, the env vars, the build commands,
the non-goals) is quoted in research §1–§8 directly from the on-disk source of truth
(config.go, teach.go, tools.go, health.go, main.go, go.mod, PRD §20/§3). The exact
v1 phrases to eliminate are enumerated in research §9 with zero false positives, and
the validation loop greps for each of them. The scope is a single markdown file with
no code, build, or test consequences. The residual 1 point reflects that a
documentation rewrite is subjective (tone/structure) and that the parallel
P1.M5.T4.S2 owns config.example.json/doc.go — mitigated by the explicit "REFERENCE,
do not edit" rule and the Level-4 scope gate.
