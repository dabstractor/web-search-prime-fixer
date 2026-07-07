# Research — P1.M2.T1.S2 Rewrite edge cases + tests

Scope: determine exactly what S2 must add to `rewrite_test.go`, prove whether the
existing S1 `rewrite.go` already handles every PRD §19.1 edge case, and pin the
exact expected outputs. All claims verified against the ON-DISK files (rewrite.go,
rewrite_test.go, config_test.go) and PRD §10/§19.1/§3.

## 0. State of the world (S1 is DONE and on disk)

`rewrite.go` and `rewrite_test.go` ALREADY EXIST and match the S1 PRP contract
verbatim (read on-disk):

- `rewrite.go`: `RewriteResult{Changed bool; Notes []string}` +
  `Rewrite(args map[string]any, aliases []string, target string) RewriteResult`.
  5-step algorithm WITH the step-2 `seen`-map de-dup. Imports only `fmt`.
- `rewrite_test.go`: `TestRewrite_Table` (the 8 PRD §10 Examples rows) +
  `TestRewrite_InPlaceMutation`. Package-level `rwAliases` + `rwTarget` +
  `cloneMap(m)` shallow-copy helper. Imports `reflect` + `testing`.

**Implication: S2 is a TEST-EXTENSION task.** It ADDS test functions to the
existing `rewrite_test.go` and MODIFIES NOTHING ELSE (rewrite.go is frozen; Mode A
docs = none).

## 1. Gap analysis: what §19.1 / the work-item require vs. what S1 already covers

| §19.1 / item requirement                         | S1 status        | S2 action                  |
|--------------------------------------------------|------------------|----------------------------|
| every §10 Examples row                           | ✓ rows 1–8       | none                       |
| empty args `{}`                                  | ✓ row 8          | none (do NOT duplicate)    |
| args without any alias `{"foo":"bar"}`           | ✓ row 7          | none                       |
| **nil args**                                     | ✗ (row 8 is `{}`)| **ADD TestRewrite_NilArgs**|
| **alias list ordering** (`q` before `query`)     | ✗ (uses default) | **ADD TestRewrite_OrderingMatters** |
| **duplicate alias entries** `[query,query]`      | ✗ (rwAliases has none) | **ADD TestRewrite_DuplicateAliasDedup** |
| **non-string alias values** (number/object/array)| ✗ (all values `"x"`)| **ADD TestRewrite_NonStringValues** |

## 2. THE critical gotcha: cloneMap(nil) ≠ nil (must bypass the helper)

`rewrite_test.go`'s table runner does `args := cloneMap(tc.in)` before calling
Rewrite. `cloneMap` is:

```go
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m)) // len(nil)==0
	for k, v := range m { }             // ranging nil map = 0 iters (legal)
	return out                          // NON-nil empty map
}
```

So **`cloneMap(nil)` returns an empty `{}` map, not nil.** A naive nil row added
to `TestRewrite_Table` would silently test the EMPTY path, never exercising
rewrite.go's `if args == nil` guard. **The nil-args test MUST call `Rewrite`
directly with a nil map, bypassing cloneMap.** ⇒ dedicated function, not a table
row. Additionally: if the S1 nil guard were ever removed, writing to a nil map
PANICS at runtime — so the nil test both proves the guard fires and guards
against a regression that would crash the proxy.

## 3. The ordering + de-dup rows need CUSTOM aliases (not the table's rwAliases)

`TestRewrite_Table` hard-codes `rwAliases` (the default 5-element slice) and
`rwTarget`. Two S2 cases need a DIFFERENT aliases slice:

- **ordering**: `aliases := []string{"q", "query"}` (q BEFORE query — reversed).
- **de-dup**: `aliases := []string{"query", "query"}` (duplicate).

These cannot be rows in the current table. ⇒ dedicated functions, each declaring
its own local `aliases` slice (mirroring how config_test.go mixes table-driven
`TestLoadConfig_FromFile` with dedicated `TestLoadConfig_EmptyPath` /
`TestDefaultConfig`).

## 4. Proof: the EXISTING rewrite.go already passes every S2 edge case (zero algo change)

Trace of each new case against the on-disk algorithm:

**nil args:** step 1 `if args == nil ... return RewriteResult{}` → Changed=false,
Notes=nil, args stays nil. ✓ No write to nil map (would panic otherwise).

