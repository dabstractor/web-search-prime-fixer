# PRP — P1.M1.T2.S2: Config discovery (file + env) + overrides + validation

## Goal

**Feature Goal**: Provide the **resolution layer** for `web-search-prime-fixer`
configuration — `func ResolveConfig() (Config, error)` — that turns the S1
primitive (`Config` + `DefaultConfig()` + `LoadConfig(path)`) into a
**fully-validated, env-aware, discoverable** config per PRD §14.3. It discovers
the config file (env override → CWD → XDG → defaults), loads it via S1's
`LoadConfig`, applies the three `WSPF_*` env overrides on top (highest
precedence), validates `Listen` (host:port) and `Upstream` (absolute URL), forces
`TargetParam` to `search_query` when empty, and returns one `Config` ready for
the bootstrap (P1.M1.T4.S1). This is the **S2** half of configuration;
discovery, env overrides, and validation are its entire job.

**Deliverable**: Two changes at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`:
1. **MODIFY** `config.go` — expand the import block to add `fmt`, `net`,
   `net/url`, `path/filepath`; **append** `func ResolveConfig() (Config, error)`,
   its unexported helper `resolveConfigPath() string`, and an unexported
   `fileExists(name string) bool`. Add the `[Mode A]` godoc comment to
   `ResolveConfig` documenting discovery precedence + the env vars. Leave S1's
   `type Config`, `DefaultConfig()`, `LoadConfig(path)` **byte-for-byte unchanged**.
2. **CREATE** `resolve_test.go` — table-driven tests (stdlib `testing` only) for
   `ResolveConfig` covering: defaults, `WSPF_CONFIG` explicit load + missing-file
   error, CWD discovery, XDG discovery, CWD-before-XDG precedence, the three env
   overrides, env-beats-file precedence, empty-env-ignored, the
   `WSPF_PATH`/`WSPF_ALIASES` scope guard, Listen/Upstream validation (good + bad),
   and `TargetParam` forcing.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. `resolve_test.go` passes with the cases above.
No logging code is added (the structured logger is P1.M1.T3); `main()` is NOT
touched (bootstrap is P1.M1.T4); no new env var beyond the contract's four
(`WSPF_CONFIG`, `WSPF_UPSTREAM`, `WSPF_LISTEN`, `WSPF_LOG_LEVEL`) is introduced.

## Why

- `ResolveConfig` is the single entry point the bootstrap (P1.M1.T4.S1) calls to
  obtain a usable `Config`. Everything downstream — the logger (P1.M1.T3, reads
  `LogLevel`), the server bind (P1.M1.T4, reads `Listen`), the proxy (P1.M4,
  reads `Upstream`), the rewrite rule (P1.M2.T1, reads `Aliases`/`TargetParam`) —
  depends on it returning a **validated** value. Centralizing discovery + override
  + validation here means those layers can assume a good `Config`.
- Implements **FR-5 "Configuration"** end-to-end: the proxy runs with no config
  file (defaults), with a project-local file (`./web-search-prime-fixer.json`),
  with a user-global file (`$XDG_CONFIG_HOME/...`), or fully from environment
  variables — the four PRD §14.3 sources, in that precedence order.
- Validation here ("clear error on a bad `Listen`/`Upstream`") is a **shift-left**
  gate: a misconfigured proxy fails fast at startup with a readable message
  instead of producing a confusing runtime connection error (PRD §14.3 §4, PRD §17).
- Establishes the project's **env-aware testing pattern** (`t.Setenv` + temp dirs)
  that the proxy/E2E suites (P1.M4/P1.M5) will reuse for deterministic, hermetic
  config injection.

## What

`config.go` gains `ResolveConfig()` which, in contract order: **(1)** picks a
path via `resolveConfigPath()` — `WSPF_CONFIG` (used verbatim if non-empty, even
if the file is missing → that surfaces as a load error, not a silent default
fallback), else the first **existing** of `web-search-prime-fixer.json` (CWD) or
`<os.UserConfigDir()>/web-search-prime-fixer/config.json`, else `""` (defaults);
if `os.UserConfigDir()` errors (no `$HOME`/`$XDG_CONFIG_HOME`) that candidate is
skipped, never fatal. **(2)** `LoadConfig(path)` (from S1). **(3)** env overrides
`WSPF_UPSTREAM` → `Upstream`, `WSPF_LISTEN` → `Listen`, `WSPF_LOG_LEVEL` →
`LogLevel`, each only when non-empty (so an empty env var never clobbers a file
or default value). **(4)** validate: `net.SplitHostPort(Listen)` must succeed and
`url.Parse(Upstream)` + `IsAbs()` must hold; on failure return a wrapped, clear
error. **(5)** if `TargetParam == ""`, force it to `"search_query"`.

`resolve_test.go` exercises every branch above with `t.Setenv` (for `WSPF_*` and
`XDG_CONFIG_HOME`) and `t.TempDir()` (for file fixtures), plus an isolated
`os.Chdir` (save/restore via `t.Cleanup`) for the CWD-discovery cases. No
`t.Parallel()` anywhere — env + cwd are process-global.

### Success Criteria

- [ ] `func ResolveConfig() (Config, error)` exists in `config.go`, `package main`.
- [ ] Discovery precedence is exactly: `WSPF_CONFIG` (verbatim if set) →
      `./web-search-prime-fixer.json` (first existing) →
      `<UserConfigDir>/web-search-prime-fixer/config.json` (first existing) → `""`.
- [ ] `WSPF_CONFIG` pointing at a **missing** file returns a non-nil error (no
      silent fallback to defaults).
- [ ] When `WSPF_CONFIG` is unset and no candidate file exists, `ResolveConfig`
      returns `DefaultConfig()` with a nil error.
- [ ] Env overrides: non-empty `WSPF_UPSTREAM`/`WSPF_LISTEN`/`WSPF_LOG_LEVEL` set
      the corresponding field and win over a file value; empty env vars are ignored.
- [ ] `WSPF_PATH` and `WSPF_ALIASES` have **no** effect (scope guard — not in the
      contract).
- [ ] Validation rejects an un-parseable `Listen` (e.g. `127.0.0.1`, `garbage`,
      empty) with a non-nil error; rejects a non-absolute `Upstream`
      (e.g. `api.z.ai/mcp`, `//host/path`) with a non-nil error; accepts valid
      `127.0.0.1:8787` and `https://example.com/mcp`.
