# PRP — P1.M1.T1.S1: Extend Config struct, DefaultConfig, and LoadConfig for the v2 schema

## Goal

**Feature Goal**: Transform the existing v1 `Config` in `config.go` into the v2
schema (PRD §18.1): rename `Aliases`→`QueryAliases` (breaking JSON-key rename
`aliases`→`query_aliases`) and add five new fields (`Tools`, `CanonicalTool`,
`CanonicalParam`, `OptionalAliases`, `TargetTool`), then make `DefaultConfig()`
return every v2 default from PRD §18.2 verbatim. `LoadConfig()` overlay logic is
unchanged. Only `config.go` is touched.

**Deliverable**: An edited `config.go` whose `Config` struct matches PRD §18.1
field-for-field and field-order-for-field-order, whose `DefaultConfig()` returns
all 11 fields with the PRD §18.2 verbatim values, whose `type Config` doc comment
documents every new field + the breaking rename, and whose `LoadConfig()` is
byte-identical in logic to v1. Consumed by P1.M1.T1.S2 (validation) and
P1.M1.T2.S1 (logStartup).

**Success Definition**: `config.go` typechecks cleanly in isolation (scoped gate —
see Validation Loop), the struct equals PRD §18.1 exactly, `DefaultConfig()`
deep-equals the PRD §18.2 literal, and NO file other than `config.go` is modified.
(The full package build is intentionally NOT green yet — see Known Gotchas.)

## Why

- This is the schema foundation for the entire v2 normalizing-MCP-server pivot.
  Every downstream subtask (extraction, upstream delegation, server wiring,
  teaching signal, docs) reads this `Config`.
- The breaking JSON-key rename (`aliases`→`query_aliases`) is a deliberate,
  documented config-migration step (PRD §18.1; codebase_patterns.md §7) that S1
  owns at the struct/JSON-tag level.
- `QueryAliases` MUST be an ordered slice, not a map: query extraction walks it in
  index order (first-present-key wins), and Go map iteration is randomized. The
  new `OptionalAliases` map is fine as a map because each key is an independent
  parameter (per-key slice order is the only ordering that matters there).
- Establishes the full default set so the server "runs with no config file at all"
  (PRD §18.2 closing line) the moment the rest of v2 lands.

## What

Edit ONLY `config.go`. Three regions change; the rest of the file is preserved:

1. **`type Config` struct** — reorder to PRD §18.1 field order; replace the
   `Aliases []string` field with `QueryAliases []string` (`json:"query_aliases"`);
   insert `Tools`, `CanonicalTool`, `CanonicalParam`, `OptionalAliases`,
   `TargetTool`. Keep `Upstream`, `Listen`, `Path`, `TargetParam`, `LogLevel`
   (names, JSON tags, semantics) unchanged. Rewrite the type doc comment.
2. **`DefaultConfig()`** — return a struct literal with all 11 fields set to the
   PRD §18.2 verbatim values. Update its doc comment to cite PRD §18.2.
3. **`LoadConfig()`** — logic byte-identical; optionally refresh wording in its
   doc comment only if it mentions the old `Aliases` name (it does not — leave it).

`resolveConfigPath()`, `fileExists()`, and `ResolveConfig()` are NOT touched here
(`ResolveConfig` validation extension is P1.M1.T1.S2). Its doc comment still says
"Path and Aliases have NO env override" — leave that stale wording for S2.

### Success Criteria

- [ ] `Config` struct field set == {Upstream, Listen, Path, Tools, CanonicalTool, CanonicalParam, QueryAliases, OptionalAliases, TargetTool, TargetParam, LogLevel}; field ORDER matches PRD §18.1 verbatim.
- [ ] JSON tags: `tools`, `canonical_tool`, `canonical_param`, `query_aliases`, `optional_aliases`, `target_tool` on the new/renamed fields; `upstream/listen/path/target_param/log_level` unchanged. The v1 `aliases` tag is GONE.
- [ ] `DefaultConfig()` returns all 11 fields; `QueryAliases` is the 14-entry PRD §18.2 list in order; `OptionalAliases` is the 3-key PRD §18.2 map; `Tools=["web_search"]`, `CanonicalTool="web_search"`, `CanonicalParam="query"`, `TargetTool="web_search_prime"`, `TargetParam="search_query"`, `LogLevel="info"`, plus the unchanged Upstream/Listen/Path.
- [ ] `LoadConfig()` body is unchanged (DefaultConfig base + `json.Unmarshal` overlay; unknown fields ignored).
- [ ] `type Config` doc comment documents every field AND calls out the breaking `aliases`→`query_aliases` rename.
- [ ] `config.go` typechecks clean in the isolated temp-module gate (Level 1).
- [ ] No edits to any file other than `config.go` (not main.go, proxy.go, ResolveConfig, tests, go.mod, PRD, tasks.json).

