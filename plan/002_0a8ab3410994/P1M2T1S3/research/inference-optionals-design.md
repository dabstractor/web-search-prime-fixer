# Research — P1.M2.T1.S3 Inference, optionals, failure, clean payload (levels 4b, 5, §10.2-10.3)

Scope: pin the exact S3 algorithm, resolve the `inferred:<key>` source-label
ambiguity, and validate the design empirically (19/19 S3 cases + 16/16 S2
backward-compat cases run green against a throwaway prototype replicating
S1+S2+S3). Every claim is grounded in the on-disk files (extract.go=S1,
config.go), the S1/S2 PRPs (the frozen contract), and PRD §10/§12/§15.

## 0. Starting point: S1 is DONE on disk; S2 is the live contract

- On disk NOW: `extract.go` = S1 (levels 1-3; object case is a Found=false STUB).
- S2 (implementing in parallel, consumed as a CONTRACT): REPLACES the
  `case map[string]any:` stub with the alias scan + `drillIn`/`firstReachableString`
  helpers, adds the `sort` import, and ends the object case with
  `return ExtractionResult{}` when no alias yields. S2 leaves Optionals nil and
  Ambiguous false on every row (those are S3).
- S3's job = (a) ATTACH normalized optionals to object-case success returns,
  (b) ADD the inference branch where S2's alias loop fell through to Found=false,
  (c) finalize the failure path, (d) ADD `ToUpstreamArgs`. Plus extend
  extract_test.go (add inference/optionals/failure rows; FLIP the S2 boundary row
  `no_alias_string_s3boundary {"foo":"bar"}` to Found=true). Add the `strconv`
  import.

## 1. THE source-label ambiguity for inference — RESOLVED (the #1 decision)

The item says `Source="inferred:<key>"`. PRD §12.2's warning example substitutes a
DEEPER form:

> `…used "<tool>"/"<source>"…` with `<source>` reflecting e.g. `inferred:messages[0].content`

and PRD §15 logs `inferred:…` as a source peer of `query`/`bare-string`/`nested:…`.

**RESOLUTION (validated):** `<key>` is a dotted/bracket JSON PATH from the root
object. It degenerates to the bare immediate key for shallow single-string
objects and reproduces the PRD §12.2 example exactly for chat-shaped input.

| input | chosen string | Source |
|---|---|---|
| `{"foo":"bar"}` | bar | `inferred:foo` |
| `{"a":{"b":"deep search text"}}` | deep search text | `inferred:a.b` |
| `{"messages":[{"role":"user","content":"rust async"}]}` | rust async | `inferred:messages[0].content` |

Path construction: object entry `k` under `path` → `join(path,k)` = `k` if
`path==""` else `path+"."+k`; array element `i` under `path` → `path+"["+i+"]"`.
This is the ONLY reading that satisfies BOTH the item's `inferred:<key>` (simple
case) AND the PRD §12.2 example (deep case). Validated green (see §6).

## 2. The inference algorithm (level 4b)

Reached ONLY when no query alias key yielded a string (S2's alias loop fell
through). At that point any query-alias key that was present is non-yielding
(null/`{}`/`[1,2,3]`/`{"text":42}`) — it has NO reachable string, so descending
into it finds nothing. **Therefore inference need only EXCLUDE optional keys**
(canonical names + their aliases), exactly per the item; query-alias keys need no
explicit exclusion. (If a query-alias key had a reachable string, S2's `drillIn`
would have caught it before inference.)

```
case map[string]any (x):
  opt := extractOptionals(x, optionalAliases)            // §10.2 (shallow); nil if none
  ... S2 alias scan, with Optionals: opt attached to each success return ...
  // (alias loop fell through) -> S3 inference:
  excluded := optionalKeySet(optionalAliases)             // canonical + alias names (a SET)
  cands := collectReachableStrings(x, excluded)           // DFS, skips excluded keys + empty strings
  if len(cands) > 0:
     picked := longestCandidate(cands)                    // longest value; tie = first in collection order
     return {Query: picked.value, Source: "inferred:"+picked.path,
              Ambiguous: len(cands) > 1, Optionals: opt, Found: true}
  return {}                                               // §10.1.5 failure; Optionals nil
```