- [ ] A config whose `TargetParam` is empty (e.g. `{"target_param":""}`) yields
      `TargetParam == "search_query"` after `ResolveConfig`.
- [ ] `os.UserConfigDir()` erroring (no `$HOME`/`$XDG_CONFIG_HOME`) does NOT crash
      `ResolveConfig` — it falls through to defaults (or the CWD candidate).
- [ ] `ResolveConfig` performs **no I/O output** (no `log`/`fmt.Println`); it only
      reads env + filesystem and returns `(Config, error)`.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.
- [ ] S1's `type Config` / `DefaultConfig()` / `LoadConfig(path)` are unchanged;
      `main.go`, `doc.go`, `go.mod`, `testdata/` are untouched.

## All Needed Context

### Context Completeness Check

_Pass._ The discovery ladder, the four env vars, the two validation checks, and
the `TargetParam` forcing are all specified verbatim in PRD §14.3 and the work
item's contract. The four stdlib primitives they rest on (`os.UserConfigDir`,
`os.Stat`, `net.SplitHostPort`, `url.Parse`+`IsAbs`) are **verified on-disk**
against the installed `go1.26.4` toolchain in
`research/verify-config-discovery.md` (exact error strings + edge-case verdicts).
The predecessor's on-disk `config.go` (the S1 INPUT) has been read and matches
the S1 contract byte-for-byte. The S1/S2 boundary is stated explicitly (what S2
adds vs. what S1 already owns). An agent with no prior knowledge of this codebase
can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative discovery + validation contract.
- file: PRD.md
  why: §14.3 "Discovery and precedence" (the 4-step ladder, the three env
        overrides, "Listen must parse / Upstream must be absolute URL", and
        "TargetParam forced to search_query if empty"); §14.2 "Defaults" (the
        fallback base); FR-5 "Configuration" (run-with-no-file requirement).
  critical: Precedence is env WSPF_CONFIG > ./web-search-prime-fixer.json >
        $XDG_CONFIG_HOME/web-search-prime-fixer/config.json > defaults, AND the
        three env overrides (WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL) are a
        SEPARATE, HIGHER-precedence layer applied AFTER file load. Do not conflate
        "WSPF_CONFIG picks the path" with "WSPF_UPSTREAM overrides a field".

# SCOPE BOUNDARY — exactly what S1 owns (the INPUT to S2); do NOT duplicate it.
- file: plan/001_c0abc3757e9a/P1M1T2S1/PRP.md
  why: S1 defines type Config (6 fields, snake_case JSON tags), DefaultConfig()
        (verbatim §14.2 defaults), and LoadConfig(path) (the merge primitive:
        cfg := DefaultConfig(); json.Unmarshal(data, &cfg); unknown fields
        ignored; missing-file-at-non-empty-path returns the os.ReadFile error).
  critical: S2 CONSUMES LoadConfig — do not reimplement file reading or the
        merge. Treat config.go's existing Config/DefaultConfig/LoadConfig as
        fixed (they are already on disk and match the contract). S2 only ADDS
        imports + ResolveConfig + 2 helpers, and creates resolve_test.go.

# PREDECESSOR'S PREDECESSOR — the module skeleton S1 compiled into.
- file: plan/001_c0abc3757e9a/P1M1T1S1/PRP.md
  why: go.mod (module web-search-prime-fixer, go 1.22, zero requires), doc.go,
        main.go (empty stub), testdata/. S2 compiles into the SAME package main.
  critical: Do NOT modify go.mod/doc.go/main.go/testdata. main() stays empty
        (P1.M1.T4 wires ResolveConfig into it) — do NOT call ResolveConfig from
        main here.