## All Needed Context

### Context Completeness Check

_Pass._ This subtask modifies one existing, well-understood file (`config.go`,
fully quoted in §"Current Codebase tree" below). The exact target struct, the
exact default values, the exact JSON tags, and the exact downstream breakage are
all pinned from PRD §18.1/§18.2 and on-disk grep. No guesswork required.

### Documentation & References

```yaml
# MUST READ — the authoritative schema and defaults. Verbatim Go literals below.
- file: PRD.md
  why: §18.1 (Schema) and §18.2 (Defaults) define the exact struct and values.
  critical: QueryAliases is a 14-ENTRY list (the contract's "13" is an off-by-one
        miscount — PRD wins, use all 14). Field ORDER in §18.1 is normative
        ("matching PRD §18.1 exactly").

- file: plan/002_0a8ab3410994/architecture/codebase_patterns.md
  why: §1 documents the v1 Config pattern to preserve (struct+defaults+overlay+
        validation split, snake_case JSON tags, unknown fields ignored); §7 is the
        authoritative v1→v2 JSON-key mapping table (aliases→query_aliases is the
        ONLY rename; 5 keys new).
  critical: §7 table is the source of truth for which keys changed. The overlay
        "unknown fields ignored" behavior must be preserved in LoadConfig.

- file: plan/002_0a8ab3410994/architecture/system_context.md
  why: Confirms the v2 pivot context (normalizing MCP server on the Go MCP SDK)
        and which v1 files survive (config.go, logger, /healthz, graceful
        shutdown) vs are deleted (proxy.go/sse.go/rewrite.go in T2.S2).
  critical: config.go SURVIVES the pivot (extended, not rewritten); proxy.go does
        NOT — that is why proxy.go references to cfg.Aliases are left broken for
        T2.S2 to remove.

- file: config.go
  why: THIS is the file being edited. Its current state (quoted in Current
        Codebase tree) is the v1 baseline.
  critical: Only the Config struct (+type doc comment), DefaultConfig() (+doc
        comment) change. resolveConfigPath/fileExists/ResolveConfig are untouched.

- file: plan/002_0a8ab3410994/P1M1T1S1/research/v2_config_schema_findings.md
  why: On-disk proof of (a) the 14-vs-13 count, (b) the exact downstream-breakage
        map, (c) the isolated-compile gate technique, (d) json.Unmarshal
        map-replace overlay semantics for OptionalAliases.
  critical: After S1 the full package will NOT build (main.go:160, proxy.go:498,
        tests). That is expected; do not "fix" it here.

- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: Confirms Unmarshal overlay semantics: for a map field, a present JSON key
        allocates a new map and decodes into it (override, not merge); omitted key
        preserves the existing value. This is why LoadConfig needs no change.
  section: "To unmarshal JSON into a map, Unmarshal first establishes a map to use."
  critical: OptionalAliases is override-on-present, keep-on-omit — exactly matching
        how Tools/QueryAliases slices override. No special merge code needed.
```

### Current Codebase tree (the v1 baseline config.go)

```bash
# Repo root: /home/dustin/projects/web-search-prime-fixer  (v1 state, pre-S1)
#   go.mod                 module web-search-prime-fixer; go 1.22   (UNCHANGED by S1)
#   config.go              <-- THIS FILE (v1 schema)                (EDIT in S1)
#   main.go                references cfg.Aliases at line 160        (T2.S1)
#   proxy.go               references cfg.Aliases at 498/499/512/532 (T2.S2 deletes)
#   sse.go, rewrite.go     deleted in T2.S2                          (not S1)
#   config_test.go, resolve_test.go, health_test.go, proxy_test.go  reference .Aliases (T3.S1)
#   doc.go, go.mod, *.json                                         (not S1)
```

The v1 `config.go` regions S1 touches (verbatim from the file):

