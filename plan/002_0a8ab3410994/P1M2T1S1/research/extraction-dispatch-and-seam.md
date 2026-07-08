# Research — P1.M2.T1.S1 Core extraction types + scalar/string/array precedence

Scope: pin the exact type-dispatch behavior, source labels, recursion design,
and the S1↔S2 seam so the PRP is unambiguous and S2 extends (not rewrites) S1.
All Go-side claims VERIFIED in a throwaway go1.25 module.

## 1. json.Unmarshal into `any` — the dispatch table (VERIFIED)

`mcp_sdk_api.md §1` (verified from SDK v1.6.1 source): the tool handler receives
`req.Params.Arguments` as `json.RawMessage` (raw JSON bytes). extract() must
`json.Unmarshal(raw, &v)` into `any`, then type-switch on `v`.

VERIFIED dispatch (encoding/json default — NO Decoder.UseNumber, so all numbers
are float64):

| JSON input     | decoded Go type | extract action                         |
|----------------|-----------------|----------------------------------------|
| `"hello"`      | `string`        | level 1 → Query=v, Source="bare-string"|
| `42`, `3.14`   | `float64`       | level 2 → Query=fmt.Sprint(v), Source="scalar" |
| `true`/`false` | `bool`          | level 2 → Query=fmt.Sprint(v), Source="scalar" |
| `["x"]`        | `[]any`         | level 3 → recurse, Source="array[0]"   |
| `{"query":"x"}`| `map[string]any`| level 4 → **S2** (stub Found=false in S1) |
| `null`         | `nil`           | Found=false                            |
| `""`/nil raw   | (guard)         | Found=false — guard BEFORE Unmarshal   |
| `{bad`         | Unmarshal error | Found=false (treat parse error as fail)|

**GOTCHA (verified):** `json.Unmarshal([]byte(""), &v)` and `Unmarshal` on
whitespace-only both return an error ("unexpected end of JSON input"). And
`req.Params.Arguments` has `json:"arguments,omitempty"` — a missing `arguments`
field arrives as a nil/zero-length RawMessage. **Therefore guard
`len(raw) == 0` BEFORE calling Unmarshal** and return Found=false.

**GOTCHA (verified):** `fmt.Sprint(float64(42))` → `"42"` (NOT `"42.0"`).
`fmt.Sprint(float64(3.14))` → `"3.14"`. `fmt.Sprint(true)` → `"true"`.
`fmt.Sprint(float64(1e20))` → `"1e+20"` (scientific — acceptable edge; contract
says "stringify via fmt.Sprint", not "preserve JSON literal"). Use `fmt.Sprint`
verbatim per contract; do NOT special-case ints.

## 2. Recursion design: extract(raw) + internal extractValue(v) [the S2 seam]

The public contract signature is `extract(raw json.RawMessage, queryAliases,
optionalAliases)`. To recurse on array ELEMENTS (which are decoded `any`, not
RawMessage) WITHOUT re-marshal overhead, split into:

```go
func extract(raw json.RawMessage, q []string, opt map[string][]string) ExtractionResult {
    if len(raw) == 0 { return ExtractionResult{} }
    var v any
    if err := json.Unmarshal(raw, &v); err != nil { return ExtractionResult{} }
    return extractValue(v, q, opt)
}
func extractValue(v any, q []string, opt map[string][]string) ExtractionResult {
    switch x := v.(type) {
    case nil:           return ExtractionResult{}
    case string:        return ExtractionResult{Query: x, Source: "bare-string", Found: true}
    case float64, bool: return ExtractionResult{Query: fmt.Sprint(v), Source: "scalar", Found: true}
    case []any:
        for _, elem := range x {
            if r := extractValue(elem, q, opt); r.Found {
                return ExtractionResult{Query: r.Query, Source: "array[0]", Ambiguous: r.Ambiguous, Optionals: r.Optionals, Found: true}
            }
        }
        return ExtractionResult{}
    case map[string]any:
        // Level 4 (alias scan + inference) — P1.M2.T1.S2. Found=false stub in S1.
        return ExtractionResult{}
    }
    return ExtractionResult{} // unreachable for valid JSON
}
```

**Why this shape:**
- Public `extract(raw)` keeps the contract signature; S2/S3/the server call it.
- `extractValue(v)` is the single type-switch S2 EXTENDS (adds the object case
  body). One seam, no rewrite.
- Array recursion calls `extractValue(elem, ...)` directly — no re-marshal.
- `case float64, bool` is a COMBINED case (both → "scalar"); `v` stays the
  original interface so `fmt.Sprint(v)` works (verified).
- The array case overrides `Source="array[0]"` (per PRD §10.1.3 + contract) but
  PRESERVES Query/Ambiguous/Optionals from the inner result — forward-compatible
  for S2/S3 (e.g. `[{"query":"x","location":"US"}]` later yields query+optionals).

## 3. Source labels (exact, contract-fixed)

| level | input shape      | Source        |
|-------|------------------|---------------|
| 1     | bare string      | `bare-string` |
| 2     | number / boolean | `scalar`      |
| 3     | array            | `array[0]`    |
| (4a)  | object alias     | `nested:<key>`| ← S2 (NOT this subtask)
| (4b)  | inferred         | `inferred:<key>` | ← S3 (NOT this subtask)

S1 only emits `bare-string`, `scalar`, `array[0]`. Found=false rows have
Source="" (zero-value).

## 4. Determinism note (sets up S2, harmless in S1)

`external_deps.md §6` (session 001, on-disk-verified): Go map iteration is
randomized. **S1 levels 1-3 do NOT iterate any map** (objects are stubbed to
Found=false; arrays are walked by index), so determinism is automatic. The
"walk QueryAliases by INDEX, never range the decoded map" rule becomes
LOAD-BEARING in S2's alias scan. extract.go must never introduce `for k := range
someMap` to pick a query — note this for S2.

## 5. Signature/parameter threading

`optionalAliases map[string][]string` is accepted for SIGNATURE STABILITY (S2/S3
consume it) and threaded into `extractValue` recursion, but S1 levels 1-3 NEVER
read it and NEVER populate `ExtractionResult.Optionals` (optionals come from
objects → S3). An unused function parameter is legal Go (no vet warning). This
is intentional: the signature is the cross-subtask contract.

## 6. S1↔S2/S3 boundaries (coordination)

- S1 ships: `ExtractionResult` struct + `extract`/`extractValue` with levels 1-3
  + nil/null/empty/invalid → Found=false + the `map[string]any` STUB (Found=false).
- S2 EXTENDS: replaces the `case map[string]any:` body with the alias-scan +
  nested-drill-in (level 4a). Updates the S1 boundary test rows
  (`empty_object`, `array_of_objects`) to expect Found=true where a query exists.
- S3 EXTENDS: adds inference fallback (4b, ambiguous/longest), optional
  normalization (§10.2 → populates Optionals), failure path (10.1.5), clean
  payload (§10.3).
- Determinism check (`grep 'range' extract.go`) lands in S2; S1 has no map range.

## 7. Test conventions (pinned from on-disk)

- `package main`, file `extract_test.go`, table-driven `t.Run` (matches
  config_test.go/resolve_test.go/logger_test.go).
- `reflect.DeepEqual` on the whole `ExtractionResult` (catches stray
  Optionals/Ambiguous). For Found=false rows, expect the zero value
  `ExtractionResult{}` (Query="", Source="", Ambiguous=false, Optionals=nil).
- Use local `queryAliases`/`optionalAliases` mirroring `DefaultConfig()` (decouple
  from config.go). Inputs as `json.RawMessage(`...`)` literals.
