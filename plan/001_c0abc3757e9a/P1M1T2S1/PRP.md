# PRP — P1.M1.T2.S1: Config struct + defaults + JSON loading

## Goal

**Feature Goal**: Provide the configuration **data model and the pure
"merge a JSON file over the built-in defaults" primitive** for the
`web-search-prime-fixer` proxy: a `Config` struct (per PRD §14.1) with
snake_case JSON tags, a `DefaultConfig()` constructor (per PRD §14.2, verbatim),
and a `LoadConfig(path)` loader that starts from the defaults and lets a file's
JSON override fields one-by-one (omitted fields keep their defaults). This is the
**S1** half of configuration — struct + defaults + loading from a *given* path.
Discovery (env vars, XDG paths), env overrides, and validation are **S2**
(P1.M1.T2.S2) and are explicitly **out of scope** here.

**Deliverable**: Two source files at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`:
1. `config.go` — `type Config` (with godoc field comments + snake_case JSON
   tags), `func DefaultConfig() Config`, `func LoadConfig(path string) (Config, error)`.
2. `config_test.go` — table-driven tests covering: defaults verbatim,
   `path==""` returns defaults, a partial JSON file overrides only named fields,
   unknown fields are ignored without error, plus an invalid-JSON error case.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. `config_test.go` passes with the cases above.
Nothing beyond `config.go` + `config_test.go` is created (no discovery logic, no
env overrides, no validation, no `config.example.json` — those are S2 / P1.M5.T3).

## Why

- `Config` is the single shared data structure consumed by every later subtask:
  P1.M1.T2.S2 (discovery loads a path then env-overrides/validates a `Config`),
  P1.M1.T3 (logger reads `LogLevel`), P1.M1.T4 (bootstrap reads `Listen`,
  `Upstream`, `Path`), P1.M2.T1 (`Rewrite` consumes `Aliases` + `TargetParam`),
  P1.M4 (proxy reads `Upstream`). Defining it exactly once here, with the merge
  primitive, prevents drift.
- The "start from defaults, overlay the file" pattern is the backbone of
  FR-5 ("Built-in defaults allow the proxy to run with no config file at all").
  PRD §14.2 states the defaults exist so the proxy runs with **no config file**;
  `LoadConfig("")` must therefore yield a fully-usable `Config`.
- Establishes the project's **first test file** (`config_test.go`), setting the
  conventions (stdlib `testing`, `t.TempDir()` for file fixtures,
  table-driven subtests, `reflect.DeepEqual` for struct/slice equality) that
  `rewrite_test.go` / `sse_test.go` / `proxy_test.go` (PRD §19) will follow.

## What

`config.go` declares the configuration struct exactly per PRD §14.1, with the
snake_case JSON keys shown in §14.1's "JSON form" block. `DefaultConfig()`
returns the six §14.2 defaults verbatim. `LoadConfig(path)` is a **pure merge
primitive**: it begins from `DefaultConfig()`; if `path == ""` it returns the
defaults unchanged with a nil error; if `path != ""` it reads the file with
`os.ReadFile` and `json.Unmarshal`s the bytes over the default struct, so the
file's present fields overwrite the defaults while omitted fields keep them.
Unknown fields are ignored (default decoder behavior — do NOT use
`DisallowUnknownFields`). Errors from `os.ReadFile` or `json.Unmarshal` are
returned to the caller; on error the returned `Config` may be partial and must be
ignored.

`config_test.go` exercises: (1) `DefaultConfig()` matches every §14.2 default;
(2) `LoadConfig("")` returns the defaults with no error; (3) a partial JSON file
overrides only the named fields and leaves the rest at default; (4) unknown
fields are ignored without error; (5) invalid JSON yields an error. File fixtures
are written into `t.TempDir()`.

### Success Criteria

- [ ] `type Config` has exactly six fields — `Upstream`, `Listen`, `Path`
      (`string`), `Aliases` (`[]string`), `TargetParam`, `LogLevel` (`string`) —
      in that order, each with the snake_case JSON tag `upstream`, `listen`,
      `path`, `aliases`, `target_param`, `log_level`.
- [ ] `DefaultConfig()` returns the §14.2 defaults verbatim (see Implementation
      Blueprint for exact strings/slice).
- [ ] `LoadConfig("")` returns a `Config` `reflect.DeepEqual` to `DefaultConfig()`
      and a nil error.
- [ ] `LoadConfig(path)` with a partial file overrides only named fields; omitted
      fields equal their defaults (verified by `reflect.DeepEqual` against a
      hand-built expected `Config`).
- [ ] `LoadConfig(path)` with a file containing unknown fields returns no error.
- [ ] `LoadConfig(path)` with invalid JSON returns a non-nil error.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.
- [ ] No discovery (env/XDG), no env overrides, no validation, no
      `config.example.json`, no other PRD §9 file is created in this subtask.

## All Needed Context

### Context Completeness Check

_Pass._ The struct contents, JSON tags, default values, and the `LoadConfig`
merge algorithm are specified verbatim below from PRD §14.1/§14.2. The one
non-obvious behavior — that `json.Unmarshal` over a pre-populated struct leaves
omitted fields at their existing (default) value while overwriting present ones,
and that unknown fields are silently ignored — is **verified on-disk** in
`research/json-merge-probe.md` using the installed `go1.26.4` toolchain. The
S1/S2 scope boundary (what is NOT done here) is stated explicitly. An agent with
no prior knowledge of this codebase can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative schema, JSON form, and defaults.
- file: PRD.md
  why: §14.1 "Schema" (struct fields + "JSON form" block with snake_case keys)
        and §14.2 "Defaults" (the six exact default values). FR-5 "Configuration"
        (the run-with-no-file requirement that motivates DefaultConfig).
  critical: JSON keys are snake_case (upstream, listen, path, aliases,
        target_param, log_level) — NOT the Go field names. Defaults must match
        §14.2 VERBATIM (exact upstream URL, listen addr, alias slice order).

# SCOPE BOUNDARY — §14.3 is the S2 contract, NOT S1's. Read it to know the line.
- file: PRD.md
  why: §14.3 "Discovery and precedence" (WSPF_CONFIG, XDG path search, env
        overrides WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL, validation of
        Listen/Upstream, TargetParam-forcing).
  critical: NONE of §14.3 is implemented in S1. S1 only exposes the *primitive*
        LoadConfig(path). Discovery/override/validation is P1.M1.T2.S2. Do not
        pre-implement them or the two subtasks will collide.

# PREDECESSOR — defines exactly what exists when this subtask starts.
- file: plan/001_c0abc3757e9a/P1M1T1S1/PRP.md
  why: P1.M1.T1.S1 creates go.mod (module web-search-prime-fixer, go 1.22, zero
        requires), doc.go (package comment), main.go (empty func main() stub),
        and testdata/. This subtask compiles into the SAME package main.
  critical: Assume go.mod + an empty main.go exist. Do NOT modify them. Add
        config.go + config_test.go alongside them. main() stays an empty stub
        (populated in P1.M1.T4) — do NOT wire LoadConfig into main() here.

# ARCHITECTURE — confirms stdlib-only invariant + greenfield file list.
- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: "No third-party dependencies are required or allowed (PRD §6, §9)."
        Lists every file to be created project-wide so this subtask knows what
        NOT to touch.
  critical: config.go uses ONLY encoding/json + os (stdlib). config_test.go uses
        ONLY os, path/filepath, reflect, testing. No `go get` of anything.

# VERIFIED GOTCHA — the merge primitive's actual behavior on this toolchain.
- file: plan/001_c0abc3757e9a/P1M1T2S1/research/json-merge-probe.md
  why: On-disk proof (go1.26.4) that json.Unmarshal over a pre-populated struct
        keeps omitted fields at their existing value, overwrites present ones,
        and silently ignores unknown fields (no DisallowUnknownFields).
  critical: The merge IS just `cfg := DefaultConfig(); json.Unmarshal(data,
        &cfg)` — no manual field-walk, no third-party merge lib. Also documents
        two edge cases: `{"aliases":null}` → nil slice; `{"aliases":[]}` → empty
        non-nil slice (out of scope for S1 tests but noted for later subtasks).

# Go stdlib refs — exact semantics relied upon.
- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: "To unmarshal JSON into a struct, Unmarshal matches incoming object keys
        to the keys used by Marshal (either the struct field name or its tag),
        preferring an exact match ... Unmarshal reuses the existing struct,
        keeping the fields that were not present in the input JSON." — this is the
        field-by-field override semantics, confirmed by the probe.
  critical: Do NOT call DisallowUnknownFields (that is on Decoder, and §14.3
        wants unknown fields ignored). Plain json.Unmarshal is correct.
- url: https://pkg.go.dev/os#ReadFile
  why: os.ReadFile reads the whole named file into []byte; returns a non-nil
        error (e.g. *PathError) if the file is missing or unreadable.
  critical: LoadConfig MUST propagate this error as-is (wrapping optional). Do
        NOT silently fall back to defaults on a missing file at a non-empty path
        — that "no file → defaults" decision belongs to S2 discovery, which
        decides whether to call LoadConfig("") or LoadConfig(foundPath).
- url: https://pkg.go.dev/testing
  why: t.TempDir() returns a per-test scratch dir that is auto-cleaned; the
        idiomatic place to write throwaway JSON fixtures for file-based tests.
  critical: Write fixtures under t.TempDir(), not the repo tree, so tests never
        pollute the working directory or collide with testdata/ (which is for
        golden SSE fixtures in P1.M3.T2).
```

