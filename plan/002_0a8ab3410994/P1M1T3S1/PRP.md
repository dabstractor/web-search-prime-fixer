# PRP — P1.M1.T3.S1: Rewrite config_test.go, resolve_test.go, and health_test.go for v2 schema

## Goal

**Feature Goal**: Bring the three surviving v1-schema test files up to the v2
11-field `Config` schema and the new SDK-handler routing, so the package's full
`go test ./...` suite is green for the first time since the v1→v2 pivot began.
This is the **M1 definition-of-done gate**: `go build ./...` is already green
(production files compile); this subtask makes the tests compile and pass by
removing every v1 reference (`Config.Aliases`, `newProxyHandler`,
`newUpstreamClient`) and asserting the v2 fields (`Tools`, `CanonicalTool`,
`QueryAliases`, `OptionalAliases`, …) and the new `/healthz` + `/` routing.

**Deliverable**: Three edited test files at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), all `package main`:
1. **REWRITE** `config_test.go` — `TestDefaultConfig` asserts the full v2 11-field
   `DefaultConfig()` literally (PRD §18.2: 14-entry `QueryAliases`, 3-key
   `OptionalAliases`); `TestLoadConfig_FromFile` table uses v2 JSON keys
   (`query_aliases`, `tools`, `canonical_tool`, `optional_aliases`, …). The two
   unchanged tests (`TestLoadConfig_EmptyPath`, `TestLoadConfig_MissingFile`) stay.
2. **EDIT** `resolve_test.go` — rename `Aliases`→`QueryAliases` (3 sites) in the
   `path_and_aliases_not_overridable` scope-guard subtest; **append** a new
   `TestResolveConfig_ToolsValidation` covering the three PRD §18.3 rules
   (`Tools` empty → error; `Tools` missing `CanonicalTool` → error; `Tools`
   contains `TargetTool` → error) plus one positive case — via config FILE
   (these fields have no env override). All 8 existing tests + 2 helpers unchanged.
3. **EDIT** `health_test.go` — rewrite `TestLogStartup` to assert `m["tools"]`,
   `m["canonical_tool"]`, `m["query_aliases"]` (not `m["aliases"]`); rewrite
   `TestRouting_HealthzOnly` to build the `/healthz` + `/` mux with a **sentinel
   catch-all** (proves isolation without coupling to the SDK handler). The 4
   `healthHandler` tests + the import block stay.

`logger_test.go` is **NOT touched** — it is already green and decoupled.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` ALL exit clean. Every surviving test file passes. No production
file (`config.go`, `logger.go`, `health.go`, `main.go`, `doc.go`) is modified;
`go.mod`/`go.sum`, `testdata/`, `logger_test.go`, `README.md`,
`config.example.json` are untouched. This is a **test-only** change (no
user-facing/config/API surface change).

## User Persona

**Target User**: The v2 implementer (this is an internal M1 quality gate, not a
user-facing feature).

**Use Case**: After this subtask, `go test ./...` is the reliable green gate for
all later milestones (M2 extract, M3 teach, M4 upstream, M5 server+E2E). The v1
breakage (red tests) that has blocked confidence since the pivot is cleared.

**Pain Points Addressed**: (1) `go vet`/`go test` are RED on three files
(`unknown field Aliases`, `newProxyHandler undefined`) — this is the M1 blocker;
(2) the surviving tests silently drifted from the v2 schema; (3) the v2
`ResolveConfig` validation rules (Tools/CanonicalTool/TargetTool) had no test
coverage.

## Why

- This is the **M1 definition-of-done**. P1.M1.T1 (config schema) and P1.M1.T2
  (SDK mount + v1 deletion) landed production code, but deliberately left the three
  surviving v1-schema test files red — explicitly assigned to THIS subtask (see the
  T2.S2 PRP's breakage map and Anti-Patterns: "config_test.go/resolve_test.go/
  health_test.go stay RED = T3.S1"). Until they are rewritten, the package's test
  suite cannot be trusted and no later milestone can run `go test ./...` cleanly.
- Restores **test coverage for the v2 schema**: `DefaultConfig()`'s 11 fields
  (especially the 14-entry `QueryAliases` order and the 3-key `OptionalAliases`),
  the v2 JSON file-overlay, and the three new `ResolveConfig` validation rules
  (PRD §18.3) that guard what the server advertises. The validation rules had zero
  coverage after the pivot.
- Keeps the **established test conventions** (table-driven `t.Run`, `reflect.DeepEqual`,
  `isolateConfigEnv(t)`/`writeConfig(t, body)` helpers, `httptest` for routing,
  `newLogger(&buf, …)` for log assertion) per `architecture/codebase_patterns.md`
  §1/§5 — so the rewritten tests look native to the codebase and M2–M5 can copy the
  patterns.
- Preserves the **security invariant** test ("startup log never emits an
  authorization field") against the v2 `logStartup` fields.

## What

### (a) `config_test.go`
- `TestDefaultConfig`: `want` is the **full 11-field** v2 `Config` literal (PRD
  §18.2 verbatim), compared with `reflect.DeepEqual`; plus belt-and-suspenders
  per-field asserts for every field including the new ones (`Tools`,
  `CanonicalTool`, `CanonicalParam`, 14-element `QueryAliases`, 3-key
  `OptionalAliases`, `TargetTool`).
- `TestLoadConfig_FromFile`: table rows use v2 JSON keys —
  `partial_override_keeps_defaults` uses `"query_aliases"`; `full_file_all_fields`
  uses all 11 v2 keys; a new `v2_fields_override` row exercises
  `tools`/`canonical_tool`/`optional_aliases`; `unknown_fields_ignored` and
  `invalid_json_returns_error` unchanged. The partial/unknown rows derive `want`
  from `DefaultConfig()` (tracks default changes; tests merge behavior).
- `TestLoadConfig_EmptyPath`, `TestLoadConfig_MissingFile`: **unchanged** (they use
  `reflect.DeepEqual` with `DefaultConfig()` / a missing-file error — both still
  valid in v2).

### (b) `resolve_test.go`
- The `path_and_aliases_not_overridable` subtest (inside
  `TestResolveConfig_EnvOverrides`): rename the 3 `Aliases` references to
  `QueryAliases` (`cfg.QueryAliases`, `DefaultConfig().QueryAliases`, error msg).
  `WSPF_ALIASES` → `WSPF_QUERY_ALIASES` (no effect either way — keeps the scope
  guard v2-accurate).
- **Append** `TestResolveConfig_ToolsValidation` (table-driven, via config file +
  `WSPF_CONFIG`, reusing `isolateConfigEnv`/`writeConfig`):
  - `tools_empty` → `{"tools":[]}` → error.
  - `tools_missing_canonical` → tools without `CanonicalTool` → error.
  - `tools_contains_target` → tools includes `TargetTool` (`web_search_prime`) → error.
  - `tools_valid_extra` → `{"tools":["web_search","search"]}` → **no error** (positive).
- All other tests (`TestResolveConfig_Defaults`, `_WSPF_CONFIG`, `_CwdDiscovery`,
  `_XDGDiscovery`, `_Precedence_CwdBeforeXDG`, `_EnvOverrides`, `_Validation`,
  `_TargetParamForced`) + the `isolateConfigEnv`/`writeConfig`/`fieldByJSONName`
  helpers are **unchanged** (they already use v2-safe `reflect.DeepEqual`/env-var
  paths — only the scope-guard subtest touched `.Aliases`).

### (c) `health_test.go`
- `TestLogStartup`: assert `m["tools"]` (JSON array), `m["canonical_tool"]` (string),
  `m["query_aliases"]` (JSON array), plus `m["listen"]`/`m["upstream"]`/
  `m["log_level"]` and the no-`authorization` invariant. Decoded arrays are `[]any`;
  compare with `reflect.DeepEqual` against a constructed `[]any`.
- `TestRouting_HealthzOnly`: build the mux with `/healthz` → `healthHandler` and
  `/` → a **sentinel catch-all** `http.HandlerFunc` that sets a flag if invoked.
  `GET /healthz` → `200` with `{"ok":true,"version":"dev"}`; assert the sentinel was
  NOT hit (proves `/healthz` is isolated from the catch-all, preserving the original
  test's intent). No SDK coupling (the SDK handler integration is P1.M5.T3's job).
- The 4 `healthHandler` tests (`TestHealthHandler`, `_VersionOverride`,
  `_NoUpstream`, `_RejectsNonGET`) + the import block are **unchanged**.

### (d) `logger_test.go`
- **Untouched.** Verify it passes (`go test -run 'TestLogger|TestRedactHeaders|
  TestNewLogger' ./...` → PASS). It tests only `newLogger`/`log`/`redactHeaders`
  (all in `logger.go`, unchanged by v2).

