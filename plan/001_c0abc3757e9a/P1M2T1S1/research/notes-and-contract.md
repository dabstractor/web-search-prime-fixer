# Research — P1.M2.T1.S1 RewriteResult + Rewrite algorithm

Scope: verify the exact contracts the S1 implementer must satisfy, and resolve
the two cross-task ambiguity points (note-string wording, de-dup ownership) so
the PRP is unambiguous. All claims are pinned to on-disk sources.

## 1. Note strings: the Examples TABLE is SHORTHAND; the ALGORITHM is literal

**This is the single most important gotcha.** PRD §10 has two conflicting
representations of "the notes":

- The **Algorithm** (§10 steps 4–5) gives the **literal** note strings the code
  must emit:
  - rename: `"<chosen>" is not a valid parameter; renamed to "<target>"`
  - drop:   `dropped redundant "<alias>"`
  - ignore: `ignored "<alias>" (use only "<target>")`
- The **Examples table** (§10 Examples) shows a **human-readable shorthand**:
  `renamed "query"`, `dropped "q"`, `ignored "query"`.

**Decision: `RewriteResult.Notes` MUST contain the literal ALGORITHM strings.**
Rationale, pinned to PRD §12.3 ("Notes are joined into the warning text"):

```
[web-search-prime-fixer] <note[0]>; <note[1]>; ... . Use "search_query" in future calls.
```

warningText (P1.M3.T3.S1) joins the Notes entries verbatim with `"; "`. The §12.3
example for `{"query":"x"}` is:

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". ...
```

That is the ALGORITHM string, NOT the table shorthand (`renamed "query"`). So the
table shorthand cannot round-trip through warningText. **Use the algorithm
strings.** This is also why PRD §19.1 says "expected notes **substrings**" rather
than exact table strings: the test should assert substrings of the algorithm
strings (e.g. `renamed to "search_query"`, `dropped redundant "q"`,
`ignored "query"`).

## 2. De-dup ownership: S1 implements the de-dup; S2 adds the dedicated test row

The S1 work-item contract step 2 states verbatim:

> present = aliases (config order) that exist as keys in args, DE-DUPED
> (PRD §19.1: duplicate alias entries produce no double note).

So **the de-dup logic belongs to the S1 algorithm**, not S2. Concrete failure
mode it prevents: with `aliases=["query","query"]` and `args={"query":"x"}`,
WITHOUT de-dup `present=["query","query"]` → step 5 promotes `query`, deletes it,
notes the rename, THEN walks `present[1:]=["query"]`, deletes `query` AGAIN
(no-op) and appends a spurious `dropped redundant "query"` → double note + wrong
semantics. WITH de-dup `present=["query"]` → clean single rename.

S2 ("Rewrite edge cases + tests (de-dup, …)") adds the **dedicated duplicate-
config test row** that exercises this path; the algorithm that makes it pass is
shipped here in S1. No conflict — S1 is the superset.

## 3. Determinism: iterate the aliases SLICE, never the args map

From `architecture/external_deps.md §6` (on-disk, go1.26.4 knowledge):

> Iterating a `map[K]V` yields keys in **randomized** order … the implementer must
> **never** range over the args map to pick an alias.

`present` is built by ranging `for _, a := range aliases` (the ordered slice) and
testing `_, ok := args[a]`. The args map is read (existence test + value copy +
delete) but NEVER iterated. This is the sole source of "first alias promoted"
determinism and is a correctness requirement, not style.

## 4. Consumer seam contract (downstream subtasks depend on these EXACTLY)

`Rewrite` is a pure-ish helper (mutates args in place, deterministic output).
Downstream consumers (must not change signature/fields):

- **P1.M4.T1.S1** (proxy request handling, PRD §11.1): for a `tools/call` request,
  `args := params.arguments`, `res := Rewrite(args, cfg.Aliases, cfg.TargetParam)`;
  if `res.Changed`, re-serialize the object with the mutated `params.arguments`
  and stash the request `id` for response correlation; else forward original bytes.
- **P1.M3.T3.S1** (warningText): `warningText(res.Notes []string)` joins the
  literal notes with `"; "` per §12.3. Only invoked when `Changed==true`.

Contract invariants the consumers rely on (document on the Rewrite doc comment):
- `Changed == true` ⟹ `len(Notes) >= 1` (every change produces ≥1 note).
- `Changed == false` ⟹ `Notes == nil` AND args is byte-for-byte untouched
  (zero-value `RewriteResult{}`; proxy uses this to skip re-serialization).
- Non-alias keys are NEVER deleted/moved/renamed (PRD §3 non-goal).

## 5. Imports: `fmt` is the only one needed

Algorithm needs: map existence test, map delete, slice append, string format.
`%q` in `fmt.Sprintf` renders `"query"`-style quotes for the notes (clean,
idiomatic). `reflect`/`slices` are NOT required (the contract's "possibly
reflect/slices" hedge is unnecessary for this implementation). Alternative:
literal `` `"`+a+`"` `` concatenation to keep zero imports — equivalent for ASCII
alias names; `%q` chosen for readability + safe quoting. See PRP gotchas.

## 6. Test conventions (pinned from on-disk files)

- Table-driven, `package main`, file `rewrite_test.go` — matches
  `config_test.go` / `resolve_test.go` style: `tests := []struct{...}{...}` over
  `for _, tc := range tests { t.Run(tc.name, ...) }`.
- Map comparison via `reflect.DeepEqual` (maps are not `==`-comparable; DeepEqual
  is order-independent, exactly what unordered args needs).
- Clone input args per row (maps are reference types) — small `cloneMap` helper.
- Define `aliases`/`target` locally in the test (do NOT couple to
  `DefaultConfig()`) so the rewrite unit stays decoupled from config defaults;
  values mirror the defaults for §10 fidelity.
