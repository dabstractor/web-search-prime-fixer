# Research: P1.M5.T2.S1 — Final Quality Gate (go vet + go test + build + version injection)

This is a **verification/gate** task (item contract = Mode A, no docs). The
deliverable is NOT new code — it is a clean, building, fully-tested, stdlib-only
binary plus a verification record. Research therefore centered on (a) the exact
on-disk baseline the gate will see, (b) the version-injection mechanism, (c) the
stdlib-only audit method, and (d) a failure-triage matrix so the gate can "fix any
lint/test failures discovered" rather than merely report them.

---

## 1. Baseline at research time (the exact state the gate encounters)

The parallel task **P1.M5.T1.S2** (`proxy_e2e_test.go`, 5 `TestE2E_*` funcs) has
ALREADY landed on disk (`?? proxy_e2e_test.go` in `git status`). So the gate runs
against the true end-state — including the five §19.3 cases.

Verified clean, no cache (`go clean -testcache` first):

| Gate | Command | Result |
|------|---------|--------|
| vet | `go vet ./...` | exit 0, no output |
| test | `go test ./...` | `ok  web-search-prime-fixer  0.070s`, exit 0 |
| e2e | `go test -run 'TestE2E_' -v` | 5/5 PASS |
| fmt | `gofmt -l .` | empty (clean) |
| build | `go build -o web-search-prime-fixer .` | exit 0, 9.9 MB binary |
| ldflags | `go build -ldflags "-X main.version=0.1.0"` | exit 0; `/healthz` → `{"ok":true,"version":"0.1.0"}` |
| default ver | (no ldflags) | `/healthz` → `{"ok":true,"version":"dev"}` |

**Test inventory:** 10 `*_test.go` files, 71 `func Test*` total:
config(4) health(5) logger(8) proxy_e2e(5) proxy_harness(2) proxy_log(3)
proxy_test(15) resolve(8) rewrite(6) sse(15) = 71.

**Conclusion:** the gate is expected to pass on the first run. The PRP's
value-add is the import audit + the runtime version-injection check + a triage
matrix for the "fix any failures" clause if the parallel file drifts.

## 2. Version-injection mechanism (PRD §16)

`main.go` lines ~133-140:
```go
// version is the proxy's build version, surfaced by GET /healthz. It defaults to
// "dev" and is overridden at link time:
//   go build -ldflags "-X main.version=<value>" -o web-search-prime-fixer .
// It MUST be a package-level var (not local to main) for the linker -X flag to set it.
var version = "dev"
```
`healthHandler` (main.go ~143) marshals `{"ok":true,"version":version}`.

**Verified at runtime:** `go build -ldflags "-X main.version=0.1.0"` → run on an
alt port → `curl /healthz` returns `{"ok":true,"version":"0.1.0"}`. Default build
returns `version:"dev"`. **The -X flag MUST target `main.version`** (package-level
var in package main) — a local in `main()` would NOT be settable. This is already
correct on disk; the gate re-confirms it.

**Gotcha for the runtime check:** a previously-started binary keeps `127.0.0.1:8787`
bound. To verify the versioned binary without a port clash, run it on an alternate
port via `WSPF_LISTEN=127.0.0.1:18787 ./web-search-prime-fixer &` then curl that
port. Do NOT use `pkill -f wspf` — that pattern matches the shell's own command
line and kills the shell. Kill by saved PID.

## 3. Stdlib-only audit (PRD §6/§9/§20)

Three independent confirmations, all green on disk:

1. **`go list -m all`** prints ONLY `web-search-prime-fixer` (the module itself) —
   zero requires in `go.mod` → no dependency modules exist.
2. **`go list -deps .`** (transitive stdlib closure) lists only stdlib packages
   (`runtime`, `net/http`, `encoding/json`, `bytes`, …). No `github.com/…`,
   `golang.org/x/…`, or other module-prefixed path.
3. **`grep` import blocks** in the 6 production files (`main.go config.go proxy.go
   rewrite.go sse.go doc.go`) → every import is a stdlib path.

`go.sum` does NOT exist (correct — it is only written when `go.mod` has requires).
`.gitignore` already ignores `/web-search-prime-fixer` (the build artifact) and
`.pi-subagents/`.

## 4. The two build commands (PRD §18 + item contract)

The contract names two distinct builds:
1. `go build -o web-search-prime-fixer .` — explicit `-o web-search-prime-fixer`
   (matches PRD §18 exactly). Produces the runnable binary.
2. `go build -ldflags "-X main.version=0.1.0"` — NO `-o`, so Go derives the output
   name from the module name → also `./web-search-prime-fixer` (overwrites #1).
   This is the version-injection proof; verified by running it and curling
   `/healthz` for `version:"0.1.0"`.

Note: the item contract literally writes the second command without `-o`; the PRP
keeps both forms (the explicit `-o` build for the runnable artifact; the `-ldflags`
build for the version proof) and notes the output-name derivation.

## 5. Failure-triage matrix (for the "fix any failures" clause)

If a gate FAILS at run time, the most likely root causes and fixes:

| Symptom | Likely cause | Fix |
|---------|--------------|-----|
| `go vet`: `imported and not used` / `declared and not used` | stray import/local in `proxy_e2e_test.go` (parallel file) | remove the unused symbol; re-run `go vet` |
| `go vet`: `redeclared in this block` | a `TestE2E_*` or reused helper name collides with a seed symbol | rename the new symbol (do NOT edit seed/harness files) |
| `go test`: `proxy_e2e_test.go` case fails | parallel-file bug OR a production regression it exposes | read the failing assertion; if it's a production bug (e.g. redaction, session forwarding), fix the prod file; if a test bug, fix the test |
| `go test`: pre-existing test regresses | the new file shadowed a seed helper | revert the new file's collision; do not touch `proxy_test.go`/`proxy_harness_test.go` |
| `gofmt -l` non-empty | a file is unformatted | `gofmt -w <file>` (single command, safe) |
| `go build` fails (compile err) | syntax/type error in any `.go` | the compiler names the file:line; fix there |
| ldflags build ok BUT `/healthz` shows `dev` | `version` is not a package-level var, or `-X` target wrong | ensure `var version = "dev"` is package-level in `package main` and the flag is `-X main.version=...` |
| `go list -m all` shows a require | a third-party import leaked in | find it via `go list -deps .` / grep import blocks; replace with stdlib; the PRD forbids external deps |

All of the above are currently GREEN; the matrix exists so the gate can fix
rather than stall. **Scope guard:** this item fixes lint/test/build/vet/format
defects and import leaks ONLY. It does NOT change product behavior, add features,
or touch `go.mod` to add a dependency (that would violate PRD §6/§9). If a failure
reveals a genuine design defect beyond a fix, stop and report rather than
expanding scope.

## 6. Scope boundary vs. neighbours

- **Consumes** P1.M1–P1.M5.T1 (all production + tests) and the parallel
  `proxy_e2e_test.go` (P1.M5.T1.S2) as its input.
- **Does NOT** write README/docs/config.example.json (P1.M5.T3 owns those).
- **Does NOT** modify `go.mod`/`PRD.md`/`tasks.json`.
- **May** fix lint/vet/test/format defects or import leaks discovered — that is
  the explicit "Fix any lint/test failures discovered" clause.
- **Output artifact:** the binary `./web-search-prime-fixer` (built both ways) +
  a verification record (this gate's command outputs).
