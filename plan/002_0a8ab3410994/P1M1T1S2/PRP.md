# PRP — P1.M1.T1.S2: Extend ResolveConfig validation for new rules (Tools, CanonicalTool, TargetTool)

## Goal

**Feature Goal**: Extend `ResolveConfig()` in `config.go` with three NEW
validation rules that run AFTER the existing Listen/Upstream checks and BEFORE
the existing force-TargetParam-if-empty logic, so that a misconfigured
`Tools`/`CanonicalTool`/`TargetTool` fails fast with a clear error at startup
instead of shipping a broken tool advertisement. The three rules (PRD §18.3):
(a) `Tools` is non-empty; (b) `Tools` contains `CanonicalTool`; (c) no entry in
`Tools` equals `TargetTool` (we must never advertise z.ai's real name
`web_search_prime` — PRD §9.3, §18.3, §3 non-goals). Edit ONLY `config.go`
(`ResolveConfig` body + its doc comment). Discovery, env-override, file-overlay,
and force-TargetParam logic are UNCHANGED.

**Deliverable**: An edited `config.go` whose `ResolveConfig()` performs the six
validation steps in order — Listen, Upstream, Tools-non-empty,
Tools-contains-CanonicalTool, no-Tools-equals-TargetTool — returns a clear
`fmt.Errorf` on the first failure, still forces `TargetParam="search_query"` when
empty, and whose `ResolveConfig` doc comment documents all six validation rules
plus fixes the stale v1 wording S1 deliberately left behind. Consumed by
`main.go` bootstrap (P1.M1.T2.S1) and the `resolve_test.go` rewrite (P1.M1.T3.S1).

**Success Definition**: `config.go` typechecks cleanly in the isolated temp-module
gate (scoped gate — see Validation Loop); the three new rules are present,
ordered, and return clear errors; the default config still validates (no
behavior change for default users); and NO file other than `config.go` is
modified. (The full package build is intentionally NOT green yet — same
documented breakage as S1.)

## User Persona (if applicable)

