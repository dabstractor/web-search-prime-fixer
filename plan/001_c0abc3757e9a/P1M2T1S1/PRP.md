name: "P1.M2.T1.S1 — RewriteResult + Rewrite algorithm (core table rows)"
description: |

  Ship the pure alias→`search_query` rewrite rule (`rewrite.go`) and its
  table-driven test (`rewrite_test.go`) covering the 8 rows of PRD §10 Examples.
  Zero third-party deps; stdlib `fmt` only. Consumed unchanged by P1.M4.T1.S1
  (proxy) and P1.M3.T3.S1 (warningText).

---

## Goal

**Feature Goal**: Deliver **PRD §10 — The rewrite rule** as a deterministic,
in-place, pure-ish function over `map[string]any` that renames configured alias
keys to the canonical `search_query` target, returning a `RewriteResult` whose
`Changed` flag drives request re-serialization and whose `Notes` slice feeds the
SSE warning text. The algorithm must be **deterministic despite Go's randomized
map iteration** by walking the ordered `aliases []string` slice (never the args
map), and must **de-dup duplicate alias entries** so they produce no double note.

**Deliverable**: Two NEW files at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`,
**zero modifications to any existing file** (rewrite.go is a standalone unit;
P1.M1.T4.S2's `proxy.go` is being implemented in parallel and is NOT touched):
1. **CREATE** `rewrite.go` — the `RewriteResult` struct, the `Rewrite` function
   (full algorithm, steps 1–5, including the step-2 de-dup), and a thorough Mode-A
   doc comment on `Rewrite`. Imports only `fmt`.
2. **CREATE** `rewrite_test.go` — a table-driven test (`TestRewrite_Table`) with
   exactly the **8 rows of PRD §10 Examples**, asserting the mutated args map,
   the exact `Notes` slice, and `Changed`, plus a `TestRewrite_InPlaceMutation`
   sanity check that the input map reference is mutated (not copied).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run TestRewrite -v`) all exit clean. Every one of
the 8 §10 Examples rows passes: alias-only inputs are renamed to `search_query`,
multi-alias inputs promote the config-first alias and drop the rest, a
target-already-present input keeps the target value and ignores the alias, and
non-alias / empty inputs pass through byte-for-byte with `Changed==false` and
`Notes==nil`. `go.mod` gains zero `require`s (only stdlib `fmt` is imported). The
`RewriteResult` / `Rewrite` signatures exactly match the contract so P1.M4.T1.S1
and P1.M3.T3.S1 consume them without edits.

## User Persona

**Target User**: (1) downstream subtask implementers — P1.M4.T1.S1 calls
`Rewrite(args, cfg.Aliases, cfg.TargetParam)` to decide re-serialization and
reqID tracking; P1.M3.T3.S1 consumes `RewriteResult.Notes` via `warningText`.
(2) the end MCP client, who gets `search_query` accepted even when they typed
`query`/`q`/`searchQuery`, plus a terse warning. (3) the maintainer, who gets a
fully isolated, table-tested pure function with no I/O or config coupling.

**Use Case**: A client sends `tools/call` with `{"query":"rust async"}`. The
proxy's request handler calls `Rewrite(args, cfg.Aliases, "search_query")`; the
function moves the value to `search_query`, deletes `query`, returns
`{Changed:true, Notes:[`"query" is not a valid parameter; renamed to "search_query"`]}`.
The proxy re-serializes and forwards; the SSE injector prepends the warning.

**User Journey**: args map in → (guard) → (build `present` in config order,
de-duped) → (empty? return unchanged) → (target present? drop aliases + ignore
notes) → (target absent? promote present[0] + rename note, drop present[1:] +
dropped notes) → mutated args map out + `RewriteResult`.

**Pain Points Addressed**: (1) without the slice-ordered walk, "first alias
promoted" would be nondeterministic (Go map randomization) — a correctness bug.
(2) without de-dup, a duplicate config alias would double-note and spuriously
"drop" the chosen alias. (3) P1.M4/P1.M3 need a stable, side-effect-only-on-args
helper to wire into the request path.

## Why

- Implements **PRD §5 FR-2** ("the one rewrite rule"), **§10** (algorithm +
  Examples), **§3** (non-goals: never touch non-alias keys, never truncate
  query text), **§19.1** (rewrite_test.go table-driven).
- Obeys **architecture/external_deps.md §6** (map iteration randomized → MUST
  iterate the ordered `aliases` slice to pick a target; this is the linchpin of
  determinism).
