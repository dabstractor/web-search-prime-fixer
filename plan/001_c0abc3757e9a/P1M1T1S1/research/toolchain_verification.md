# Research Note — P1.M1.T1.S1 Toolchain Verification

On-disk verifications performed (go1.26.4-X:nodwarf5, linux/amd64, GOROOT=/usr/lib/go).

## 1. `go mod init` default directive (CONFIRMED)
`go mod init web-search-prime-fixer` produces:
```
module web-search-prime-fixer

go 1.26.4
```
The directive defaults to the INSTALLED toolchain version, NOT the PRD floor.
The task requires the directive to read `go 1.22` (conservative floor per PRD §9
"go 1.22+" and the contract). **The executing agent MUST edit the directive down
to `go 1.22` after `go mod init`.**

## 2. `go 1.22` directive does NOT trigger a `toolchain` line (CONFIRMED)
With `GOTOOLCHAIN=auto` (default), manually writing `go 1.22` in go.mod and
running `go build ./...` / `go vet ./...`:
- Succeeds with NO output (clean).
- Does NOT auto-add a `toolchain go1.26.4` line.
- go.mod remains exactly:
```
module web-search-prime-fixer

go 1.22
```
Rationale: installed toolchain (1.26.4) >= required floor (1.22), so Go uses the
local toolchain and records no `toolchain` directive. Safe and stable.

## 3. Empty-module validation commands (CONFIRMED, stdlib-only)
With only `main.go` (`package main` + `func main(){}`):
- `go vet ./...`   → no output, exit 0.
- `go build ./...`  → no output, exit 0.
- `go test ./...`   → `?       web-search-prime-fixer   [no test files]`, exit 0.

These match PRD §18 verbatim.

## 4. PRD §9 file layout (CONFIRMED, lines 223 & 229 of PRD.md)
- `go.mod   module web-search-prime-fixer; go 1.22+`
- `doc.go   package comment`
No exact package-comment wording is prescribed by the PRD → derive from §1/§2.
Package is `main`, so the comment MUST begin `// Package main`.

## 5. testdata/ directory
PRD §9 does not list `testdata/` explicitly in the file-layout block, but
system_context.md "Files to create" lists `testdata/initialize.sse` and
`testdata/tools_call.sse` (golden fixtures, populated by P1.M3.T2). Go treats any
directory named `testdata` as ignored by the build toolchain — it is the correct
place for fixtures. Git does not track empty dirs; a `.gitkeep` keeps the
directory in version control until fixtures land.
