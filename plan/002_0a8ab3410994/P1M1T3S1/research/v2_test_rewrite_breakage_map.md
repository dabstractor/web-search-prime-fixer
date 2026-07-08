# Research — v2 test rewrite breakage map & decisions (T3.S1)

## Question
P1.M1.T3.S1 rewrites the three surviving v1-schema test files
(`config_test.go`, `resolve_test.go`, `health_test.go`) for the v2 11-field Config
schema + the new SDK handler, leaving `logger_test.go` untouched, so that
`go test ./...` goes green (the M1 definition-of-done gate). This note pins the
exact breakage, the v1→v2 field mapping, and the key test-design decisions.

## Method
Read the on-disk production code (`config.go`, `logger.go`, `health.go`, `main.go`),
all four surviving test files, the parallel T2.S2 PRP + its research
(`sdk_mount_and_v1_deletion.md`), and `architecture/codebase_patterns.md`. Ran
`go build`/`go vet`/`grep` on the live working tree.

## Verified current state (working tree, after parallel T2.S2)
- `go build ./...` → **exit 0** (production files green: config.go, logger.go,
  health.go, main.go, doc.go all compile; v1 proxy.go/sse.go/rewrite.go DELETED).
- v1 files confirmed gone: `ls proxy.go sse.go ...` → "No such file or directory".
- `main.go` is the T2.S2 version: `authHeaderKey` + `authMiddleware` defined;
  `main()` builds `mcp.NewServer` + `mcp.NewStreamableHTTPHandler`, mounts
  `/healthz` → `healthHandler`, `/` → `authMiddleware(sdkHandler)`. NO
  `newProxyHandler`/`newUpstreamClient` refs.
- `go vet ./...` → **exit 1**, first error:
  `./config_test.go:19:3: unknown field Aliases in struct literal of type Config`.
  (vet compiles the WHOLE package test binary at once, so all three test files'
  errors are masked behind the first; fixing all three is required for green.)

## Exact v1 references in the 3 target files (grep-confirmed)
```
config_test.go:19   Aliases: []string{...}            (TestDefaultConfig want struct)
config_test.go:37   def.Aliases ...                   (TestDefaultConfig belt-and-suspenders)
config_test.go:38   def.Aliases                       (error message)
config_test.go:68   partial.Aliases                   (TestLoadConfig partial case)
config_test.go:100  Aliases: []string{"a","b"}        (full_file case)
   (+ JSON key "aliases" in 2 table rows)
resolve_test.go:212 comment + WSPF_ALIASES            (path_and_aliases_not_overridable subtest)
resolve_test.go:220 cfg.Aliases, DefaultConfig().Aliases
resolve_test.go:221 cfg.Aliases                       (error message)
health_test.go:109  comment cfg.Aliases               (TestLogStartup)
health_test.go:114  len(cfg.Aliases)                  (TestLogStartup)
health_test.go:115  range cfg.Aliases                 (TestLogStartup)
health_test.go:151  newProxyHandler(...) + newUpstreamClient()  (TestRouting_HealthzOnly)
   (+ m["aliases"] assertion ~L120-128 in TestLogStartup)
```
`logger_test.go`: grep for `Aliases|newProxyHandler|newUpstreamClient|proxy|sse\.` →
**(clean)**. It tests only `newLogger`/`log`/`redactHeaders` (all in logger.go,
unchanged by v2). **Passes untouched.**

## v1 → v2 field mapping (PRD §18.1, codebase_patterns.md §7)
| v1 | v2 | JSON key |
|----|----|----------|
| `Aliases` | `QueryAliases` (14-entry slice) | `query_aliases` (renamed from `aliases`) |
| — | `Tools` (`["web_search"]`) | `tools` |
| — | `CanonicalTool` (`"web_search"`) | `canonical_tool` |
| — | `CanonicalParam` (`"query"`) | `canonical_param` |
| — | `OptionalAliases` (3-key map) | `optional_aliases` |
| — | `TargetTool` (`"web_search_prime"`) | `target_tool` |
| (TargetParam/listen/upstream/path/log_level unchanged) | | |

## Key decisions

### D1 — logStartup emits v2 fields (verified from main.go)
`logStartup(l, cfg)` logs: `tools` ([]string→JSON array), `canonical_tool`,
`query_aliases` ([]string→JSON array), `listen`, `upstream`, `log_level`.
`TestLogStartup` must assert `m["tools"]`, `m["canonical_tool"]`, `m["query_aliases"]`
(plus listen/upstream/log_level + the no-credential invariant). Decoded JSON arrays
are `[]any`; compare with `reflect.DeepEqual` against a constructed `[]any`.

