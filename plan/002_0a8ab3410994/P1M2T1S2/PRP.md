name: "P1.M2.T1.S2 — Object alias scan with nested drill-in (level 4a)"
description: |

  Extend the EXISTING `extract.go` (S1, on disk) with PRD §10.1.4.1 level 4a:
  replace the `case map[string]any:` Found=false STUB with the object alias scan
  + nested drill-in. Walk `queryAliases` in index order (deterministic); for the
  first alias key that YIELDS a string: shallow string → `Source=<key>`,
  number/bool → `Source="scalar"`, map/array value → DRILL IN (sub-key priority
  `[text,value,content,query,q,data,input]` then sorted recursive descent) →
  `Source="nested:<key>"`. Add `sort` to imports. Update the `extract` doc comment
  (Mode A). Extend `extract_test.go` (add object/nested rows; flip the
  `array_of_objects` S1-boundary row to Found=true). No signature/struct change;
  S3 (inference/optionals/failure) consumes everything unchanged.

---

## Goal

**Feature Goal**: Complete PRD §10.1.4.1 for the common case. After S1 handled
levels 1-3 (bare-string/scalar/array) and stubbed objects to Found=false, S2
makes the OBJECT case (the most common `tools/call` shape) extract a query from a
configured alias key, drilling into nested wrappers/objects/arrays when the alias
value isn't already a usable scalar. The result is deterministic (alias slice
walk + sorted descent), pure, and correctly labeled for the teaching signal (M3)
and logging (§15).

**Deliverable**: TWO MODIFIED files (no new files):
1. **MODIFY** `extract.go` — replace the `case map[string]any:` STUB body with
   the alias-scan + drill-in; add two unexported helpers `drillIn(any) (string,
   bool)` and `firstReachableString(any) (string, bool)`; add `"sort"` to the
   import block; rewrite the `extract` doc comment (Mode A) to document levels
   1-4a, the source labels, the drill-in sub-key list, and determinism.
2. **MODIFY** `extract_test.go` — ADD object/nested rows to `TestExtract` (every
   §10.1.4.1 / §19.1 shape); FLIP the `array_of_objects` row from Found=false to
   `{Query:"x", Source:"array[0]", Found:true}`; keep `empty_object` Found=false.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run TestExtract -v`) all exit clean. Every object
row yields the exact `Query`/`Source`/`Found` in the validated table (research §8).
`go test -run TestExtract -count=100` never flakes (determinism). `ExtractionResult`
fields and the `extract`/`extractValue` signatures are byte-for-byte unchanged from
S1 (S3 consumes them). `Optionals` stays nil and `Ambiguous` stays false in every
S2 row (those are S3). `go.mod` gains zero `require`s (only stdlib `sort` added).

## User Persona

**Target User**: (1) S3 (P1.M2.T1.S3), which extends `extractValue` further with
inference (no-alias objects), optionals (§10.2), and the failure path (§10.1.5) —
it consumes S2's object case unchanged and only adds the "no alias yielded"
branch. (2) M3 / P1.M3.T1.S1 (teach.go), which reads `Source` to detect canonical
usage and phrase the warning — it depends on S2's exact labels. (3) the agent,
whose `{"query":"x"}`, `{"q":"x"}`, `{"input":{"query":"x"}}}`, or
`{"query":{"text":"x"}}` now all resolve to a query instead of failing. (4) the
maintainer, who gets a deterministic, table-tested object extractor.

**Use Case**: Agent sends `tools/call` with `{"input":{"query":"rust async"}}`
(a nested wrapper). The server calls `extract(raw, cfg.QueryAliases, …)`; S2 finds
alias "input", its value is an object, drills into sub-key "query" → "rust async",
returns `{Query:"rust async", Source:"nested:input", Found:true}`. The server
forwards `search_query="rust async"`; M3 appends a warning citing `nested:input`.

**User Journey**: object args → unmarshal to `map[string]any` → `extractValue`
object case → walk `queryAliases` by index → first present alias key → resolve
(string→`<key>` / number,bool→`scalar` / map,array→drill→`nested:<key>`) →
`ExtractionResult` with Found=true; or no alias yields → Found=false (S3 handles).

**Pain Points Addressed**: (1) Without S2, EVERY object call (the common case)
returns Found=false → no upstream call (FR-6) → the agent gets nothing. S2 is the
slice that makes the server actually work for real `tools/call`s. (2) Nested
wrappers (`{"input":{...}}`) and value-is-object (`{"query":{"text":...}}`) are
common agent mistakes; the drill-in recovers them. (3) Determinism: a naive
"range the args map" or "range the nested map" would make the chosen query
flaky run-to-run — S2 walks slices and sorts descent keys.

## Why

- Implements **PRD §10.1.4.1** (alias scan: shallow then nested drill-in),
  **§5 FR-4** ("extract a query from ANY input … accept any alias key at any
  depth … a nested wrapper"), **§10.4** (source is recorded for logging/surfacing),
  and the object half of **§19.1** (extract_test.go over every structure).
- Honors the determinism mandate from **architecture/external_deps.md §6** (map
  iteration randomized → walk the ordered `queryAliases` SLICE; sort keys in the
  recursive-descent fallback). This is the load-bearing determinism subtask.
- Establishes the object-extraction contract S3 extends and M3/§15 consume: the
  exact source labels (`<key>` / `scalar` / `nested:<key>`), the drill-in sub-key
  priority list, and the "no alias yields → Found=false" handoff to inference.
- Pure function extension; no I/O/config/globals. Keeps the M1 test gate green.

## What

### Source-label contract (resolved from PRD §15 + §12.2; see research §1)

| alias-key value shape | Source |
|---|---|
| string | the matched alias key verbatim (e.g. `query`, `q`, `search`) |
| float64 / bool | `scalar` |
| map / array (drill-in yields) | `nested:<matched-alias-key>` (`<key>` = top-level alias key, NOT the inner sub-key) |

### Object case algorithm (replaces the S1 stub)

```text
case map[string]any (x):
  for each alias `a` in queryAliases (INDEX order):          # deterministic
    val, present := x[a]; if !present: continue
    switch val.(type):
      string:        return {Query: val, Source: a, Found: true}
      float64, bool: return {Query: fmt.Sprint(val), Source: "scalar", Found: true}
      map[string]any, []any:
        if s, ok := drillIn(val); ok:
          return {Query: s, Source: "nested:" + a, Found: true}
        # drill found nothing -> CONTINUE to next alias key (not a hard fail)
      default:        # nil (alias present but JSON null) -> skip to next alias
  return {}           # no alias yielded -> S3 inference (Found=false here)

