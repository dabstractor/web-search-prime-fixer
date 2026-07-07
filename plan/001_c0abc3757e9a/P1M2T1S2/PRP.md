name: "P1.M2.T1.S2 — Rewrite edge cases + tests (de-dup, ordering, non-string values, nil/empty)"
description: |

  Extend the EXISTING `rewrite_test.go` (produced by P1.M2.T1.S1, already on
  disk) with the PRD §19.1 edge-case tests that the 8-row core table does NOT
  cover: **nil args**, **alias-list ordering** (custom `[q,query]` promotes `q`),
  **duplicate alias entries in config** (`[query,query]` de-duped → single note),
  and **non-string alias values** (number/object/array carried through to
  `search_query` unchanged). Verify the S1 `rewrite.go` algorithm passes every
  case (analysis says it already does — zero algorithm change expected), then
  freeze `Rewrite`/`RewriteResult` for P1.M3/P1.M4. Test-only: `package main`,
  stdlib `reflect`+`testing` only. Mode A docs (none).

---

## Goal

**Feature Goal**: Close the PRD §19.1 test-coverage gap. S1 shipped the 8 §10
Examples rows; S2 adds the §19.1 edge cases those rows omit (nil, ordering,
de-dup, non-string values). Every edge case must pass against the **existing**
`rewrite.go`, proving the algorithm is correct and deterministic under all the
boundary conditions the PRD enumerates, so `Rewrite`/`RewriteResult` can be
frozen as a fixed seam for P1.M3 (warningText) and P1.M4 (proxy).

**Deliverable**: ONE MODIFIED file — `rewrite_test.go` — gaining four NEW test
functions (`TestRewrite_NilArgs`, `TestRewrite_OrderingMatters`,
`TestRewrite_DuplicateAliasDedup`, `TestRewrite_NonStringValues`). `rewrite.go`
is **verified-then-frozen** (no change expected; see "Proof" below). Nothing else
is touched (Mode A docs = none; no README/config/source changes).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. `go test -run TestRewrite -v` shows the original 8
rows + in-place test + the 4 new edge-case functions ALL PASS.
`go test -run TestRewrite -count=100` never flakes (determinism).
`RewriteResult`/`Rewrite` signatures are byte-for-byte unchanged from S1 (frozen).
`go.mod` gains zero `require`s.

## User Persona

**Target User**: (1) downstream implementers — P1.M4.T1.S1 (proxy) and
P1.M3.T3.S1 (warningText) consume `Rewrite`/`RewriteResult` as a frozen seam;
S2's tests are the guarantee that the seam is correct at its boundaries, so they
can build on it without re-auditing the algorithm. (2) the maintainer, who gets
a regression net that pins nil-safety, config-order determinism, de-dup, and
value-passthrough (PRD §3) — the four things most likely to silently break later.

**Use Case**: A future change to `rewrite.go` (e.g. someone "optimizes" the
present-builder by ranging the args map, or drops the nil guard) is caught
immediately by `TestRewrite_OrderingMatters` (nondeterministic promotion) or
`TestRewrite_NilArgs` (nil-map write panic), instead of shipping a
nondeterministic/crashing proxy.

**User Journey**: edge-case input → new dedicated test func → calls
`Rewrite(...)` directly (nil/nil-bypass) or with a custom aliases slice → asserts
mutated args + Notes + Changed against the exact expected values (research §5) →
green. Existing 8 rows untouched.

**Pain Points Addressed**: (1) S1's table cannot express nil (cloneMap turns nil
into `{}`) or custom alias order (table hard-codes `rwAliases`) — dedicated funcs
fix both. (2) Without the de-dup test, a future regression that re-introduces a
double note is invisible. (3) Without the non-string test, a future "let's also
stringify the value" change violates PRD §3 silently.

## Why

- Completes **PRD §19.1** (rewrite_test.go must cover, beyond the §10 table:
  nil/empty args, no-alias, **alias-list ordering**, **duplicate alias entries**,
  **non-string alias values carried through as-is**). S1 covered empty/no-alias
  via rows 7–8; S1 covered the §10 table; S2 covers the remaining four.
- Enforces **PRD §3** (non-goals: never normalize/truncate/move non-alias values)
  via the non-string-values test — the only place value-passthrough is asserted.