### D2 — resolve_test.go: add Tools validation via CONFIG FILE (not env)
The three new validation rules (PRD §18.3) — `Tools` empty / missing `CanonicalTool` /
contains `TargetTool` — CANNOT be exercised via env vars: `Tools`/`CanonicalTool`/
`TargetTool` have **no env override** (only `WSPF_UPSTREAM`/`WSPF_LISTEN`/
`WSPF_LOG_LEVEL` do, per config.go ResolveConfig). So these cases write a config
file via the existing `writeConfig(t, body)` helper + `t.Setenv("WSPF_CONFIG", p)`,
reusing `isolateConfigEnv(t)` for hermeticity. Default `CanonicalTool="web_search"`
and `TargetTool="web_search_prime"`, so a file setting `{"tools":[...]}` alone is
enough to trigger each rule. Existing Listen/Upstream validation table is UNCHANGED
(still valid; uses env vars).

### D3 — resolve_test.go scope-guard subtest: rename Aliases → QueryAliases
The `path_and_aliases_not_overridable` subtest (L210-224) references `cfg.Aliases` /
`DefaultConfig().Aliases`. Rename to `QueryAliases` (3 sites: comparison + DeepEqual
+ error message). `WSPF_ALIASES`/`WSPF_QUERY_ALIASES` still have no effect (no such
override in v2) — keep the assertion; it still proves the scope guard.

### D4 — TestRouting_HealthzOnly: sentinel catch-all (NOT SDK-coupled) — DECIDED
The contract offers "build the mux with the new SDK handler (or simplify to just
test /healthz routing without the catch-all proxy)". **Decision: sentinel catch-all.**
Rationale for one-pass success:
- Zero SDK coupling in health_test.go → zero risk of SDK-in-test surprises
  (DNS-rebinding protection, session init, etc.).
- Explicitly sanctioned by the contract ("or simplify...").
- The actual unit under test is `/healthz` ROUTING → healthHandler; a sentinel
  catch-all (set a flag if hit) proves isolation from the catch-all, preserving the
  original test's intent (no upstream/fall-through).
- The SDK handler's full integration is P1.M5.T3's job (server_test.go E2E), NOT M1.
- No new import needed (health_test.go keeps bytes/encoding/json/io/net/http/
  net/http/httptest/reflect/testing).

The real mux in main() DOES use `authMiddleware(sdkHandler)` at `/`; this test
intentionally substitutes a sentinel to stay decoupled and deterministic.

### D5 — config_test.go TestDefaultConfig: literal want struct (not DefaultConfig())
Transcribe the full 11-field expected struct LITERALLY from PRD §18.2 (not
`DefaultConfig()` — which would be tautological). The 14-entry `QueryAliases` and
3-key `OptionalAliases` are spelled out so a typo in `DefaultConfig` is caught.
(The `TestLoadConfig_FromFile` partial/unknown rows DO derive `want` from
`DefaultConfig()` — correct, since they test the MERGE behavior, not the defaults.)

### D6 — import blocks
- `config_test.go`: os, path/filepath, reflect, testing — **unchanged**.
- `resolve_test.go`: os, path/filepath, reflect, testing — **unchanged**.
- `health_test.go`: bytes, encoding/json, io, net/http, net/http/httptest, reflect,
  testing — **unchanged** (sentinel catch-all needs no SDK import).
- `logger_test.go`: **untouched**.

## Conclusions for the implementation
1. config_test.go: full rewrite — 11-field literal `want`, v2 belt-and-suspenders,
   table rows use `query_aliases`/`tools`/`canonical_tool`/`optional_aliases` keys.
2. resolve_test.go: rename Aliases→QueryAliases in the scope-guard subtest (3 sites)
   + append `TestResolveConfig_ToolsValidation` (3 error cases via config file + 1
   positive). Leave the 8 other tests + 2 helpers unchanged.
3. health_test.go: rewrite `TestLogStartup` (assert tools/canonical_tool/
   query_aliases) + `TestRouting_HealthzOnly` (sentinel catch-all). Leave the 4
   healthHandler tests + import block unchanged.
4. logger_test.go: DO NOT TOUCH — verify it passes.
5. Gate: `go test ./...` green (all surviving files); `go vet ./...` clean;
   `go build ./...` stays green.