### Current Codebase tree (after P1.M1.T1.S1, which this depends on)

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22; NO requires (from T1.S1)
  doc.go            # "Package main ..." comment (from T1.S1)
  main.go           # package main + func main() {} STUB (from T1.S1; filled in P1.M1.T4)
  testdata/.gitkeep # dir placeholder (from T1.S1; P1.M3.T2 adds *.sse)
  PRD.md            # unchanged
  plan/...          # unchanged (READ-ONLY planning scaffolding)
# NOTE: no config.go, no *_test.go yet. This subtask adds config.go + config_test.go.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod            # UNCHANGED
  doc.go            # UNCHANGED
  main.go           # UNCHANGED (still an empty stub — do NOT wire LoadConfig in)
  config.go         # NEW — type Config + DefaultConfig() + LoadConfig(path)   [THIS SUBTASK]
  config_test.go    # NEW — table-driven tests for defaults + partial + unknown + invalid [THIS]
  testdata/.gitkeep # UNCHANGED
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: JSON tags are snake_case, NOT the Go field names.
//   Go field        JSON key (per PRD §14.1 "JSON form")
//   Upstream     -> "upstream"
//   Listen       -> "listen"
//   Path         -> "path"
//   Aliases      -> "aliases"
//   TargetParam  -> "target_param"   <-- snake_case of TargetParam, NOT "targetParam"
//   LogLevel     -> "log_level"      <-- snake_case of LogLevel,   NOT "logLevel"
// If you omit a tag, encoding/json falls back to the field name (CamelCase),
// which would NOT match the PRD's snake_case keys and would silently break
// file loading. Every field MUST carry its snake_case tag.