- Enforces **architecture/external_deps.md §6** (map iteration randomized) via
  the ordering test: `aliases=[q,query]` MUST promote `q`, deterministically, 100
  runs in a row — a future `range args` regression flips this and flakes.
- **Freezes the consumer seam.** P1.M3.T3.S1 (warningText joins `Notes` verbatim)
  and P1.M4.T1.S1 (proxy re-serializes on `Changed`) depend on the EXACT
  signatures/fields/note-wording. S2's green suite is the freeze certificate.

## What

ADD four test functions to the existing `rewrite_test.go` (`package main`). Each
is self-contained and deterministic; **no mocking** (pure function). They reuse
the file's existing `rwTarget` constant and `reflect`/`testing` imports, and add a
LOCAL `aliases` slice where the case needs a non-default order/duplicates.

1. **`TestRewrite_NilArgs`** — pass a nil `map[string]any` DIRECTLY (bypass
   `cloneMap`, which turns nil into `{}`). Assert `Changed==false`, `Notes==nil`,
   and the map is still nil (the S1 guard must fire before any write to a nil
   map, which would otherwise PANIC).
2. **`TestRewrite_OrderingMatters`** — `aliases := []string{"q","query"}`,
   `args={"q":"y","query":"x"}`. Assert `q` (config-first) is promoted:
   `args={"search_query":"y"}`, notes=[rename(q), dropped(query)], `Changed==true`.
3. **`TestRewrite_DuplicateAliasDedup`** — two `t.Run` sub-cases:
   (a) `aliases=[query,query]`, `args={query:"x"}` → single rename note, no
   spurious drop; (b) `aliases=[query,query]`, `args={query:"x",search_query:"y"}`
   → single ignored note (de-dup holds in the target-present path too).
4. **`TestRewrite_NonStringValues`** — three `t.Run` sub-cases (number / object /
   array): `args={"query":<val>}` → `args={"search_query":<same val>}`, one rename
   note, `Changed==true`. Proves Rewrite moves keys without inspecting/coercing
   values (PRD §3).

### Success Criteria

- [ ] `rewrite_test.go` contains the 4 new functions above and the S1
      `TestRewrite_Table` (8 rows) + `TestRewrite_InPlaceMutation` are PRESERVED
      unchanged.
- [ ] `TestRewrite_NilArgs` passes a nil map DIRECTLY (not via `cloneMap`).
- [ ] `TestRewrite_OrderingMatters` uses a LOCAL `aliases=[q,query]` (not
      `rwAliases`) and asserts `q` is promoted.
- [ ] `TestRewrite_DuplicateAliasDedup` covers BOTH the target-absent (single
      rename) and target-present (single ignored) de-dup paths.
- [ ] `TestRewrite_NonStringValues` covers number, object, AND array values.
- [ ] `rewrite.go` is UNCHANGED in signature/fields/algorithm (frozen); if a test
      fails, the fix is a minimal localized correction to rewrite.go (per the
      Proof below, NO change is expected).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `go test -run TestRewrite -count=100` never flakes.

## All Needed Context

### Context Completeness Check

_Pass._ The deliverable is fully pinned: (a) the two on-disk files
(`rewrite.go`, `rewrite_test.go`) define the exact function under test, the
existing test helpers (`cloneMap`, `rwAliases`, `rwTarget`), and the project test
conventions; (b) PRD §19.1 enumerates the exact edge cases; (c) the S1 research
(`notes-and-contract.md` §1) pins the literal note strings; (d) this PRP's
research (`edge-case-analysis.md` §4–§5) traces each new case through the actual
on-disk algorithm and lists the exact expected `wantArgs`/`wantNotes`/`Changed`.
The one non-obvious trap — `cloneMap(nil)` returns `{}`, so the nil test must
bypass it — is flagged in §"Known Gotchas". An agent with no prior knowledge of
this codebase can implement this from the PRP + the two on-disk files alone.

### Documentation & References

