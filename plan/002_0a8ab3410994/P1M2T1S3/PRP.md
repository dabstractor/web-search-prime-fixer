name: "P1.M2.T1.S3 — Inference fallback, optional normalization, failure path, and clean payload (levels 4b, 5, §10.2-10.3)"
description: |

  Complete the EXISTING `extract.go` (S1 on disk; S2 as the live parallel contract
  that replaces the map-case stub with the alias scan + drill-in) with the FINAL
  behaviors of PRD §10: (a) INFERENCE (§10.1.4.2) — when no query alias yields,
  recursively collect every reachable NON-EMPTY string value EXCLUDING recognized
  optional keys (canonical + aliases), longest-wins, `Source="inferred:<path>"`,
  `Ambiguous = (candidates > 1)`; (b) OPTIONAL NORMALIZATION (§10.2) — read
  optionals SHALLOWLY from the top-level object (canonical name first, then its
  aliases in slice order), store under the CANONICAL name, attach to every object
  success return (alias-scan AND inference), nil on failure; (c) FAILURE (§10.1.5)
  — no usable string anywhere → zero `ExtractionResult{}` (Optionals nil, no
  upstream call per FR-6); (d) CLEAN PAYLOAD — `func (r ExtractionResult)
  ToUpstreamArgs(targetParam string) map[string]any` returning EXACTLY
  `{targetParam: Query}` + normalized optionals, nothing else (§10.3). Add the
  `strconv` import. Rewrite the `extract` doc comment (Mode A) to cover ALL levels
  1-5, optionals, the failure path, and the §10.3 guarantee. Extend
  `extract_test.go` (add inference/optionals/failure/ToUpstreamArgs rows; FLIP the
  S2 boundary row `no_alias_string_s3boundary {"foo":"bar"}` to Found=true). No
  struct/signature change beyond the new method; S3 is the COMPLETION of the extract
  module (consumed by P1.M3 teaching, P1.M4 upstream, P1.M5.T2 server).

---

## Goal

**Feature Goal**: Finish PRD §10 — Query extraction. After S1 (levels 1-3) and S2
(level 4a alias scan + nested drill-in), S3 adds the last three behaviors so
`extract()` recovers a usable search query from ANY `tools/call` arguments payload
the way PRD §10/FR-4 demand: (1) when no configured alias key yields, INFER the
query from the single reachable string (or the longest of several, flagged
ambiguous), excluding recognized optional keys; (2) normalize recognized optional
parameters (location / content_size / search_recency_filter and their aliases)
shallowly from the top-level object and forward them under z.ai's canonical names;
(3) fail cleanly (Found=false, zero result, Optionals nil) when there is genuinely
no usable string anywhere; and (4) expose `ToUpstreamArgs(targetParam)` that emits
the schema-valid `{search_query, …optionals}` payload z.ai requires — dropping
everything else. The result is pure, deterministic, and completely table-tested.