### Success Criteria
- [ ] `config_test.go` `TestDefaultConfig` `want` is the literal v2 11-field Config
      matching PRD §18.2 (incl. 14-entry `QueryAliases`, 3-key `OptionalAliases`).
- [ ] `config_test.go` has NO reference to `Aliases` / the `aliases` JSON key; all
      table JSON uses v2 keys (`query_aliases`, `tools`, `canonical_tool`, …).
- [ ] `resolve_test.go` has NO `Aliases` reference; the scope-guard subtest uses
      `QueryAliases`.
- [ ] `resolve_test.go` has a `TestResolveConfig_ToolsValidation` with the three
      error cases (empty / missing-canonical / contains-target) + one positive.
- [ ] `health_test.go` `TestLogStartup` asserts `m["tools"]`, `m["canonical_tool"]`,
      `m["query_aliases"]` (not `m["aliases"]`).
- [ ] `health_test.go` `TestRouting_HealthzOnly` does NOT reference
      `newProxyHandler`/`newUpstreamClient`; it proves `/healthz` isolation.
- [ ] `logger_test.go` is byte-for-byte unchanged.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.
- [ ] No production file (`config.go`/`logger.go`/`health.go`/`main.go`/`doc.go`),
      `go.mod`/`go.sum`, `testdata/`, `README.md`, `config.example.json` is modified.

## All Needed Context

### Context Completeness Check

_Pass._ The exact v1 references in all three target files are **grep-confirmed**
(`research/v2_test_rewrite_breakage_map.md` lists every line). The v2 production
code they must compile against (`config.go` 11-field schema + `DefaultConfig` +
`LoadConfig` + `ResolveConfig` with the three Tools rules; `health.go`
`healthHandler`/`var version`; `logger.go` `newLogger`/`log`/`redactHeaders`;
`main.go` `logStartup` with the exact v2 fields) has been **read on-disk**. The
parallel T2.S2 PRP (treated as a landed contract) confirms `main.go` defines
`authHeaderKey`/`authMiddleware` and mounts the SDK handler, and that `go build
./...` is already GREEN while `go vet`/`go test` are RED only on these three files.
`logger_test.go` is confirmed clean (no v1 refs). The complete rewritten test code
is given verbatim below. An agent with no prior knowledge can implement this from
the PRP alone.

### Documentation & References

