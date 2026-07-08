name: "P1.M2.T1.S1 — Core extraction types + scalar/string/array precedence (levels 1-3)"
description: |

  Ship `extract.go` (the `ExtractionResult` type + the `extract` query-extraction
  entry point handling PRD §10.1 precedence levels 1-3: bare-string, scalar,
  array) and `extract_test.go` (table-driven, all level 1-3 + failure rows).
  Stubs the object case (level 4) as Found=false — that is S2's job. Zero new
  deps; stdlib `encoding/json` + `fmt` only. Consumed/extended by S2 (objects)
  and S3 (optionals + inference + failure).

---

## Goal

**Feature Goal**: Deliver the first slice of **PRD §10 — Query extraction**: the
`ExtractionResult` struct and a deterministic, pure `extract()` function that
recovers a search query string from a `json.RawMessage` arguments payload for
precedence **levels 1–3** (bare string → `bare-string`; number/boolean →
`scalar`; array → first-yielding element → `array[0]`), and deterministically
reports **failure** (`Found=false`) for nil/null/empty/invalid inputs and —
temporarily in this subtask — for object inputs (level 4 is S2). The function
must be pure and side-effect-free, dispatching on the Go type produced by
`json.Unmarshal` into `any`.

**Deliverable**: Two NEW files at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`,
**zero modifications to any existing file**:
1. **CREATE** `extract.go` — the `ExtractionResult` struct (exact fields per
   contract), the public `func extract(raw json.RawMessage, queryAliases
   []string, optionalAliases map[string][]string) ExtractionResult` entry point,
   an unexported `extractValue(v any, ...) ExtractionResult` type-switch
   dispatcher (the S2 extension seam), and a Mode-A doc comment on `extract`.
   Imports only `encoding/json` + `fmt`.
2. **CREATE** `extract_test.go` — a table-driven `TestExtract` covering every
   level 1-3 shape + every failure shape (nil raw, empty raw, `null`, empty
   array, invalid JSON) + the two S1↔S2 boundary rows (`{}` empty object and
   `[{"query":"x"}]` array-of-objects both `Found=false` until S2). Asserts the
   whole `ExtractionResult` via `reflect.DeepEqual`.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run TestExtract -v`) all exit clean. Every level
1-3 row yields the exact `Query`/`Source`/`Found` (string→`bare-string`; number
or bool→`scalar` via `fmt.Sprint`; array→first element that yields, `array[0]`).
Every failure row yields the zero-value `ExtractionResult{}`. `go.mod` gains zero
`require`s (only stdlib). The `extract`/`ExtractionResult` signatures exactly
match the contract so S2 and S3 consume/extend them without edits, and the
`map[string]any` case is an explicit Found=false stub that S2 replaces.

## User Persona

**Target User**: (1) downstream subtask implementers — S2 extends the object case
in `extractValue`; S3 adds inference/optionals/failure; the server dispatch
(P1.M5.T2) calls `extract(req.Params.Arguments, cfg.QueryAliases,
cfg.OptionalAliases)`. (2) the end MCP client/agent, whose malformed
`tools/call` arguments (a bare string, a number, an array) get coerced into a
usable `search_query`. (3) the maintainer, who gets a pure, deterministic,
table-tested function with no I/O.

**Use Case**: An agent sends `tools/call` with `arguments: "rust async"` (a bare
string, not the expected object). The server handler calls `extract(raw, …)`;
extract unmarshals to `string`, returns `{Query:"rust async", Source:"bare-string",
Found:true}`. The server forwards `search_query="rust async"` upstream and (per
M3) appends a teaching warning. An agent sending `arguments: 42` gets coerced to
`"42"` (`scalar`). An agent sending `arguments: ["x"]` gets `"x"` (`array[0]`).

**User Journey**: raw bytes in → (empty guard) → `json.Unmarshal`→`any` →
type-switch: string/scalar/array/nil → (array recurses on elements) →
`ExtractionResult` out (Query + Source + Found).

**Pain Points Addressed**: (1) without coercion, a bare-string/number/array
argument would yield no query → false failure → no upstream call (FR-6), even
though the agent clearly meant to search. (2) S2/S3 need a stable, single
type-switch seam to extend (`extractValue`) rather than a rewrite. (3)
determinism: the dispatch is on the decoded Go TYPE (not map iteration), so it is
automatically deterministic for levels 1-3.

## Why

- Implements **PRD §5 FR-4** ("extract a query from ANY input"), **§10.1
  precedence levels 1-3**, **§10.1.5 failure** (Found=false), and **§10.4**
  (source is recorded for logging/surfacing).
- Honors **architecture/mcp_sdk_api.md §1** (handler receives
  `req.Params.Arguments` as `json.RawMessage`; unmarshal-then-dispatch) and the
  determinism note from **architecture/external_deps.md §6** (session 001) —
  though that note is load-bearing only in S2's map walk, S1 establishes the
  no-map-iteration discipline.
- Establishes the **stable seam** S2 (object alias scan + nested drill-in) and
  S3 (inference + optionals + failure path + clean payload) extend, and that
  P1.M5.T2 (server dispatch) calls. The `ExtractionResult` fields and the
  `extract`/`extractValue` signatures are the cross-subtask contract.
