name: "P1.M5.T2.S1 — Final quality gate: go vet ./... + go test ./... + build (default + -ldflags version injection) + stdlib-only import audit"
description: |

  OWN and VERIFY the final quality gate for the whole web-search-prime-fixer
  module (PRD §18 build/test commands, §16 version injection via -ldflags, §20
  success criteria, §15 logging). This is a GATE task (item contract = Mode A:
  verification step, no docs). The deliverable is NOT new code — it is a clean,
  building, fully-tested, **stdlib-only** binary plus a verification record.

  INPUT: all completed production source + tests from P1.M1–P1.M5.T1, INCLUDING
  the parallel `proxy_e2e_test.go` (P1.M5.T1.S2) which has landed on disk.

  WHAT THE GATE RUNS (the item contract, verbatim):
    1. `go vet ./...`           — MUST be clean (exit 0, no output).
    2. `go test ./...`          — MUST be all green (exit 0; 71 funcs / 10 files
                                  incl. the 5 TestE2E_* §19.3 cases).
    3. `go build -o web-search-prime-fixer .`   — binary produced (PRD §18).
    4. `go build -ldflags "-X main.version=0.1.0"` — version injection works;
                                  /healthz reflects `version:"0.1.0"` at runtime.
    5. IMPORT AUDIT: confirm ONLY stdlib is imported (zero `require`s in go.mod,
                                  no third-party path in `go list -deps .`).

  FIX CLAUSE: if any gate surfaces a lint/vet/test/format/build defect or an
  import leak, FIX it (that is the "Fix any lint/test failures discovered" part
  of the contract) — within scope (see Scope Boundary). If all gates are already
  green, the task is: run them, record the output, ship the binary.

  SCOPE BOUNDARY: this item fixes defects (format/vet/test/import) ONLY. It does
  NOT change product behavior, add features, write README/docs (P1.M5.T3 owns
  those), or add a `require` to `go.mod` (PRD §6/§9 forbid external deps). It does
  NOT modify `PRD.md`, any `tasks.json`, or any `prd_snapshot.md`. If a failure
  reveals a genuine design defect beyond a localized fix, STOP and report rather
  than expand scope.

  BASELINE (research time, with proxy_e2e_test.go already on disk): ALL FIVE GATES
  GREEN. `go vet ./...` exit 0; `go test ./...` `ok 0.070s`; `gofmt -l .` empty;
  both builds exit 0; ldflags build → `/healthz` `{"ok":true,"version":"0.1.0"}`;
  `go list -m all` prints only `web-search-prime-fixer`. See research/baseline-and-gates.md.

---

## Goal

**Feature Goal**: Prove, with executable commands, that the entire module is
  `go vet`-clean, fully tested (`go test ./...` green), builds to a runnable binary
  (`go build -o web-search-prime-fixer .`), injects the version at link time
  (`-ldflags "-X main.version=0.1.0"`) such that `/healthz` returns that version,
  and imports ONLY the Go standard library (zero `require`s, no external paths).

**Deliverable**:
  1. The binary `./web-search-prime-fixer` (built; gitignored by `.gitignore`).
  2. A second build with `-ldflags "-X main.version=0.1.0"` whose `/healthz`
     returns `{"ok":true,"version":"0.1.0"}` (runtime proof of PRD §16).
  3. A verification record (the captured stdout/stderr of every gate command)
     demonstrating all five gates pass.
  4. Any localized fix (format/vet/test/import) discovered by the gates, applied
     in place — IF AND ONLY IF a gate fails. If all gates are green at run time,
     NO source file is modified.

**Success Definition** (PRD §20 + item contract):
  - `go vet ./...` exits 0 with no diagnostics.
  - `go test ./...` exits 0 (`ok  web-search-prime-fixer`).
  - `go build -o web-search-prime-fixer .` exits 0 and produces an executable.
  - `go build -ldflags "-X main.version=0.1.0"` exits 0, AND a run of that binary
    answers `GET /healthz` with `{"ok":true,"version":"0.1.0"}`.
  - `go list -m all` prints ONLY `web-search-prime-fixer` (zero requires);
    `go list -deps .` shows no `github.com/…` / `golang.org/x/…` / module-prefixed
    path; `gofmt -l .` is empty.
  - `git status` shows NO change to `go.mod` (still `module web-search-prime-fixer`
    + `go 1.22`, zero requires). The only committed changes, if any, are localized
    defect fixes — never a new dependency or a behavior change.