```yaml
# MUST READ — the function under test and the file being extended.
- file: rewrite.go
  why: the EXACT algorithm S2's tests exercise. Step 1 is the nil guard; step 2 is
        the seen-map de-dup; step 5 is the value-agnostic move
        (`args[target]=args[chosen]`). S2 must NOT change this file (frozen).
  pattern: the 5-step algorithm + doc comment (already documents de-dup + §3).
  gotcha: writing to a nil map PANICS — the step-1 `if args == nil` guard is what
        prevents it; TestRewrite_NilArgs both proves it fires and guards the
        regression.

- file: rewrite_test.go
  why: the file S2 EXTENDS. Reuse `rwTarget`, `cloneMap`, `reflect`/`testing`
        imports, the `TestRewrite_*` naming, and the `t.Run` subtest style.
  pattern: table-driven `TestRewrite_Table` (do NOT modify) + dedicated
        `TestRewrite_InPlaceMutation` (the style the 4 new funcs mirror).
  gotcha: cloneMap(nil) returns an EMPTY map, not nil — so the nil case CANNOT be
        a table row and CANNOT go through cloneMap. See Known Gotchas.

- file: PRD.md
  section: "§19.1 rewrite_test.go (table-driven)"
  why: enumerates the EXACT edge cases S2 must add (nil/empty, no-alias, alias
        ordering, duplicate entries de-duped, non-string values as-is).
  critical: §19.1 says expected notes are SUBSTRINGS of the algorithm strings —
        but S2 asserts full DeepEqual on Notes (S1 already does; keep consistent).

- file: PRD.md
  section: "§10 The rewrite rule (rewrite.go)" + "§3 Non-goals"
  why: §10 is the authoritative algorithm + the literal note strings (rename /
        dropped / ignored); §3 is why non-string values MUST pass through
        untouched (never normalize/truncate).
  critical: the §10 Examples-table "Notes" column is SHORTHAND; emit the
        ALGORITHM-literal strings (S1 research §1). S2's wantNotes use the literal
        strings, same as S1's rows.

- file: config_test.go
  why: the project's test-layout convention to match: `package main`, dedicated
        funcs alongside table-driven ones (TestDefaultConfig, TestLoadConfig_EmptyPath
        are dedicated; TestLoadConfig_FromFile is table-driven). Confirms mixing
        dedicated funcs with the existing table is idiomatic here.
  pattern: `reflect.DeepEqual`, `t.Run` subtests, `t.Helper()`/`t.TempDir()` as
        needed (none needed for these pure-func tests).

- docfile: plan/001_c0abc3757e9a/P1M2T1S2/research/edge-case-analysis.md
  why: the gap analysis (§1), the cloneMap-nil proof (§2), the custom-aliases
        reasoning (§3), the algorithm-trace proving rewrite.go needs ZERO changes
        (§4), and the EXACT expected want*/Changed for every new test (§5). This
        is the implementation cheat-sheet.

# CONTRACTS (read-only — do not break):
- file: plan/001_c0abc3757e9a/P1M2T1S1/PRP.md
  why: defines Rewrite/RewriteResult exactly as shipped on disk. S2 FREEZES them;
        P1.M4.T1.S1 (.Changed) and P1.M3.T3.S1 (.Notes) consume them unchanged.

- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§6. Go map ordering is non-deterministic"
  why: PROVES why TestRewrite_OrderingMatters matters — a `range args` regression
        would make `q`-vs-`query` promotion nondeterministic and flaky.
```

### Current Codebase tree (run `tree` / `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package comment (package main)
  main.go           # bootstrap (P1.M1.T4) — UNTOUCHED by S2
  config.go         # Config{...,Aliases,TargetParam,...} — UNTOUCHED
  config_test.go    # test-convention reference — UNTOUCHED
  resolve_test.go   # test-convention reference — UNTOUCHED
  logger_test.go    # UNTOUCHED
  health_test.go    # UNTOUCHED
  proxy.go          # passthrough forward core (P1.M1.T4.S2) — UNTOUCHED
  proxy_test.go     # UNTOUCHED
  rewrite.go        # S1 — RewriteResult + Rewrite (5-step, de-duped). FROZEN by S2.
  rewrite_test.go   # S1 — TestRewrite_Table (8 rows) + InPlaceMutation. <- S2 EXTENDS THIS.
  testdata/.gitkeep
  PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
rewrite_test.go   # MODIFIED (not new). +4 funcs: TestRewrite_NilArgs,
                  #      TestRewrite_OrderingMatters, TestRewrite_DuplicateAliasDedup,
                  #      TestRewrite_NonStringValues. Existing 8 rows + InPlaceMutation
                  #      preserved verbatim. No new imports (reflect+testing already in).