drillIn(val) -> (string, bool):                              # val is map or []any
  STEP 1 (map only): walk [text,value,content,query,q,data,input]; return the
       FIRST present sub-key whose value is a STRING. (Non-string values skipped.)
  STEP 2 (fallback): firstReachableString(val) — DFS; []any by index, maps by
       SORTED keys; return first string reached.

firstReachableString(v) -> (string, bool):
  string -> (v, true)
  map[string]any -> collect keys, sort.Strings, recurse in order; first hit
  []any -> recurse by index; first hit
  else -> ("", false)
```

### Mode-A doc comment update on `extract`

Rewrite the existing `extract` doc comment to cover levels 1-4a: precedence
(string→`bare-string`; number/bool→`scalar`; array→`array[0]`; object alias scan
→ `<key>`/`scalar`/`nested:<key>`), the drill-in sub-key priority list
`[text,value,content,query,q,data/input]`, the recursive-descent fallback, the
determinism rationale (alias slice walk + sorted descent; never range a map to
pick a query), the S3 handoff (no-alias objects / optionals / inference are S3),
and "pure + deterministic".

### Success Criteria

- [ ] `case map[string]any:` body is the alias scan above (no longer the S1 stub);
      it walks `queryAliases` BY INDEX and never ranges `x` to pick a query.
- [ ] Shallow string → `Source=<key>`; number/bool → `Source="scalar"`; nested →
      `Source="nested:<key>"` (`<key>` = the top-level alias key, verified by
      `nested:input`/`nested:query` examples).
- [ ] `drillIn` checks the sub-key priority list `[text,value,content,query,q,
      data,input]` (string values only) then falls back to sorted recursive descent.
- [ ] `firstReachableString` SORTS map keys before recursing (deterministic).
- [ ] A non-yielding alias (null / empty nested / array-of-non-strings) is SKIPPED
      and the scan continues to the next alias key; `{"query":{},"q":"x"}` → "q".
- [ ] No-alias objects (`{}`, `{"foo":"bar"}`) return Found=false (S3 inference).
- [ ] `ExtractionResult` fields + `extract`/`extractValue` signatures UNCHANGED
      from S1; `Optionals` nil + `Ambiguous` false in every S2 row (S3 owns them).
- [ ] extract.go adds ONLY the `sort` import; `go.mod` gains zero requires.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` clean;
      `go test -run TestExtract -count=100` never flakes.

## All Needed Context

### Context Completeness Check