## User Persona

**Target User**: **P1.M5.T3** (documentation) — it cites the green gate as the
  proof that "the proxy runs as one Go binary with stdlib only" (PRD §20) and that
  the install/build commands in the README actually work. Secondary: any future
  maintainer running `go vet`/`go test`/`go build` before a release.

**Use Case**: A maintainer is about to cut a release or write the README. They run
  the gate, see all five checks pass, and know the binary is shippable and the
  stdlib-only invariant holds.

**Pain Points Addressed**: (1) Without the explicit ldflags+`/healthz` runtime
  check, a future refactor that moved `version` out of package scope (e.g. into a
  local in `main()`) would silently break version injection and no test would
  catch it — the runtime check catches it. (2) Without the import audit, a stray
  `github.com/…` import would violate PRD §6/§9 (stdlib only) and no `go vet`
  would flag it — the audit catches it.

## Why

- **PRD §18 (Building and running)** is the exact contract for the build/test
  commands (`go build -o web-search-prime-fixer .`, `go test ./...`). This gate
  runs them verbatim.
- **PRD §16 (Health and operations)** requires version injected at build via
  `-ldflags "-X main.version=..."` (default `dev`). The gate proves it end-to-end
  by BUILDING with the flag and curling `/healthz` for the injected value — not
  just asserting the source declares `var version`.
- **PRD §20 (Success criteria)**: "The proxy runs as one Go binary with stdlib
  only." The import audit is the proof of the "stdlib only" half; the build is the
  proof of the "one Go binary" half.
- **PRD §6/§9**: no third-party dependencies. `go.mod` lists zero requires. The
  audit confirms no import leaked in across all production files.
- **Coherence across the chain**: P1.M1–P1.M5.T1 built the module incrementally,
  each unit shipping its own tests. This is the SINGLE end-to-end gate that proves
  they integrate cleanly (vet + test + build) and that the whole is stdlib-only.

## What

No user-visible behavior change (verification step). The five gates run in order;
each must pass. If a gate fails, the implementer reads the diagnostic, applies the
minimal localized fix from the triage matrix, and re-runs from the failed gate. No
gate may be skipped; no dependency may be added.

### Success Criteria

- [ ] `go vet ./...` → exit 0, no output.
- [ ] `go test ./...` → exit 0; report line `ok  web-search-prime-fixer …`.
- [ ] `go build -o web-search-prime-fixer .` → exit 0; `./web-search-prime-fixer`
      is an executable file.
- [ ] `go build -ldflags "-X main.version=0.1.0"` → exit 0; running that binary
      and `curl /healthz` returns a body containing `"version":"0.1.0"`.
- [ ] `go list -m all` prints exactly `web-search-prime-fixer` (zero requires).
- [ ] `go list -deps .` contains NO module-prefixed import (only stdlib).
- [ ] `gofmt -l .` prints nothing.
- [ ] `git status --short go.mod` shows no modification (still zero requires).

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can run this gate from this PRP alone
because: (a) every command is given verbatim with its exact expected output; (b)
the version-injection mechanism (`var version = "dev"` in package main, set by
`-X main.version=...`) is explained with the runtime check that proves it; (c)
the import-audit commands and their pass/fail criteria are given; (d) the
failure-triage matrix maps each gate to its likely root cause and fix; (e) the
scope boundary forbids the two dangerous moves (adding a dependency, changing
behavior); (f) the baseline at research time is recorded so the implementer knows
the expected (all-green) outcome.

### Documentation & References

