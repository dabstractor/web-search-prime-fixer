# PRP — P1.M1.T2.S1: Add SDK dependency; extract logger→logger.go and health→health.go; update logStartup

## Goal

**Feature Goal**: Refactor the existing `main.go` into three files — move the
redacting structured logger into a dedicated `logger.go`, move `/healthz` into a
dedicated `health.go`, and leave a trimmed `main.go` whose `logStartup` now emits
the **v2** config fields (`tools`, `canonical_tool`, `query_aliases`) instead of
the deleted v1 `aliases` field — and add the official Go MCP SDK
(`github.com/modelcontextprotocol/go-sdk v1.6.1`) as the project's sole external
dependency. Behavior of the moved logger and health code is **unchanged** (pure
mechanical extraction); only `logStartup`'s emitted field set changes. This
prepares the ground for P1.M1.T2.S2 (mount the SDK `StreamableHTTPHandler` and
delete the v1 proxy/sse/rewrite code).

**Deliverable**: Four artifacts in the repo root
(`/home/dustin/projects/web-search-prime-fixer`):
1. `logger.go` (NEW) — `package main`: the `levelDebug/Info/Warn/Error` consts,
   `levelNum`, `type logger`, `newLogger`, `(*logger).log`, `redactHeaders`,
   moved verbatim from `main.go`. Imports: `encoding/json`, `io`, `net/http`,
   `time`.
2. `health.go` (NEW) — `package main`: `var version`, `healthHandler`, moved
   verbatim from `main.go`. Imports: `encoding/json`, `net/http`.
3. `main.go` (EDITED) — `package main`: the moved definitions are **removed**;
   `main()` stays (its proxy references are expected-broken until T2.S2);
   `logStartup` is **updated** to emit `tools`, `canonical_tool`, `query_aliases`,
   `listen`, `upstream`, `log_level` (dropping the deleted `aliases`). Imports
   trimmed to `context`, `net/http`, `os`, `os/signal`, `syscall`, `time`.
4. `go.mod` (EDITED) + `go.sum` (NEW) — `require github.com/modelcontextprotocol/go-sdk v1.6.1` added (plus the indirect deps `go mod tidy` resolves); the `go` directive
   **bumps `1.22 → 1.25.0`** (the SDK requires it — verified); a one-line `//`
   comment above the SDK require notes it is the sole external dependency
   (PRD §7/§13).

**Success Definition**: `logger.go` and `health.go` each compile cleanly in an
isolated temp-module gate (stdlib only); `logStartup` compiles against the v2
`Config` and emits exactly the six v2 fields (verified by a throwaway isolated
test); the SDK dependency resolves and builds in an isolated temp module
(`go build ./...` of an `import ".../mcp"` program exits 0); and the real repo's
`go build ./...` is **expected** to remain red — but **only** on the out-of-scope
files (`proxy.go`, `*_test.go` referencing the deleted `cfg.Aliases`), never
inside `logger.go`/`health.go`/`main.go`/`config.go`. No file other than these
four (go.mod, logger.go, health.go, main.go) plus go.sum is created or modified.

## User Persona (if applicable)

**Target User**: Developer/maintainer of the v2 normalizing MCP server.
**Use Case**: After the v1→v2 pivot (PRD §0), the monolithic `main.go` must be
split so that `logger.go`/`health.go` survive the deletion of `proxy.go`
(P1.M1.T2.S2) and the logger/health can be reused by the SDK-based server.
**User Journey**: `go build ./...` after this subtask fails only on the v1 proxy
+ test files (the S2-era breakage set), proving the extracted infrastructure is
intact and ready for the handler-mount subtask.
**Pain Points Addressed**: A single `main.go` mixing logger, health, bootstrap,
and v1-proxy wiring is the top obstacle to cleanly swapping the handler; this
subtask isolates the two reusable pieces and fixes the one stale v1 config
reference (`cfg.Aliases`) inside `main.go`.

## Why

- The v2 architecture (PRD §14) lists `logger.go` and `health.go` as **separate
  files** that carry over from v1. Splitting them now means T2.S2 can delete
  `proxy.go` without losing the logger or `/healthz`.