- Pure function with no I/O/config/globals → fully unit-testable in isolation;
  the M1 test gate (P1.M1.T3.S1, running in parallel) keeps `go test ./...`
  green and this subtask must not break it.

## What

`extract.go` (NEW, `package main`) exports/defines exactly:

- **`type ExtractionResult struct { Query string; Source string; Ambiguous bool;
  Optionals map[string]any; Found bool }`** — verbatim from the contract. S1
  populates only `Query`, `Source`, `Found` (levels 1-3); `Ambiguous` stays
  `false` and `Optionals` stays `nil` until S3. Invariants: `Found==true` ⟹
  `Query != ""`; `Found==false` ⟹ the whole result is the zero value.
- **`func extract(raw json.RawMessage, queryAliases []string, optionalAliases
  map[string][]string) ExtractionResult`** — the public entry point. Pure,
  deterministic, no side effects. Empty-raw guard → `json.Unmarshal` into `any`
  → delegate to `extractValue`. `queryAliases`/`optionalAliases` are accepted for
  signature stability and threaded to recursion but NOT read by levels 1-3
  (objects/S3 read them).

And unexported:

- **`func extractValue(v any, queryAliases []string, optionalAliases
  map[string][]string) ExtractionResult`** — the type-switch dispatcher. THIS is
  the S2 extension seam: S2 replaces the `case map[string]any:` body.

### Algorithm (levels 1-3 + failure; object stubbed)

```text
extract(raw, q, opt):
  1. if len(raw) == 0               -> return {} (Found=false)   // missing arguments field
  2. v, err := json.Unmarshal(raw)  -> if err: return {} (Found=false)  // invalid JSON
  3. return extractValue(v, q, opt)

extractValue(v, q, opt) — type switch on v:
  case nil:           return {}                                   // JSON null
  case string:        return {Query: v, Source: "bare-string", Found: true}
  case float64, bool: return {Query: fmt.Sprint(v), Source: "scalar", Found: true}
  case []any:         for each elem (index order): r := extractValue(elem, q, opt);
                        if r.Found: return {Query: r.Query, Source: "array[0]",
                        Ambiguous: r.Ambiguous, Optionals: r.Optionals, Found: true}
                      return {}                                   // no element yielded
  case map[string]any: return {}                                  // LEVEL 4 — S2 (stub)
  default:            return {}                                   // unreachable for valid JSON
```

`extract_test.go` (NEW, `package main`) — see Validation Loop Level 2.

### Success Criteria

- [ ] `extract.go` defines `ExtractionResult{Query, Source, Ambiguous, Optionals,
      Found}` with EXACTLY those fields/types (consumers depend on them).
- [ ] `extract(raw, queryAliases []string, optionalAliases map[string][]string)`
      has EXACTLY that signature (contract); `extractValue` is the internal
      type-switch seam S2 extends.
- [ ] Empty/nil `raw` returns the zero `ExtractionResult` WITHOUT calling
      `json.Unmarshal` (guard first); invalid JSON also returns zero.
- [ ] string → `bare-string`; float64/bool → `scalar` via `fmt.Sprint(v)` (NOT a
      literal "42" hardcode — must use Sprint); array → first-yielding element,
      `array[0]`; nil/null → Found=false.
- [ ] The array case returns the FIRST element (index order) that yields; a
      non-yielding first element (e.g. `[]`, or an object in S1) is SKIPPED.
- [ ] The array case overrides `Source="array[0]"` but PRESERVES Query/Ambiguous/
      Optionals from the inner result (forward-compat for S2/S3).
- [ ] `case map[string]any:` is an explicit `return ExtractionResult{}` STUB with
      a comment naming P1.M2.T1.S2 (level 4). No map is iterated anywhere in S1.
- [ ] `extract` has a Mode-A doc comment covering precedence levels 1-3, the
      exact source labels (`bare-string`/`scalar`/`array[0]`), the failure cases,
      and "pure + deterministic".
- [ ] `go.mod` gains zero `require`s; `extract.go` imports only `encoding/json` +
      `fmt`; `extract_test.go` imports only `encoding/json` + `reflect` + `testing`.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean
      (the parallel M1 test gate must stay green).

## All Needed Context

### Context Completeness Check