- Establishes the **stable seam** P1.M4.T1.S1 (request handling: `Changed` →
  re-serialize + reqID; `Notes` → warning) and P1.M3.T3.S1 (warningText joins
  `Notes`) consume. Both depend on the EXACT signature/fields and the EXACT note
  string wording shipped here.
- The Notes string wording is **fixed by the algorithm contract** (not the
  Examples-table shorthand) so it round-trips through PRD §12.3's warningText.

## What

`rewrite.go` (NEW, `package main`) exports exactly:

- **`type RewriteResult struct { Changed bool; Notes []string }`** — verbatim
  from PRD §10. `Notes` is "human-readable, one per affected alias". Invariants
  enforced by the algorithm (document on the doc comment): `Changed==true`
  ⟹ `len(Notes) >= 1`; `Changed==false` ⟹ `Notes==nil` AND args is untouched
  (zero-value `RewriteResult{}`).
- **`func Rewrite(args map[string]any, aliases []string, target string)
  RewriteResult`** — the algorithm below, mutating `args` IN PLACE and returning
  the result. Pure otherwise (no I/O, no globals, no config dependency).

### Algorithm (implemented exactly; `present` is built de-duped per contract step 2)

1. **Guard**: `if args == nil || target == "" || len(aliases) == 0 { return
   RewriteResult{} }` — zero-value result, `Changed==false`, `Notes==nil`.
2. **Build `present`** in CONFIG ORDER, DE-DUPED: range `for _, a := range
   aliases`; skip `a` if already seen (a `seen map[string]bool`); else mark seen;
   if `_, ok := args[a]; ok` then `present = append(present, a)`. **Never range
   the args map** (external_deps.md §6).
3. **Empty short-circuit**: `if len(present) == 0 { return RewriteResult{} }`.
4. **Target already present** (`_, ok := args[target]; ok`): for each `a` in
   `present`: `delete(args, a)`; append note
   `` `ignored "` + a + `" (use only "` + target + `")` `` (or `fmt.Sprintf("ignored
   %q (use only %q)", a, target)`). Return `RewriteResult{Changed:true, Notes:
   notes}`.
5. **Target absent**: `chosen := present[0]`; `args[target] = args[chosen]`;
   `delete(args, chosen)`; append note
   `` `"` + chosen + `" is not a valid parameter; renamed to "` + target + `"` ``
   (or `fmt.Sprintf("%q is not a valid parameter; renamed to %q", chosen, target)`);
   then for each `a` in `present[1:]`: `delete(args, a)`; append note
   `` `dropped redundant "` + a + `"` `` (or `fmt.Sprintf("dropped redundant %q", a)`).
   Return `RewriteResult{Changed:true, Notes: notes}`.

`rewrite_test.go` (NEW, `package main`) — see Validation Loop Level 2.

### Success Criteria

- [ ] `rewrite.go` defines `RewriteResult{Changed bool; Notes []string}` and
      `Rewrite(args map[string]any, aliases []string, target string) RewriteResult`
      with EXACTLY those identifiers/types (consumers depend on them).
- [ ] The algorithm walks the ordered `aliases` SLICE to build `present` and NEVER
      ranges the `args` map (grep the file: `range args` must NOT appear; only
      `args[k]` reads / `delete(args, k)`).
- [ ] Duplicate alias entries in `aliases` produce a single `present` entry (no
      double note; no spurious "dropped" of the chosen alias).
- [ ] `Changed==false` returns a zero-value result (`Notes==nil`) and leaves `args`
      byte-for-byte unchanged (no key added, removed, or moved).
- [ ] `Changed==true` always has `len(Notes) >= 1`; non-alias keys are never
      deleted/moved/renamed (PRD §3).
- [ ] `Rewrite` has a Mode-A doc comment documenting: the algorithm overview,
      alias-order determinism (why the slice is walked, not the map), the in-place
      mutation contract, and the `Changed`/`Notes` invariants.
- [ ] All 8 PRD §10 Examples rows pass (see Validation Loop Level 2 table).
- [ ] `go.mod` gains zero `require`s; `rewrite.go` imports only `fmt` (stdlib);
      `rewrite_test.go` imports only `reflect` + `testing` (stdlib).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.

## All Needed Context

### Context Completeness Check