```go
// ... imports: encoding/json, fmt, net, net/url, os, path/filepath (UNCHANGED) ...

// Config is the resolved configuration for the web-search-prime-fixer proxy.
// ... (current doc comment) ...
type Config struct {
	Upstream    string   `json:"upstream"`
	Listen      string   `json:"listen"`
	Path        string   `json:"path"`
	Aliases     []string `json:"aliases"`     // <-- RENAME to QueryAliases / query_aliases
	TargetParam string   `json:"target_param"`
	LogLevel    string   `json:"log_level"`
}

func DefaultConfig() Config {
	return Config{
		Upstream:    "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:      "127.0.0.1:8787",
		Path:        "/mcp",
		Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"},
		TargetParam: "search_query",
		LogLevel:    "info",
	}
}

// LoadConfig ... (logic unchanged — keep as-is)
// resolveConfigPath / fileExists / ResolveConfig ... (NOT touched in S1; S2 edits ResolveConfig)
```

### Desired Codebase tree after S1

```bash
# Only config.go changes. Field set/order in the Config struct becomes PRD §18.1.
config.go            # EDITED: Config struct (v2), DefaultConfig (v2 verbatim), type doc comment
# (every other file untouched)
```

The exact v2 `Config` struct + `DefaultConfig` to produce (transcribe verbatim):

```go
// Config is the resolved configuration for the web-search-prime-fixer
// normalizing MCP server (v2 schema, PRD §18.1).
//
// Fields are populated by overlaying a JSON config file onto the built-in
// defaults (see DefaultConfig and LoadConfig). Every field has a snake_case JSON
// key used when reading a config file.
//
// BREAKING v2 change: the v1 field Aliases (JSON key "aliases") was renamed to
// QueryAliases (JSON key "query_aliases"); the []string semantics are unchanged.
// Operators with a v1 config file must rename the key "aliases" -> "query_aliases"
// (documented in the README). Five fields are new in v2: Tools, CanonicalTool,
// CanonicalParam, OptionalAliases, TargetTool.
//
// Field order matches PRD §18.1 verbatim.
type Config struct {
	// Upstream is the z.ai MCP endpoint the server delegates to.
	// JSON key: "upstream".
	Upstream string `json:"upstream"`

	// Listen is the local bind address (host:port). Local only (127.0.0.1).
	// JSON key: "listen".
	Listen string `json:"listen"`

	// Path is reserved (informational; default "/mcp").
	// JSON key: "path".
	Path string `json:"path"`

	// Tools is the list of advertised tool names; Tools[0] is the canonical tool.
	// Must be non-empty and contain CanonicalTool; must never contain TargetTool.
	// JSON key: "tools".
	Tools []string `json:"tools"`

	// CanonicalTool is the tool name the server teaches (default "web_search").
	// Must be present in Tools.
	// JSON key: "canonical_tool".
	CanonicalTool string `json:"canonical_tool"`

	// CanonicalParam is the parameter name the server teaches (default "query").
	// JSON key: "canonical_param".
	CanonicalParam string `json:"canonical_param"`

	// QueryAliases is the ordered query-extraction key priority list. It MUST be a
	// slice, never a map: extraction walks it in INDEX ORDER (first present key
	// wins) and Go map iteration is randomized. (v1 field: Aliases, key "aliases".)
	// JSON key: "query_aliases".
	QueryAliases []string `json:"query_aliases"`

	// OptionalAliases maps each z.ai canonical optional parameter to the
	// client-facing alias names normalized into it. Each top-level key is an
	// independent parameter (map order irrelevant); each per-key slice is the
	// alias priority order for that parameter.
	// JSON key: "optional_aliases".
	OptionalAliases map[string][]string `json:"optional_aliases"`

	// TargetTool is the z.ai tool to call (always "web_search_prime"); never
	// advertised to clients.
	// JSON key: "target_tool".
	TargetTool string `json:"target_tool"`

	// TargetParam is the z.ai canonical parameter sent upstream
	// (always "search_query").
	// JSON key: "target_param".
	TargetParam string `json:"target_param"`

	// LogLevel is one of debug | info | warn | error.
	// JSON key: "log_level".
	LogLevel string `json:"log_level"`
}

// DefaultConfig returns the built-in default configuration (PRD §18.2, verbatim).
//
// The server runs with no config file at all by starting from these defaults;
// LoadConfig("") yields this exact value, and LoadConfig(path) overlays a file's
// JSON on top of it, overriding only the fields the file names (unknown fields
// ignored). Field order matches the struct / PRD §18.1.
func DefaultConfig() Config {
	return Config{
		Upstream:       "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:         "127.0.0.1:8787",
		Path:           "/mcp",
		Tools:          []string{"web_search"},
		CanonicalTool:  "web_search",
		CanonicalParam: "query",
		QueryAliases: []string{
			"query", "search_query", "q", "search", "searchQuery",
			"search_term", "term", "text", "input", "prompt",
			"question", "keywords", "topic", "searchString",
		},
		OptionalAliases: map[string][]string{
			"location":              {"country", "region"},
			"content_size":          {"size", "contentSize", "detail"},
			"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
		},
		TargetTool:  "web_search_prime",
		TargetParam: "search_query",
		LogLevel:    "info",
	}
}
```