```yaml
# MUST READ — the v2 config schema + defaults + validation rules.
- file: PRD.md
  why: §18.1 "Schema" (the 11-field Config); §18.2 "Defaults" (verbatim:
        Tools=["web_search"], CanonicalTool="web_search", CanonicalParam="query",
        the 14-entry QueryAliases, the 3-key OptionalAliases, TargetTool=
        "web_search_prime", TargetParam="search_query", LogLevel="info");
        §18.3 "Discovery and precedence" (env overrides WSPF_UPSTREAM/LISTEN/
        LOG_LEVEL only; validation: Tools non-empty + contains CanonicalTool +
        no entry == TargetTool); §15 "Logging" (startup event fields; never creds);
        §16 "Health" (GET /healthz -> {"ok":true,"version":...}).
  critical: The validation table for Tools MUST use a config FILE, not env —
        Tools/CanonicalTool/TargetTool have NO env override (only upstream/listen/
        log_level). The 14-entry QueryAliases order matters (DeepEqual). Tools must
        NEVER contain TargetTool ("web_search_prime") — that's the third rule.

# VERIFIED BREAKAGE MAP — every v1 reference to fix + the v1->v2 mapping + decisions.
- file: plan/002_0a8ab3410994/P1M1T3S1/research/v2_test_rewrite_breakage_map.md
  why: grep-confirmed list of every Aliases/newProxyHandler/newUpstreamClient site
        (with line numbers) in the three files; logger_test.go confirmed clean; the
        v1->v2 field table; decisions D1-D6 (logStartup fields, Tools-validation-
        via-file, scope-guard rename, sentinel catch-all, literal want struct,
        import blocks unchanged).
  critical: D2 — the three Tools rules are exercised via writeConfig + WSPF_CONFIG
        (NOT env). D4 — TestRouting uses a SENTINEL catch-all (not the SDK handler)
        to stay decoupled; the SDK integration is P1.M5.T3's job. D5 — TestDefaultConfig
        transcribes the want struct LITERALLY (not DefaultConfig()).

# PRODUCTION CONTRACT — the exact code the tests must compile against (READ).
- file: config.go
  why: The 11-field Config + JSON tags (upstream/listen/path/tools/canonical_tool/
        canonical_param/query_aliases/optional_aliases/target_tool/target_param/
        log_level); DefaultConfig() (verbatim PRD §18.2); ResolveConfig() with the
        three Tools validation rules (lines: "tools list must not be empty" /
        "must contain the canonical tool" / "must not contain the target tool").
  critical: ResolveConfig validates Tools AFTER Listen/Upstream, BEFORE TargetParam
        forcing. slices.Contains is used. A file with {"tools":[]} fails first
        (Tools check) regardless of other fields. The validation error strings are
        NOT asserted by the tests (only err != nil) — so they stay robust to wording.

# PRODUCTION CONTRACT — logStartup fields (for TestLogStartup).
- file: main.go
  why: logStartup(l, cfg) logs: "tools" (cfg.Tools, []string->array),
        "canonical_tool", "query_aliases" (cfg.QueryAliases, []string->array),
        "listen", "upstream", "log_level". Defines authHeaderKey/authMiddleware.
  critical: TestLogStartup MUST assert exactly these keys (tools/canonical_tool/
        query_aliases), NOT "aliases". The values for tools/query_aliases decode to
        []any; compare with reflect.DeepEqual against constructed []any.

# PARALLEL WORK (CONTRACT) — defines what exists when this subtask starts.
- file: plan/002_0a8ab3410994/P1M1T2S2/PRP.md
  why: T2.S2 rewrote main() (SDK handler + authMiddleware), deleted proxy.go/sse.go/
        rewrite.go + 6 test files, and LEFT config_test.go/resolve_test.go/
        health_test.go RED (= this subtask). logger_test.go GREEN. go build ./... GREEN.
  critical: When this subtask runs, newProxyHandler/newUpstreamClient are GONE (do
        NOT reintroduce them). main.go has authMiddleware — but health_test.go does
        NOT need it (sentinel approach). The v1 proxy/sse/rewrite symbols are deleted.

# PATTERNS — the test conventions to follow (so the rewrites look native).
- file: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  why: §1 (config patterns: reflect.DeepEqual vs DefaultConfig, isolateConfigEnv,
        writeConfig, table-driven t.Run); §2 (logger: newLogger(&buf,...), redactHeaders);
        §5 (test harness: httptest, io.Discard, table-driven, package main); §7
        (v1->v2 JSON key table: aliases->query_aliases + 5 new keys).
  critical: REUSE the existing isolateConfigEnv(t)/writeConfig(t,body) helpers in
        resolve_test.go — do NOT redefine them. Keep tests in package main. No
        t.Parallel in resolve_test.go (env+chdir are process-global — the helper
        already enforces this).

# Go stdlib refs (stable, used by the tests).
- url: https://pkg.go.dev/reflect#DeepEqual
  why: Compares the v2 Config struct, the []string slices, and the []any-decoded
        JSON arrays. Works across maps (OptionalAliases) and slices (QueryAliases).
- url: https://pkg.go.dev/testing#T.Setenv
  why: Drives WSPF_CONFIG / WSPF_* in resolve_test.go. Restored after the test.
        Cannot be used with t.Parallel (irrelevant — tests are serial).
```

### Current Codebase tree (the INPUT state — after parallel T2.S2)

```bash
# Repo root: /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # go 1.25.0 + require go-sdk v1.6.1 (T2.S1)              [UNCHANGED]
  go.sum            # (T2.S1)                                                     [UNCHANGED]
  config.go         # v2 11-field Config + DefaultConfig + LoadConfig +         [UNCHANGED]
                    #   resolveConfigPath + fileExists + ResolveConfig (T1.x)
  logger.go         # logger/newLogger/log/redactHeaders (T2.S1)                [UNCHANGED]
  health.go         # var version + healthHandler (T2.S1)                       [UNCHANGED]
  main.go           # logStartup(v2 fields) + authHeaderKey + authMiddleware +  [UNCHANGED]
                    #   main() with SDK handler (T2.S2)
  doc.go            # package comment                                            [UNCHANGED]
  config_test.go    # v1 schema (cfg.Aliases) — RED                              [REWRITE here]
  resolve_test.go   # v1 schema (cfg.Aliases) — RED                              [EDIT here]
  health_test.go    # v1 (newProxyHandler, cfg.Aliases, m["aliases"]) — RED     [EDIT here]
  logger_test.go    # tests newLogger only — GREEN                               [UNCHANGED]
  testdata/*.sse    # golden fixtures (kept; reused by upstream_test.go)         [UNCHANGED]
  config.example.json README.md                                                  [UNCHANGED]
  # v1 proxy.go/sse.go/rewrite.go + 6 proxy/sse/rewrite test files = DELETED (T2.S2)
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  # UNCHANGED: go.mod go.sum config.go logger.go health.go main.go doc.go
  #            testdata/*.sse config.example.json README.md logger_test.go
  config_test.go    # REWRITTEN: v2 11-field DefaultConfig + v2 JSON table keys
  resolve_test.go   # EDITED: Aliases->QueryAliases (scope guard) + ToolsValidation test
  health_test.go    # EDITED: TestLogStartup (v2 fields) + TestRouting_HealthzOnly (sentinel)
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: go vet ./... compiles the WHOLE package test binary at once. All three
// target files' errors are masked behind the FIRST one (config_test.go:19 Aliases).
// You MUST fix all three files before go vet/go test can go green — fixing only one
// just un-masks the next. Don't stop after config_test.go "compiles in isolation".

// CRITICAL: The three Tools validation rules (PRD §18.3) have NO env override —
// Tools/CanonicalTool/TargetTool can only be set via a config FILE. So the new
// TestResolveConfig_ToolsValidation cases use writeConfig(t, body) + t.Setenv(
// "WSPF_CONFIG", p), NOT t.Setenv("WSPF_TOOLS", ...). (Only WSPF_UPSTREAM/LISTEN/
// LOG_LEVEL are env-overridable; see config.go ResolveConfig.)

// CRITICAL: Default CanonicalTool="web_search" and TargetTool="web_search_prime"
// are baked into DefaultConfig(). A file with {"tools":[]} overrides Tools to empty
// -> ResolveConfig fails the "tools list must not be empty" check. A file with
// {"tools":["x"]} leaves canonical at "web_search" -> fails "must contain the
// canonical tool". A file with {"tools":["web_search","web_search_prime"]} -> fails
// "must not contain the target tool". Use these exact bodies.

// CRITICAL: TestDefaultConfig MUST transcribe the want struct LITERALLY from PRD
// §18.2 — NOT `want := DefaultConfig()` (which would make the test tautological and
// could not catch a typo in DefaultConfig). The 14-entry QueryAliases and 3-key
// OptionalAliases are spelled out. (The LoadConfig table rows DO derive `want` from
// DefaultConfig() — correct, they test merge behavior.)

// CRITICAL: QueryAliases has 14 entries (PRD §18.2 lists: query, search_query, q,
// search, searchQuery, search_term, term, text, input, prompt, question, keywords,
// topic, searchString). The work-item blurb said "13 entries" — that's a miscount;
// the AUTHORITATIVE source is config.go's DefaultConfig() (14) and PRD §18.2 (14).
// The test uses reflect.DeepEqual against the literal 14-element slice, so it tracks
// the real default. Do NOT hardcode 13.

// CRITICAL: TestRouting_HealthzOnly uses a SENTINEL catch-all, NOT the SDK handler.
// Do NOT add `github.com/modelcontextprotocol/go-sdk/mcp` to health_test.go — it is
// unnecessary and adds coupling/risk. The sentinel (a http.HandlerFunc that sets a
// flag) proves /healthz is isolated from the catch-all. The SDK integration is
// P1.M5.T3's job (server_test.go E2E).

// GOTCHA: json.Marshal of a []string produces a JSON array; json.Unmarshal of that
// into map[string]any produces []any (each element is the string boxed). So
// m["tools"] is []any, NOT []string. Compare with reflect.DeepEqual against a
// constructed []any (loop cfg.Tools -> []any). m["canonical_tool"] is a string.

// GOTCHA: logStartup emits "tools", "canonical_tool", "query_aliases" (NOT
// "aliases"). Asserting m["aliases"] would be nil and fail. Read main.go's
// logStartup for the exact field set before writing TestLogStartup.

// GOTCHA: keep resolve_test.go's isolateConfigEnv(t) usage — it clears WSPF_*,
// isolates XDG + CWD. The Tools-validation cases MUST call it too (hermeticity),
// then set WSPF_CONFIG to the written file. No t.Parallel anywhere in resolve_test.go.

// GOTCHA: the tests assert ONLY err != nil for validation failures — NOT the exact
// error string. config.go's ResolveConfig error wording may be refined later; the
// tests stay green by checking the error's PRESENCE, not its text.

// SCOPE GUARD: Do NOT touch any production file (config.go/logger.go/health.go/
// main.go/doc.go), go.mod/go.sum, testdata/, README.md, config.example.json, or
// logger_test.go. Edit ONLY config_test.go, resolve_test.go, health_test.go.

// SCOPE GUARD: Do NOT add new tests for extract/teach/upstream/server — those are
// M2/M3/M4/M5. This subtask rewrites the SURVIVING v1 tests only. Do NOT create new
// test files. Do NOT run `go get`/`go mod tidy`.
```