rewrite.go        # UNCHANGED (frozen). Only edited if a new test reveals an S1 bug
                  #      (none expected per research §4); any edit is minimal + localized.
# NO other files created or modified. No README, no config.example.json (Mode A docs=none).
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: cloneMap(nil) returns an EMPTY map, NOT nil:
//   func cloneMap(m map[string]any) map[string]any {
//       out := make(map[string]any, len(m))   // len(nil)==0
//       for k, v := range m { }               // ranging nil map = 0 iters (legal)
//       return out                            // non-nil empty map
//   }
// So a nil row added to TestRewrite_Table would silently test the EMPTY path and
// NEVER exercise rewrite.go's `if args == nil` guard. The nil test MUST call
// Rewrite DIRECTLY with a nil map, bypassing cloneMap. (research §2)

// CRITICAL: the step-1 nil guard is not just a correctness nicety — writing to a
// nil map PANICS at runtime. If a future change removed the guard, Rewrite would
// crash the proxy on a nil-arguments tools/call. TestRewrite_NilArgs locks it in.

// CRITICAL: TestRewrite_OrderingMatters + TestRewrite_DuplicateAliasDedup need a
// CUSTOM aliases slice ([q,query] / [query,query]). The existing TestRewrite_Table
// hard-codes rwAliases, so these CANNOT be plain table rows — use dedicated funcs
// that declare a LOCAL `aliases := []string{...}`. Do NOT mutate the package-level
// rwAliases (other tests depend on it).

// GOTCHA: note strings use the ALGORITHM-literal wording (S1 research §1), NOT the
// §10 Examples-table shorthand. reuse the exact forms already in rewrite_test.go:
//   rename:  `"<chosen>" is not a valid parameter; renamed to "search_query"`
//   drop:    `dropped redundant "<alias>"`
//   ignore:  `ignored "<alias>" (use only "search_query")`
// (S1's rows already encode these; copy the wording verbatim.)

// GOTCHA: cloneMap is a SHALLOW copy. For the non-string object/array cases this is
// SAFE: Rewrite only moves the top-level key (args[target]=args[chosen]) and never
// mutates nested values, so reflect.DeepEqual(args, wantArgs) is true. Do NOT add
// a deep-clone helper for these tests.

// GOTCHA: JSON value types beyond the item's three — bool, float, nil-value — are
// OUT OF SCOPE for S2 (the item lists number/object/array only). Stick to those
// three to keep the test set scoped to the contract. (A nil VALUE {query:nil} is a
// separate semantic decision not enumerated by §19.1; do not add it.)