_Pass._ The function under test (extract.go) and the file being extended
(extract_test.go) are on disk and read. The level-4a algorithm is fixed by the
item + PRD §10.1.4.1; the ONE ambiguity (shallow-string source label) is resolved
from PRD §15 line 496 + §12.2 line 393 (research §1). The determinism mechanism
(slice walk + sorted descent) is pinned to external_deps.md §6. The exact expected
Query/Source/Found for every case is **empirically validated** by a throwaway
prototype that ran 25/25 green against the real on-disk S1 extract (research §8).
The import addition (`sort`) and the S1↔S2 boundary row flip (`array_of_objects`)
are identified. An agent with no prior knowledge can implement this from the PRP +
the two on-disk files alone.

### Documentation & References

```yaml
# MUST READ — the function under test and the file being extended.
- file: extract.go
  why: the EXACT file S2 modifies. The `case map[string]any:` STUB (line ~76) is
        what S2 replaces; the `extractValue` signature + `case []any` (which now
        recurses INTO the object case) are the integration points. Imports today:
        encoding/json + fmt (S2 adds `sort`).
  pattern: S1's type-switch shape, the array case's "override Source=array[0],
        preserve Query/Ambiguous/Optionals" rule, and the pure/deterministic style.
  gotcha: the array case recurses via extractValue(elem) — once S2 lands, an array
        of objects ([{"query":"x"}]) yields through the object case; the array case
        overrides Source to "array[0]". This flips the S1 boundary test row.

- file: extract_test.go
  why: the test file S2 extends. Reuse extractQA/extractOpt, the table-driven
        TestExtract shape, reflect.DeepEqual. UPDATE the array_of_objects row;
        ADD the object/nested rows; KEEP empty_object as Found=false.
  pattern: existing rows assert the whole ExtractionResult via DeepEqual; S2's new
        rows do the same (Optionals nil, Ambiguous false).

- file: PRD.md
  section: "§10.1.4.1" (alias scan: shallow then nested; sub-key list
        text/value/content/query/q/data/input; first reachable string; source
        nested:<key>); "§10.4" (source recorded/logged/surfaced); "§5 FR-4"
        (accept any alias key at any depth; a nested wrapper); "§19.1" (table over
        every structure incl. nested wrapper {"input":{"query":"x"}} and
        value-is-object {"query":{"text":"x"}}).
  critical: §10.1.4.1 fixes the drill-in sub-key list AND the nested:<key> label.
        §15 line 496 + §12.2 line 393 fix the shallow-string label to the bare key.

- file: config.go
  why: DefaultConfig().QueryAliases is the 14-entry ordered slice the real caller
        passes; the test's extractQA mirrors it. Confirms QueryAliases is a SLICE
        (must walk by index, never as a map) per the determinism mandate.
  pattern: extract takes []string (not Config) — stays pure/unit-testable.

- docfile: plan/002_0a8ab3410994/P1M2T1S2/research/object-scan-design.md
  why: the source-label resolution (§1, with PRD line citations), the drill-in
        algorithm (§2), the continuation semantics + the {"query":{},"q":"x"} bug
        it prevents (§3), the S2↔S3 boundary (§4), the determinism-sort nuance
        (§7), and the VALIDATED 25-case table (§8 — copy the want* values verbatim).

# DETERMINISM MANDATE (load-bearing in S2).
- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§6. Go map ordering is non-deterministic"
  why: PROVES the alias scan must walk the QueryAliases SLICE by index and the
        recursive-descent fallback must SORT map keys. A plain `for k := range m`
        anywhere that picks a query or decides query order is a nondeterminism bug.
  critical: the ONLY acceptable `for k := range <map>` in extract.go is inside
        firstReachableString, immediately followed by sort.Strings. See research §7.

# CONTRACTS (read-only — do not break):
- file: plan/002_0a8ab3410994/P1M2T1S1/PRP.md
  why: defines ExtractionResult/extract/extractValue EXACTLY as shipped on disk.
        S2 replaces only the map-case body; S3 extends the no-alias branch.
- file: plan/002_0a8ab3410994/P1M1T3S1/PRP.md
  why: M1 test rewrite owns config_test.go/resolve_test.go/health_test.go — DO NOT
        TOUCH. extract.go shares no symbols with them; both keep `go test ./...` green.

# Go stdlib refs (stable).
- url: https://pkg.go.dev/sort#Strings
  why: sort map keys lexicographically in firstReachableString for determinism.
- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: confirms object → map[string]any, array → []any, numbers → float64 (S1
        research already verified; S2 relies on these decoded types).
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap — UNTOUCHED by S2
  config.go       # Config{QueryAliases,OptionalAliases,...} — UNTOUCHED
  logger.go, health.go — UNTOUCHED
  config_test.go, resolve_test.go, health_test.go — owned by P1.M1.T3.S1 — DO NOT TOUCH
  logger_test.go  # green — UNTOUCHED
  extract.go      # S1 — ExtractionResult + extract/extractValue (levels 1-3; object STUB). <- S2 MODIFIES
  extract_test.go # S1 — TestExtract (levels 1-3 + boundary rows). <- S2 MODIFIES
  testdata/, README.md, config.example.json, PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
extract.go       # MODIFIED (not new). +drillIn +firstReachableString helpers;
                  #      map-case body = alias scan + drill-in; +`sort` import;
                  #      extract doc comment rewritten (Mode A, levels 1-4a).
extract_test.go  # MODIFIED (not new). +object/nested rows in TestExtract;
                  #      array_of_objects row FLIPPED to Found=true; empty_object
                  #      kept Found=false. No new imports.
# NO other files created or modified.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL (determinism): NEVER range a map to pick a query or to decide the
// order in which alias keys / nested keys are tried. The alias scan MUST walk the
// queryAliases SLICE by index. The recursive-descent fallback MUST collect map
// keys and sort.Strings them BEFORE recursing. The ONLY `for k := range <map>` in
// extract.go lives inside firstReachableString, immediately followed by sort.Strings.
// (external_deps.md §6.) A plain descent that ranges a nested map is a flake bug.

// CRITICAL (source labels — resolved ambiguity): the shallow-string alias case
// logs the matched key VERBATIM (e.g. "query", "q"), NOT "alias:query" and NOT
// "nested:query". Evidence: PRD §15 line 496 lists `query` as a source peer of
// `bare-string`; §12.2 substitutes <source> into `used "<tool>"/"<source>"`.
// number/bool -> "scalar" (item explicit); map/array -> "nested:<key>" (item
// explicit; <key> = the TOP-LEVEL alias key, e.g. "nested:input", NOT the sub-key).

// CRITICAL (nested <key> is the ALIAS key): for {"input":{"query":"x"}}, the
// matched alias is "input" -> Source="nested:input" (NOT "nested:query"). For
// {"query":{"text":"x"}}, Source="nested:query". The sub-key found inside is NOT
// part of the label. (PRD §10.1.4.1 examples.)

// CRITICAL (drill-in is STRINGS-ONLY): STEP 1 returns a sub-key only if its value
// is a STRING; {"query":{"text":42}} skips "text" (number) and STEP 2 finds no
// string -> drill fails -> alias skipped -> (if no other alias) Found=false.
// Coercion does NOT happen inside drill-in; only the top-level shallow path coerces
// number/bool. This asymmetry ({"query":42} -> "42"/scalar but {"query":{"text":42}}
// -> fail) is per the item's "first reachable STRING" wording — documented, intended.

// CRITICAL (continuation): a present alias key whose value is a non-yielding nested
// structure (null / {} / [1,2,3] / {"text":42}) does NOT abort the scan — CONTINUE
// to the next alias key. Without this, {"query":{},"q":"x"} wrongly fails (S3
// inference excludes alias keys, so "q" would be skipped). Verified green.

// GOTCHA (S2↔S3 boundary): S2 returns Found=false for ANY object where no alias
// yields — INCLUDING no-alias objects that contain a string ({"foo":"bar"}).
// Inference (which would pick "bar" -> inferred:foo) is S3. Do NOT implement
// inference, optionals (§10.2), or the §10.3 clean payload here.

// GOTCHA (import): add ONLY "sort" to extract.go's import block. Do NOT add
// encoding/json usage changes, reflect, slices, or any third-party import.

// GOTCHA (array recursion flip): the S1 []any case recurses via extractValue(elem).
// After S2, [{"query":"x"}] yields "x" from the object case; the array case
// overrides Source to "array[0]" (unchanged S1 rule). UPDATE the array_of_objects
// test row accordingly; it is no longer Found=false.

// GOTCHA (combined case + val): `case map[string]any, []any:` makes the switch
// variable the original `any` type — pass `val` (not a typed `v`) to drillIn.
// `case float64, bool:` similarly; use fmt.Sprint(val) for the scalar string.
```