`LoadConfig` stays exactly as in v1 (DefaultConfig base + `os.ReadFile` + `json.Unmarshal` overlay + error return). Do not alter it.

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: The full package WILL NOT BUILD after S1. main.go:160 emits
// "aliases": cfg.Aliases and proxy.go:498/499 calls Rewrite(args, cfg.Aliases, ...).
// The Aliases field no longer exists after S1. This is BY DESIGN:
//   - main.go logStartup is rewritten in P1.M1.T2.S1
//   - proxy.go is DELETED in P1.M1.T2.S2
//   - config_test.go/resolve_test.go/health_test.go/proxy_test.go are rewritten in P1.M1.T3.S1
// S1 MUST NOT touch those files. Verify config.go correctness with the isolated
// temp-module gate (Validation Loop L1), NOT `go build ./...`.

// CRITICAL: QueryAliases default is 14 entries, not 13. The contract text says
// "13-entry list" — that is an off-by-one miscount. PRD §18.2 is authoritative;
// transcribe all 14: query, search_query, q, search, searchQuery, search_term,
// term, text, input, prompt, question, keywords, topic, searchString.

// CRITICAL: QueryAliases must be a SLICE (ordered, index-walked), never a map —
// Go map iteration order is randomized and extraction needs first-present-key-wins.
// OptionalAliases is correctly a map (each key = independent param).

// GOTCHA: Field ORDER is normative — contract says "matching PRD §18.1 exactly."
// PRD §18.1 order: Upstream, Listen, Path, Tools, CanonicalTool, CanonicalParam,
// QueryAliases, OptionalAliases, TargetTool, TargetParam, LogLevel.

// GOTCHA: json.Unmarshal overlay for the OptionalAliases MAP replaces wholesale
// when the JSON key is present (not a merge). That matches slice override
// semantics and needs NO LoadConfig change. (Confirmed in research note §5.)