**ordering `aliases=[q,query]`, `args={q:"y",query:"x"}`:** step 2 ranges the
slice → present=[q,query] (both exist). step 3 non-empty. step 4 target absent.
step 5 chosen=present[0]=q; `args[search_query]=args[q]="y"`; delete q; rename
note(q); present[1:]=[query] → delete query, dropped note(query). Result:
`args={search_query:"y"}`, notes=[rename(q), dropped(query)]. ✓ **q promoted
because it is config-first** — proves order matters. Deterministic (slice walk,
never the map).

**de-dup `aliases=[query,query]`, `args={query:"x"}`:** step 2 `seen` map: 1st
query → seen, present=[query]; 2nd query → seen, skip. present=[query]. step 5
chosen=query; rename; present[1:]=[] → no dropped note. Result:
`args={search_query:"x"}`, notes=[rename(query)] — **single note, no double, no
spurious drop.** ✓

**de-dup + target present `aliases=[query,query]`,
`args={query:"x",search_query:"y"}`:** present=[query] (deduped). step 4 target
present → delete query, ignored note(query) ONCE. Result: `args={search_query:
"y"}`, notes=[ignored(query)]. ✓ (second sub-case to add.)

**non-string `{query:123}` / `{query:map[string]any{"k":1}}` /
`{query:[]any{1,2}}`:** step 2 `_, ok := args["query"]` is true regardless of
value type. step 5 `args[target]=args[chosen]` copies the `any` value verbatim
(never inspects/coerces). Result: value carried to `search_query` unchanged.
notes=[rename(query)]. ✓ PRD §3 honored (never normalize/truncate).

**Conclusion: rewrite.go needs ZERO changes.** S2 only adds tests. If any new
test fails, it reveals an S1 regression and the fix is minimal + localized to
rewrite.go's guard/de-dup — but per this trace none is expected.

## 5. Exact expected outputs (the want* values for each new test)

```
TestRewrite_NilArgs:
  in: var args map[string]any  // nil (bypass cloneMap)
  want: Changed=false, Notes=nil, args==nil (still nil)

TestRewrite_OrderingMatters:
  aliases=[q,query], args={q:"y",query:"x"}
  wantArgs  = {search_query:"y"}
  wantNotes = [`"q" is not a valid parameter; renamed to "search_query"`,
              `dropped redundant "query"`]
  wantChange = true

TestRewrite_DuplicateAliasDedup (two sub-cases):
  (a) aliases=[query,query], args={query:"x"}
      wantArgs  = {search_query:"x"}
      wantNotes = [`"query" is not a valid parameter; renamed to "search_query"`]
      wantChange = true
  (b) aliases=[query,query], args={query:"x",search_query:"y"}
      wantArgs  = {search_query:"y"}
      wantNotes = [`ignored "query" (use only "search_query")`]
      wantChange = true

TestRewrite_NonStringValues (subtests number/object/array):
  {query: 123}                      -> {search_query: 123}            , 1 rename note
  {query: map[string]any{"k":1}}    -> {search_query: {"k":1}}        , 1 rename note
  {query: []any{1,2}}               -> {search_query: [1,2]}          , 1 rename note
```

Note strings use the ALGORITHM-literal wording (S1 research/notes-and-contract.md
§1), so they round-trip through PRD §12.3 warningText unchanged.

## 6. reflect.DeepEqual correctness for non-string values

`cloneMap` shallow-copies top-level keys (`out[k]=v`); nested object/array values
are shared by reference. This is SAFE here because Rewrite only moves the
top-level key (`args[target]=args[chosen]`) and never mutates nested values. So
`reflect.DeepEqual(args, wantArgs)` is true for number/object/array (DeepEqual
compares deeply; the shared reference trivially equals itself). No aliasing bug.

## 7. Test conventions to follow (pinned from config_test.go / rewrite_test.go)

- `package main`, top of `rewrite_test.go` (already imports `reflect`+`testing`).
- Dedicated funcs named `TestRewrite_<Concern>` (mirrors `TestRewrite_Table` /
  `TestRewrite_InPlaceMutation` already present).
- `reflect.DeepEqual` for map + slice comparison (maps are not `==`-comparable).
- `t.Run(name, ...)` subtests for the multi-case funcs (de-dup, non-string).
- Reuse the existing `rwTarget` constant; declare a LOCAL `aliases` slice per
  ordering/de-dup func (do NOT mutate the package-level `rwAliases`).

## 8. Validation commands (Go, verified on this toolchain)

```bash
gofmt -l rewrite_test.go rewrite.go     # must print nothing
go vet ./...                            # must exit 0
go test -run TestRewrite -v             # all funcs, incl. the 4 new ones
go test -run TestRewrite -count=100     # determinism (ordering never flakes)
go test ./...                           # whole module still green
```

No new dependencies; `go.mod` gains zero `require`s.