# VERIFIED GOTCHAS — the four stdlib primitives' actual behavior on this toolchain.
- file: plan/001_c0abc3757e9a/P1M1T2S2/research/verify-config-discovery.md
  why: On-disk proof (go1.26.4) of: os.UserConfigDir reads $XDG_CONFIG_HOME (so
        t.Setenv drives it in tests) and ERRORS when neither it nor $HOME is set
        (must skip the XDG candidate); net.SplitHostPort verdicts for default
        + bad + empty + multi-colon inputs; url.Parse+IsAbs verdicts (api.z.ai/mcp
        and //host/path rejected; http:// accepted-lenient); os.Stat err==nil ⇒
        exists.
  critical: os.UserConfigDir CAN error — guard it. IsAbs() (scheme present) is
        the real "absolute" check, not url.Parse success. :8787 (empty host)
        passes Listen validation — acceptable (loopback is the default, not an
        enforced constraint; PRD §13).

# ARCHITECTURE — stdlib-only invariant + greenfield file list.
- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: "No third-party dependencies are required or allowed (PRD §6, §9)."
        Confirms config.go is the config file (no internal/config package).
  critical: config.go uses ONLY stdlib. The new imports (fmt, net, net/url,
        path/filepath) are all stdlib — no `go get`. resolve_test.go uses ONLY
        os, path/filepath, reflect, testing.

# SECURITY — why logging the returned Config is safe.
- file: PRD.md
  why: §13 "Headers, credentials, security" (Authorization is forwarded as a
        header, NEVER read/logged/stored); §15 "Logging" (the `startup` event
        logs resolved config; "Never logs credentials").
  critical: Config has NO credential fields (only Upstream/Listen/Path/Aliases/
        TargetParam/LogLevel). So "no creds ever present" when the bootstrap
        logs the returned Config is STRUCTURALLY guaranteed — ResolveConfig need
        not redact anything. (And it does not log at all; see Integration Points.)

# Go stdlib refs — exact semantics relied upon (stable; verified on-disk above).
- url: https://pkg.go.dev/os#UserConfigDir
  why: "On Unix, returns $XDG_CONFIG_HOME or $HOME/.config." The portable XDG
        resolver. Returns error if neither is defined.
- url: https://pkg.go.dev/net#SplitHostPort
  why: Splits "host:port"; errors on missing port / too many colons. The Listen
        validator.
- url: https://pkg.go.dev/net/url#URL.IsAbs
  why: "reports whether the URL has a non-empty scheme." The Upstream "absolute"
        check (url.Parse rarely errors; IsAbs is the decisive test).
- url: https://pkg.go.dev/testing#T.Setenv
  why: "records the value ... restored after the test"; drives os.UserConfigDir
        (via XDG_CONFIG_HOME) and the WSPF_* vars deterministically. Cannot be
        used with t.Parallel (irrelevant — our tests are serial by design).
```

### Current Codebase tree (after P1.M1.T2.S1, the INPUT state of this subtask)

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22; NO requires (T1.S1)
  doc.go            # "Package main ..." comment (T1.S1)
  main.go           # package main + func main() {} STUB (T1.S1; filled in P1.M1.T4)
  config.go         # package main; imports (encoding/json, os);               [S1 — DO NOT REWRITE]
                    #   type Config (6 fields, snake_case JSON tags)
                    #   func DefaultConfig() Config   (verbatim §14.2)
                    #   func LoadConfig(path string) (Config, error)
  config_test.go    # TestDefaultConfig + TestLoadConfig_* (S1)                 [S1 — DO NOT EDIT]
  testdata/.gitkeep # placeholder (T1.S1; P1.M3.T2 adds *.sse)
  PRD.md            # unchanged
  plan/...          # unchanged (READ-ONLY planning scaffolding)
# NOTE: no resolve_test.go, no discovery/env/validation logic yet. This subtask
# adds ResolveConfig + helpers to config.go AND creates resolve_test.go.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod            # UNCHANGED
  doc.go            # UNCHANGED
  main.go           # UNCHANGED (still empty — do NOT wire ResolveConfig in)
  config.go         # MODIFIED — import block += fmt, net, net/url, path/filepath;
                    #            += func ResolveConfig() (Config, error)         [THIS SUBTASK]
                    #            += func resolveConfigPath() string  (unexported) [THIS]
                    #            += func fileExists(name string) bool (unexported) [THIS]
                    #            (Config / DefaultConfig / LoadConfig UNCHANGED)
  config_test.go    # UNCHANGED (S1's tests stay)
  resolve_test.go   # NEW — TestResolveConfig_* (discovery / overrides /        [THIS SUBTASK]
                    #        validation / TargetParam) using t.Setenv + t.TempDir
  testdata/.gitkeep # UNCHANGED
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: os.UserConfigDir() CAN RETURN AN ERROR (when neither $XDG_CONFIG_HOME
// nor $HOME is set — verified: returns "" + "neither $XDG_CONFIG_HOME nor $HOME
// are defined"). Guard it; on error, SKIP the XDG candidate and fall through to
// "". Do NOT crash ResolveConfig on a missing HOME.

// CRITICAL: WSPF_CONFIG is used VERBATIM if non-empty. Do NOT os.Stat it. If the
// named file is missing, LoadConfig returns os.ReadFile's error and ResolveConfig
// propagates it (explicit override → missing file = clear error, NOT silent
// defaults). Only the CWD and XDG candidates use "first existing" (os.Stat).
// Mixing these up breaks the precedence contract.

// CRITICAL: Env overrides are a SEPARATE layer from discovery. WSPF_CONFIG picks
// the FILE; WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL override FIELDS, applied
// AFTER LoadConfig. There is NO WSPF_PATH and NO WSPF_ALIASES (scope guard —
// PRD §14.3 lists exactly three override vars; do not invent more).

// CRITICAL: Apply env overrides only when NON-EMPTY. `if v := os.Getenv("WSPF_UPSTREAM");
// v != "" { cfg.Upstream = v }`. An empty env var must NOT clobber the file/default
// value (so `WSPF_UPSTREAM=` in the shell keeps the configured upstream).

// GOTCHA: url.Parse rarely errors (it is lenient). The real "absolute URL" check
// is u.IsAbs() (u.Scheme != ""). Missing-scheme inputs (api.z.ai/mcp, //host/path)
// parse fine but IsAbs()==false → reject. http:// (empty host) IsAbs()==true →
// accept (lenient; the contract only requires a scheme). Verified on-disk.

// GOTCHA: net.SplitHostPort(":8787") SUCCEEDS with host="" (binds all interfaces).
// The contract requires "parseable host:port", so this passes. Loopback is the
// DEFAULT (127.0.0.1:8787), not an enforced constraint (PRD §13). Don't add a
// host-presence check — it would reject a legitimate ":8787".

// GOTCHA: An empty Listen ("") fails net.SplitHostPort ("missing port in address").
// The defaults prevent this, but a file like {"listen":""} will now FAIL at
// validation — which is the intended "clear error" behavior (better than binding
// to a garbage address at runtime).

// SCOPE GUARD: ResolveConfig does NOT log. No logger exists yet (P1.M1.T3), and
// the contract's "Logs resolved config at startup (no creds ever present)"
// describes the bootstrap `startup` event (PRD §15) emitted in P1.M1.T4 from the
// Config this function RETURNS. Config has no credential fields (PRD §13), so
// logging it is inherently safe. Do NOT import "log" or call fmt.Fprintln in
// ResolveConfig — it is a pure env/filesystem function (the contract's MOCKING
// note: "pure function over os env/filesystem; test by setting env + temp dirs +
// t.Setenv").

// SCOPE GUARD: Do NOT validate LogLevel against {debug,info,warn,error}. PRD §14.3
// validation is ONLY Listen + Upstream. An unknown LogLevel is passed through;
// the logger (P1.M1.T3) decides how to handle it. Adding enum validation here
// would be out of scope and could reject configs the logger would tolerate.

// TEST GOTCHA: os.Chdir is PROCESS-GLOBAL. The CWD-discovery tests must (a) save
// os.Getwd(), (b) chdir into a t.TempDir(), (c) restore via t.Cleanup, and (d)
// NEVER call t.Parallel() (env + cwd are process-global). The XDG tests need NO
// chdir — t.Setenv("XDG_CONFIG_HOME", t.TempDir()) drives os.UserConfigDir.

// TEST GOTCHA: A "no file / defaults" test must isolate BOTH cwd (chdir to an
// EMPTY temp dir so no stray ./web-search-prime-fixer.json is found) AND XDG
// (point XDG_CONFIG_HOME at a SEPARATE empty temp dir) — otherwise a real user
// config on the dev machine (~/.config/web-search-prime-fixer/config.json)
// could leak into the result and make the test non-hermetic.

// CONVENTION: S2 MODIFIES config.go (adds imports + functions) and CREATES a
// SEPARATE resolve_test.go (package main) rather than editing S1's config_test.go.
// This avoids any edit conflict with S1's tests and keeps Config/LoadConfig tests
// (S1) separate from ResolveConfig tests (S2). Both files share the package-main
// test binary; unexported helpers in config.go are visible to resolve_test.go.
```

## Implementation Blueprint

### Data models and structure

No new data model. S2 **consumes** S1's `Config` (see
`plan/001_c0abc3757e9a/P1M1T2S1/PRP.md` and the on-disk `config.go`):
`Upstream`, `Listen`, `Path` (`string`), `Aliases` (`[]string`), `TargetParam`,
`LogLevel` (`string`), snake_case JSON tags `upstream`/`listen`/`path`/`aliases`/
`target_param`/`log_level`. `DefaultConfig()` and `LoadConfig(path)` are the only
existing symbols S2 calls.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY PREDECESSORS (read-only, no edits)
  - RUN: `cd /home/dustin/projects/web-search-prime-fixer && ls go.mod doc.go main.go config.go config_test.go`
  - EXPECT: all five exist. go.mod contains `module web-search-prime-fixer` +
    `go 1.22` with NO require block.
  - RUN: `grep -c 'func DefaultConfig\|func LoadConfig\|type Config' config.go`
    → expect 3 (S1 landed). `go build ./...` MUST already exit 0.
  - WHY: S2's INPUT is "Config, DefaultConfig(), LoadConfig(path) from P1.M1.T2.S1".
    If config.go is absent or doesn't build, STOP — S1 has not landed (parallel
    execution). Do NOT recreate S1's symbols here.

Task 1: MODIFY config.go — expand the import block
  - FILE: /home/dustin/projects/web-search-prime-fixer/config.go
  - FIND the existing import block (S1):
        import (
            "encoding/json"
            "os"
        )
  - REPLACE with (stdlib only; alphabetical, gofmt-stable):
        import (
            "encoding/json"
            "fmt"
            "net"
            "net/url"
            "os"
            "path/filepath"
        )
  - WHY: ResolveConfig needs fmt (errors), net (SplitHostPort), net/url (Parse +
    IsAbs), path/filepath (Join for the XDG path). encoding/json + os are already
    imported (used by LoadConfig). Do NOT add "log".

Task 2: MODIFY config.go — append resolveConfigPath + fileExists + ResolveConfig
  - FILE: same config.go (append AFTER LoadConfig's closing brace)
  - APPEND the three functions EXACTLY as in "Implementation Patterns & Key
    Details" below (verbatim — they are small and fully specified).
  - GOTCHA: resolveConfigPath and fileExists are UNEXPORTED (lowercase) — they are
    implementation details of ResolveConfig, not part of the public surface. Only
    ResolveConfig is exported (consumed by P1.M1.T4).
  - GOTCHA: the CWD candidate is the bare string "web-search-prime-fixer.json"
    (Go resolves a bare name against the process CWD; this is the
    "./web-search-prime-fixer.json" of PRD §14.3). Do NOT prepend "./" — it is
    equivalent and the bare form is gofmt-clean.
  - GOTCHA: do NOT os.Stat the WSPF_CONFIG value. Use it verbatim. Stat ONLY the
    CWD and XDG candidates.

Task 3: CREATE resolve_test.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/resolve_test.go
  - PACKAGE: `package main`
  - IMPORTS: `"os"`, `"path/filepath"`, `"reflect"`, `"testing"`  (stdlib only)
  - IMPLEMENT the tests in "Validation Loop → Level 2" (each spelled out with
    setup + assertion). Use a shared helper `isolateConfigEnv(t)` (see "Test
    helper" below) for every test so NO real user config / no stray CWD file
    leaks in and tests are deterministic.
  - NAMING: TestResolveConfig_<Scenario>; table-driven subtests via t.Run where
    natural (overrides, validation).
  - GOTCHA: NO t.Parallel() anywhere (env + os.Chdir are process-global).
  - WHY: Covers discovery precedence (4 branches), env overrides (3 vars +
    empty + beats-file), the WSPF_PATH/ALIASES scope guard, validation (good +
    bad for both fields), and TargetParam forcing.

Task 4: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w config.go resolve_test.go
        go build ./...
        go vet ./...
        gofmt -l .
        go test ./...
        go test -run 'TestResolveConfig' -v ./...
  - EXPECT: gofmt silent; build/vet clean; all tests PASS including every
    TestResolveConfig_* subtest. Re-run after any edit until all clean.
```

### Implementation Patterns & Key Details

```go
// Append these THREE functions to config.go (after LoadConfig). Verbatim.

// resolveConfigPath returns the config file path to load, per PRD §14.3 discovery
// precedence (first match wins):
//  1. WSPF_CONFIG: if set (non-empty), used VERBATIM — even if the file is
//     missing, which surfaces as a load error from LoadConfig rather than a
//     silent fallback to defaults.
//  2. Otherwise the first EXISTING of:
//       ./web-search-prime-fixer.json                         (process CWD)
//       $XDG_CONFIG_HOME/web-search-prime-fixer/config.json   (portable: os.UserConfigDir)
//  3. If none of the above is usable, "" is returned (no file → built-in defaults).
//
// os.UserConfigDir resolves $XDG_CONFIG_HOME or ~/.config portably and is
// consulted at runtime; if it errors (no $HOME/$XDG_CONFIG_HOME) the XDG
// candidate is skipped, never fatal.
func resolveConfigPath() string {
	if p := os.Getenv("WSPF_CONFIG"); p != "" {
		return p // explicit override: used verbatim (missing file → load error)
	}
	const cwdCandidate = "web-search-prime-fixer.json" // "./web-search-prime-fixer.json" (PRD §14.3); bare name resolves against CWD
	if fileExists(cwdCandidate) {
		return cwdCandidate
	}
	if dir, err := os.UserConfigDir(); err == nil {
		xdgCandidate := filepath.Join(dir, "web-search-prime-fixer", "config.json")
		if fileExists(xdgCandidate) {
			return xdgCandidate
		}
	}
	return "" // no file found → DefaultConfig() base
}

// fileExists reports whether name exists and is stat-able. Used for the "first
// existing" CWD/XDG search only (NOT for WSPF_CONFIG, which is verbatim).
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// ResolveConfig discovers, loads, env-overrides, and validates the proxy
// configuration, returning a fully-validated Config ready for startup (PRD §14.3).
//
// # Discovery precedence (resolveConfigPath)
//
//  1. WSPF_CONFIG: if set (non-empty), that path is loaded VERBATIM. A missing
//     file here is a hard error (the caller asked for a specific file), not a
//     silent fallback to defaults.
//  2. Otherwise the first EXISTING of:
//       ./web-search-prime-fixer.json
//       $XDG_CONFIG_HOME/web-search-prime-fixer/config.json
//     (resolved portably via os.UserConfigDir; defaults to
//     ~/.config/web-search-prime-fixer/config.json on Linux/macOS).
//  3. If none exist, no file is loaded and the built-in defaults (DefaultConfig)
//     form the base.
//
// # Environment overrides (applied AFTER the file load — highest precedence)
//
//	WSPF_UPSTREAM   -> Config.Upstream
//	WSPF_LISTEN     -> Config.Listen
//	WSPF_LOG_LEVEL  -> Config.LogLevel
//
// An empty env var is ignored (the file/default value is kept). Path and Aliases
// have NO env override.
//
// # Validation (returns a clear, wrapped error on failure)
//
//   - Listen must be a parseable host:port (net.SplitHostPort).
//   - Upstream must be an absolute URL (url.Parse succeeds and URL.IsAbs reports
//     a non-empty scheme).
//
// After validation, if TargetParam is empty it is forced to "search_query".
//
// ResolveConfig performs no I/O output. The returned Config contains no
// credential fields (Authorization is forwarded as a request header, never part
// of Config — see PRD §13), so the bootstrap logging it at startup (PRD §15)
// never exposes secrets.
func ResolveConfig() (Config, error) {
	path := resolveConfigPath()
	cfg, err := LoadConfig(path) // S1 primitive: DefaultConfig base, file overlay, unknown fields ignored
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

	// Force TargetParam if empty (PRD §14.3).
	if cfg.TargetParam == "" {
		cfg.TargetParam = "search_query"
	}

	return cfg, nil
}
```

```go
// resolve_test.go — shared test helper (put at the top of resolve_test.go).

// isolateConfigEnv makes a ResolveConfig test hermetic:
//   - clears the four WSPF_* env vars (so a dev shell can't leak in),
//   - points XDG_CONFIG_HOME at an EMPTY temp dir (so no real user config is
//     discovered via os.UserConfigDir),
//   - chdir's into a fresh temp dir (so no stray ./web-search-prime-fixer.json
//     in the repo root is discovered),
//   - restores cwd via t.Cleanup.
// Returns the CWD temp dir so a test can opt INTO cwd discovery by writing
// ./web-search-prime-fixer.json into it.
//
// MUST NOT be used with t.Parallel (env + chdir are process-global).
func isolateConfigEnv(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"WSPF_CONFIG", "WSPF_UPSTREAM", "WSPF_LISTEN", "WSPF_LOG_LEVEL"} {
		t.Setenv(k, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty → XDG candidate never found unless a test writes into it
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return dir
}

// writeConfig writes a JSON config file into a fresh t.TempDir() and returns
// its absolute path (for WSPF_CONFIG tests).
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"           # config.go + resolve_test.go are package main, repo root

SYMBOLS INTRODUCED (consumed by later subtasks):
  - func ResolveConfig() (Config, error)   # -> P1.M1.T4.S1 bootstrap calls this ONCE
        at startup; uses the returned (validated) Config to build the logger
        (LogLevel), bind the server (Listen), and dial the upstream (Upstream).
  - func resolveConfigPath() string         # UNEXPORTED — implementation detail
  - func fileExists(name string) bool       # UNEXPORTED — implementation detail

ENVIRONMENT VARIABLES (the ONLY four S2 introduces; PRD §14.3):
  - WSPF_CONFIG    # path override (discovery); used verbatim if non-empty
  - WSPF_UPSTREAM  # field override (after file load)
  - WSPF_LISTEN    # field override (after file load)
  - WSPF_LOG_LEVEL # field override (after file load)
  # NOTE for P1.M5.T3.S1 (README sweep): these four WSPF_* vars are the complete
  # user-facing config env surface and should be documented there. S2 only adds
  # the doc comment on ResolveConfig (contract "[Mode A]").

NO INTEGRATION POINTS IN main() THIS SUBTASK:
  - Do NOT call ResolveConfig from main(). Do NOT add flag parsing. The bootstrap
    (ResolveConfig -> build logger -> server -> graceful shutdown) is P1.M1.T4.
    main() stays the empty T1.S1 stub.

NO LOGGING THIS SUBTASK:
  - ResolveConfig returns (Config, error) and performs no output. The `startup`
    log event (PRD §15) that "logs resolved config" is emitted by the bootstrap
    in P1.M1.T4 using the logger from P1.M1.T3. Config has no credential fields
    (PRD §13), so that future log is inherently safe — nothing for S2 to redact.

NO VALIDATION OF LogLevel / Path / Aliases THIS SUBTASK:
  - PRD §14.3 validation is Listen + Upstream only. TargetParam is forced (not
    validated). LogLevel enum, Path presence, and Aliases emptiness are NOT
    checked here (out of scope; downstream layers tolerate them).
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -w config.go resolve_test.go          # format in place
gofmt -l .                                  # MUST print nothing
go vet ./...                                # MUST exit 0, no output
go build ./...                              # MUST exit 0, no output

# Dependency-free invariant (PRD §6/§9) — grep for stray imports:
grep -E '^\s*"github\.com|^\s*"gopkg\.in|^\s*"golang\.org/x' config.go resolve_test.go || true
# Expected: empty. config.go may import only: encoding/json, fmt, net, net/url,
# os, path/filepath. resolve_test.go: os, path/filepath, reflect, testing.

# Expected: gofmt silent; vet/build clean; no third-party imports.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...                               # MUST pass (incl. S1's config_test.go)
go test -run 'TestResolveConfig' -v ./...   # see the S2 subtests
```

Required `resolve_test.go` test cases (every test calls `isolateConfigEnv(t)`
first unless noted; none use `t.Parallel`):

```go
// (a) Defaults — no file, no env.
func TestResolveConfig_Defaults(t *testing.T) {
	isolateConfigEnv(t)            // empty cwd + empty XDG + cleared WSPF_*
	cfg, err := ResolveConfig()
	if err != nil { t.Fatalf("err: %v", err) }
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("got %+v\nwant %+v", cfg, DefaultConfig())
	}
}

// (b) WSPF_CONFIG explicit load + missing-file error (table).
func TestResolveConfig_WSPF_CONFIG(t *testing.T) {
	t.Run("explicit_path_loads", func(t *testing.T) {
		isolateConfigEnv(t)
		p := writeConfig(t, `{"listen":"0.0.0.0:9999","log_level":"warn"}`)
		t.Setenv("WSPF_CONFIG", p)
		cfg, err := ResolveConfig()
		if err != nil { t.Fatalf("err: %v", err) }
		want := DefaultConfig(); want.Listen = "0.0.0.0:9999"; want.LogLevel = "warn"
		if !reflect.DeepEqual(cfg, want) { t.Errorf("got %+v\nwant %+v", cfg, want) }
	})
	t.Run("missing_explicit_path_errors", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_CONFIG", filepath.Join(t.TempDir(), "nope.json"))
		if _, err := ResolveConfig(); err == nil {
			t.Fatal("want error for missing WSPF_CONFIG file; got nil")
		}
	})
	t.Run("env_beats_file", func(t *testing.T) {
		isolateConfigEnv(t)
		p := writeConfig(t, `{"upstream":"http://from-file.invalid/mcp"}`)
		t.Setenv("WSPF_CONFIG", p)
		t.Setenv("WSPF_UPSTREAM", "https://from-env.example.com/mcp")
		cfg, err := ResolveConfig()
		if err != nil { t.Fatalf("err: %v", err) }
		if cfg.Upstream != "https://from-env.example.com/mcp" {
			t.Errorf("Upstream=%q want env value", cfg.Upstream)
		}
	})
}

// (c) CWD discovery.
func TestResolveConfig_CwdDiscovery(t *testing.T) {
	dir := isolateConfigEnv(t)               // dir is the current CWD now
	if err := os.WriteFile(filepath.Join(dir, "web-search-prime-fixer.json"),
		[]byte(`{"upstream":"https://cwd.example.com/mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveConfig()
	if err != nil { t.Fatalf("err: %v", err) }
	if cfg.Upstream != "https://cwd.example.com/mcp" {
		t.Errorf("Upstream=%q want cwd file value", cfg.Upstream)
	}
}

// (d) XDG discovery.
func TestResolveConfig_XDGDiscovery(t *testing.T) {
	isolateConfigEnv(t)
	// isolateConfigEnv already pointed XDG_CONFIG_HOME at a temp dir; write into it.
	xdg := os.Getenv("XDG_CONFIG_HOME")
	dir := filepath.Join(xdg, "web-search-prime-fixer")
	if err := os.MkdirAll(dir, 0o755); err != nil { t.Fatal(err) }
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"upstream":"https://xdg.example.com/mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveConfig()
	if err != nil { t.Fatalf("err: %v", err) }
	if cfg.Upstream != "https://xdg.example.com/mcp" {
		t.Errorf("Upstream=%q want xdg file value", cfg.Upstream)
	}
}

// (e) Precedence: CWD before XDG.
func TestResolveConfig_Precedence_CwdBeforeXDG(t *testing.T) {
	cwd := isolateConfigEnv(t)
	// XDG file
	xdg := os.Getenv("XDG_CONFIG_HOME")
	xdgDir := filepath.Join(xdg, "web-search-prime-fixer")
	os.MkdirAll(xdgDir, 0o755)
	os.WriteFile(filepath.Join(xdgDir, "config.json"),
		[]byte(`{"upstream":"https://xdg.example.com/mcp"}`), 0o644)
	// CWD file (should win)
	os.WriteFile(filepath.Join(cwd, "web-search-prime-fixer.json"),
		[]byte(`{"upstream":"https://cwd.example.com/mcp"}`), 0o644)
	cfg, err := ResolveConfig()
	if err != nil { t.Fatalf("err: %v", err) }
	if cfg.Upstream != "https://cwd.example.com/mcp" {
		t.Errorf("Upstream=%q want cwd (precedence)", cfg.Upstream)
	}
}

// (f) Env overrides (table) + empty-ignored + scope guard.
func TestResolveConfig_EnvOverrides(t *testing.T) {
	cases := []struct {
		name, key, val, field string
		want                  string
	}{
		{"upstream", "WSPF_UPSTREAM", "https://env.example.com/mcp", "Upstream", "https://env.example.com/mcp"},
		{"listen", "WSPF_LISTEN", "0.0.0.0:7000", "Listen", "0.0.0.0:7000"},
		{"log_level", "WSPF_LOG_LEVEL", "debug", "LogLevel", "debug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv(c.key, c.val)
			cfg, err := ResolveConfig()
			if err != nil { t.Fatalf("err: %v", err) }
			got := fieldByJSONName(&cfg, c.field) // tiny helper, see below
			if got != c.want { t.Errorf("%s=%q want %q", c.field, got, c.want) }
		})
	}
	t.Run("empty_env_ignored", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_LOG_LEVEL", "") // empty must NOT override the default "info"
		cfg, err := ResolveConfig()
		if err != nil { t.Fatalf("err: %v", err) }
		if cfg.LogLevel != "info" { t.Errorf("LogLevel=%q want default info", cfg.LogLevel) }
	})
	t.Run("path_and_aliases_not_overridable", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_PATH", "/should/be/ignored")
		t.Setenv("WSPF_ALIASES", "ignored") // no such override; DefaultConfig.Aliases kept
		cfg, err := ResolveConfig()
		if err != nil { t.Fatalf("err: %v", err) }
		if cfg.Path != "/mcp" { t.Errorf("Path=%q want /mcp (no WSPF_PATH override)", cfg.Path) }
		if !reflect.DeepEqual(cfg.Aliases, DefaultConfig().Aliases) {
			t.Errorf("Aliases changed by non-existent WSPF_ALIASES: %+v", cfg.Aliases)
		}
	})
}
// fieldByJSONName returns cfg.Upstream/Listen/LogLevel by the logical field name.
func fieldByJSONName(cfg *Config, field string) string {
	switch field {
	case "Upstream": return cfg.Upstream
	case "Listen":   return cfg.Listen
	case "LogLevel": return cfg.LogLevel
	}
	return ""
}