_Pass._ The work-item contract fixes the struct fields, the function signature,
and the full 5-step algorithm verbatim (including the step-2 de-dup and the exact
note strings). The determinism requirement is pinned to
`architecture/external_deps.md §6` (map randomization → walk the alias slice). The
input state — `config.go`'s `Config{Aliases []string; TargetParam string}` (the
real callers pass `cfg.Aliases`, `cfg.TargetParam`) — is read on-disk; the test
defines its own local `aliases`/`target` to stay decoupled. The note-string
wording is pinned to PRD §10 ALGORITHM (literal), cross-checked against §12.3's
warningText example (which consumes the same strings). The test conventions
(table-driven, `reflect.DeepEqual`, `package main`) are read on-disk from
`config_test.go`/`resolve_test.go`. rewrite.go has NO dependency on the
in-parallel `proxy.go` (P1.M1.T4.S2) — it is a standalone unit, so there is no
file-level coordination risk. An agent with no prior knowledge of this codebase
can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative algorithm + examples.
- file: PRD.md
  why: §10 (THE rewrite rule — struct, signature, 5-step algorithm, Examples
        table); §3 (non-goals — never touch non-alias keys / truncate query);
        §5 FR-2 (the one rewrite rule, first-alias-promoted semantics);
        §19.1 (rewrite_test.go is table-driven; rows = input/expected-args/
        expected-notes-substrings/Changed).
  critical: §10's Examples-table "Notes" column (renamed "query", dropped "q",
        ignored "query") is HUMAN SHORTHAND. The LITERAL note strings live in the
        §10 Algorithm steps 4–5 and are what RewriteResult.Notes must contain,
        because §12.3 warningText joins them verbatim. See research note.

- file: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§6. Go map ordering is non-deterministic"
  why: PROVES why `present` MUST be built by ranging the ordered `aliases`
        []string` slice, NOT the args map — otherwise "first alias promoted" is
        nondeterministic (a correctness bug, not style).
  critical: NEVER write `for k := range args`; only `args[k]` / `delete(args,k)`.

- file: config.go
  why: shows the REAL caller shape — `Rewrite(args, cfg.Aliases, cfg.TargetParam)`.
        Config.Aliases is `[]string{"query","q","search","searchQuery","search_term"}`
        and Config.TargetParam is `"search_query"` (DefaultConfig, the §10 implicit
        inputs). The test re-declares these locally to stay decoupled.
  pattern: the function takes RAW `aliases []string` + `target string` params, NOT
        a Config — this keeps rewrite.go config-free and unit-testable in isolation.

- file: config_test.go
  why: the project's table-driven test convention to follow for rewrite_test.go:
        `tests := []struct{...}{...}`; `for _, tc := range tests { t.Run(tc.name,
        func(t){...}) }`; `reflect.DeepEqual` for struct/map comparison.
  pattern: per-row `want` derived explicitly (do not couple rewrite_test to
        DefaultConfig); helper funcs (e.g. writeConfig) are local + t.Helper().

- file: resolve_test.go
  why: confirms `package main` + `import ("reflect"; "testing")` test layout and
        the `t.Helper()` / `t.Cleanup` conventions; shows map/struct assertions.
  pattern: short test funcs, one logical group each, table-driven where row-based.

- docfile: plan/001_c0abc3757e9a/P1M2T1S1/research/notes-and-contract.md
  why: resolves the two cross-task ambiguities — (1) Notes use ALGORITHM strings
        not table shorthand; (2) de-dup is OWNED by S1 (S2 only adds a test row).
        Also pins the consumer-seam contract invariants (Changed⟺Notes rules).

# CONSUMER SEAMS (do not change signature/fields; they depend on this PRP):
- file: plan/001_c0abc3757e9a/P1M1T4S2/PRP.md
  why: defines the proxy.go forward core (in parallel). rewrite.go is INDEPENDENT
        of proxy.go (no shared symbols, no file overlap) — zero coordination risk.
        P1.M4.T1.S1 will call Rewrite from the request handler built on proxy.go.
```

### Current Codebase tree (run `tree` / `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package comment (package main)
  main.go           # bootstrap: config load, /healthz, server, graceful shutdown (T4.S1/S2)
  config.go         # Config{Upstream,Listen,Path,Aliases[]string,TargetParam,LogLevel}
  config_test.go    # table-driven config tests (PATTERN TO FOLLOW)
  resolve_test.go   # ResolveConfig tests (PATTERN TO FOLLOW)
  logger_test.go    # logger tests
  health_test.go    # /healthz isolation tests
  proxy.go          # transparent passthrough forward core (T4.S2, IN PARALLEL)
  testdata/.gitkeep
  PRD.md
  # --- ABSENT (this subtask creates them): ---
  # rewrite.go       <- NEW (this subtask)
  # rewrite_test.go  <- NEW (this subtask)
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
rewrite.go        # NEW. RewriteResult struct + Rewrite() pure function (5-step
                  #      algorithm, de-duped `present`, in-place mutation). fmt only.