// CRITICAL: The "field-by-field override; omitted fields keep defaults" merge is
// JUST `cfg := DefaultConfig(); json.Unmarshal(data, &cfg)`. Do NOT hand-roll a
// field walk and do NOT pull in a third-party merge library (mergo etc.) — stdlib
// only. Verified: json.Unmarshal reuses the existing struct and leaves absent
// fields untouched (see research/json-merge-probe.md, Case A).

// CRITICAL: Do NOT use DisallowUnknownFields. PRD §14.3 says "Unknown fields are
// ignored." Plain json.Unmarshal ignores them (verified, Case B). DisallowUnknownFields
// is a *Decoder* option and would turn a harmless extra key into a hard error.

// GOTCHA (edge cases — do NOT need tests in S1, but do NOT crash on them):
//   {"aliases": null}  -> cfg.Aliases becomes nil           (verified, Case C)
//   {"aliases": []}    -> cfg.Aliases becomes empty non-nil (verified, Case D)
// Both yield an empty alias list. The rewrite layer (P1.M2.T1) already guards
// len(aliases)==0, so this is safe downstream. S1 does not need to special-case it.

// SCOPE GUARD: LoadConfig(path) is a PRIMITIVE — it takes a path and merges.
// Do NOT implement inside it: WSPF_CONFIG / XDG path discovery, env overrides
// (WSPF_UPSTREAM/WSPF_LISTEN/WSPF_LOG_LEVEL), validation (Listen parses,
// Upstream is absolute URL), or "TargetParam forced to search_query if empty".
// All of those are P1.M1.T2.S2 (§14.3). Adding them here duplicates/collides
// with S2 and breaks the scope boundary. On a non-empty path that is missing,
// RETURN the os.ReadFile error — do NOT fall back to defaults (that is S2's job).