## Implementation Blueprint

### Data models and structure

No new data models. The tests exercise the existing v2 `Config` (11 fields, PRD
§18.1), `DefaultConfig()`, `LoadConfig`, `ResolveConfig`, `healthHandler`,
`logStartup`, `newLogger`/`log`. See `config.go` for the exact struct.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT STATE (read-only)
  - RUN: `cd /home/dustin/projects/web-search-prime-fixer && go build ./...`
    -> MUST exit 0 (production green — T2.S2 landed).
  - RUN: `test ! -e proxy.go && test ! -e proxy_test.go && echo "v1 deleted"`
    -> the v1 files are GONE (newProxyHandler/newUpstreamClient undefined is expected).
  - RUN: `go vet ./... 2>&1 | head` -> shows "config_test.go:19:3: unknown field
    Aliases" (the RED state this subtask fixes).
  - RUN: `grep -nE 'Aliases|newProxyHandler|newUpstreamClient' config_test.go
    resolve_test.go health_test.go` -> lists every site to fix (the breakage map).
  - RUN: `grep -nE 'Aliases|newProxyHandler|newUpstreamClient|proxy|sse\.' logger_test.go
    || echo CLEAN` -> CLEAN (logger_test.go is untouched by this subtask).
  - WHY: Confirms the INPUT is "T2.S2 landed, v1 deleted, 3 test files RED". If
    proxy.go still exists or go build is RED, STOP — T2.S2 has not landed (parallel).

Task 1: REWRITE config_test.go (full file)
  - FILE: /home/dustin/projects/web-search-prime-fixer/config_test.go
  - WRITE the full content in "Implementation Patterns & Key Details" (a) below.
  - WHY: Changes span the want struct (11 fields), belt-and-suspenders (new fields),
    and 2 table rows (v2 JSON keys) — a full rewrite is cleaner than scattered edits.
    Imports unchanged (os, path/filepath, reflect, testing).

Task 2: EDIT resolve_test.go (targeted) + append new test
  - FILE: /home/dustin/projects/web-search-prime-fixer/resolve_test.go
  - EDIT the path_and_aliases_not_overridable subtest: 3 Aliases->QueryAliases
    renames (see "Implementation Patterns" (b)).
  - APPEND TestResolveConfig_ToolsValidation (verbatim from (b)) at END of file.
  - WHY: Only 1 subtest + 1 new test change; the other 8 tests + 2 helpers are
    correct as-is. Targeted edits minimize churn. Imports unchanged.

Task 3: EDIT health_test.go (two functions)
  - FILE: /home/dustin/projects/web-search-prime-fixer/health_test.go
  - REPLACE TestLogStartup (assert tools/canonical_tool/query_aliases) verbatim (c).
  - REPLACE TestRouting_HealthzOnly (sentinel catch-all) verbatim (c).
  - WHY: Only these two functions touch v1 symbols; the 4 healthHandler tests + the
    import block are unchanged. Imports unchanged (NO mcp import).

Task 4: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w config_test.go resolve_test.go health_test.go
        go build ./...        # MUST stay green (production untouched)
        go vet ./...          # MUST be clean now (all 3 files fixed)
        gofmt -l .            # MUST print nothing
        go test ./...         # THE M1 GATE: all surviving tests PASS
  - EXPECT: all green. If vet/test still red, it is one of the 3 files — read the
    compiler error (it names the file:line), fix, re-run.
