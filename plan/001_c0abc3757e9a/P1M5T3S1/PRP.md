name: "P1.M5.T3.S1 — README.md: install, run, client config, behavior, env vars (Mode B changeset-level doc sweep)"
description: |

  Write the project README.md for web-search-prime-fixer, the local MCP
  alias-fixing proxy. This is a MODE B changeset-level documentation sweep (SOW
  §5): all implementing subtasks are complete, so the README must describe the
  SHIPPED behavior verified against the on-disk source, not aspirational text.

  OUTPUT: a single new file `README.md` at the repository root.

  INPUT (final behavior of completed subtasks):
    - P1.M4.T2.S2  rewrite + warning injection (what the agent sees)
    - P1.M1.T2.S2  config discovery + WSPF_* env overrides + validation
    - P1.M1.T4.S2  /healthz, listen, graceful shutdown, bootstrap
    - P1.M4.T3.S1  log levels + events
    - P1.M5.T2.S1  (PARALLEL, in flight) verified build/test gate + stdlib-only
                   audit + version injection. Treat its PRP as a CONTRACT: the
                   module builds (`go build -o web-search-prime-fixer .`), tests
                   pass (`go test ./...`), and /healthz reflects an ldflags
                   version. The README cites these facts with confidence.

  SCOPE BOUNDARY: this item writes ONLY `README.md`. The sibling P1.M5.T3.S2 owns
  `config.example.json` and the `doc.go` package-comment polish. config.example.json
  does NOT exist yet at planning time. Therefore the README documents the config
  schema INLINE (the schema is stable, read from config.go) and POINTS to
  `config.example.json` ("see config.example.json for a ready-to-copy example")
  so the cross-reference resolves once S2 lands. Do NOT create or modify
  config.example.json, doc.go, or any source file.

  STYLE: use the write-tech-docs skill rules (no em dashes, no marketing
  tell-words, no hedging, do not narrate the codebase). Run its linter and reach
  exit 0. No marketing tone. Concise and accurate.

---

## Goal

**Feature Goal**: Ship a single `README.md` at the repo root that lets a new
  operator (a developer running one local instance for their own agents) install,
  configure, run, and point an MCP client at the proxy, and that accurately
  documents the shipped v1.0 behavior: the one rewrite rule, the exact warning the
  agent sees, the client config block, /healthz, the config file format + WSPF_*
  env vars, and the explicit non-goals.

**Deliverable**: `README.md` (new file, repo root). Markdown only. Passes the
  write-tech-docs linter (exit 0). Every command and every JSON block in it is
  runnable/accurate against the shipped binary.

**Success Definition**:
  - A reader who knows nothing about this repo can, from the README alone: build
    the binary, start it, point an MCP client at it, and predict exactly what
    happens on a `{"query": "x"}` call vs a `{"search_query": "x"}` call.
  - The README matches SHIPPED behavior on every claim (verified against
    rewrite.go, sse.go, config.go, main.go). No aspirational or future-tense
    statements about features that are not yet shipped.
  - `bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md`
    exits 0 (no em dashes, no marketing tell-words, no long paragraphs).
  - The README does not duplicate or conflict with sibling P1.M5.T3.S2
    (config.example.json + doc.go); it references config.example.json by name.

## User Persona

**Target User**: A single developer (the "operator") running one local instance of
  the proxy for their own MCP clients (pi, Claude Code, Cursor, etc.).

**Use Case**: The operator's agent keeps calling the z.ai web-search-prime tool
  with `query` instead of `search_query`, getting empty or wrong results and
  retrying. They build this proxy, start it, change one URL in their client
  config, and the next `{"query": "..."}` call just works and tells the agent the
  right name.

**User Journey** (what the README must enable, in order):
  1. Read the opening: what it is, what problem it solves (1-2 sentences).
  2. Build: `go build -o web-search-prime-fixer .`.
  3. Run: `./web-search-prime-fixer` (defaults) or with WSPF_* env.
  4. Configure the MCP client: paste the §7.1 JSON block, change only the URL.
  5. Know what the agent sees on a misspelled call (the warning example).
  6. Confirm it is alive: `curl http://127.0.0.1:8787/healthz`.
  7. (Optional) drop a config file or set env vars to change listen/upstream/log level.