// SCOPE GUARD: Do NOT edit ResolveConfig() or its doc comment (S2 owns it). Do
// NOT add the SDK dep, extract logger/health, edit main.go, delete proxy.go, or
// rewrite tests. Edit ONLY config.go.
```

## Implementation Blueprint

### Data models and structure

The only data model is the `Config` struct (shown in full above). No new types,
no methods, no exported symbols beyond the renamed field. Type safety comes from
the struct tags + the literal-typed defaults (slices stay `[]string`, the
optional map stays `map[string][]string`).

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: EDIT config.go — replace the `type Config` struct (and its doc comment)
  - IMPLEMENT: the v2 Config struct exactly as shown in "Desired Codebase tree"
    (11 fields, PRD §18.1 order, snake_case JSON tags).
  - RENAME: the Aliases []string field -> QueryAliases []string (json:"query_aliases").
  - ADD fields: Tools []string (json:"tools"); CanonicalTool string
    (json:"canonical_tool"); CanonicalParam string (json:"canonical_param");
    OptionalAliases map[string][]string (json:"optional_aliases"); TargetTool
    string (json:"target_tool").
  - PRESERVE (name+tag+meaning): Upstream, Listen, Path, TargetParam, LogLevel.
  - REWRITE the type doc comment to document every field + the breaking
    aliases->query_aliases rename (the [Mode A] DOCS deliverable).
  - NAMING: exported Go field names CamelCase; JSON tags snake_case. PLACEMENT:
    field order == PRD §18.1.
  - GOTCHA: do not reorder TargetParam early — in v2 it sits at position 10
    (after TargetTool), NOT position 5 as in v1.

Task 2: EDIT config.go — replace the `DefaultConfig()` body (and its doc comment)
  - IMPLEMENT: return all 11 fields with the PRD §18.2 verbatim values shown in
    "Desired Codebase tree".
  - TRANSCRIBE the 14-entry QueryAliases slice EXACTLY (order matters; the slice
    is walked in index order by extraction later).
  - TRANSCRIBE the OptionalAliases 3-key map EXACTLY: location->[country,region],
    content_size->[size,contentSize,detail],
    search_recency_filter->[recency,freshness,time_filter,date_filter].
  - UPDATE the DefaultConfig doc comment to cite PRD §18.2 (not the v1 §14.2).
  - VERIFY: Tools=["web_search"], CanonicalTool="web_search", CanonicalParam="query",
    TargetTool="web_search_prime", TargetParam="search_query", LogLevel="info",
    Upstream/Listen/Path unchanged from v1.

Task 3: VERIFY LoadConfig() is unchanged
  - READ LoadConfig: confirm it is still DefaultConfig() base + os.ReadFile +
    json.Unmarshal overlay + propagated error, with unknown fields ignored.
  - DO NOT change its logic. (Only refresh its doc comment IF it names the old
    Aliases field — it does not, so leave it untouched.)

Task 4: LEAVE resolveConfigPath / fileExists / ResolveConfig ALONE
  - Do NOT edit these. ResolveConfig validation extension is P1.M1.T1.S2.
  - Its doc comment still says "Path and Aliases have NO env override" — that
    stale wording is S2's to fix, not S1's.

Task 5: VALIDATE (scoped — see Validation Loop)
  - gofmt -l config.go                                  # MUST be empty
  - isolated temp-module build of config.go (L1)        # MUST be clean
  - sanity-grep: no `aliases` json tag remains; new tags present
  - EXPECT full-repo `go build ./...` to FAIL on main.go:160 + proxy.go:498/499
    (out of scope for S1).
```

### Implementation Patterns & Key Details

```go
// Overlay pattern (UNCHANGED LoadConfig) — kept for reference; do not modify:
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()           // v2 defaults base (Task 2)
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil { // overlay; unknown fields ignored
		return cfg, err
	}
	return cfg, nil
}
// json.Unmarshal over OptionalAliases (a map) replaces the default map wholesale
// when "optional_aliases" is present in the file, and preserves it when omitted.
// => no LoadConfig change is needed for the new map field.
```

### Integration Points

```yaml
CONFIG STRUCT (config.go):
  - rename:   Aliases  []string           (json:"aliases")      ->  QueryAliases []string (json:"query_aliases")
  - add:      Tools []string (json:"tools"); CanonicalTool string (json:"canonical_tool");
              CanonicalParam string (json:"canonical_param"); OptionalAliases map[string][]string
              (json:"optional_aliases"); TargetTool string (json:"target_tool")
  - keep:     Upstream, Listen, Path, TargetParam (json:"target_param"), LogLevel
  - order:    PRD §18.1 verbatim

DEFAULTS (DefaultConfig):
  - PRD §18.2 verbatim (14-entry QueryAliases, 3-key OptionalAliases map)

NO INTEGRATION POINTS TOUCHED BY S1:
  - go.mod:                 unchanged (no SDK yet — that is T2.S1)
  - ResolveConfig/env vars: unchanged (S2)
  - main.go / proxy.go:     unchanged (T2.S1 / T2.S2)  <-- will not compile until then
  - tests:                  unchanged (T3.S1)
  - config.example.json:    unchanged (M5.T4.S2)
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback) — SCOPED GATE

```bash
cd /home/dustin/projects/web-search-prime-fixer

# (a) Formatting.
gofmt -l config.go                 # MUST print nothing (empty == clean)

# (b) SCOPED COMPILE: config.go typechecks in isolation. config.go imports only
#     stdlib and references no package-internal symbols, so it compiles standalone.
#     This proves the new struct/defaults are correct WITHOUT requiring the
#     out-of-scope main.go/proxy.go/test fixes (those land in T2/T3).
rm -rf /tmp/cfg-iso && mkdir -p /tmp/cfg-iso && cd /tmp/cfg-iso && \
  go mod init cfgiso >/dev/null 2>&1 && \
  cp /home/dustin/projects/web-search-prime-fixer/config.go . && \
  printf 'package main\n\nfunc main() {}\n' > main_stub.go && \
  go vet . && go build . && echo "OK: config.go typechecks in isolation" && \
  cd /home/dustin/projects/web-search-prime-fixer && rm -rf /tmp/cfg-iso
