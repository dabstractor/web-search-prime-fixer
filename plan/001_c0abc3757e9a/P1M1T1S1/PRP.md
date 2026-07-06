# PRP — P1.M1.T1.S1: go.mod + doc.go + package skeleton

## Goal

**Feature Goal**: Establish the root of the dependency tree for the
`web-search-prime-fixer` project: a compiling, `go vet`-clean Go module named
`web-search-prime-fixer` (package `main`, stdlib-only, `go 1.22` floor) that
every subsequent subtask (config.go, logger, bootstrap, rewrite, sse, proxy,
tests) builds on top of.

**Deliverable**: Four artifacts at the repository root (`/home/dustin/projects/web-search-prime-fixer`):
1. `go.mod` — `module web-search-prime-fixer`, `go 1.22`, **zero** `require` entries.
2. `doc.go` — the package comment (`// Package main ...`) per PRD §9.
3. `main.go` — minimal stub: `package main` + `func main() {}` (populated later by P1.M1.T4).
4. `testdata/` — directory present (plus a `.gitkeep` so git tracks it until P1.M3.T2 adds golden fixtures).

**Success Definition**: `go build ./...`, `go vet ./...`, and `go test ./...` all
exit 0 with no errors. `go.mod` contains no `require` block and no `toolchain`
line. `doc.go` carries the package comment. Nothing beyond these four artifacts
is created (no config.go, no server code, no logger — those are later subtasks).

## Why

- This is the single root of the dependency tree (per task contract "INPUT: None").
  Every later file is compiled as part of `package main` in this module.
- Establishes the stdlib-only invariant (PRD §6, §9) at the module boundary so no
  subtask can accidentally introduce a third-party dependency.
- Pins the language floor to the conservative `go 1.22` (PRD §9 "go 1.22+") rather
  than the installed toolchain version, maximizing portability.
- Produces the `// Package main` package comment required by PRD §9, which is the
  "DOCS" deliverable for this subtask ([Mode A] per the contract).

## What

A brand-new Go module is initialized at the repo root. The default `go` directive
emitted by the installed toolchain (`go 1.26.4`) is overridden down to `go 1.22`.
A `doc.go` file carries the package comment. A minimal `main.go` exists only so
that `go build ./...` / `go vet ./...` succeed against a non-empty package (a
module with zero `.go` files is not a buildable package). `main()` is left empty
on purpose — its real body (config load, server bootstrap, graceful shutdown) is
P1.M1.T4 and must NOT be implemented here.

### Success Criteria

- [ ] `go.mod` exists, reads `module web-search-prime-fixer`, then `go 1.22`, with NO `require` block and NO `toolchain` line.
- [ ] `doc.go` exists, is `package main`, and has a `// Package main ...` comment (godoc-visible).
- [ ] `main.go` exists, is `package main`, contains `func main() {}` and nothing else of substance.
- [ ] `testdata/` directory exists at repo root (with a `.gitkeep` placeholder).
- [ ] `go build ./...` exits 0 with no output.
- [ ] `go vet ./...` exits 0 with no output.
- [ ] `go test ./...` exits 0 (prints `?  web-search-prime-fixer  [no test files]`).
- [ ] No files outside this subtask's scope are created (no config.go, proxy.go, README.md, config.example.json, etc.).

## All Needed Context

### Context Completeness Check

_Pass._ This is a greenfield subtask with no codebase predecessors. Every file's
exact content, naming, and placement is specified below. The only non-obvious
detail (the `go` directive defaulting to the installed version instead of the
floor, and the question of an auto-added `toolchain` line) is **verified** in
`research/toolchain_verification.md` and called out in the gotchas.

### Documentation & References