**Pain Points Addressed**: Misspelled `search_query` parameter causing empty/wrong
  results and wasted agent turns; the tedium of a per-call schema fix; uncertainty
  about what the proxy will and will not change.

## Why

- The proxy solves the single most common z.ai web-search-prime failure mode
  (agents send `query`, not `search_query`) with a one-line client config change
  and no behavior change on correct calls (PRD §1, §21).
- The README is the entry point for the shipped v1.0 binary: it is the only
  artifact a new operator reads before building and running. Accuracy here is the
  difference between a working proxy and a confused operator.
- It is the Mode B changeset-level documentation sweep: it must summarize the
  final behavior of all implementing subtasks (P1.M1.T2.S2, P1.M1.T4.S2,
  P1.M4.T2.S2, P1.M4.T3.S1) in one accurate document.

## What

A new `README.md` at the repo root, written in the write-tech-docs voice. It must
cover, at minimum, the sections listed in the Implementation Tasks below, using
the exact shipped facts in the research file
(`plan/001_c0abc3757e9a/P1M5T3S1/research/shipped-behavior-and-conventions.md`).

Non-negotiable accuracy points (each verified against source):
- The one rewrite rule: alias list, target, promote-vs-drop behavior.
- The EXACT warning string the agent sees (must match sse.go warningText output,
  which matches PRD §7.2 byte-for-byte).
- The EXACT client config JSON (PRD §7.1), with the note that only the URL
  changes and the Authorization header is still carried by the client.
- /healthz body shape and the version default/injection.
- All four WSPF_* env vars with their exact precedence and the WSPF_CONFIG
  "verbatim, missing = error" gotcha.
- Config file discovery order and the default config values.
- The non-goals list (PRD §3) verbatim in spirit.
- Build/run/test commands (PRD §18).

### Success Criteria

- [ ] `README.md` exists at repo root and is valid Markdown.
- [ ] Opening 1-2 sentences state what it is and who it is for (no "Welcome to").
- [ ] Install/build section has the exact, tested `go build -o web-search-prime-fixer .`.
- [ ] Run section shows default run + at least the WSPF_LISTEN and WSPF_CONFIG examples.
- [ ] Client config section contains the PRD §7.1 JSON block verbatim and notes
      only the URL changes + the key is still carried by the client.
- [ ] "What the agent sees" section quotes the PRD §7.2 / warningText example
      verbatim, and states the agent does not need to retry.
- [ ] /healthz section documents `GET /healthz` -> `{"ok":true,"version":"<v>"}`,
      default `dev`, ldflags injection, and that it does not touch upstream.
- [ ] Configuration section lists all four WSPF_* env vars, the discovery order,
      the defaults, and points to config.example.json.
- [ ] Non-goals section lists the PRD §3 items (does NOT normalize
      location/content_size/etc., does not warn about unsupported params, etc.).
- [ ] write-tech-docs linter exits 0 on the file.
- [ ] No em dashes, no marketing tell-words, no hedging/formulaic transitions,
      no codebase narration, no paragraph over ~100 words.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can write this README from this PRP
alone because: (a) every shipped fact is stated verbatim in the research file with
its source file/line; (b) the exact JSON blocks and warning strings to copy are
given; (c) the README's section order is specified as ordered tasks; (d) the
style rules (write-tech-docs) and the linter command + path are given; (e) the
scope boundary forbids touching sibling-owned files and tells the writer how to
reference config.example.json; (f) the runnable smoke commands to verify the
README's claims are listed.

### Documentation & References