// CONVENTION: config.go and config_test.go are BOTH `package main` at the repo
// root, matching go.mod + doc.go + main.go from T1.S1 and the *_test.go naming
// in PRD §9 (rewrite_test.go etc.). Do not create an internal/config/ package.

// CONVENTION: This is the project's FIRST *_test.go. It sets the pattern:
//   - stdlib "testing" only
//   - table-driven with t.Run subtests
//   - t.TempDir() for throwaway file fixtures (never write into the repo tree)
//   - reflect.DeepEqual for struct/slice comparison
// rewrite_test.go / sse_test.go / proxy_test.go will follow this lead.
```

## Implementation Blueprint

### Data models and structure

```go
// Config is the resolved configuration for the web-search-prime-fixer proxy.
//
// Fields are populated by overlaying a JSON config file onto the built-in
// defaults (see DefaultConfig and LoadConfig). Every field has a snake_case JSON
// key used when reading a config file (PRD §14.1 "JSON form").
type Config struct {
	// Upstream is the z.ai MCP endpoint the proxy forwards to.
	// JSON key: "upstream".
	Upstream string `json:"upstream"`

	// Listen is the local bind address (host:port). Local only (127.0.0.1).
	// JSON key: "listen".
	Listen string `json:"listen"`

	// Path is reserved (informational; default "/mcp"). The proxy forwards all
	// non-/healthz paths to Upstream regardless of this value.
	// JSON key: "path".
	Path string `json:"path"`

	// Aliases is the ordered list of argument keys renamed to TargetParam when
	// present in a tools/call request. Order matters: the first present alias is
	// promoted when the target is absent.
	// JSON key: "aliases".
	Aliases []string `json:"aliases"`

	// TargetParam is the canonical parameter aliases are renamed to
	// (always "search_query").
	// JSON key: "target_param".
	TargetParam string `json:"target_param"`

	// LogLevel is one of debug | info | warn | error.
	// JSON key: "log_level".
	LogLevel string `json:"log_level"`
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY PREDECESSOR (read-only, no edits)
  - RUN: `ls go.mod doc.go main.go` in the repo root
    (/home/dustin/projects/web-search-prime-fixer).
  - EXPECT: all three exist (created by P1.M1.T1.S1). go.mod contains
    `module web-search-prime-fixer` and `go 1.22` with NO require block.
  - RUN: `go build ./...` — MUST already exit 0 (the module compiles).
  - WHY: This subtask's INPUT is "Module from P1.M1.T1.S1". If these are absent
    or the module doesn't build, STOP — T1.S1 has not landed yet (parallel
    execution). Do not create go.mod/main.go here.