// GOTCHA: rewrite.go MUST NOT change signature/fields. P1.M3/P1.M4 depend on
// RewriteResult{Changed bool; Notes []string} and
// Rewrite(args map[string]any, aliases []string, target string) RewriteResult.
```

## Implementation Blueprint

### Data models and structure

No data models are introduced or changed. `RewriteResult`/`Rewrite` (S1) are
frozen. The only "structure" is the four new test functions, which use the
existing `map[string]any` / `[]string` / `RewriteResult` types and the existing
`rwTarget` constant.

### Proof: the EXISTING rewrite.go passes every S2 case (zero algo change)

Traced against the on-disk algorithm (research `edge-case-analysis.md` §4):

- **nil**: step 1 `if args == nil` → returns zero-value result; no write (would
  panic otherwise). ✓
- **ordering `[q,query]` `{q:"y",query:"x"}`**: step 2 builds present=[q,query]
  (slice order); step 5 chosen=q; `search_query`←"y"; delete q; rename(q);
  present[1:]=[query] → delete query, dropped(query). ✓ q promoted (config-first).
- **de-dup `[query,query]` `{query:"x"}`**: step 2 `seen` skips 2nd query;
  present=[query]; step 5 single rename, no dropped note. ✓
- **non-string `{query:123}` etc.**: step 2 existence check true; step 5
  `args[target]=args[chosen]` copies the `any` verbatim. ✓

**Therefore S2 is test-only.** If a new test fails, it is an S1 regression; fix it
minimally in rewrite.go's guard or present-builder — but per the trace above, no
fix is expected. Do NOT "improve" the algorithm.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: READ rewrite.go and rewrite_test.go (confirm S1 state on disk)
  - VERIFY: RewriteResult{Changed bool; Notes []string} + the 5-step Rewrite with
        the step-2 seen-map de-dup exist exactly as the S1 PRP specifies.
  - VERIFY: rewrite_test.go has TestRewrite_Table (8 rows) + TestRewrite_InPlaceMutation,
        package-level rwAliases/rwTarget, cloneMap helper, imports reflect+testing.
  - DO NOT modify any of these; they are the frozen baseline S2 extends.

Task 2: MODIFY rewrite_test.go — ADD TestRewrite_NilArgs
  - IMPLEMENT: a dedicated func that declares `var args map[string]any` (nil) and
        calls `Rewrite(args, rwAliases, rwTarget)` DIRECTLY (do NOT cloneMap).
  - ASSERT: got.Changed == false; got.Notes == nil; args == nil (still nil — proves
        the guard fired before any map write, which would otherwise PANIC).
  - WHY DEDICATED: cloneMap(nil) returns {} (research §2), so a table row would
        never exercise the nil guard. This func bypasses cloneMap by design.
  - NAMING: TestRewrite_NilArgs. PLACEMENT: after TestRewrite_InPlaceMutation.

Task 3: MODIFY rewrite_test.go — ADD TestRewrite_OrderingMatters
  - IMPLEMENT: `aliases := []string{"q", "query"}` (q BEFORE query — reversed from
        the default config order); `args := map[string]any{"q":"y","query":"x"}`;
        `got := Rewrite(args, aliases, rwTarget)`.
  - ASSERT (reflect.DeepEqual): args == {"search_query":"y"} (q's value promoted);
        got.Notes == [`"q" is not a valid parameter; renamed to "search_query"`,
        `dropped redundant "query"`]; got.Changed == true.
  - WHY DEDICATED: needs a custom aliases slice (table hard-codes rwAliases).
        Proves config ORDER decides promotion (external_deps.md §6).
  - NAMING: TestRewrite_OrderingMatters. PLACEMENT: after TestRewrite_NilArgs.

Task 4: MODIFY rewrite_test.go — ADD TestRewrite_DuplicateAliasDedup
  - IMPLEMENT: two t.Run sub-cases, both with `aliases := []string{"query","query"}`:
      (a) "target_absent_single_rename": args={query:"x"} → wantArgs={search_query:"x"},
          wantNotes=[rename(query)], Changed=true.
      (b) "target_present_single_ignore": args={query:"x",search_query:"y"} →
          wantArgs={search_query:"y"}, wantNotes=[ignored(query)], Changed=true.
  - ASSERT each via reflect.DeepEqual; the POINT is exactly ONE note in each case
        (de-dup prevents a double note / spurious drop).
  - WHY DEDICATED: needs a duplicate aliases slice. Covers BOTH the step-5
        (target-absent) and step-4 (target-present) de-dup paths.
  - NAMING: TestRewrite_DuplicateAliasDedup; subtests target_absent_single_rename /
        target_present_single_ignore. PLACEMENT: after TestRewrite_OrderingMatters.

Task 5: MODIFY rewrite_test.go — ADD TestRewrite_NonStringValues
  - IMPLEMENT: a small table `cases := []struct{name string; val any}` with:
      {"number", 123},
      {"object", map[string]any{"k": 1}},
      {"array",  []any{1, 2}},
    loop with t.Run; per case: args={query:val}; got:=Rewrite(args, rwAliases,
    rwTarget); assert reflect.DeepEqual(args, {search_query:val}); assert
    len(got.Notes)==1 && got.Changed (a single rename note; value type is
    irrelevant to the note).
  - WHY: pins PRD §3 (Rewrite moves keys, never inspects/coerces values). The
        value carried to search_query must be byte-for-byte the input value.
  - SCOPE: only number/object/array (the item's three). Do NOT add bool/float/nil-
        value (out of scope; research §7).
  - NAMING: TestRewrite_NonStringValues; subtests number/object/array.
  - PLACEMENT: last in the file.

Task 6: VALIDATE + FREEZE
  - RUN: gofmt -w rewrite_test.go; go vet ./...; go test -run TestRewrite -v;
        go test -run TestRewrite -count=100; go test ./...; go build ./...
  - CONFIRM: rewrite.go is UNCHANGED (git diff rewrite.go is empty unless a test
        revealed an S1 bug — none expected). If unchanged, Rewrite/RewriteResult
        are FROZEN for P1.M3/P1.M4.
  - CONFIRM: go.mod gains zero requires.
```