_Pass._ The work-item contract fixes the `ExtractionResult` fields, the `extract`
signature, the source labels, and the level 1-3 algorithm verbatim. The two
non-obvious Go behaviors it rests on — `json.Unmarshal`→`any` type dispatch
(string/float64-all-numbers/bool/`[]any`/`map[string]any`/nil) and
`fmt.Sprint(float64(42))`→`"42"` — are **VERIFIED in a throwaway go1.25 module**
(see research note; the empty-raw-before-Unmarshal guard is also verified). The
input shape (`req.Params.Arguments` is `json.RawMessage`, mcp_sdk_api.md §1, read
from SDK v1.6.1 source) is confirmed. The config the real caller passes
(`cfg.QueryAliases`, `cfg.OptionalAliases`) is read on-disk from `config.go`; the
test defines its own local aliases to stay decoupled. The test conventions
(table-driven, `reflect.DeepEqual`, `package main`) are read on-disk from
`config_test.go`. extract.go has NO dependency on the in-parallel M1 test rewrite
(P1.M1.T3.S1) — it is a standalone unit, no file/symbol overlap, so there is no
coordination risk; both must simply keep `go test ./...` green. An agent with no
prior knowledge can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative precedence + failure semantics.
- file: PRD.md
  why: §10.1 (precedence levels 1-3 verbatim + level 5 failure); §10.4 (source
        is recorded/logged/surfaced — Source is the contract label); §5 FR-4
        (extract a query from ANY input: bare string, numeric/boolean coercion,
        array first-usable-element); §19.1 (extract_test.go is table-driven over
        every structure; assert query, source label, found/failure).
  critical: §10.1 says array → "recurse on the first element that yields a query
        (source `array[0]`)" — Source is "array[0]", NOT the inner element's source.
        §10.1.5 failure = Found=false (no upstream call). S1 does ONLY levels 1-3
        + failure; the object case (§10.1.4) is S2.

# VERIFIED SDK INPUT SHAPE — what extract receives.
- file: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§1 SERVER SIDE — CallToolParamsRaw / ToolHandler"
  why: PROVES the tool handler gets `req.Params.Arguments` as `json.RawMessage`
        (raw bytes). So extract must `json.Unmarshal(raw, &v)` into `any` then
        dispatch on v's type. Verified from go-sdk@v1.6.1 source (cited file:line).
  critical: Arguments is `json.RawMessage` with `json:"arguments,omitempty"` — a
        missing arguments field arrives as nil/zero-length RawMessage. Guard
        len(raw)==0 BEFORE Unmarshal (Unmarshal on empty bytes is an error).

# DETERMINISM NOTE — load-bearing in S2, harmless in S1.
- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§6. Go map ordering is non-deterministic"
  why: PROVES alias scanning must walk the ordered QueryAliases SLICE by index,
        never range the decoded map. S1 levels 1-3 do NOT iterate any map
        (objects stubbed), so this is automatic now — but extract.go must never
        introduce `for k := range map` to pick a query (S2 enforces the rule).
  critical: Sets the discipline for S2. S1 has zero map iteration by construction.

# VERIFIED GO BEHAVIORS — the dispatch table + Sprint results (read once).
- docfile: plan/002_0a8ab3410994/P1M2T1S1/research/extraction-dispatch-and-seam.md
  why: VERIFIED (throwaway go1.25 module) table of json.Unmarshal→any types and
        fmt.Sprint outputs; the empty-raw guard; the recursion design
        (extract+extractValue); the exact S1↔S2 seam; source labels.
  critical: fmt.Sprint(float64(42))=="42" (no ".0"); numbers→float64 (no
        json.Number); empty/whitespace raw → Unmarshal error → guard first.

# CONFIG (read-only) — the real caller's args; extract.go does NOT import config.
- file: config.go
  why: shows the real call `extract(raw, cfg.QueryAliases, cfg.OptionalAliases)`.
        QueryAliases is the 14-entry []string; OptionalAliases is the 3-key
        map[string][]string. extract takes these as PLAIN params (no Config) so it
        stays pure/unit-testable. S1 threads them but does not read them.
  pattern: function takes raw + []string + map[string][]string, NOT Config.

# TEST CONVENTIONS — table-driven style to follow.
- file: config_test.go
  why: the project's table-driven convention: `tests := []struct{...}{...}`;
        `for _, tc := range tests { t.Run(tc.name, ...) }`; `reflect.DeepEqual`
        for struct comparison; `package main`. extract_test.go mirrors this.
  pattern: belt-and-suspenders where helpful; derive local aliases from
        DefaultConfig() values but declare them locally (decouple from config.go).

# PARALLEL WORK (CONTRACT) — what exists when this subtask starts.
- file: plan/002_0a8ab3410994/P1M1T3S1/PRP.md
  why: M1 test rewrite (config_test/resolve_test/health_test) running in parallel.
        extract.go shares NO symbols/files with it → zero conflict. Both must keep
        `go test ./...` green. logger_test.go is already green and untouched.
  critical: Do NOT touch config_test.go/resolve_test.go/health_test.go (T3.S1
        owns them). Do NOT modify any production file. extract.go is standalone.

# Go stdlib refs (stable, used here).
- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: Unmarshal into `*any` yields the type dispatch table (string/float64/bool/
        []any/map[string]any/nil). Default behavior (no UseNumber) → all numbers
        are float64.
- url: https://pkg.go.dev/fmt#Sprint
  why: stringify scalars per contract. Sprint(float64(42))=="42", Sprint(true)=="true".