Task 1: CREATE config.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/config.go
  - PACKAGE: `package main`
  - IMPORTS: `"encoding/json"`, `"os"`  (stdlib only — both in /usr/lib/go/src)
  - IMPLEMENT: the `type Config` struct EXACTLY as in "Data models and structure"
    above (field order Upstream, Listen, Path, Aliases, TargetParam, LogLevel;
    every field carries its snake_case JSON tag; godoc comment on the type and
    each field per the contract's "[Mode A] doc comment" deliverable).
  - IMPLEMENT DefaultConfig() returning PRD §14.2 verbatim:
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
    (EXACT strings/slice — copy verbatim. Aliases order matters for P1.M2.T1.)
  - IMPLEMENT LoadConfig(path string) (Config, error):
        func LoadConfig(path string) (Config, error) {
            cfg := DefaultConfig()
            if path == "" {
                return cfg, nil
            }
            data, err := os.ReadFile(path)
            if err != nil {
                return cfg, err
            }
            if err := json.Unmarshal(data, &cfg); err != nil {
                return cfg, err
            }
            return cfg, nil
        }
  - GOTCHA: `cfg` is seeded with defaults BEFORE Unmarshal so omitted fields
    survive — verified in research/json-merge-probe.md. Do NOT re-zero cfg.
  - GOTCHA: do NOT call DisallowUnknownFields; do NOT validate; do NOT read env.
  - WHY: This is the S1 primitive consumed by S2 (discovery) and P1.M1.T4 (boot).

Task 2: CREATE config_test.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/config_test.go
  - PACKAGE: `package main`
  - IMPORTS: `"os"`, `"path/filepath"`, `"reflect"`, `"testing"`
  - IMPLEMENT the following tests (table-driven where natural; stdlib only):
    (a) TestDefaultConfig:
            def := DefaultConfig()
            assert def.Upstream    == "https://api.z.ai/api/mcp/web_search_prime/mcp"
            assert def.Listen      == "127.0.0.1:8787"
            assert def.Path        == "/mcp"
            assert reflect.DeepEqual(def.Aliases,
                []string{"query","q","search","searchQuery","search_term"})
            assert def.TargetParam == "search_query"
            assert def.LogLevel    == "info"
        (use a small helper or inline t.Errorf per field; a reflect.DeepEqual
        against a literal Config{...} is also acceptable and DRY.)
    (b) TestLoadConfig_EmptyPath:
            cfg, err := LoadConfig("")
            assert err == nil
            assert reflect.DeepEqual(cfg, DefaultConfig())
    (c) TestLoadConfig_FromFile (TABLE-DRIVEN, subtests via t.Run):
        each row writes its JSON to <t.TempDir()>/config.json and calls
        LoadConfig(path). Columns: name, jsonBody, wantErr, want (Config).
        Rows:
          1. "partial_override_keeps_defaults":
               jsonBody = `{"listen":"0.0.0.0:9999","aliases":["foo"]}`
               wantErr  = false
               want     = DefaultConfig() with Listen="0.0.0.0:9999"
                          and Aliases=[]string{"foo"} (everything else default)
                          -> proves Upstream/Path/TargetParam/LogLevel stay default.
          2. "unknown_fields_ignored":
               jsonBody = `{"upstream":"http://example.invalid/mcp","banana":42,"nested":{"a":1},"log_level":"warn"}`
               wantErr  = false
               want     = DefaultConfig() with Upstream="http://example.invalid/mcp"
                          and LogLevel="warn" (banana/nested dropped silently).
          3. "full_file_all_fields":
               jsonBody = `{"upstream":"http://u","listen":"127.0.0.1:1","path":"/p","aliases":["a","b"],"target_param":"search_query","log_level":"debug"}`
               wantErr  = false
               want     = Config{Upstream:"http://u",Listen:"127.0.0.1:1",
                          Path:"/p",Aliases:[]string{"a","b"},
                          TargetParam:"search_query",LogLevel:"debug"}
          4. "invalid_json_returns_error":
               jsonBody = `{not valid json`
               wantErr  = true
               (do not assert cfg contents when wantErr is true)
        In each subtest:
          - dir := t.TempDir(); p := filepath.Join(dir, "config.json")
          - os.WriteFile(p, []byte(tt.jsonBody), 0o644)
          - got, err := LoadConfig(p)
          - if (err != nil) != tt.wantErr { t.Fatalf(...) }
          - if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
                t.Errorf("LoadConfig mismatch:\n got  %+v\n want %+v", got, tt.want) }
        Build `want` for rows 1-2 from DefaultConfig() with a couple fields
        reassigned, so the test automatically tracks any future default change.
    (d) TestLoadConfig_MissingFile (NOT in the table; no file is written):
            cfg, err := LoadConfig(filepath.Join(t.TempDir(), "does-not-exist.json"))
            assert err != nil            (a non-empty path to a missing file is an error)
            (do not assert cfg; it is a partial/DefaultConfig value on the error path)
  - WHY: Covers the contract's three required cases (defaults / partial /
    unknown) plus invalid-JSON and missing-file for robustness and to pin the
    S1/S2 boundary (missing file at a given path is an ERROR in S1; S2 decides
    whether to even call LoadConfig with that path).