### Implementation Patterns & Key Details

```go
// ---- TestRewrite_NilArgs: bypass cloneMap (cloneMap(nil) == {}). ----
func TestRewrite_NilArgs(t *testing.T) {
	var args map[string]any // nil — do NOT cloneMap (it would turn this into {})
	got := Rewrite(args, rwAliases, rwTarget)
	if got.Changed {
		t.Errorf("Changed = true, want false for nil args")
	}
	if got.Notes != nil {
		t.Errorf("Notes = %#v, want nil for nil args", got.Notes)
	}
	if args != nil {
		t.Errorf("args = %#v, want nil (guard must fire before any nil-map write)", args)
	}
}

// ---- TestRewrite_OrderingMatters: custom aliases, q before query. ----
func TestRewrite_OrderingMatters(t *testing.T) {
	aliases := []string{"q", "query"} // reversed from the default config order
	args := map[string]any{"q": "y", "query": "x"}
	got := Rewrite(args, aliases, rwTarget)
	if !got.Changed {
		t.Fatal("Changed = false, want true")
	}
	wantArgs := map[string]any{"search_query": "y"} // q's value promoted
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %#v, want %#v (q promoted, NOT query)", args, wantArgs)
	}
	wantNotes := []string{
		`"q" is not a valid parameter; renamed to "search_query"`,
		`dropped redundant "query"`,
	}
	if !reflect.DeepEqual(got.Notes, wantNotes) {
		t.Errorf("Notes = %#v, want %#v", got.Notes, wantNotes)
	}
}

// ---- TestRewrite_DuplicateAliasDedup: both de-dup paths. ----
func TestRewrite_DuplicateAliasDedup(t *testing.T) {
	aliases := []string{"query", "query"}
	t.Run("target_absent_single_rename", func(t *testing.T) {
		args := map[string]any{"query": "x"}
		got := Rewrite(args, aliases, rwTarget)
		if !got.Changed {
			t.Fatal("Changed = false, want true")
		}
		wantArgs := map[string]any{"search_query": "x"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %#v, want %#v", args, wantArgs)
		}
		wantNotes := []string{`"query" is not a valid parameter; renamed to "search_query"`}
		if !reflect.DeepEqual(got.Notes, wantNotes) {
			t.Errorf("Notes = %#v, want exactly one rename note (no double/drop): %#v", got.Notes, wantNotes)
		}
	})
	t.Run("target_present_single_ignore", func(t *testing.T) {
		args := map[string]any{"query": "x", "search_query": "y"}
		got := Rewrite(args, aliases, rwTarget)
		if !got.Changed {
			t.Fatal("Changed = false, want true")
		}
		wantArgs := map[string]any{"search_query": "y"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %#v, want %#v", args, wantArgs)
		}
		wantNotes := []string{`ignored "query" (use only "search_query")`}
		if !reflect.DeepEqual(got.Notes, wantNotes) {
			t.Errorf("Notes = %#v, want exactly one ignored note: %#v", got.Notes, wantNotes)
		}
	})
}

// ---- TestRewrite_NonStringValues: number/object/array carried as-is (PRD §3). ----
func TestRewrite_NonStringValues(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"number", 123},
		{"object", map[string]any{"k": 1}},
		{"array", []any{1, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := map[string]any{"query": c.val}
			got := Rewrite(args, rwAliases, rwTarget)
			if !got.Changed {
				t.Fatal("Changed = false, want true")
			}
			wantArgs := map[string]any{"search_query": c.val}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Errorf("args = %#v, want search_query to carry the value as-is: %#v", args, wantArgs)
			}
			if len(got.Notes) != 1 {
				t.Errorf("Notes = %#v, want exactly one rename note", got.Notes)
			}
		})
	}
}
```

### Integration Points