## Implementation Blueprint

### Data models and structure

No data models change. `ExtractionResult` (S1) is frozen. S2 adds two unexported
helpers (`drillIn`, `firstReachableString`) operating on `any` / decoded types.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: MODIFY extract.go — add helpers (drillIn + firstReachableString)
  - DEFINE: `func drillIn(val any) (string, bool)` — STEP 1: if val is
        map[string]any, walk [text,value,content,query,q,data,input], return first
        present sub-key whose value is a STRING. STEP 2: return firstReachableString(val).
  - DEFINE: `func firstReachableString(v any) (string, bool)` — string->(v,true);
        map[string]any->collect keys, sort.Strings, recurse in order, first hit;
        []any->recurse by index, first hit; else ("",false).
  - ADD import: "sort" (keep encoding/json + fmt).
  - DOC COMMENT on each helper (brief): drillIn = "extract a string from a nested
        alias value (sub-key priority then sorted descent)"; firstReachableString =
        "first reachable string via DFS; maps sorted for determinism".
  - NAMING: drillIn / firstReachableString (unexported). PLACEMENT: after extractValue.

Task 2: MODIFY extract.go — replace the map-case STUB with the alias scan
  - REPLACE the `case map[string]any:` body (currently `return ExtractionResult{}`
        + the S2 comment) with the alias scan (see "Object case algorithm"):
        `for _, a := range queryAliases { ... }` returning on the first alias that
        yields (string->Source=a; float64,bool->Source="scalar"; map,array->
        drillIn->Source="nested:"+a; nil/default->skip); trailing `return ExtractionResult{}`.
  - CONSTRAINT: walk queryAliases BY INDEX (it IS a slice; `for _, a := range`
        over a slice is index-ordered and deterministic). NEVER `for k := range x`.
  - PRESERVE: every other case (nil/string/float64,bool/[]any) and the function
        signatures. The `case []any` recursion now flows through the object case.