# Expected: "OK: config.go typechecks in isolation", exit 0, no vet/build errors.

# (c) JSON-tag sanity (grep):
grep -c 'json:"query_aliases"' config.go      # >= 1 (struct tag present)
grep -c 'json:"aliases"' config.go            # 0     (v1 tag GONE)
grep -c 'json:"tools"' config.go              # >= 1
grep -c 'json:"optional_aliases"' config.go   # >= 1
grep -c 'json:"target_tool"' config.go        # >= 1
! grep -nE '\bAliases\b' config.go            # the OLD Go field name is gone
grep -nE '\bQueryAliases\b' config.go         # the NEW Go field name is present
# Expected: all counts/non-matchers hold.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# The EXISTING tests (config_test.go, resolve_test.go, health_test.go,
# proxy_test.go) reference the old .Aliases field and will NOT COMPILE after S1.
# Rewriting them is P1.M1.T3.S1 (out of scope here). Therefore `go test ./...`
# is EXPECTED to fail to compile in this subtask. Do NOT fix the tests now.

# What you CAN verify (a throwaway, non-committed sanity check — delete after):
rm -rf /tmp/cfg-iso && mkdir -p /tmp/cfg-iso && cd /tmp/cfg-iso && \
  go mod init cfgiso >/dev/null 2>&1 && \
  cp /home/dustin/projects/web-search-prime-fixer/config.go . && \
  printf '%s\n' \
    'package main' '' 'import "reflect"' '' 'import "testing"' '' \
    'func TestDefaultV2(t *testing.T){' \
    '  d := DefaultConfig()' \
    '  want := DefaultConfig()' \
    '  if !reflect.DeepEqual(d, want){t.Fatal("defaults")}' \
    '  if len(d.QueryAliases)!=14{t.Fatalf("QueryAliases len=%d want 14",len(d.QueryAliases))}' \
    '  if len(d.OptionalAliases)!=3{t.Fatalf("OptionalAliases len=%d want 3",len(d.OptionalAliases))}' \
    '  if d.Tools[0]!="web_search"||d.CanonicalTool!="web_search"||d.TargetTool!="web_search_prime"{t.Fatal("tool defaults")}' \
    '  if d.TargetParam!="search_query"||d.CanonicalParam!="query"{t.Fatal("param defaults")}' \
    '}' > cfg_test.go && \
  go test . && echo "OK: default-shape sanity passes in isolation" && \
  cd /home/dustin/projects/web-search-prime-fixer && rm -rf /tmp/cfg-iso
# Expected: "OK: default-shape sanity passes in isolation". Confirms 14 aliases,
# 3 optional keys, and the tool/param defaults — the PRD §18.2 shape.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# EXPECTED-FAILURE documentation (NOT a gate to make pass in S1):
go build ./... 2>&1 | sort -u
# Expected output references EXACTLY these call sites (all out of scope):
#   ./main.go:160:??  cfg.Aliases undefined            (fixed in P1.M1.T2.S1)
#   ./proxy.go:498:??  cfg.Aliases undefined           (fixed in P1.M1.T2.S2)
#   ./proxy.go:499:??  cfg.Aliases undefined           (fixed in P1.M1.T2.S2)
# (test files in config_test.go/resolve_test.go/health_test.go/proxy_test.go
#  also fail to compile — fixed in P1.M1.T3.S1)
#
# IF `go build ./...` reports ANY error INSIDE config.go (e.g. syntax error,
# unknown field, type mismatch), that IS an S1 defect — fix config.go.
# IF it reports ONLY main.go/proxy.go/test references to Aliases, that is
# EXPECTED and correct — do not edit those files.