```

### Current Codebase tree (the INPUT state — after parallel M1; run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (still v1 wording; rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap: ResolveConfig, SDK StreamableHTTPHandler mount, authMiddleware
  config.go       # Config{...,QueryAliases[]string,OptionalAliases map[string][]string,...}
  logger.go       # newLogger/log/redactHeaders
  health.go       # healthHandler, var version
  config_test.go  # (being rewritten by P1.M1.T3.S1 in parallel — DO NOT TOUCH)
  resolve_test.go # (being rewritten by P1.M1.T3.S1 in parallel — DO NOT TOUCH)
  health_test.go  # (being rewritten by P1.M1.T3.S1 in parallel — DO NOT TOUCH)
  logger_test.go  # green, untouched
  testdata/, README.md, config.example.json, PRD.md
  # --- ABSENT (this subtask creates them): ---
  # extract.go       <- NEW (this subtask)
  # extract_test.go  <- NEW (this subtask)
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
extract.go       # NEW. ExtractionResult struct + extract(raw) entry + extractValue(v)
                  #      type-switch (levels 1-3 + nil/null/empty/invalid failure;
                  #      object case = Found=false stub for S2). encoding/json + fmt.
extract_test.go   # NEW. Table-driven TestExtract: all level 1-3 shapes + failure
                  #      shapes + the two S1↔S2 boundary rows (object/array-of-object
                  #      Found=false until S2). encoding/json + reflect + testing.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: json.Unmarshal([]byte(""), &v) and whitespace-only raw BOTH error
// ("unexpected end of JSON input"). And req.Params.Arguments has
// json:"arguments,omitempty" — a MISSING arguments field arrives as a zero-length
// RawMessage. GUARD len(raw)==0 BEFORE Unmarshal and return ExtractionResult{}.

// CRITICAL: json.Unmarshal into `any` (default, no Decoder.UseNumber) yields
// float64 for ALL JSON numbers (int or float). There is NO `int`, NO `json.Number`.
// So the type switch needs `case float64:` (and `bool:`) for scalars — NOT int.

// CRITICAL: fmt.Sprint(float64(42)) == "42" (NOT "42.0"); Sprint(3.14)=="3.14";
// Sprint(true)=="true"; Sprint(1e20)=="1e+20" (scientific — acceptable per contract
// "stringify via fmt.Sprint"). Use fmt.Sprint(v) verbatim; do NOT hardcode/format.

// CRITICAL (array Source): PRD §10.1.3 fixes the array source label to "array[0]"
// regardless of what the inner element was. Override Source="array[0]" but PRESERVE
// Query/Ambiguous/Optionals from the inner result (forward-compat for S2/S3, e.g.
// an array of objects later yielding query+optionals).

// CRITICAL (array "first that yields"): iterate elements in INDEX ORDER and return
// the FIRST whose recursive extractValue yields Found=true. A non-yielding first
// element (empty array [], or — in S1 — an object) is SKIPPED, not fatal.

// GOTCHA (combined case): `case float64, bool:` is legal; inside it `v` retains
// the original interface type so fmt.Sprint(v) works. Both produce Source="scalar".

// GOTCHA (S2 seam): the `case map[string]any:` body MUST be an explicit
// `return ExtractionResult{}` stub with a "// Level 4 — P1.M2.T1.S2" comment. S2
// replaces exactly that body. Do NOT omit the case (letting it hit default) — the
// explicit stub is the documented contract boundary.

// GOTCHA (determinism, for S2): extract.go must NEVER pick a query by ranging a
// map (`for k := range m`). S1 has no map iteration by construction; S2's alias
// scan must walk the ordered QueryAliases SLICE by index. (external_deps.md §6.)

// GOTCHA (unused param): optionalAliases is accepted but unread in levels 1-3.
// This is INTENTIONAL (signature stability for S2/S3) — legal Go, no vet warning.
// Thread it into extractValue recursion so S2 object elements can use it later.

// GOTCHA (Found invariants): Found==true ⟹ Query!="" (a found query is never
// empty). Found==false ⟹ return the ZERO ExtractionResult{} (Query="", Source="",
// Ambiguous=false, Optionals=nil). Never return Found=false with a non-empty Source.
```

## Implementation Blueprint

### Data models and structure

```go
// ExtractionResult is the entire data surface of this subtask. Fields are fixed
// by the contract and consumed by S2 (extends), S3 (optionals/inference), and the
// server dispatch (P1.M5.T2). Do NOT add/remove/rename fields.
//
// S1 populates only Query/Source/Found. Ambiguous stays false and Optionals stays
// nil until S3 (inference/optionals come from object inputs).
type ExtractionResult struct {
	Query     string
	Source    string
	Ambiguous bool
	Optionals map[string]any
	Found     bool
}
```