**Ambiguous** = `len(cands) > 1` (item: "If multiple → pick the LONGEST …
Ambiguous=true"). Single candidate → Ambiguous=false.

**Longest wins; ties:** among candidates of equal max length, the FIRST in
deterministic collection order wins (objects walked by SORTED keys, arrays by
index). `{"a":"xx","b":"yy"}` → Query="xx", Source="inferred:a", Ambiguous=true.
Validated.

**Empty strings are NOT collected.** `{"a":"","b":"real"}` → only "real" →
`inferred:b` (single, not ambiguous). Rationale: §10.1.5 frames failure as "no
usable string anywhere", an empty string is not a usable query, and the
`Found==true ⟹ Query != ""` invariant (S1 ExtractionResult doc) must hold. So an
object of only empty strings (`{"a":"","b":""}`) → failure. Validated.

## 3. collectReachableStrings — determinism (external_deps.md §6, load-bearing)

Identical discipline to S2's `firstReachableString`: the ONLY `for k := range
<map>` collects keys and is IMMEDIATELY followed by `sort.Strings(keys)`. Arrays
walked by index. Objects walked by sorted keys. 500-iteration stress on
`{"zebra":"zz","apple":"aa","mango":"mmm"}` always picks "mmm"/`inferred:mango`
never flakes; `{"messages":[…]}` path always `inferred:messages[0].content`.

```
collectReachableStrings(root map, excluded) -> []candidate   // candidate{path,value}
collect(v, excluded, path, *out):
  string -> if v != "" append {path, v}
  map -> keys=collect; sort.Strings(keys); for k in keys: if excluded[k]: continue;
           collect(x[k], excluded, join(path,k), out)
  []any -> for i,e: collect(e, excluded, path+"["+i+"]", out)
```

## 4. Optional normalization (§10.2) — SHALLOW, canonical-first, nil-when-empty

`extractOptionals(x, optionalAliases)` reads ONLY the top-level object. For each
canonical optional, check the canonical name FIRST, then its aliases in SLICE
order; first present key wins; store its RAW value under the canonical name.
Return nil when nothing found (so non-optional S2 rows keep Optionals=nil →
backward compat with S2's DeepEqual rows).

```
extractOptionals(x, optionalAliases) map[string]any:
  canonOrder := sorted(optionalAliases keys)               // deterministic (order irrelevant, distinct keys)
  for canon in canonOrder:
     keys := [canon] + optionalAliases[canon]               // canonical first, then aliases in slice order
     for k in keys: if v,ok := x[k]; ok { out[canon]=v; break }
  return out (nil if empty)
```

Validated: `{"q":"rust","country":"France"}` → opt={location:France};
`{"q":"rust","location":"US","country":"France"}` → location=US (canonical beats
alias); `{"q":"rust","country":"France","size":"large","recency":"day"}` → all
three optionals. Enum VALUES are NOT validated (out of scope; §10.2) — the raw
value is forwarded as-is.

**No deep read:** a nested `country` under a sub-object is ignored (shallow only).
Validated implicitly (only top-level checked).

**Optionals attach to EVERY object success return** (alias-scan and inference),
NOT just inference — §10.2 is unconditional for object inputs. Validated
(`alias_plus_optional`, `array_obj_optionals`). On FAILURE, Optionals is nil
(item explicit; no query → no upstream call → optionals irrelevant).

## 5. Failure (§10.1.5) + clean payload (§10.3) + ToUpstreamArgs

Failure = no usable string anywhere: `{}`, `{"a":1,"b":true}`, `{"location":"FR"}`
(only an optional, excluded → no candidates), `{"a":[1,2]}`, `null`. Returns the
ZERO `ExtractionResult{}` (Found=false, Query="", Source="", Ambiguous=false,
Optionals=nil). Per FR-6 the server makes NO upstream call (gate on Found).

`ToUpstreamArgs(targetParam)` — the §10.3 guarantee. Returns EXACTLY
`{targetParam: r.Query}` plus normalized optionals; everything else dropped.

```
func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any {
    args := map[string]any{targetParam: r.Query}
    for k, v := range r.Optionals { args[k] = v }
    return args
}
```

Validated: `{"q":"rust","junk":1,"country":"France"}` (Found via alias q=rust,
opt location=France) → ToUpstreamArgs("search_query") =
`{search_query:rust, location:France}` — no "q", no "junk", no "country". This is
the §10.3 "drop everything not recognized" guarantee. Intended for Found==true
results (callers gate on Found; FR-6).

## 6. Empirically validated case table (prototype: 19/19 S3 + 16/16 S2-compat green)

Throwaway standalone module replicating S1+S2+S3 (deleted after; no source
touched). S3 cases (all PASS):

| input | Query | Source | Ambig | Optionals | Found | ToUpstreamArgs("search_query") |
|---|---|---|---|---|---|---|
| `{"foo":"bar"}` | bar | inferred:foo | F | nil | T | {search_query:bar} |
| `{"a":"short","b":"longest string"}` | longest string | inferred:b | T | nil | T | {search_query:longest string} |
| `{"messages":[{"role":"user","content":"rust async"}]}` | rust async | inferred:messages[0].content | T | nil | T | {search_query:rust async} |
| `{"a":{"b":"deep search text"}}` | deep search text | inferred:a.b | F | nil | T | {search_query:deep search text} |
| `{"a":"xx","b":"yy"}` | xx | inferred:a | T | nil | T | {search_query:xx} |
| `{"a":"","b":"real"}` | real | inferred:b | F | nil | T | {search_query:real} |
| `{"location":"France","description":"search rust"}` | search rust | inferred:description | F | {location:France} | T | {search_query:search rust, location:France} |
| `{"country":"France","description":"search rust"}` | search rust | inferred:description | F | {location:France} | T | {search_query:search rust, location:France} |
| `{"q":"rust","country":"France"}` | rust | q | F | {location:France} | T | {search_query:rust, location:France} |
| `{"q":"rust","location":"US","country":"France"}` | rust | q | F | {location:US} | T | {search_query:rust, location:US} |
| `{"q":"rust","country":"France","size":"large","recency":"day"}` | rust | q | F | {location:France,content_size:large,search_recency_filter:day} | T | (all 4 keys) |
| `{"q":"rust","junk":1,"country":"France"}` | rust | q | F | {location:France} | T | {search_query:rust, location:France} |
| `[{"q":"x","country":"FR"}]` | x | array[0] | F | {location:FR} | T | {search_query:x, location:FR} |
| `{"country":"FR","meta":{"x":"noise"},"description":"real query"}` | real query | inferred:description | T | {location:FR} | T | {search_query:real query, location:FR} |
| `{}` | "" | "" | F | nil | F | — |
| `{"a":1,"b":true}` | "" | "" | F | nil | F | — |
| `{"location":"France"}` | "" | "" | F | nil | F | — |
| `{"a":[1,2]}` | "" | "" | F | nil | F | — |
| `null` | "" | "" | F | nil | F | — |

S2 backward-compat (16/16 OK): every S2 object/array row produces the SAME
Query/Source/Found under S3 (attaching `Optionals: opt` where opt is nil keeps
them passing). The ONLY row that FLIPS is the S2 boundary row
`no_alias_string_s3boundary {"foo":"bar"}`: S2 → Found=false; S3 → Found=true,
Source=`inferred:foo` (inference). The 5 genuine-failure S2 rows
(`{}`, `{"query":null}`, `{"query":{}}`, `{"query":[1,2,3]}`, `{"query":{"text":42}}`)
all STAY Found=false (no strings anywhere). Determinism: 500-iter stress on
inference + tie + messages-path never flakes.

## 7. Imports + determinism-grep discipline (updated for S3)

extract.go imports after S3: `encoding/json`, `fmt`, `sort` (S2), **`strconv`**
(S3 — for `strconv.Itoa` in array-index path segments). go.mod gains ZERO requires.

Acceptable `range`s in extract.go after S3 (the rule: a map range may NEVER
decide which query/source is picked unless immediately sorted):
- `for _, elem := range x` ([]any array case) — slice.
- `for _, a := range queryAliases` (alias scan) — slice.
- sub-key priority literal `[text,value,content,query,q,data,input]` in drillIn — slice.
- `for k := range x` in `firstReachableString` — collect, **immediately** `sort.Strings`.
- `for k := range x` in `collect` (inference) — collect, **immediately** `sort.Strings`.
- `for c := range optionalAliases` in `extractOptionals` — collect canonOrder, **immediately** `sort.Strings`.
- `for canon, aliases := range optionalAliases` in `optionalKeySet` — building a SET (order-irrelevant; no sort needed).
- `for k, v := range r.Optionals` in `ToUpstreamArgs` — copying into a map (order-irrelevant; no sort needed).

The two non-sorted map ranges (optionalKeySet, ToUpstreamArgs) are SET-BUILDING /
COPYING — their iteration order cannot affect any decision, so they are safe and
must NOT be false-flagged by a naive grep. The validation grep in the PRP is
written to allow exactly these.