// (g) Validation (table: good + bad for Listen and Upstream).
func TestResolveConfig_Validation(t *testing.T) {
	cases := []struct {
		name, key, val string
		wantErr        bool
	}{
		{"good_listen", "WSPF_LISTEN", "0.0.0.0:9000", false},
		{"default_listen_ok", "WSPF_LISTEN", "127.0.0.1:8787", false},
		{"bad_listen_no_port", "WSPF_LISTEN", "127.0.0.1", true},
		{"bad_listen_garbage", "WSPF_LISTEN", "not an addr", true},
		{"bad_listen_empty", "WSPF_LISTEN", "", false}, // empty → ignored → default (valid) — NOT an error
		{"good_upstream_https", "WSPF_UPSTREAM", "https://example.com/mcp", false},
		{"bad_upstream_no_scheme", "WSPF_UPSTREAM", "api.z.ai/mcp", true},
		{"bad_upstream_scheme_relative", "WSPF_UPSTREAM", "//host/path", true},
		{"bad_upstream_empty", "WSPF_UPSTREAM", "", false}, // empty → ignored → default (valid)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv(c.key, c.val)
			_, err := ResolveConfig()
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}
// NOTE: "bad_listen_empty"/"bad_upstream_empty" are NON-errors because an empty
// env var is ignored (the field keeps its default, which is valid). They are in
// the table to pin that "empty = ignored, not = invalid" semantics.

// (h) TargetParam forcing.
func TestResolveConfig_TargetParamForced(t *testing.T) {
	isolateConfigEnv(t)
	p := writeConfig(t, `{"target_param":""}`) // file forces TargetParam empty
	t.Setenv("WSPF_CONFIG", p)
	cfg, err := ResolveConfig()
	if err != nil { t.Fatalf("err: %v", err) }
	if cfg.TargetParam != "search_query" {
		t.Errorf("TargetParam=%q want forced \"search_query\"", cfg.TargetParam)
	}
}
```

```bash
# Expected: every subtest PASS. If a validation case fails, read the wrapped error
# (e.g. `invalid listen address "127.0.0.1": address 127.0.0.1: missing port in
# address`) — a mismatch usually means a wrong env var name or a missing
# non-empty guard. If a discovery case fails, check that isolateConfigEnv's
# XDG_CONFIG_HOME/CWD isolation is actually in effect.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Smoke: the module still builds a binary with the modified config.go.
go build -o /tmp/wspf-resolve-smoke . && /tmp/wspf-resolve-smoke; echo "exit=$?"
rm -f /tmp/wspf-resolve-smoke
# Expected: builds; runs and exits 0 with no output (main() is still the empty
# T1.S1 stub — ResolveConfig is NOT wired into main in this subtask).

# Hermeticity check: ResolveConfig with a fully isolated env yields DefaultConfig
# with NO dependency on the dev machine's ~/.config or repo contents. (This is
# what TestResolveConfig_Defaults asserts; the command below is a manual echo.)
XDG_CONFIG_HOME=/tmp/empty-$$ WSPF_CONFIG= WSPF_UPSTREAM= WSPF_LISTEN= WSPF_LOG_LEVEL= \
  bash -c 'cd "$(mktemp -d)" && echo "isolation dir: $PWD"'
rm -rf /tmp/empty-$$
# (Authoritative integration check is `go test ./...`; ResolveConfig's real
#  caller — the bootstrap — arrives in P1.M1.T4.S1.)
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# godoc check — the "[Mode A] doc comment" deliverable on ResolveConfig:
go doc . ResolveConfig
# Expected: prints ResolveConfig's godoc (discovery precedence, env overrides,
# validation, TargetParam forcing, no-creds note).

# Precedence surface check: confirm ONLY the four contract env vars are consulted.
grep -oE 'WSPF_[A-Z_]+' config.go | sort -u
# Expected exactly:
#   WSPF_CONFIG
#   WSPF_LISTEN
#   WSPF_LOG_LEVEL
#   WSPF_UPSTREAM
# If WSPF_PATH / WSPF_ALIASES appear, a scope violation leaked in.

# No-logging guard: ResolveConfig must not emit anything.
! grep -nE '\blog\.|\bfmt\.F?(Print|Sprint)|os\.Stderr' config.go
# Expected: the negated grep succeeds (exit 0) — ResolveConfig is pure.

# No-mutation guard: S1 symbols untouched (must still be present, unchanged).
grep -c 'func DefaultConfig\|func LoadConfig\|type Config ' config.go   # expect 3
# And go test still passes S1's suite:
go test -run 'TestDefaultConfig|TestLoadConfig' ./...                   # expect PASS

# Validation primitives actually wired (not stubbed):
grep -n 'net.SplitHostPort\|url.Parse\|\.IsAbs()' config.go
# Expected: one SplitHostPort call and one url.Parse + IsAbs call in ResolveConfig.
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0, no output.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` passes (exit 0), including all `resolve_test.go` subtests.
- [ ] `config.go` imports only `encoding/json`, `fmt`, `net`, `net/url`, `os`, `path/filepath`.
- [ ] `resolve_test.go` imports only `os`, `path/filepath`, `reflect`, `testing`.

### Feature Validation

- [ ] `ResolveConfig()` exists and is exported in `config.go` (`package main`).
- [ ] Discovery: `WSPF_CONFIG` (verbatim) → CWD file → XDG file → `""` (defaults).
- [ ] `WSPF_CONFIG` missing-file → non-nil error; unset-and-no-file → `DefaultConfig()`, nil error.
- [ ] Env overrides `WSPF_UPSTREAM`/`WSPF_LISTEN`/`WSPF_LOG_LEVEL` apply when non-empty and beat file values.
- [ ] Empty env vars are ignored (default/file value kept).
- [ ] `WSPF_PATH`/`WSPF_ALIASES` have no effect (scope guard).
- [ ] `Listen` validation: rejects bad/empty-after-file, accepts `127.0.0.1:8787`/`0.0.0.0:9000`.
- [ ] `Upstream` validation: rejects non-absolute (`api.z.ai/mcp`, `//host/path`), accepts `https://…`.
- [ ] Empty `TargetParam` is forced to `search_query`.
- [ ] `os.UserConfigDir()` error does not crash `ResolveConfig` (falls through).
- [ ] `go doc . ResolveConfig` shows the precedence + env + validation doc comment (Mode A).

### Code Quality Validation

- [ ] `ResolveConfig` performs no I/O output (no `log`/`fmt.Print`); returns `(Config, error)` only.
- [ ] `resolveConfigPath` / `fileExists` are unexported (implementation details).
- [ ] S1's `Config` / `DefaultConfig` / `LoadConfig` are byte-for-byte unchanged (S1's `config_test.go` still passes).
- [ ] `main.go` / `doc.go` / `go.mod` / `testdata/` untouched; `main()` still empty.
- [ ] Tests are hermetic (`isolateConfigEnv` clears WSPF_*, isolates XDG + CWD) and serial (no `t.Parallel`).
- [ ] No third-party dependencies added (stdlib only).