No other types. `extract`/`extractValue` take the raw bytes / decoded value plus
the plain alias params (not Config) to stay pure.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: CREATE extract.go
  - DEFINE: `type ExtractionResult struct { Query string; Source string;
        Ambiguous bool; Optionals map[string]any; Found bool }` (field order per
        contract; doc comment on the type noting S1 populates Query/Source/Found).
  - DEFINE: `func extract(raw json.RawMessage, queryAliases []string,
        optionalAliases map[string][]string) ExtractionResult` — empty guard →
        json.Unmarshal (error→{}) → delegate to extractValue.
  - DEFINE: `func extractValue(v any, queryAliases []string, optionalAliases
        map[string][]string) ExtractionResult` — the type switch (THE S2 SEAM):
        nil→{}; string→bare-string; float64,bool→scalar via fmt.Sprint(v);
        []any→recurse+array[0]; map[string]any→{} STUB (comment: S2); default→{}.
  - DOC COMMENT (Mode A) on extract MUST cover: (a) precedence levels 1-3
        (bare-string / scalar / array[0]); (b) the EXACT source labels; (c) the
        failure cases (nil/null/empty/invalid → Found=false; object → Found=false
        pending S2); (d) "pure and deterministic — no I/O, no globals, dispatch on
        the decoded Go type"; (e) note that queryAliases/optionalAliases are read
        only by the object path (S2/S3).
  - IMPORTS: only `encoding/json` and `fmt`.
  - NAMING: ExtractionResult (exported), extract + extractValue (lowercase;
        extract is the public-by-convention entry, extractValue is internal).
  - PLACEMENT: repo root, `package main`.
  - CONSTRAINT: no `for ... range <map>` anywhere (grep check in Level 1). gofmt
        clean.

