# Research — Adding the MCP SDK dependency (verified on-disk)

## The contract claim under test
The item contract says: "SDK v1.6.1 is in the local module cache so `go mod tidy`
needs no network." This research **verified that claim on-disk** and found it
**FALSE**. The SDK module itself is cached, but its transitive dependencies are
not. The network IS available and IS required. The PRP is written to the verified
reality, not the contract's optimistic claim.

## Environment
- Installed toolchain: `go1.26.4-X:nodwarf5 linux/amd64` (`GOROOT=/usr/lib/go`,
  `GOMODCACHE=/home/dustin/go/pkg/mod`, `GOPROXY=https://proxy.golang.org,direct`,
  `GOTOOLCHAIN=auto`).
- Repo `go.mod` today: `module web-search-prime-fixer` + `go 1.22`, zero requires.
- SDK module cache: `github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/` **PRESENT**
  (mcp/*.go source readable).

## Finding 1 — the SDK's own go.mod requires `go 1.25.0`
```
module github.com/modelcontextprotocol/go-sdk
go 1.25.0
require (
  github.com/golang-jwt/jwt/v5 v5.3.1
  github.com/google/go-cmp v0.7.0
  github.com/google/jsonschema-go v0.4.3
  github.com/segmentio/encoding v0.5.4
  github.com/yosida95/uritemplate/v3 v3.0.2
  golang.org/x/oauth2 v0.35.0
  golang.org/x/tools v0.42.0
)
require (
  github.com/segmentio/asm v1.1.3 // indirect
  golang.org/x/sys v0.41.0 // indirect
)
```
**Consequence (VERIFIED):** `go get github.com/modelcontextprotocol/go-sdk@v1.6.1`
on a `go 1.22` module prints `go: upgraded go 1.22 => 1.25.0` and rewrites the
directive to `go 1.25.0`. This is **unavoidable** and **correct** — the installed
go1.26.4 satisfies `go 1.25.0`, no `toolchain` line is added (verified). The
implementer MUST accept the bump and MUST NOT revert to `go 1.22` (that breaks the
SDK build). Prior PRPs (plan/001, plan/002 T1) chose `go 1.22` for portability;
adopting the SDK (PRD §13) relinquishes that, which PRD §13 explicitly anticipated
("the original 'build fully offline / no go.sum' purity is explicitly relinquished").

## Finding 2 — transitive deps are NOT in the cache; network IS required
Checked `$GOMODCACHE/<dep>@<ver>` for every SDK dependency:
```
MISSING  github.com/google/jsonschema-go@v0.4.3
MISSING  github.com/yosida95/uritemplate/v3@v3.0.2
MISSING  golang.org/x/oauth2@v0.35.0
MISSING  github.com/segmentio/encoding@v0.5.4
MISSING  github.com/segmentio/asm@v1.1.3
MISSING  golang.org/x/sys@v0.41.0
MISSING  golang.org/x/tools@v0.42.0
PRESENT  github.com/golang-jwt/jwt/v5@v5.3.1
PRESENT  github.com/google/go-cmp@v0.7.0
```
`GOPROXY=off go mod tidy` FAILS with "module lookup disabled by GOPROXY=off" for
jsonschema-go / uritemplate / oauth2 / segmentio (the `mcp` package imports them).
So `go mod tidy` **MUST run with network** (default GOPROXY). Reachability check:
`curl -sI https://proxy.golang.org/.../v0.4.3.info` → `HTTP/2 200` (reachable).

## Finding 3 — VERIFIED happy path (the sequence the PRP prescribes)
Temp module `web-search-prime-fixer` + `go 1.22` + `import "github.com/modelcontextprotocol/go-sdk/mcp"`:
```
go get github.com/modelcontextprotocol/go-sdk@v1.6.1   # -> "upgraded go 1.22 => 1.25.0", adds require
go mod tidy                                             # downloads the 7 transitive deps, writes go.sum
go build ./...                                          # exit 0
go vet ./...                                            # exit 0
```
Final `go.mod` (note: NO `toolchain` line; `go mod tidy` prunes jwt/go-cmp/x/tools
to `// indirect`-omitted because they are not on the `mcp.NewServer` import path):
```
module web-search-prime-fixer

go 1.25.0

require github.com/modelcontextprotocol/go-sdk v1.6.1

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
)
```
`go.sum` = 20 lines (h1 + go.mod hashes for SDK + 7 deps + pruned). The exact
`// indirect` set is `go mod tidy`'s decision; the implementer should NOT hand-edit
indirect requires — just run `go get` + `go mod tidy` and commit the result.

## Finding 4 — Mode-A go.mod comment placement
`go mod tidy` preserves `//` comments adjacent to require lines but the robust
play is to add the one-line SDK comment **AFTER** `go mod tidy` completes (as the
final go.mod edit), so tidy cannot reflow or drop it. Place it directly above the
`require github.com/modelcontextprotocol/go-sdk v1.6.1` line.

## Net implications for the PRP
1. Prescribe `go get github.com/modelcontextprotocol/go-sdk@v1.6.1` then
   `go mod tidy` (default GOPROXY — network needed, network available). **Do not**
   use `GOPROXY=off`.
2. Accept the `go 1.22` → `go 1.25.0` directive bump. Do not revert.
3. A `go.sum` IS created (~20 lines) — PRD §13 explicitly relinquished the
   "no go.sum" purity, so this is expected and fine.
4. The SDK build (Gate D) is validated in an isolated temp module because the real
   package is still red (proxy.go/tests reference the deleted v1 `cfg.Aliases` —
   fixed by T2.S2/T3.S1, out of scope here). This mirrors the T1.S2 isolated-gate
   precedent.