**Target User**: Operator / developer running the normalizing MCP server locally.
**Use Case**: Starting the server with a hand-written `web-search-prime-fixer.json`
or `WSPF_CONFIG` that misconfigures `tools`/`canonical_tool`/`target_tool`.
**User Journey**: `web-search-prime-fixer` boots → `ResolveConfig()` runs → on the
first violated rule the server logs a clear one-line error and exits non-zero
(via `main.go` fail-fast, P1.M1.T2.S1) instead of starting with a broken tool
advertisement.
**Pain Points Addressed**: A server that boots and silently advertises
`web_search_prime` (leaking z.ai's real name) or with an empty/`canonical`-less
tool list would defeat the entire v2 design ("make the agent's first try work").
Fail-fast with a clear message catches this at boot.

## Why

- The whole v2 design hinges on the server advertising exactly the right tool
  surface (PRD §9): one canonical `web_search` plus optional terse aliases, and
  **never** z.ai's real `web_search_prime` (PRD §9.3, §3 non-goals, §18.3).
  Validation is the last line of defense before that surface reaches the agent.
- `main.go` (P1.M1.T2.S1) calls `ResolveConfig()` and fail-fasts on its error, so
  extending validation here means a bad config can never produce a running server.
- The default config (`DefaultConfig()` from S1) already satisfies all three new
  rules, so this change is invisible to users who run with no config file — it
  only ever fires on genuine misconfiguration.
- This is the validation half of the config pair: S1 owned the **schema**
  (struct + defaults + overlay); S2 owns the **rules** (`ResolveConfig` body). The
  split keeps each PRP a single-file, single-concern edit.

## What

Edit ONLY `config.go`. Two regions change; the rest of the file is preserved:

1. **`ResolveConfig()` body** — between the existing Upstream validation block
   (`!u.IsAbs()` check) and the existing "Force TargetParam if empty" block,
   insert three new checks using `slices.Contains` (Go 1.21+ stdlib; go.mod is
   `go 1.22`). Each returns a clear `fmt.Errorf` on failure (no `%w` — these are
   pure logical checks with no underlying stdlib error to chain, exactly like the
   existing "not absolute" branch). Add `"slices"` to the import block.
2. **`ResolveConfig` doc comment** — append the three new validation rules to the
   `# Validation` section, and fix three pieces of stale v1 wording S1 left for
   S2: `proxy`→`normalizing MCP server`, `PRD §14.3`→`PRD §18.3`, and the
   "Path and Aliases have NO env override" line (update to the v2 field set).

`resolveConfigPath()`, `fileExists()`, `LoadConfig()`, `DefaultConfig()`, the
`type Config` struct, the env-override block, and the force-TargetParam block are
NOT touched. No other file is touched.

### Success Criteria

- [ ] `ResolveConfig()` performs validation in this exact order: Listen → Upstream → Tools-non-empty → Tools-contains-CanonicalTool → no-Tools-equals-TargetTool → (force TargetParam if empty) → return.
- [ ] Rule (a): `len(cfg.Tools)==0` returns a clear `fmt.Errorf`.
- [ ] Rule (b): `!slices.Contains(cfg.Tools, cfg.CanonicalTool)` returns a clear `fmt.Errorf`.
- [ ] Rule (c): `slices.Contains(cfg.Tools, cfg.TargetTool)` returns a clear `fmt.Errorf`.
- [ ] The default config (`DefaultConfig()`: `Tools=["web_search"]`, `CanonicalTool="web_search"`, `TargetTool="web_search_prime"`) still validates with `nil` error — no behavior change for default users.
- [ ] The force-TargetParam-if-empty block is unchanged and still runs after all validation.
- [ ] Discovery (`resolveConfigPath`) and env-override (`WSPF_*`) logic unchanged.
- [ ] `ResolveConfig` doc comment documents all six validation rules and has no stale v1 wording (`proxy`→`normalizing MCP server`, §14.3→§18.3, env-override line updated).
- [ ] `config.go` typechecks clean in the isolated temp-module gate (Level 1). `"slices"` is the only new import; no `go.mod` change.
- [ ] No edits to any file other than `config.go` (not main.go, proxy.go, tests, go.mod, ResolveConfig's discovery/override code, PRD, tasks.json).

## All Needed Context

### Context Completeness Check

_Pass._ This subtask modifies one existing, well-understood function
(`ResolveConfig` in `config.go`, whose full current body is quoted below) and its
doc comment. The exact three rules, their order, the exact error convention to
follow, the Go-version/stdlib availability (`slices` in `go 1.22`), the default
config that must keep validating, and the exact downstream breakage are all pinned
from PRD §18.3, the v1 `ResolveConfig` source, `go.mod`, and on-disk grep of
callers/literals. No guesswork required.

### Documentation & References

```yaml
# MUST READ — the authoritative validation spec.
- file: PRD.md
  why: §18.3 (lines ~552-559) defines the validation rules verbatim: "Tools is
       non-empty and contains CanonicalTool, and no entry equals TargetTool
       (we never advertise z.ai's real name); else exit with a clear error."
       §9.3 + §3 non-goals give the WHY behind rule (c) (never leak web_search_prime).
  critical: The PRD phrase is "exit with a clear error" — ResolveConfig RETURNS
        the error; main.go (P1.M1.T2.S1) is what exits. S2 only returns it.

- file: plan/002_0a8ab3410994/P1M1T1S1/PRP.md
  why: S1 is the upstream dependency (running in parallel). It defines the EXACT
        v2 Config struct + DefaultConfig that S2 validates. Treat it as a contract:
        after S1 the fields cfg.Tools, cfg.CanonicalTool, cfg.TargetTool EXIST and
        DefaultConfig yields Tools=["web_search"], CanonicalTool="web_search",
        TargetTool="web_search_prime" (which must keep passing validation).
  critical: S2's edits apply to the SAME file (config.go) as S1, but a DIFFERENT
        region (ResolveConfig vs Config struct/DefaultConfig/LoadConfig). The regions
        do not overlap. S1 lands first (it is the S2 INPUT).

- file: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  why: §1 documents the v1 ResolveConfig pattern to preserve (validation is a
        SEPARATE step after discovery+load+env-override, before return) and states
        the v2 validation additions explicitly. §7 maps v1→v2 JSON keys (no key S2
        cares about changed).
  critical: Discovery/env-override/overlay are S2-INVARIANT. Only the validation
        step grows. The error style (fmt.Errorf, %q for values, %w only when
        wrapping a stdlib error) is the v1 convention to follow.

- file: config.go
  why: THIS is the file being edited. Its current ResolveConfig body + doc comment
        (v1 state) is the baseline, quoted in "Current Codebase tree" below.
  critical: Only the ResolveConfig function body (insert 3 checks) + the ResolveConfig
        doc comment change. resolveConfigPath/fileExists/LoadConfig/DefaultConfig/
        type Config are S1's or untouched.

- file: plan/002_0a8ab3410994/P1M1T1S2/research/resolveconfig_validation_findings.md
  why: On-disk proof of: go 1.22 => slices.Contains available (stdlib, no dep); the
        exact caller set (main.go:174 bootstrap + resolve_test.go owned by T3.S1);
        the default config passing all three rules; the full-package build staying
        red (same as S1) and therefore requiring the isolated gate.
  critical: Rule errors are fmt.Errorf WITHOUT %w (no stdlib error to chain), matching
        the existing "upstream not absolute" branch — NOT the %w-wrapping Listen/
        Upstream branches.

- file: resolve_test.go
  why: Reference for the hermetic test pattern (isolateConfigEnv/writeConfig) the
        T3.S1 rewrite will use to exercise these rules. S2 does NOT edit this file.
  critical: The current happy-path tests (defaults, env-only overrides, target_param
        forcing) all STILL PASS the new rules because they never empty Tools or
        inject CanonicalTool/TargetTool. Only T3.S1 will ADD explicit rule-failure
        cases. S2's validation is a throwaway isolated-module test, not a commit.

- url: https://pkg.go.dev/slices#Contains
  why: Confirms slices.Contains[T](s []T, v T) bool is stdlib since Go 1.21.
  section: "func Contains"
  critical: go.mod is go 1.22, so this import needs NO go.mod change and NO new
        third-party require.
```

### Current Codebase tree (the v1 ResolveConfig to extend)

```bash
# Repo root: /home/dustin/projects/web-search-prime-fixer
#   config.go            <-- THIS FILE; only ResolveConfig() body + doc comment  (EDIT in S2)
#     imports (v1):      encoding/json, fmt, net, net/url, os, path/filepath
#     ADD import:        "slices"   (stdlib; go.mod is go 1.22)
#   main.go:174          calls ResolveConfig() (fail-fast on err) — UNCHANGED (T2.S1)
#   resolve_test.go      exercises ResolveConfig — UNCHANGED by S2 (T3.S1 rewrites)
#   go.mod               module web-search-prime-fixer; go 1.22  (UNCHANGED — slices is stdlib)
# (struct/DefaultConfig/LoadConfig are S1's region; discovery/override are invariant)
```

The v1 `ResolveConfig` regions S2 touches (verbatim from the current file):

```go
import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	// S2 ADDS: "slices"
)

// ResolveConfig discovers, loads, env-overrides, and validates the proxy
// configuration, returning a fully-validated Config ready for startup (PRD §14.3).
// ...
// # Validation (returns a clear, wrapped error on failure)
//
//   - Listen must be a parseable host:port (net.SplitHostPort).
//   - Upstream must be an absolute URL (url.Parse succeeds and URL.IsAbs reports
//     a non-empty scheme).
//
// After validation, if TargetParam is empty it is forced to "search_query".
// ...
// An empty env var is ignored (the file/default value is kept). Path and Aliases
// have NO env override.
func ResolveConfig() (Config, error) {
	path := resolveConfigPath()
	cfg, err := LoadConfig(path)
	if err != nil {
		return cfg, fmt.Errorf("load config %q: %w", path, err)
	}

	// Env overrides (highest precedence; empty values ignored).
	if v := os.Getenv("WSPF_UPSTREAM"); v != "" {
		cfg.Upstream = v
	}
	if v := os.Getenv("WSPF_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("WSPF_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	// Validate Listen: must parse as host:port.
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return cfg, fmt.Errorf("invalid listen address %q: %w", cfg.Listen, err)
	}
	// Validate Upstream: must be an absolute URL (non-empty scheme).
	u, err := url.Parse(cfg.Upstream)
	if err != nil {
		return cfg, fmt.Errorf("invalid upstream URL %q: %w", cfg.Upstream, err)
	}
	if !u.IsAbs() {
		return cfg, fmt.Errorf("upstream URL %q is not absolute (missing scheme)", cfg.Upstream)
	}

	// <--- S2 INSERTS THE THREE NEW Tools CHECKS HERE --->

	// Force TargetParam if empty (PRD §14.3).
	if cfg.TargetParam == "" {
		cfg.TargetParam = "search_query"
	}

	return cfg, nil
}
```

### Desired Codebase tree after S2

```bash
# Only config.go changes. ResolveConfig gains 3 checks (between Upstream and
# force-TargetParam) + "slices" import + an updated doc comment.
config.go            # EDITED: ResolveConfig body (3 checks), imports (+slices), ResolveConfig doc comment
# (every other file untouched; go.mod unchanged)
```

The exact insertion block to produce (transcribe verbatim — goes AFTER the
`!u.IsAbs()` block and BEFORE the "Force TargetParam if empty" block):

```go
	// Validate Tools (PRD §18.3) — three rules guarding what the server advertises.
	// These run after the Listen/Upstream checks and before the TargetParam forcing.
	//   (a) Tools must be non-empty: the server always advertises >= the canonical tool.
	//   (b) Tools must contain CanonicalTool: Tools[0] is canonical and tool
	//       registration + the teaching signal key off it.
	//   (c) No Tools entry may equal TargetTool: TargetTool is z.ai's real name
	//       ("web_search_prime"), which the server must NEVER advertise
	//       (PRD §9.3, §18.3, §3 non-goals).
	// Like the "upstream not absolute" rule above, these are pure logical checks
	// with no underlying error to %w-wrap; they return a plain, clear fmt.Errorf.
	if len(cfg.Tools) == 0 {
		return cfg, fmt.Errorf("tools list must not be empty")
	}
	if !slices.Contains(cfg.Tools, cfg.CanonicalTool) {
		return cfg, fmt.Errorf("tools list must contain the canonical tool %q", cfg.CanonicalTool)
	}
	if slices.Contains(cfg.Tools, cfg.TargetTool) {
		return cfg, fmt.Errorf("tools list must not contain the target tool %q (it would advertise z.ai's real name)", cfg.TargetTool)
	}
```

The import block after the edit:

```go
import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"slices"
)
```

The `ResolveConfig` doc-comment edits (three small fixes + the new bullets).
Current `# Validation` subsection becomes:

```go
// # Validation (returns a clear error on the first failure; main.go exits non-zero)
//
//   - Listen must be a parseable host:port (net.SplitHostPort).
//   - Upstream must be an absolute URL (url.Parse succeeds and URL.IsAbs reports
//     a non-empty scheme).
//   - Tools must be non-empty (the server always advertises >= the canonical tool).
//   - Tools must contain CanonicalTool (Tools[0] is canonical).
//   - No Tools entry may equal TargetTool (never advertise z.ai's real name;
//     PRD §9.3, §18.3).
//
// After validation, if TargetParam is empty it is forced to "search_query".
```

And fix the stale wording in the same comment:
- first line: `validates the proxy configuration` → `validates the normalizing MCP server configuration`
- every `PRD §14.3` reference → `PRD §18.3` (PRD renumbered; S1 already did §14.2→§18.2 in DefaultConfig)
- the env-override line `Path and Aliases have NO env override.` → `Path, Tools, CanonicalTool, CanonicalParam, QueryAliases, OptionalAliases, TargetTool, and TargetParam have NO env override.`

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: The full package WILL NOT BUILD after S2 — same as after S1.
//   main.go:160 cfg.Aliases undefined           (fixed in P1.M1.T2.S1)
//   proxy.go:498/499/512/532 cfg.Aliases        (proxy.go DELETED in P1.M1.T2.S2)
//   config_test.go/resolve_test.go/health_test.go/proxy_test.go reference stale
//   schema (.Aliases)                            (rewritten/deleted in T3.S1 / T2.S2)
// S2 MUST NOT touch those files. Verify config.go with the isolated temp-module
// gate (Validation Loop L1/L2), NOT `go build ./...`.

// CRITICAL: Rule errors use fmt.Errorf WITHOUT %w. Listen/Upstream use %w because
// they wrap a stdlib error (net/url). The 3 new rules are pure logical checks
// with nothing to chain — exactly like the existing "upstream not absolute" branch
// (`fmt.Errorf("upstream URL %q is not absolute ...", cfg.Upstream)`). Do NOT
// force %w onto them; there is no error to wrap.

// CRITICAL: Do NOT change the ORDER of the force-TargetParam block. The contract
// says the 3 new rules go "AFTER the existing Listen/Upstream checks" and to
// "keep the existing force-TargetParam-if-empty logic." Insert the 3 checks
// BETWEEN the Upstream check and the force-TargetParam block. (This is also
// correct functionally: rule (c) compares against TargetTool, which the
// force-TargetParam logic never touches — it only sets TargetParam.)

// CRITICAL: Do NOT touch resolveConfigPath, fileExists, LoadConfig, DefaultConfig,
// the type Config struct, or the env-override (WSPF_*) block. Discovery, overlay,
// and env-override are S2-INVARIANT. Only the ResolveConfig validation step grows.

// GOTCHA: slices.Contains is stdlib (Go 1.21+); go.mod is go 1.22, so adding the
// import needs NO go.mod change and NO new require line. Do NOT add a dependency.

// GOTCHA: The default config (Tools=["web_search"], CanonicalTool="web_search",
// TargetTool="web_search_prime") must STILL validate to nil. After your edit,
// re-run the isolated default-shape test — it must pass unchanged. If it fails,
// you wrote a rule that is too strict (e.g. checked TargetTool emptiness or
// required Tools[0] exactly, which the contract does NOT ask for).

// SCOPE GUARD: Do NOT edit resolve_test.go to add rule-failure cases — that is
// P1.M1.T3.S1's job. S2 validates the rules with a throwaway isolated-module test
// that is deleted before finishing. Do NOT edit main.go, proxy.go, go.mod, tests,
// PRD, or tasks.json. Edit ONLY config.go.
```

## Implementation Blueprint

### Data models and structure

No new data models. S2 validates fields that already exist after S1
(`cfg.Tools []string`, `cfg.CanonicalTool string`, `cfg.TargetTool string`) using
only stdlib `len` and `slices.Contains`. No new types, methods, or exported
symbols.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: EDIT config.go — add the "slices" import
  - IMPLEMENT: add "slices" to the existing import block (alphabetical order,
    after "path/filepath").
  - VERIFY: go.mod stays go 1.22; NO new require line (slices is stdlib).

Task 2: EDIT config.go — insert the three Tools validation checks into ResolveConfig
  - IMPLEMENT: the three-check block verbatim from "Desired Codebase tree",
    placed AFTER the `!u.IsAbs()` Upstream block and BEFORE the
    "Force TargetParam if empty" block.
  - RULE (a): if len(cfg.Tools)==0 -> return cfg, fmt.Errorf("tools list must not be empty")
  - RULE (b): if !slices.Contains(cfg.Tools, cfg.CanonicalTool) ->
        return cfg, fmt.Errorf("tools list must contain the canonical tool %q", cfg.CanonicalTool)
  - RULE (c): if slices.Contains(cfg.Tools, cfg.TargetTool) ->
        return cfg, fmt.Errorf("tools list must not contain the target tool %q (it would advertise z.ai's real name)", cfg.TargetTool)
  - NAMING/STYLE: follow the existing error style (fmt.Errorf, %q for values, no
        %w on pure logical checks). Return the (partial) cfg alongside the error,
        matching every other error return in this function.
  - DO NOT reorder the force-TargetParam block; DO NOT touch Listen/Upstream checks.

Task 3: EDIT config.go — update the ResolveConfig doc comment ([Mode A])
  - APPEND to the "# Validation" subsection the three new bullets (Tools non-empty,
        Tools contains CanonicalTool, no Tools entry == TargetTool), each citing
        the PRD section it comes from (§18.3; §9.3 for the no-leak rationale).
  - FIX stale wording: "proxy configuration" -> "normalizing MCP server configuration";
        every "PRD §14.3" -> "PRD §18.3"; the "Path and Aliases have NO env override"
        line -> the v2 field list (Path, Tools, CanonicalTool, CanonicalParam,
        QueryAliases, OptionalAliases, TargetTool, TargetParam).
  - KEEP the Discovery and Environment-overrides subsections of the comment intact
        (they describe S2-invariant behavior).

Task 4: LEAVE everything else ALONE
  - Do NOT edit resolveConfigPath / fileExists / LoadConfig / DefaultConfig /
        type Config / the env-override block / main.go / proxy.go / any *_test.go /
        go.mod / PRD / tasks.json.
  - resolve_test.go rule-failure cases are P1.M1.T3.S1.

Task 5: VALIDATE (scoped — see Validation Loop)
  - gofmt -l config.go                                  # MUST be empty
  - isolated temp-module go vet + go build of config.go # MUST be clean (L1)
  - throwaway isolated test: all 3 rules fire + default passes (L2), then DELETE it
  - EXPECT full-repo `go build ./...` to STILL FAIL on main.go:160 + proxy.go +
        test files (same set as S1; out of scope).
```

### Implementation Patterns & Key Details

```go
// PATTERN: error-return shape in ResolveConfig is uniform — return the (partial)
// cfg together with the error on every failure path. Match it:
if len(cfg.Tools) == 0 {
	return cfg, fmt.Errorf("tools list must not be empty") // no %w: nothing to chain
}

// PATTERN: membership test via stdlib slices (Go 1.21+; go.mod is 1.22). Both
// (b) and (c) are Contains checks, kept symmetric:
if !slices.Contains(cfg.Tools, cfg.CanonicalTool) { /* error */ }
if slices.Contains(cfg.Tools, cfg.TargetTool)    { /* error */ }

// CONTRAST — do NOT copy the %w style from Listen/Upstream; those wrap a stdlib
// error object. The new rules have no such object:
//   return cfg, fmt.Errorf("invalid listen address %q: %w", cfg.Listen, err)  // <-- has err to wrap
//   return cfg, fmt.Errorf("upstream URL %q is not absolute ...", ...)        // <-- pure check, NO %w (this is your model)

// PLACEMENT: insert AFTER the Upstream `!u.IsAbs()` block, BEFORE force-TargetParam.
// The 3 checks are independent of TargetParam (TargetTool is a different field),
// so this is the minimal-diff, contract-compliant spot.
```

### Integration Points

```yaml
VALIDATION STEP (config.go ResolveConfig):
  - insert: 3 checks (Tools non-empty; contains CanonicalTool; not contains TargetTool)
            between the Upstream check and the force-TargetParam block
  - import: add "slices" (stdlib; go 1.22 already satisfied)
  - errors: fmt.Errorf, plain (no %w); clear messages quoting offending values

INVARIANT (do NOT change in S2):
  - resolveConfigPath / fileExists         discovery WSPF_CONFIG > CWD > XDG > ""
  - LoadConfig                             DefaultConfig base + json.Unmarshal overlay
  - env overrides                          WSPF_UPSTREAM/LISTEN/LOG_LEVEL (empty ignored)
  - Listen / Upstream checks               unchanged
  - force TargetParam if empty -> "search_query"   unchanged, still runs after all checks

NO INTEGRATION POINTS TOUCHED BY S2:
  - go.mod:                 unchanged (slices is stdlib)
  - main.go:174 caller:     unchanged — it already fail-fasts on ResolveConfig's error
  - resolve_test.go:        unchanged (P1.M1.T3.S1 rewrites it)
  - Config struct/defaults: S1's region, untouched
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback) — SCOPED GATE

```bash
cd /home/dustin/projects/web-search-prime-fixer

# (a) Formatting.
gofmt -l config.go                 # MUST print nothing (empty == clean)

# (b) SCOPED COMPILE: config.go typechecks in isolation. config.go imports only
#     stdlib (incl. the new "slices") and references no package-internal symbols
#     beyond its own resolveConfigPath/LoadConfig/DefaultConfig (all in this file),
#     so it compiles standalone. This proves the new checks are correct WITHOUT
#     requiring the out-of-scope main.go/proxy.go/test fixes.
rm -rf /tmp/cfg-iso && mkdir -p /tmp/cfg-iso && cd /tmp/cfg-iso && \
  go mod init cfgiso >/dev/null 2>&1 && \
  cp /home/dustin/projects/web-search-prime-fixer/config.go . && \
  printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
  go vet . && go build . && echo "OK: config.go typechecks in isolation" && \
  cd /home/dustin/projects/web-search-prime-fixer && rm -rf /tmp/cfg-iso
# Expected: "OK: config.go typechecks in isolation", exit 0, no vet/build errors.

# (c) Edit-presence sanity (grep):
grep -c '"slices"' config.go                        # >= 1 (import added)
grep -c 'slices.Contains(cfg.Tools' config.go       # 2 (rules b and c)
grep -c 'len(cfg.Tools) == 0' config.go             # 1 (rule a)
grep -c 'slices.Contains(cfg.Tools, cfg.TargetTool)' config.go  # 1 (rule c)
# Expected: all counts hold. Confirms the 3 checks + import are present.
```

### Level 2: Behavior Validation (Component) — THROWAWAY, DO NOT COMMIT

```bash
cd /home/dustin/projects/web-search-prime-fixer

# The repo tests (resolve_test.go, config_test.go) reference the old .Aliases
# field and will NOT COMPILE after S1/S2. Rewriting them is P1.M1.T3.S1 (out of
# scope). So exercise the NEW rules with a throwaway test in the ISOLATED module,
# then delete it. (resolve_test.go is T3.S1's; do not add to it here.)
rm -rf /tmp/cfg-iso && mkdir -p /tmp/cfg-iso && cd /tmp/cfg-iso && \
  go mod init cfgiso >/dev/null 2>&1 && \
  cp /home/dustin/projects/web-search-prime-fixer/config.go . && \
  printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
cat > rules_test.go <<'EOF'
package main
import ("os";"testing")
func mustNoErr(t *testing.T){ t.Helper(); clearEnv(); if _,err:=ResolveConfig();err!=nil{t.Fatalf("default must validate: %v",err)} }
func mustErr(t *testing.T){ t.Helper(); clearEnv(); if _,err:=ResolveConfig();err==nil{t.Fatal("want error, got nil")} }
func clearEnv(){ for _,k:=range []string{"WSPF_CONFIG","WSPF_UPSTREAM","WSPF_LISTEN","WSPF_LOG_LEVEL"}{ os.Unsetenv(k) } }

// DEFAULT still validates (no behavior change for default users).
func TestRules_DefaultValid(t *testing.T){ mustNoErr(t) }

// (a) empty Tools fails. (b) missing CanonicalTool fails. (c) TargetTool in Tools fails.
func TestRules_EmptyTools(t *testing.T){
  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":[]}`)); mustErr(t)
}
func TestRules_MissingCanonical(t *testing.T){
  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":["search"],"canonical_tool":"web_search"}`)); mustErr(t)
}
func TestRules_CanonicalPresentOk(t *testing.T){
  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":["web_search","search"]}`)); mustNoErr(t) // canonical default present
}
func TestRules_LeakTargetTool(t *testing.T){
  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":["web_search","web_search_prime"]}`)); mustErr(t)
}
EOF
# helper file
cat > helpers_test.go <<'EOF'
package main
import ("os";"path/filepath";"testing")
func writeRaw(t *testing.T, body string) string {
  t.Helper()
  p := filepath.Join(t.TempDir(),"c.json")
  if err:=os.WriteFile(p,[]byte(body),0o644);err!=nil{t.Fatal(err)}
  return p
}
EOF
go test -run 'TestRules_' -v . && echo "OK: all 3 rules fire + default/canonical-ok pass" && \
  cd /home/dustin/projects/web-search-prime-fixer && rm -rf /tmp/cfg-iso
# Expected: all TestRules_* PASS. Confirms (a) empty->err, (b) missing canonical->err,
# canonical-present is OK, (c) leaking TargetTool->err, and the default validates.
# Then DELETE the temp module; do NOT commit rules_test.go/helpers_test.go.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# EXPECTED-FAILURE documentation (NOT a gate to make pass in S2):
go build ./... 2>&1 | sort -u
# Expected: errors reference EXACTLY the S1-era out-of-scope sites (all unchanged
# by S2 — S2 only added checks, it did not introduce new breakage):
#   ./main.go:160  cfg.Aliases undefined            (fixed in P1.M1.T2.S1)
#   ./proxy.go:498/499/... cfg.Aliases              (fixed in P1.M1.T2.S2 — proxy.go deleted)
#   config_test.go / resolve_test.go / health_test.go / proxy_test.go compile errors (.Aliases)
#                                                  (fixed in P1.M1.T3.S1 / T2.S2)
#
# IF `go build ./...` reports ANY error INSIDE config.go (syntax, unknown field,
# unused import, type mismatch) that IS an S2 defect — fix config.go.
# IF it reports ONLY main.go/proxy.go/test references to Aliases, that is EXPECTED
# and correct — do not edit those files.

# Real production behavior (ResolveConfig wired into main.go fail-fast) is verified
# end-to-end at the P1.M1 milestone / quality gate, once main.go is updated (T2.S1)
# and tests rewritten (T3.S1).
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Domain-specific: confirm the error MESSAGES are clear and quote the offender.
# (Run inside the /tmp/cfg-iso isolated module from L2; do not commit.)
cat > msg_test.go <<'EOF'
package main
import ("strings";"testing")
func TestRuleMessages(t *testing.T){
  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":[]}`))
  _,err:=ResolveConfig()
  if err==nil || !strings.Contains(err.Error(),"tools list must not be empty"){t.Fatalf("empty msg: %v",err)}

  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":["search"],"canonical_tool":"web_search"}`))
  _,err=ResolveConfig()
  if err==nil || !strings.Contains(err.Error(),`canonical tool "web_search"`){t.Fatalf("canonical msg: %v",err)}

  t.Setenv("WSPF_CONFIG", writeRaw(t, `{"tools":["web_search","web_search_prime"]}`))
  _,err=ResolveConfig()
  if err==nil || !strings.Contains(err.Error(),`target tool "web_search_prime"`){t.Fatalf("target msg: %v",err)}
}
EOF
go test -run TestRuleMessages -v . && echo "OK: error messages are clear and quote offenders" && \
  cd /home/dustin/projects/web-search-prime-fixer && rm -rf /tmp/cfg-iso
# Expected: PASS — each error string contains the offending tool name, so an
# operator reading the startup log knows exactly what to fix.
```

## Final Validation Checklist

### Technical Validation

- [ ] `gofmt -l config.go` prints nothing.
- [ ] Isolated temp-module `go vet . && go build .` on config.go is clean (L1).
- [ ] `"slices"` is the only new import in config.go; `go.mod` unchanged (no new require).
- [ ] Full-repo `go build ./...` fails ONLY on main.go:160 + proxy.go + test `.Aliases` references (the S1-era set) — i.e. no error originates inside config.go.

### Feature Validation

- [ ] `ResolveConfig()` validation order: Listen → Upstream → Tools-non-empty → Tools-contains-CanonicalTool → no-Tools-equals-TargetTool → (force TargetParam if empty).
- [ ] Rule (a) empty Tools → clear error; Rule (b) missing CanonicalTool → clear error; Rule (c) TargetTool in Tools → clear error (L2 + L4).
- [ ] Default config still validates to `nil` error (no behavior change for default users) (L2).
- [ ] force-TargetParam-if-empty block unchanged and still runs after all checks.
- [ ] Discovery / env-override / overlay logic untouched.

### Code Quality Validation

- [ ] Follows the v1 ResolveConfig pattern (codebase_patterns.md §1): validation is the step that grows; error style matches (fmt.Errorf, %q values, no %w on pure logical checks).
- [ ] Returns the partial `cfg` alongside each error (uniform with the rest of the function).
- [ ] Scope respected: ONLY config.go edited (ResolveConfig body + doc comment + import). No main.go/proxy.go/tests/go.mod/PRD/tasks.json changes.

### Documentation & Deployment

- [ ] `ResolveConfig` doc comment documents all six validation rules ([Mode A] deliverable) with PRD citations.
- [ ] Stale v1 wording fixed in that comment (`proxy`→`normalizing MCP server`, §14.3→§18.3, env-override field line updated to v2).
- [ ] No env vars or go.mod changes in S2.

---

## Anti-Patterns to Avoid

- ❌ Don't use `%w` on the three new rule errors — they are pure logical checks with no underlying error to chain; mirror the existing "upstream not absolute" branch, not the Listen/Upstream `%w` branches.
- ❌ Don't reorder or remove the force-TargetParam-if-empty block — the contract says to keep it; insert the 3 checks before it.
- ❌ Don't touch discovery (`resolveConfigPath`/`fileExists`), overlay (`LoadConfig`), env overrides (`WSPF_*`), the `Config` struct, or `DefaultConfig` — those are S2-invariant or S1's region.
- ❌ Don't "fix" the broken `go build ./...` by editing main.go / proxy.go / tests — those are T2.S1, T2.S2, T3.S1. Verify with the isolated gate instead.
- ❌ Don't add the `slices` membership logic as a hand-rolled loop AND `slices.Contains` — pick `slices.Contains` for both (b) and (c) and keep them symmetric; go 1.22 already provides it.
- ❌ Don't add a `go.mod` require for `slices` — it is stdlib since Go 1.21; go.mod is already `go 1.22`.
- ❌ Don't add rule-failure test cases to `resolve_test.go` — that is P1.M1.T3.S1. S2's validation is a throwaway isolated-module test that is deleted before finishing.
- ❌ Don't make the rules stricter than the contract (e.g. don't require `Tools[0]==CanonicalTool`, don't reject an empty `CanonicalTool` separately, don't dedupe Tools). The three rules are exactly: non-empty, contains CanonicalTool, not contains TargetTool.
- ❌ Don't change `ResolveConfig`'s signature — it still returns `(Config, error)`; only the body and doc comment change.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The change is a small, fully-specified insertion into one existing
function whose exact current body, exact insertion point (between the Upstream
check and the force-TargetParam block), exact three rules, and exact error
convention (plain `fmt.Errorf`, no `%w`, matching the "not absolute" branch) are
all quoted verbatim above. The only non-obvious risks are (a) mistakenly using
`%w` where there is no error to chain (pinned: don't), (b) reordering the
force-TargetParam block (pinned: keep it, insert before it), (c) over-constraining
the rules beyond the three named (pinned: exactly three), and (d) an agent that
over-eagerly "repairs" the still-broken `go build ./...` by editing out-of-scope
files (pinned: use the isolated gate, do not touch them). The `-1` reserves for
that last over-eager-repair risk. The default config trivially satisfies all three
rules, so there is no risk of accidentally breaking the no-config-file happy path.
