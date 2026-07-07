# Research Notes — P1.M5.T3.S2 (config.example.json + doc.go polish)

Mode B changeset-level documentation sweep. All implementing code is shipped and
green (P1.M5.T2.S1). This item produces two files and touches nothing else.

## 1. config.example.json — required content and why it is safe

PRD §14.1 "JSON form" and PRD §14.2 "Defaults" define the exact values, and
config.go `DefaultConfig()` returns them verbatim. config_test.go `TestDefaultConfig`
pins every field. So the example file is exactly `DefaultConfig()` serialized.

DefaultConfig() (config.go):
```go
Upstream:    "https://api.z.ai/api/mcp/web_search_prime/mcp"
Listen:      "127.0.0.1:8787"
Path:        "/mcp"
Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"}
TargetParam: "search_query"
LogLevel:    "info"
```

JSON keys are snake_case (config.go struct tags: upstream, listen, path, aliases,
target_param, log_level). Confirmed by `LoadConfig` using `json.Unmarshal`.

Recommended content = output of `json.MarshalIndent(DefaultConfig(), "", "  ")`
plus a trailing newline. That guarantees the file round-trips to DefaultConfig()
under `encoding/json` (the loader the proxy actually uses):
```json
{
  "upstream": "https://api.z.ai/api/mcp/web_search_prime/mcp",
  "listen": "127.0.0.1:8787",
  "path": "/mcp",
  "aliases": [
    "query",
    "q",
    "search",
    "searchQuery",
    "search_term"
  ],
  "target_param": "search_query",
  "log_level": "info"
}
```
This differs from the PRD §14.1 "JSON form" ONLY in whitespace/alignment (PRD
aligns values with extra spaces; we use plain 2-space indent). Whitespace is
cosmetic; the parsed values are identical. Plain 2-space indent is the idiomatic
shape for a config.example.* file and matches what `encoding/json` MarshalIndent
produces.

JSON has no comments. Per the contract we keep the file valid JSON (no // or /* */).
Field documentation lives in README.md (sibling P1.M5.T3.S1), which documents the
schema inline and points to this file by name. Division of labor is clean: the
JSON file is the ready-to-copy values; the README is the prose.

## 2. doc.go — current state and what to polish

Current package comment (already gofmt-clean, `gofmt -l doc.go` empty):
```go
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
```

The contract asks the polish to "accurately describe the proxy, its single
rewrite rule, and the non-goals (PRD §3) in a few lines." Gaps to fill:
- The single rewrite rule is stated loosely ("rewrites aliases"). Make the
  promote-vs-drop semantics explicit: when search_query is absent the first
  present alias (config order) is promoted; when search_query is present all
  aliases are dropped (canonical wins). Default aliases: query, q, search,
  searchQuery, search_term.
- Non-goals are absent. Add a short line or two naming the main exclusions from
  PRD §3: does not normalize location/content_size/other enums, does not warn
  about unsupported params, does not manage keys/retry/cache.

Keep it "a few lines" — this is a package comment, not a README. Target ~12-18
comment lines. The `// ` prefix and a single blank `//` paragraph separator keep
gofmt stable.

gofmt does NOT reflow comment text; it only checks `//` alignment. So the writer
controls wording; just keep the `// ` prefix on every line and run `gofmt -l`.

## 3. write-tech-docs linter scope — IMPORTANT

`lint.sh` is MARKDOWN-ONLY: its awk strips fenced ``` blocks and inline `code`,
then greps prose. It does not run on a .go file. So:
- For doc.go, apply the SAME style rules manually (no em dashes U+2014, no " -- ",
  no marketing tell-words from the reference list, concise). Verify with explicit
  greps (see PRP Validation Level 1). Current doc.go has none of these; the polish
  must keep it that way.
- For config.example.json there is no prose to lint; it is data.

## 4. Validation contract (from P1.M5.T2.S1 verification record)

Green baseline gates (must remain green after edits):
- `gofmt -l .` -> empty
- `go vet ./...` -> no output
- `go test ./...` -> ok (71 Test* funcs / 10 files)
- `go build -o web-search-prime-fixer .` -> exit 0

Added for this item:
- `jq . config.example.json` -> exit 0 (valid JSON).
- Round-trip: temporarily load config.example.json via LoadConfig and assert it
  equals DefaultConfig() (throwaway test, deleted before finishing so the
  deliverable stays exactly two files).
- `go doc .` -> renders the polished package comment (sanity).

## 5. Scope boundaries (parallel sibling P1.M5.T3.S1)

- S1 owns README.md and references config.example.json by name. We MUST create
  config.example.json at repo root (PRD §9 file layout) so that reference resolves.
- S1 documents the config schema inline; we do not duplicate prose in the JSON.
- We do NOT create or modify README.md, main.go, config.go, any *_test.go, PRD.md,
  tasks.json, prd_snapshot.md, .gitignore. Output is exactly two files.