```yaml
# MUST READ — the verified shipped-behavior facts (single source of truth for the README).
- docfile: plan/001_c0abc3757e9a/P1M5T3S1/research/shipped-behavior-and-conventions.md
  why: every factual claim the README makes (rewrite rule, exact warning string,
        env vars, defaults, /healthz, non-goals) is here with its source file.
        Copy the JSON blocks and the warning example from here verbatim.
  critical: the warning string and the client config JSON must match the code
        byte-for-byte; do not paraphrase them. The warning has TWO possible
        suffixes (rename vs all-ignored); document the common one and mention the
        variant, or keep the example to the documented §7.2 case.

# MUST READ — the exact client config block + the warning example to quote.
- file: PRD.md
  section: "§7.1 Client configuration" and "§7.2 What the agent sees"
  why: §7.1 is the JSON block to paste; §7.2 is the warning string to quote.
        Both are confirmed to match the shipped code (sse.go warningText, and
        the proxy forwarding the Authorization header verbatim).
  critical: paste the §7.1 JSON exactly; only the url differs from a direct z.ai
        config. Do not add fields the block does not have.

# MUST READ — the build/run/test command contract.
- file: PRD.md
  section: "§18 Building and running"
  why: §18 gives the exact commands: `go build -o web-search-prime-fixer .`,
        the WSPF_LISTEN / WSPF_CONFIG run examples, and `go test ./...`.

# MUST READ — config schema, defaults, discovery, env precedence.
- file: PRD.md
  section: "§14 Configuration (config.go)" (§14.1 schema, §14.2 defaults, §14.3 discovery)
  why: the Configuration section of the README mirrors this. Cross-check against
        config.go (DefaultConfig, ResolveConfig, resolveConfigPath).

# MUST READ — /healthz, version, graceful shutdown.
- file: PRD.md
  section: "§16 Health and operations"
  why: the /healthz body, the `dev` default, the -ldflags injection, and SIGINT/SIGTERM.

# MUST READ — the non-goals to state plainly.
- file: PRD.md
  section: "§3 Non-goals (explicitly out of scope)"
  why: the README's Non-goals section restates these so operators know the proxy
        will NOT touch location/content_size/etc.

# READ — the source files the facts are verified against (do not copy prose from them;
# confirm specifics if any PRD/research detail is ambiguous).
- file: main.go
  section: "healthHandler (~line 143), `var version = "dev"` (~133), bootstrap/routing/shutdown"
  why: confirms /healthz body shape, version var scope, the /healthz + catch-all routing.
- file: config.go
  section: "Config struct, DefaultConfig, ResolveConfig, resolveConfigPath"
  why: confirms JSON keys, defaults, env overrides, discovery order, validation.
- file: rewrite.go
  section: "Rewrite + per-alias note strings"
  why: confirms the promote-vs-drop rule and the exact rename/ignored/dropped notes.
- file: sse.go
  section: "warningText (~line 150)"
  why: confirms the exact `[web-search-prime-fixer] ... . <suffix>` format and the
        two suffix variants.

# MUST USE — the prose style rules and the linter to run before finishing.
- skill: write-tech-docs  (location: /home/dustin/.pi/agent/skills/write-tech-docs/SKILL.md)
  why: item contract says "use the write-tech-docs skill if available; no marketing tone."
        Hard rules: no em dashes, no marketing tell-words, no hedging, do not narrate
        the codebase. Concise, front-loaded, concrete task headings, real examples.
  critical: run the linter and reach exit 0:
        bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md

# READ — the parallel gate PRP (contract that the module builds + tests pass + stdlib-only).
- docfile: plan/001_c0abc3757e9a/P1M5T2S1/PRP.md
  why: proves the README can state "Go standard library only" and "go test ./..."
        with confidence. Cite `go test ./...` as the test command.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod                  # module web-search-prime-fixer; go 1.22  (ZERO requires; stdlib only)
  doc.go                  # package main comment (S2 may polish; do NOT edit here)
  main.go                 # bootstrap, /healthz, version var, routing, graceful shutdown
  config.go               # Config + DefaultConfig + ResolveConfig + resolveConfigPath
  proxy.go                # request handling, forwarding, conditional SSE injection
  rewrite.go              # Rewrite (the one rename rule) + note strings
  sse.go                  # SSE reader + Inject + warningText (exact warning format)
  *_test.go               # tests (71 funcs / 10 files); run with `go test ./...`
  testdata/*.sse          # golden SSE fixtures
  .gitignore              # ignores /web-search-prime-fixer (build artifact) + .pi-subagents/
  PRD.md                  # design doc (read-only)
  # NOTE: README.md does NOT exist yet (this item creates it).
  # NOTE: config.example.json does NOT exist yet (sibling P1.M5.T3.S2 creates it).
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
README.md                 # NEW (this item). Single source of operator-facing truth for
                          # the shipped v1.0 binary: what it is, why, install, run,
                          # client config, what the agent sees, /healthz, configuration
                          # (file + WSPF_* env), non-goals. References config.example.json
                          # (created by S2) for the ready-to-copy config example.
# No other file is created or modified by this item.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — Mode B accuracy. The README must describe SHIPPED behavior. Every
  factual claim is verified in research/shipped-behavior-and-conventions.md
  against the on-disk source. Do NOT document intended/future behavior, and do
  NOT copy prose from the PRD that describes something the code does differently
  (where they differ, the CODE wins; re-check rewrite.go / sse.go / config.go).

CRITICAL — the warning string has TWO suffix variants (sse.go warningText):
  - rename or dropped present:  ` Use "search_query" in future calls.`
  - ALL notes are "ignored":    ` Use only "search_query" to avoid this notice.`
  The README's main example should be the PRD §7.2 case (a rename):
  [web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
  Mention the all-ignored variant in one line so operators are not surprised.

CRITICAL — the client config block (PRD §7.1) must be pasted VERBATIM. Only the
  url differs from a direct z.ai config (it points at the proxy). The
  Authorization header stays on the client; the proxy forwards it verbatim and
  holds no key (PRD §13/FR-4). Do NOT add a "proxy adds its own key" claim.

CRITICAL — config.example.json is owned by sibling P1.M5.T3.S2 and does NOT
  exist yet. The README must DOCUMENT the config schema inline (stable, from
  config.go) and REFERENCE config.example.json by name ("see config.example.json
  for a ready-to-copy example"). Do NOT create config.example.json, and do NOT
  hard-depend on its exact contents/wording.

CRITICAL — write-tech-docs hard rule: NO em dashes (U+2014) and no " -- "
  pseudo-dashes anywhere. The linter fails the file on any hit. Use a colon,
  parentheses, comma, or period. This is the single most common failure.

CRITICAL — no marketing tell-words (powerful, robust, elegant, seamless,
  comprehensive, lightweight (unless measured), leverage, utilize, unlock,
  empower, streamline, etc.). Replace with evidence or delete. The linter flags them.

CRITICAL — do NOT narrate the codebase. The reader does not need a file-by-file
  walkthrough. Document what the code cannot show on its own: what it is, why it
  exists, how to use it, and the gotchas (the WSPF_CONFIG verbatim gotcha, the
  two warning suffixes, the "only the URL changes" client note, the non-goals).

GOTCHA — keep prose paragraphs under ~4 sentences / 100 words. The linter flags
  long paragraphs. Prefer lists, tables, and code blocks over prose.

GOTCHA — version in /healthz defaults to `dev` and is set at BUILD time via
  `-ldflags "-X main.version=..."`. Document both: a plain `go build` yields
  `version:"dev"`; a flagged build yields the injected value.

GOTCHA — WSPF_CONFIG is used VERBATIM. If the file it names is missing, the proxy
  exits with an error rather than falling back to defaults. State this explicitly;
  it is the one env var whose "missing" is fatal.

GOTCHA — the proxy binds 127.0.0.1 (local only). State this. It is not a
  general-purpose or open proxy (PRD §6).

GOTCHA — go.sum does NOT exist (zero-requires module). Do not tell readers to run
  `go mod download` or mention a go.sum; `go build`/`go test` work with no network.
```