Task 3: MODIFY extract.go — rewrite the extract doc comment (Mode A)
  - COVER: precedence levels 1-4a (string->bare-string; number/bool->scalar;
        array->array[0]; object alias scan-><key>/scalar/nested:<key>); the drill-in
        sub-key priority list [text,value,content,query,q,data,input] + sorted
        recursive-descent fallback; the source-label rules; DETERMINISM (alias slice
        walk + sorted descent; never range a map to pick a query); the S3 handoff
        (no-alias objects / optionals / inference / §10.3 payload are S3); pure+det.
  - NOTE: remove the S1 "until then it returns Found==false" line (no longer true).

Task 4: MODIFY extract_test.go — extend TestExtract
  - ADD object/nested rows (copy want* from research §8 verbatim). Minimum set:
      shallow_canonical {"query":"x"} -> {x, "query", true}
      shallow_noncanonical_q {"q":"x"} -> {x, "q", true}
      shallow_search {"search":"x"} -> {x, "search", true}
      shallow_number {"query":42} -> {"42", "scalar", true}
      shallow_bool {"query":true} -> {"true", "scalar", true}
      nested_wrapper {"input":{"query":"x"}} -> {x, "nested:input", true}
      nested_value_is_object {"query":{"text":"x"}} -> {x, "nested:query", true}
      nested_subkey_priority {"query":{"text":"first","value":"second"}} -> {first, "nested:query", true}
      nested_array_value {"query":["x","y"]} -> {x, "nested:query", true}
      nested_array_of_objects {"query":[{"text":"x"}]} -> {x, "nested:query", true}
      nested_descent_nonpriority {"query":{"foobar":"x"}} -> {x, "nested:query", true}
      nested_descent_sorted {"query":{"zebra":"z","apple":"a"}} -> {a, "nested:query", true}
      nested_descent_deep {"query":{"a":{"b":"deep"}}} -> {deep, "nested:query", true}
      config_order {"query":"a","q":"b"} -> {a, "query", true}
      map_order_irrelevant {"q":"b","query":"a"} -> {a, "query", true}
      drill_continuation {"query":{},"q":"x"} -> {x, "q", true}
      alias_null {"query":null} -> {} (Found=false)
      nested_empty {"query":{}} -> {} (Found=false)
      nested_array_no_string {"query":[1,2,3]} -> {} (Found=false)
      nested_subkey_nonstring {"query":{"text":42}} -> {} (Found=false)
      no_alias_string_s3boundary {"foo":"bar"} -> {} (Found=false; S3 inference)
  - UPDATE the S1 boundary row:
      array_of_objects [{"query":"x"}] -> {Query:"x", Source:"array[0]", Found:true}
      (was Found=false; now the object case yields and the array case overrides Source).
  - KEEP empty_object {} -> {} (Found=false; S3).
  - ASSERT each via reflect.DeepEqual on the whole ExtractionResult (catches stray
        Optionals/Ambiguous). Optionals nil + Ambiguous false on every row.
  - NAMING: subtest names match the row semantics. PLACEMENT: append to the tests slice.