rewrite_test.go   # NEW. Table-driven test of the 8 PRD §10 Examples rows + an
                  #      in-place-mutation sanity test. reflect+testing only.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL: Go MAP iteration order is randomized (runtime). NEVER build `present`
// by ranging the args map — "first alias promoted" would flip run-to-run. Walk
// the ordered `aliases []string` slice. (external_deps.md §6, on-disk verified.)
//   WRONG: for k := range args { ... }            // nondeterministic
//   RIGHT: for _, a := range aliases { if _,ok:=args[a]; ok{...} }

// CRITICAL: the §10 Examples "Notes" column (renamed "query" / dropped "q" /
// ignored "query") is SHORTHAND. Emit the ALGORITHM's literal strings, because
// §12.3 warningText joins RewriteResult.Notes verbatim and its example is the
// full algorithm string:
//   rename:  `"<chosen>" is not a valid parameter; renamed to "<target>"`
//   drop:    `dropped redundant "<alias>"`
//   ignore:  `ignored "<alias>" (use only "<target>")`

// CRITICAL: de-dup `aliases` while building `present` (contract step 2). Without
// it, aliases=["query","query"] + args={"query":"x"} yields present=["query",
// "query"] → step 5 renames+deletes "query" THEN walks present[1:]=["query"],
// deletes it AGAIN (no-op) and appends a spurious `dropped redundant "query"`.
// Fix: `seen map[string]bool`; skip `a` if seen[a] BEFORE the args-existence test.

// GOTCHA: maps are reference types — Rewrite mutates `args` IN PLACE (the doc
// comment MUST state this). Callers that need the original must clone first.
// `Changed==false` returns a zero-value RewriteResult (Notes==nil, NOT []); the
// warningText consumer must tolerate nil (it is only called when Changed==true).

// GOTCHA (import choice): the contract says "zero imports beyond possibly
// reflect/slices". This implementation needs only `fmt` (for Sprintf %q quoting).
// reflect/slices are NOT required. `%q` renders "query" for ASCII alias names —
// equivalent to literal `"`+a+`"` concat but safer/cleaner. Either is acceptable.

// GOTCHA: args values can be non-strings (numbers, objects, arrays). The
// algorithm only moves keys (`args[target]=args[chosen]`) and deletes keys — it
// NEVER inspects/converts values, so non-string values pass through as-is. Do
// NOT add any value-type check (PRD §3: don't normalize/truncate).

// GOTCHA: `len(present)==0` must return BEFORE the Changed=true path so a map
// with only non-alias keys (e.g. {"foo":"bar"}) is byte-for-byte unchanged and
// Notes stays nil (proxy then forwards original bytes, no re-serialization).
```

## Implementation Blueprint

### Data models and structure

```go
// RewriteResult is the entire data surface of this subtask. Fields are fixed by
// PRD §10 and consumed verbatim by P1.M4.T1.S1 (.Changed) and P1.M3.T3.S1
// (.Notes). Do NOT add/remove/rename fields.
type RewriteResult struct {
    Changed bool
    Notes   []string // one entry per affected alias, algorithm-literal wording
}
```

No other types are introduced. `args`/`aliases`/`target` are the caller's; the
function takes `map[string]any`, `[]string`, `string` (not Config) to stay pure.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: CREATE rewrite.go
  - DEFINE: `type RewriteResult struct { Changed bool; Notes []string }`
  - DEFINE: `func Rewrite(args map[string]any, aliases []string, target string) RewriteResult`
  - IMPLEMENT: the 5-step algorithm verbatim (see "Algorithm" above), with the
        step-2 de-dup via a `seen map[string]bool`.
  - DOC COMMENT (Mode A) on Rewrite MUST cover: (a) one-paragraph algorithm
        overview; (b) alias-order DETERMINISM — explicitly "the ordered aliases
        slice is walked, never the args map, because Go map iteration is
        randomized (would make first-alias-promoted nondeterministic)"; (c) the
        IN-PLACE mutation contract ("args is mutated in place; Changed==false
        leaves it untouched"); (d) the Changed/Notes invariants ("Changed==true
        implies len(Notes)>=1; Changed==false implies Notes==nil and args
        untouched"); (e) non-alias keys are never touched (PRD §3).
  - IMPORTS: only `fmt` (for Sprintf %q) — OR drop `fmt` and use literal
        `"`+v+`"` concatenation (your call; %q is recommended).
  - NAMING: RewriteResult / Rewrite (exported, exactly per PRD §10).
  - PLACEMENT: repo root, `package main`.
  - CONSTRAINT: grep the finished file — `range args` MUST NOT appear; only
        `args[<expr>]` and `delete(args, <expr>)`. `go vet`/`gofmt` clean.

Task 2: CREATE rewrite_test.go
  - DEFINE: a table `TestRewrite_Table` with EXACTLY the 8 PRD §10 Examples rows
        (see the row table in Validation Loop Level 2 — copy the wantArgs /
        wantNotes / wantChanged values verbatim from there).
  - PER ROW: clone the input args (maps are reference types) via a local
        `cloneMap(map[string]any) map[string]any` helper; call
        `got := Rewrite(clone, aliases, target)`; assert
        `reflect.DeepEqual(clone, tc.wantArgs)`; assert
        `reflect.DeepEqual(got.Notes, tc.wantNotes)`; assert `got.Changed ==
        tc.wantChanged`.
  - DEFINE local (NOT DefaultConfig-coupled) `aliases := []string{"query","q",
        "search","searchQuery","search_term"}` and `target := "search_query"` so
        the rewrite unit is decoupled from config.go defaults.
  - ADD: `TestRewrite_InPlaceMutation` — pass a known map, confirm the SAME map
        variable is mutated (not a copy): e.g. args={"query":"x"}; Rewrite(args,
        aliases, target); assert args has key "search_query"==value and NO key
        "query" (proves in-place mutation of the caller's map reference).
  - FOLLOW pattern: config_test.go (table-driven, t.Run subtests, reflect.DeepEqual).
  - NAMING: test funcs `TestRewrite_Table`, `TestRewrite_InPlaceMutation`; subtest
        names match the Examples row semantics (e.g. "query_renamed").
  - IMPORTS: only `reflect` + `testing`.
  - PLACEMENT: repo root, `package main`, alongside rewrite.go.
  - COVERAGE: the 8 Examples rows exercise rename (3), rename+drop (1),
        ignore-when-target-present (1), unchanged/no-alias (1), unchanged/empty (1),
        unchanged/target-only (1).
```