```yaml
# MUST READ — the build/test command contract this gate enforces.
- file: PRD.md
  section: "§18 Building and running"
  why: §18 lists the EXACT commands: `go build -o web-search-prime-fixer .` and
        `go test ./...`. This gate runs them verbatim and asserts their exit codes.
  critical: the `-o web-search-prime-fixer` output name is part of the contract;
        do not substitute `go build` alone (the explicit name is what §18 specifies).

# MUST READ — the version-injection contract the ldflags build proves.
- file: PRD.md
  section: "§16 Health and operations"
  why: §16 "Version is injected at build via `-ldflags "-X main.version=..."` or
        defaults to `dev`." The gate builds WITH the flag and curls /healthz for
        the injected value — the only end-to-end proof that injection works.
  critical: `-X` MUST target `main.version`, a PACKAGE-LEVEL var in package main.
        A local in main() is NOT settable by the linker; if /healthz shows "dev"
        after a flagged build, `version` is not package-level (fix: hoist it).

# MUST READ — the success criteria this gate is the proof of.
- file: PRD.md
  section: "§21 Success criteria"
  why: the last bullet — "The proxy runs as one Go binary with stdlib only." The
        build proves "one Go binary"; the import audit proves "stdlib only".

# MUST READ — the stdlib-only invariant the import audit guards.
- file: PRD.md
  section: "§6 Non-functional requirements" + "§9 Architecture (File layout)"
  why: §6/§9 forbid third-party deps; `go.mod` lists zero requires. The audit
        confirms no import leaked in.
  critical: adding ANY `require` to go.mod is OUT OF SCOPE and violates the PRD.
        If an import leak is found, the fix is to replace it with a stdlib equiv,
        NOT to `go get` the dependency.

# READ — the version var + healthHandler the ldflags build sets and serves.
- file: main.go
  section: "`var version = "dev"` (package-level, ~line 133) + `healthHandler` (~143)"
  why: the flag `-X main.version=0.1.0` sets THIS var; healthHandler marshals it as
        `{"ok":true,"version":<version>}`. The runtime /healthz check observes the
        linked value.
  critical: if the value is package-scoped and still "dev" after a flagged build,
        the `-X` path was wrong (must be `main.version`, not e.g. `main.Version`).

# READ — the import audit reference (external-deps brief confirms stdlib suffices).
- docfile: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§2 Go stdlib SSE proxying — CONFIRMED; no stdlib SSE framer, must hand-roll"
  why: documents that net/http + io + http.Flusher suffice (no SSE lib needed) and
        that go.mod lists zero requires. The import audit operationalizes this claim.

# READ — the on-disk baseline this gate is expected to reproduce.
- docfile: plan/001_c0abc3757e9a/P1M5T2S1/research/baseline-and-gates.md
  section: "§1 Baseline at research time" + "§5 Failure-triage matrix"
  why: records that ALL FIVE GATES were green at research time (incl. proxy_e2e_test.go)
        and gives the per-gate triage matrix for the fix clause.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod                  # module web-search-prime-fixer; go 1.22  (ZERO requires; stdlib only)
  doc.go                  # package main comment
  main.go                 # `var version = "dev"` (pkg-level) + healthHandler + bootstrap + routing
  config.go               # Config + DefaultConfig + ResolveConfig
  proxy.go                # newProxyHandler + forward + decideRewrite + log call sites
  rewrite.go              # Rewrite
  sse.go                  # NewSSEReader/Inject/warningText
  config_test.go          # 4 tests
  health_test.go          # 5 tests
  logger_test.go          # 8 tests
  rewrite_test.go         # 6 tests
  resolve_test.go         # 8 tests
  sse_test.go             # 15 tests
  proxy_test.go           # 15 tests (seed helpers: newTestProxy/captureProxy/testSID/…)
  proxy_harness_test.go   # 2 tests (S1 harness: fakeMCP/newFakeMCP/postRPC/recorded)
  proxy_log_test.go       # 3 tests (M4.T3.S1 log events)
  proxy_e2e_test.go       # 5 tests (M5.T1.S2 §19.3 cases — landed in parallel)
  testdata/initialize.sse         # golden fixture (id:1 initialize)
  testdata/tools_call.sse         # golden fixture (id:2 tools/call)
  testdata/tools_call_multiline.sse
  .gitignore              # ignores /web-search-prime-fixer (build artifact) + .pi-subagents/
  PRD.md
```
**No `go.sum`** — correct for a zero-requires module (it is only written when
`go.mod` has requires). Its absence is itself a confirmation of stdlib-only.

### Desired Codebase tree with files to be added and responsibility of file