Task 5: VALIDATE
  - RUN: gofmt -w extract.go extract_test.go; go vet ./...; go test -run TestExtract -v;
        go test -run TestExtract -count=100; go test ./...; go build ./...
  - CONFIRM determinism grep: the only `for k := range <map>` is in firstReachableString
        and is immediately followed by sort.Strings; the alias scan ranges a slice.
  - CONFIRM no Optionals/Ambiguous set on any S2 row; go.mod unchanged.
```

### Implementation Patterns & Key Details

```go
// drillIn — extract a string from a nested alias value (map or []any).
// STEP 1: immediate sub-key priority (maps only), STRING values only.
// STEP 2: sorted recursive descent (first reachable string).
func drillIn(val any) (string, bool) {
	if m, ok := val.(map[string]any); ok {
		for _, sk := range []string{"text", "value", "content", "query", "q", "data", "input"} {
			if sv, present := m[sk]; present {
				if str, isStr := sv.(string); isStr {
					return str, true
				}
			}
		}
	}
	return firstReachableString(val)
}

// firstReachableString returns the first reachable STRING via DFS. Map keys are
// SORTED before recursion so the result is deterministic (Go map iteration is
// randomized; external_deps.md §6). Never used to pick a query by ranging a map.
func firstReachableString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x { // collect only — sorted immediately below
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := firstReachableString(x[k]); ok {
				return s, true
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := firstReachableString(e); ok {
				return s, true
			}
		}
	}
	return "", false
}

// --- the map case in extractValue (replaces the S1 stub) ---
	case map[string]any:
		// Level 4a (P1.M2.T1.S2): alias scan, shallow then nested drill-in.
		// Walk queryAliases BY INDEX (deterministic); first alias that YIELDS wins.
		for _, a := range queryAliases {
			val, ok := x[a]
			if !ok {
				continue
			}
			switch val.(type) {
			case string:
				return ExtractionResult{Query: val.(string), Source: a, Found: true}
			case float64, bool:
				return ExtractionResult{Query: fmt.Sprint(val), Source: "scalar", Found: true}
			case map[string]any, []any:
				if s, found := drillIn(val); found {
					return ExtractionResult{Query: s, Source: "nested:" + a, Found: true}
				}
				// drill found nothing -> try the next alias key
			default:
				// nil (alias present but JSON null) -> skip to the next alias key
			}
		}
		return ExtractionResult{} // no alias yielded -> S3 inference (Found=false here)
```

### Integration Points

```yaml
FILES MODIFIED:
  - extract.go: replace the map-case body; add drillIn + firstReachableString; add
    the `sort` import; rewrite the extract doc comment (Mode A).
  - extract_test.go: add object/nested rows; flip array_of_objects to Found=true;
    keep empty_object Found=false.
NO OTHER FILES TOUCHED:
  - config*.go, logger.go, health.go, main.go, doc.go, proxy: untouched.
  - config_test.go/resolve_test.go/health_test.go: owned by P1.M1.T3.S1 (parallel) — DO NOT TOUCH.
  - logger_test.go: green, untouched.
CONSUMER SEAMS (frozen; S2 changes no signature/field):
  - P1.M2.T1.S3 (inference/optionals/failure): extends the `return ExtractionResult{}`
        trailing branch of the map case (no-alias / no-yield objects) with inference
        (§10.1.4.2), populates Optionals (§10.2), finalizes §10.1.5 + §10.3. Consumes
        S2's object case unchanged.
  - P1.M3.T1.S1 (teach.go canonical detection): reads Source — S2's `<key>`/`scalar`/
        `nested:<key>` labels are the contract. Canonical iff Source == CanonicalParam
        ("query") for the shallow canonical case.
  - P1.M5.T2 (server dispatch): unchanged call `extract(req.Params.Arguments,
        cfg.QueryAliases, cfg.OptionalAliases)`.
DATABASE / ROUTES / ENV: none. Pure function, no I/O, no globals.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# After editing extract.go + extract_test.go — fix before running tests.
gofmt -w extract.go extract_test.go
go vet ./...
go build ./...                       # must compile; only `sort` added (stdlib)

# Determinism grep (UPDATED for S2):
#   - the alias scan ranges a SLICE (queryAliases), NOT the args map x.
#   - the ONLY `for ... range <map>` is inside firstReachableString, immediately
#     followed by sort.Strings.
grep -n 'range x\b' extract.go                       # MUST print nothing (never range the args map)
grep -n 'range queryAliases' extract.go              # MUST show the alias-scan loop
grep -nA1 'for k := range' extract.go                # MUST show key-collect + sort.Strings

# Confirm only `sort` was added (no new third-party/stdlib imports beyond sort):
git diff --stat extract.go
grep -n 'sort' extract.go                            # the import + sort.Strings call