Task 2: CREATE extract_test.go
  - DEFINE: `TestExtract` table with the rows in Validation Loop Level 2 — every
        level 1-3 shape (bare string; int/float/bool scalar; array of string;
        array first-yields; array skips non-yielding first; nested array; array of
        scalar) + every failure shape (nil raw, empty raw, null, empty array,
        invalid JSON) + the two S1↔S2 boundary rows (empty object {}, array of
        objects [{"query":"x"}]) both Found=false.
  - PER ROW: `got := extract(json.RawMessage(tc.raw), queryAliases, optAliases)`;
        build `want := ExtractionResult{Query: tc.q, Source: tc.src, Found: tc.f}`
        (failure rows → want = ExtractionResult{}); assert
        `reflect.DeepEqual(got, want)` (catches stray Ambiguous/Optionals).
  - DEFINE local `queryAliases` (mirror DefaultConfig's 14) and `optAliases`
        (mirror the 3-key map) so the test is decoupled from config.go.
  - ADD (optional, recommended): `TestExtract_Pure` — call extract twice on the
        same raw, assert identical results (pure/deterministic); and a row
        documenting `arguments`-omitted (`extract(nil, …)` → Found=false).
  - FOLLOW pattern: config_test.go (table-driven, t.Run subtests, reflect.DeepEqual).
  - NAMING: test func `TestExtract` (and `TestExtract_Pure`); subtest names match
        the row semantics (e.g. "bare_string", "scalar_number", "array_first_yields").
  - IMPORTS: only `encoding/json`, `reflect`, `testing`.
  - PLACEMENT: repo root, `package main`, alongside extract.go.
  - COVERAGE: levels 1-3 happy paths; "first element that yields" semantics;
        failure paths; the S1↔S2 object boundary.
```

### Implementation Patterns & Key Details

```go
// extract.go — reference shape (verified against a throwaway go1.25 module).

package main

import (
	"encoding/json"
	"fmt"
)

// ExtractionResult describes the outcome of extracting a search query from a
// tools/call arguments payload. S1 (this subtask) populates Query, Source, and
// Found for precedence levels 1-3; Ambiguous and Optionals are populated by S3
// (inference fallback and optional-parameter normalization) for object inputs.
//
// Invariants: Found==true implies Query != "". Found==false implies the zero
// value (Query=="", Source=="", Ambiguous==false, Optionals==nil).
type ExtractionResult struct {
	Query     string
	Source    string
	Ambiguous bool
	Optionals map[string]any
	Found     bool
}

// extract recovers a search query string from a raw tools/call arguments payload
// (PRD §10.1 precedence). It is PURE and DETERMINISTIC: no I/O, no globals, no
// side effects; it dispatches on the Go type produced by json.Unmarshal.
//
// Precedence (levels handled in this subtask):
//  1. arguments is a STRING  -> that is the query (source "bare-string").
//  2. arguments is a NUMBER/BOOLEAN -> stringify via fmt.Sprint (source "scalar").
//  3. arguments is an ARRAY  -> recurse on the FIRST element that yields a query
//     (source "array[0]"); if none yields, extraction fails.
//
// Failure (Found==false, zero value): nil/zero-length raw (no arguments field),
// invalid JSON, JSON null, or an empty array with no yielding element. An OBJECT
// input (level 4) is handled by P1.M2.T1.S2 (alias scan + inference); until then
// it returns Found==false.
//
// queryAliases and optionalAliases are read only by the object path (S2/S3) and
// are accepted here for signature stability; they are threaded to recursive
// extraction so array-of-object inputs work once S2 lands.
func extract(raw json.RawMessage, queryAliases []string, optionalAliases map[string][]string) ExtractionResult {
	if len(raw) == 0 { // missing "arguments" field arrives as a zero-length RawMessage
		return ExtractionResult{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil { // invalid JSON
		return ExtractionResult{}
	}
	return extractValue(v, queryAliases, optionalAliases)
}

// extractValue dispatches on the decoded value's Go type. It is the extension
// seam: P1.M2.T1.S2 replaces the `case map[string]any` body with the level-4
// alias scan + nested drill-in; P1.M2.T1.S3 adds inference, optionals, failure.
func extractValue(v any, queryAliases []string, optionalAliases map[string][]string) ExtractionResult {
	switch x := v.(type) {
	case nil:
		return ExtractionResult{}
	case string:
		return ExtractionResult{Query: x, Source: "bare-string", Found: true}
	case float64, bool:
		return ExtractionResult{Query: fmt.Sprint(v), Source: "scalar", Found: true}
	case []any:
		for _, elem := range x {
			if r := extractValue(elem, queryAliases, optionalAliases); r.Found {
				return ExtractionResult{
					Query:     r.Query,
					Source:    "array[0]", // PRD §10.1.3: array source is always "array[0]"
					Ambiguous: r.Ambiguous,
					Optionals: r.Optionals,
					Found:     true,
				}
			}
		}
		return ExtractionResult{} // no element yielded -> fail
	case map[string]any:
		// Level 4 (object alias scan + nested drill-in + inference) — P1.M2.T1.S2.
		return ExtractionResult{}
	}
	return ExtractionResult{} // unreachable for any valid JSON value
}

// ---- extract_test.go ----
// Table-driven; local aliases mirror DefaultConfig() to stay decoupled.

var extractQA = []string{
	"query", "search_query", "q", "search", "searchQuery",
	"search_term", "term", "text", "input", "prompt",
	"question", "keywords", "topic", "searchString",
}
var extractOpt = map[string][]string{
	"location":              {"country", "region"},
	"content_size":          {"size", "contentSize", "detail"},
	"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		raw  string // string form of json.RawMessage; "" means pass nil RawMessage
		want ExtractionResult
	}{
		// Level 1: bare string.
		{"bare_string", `"hello"`, ExtractionResult{Query: "hello", Source: "bare-string", Found: true}},
		// Level 2: scalar (number/bool) via fmt.Sprint.
		{"scalar_int", `42`, ExtractionResult{Query: "42", Source: "scalar", Found: true}},
		{"scalar_float", `3.14`, ExtractionResult{Query: "3.14", Source: "scalar", Found: true}},
		{"scalar_bool_true", `true`, ExtractionResult{Query: "true", Source: "scalar", Found: true}},
		{"scalar_bool_false", `false`, ExtractionResult{Query: "false", Source: "scalar", Found: true}},
		// Level 3: array -> first element that yields, source "array[0]".
		{"array_string", `["x"]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_first_yields", `[42,"x"]`, ExtractionResult{Query: "42", Source: "array[0]", Found: true}},
		{"array_skips_nonyielding_first", `[[],"x"]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_nested", `[["x"]]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_scalar_elem", `[true]`, ExtractionResult{Query: "true", Source: "array[0]", Found: true}},
		// Failure paths (Found=false -> zero value).
		{"null", `null`, ExtractionResult{}},
		{"empty_array", `[]`, ExtractionResult{}},
		{"empty_raw", ``, ExtractionResult{}},
		{"invalid_json", `{bad`, ExtractionResult{}},
		// S1<->S2 boundary: object level-4 is STUBBED (Found=false) until S2.
		{"empty_object", `{}`, ExtractionResult{}},
		{"array_of_objects", `[{"query":"x"}]`, ExtractionResult{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			got := extract(raw, extractQA, extractOpt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extract = %+v\nwant     %+v", got, tc.want)
			}
		})
	}
}

func TestExtract_PureAndDeterministic(t *testing.T) {
	// Same input twice -> identical result (pure/deterministic).
	raw := json.RawMessage(`["x"]`)
	a := extract(raw, extractQA, extractOpt)
	b := extract(raw, extractQA, extractOpt)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic: %+v vs %+v", a, b)
	}
	// Nil raw (arguments field omitted entirely).
	if got := extract(nil, extractQA, extractOpt); got.Found {
		t.Fatalf("nil raw -> Found=true, want false: %+v", got)
	}
}
```

### Integration Points

```yaml
NO FILES MODIFIED:
  - This subtask ONLY adds extract.go + extract_test.go. It touches NOTHING else.
  - config_test.go / resolve_test.go / health_test.go are owned by the in-parallel
    P1.M1.T3.S1 — DO NOT TOUCH. logger_test.go is green — DO NOT TOUCH. No
    production file (config.go/logger.go/health.go/main.go/doc.go) is modified.

CONSUMER SEAMS (future subtasks wire/extend here; signatures fixed by this PRP):
  - P1.M2.T1.S2 (object alias scan + nested drill-in): REPLACES the
        `case map[string]any:` body in extractValue; UPDATE the two boundary test
        rows (empty_object stays Found=false; array_of_objects -> Found=true with
        Query="x", Source="array[0]").
  - P1.M2.T1.S3 (inference + optionals + failure + clean payload): ADDS inference
        (Ambiguous, inferred:<key>), populates Optionals from optionalAliases,
        finalizes the 10.1.5 failure path and the §10.3 clean payload.
  - P1.M5.T2 (server dispatch): `res := extract(req.Params.Arguments,
        cfg.QueryAliases, cfg.OptionalAliases)`; on res.Found==false -> FR-6
        immediate warning, no upstream call; else forward search_query=res.Query.

CONFIG (read-only reference; extract.go does NOT import config):
  - Config.QueryAliases []string (default 14 entries) — passed as queryAliases.
  - Config.OptionalAliases map[string][]string (default 3 keys) — passed as opt.
  - extract takes plain params (no Config) to stay pure/unit-testable.

DATABASE / ROUTES / ENV: none. Pure function, no I/O, no globals.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# Run after creating extract.go — fix before writing the test.
gofmt -w extract.go extract_test.go     # format in place
go vet ./...                            # vet the whole module
go build ./...                          # must compile with zero new requires

# Confirm the determinism discipline (S1 must not iterate any map to pick a query;
# the only acceptable `range` is over the []any array or the test table):
grep -n 'range' extract.go               # MUST show ONLY the `range x` over []any
# (no `for k := range someMap` anywhere; map handling is the S2 stub, untouched)

# Confirm no config coupling (extract.go is pure):
grep -n 'Config\|cfg\.' extract.go        # MUST print nothing

# Confirm the S2 seam stub is present and commented:
grep -n 'map\[string\]any' extract.go     # MUST show the case + the S2 comment

# Expected: zero errors; the only `range` in extract.go is over the array; no
# Config refs; the object case is an explicit Found=false stub.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the extract tests in isolation, verbose.
go test -run TestExtract -v

# Full module suite (extract + all existing tests must stay green; the parallel
# M1 test rewrite must also be green at integration time).
go test ./...

# Expected: PASS. Every row MUST match this table exactly (reflect.DeepEqual on
# the whole ExtractionResult):

# | row                       | raw              | Query   | Source      | Found |
# |---------------------------|------------------|---------|-------------|-------|
# | bare_string               | `"hello"`        | hello   | bare-string | true  |
# | scalar_int                | `42`             | 42      | scalar      | true  |
# | scalar_float              | `3.14`           | 3.14    | scalar      | true  |
# | scalar_bool_true          | `true`           | true    | scalar      | true  |
# | scalar_bool_false         | `false`          | false   | scalar      | true  |
# | array_string              | `["x"]`          | x       | array[0]    | true  |
# | array_first_yields        | `[42,"x"]`       | 42      | array[0]    | true  |
# | array_skips_nonyielding…  | `[[],"x"]`       | x       | array[0]    | true  |
# | array_nested              | `[["x"]]`        | x       | array[0]    | true  |
# | array_scalar_elem         | `[true]`         | true    | array[0]    | true  |
# | null                      | `null`           | ""      | ""          | false |
# | empty_array               | `[]`             | ""      | ""          | false |
# | empty_raw                 | `` (nil)         | ""      | ""          | false |
# | invalid_json              | `{bad`           | ""      | ""          | false |
# | empty_object (S1 boundary)| `{}`             | ""      | ""          | false |
# | array_of_objects (S1 bdy) | `[{"query":"x"}]`| ""      | ""          | false |

# Notes:
#  - scalar Query values come from fmt.Sprint (42 -> "42", NOT "42.0"); verified.
#  - array Source is ALWAYS "array[0]" (PRD §10.1.3), never the inner source.
#  - "array_skips_nonyielding_first" proves the FIRST-YIELDS rule: [] yields
#    nothing, so the second element "x" is taken.
#  - The last two rows are the S1<->S2 boundary: objects yield Found=false until
#    S2 replaces the `case map[string]any` stub. S2 will flip array_of_objects to
#    Found=true (Query="x", Source="array[0]"); empty_object stays Found=false
#    (no query anywhere) via the S3 failure path.
```

### Level 3: Integration Testing (System Validation)

```bash
# extract() is a PURE function with NO runtime integration (no server, no config
# load, no upstream). The "integration" is the consumer contract, verified by a
# smoke test mirroring the real call shape (P1.M5.T2):

cat > /tmp/extract_smoke_test.go <<'EOF'
package main
import "testing"
func TestExtractSmoke_RealCallerShape(t *testing.T) {
	// Mirror the server dispatch call: extract(raw, cfg.QueryAliases, cfg.OptionalAliases).
	cfg := DefaultConfig()
	for _, c := range []struct{ raw, q, src string; found bool }{
		{`"rust async"`, "rust async", "bare-string", true},
		{`42`, "42", "scalar", true},
		{`["x"]`, "x", "array[0]", true},
		{`null`, "", "", false},
	} {
		got := extract([]byte(c.raw), cfg.QueryAliases, cfg.OptionalAliases)
		if got.Query != c.q || got.Source != c.src || got.Found != c.found {
			t.Errorf("%s: got %+v want q=%q src=%q found=%v", c.raw, got, c.q, c.src, c.found)
		}
	}
}
EOF
cp /tmp/extract_smoke_test.go ./zz_smoke_test.go && go test -run TestExtractSmoke -v && rm ./zz_smoke_test.go

# Expected: smoke compiles (signature matches the real caller: raw + []string +
# map[string][]string from DefaultConfig) and passes.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Determinism + idempotence stress: pure functions return identical results on
# repeated calls and never mutate input. Run the table many times (a hidden map
# iteration would make results flaky; there is none in S1, so this never flakes).
go test -run TestExtract -count=100

# Non-mutation check: extract must not mutate the input RawMessage (it is
# logically read-only). The table already reuses static raw literals across
# count=100 runs; if extract mutated them, later runs would diverge -> fail.

# Source-label contract grep: confirm ONLY the three S1 labels exist in extract.go
# (no stray "nested:"/"inferred:" which belong to S2/S3):
grep -cE '"bare-string"|"scalar"|"array\[0\]"' extract.go   # >= 3 (the three labels)
grep -nE '"nested:|"inferred:' extract.go                  # MUST print nothing (S2/S3)

# Expected: count=100 passes every run; exactly the three S1 source labels; no
# nested:/inferred: labels (those are S2/S3 scope).
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0 (compiles with stdlib only; no new requires).
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l .` prints nothing (extract.go + extract_test.go formatted).
- [ ] `go test ./...` exits 0 (extract + all pre-existing tests pass; M1 gate green).
- [ ] `go test -run TestExtract -v` shows all table rows + purity test PASS.

### Feature Validation

- [ ] All Level-2 table rows pass with the EXACT want (reflect.DeepEqual on the
      whole ExtractionResult).
- [ ] string→`bare-string`; float64/bool→`scalar` via `fmt.Sprint(v)` (verified:
      42→"42", 3.14→"3.14", true→"true"); array→first-yielding element, `array[0]`.
- [ ] Empty/nil raw returns Found=false WITHOUT calling json.Unmarshal (guard
      first); invalid JSON and `null` also return the zero result.
- [ ] Array returns the FIRST element that yields (index order); a non-yielding
      first element is skipped (`[[],"x"]`→"x").
- [ ] `grep 'range' extract.go` shows ONLY the array-element range (no map range).
- [ ] `go test -run TestExtract -count=100` never flakes (deterministic + pure).
- [ ] `ExtractionResult` fields and `extract`/`extractValue` signatures EXACTLY
      match the contract (S2/S3/the server depend on them).

### Code Quality Validation

- [ ] Mode-A doc comment on `extract` covers precedence levels 1-3, the exact
      source labels, failure cases, and "pure + deterministic".
- [ ] `extract.go` imports only `encoding/json` + `fmt`; `extract_test.go` imports
      only `encoding/json` + `reflect` + `testing`; `go.mod` gains zero requires.
- [ ] `extract_test.go` defines `queryAliases`/`optionalAliases` locally (decoupled
      from `DefaultConfig()`); follows config_test.go's table-driven `t.Run` style.
- [ ] The `case map[string]any:` is an explicit Found=false stub with an S2 comment
      (the documented extension seam). No anti-patterns (see below).
- [ ] No value mutation; `optionalAliases` threaded but documented as unread in S1.

### Documentation & Deployment

- [ ] Doc comment is self-contained (reading only extract.go explains levels 1-3,
      source labels, failure, purity, and the S2/S3 extension points).
- [ ] No new env vars / config keys / routes (pure function).
- [ ] No existing file modified (extract.go + extract_test.go are the only adds).

---

## Anti-Patterns to Avoid

- ❌ Don't call `json.Unmarshal` on an empty/zero-length `raw` — guard `len(raw)==0`
  first (Unmarshal on empty/whitespace bytes is an error). A missing `arguments`
  field arrives as a zero-length RawMessage (it has `omitempty`).
- ❌ Don't dispatch on `int`/`json.Number` — `json.Unmarshal` into `any` yields
  `float64` for ALL numbers (default, no UseNumber). Use `case float64:`.
- ❌ Don't hardcode scalar stringification — use `fmt.Sprint(v)` per the contract
  (so `42`→"42", `3.14`→"3.14", `true`→"true"). Verified outputs.
- ❌ Don't let the array case inherit the inner element's Source — PRD §10.1.3
  fixes it to `"array[0]"`. Override Source but PRESERVE Query/Ambiguous/Optionals.
- ❌ Don't return the first array element unconditionally — return the first that
  YIELDS (a non-yielding element like `[]`, or an object in S1, is skipped).
- ❌ Don't implement the object case (level 4) — that is S2. Leave an explicit
  `case map[string]any: return ExtractionResult{}` stub with a comment. Implementing
  it now is scope creep that collides with S2.
- ❌ Don't iterate any map to pick a query (`for k := range m`). S1 has no map
  iteration by construction; the rule is load-bearing in S2 (external_deps.md §6).
- ❌ Don't couple extract.go to `config.go` (no `Config`/`cfg` imports). Take plain
  params so it stays pure and unit-testable; the test defines aliases locally.
- ❌ Don't touch any existing file. This subtask adds extract.go + extract_test.go
  only. The M1 test files (config_test/resolve_test/health_test) are owned by the
  in-parallel P1.M1.T3.S1.
- ❌ Don't add fields to `ExtractionResult` or change the `extract` signature.
  S2, S3, and the server consume them as fixed contracts.
- ❌ Don't populate `Optionals`/set `Ambiguous` in S1 — those are object-path
  outputs belonging to S3. S1 levels 1-3 leave them zero.