```yaml
# MUST READ — authoritatively defines module name, language floor, and file layout.
- file: PRD.md
  why: §9 "File layout" (go.mod = "module web-search-prime-fixer; go 1.22+";
        doc.go = "package comment") and §6 "Non-functional requirements"
        ("Go, stdlib only", "Single binary, no external runtime dependencies").
  critical: The module is stdlib-only. ZERO requires in go.mod. Language floor is
        go 1.22 (NOT the installed 1.26.4).

- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: Confirms greenfield state, installed toolchain go1.26.4 satisfies the floor,
        and that NO third-party deps are allowed. Lists every file to be created
        across the whole project (so this subtask knows what NOT to create yet).
  critical: "There is no Go source, no go.mod/go.sum... Every source file listed
        in PRD §9 must be created from scratch." This subtask creates only
        go.mod / doc.go / main.go stub / testdata/.

- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  why: §2 confirms the stdlib-only invariant is buildable (io.Copy, http.Flusher
        all in /usr/lib/go/src). §4 confirms a manual http.Handler (not
        httputil.ReverseProxy) is the right architecture for LATER subtasks.
  critical: No action needed THIS subtask; cited so the executing agent trusts
        the "stdlib only" constraint is genuinely sufficient downstream.

- file: plan/001_c0abc3757e9a/P1M1T1S1/research/toolchain_verification.md
  why: On-disk proof that `go 1.22` directive does not trigger an auto `toolchain`
        line under default GOTOOLCHAIN=auto, and that build/vet/test all pass clean.
  critical: After `go mod init`, the directive reads `go 1.26.4`; you MUST edit it
        to `go 1.22`. Editing it down is safe (verified).

- url: https://go.dev/doc/modules/gomod-ref
  why: go.mod file reference — the `go` directive semantics and that omitting a
        `require` block is valid (a module may have zero dependencies).
  critical: A go.mod with only `module` + `go` directives (no `require`,
        `go.sum`) is a legal, buildable module for a stdlib-only program.

- url: https://go.dev/doc/comment
  why: Go doc-comment conventions — package comments go on a file in the package,
        begin with "Package <name> ...", and doc.go is the conventional home for
        the package comment.
  critical: The package is `main`. Per convention the comment still begins
        "Package main" (even though `main` is a command). This satisfies PRD §9
        "doc.go: package comment".
```

### Current Codebase tree

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  .git/                 # single commit, branch main
  .gitignore            # ignores node_modules/, dist/, .env*, .pi-subagents/ (does NOT ignore testdata/)
  PRD.md                # 581-line design doc
  plan/
    001_c0abc3757e9a/
      architecture/     # system_context.md, external_deps.md (READ-ONLY research)
      prd_snapshot.md   # READ-ONLY
      prd_index.txt     # READ-ONLY
      tasks.json        # READ-ONLY (orchestrator-owned)
      P1M1T1S1/         # THIS subtask's dir (PRP.md + research/)
  .pi-subagents/        # gitignored agent artifacts
# NOTE: No go.mod, no go.sum, no *.go files, no testdata/ yet. Greenfield.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.22; NO requires, NO toolchain
  doc.go          # package main + "// Package main ..." comment (PRD §9 deliverable)
  main.go         # package main + func main() {}   (STUB — P1.M1.T4 fills it in)
  testdata/
    .gitkeep      # placeholder so the empty dir is tracked; P1.M3.T2 adds *.sse fixtures
  PRD.md          # unchanged
  plan/...        # unchanged
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: `go mod init` writes the INSTALLED toolchain version as the `go`
// directive, not the floor. On this machine that is `go 1.26.4`. The contract
// and PRD §9 require `go 1.22`. After `go mod init`, EDIT the directive to
// `go 1.22`. (Verified: GOTOOLCHAIN=auto does NOT then add a `toolchain` line;
// see research/toolchain_verification.md.)

// CRITICAL: Do NOT add a `require` block or a go.sum. PRD §6/§9 mandate stdlib
// only. A go.mod with just `module` + `go` directives is legal for a dependency-
// free program.

// GOTCHA: A module with ZERO .go files is not a buildable package —
// `go build ./...` prints "no Go files in <dir>". That is why a minimal main.go
// stub is required NOW even though P1.M1.T4 later populates main().

// GOTCHA: git does not track empty directories. testdata/ with no files would
// vanish on the next clone. Add testdata/.gitkeep so the directory is committed.
// (testdata/ is also special-cased by the Go toolchain: it is never compiled.)

// CONVENTION: For package `main` the package comment still begins "Package main"
// (https://go.dev/doc/comment). doc.go is the conventional home for it.