```

### Implementation Patterns & Key Details

#### (a) Full `config_test.go` — WRITE THIS ENTIRE FILE

```go
package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestDefaultConfig verifies every field of DefaultConfig() matches PRD §18.2
// verbatim (the v2 11-field schema: exact strings, the 14-entry QueryAliases slice
// in order, and the 3-key OptionalAliases map).
func TestDefaultConfig(t *testing.T) {
	def := DefaultConfig()

	want := Config{
		Upstream:       "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:         "127.0.0.1:8787",
		Path:           "/mcp",
		Tools:          []string{"web_search"},
		CanonicalTool:  "web_search",
		CanonicalParam: "query",
		QueryAliases: []string{
			"query", "search_query", "q", "search", "searchQuery",
			"search_term", "term", "text", "input", "prompt",
			"question", "keywords", "topic", "searchString",
		},
		OptionalAliases: map[string][]string{
			"location":              {"country", "region"},
			"content_size":          {"size", "contentSize", "detail"},
			"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
		},
		TargetTool:  "web_search_prime",
		TargetParam: "search_query",
		LogLevel:    "info",
	}
	if !reflect.DeepEqual(def, want) {
		t.Fatalf("DefaultConfig mismatch:\n got  %+v\n want %+v", def, want)
	}

	// Belt-and-suspenders per-field asserts so a typo names itself.
	if def.Upstream != "https://api.z.ai/api/mcp/web_search_prime/mcp" {
		t.Errorf("Upstream = %q, want the z.ai mcp URL", def.Upstream)
	}
	if def.Listen != "127.0.0.1:8787" {
		t.Errorf("Listen = %q, want 127.0.0.1:8787", def.Listen)
	}
	if def.Path != "/mcp" {
		t.Errorf("Path = %q, want /mcp", def.Path)
	}
	if !reflect.DeepEqual(def.Tools, []string{"web_search"}) {
		t.Errorf("Tools = %#v, want [\"web_search\"]", def.Tools)
	}
	if def.CanonicalTool != "web_search" {
		t.Errorf("CanonicalTool = %q, want web_search", def.CanonicalTool)
	}
	if def.CanonicalParam != "query" {
		t.Errorf("CanonicalParam = %q, want query", def.CanonicalParam)
	}
	wantAliases := []string{
		"query", "search_query", "q", "search", "searchQuery",
		"search_term", "term", "text", "input", "prompt",
		"question", "keywords", "topic", "searchString",
	}
	if !reflect.DeepEqual(def.QueryAliases, wantAliases) {
		t.Errorf("QueryAliases = %#v, want the 14-element default slice in order", def.QueryAliases)
	}
	wantOptionals := map[string][]string{
		"location":              {"country", "region"},
		"content_size":          {"size", "contentSize", "detail"},
		"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
	}
	if !reflect.DeepEqual(def.OptionalAliases, wantOptionals) {
		t.Errorf("OptionalAliases = %#v, want the 3-key default map", def.OptionalAliases)
	}
	if def.TargetTool != "web_search_prime" {
		t.Errorf("TargetTool = %q, want web_search_prime", def.TargetTool)
	}
	if def.TargetParam != "search_query" {
		t.Errorf("TargetParam = %q, want search_query", def.TargetParam)
	}
	if def.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", def.LogLevel)
	}
}

// TestLoadConfig_EmptyPath verifies LoadConfig("") returns the defaults with no
// error — this is how the server runs with no config file at all.
func TestLoadConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") returned error %v, want nil", err)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("LoadConfig(\"\") = %+v, want DefaultConfig() %+v", cfg, DefaultConfig())
	}
}