Task 3: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w config.go config_test.go        # format (or rely on editor)
        go build ./...                           # MUST exit 0, no output
        go vet ./...                             # MUST exit 0, no output
        gofmt -l .                               # MUST print nothing
        go test ./...                            # MUST pass, including config_test
        go test -run 'TestDefaultConfig|TestLoadConfig' -v ./...   # see subtests
  - EXPECT: all clean; `go test -v` shows TestDefaultConfig, TestLoadConfig_EmptyPath,
    TestLoadConfig_FromFile/{partial_override_keeps_defaults,unknown_fields_ignored,
    full_file_all_fields,invalid_json_returns_error}, TestLoadConfig_MissingFile all PASS.
  - RE-RUN after any edit until all are clean.
```

### Implementation Patterns & Key Details

```go
// The entire non-obvious logic of this subtask is the merge primitive. It is
// three lines, and its behavior is verified (see research/json-merge-probe.md):

func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig() // 1. start from the built-in defaults
	if path == "" {        // 2. no file requested -> defaults are the whole config
		return cfg, nil    //    (this is how the proxy runs with NO config file)
	}
	data, err := os.ReadFile(path) // 3a. read the named file; missing -> error
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil { // 3b. overlay; unknown fields ignored
		return cfg, err
	}
	return cfg, nil
}

// Why this works (verified): encoding/json's Unmarshal "reuses the existing
// struct, keeping the fields that were not present in the input JSON." So a file
// like {"listen":"0.0.0.0:9999"} leaves Upstream/Path/Aliases/TargetParam/
// LogLevel at their DefaultConfig() values — exactly "field-by-field override;
// omitted fields keep defaults".

// Test equality: use reflect.DeepEqual (slices compare element-wise).
//   reflect.DeepEqual(got, want)            // whole struct
//   reflect.DeepEqual(def.Aliases, []string{"query","q","search","searchQuery","search_term"})
```

### Integration Points

```yaml
MODULE (from T1.S1, UNCHANGED here):
  - name: "web-search-prime-fixer"
  - directive: "go 1.22"
  - dependencies: "NONE"   # config.go uses only encoding/json + os (stdlib)

PACKAGE:
  - name: "main"           # config.go + config_test.go are package main, repo root

SYMBOLS INTRODUCED (consumed by later subtasks):
  - type Config struct                 # -> S2 (discovery/override/validate), P1.M1.T3 (LogLevel),
                                       #    P1.M1.T4 (Listen/Upstream/Path), P1.M2.T1 (Aliases/TargetParam)
  - func DefaultConfig() Config        # -> S2 fallback when no file found; P1.M1.T4 bootstrap
  - func LoadConfig(path string) (Config, error)  # -> S2 calls this with the discovered path,
                                       #    then applies env overrides + validation on the returned Config

NO INTEGRATION POINTS IN main() THIS SUBTASK:
  - Do NOT call LoadConfig from main(). Do NOT add flag parsing. The bootstrap
    (flag -> discover path -> LoadConfig -> env override -> validate -> server)
    is P1.M1.T4 / S2. main() remains the empty stub from T1.S1.

NO ENV VARS / NO VALIDATION THIS SUBTASK:
  - WSPF_CONFIG, WSPF_UPSTREAM, WSPF_LISTEN, WSPF_LOG_LEVEL are S2 (§14.3).
  - Listen/Upstream validation and TargetParam-forcing are S2 (§14.3).
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -l config.go config_test.go        # MUST print nothing (already formatted)
go vet ./...                             # static checks; MUST exit 0, no output
go build ./...                           # MUST exit 0, no output