// SCOPE GUARD: Do NOT create config.go, proxy.go, rewrite.go, sse.go,
// config.example.json, README.md, or any *_test.go in this subtask. Those are
// P1.M1.T2+ . Creating them now violates the scope boundary and will collide
// with later subtasks.
```

## Implementation Blueprint

### Data models and structure

None. This subtask introduces no types, no structs, no exported symbols. It only
establishes the module and the `package main` compilation unit.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: CREATE go.mod
  - COMMAND: run `go mod init web-search-prime-fixer` in the repo root
    (/home/dustin/projects/web-search-prime-fixer).
  - THEN EDIT the resulting go.mod: change the `go <version>` line from the
    toolchain default (`go 1.26.4`) to exactly `go 1.22`.
  - RESULTING CONTENT (exactly, nothing else):
        module web-search-prime-fixer

        go 1.22
  - VERIFY: no `require` block, no `toolchain` line, no go.sum created.
  - WHY: PRD §9 ("go 1.22+") + contract; conservative floor maximizes portability.
  - GOTCHA: see Known Gotchas — default directive must be overridden.

Task 2: CREATE doc.go
  - IMPLEMENT: `package main` with a `// Package main ...` godoc comment.
  - FOLLOW: https://go.dev/doc/comment — package comment lives on a file in the
    package; doc.go is the conventional home (PRD §9 lists doc.go for this).
  - RECOMMENDED CONTENT (wording may be refined, but MUST begin "Package main"
    and accurately describe the program per PRD §1/§2):
        // Package main implements web-search-prime-fixer, a local MCP
        // alias-fixing proxy. It is a transparent reverse proxy for the z.ai
        // web-search-prime MCP endpoint that rewrites commonly misspelled
        // tools/call argument aliases (e.g. "query") to the canonical
        // "search_query" parameter and injects a one-line SSE warning when a
        // rewrite is applied. Every other request passes through unchanged.
        //
        // The proxy binds to 127.0.0.1 (local only) and uses the Go standard
        // library exclusively.
        package main
  - PLACEMENT: repo root (/home/dustin/projects/web-search-prime-fixer/doc.go).
  - WHY: This file IS the "DOCS" deliverable for the subtask (Mode A, PRD §9).

Task 3: CREATE main.go (STUB ONLY)
  - IMPLEMENT: `package main` + `func main() {}`. NOTHING ELSE.
  - CONTENT (exactly):
        package main

        func main() {}
  - CONSTRAINT: Do NOT add server code, flag parsing, config loading, signal
    handling, or a version string. That is P1.M1.T4. Adding it now is an
    anti-pattern that breaks the scope boundary.
  - PLACEMENT: repo root (/home/dustin/projects/web-search-prime-fixer/main.go).
  - WHY: A module needs at least one .go file in package main for `go build ./...`
    and `go vet ./...` to succeed (a zero-file module is not a buildable package).

Task 4: CREATE testdata/ directory
  - CREATE directory: /home/dustin/projects/web-search-prime-fixer/testdata/
  - CREATE file: testdata/.gitkeep  (empty placeholder so the dir is tracked by git).
  - WHY: PRD §9 / system_context.md list testdata/{initialize,tools_call}.sse as
    golden fixtures for P1.M3.T2. Establishing the directory now matches the
    contract ("Create testdata/ directory"). The Go toolchain ignores any
    directory literally named testdata, so it is safe from day one.
  - GOTCHA: empty dirs are not tracked by git → that is what .gitkeep fixes.

Task 5: VALIDATE
  - RUN (exact commands, PRD §18):
        go build ./...
        go vet ./...
        go test ./...
  - EXPECT:
        go build ./...  → (no output, exit 0)
        go vet ./...    → (no output, exit 0)
        go test ./...   → "?       web-search-prime-fixer  [no test files]"  exit 0
  - RE-RUN after any edit until all three are clean.
```

### Implementation Patterns & Key Details

```go
// There is essentially no logic in this subtask. The two non-trivial points:

// (1) The go.mod directive is the conservative floor, not the installed version.
//     After `go mod init`, manually set it:
//         module web-search-prime-fixer
//
//         go 1.22
//     (Verified: GOTOOLCHAIN=auto will not rewrite this or inject a toolchain line.)

// (2) The package comment belongs on doc.go and must read "Package main ...".
//     Even though `main` is a command package, Go convention (and godoc) expect
//     the "Package <name>" prefix.

// main.go is deliberately a stub. Resist the urge to start the HTTP server here.
package main

func main() {}
```

### Integration Points

```yaml
MODULE:
  - name: "web-search-prime-fixer"   # consumed by `go build`, imports in later files
  - directive: "go 1.22"             # PRD §9 floor; NOT the installed 1.26.4
  - dependencies: "NONE"             # zero require entries; stdlib only (PRD §6/§9)

PACKAGE:
  - name: "main"                     # single binary; every later .go file is package main

FILE LAYOUT (this subtask owns the first 4 entries of PRD §9):
  - go.mod:   module + go directive only
  - doc.go:   package comment
  - main.go:  func main() stub   (→ P1.M1.T4 populates)
  - testdata/: directory + .gitkeep   (→ P1.M3.T2 adds *.sse golden fixtures)

NO INTEGRATION POINTS YET:
  - No config, no routes, no DB, no env vars, no logger. All are later subtasks.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# gofmt is the canonical formatter (PRD mandates stdlib toolchain; no external linter).
gofmt -l .                 # list files needing formatting; MUST be empty
go vet ./...               # static checks; MUST print nothing and exit 0