### Implementation Patterns & Key Details

```go
// rewrite.go — reference shape (the implementer may choose %q vs concat for notes).

package main

import "fmt"

// RewriteResult describes the outcome of one Rewrite call. Notes carries the
// algorithm-literal per-alias messages that warningText joins into the SSE
// warning (PRD §12.3). Invariants: Changed==true ⟹ len(Notes)>=1;
// Changed==false ⟹ Notes==nil and args untouched.
type RewriteResult struct {
	Changed bool
	Notes   []string
}

// Rewrite applies the configured alias→target rename to args IN PLACE and
// returns what happened. aliases is walked in CONFIG ORDER (the first present
// alias is promoted when target is absent); the args map is NEVER iterated,
// because Go map iteration is randomized and would make the promoted-alias
// choice nondeterministic. Duplicate alias entries are de-duped (no double note).
// Non-alias keys are never touched (PRD §3). When nothing matches, args is left
// byte-for-byte unchanged and a zero-value RewriteResult is returned.
//
// Algorithm (PRD §10):
//  1. nil args / empty target / empty aliases → unchanged.
//  2. present = aliases (config order) that exist as keys in args, de-duped.
//  3. present empty → unchanged.
//  4. target already a key: drop every present alias, note `ignored ...`.
//  5. target absent: promote present[0] (rename note), drop present[1:]
//     (dropped notes).
func Rewrite(args map[string]any, aliases []string, target string) RewriteResult {
	// 1. Guard.
	if args == nil || target == "" || len(aliases) == 0 {
		return RewriteResult{}
	}
	// 2. Build present in config order, de-duped. NEVER range over args.
	seen := make(map[string]bool)
	var present []string
	for _, a := range aliases {
		if seen[a] {
			continue
		}
		seen[a] = true
		if _, ok := args[a]; ok {
			present = append(present, a)
		}
	}
	// 3. Empty short-circuit.
	if len(present) == 0 {
		return RewriteResult{}
	}
	notes := make([]string, 0, len(present))
	// 4. Target already present: canonical value wins; drop all aliases.
	if _, ok := args[target]; ok {
		for _, a := range present {
			delete(args, a)
			notes = append(notes, fmt.Sprintf("ignored %q (use only %q)", a, target))
		}
		return RewriteResult{Changed: true, Notes: notes}
	}
	// 5. Target absent: promote the first present alias, drop the rest.
	chosen := present[0]
	args[target] = args[chosen]
	delete(args, chosen)
	notes = append(notes, fmt.Sprintf("%q is not a valid parameter; renamed to %q", chosen, target))
	for _, a := range present[1:] {
		delete(args, a)
		notes = append(notes, fmt.Sprintf("dropped redundant %q", a))
	}
	return RewriteResult{Changed: true, Notes: notes}
}

// ---- rewrite_test.go ----
// Table-driven. aliases/target are LOCAL constants (decoupled from config.go).

var rwAliases = []string{"query", "q", "search", "searchQuery", "search_term"}
const rwTarget = "search_query"

func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestRewrite_Table(t *testing.T) {
	tests := []struct {
		name       string
		in         map[string]any
		wantArgs   map[string]any
		wantNotes  []string
		wantChange bool
	}{
		// Row 1: {"query":"x"} → renamed
		{"query_renamed",
			map[string]any{"query": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"query" is not a valid parameter; renamed to "search_query"`},
			true},
		// Row 2: {"searchQuery":"x"} → renamed
		{"searchQuery_renamed",
			map[string]any{"searchQuery": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"searchQuery" is not a valid parameter; renamed to "search_query"`},
			true},
		// Row 3: {"q":"x"} → renamed
		{"q_renamed",
			map[string]any{"q": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"q" is not a valid parameter; renamed to "search_query"`},
			true},
		// Row 4: {"query":"x","q":"y"} → query promoted, q dropped
		{"query_promoted_q_dropped",
			map[string]any{"query": "x", "q": "y"},
			map[string]any{"search_query": "x"},
			[]string{
				`"query" is not a valid parameter; renamed to "search_query"`,
				`dropped redundant "q"`,
			},
			true},
		// Row 5: {"query":"x","search_query":"y"} → target wins, query ignored
		{"target_present_query_ignored",
			map[string]any{"query": "x", "search_query": "y"},
			map[string]any{"search_query": "y"},
			[]string{`ignored "query" (use only "search_query")`},
			true},
		// Row 6: {"search_query":"x"} → unchanged (target only, no alias)
		{"target_only_unchanged",
			map[string]any{"search_query": "x"},
			map[string]any{"search_query": "x"},
			nil,
			false},
		// Row 7: {"foo":"bar"} → unchanged (no alias present)
		{"non_alias_unchanged",
			map[string]any{"foo": "bar"},
			map[string]any{"foo": "bar"},
			nil,
			false},
		// Row 8: {} → unchanged (empty)
		{"empty_unchanged",
			map[string]any{},
			map[string]any{},
			nil,
			false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := cloneMap(tc.in)
			got := Rewrite(args, rwAliases, rwTarget)
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %#v\nwant    %#v", args, tc.wantArgs)
			}
			if !reflect.DeepEqual(got.Notes, tc.wantNotes) {
				t.Errorf("Notes = %#v\nwant     %#v", got.Notes, tc.wantNotes)
			}
			if got.Changed != tc.wantChange {
				t.Errorf("Changed = %v, want %v", got.Changed, tc.wantChange)
			}
		})
	}
}

