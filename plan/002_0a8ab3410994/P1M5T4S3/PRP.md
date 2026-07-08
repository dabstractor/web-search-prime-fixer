name: "P1.M5.T4.S3 — Quality gate: go vet ./... + go test ./... + go build clean; confirm single SDK require; verify binary boots and /healthz returns 200"
description: |

  THIS IS A VERIFICATION-ONLY GATE — the final definition-of-done for the entire Phase P1
  (PRD §21 step 8 + §22 success criteria; PRD §7/§13 single-dependency rule). It produces
  **NO code and NO documentation surface change** (item contract: "DOCS: none —
  verification-only"). The deliverable is a **clean quality-gate pass** plus a concise
  evidence report. If any check fails, the agent DOCUMENTS the failure for remediation
  (it does not fix it — that is out of scope and would mask the gate).

  CONTRACT (item description, PRD §21.8 / §22 / §7 / §13):
    1. RESEARCH NOTE: the final gate is `go vet ./...` AND `go test ./...` clean. The sole
       external dependency is the Go MCP SDK — confirm go.mod has exactly one DIRECT require.
    2. INPUT: all code and tests from every milestone/task of Phase P1 (M1–M5).
    3. LOGIC — run six checks, all must pass clean:
         (a) `go vet ./...`        — clean (no warnings, exit 0).
         (b) `go test ./...`       — clean (all tests pass, exit 0).
         (c) `go build`            — produces the binary.
         (d) go.mod                — exactly ONE direct (non-indirect) `require`, and it is
                                     `github.com/modelcontextprotocol/go-sdk` (PRD §7/§13).
         (e) no v1 proxy/sse/rewrite files remain (PRD §0 pivot; §13 SDK adoption).
         (f) the binary boots and `GET /healthz` returns 200 (PRD §16).
       If any check fails, document it for remediation.
    4. OUTPUT: a clean quality-gate pass (PRD §22 success criteria).
    5. DOCS: none — verification-only.

  PARALLEL CONTEXT: P1.M5.T4.S2 (config.example.json + doc.go rewrite) is implemented in
  parallel. The gate is INSENSITIVE to T4.S2's timing (see Context §Parallel Sensitivity):
  doc.go vets clean in both v1/v2 comment forms, and config.example.json is not in the
  build/test/vet graph. Run the gate against current HEAD; RE-RUN after T4.S2 lands to
  capture the final phase state. Both runs are expected green (see research/gate-baseline.md).

---

## Goal

**Feature Goal**: Prove, by running the exact gate commands, that the entire Phase P1
  deliverable — the v2 normalizing MCP server built on the official Go MCP SDK — meets
  PRD §21 step 8 (`go vet ./...` and `go test ./...` clean), PRD §22 (success criteria:
  single binary on the official SDK, one `web_search` tool, correct first-call behavior
  exercised by the test suites), and PRD §7/§13 (sole external dependency is
  `github.com/modelcontextprotocol/go-sdk`).

**Deliverable**: A **clean quality-gate pass** plus a concise evidence report (captured
  in the implementing agent's completion summary; a copy may be written to
  `plan/002_0a8ab3410994/P1M5T4S3/research/gate-report.md`). NO source files, docs,
  go.mod, go.sum, or test files are created or modified by this item. If any check fails,
  the report documents the failure precisely for a follow-up remediation item — it does
  NOT patch the code.

**Success Definition**:
  - `go vet ./...` exits 0 with no output.
  - `go test ./...` exits 0 with `ok  web-search-prime-fixer <duration>` (all 10 test
    files green — see Context §Test Surface).
  - `go build -o <binary> .` succeeds and the binary exists on disk.
  - `go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all` (excluding the
    main module) prints EXACTLY ONE line: `github.com/modelcontextprotocol/go-sdk v1.6.1`;
    AND the only single-line `require` in go.mod is that SDK line; AND every entry in the
    `require (...)` block is marked `// indirect`.
  - No `proxy*.go` / `sse*.go` / `rewrite*.go` files exist on disk, and no dead v1 markers
    (`mark3labs`, `mcp-go`, `NewSingleHostReverseProxy`, `transparent proxy`) appear in any
    `.go` file.
  - A freshly built binary, launched with `WSPF_LISTEN=127.0.0.1:<high-port>`, responds to
    `GET /healthz` with `200` and body `{"ok":true,"version":"dev"}`, and emits one
    structured JSON startup line on stderr.

## User Persona

**Target User**: The phase owner / release engineer who needs a single trustworthy
  "Phase P1 is done" signal before declaring v2.0 shippable.
**Use Case**: Run the six gate checks in order; if all green, mark Phase P1 complete
  against PRD §22; if any red, route the documented failure to a remediation task.
**User Journey**: invoke the gate → read the per-check evidence → either confirm green
  (phase done) or copy the failure section into a new remediation work item.
**Pain Points Addressed**: removes ambiguity about "is it actually done"; enforces the
  single-dependency invariant (PRD §7/§13) that the whole pivot relied on; catches any
  v1-era residue that would contradict PRD §0; confirms the binary actually serves
  `/healthz` (PRD §16) rather than merely compiling.

## Why

- This IS PRD §21 step 8 and the operational expression of PRD §22. Every prior milestone
  (M1 foundation/SDK/v1-deletion, M2 extract, M3 teach, M4 upstream, M5 wiring+tests+docs)
  funnels into these checks. A green gate is the only acceptable definition-of-done.
- The single-direct-require invariant (PRD §7 "sole external dependency", §13 "we adopt
  the official SDK") is load-bearing: the entire reason for deleting the v1 stdlib proxy
  and rejecting mark3labs/mcp-go was to own exactly one dependency. This gate PROVES that
  invariant holds in the committed go.mod, not just in intent.
- v1-residue checks (no proxy/sse/rewrite files, no dead markers) lock in PRD §0's pivot;
  a stray v1 file would silently re-introduce the SSE/line-length bug classes PRD §13
  lists as retired.
- The `/healthz` boot check converts "it compiles" into "it runs and serves" (PRD §16),
  which is the operator's actual definition of working.

## What

A six-check verification runbook, executed in dependency order, each check producing a
binary pass/fail result captured in the report. No code is written. The full, exact
commands and their expected "clean" output are in the Validation Loop (these ARE the
task). The checks are:

1. **vet** — `go vet ./...` clean.
2. **test** — `go test ./...` clean (full suite; use `-count=1` to bypass cache).
3. **build** — `go build -o <binary> .` produces a binary.
4. **single require** — exactly one DIRECT require in go.mod, and it is the SDK; the
   `require (...)` block is entirely `// indirect`.
5. **no v1 residue** — no proxy/sse/rewrite files on disk; no dead v1 markers in `.go`.
6. **boot + healthz** — built binary serves `GET /healthz` → 200 `{"ok":true,"version":"dev"}`.

### Success Criteria
- [ ] All six checks pass clean with the exact expected output in the Validation Loop.
- [ ] A gate report exists (agent summary + optional `research/gate-report.md`) recording
      each check's command, actual output, and PASS/FAIL.
- [ ] NO repo file is created or modified by this item other than (optionally)
      `research/gate-report.md` under this work item's plan directory.
- [ ] If any check failed, the report contains a "Failures" section precise enough to seed
      a remediation work item (command, output, root-cause hypothesis), and the agent did
      NOT attempt a fix.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can run this gate from this PRP alone because:
(a) every check is an exact, copy-pasteable shell command with its expected "clean"
output spelled out (Validation Loop = the task); (b) the one subtle check — "exactly one
require" — is disambiguated (direct vs indirect) with an authoritative `go list -m`
command, the go.mod grep, and the block-must-be-all-indirect assertion; (c) the binary
boot/healthz procedure (build → background with `WSPF_LISTEN` → poll `/healthz` → kill)
is fully specified including port selection and startup-line assertion; (d) the current
green baseline for every check is recorded in `research/gate-baseline.md` so the agent
knows exactly what green looks like; (e) the scope boundary (verification-only; no
code/doc/go.mod/go.sum edits) is explicit, with the single allowed optional output file.

### Documentation & References

```yaml
# MUST READ — the known-green baseline for every check (what "clean" looks like, captured
# by actually running the gate at research time). Cross-check live results against this.
- docfile: plan/002_0a8ab3410994/P1M5T4S3/research/gate-baseline.md
  why: per-check current PASS evidence; the 10-file (not 8) test surface; the
        direct-vs-indirect require disambiguation; the optional `go mod tidy` go.sum note;
        parallel-sensitivity vs T4.S2.
  critical: "exactly one require" means exactly one DIRECT require; the 6-entry indirect
        block is EXPECTED and is NOT a violation. Use the `go list -m` command as authority.

# MUST READ — the PRD sections this gate enforces.
- file: PRD.md
  section: "§21 Implementation order (step 8: go vet + go test clean)"
  why: the gate's origin — step 8 is the final implementation step.
- file: PRD.md
  section: "§22 Success criteria"
  why: the definition-of-done this gate certifies (single binary on official SDK; one
        web_search tool; correct first-call behavior — exercised by the test suites).
- file: PRD.md
  section: "§7 Non-functional requirements (sole external dependency = Go MCP SDK)"
  why: the single-direct-require invariant this gate proves in go.mod.
- file: PRD.md
  section: "§13 MCP SDK (decided: adopt the official SDK; reject mark3labs/mcp-go + stdlib)"
  why: basis for the "no v1 residue / no mark3labs / single SDK require" checks.
- file: PRD.md
  section: "§16 Health and operations"
  why: the GET /healthz contract the boot check validates (200 + {"ok":true,"version":...}).

# READ — the artifacts the gate inspects (DO NOT EDIT any of them).
- file: go.mod
  why: the single-direct-require + all-indirect-block check parses this. go 1.25.0 directive
        is forward-compatible with the installed go1.26.x (NOT a gate issue).
- file: main.go
  why: boot route table — `mux.HandleFunc("/healthz", healthHandler)`; `WSPF_LISTEN` env
        sets `cfg.Listen`; `&http.Server{Addr: cfg.Listen}` (NO timeouts, preserves SSE).
- file: health.go
  why: the /healthz contract — GET only, 405 otherwise; body `{"ok":true,"version":<ver>}`;
        `var version = "dev"` (settable via `-ldflags "-X main.version=..."`).
- file: go.sum
  why: `go mod verify` must pass ("all modules verified"). An extra/unused go.sum line is
        NOT a failure (see gate-baseline.md §Optional housekeeping).

# READ — confirm the SDK is the sole direct dep and no rejected SDK leaked in.
- docfile: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  section: "§1 Config patterns, §3 Health patterns, §4 main.go bootstrap"
  why: documents the /healthz GET-only contract, the WSPF_LISTEN env override, and the
        no-timeout server (so the boot check's expectations match the design).
- docfile: plan/002_0a8ab3410994/architecture/system_context.md
  why: confirms "v1 transparent proxy deleted" and "stdlib-only / no-go.sum constraint
        explicitly relinquished" (basis for the no-v1-residue check).
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  why: confirms the official SDK is what main.go/upstream.go import (no mark3labs).

# READ — the parallel item whose output this gate (re-)verifies.
- docfile: plan/002_0a8ab3410994/P1M5T4S2/PRP.md
  why: T4.S2 rewrites doc.go (comment-only) + config.example.json (not compiled). The gate
        is insensitive to its timing, but should be RE-RUN after T4.S2 lands for the final
        phase sign-off. Do not duplicate or conflict with T4.S2's edits.
```

### Current Codebase tree (run `ls *.go` at repo root)

```bash
web-search-prime-fixer/
  doc.go                 # package comment (T4.S2 rewrites concurrently; vets clean either form)
  main.go                # bootstrap: ResolveConfig → logger → /healthz route → http.Server
  health.go              # healthHandler: GET /healthz → 200 {"ok":true,"version":"dev"}
  logger.go              # structured JSON logger to stderr (redacts Authorization)
  config.go              # v2 Config + DefaultConfig + ResolveConfig (env: WSPF_LISTEN etc.)
  extract.go  extract_test.go     # query extraction from arbitrary input (M2)
  teach.go    teach_test.go       # append-after-results warning (M3)
  upstream.go upstream_test.go    # z.ai MCP client: session, re-init, Auth threading (M4)
  tools.go    tools_test.go       # advertised tool defs: one web_search + terse aliases (M5.T1)
  server.go   server_test.go      # AddTool + extract→delegate→teach dispatch (M5.T2/T3)
  server.go   dispatch_test.go    # dispatch-handler unit tests (M5.T2)
  config_test.go resolve_test.go health_test.go logger_test.go  # M1/M5 surviving suites
  config.example.json    # T4.S2 rewrites concurrently (not in build graph)
  go.mod  go.sum         # sole direct require = go-sdk v1.6.1 (gate inspects, does not edit)
  PRD.md                 # READ-ONLY; §21.8 / §22 / §7 / §13 / §16 are this gate's authority
  web-search-prime-fixer # pre-built binary (gate builds its OWN fresh copy; ignores this)
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
# NO repo-surface file is added or modified by this item (verification-only).
# The ONLY optional output is evidence, living under this work item's plan dir:
plan/002_0a8ab3410994/P1M5T4S3/research/gate-report.md   # OPTIONAL: per-check PASS/FAIL + output
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — "exactly one require" means exactly one DIRECT (non-indirect) require. go.mod
has TWO require stanzas: a single-line DIRECT one (go-sdk) AND a require(...) block of 6
// indirect entries. The indirect block is EXPECTED (transitive SDK deps) and is NOT a
violation. A naive `grep -c '^require' go.mod` returns 2 and would FALSE-FAIL the gate.
Authority: `go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all` minus
the main module must print exactly ONE line (the SDK). Also assert every line in the
require(...) block carries `// indirect`.

CRITICAL — this item is VERIFICATION-ONLY. Do NOT edit any source, test, go.mod, go.sum,
config.example.json, doc.go, README.md, or PRD.md. If a check fails, DOCUMENT it in the
report (command + output + root-cause hypothesis) for a remediation task; do NOT patch.
Patching would mask the gate and is out of scope.

CRITICAL — build a FRESH binary for the boot check (`go build -o /tmp/... .`). Do NOT
exercise the stale committed `web-search-prime-fixer` binary — it predates this run and
isn't evidence about the current source.

CRITICAL — the test surface is 10 files, not the 8 named in the item contract. The item
lists extract/teach/upstream/server/config/resolve/health/logger tests; the shipped repo
ALSO has tools_test.go (M5.T1) and dispatch_test.go (M5.T2). `go test ./...` runs all 10;
all must pass. This is not extra scope — `./...` is comprehensive by construction.

CRITICAL — the binary binds to cfg.Listen. To boot for the healthz check you MUST set
WSPF_LISTEN=127.0.0.1:<port> (or WSPF_CONFIG) to a free high port (e.g. 18787) so it does
not collide with a real instance or fail to bind 127.0.0.1:8787 if occupied. main.go fails
the process on ListenAndServe error, so a bind failure looks like an immediate exit.

CRITICAL — /healthz is GET-only (health.go enforces 405 on other methods). curl must use
GET (the default). A POST would return 405 and FALSE-FAIL the "200" assertion.

GOTCHA — `go test ./...` is cached. Use `go test -count=1 ./...` to force a fresh run for
authoritative evidence; `go test ./...` alone is acceptable if it prints `ok`.

GOTCHA — go.mod declares `go 1.25.0`; the installed toolchain is go1.26.x. This is
forward-compatible and is NOT a gate issue. Do not "fix" the directive.

GOTCHA — `go mod tidy` will trim one unused go.sum line (cloud.google.com/.../metadata
/go.mod hash). This is cosmetic, does not affect vet/build/test, and is NOT a gate
failure. Optionally running `go mod tidy` for a tidy go.sum is allowed but must not be
reported as a required fix or a failure.

GOTCHA — the SDK must be the OFFICIAL `github.com/modelcontextprotocol/go-sdk`, NOT
`github.com/mark3labs/mcp-go` (rejected, PRD §13). The single-require check inherently
covers this, but the no-v1-residue grep also checks for `mark3labs`/`mcp-go` strings.
```

## Implementation Blueprint

### Data models and structure

None. This is a verification gate. There are no data models, no code, no schemas. The
"model" is the six-check runbook in the Validation Loop below (the Validation Loop IS the
implementation for this item).

### Verification Tasks (ordered by dependencies)

```yaml
Task 0: PREREQUISITE — confirm the inputs exist on disk
  - RUN: test -f go.mod && test -f go.sum && test -f main.go && test -f health.go \
        && grep -q 'modelcontextprotocol/go-sdk' go.mod \
        && grep -q 'func healthHandler' health.go && echo OK
  - EXPECT: OK. If any fail, the v2 sources are missing — STOP (prerequisite not met) and
        report it; do NOT proceed to the gate.

Task 1: GATE CHECK (a) — go vet ./... clean
  - RUN: `go vet ./... 2>&1; echo "vet_exit=$?"`
  - EXPECT: no output on stdout/stderr before the marker; `vet_exit=0`.
  - ON FAIL: capture the exact vet diagnostic; report; do not fix.

Task 2: GATE CHECK (b) — go test ./... clean
  - RUN: `go test -count=1 ./... 2>&1; echo "test_exit=$?"`
  - EXPECT: `ok  web-search-prime-fixer <duration>` and `test_exit=0` (all 10 test files).
  - ON FAIL: capture the failing test name + output; report; do not fix.

Task 3: GATE CHECK (c) — go build produces a binary
  - RUN: `go build -o /tmp/wspf-gate-"$PID".bin . && test -s /tmp/wspf-gate-"$PID".bin \
        && echo "build OK: $(ls -l /tmp/wspf-gate-*.bin | awk '{print $5}') bytes"`
  - EXPECT: a non-empty binary; "build OK". (Also run `go build ./...` to confirm every
        package compiles, exit 0.)
  - ON FAIL: capture the compile error; report; do not fix.

Task 4: GATE CHECK (d) — exactly one DIRECT require, and it is the SDK
  - RUN (authority): `go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all \
        | grep -v '^web-search-prime-fixer'`
  - EXPECT: exactly ONE line: `github.com/modelcontextprotocol/go-sdk v1.6.1`.
  - RUN (go.mod grep): `grep -nE '^require [^(]' go.mod`
  - EXPECT: one line — `6:require github.com/modelcontextprotocol/go-sdk v1.6.1`.
  - RUN (indirect-block purity): `awk '/^require \(/{f=1;next} /^\)/{f=0} f' go.mod \
        | grep -v '// indirect'`
  - EXPECT: EMPTY output (every block entry is // indirect).
  - ON FAIL (e.g. a second direct dep, or mark3labs present): report the exact lines; do
        not edit go.mod.

Task 5: GATE CHECK (e) — no v1 proxy/sse/rewrite files; no dead v1 markers
  - RUN (files): `ls *.go | grep -iE 'proxy|sse|rewrite'`
  - EXPECT: empty (NONE).
  - RUN (markers): `grep -rniE 'mark3labs|mcp-go|NewSingleHostReverseProxy|transparent proxy|sse rejoin' --include=*.go .`
  - EXPECT: empty (NONE).
  - RUN (deleted-history sanity, optional): `git log --diff-filter=D --name-only --pretty=format: \
        | grep -iE 'proxy|sse|rewrite'` → should list the 9 deleted v1 files (confirms they
        WERE removed); not a pass/fail, just context.
  - ON FAIL (a v1 file or marker present): report exact file/line; do not delete (that is a
        remediation action, not a gate action) — unless the residue is an obvious oversight
        the agent is explicitly authorized to flag-only.

Task 6: GATE CHECK (f) — binary boots and GET /healthz returns 200
  - BUILD a fresh binary (Task 3 already did; reuse it or rebuild).
  - RUN (boot + probe): set PORT=18787 (or another free high port);
        `WSPF_LISTEN=127.0.0.1:$PORT /tmp/wspf-gate.bin > /tmp/wspf.stderr 2>&1 & PID=$!`
        then poll up to ~5s: `for i in $(seq 1 20); do code=$(curl -s -o /tmp/hb -w '%{http_code}' http://127.0.0.1:$PORT/healthz); [ "$code" = 200 ] && break; sleep 0.25; done`
        then capture body: `cat /tmp/hb`, then `kill $PID; wait $PID 2>/dev/null`.
  - EXPECT: `code=200`; body == `{"ok":true,"version":"dev"}`; stderr contains one JSON
        line with `"msg":"startup"` and `"tools":["web_search"]`.
  - ON FAIL (non-200, wrong body, no startup line, immediate exit): report the stderr
        startup/error line + the curl status/body; do not fix main.go/health.go.

Task 7: REPORT — assemble the gate result
  - WRITE (optional, under this work item's plan dir only):
        plan/002_0a8ab3410994/P1M5T4S3/research/gate-report.md
  - CONTENT: for each of (a)-(f): the command run, the actual output (trimmed), PASS/FAIL.
        End with a one-line verdict: "GATE PASS" iff all six are PASS, else "GATE FAIL" +
        a "Failures" section precise enough to seed a remediation work item.
  - ALSO surface the verdict in the agent's completion summary.
  - DO NOT write anywhere outside plan/002_0a8ab3410994/P1M5T4S3/research/. DO NOT touch
        any repo-surface file.

Task 8 (RE-RUN after T4.S2 lands): because T4.S2 edits concurrently, RE-RUN Tasks 1-3
  against the final committed tree to capture the definitive phase state (vet parses the
  new doc.go; test/build are unaffected by doc.go/config.example.json). Both runs are
  expected green (see Parallel Sensitivity). If the re-run is not possible in this
  session, note that the gate should be re-run post-T4.S2-merge.
```

### Implementation Patterns & Key Details

```bash
# PATTERN: every check is a shell command whose EXIT CODE + OUTPUT define pass/fail.
#   Capture both: `<cmd> 2>&1; echo "<name>_exit=$?"`. Green = exit 0 AND expected output.

# PATTERN: the single-require check has THREE complementary assertions because "exactly
#   one require" is ambiguous on its face (go.mod legitimately has a require(...) block).
#   The `go list -m` command is the authority (it knows direct vs indirect). Do not rely
#   on grep alone.

# PATTERN: boot check must (1) build fresh, (2) pick a free high port via WSPF_LISTEN,
#   (3) background, (4) POLL /healthz (do not sleep a fixed 3s — boot may be faster or
#   the box slower), (5) assert 200 + exact body, (6) assert the startup JSON line on
#   stderr, (7) kill the process and wait so the shell is clean. curl defaults to GET
#   (health.go is GET-only; a POST → 405 would false-fail).

# GOTCHA: do not exercise the committed ./web-search-prime-fixer binary — build your own
#   to /tmp and probe THAT. The committed binary is not evidence about current source.

# GOTCHA: this item changes nothing in the repo. If you find yourself editing a file,
#   STOP — you have left the gate's scope. The only allowed write is the optional report
#   under plan/002_0a8ab3410994/P1M5T4S3/research/.
```

### Integration Points

```yaml
FILES MODIFIED BY THIS ITEM: NONE (verification-only).
OPTIONAL OUTPUT (under this work item's plan dir only):
  - plan/002_0a8ab3410994/P1M5T4S3/research/gate-report.md  # per-check PASS/FAIL + verdict
FILES INSPECTED (READ-ONLY):
  - go.mod / go.sum        : single-direct-require + all-indirect-block + go mod verify.
  - *.go + *_test.go       : go vet / go test / go build / no-v1-residue grep.
  - main.go / health.go    : boot + /healthz contract (no edits; just exercise).
  - doc.go                 : parsed by `go vet ./...` (T4.S2 may have rewritten it; vets
                             clean in either comment form — see Parallel Sensitivity).
  - config.example.json    : NOT in build/test/vet graph; not exercised by this gate.
CONSUMER SEAM:
  - This gate's verdict is the Phase P1 definition-of-done (PRD §22). A "GATE PASS" closes
        the phase; a "GATE FAIL" feeds a remediation work item with the documented failure.
  - RE-RUN dependency: because T4.S2 runs in parallel, the definitive gate should be re-run
        after T4.S2 lands (vet re-parses the final doc.go). The pre-T4.S2 run is also valid
        green because doc.go vets clean in both v1/v2 forms and config.example.json is not
        compiled.
DATABASE / ROUTES / ENV / CONFIG: none added or changed. The boot check CONSUMES the
  existing WSPF_LISTEN env override (config.go) and the existing GET /healthz route
  (main.go/health.go) — it does not create them.
```

## Validation Loop

> For this item the Validation Loop IS the work: the six gate checks below are the task.
> Run them in order; each must be clean. The expected output is the definition of "clean".
> A known-green baseline for every check is in `research/gate-baseline.md`.

### Level 1: Static analysis — `go vet ./...` clean (GATE CHECK a)

```bash
go vet ./... 2>&1; echo "vet_exit=$?"
# Expected: (no output) then `vet_exit=0`. Any diagnostic or non-zero exit = FAIL.
```

### Level 2: Tests — `go test ./...` clean (GATE CHECK b)

```bash
go test -count=1 ./... 2>&1; echo "test_exit=$?"
# Expected: `ok  web-search-prime-fixer <duration>` then `test_exit=0`.
# All 10 test files must pass: config_test, dispatch_test, extract_test, health_test,
# logger_test, resolve_test, server_test, teach_test, tools_test, upstream_test.
# (The item contract names 8; tools_test.go + dispatch_test.go were added in M5.T1/T2 and
#  are covered by `./...`.) Any FAIL/PANIC or non-zero exit = FAIL.
```

### Level 3: Build — `go build` produces a binary (GATE CHECK c)

```bash
go build ./... 2>&1; echo "build_pkgs_exit=$?"                       # every package compiles
BIN=/tmp/wspf-gate-$$.bin
go build -o "$BIN" . && test -s "$BIN" && echo "binary OK ($(wc -c <"$BIN") bytes)"
# Expected: `build_pkgs_exit=0` and `binary OK (<~13_000_000 bytes)`.
# Compile error or missing/empty binary = FAIL.
```

### Level 4: Dependency invariant — exactly one DIRECT require, and it is the SDK (GATE CHECK d)

```bash
# (1) Authority: direct (non-indirect) modules, excluding the main module.
go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all | grep -v '^web-search-prime-fixer'
# Expected: EXACTLY ONE line -> `github.com/modelcontextprotocol/go-sdk v1.6.1`.

# (2) go.mod: the single-line require is the SDK.
grep -nE '^require [^(]' go.mod
# Expected: `6:require github.com/modelcontextprotocol/go-sdk v1.6.1` (one line).

# (3) The require(...) block is entirely // indirect (no stray direct dep hidden in it).
awk '/^require \(/{f=1;next} /^\)/{f=0} f' go.mod | grep -v '// indirect' || echo "block all-indirect OK"
# Expected: `block all-indirect OK` (the grep -v produced no other output).

# (4) No rejected/community SDK leaked in.
go mod verify 2>&1 | tail -1   # Expected: `all modules verified`
grep -niE 'mark3labs|mcp-go' go.mod go.sum && echo "FAIL: rejected SDK present" || echo "OK: no rejected SDK"
# Expected: `all modules verified` and `OK: no rejected SDK`.
```

### Level 5: v1-residue — no proxy/sse/rewrite files; no dead markers (GATE CHECK e)

```bash
# (1) No v1 source files on disk.
ls *.go 2>/dev/null | grep -iE 'proxy|sse|rewrite' && echo "FAIL: v1 file on disk" || echo "OK: no v1 files"
# Expected: `OK: no v1 files`.

# (2) No dead v1 markers anywhere in *.go (imports, comments, identifiers).
grep -rniE 'mark3labs|mcp-go|NewSingleHostReverseProxy|transparent proxy|sse rejoin' --include=*.go . \
  && echo "FAIL: dead v1 marker" || echo "OK: no v1 markers"
# Expected: `OK: no v1 markers`.

# (3) Sanity: the 9 v1 files were actually deleted in history (context only, not pass/fail).
git log --diff-filter=D --name-only --pretty=format: 2>/dev/null \
  | grep -iE 'proxy|sse|rewrite' | sort -u
# Expected (context): proxy.go, proxy_test.go, proxy_e2e_test.go, proxy_harness_test.go,
# proxy_log_test.go, rewrite.go, rewrite_test.go, sse.go, sse_test.go.
```

### Level 6: Runtime — binary boots and GET /healthz returns 200 (GATE CHECK f)

```bash
BIN=/tmp/wspf-gate-$$.bin
go build -o "$BIN" . || { echo "FAIL: cannot build for boot check"; exit 1; }
PORT=18787   # free high port; change if occupied
WSPF_LISTEN="127.0.0.1:$PORT" "$BIN" > /tmp/wspf-$$.stderr 2>&1 &
PID=$!
CODE=000
for i in $(seq 1 20); do
  CODE=$(curl -s -o /tmp/wspf-$$.body -w '%{http_code}' "http://127.0.0.1:$PORT/healthz" 2>/dev/null)
  [ "$CODE" = "200" ] && break
  sleep 0.25
done
echo "healthz_code=$CODE"
echo "healthz_body=$(cat /tmp/wspf-$$.body)"
echo "startup_line=$(grep -m1 '"msg":"startup"' /tmp/wspf-$$.stderr || echo MISSING)"
kill "$PID" 2>/dev/null; wait "$PID" 2>/dev/null
# Expected:
#   healthz_code=200
#   healthz_body={"ok":true,"version":"dev"}
#   startup_line={"canonical_tool":"web_search",... ,"msg":"startup",... ,"tools":["web_search"], ...}
# Non-200, wrong/empty body, no startup line, or immediate process exit = FAIL.
```

### Level 7: Report assembly (the deliverable)

```bash
# Optional: write the evidence report UNDER THIS WORK ITEM'S plan dir only.
# (The implementing agent writes this file with each check's real output + verdict.)
test -d plan/002_0a8ab3410994/P1M5T4S3/research && \
  echo "report dir exists — write gate-report.md there"
# Final verdict line in the report (and the agent summary):
#   "GATE PASS" iff (a)-(f) all PASS; else "GATE FAIL" + a Failures section.
```

## Final Validation Checklist

### Technical Validation (the six gate checks — all must PASS)
- [ ] (a) `go vet ./...` exits 0 with no output.
- [ ] (b) `go test -count=1 ./...` exits 0 with `ok  web-search-prime-fixer <duration>`.
- [ ] (c) `go build ./...` exits 0 AND `go build -o <bin> .` yields a non-empty binary.
- [ ] (d) exactly ONE direct require (`go list -m` authority) = go-sdk v1.6.1; the
      `require (...)` block is entirely `// indirect`; `go mod verify` clean; no mark3labs.
- [ ] (e) no `proxy*.go`/`sse*.go`/`rewrite*.go` on disk; no dead v1 markers in any `.go`.
- [ ] (f) fresh binary boots under `WSPF_LISTEN=127.0.0.1:<port>` and `GET /healthz`
      returns `200` + `{"ok":true,"version":"dev"}`; stderr has the startup JSON line.

### Feature Validation (definition-of-done, PRD §22)
- [ ] Runs as one Go binary built on the official Go MCP SDK (checks c + d).
- [ ] Exactly one advertised tool `web_search` (check f startup line shows `"tools":["web_search"]`;
      the server_test.go + tools_test.go suites — part of check b — prove no z.ai-branded
      names are advertised and tools/list shows one tool).
- [ ] Correct first-call behavior (canonical, alias/junk, bare/nested/array, empty,
      session-expiry) is green via the M5.T3 server_test.go suite (part of check b).
- [ ] z.ai receives a schema-valid web_search_prime/search_query call (upstream_test.go +
      server_test.go, part of check b).
- [ ] No v1 proxy/sse/rewrite residue contradicts PRD §0/§13 (check e).

### Scope / Code-Quality Validation (verification-only discipline)
- [ ] NO repo-surface file created or modified (no *.go, *_test.go, go.mod, go.sum,
      README.md, config.example.json, doc.go, PRD.md).
- [ ] The ONLY optional write is `plan/002_0a8ab3410994/P1M5T4S3/research/gate-report.md`.
- [ ] No check failure was silently patched; any failure is DOCUMENTED for remediation.
- [ ] Report's verdict line matches the per-check results.

### Documentation & Deployment
- [ ] No documentation surface change (item contract: "DOCS: none — verification-only").
- [ ] If T4.S2 has not yet landed, the report notes that the gate should be RE-RUN
      post-T4.S2-merge for the definitive phase sign-off (vet re-parses the final doc.go).

---

## Anti-Patterns to Avoid

- ❌ Don't count `require` occurrences in go.mod with grep and conclude "2 requires = fail."
  go.mod legitimately has a single-line DIRECT require (the SDK) plus a `require (...)`
  block of 6 `// indirect` entries. "Exactly one require" means one DIRECT require. Use
  `go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all` as authority.
- ❌ Don't edit any file to make a check pass. This is a GATE, not an implementation. A
  failure is DOCUMENTED, not patched. Patching masks the gate and breaks scope.
- ❌ Don't probe the committed `./web-search-prime-fixer` binary. Build a fresh one to
  `/tmp` and probe THAT — the committed binary isn't evidence about current source.
- ❌ Don't use a fixed `sleep 3` before probing /healthz. POLL the endpoint in a short loop
  (boot is usually sub-second); a fixed sleep both wastes time and can miss a fast crash.
- ❌ Don't POST to /healthz. health.go is GET-only (405 otherwise); a POST would false-fail.
- ❌ Don't boot on 127.0.0.1:8787 unconditionally. Use `WSPF_LISTEN=127.0.0.1:<high-port>`
  (e.g. 18787) to avoid colliding with a running instance or an occupied port.
- ❌ Don't treat the extra go.sum line that `go mod tidy` would trim as a failure. Extra
  go.sum entries do not affect vet/build/test and `go mod verify` passes. It is cosmetic.
- ❌ Don't treat `go.mod`'s `go 1.25.0` directive vs an installed go1.26.x as a failure —
  Go is forward-compatible. Do not "fix" the directive.
- ❌ Don't claim only 8 test files. The shipped suite is 10 (adds tools_test.go +
  dispatch_test.go). `go test ./...` covers all; the count is not extra scope.
- ❌ Don't write the report (or any file) outside
  `plan/002_0a8ab3410994/P1M5T4S3/research/`. No repo-surface writes are permitted.
- ❌ Don't skip the re-run note: if T4.S2 (doc.go/config.example.json) hasn't landed, flag
  that the definitive gate should be re-run after it does (vet re-parses the final doc.go).

---

## Confidence Score

**9/10** for one-pass success. This is a verification gate over a phase that is ALREADY
green at research time (every one of the six checks was run and passed — see
research/gate-baseline.md). The exact commands and expected "clean" output are spelled out
per check, the one ambiguous check (single require) is disambiguated with a `go list -m`
authority plus two corroborating assertions, the boot/healthz procedure (fresh build, free
high port via WSPF_LISTEN, poll loop, GET-only, kill) is fully specified, and the
verification-only scope (no edits; document failures, don't patch) is explicit. The
residual 1 point reflects: (1) the gate may need a re-run after the parallel T4.S2 lands
to capture the final phase state (low risk — doc.go vets clean in both comment forms and
config.example.json is not compiled); and (2) a transient environment issue (port
collision, network for `go mod verify`) could force a port bump — both handled by the
runbook's fallbacks.
