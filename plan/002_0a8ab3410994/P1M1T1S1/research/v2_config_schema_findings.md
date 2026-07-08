# Research Note — P1.M1.T1.S1 (v2 Config schema extension)

On-disk verifications against the live v1 repo at /home/dustin/projects/web-search-prime-fixer.
PRD source: plan/002_0a8ab3410994/prd_snapshot.md §18 (h2.18 / h3.27 §18.1 / h3.28 §18.2).

## 1. The v1 config.go (the INPUT to this subtask) — CONFIRMED
`config.go` currently defines, in this field order:
`Upstream, Listen, Path, Aliases []string (json:"aliases"), TargetParam, LogLevel`
plus `DefaultConfig()`, `LoadConfig(path)`, `resolveConfigPath()`, `fileExists()`,
`ResolveConfig()`. Matches codebase_patterns.md §1/§7. Imports are stdlib only:
`encoding/json, fmt, net, net/url, os, path/filepath`. config.go references NO
package-internal symbols from main.go/proxy.go → it is fully self-contained.

## 2. QueryAliases default is 14 entries, NOT 13 (DISCREPANCY — flag)
PRD §18.2 verbatim list:
  query, search_query, q, search, searchQuery, search_term, term, text, input,
  prompt, question, keywords, topic, searchString
Counted mechanically: **14 entries**. The contract text says "the 13-entry list"
— an off-by-one miscount. **PRD §18.2 is authoritative → use all 14.** Transcribe
verbatim; do not drop one to hit "13".

## 3. Downstream breakage from the `Aliases` → `QueryAliases` rename (MAPPED)
S1 changes ONLY config.go. Renaming the field breaks these callers, which is BY
DESIGN of the task breakdown (they are fixed in LATER subtasks):
- `main.go:160`   — logStartup emits `"aliases": cfg.Aliases`        → fixed in P1.M1.T2.S1 (logStartup update)
- `proxy.go:498`  — `aliasPresence(args, cfg.Aliases)`               → fixed in P1.M1.T2.S2 (proxy.go DELETED)
- `proxy.go:499`  — `Rewrite(args, cfg.Aliases, cfg.TargetParam)`    → fixed in P1.M1.T2.S2 (proxy.go DELETED)
- `proxy.go:512,532` — comments referencing cfg.Aliases              → fixed in P1.M1.T2.S2 (proxy.go DELETED)
- `config_test.go`, `resolve_test.go`, `health_test.go`, `proxy_test.go` — reference `.Aliases` / old `DefaultConfig()` → fixed in P1.M1.T3.S1
CONSEQUENCE: after S1, `go build ./...` and `go test ./...` FAIL on main.go +
proxy.go (and tests). **This is expected; S1 must NOT edit those files.** Do not
attempt to keep the whole package green by editing out-of-scope files.

## 4. Scoped compile gate for config.go (VERIFIED technique)
Because config.go is self-contained, it typechecks in isolation. Verified:
copy config.go into a fresh temp module with a throwaway `func main(){}` stub,
run `go vet .` + `go build .` → clean (no output, exit 0). This proves config.go
is correct WITHOUT requiring main.go/proxy.go/tests to compile. See PRP Level 1.

## 5. json.Unmarshal overlay semantics for the map field (CONFIRMED, no code change)
`LoadConfig` overlays a file onto `DefaultConfig()` via `json.Unmarshal`. For the
new `OptionalAliases map[string][]string`:
- If the file OMITS "optional_aliases" → the default 3-entry map is preserved.
- If the file SETS "optional_aliases" → json.Unmarshal ALLOCATES A NEW MAP and
  decodes into it, REPLACING the default map wholesale (override, not merge) —
  identical override semantics to how slices (Tools/QueryAliases) are handled.
=> LoadConfig needs ZERO logic changes. Contract-confirmed ("overlay unchanged").

## 6. Field ORDER must match PRD §18.1 verbatim
Contract: "A Config struct matching PRD §18.1 exactly." PRD §18.1 field order:
Upstream, Listen, Path, Tools, CanonicalTool, CanonicalParam, QueryAliases,
OptionalAliases, TargetTool, TargetParam, LogLevel. (Aliased field moved from
position 4 → position 7 and renamed; TargetParam slid to position 10.)

## 7. Scope boundaries (what S1 must NOT do)
- Do NOT touch `ResolveConfig()` logic or its doc comment (that is S2: "extend
  validation for Tools/CanonicalTool/TargetTool"). Its comment still says
  "Path and Aliases have NO env override" — leave that stale wording for S2.
- Do NOT add the SDK dependency, extract logger/health, touch main.go, or delete
  proxy.go (all T2).
- Do NOT rewrite config_test.go/resolve_test.go/health_test.go (T3.S1).
- DO update the `type Config` doc comment (the [Mode A] DOCS deliverable),
  documenting every new field and the breaking `aliases` → `query_aliases` rename.