### Documentation & Deployment

- [ ] `ResolveConfig` godoc documents discovery precedence, the four env vars, validation, and the no-creds guarantee (contract "[Mode A]").
- [ ] No README change here (the final README env-var sweep is P1.M5.T3.S1 — explicitly deferred).
- [ ] No `config.example.json` created here (P1.M5.T3.S2 — explicitly deferred).

---

## Anti-Patterns to Avoid

- ❌ Don't `os.Stat` the `WSPF_CONFIG` value — it is used VERBATIM. Only the CWD and XDG candidates use "first existing". Stat-ing WSPF_CONFIG would turn a user's explicit "load THIS file" into a silent default fallback, violating the precedence contract.
- ❌ Don't hand-read `XDG_CONFIG_HOME` — use `os.UserConfigDir()` (the contract's research note). It resolves `$XDG_CONFIG_HOME` or `~/.config` portably AND signals (via error) when neither is set.
- ❌ Don't crash on `os.UserConfigDir()` error — guard it and skip the XDG candidate (verified: it errors when `$HOME`/`$XDG_CONFIG_HOME` are unset).
- ❌ Don't apply env overrides when the value is empty — an empty `WSPF_UPSTREAM=` must keep the file/default value, not blank it.
- ❌ Don't invent `WSPF_PATH` or `WSPF_ALIASES` — PRD §14.3 lists exactly three override vars (`WSPF_UPSTREAM`/`WSPF_LISTEN`/`WSPF_LOG_LEVEL`). Adding more is a scope violation.
- ❌ Don't use `url.Parse` success as the "absolute" check — `IsAbs()` (non-empty scheme) is decisive. `url.Parse` accepts `api.z.ai/mcp` (then `IsAbs()==false` → reject) and even `http://` (then `IsAbs()==true` → accept, lenient).
- ❌ Don't add a host-presence check to Listen validation — `:8787` legitimately passes `net.SplitHostPort`; loopback is the DEFAULT, not enforced (PRD §13).
- ❌ Don't validate `LogLevel` against `{debug,info,warn,error}` — PRD §14.3 validation is Listen + Upstream only. The logger (P1.M1.T3) handles unknown levels.
- ❌ Don't log from `ResolveConfig` — no logger exists yet (P1.M1.T3), and the contract's "logs resolved config at startup" is the bootstrap's `startup` event (P1.M1.T4) over the returned Config. `Config` has no credential fields, so that future log is inherently safe.
- ❌ Don't wire `ResolveConfig` into `main()` or add flag parsing — bootstrap is P1.M1.T4. `main()` stays the empty T1.S1 stub.
- ❌ Don't reimplement `LoadConfig` / merge logic / `Config` / `DefaultConfig` — those are S1's, already on disk. S2 only ADDS to `config.go`.
- ❌ Don't edit `config_test.go` — create a separate `resolve_test.go` (avoids any conflict with S1's tests; both share package main).
- ❌ Don't use `t.Parallel()` in `resolve_test.go` — `t.Setenv` forbids it and `os.Chdir` is process-global. Keep every test serial; isolate env + cwd via `isolateConfigEnv`.
- ❌ Don't let a "defaults" test run in the real CWD or with the real `XDG_CONFIG_HOME` — isolate BOTH (empty temp CWD + separate empty temp XDG dir) or a stray `~/.config/web-search-prime-fixer/config.json` will make the test non-hermetic.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is small and fully specified — `ResolveConfig`,
`resolveConfigPath`, and `fileExists` are given verbatim above, and every test
case is spelled out with setup + expected value. The four stdlib primitives it
depends on (`os.UserConfigDir`, `os.Stat`, `net.SplitHostPort`, `url.Parse`+
`IsAbs`) are **verified on-disk** against the installed `go1.26.4` toolchain in
`research/verify-config-discovery.md`, including exact error strings and edge-case
verdicts (`:8787` passes; `api.z.ai/mcp` rejected; `os.UserConfigDir` errors when
HOME/XDG unset). The predecessor's `config.go` (the INPUT) has been read and
matches the S1 contract byte-for-byte, so the MODIFY instructions target real,
stable anchor text. The residual risk is an agent (a) `os.Stat`-ing `WSPF_CONFIG`
(turning an explicit override into a silent default fallback) — mitigated by the
explicit "don't stat WSPF_CONFIG" gotcha, the `resolveConfigPath` verbatim code,
and the `missing_explicit_path_errors` test; (b) using `t.Parallel` with
`t.Setenv`/`os.Chdir` — mitigated by the `isolateConfigEnv` helper, the no-Parallel
rule, and the Anti-Patterns list; or (c) editing S1's `config_test.go` instead of
creating `resolve_test.go` — mitigated by the separate-file instruction and the
Level-4 `grep -c 'func DefaultConfig…'` guard. Hence 9, not 10.