# Dependency-free invariant (PRD §6/§9) — grep the new file for stray imports:
grep -E '^\s*"github\.com|^\s*"gopkg\.in|^\s*"golang\.org/x' config.go config_test.go || true
# Expected: empty. Only encoding/json, os, path/filepath, reflect, testing allowed.

# Expected: gofmt silent; vet/build clean; no third-party imports.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...                                          # MUST pass (exit 0)
go test -run 'TestDefaultConfig|TestLoadConfig' -v ./...   # see the subtests

# Expected: PASS for TestDefaultConfig, TestLoadConfig_EmptyPath,
#   TestLoadConfig_FromFile/partial_override_keeps_defaults,
#   TestLoadConfig_FromFile/unknown_fields_ignored,
#   TestLoadConfig_FromFile/full_file_all_fields,
#   TestLoadConfig_FromFile/invalid_json_returns_error,
#   TestLoadConfig_MissingFile.
# If a case fails, READ the diff in the t.Errorf output (got vs want) — a
# mismatch almost always means a wrong JSON tag or a default value typo.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Smoke: the module still builds a binary with the new file present.
go build -o /tmp/wspf-cfg-smoke . && /tmp/wspf-cfg-smoke; echo "exit=$?"
rm -f /tmp/wspf-cfg-smoke
# Expected: builds; running it exits 0 with no output (main() is still the empty
# T1.S1 stub — LoadConfig is NOT wired into main in this subtask).

# Sanity: DefaultConfig + LoadConfig behave as a library when called via a
# one-off program (optional, belt-and-suspenders). Skip if go test above passes.
cat > /tmp/cfg_smoke.go <<'EOF'
//go:build ignore
package main
import ("fmt"; "os")
func main() {
    os.Chdir("/home/dustin/projects/web-search-prime-fixer") // ensure module root not needed for this
}
EOF
# (The authoritative integration check is `go test ./...`; this smoke step is
#  optional. LoadConfig's real caller — the bootstrap — arrives in P1.M1.T4.)
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# godoc check — the "[Mode A] doc comment" deliverable on type Config:
go doc . Config 2>/dev/null || go doc . 2>/dev/null | sed -n '/type Config/,/^$/p'
# Expected: prints the Config type with its field doc comments + JSON tags visible.

# Verify the six snake_case JSON tags are present (grep the source):
grep -c 'json:"upstream"\|json:"listen"\|json:"path"\|json:"aliases"\|json:"target_param"\|json:"log_level"' config.go
# Expected: 6 (one per field). If <6, a field is missing its tag and file loading
# will silently fail for that field.

# Verify the exact default values are present (grep the source):
grep -F '"https://api.z.ai/api/mcp/web_search_prime/mcp"' config.go   # Upstream default
grep -F '127.0.0.1:8787' config.go                                    # Listen default
grep -F '"query", "q", "search", "searchQuery", "search_term"' config.go  # Aliases default
grep -F 'TargetParam: "search_query"' config.go                       # TargetParam default
grep -F 'LogLevel:    "info"' config.go                               # LogLevel default
# Expected: each grep matches exactly once.

