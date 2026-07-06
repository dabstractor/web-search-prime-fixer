# Research — `json.Unmarshal` over a pre-populated `Config` (the merge primitive)

## Question
P1.M1.T2.S1 requires `LoadConfig(path)` to start from `DefaultConfig()` and let a
file's JSON override **field-by-field**, with **omitted fields keeping their
defaults**. The whole feature rests on `encoding/json`'s behavior when you
unmarshal into an already-populated struct. This probe verifies that behavior
on-disk with the actual installed toolchain (`go1.26.4-X:nodwarf5`, stdlib only).

## Method
A scratch Go program (`/tmp/wspf-probe/main.go`, no deps) defines a small
`Config{ Upstream string; Aliases []string; TargetParam string }` with snake_case
JSON tags, seeds `def := Config{Upstream:"DEF_UP", Aliases:["query","q"],
TargetParam:"search_query"}`, copies `def` into a fresh value, and runs
`json.Unmarshal(jsonBytes, &cfg)` for four payloads. Ran with `go run .`.

## Results

| Case | JSON payload | Result | Verdict |
|------|--------------|--------|---------|
| A — partial override | `{"aliases":["x","y"]}` | `upstream="DEF_UP"` (default kept), `aliases=[x y]` (overridden), `target="search_query"` (default kept) | ✅ omitted fields keep defaults; present fields overwrite |
| B — unknown fields | `{"upstream":"U","banana":42,"unknown":{"deep":true}}` | `err=<nil>`, `upstream="U"`, `aliases=[query q]` (default kept) | ✅ unknown fields silently ignored — default `json.Unmarshal`, NO `DisallowUnknownFields` |
| C — explicit null slice | `{"aliases":null}` | `aliases=nil` (len 0, nil==true) | ⚠️ edge case: `null` nils out the default slice |
| D — empty array | `{"aliases":[]}` | `aliases=[]` (len 0, nil==false) | ⚠️ edge case: `[]` replaces default with empty non-nil slice |

Raw output:
```
A (partial override): upstream="DEF_UP" (kept default?) aliases=[x y] target="search_query"
B (unknown ignored): err=<nil> upstream="U" aliases=[x y]
C (null slice): aliases=[] (len 0, nil? true)
D (empty array): aliases=[] (len 0, nil? false)
```

## Conclusions for the implementation
1. **The merge primitive is just `cfg := DefaultConfig(); json.Unmarshal(data, &cfg)`.**
   No manual field-walk, no `mergo`, no custom merging logic. Go's decoder already
   leaves untouched fields alone and overwrites present ones. This is the exact
   "field-by-field override; omitted fields keep defaults" semantics required by
   the contract.
2. **Do NOT use `json.NewDecoder(...).DisallowUnknownFields()`** — §14.3 mandates
   unknown fields are ignored; the probe confirms plain `Unmarshal` ignores them.
3. **Edge cases (C, D) are out of scope for S1's required tests** (defaults /
   partial / unknown), but are documented in the PRP's Known Gotchas so a later
   subtask or reviewer isn't surprised. `nil`/`[]` over a default slice both
   produce an empty alias list — S2 validation / the rewrite layer must tolerate
   an empty `Aliases` (PRD §10 algorithm already guards `len(aliases)==0`).
4. **`reflect.DeepEqual` is the correct equality primitive** in the tests for
   comparing a loaded `Config` against an expected `Config` that contains a slice
   — it compares slices element-wise, so the partial-override test can assert
   `got == DefaultConfig()-with-only-the-named-fields-overridden`.