# Real build (S1 + everything) goes green only at the P1.M1 milestone / quality gate.
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Overlay-behavior proof (no LoadConfig change): show that a file overriding
# optional_aliases REPLACES the default map (not merges), and omitting it keeps
# defaults. (Run inside the /tmp/cfg-iso isolated module from L1.)
cat > overlay_test.go <<'EOF'
package main
import ("encoding/json";"testing")
func TestOverlayReplace(t *testing.T){
  cfg := DefaultConfig()
  in := []byte(`{"optional_aliases":{"location":["us"]}}`)
  if err := json.Unmarshal(in,&cfg); err!=nil{t.Fatal(err)}
  if got:=cfg.OptionalAliases["location"]; len(got)!=1||got[0]!="us"{t.Fatalf("location=%v",got)}
  if _,ok:=cfg.OptionalAliases["content_size"]; ok{t.Fatal("content_size should be GONE (replace not merge)")}
}
func TestOverlayOmit(t *testing.T){
  cfg := DefaultConfig()
  if err:=json.Unmarshal([]byte(`{"listen":"0.0.0.0:9"}`),&cfg);err!=nil{t.Fatal(err)}
  if len(cfg.OptionalAliases)!=3{t.Fatalf("optional map lost on omit: %d",len(cfg.OptionalAliases))}
}
EOF
go test -run TestOverlay ./...   # in the isolated module
# Expected: PASS — confirms LoadConfig needs no change for the new map field.
# (Delete the temp module after; do NOT commit overlay_test.go into the repo.)
```

## Final Validation Checklist

### Technical Validation

- [ ] `gofmt -l config.go` prints nothing.
- [ ] Isolated temp-module `go vet . && go build .` on config.go is clean (L1).
- [ ] No `json:"aliases"` tag remains; new tags (`tools`, `canonical_tool`, `canonical_param`, `query_aliases`, `optional_aliases`, `target_tool`) present.
- [ ] No Go identifier `Aliases` remains in config.go; `QueryAliases` is present.
- [ ] Full-repo `go build ./...` fails ONLY on main.go:160 + proxy.go:498/499 (+ test compile errors) — i.e. no error originates inside config.go.

### Feature Validation

- [ ] `Config` struct == PRD §18.1 field set AND field order.
- [ ] `DefaultConfig()` == PRD §18.2 verbatim (14-entry QueryAliases, 3-key OptionalAliases, all scalar defaults).
- [ ] `LoadConfig()` body unchanged from v1.
- [ ] `type Config` doc comment documents every new field and the breaking `aliases`→`query_aliases` rename.

### Code Quality Validation

- [ ] Follows the v1 config pattern (codebase_patterns.md §1): struct + DefaultConfig + LoadConfig overlay + (separate) ResolveConfig validation.
- [ ] snake_case JSON tags on every field; unknown-fields-ignored preserved.
- [ ] Scope respected: ONLY config.go edited. ResolveConfig, main.go, proxy.go, tests, go.mod untouched.

### Documentation & Deployment

- [ ] `type Config` doc comment accurate and complete (the [Mode A] deliverable).
- [ ] Breaking rename surfaced in the doc comment for downstream README/doc writers (M5.T4).
- [ ] No env vars or config.example.json changes in S1 (those are later subtasks).

---

## Anti-Patterns to Avoid

- ❌ Don't drop a QueryAliases entry to satisfy the contract's "13" — PRD §18.2 lists 14; use all 14.
- ❌ Don't make QueryAliases a map — it must be an ordered slice (index-walked extraction; Go map iteration is randomized).
- ❌ Don't reorder fields to anything other than PRD §18.1 order — the contract requires "matching PRD §18.1 exactly."
- ❌ Don't "fix" the broken `go build ./...` by editing main.go / proxy.go / tests — those are T2.S1, T2.S2, T3.S1. Verify with the isolated gate instead.
- ❌ Don't edit ResolveConfig() or its doc comment — validation extension is S2.
- ❌ Don't add the SDK dependency, extract logger/health, or change go.mod in S1 — that is T2.S1.
- ❌ Don't merge OptionalAliases on overlay — json.Unmarshal replaces a present map wholesale (override semantics); LoadConfig must stay as-is.
- ❌ Don't leave the v1 `aliases` JSON tag or the `Aliases` Go field around "for compatibility" — the rename is intentional and breaking (codebase_patterns.md §7).

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The change is a mechanical, fully-specified edit to a single file
whose exact target contents are quoted verbatim above (struct + DefaultConfig +
doc comment). The only non-obvious risks are (a) the 14-vs-13 alias count
(pinned: PRD wins, use 14), (b) the deliberate full-package build breakage after
S1 (pinned: expected, use the isolated gate, do not edit out-of-scope files), and
(c) leaving ResolveConfig/LoadConfig untouched (pinned: S2 owns ResolveConfig;
LoadConfig needs no change). The -1 reserves for an agent that over-eagerly
"repairs" the broken build by editing main.go/proxy.go.