```bash
./web-search-prime-fixer   # BUILD ARTIFACT (gitignored): produced by BOTH build commands.
                           #   - `go build -o web-search-prime-fixer .`   (the runnable binary, PRD §18)
                           #   - `go build -ldflags "-X main.version=0.1.0"` (overwrites it with the
                           #     versioned build; /healthz then reports version:"0.1.0")
                           # Not committed (already in .gitignore as /web-search-prime-fixer).
# NO new source file is created by this gate. If a gate fails, the fix is applied
# IN PLACE to the offending existing file (see Failure-Triage Matrix). go.mod is
# NEVER modified (zero requires is an invariant).
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — the ldflags build must be verified at RUNTIME, not just that it exits 0.
  `go build -ldflags "-X main.version=0.1.0"` succeeds even if `version` is the
  wrong symbol (the linker silently no-ops an unmatched -X). The ONLY proof that
  injection worked is: run the binary, curl /healthz, assert the body contains
  `"version":"0.1.0"`. Asserting just the build exit code is a vacuous check.

CRITICAL — `-X` target is `main.version` (lowercase package-level var in package
  main). NOT `main.Version`, NOT a local in main(). If the runtime check shows
  "dev" after a flagged build, the var is not package-level/settable — hoist it.

CRITICAL — port clash when verifying the versioned binary. A prior `./web-search-
  prime-fixer` may still bind 127.0.0.1:8787. Run the versioned binary on an
  ALTERNATE port: `WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer &` then
  `curl http://127.0.0.1:18787/healthz`. Capture the PID and `kill $PID` after.
  DO NOT use `pkill -f wspf` — that pattern matches the shell's own command line
  (it contains "wspf") and kills the shell. Kill by saved PID only.

CRITICAL — go.sum must NOT exist. A zero-requires module writes no go.sum. If a
  `go.sum` appears, a `require` leaked into go.mod — that is a stdlib-only
  violation (PRD §6/§9); find and remove the third-party import.

CRITICAL — adding ANY `require` to go.mod is OUT OF SCOPE and violates the PRD.
  If `go list -deps` / grep finds a `github.com/…` or `golang.org/x/…` import,
  the fix is to replace it with the stdlib equivalent (the module was deliberately
  stdlib-only — see architecture/external_deps.md §2). Never `go get` it.

GOTCHA — `go vet` catches unused imports/locals and redeclared symbols (a risk if
  the parallel proxy_e2e_test.go collides with a seed helper name). Read the
  diagnostic; the file:line is exact. Fix by removing the unused symbol or
  renaming the collision (do NOT edit proxy_test.go / proxy_harness_test.go).

GOTCHA — `gofmt -l` lists files that are not gofmt-clean. Fix is a single safe
  command: `gofmt -w <file>`. Re-run `gofmt -l .` to confirm empty.

