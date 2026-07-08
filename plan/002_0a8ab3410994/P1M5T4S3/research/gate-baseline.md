# Gate Baseline — evidence captured at research time (2026-07-08)

This file records the CURRENT state of every check P1.M5.T4.S3 must verify, so the
implementing agent has a known-good baseline to reproduce and a reference for what
"clean" looks like. All evidence below was captured by running the actual commands.

## Environment
- Repo root: `/home/dustin/projects/web-search-prime-fixer`
- Installed toolchain: `go version go1.26.4-X:nodwarf5 linux/amd64`
- `go.mod` declares `go 1.25.0` — forward-compatible with the installed 1.26.x; NOT a gate issue.

## Gate 1 — `go vet ./...`
- Result: **CLEAN**, exit 0, no output.

## Gate 2 — `go test ./...`
- Result: **CLEAN**, exit 0.
- Output: `ok  	web-search-prime-fixer	0.054s`
- Test files on disk (10 total — `go test ./...` runs all):
  `config_test.go, dispatch_test.go, extract_test.go, health_test.go, logger_test.go,
   resolve_test.go, server_test.go, teach_test.go, tools_test.go, upstream_test.go`
- NOTE: the item contract names 8 test files (extract, teach, upstream, server, config,
  resolve, health, logger). The shipped repo adds **2 more** from M5.T1/M5.T2:
  `tools_test.go` (tools.go) and `dispatch_test.go` (server.go dispatch). `go test ./...`
  covers all 10; all must pass.

## Gate 3 — `go build`
- `go build ./...` → exit 0 (all packages compile).
- `go build -o /tmp/wspf-gate .` → produces a ~13 MB binary. **CLEAN.**

## Gate 4 — single direct require (the subtle one)
`go.mod` has TWO `require` stanzas:
1. A single-line DIRECT require (line 6): `require github.com/modelcontextprotocol/go-sdk v1.6.1`
2. A block `require (...)` (lines 8–15) containing ONLY `// indirect` entries (6 of them).

"Exactly one require" in the contract means **exactly one DIRECT (non-indirect) require,
and it is the SDK.** The indirect block is EXPECTED (transitive deps of the SDK) and is
NOT a violation. Authoritative check:
```
go list -m -f '{{if not .Indirect}}{{.Path}} {{.Version}}{{end}}' all | grep -v '^web-search-prime-fixer'
```
→ prints exactly one line: `github.com/modelcontextprotocol/go-sdk v1.6.1`.

Indirect deps in the block (all legitimate, pulled by the SDK):
- github.com/google/jsonschema-go v0.4.3
- github.com/segmentio/asm v1.1.3
- github.com/segmentio/encoding v0.5.4
- github.com/yosida95/uritemplate/v3 v3.0.2
- golang.org/x/oauth2 v0.35.0
- golang.org/x/sys v0.41.0

`go mod verify` → "all modules verified". No mark3labs/mcp-go present (rejected SDK).

## Gate 5 — no v1 proxy/sse/rewrite files remain
- On disk: `ls *.go | grep -iE 'proxy|sse|rewrite'` → NONE.
- Dead-marker grep: `grep -rniE 'mark3labs|mcp-go|NewSingleHostReverseProxy|transparent proxy|sse rejoin' --include=*.go .` → NONE.
- git history shows these v1 files were DELETED:
  `proxy.go, proxy_test.go, proxy_e2e_test.go, proxy_harness_test.go, proxy_log_test.go,
   rewrite.go, rewrite_test.go, sse.go, sse_test.go` (9 files).

## Gate 6 — binary boots + /healthz returns 200
- `WSPF_LISTEN=127.0.0.1:18787 /tmp/wspf-gate` boots.
- `curl http://127.0.0.1:18787/healthz` → `HTTP/1.1 200 OK`,
  `Content-Type: application/json`, body `{"ok":true,"version":"dev"}`.
- stderr emits ONE structured JSON startup line on boot:
  `{"canonical_tool":"web_search","level":"info","listen":"127.0.0.1:18787",
   "log_level":"info","msg":"startup","query_aliases":[14 entries],"tools":["web_search"],...}`

## OPTIONAL housekeeping (NOT a gate failure)
- `go mod tidy` trims ONE unused entry from `go.sum`:
  `cloud.google.com/go/compute/metadata v0.3.0/go.mod ...` (a stale `/go.mod`-only hash).
  This does NOT affect vet/build/test (extra go.sum lines are harmless) and does NOT touch
  `go.mod`. It is cosmetic. The gate does NOT require running `go mod tidy`; if the agent
  wants a tidy go.sum as a bonus it may run it, but it must not be reported as a failure.

## Parallel-execution sensitivity (vs P1.M5.T4.S2)
T4.S2 rewrites `doc.go` (comment-only, package main) and `config.example.json` (not
compiled) concurrently. The gate is INSENSITIVE to T4.S2's timing because:
- doc.go vets clean in BOTH its v1 and v2 comment forms (both are valid Go); `go vet ./...`
  parses it either way. (T4.S2's PRP guarantees gofmt+vet clean on its output too.)
- config.example.json is not in the build/test/vet graph.
→ The gate may be run now AND should be RE-RUN after T4.S2 lands to capture the final
phase state. Both runs are expected green.