# Confirm no config coupling:
grep -n 'Config\|cfg\.' extract.go                   # MUST print nothing

# Expected: zero errors; alias scan ranges a slice; the sole map-range is
# collect+sort; only `sort` added; no Config refs.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the extract tests in isolation, verbose.
go test -run TestExtract -v

# Full module suite (extract + all existing tests stay green).
go test ./...

# Expected: PASS. Every S2 object row MUST match the validated table (research §8).
# Spot-check the load-bearing rows:
#   {"query":"x"}                      -> {x, "query",      true}
#   {"q":"x"}                          -> {x, "q",          true}
#   {"query":42}                       -> {"42","scalar",   true}
#   {"input":{"query":"x"}}            -> {x, "nested:input", true}
#   {"query":{"text":"x"}}             -> {x, "nested:query",true}
#   {"query":{"text":"f","value":"s"}} -> {f, "nested:query",true}   # sub-key priority
#   {"query":{"zebra":"z","apple":"a"}}-> {a, "nested:query",true}   # sorted descent
#   {"query":"a","q":"b"}              -> {a, "query",      true}    # config order
#   {"query":{},"q":"x"}               -> {x, "q",          true}    # drill continuation
#   [{"query":"x"}]                    -> {x, "array[0]",   true}    # boundary flip
#   {}                                 -> {"" ,"" ,          false}  # S3 handoff
#   {"foo":"bar"}                      -> {"" ,"" ,          false}  # no-alias -> S3
#   {"query":null} / {"query":{}} / {"query":[1,2,3]} / {"query":{"text":42}}
#                                      -> {"" ,"" ,          false}  # non-yielding
# Notes:
#  - Optionals is nil and Ambiguous is false on EVERY S2 row (S3 owns them).
#  - Source for shallow string is the bare matched key (PRD §15 line 496), not "alias:…".
```

### Level 3: Integration Testing (System Validation)

```bash
# extract() is a PURE function — the "integration" is the consumer contract +
# the array-recursion-through-object path, verified by a smoke test mirroring the
# real call (P1.M5.T2):

cat > /tmp/extract_smoke_test.go <<'EOF'
package main
import ("encoding/json"; "testing")
func TestExtractSmoke_S2ObjectPaths(t *testing.T) {
	cfg := DefaultConfig()
	for _, c := range []struct{ raw, q, src string; found bool }{
		{`{"query":"rust async"}`, "rust async", "query", true},
		{`{"q":"rust"}`, "rust", "q", true},
		{`{"input":{"query":"nested"}}`, "nested", "nested:input", true},
		{`{"query":{"text":"obj"}}`, "obj", "nested:query", true},
		{`[{"query":"arr"}]`, "arr", "array[0]", true},
		{`{"foo":"bar"}`, "", "", false}, // no alias -> S3 (Found=false in S2)
	} {
		got := extract(json.RawMessage(c.raw), cfg.QueryAliases, cfg.OptionalAliases)
		if got.Query != c.q || got.Source != c.src || got.Found != c.found {
			t.Errorf("%s: got %+v want q=%q src=%q found=%v", c.raw, got, c.q, c.src, c.found)
		}
		if got.Found && (got.Optionals != nil || got.Ambiguous) {
			t.Errorf("%s: S2 must not set Optionals/Ambiguous: %+v", c.raw, got)
		}
	}
}
EOF
cp /tmp/extract_smoke_test.go ./zz_smoke_test.go && go test -run TestExtractSmoke -v && rm ./zz_smoke_test.go

# Expected: smoke compiles (real DefaultConfig call shape) and passes; S2 rows
# never set Optionals/Ambiguous.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Determinism stress: the two load-bearing determinism guarantees (alias-slice
# walk; sorted descent) must NEVER flake. Run the table many times.
go test -run TestExtract -count=100

# Targeted descent determinism: a nested map with many keys must always pick the
# lexicographically-first reachable string, never a random one.
cat > /tmp/det_stress.go <<'EOF'
package main
import ("encoding/json"; "testing")
func TestS2DescentDeterminism(t *testing.T) {
	for i := 0; i < 1000; i++ {
		got := extract(json.RawMessage(`{"query":{"zebra":"z","apple":"a","mango":"m","banana":"b"}}`),
			DefaultConfig().QueryAliases, DefaultConfig().OptionalAliases)
		if got.Query != "a" || got.Source != "nested:query" {
			t.Fatalf("run %d: nondeterministic descent: %+v", i, got)
		}
	}
}
EOF
cp /tmp/det_stress.go ./zz_det_test.go && go test -run TestS2DescentDeterminism -v && rm ./zz_det_test.go