func TestRewrite_InPlaceMutation(t *testing.T) {
	// Proves the caller's map reference is mutated, not copied.
	args := map[string]any{"query": "x"}
	Rewrite(args, rwAliases, rwTarget)
	if _, ok := args["query"]; ok {
		t.Errorf(`args still has "query"; expected it deleted in place`)
	}
	if v, ok := args["search_query"]; !ok || v != "x" {
		t.Errorf(`args["search_query"] = %v, ok=%v; want "x", true (in-place move)`, v, ok)
	}
}
```

### Integration Points

```yaml
NO FILES MODIFIED:
  - This subtask ONLY adds rewrite.go + rewrite_test.go. It touches NOTHING else.
  - main.go / proxy.go / config.go are untouched. P1.M1.T4.S2 (proxy.go, in
    parallel) and rewrite.go share no symbols and no files → zero conflict.

CONSUMER SEAMS (future subtasks wire here; signatures fixed by this PRP):
  - P1.M4.T1.S1 (proxy.go request handling, PRD §11.1):
        res := Rewrite(params.Arguments, cfg.Aliases, cfg.TargetParam)
        if res.Changed { re-serialize; stash reqID for SSE correlation }
  - P1.M3.T3.S1 (warningText, PRD §12.3):
        warningText(res.Notes)  // joins Notes with "; ", adds the suffix

CONFIG (read-only reference; rewrite.go does NOT import config):
  - Config.Aliases []string  (default: query,q,search,searchQuery,search_term)
  - Config.TargetParam string (default/forced: "search_query")
  - The caller passes cfg.Aliases / cfg.TargetParam as plain args; rewrite.go
    takes []string + string, keeping it config-free and unit-testable.