# Scope guard: confirm NO S2/discovery/validation logic leaked into config.go.
! grep -E 'WSPF_CONFIG|WSPF_UPSTREAM|WSPF_LISTEN|WSPF_LOG_LEVEL|DisallowUnknownFields|XDG_CONFIG_HOME|os.Getenv' config.go
# Expected: the negated grep succeeds (exit 0) — none of those tokens appear.
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0, no output.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` passes (exit 0), including all `config_test.go` subtests.
- [ ] `config.go` imports only `encoding/json` and `os`.
- [ ] `config_test.go` imports only `os`, `path/filepath`, `reflect`, `testing`.

### Feature Validation

- [ ] `type Config` has exactly the six §14.1 fields in order, each with its snake_case JSON tag.
- [ ] `DefaultConfig()` returns the §14.2 defaults verbatim (exact strings + alias slice order).
- [ ] `LoadConfig("")` returns defaults, nil error (proxy runs with no config file).
- [ ] `LoadConfig(path)` partial file overrides only named fields (omitted keep defaults).
- [ ] `LoadConfig(path)` with unknown fields returns no error.
- [ ] `LoadConfig(path)` with invalid JSON returns a non-nil error.
- [ ] `LoadConfig(path)` with a missing file returns a non-nil error (S1/S2 boundary).
- [ ] `go doc . Config` shows the type + field doc comments (Mode A DOCS deliverable).

### Code Quality Validation

- [ ] Follows PRD §9 file layout (config.go + config_test.go at repo root, package main).
- [ ] Sets the project's test conventions (stdlib `testing`, `t.TempDir()`, table-driven, `reflect.DeepEqual`).
- [ ] Doc comments on `type Config` and each field (contract "[Mode A]").
- [ ] Scope boundary respected: no discovery/env/validation/main-wiring/config.example.json.

### Documentation & Deployment

- [ ] `type Config` doc comment documents each field and its snake_case JSON key (contract deliverable).
- [ ] No environment variables introduced (env handling is S2).
- [ ] No external doc file created (config.example.json is P1.M5.T3 — explicitly out of scope here).

---

## Anti-Patterns to Avoid

- ❌ Don't omit JSON tags or use CamelCase tags — PRD §14.1 JSON form is snake_case (`target_param`, `log_level`). A missing/wrong tag silently breaks file loading.
- ❌ Don't hand-roll a field-merge or pull in a merge library — `cfg := DefaultConfig(); json.Unmarshal(data, &cfg)` already does field-by-field override (verified).
- ❌ Don't use `DisallowUnknownFields` — §14.3 requires unknown fields be ignored.
- ❌ Don't implement §14.3 in S1 (env vars WSPF_*, XDG discovery, Listen/Upstream validation, TargetParam-forcing) — that is P1.M1.T2.S2. Adding it now duplicates/collides with S2.
- ❌ Don't make `LoadConfig` fall back to defaults on a *missing file at a non-empty path* — return the `os.ReadFile` error. The "no file → defaults" decision is S2's (it chooses whether to call `LoadConfig("")`).
- ❌ Don't wire `LoadConfig` into `main()`, add flag parsing, or touch `go.mod`/`doc.go`/`main.go`/`testdata/` — those belong to T1.S1 (done) / P1.M1.T4 (later).
- ❌ Don't create `config.example.json`, `README.md`, or any other PRD §9 file — out of scope (config.example.json is P1.M5.T3).
- ❌ Don't write test fixtures into the repo tree or `testdata/` — use `t.TempDir()` (testdata/ is reserved for P1.M3.T2 SSE golden fixtures).
- ❌ Don't assert on the returned `Config` in the error-path tests (invalid JSON / missing file) — on error it may be partial; assert only `err != nil`.

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is small and fully specified — the struct contents,
JSON tags, default values, and the `LoadConfig` body are given verbatim above
(copied from PRD §14.1/§14.2). The one genuinely non-obvious behavior
(`json.Unmarshal` over a pre-populated struct keeping omitted fields at default
while ignoring unknown fields, WITHOUT `DisallowUnknownFields`) is **verified
on-disk** against the installed `go1.26.4` toolchain in
`research/json-merge-probe.md`. The tests are spelled out case-by-case with
expected `want` values derived from `DefaultConfig()`. The residual risk is an
agent (a) mis-scoping into S2 territory (env/validation/discovery) — mitigated
by the SCOPE GUARD, the Anti-Patterns list, and the `! grep WSPF_*/XDG/Getenv`
Level-4 check; or (b) a JSON-tag typo (`targetParam` vs `target_param`) —
mitigated by the explicit tag table, the Level-4 `grep -c ... == 6` check, and
the `unknown_fields_ignored` test (a wrong tag would itself show up as a
field that fails to override). Hence 9, not 10.