// TestLoadConfig_FromFile exercises the v2 file-merge path with table-driven
// subtests: partial override keeps defaults, unknown fields are ignored, v2-only
// fields override, a full v2 file sets every field, and invalid JSON yields an error.
func TestLoadConfig_FromFile(t *testing.T) {
	// Rows 1/2/4 derive `want` from DefaultConfig() so the test tracks future
	// default changes automatically rather than spelling them out again.
	partial := DefaultConfig()
	partial.Listen = "0.0.0.0:9999"
	partial.QueryAliases = []string{"foo"}

	unknown := DefaultConfig()
	unknown.Upstream = "http://example.invalid/mcp"
	unknown.LogLevel = "warn"

	v2fields := DefaultConfig()
	v2fields.Tools = []string{"web_search", "search"}
	v2fields.CanonicalTool = "web_search"
	v2fields.OptionalAliases = map[string][]string{
		"location": {"country"},
	}

	tests := []struct {
		name    string
		json    string
		wantErr bool
		want    Config
	}{
		{
			name:    "partial_override_keeps_defaults",
			json:    `{"listen":"0.0.0.0:9999","query_aliases":["foo"]}`,
			wantErr: false,
			want:    partial,
		},
		{
			name:    "unknown_fields_ignored",
			json:    `{"upstream":"http://example.invalid/mcp","banana":42,"nested":{"a":1},"log_level":"warn"}`,
			wantErr: false,
			want:    unknown,
		},
		{
			name:    "v2_fields_override",
			json:    `{"tools":["web_search","search"],"canonical_tool":"web_search","optional_aliases":{"location":["country"]}}`,
			wantErr: false,
			want:    v2fields,
		},
		{
			name: "full_file_all_fields",
			json: `{"upstream":"http://u","listen":"127.0.0.1:1","path":"/p","tools":["web_search"],"canonical_tool":"web_search","canonical_param":"query","query_aliases":["a","b"],"optional_aliases":{"location":["country"]},"target_tool":"web_search_prime","target_param":"search_query","log_level":"debug"}`,
			wantErr: false,
			want: Config{
				Upstream:       "http://u",
				Listen:         "127.0.0.1:1",
				Path:           "/p",
				Tools:          []string{"web_search"},
				CanonicalTool:  "web_search",
				CanonicalParam: "query",
				QueryAliases:   []string{"a", "b"},
				OptionalAliases: map[string][]string{
					"location": {"country"},
				},
				TargetTool:  "web_search_prime",
				TargetParam: "search_query",
				LogLevel:    "debug",
			},
		},
		{
			name:    "invalid_json_returns_error",
			json:    `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "config.json")
			if err := os.WriteFile(p, []byte(tt.json), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got, err := LoadConfig(p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfig(%q) err = %v, wantErr = %v", p, err, tt.wantErr)
			}
			if tt.wantErr {
				// On error cfg may be partial; do not assert its contents.
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadConfig mismatch:\n got  %+v\n want %+v", got, tt.want)
			}
		})
	}
}

// TestLoadConfig_MissingFile pins the LoadConfig/ResolveConfig boundary: a
// non-empty path to a missing file is an ERROR (the "no file → defaults" decision
// belongs to ResolveConfig, which decides whether to even call LoadConfig).
func TestLoadConfig_MissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does-not-exist.json")
	cfg, err := LoadConfig(p)
	if err == nil {
		t.Fatalf("LoadConfig(%q) returned nil error, want non-nil for a missing file", p)
	}
	// cfg may be partial/DefaultConfig on the error path; do not assert it.
	_ = cfg
}
```

#### (b) `resolve_test.go` — targeted edits

**Edit 1 — the `path_and_aliases_not_overridable` subtest** (inside
`TestResolveConfig_EnvOverrides`). Replace the subtest body:

```go
	t.Run("path_and_aliases_not_overridable", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_PATH", "/should/be/ignored")
		t.Setenv("WSPF_QUERY_ALIASES", "ignored") // no such override; DefaultConfig.QueryAliases kept
		cfg, err := ResolveConfig()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if cfg.Path != "/mcp" {
			t.Errorf("Path=%q want /mcp (no WSPF_PATH override)", cfg.Path)
		}
		if !reflect.DeepEqual(cfg.QueryAliases, DefaultConfig().QueryAliases) {
			t.Errorf("QueryAliases changed by non-existent WSPF_QUERY_ALIASES: %+v", cfg.QueryAliases)
		}
	})
```
(Only 3 changes from the original: `WSPF_ALIASES`→`WSPF_QUERY_ALIASES`, the comment,
`cfg.Aliases`→`cfg.QueryAliases`, `DefaultConfig().Aliases`→`DefaultConfig().QueryAliases`,
and the error-message field. The logic is identical.)

**Edit 2 — APPEND this test at the END of `resolve_test.go`** (after
`TestResolveConfig_TargetParamForced`):

```go
// (i) Tools validation (PRD §18.3). Tools/CanonicalTool/TargetTool have NO env
// override, so these cases drive ResolveConfig via a config FILE (writeConfig +
// WSPF_CONFIG), reusing isolateConfigEnv for hermeticity. The three rules:
// (1) Tools empty -> error; (2) Tools missing CanonicalTool -> error;
// (3) Tools contains TargetTool -> error. Plus one positive (valid extra tool).
// Tests assert err != nil (presence), not the exact wording.
func TestResolveConfig_ToolsValidation(t *testing.T) {
	cases := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "tools_empty",
			json:    `{"tools":[]}`,
			wantErr: true,
		},
		{
			// Tools set to a list that does NOT contain the default CanonicalTool
			// ("web_search") -> "must contain the canonical tool".
			name:    "tools_missing_canonical",
			json:    `{"tools":["not_the_canonical"],"canonical_tool":"web_search"}`,
			wantErr: true,
		},
		{
			// Tools includes the default TargetTool ("web_search_prime") ->
			// "must not contain the target tool".
			name:    "tools_contains_target",
			json:    `{"tools":["web_search","web_search_prime"]}`,
			wantErr: true,
		},
		{
			// Positive: an extra advertised tool that is neither canonical-only nor
			// the target. "search" is a legitimate client-facing alias tool name.
			name:    "tools_valid_extra",
			json:    `{"tools":["web_search","search"]}`,
			wantErr: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv("WSPF_CONFIG", writeConfig(t, c.json))
			_, err := ResolveConfig()
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}
```

> NOTE: leave `isolateConfigEnv`, `writeConfig`, `fieldByJSONName`, and all 8 other
> tests (`TestResolveConfig_Defaults`, `_WSPF_CONFIG`, `_CwdDiscovery`, `_XDGDiscovery`,
> `_Precedence_CwdBeforeXDG`, `_EnvOverrides`, `_Validation`, `_TargetParamForced`)
> byte-for-byte unchanged. They already use v2-safe paths
> (`reflect.DeepEqual(cfg, DefaultConfig())`, env vars).

#### (c) `health_test.go` — two function replacements

Leave `TestHealthHandler`, `TestHealthHandler_VersionOverride`,
`TestHealthHandler_NoUpstream`, `TestHealthHandler_RejectsNonGET`, and the import
block (`bytes`, `encoding/json`, `io`, `net/http`, `net/http/httptest`, `reflect`,
`testing`) **unchanged**. Replace ONLY these two functions:

```go
// (d) logStartup emits one info/startup line with the v2 config fields and NO
// authorization field (PRD §15 "Never logs credentials"). v2 fields: tools,
// canonical_tool, query_aliases (as JSON arrays / scalar), listen, upstream,
// log_level.
func TestLogStartup(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	cfg := DefaultConfig()

	logStartup(l, cfg)

	// Exactly one line.
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (raw=%q)", len(lines), buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("line not valid JSON: %v (raw=%q)", err, lines[0])
	}
	if m["level"] != "info" {
		t.Errorf("level = %#v, want info", m["level"])
	}
	if m["msg"] != "startup" {
		t.Errorf("msg = %#v, want startup", m["msg"])
	}
	// tools is a JSON array matching cfg.Tools ([]any after decode).
	tools, ok := m["tools"].([]any)
	if !ok {
		t.Fatalf("tools = %#v, want a JSON array", m["tools"])
	}
	wantTools := make([]any, len(cfg.Tools))
	for i, tt := range cfg.Tools {
		wantTools[i] = tt
	}
	if !reflect.DeepEqual(tools, wantTools) {
		t.Errorf("tools = %#v, want %#v", tools, wantTools)
	}
	// canonical_tool is the canonical tool name string.
	if m["canonical_tool"] != cfg.CanonicalTool {
		t.Errorf("canonical_tool = %#v, want %q", m["canonical_tool"], cfg.CanonicalTool)
	}
	// query_aliases is a JSON array matching cfg.QueryAliases ([]any after decode).
	aliases, ok := m["query_aliases"].([]any)
	if !ok {
		t.Fatalf("query_aliases = %#v, want a JSON array", m["query_aliases"])
	}
	wantAliases := make([]any, len(cfg.QueryAliases))
	for i, a := range cfg.QueryAliases {
		wantAliases[i] = a
	}
	if !reflect.DeepEqual(aliases, wantAliases) {
		t.Errorf("query_aliases = %#v, want %#v", aliases, wantAliases)
	}
	if m["listen"] != cfg.Listen {
		t.Errorf("listen = %#v, want %q", m["listen"], cfg.Listen)
	}
	if m["upstream"] != cfg.Upstream {
		t.Errorf("upstream = %#v, want %q", m["upstream"], cfg.Upstream)
	}
	if m["log_level"] != cfg.LogLevel {
		t.Errorf("log_level = %#v, want %q", m["log_level"], cfg.LogLevel)
	}
	// Security invariant: no credential field (Config has none; assert defensively).
	if _, present := m["authorization"]; present {
		t.Errorf("startup line contains an authorization field: %#v", m["authorization"])
	}
}

// (e) Routing (PRD §16/§9): /healthz routes to the local healthHandler and NEVER
// falls through to the catch-all. main() mounts the SDK StreamableHTTPHandler at
// "/"; this test substitutes a SENTINEL catch-all (decoupled from the SDK — the
// SDK integration is exercised by server_test.go in P1.M5.T3) that fails if hit,
// proving /healthz isolation. The /healthz body {"ok":true,...} also proves it
// reached healthHandler, not the catch-all.
func TestRouting_HealthzOnly(t *testing.T) {
	catchAllHit := false
	catchAll := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		catchAllHit = true
		w.WriteHeader(http.StatusNotFound)
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.Handle("/", catchAll) // sentinel stands in for the SDK handler at "/"

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("/healthz body not valid JSON: %v (raw=%q)", err, body)
	}
	if m["ok"] != true {
		t.Errorf("/healthz body ok = %#v, want true (routed to healthHandler)", m["ok"])
	}
	if catchAllHit {
		t.Error("/healthz fell through to the catch-all; want isolated routing to healthHandler")
	}
}
```

```go
// PATTERN: validate-then-assert. The resolve_test.go Tools-validation cases assert
// ONLY err != nil (not the error string) — config.go's wording may evolve, and the
// contract is the rule's BEHAVIOR (reject), not its message. Same for the existing
// Listen/Upstream validation table.

// PATTERN: config-file-driven ResolveConfig tests. Tools/CanonicalTool/TargetTool
// have no env override, so write them into a JSON file via writeConfig(t, body) and
// point WSPF_CONFIG at it. isolateConfigEnv(t) first, for hermeticity.

// PATTERN: sentinel-not-SDK in routing tests. Mounting the real SDK handler in a
// routing test couples the test to SDK behavior (DNS-rebinding protection, session
// init). A sentinel http.HandlerFunc proves the routing table in isolation; the SDK
// round-trip is an E2E concern (P1.M5.T3 server_test.go).
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"     # all three test files are package main, repo root

SYMBOLS CONSUMED (read-only — must already exist, owned by prior subtasks):
  - config.go:   Config (11 fields), DefaultConfig(), LoadConfig(path),
                 ResolveConfig(), resolveConfigPath(), fileExists()
  - main.go:     logStartup(l *logger, cfg Config)
  - logger.go:   newLogger(w io.Writer, level string) *logger, (*logger).log(...)
  - health.go:   var version, healthHandler(w, r)
  - resolve_test.go (internal): isolateConfigEnv(t), writeConfig(t, body),
                 fieldByJSONName(cfg, field) — REUSED, not redefined

SYMBOLS INTRODUCED:
  - func TestResolveConfig_ToolsValidation(t *testing.T)   # NEW in resolve_test.go

NO INTEGRATION POINTS TOUCHED BY THIS SUBTASK:
  - No production file edited (config.go/logger.go/health.go/main.go/doc.go).
  - No go.mod/go.sum change. No testdata/ change. No README/config.example.json change.
  - logger_test.go untouched. No new test files created.
  - This is a TEST-ONLY change; the M1 gate is `go test ./...` green.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -w config_test.go resolve_test.go health_test.go   # format in place
gofmt -l .                                               # MUST print nothing
go vet ./...                                             # MUST exit 0, no output
go build ./...                                           # MUST stay exit 0 (production untouched)

# No v1 references survive in the three target files:
! grep -nE '\bAliases\b|newProxyHandler|newUpstreamClient|"aliases"' config_test.go resolve_test.go health_test.go
# MUST succeed (exit 0). If it prints anything, a v1 ref remains — fix it.

# logger_test.go untouched:
git diff --name-only | grep -x 'logger_test.go' && echo "ERROR: logger_test.go was modified" || echo "logger_test.go untouched (correct)"
# Expected: "logger_test.go untouched (correct)".

# Expected: gofmt silent; vet/build clean; no v1 refs in the three files.
```

### Level 2: Unit Tests (Component Validation — THE M1 GATE)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...                                            # THE GATE: MUST pass (exit 0)
go test -v -run 'TestDefaultConfig|TestLoadConfig' ./... # config_test.go cases
go test -v -run 'TestResolveConfig' ./...               # resolve_test.go (incl. new ToolsValidation)
go test -v -run 'TestLogStartup|TestRouting_HealthzOnly|TestHealthHandler' ./...  # health_test.go
go test -v -run 'TestLogger|TestRedactHeaders|TestNewLogger' ./...  # logger_test.go (unchanged, must PASS)
```

```bash
# Expected: every command exits 0, all tests PASS. If TestDefaultConfig fails, diff
# the literal `want` against DefaultConfig() (a field typo names itself). If
# TestResolveConfig_ToolsValidation/tools_missing_canonical unexpectedly passes
# (no error), the file body is wrong — canonical "web_search" must NOT be in the
# tools list for that case. If TestRouting_HealthzOnly fails on catchAllHit, the
# mux routed /healthz to "/" (shouldn't happen — /healthz exact-match wins).
```

### Level 3: Integration / Hermeticity (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Confirm no real user config / no stray CWD file leaks into ResolveConfig tests
# (isolateConfigEnv isolates XDG + CWD + clears WSPF_*). Re-run the full suite to
# catch any ordering/env leakage:
go test -count=2 ./...        # MUST pass twice (idempotent, no cross-test state)

# Confirm the package still builds a binary (production code is untouched here, but
# assert it for the M1 gate):
go build -o /tmp/wspf-m1gate . && echo "binary builds" && rm -f /tmp/wspf-m1gate
# Expected: "binary builds".
```

### Level 4: Coverage & Scope Checks (Domain-Specific)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# The three PRD §18.3 Tools rules each have an error case + one positive:
go test -v -run 'TestResolveConfig_ToolsValidation' ./... 2>&1 | grep -E 'tools_empty|tools_missing_canonical|tools_contains_target|tools_valid_extra'
# Expected: all four subtests PASS.

# TestLogStartup asserts the v2 fields (not "aliases"):
grep -nE 'm\["tools"\]|m\["canonical_tool"\]|m\["query_aliases"\]' health_test.go
# Expected: three matches. (No m["aliases"].)

# TestDefaultConfig covers all 11 fields:
grep -cE 'def\.(Upstream|Listen|Path|Tools|CanonicalTool|CanonicalParam|QueryAliases|OptionalAliases|TargetTool|TargetParam|LogLevel)' config_test.go
# Expected: >= 11 (belt-and-suspenders per-field asserts).

# Scope guard: NO production file was modified:
git diff --name-only | grep -vE '_test\.go$' || echo "only test files changed (correct)"
# Expected: "only test files changed (correct)" (or empty if only the 3 test files).

# go.mod/go.sum untouched:
git diff --name-only go.mod go.sum   # MUST be empty (unchanged)
```

## Final Validation Checklist

### Technical Validation
- [ ] `gofmt -l .` prints nothing.
- [ ] `go build ./...` exits 0 (production green — untouched, must remain so).
- [ ] `go vet ./...` exits 0 (was RED on config_test.go:19 before; now clean).
- [ ] **`go test ./...` passes** (exit 0) — the M1 definition-of-done gate.
- [ ] `go test -count=2 ./...` passes twice (no cross-test env/state leakage).

### Feature Validation
- [ ] `config_test.go` `TestDefaultConfig` `want` is the literal 11-field v2 Config (PRD §18.2: 14-entry `QueryAliases`, 3-key `OptionalAliases`).
- [ ] `config_test.go` table JSON uses v2 keys (`query_aliases`, `tools`, `canonical_tool`, `optional_aliases`); NO `aliases` key.
- [ ] `resolve_test.go` has NO `Aliases` reference; scope-guard subtest uses `QueryAliases`.
- [ ] `resolve_test.go` `TestResolveConfig_ToolsValidation` covers empty / missing-canonical / contains-target (errors) + one valid case (no error).
- [ ] `health_test.go` `TestLogStartup` asserts `m["tools"]`, `m["canonical_tool"]`, `m["query_aliases"]` (not `m["aliases"]`).
- [ ] `health_test.go` `TestRouting_HealthzOnly` uses a sentinel catch-all (no `newProxyHandler`/`newUpstreamClient`); proves `/healthz` isolation.
- [ ] `logger_test.go` passes **unchanged** (`git diff` shows no change to it).

### Code Quality Validation
- [ ] Tests follow existing conventions (`reflect.DeepEqual`, table-driven `t.Run`, `isolateConfigEnv`/`writeConfig` reuse, `httptest`, `package main`).
- [ ] No SDK coupling in health_test.go (sentinel, not `mcp.*`); no new imports in any of the three files.
- [ ] Validation tests assert `err != nil` (presence), not error wording.
- [ ] Scope respected: ONLY `config_test.go` (rewrite), `resolve_test.go` (1 subtest + 1 new test), `health_test.go` (2 functions) edited. Nothing else.

### Documentation & Deployment
- [ ] No user-facing/config/API surface change (test-only) — no README/config.example.json/doc.go change.
- [ ] No new env vars (the tests use the existing `WSPF_*` set; no production change).

---

## Anti-Patterns to Avoid

- ❌ Don't fix only `config_test.go` and call it done — `go vet`/`go test` compile the WHOLE package test binary; the resolve_test.go and health_test.go errors are masked behind the first. All three must be fixed together for green.
- ❌ Don't drive the Tools-validation cases via env vars — `Tools`/`CanonicalTool`/`TargetTool` have NO env override. Use `writeConfig(t, body)` + `WSPF_CONFIG` (reusing `isolateConfigEnv`).
- ❌ Don't write `want := DefaultConfig()` in `TestDefaultConfig` — that's tautological and can't catch a DefaultConfig typo. Transcribe the 11 fields LITERALLY from PRD §18.2.
- ❌ Don't hardcode `QueryAliases` as 13 entries — PRD §18.2 and config.go have **14** (the blurb's "13" is a miscount). Use `reflect.DeepEqual` against the literal 14-element slice.
- ❌ Don't assert `m["aliases"]` in `TestLogStartup` — v2 `logStartup` emits `tools`/`canonical_tool`/`query_aliases` (read main.go). Asserting `aliases` returns nil and fails.
- ❌ Don't compare decoded JSON arrays as `[]string` — `json.Unmarshal` into `map[string]any` yields `[]any`. Build an `[]any` (loop the slice) and compare with `reflect.DeepEqual`.
- ❌ Don't mount the real SDK handler in `TestRouting_HealthzOnly` — it couples the test to SDK behavior (DNS-rebinding protection, session init). Use a sentinel `http.HandlerFunc`; the SDK round-trip is P1.M5.T3's E2E job.
- ❌ Don't add `github.com/modelcontextprotocol/go-sdk/mcp` to health_test.go — the sentinel approach needs no SDK import.
- ❌ Don't touch `logger_test.go` — it is already green and decoupled. "Verify it passes" means RUN it, not EDIT it.
- ❌ Don't edit any production file (`config.go`/`logger.go`/`health.go`/`main.go`/`doc.go`), `go.mod`/`go.sum`, `testdata/`, `README.md`, or `config.example.json`. This is test-only.
- ❌ Don't assert the exact `ResolveConfig` error strings — assert `err != nil`. The wording may be refined; the rule's BEHAVIOR (reject) is the contract.
- ❌ Don't redefine `isolateConfigEnv`/`writeConfig`/`fieldByJSONName` — they already exist in resolve_test.go; reuse them.
- ❌ Don't run `go get`/`go mod tidy`/edit `go.mod` — no dependency change is needed or allowed here.
- ❌ Don't add tests for extract/teach/upstream/server — those are M2–M5. This subtask rewrites the surviving v1 tests only.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is a test-only rewrite with every changed line spelled out
verbatim. The exact v1 references in all three files are **grep-confirmed** in
`research/v2_test_rewrite_breakage_map.md` (file:line for each `Aliases`/
`newProxyHandler`/`newUpstreamClient` site). The v2 production contract they compile
against (`config.go` 11-field schema + `DefaultConfig` + the three Tools rules;
`main.go` `logStartup` fields; `health.go`/`logger.go`) has been **read on-disk**, and
`go build ./...` is **verified GREEN** on the live tree (so production is intact).
`logger_test.go` is confirmed clean and is left untouched. The riskiest piece —
`TestRouting_HealthzOnly` — is deliberately decoupled from the SDK (sentinel
catch-all), eliminating SDK-in-test surprises; it's explicitly sanctioned by the
contract ("or simplify to just test /healthz routing"). The three Tools-validation
cases are driven by config files (the only way to set those fields) with exact JSON
bodies that map to each rule. The residual 1/10 risk is an agent (a) stopping after
config_test.go (vet compiles the whole package — masked errors), (b) using an env var
for Tools validation (impossible — no override), (c) asserting `m["aliases"]` instead
of the v2 fields, or (d) accidentally editing a production file — all four are pinned
explicitly in the Gotchas/Anti-Patterns with the exact grep checks that catch them.