## Implementation Blueprint

### Data models and structure

None. This item produces a Markdown document, not code. There are no types,
schemas, or models to create. (The config SCHEMA is documented in the README, but
it already exists in config.go; the README only describes it.)

### Implementation Tasks (the README's section order; write top to bottom)

> These tasks ARE the README's sections, in the order a reader needs them. Write
> each section, then move on. Keep each section to its job (one job per section).
> Pull every factual claim from the research file; pull the JSON blocks and the
> warning string verbatim from PRD §7.1 / §7.2 (which match the shipped code).

```yaml
Task 0: VERIFY INPUTS before writing
  - READ: plan/001_c0abc3757e9a/P1M5T3S1/research/shipped-behavior-and-conventions.md
  - CONFIRM: README.md does not yet exist (ls README.md -> not found). If it
    exists, this is a revision, not a creation; read it first and reconcile.
  - CONFIRM: the shipped facts (warning string, env vars, defaults) by glancing at
    sse.go warningText, config.go ResolveConfig/DefaultConfig, main.go healthHandler.
  - DO NOT read or modify config.example.json or doc.go (owned by S2).

Task 1: TITLE + OPENING (what it is, who it is for)
  - WRITE: a top-level `# web-search-prime-fixer` heading.
  - WRITE: 1-2 sentences. First sentence: what it is (a local proxy that renames
    `query` -> `search_query` for the z.ai web-search-prime MCP tool) and who it
    is for (a developer running one local instance for their own MCP clients).
  - FOLLOW: write-tech-docs "front-load the answer"; no "Welcome to", no emoji
    fanfare, no mission statement. State the problem in one sentence (agents send
    `query`, not `search_query`, get empty/wrong results, and retry).
  - NAMING: refer to the product as "web-search-prime-fixer" or "the proxy"
    consistently. One name per concept.