GOTCHA — the test suite uses `httptest` fakes + golden fixtures (testdata/*.sse);
  `go test ./...` makes NO network calls and needs NO credentials. A test failure
  is a real defect, never an environment/credential issue.

GOTCHA — `go test` caches results. To get a TRUE run (not a stale `(cached)`),
  run `go clean -testcache` before the gate, or use `go test -count=1 ./...`.
  The gate should use a clean run so the record reflects reality.

GOTCHA — `go build` (no -o, no path) in the module root derives the output name
  from the MODULE name → `./web-search-prime-fixer`. So both build commands write
  to the same path; the second (ldflags) overwrites the first. That is fine for
  the gate (we want the final artifact to be the versioned one). If you need to
  keep the default-`dev` binary for comparison, build it to a different path.
```

## Implementation Blueprint

### Data models and structure

None. This is a gate task; no types, models, or new code are produced. The only
artifacts are the binary (`./web-search-prime-fixer`, gitignored) and the
verification record (captured command output).

### Verification Steps (ordered by dependencies — run top to bottom)

> Run each step; assert the Expected before proceeding. If a step fails, apply the
> minimal fix from the Failure-Triage Matrix, then RE-RUN from the failed step
> (earlier steps may have been invalidated by the fix). Do not skip ahead.

```yaml
Step 0: (PREREQUISITE) confirm the on-disk state matches the gate's input
  - RUN: ls go.mod doc.go main.go config.go proxy.go rewrite.go sse.go
         ls *_test.go testdata/*.sse
  - EXPECT: all present. proxy_e2e_test.go present (P1.M5.T1.S2 landed in parallel).
  - RUN: cat go.mod
  - EXPECT: `module web-search-prime-fixer` + blank + `go 1.22` + NOTHING ELSE
            (zero requires). If a require block exists, that is a stdlib-only
            violation — STOP (out of scope; report) unless the import can be
            trivially replaced with stdlib.

Step 1: `gofmt` clean (cheapest gate; run first so vet sees formatted code)
  - RUN: gofmt -l .
  - EXPECT: empty output (no files listed). EXIT 0.
  - ON FAIL: for each listed file run `gofmt -w <file>`; re-run `gofmt -l .`.

Step 2: `go vet ./...`
  - RUN: go vet ./...
  - EXPECT: no output, exit 0.
  - ON FAIL: read the diagnostic (file:line + message). Apply the matching fix
            from the Failure-Triage Matrix (unused import/local → remove;
            redeclared symbol → rename the NEW symbol, never the seed/harness).
            Re-run Step 2.

Step 3: `go test ./...` (clean run — no stale cache)
  - RUN: go clean -testcache && go test ./...
  - EXPECT: `ok  web-search-prime-fixer …`, exit 0. (71 funcs / 10 files.)
  - OPTIONAL spot-check: go test -run 'TestE2E_' -v  → 5/5 PASS (the §19.3 cases).
  - ON FAIL: read the failing test + assertion. If a PRODUCTION bug is exposed
            (e.g. redaction leak, session-forward regression), fix the prod file
            minimally; if a TEST bug, fix the test. Re-run Step 3. If a
            PRE-EXISTING test regressed because the new file shadowed a seed
            helper, revert the collision (do NOT edit proxy_test.go).

Step 4: build the runnable binary (PRD §18 exact command)
  - RUN: go build -o web-search-prime-fixer .
  - EXPECT: exit 0; `./web-search-prime-fixer` is an executable file
            (`ls -la web-search-prime-fixer`).
  - ON FAIL: the compiler names file:line; fix the syntax/type error there.
            Re-run Step 4.

Step 5: build with version injection + RUNTIME /healthz check (PRD §16)
  - RUN: go build -ldflags "-X main.version=0.1.0"
  - EXPECT: exit 0 (overwrites ./web-search-prime-fixer with the versioned build).
  - THEN: run it on an alt port and curl /healthz:
         WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer & PID=$!
         sleep 1.2
         curl -s http://127.0.0.1:18787/healthz
         kill $PID; wait $PID 2>/dev/null
  - EXPECT: the curl body is exactly `{"ok":true,"version":"0.1.0"}`.
  - ON FAIL (body shows "dev"): `version` is not a package-level var settable by
            `-X main.version`. Confirm main.go has `var version = "dev"` at
            package scope in `package main`; hoist it if it's a local. Re-run Step 5.

Step 6: stdlib-only import audit (PRD §6/§9/§20)
  - 6a. RUN: go list -m all
         EXPECT: prints ONLY `web-search-prime-fixer` (the module itself; zero deps).
  - 6b. RUN: go list -deps . | grep -E '^[A-Za-z0-9.~-]+\.[A-Za-z]{2,}/|^(github|golang|gopkgin|google)\.'
              (or simpler: inspect go list -deps . for any path containing a dot
               before the first slash that is NOT a stdlib package)
         EXPECT: empty (no module-prefixed import paths; only stdlib).
  - 6c. RUN: grep -A20 '"import"\|import (' *_test.go main.go config.go proxy.go rewrite.go sse.go doc.go \
              | grep '"' | grep -E '\.(com|org|dev|io)/|github|golang\.org/x|gopkgin'
         EXPECT: empty (no third-party import literals in any source file).
  - 6d. RUN: ls go.sum
         EXPECT: "No such file" (a zero-requires module writes no go.sum).
  - ON FAIL (a third-party path appears): the fix is to replace it with the stdlib
            equivalent (see architecture/external_deps.md). NEVER `go get` it and
            NEVER add a `require` to go.mod — that violates PRD §6/§9. If no
            stdlib equivalent exists, STOP and report (out of scope).

Step 7: go.mod unchanged + final regression
  - RUN: git status --short go.mod
         EXPECT: no output (go.mod unmodified; still zero requires).
  - RUN: go vet ./... && go test -count=1 ./... && gofmt -l .
         EXPECT: all clean/green (re-confirm after any Step 1-6 fix).
  - RECORD: capture the stdout/stderr of every step above as the verification record.
```

### Failure-Triage Matrix (the "fix any failures" clause, operationalized)

```
SYMPTOM                              LIKELY CAUSE                       FIX (in scope)
-----------------------------------------------------------------------------------------
gofmt -l lists a file                unformatted source                 gofmt -w <file>
vet: "imported and not used"         stray import (likely in the        drop the import line;
                                     parallel proxy_e2e_test.go)        re-run vet
vet: "declared and not used"         stray local var                    remove it; re-run vet
vet: "redeclared in this block"      new symbol collides with a seed/   rename the NEW symbol;
                                     harness helper (same package)      do NOT edit proxy_test.go
                                                                        or proxy_harness_test.go
test: proxy_e2e_* case FAILS         parallel-file bug OR a prod        read the assertion; if prod
                                     regression it exposes              bug, fix prod minimally; if test
                                                                        bug, fix the test
test: pre-existing test regresses    new file shadowed a seed helper    revert the collision; re-run
build: compile error                 syntax/type error anywhere         compiler names file:line; fix there
ldflags build ok, /healthz="dev"     version not package-level OR       hoist `var version` to package
                                     wrong -X target                    scope in package main; flag must
                                                                        be -X main.version (lowercase)
go list -m all shows a require       a third-party import leaked in     replace with stdlib equiv (see
                                                                        external_deps.md §2); NEVER go get
go.sum appears                       a require leaked into go.mod       remove the third-party import;
                                                                        go.sum disappears on next build
genuine design defect beyond a fix   n/a                                STOP; report (out of scope) — do
                                                                        NOT expand into a feature change
```

**Scope guard (re-stated):** every fix above is LOCALIZED (format, vet, test,
import, package-scope of `version`). None changes product behavior, adds a feature,
or adds a dependency. If a failure implies a design change, STOP and report.

### Implementation Patterns & Key Details

```bash
# PATTERN: run gates in order gofmt -> vet -> test -> build -> ldflags+runtime -> audit.
# Each later gate depends on the earlier being clean (vet needs formatted code;
# build needs vet-clean; the runtime check needs the build).

# PATTERN: clean test run (no stale cache) — the gate record must reflect reality:
go clean -testcache && go test ./...
# (equivalently: go test -count=1 ./...)

# PATTERN: runtime version-injection proof (PRD §16) — the ONLY real test that -X worked:
WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer & PID=$!
sleep 1.2
curl -s http://127.0.0.1:18787/healthz      # MUST print {"ok":true,"version":"0.1.0"}
kill $PID; wait $PID 2>/dev/null
# GOTCHA: alt port avoids clashing with a prior default-port instance. Kill by
#         saved PID; never `pkill -f wspf` (it matches and kills this shell).

# PATTERN: the two builds both write ./web-search-prime-fixer (module-name derived):
#   go build -o web-search-prime-fixer .           # explicit name (PRD §18)
#   go build -ldflags "-X main.version=0.1.0"      # no -o -> derives name from module
# The second overwrites the first; the final artifact is the versioned binary.

# PATTERN: stdlib-only audit is THREE independent checks (belt and suspenders):
#   go list -m all            # module-level: zero requires
#   go list -deps .           # transitive: only stdlib package paths
#   grep import literals      # source-level: no third-party import strings
# Plus: ls go.sum must fail (no go.sum for a zero-requires module).
```

### Integration Points

```yaml
FILES PRODUCED:
  - ./web-search-prime-fixer   # build artifact (gitignored via .gitignore /web-search-prime-fixer).
                               # Built twice (default + ldflags); final = versioned.
FILES POTENTIALLY MODIFIED (ONLY IF A GATE FAILS):
  - the single offending file named by the diagnostic (gofmt/vet/test/import).
    Minimal localized fix per the Failure-Triage Matrix. No behavior change.
FILES NEVER MODIFIED (contract):
  - go.mod: zero requires is an INVARIANT (PRD §6/§9). Never add a require.
  - PRD.md, any tasks.json, any prd_snapshot.md: read-only (orchestrator-owned).
  - README.md / config.example.json / doc.go package comment: P1.M5.T3 owns these;
    this gate does NOT touch docs (Mode A).
CONSUMER SEAM (keep stable for P1.M5.T3):
  - The green gate + the recorded command outputs are P1.M5.T3.S1 (README)'s proof
    that the §18 install/build commands and the §16 version-injection work. Keep
    the verification record (it documents "this binary builds and is stdlib-only").
DATABASE / ROUTES / ENV / CONFIG: none (build/test gate; no runtime config change).
```

## Validation Loop

### Level 1: The five gates (this IS the gate — run in order)

```bash
# 0. baseline state
cat go.mod                       # expect: module + go 1.22, ZERO requires
ls go.sum 2>&1                   # expect: "No such file"

# 1. gofmt
gofmt -l .                       # expect: EMPTY

# 2. vet
go vet ./...                     # expect: no output, exit 0

# 3. test (clean run)
go clean -testcache && go test ./...    # expect: ok  web-search-prime-fixer …

# 4. build (PRD §18 exact)
go build -o web-search-prime-fixer .     # expect: exit 0; ./web-search-prime-fixer exists

# 5. version injection (PRD §16) — BUILD + RUNTIME check
go build -ldflags "-X main.version=0.1.0"   # expect: exit 0
WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer & PID=$!
sleep 1.2
curl -s http://127.0.0.1:18787/healthz      # expect: {"ok":true,"version":"0.1.0"}
kill $PID; wait $PID 2>/dev/null

# 6. stdlib-only audit
go list -m all                   # expect: ONLY web-search-prime-fixer
go list -deps .                  # expect: only stdlib package paths (no github.com/…)
ls go.sum 2>&1                   # expect: "No such file"
git status --short go.mod        # expect: empty (unmodified)

# Expected: every command meets its expectation. If any fails, apply the
# Failure-Triage Matrix fix and re-run from the failed step. Do not skip.
```

### Level 2: Spot-checks (deeper confidence, optional but recommended)

```bash
# The §19.3 end-to-end cases specifically (the P1.M5.T1.S2 suite):
go test -run 'TestE2E_' -v       # expect: 5/5 PASS (RewriteAndWarning, CanonicalPassthrough,
                                 #        SessionRoundTrip, AuthForwardedAndRedacted, HealthzIsolated)

# Test inventory sanity (expect 71 funcs across 10 files):
grep -rho "^func Test" *_test.go | wc -l    # expect: 71

# Default-version sanity (no ldflags -> "dev"):
go build -o /tmp/wspf-default .
WSPF_LISTEN=127.0.0.1:18788 /tmp/wspf-default & PID=$!
sleep 1.2; curl -s http://127.0.0.1:18788/healthz   # expect: {"ok":true,"version":"dev"}
kill $PID; wait $PID 2>/dev/null

# Graceful-shutdown sanity (PRD §16): send SIGTERM, observe the "shutdown" log + exit 0:
WSPF_LISTEN=127.0.0.1:18789 ./web-search-prime-fixer > /tmp/wspf.log 2>&1 & PID=$!
sleep 1.2; kill -TERM $PID; wait $PID 2>/dev/null
grep -q '"msg":"shutdown"' /tmp/wspf.log && echo "shutdown logged OK"
```

### Level 3: Final regression after any fix

```bash
# If ANY Step 1-6 fix was applied, re-run the whole gate clean to prove no
# regression was introduced by the fix:
gofmt -l . && go vet ./... && go clean -testcache && go test ./... && \
  go build -o web-search-prime-fixer . && go build -ldflags "-X main.version=0.1.0"
git status --short go.mod        # still empty (no new requires)

# Expected: all green; go.mod untouched. If a fix regressed something, the fix
# was too broad — revert to the minimal localized change and re-run.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Prove the "one Go binary" criterion (PRD §20) is a single static-ish binary:
file web-search-prime-fixer      # expect: ELF 64-bit LSB executable, dynamically linked
ls -la web-search-prime-fixer    # expect: ~10 MB, executable bit set

# Prove stdlib-only at the BINARY level (no third-party module compiled in):
go version -m web-search-prime-fixer | head    # the build info; "path web-search-prime-fixer"
                                               # and NO "dep" lines (zero requires -> no dep info)
# Expect: the module path line; NO "dep\tgithub.com/…" / "dep\tgolang.org/x/…" lines.

# Prove /healthz isolation holds in the shipped binary (PRD §16, §19.3 case 5):
WSPF_LISTEN=127.0.0.1:18790 ./web-search-prime-fixer & PID=$!
sleep 1.2
curl -s -o /dev/null -w "%{http_code}\n" http://127.0.0.1:18790/healthz   # expect: 200
kill $PID; wait $PID 2>/dev/null

# Expected: the binary embeds no third-party module (go version -m shows no dep lines);
# /healthz returns 200 from the shipped binary.
```

## Final Validation Checklist

### Technical Validation

- [ ] Step 1: `gofmt -l .` empty.
- [ ] Step 2: `go vet ./...` exit 0, no output.
- [ ] Step 3: `go test ./...` exit 0, `ok  web-search-prime-fixer` (clean run, no cache).
- [ ] Step 4: `go build -o web-search-prime-fixer .` exit 0; executable produced.
- [ ] Step 5: `go build -ldflags "-X main.version=0.1.0"` exit 0 AND `/healthz`
      runtime body == `{"ok":true,"version":"0.1.0"}`.
- [ ] Step 6: `go list -m all` == only `web-search-prime-fixer`; `go list -deps .`
      has no module-prefixed path; `go.sum` absent; `go.mod` unmodified.

### Feature Validation (PRD §16/§18/§20)

- [ ] PRD §18 build command (`go build -o web-search-prime-fixer .`) works verbatim.
- [ ] PRD §18 test command (`go test ./...`) is green.
- [ ] PRD §16 version injection works end-to-end (build flag + runtime /healthz value).
- [ ] PRD §16 default version is `dev` when no flag is passed (Level 2 spot-check).
- [ ] PRD §16 graceful shutdown logs "shutdown" on SIGTERM (Level 2 spot-check).
- [ ] PRD §20 "one Go binary" + "stdlib only" both proven (build + import audit +
      `go version -m` shows no dep lines).

### Code Quality Validation

- [ ] No new dependency (`go.mod` still zero requires; `go.sum` absent).
- [ ] Any fix applied was MINIMAL and LOCALIZED (named file only; no behavior change).
- [ ] No edits to `go.mod`, `PRD.md`, any `tasks.json`, any `prd_snapshot.md`.
- [ ] No edits to docs (`README.md` / `config.example.json` / `doc.go`) — P1.M5.T3 owns those.

### Documentation & Deployment

- [ ] **Mode A** (per item contract): verification step — NO documentation change required.
      The verification record (captured command outputs) is the artifact; P1.M5.T3.S1
      will cite the green gate in the README.
- [ ] The build artifact `./web-search-prime-fixer` is produced and (per `.gitignore`)
      NOT committed.

---

## Anti-Patterns to Avoid

- ❌ Don't assert the ldflags build "works" by exit code alone — the linker silently
  no-ops an unmatched `-X`. The ONLY proof is the runtime `/healthz` value.
- ❌ Don't run the versioned binary on the default port (8787) while a prior instance
  is bound — you'll get a "bind: address already in use" and curl the OLD binary.
  Use an alternate `WSPF_LISTEN` port; kill by saved PID.
- ❌ Don't use `pkill -f wspf` to clean up — the pattern matches this shell's command
  line and kills the shell. Kill by saved `$PID` only.
- ❌ Don't trust `go test ./...` `(cached)` as the gate result — run `go clean
  -testcache` (or `-count=1`) so the record reflects a true run.
- ❌ Don't add a `require` to `go.mod` to "fix" a missing import — that violates PRD
  §6/§9. Replace the third-party import with its stdlib equivalent (see
  architecture/external_deps.md §2). If none exists, STOP and report.
- ❌ Don't expand scope into a feature/behavior change to "fix" a failing test — if a
  failure implies a design defect, STOP and report rather than rearchitect under a gate.
- ❌ Don't edit `proxy_test.go` or `proxy_harness_test.go` to resolve a redeclaration —
  rename the NEW symbol; the seed/harness files are consumed by parallel tests.
- ❌ Don't modify `go.mod`, `PRD.md`, `tasks.json`, `prd_snapshot.md`, or any doc file —
  this gate owns the binary + the verification record, nothing else.
- ❌ Don't skip a gate because "the previous one passed" — each gate tests a different
  invariant (format vs vet vs test vs build vs version-injection vs stdlib-only).

---

## Confidence Score

**9/10** for one-pass success. At research time, with the parallel
`proxy_e2e_test.go` already on disk, ALL FIVE GATES were verified green by direct
execution (`go vet ./...` exit 0; `go test ./...` `ok 0.070s`, 71 funcs;
`gofmt -l .` empty; both builds exit 0; ldflags build → `/healthz`
`{"ok":true,"version":"0.1.0"}`; `go list -m all` == only the module). The version
injection mechanism (`var version = "dev"` package-level + `-X main.version=...`)
and the stdlib-only invariant (zero requires, no go.sum, all imports stdlib) are
confirmed on disk. The gate is therefore expected to pass on the first run; the
PRP's residual value is the runtime version-injection check (the one assertion that
is NOT covered by any unit test and that a naive gate would skip) and the
Failure-Triage Matrix for the fix clause. Deducted 1 point for the small chance the
parallel `proxy_e2e_test.go` drifts before this gate runs (mitigated by Step 0's
on-disk check, the ordered gates that re-run from the failed step, and the triage
matrix's per-symptom fixes).