DATABASE / ROUTES / ENV: none. Pure function, no I/O, no globals.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# Run after creating rewrite.go — fix before writing the test.
gofmt -w rewrite.go rewrite_test.go     # format in place
go vet ./...                            # vet the whole module (catches shadowing, etc.)
go build ./...                          # must compile with zero new requires

# Confirm the determinism constraint holds (grep is a cheap static check):
grep -n 'range args' rewrite.go         # MUST print nothing (args map never iterated)
grep -n 'range aliases' rewrite.go      # MUST print the present-building loop

# Confirm no accidental config coupling:
grep -n 'config\|Config\|cfg' rewrite.go # MUST print nothing (rewrite.go is config-free)

# Expected: zero errors; `range args` absent; `range aliases` present; no config refs.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the rewrite tests in isolation, verbose.
go test -run TestRewrite -v

# Full module suite (rewrite + all existing tests must still pass).
go test ./...

# Expected: PASS. The 8 PRD §10 Examples rows MUST match this table exactly:

# | # | Input args                         | Output args            | Notes                                                  | Changed |
# |---|------------------------------------|------------------------|--------------------------------------------------------|---------|
# | 1 | {"query":"x"}                      | {"search_query":"x"}   | `"query" is not a valid parameter; renamed to "search_query"` | true    |
# | 2 | {"searchQuery":"x"}                | {"search_query":"x"}   | `"searchQuery" is not a valid parameter; renamed to "search_query"` | true  |
# | 3 | {"q":"x"}                          | {"search_query":"x"}   | `"q" is not a valid parameter; renamed to "search_query"` | true    |
# | 4 | {"query":"x","q":"y"}              | {"search_query":"x"}   | rename(query), `dropped redundant "q"`                 | true    |
# | 5 | {"query":"x","search_query":"y"}   | {"search_query":"y"}   | `ignored "query" (use only "search_query")`            | true    |
# | 6 | {"search_query":"x"}               | {"search_query":"x"}   | nil                                                    | false   |
# | 7 | {"foo":"bar"}                      | {"foo":"bar"}          | nil                                                    | false   |
# | 8 | {}                                 | {}                     | nil                                                    | false   |

# Notes use the ALGORITHM-literal strings (PRD §10 steps 4–5), NOT the Examples-
# table shorthand, so they round-trip through §12.3 warningText. Compare each row
# to the wantNotes slice encoded in TestRewrite_Table — they must be identical.
```

### Level 3: Integration Testing (System Validation)

```bash
# rewrite.go is a pure function with NO runtime integration (no server, no I/O,
# no config load). The "integration" is the consumer contract, verified statically:

# (a) The function compiles and is callable with the real caller's arg types:
cat > /tmp/rewrite_smoke_test.go <<'EOF'
package main
import "testing"
func TestRewriteSmoke_RealCallerShape(t *testing.T) {
	// Mirror P1.M4.T1.S1's exact call: Rewrite(args, cfg.Aliases, cfg.TargetParam).
	args := map[string]any{"query": "rust async"}
	res := Rewrite(args, []string{"query","q","search","searchQuery","search_term"}, "search_query")
	if !res.Changed || len(res.Notes) != 1 {
		t.Fatalf("expected Changed+1 note, got %+v", res)
	}
	if args["search_query"] != "rust async" { t.Fatalf("value not moved: %#v", args) }
}
EOF
cp /tmp/rewrite_smoke_test.go ./zz_smoke_test.go && go test -run TestRewriteSmoke -v && rm ./zz_smoke_test.go

# (b) Determinism: run the table test many times; map-randomization bugs would
# make "first alias promoted" flaky across runs (it must NEVER flake).
go test -run TestRewrite_Table -count=100

# Expected: smoke compiles (signature matches the real caller); count=100 passes
# every time (determinism via the alias-slice walk).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Determinism stress: confirm config-order promotion is stable under map
# randomization. Build a multi-alias input and assert the SAME chosen alias
# across 1000 runs (a bug ranging the args map would flip the chosen alias).
cat > /tmp/det_stress.go <<'EOF'
package main
import "testing"
func TestRewriteDeterminism_Stress(t *testing.T) {
	for i := 0; i < 1000; i++ {
		args := map[string]any{"query": 1, "q": 2, "search": 3, "searchQuery": 4, "search_term": 5}
		res := Rewrite(args, []string{"query","q","search","searchQuery","search_term"}, "search_query")
		// query is config-first AND present → MUST be the promoted value (1).
		if args["search_query"] != 1 {
			t.Fatalf("run %d: nondeterministic promotion; search_query=%v (want 1)", i, args["search_query"])
		}
		// Exactly one rename note (query) + 4 dropped notes.
		if len(res.Notes) != 5 { t.Fatalf("run %d: notes=%v", i, res.Notes) }
	}
}
EOF
cp /tmp/det_stress.go ./zz_det_stress_test.go && go test -run TestRewriteDeterminism_Stress -v && rm ./zz_det_stress_test.go