- `main.go`'s `logStartup` currently references `cfg.Aliases` (main.go:160), which
  **no longer exists** in the v2 `Config` (renamed to `QueryAliases` in T1.S1).
  This is the one compile error inside `main.go` that THIS subtask owns and fixes;
  updating it to emit `tools`/`canonical_tool`/`query_aliases` aligns the startup
  log with PRD §15 ("startup: resolved config — advertised tools, alias order,
  canonical, listen, upstream, log level").
- Adding the SDK dependency (PRD §13, FR-1) is the foundational step for the whole
  v2 server: every later subtask (server.go, upstream.go, the in-memory test
  transports) imports `github.com/modelcontextprotocol/go-sdk/mcp`. Pinning v1.6.1
  now lets T2.S2 mount the handler without further module juggling.
- The go.mod Mode-A comment records that the SDK is the **sole** external
  dependency (PRD §7), so future reviewers/agents don't reintroduce the
  "stdlib-only" assumption that v1 held.

## What

This is a **mechanical extraction + one small behavior update + a dependency add**.
Concretely:

1. **go.mod / go.sum**: run `go get github.com/modelcontextprotocol/go-sdk@v1.6.1`
   then `go mod tidy` (default GOPROXY — **network required**, see Gotchas). The
   `go` directive becomes `1.25.0`; a `go.sum` (~20 lines) is created. After tidy,
   add the one-line `//` comment above the SDK require.
2. **logger.go**: cut the level consts, `levelNum`, `logger` struct, `newLogger`,
   `(*logger).log`, and `redactHeaders` from `main.go` and paste them (with their
   doc comments, verbatim) into a new `logger.go` with imports
   `encoding/json`/`io`/`net/http`/`time`. No logic change.
3. **health.go**: cut `var version` and `healthHandler` from `main.go` and paste
   them (with doc comments, verbatim) into a new `health.go` with imports
   `encoding/json`/`net/http`. No logic change.
4. **main.go**: delete the cut blocks; **update `logStartup`** to the v2 field set;
   trim the import block to what `main()` + `logStartup` still use
   (`context`/`net/http`/`os`/`os/signal`/`syscall`/`time`). Leave `main()`'s body
   as-is (its `newProxyHandler`/`newUpstreamClient` calls stay — they live in
   `proxy.go`, which T2.S2 deletes and replaces).

`main()` is **not** rewired to the SDK here — that is P1.M1.T2.S2. After this
subtask the full package is still red because `proxy.go` and the v1 test files
reference the deleted `cfg.Aliases`; that breakage is owned by T2.S2 (delete
proxy) and T3.S1 (rewrite tests), NOT this subtask. The isolated temp-module
gates below prove the four deliverables are correct without needing the whole
package to build.

### Success Criteria

- [ ] `logger.go` exists, is `package main`, contains (verbatim from main.go) the
      `levelDebug/levelInfo/levelWarn/levelError` consts, `levelNum`, `type logger`,
      `newLogger`, `(*logger).log`, `redactHeaders`, and imports exactly
      `encoding/json`, `io`, `net/http`, `time`.
- [ ] `health.go` exists, is `package main`, contains (verbatim) `var version` and
      `healthHandler`, and imports exactly `encoding/json`, `net/http`.
- [ ] `main.go` no longer defines any of the moved symbols; it retains `main()` and
      a `logStartup` that references `cfg.Tools`, `cfg.CanonicalTool`,
      `cfg.QueryAliases`, `cfg.Listen`, `cfg.Upstream`, `cfg.LogLevel` and does NOT
      reference `cfg.Aliases`.
- [ ] `main.go` imports exactly `context`, `net/http`, `os`, `os/signal`,
      `syscall`, `time` (no unused imports).
- [ ] `go.mod` requires `github.com/modelcontextprotocol/go-sdk v1.6.1`, has the
      `go 1.25.0` directive (NOT `1.22`), and a one-line `//` comment above the SDK
      require noting it is the sole external dependency.
- [ ] `go.sum` exists (~20 lines; SDK + its transitive deps).
- [ ] Isolated gate: `logger.go` and `health.go` each `go vet`+`go build` in a
      standalone temp module.
- [ ] Isolated gate: `logStartup` (in a temp module with `config.go`+`logger.go`)
      emits a JSON line containing keys `tools`, `canonical_tool`, `query_aliases`,
      `listen`, `upstream`, `log_level` and NOT `aliases`.
- [ ] Isolated gate: a temp module importing `github.com/modelcontextprotocol/go-sdk/mcp` builds with the new `go.mod` (`go build ./...` exit 0).
- [ ] The real repo `go build ./...` fails ONLY on `proxy.go` + `*_test.go`
      `.Aliases`/v1-proxy references (the T1-era set) — no error originates inside
      `logger.go`/`health.go`/`main.go`/`config.go`.

## All Needed Context

### Context Completeness Check

_Pass._ Every code block to move is quoted verbatim below from the current
`main.go`; the exact `logStartup` replacement is given; the exact `go get`/`go mod
tidy` sequence and its **verified** effect on `go.mod` (the `1.22→1.25.0` bump, the
indirect-dep set, no `toolchain` line) and `go.sum` are reproduced on-disk in
`research/sdk_dependency_findings.md`. The one contract claim that is FALSE ("go
mod tidy needs no network") is corrected with a verified happy path. An agent with
no prior knowledge of this repo can implement this from the PRP + the quoted source.

### Documentation & References

```yaml
# MUST READ — the decision that justifies the dependency + the logging/health spec.
- file: PRD.md
  why: §13 "MCP SDK" (adopt go-sdk v1.6.1; "no go.sum" purity explicitly
        relinquished); §7 (SDK is the SOLE external dependency); §15 "Logging"
        (startup emits advertised tools, alias order, canonical, listen, upstream,
        log level; no credentials); §16 "Health" (GET /healthz, version via -ldflags);
        §14 file layout (logger.go + health.go are separate files).
  critical: §13/§7 => the go directive bump + go.sum are EXPECTED, not regressions.
        §15 => logStartup's new field set is `tools`, `canonical_tool`,
        `query_aliases`, `listen`, `upstream`, `log_level` (drop `aliases`).

# PREDECESSOR — defines the v2 Config that logStartup must reference.
- file: config.go
  why: T1.S1 made Config v2. logStartup MUST use cfg.Tools, cfg.CanonicalTool,
        cfg.QueryAliases (NOT cfg.Aliases). The current main.go:160 `cfg.Aliases`
        is the compile error this subtask fixes.
  critical: QueryAliases is []string; Tools is []string; CanonicalTool is string.
        All three log cleanly as JSON (array/string/string). Config has NO
        credential field, so logging it can never leak a secret (PRD §13).

# PARALLEL SIBLING — owns ResolveConfig validation, NOT this file.
- file: plan/002_0a8ab3410994/P1M1T1S2/PRP.md
  why: T1.S2 edits config.go's ResolveConfig (adds 3 Tools validation checks +
        `slices` import). It does NOT touch main.go/logger.go/health.go/go.mod.
        This subtask does the inverse (extracts from main.go + adds the SDK dep),
        so the two files being edited do NOT overlap.
  critical: Both T1.S2 and T2.S1 leave the full package red (proxy.go + tests
        reference cfg.Aliases). T2.S2 (delete proxy) + T3.S1 (rewrite tests) turn
        it green. Use the isolated gates, NOT `go build ./...`, to validate.

# RESEARCH — the verified dependency mechanics (corrects a false contract claim).
- file: plan/002_0a8ab3410994/P1M1T2S1/research/sdk_dependency_findings.md
  why: On-disk proof that (a) the SDK requires `go 1.25.0` so `go get` bumps the
        directive `1.22 -> 1.25.0`; (b) the SDK's transitive deps
        (jsonschema-go, uritemplate/v3, oauth2, segmentio/encoding, segmentio/asm,
        x/sys, x/tools) are NOT in the module cache — `go mod tidy` NEEDS the
        network (which IS reachable); (c) the full happy path (go get + go mod tidy
        + go build + go vet) exits 0 with network; (d) final go.mod shape + 20-line
        go.sum + NO toolchain line.
  critical: Do NOT use GOPROXY=off. Do NOT revert go 1.25.0 to 1.22 (build breaks).
        The exact `// indirect` set is tidy's choice — run the commands and commit
        the result; do not hand-edit indirect requires.

# PATTERNS — the v1 logger/health conventions to preserve verbatim.
- file: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  why: §2 documents the logger contract (one JSON line per call; ts RFC3339 UTC;
        level filtering debug<info<warn<error; redactHeaders redacts
        Authorization/Cookie/Set-Cookie/Proxy-Authorization). §3 documents health
        (GET only 405-otherwise; {"ok":true,"version":...}; pure local). §6 maps
        the v1->v2 config key rename (aliases -> query_aliases).
  critical: logger/health behavior is UNCHANGED — this is a move, not a rewrite.
        Copy the code + doc comments verbatim. Only logStartup's field set changes.

# Go stdlib / tooling refs.
- url: https://go.dev/ref/mod#go-mod-tidy
  why: `go mod tidy` adds missing module requirements and prunes unused ones,
        writing go.sum entries for everything needed to build/test. Run it after
        adding the require so go.sum is complete.
  critical: tidy NEEDS network here (transitive deps not cached). If the build
        later complains "missing go.sum entry", run `go mod tidy` again.
- url: https://go.dev/ref/mod#go-mod-directive
  why: The `go` directive is the minimum language version. A dependency (the SDK)
        declaring `go 1.25.0` forces the main module's directive up to match (Go's
        "module graph" rule since 1.21).
  critical: This is WHY 1.22 becomes 1.25.0. It is not optional and not a mistake.
```

### Current Codebase tree (the v1 main.go being split)

```bash
# Repo root: /home/dustin/projects/web-search-prime-fixer
#   main.go            <-- THIS FILE is split (logger/health out; logStartup updated)
#     package main; imports: context, encoding/json, io, net/http, os, os/signal,
#                              syscall, time
#     defines (TO MOVE to logger.go):
#       levelDebug/levelInfo/levelWarn/levelError consts, levelNum(),
#       type logger struct, newLogger(), (*logger).log(), redactHeaders()
#     defines (TO MOVE to health.go):
#       var version = "dev", healthHandler()
#     defines (TO STAY + UPDATE): logStartup()   <-- drop cfg.Aliases, add v2 fields
#     defines (TO STAY UNCHANGED): main()         <-- still calls newProxyHandler/
#                                                       newUpstreamClient (proxy.go);
#                                                       T2.S2 replaces these
#   config.go          <-- v2 (T1.S1). logStartup references its v2 fields. UNCHANGED.
#   proxy.go           <-- v1, BROKEN (cfg.Aliases at :498/:499). DELETED in T2.S2.
#   sse.go/rewrite.go  <-- v1, deleted in T2.S2 (no cfg.Aliases refs of their own).
#   *_test.go          <-- v1 tests, BROKEN (.Aliases / v1 proxy refs). T3.S1/T2.S2.
#   go.mod             <-- module web-search-prime-fixer; go 1.22; NO requires. EDITED here.
# (no go.sum yet — created here)
```

### Desired Codebase tree after this subtask

```bash
web-search-prime-fixer/
  go.mod            # EDITED: require go-sdk v1.6.1 + indirect deps; go 1.25.0; 1-line // comment
  go.sum            # NEW (~20 lines): SDK + transitive deps
  logger.go         # NEW: consts + levelNum + logger + newLogger + log + redactHeaders
  health.go         # NEW: var version + healthHandler
  main.go           # EDITED: moved defs removed; logStartup -> v2 fields; imports trimmed
  config.go         # UNCHANGED (v2)
  proxy.go ...      # UNCHANGED (still broken — T2.S2 deletes)
  *_test.go         # UNCHANGED (still broken — T3.S1 rewrites)
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: The SDK dependency REQUIRES NETWORK for `go mod tidy`. The item
// contract says "go mod tidy needs no network" — that is FALSE (verified in
// research/sdk_dependency_findings.md). The SDK's transitive deps
// (jsonschema-go, uritemplate/v3, oauth2, segmentio/encoding, segmentio/asm,
// x/sys, x/tools) are NOT in the module cache; only jwt/go-cmp are. Run the
// commands with the DEFAULT GOPROXY (network IS reachable: proxy.golang.org 200).
// Do NOT set GOPROXY=off — tidy will fail with "module lookup disabled".

// CRITICAL: `go get github.com/modelcontextprotocol/go-sdk@v1.6.1` BUMPS the
// go directive from 1.22 to 1.25.0 ("go: upgraded go 1.22 => 1.25.0"). This is
// UNAVOIDABLE (the SDK's go.mod requires go 1.25.0) and CORRECT (installed
// go1.26.4 >= 1.25.0; no `toolchain` line is added). Do NOT revert go.mod back to
// `go 1.22` — that breaks the SDK build. PRD §13 explicitly relinquished the old
// "no go.sum / fully offline" purity; the go.sum that appears here is expected.

// CRITICAL: This is a MOVE, not a rewrite. logger.go and health.go must reproduce
// the moved functions and their doc comments VERBATIM from main.go (quoting below).
// The only behavior change in the whole subtask is logStartup's field set. If you
// "improve" the logger or health code, you are out of scope.

// CRITICAL: logStartup must reference v2 fields. The current main.go line 160
// `cfg.Aliases` will NOT COMPILE (Config has no Aliases field post-T1.S1; it is
// QueryAliases now). Replace the whole fields map (see Implementation Blueprint).
// After the fix, `grep -n 'cfg.Aliases' main.go` must return nothing.

// GOTCHA: main()'s body still calls newProxyHandler(cfg, log, client) and
// newUpstreamClient(), which live in proxy.go. LEAVE THEM. The full package will
// not build until T2.S2 (proxy deleted, handler mounted). Do NOT stub them out,
// do NOT delete main()'s bootstrap — that is T2.S2's edit. Validate with the
// isolated gates instead of `go build ./...`.

// GOTCHA: main.go's import list shrinks. After the move, main.go no longer uses
// encoding/json (moved to logger.go/health.go) or io (moved to logger.go). Keep
// only: context, net/http, os, os/signal, syscall, time. An unused import is a
// compile error in Go — `goimports`/`gofmt` won't remove them for you reliably;
// edit the import block explicitly. (logger.go needs encoding/json+io+net/http+
// time; health.go needs encoding/json+net/http.)

// GOTCHA: Add the go.mod `//` SDK comment AFTER `go mod tidy` (as the final
// go.mod edit), so tidy cannot reflow/drop it. A `//` line directly above
// `require github.com/modelcontextprotocol/go-sdk v1.6.1` survives subsequent
// tidy runs. Verify with: grep -F '// ' go.mod | head.

// SCOPE GUARD: Do NOT mount the SDK handler, do NOT delete proxy.go/sse.go/
// rewrite.go, do NOT edit config.go (T1.S2 owns ResolveConfig), do NOT touch any
// *_test.go (T3.S1 rewrites them), do NOT edit doc.go, do NOT write
// config.example.json or README.md (P1.M5.T4). Edit ONLY go.mod, logger.go,
// health.go, main.go; create go.sum.
```

## Implementation Blueprint

### Data models and structure

No new types. This subtask only relocates existing ones. The `type logger` (and
its `level int` field), the level consts, `version` string var, and the `Config`
itself are all unchanged. `logStartup`'s signature `func logStartup(l *logger, cfg Config)`
is unchanged; only its `fields` map changes.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT STATE (read-only)
  - RUN: `head -3 go.mod` -> expect `module web-search-prime-fixer` + `go 1.22`
        + NO require block. `test ! -f go.sum` -> succeeds (no go.sum yet).
  - RUN: `go build ./... 2>&1 | head` -> expect errors at ./main.go:160
        (cfg.Aliases undefined) and ./proxy.go:498/499 (cfg.Aliases). This is the
        documented S1-era breakage; it is PRE-EXISTING, not caused by you.
  - RUN: confirm main.go contains `logStartup` referencing `cfg.Aliases` and the
        logger/health definitions listed below (grep -n 'type logger struct\|
        func newLogger\|func (l \*logger).log\|func redactHeaders\|var version\|
        func healthHandler\|func logStartup' main.go).
  - WHY: This subtask's INPUT is "the existing main.go + the updated Config". If
        go.mod already has the SDK or main.go is already split, a prior step
        overlapped — stop and surface it.

Task 1: ADD THE SDK DEPENDENCY (go.mod + go.sum)  [needs NETWORK]
  - COMMAND (repo root, default GOPROXY):
        go get github.com/modelcontextprotocol/go-sdk@v1.6.1
        go mod tidy
  - EXPECT stdout from `go get`: "go: upgraded go 1.22 => 1.25.0" +
        "go: added github.com/modelcontextprotocol/go-sdk v1.6.1".
  - EXPECT `go mod tidy`: downloads 7 transitive deps
        (jsonschema-go, uritemplate/v3, oauth2, segmentio/encoding, segmentio/asm,
        x/sys, x/tools) and writes go.sum (~20 lines).
  - VERIFY go.mod now reads `go 1.25.0` (NOT 1.22) and has the SDK require + an
        `// indirect` require block. DO NOT hand-edit the indirect requires.
  - GOTCHA: if tidy fails with "module lookup disabled by GOPROXY=off", you set
        GOPROXY=off — unset it (network is required and available).
  - NOTE: importing the SDK in main.go is NOT required by this subtask; T2.S2 does
        the first real import. But the require must resolve. Verify with the
        isolated build gate (Task 5 / Gate D).

Task 2: ADD THE Mode-A go.mod COMMENT
  - EDIT go.mod: add a single `//` comment line directly ABOVE the
        `require github.com/modelcontextprotocol/go-sdk v1.6.1` line:
            // Sole external dependency: the official Go MCP SDK (PRD §7, §13).
  - VERIFY: `grep -F 'Sole external dependency' go.mod` matches once. Re-run
        `go mod tidy` once more and confirm the comment survives (it should, being
        adjacent to the require; if not, re-add it).

Task 3: CREATE logger.go (verbatim move from main.go)
  - FILE: /home/dustin/projects/web-search-prime-fixer/logger.go
  - PACKAGE: `package main`
  - IMPORTS (exactly, alphabetical):
        import (
            "encoding/json"
            "io"
            "net/http"
            "time"
        )
  - PASTE VERBATIM from main.go (each with its existing doc comment, unchanged):
        (a) the level const block:
                const (
                    levelDebug = iota // 0
                    levelInfo         // 1
                    levelWarn         // 2
                    levelError        // 3
                )
        (b) func levelNum(level string) int
        (c) type logger struct { w io.Writer; level int }   (+ its doc comment)
        (d) func newLogger(w io.Writer, level string) *logger
        (e) func (l *logger) log(level string, msg string, fields map[string]any)
        (f) func redactHeaders(h http.Header) map[string]any
  - BEHAVIOR: identical to the current main.go versions. Do not alter JSON shape,
        level filtering, redaction set, or the time format.
  - WHY: PRD §14 lists logger.go as a standalone carry-over file; §6/§13 redaction
        contract must be preserved byte-for-byte.

Task 4: CREATE health.go (verbatim move from main.go)
  - FILE: /home/dustin/projects/web-search-prime-fixer/health.go
  - PACKAGE: `package main`
  - IMPORTS (exactly):
        import (
            "encoding/json"
            "net/http"
        )
  - PASTE VERBATIM from main.go (with doc comments):
        (a) var version = "dev"   (+ the -ldflags/-X main.version doc comment)
        (b) func healthHandler(w http.ResponseWriter, r *http.Request)
  - BEHAVIOR: identical (GET-only 405-otherwise; {"ok":true,"version":<version>};
        Content-Type application/json; no upstream call).

Task 5: EDIT main.go — remove moved defs, update logStartup, trim imports
  - DELETE from main.go: the level const block, levelNum, type logger, newLogger,
        (*logger).log, redactHeaders, var version, healthHandler (now in
        logger.go / health.go).
  - REPLACE the logStartup function body's fields map. CURRENT (v1, broken):
        func logStartup(l *logger, cfg Config) {
            l.log("info", "startup", map[string]any{
                "aliases":   cfg.Aliases, // []string -> JSON array
                "listen":    cfg.Listen,
                "upstream":  cfg.Upstream,
                "log_level": cfg.LogLevel,
            })
        }
    NEW (v2):
        func logStartup(l *logger, cfg Config) {
            l.log("info", "startup", map[string]any{
                "tools":          cfg.Tools,         // []string -> JSON array
                "canonical_tool": cfg.CanonicalTool,
                "query_aliases":  cfg.QueryAliases,  // []string -> JSON array
                "listen":         cfg.Listen,
                "upstream":       cfg.Upstream,
                "log_level":      cfg.LogLevel,
            })
        }
    Keep logStartup's existing doc comment but update its field list from
    "aliases ... listen, upstream, log_level" to "tools (JSON array),
    canonical_tool, query_aliases (JSON array), listen, upstream, log_level" and
    keep the "NEVER credentials" note (Config has no credential field — PRD §13).
  - LEAVE main()'s body UNCHANGED (ResolveConfig -> newLogger -> logStartup ->
        newUpstreamClient -> mux with healthHandler + newProxyHandler -> srv ->
        shutdown goroutine -> ListenAndServe). Its newProxyHandler/newUpstreamClient
        calls resolve against proxy.go and stay red until T2.S2.
  - TRIM main.go's import block to EXACTLY:
        import (
            "context"
            "net/http"
            "os"
            "os/signal"
            "syscall"
            "time"
        )
    (Remove "encoding/json" and "io" — they moved with the logger/health code and
    are no longer referenced in main.go. context/net/http/os/os/signal/syscall/time
    are all still used by main().)