Task 2: HOW IT WORKS (the one rewrite rule + the warning)
  - WRITE: a short "How it works" section.
  - STATE the rule (from rewrite.go / PRD §10/FR-2): for a tools/call, if the
    arguments contain a configured alias for search_query:
      * if search_query is ABSENT, the first present alias (config order) is
        promoted to search_query and the other aliases are removed;
      * if search_query is PRESENT, all aliases are removed (canonical wins);
      * every other parameter is left untouched.
  - LIST the default aliases: query, q, search, searchQuery, search_term.
  - STATE the target is always search_query.
  - KEEP to ~4-6 lines. This is behavior, not a code walkthrough.

Task 3: WHAT THE AGENT SEES (the warning example)
  - WRITE: a "What the agent sees" section.
  - PASTE the exact shipped warning (PRD §7.2 == sse.go warningText), in a code
    block, as the primary example:
      [web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
  - STATE: the warning is prepended to the real result; the agent gets the correct
    results on the first call and does not need to retry.
  - STATE (one line): when search_query was already present and only aliases were
    ignored, the suffix is ` Use only "search_query" to avoid this notice.` instead.
  - STATE: a correct call ({"search_query": "x"}) passes through with no warning
    and is byte-for-byte identical to talking to z.ai directly.

Task 4: INSTALL / BUILD
  - WRITE: an "Install" or "Build" section.
  - STATE prerequisite: Go 1.22+ (matches go.mod). Stdlib only, no dependencies,
    no network needed to build.
  - GIVE the exact command (PRD §18): `go build -o web-search-prime-fixer .`
  - STATE the produced binary is `./web-search-prime-fixer`.
  - (Optional, one line) version injection: `go build -ldflags "-X main.version=0.1.0" -o web-search-prime-fixer .`
    sets the /healthz version; a plain build reports `dev`.

Task 5: RUN
  - WRITE: a "Run" section.
  - GIVE the default run (PRD §18): `./web-search-prime-fixer`
    listens on 127.0.0.1:8787, forwards to the z.ai upstream, no config file needed.
  - GIVE the two env examples (PRD §18):
      WSPF_LISTEN=127.0.0.1:9000 ./web-search-prime-fixer
      WSPF_CONFIG=./my-config.json ./web-search-prime-fixer
  - STATE: binds 127.0.0.1 (local only). Graceful shutdown on Ctrl-C / SIGTERM.

Task 6: CONFIGURE YOUR MCP CLIENT
  - WRITE: a "Configure your MCP client" section.
  - PASTE the PRD §7.1 JSON block VERBATIM in a code block (the mcpServers /
    web-search-prime / type http / url / headers.Authorization block).
  - STATE: this is identical to a direct z.ai config EXCEPT the url points at the
    proxy (http://127.0.0.1:8787/mcp). The Authorization header stays on the
    client; the proxy forwards it verbatim and holds no key.
  - STATE: replace `your_api_key` with your z.ai API key.

Task 7: HEALTH CHECK
  - WRITE: a short "Health check" section.
  - GIVE: `curl http://127.0.0.1:8787/healthz`
  - STATE the response: `200` with body `{"ok":true,"version":"<version>"}`.
  - STATE: /healthz does not call the upstream; it is a pure local liveness check.

Task 8: CONFIGURATION (file + env vars)
  - WRITE: a "Configuration" section.
  - STATE: the proxy runs with no config file (built-in defaults). To override,
    set env vars or drop a JSON config file.
  - ENV VARS table or list (exact, from config.go):
      WSPF_CONFIG     config file path (VERBATIM; a missing file is a hard error,
                      not a silent fallback to defaults)
      WSPF_UPSTREAM   overrides the upstream z.ai MCP URL
      WSPF_LISTEN     overrides the bind address
      WSPF_LOG_LEVEL  debug | info | warn | error (default info)
  - CONFIG FILE DISCOVERY order (PRD §14.3 / resolveConfigPath):
      1. WSPF_CONFIG if set (verbatim)
      2. else first existing of:
         ./web-search-prime-fixer.json
         $XDG_CONFIG_HOME/web-search-prime-fixer/config.json
         (defaults to ~/.config/web-search-prime-fixer/config.json)
      3. else: built-in defaults
  - CONFIG SCHEMA: list the JSON keys (upstream, listen, path, aliases,
    target_param, log_level) and the DEFAULT values (from DefaultConfig):
      upstream     https://api.z.ai/api/mcp/web_search_prime/mcp
      listen       127.0.0.1:8787
      path         /mcp   (informational; all non-/healthz paths forward to upstream)
      aliases      ["query","q","search","searchQuery","search_term"]
      target_param search_query   (forced to search_query if empty)
      log_level    info
  - POINTER: "See config.example.json for a ready-to-copy example." (S2 creates it.)
  - STATE precedence: env vars override the file; the file overrides defaults.

Task 9: NON-GOALS (what it deliberately does NOT do)
  - WRITE: a "Non-goals" section.
  - LIST (PRD §3): the proxy does NOT
      * infer or default location
      * normalize content_size, search_recency_filter, or any enum
      * truncate or shorten the query text
      * drop, map, or warn about unsupported parameters (e.g. max_results,
        safe_search); they pass through untouched
      * rewrite the tool schema or the tools/list response (forwarded verbatim)
      * manage API keys or rotate credentials
      * retry failed upstream calls, rate-limit, or cache results
  - STATE the rule of thumb: if a behavior is not "rename a configured alias to
    search_query", it passes through unchanged.

Task 10: TESTS (one short section)
  - WRITE: a "Tests" section (one or two lines).
  - GIVE: `go test ./...`
  - STATE: the suite uses httptest fakes and golden fixtures; it makes no network
    calls and needs no credentials.

Task 11: LICENSE / trailing (optional, only if the repo has one)
  - IF a LICENSE file exists at repo root, state the license plainly in one line.
  - IF none exists, OMIT this section (write-tech-docs: no template-only sections).

Task 12: LINT + VERIFY
  - RUN: bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md
  - EXPECT: exit 0. Fix every hit (em dashes, tell-words, long paragraphs) and
    re-run until clean.
  - RUN the smoke commands from the README against the shipped binary to confirm
    every command and JSON block is accurate (see Validation Loop).
```

### Implementation Patterns & Key Details

```markdown
# PATTERN: every literal (command, JSON block, warning string) goes in a fenced
# code block. Copy the client config JSON and the warning string verbatim from
# the research file / PRD §7.1 / §7.2. Do not retype them from memory.

# PATTERN: one job per section. "Configure your MCP client" does only that; it
# does not also explain the rewrite rule. Split if a section does two things.

# PATTERN: concrete task headings (Build, Run, Configure your MCP client,
# Health check, Configuration, Non-goals, Tests), not cute labels.

# PATTERN: second person ("you") for the guide parts (Build/Run/Configure);
# imperative mood for steps ("Run ...", "Paste ..."). Reference voice elsewhere.

# PATTERN: the README is self-sufficient about the config schema (documented
# inline) AND points to config.example.json for the ready-to-copy file. This way
# the README is accurate whether or not S2 has landed yet.

# GOTCHA: write-tech-docs forbids em dashes. When you would naturally write an
# em dash, use a colon, parentheses, comma, or period. Example: instead of
# "the proxy - a local process - forwards..." write "the proxy (a local process)
# forwards...". Scan the whole file for U+2014 and " -- " before finishing.
```

### Integration Points

```yaml
FILE PRODUCED:
  - README.md   # NEW, repo root. Markdown. Passes the write-tech-docs linter.

FILES REFERENCED (not created/modified by this item):
  - config.example.json   # created by sibling P1.M5.T3.S2. README points to it
                          # by name; the README is accurate without it because the
                          # schema is documented inline.
  - doc.go                # S2 may polish the package comment. Do not edit here.

FILES NEVER MODIFIED (contract):
  - go.mod, any *_test.go, *.go source, PRD.md, tasks.json, prd_snapshot.md,
    .gitignore. This item writes ONLY README.md.

CROSS-ITEM COHERENCE:
  - The README cites `go test ./...` and "Go standard library only" with
    confidence because the parallel gate (P1.M5.T2.S1) proves them. If, at write
    time, the gate has NOT yet passed, state the build/test commands as the
    intended commands (they are the PRD §18 contract) rather than asserting a
    green result you cannot see.
  - The README must not claim features that are not shipped. Everything in the
    research file is shipped (verified against source).
```

## Validation Loop

### Level 1: Prose quality (write-tech-docs linter)

```bash
# Run after writing the README. Fix every hit; re-run until exit 0.
bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md
# Expected: exit 0. Common hits: em dashes (U+2014), " -- ", marketing
# tell-words, paragraphs over 100 words. Replace or cut each.

# Manual tell-word scan (belt and suspenders):
grep -nE 'powerful|robust|elegant|seamless|comprehensive|leverage|utilize|unlock|empower|streamline|elevate|lightweight|scalable' README.md
# Expected: no matches (or only inside a literal code block where it is data).

# Em-dash scan (the linter covers this, but double-check):
grep -nP '\x{2014}| -- ' README.md
# Expected: no matches.
```

### Level 2: Accuracy against shipped source

```bash
# The warning string in the README must equal sse.go warningText output for a
# single rename. Confirm the documented example matches:
grep -F '[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.' README.md
# Expected: one match (the example block).

# The client config block must contain the §7.1 url and the Authorization header:
grep -F 'http://127.0.0.1:8787/mcp' README.md
grep -F '"Authorization": "Bearer your_api_key"' README.md
# Expected: one match each.

# All four env vars must appear:
for v in WSPF_CONFIG WSPF_UPSTREAM WSPF_LISTEN WSPF_LOG_LEVEL; do
  printf '%s: ' "$v"; grep -c "$v" README.md
done
# Expected: each >= 1.

# The defaults and the non-goals must appear:
grep -F 'search_query' README.md            # target + aliases context
grep -F '127.0.0.1:8787' README.md          # default listen
grep -iE 'non-goals|does not' README.md     # non-goals section present
grep -F 'config.example.json' README.md     # pointer to S2's file
# Expected: all present.
```

### Level 3: Runnable smoke (every command in the README actually works)

```bash
# Build with the README's exact command:
go build -o web-search-prime-fixer .
# Expected: exit 0; ./web-search-prime-fixer is executable.

# Run on an alt port (avoid clashing with any running instance) and hit /healthz:
WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer & PID=$!
sleep 1.2
curl -s http://127.0.0.1:18787/healthz        # Expected: {"ok":true,"version":"dev"}
kill $PID; wait $PID 2>/dev/null
# GOTCHA: kill by saved $PID only. Never `pkill -f wspf` (it matches this shell).

# Tests (the README cites this command):
go test ./...
# Expected: ok  web-search-prime-fixer ...  (green; the parallel gate confirms it).

# Expected: every command copied out of the README runs as documented.
```

### Level 4: Markdown sanity + cross-references

```bash
# Valid Markdown, no broken in-repo links. If the README links to config.example.json,
# note it is created by S2 (the link resolves once S2 lands; until then it is a
# forward reference, which is acceptable for a sibling in the same changeset).
grep -nE '\]\(' README.md        # list all markdown links; eyeball them.

# Render check (if a renderer is handy):
#   glow README.md  | head -60    # or: mdcat README.md | head -60
# Expected: headings render as a clean task-oriented outline; code blocks intact.

# Length sanity: a focused README, not a novella. Rough target: under ~250 lines.
wc -l README.md
```

## Final Validation Checklist

### Technical Validation

- [ ] `README.md` exists at repo root and is valid Markdown.
- [ ] write-tech-docs linter exits 0:
      `bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md`.
- [ ] No em dashes / " -- " anywhere (`grep -nP '\x{2014}| -- ' README.md` empty).
- [ ] No marketing tell-words (manual grep empty outside literal code blocks).
- [ ] No prose paragraph over ~100 words (linter-enforced).
- [ ] Every command in the README runs as documented (Level 3 smoke).

### Feature Validation (accuracy vs shipped behavior)

- [ ] Opening states what it is + who it is for in 1-2 sentences.
- [ ] The one rewrite rule is stated correctly (promote-vs-drop, aliases, target).
- [ ] The warning example matches sse.go warningText / PRD §7.2 byte-for-byte; the
      all-ignored suffix variant is mentioned.
- [ ] The client config JSON matches PRD §7.1 verbatim; notes only the URL changes
      and the key stays on the client.
- [ ] /healthz documented: body, `dev` default, ldflags injection, no upstream call.
- [ ] All four WSPF_* env vars documented, including WSPF_CONFIG verbatim/fatal
      gotcha; discovery order and defaults listed; precedence stated.
- [ ] Non-goals list present (PRD §3): no location/content_size normalization, no
      query truncation, no warning on unsupported params, no schema rewrite, no key
      management, no retry/rate-limit/cache.
- [ ] config.example.json referenced by name (S2 owns it; schema documented inline).

### Code Quality / Scope Validation

- [ ] ONLY `README.md` was created/modified. No other file touched.
- [ ] No edits to go.mod, source files, doc.go, config.example.json, PRD.md,
      tasks.json, prd_snapshot.md, or .gitignore.
- [ ] Consistent terminology (one name per concept: "the proxy", "search_query",
      "alias", "upstream").
- [ ] No template-only sections (e.g. a Contributing section with no content).

### Documentation & Deployment

- [ ] README is self-sufficient for the operator journey (build -> run -> configure
      client -> know what the agent sees -> health check).
- [ ] Cross-references (config.example.json) are forward references that resolve
      once the sibling item lands.
- [ ] No aspirational/future-tense claims about unshipped features.

---

## Anti-Patterns to Avoid

- ❌ Don't document intended behavior. Mode B requires SHIPPED behavior. Where the
  PRD and the code differ, the code wins (re-check rewrite.go / sse.go / config.go).
- ❌ Don't use em dashes (U+2014) or " -- ". The linter fails the file. Use a colon,
  parentheses, comma, or period.
- ❌ Don't use marketing tell-words (powerful, robust, seamless, comprehensive,
  leverage, utilize, unlock, empower, streamline, etc.). Replace with evidence or cut.
- ❌ Don't narrate the codebase (no file-by-file walkthrough). Document what the code
  cannot show: what it is, why, how to use it, the gotchas.
- ❌ Don't paraphrase the warning string or the client config JSON. Paste them
  verbatim; both must match the shipped output byte-for-byte.
- ❌ Don't create or modify config.example.json, doc.go, or any source file. S2 owns
  config.example.json and the doc.go comment. Reference config.example.json by name.
- ❌ Don't claim the proxy manages keys, normalizes enums, retries, or caches. It
  does not (PRD §3). State the non-goals plainly.
- ❌ Don't add template-only sections (Contributing, Roadmap, Acknowledgements) with
  no real content. Cut them.
- ❌ Don't write long prose paragraphs. Keep under ~4 sentences / 100 words; prefer
  lists, tables, and code blocks.
- ❌ Don't edit PRD.md, tasks.json, prd_snapshot.md, go.mod, or .gitignore.

---

## Confidence Score

**9/10** for one-pass success. Every factual claim the README needs is already
verified against the on-disk source in the research file (the exact warning
string from sse.go warningText, the exact client config JSON from PRD §7.1 which
matches the shipped forwarding behavior, the env vars and defaults from
config.go, the /healthz shape from main.go). The style rules (write-tech-docs)
and the linter command + absolute path are given, so the prose-quality gate is
deterministic. The build/test/run smoke commands are confirmed runnable. The only
residual risk is the forward reference to config.example.json (owned by the
parallel sibling S2): the README is written to be accurate with OR without that
file present (schema documented inline, file referenced by name), so the
cross-reference resolves cleanly once S2 lands. Deducted 1 point for that
cross-item coupling and for the inherent subjectivity of the linter's paragraph
-length and tell-word checks (mitigated by the explicit grep scans in Level 1).