# Mutation-contract check: non-alias keys survive untouched (PRD §3).
go test -run TestRewrite_Table -v   # row 7 {"foo":"bar"} already asserts this

# Expected: 1000/1000 runs pick `query` (deterministic); non-alias keys intact.
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0 (compiles with stdlib only).
- [ ] `go vet ./...` exits 0.
- [ ] `gofmt -l .` prints nothing (rewrite.go + rewrite_test.go formatted).
- [ ] `go test ./...` exits 0 (rewrite + all pre-existing tests pass).
- [ ] `go test -run TestRewrite -v` shows all 8 Examples rows + in-place test PASS.

### Feature Validation

- [ ] All 8 PRD §10 Examples rows pass with the EXACT wantArgs/wantNotes/Changed
      in the Level 2 table.
- [ ] `grep 'range args' rewrite.go` returns nothing; `grep 'range aliases'`
      returns the present-building loop (determinism constraint enforced).
- [ ] `go test -run TestRewrite_Table -count=100` never flakes (deterministic).
- [ ] Non-alias keys (`foo`) and target-only keys (`search_query` alone) leave
      args byte-for-byte unchanged with `Changed==false`, `Notes==nil`.
- [ ] Multi-alias input (row 4) promotes the config-FIRST alias (`query`) and
      drops the rest (`q`), in config order.
- [ ] Target-already-present input (row 5) keeps the target value and emits an
      `ignored` note (canonical value wins).
- [ ] `RewriteResult`/`Rewrite` identifiers/signatures EXACTLY match PRD §10
      (consumers P1.M4.T1.S1 / P1.M3.T3.S1 depend on them unchanged).

### Code Quality Validation

- [ ] Mode-A doc comment on `Rewrite` covers algorithm overview, alias-order
      determinism, in-place mutation contract, Changed/Notes invariants, and the
      PRD §3 non-alias guarantee.
- [ ] `rewrite.go` imports only `fmt` (or zero imports via concat); `rewrite_test.go`
      imports only `reflect` + `testing`; `go.mod` gains zero `require`s.
- [ ] `rewrite_test.go` defines `aliases`/`target` locally (decoupled from
      `DefaultConfig()`); follows config_test.go's table-driven `t.Run` style.
- [ ] No anti-patterns (see below); no value-type inspection (PRD §3); no config
      coupling; no mutation when `Changed==false`.

### Documentation & Deployment

- [ ] Doc comment is self-contained (an agent reading only rewrite.go understands
      the algorithm, determinism rationale, and mutation contract).
- [ ] No new env vars / config keys / routes introduced (pure function).
- [ ] No existing file modified (rewrite.go + rewrite_test.go are the only adds).

---

## Anti-Patterns to Avoid

- ❌ Don't range over the `args` map to build `present` or pick an alias — Go map
  iteration is randomized; "first alias promoted" would be nondeterministic
  (external_deps.md §6). Walk the ordered `aliases []string` slice.
- ❌ Don't emit the Examples-table SHORTHAND notes (`renamed "query"`). Emit the
  ALGORITHM-literal strings — §12.3 warningText joins `Notes` verbatim and its
  example is the full algorithm string.
- ❌ Don't skip the de-dup in step 2. A duplicate config alias would double-note
  and spuriously "drop" the chosen alias. De-dup while building `present`.
- ❌ Don't mutate args when `Changed==false`. The proxy uses `Changed==false` to
  forward ORIGINAL bytes (no re-serialization); any mutation there is a bug.
- ❌ Don't inspect/convert/normalize arg VALUES (numbers, objects). PRD §3: never
  normalize or truncate. Only move/delete keys; `args[target]=args[chosen]` as-is.
- ❌ Don't couple rewrite.go to `config.go` (no `Config`/`cfg` imports). The
  function takes `[]string` + `string` so it stays a pure, isolated, unit-testable
  unit; the test defines aliases/target locally.
- ❌ Don't touch any existing file. This subtask adds rewrite.go + rewrite_test.go
  only. proxy.go (P1.M1.T4.S2, in parallel) shares no symbols — no edits to it.
- ❌ Don't add fields to `RewriteResult` or change the `Rewrite` signature.
  P1.M4.T1.S1 and P1.M3.T3.S1 consume them as fixed contracts.