Task 6: VALIDATE (isolated gates — see Validation Loop; full repo build is red on purpose)
  - Gate A: logger.go isolated vet+build.
  - Gate B: health.go isolated vet+build.
  - Gate C: logStartup isolated vet + throwaway field-set test, then DELETE the temp module.
  - Gate D: SDK dependency isolated build (import the mcp package with the new go.mod).
  - EXPECT full-repo `go build ./...` to STILL FAIL on proxy.go + *_test.go
        (.Aliases / v1 proxy refs) — that is the T1-era set, unchanged by this
        subtask. Any error INSIDE logger.go/health.go/main.go/config.go is a defect.
```

### Implementation Patterns & Key Details

```go
// PATTERN: mechanical move. The logger and health code are copied as-is into
// their own files with their own minimal import blocks. Example shape of logger.go:
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

const (
	levelDebug = iota // 0
	levelInfo         // 1
	levelWarn         // 2
	levelError        // 3
)

// ... levelNum, logger, newLogger, (*logger).log, redactHeaders ...
// (identical bodies to the current main.go — this is a cut/paste, not a rewrite.)

// PATTERN: logStartup fields map is the ONLY logic change. Build it as a
// map[string]any so json.Marshal emits each value in its natural JSON form
// ([]string -> array, string -> string). Passing cfg.Tools / cfg.QueryAliases
// directly (not pre-marshaled) is correct: the logger marshals the whole map.
l.log("info", "startup", map[string]any{
	"tools":          cfg.Tools,        // []string{"web_search"} -> ["web_search"]
	"canonical_tool": cfg.CanonicalTool, // "web_search"
	"query_aliases":  cfg.QueryAliases, // [...] -> [...]
	"listen":         cfg.Listen,
	"upstream":       cfg.Upstream,
	"log_level":      cfg.LogLevel,
})