```yaml
FILES MODIFIED:
  - rewrite_test.go ONLY (ADD 4 funcs; preserve the existing 8 rows + InPlaceMutation).
  - rewrite.go: UNCHANGED (frozen). Edit ONLY if a new test reveals an S1 bug; the
    fix must keep the signature/fields/algorithm-literal note strings intact
    (P1.M3/P1.M4 depend on them). Per research §4, NO edit is expected.

NO OTHER FILES TOUCHED:
  - main.go / proxy.go / proxy_test.go / config*.go / logger_test.go / health_test.go:
    untouched. No README, no config.example.json, no doc.go change (Mode A docs=none).

CONSUMER SEAMS (frozen by this subtask's green suite):
  - P1.M4.T1.S1: res := Rewrite(params.Arguments, cfg.Aliases, cfg.TargetParam);
        res.Changed drives re-serialization. (Nil args → Changed=false → forward
        original bytes; the nil test locks this safe path.)
  - P1.M3.T3.S1: warningText(res.Notes). The note-wording is pinned by S1 + reused
        verbatim in S2's wantNotes; S2's tests prove the wording is stable across
        every edge case.

DATABASE / ROUTES / ENV: none. Pure function tests, no I/O, no globals.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# After editing rewrite_test.go — fix before running tests.
gofmt -w rewrite_test.go          # format in place
go vet ./...                      # vet the whole module
go build ./...                    # must compile with zero new requires

# Confirm rewrite.go was NOT modified (frozen) unless a test exposed an S1 bug:
git diff --stat rewrite.go        # expect empty (or a minimal, justified fix)

# Confirm no accidental new dependencies:
git diff go.mod                   # expect empty

# Expected: zero errors; rewrite.go diff empty; go.mod diff empty.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run ONLY the rewrite tests, verbose — the 4 new funcs must appear and PASS
# alongside S1's 8 rows + InPlaceMutation.
go test -run TestRewrite -v

# Full module suite (rewrite + all pre-existing tests must still pass).
go test ./...

# Expected: PASS. The new funcs and their subtests:
#   TestRewrite_NilArgs                                  PASS
#   TestRewrite_OrderingMatters                          PASS
#   TestRewrite_DuplicateAliasDedup/target_absent_single_rename    PASS
#   TestRewrite_DuplicateAliasDedup/target_present_single_ignore   PASS
#   TestRewrite_NonStringValues/number                   PASS
#   TestRewrite_NonStringValues/object                   PASS
#   TestRewrite_NonStringValues/array                    PASS
# Compare each wantArgs/wantNotes to research edge-case-analysis.md §5 — identical.
```

### Level 3: Integration Testing (System Validation)

```bash
# rewrite.go is a pure function — the "integration" is the determinism + freeze
# contract, verified statically and by repeated runs.

# (a) Determinism: config-order promotion must NEVER flake (a `range args`
# regression would flip q-vs-query run-to-run). 100 passes must hold.
go test -run TestRewrite_OrderingMatters -count=100

# (b) De-dup stability: duplicate-alias cases must never produce a double note
# across repeated runs.
go test -run TestRewrite_DuplicateAliasDedup -count=100

# (c) Freeze certificate: the signature/fields are byte-for-byte unchanged from
# S1 (downstream consumers depend on them).
grep -n 'func Rewrite(args map\[string\]any, aliases \[\]string, target string) RewriteResult' rewrite.go
grep -nA3 'type RewriteResult struct' rewrite.go   # Changed bool; Notes []string

# Expected: 100/100 ordering passes; 100/100 de-dup passes; grep finds the exact
# S1 signature + struct (unchanged).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Consumer-shape smoke: confirm Rewrite is callable exactly as P1.M4.T1.S1 will
# call it, including the nil-arguments edge (a tools/call with absent arguments).
cat > /tmp/edge_smoke.go <<'EOF'
package main
import "testing"
func TestRewriteEdgeSmoke_ConsumerShape(t *testing.T) {
	// Mirror P1.M4.T1.S1's exact call with a nil arguments object.
	var nilArgs map[string]any
	if r := Rewrite(nilArgs, []string{"query"}, "search_query"); r.Changed || r.Notes != nil {
		t.Fatalf("nil args must be a no-op: %+v", r)
	}
	// And a non-string value (a real z.ai-style object arg).
	obj := map[string]any{"query": map[string]any{"nested": true}}
	r := Rewrite(obj, []string{"query"}, "search_query")
	if !r.Changed || obj["search_query"] == nil {
		t.Fatalf("non-string value not carried: %+v %#v", r, obj)
	}
}
EOF
cp /tmp/edge_smoke.go ./zz_edge_smoke_test.go && go test -run TestRewriteEdgeSmoke -v && rm ./zz_edge_smoke_test.go

# Expected: smoke compiles (consumer call shape + nil/no-op + value-passthrough).
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0 (compiles with stdlib only).
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l .` prints nothing (rewrite_test.go formatted).
- [ ] `go test ./...` exits 0 (rewrite + all pre-existing tests pass).
- [ ] `go test -run TestRewrite -v` shows the 8 S1 rows + InPlaceMutation + the 4
      new funcs (and their subtests) ALL PASS.

