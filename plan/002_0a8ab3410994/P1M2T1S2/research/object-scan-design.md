# Research — P1.M2.T1.S2 Object alias scan with nested drill-in (level 4a)

Scope: pin the exact level-4a algorithm, resolve the source-label ambiguity, and
validate the design empirically (all 25 cases run green against a throwaway
prototype replicating S1's extract + the S2 object case). Every claim is grounded
in the on-disk files (extract.go, config.go) and PRD §10/§12/§15.

## 0. Starting point: S1 is DONE and on disk

`extract.go` + `extract_test.go` EXIST and match the S1 PRP contract verbatim:
- `ExtractionResult{Query, Source, Ambiguous, Optionals, Found}`.
- `extract(raw, queryAliases, optionalAliases)` + internal `extractValue(v, …)`
  type switch: nil→{}; string→bare-string; float64,bool→scalar; []any→array[0]
  (first that yields); **`case map[string]any:` is an explicit Found=false STUB**
  with the comment "// Level 4 … — P1.M2.T1.S2".
- Imports: `encoding/json` + `fmt` only.

**S2's job = REPLACE that stub body + add 2 helpers + add the `sort` import +
update the doc comment (Mode A) + extend extract_test.go.** No signature/struct
change; S3 consumes everything unchanged.

## 1. THE source-label ambiguity — RESOLVED (the #1 contract decision)

The item is EXPLICIT on two of three object sub-cases but SILENT on the shallow-
string case:
- (a) shallow string value at alias key: "use directly" — **Source not stated.**
- (b) shallow number/bool: `Source="scalar"` (explicit).
- (c) map/array value (drill-in): `Source="nested:<key>"` (explicit).

Evidence that resolves (a):

**PRD §15 (line 496) — the `delegate` log enumerates source values:**
> `source` (where the query came from: `query`/`bare-string`/`nested:…`/`inferred:…`/`ambiguous`)

`query` is listed as a PEER of `bare-string`/`nested:…`/`inferred:…`. That is a
bare KEY NAME, not a prefixed label. So a shallow alias match logs the matched
key verbatim ("query" for the canonical case; "q"/"search"/… otherwise).

**PRD §12.2 (line 393) — the warning substitutes `<source>`:**
> `…used "<tool>"/"<source>" rather than the canonical form…`

A bare key name reads correctly here: `used "web_search"/"q"`.

**RESOLUTION (the S2 source-label contract):**
| sub-case | value shape at alias key | Source |
|---|---|---|
| shallow string | string | **the matched alias key verbatim** (e.g. `query`, `q`, `search`) |
| shallow scalar | float64 / bool | `scalar` (item explicit; reuses §10.1.2 label) |
| nested (drill-in) | map / array | `nested:<matched-alias-key>` (item explicit; `<key>` = the TOP-LEVEL alias key, NOT the inner sub-key — per examples `nested:input`, `nested:query`) |

PRD §10.1's label set has NO `alias:<key>` form — do NOT invent one. The shallow
case is the bare key. This is also what makes the M3 canonical check clean
(`source == cfg.CanonicalParam` ⟺ the canonical `query` key was used).

**One edge to flag for cross-task cohesion:** `{"query":42}` → `scalar` (loses
the key), while `{"query":"x"}` → `query`. This honors the item's explicit
(b)="scalar" but means a scalar-at-alias-key is logged structurally, not by key.
M3 (P1.M3.T1.S1) consumes Source as-is; no change needed here, but note it.

## 2. The drill-in algorithm (level 4a, nested case)

When the first-yielding alias key's value is a `map[string]any` or `[]any`:

```
drillIn(val) -> (string, bool):
  STEP 1 (maps only): walk the FIXED sub-key priority slice
     [text, value, content, query, q, data, input]
     and return the value of the FIRST present sub-key whose value is a STRING.
     (Non-string sub-key values are SKIPPED — drill-in extracts STRINGS only.)
  STEP 2 (fallback): recursive descent for the FIRST reachable string:
     - string  -> return it
     - []any   -> recurse by INDEX; return first hit
     - map     -> collect keys, SORT lexicographically, recurse in sorted order
     - else    -> nothing
```

**Determinism (external_deps.md §6, load-bearing here):** STEP 2 MUST sort map
keys before recursing — Go map iteration is randomized, so a plain `for k := range
m` would make "first reachable string" nondeterministic. `sort.Strings(keys)` is
the fix. This is the ONE acceptable map-range in extract.go (collect-then-sort);
it must NOT be confused with the forbidden "range a map to PICK a query".

**STEP 1 is strings-only by design.** `{"query":{"text":42}}`: "text" present but
value is a number → skipped; STEP 2 finds no string → drill fails → (see §3) the
alias key is skipped. Verified: this case yields Found=false (no string anywhere
→ §10.1.5, which is S3's failure path). Coercion does NOT happen inside drill-in
(only the top-level shallow scalar path coerces).

## 3. Alias-scan semantics — "first alias key that YIELDS" (continuation)

The item says "For the first key present in the map" (singular). But the robust,
PRD-faithful ("treat as a query somehow") reading that avoids a real bug:

**Walk `queryAliases` in INDEX order. Return on the FIRST alias key that YIELDS a
string. A present alias key whose value is a non-yielding nested structure (empty
object, array of non-strings) does NOT yield → CONTINUE to the next alias key.**

Why continuation matters — `{"query":{},"q":"x"}`:
- Without continuation (stop at first present key "query"): drill into `{}` → no
  string → alias scan "fails" → S3 inference. But §10.1.4.2 inference EXCLUDES
  alias keys, so "q" is skipped → Found=false → no upstream call. **WRONG** (the
  agent clearly meant "x").
- WITH continuation: "query"→{} drill fails → continue → "q"→"x" → Found=true,
  Source="q". **Correct.**

Shallow string/number/bool ALWAYS yield (immediate return), so continuation only
fires after a nested-drill failure. This still honors "first present alias key"
for the common case (the first present key almost always yields directly).

```
case map[string]any:
  for _, a := range queryAliases {       // INDEX order (deterministic)
    val, ok := x[a]; if !ok { continue }
    switch val.(type) {
    case string:        return {Query: val, Source: a, Found: true}
    case float64, bool: return {Query: fmt.Sprint(val), Source: "scalar", Found: true}
    case map[string]any, []any:
      if s, found := drillIn(val); found { return {Query: s, Source: "nested:"+a, Found: true} }
      // drill found nothing -> CONTINUE to next alias key
    default: // nil (alias present but JSON null) -> skip
    }
  }
  return {}   // no alias yielded -> S3 inference handles no-alias objects
```

## 4. The S2↔S3 boundary (do NOT cross it)

S2 = level 4a ONLY (alias scan + nested drill-in). It does NOT:
- populate `Optionals` (that's §10.2, S3 — optionals come from object inputs),
- set `Ambiguous` (that's §10.1.4.2 inference, S3),
- run inference for no-alias objects (S3),
- build the §10.3 clean payload (S3).

S2 returns `Found=false` (zero result) when NO alias key yields — i.e.:
- no alias key present at all (`{}`, `{"foo":"bar"}`), OR
- alias key(s) present but none yields (`{"query":null}`, `{"query":{}}`,
  `{"query":[1,2,3]}`, `{"query":{"text":42}}`).

**Important:** a no-alias object that DOES contain a string (`{"foo":"bar"}`)
returns Found=false in S2 — inference (which would pick "bar") is S3. This is the
documented S2↔S3 handoff; do not implement inference now.

## 5. Test-row updates vs S1 (the boundary flips)

S1 shipped two explicit S1↔S2 boundary rows in `TestExtract`:
- `empty_object` (`{}`) → **STAYS** `Found=false` (no alias; inference is S3).
- `array_of_objects` (`[{"query":"x"}]`) → **FLIPS to** `{Query:"x",
  Source:"array[0]", Found:true}` (the array recurses into the object, which now
  yields "x" via the alias scan; the array case overrides Source to "array[0]").

S2 ADDS rows for every §10.1.4.1 / §19.1 object shape (see PRP table). Optionals
stay nil and Ambiguous stays false in every S2 row (S3's job).

## 6. Imports: add `sort` (stdlib only)

extract.go currently imports `encoding/json` + `fmt`. S2 adds `sort` (for
`sort.Strings` in the descent fallback). `go.mod` gains ZERO requires.

## 7. Determinism check — UPDATED for S2 (don't false-flag the sort)

S1's Level-1 grep was "range extract.go shows ONLY the []any range". S2 adds two
more `range`s that are both LEGAL:
- `for _, a := range queryAliases` — ranging a SLICE (the alias scan). Deterministic.
- `for k := range <map>` inside firstReachableString — collecting keys that are
  IMMEDIATELY `sort.Strings`'d. Collect-then-sort is deterministic.

The rule to enforce: **extract.go must never range a map whose iteration order
affects which query is picked.** Concretely, the ONLY `for k := range <map>` may
be inside the descent helper, immediately followed by `sort.Strings(...)`. The
alias scan and the sub-key priority walk MUST range SLICES. (Validated by grep in
the PRP Level 1.)

## 8. Empirically validated case table (prototype ran 25/25 green)

All of these PASSED against a throwaway prototype replicating S1 + the S2 object
case (file deleted after; no source touched):

| input | Query | Source | Found |
|---|---|---|---|
| `{"query":"x"}` | x | query | true |
| `{"q":"x"}` | x | q | true |
| `{"search":"x"}` | x | search | true |
| `{"query":42}` | 42 | scalar | true |
| `{"query":true}` | true | scalar | true |
| `{"query":3.14}` | 3.14 | scalar | true |
| `{"input":{"query":"x"}}` | x | nested:input | true |
| `{"query":{"text":"x"}}` | x | nested:query | true |
| `{"query":{"value":"x"}}` | x | nested:query | true |
| `{"query":{"text":"first","value":"second"}}` | first | nested:query | true |
| `{"query":["x","y"]}` | x | nested:query | true |
| `{"query":[{"text":"x"}]}` | x | nested:query | true |
| `{"query":{"foobar":"x"}}` | x | nested:query | true |
| `{"query":{"zebra":"z","apple":"a"}}` | a | nested:query | true (sorted descent) |
| `{"query":{"a":{"b":"deep"}}}` | deep | nested:query | true |
| `{"query":"a","q":"b"}` | a | query | true (config order) |
| `{"q":"b","query":"a"}` | a | query | true (map order irrelevant) |
| `{"query":{},"q":"x"}` | x | q | true (drill continuation) |
| `[{"query":"x"}]` | x | array[0] | true (boundary flip) |
| `{"foo":"bar"}` | "" | "" | false (no alias → S3) |
| `{}` | "" | "" | false |
| `{"query":null}` | "" | "" | false (null alias skipped) |
| `{"query":{}}` | "" | "" | false (drill empty) |
| `{"query":[1,2,3]}` | "" | "" | false (no string) |
| `{"query":{"text":42}}` | "" | "" | false (sub-key non-string, no descent string) |

Determinism stress: 200 runs of `{"query":{"zebra":"z","apple":"a","mango":"m"}}`
→ always Query="a" (sorted descent never flakes). Config-order alias scan never
flakes. These are the load-bearing determinism guarantees.