// SECURITY: Config has no credential field (PRD §13 — Authorization is a request
// header, never part of Config), so logging cfg fields structurally cannot leak
// a secret. Do NOT add header values to the startup log.

// CRITICAL: do NOT import the SDK in main.go yet. T2.S2 does the first
// `import "github.com/modelcontextprotocol/go-sdk/mcp"`. This subtask only adds
// the dependency to go.mod/go.sum; no source file references it.
```

### Integration Points

```yaml
go.mod / go.sum (THIS SUBTASK):
  - require: github.com/modelcontextprotocol/go-sdk v1.6.1 (direct) + ~6 indirect
  - directive: go 1.25.0   (was 1.22; bumped by the SDK's go 1.25.0 requirement)
  - go.sum: NEW, ~20 lines
  - comment: "// Sole external dependency: the official Go MCP SDK (PRD §7, §13)."

FILE OWNERSHIP (package main):
  - logger.go:   level consts, levelNum, logger, newLogger, (*logger).log, redactHeaders
  - health.go:   var version, healthHandler
  - main.go:     main() (UNCHANGED body), logStartup (v2 fields)
  - config.go:   UNCHANGED (T1.S1/T1.S2 own it)

DOWNSTREAM CONSUMERS:
  - P1.M1.T2.S2: imports the SDK (first mcp import), mounts StreamableHTTPHandler,
        deletes proxy.go/sse.go/rewrite.go, reuses logger.go + health.go as-is.
  - P1.M1.T3.S1: rewrites logger_test.go/health_test.go for the new locations.
  - PRD §15 startup event: now emits tools/canonical_tool/query_aliases.

NO INTEGRATION POINTS TOUCHED BY THIS SUBTASK:
  - config.go, proxy.go, sse.go, rewrite.go, doc.go, *_test.go, README.md,
        config.example.json — all UNCHANGED.
```

## Validation Loop

> The full-repo package is INTENTIONALLY red after this subtask (proxy.go +
> `*_test.go` still reference the v1 `cfg.Aliases`/proxy symbols — fixed by
> T2.S2/T3.S1). So validation uses **isolated temp-module gates**, exactly like
> the P1.M1.T1.S2 precedent. Each gate creates a throwaway temp module under
> `/tmp`, copies in the relevant file(s), and proves it compiles/behaves; the temp
> module is deleted afterward.

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -l logger.go health.go main.go          # MUST print nothing (all formatted)
go vet ./... 2>&1 | grep -E 'logger\.go|health\.go' || true   # MUST print nothing
# (go vet ./... overall will error on proxy.go/tests — that is expected; we only
#  assert NO error mentions logger.go or health.go.)

# main.go must no longer reference the v1 field, and must reference the v2 fields:
! grep -n 'cfg\.Aliases' main.go              # MUST succeed (exit 0): no v1 alias ref
grep -n 'cfg\.Tools' main.go                  # MUST match (logStartup)
grep -n 'cfg\.CanonicalTool' main.go          # MUST match
grep -n 'cfg\.QueryAliases' main.go           # MUST match

# Import-block sanity on the new/edited files:
grep -A8 '^import (' logger.go | grep -E '"encoding/json"|"io"|"net/http"|"time"'   # 4 stdlib imports
grep -A4 '^import (' health.go  | grep -E '"encoding/json"|"net/http"'              # 2 stdlib imports
grep -A8 '^import (' main.go    | grep -E '"context"|"net/http"|"os"|"os/signal"|"syscall"|"time"'  # 6

# go.mod sanity:
grep -E '^go 1\.25\.0$' go.mod                                  # MUST match (NOT 1.22)
grep -F 'require github.com/modelcontextprotocol/go-sdk v1.6.1' go.mod  # MUST match
grep -F 'Sole external dependency' go.mod                       # MUST match (Mode-A comment)
test -f go.sum && test -s go.sum                                # go.sum exists and non-empty
! grep -qE '^toolchain ' go.mod                                 # NO toolchain line (verified)
```

### Level 2: Isolated Compile Gates (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer
R=/home/dustin/projects/web-search-prime-fixer

# --- Gate A: logger.go compiles standalone (stdlib only) ---
rm -rf /tmp/log-iso && mkdir -p /tmp/log-iso && cd /tmp/log-iso && \
  go mod init logiso >/dev/null 2>&1 && \
  cp "$R/logger.go" . && printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
  go vet . && go build . && echo "OK A: logger.go compiles standalone" && \
  cd "$R" && rm -rf /tmp/log-iso
# Expected: "OK A: logger.go compiles standalone", exit 0. (logger.go uses only stdlib.)

# --- Gate B: health.go compiles standalone (stdlib only) ---
rm -rf /tmp/health-iso && mkdir -p /tmp/health-iso && cd /tmp/health-iso && \
  go mod init healthiso >/dev/null 2>&1 && \
  cp "$R/health.go" . && printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
  go vet . && go build . && echo "OK B: health.go compiles standalone" && \
  cd "$R" && rm -rf /tmp/health-iso
# Expected: "OK B: health.go compiles standalone", exit 0.
```

### Level 3: Behavior Gate — logStartup emits v2 fields (throwaway temp module)

```bash
cd /home/dustin/projects/web-search-prime-fixer
R=/home/dustin/projects/web-search-prime-fixer

rm -rf /tmp/ls-iso && mkdir -p /tmp/ls-iso && cd /tmp/ls-iso && \
  go mod init lsiso >/dev/null 2>&1 && \
  cp "$R/config.go" . && cp "$R/logger.go" . && \
  printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
# Extract logStartup into its own temp file (it is defined in the real main.go;
# here we isolate it with config.go + logger.go so we can vet + test it):
cat > logstartup.go <<'EOF'
package main
func logStartup(l *logger, cfg Config) {
	l.log("info", "startup", map[string]any{
		"tools":          cfg.Tools,
		"canonical_tool": cfg.CanonicalTool,
		"query_aliases":  cfg.QueryAliases,
		"listen":         cfg.Listen,
		"upstream":       cfg.Upstream,
		"log_level":      cfg.LogLevel,
	})
}
EOF
cat > logstartup_test.go <<'EOF'
package main
import ("bytes";"encoding/json";"testing")
func TestLogStartup_V2Fields(t *testing.T){
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	logStartup(l, DefaultConfig())
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil { t.Fatalf("json: %v\n%s", err, buf.String()) }
	if m["msg"] != "startup" { t.Fatalf("msg=%v", m["msg"]) }
	want := []string{"tools","canonical_tool","query_aliases","listen","upstream","log_level"}
	for _, k := range want {
		if _, ok := m[k]; !ok { t.Errorf("missing field %q in %s", k, buf.String()) }
	}
	if _, ok := m["aliases"]; ok { t.Errorf("v1 field 'aliases' must be gone: %s", buf.String()) }
	if _, ok := m["tools"].([]any); !ok { t.Errorf("tools not a JSON array: %T", m["tools"]) }
	if _, ok := m["query_aliases"].([]any); !ok { t.Errorf("query_aliases not a JSON array: %T", m["query_aliases"]) }
	if m["canonical_tool"] != "web_search" { t.Errorf("canonical_tool=%v", m["canonical_tool"]) }
}
EOF
go vet . && go test -run TestLogStartup_V2Fields -v . && echo "OK C: logStartup emits v2 fields" && \
  cd "$R" && rm -rf /tmp/ls-iso
# Expected: "OK C: logStartup emits v2 fields", exit 0. Confirms tools/canonical_tool/
# query_aliases/listen/upstream/log_level are present, aliases is gone, arrays marshal.
# This temp logstartup.go mirrors the real logStartup in main.go byte-for-byte — if
# the real one differs, the implementer must reconcile to THIS shape.
```

### Level 4: SDK Dependency Gate + Expected-Failure Documentation

```bash
cd /home/dustin/projects/web-search-prime-fixer
R=/home/dustin/projects/web-search-prime-fixer

# --- Gate D: the new go.mod actually resolves + builds an SDK import ---
rm -rf /tmp/sdk-iso && mkdir -p /tmp/sdk-iso && cd /tmp/sdk-iso && \
  cp "$R/go.mod" . && cp "$R/go.sum" . && \
  printf 'package main\nimport "github.com/modelcontextprotocol/go-sdk/mcp"\nvar _ = mcp.NewServer\nfunc main(){}\n' > main.go && \
  go build ./... && echo "OK D: SDK resolves + builds with the new go.mod/go.sum" && \
  cd "$R" && rm -rf /tmp/sdk-iso
# Expected: "OK D: ...", exit 0. Proves go.mod/go.sum are complete and consistent.
# (Network may be needed only if go.sum is somehow incomplete; default GOPROXY is fine.)

# --- Expected-failure documentation (NOT a gate to fix): ---
cd "$R"
go build ./... 2>&1 | sort -u
# Expected: errors reference EXACTLY the out-of-scope v1 sites (unchanged by this
# subtask — it only moved code + added a dep; it introduced no new breakage):
#   ./proxy.go:498/499/...  cfg.Aliases undefined        (DELETED in P1.M1.T2.S2)
#   *_test.go ...           .Aliases / v1 proxy refs      (rewritten in P1.M1.T3.S1 / T2.S2)
# IF `go build ./...` reports ANY error INSIDE logger.go, health.go, main.go, or
# config.go (syntax, undefined symbol, unused import, type mismatch) that IS a
# defect in THIS subtask — fix it. IF it reports ONLY proxy.go + *_test.go, that
# is EXPECTED and correct — do not edit those files.

# Cross-check: confirm main.go's own cfg references are all v2 (no v1 leftovers):
grep -nE 'cfg\.(Aliases|Tools|CanonicalTool|CanonicalParam|QueryAliases|OptionalAliases|TargetTool|TargetParam|Listen|Upstream|LogLevel|Path)' main.go
# Expected: matches only on Tools/CanonicalTool/QueryAliases/Listen/Upstream/LogLevel
# (inside logStartup). NO `cfg.Aliases`.
```

## Final Validation Checklist

### Technical Validation

- [ ] `gofmt -l logger.go health.go main.go` prints nothing.
- [ ] `go vet ./...` reports no error mentioning `logger.go` or `health.go`.
- [ ] Gate A: `logger.go` vet+build standalone (stdlib only).
- [ ] Gate B: `health.go` vet+build standalone (stdlib only).
- [ ] Gate C: `logStartup` emits the 6 v2 fields, drops `aliases` (throwaway test).
- [ ] Gate D: an `import ".../mcp"` program builds with the new go.mod/go.sum.
- [ ] Full-repo `go build ./...` fails ONLY on proxy.go + `*_test.go` (out of scope); no error inside logger.go/health.go/main.go/config.go.

### Feature Validation

- [ ] `logger.go` reproduces the level consts, levelNum, logger, newLogger, log, redactHeaders verbatim (behavior unchanged).
- [ ] `health.go` reproduces `var version` and `healthHandler` verbatim (behavior unchanged).
- [ ] `main.go` `logStartup` emits `tools`, `canonical_tool`, `query_aliases`, `listen`, `upstream`, `log_level`; no `cfg.Aliases`.
- [ ] `main()` body unchanged (still calls newProxyHandler/newUpstreamClient — T2.S2's job to replace).
- [ ] `go.mod` requires `github.com/modelcontextprotocol/go-sdk v1.6.1`, directive is `go 1.25.0`, with the Mode-A comment; `go.sum` present.

### Code Quality Validation

- [ ] Pure mechanical move (no logic change to logger/health); only logStartup's field set changed.
- [ ] Import blocks minimal and correct per file (logger.go 4, health.go 2, main.go 6 — no unused imports).
- [ ] Scope respected: ONLY go.mod, logger.go, health.go, main.go edited; go.sum created. No config.go/proxy.go/test/doc/README edits.

### Documentation & Deployment

- [ ] go.mod Mode-A `//` comment notes the SDK is the sole external dependency (PRD §7/§13).
- [ ] logStartup doc comment field list updated to the v2 set (Mode A).
- [ ] No env vars changed; no README/config.example.json changes (later subtasks).

---

## Anti-Patterns to Avoid

- ❌ Don't use `GOPROXY=off` — the SDK's transitive deps are NOT cached; `go mod tidy` needs the (reachable) network. The contract's "needs no network" is FALSE (verified).
- ❌ Don't revert `go.mod` to `go 1.22` after `go get` bumps it to `go 1.25.0` — the SDK requires 1.25.0; reverting breaks the build. The bump is correct and expected (PRD §13).
- ❌ Don't hand-edit the `// indirect` require block or go.sum — run `go get` + `go mod tidy` and commit their output.
- ❌ Don't rewrite or "improve" the logger/health code — this is a verbatim move. Only `logStartup`'s field set changes.
- ❌ Don't import the SDK in `main.go` yet, mount the handler, or delete `proxy.go`/`sse.go`/`rewrite.go` — that is P1.M1.T2.S2.
- ❌ Don't try to make the full `go build ./...` green by editing `proxy.go`/`*_test.go`/`config.go` — those are T2.S2/T3.S1/T1.S2. Use the isolated gates.
- ❌ Don't leave unused imports (`encoding/json`/`io`) in `main.go` after the move — Go fails to compile on unused imports; trim the import block explicitly to the 6 main() needs.
- ❌ Don't touch `config.go` (T1.S1/T1.S2 own it), `doc.go`, `*_test.go`, `README.md`, or `config.example.json`.
- ❌ Don't log any header/credential value in `logStartup` — Config carries no credential field; keep it that way (PRD §13).

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is a well-bounded mechanical move (the exact symbols to
cut/paste are quoted verbatim from the current `main.go`), one small field-set edit
to `logStartup` (the exact new `map[string]any` is given), and a dependency add
whose **entire** mechanics — including the two things that contradict the contract
(the `go 1.22 → 1.25.0` bump and the network requirement) — are reproduced on-disk
in `research/sdk_dependency_findings.md` with the exact `go get`/`go mod tidy`/
`go build` happy path that exits 0. The validation avoids the trap of the
intentionally-red full package by using four isolated temp-module gates (mirroring
the P1.M1.T1.S2 precedent). The residual 1/10 risk is an agent either (a) believing
the false "no network" contract and setting `GOPROXY=off`, (b) reverting the
`go 1.25.0` bump "to match the old PRP", or (c) over-reaching into proxy.go/test
files to "fix" the expected red build — all three are pinned explicitly in the
Gotchas and Anti-Patterns with the exact grep checks that catch them.