# go.mod sanity (grep-based assertions):
grep -E '^module web-search-prime-fixer$' go.mod            # MUST match
grep -E '^go 1.22$' go.mod                                  # MUST match (NOT 1.26.x)
! grep -qE '^(require|toolchain) ' go.mod                   # MUST be absent (stdlib only)
test ! -f go.sum                                            # MUST not exist (no deps)

# Expected: all greps match, the negated grep/`! -f` hold, gofmt prints nothing.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...
# Expected: "?       web-search-prime-fixer   [no test files]"  (exit 0)
# There are intentionally no tests in this subtask; the first tests land in P1.M2.T1.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Build the binary (PRD §18 build command):
go build ./...                                 # MUST exit 0, no output
go build -o /tmp/wspf-smoke .                  # produces a binary; smoke-build only
/tmp/wspf-smoke; echo "exit=$?"                # main() is empty → exits 0 immediately
rm -f /tmp/wspf-smoke

# Expected: binary builds; running it exits 0 with no output (it does nothing yet —
# the server is wired up in P1.M1.T4).
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# godoc / package-comment check (the §9 "DOCS" deliverable):
go doc . 2>/dev/null | head -20
# Expected: prints the "Package main implements web-search-prime-fixer ..." comment.

# Dependency-free invariant (PRD §6/§9) — list all transitive modules of the build:
go list -deps ./... | grep -v -E '^(internal/|runtime/|sync|errors|unicode|unsafe|syscall|io|os|fmt|strings|strconv|bytes|math|time|sort|context|path|reflect|regexp|bufio|log|net|crypto|hash|encoding|image|index|debug|database|archive|compress|container|plugin|text|go/|cmd/|vendor/|testing)' || true
# Expected: empty (or only stdlib paths). NO third-party module paths appear.
# (This is a belt-and-suspenders check; with zero requires it is trivially true.)

# Directory structure check:
test -f go.mod && test -f doc.go && test -f main.go && test -d testdata && test -f testdata/.gitkeep
# Expected: the chained `test` exits 0 (all artifacts present).
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0, no output.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` exits 0 (`[no test files]`).
- [ ] `go.mod` has `module web-search-prime-fixer` and `go 1.22`, no `require`, no `toolchain`.
- [ ] No `go.sum` file exists.

### Feature Validation

- [ ] `go doc .` shows the `// Package main implements web-search-prime-fixer ...` comment (the §9 DOCS deliverable).
- [ ] `go list -deps ./...` shows stdlib only — no third-party modules.
- [ ] `go.mod`, `doc.go`, `main.go`, `testdata/.gitkeep` all present at repo root.
- [ ] `main.go` contains ONLY `package main` and an empty `func main() {}` (no server/config/log code).

### Code Quality Validation

- [ ] Follows PRD §9 file layout exactly (names, placement).
- [ ] The conservative language floor (`go 1.22`) is honored, not the installed `go 1.26.4`.
- [ ] Stdlib-only invariant is established at the module boundary (zero requires).
- [ ] Scope boundary respected: no config.go / proxy.go / rewrite.go / sse.go / README.md / config.example.json / *_test.go created.

### Documentation & Deployment

- [ ] `doc.go` package comment is accurate to PRD §1/§2 and begins with "Package main".
- [ ] No environment variables introduced (none are needed at this layer).
- [ ] No logs expected (no logger exists yet — that is P1.M1.T3).

---

## Anti-Patterns to Avoid

- ❌ Don't leave the `go` directive at the `go mod init` default (`go 1.26.4`) — override it to `go 1.22`.
- ❌ Don't add a `require` block, a `go.sum`, or any third-party import — PRD §6/§9 forbid it.
- ❌ Don't populate `main()` with server/config/shutdown logic — that is P1.M1.T4; this subtask ships a stub.
- ❌ Don't create any other PRD §9 file (config.go, proxy.go, rewrite.go, sse.go, README.md, config.example.json, tests) — out of scope and will collide with later subtasks.
- ❌ Don't skip `testdata/` or leave it untracked (git drops empty dirs) — create it with a `.gitkeep`.
- ❌ Don't run `go get` for anything — there is nothing to fetch; stdlib only.
- ❌ Don't add a `toolchain` directive by hand or accept an auto-added one — verified it is not added under `go 1.22` + default GOTOOLCHAIN.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The subtask is small and the exact contents of every file are
specified verbatim. The only non-obvious step (editing the `go` directive from
the toolchain default `1.26.4` down to the floor `1.22`, and confirming no
`toolchain` line appears) is verified on-disk in
`research/toolchain_verification.md`. The remaining risk is scope creep (an agent
"helpfully" starting the HTTP server or config loader early); the SCOPE GUARD
and Anti-Patterns sections make the boundary explicit. The -1 reserves for an
agent ignoring the scope guard or the directive override.