### Feature Validation

- [ ] `TestRewrite_NilArgs` passes a nil map DIRECTLY (bypasses cloneMap); asserts
      Changed=false, Notes=nil, args still nil.
- [ ] `TestRewrite_OrderingMatters` uses LOCAL `aliases=[q,query]`; asserts `q`
      promoted (`search_query`=="y"), rename(q)+dropped(query) notes.
- [ ] `TestRewrite_DuplicateAliasDedup` covers BOTH target-absent (single rename)
      and target-present (single ignored) de-dup paths.
- [ ] `TestRewrite_NonStringValues` covers number, object, AND array; asserts each
      value carried to `search_query` unchanged with one rename note.
- [ ] `go test -run TestRewrite_OrderingMatters -count=100` never flakes
      (determinism via the alias-slice walk).
- [ ] Empty args ({}) and no-alias ({foo:bar}) remain covered by S1 rows 7–8
      (NOT duplicated by S2).

### Code Quality Validation

- [ ] `rewrite.go` is UNCHANGED (frozen) unless a test exposed an S1 bug; `git
      diff rewrite.go` empty or minimal + justified. Signature/fields/note-wording
      identical to S1 (P1.M3/P1.M4 depend on them).
- [ ] New funcs follow the existing `TestRewrite_*` naming, `reflect.DeepEqual`
      map/slice comparison, and `t.Run` subtest conventions (config_test.go style).
- [ ] `rewrite_test.go` gains NO new imports (reflect+testing already present);
      `go.mod` gains zero `require`s.
- [ ] No anti-patterns (see below); no value-type inspection (PRD §3); no mutation
      when `Changed==false`; nil test bypasses cloneMap.

### Documentation & Deployment

- [ ] Mode A docs honored: NO README/config.example.json/doc.go changes (test-only,
      no surface change). The rewrite.go doc comment already documents de-dup + §3
      (S1); no doc edit needed.
- [ ] No new env vars / config keys / routes introduced.

---

## Anti-Patterns to Avoid

- ❌ Don't add the nil-args case as a row in `TestRewrite_Table` (or pass it through
  `cloneMap`). `cloneMap(nil)` returns an empty `{}` map, so the row would test
  the EMPTY path and NEVER exercise rewrite.go's `if args == nil` guard. Use a
  dedicated func that calls Rewrite directly with a nil map (research §2).
- ❌ Don't reuse `rwAliases` for the ordering or de-dup cases. They need a CUSTOM
  aliases slice (`[q,query]` / `[query,query]`); the table hard-codes rwAliases.
  Declare a local `aliases` per dedicated func; never mutate the package-level
  rwAliases.
- ❌ Don't modify `rewrite.go`'s signature, fields, or note wording. P1.M3/P1.M4
  consume them as a frozen seam. The existing algorithm already passes every S2
  case (research §4); S2 is test-only. If a test fails, fix the localized bug, do
  not "redesign" the algorithm.
- ❌ Don't emit the §10 Examples-table SHORTHAND notes (`renamed "query"`). Reuse
  the ALGORITHM-literal strings already in S1's rows (rename/dropped/ignored) so
  Notes round-trips through §12.3 warningText.
- ❌ Don't add bool/float/nil-value cases to `TestRewrite_NonStringValues`. The
  item scopes non-string values to number/object/array only; stay within that.
- ❌ Don't duplicate the empty-args ({}) or no-alias ({foo:bar}) cases — S1 rows
  7–8 already cover them (§19.1 lists them, but S1 satisfied them).
- ❌ Don't add a deep-clone helper for the object/array cases. `cloneMap`'s shallow
  copy is safe because Rewrite never mutates nested values; reflect.DeepEqual is
  correct on the shared reference.
- ❌ Don't touch any file other than `rewrite_test.go` (and, only if a test fails,
  a minimal fix to `rewrite.go`). No README, no config.example.json, no doc.go.