# Config-order determinism: which alias wins must depend ONLY on queryAliases order,
# never on map iteration. {"q":"b","query":"a"} always yields "a" (query is earlier).
go test -run 'TestExtract/config_order|TestExtract/map_order_irrelevant' -count=100

# Expected: count=100 table passes; 1000-run descent always picks "a"; config-order
# rows never flake.
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0 (only stdlib `sort` added; no new requires).
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l .` prints nothing (extract.go + extract_test.go formatted).
- [ ] `go test ./...` exits 0 (extract + all pre-existing tests pass; M1 gate green).
- [ ] `go test -run TestExtract -v` shows all S1 rows + all new object/nested rows PASS.

### Feature Validation

- [ ] `case map[string]any:` walks `queryAliases` BY INDEX; `grep 'range x\b' extract.go`
      prints nothing (never ranges the args map to pick a query).
- [ ] Shallow string → `Source=<matched key>`; number/bool → `scalar`; nested →
      `nested:<matched alias key>` (e.g. `nested:input`, `nested:query`).
- [ ] `drillIn` uses sub-key priority `[text,value,content,query,q/data,input]`
      (string values only) then sorted recursive descent.
- [ ] A non-yielding alias (null / `{}` / `[1,2,3]` / `{"text":42}`) is skipped and
      the scan continues; `{"query":{},"q":"x"}` → "q".
- [ ] No-alias objects (`{}`, `{"foo":"bar"}`) return Found=false (S3 handoff).
- [ ] The `array_of_objects` row is FLIPPED to `{x, "array[0]", true}`; `empty_object`
      stays Found=false.
- [ ] `go test -run TestExtract -count=100` never flakes; the 1000-run descent stress
      always picks the sorted-first string.

### Code Quality Validation

- [ ] `ExtractionResult` fields + `extract`/`extractValue` signatures UNCHANGED from
      S1; `Optionals` nil + `Ambiguous` false on every S2 row (S3 owns them).
- [ ] extract.go adds ONLY the `sort` import; no third-party/reflect/slices imports.
- [ ] Mode-A doc comment on `extract` covers levels 1-4a, source labels, the drill-in
      sub-key list, determinism rationale, and the S3 handoff.
- [ ] The sole `for k := range <map>` is inside firstReachableString and is
      immediately followed by `sort.Strings` (no anti-pattern; see below).

### Documentation & Deployment

- [ ] Doc comment is self-contained (reading only extract.go explains the object
      alias scan, drill-in, source labels, determinism, and the S3 boundary).
- [ ] No new env vars / config keys / routes (pure function).
- [ ] No file other than extract.go + extract_test.go is modified.

---

## Anti-Patterns to Avoid

- ❌ Don't range the args map (`for k := range x`) to find an alias, and don't range
  a nested map to find a string in iteration order. Walk `queryAliases` by index;
  sort keys in the descent fallback. (external_deps.md §6 — a flake bug otherwise.)
- ❌ Don't label the shallow-string case `alias:query` or `nested:query`. It is the
  BARE matched key (`query`, `q`, …) per PRD §15 line 496 / §12.2 line 393.
  (research §1.)
- ❌ Don't put the inner sub-key in the nested label. `{"input":{"query":"x"}}` →
  `nested:input`, NOT `nested:query`. The label is the TOP-LEVEL alias key.
- ❌ Don't coerce scalars inside drill-in. `{"query":{"text":42}}` yields nothing
  (drill-in extracts STRINGS only); `{"query":42}` coerces to "42" because it's the
  TOP-LEVEL shallow path. This asymmetry is intended (item wording: "first reachable
  STRING").
- ❌ Don't abort the alias scan when the first present alias key is a non-yielding
  nested structure. CONTINUE to the next alias key (else `{"query":{},"q":"x"}`
  wrongly fails because S3 inference excludes alias keys).
- ❌ Don't implement inference, optionals (§10.2), or the §10.3 clean payload — that
  is S3. S2 returns Found=false for no-alias / no-yield objects and leaves
  Optionals nil / Ambiguous false.
- ❌ Don't change `ExtractionResult` fields or the `extract`/`extractValue` signature.
  S3, M3, and the server consume them as frozen contracts.
- ❌ Don't add imports beyond `sort`. No reflect/slices/third-party.
- ❌ Don't touch any file other than extract.go + extract_test.go. The M1 test files
  (config_test/resolve_test/health_test) are owned by the in-parallel P1.M1.T3.S1.
- ❌ Don't forget to flip the `array_of_objects` test row (S1 left it Found=false as
  the boundary; S2 makes the object case yield, so it is now Found=true, "array[0]").