**Deliverable**: TWO MODIFIED files (no new files):
1. **MODIFY** `extract.go` — in `extractValue`'s `case map[string]any:`: compute
   optionals ONCE (shallow) and attach them to every object success return
   (alias-scan and inference); after the S2 alias loop falls through, add the
   inference branch (collect → longest → `inferred:<path>`); keep the trailing
   `return ExtractionResult{}` as the §10.1.5 failure path. ADD four unexported
   helpers: `extractOptionals(map[string]any, map[string][]string) map[string]any`,
   `optionalKeySet(map[string][]string) map[string]bool`, plus an unexported
   `inferredCandidate` struct and `collectReachableStrings`/`collect`/`longestCandidate`
   (reuse S2's `sort`). ADD the exported method
   `func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any`.
   ADD `"strconv"` to the import block (for `strconv.Itoa` in array-index path
   segments). REWRITE the `extract` doc comment (Mode A) to cover levels 1-5,
   optionals, failure, and §10.3.
2. **MODIFY** `extract_test.go` — ADD inference / optionals / failure /
   ToUpstreamArgs rows to `TestExtract`; ADD a `TestToUpstreamArgs` table; FLIP
   the S2 boundary row `no_alias_string_s3boundary` (`{"foo":"bar"}`) from
   Found=false to `{Query:"bar", Source:"inferred:foo", Found:true}`; KEEP the 5
   genuine-failure S2 rows (`empty_object`, `{"query":null}`, `{"query":{}}`,
   `{"query":[1,2,3]}`, `{"query":{"text":42}}`) as Found=false.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run 'TestExtract|TestToUpstreamArgs' -v`) all exit
clean. Every S3 row yields the exact Query/Source/Ambiguous/Optionals/Found in the
validated table (research §6). `go test -run TestExtract -count=100` never flakes
(determinism). `ExtractionResult`'s fields and the `extract`/`extractValue`
signatures are byte-for-byte unchanged from S1/S2 (the ONLY new exported symbol is
the `ToUpstreamArgs` method). All S2 object/array rows still pass unchanged
(attaching `Optionals: opt` where opt is nil keeps them green). `go.mod` gains zero
`require`s (only stdlib `strconv` added).

## User Persona

**Target User**: (1) P1.M3.T1.S1 (teach.go), which reads `Source`/`Ambiguous`/
`Found` to decide the warning and its wording — S3's `inferred:<path>` label and
`Ambiguous` flag are the contract for the "could be wrong guess" warning (PRD §12.2
substitutes `<source>`; the example is literally `inferred:messages[0].content`).
(2) P1.M4.T1.S1+ (upstream.go), which calls `res.ToUpstreamArgs(cfg.TargetParam)`
to build the exact `tools/call` arguments sent to z.ai — S3's clean-payload
guarantee is what makes the upstream call schema-valid. (3) P1.M5.T2 (server
dispatch), which calls `extract(...)` and gates the upstream call on `Found` (FR-6).
(4) the agent, whose `{"foo":"bar"}`, `{"messages":[…]}`, or `{"q":"x","country":…}`
calls now resolve to a query + forwarded optionals instead of failing or losing
data. (5) the maintainer, who gets the COMPLETED, deterministic, table-tested
extractor.

**Use Case**: Agent sends `tools/call` with `{"messages":[{"role":"user","content":
"rust async runtime"}]}` (chat-shaped, no recognized alias). The server calls
`extract(raw, cfg.QueryAliases, cfg.OptionalAliases)`; the alias scan finds no
alias key; S3 inference collects the reachable strings ("user", "rust async
runtime"), excludes none (no optional keys present), picks the LONGEST
("rust async runtime", 17 > 4), flags Ambiguous=true, and returns
`{Query:"rust async runtime", Source:"inferred:messages[0].content", Ambiguous:true,
Found:true}`. The server forwards `search_query="rust async runtime"`; M3 appends a
warning citing `inferred:messages[0].content` so the agent can confirm.

**User Journey**: object args → unmarshal → `extractValue` map case → compute
optionals (shallow) → S2 alias scan (attach optionals on success) → if no alias
yielded → S3 inference (collect non-empty strings excluding optional keys; longest
wins; `inferred:<path>`; ambiguous if >1; attach optionals) → or failure (zero
result, Optionals nil). For an array-of-objects, the `[]any` case recurses into the
object case and PRESERVES the resulting `Optionals` (S1 built this in).

**Pain Points Addressed**: (1) Without S3, ANY object with no recognized alias key
(including the very common `{"foo":…}` and chat-shaped `{"messages":[…]}`) returns
Found=false → no upstream call → the agent gets nothing, even though a query is
clearly present. Inference is the "treat as a query somehow" last resort (FR-4).
(2) Without optional normalization, `location`/`content_size`/`search_recency_filter`
and their aliases are silently dropped, losing functionality the agent intended
(§10.2). (3) Without the §10.3 clean payload, unknown keys (`junk`) would leak
upstream and z.ai would reject the call as schema-invalid. S3 makes every upstream
call schema-valid by construction. (4) Determinism: a naive map range would make
the inferred query and its path label flaky run-to-run — S3 sorts keys in the
collection (external_deps.md §6).

## Why

- Implements **PRD §10.1.4.2** (inference: recursively collect reachable strings,
  excluding optional keys; longest-wins; ambiguous), **§10.1.5** (failure:
  Found=false, no upstream call), **§10.2** (optional normalization, shallow,
  canonical-forwarded), **§10.3** (clean payload: search_query + optionals ONLY),
  **§10.4** (source recorded/logged/surfaced, incl. `inferred:…`/`ambiguous`),
  **§5 FR-4** (extract a query from ANY input; "as a last resort, the longest
  reachable string"; "all other keys are dropped"), **§5 FR-6** (no query →
  immediate warning, no upstream call), and the second half of **§19.1** (the
  inference/optionals/failure rows of the extract test table).
- Honors the determinism mandate from **architecture/external_deps.md §6** (map
  iteration randomized → SORT keys in the inference collection; walk slices where
  order decides the winner). This is the second load-bearing determinism subtask
  (after S2's drill-in).
- Completes the extract module: S3 is the LAST slice of P1.M2.T1. The
  `ExtractionResult` contract (fields + `extract`/`extractValue` signatures) is
  frozen from S1/S2; S3 adds only the `ToUpstreamArgs` method and the inference/
  optionals internals. P1.M3 (teach), P1.M4 (upstream), P1.M5.T2 (server) all
  consume the finished module.
- Pure function completion; no I/O/config/globals. Keeps the M1 + M2.T1.S1/S2 test
  gate green.

## What

### Source-label contract for inference (resolved from PRD §12.2 + §15; see research §1)

The item says `Source="inferred:<key>"`. PRD §12.2's warning example substitutes
`inferred:messages[0].content`. **Resolution:** `<key>` is a dotted/bracket JSON
PATH from the root object. It degenerates to the bare immediate key for shallow
single-string objects and reproduces the PRD example exactly for chat-shaped input.

| chosen string location | Source |
|---|---|
| `{"foo":"bar"}` | `inferred:foo` |
| `{"a":{"b":"deep"}}` | `inferred:a.b` |
| `{"messages":[{"content":"rust async"}]}` | `inferred:messages[0].content` |

Path rules: object entry `k` under path `p` → `p+"."+k` (or bare `k` if `p==""`);
array element `i` under path `p` → `p+"["+strconv.Itoa(i)+"]"`.

### Object case (S3 adds to S2's map case)

```text
case map[string]any (x):
  opt := extractOptionals(x, optionalAliases)                 # §10.2; nil if none
  # --- S2 alias scan (level 4a), with Optionals: opt attached to each return ---
  for each alias a in queryAliases (INDEX order):             # deterministic
    val, present := x[a]; if !present: continue
    switch val.(type):
      string:        return {Query: val, Source: a,            Optionals: opt, Found: true}
      float64, bool: return {Query: fmt.Sprint(val), Source:"scalar", Optionals: opt, Found: true}
      map[string]any, []any:
        if s, ok := drillIn(val); ok: return {Query: s, Source: "nested:"+a, Optionals: opt, Found: true}
        # drill found nothing -> CONTINUE to next alias (S2 continuation)
      default: # nil -> skip
  # --- S3 inference (level 4b) — S2 fell through to Found=false here ---
  excluded := optionalKeySet(optionalAliases)                 # canonical + alias names (a SET)
  cands := collectReachableStrings(x, excluded)               # DFS; skips excluded keys + empty strings
  if len(cands) > 0:
     picked := longestCandidate(cands)                        # longest value; tie = first in collection order
     return {Query: picked.value, Source: "inferred:"+picked.path,
             Ambiguous: len(cands) > 1, Optionals: opt, Found: true}
  return ExtractionResult{}                                   # §10.1.5 failure; Optionals nil
```

### Inference semantics (pinned)

- **Ambiguous = `len(cands) > 1`** (item: "If multiple → pick the LONGEST …
  Ambiguous=true"). Single candidate → Ambiguous=false.
- **Longest wins; ties** → first in deterministic collection order (objects walked
  by SORTED keys, arrays by index). `{"a":"xx","b":"yy"}` → "xx"/`inferred:a`.
- **Empty strings are NOT collected** (`v != ""` guard). An empty string is not a
  "usable" query (§10.1.5) and the `Found==true ⟹ Query != ""` invariant must
  hold. `{"a":"","b":""}` → failure.
- **Only optional keys are excluded** (canonical names + their aliases). Query-alias
  keys need no explicit exclusion: if one had a reachable string, S2's `drillIn`
  would have caught it before inference; if it didn't, descending finds nothing.

### Optional normalization (§10.2) — shallow, canonical-first, nil-when-empty

```text
extractOptionals(x, optionalAliases) map[string]any:
  for each canon in sorted(optionalAliases keys):             # deterministic (distinct keys; order irrelevant)
     keys := [canon] + optionalAliases[canon]                 # canonical FIRST, then aliases in slice order
     for k in keys: if v,ok := x[k]; ok { out[canon] = v; break }   # first present wins
  return out   # nil if nothing found (so S2 rows keep Optionals nil)
```

- **Shallow only:** only the TOP-LEVEL object is read; a nested optional key is
  ignored.
- **Raw value forwarded:** enum VALUES are NOT validated/translated (out of scope,
  §10.2). If z.ai rejects a value, the error surfaces honestly upstream.
- **Attaches to every object success return** (alias-scan AND inference), nil on
  failure (item explicit).

### Clean payload (§10.3) — ToUpstreamArgs

```go
// ToUpstreamArgs builds the arguments forwarded to z.ai (PRD §10.3): exactly
// targetParam -> Query plus any normalized optionals. Everything else is dropped.
// Intended for Found==true results; callers gate upstream calls on Found (FR-6).
func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any {
	args := map[string]any{targetParam: r.Query}
	for k, v := range r.Optionals {
		args[k] = v
	}
	return args
}
```

### Success Criteria

- [ ] The `case map[string]any:` body computes `opt := extractOptionals(...)` once
      and attaches `Optionals: opt` to EVERY success return (alias string/scalar/
      nested AND inference); the trailing `return ExtractionResult{}` is the
      §10.1.5 failure path (Optionals nil).
- [ ] Inference: when no alias yields, `collectReachableStrings` recursively
      collects every reachable NON-EMPTY string EXCLUDING optional canonical names
      + their aliases; `longestCandidate` picks the longest (tie = first in sorted
      collection order); `Source="inferred:"+path`; `Ambiguous = len(cands) > 1`.
- [ ] Inference path labels match: `{"foo":"bar"}`→`inferred:foo`;
      `{"a":{"b":"x"}}`→`inferred:a.b`;
      `{"messages":[{"content":"x"}]}`→`inferred:messages[0].content`.
- [ ] Optionals are read SHALLOWLY (top-level only); canonical name checked first,
      then aliases in slice order; first present wins; stored under the canonical
      name; nil when none found (S2 rows keep Optionals nil).
- [ ] `ToUpstreamArgs(targetParam)` returns EXACTLY `{targetParam: Query}` plus
      normalized optionals; no alias names, no unrecognized keys (verified:
      `{"q":"x","junk":1,"country":"FR"}` → `{search_query:x, location:FR}`).
- [ ] Failure (`{}`, `{"a":1}`, `{"location":"FR"}`, `{"a":[1,2]}`, `null`,
      `{"a":""}`) → zero `ExtractionResult{}` (Found=false, Optionals nil).
- [ ] `ExtractionResult` fields + `extract`/`extractValue` signatures UNCHANGED
      from S1/S2; the ONLY new exported symbol is `ToUpstreamArgs`.
- [ ] extract.go adds ONLY the `strconv` import; `go.mod` gains zero requires.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` clean;
      `go test -run TestExtract -count=100` never flakes.

## All Needed Context

### Context Completeness Check

_Pass._ The function under test (`extract.go`, currently S1 on disk) and the file
being extended (`extract_test.go`) are read. The S2 PRP (the live parallel
contract) is read in full — it fixes the alias scan, `drillIn`,
`firstReachableString`, the source labels, and the "no alias yields → Found=false"
handoff that S3 extends. The ONE ambiguity (`inferred:<key>` vs the PRD §12.2 deep
example `inferred:messages[0].content`) is RESOLVED to a dotted/bracket path and is
**empirically validated** by a throwaway prototype that ran 19/19 S3 cases + 16/16
S2-backward-compat cases green (research §6), including the exact
`inferred:messages[0].content` label. The determinism mechanism (sorted keys in the
collection) is pinned to external_deps.md §6. The exact expected
Query/Source/Ambiguous/Optionals/Found/ToUpstreamArgs for every case is in the
validated table. The import addition (`strconv`), the one test-row FLIP
(`no_alias_string_s3boundary`), and the 5 S2 failure rows that STAY false are all
identified. An agent with no prior knowledge can implement this from the PRP + the
on-disk S1 `extract.go` + the S2 PRP alone.

### Documentation & References

```yaml
# MUST READ — the function under test and the file being extended.
- file: extract.go
  why: the EXACT file S3 modifies. Today it is S1 (levels 1-3; object STUB). When
        S3 starts, S2 will have replaced the map-case stub with the alias scan +
        drillIn/firstReachableString and added the `sort` import; S3 EDITS that map
        case (compute optionals, attach to success returns, add inference, keep
        failure) and ADDS ToUpstreamArgs + 4 helpers + the `strconv` import.
  pattern: S1's type-switch shape; the array case's "preserve Query/Ambiguous/
        Optionals" rule (so array-of-object optionals flow through); pure/det style.
  gotcha: the array case already copies r.Optionals into its return — S3 populating
        Optionals in the object case AUTOMATICALLY works for arrays of objects
        (validated: [{"q":"x","country":"FR"}] -> array[0] + location:FR).

- file: extract_test.go
  why: the test file S3 extends. Reuse extractQA/extractOpt, the table-driven
        TestExtract shape, reflect.DeepEqual. FLIP no_alias_string_s3boundary;
        ADD inference/optionals/failure rows + TestToUpstreamArgs; KEEP the 5 S2
        failure rows as Found=false.
  pattern: existing rows assert the whole ExtractionResult via DeepEqual (catches
        stray Optionals/Ambiguous); S3's new rows do the same.

- file: PRD.md
  section: "§10.1.4.2" (inference: collect every reachable string excluding optional
        keys; exactly-one -> use it; several -> longest, ambiguous=true; source
        inferred:<key>); "§10.1.5" (failure: no usable string anywhere -> fail,
        Found=false, no upstream call, FR-6); "§10.2" (optionals location/content_size/
        search_recency_filter + their aliases; forwarded under canonical name; NOT
        validated; shallow); "§10.3" (send z.ai EXACTLY search_query + optionals;
        everything else dropped); "§10.4" (source incl. inferred:…/ambiguous logged);
        "§5 FR-4" (last resort = longest reachable string; all other keys dropped);
        "§12.2" (warning substitutes <source>; EXAMPLE is inferred:messages[0].content);
        "§15" (delegate log: source values include inferred:…/ambiguous; optionals
        field = normalized names provided); "§19.1" (extract_test over every structure
        incl. multi-string longest-wins ambiguous, chat-shaped messages, failure).
  critical: §12.2 line ~404 pins the inference source label shape to a PATH
        (inferred:messages[0].content) — S3's dotted/bracket path reproduces it.
        §10.3 is the "drop everything not recognized" guarantee ToUpstreamArgs enforces.

# VERIFIED DESIGN — the resolved source-label/path, the algorithm, the validated table.
- docfile: plan/002_0a8ab3410994/P1M2T1S3/research/inference-optionals-design.md
  why: §1 resolves inferred:<key> -> dotted/bracket path (with PRD line citations);
        §2 the inference algorithm + ambiguous + longest-wins + empty-skip; §3 the
        collectReachableStrings determinism; §4 optional normalization (shallow,
        canonical-first, nil-when-empty); §5 failure + ToUpstreamArgs; §6 the
        VALIDATED 19-case S3 table + 16-case S2-compat table (copy want* verbatim);
        §7 imports + the determinism-grep discipline (which map ranges are legal).

# CONTRACTS (read-only — do not break):
- file: plan/002_0a8ab3410994/P1M2T1S2/PRP.md
  why: defines what S2 ships on disk BEFORE S3 starts: the map-case body = alias
        scan + drillIn/firstReachableString; the `sort` import; source labels
        (<key>/scalar/nested:<key>); the trailing `return ExtractionResult{}` when
        no alias yields (the seam S3 replaces with inference). S3 EDITS this map
        case (attaches optionals, adds inference) — do not re-implement the alias
        scan or drillIn; reuse S2's.
- file: plan/002_0a8ab3410994/P1M2T1S1/PRP.md
  why: defines ExtractionResult/extract/extractValue EXACTLY as shipped. S3 adds
        ONLY the ToUpstreamArgs method; fields/signatures frozen.
- file: plan/002_0a8ab3410994/P1M1T3S1/PRP.md
  why: M1 test rewrite owns config_test.go/resolve_test.go/health_test.go — DO NOT
        TOUCH. extract.go shares no symbols with them; both keep go test ./... green.

# DETERMINISM MANDATE (load-bearing in S3, as in S2).
- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§6. Go map ordering is non-deterministic"
  why: PROVES inference collection must SORT map keys before recursing (else the
        chosen candidate + its path label flake). Set-building (optionalKeySet) and
        copy-into-map (ToUpstreamArgs) are exempt (order-irrelevant).
  critical: the ONLY `for k := range <map>` that decides a result is inside
        collect/firstReachableString/extractOptionals and is immediately followed by
        sort.Strings. See research §7.

# CONFIG (read-only) — the real caller's args; extract.go does NOT import config.
- file: config.go
  why: shows the real call extract(raw, cfg.QueryAliases, cfg.OptionalAliases) and
        cfg.TargetParam ("search_query") for ToUpstreamArgs. OptionalAliases is the
        3-key map[string][]string S3 reads in extractOptionals/optionalKeySet.
  pattern: extract takes plain params (no Config) -> stays pure/unit-testable.

# CONSUMERS (forward references — what S3 must produce for them):
- file: plan/002_0a8ab3410994/prd_snapshot.md
  section: "§11.3" (upstream CallTool args come from the extract result); "§12.1"/
        "§12.2" (teach.go reads Source/Ambiguous/Found; warning substitutes <source>,
        example inferred:messages[0].content); "§19.3" (server_test case 4: {} -> no
        upstream call; case 2: junk dropped -> clean search_query).
  why: pins what M3 (Source/Ambiguous/Found), M4 (ToUpstreamArgs), and M5.T2 (Found
        gate + ToUpstreamArgs) consume from S3's result.

# Go stdlib refs (stable).
- url: https://pkg.go.dev/strconv#Itoa
  why: format array-index path segments ("[0]", "[1]") deterministically.
- url: https://pkg.go.dev/sort#Strings
  why: sort map keys in collect/extractOptionals for determinism.
```

### Current Codebase tree (the INPUT state — when S3 starts, S2 is merged on disk; run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap — UNTOUCHED by S3
  config.go       # Config{QueryAliases,OptionalAliases,TargetParam,...} — UNTOUCHED
  logger.go, health.go — UNTOUCHED
  config_test.go, resolve_test.go, health_test.go — owned by P1.M1.T3.S1 — DO NOT TOUCH
  logger_test.go  # green — UNTOUCHED
  extract.go      # S1+S2 — ExtractionResult + extract/extractValue (levels 1-4a;
                  #   map case = alias scan + drillIn; imports json/fmt/sort). <- S3 MODIFIES
  extract_test.go # S1+S2 — TestExtract (levels 1-4a + boundary rows). <- S3 MODIFIES
  testdata/, README.md, config.example.json, PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
extract.go       # MODIFIED (not new). map case: +extractOptionals attach, +inference
                  #      branch (collectReachableStrings/collect/longestCandidate/
                  #      optionalKeySet/inferredCandidate), failure stays. +ToUpstreamArgs
                  #      method. +`strconv` import. extract doc comment rewritten (Mode A,
                  #      levels 1-5 + optionals + §10.3).
extract_test.go  # MODIFIED (not new). +inference/optionals/failure rows in TestExtract;
                  #      +TestToUpstreamArgs; no_alias_string_s3boundary FLIPPED to
                  #      Found=true; 5 S2 failure rows kept Found=false. No new imports.
# NO other files created or modified.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL (inference source = PATH, not bare key): the item says inferred:<key> but
// PRD §12.2's example is inferred:messages[0].content. <key> is a dotted/bracket JSON
// PATH from the root object: object entry -> parent+"."+key; array element ->
// parent+"["+i+"]". {"foo":"bar"} -> inferred:foo; {"messages":[{"content":"x"}]} ->
// inferred:messages[0].content. (Validated green; research §1.)

// CRITICAL (empty strings are NOT inference candidates): collectReachableStrings must
// SKIP empty strings (v != ""). An empty string is not a "usable" query (§10.1.5) and
// Found==true implies Query != "" (S1 invariant). {"a":"","b":""} -> failure.

// CRITICAL (only OPTIONAL keys are excluded from inference): do NOT exclude query-alias
// keys. If a query-alias key had a reachable string, S2's drillIn caught it before
// inference; if it didn't, descending finds nothing. Build the excluded set from
// optionalAliases (canonical names + every alias) ONLY. (research §2.)

// CRITICAL (optionals attach to EVERY object success return, alias-scan AND inference):
// §10.2 is unconditional for object inputs. Compute opt ONCE at the top of the map case
// and attach it to each success return. On FAILURE, return ExtractionResult{} (opt is
// NOT attached -> Optionals nil). A present optional with no query -> failure (no
// upstream call; FR-6) -> optionals irrelevant.

// CRITICAL (extractOptionals returns nil when empty): so S2's no-optional rows keep
// Optionals == nil and still pass reflect.DeepEqual. Do NOT return an empty non-nil map.

// CRITICAL (Ambiguous = candidates > 1, regardless of length gap): {"a":"x","b":
// "verylong"} -> Ambiguous=true even though "verylong" is unambiguously longest. The
// flag means "there was more than one reachable string"; the query is the longest.

// CRITICAL (longest tie-break = first in deterministic collection order): when two
// candidates share the max length, pick the first encountered while walking SORTED map
// keys + INDEX array order. {"a":"xx","b":"yy"} -> "xx"/inferred:a. Do NOT range the
// map unsorted to break ties (nondeterminism; external_deps.md §6).

// CRITICAL (determinism): the ONLY `for k := range <map>` that decides a result is in
// collect / firstReachableString / extractOptionals and is IMMEDIATELY followed by
// sort.Strings. optionalKeySet (set-building) and ToUpstreamArgs (copy-into-map) range
// maps but their order cannot affect any decision -> legal, NOT a flake. (research §7.)

// CRITICAL (S2 backward-compat): S3 must NOT change Query/Source/Found for any S2
// object/array row. Attaching Optionals: opt where opt is nil keeps them byte-identical.
// The ONLY row that changes is no_alias_string_s3boundary {"foo":"bar"} (S2 Found=false
// -> S3 Found=true, inferred:foo). The 5 genuine-failure S2 rows STAY Found=false.

// GOTCHA (import): add ONLY "strconv" to extract.go's import block (S2 already added
// "sort"). Keep encoding/json + fmt + sort. Do NOT add reflect/slices/maps/third-party.

// GOTCHA (array recursion already forwards optionals): the S1 []any case returns
// {Query: r.Query, Source: "array[0]", Ambiguous: r.Ambiguous, Optionals: r.Optionals,
// Found: true}. S3 populating Optionals in the object case makes array-of-object
// optionals work with NO change to the array case. Do not touch the array case.

// GOTCHA (failure is the ZERO value): return ExtractionResult{} literally (Query:"",
// Source:"", Ambiguous:false, Optionals:nil, Found:false). Never return Found=false with
// a non-empty Source or a non-nil Optionals.

// GOTCHA (ToUpstreamArgs is for Found==true): it blindly returns {targetParam: Query} +
// optionals. On a Found==false result that is {search_query: ""}. Callers (M4/M5) gate
// on Found; do not add a Found check inside ToUpstreamArgs (it is a pure transform).
```

## Implementation Blueprint

### Data models and structure

No data-model change. `ExtractionResult` (S1) is frozen. S3 adds:

```go
// inferredCandidate is a reachable non-empty string collected during inference
// (level 4b), paired with its dotted/bracket path from the root object.
type inferredCandidate struct {
	path  string
	value string
}
```

…plus four unexported helpers (`extractOptionals`, `optionalKeySet`,
`collectReachableStrings` + internal `collect`, `longestCandidate`) and the exported
`ToUpstreamArgs` method.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: MODIFY extract.go — add the `strconv` import + the inference/optional helpers
  - ADD import: "strconv" (keep encoding/json, fmt, sort). PLACEMENT: alphabetical in
        the import block (after sort, before nothing — actually strconv > sort
        lexicographically, so after "sort").
  - DEFINE: `type inferredCandidate struct { path string; value string }` (unexported).
  - DEFINE: `func extractOptionals(x map[string]any, optionalAliases map[string][]string)
        map[string]any` — sorted canonical order; for each, check [canon]+aliases in
        slice order; first present -> out[canon]=raw value; return nil if empty.
  - DEFINE: `func optionalKeySet(optionalAliases map[string][]string) map[string]bool` —
        set of every canonical name + every alias (order-irrelevant set-building).
  - DEFINE: `func collectReachableStrings(root map[string]any, excluded map[string]bool)
        []inferredCandidate` — allocates `out := []inferredCandidate{}` and calls
        `collect(root, excluded, "", &out)`; returns out.
  - DEFINE: `func collect(v any, excluded map[string]bool, path string, out
        *[]inferredCandidate)` — string(v!="") -> append{path,v}; map -> collect keys,
        sort.Strings, skip excluded[k], recurse with join(path,k); []any -> recurse by
        index with path+"["+strconv.Itoa(i)+"]".
  - DEFINE: `func longestCandidate(cands []inferredCandidate) inferredCandidate` — first
        element, then keep any with strictly greater len(value) (so ties keep the FIRST
        in collection order).
  - DOC COMMENT on each helper (brief): extractOptionals = "shallow optional-parameter
        normalization (PRD §10.2)"; optionalKeySet = "set of optional canonical names +
        aliases to exclude from inference"; collectReachableStrings/collect = "DFS for
        reachable non-empty strings excluding optional keys; maps sorted for determinism";
        longestCandidate = "longest value; ties keep first in collection order".
  - NAMING: all unexported. PLACEMENT: after extractValue / after S2's drillIn helpers.

Task 2: MODIFY extract.go — extend the map case (optionals attach + inference)
  - AT THE TOP of `case map[string]any:` (x), ADD: `opt := extractOptionals(x,
        optionalAliases)`.
  - EDIT each S2 alias-scan success return to attach optionals:
        string -> {Query: val.(string), Source: a, Optionals: opt, Found: true}
        float64,bool -> {Query: fmt.Sprint(val), Source: "scalar", Optionals: opt, Found: true}
        map,array drillIn ok -> {Query: s, Source: "nested:"+a, Optionals: opt, Found: true}
        (i.e. add `Optionals: opt,` to each; S2 had no Optionals field set.)
  - REPLACE S2's trailing `return ExtractionResult{}` (after the alias loop, the "no
        alias yielded -> S3 inference" comment) with the inference branch:
        excluded := optionalKeySet(optionalAliases)
        cands := collectReachableStrings(x, excluded)
        if len(cands) > 0 {
            picked := longestCandidate(cands)
            return ExtractionResult{Query: picked.value, Source: "inferred:"+picked.path,
                Ambiguous: len(cands) > 1, Optionals: opt, Found: true}
        }
        return ExtractionResult{}   // §10.1.5 failure; Optionals nil
  - CONSTRAINT: walk queryAliases BY INDEX (S2, unchanged); sort keys in collect and
        extractOptionals; NEVER range a map to pick a query/path order unsorted.
  - PRESERVE: every other case (nil/string/float64,bool/[]any) and the function
        signatures. Do NOT touch the []any case (it already forwards Optionals).

Task 3: MODIFY extract.go — add the ToUpstreamArgs method
  - DEFINE: `func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any`
        per the blueprint (args := map[string]any{targetParam: r.Query}; copy r.Optionals;
        return args). Value receiver. Mode-A doc comment citing PRD §10.3 + FR-6 gate.
  - PLACEMENT: after the ExtractionResult type definition (methods near the type) OR
        after extractValue — either is fine; keep gofmt happy.

Task 4: MODIFY extract.go — rewrite the extract doc comment (Mode A) to cover ALL levels
  - COVER: levels 1-5 — string->bare-string; number/bool->scalar; array->array[0]
        (first that yields); object alias scan-><key>/scalar/nested:<key> (S2); object
        inference (no alias yields)->inferred:<path>, longest-wins, Ambiguous=candidates>1,
        excludes optional keys, skips empty strings (S3); failure (no usable string
        anywhere)->Found=false, no upstream call (FR-6). PLUS §10.2 optional normalization
        (shallow, canonical-first, forwarded under canonical name, NOT validated) and the
        §10.3 ToUpstreamArgs guarantee (targetParam+optionals ONLY, everything else
        dropped). PLUS "pure and deterministic — no I/O, no globals; dispatch on the
        decoded Go type; map keys sorted wherever a result depends on order".
  - REMOVE the S1 "until then it returns Found==false" / S2 "no alias -> S3" lines.

Task 5: MODIFY extract_test.go — extend TestExtract + add TestToUpstreamArgs
  - FLIP the S2 boundary row:
        no_alias_string_s3boundary {"foo":"bar"} -> {Query:"bar", Source:"inferred:foo", Found:true}
        (was Found=false; S3 inference now resolves it).
  - KEEP Found=false: empty_object {}, and (if present) alias_null {"query":null},
        nested_empty {"query":{}}, nested_array_no_string {"query":[1,2,3]},
        nested_subkey_nonstring {"query":{"text":42}}.
  - ADD inference rows (copy want* from research §6 verbatim). Minimum set:
      infer_single {"foo":"bar"} -> {bar, "inferred:foo", Found:true}              (Ambiguous false)
      infer_multi_longest {"a":"short","b":"longest string"} -> {longest string, "inferred:b", Ambiguous:true}
      infer_messages {"messages":[{"role":"user","content":"rust async"}]} -> {rust async, "inferred:messages[0].content", Ambiguous:true}
      infer_nested_single {"a":{"b":"deep search text"}} -> {deep search text, "inferred:a.b"}
      infer_tie {"a":"xx","b":"yy"} -> {xx, "inferred:a", Ambiguous:true}
      infer_skip_empty {"a":"","b":"real"} -> {real, "inferred:b"}                 (single, NOT ambiguous)
      infer_exclude_optional {"location":"France","description":"search rust"} ->
           {search rust, "inferred:description", Optionals:{location:"France"}}
      infer_exclude_optalias {"country":"France","description":"search rust"} ->
           {search rust, "inferred:description", Optionals:{location:"France"}}
  - ADD optional-normalization rows:
      alias_plus_optional {"q":"rust","country":"France"} -> {rust, "q", Optionals:{location:"France"}}
      canon_wins_over_alias {"q":"rust","location":"US","country":"France"} -> {rust, "q", Optionals:{location:"US"}}
      multi_optionals {"q":"rust","country":"France","size":"large","recency":"day"} ->
           {rust, "q", Optionals:{location:"France",content_size:"large",search_recency_filter:"day"}}
      array_obj_optionals [{"q":"x","country":"FR"}] -> {x, "array[0]", Optionals:{location:"FR"}}
  - ADD failure rows (assert want == ExtractionResult{}):
      fail_empty_object {}, fail_no_strings {"a":1,"b":true}, fail_only_optional
        {"location":"France"}, fail_array_nons {"a":[1,2]}, (null already exists).
  - ADD `TestToUpstreamArgs` table: for each Found==true row above that has optionals,
        assert reflect.DeepEqual(got.ToUpstreamArgs("search_query"), wantMap). Include:
        dropped_junk {"q":"rust","junk":1,"country":"France"} ->
           {search_query:"rust", location:"France"} (no "q", no "junk", no "country").
        A no-optional row {"q":"rust"} -> {search_query:"rust"} (only the target param).
  - ASSERT each TestExtract row via reflect.DeepEqual on the WHOLE ExtractionResult
        (catches stray Optionals/Ambiguous). For Optionals, build the want map
        explicitly; nil when none.
  - NAMING: subtest names match row semantics. PLACEMENT: append to the tests slice;
        TestToUpstreamArgs as a new top-level test func.

Task 6: VALIDATE
  - RUN: gofmt -w extract.go extract_test.go; go vet ./...; go build ./...;
        go test -run 'TestExtract|TestToUpstreamArgs' -v; go test -run TestExtract
        -count=100; go test ./...
  - CONFIRM determinism grep: every `for k := range <map>` that affects a result is
        immediately followed by sort.Strings; optionalKeySet/ToUpstreamArgs ranges are
        set-building/copying (legal). The alias scan ranges a slice.
  - CONFIRM only `strconv` added (git diff --stat extract.go); go.mod unchanged.
```

### Implementation Patterns & Key Details

```go
// extractOptionals — shallow optional normalization (PRD §10.2). Returns nil when
// nothing found so S2 rows keep Optionals nil.
func extractOptionals(x map[string]any, optionalAliases map[string][]string) map[string]any {
	var out map[string]any
	canonOrder := make([]string, 0, len(optionalAliases))
	for c := range optionalAliases { // collect — sorted immediately below
		canonOrder = append(canonOrder, c)
	}
	sort.Strings(canonOrder)
	for _, canon := range canonOrder {
		keys := append([]string{canon}, optionalAliases[canon]...) // canonical first, then aliases in slice order
		for _, k := range keys {
			if v, ok := x[k]; ok {
				if out == nil {
					out = map[string]any{}
				}
				out[canon] = v
				break // first present key for this canonical wins
			}
		}
	}
	return out
}

// optionalKeySet — every optional canonical name + alias (the keys excluded from
// inference). Set-building: map iteration order is irrelevant here.
func optionalKeySet(optionalAliases map[string][]string) map[string]bool {
	set := map[string]bool{}
	for canon, aliases := range optionalAliases {
		set[canon] = true
		for _, a := range aliases {
			set[a] = true
		}
	}
	return set
}

type inferredCandidate struct {
	path  string
	value string
}

func collectReachableStrings(root map[string]any, excluded map[string]bool) []inferredCandidate {
	out := []inferredCandidate{}
	collect(root, excluded, "", &out)
	return out
}

// collect — DFS for reachable NON-EMPTY strings, skipping excluded keys. Map keys
// are SORTED before recursion (deterministic; external_deps.md §6). path is the
// dotted/bracket JSON path from the root object.
func collect(v any, excluded map[string]bool, path string, out *[]inferredCandidate) {
	switch x := v.(type) {
	case string:
		if x != "" { // empty strings are not usable candidates
			*out = append(*out, inferredCandidate{path: path, value: x})
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x { // collect — sorted immediately below
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if excluded[k] {
				continue
			}
			cp := k
			if path != "" {
				cp = path + "." + k
			}
			collect(x[k], excluded, cp, out)
		}
	case []any:
		for i, e := range x {
			collect(e, excluded, path+"["+strconv.Itoa(i)+"]", out)
		}
	}
}

// longestCandidate returns the candidate with the longest value; ties keep the FIRST
// in collection order (strictly-greater comparison).
func longestCandidate(cands []inferredCandidate) inferredCandidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if len(c.value) > len(best.value) {
			best = c
		}
	}
	return best
}

// ToUpstreamArgs builds the clean arguments forwarded to z.ai (PRD §10.3): exactly
// targetParam -> Query plus any normalized optionals. Everything else is dropped.
// Intended for Found==true results; callers gate upstream calls on Found (FR-6).
func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any {
	args := map[string]any{targetParam: r.Query}
	for k, v := range r.Optionals {
		args[k] = v
	}
	return args
}

// --- the inference branch added inside the map case (after S2's alias loop) ---
	excluded := optionalKeySet(optionalAliases)
	cands := collectReachableStrings(x, excluded)
	if len(cands) > 0 {
		picked := longestCandidate(cands)
		return ExtractionResult{
			Query:     picked.value,
			Source:    "inferred:" + picked.path,
			Ambiguous: len(cands) > 1,
			Optionals: opt,
			Found:     true,
		}
	}
	return ExtractionResult{} // §10.1.5 failure; Optionals nil
```

### Integration Points

```yaml
FILES MODIFIED:
  - extract.go: edit the map case (compute opt, attach to success returns, add
    inference, keep failure); add extractOptionals/optionalKeySet/collectReachableStrings/
    collect/longestCandidate/inferredCandidate; add ToUpstreamArgs; add `strconv` import;
    rewrite the extract doc comment (Mode A, levels 1-5 + §10.2 + §10.3).
  - extract_test.go: add inference/optionals/failure rows + TestToUpstreamArgs; flip
    no_alias_string_s3boundary to Found=true; keep the 5 S2 failure rows false.
NO OTHER FILES TOUCHED:
  - config*.go, logger.go, health.go, main.go, doc.go: untouched.
  - config_test.go/resolve_test.go/health_test.go: owned by P1.M1.T3.S1 — DO NOT TOUCH.
  - logger_test.go: green, untouched.
CONSUMER SEAMS (S3 finalizes the extract module; no further signature change):
  - P1.M3.T1.S1 (teach.go): reads Source/Ambiguous/Found. S3's inferred:<path> label
        and Ambiguous flag drive the "could be wrong guess" warning (PRD §12.2
        substitutes <source>; example inferred:messages[0].content). Canonical iff
        Source == cfg.CanonicalParam ("query").
  - P1.M4.T1 (upstream.go): calls res.ToUpstreamArgs(cfg.TargetParam) to build the
        exact tools/call arguments ("search_query" + optionals).
  - P1.M5.T2 (server dispatch): `res := extract(req.Params.Arguments,
        cfg.QueryAliases, cfg.OptionalAliases)`; on res.Found==false -> FR-6 immediate
        warning, NO upstream call; else forward res.ToUpstreamArgs(cfg.TargetParam).
DATABASE / ROUTES / ENV: none. Pure function, no I/O, no globals.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# After editing extract.go + extract_test.go — fix before running tests.
gofmt -w extract.go extract_test.go
go vet ./...
go build ./...                       # must compile; only `strconv` added (stdlib)

# Determinism grep (UPDATED for S3 — allow the set-building + copy ranges):
#   1. alias scan ranges a SLICE (queryAliases), NOT the args map x.
#   2. every `for k := range <map>` that affects a RESULT is immediately sorted:
grep -n 'range x\b' extract.go                       # MUST print nothing (never range the args map)
grep -n 'range queryAliases' extract.go              # MUST show the alias-scan loop (S2)
# show every map-range + the next line; each that affects a result must be sort.Strings:
grep -nA1 'for k := range\|for c := range\|for canon, aliases := range\|for k, v := range r.Optionals' extract.go
#   -> expect: in collect / firstReachableString / extractOptionals -> sort.Strings on
#      the next non-blank line. In optionalKeySet (set) and ToUpstreamArgs (copy) -> NOT
#      sorted, which is CORRECT (order-irrelevant).

# Confirm only `strconv` was added (no new third-party/stdlib imports beyond strconv):
git diff --stat extract.go
grep -n 'strconv' extract.go                         # the import + strconv.Itoa calls

# Confirm no config coupling:
grep -n 'Config\|cfg\.' extract.go                   # MUST print nothing

# Expected: zero errors; the three result-deciding map ranges are each sorted;
# optionalKeySet/ToUpstreamArgs ranges are unsorted (set/copy); only `strconv` added.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the extract + ToUpstreamArgs tests in isolation, verbose.
go test -run 'TestExtract|TestToUpstreamArgs' -v

# Full module suite (extract + all existing tests stay green; S2 rows unchanged).
go test ./...

# Expected: PASS. Every S3 row MUST match the validated table (research §6).
# Spot-check the load-bearing rows:
#   {"foo":"bar"}                                              -> {bar, "inferred:foo",                  F, nil, T}
#   {"a":"short","b":"longest string"}                         -> {longest string, "inferred:b",          T, nil, T}
#   {"messages":[{"role":"user","content":"rust async"}]}      -> {rust async, "inferred:messages[0].content", T, nil, T}
#   {"a":"","b":"real"}                                        -> {real, "inferred:b",                    F, nil, T}  # empty skipped, single
#   {"location":"France","description":"search rust"}          -> {search rust, "inferred:description",  F, {location:France}, T}
#   {"q":"rust","country":"France"}                            -> {rust, "q",                             F, {location:France}, T}
#   {"q":"rust","location":"US","country":"France"}            -> {rust, "q",                             F, {location:US},     T}  # canonical wins
#   {"q":"rust","junk":1,"country":"France"}                   -> {rust, "q",                             F, {location:France}, T}  # junk ignored
#   [{"q":"x","country":"FR"}]                                 -> {x, "array[0]",                         F, {location:FR},    T}  # array forwards optionals
#   {} / {"a":1} / {"location":"France"} / {"a":[1,2]} / null  -> {ExtractionResult{}}                                                      # failure
#   ToUpstreamArgs: {"q":"rust","junk":1,"country":"France"} -> {search_query:rust, location:France}   (no q/junk/country)
# Notes:
#  - Ambiguous is TRUE for every multi-candidate inference (even with a clear longest).
#  - Optionals is nil on failure and on no-optionals rows (S2 backward-compat).
#  - The 5 S2 failure rows ({}, {"query":null}, {"query":{}}, {"query":[1,2,3]},
#    {"query":{"text":42}}) STAY Found=false (no strings anywhere).
```

### Level 3: Integration Testing (System Validation)

```bash
# extract()/ToUpstreamArgs are PURE — the "integration" is the consumer contract +
# the array-recursion-through-object-optionals path, verified by a smoke test
# mirroring the real calls (P1.M5.T2 server + P1.M4 upstream):

cat > /tmp/extract_smoke_test.go <<'EOF'
package main
import ("encoding/json"; "reflect"; "testing")
func TestExtractSmoke_S3Full(t *testing.T) {
	cfg := DefaultConfig()
	cases := []struct {
		raw      string
		q, src   string
		amb      bool
		optNil   bool
		found    bool
		upstream map[string]any
	}{
		{`{"foo":"bar"}`, "bar", "inferred:foo", false, true, true, map[string]any{"search_query": "bar"}},
		{`{"messages":[{"role":"user","content":"rust async"}]}`, "rust async", "inferred:messages[0].content", true, true, true, map[string]any{"search_query": "rust async"}},
		{`{"q":"rust","junk":1,"country":"France"}`, "rust", "q", false, false, true, map[string]any{"search_query": "rust", "location": "France"}},
		{`[{"q":"x","country":"FR"}]`, "x", "array[0]", false, false, true, map[string]any{"search_query": "x", "location": "FR"}},
		{`{}`, "", "", false, true, false, nil}, // failure -> no upstream call (FR-6)
	}
	for _, c := range cases {
		got := extract(json.RawMessage(c.raw), cfg.QueryAliases, cfg.OptionalAliases)
		if got.Query != c.q || got.Source != c.src || got.Ambiguous != c.amb ||
			got.Found != c.found || (c.optNil && got.Optionals != nil) {
			t.Errorf("%s: got %+v want q=%q src=%q amb=%v optNil=%v found=%v",
				c.raw, got, c.q, c.src, c.amb, c.optNil, c.found)
		}
		if c.found && !reflect.DeepEqual(got.ToUpstreamArgs(cfg.TargetParam), c.upstream) {
			t.Errorf("%s: ToUpstreamArgs=%v want %v", c.raw, got.ToUpstreamArgs(cfg.TargetParam), c.upstream)
		}
		if !c.found && got.Optionals != nil {
			t.Errorf("%s: failure must have nil Optionals: %+v", c.raw, got)
		}
	}
}
EOF
cp /tmp/extract_smoke_test.go ./zz_smoke_test.go && go test -run TestExtractSmoke -v && rm ./zz_smoke_test.go

# Expected: smoke compiles (real DefaultConfig call shape + TargetParam) and passes;
# failure row has nil Optionals and would NOT call ToUpstreamArgs upstream (FR-6 gate).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Determinism stress: the load-bearing determinism guarantees (sorted inference
# collection; longest tie-break) must NEVER flake. Run the table many times.
go test -run TestExtract -count=100

# Targeted inference determinism: a multi-key object must always pick the longest
# string and the sorted-first path on a tie, never a random one.
cat > /tmp/det_stress.go <<'EOF'
package main
import ("encoding/json"; "testing")
func TestS3InferenceDeterminism(t *testing.T) {
	for i := 0; i < 1000; i++ {
		// longest wins deterministically
		g := extract(json.RawMessage(`{"zebra":"zz","apple":"aa","mango":"mmm"}`),
			DefaultConfig().QueryAliases, DefaultConfig().OptionalAliases)
		if g.Query != "mmm" || g.Source != "inferred:mango" || !g.Ambiguous {
			t.Fatalf("run %d: nondeterministic longest: %+v", i, g)
		}
		// tie -> sorted-first path deterministically
		t1 := extract(json.RawMessage(`{"b":"xx","a":"xx"}`),
			DefaultConfig().QueryAliases, DefaultConfig().OptionalAliases)
		if t1.Query != "xx" || t1.Source != "inferred:a" {
			t.Fatalf("run %d: nondeterministic tie: %+v", i, t1)
		}
		// chat-shaped path always inferred:messages[0].content
		m := extract(json.RawMessage(`{"messages":[{"role":"u","content":"rust async"}]}`),
			DefaultConfig().QueryAliases, DefaultConfig().OptionalAliases)
		if m.Source != "inferred:messages[0].content" {
			t.Fatalf("run %d: nondeterministic path: %+v", i, m)
		}
	}
}
EOF
cp /tmp/det_stress.go ./zz_det_test.go && go test -run TestS3InferenceDeterminism -v && rm ./zz_det_test.go

# §10.3 clean-payload invariant: ToUpstreamArgs NEVER contains a query-alias name or
# an unrecognized key, for a battery of inputs (grep the result keys).
cat > /tmp/clean_test.go <<'EOF'
package main
import ("encoding/json"; "testing")
func TestS3CleanPayloadInvariant(t *testing.T) {
	cfg := DefaultConfig()
	for _, raw := range []string{
		`{"q":"rust","junk":1,"country":"France","random":"x"}`,
		`{"foo":"bar","size":"big"}`,
		`{"messages":[{"content":"rust async"}],"recency":"day"}`,
	} {
		r := extract(json.RawMessage(raw), cfg.QueryAliases, cfg.OptionalAliases)
		if !r.Found { t.Fatalf("%s: expected Found", raw) }
		args := r.ToUpstreamArgs(cfg.TargetParam) // "search_query"
		for bad := range args {
			// allowed keys: targetParam + canonical optionals only
			switch bad {
			case cfg.TargetParam, "location", "content_size", "search_recency_filter":
			default:
				t.Errorf("%s: ToUpstreamArgs leaked key %q (args=%v)", raw, bad, args)
			}
		}
		if _, ok := args[cfg.TargetParam]; !ok {
			t.Errorf("%s: ToUpstreamArgs missing %q", raw, cfg.TargetParam)
		}
	}
}
EOF
cp /tmp/clean_test.go ./zz_clean_test.go && go test -run TestS3CleanPayloadInvariant -v && rm ./zz_clean_test.go

# Expected: count=100 table passes; 1000-run inference stress always picks longest /
# sorted-first path / messages[0].content; clean-payload invariant holds (only
# search_query + canonical optionals ever appear).
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0 (only stdlib `strconv` added; no new requires).
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l .` prints nothing (extract.go + extract_test.go formatted).
- [ ] `go test ./...` exits 0 (extract + all pre-existing tests pass; M1 + S1/S2 gate green).
- [ ] `go test -run 'TestExtract|TestToUpstreamArgs' -v` shows all rows PASS.

### Feature Validation

- [ ] Inference: `{"foo":"bar"}`→`inferred:foo`; `{"messages":[{"content":"x"}]}`→
      `inferred:messages[0].content`; `{"a":{"b":"x"}}`→`inferred:a.b`.
- [ ] Inference longest-wins + Ambiguous: multi-candidate → longest Query,
      `Ambiguous=true`; single candidate → `Ambiguous=false`; empty strings skipped.
- [ ] Inference excludes optional canonical names + aliases (and normalizes them).
- [ ] Optionals read SHALLOWLY (top-level only); canonical name first, then aliases in
      slice order; first present wins; stored under canonical name; nil when none.
- [ ] Optionals attach to EVERY object success return (alias-scan AND inference); nil
      on failure. Array-of-object inputs forward optionals via the unchanged []any case.
- [ ] `ToUpstreamArgs(targetParam)` returns EXACTLY `{targetParam: Query}` + optionals;
      no alias names, no unrecognized keys (clean-payload invariant test green).
- [ ] Failure (`{}`, `{"a":1}`, `{"location":"France"}`, `{"a":[1,2]}`, `null`,
      `{"a":""}`) → zero `ExtractionResult{}` (Optionals nil).
- [ ] The S2 boundary row `no_alias_string_s3boundary` is FLIPPED to Found=true
      (`inferred:foo`); the 5 S2 genuine-failure rows STAY Found=false.
- [ ] `go test -run TestExtract -count=100` never flakes; the 1000-run inference
      stress always picks longest / sorted-first path / messages[0].content.

### Code Quality Validation

- [ ] `ExtractionResult` fields + `extract`/`extractValue` signatures UNCHANGED from
      S1/S2; the ONLY new exported symbol is `ToUpstreamArgs`.
- [ ] extract.go adds ONLY the `strconv` import; no third-party/reflect/slices/maps.
- [ ] Mode-A doc comment on `extract` covers levels 1-5, optionals, failure, the §10.3
      guarantee, and determinism.
- [ ] Every result-deciding `for k := range <map>` is immediately followed by
      `sort.Strings`; optionalKeySet/ToUpstreamArgs ranges are set/copy (legal).

### Documentation & Deployment

- [ ] Doc comment is self-contained (reading only extract.go explains levels 1-5,
      inference path labels, optionals, failure, the clean payload, and determinism).
- [ ] No new env vars / config keys / routes (pure function + one method).
- [ ] No file other than extract.go + extract_test.go is modified.

---

## Anti-Patterns to Avoid

- ❌ Don't label inference `inferred:<bare-immediate-key>` for nested strings. The
  source is a dotted/bracket PATH (`inferred:messages[0].content`), per PRD §12.2's
  example. It only LOOKS like a bare key for shallow single-string objects. (research §1.)
- ❌ Don't collect empty strings during inference. An empty string is not a usable
  query; `Found==true ⟹ Query != ""`. Skip `v == ""`. `{"a":""}` → failure.
- ❌ Don't exclude query-alias keys from inference. Only OPTIONAL canonical names +
  aliases are excluded. A query-alias key with a string was caught by S2's drillIn; one
  without has no string to collect. (research §2.)
- ❌ Don't set `Ambiguous=false` for a clear-longest multi-candidate case. Ambiguous =
  `len(candidates) > 1`, period. The longest is still the query; the flag means "there
  were several reachable strings."
- ❌ Don't break longest-wins ties by ranging a map unsorted. Walk SORTED keys + INDEX
  array order and keep the FIRST max-length candidate. (external_deps.md §6.)
- ❌ Don't read optionals deeply. §10.2 is SHALLOW (top-level object only). A nested
  `country` is ignored.
- ❌ Don't return a non-nil empty map from `extractOptionals` when nothing is found.
  Return nil so S2's no-optionals rows keep `Optionals == nil` and pass DeepEqual.
- ❌ Don't attach optionals only to the inference return. They attach to EVERY object
  success return (alias-scan string/scalar/nested AND inference). On FAILURE, return
  the zero value (Optionals nil) — a present optional with no query still fails (FR-6).
- ❌ Don't validate/translate optional enum VALUES. Forward the raw value under the
  canonical name; if z.ai rejects it, the error surfaces honestly (§10.2 out of scope).
- ❌ Don't add a `Found` check inside `ToUpstreamArgs`. It is a pure transform; callers
  (M4/M5) gate on `Found` per FR-6.
- ❌ Don't touch the `[]any` case. It already forwards `r.Optionals`; S3 populating
  Optionals in the object case makes array-of-object optionals work with no change.
- ❌ Don't change `ExtractionResult` fields or the `extract`/`extractValue` signature.
  The ONLY new exported symbol is `ToUpstreamArgs`. S2/M3/M4/the server depend on the
  frozen contract.
- ❌ Don't add imports beyond `strconv`. No reflect/slices/maps/third-party.
- ❌ Don't touch any file other than extract.go + extract_test.go. The M1 test files
  (config_test/resolve_test/health_test) are owned by P1.M1.T3.S1.
- ❌ Don't leave the S2 boundary row `no_alias_string_s3boundary` as Found=false. S3
  inference now resolves `{"foo":"bar"}` to Found=true, `inferred:foo`. (The 5 genuine-
  failure S2 rows DO stay false.)
