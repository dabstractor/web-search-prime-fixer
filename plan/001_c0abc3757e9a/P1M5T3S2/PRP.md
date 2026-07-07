name: "P1.M5.T3.S2 — config.example.json + doc.go package comment polish (Mode B changeset-level doc sweep)"
description: |

  Ship the two remaining user-facing config-surface artifacts for
  web-search-prime-fixer: a ready-to-copy `config.example.json` at the repo root
  (matching the Config schema and DefaultConfig from P1.M1.T2.S1 / PRD §14.1),
  and a polished `doc.go` package comment that accurately describes the proxy,
  its single rewrite rule, and the non-goals (PRD §3). This is a MODE B
  changeset-level documentation sweep (SOW §5): all implementing code is shipped
  and the quality gate is green (P1.M5.T2.S1). The work documents SHIPPED behavior
  and must not change any runtime behavior.

  OUTPUT (exactly two files):
    - config.example.json  (NEW, repo root) — valid JSON, values == DefaultConfig()
    - doc.go               (MODIFY, package comment only) — polished, accurate, concise

  INPUT (final, shipped state):
    - P1.M1.T2.S2  Config struct + DefaultConfig() + LoadConfig + ResolveConfig (the schema this file must match)
    - P1.M1.T1.S1  doc.go package comment (the text to polish)
    - P1.M5.T2.S1  green quality gate (gofmt/vet/test/build all pass; stdlib only)
    - PRD §14.1/§14.2 schema + defaults, §3 non-goals, §9 file layout

  SCOPE BOUNDARY: this item writes ONLY config.example.json and doc.go.
    - Sibling P1.M5.T3.S1 owns README.md and references config.example.json by name
      ("see config.example.json for a ready-to-copy example") and documents the
      config schema inline. So config.example.json MUST land at the repo root so
      that reference resolves, and it should be PURE VALID JSON (no // comments)
      because the README carries the field-by-field prose. Do NOT create or modify
      README.md, main.go, config.go, any *_test.go, PRD.md, tasks.json,
      prd_snapshot.md, or .gitignore.
    - Do NOT add a committed test file. Validate config.example.json with a
      THROWAWAY test that you delete before finishing, so the deliverable is
      exactly two files.

  STYLE: apply the write-tech-docs hard rules to doc.go prose (no em dashes, no
  marketing tell-words, concise, front-loaded). NOTE: the write-tech-docs linter
  (scripts/lint.sh) is MARKDOWN-ONLY, so it does NOT run on doc.go; use manual
  grep scans instead (see Validation Loop).

---

## Goal

**Feature Goal**: Ship the final two user-facing config/documentation artifacts so
  the v1.0 changeset is self-describing: (a) a valid, ready-to-copy
  `config.example.json` at the repo root whose values exactly equal
  `DefaultConfig()` and whose keys are the snake_case JSON keys from the Config
  struct; and (b) a polished `doc.go` package comment that states what the proxy
  is, its single rewrite rule (promote-vs-drop), and its non-goals, in a few lines.

**Deliverable**: Two files at the repo root: `config.example.json` (new) and
  `doc.go` (package comment modified). `config.example.json` is valid JSON that
  unmarshals to `DefaultConfig()`. `doc.go` passes `gofmt -l`, `go vet`, `go
  build`, and `go test`, and its package comment reads cleanly under `go doc`.

**Success Definition**:
  - `config.example.json` exists at repo root, is valid JSON (`jq .` exit 0), and
    `LoadConfig("config.example.json") == DefaultConfig()` (verified via a
    throwaway test, then deleted).
  - `doc.go` package comment accurately and concisely describes: what the proxy
    is, the single rewrite rule (aliases, target, promote-when-absent /
    drop-when-present), the injected SSE warning, local-only + stdlib-only, and
    the non-goals (PRD §3).
  - The green quality gate stays green: `gofmt -l .` empty, `go vet ./...` clean,
    `go test ./...` ok, `go build -o web-search-prime-fixer .` exit 0.
  - doc.go prose has no em dashes (U+2014), no " -- ", no marketing tell-words.
  - Exactly two files changed (config.example.json created, doc.go modified).
    Nothing else is created, modified, or deleted.

## User Persona

**Target User**: A single operator (developer) running one local instance for
  their own MCP clients, and any developer reading the Go package documentation.

**Use Case**: The operator wants to customize the proxy (change listen port,
  upstream, log level, or the alias list) by copying a known-good config file
  rather than reading the struct. A developer exploring the code runs `go doc` and
  in a few lines understands what the binary does, its one rule, and what it
  deliberately does not do.

**User Journey**:
  1. Operator opens `config.example.json`, copies it to `web-search-prime-fixer.json`
     in the repo dir (or `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json`).
  2. Operator edits one value (e.g. `"listen"`), restarts, and it just works
     because the file is valid JSON the proxy already understands.
  3. Developer runs `go doc .` and reads a concise, accurate summary of the proxy,
     its single rewrite rule, and its non-goals.

**Pain Points Addressed**: Guessing the config file shape; writing invalid JSON;
  misunderstanding the proxy as a general-purpose parameter fixer (it is not).

## Why

- `config.example.json` is listed as a deliverable in PRD §9 (file layout) and is
  the user-facing config surface. It must match the shipped Config schema
  (snake_case keys, PRD §14.1) and the shipped defaults (PRD §14.2) exactly so an
  operator can copy it and edit one field without surprises.
- `doc.go` is the Go package documentation (shown by `go doc` and on pkg.go.dev).
  Its comment must accurately reflect the shipped single-rewrite behavior and the
  explicit non-goals (PRD §3) so readers do not assume the proxy normalizes enums,
  warns about unsupported params, manages keys, retries, or caches.
- It is the Mode B changeset-level documentation sweep: the code is done and green;
  these two files close out the documentation surface for v1.0.

## What

Two files. Behavior does not change at all; these are pure documentation/config
artifacts over the shipped code.

### config.example.json (NEW, repo root)
- Valid JSON. No `//` or `/* */` comments (JSON does not support comments; the
  proxy parses with `encoding/json`, which rejects them).
- Keys (snake_case, from Config struct json tags): `upstream`, `listen`, `path`,
  `aliases`, `target_param`, `log_level`.
- Values equal `DefaultConfig()` exactly:
  - `upstream`: `https://api.z.ai/api/mcp/web_search_prime/mcp`
  - `listen`: `127.0.0.1:8787`
  - `path`: `/mcp`
  - `aliases`: `["query", "q", "search", "searchQuery", "search_term"]`
  - `target_param`: `search_query`
  - `log_level`: `info`
- Ends with a single trailing newline.
- Recommended content (the output of `json.MarshalIndent(DefaultConfig(), "", "  ")`
  + trailing newline). Plain 2-space indent. This differs from the PRD §14.1
  "JSON form" only in whitespace/alignment, which is cosmetic; the parsed values
  are identical.

### doc.go (MODIFY, package comment only)
- Keep `package main`.
- Polish the comment so it states, in a few lines:
  1. What the proxy is: a local, transparent reverse proxy for the z.ai
     web-search-prime MCP endpoint, stdlib-only, binds 127.0.0.1.
  2. The single rewrite rule: on a tools/call, argument keys in the alias list are
     renamed to `search_query`. If `search_query` is absent, the first present
     alias (config order) is promoted and the other aliases are removed; if
     `search_query` is present, all aliases are removed (canonical wins). Every
     other argument passes through untouched.
  3. The SSE warning: a one-line warning is injected into the tools/call result
     when a rewrite is applied.
  4. The default alias list: query, q, search, searchQuery, search_term; target is
     always `search_query`.
  5. The non-goals (PRD §3), stated briefly: it does not normalize other
     parameters (location, content_size, search_recency_filter, enums), does not
     warn about or drop unsupported params, does not rewrite the tool schema, and
     does not manage keys, retry, rate-limit, or cache.
- Keep it to "a few lines" (~12 to 18 comment lines). This is a package comment,
  not a README.
- Keep the `// ` prefix on every line and use a single blank `//` line to separate
  paragraphs so `gofmt` stays stable (gofmt does not reflow comment text).

### Success Criteria
- [ ] `config.example.json` exists at repo root, valid JSON, values == DefaultConfig().
- [ ] `doc.go` package comment is polished, accurate, and gofmt-clean.
- [ ] `gofmt -l .` is empty.
- [ ] `go vet ./...` prints nothing (exit 0).
- [ ] `go test ./...` is `ok` (exit 0); no new committed test file is added.
- [ ] `go build -o web-search-prime-fixer .` exits 0.
- [ ] doc.go prose has no em dashes / " -- " / marketing tell-words.
- [ ] Exactly two files changed: config.example.json (new) and doc.go (modified).

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can complete this item from this PRP
alone because: (a) the exact bytes of config.example.json are given (the
MarshalIndent output), and the throwaway test that proves it round-trips to
DefaultConfig() is specified; (b) the doc.go polish requirements are enumerated
point by point with the source-of-truth PRD sections; (c) the green-gate
commands are listed verbatim with their expected output; (d) the write-tech-docs
style rules that apply to doc.go are stated, including the critical note that the
markdown-only linter does not run on a .go file; (e) the scope boundary forbids
touching any file other than the two deliverables.

### Documentation & References

```yaml
# MUST READ — the Config schema, defaults, JSON keys, and the loader this file must satisfy.
- file: config.go
  why: Config struct (json tags = the snake_case keys), DefaultConfig() (the exact
        values the example must equal), LoadConfig (how the file is parsed:
        json.Unmarshal over DefaultConfig, unknown fields ignored).
  critical: the example MUST unmarshal to DefaultConfig() via encoding/json.
        Unknown fields are tolerated, but keep exactly the six documented keys.
  pattern: DefaultConfig() returns the six values; mirror them verbatim.

# MUST READ — PRD §14 config schema + defaults (the contract this file must match).
- file: PRD.md
  section: "§14 Configuration (config.go)" — §14.1 Schema + JSON form, §14.2 Defaults
  why: §14.1 lists the snake_case JSON keys and the example JSON form; §14.2 lists
        the exact default values. Both equal config.go DefaultConfig().
  critical: PRD §14.1 "JSON form" uses aligned whitespace for readability; we use
        plain 2-space indent. Whitespace is cosmetic; values must match.

# MUST READ — the file to polish.
- file: doc.go
  why: the existing package comment. Polish it per the "What" section; keep package main.

# MUST READ — the non-goals to state in doc.go.
- file: PRD.md
  section: "§3 Non-goals (explicitly out of scope)"
  why: doc.go must briefly state these so readers do not mistake the proxy for a
        general-purpose fixer. Name the main exclusions (no enum/other-param
        normalization, no warning on unsupported params, no schema rewrite, no key
        management, no retry/rate-limit/cache).

# READ — the rewrite behavior to describe accurately in doc.go.
- file: rewrite.go
  why: confirms the promote-vs-drop semantics (first present alias promoted when
        target absent; all aliases removed when target present) and the default
        alias set. Describe the rule, do not paste code.

# READ — the warning behavior to describe accurately in doc.go.
- file: sse.go
  section: warningText
  why: confirms the one-line SSE warning is injected into the tools/call result on
        a rewrite. Describe it; do not quote the full string here.

# READ — PRD §9 file layout (config.example.json + doc.go are listed deliverables).
- file: PRD.md
  section: "§9 Architecture > File layout"
  why: confirms config.example.json lives at the repo root next to README.md.

# READ — the green-gate baseline (contract that gofmt/vet/test/build are green).
- docfile: plan/001_c0abc3757e9a/P1M5T2S1/verification-record.md
  why: proves the module is green before this item starts. Re-run the same gates
        after editing; they must stay green.

# READ — the verified shipped-behavior facts (single source of truth for prose).
- docfile: plan/001_c0abc3757e9a/P1M5T3S1/research/shipped-behavior-and-conventions.md
  why: the rewrite rule, warning, defaults, and non-goals are stated here with
        source-file citations. Use it to keep doc.go accurate.

# STYLE — prose rules for doc.go (the markdown linter does NOT run on .go).
- skill: write-tech-docs  (location: /home/dustin/.pi/agent/skills/write-tech-docs/SKILL.md)
  why: apply the hard rules to the doc.go comment prose: no em dashes (U+2014), no
        " -- ", no marketing tell-words, front-loaded, concise, one idea per line.
  critical: scripts/lint.sh strips ``` blocks and inline code and greps prose, so
        it is markdown-only. Do NOT run it on doc.go. Use the manual greps in
        Validation Level 1 instead.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod                  # module web-search-prime-fixer; go 1.22 (ZERO requires; stdlib only)
  doc.go                  # package main comment (THIS item polishes it)
  main.go                 # bootstrap, /healthz, version, routing, graceful shutdown
  config.go               # Config + DefaultConfig + LoadConfig + ResolveConfig (schema source)
  proxy.go                # request handling, forwarding, conditional SSE injection
  rewrite.go              # Rewrite (the one rename rule) + note strings
  sse.go                  # SSE reader + Inject + warningText
  *_test.go               # tests (71 funcs / 10 files); `go test ./...`
  testdata/*.sse          # golden SSE fixtures
  .gitignore              # ignores /web-search-prime-fixer (build artifact) + .pi-subagents/
  PRD.md                  # design doc (read-only)
  # NOTE: config.example.json does NOT exist yet (this item creates it).
  # NOTE: README.md is created by sibling P1.M5.T3.S1 (in flight); do not touch it.
```

### Desired Codebase tree with files to be added / responsibility

```bash
config.example.json       # NEW (this item). Ready-to-copy valid JSON config whose
                          # values equal DefaultConfig() and whose keys are the
                          # Config struct snake_case json tags. Pure JSON, no
                          # comments. README.md (S1) points here and documents fields.
doc.go                    # MODIFIED (this item, package comment only). Accurate,
                          # concise description of the proxy, its single rewrite
                          # rule, and the non-goals (PRD §3).
# No other file is created or modified by this item.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — JSON has no comments. encoding/json REJECTS // and /* */. The example
  file must be pure valid JSON. Field documentation lives in README.md (S1), which
  documents the schema inline and points here. Do not try to inline-comment the JSON.

CRITICAL — the example must unmarshal to DefaultConfig() under the proxy's own
  loader (LoadConfig -> json.Unmarshal over DefaultConfig). Validate this with the
  throwaway round-trip test, not just eyeballing.

CRITICAL — write-tech-docs lint.sh is MARKDOWN-ONLY (it strips code blocks and
  inline code, then greps prose). It does NOT run on doc.go. Apply the same hard
  rules to doc.go manually and verify with the greps in Validation Level 1.

CRITICAL — gofmt does NOT reflow comment text; it only checks // alignment. So the
  writer controls wording. Keep the "// " prefix on every line and a single blank
  "//" between paragraphs. Run "gofmt -l doc.go" (must print nothing).

CRITICAL — keep doc.go short. It is a package comment, not a README. ~12-18 comment
  lines. Do not paste the full warning string, the full alias algorithm code, or a
  file-by-file tour. State the rule, the warning, local/stdlib-only, and the
  non-goals.

GOTCHA — PRD §14.1 "JSON form" aligns values with extra spaces for readability. We
  use plain 2-space indent (the MarshalIndent output). Whitespace is cosmetic; the
  parsed values are identical to DefaultConfig(). Do not hand-align with spaces.

GOTCHA — Aliases is a []string; MarshalIndent renders it as a multi-line array
  (one element per line). That is fine and idiomatic. Keep that shape.

GOTCHA — config.example.json is consumed by BOTH humans (copy/edit) and the proxy
  (json.Unmarshal). The throwaway round-trip test is the authoritative check that
  both views agree.

GOTCHA — sibling P1.M5.T3.S1 (README.md, in flight) references config.example.json
  by name and documents the schema inline. Do not duplicate that prose in the JSON;
  do not modify README.md. The JSON must land at the repo root so S1's reference
  resolves.

GOTCHA — go.sum does NOT exist (zero-requires module). No network is needed for
  gofmt/vet/test/build. Do not run `go mod download`.
```

## Implementation Blueprint

### Data models and structure

None. No Go types change. The Config struct and DefaultConfig already exist in
config.go and are the source of truth this item mirrors into config.example.json.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUTS before writing
  - READ: plan/001_c0abc3757e9a/P1M5T3S2/research/findings.md
  - CONFIRM: config.example.json does not exist yet (ls config.example.json ->
    not found). If it exists, read it and reconcile rather than overwrite blindly.
  - CONFIRM: the green baseline before editing:
      gofmt -l .            # empty
      go vet ./...          # no output
      go test ./...         # ok
    (P1.M5.T2.S1 verified all green; re-check so edits are not blamed for prior
     breakage.)
  - GLANCE at config.go DefaultConfig() and the Config struct json tags to
    confirm the six snake_case keys and the exact default values.

Task 1: CREATE config.example.json (repo root)
  - WRITE the file with this exact content (MarshalIndent of DefaultConfig() +
    trailing newline). Plain 2-space indent:
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
  - NAMING: snake_case keys exactly as the Config struct json tags
    (upstream, listen, path, aliases, target_param, log_level).
  - PLACEMENT: repo root, next to README.md (PRD §9 file layout).
  - CONSTRAINT: pure valid JSON. No // or /* */. Ends with a single newline.
  - EQUIVALENT: generating via
      go run -mod=mod - <<'GO'   (see Validation Level 2 for the generator)
    is acceptable too; the byte content above is the expected output.

Task 2: POLISH doc.go package comment
  - MODIFY only the comment block above "package main"; keep "package main".
  - STATE (a few lines, // prefixed):
      * What it is: a local, transparent reverse proxy for the z.ai
        web-search-prime MCP endpoint; Go standard library only; binds 127.0.0.1.
      * The single rewrite rule: on a tools/call, argument keys found in the
        alias list are renamed to search_query. If search_query is ABSENT, the
        first present alias (config order) is promoted and the other aliases are
        removed; if search_query is PRESENT, all aliases are removed (canonical
        wins). Every other argument is passed through untouched.
      * The SSE warning: when a rewrite is applied, a one-line warning is
        injected into the tools/call result so the agent learns the correct name.
      * Defaults: aliases query, q, search, searchQuery, search_term; target is
        always search_query.
      * Non-goals (PRD §3), briefly: it does not normalize other parameters
        (location, content_size, search_recency_filter, any enum), does not warn
        about or drop unsupported parameters, does not rewrite the tool schema,
        and does not manage API keys, retry, rate-limit, or cache.
  - FOLLOW: write-tech-docs hard rules for prose (no em dashes U+2014, no " -- ",
    no marketing tell-words, front-loaded, concise, one idea per sentence).
  - CONSTRAINT: ~12-18 comment lines. Keep "// " on every line and a single
    blank "//" between paragraphs. Do not paste the full warning string or code.
  - DO NOT touch any other line of doc.go or any other file.

Task 3: VALIDATE (do NOT commit the throwaway test)
  - RUN all gates in the Validation Loop (Levels 1-3). In particular:
      gofmt -l .            # must be empty
      go vet ./...          # must print nothing
      go test ./...         # must be ok
      go build -o web-search-prime-fixer .   # must exit 0
      jq . config.example.json               # must exit 0 (valid JSON)
  - RUN the throwaway round-trip test (Level 2) to prove
      LoadConfig("config.example.json") == DefaultConfig(), then DELETE the
      throwaway test file before finishing so the deliverable is exactly two files.
  - RUN `go doc .` and confirm the polished comment renders cleanly.
```

### Implementation Patterns & Key Details

```go
// PATTERN: config.example.json is the marshalled DefaultConfig. Do not hand-edit
// values; they must equal DefaultConfig(). If you prefer generation over pasting:
//
//   go run -mod=mod - <<'GO'
//   package main
//
//   import (
//       "encoding/json"
//       "os"
//   )
//
//   func main() {
//       b, _ := json.MarshalIndent(DefaultConfig(), "", "  ")
//       os.Stdout.Write(b)
//       os.Stdout.WriteString("\n")
//   }
//   GO
//
// This guarantees byte-identical key/value match to the schema the loader uses.
// (DefaultConfig is already defined in package main; a standalone `go run -` heredoc
//  is a separate program WITHOUT access to it, so generate into a temp file in the
//  module or just paste the content given in Task 1, which is that exact output.)

// PATTERN: doc.go comment shape (gofmt-stable):
//   // Package main implements web-search-prime-fixer, ...
//   // <rule sentence>
//   // <warning sentence>
//   //
//   // <non-goals sentence(s)>
//   package main
//
// Keep "// " on every line. One blank "//" separates the summary from the
// non-goals. gofmt does not reflow text; you control the wording.

// GOTCHA: do NOT use em dashes (—) or " -- " anywhere in the doc.go comment.
// write-tech-docs forbids them; apply the rule manually since lint.sh is
// markdown-only. Use a colon, parentheses, comma, or period instead.

// GOTCHA: do NOT describe behaviors the proxy does not have. Re-check
// rewrite.go (promote-vs-drop) and PRD §3 (non-goals) before finalizing wording.
```

### Integration Points

```yaml
FILE PRODUCED:
  - config.example.json   # NEW, repo root. Valid JSON, values == DefaultConfig().
  - doc.go                # MODIFIED, package comment only. gofmt-clean.

FILES REFERENCED (not created/modified):
  - README.md             # sibling P1.M5.T3.S1. References config.example.json by
                          # name; we must NOT edit it. Our JSON must land at repo
                          # root so that reference resolves.
  - config.go             # schema + defaults source of truth (DefaultConfig).
  - PRD.md                # §14.1/§14.2 schema+defaults, §3 non-goals, §9 layout.

FILES NEVER MODIFIED (contract):
  - README.md, main.go, config.go, proxy.go, rewrite.go, sse.go, any *_test.go,
    go.mod, PRD.md, tasks.json, prd_snapshot.md, .gitignore.
  - No committed test file is added. The round-trip test is THROWAWAY (deleted).

CROSS-ITEM COHERENCE:
  - Sibling S1 (README) documents the config schema inline and points here for the
    ready-to-copy file. Our JSON keys/values must match the schema S1 describes
    (snake_case, DefaultConfig values) so the two artifacts agree.
  - The green gate (P1.M5.T2.S1) must remain green; these edits are doc/config
    only and must not change any runtime behavior.
```

## Validation Loop

### Level 1: Prose quality (doc.go) + JSON validity (config.example.json)

```bash
# doc.go is a .go file; the markdown-only write-tech-docs linter does NOT apply.
# Apply the same hard rules manually via grep.

# No em dashes (U+2014) and no " -- " pseudo-dashes anywhere in doc.go.
grep -nP '\x{2014}| -- ' doc.go
# Expected: no matches.

# No marketing tell-words in doc.go (case-insensitive, whole word).
grep -niEw 'powerful|robust|elegant|seamless|comprehensive|cutting-edge|state-of-the-art|revolutionary|game-changing|next-generation|blazing-fast|lightning-fast|intuitive|effortless|frictionless|ultimate|stunning|beautiful|incredible|leverage|utilize|unlock|empower|supercharge|streamline|elevate|delve|tapestry|realm|landscape|moreover|furthermore|truly|incredibly' doc.go
# Expected: no matches.

# config.example.json is valid JSON.
jq . config.example.json >/dev/null && echo "valid JSON"
# Expected: "valid JSON", exit 0.

# No // or /* comment markers leaked into the JSON (would break encoding/json).
grep -nE '/\*|\*/|(^|[^:])//' config.example.json
# Expected: no matches. (The regex avoids matching "https://" false positives via
# the [^:] guard before //; eyeball any hit before treating it as a failure.)

# JSON ends with exactly one trailing newline.
test "$(tail -c1 config.example.json | wc -l)" -eq 1 && echo "trailing newline ok"
```

### Level 2: Round-trip equality (config.example.json == DefaultConfig())

```bash
# Throwaway test: prove LoadConfig("config.example.json") == DefaultConfig().
# Create it, run it, confirm PASS, then DELETE it (deliverable = exactly two files).

cat > zz_example_check_test.go <<'GO'
package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func TestConfigExampleMatchesDefault(t *testing.T) {
	abs, err := filepath.Abs("config.example.json")
	if err != nil {
		t.Fatalf("abs path: %v", err)
	}
	got, err := LoadConfig(abs)
	if err != nil {
		t.Fatalf("LoadConfig(config.example.json): %v", err)
	}
	want := DefaultConfig()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("config.example.json != DefaultConfig():\n got  %+v\n want %+v", got, want)
	}
}
GO

go test -run TestConfigExampleMatchesDefault -v
# Expected: --- PASS: TestConfigExampleMatchesDefault ... (ok)

# Then DELETE the throwaway test so the deliverable is exactly two files.
rm zz_example_check_test.go
test ! -f zz_example_check_test.go && echo "throwaway test removed"

# (Optional) generator check: confirm the file equals MarshalIndent output.
go run - <<'GO'
package main
import ("encoding/json";"os")
func main(){
  // mirror DefaultConfig() values here to avoid a separate-program import limit
  b,_:=json.MarshalIndent(map[string]any{
    "upstream":"https://api.z.ai/api/mcp/web_search_prime/mcp",
    "listen":"127.0.0.1:8787","path":"/mcp",
    "aliases":[]string{"query","q","search","searchQuery","search_term"},
    "target_param":"search_query","log_level":"info",
  },"","  ")
  os.Stdout.Write(b);os.Stdout.WriteString("\n")
}
GO > /tmp/wspf_example_canonical.json
diff -u /tmp/wspf_example_canonical.json config.example.json
# Expected: no diff. (Hand-written with identical values also passes; this just
#  confirms formatting. The authoritative check is the round-trip test above.)
```

### Level 3: Go quality gate (must stay green; P1.M5.T2.S1 baseline)

```bash
# After BOTH files are written AND the throwaway test is deleted:
gofmt -l .                       # Expected: empty (nothing printed)
go vet ./...                     # Expected: no output, exit 0
go test ./...                    # Expected: ok  web-search-prime-fixer ...
go build -o web-search-prime-fixer .   # Expected: exit 0

# Package doc renders cleanly (shows the polished comment).
go doc . | head -40
# Expected: prints "package main ..." followed by the polished comment text.

# Clean up the build artifact if you do not want it left around (.gitignore already
# covers it). Do NOT commit the binary.
rm -f web-search-prime-fixer
```

### Level 4: Content sanity (manual eyeball)

```bash
# config.example.json has exactly the six documented keys, snake_case.
jq -r 'keys[]' config.example.json | sort
# Expected:
#   aliases
#   listen
#   log_level
#   path
#   target_param
#   upstream

# Values equal the PRD §14.2 / DefaultConfig defaults.
jq -r '.upstream'   config.example.json   # https://api.z.ai/api/mcp/web_search_prime/mcp
jq -r '.listen'     config.example.json   # 127.0.0.1:8787
jq -r '.path'       config.example.json   # /mcp
jq -r '.target_param' config.example.json # search_query
jq -r '.log_level'  config.example.json   # info
jq -r '.aliases|join(",")' config.example.json  # query,q,search,searchQuery,search_term

# doc.go still declares package main and still has a non-empty comment.
grep -n '^package main$' doc.go            # Expected: one match
grep -n '^// Package main' doc.go          # Expected: one match (comment present)

# doc.go mentions the rewrite rule and the non-goals.
grep -niE 'search_query|alias' doc.go      # Expected: >=1 (the rule)
grep -niE 'non-goal|does not' doc.go       # Expected: >=1 (the exclusions)

# Exactly two files changed vs the pre-item state (config.example.json new,
# doc.go modified). If using git:
git status --porcelain
# Expected (roughly):
#   ?? config.example.json
#    M doc.go
# (README.md may also appear if sibling S1 has landed; that is not our change.)
```

## Final Validation Checklist

### Technical Validation
- [ ] `gofmt -l .` is empty.
- [ ] `go vet ./...` prints nothing (exit 0).
- [ ] `go test ./...` is `ok` (exit 0); no committed test file added.
- [ ] `go build -o web-search-prime-fixer .` exits 0.
- [ ] `jq . config.example.json` exits 0 (valid JSON).
- [ ] Round-trip test PASSed, then the throwaway test file was DELETED.
- [ ] `go doc .` renders the polished package comment.

### Feature Validation (accuracy vs shipped behavior)
- [ ] config.example.json values equal DefaultConfig() (all six fields).
- [ ] config.example.json keys are snake_case (upstream, listen, path, aliases,
      target_param, log_level).
- [ ] config.example.json has no // or /* */ comments and ends with one newline.
- [ ] doc.go states what the proxy is (local, transparent, z.ai web-search-prime,
      stdlib-only, binds 127.0.0.1).
- [ ] doc.go states the single rewrite rule correctly (promote-when-absent /
      drop-when-present, aliases, target = search_query).
- [ ] doc.go mentions the injected SSE warning.
- [ ] doc.go states the non-goals (PRD §3): no other-param/enum normalization, no
      warning on unsupported params, no schema rewrite, no key/retry/cache.
- [ ] doc.go is concise (~12-18 comment lines; not a README).

### Code Quality / Scope Validation
- [ ] Exactly two files changed: config.example.json (new) and doc.go (modified).
- [ ] No edits to README.md, main.go, config.go, proxy.go, rewrite.go, sse.go, any
      *_test.go, go.mod, PRD.md, tasks.json, prd_snapshot.md, or .gitignore.
- [ ] No committed test file added; no build artifact committed.
- [ ] doc.go prose has no em dashes / " -- " / marketing tell-words.
- [ ] Terminology is consistent ("search_query", "alias", "upstream", "proxy").

### Documentation & Deployment
- [ ] config.example.json lands at repo root so S1's README reference resolves.
- [ ] The JSON and the README schema agree (same keys/values; S1 documents inline).
- [ ] doc.go describes SHIPPED behavior only (verified vs rewrite.go / sse.go /
      config.go / PRD §3); no aspirational claims.

---

## Anti-Patterns to Avoid

- ❌ Don't add comments to config.example.json. JSON has none; encoding/json
  rejects them. Field prose lives in README.md (S1).
- ❌ Don't deviate the config.example.json values from DefaultConfig(). The example
  is a ready-to-copy default; if it differs from the code, operators get confused.
- ❌ Don't hand-align JSON values with extra spaces to mimic PRD §14.1. Use plain
  2-space indent (the MarshalIndent output). Whitespace is cosmetic; values win.
- ❌ Don't run lint.sh on doc.go. It is markdown-only. Apply the rules with greps.
- ❌ Don't use em dashes (U+2014) or " -- " in the doc.go comment. Use a colon,
  parentheses, comma, or period.
- ❌ Don't write a long doc.go comment. It is a package comment, not a README. Keep
  it to a few lines. Do not paste the full warning string or the algorithm code.
- ❌ Don't describe a behavior the proxy does not have. Re-check rewrite.go
  (promote-vs-drop) and PRD §3 (non-goals) before finalizing wording.
- ❌ Don't add a committed test file. Validate with a throwaway test, then delete it.
- ❌ Don't modify README.md, doc.go-unrelated lines, any *.go source, *_test.go,
  go.mod, PRD.md, tasks.json, prd_snapshot.md, or .gitignore.
- ❌ Don't leave the build binary committed (.gitignore already ignores it; remove
  it after the build gate if you do not want it on disk).

---

## Confidence Score

**9/10** for one-pass success. The deliverable is small and fully pinned: the exact
bytes of config.example.json are given (they are DefaultConfig() marshalled, and the
throwaway round-trip test makes correctness deterministic, not a matter of opinion);
doc.go's polish requirements are enumerated point by point against PRD §3 / §14 and
the shipped rewrite.go / sse.go. The green-gate commands are listed verbatim with
expected output, and the markdown-linter-vs-grep nuance is called out explicitly
(the single most likely implementation mistake). Residual 1-point deduction: doc.go
prose wording is inherently subjective and must satisfy manual em-dash/tell-word
greps, and there is minor cross-item coupling with the parallel sibling S1
(README.md), which we mitigate by pinning the JSON to DefaultConfig() so both
artifacts necessarily agree. No source/runtime behavior changes, so the gate cannot
regress.
