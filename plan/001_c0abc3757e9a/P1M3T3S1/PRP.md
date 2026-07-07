name: "P1.M3.T3.S1 — warningText(notes): SSE warning string format (renamed/ignored/dropped + suffix rules)"
description: |

  Implement the **warning-text formatter** for the SSE module: a pure, unexported
  `func warningText(notes []string) string` that renders `RewriteResult.Notes`
  (`rewrite.go`, P1.M2.T1.S1) as the single-line agent-facing warning defined by
  **PRD §12.3**. It is the formatter half of the SSE injection feature; the
  injector that calls it is P1.M3.T3.S2. The notes arrive **already formatted**
  by `Rewrite` as the literal PRD §10 algorithm strings — `warningText` does NOT
  re-derive them; it only joins them with `"; "`, wraps them in the
  `[web-search-prime-fixer] … .` envelope, and appends the **suffix** chosen by
  the suffix rule:

  - if **EVERY** note is an "ignored" note → suffix ` Use only "search_query" to avoid this notice.`
  - otherwise → suffix ` Use "search_query" in future calls.`

  The single load-bearing decision: an "ignored" note is detected by the literal
  prefix `ignored ` (`strings.HasPrefix`) — the only one of the three note kinds
  that begins with that prefix (renamed begins with `"`, dropped with `dropped `),
  so the classifier is exact and unambiguous. **Append** `warningText` to the
  existing `sse.go` (created by P1.M3.T1.S1 reader) and **append** its table-driven
  tests to the existing `sse_test.go`. Pure function, stdlib-only, zero new
  `require`s, no other `.go` file touched. **Mode A docs**: a doc comment on
  `warningText` documenting both suffix rules (PRD §12.3).

---

## Goal

**Feature Goal**: A pure, deterministic `warningText(notes []string) string` that
turns `RewriteResult.Notes` into the exact PRD §12.3 warning line — byte-for-byte
matching the two documented examples and the ignored-suffix case — including the
all-ignored-suffix branch.

**Deliverable**: NO new files. Two edits to existing files (both created by
P1.M3.T1.S1, the reader):
- **APPEND to `sse.go`** — `func warningText(notes []string) string` + a Mode-A
  doc comment documenting the two suffix rules. Ensure `"strings"` is imported
  (the reader's `sse.go` already imports it; add only if absent).
- **APPEND to `sse_test.go`** — `TestWarningText_Table` (table-driven) covering
  renamed-only, renamed+dropped, ignored-only, multiple-ignored, mixed, and the
  empty-notes edge case, with byte-exact expected strings.

`go.mod` gains ZERO `require`s (stdlib only). No other `.go` file is modified.

**Success Definition**: `go test -run 'TestWarningText' -v` passes; in particular
the three PRD §12.3 contract cases produce byte-identical output to the strings
in §12.3 (reproduced verbatim below). `go vet ./...` and `go test ./...` stay
clean. `go doc . warningText` shows the suffix-rule doc comment (Mode A).

## Hard Prerequisites

1. **`sse.go` exists** (created by P1.M3.T1.S1 reader, PRP at
   `plan/001_c0abc3757e9a/P1M3T2S1/PRP.md`). When this item runs, `sse.go` already
   defines `Event`/`Reader`/`NewSSEReader`/`Next` and imports `bufio`, `io`,
   `strings`. This item APPENDS `warningText` to it. **If `sse.go` is absent at
   implementation time, the reader has not run — STOP and flag it; do NOT create
   a competing `sse.go`** (that would collide with the reader's output).
2. **`rewrite.go` exists and emits the literal algorithm note strings** (P1.M2.T1.S1,
   COMPLETE — confirmed on disk). `warningText` consumes `RewriteResult.Notes`
   verbatim; the exact note strings are reproduced in "Input contract" below.
3. **`sse_test.go` exists** (reader). APPEND `TestWarningText_*` to it; do NOT
   overwrite the reader's tests, and do NOT redeclare `const initSSE`
   (already declared in `proxy_test.go` and reused by the reader's `sse_test.go`).

## User Persona

**Target User**: the **next work item** that calls this function —
P1.M3.T3.S2 (injector): when a rewritten `tools/call` result streams back, it
prepends `map[string]string{"type":"text","text":warningText(notes)}` into
`result.content` (PRD §12.2). Plus the **maintainer**, who gets a single audited
place where the agent-facing warning wording lives (easy to change wording without
touching `rewrite.go` or the injector).

**Use Case**: `text := warningText(res.Notes)` → inject as the first `content`
element of the rewritten `tools/call` result. Only called when
`res.Changed == true` (⟹ `len(res.Notes) >= 1`).

**User Journey**: implementer reads this PRP → appends `warningText` to `sse.go`
→ appends `TestWarningText_Table` to `sse_test.go` → `go test -run TestWarningText
-v` → green → P1.M3.T3.S2 imports/uses `warningText`.

**Pain Points Addressed**: (1) the suffix rule has two branches that are easy to
get backwards (all-ignored ⇒ "avoid this notice", else ⇒ "in future calls") — a
byte-exact table test pins both. (2) The notes are pre-formatted by `Rewrite`;
naively re-quoting them would double-quote. This PRP makes clear the notes are
joined VERBATIM. (3) "ignored" detection could be fragile if done by substring;
the literal-prefix approach is exact.

## Why

- **PRD §12.3 (Warning text format)**: defines the envelope, the join separator,
  and the two-branch suffix rule this item implements.
- **PRD §12.2 (Inject)**: the consumer — `warningText(notes)` is the `text` of the
  prepended content element.
- **PRD §19.2 (sse_test.go)**: names the warning-text test cases this item owns.
- **Decouples formatting from injection.** `warningText` is a pure, table-testable
  unit before the injector (P1.M3.T3.S2) and the proxy wiring (P1.M4.T2) exist.
- **Coherence across the chain.** `rewrite.go` (DONE) emits the literal §10
  algorithm strings; this item joins them into the §12.3 line; the injector
  prepends that line. Each stage's contract is fixed so the others don't change.

## What

`warningText` appended to `sse.go`. Visible behavior: given the note slice from a
`Changed` `RewriteResult`, returns the single-line PRD §12.3 warning with the
correct suffix. Empty input returns `""`.

### Success Criteria

- [ ] `func warningText(notes []string) string` exists in `sse.go`, unexported.
- [ ] `{"query":"x"}` notes (renamed only) →
      `[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`
- [ ] `{"query":"x","q":"y"}` notes (renamed + dropped) →
      `[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query"; dropped redundant "q". Use "search_query" in future calls.`
- [ ] `{"query":"x","search_query":"y"}` notes (ignored only) →
      `[web-search-prime-fixer] ignored "query" (use only "search_query"). Use only "search_query" to avoid this notice.`
- [ ] All-ignored with multiple notes → avoid-notice suffix; a single
      renamed-or-dropped note anywhere → future-calls suffix.
- [ ] Empty `notes` → `""`.
- [ ] `go vet ./...` clean; `go test ./...` green (no regression); `go.mod`
      unchanged; no `.go` file other than `sse.go`/`sse_test.go` edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge of this codebase can implement from this
PRP alone because: (a) the exact input note strings are reproduced from
`rewrite.go` verbatim (Input contract); (b) the exact output strings are
reproduced from PRD §12.3 byte-for-byte (with a per-example assembly trace); (c)
the suffix rule is stated as a two-branch decision with the exact classifier
(`strings.HasPrefix(note, "ignored ")`) and why it is unambiguous; (d) the
reference implementation is given; (e) the test table (input → expected → PRD §)
is enumerated with literal expected strings; (f) codebase conventions (package,
imports, table-test style, the existing `initSSE` constant, append-don't-overwrite)
are captured; (g) the placement (APPEND to `sse.go`/`sse_test.go`) and the
hard prerequisite (reader must have run) are explicit.

### Documentation & References

```yaml
# MUST READ — the format this item implements.
- file: PRD.md
  section: "§12.3 Warning text format" + "§12.2 Inject"
  why: the envelope `[web-search-prime-fixer] <notes>. <suffix>`, the join "; ",
        and the two-branch suffix rule (all-ignored ⇒ avoid-notice; else ⇒ future-calls).
  critical: §12.3 "The `Use \"search_query\" in future calls.` suffix is OMITTED when
        every note is an 'ignored' note … replaced by: `... Use only \"search_query\" to avoid this notice.`"

# MUST READ — the exact INPUT note strings warningText joins (verbatim).
- file: rewrite.go
  why: the three `fmt.Sprintf` calls that populate `RewriteResult.Notes`. These ARE
        the literal strings; warningText does not re-format them.
  pattern: renamed `"%q is not a valid parameter; renamed to %q"`; dropped
        `"dropped redundant %q"`; ignored `"ignored %q (use only %q)"`.
  critical: the ignored note begins with the literal `ignored ` — that prefix is the
        classifier for the suffix branch.

# The reader that created sse.go + sse_test.go (this item EXTENDS both).
- docfile: plan/001_c0abc3757e9a/P1M3T2S1/PRP.md
  why: defines sse.go (Event/Reader/...) and sse_test.go which this item APPENDS to.
        Confirms sse.go already imports "strings" and that sse_test.go must not
        redeclare `const initSSE`.
  section: "Reference implementation" (imports block) + "Known Gotchas" (initSSE).

# This item's own research — bit-for-bit input/output traces + the classifier proof.
- docfile: plan/001_c0abc3757e9a/P1M3T3S1/research/warning-text-format.md
  why: proves the three PRD §12.3 outputs byte-for-byte from the rewrite.go inputs,
        justifies `strings.HasPrefix(note,"ignored ")`, enumerates edge cases + imports.
  section: "§3 OUTPUT format" + "§2 'ignored' detection".

# The cross-task note-format decision (algorithm strings, not §10 table shorthand).
- docfile: plan/001_c0abc3757e9a/P1M2T1S1/research/notes-and-contract.md
  why: §1 settles that Notes carry the LITERAL algorithm strings; §4 the consumer
        seam contract (warningText joins with "; ", invoked only when Changed==true).
  section: "§1 Note strings" + "§4 Consumer seam contract".

# CODEBASE CONVENTIONS — follow these patterns.
- file: rewrite_test.go
  why: table-driven test style — `package main`, `tests := []struct{...}{...}`,
        `t.Run(tc.name, ...)`, per-case comments citing PRD §.
  pattern: table of name/in/want rows; compare with `got != tc.want` (strings are
        ==-comparable) or reflect.DeepEqual.

- file: proxy_test.go
  why: defines `const initSSE` reused by sse_test.go.
  pattern: DO NOT redeclare initSSE in sse_test.go (duplicate package-level name
        is a compile error). warningText tests do not need initSSE anyway.

- url: https://pkg.go.dev/strings#HasPrefix
  why: the classifier — `strings.HasPrefix(note, "ignored ")` selects the suffix branch.
  critical: include the trailing space in the prefix (`"ignored "`, not `"ignored"`)
        so a hypothetical note like "ignored-something" would not misclassify.

- url: https://pkg.go.dev/strings#Join
  why: `strings.Join(notes, "; ")` — the verbatim join separator from PRD §12.3.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment
  main.go           # bootstrap + logger (P1.M1.T4)
  config.go         # Config + DefaultConfig + LoadConfig (P1.M1.T2)
  proxy.go          # passthrough forward core (P1.M1.T4.S2)  — UNTOUCHED
  rewrite.go        # Rewrite + RewriteResult (P1.M2.T1)      — INPUT SOURCE, UNTOUCHED
  sse.go            # Event/Reader/NewSSEReader/Next (P1.M3.T1.S1 reader) — EXTEND THIS
  sse_test.go       # reader tests (P1.M3.T1.S1)              — EXTEND THIS
  *_test.go         # config/resolve/logger/health/proxy/rewrite tests — UNTOUCHED
  testdata/*.sse    # fixtures (P1.M3.T2.S1)                  — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
sse.go        # EXTEND (append): warningText(notes []string) string + doc comment.
               #   Imports unchanged UNLESS "strings" is somehow absent (it is not).
sse_test.go   # EXTEND (append): TestWarningText_Table (+ optional focused tests).
               #   No new imports unless "testing" absent (it is present).
```

No new files. No other file changes.

### Input contract (the EXACT note strings `Rewrite` produces — join verbatim)

```
renamed : "<chosen>" is not a valid parameter; renamed to "<target>"
dropped : dropped redundant "<alias>"
ignored : ignored "<alias>" (use only "<target>")
```

For target `"search_query"`:
- renamed  → `"query" is not a valid parameter; renamed to "search_query"`
- dropped  → `dropped redundant "q"`
- ignored  → `ignored "query" (use only "search_query")`

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — notes are ALREADY FORMATTED. rewrite.go emits the literal PRD §10
  algorithm strings (NOT the §10 "Examples" table shorthand like `renamed "query"`).
  warningText must join them VERBATIM with "; " — do NOT re-quote, re-case, or
  re-derive wording. Joining the table shorthand would NOT round-trip to the §12.3
  examples (see P1M2T1S1/research/notes-and-contract.md §1).

CRITICAL — the suffix rule is two-branch and easy to invert. ALL notes ignored ⇒
  ` Use only "search_query" to avoid this notice.`; otherwise (≥1 renamed/dropped) ⇒
  ` Use "search_query" in future calls.`. A single non-ignored note flips to the
  default branch. Track allIgnored with an early-break loop: start true, set false
  on the first note NOT HasPrefix("ignored ").

CRITICAL — "ignored" classifier is a LITERAL PREFIX, not a substring. Use
  strings.HasPrefix(note, "ignored ") — note the trailing space. Do NOT use
  Contains(note, "ignored") (a future note could contain the word elsewhere) and
  do NOT compare whole notes (the alias name varies). The other two kinds begin
  with `"` and `dropped ` respectively, so there is no collision.

GOTCHA — assemble as prefix + Join + "." + suffix. The suffix strings EACH begin
  with a single leading space, so `Join + "." + suffix` yields the `. ` gap
  automatically. Do NOT add an extra space. Full:
  "[web-search-prime-fixer] " + strings.Join(notes, "; ") + "." + suffix

GOTCHA — empty notes ⇒ "". Do not emit a malformed "[web-search-prime-fixer] . Use…"
  stub. The injector only calls warningText when Changed==true (Notes non-empty),
  but the empty case is a cheap defensive guard; test it.

GOTCHA — APPEND, do not OVERWRITE. sse.go and sse_test.go are created by P1.M3.T1.S1.
  Add warningText at the END of sse.go; add TestWarningText_* at the END of
  sse_test.go. Do not touch the reader's Event/Reader/Next or its tests.

GOTCHA — do NOT redeclare `const initSSE`. proxy_test.go defines it; the reader's
  sse_test.go reuses it. A second declaration in the same package is a compile
  error. warningText's tests do not need initSSE anyway.

GOTCHA — duplicate import is a Go compile error. sse.go ALREADY imports "strings"
  (reader uses TrimRight/Join/IndexByte/HasPrefix). Verify "strings" is present;
  add it ONLY if genuinely absent. Do not blindly append it.

GOTCHA — keep warningText UNEXPORTED. The injector (P1.M3.T3.S2) is package main
  and calls it directly. An exported WarningText would needlessly widen the API
  and would not match the PRD §12.3 signature `warningText(notes []string) string`.
```

## Implementation Blueprint

### Data models and structure

No new types. `warningText` takes `[]string` (the `RewriteResult.Notes` slice) and
returns `string`. It depends on nothing but the stdlib `"strings"` package.

### Reference implementation (APPEND this to `sse.go`)

```go
// warningText renders RewriteResult.Notes (rewrite.go) as the single-line
// agent-facing SSE warning defined by PRD §12.3. The notes arrive already
// formatted by Rewrite as the literal PRD §10 algorithm strings; warningText
// joins them VERBATIM with "; " and wraps them as:
//
//	[web-search-prime-fixer] <note[0]>; <note[1]>; ... . <suffix>
//
// SUFFIX RULE (PRD §12.3):
//   - If EVERY note is an "ignored" note (Rewrite emitted it because the target
//     was already present), the suffix is:
//     ` Use only "search_query" to avoid this notice.`
//   - Otherwise (at least one renamed or dropped note), the suffix is:
//     ` Use "search_query" in future calls.`
//
// An "ignored" note is one beginning with the literal prefix "ignored " (the only
// one of the three note kinds that does — renamed begins with `"` and dropped
// with `dropped `). An empty notes slice returns "" (nothing to inject); callers
// only invoke warningText with a non-empty Notes from a Changed RewriteResult.
func warningText(notes []string) string {
	if len(notes) == 0 {
		return ""
	}
	allIgnored := true
	for _, n := range notes {
		if !strings.HasPrefix(n, "ignored ") {
			allIgnored = false
			break
		}
	}
	suffix := ` Use "search_query" in future calls.`
	if allIgnored {
		suffix = ` Use only "search_query" to avoid this notice.`
	}
	return "[web-search-prime-fixer] " + strings.Join(notes, "; ") + "." + suffix
}
```

> The reader's `sse.go` already has `import ( "bufio"; "io"; "strings" )`. After
> appending `warningText`, run `gofmt -w sse.go`; if the compiler reports
> `"strings" imported and not used`, the reader's file did NOT import strings —
> add it then. (In the normal case it is already present; do nothing.)

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify sse.go + sse_test.go exist
  - RUN: ls sse.go sse_test.go
  - IF ABSENT: STOP — the reader (P1.M3.T1.S1, plan/.../P1M3T2S1/PRP.md) has not
        run. Do NOT create a competing sse.go (it would collide). Flag the blocker.
  - IF PRESENT: proceed. Confirm "strings" is in sse.go's import block (it is).

Task 1: APPEND warningText to sse.go
  - APPEND the "Reference implementation" block above to the END of sse.go
        (after the reader's reset() helper).
  - KEEP the doc comment — it IS the Mode A deliverable (both suffix rules).
  - VERIFY "strings" is imported; add it ONLY if the compiler complains it is
        absent (do not duplicate an existing import).
  - DO NOT touch Event/Reader/NewSSEReader/Next/dispatch/reset or any other file.

Task 2: APPEND TestWarningText_Table to sse_test.go
  - PACKAGE: main (same package; do not redeclare `const initSSE`).
  - APPEND at the END of sse_test.go, after the reader's tests.
  - IMPORTS: "testing" is already imported by sse_test.go; add "reflect" ONLY if
        you use reflect.DeepEqual (strings compare with ==, so reflect is optional).
  - TESTS: see the test table below — one table-driven test with t.Run subtests,
        each row passing a literal note slice and asserting a byte-exact string.
  - NAMING: TestWarningText_Table with subtests via t.Run(tc.name, ...).
  - COVERAGE: renamed-only, renamed+dropped, ignored-only, multiple-ignored,
        mixed (ignored+renamed), empty.
  - DO NOT call Rewrite (the contract says MOCKING: pure function); pass literal
        note strings matching rewrite.go's algorithm output.

Task 3: VALIDATE
  - gofmt -w sse.go sse_test.go
  - go vet ./...
  - go test -run 'TestWarningText' -v
  - go test ./...
  - ALL green. git diff must show ONLY sse.go + sse_test.go (append-only edits).
```

### Test table (sse_test.go — APPEND)

```go
func TestWarningText_Table(t *testing.T) {
	// Note literals come from rewrite.go's PRD §10 algorithm strings (joined
	// verbatim). PRD §12.3 dictates the envelope + suffix.
	tests := []struct {
		name  string
		notes []string
		want  string
	}{
		{
			// PRD §12.3 example 1: {"query":"x"} -> renamed.
			"renamed_only_future_calls",
			[]string{`"query" is not a valid parameter; renamed to "search_query"`},
			`[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`,
		},
		{
			// PRD §12.3 example 2: {"query":"x","q":"y"} -> renamed + dropped.
			"renamed_and_dropped_future_calls",
			[]string{
				`"query" is not a valid parameter; renamed to "search_query"`,
				`dropped redundant "q"`,
			},
			`[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query"; dropped redundant "q". Use "search_query" in future calls.`,
		},
		{
			// {"query":"x","search_query":"y"} -> target wins, query ignored.
			"ignored_only_avoid_notice",
			[]string{`ignored "query" (use only "search_query")`},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"). Use only "search_query" to avoid this notice.`,
		},
		{
			// Multiple ignored aliases (e.g. {"query":x,"q":y,"search_query":z})
			// -> still all-ignored -> avoid-notice suffix.
			"multiple_ignored_avoid_notice",
			[]string{
				`ignored "query" (use only "search_query")`,
				`ignored "q" (use only "search_query")`,
			},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"); ignored "q" (use only "search_query"). Use only "search_query" to avoid this notice.`,
		},
		{
			// Mixed: one ignored + one renamed -> NOT all-ignored -> future-calls.
			"mixed_ignored_and_renamed_future_calls",
			[]string{
				`ignored "query" (use only "search_query")`,
				`"search" is not a valid parameter; renamed to "search_query"`,
			},
			`[web-search-prime-fixer] ignored "query" (use only "search_query"); "search" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`,
		},
		{
			// Defensive: empty notes -> "" (injector only calls with non-empty,
			// but guard against a malformed "[web-search-prime-fixer] . Use…" stub).
			"empty_returns_empty",
			nil,
			``,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := warningText(tc.notes)
			if got != tc.want {
				t.Errorf("warningText(%#v):\n got %q\nwant %q", tc.notes, got, tc.want)
			}
		})
	}
}
```

### Implementation Patterns & Key Details

```go
// PATTERN: pure formatter — no I/O, no globals, deterministic. Same shape as a
// helper in rewrite.go (guard-clauses-first, literal-string constants inline).

// PATTERN (suffix branch): track allIgnored with an early-break loop. Do NOT
// compute "is every note ignored?" by counting or by Contains — a single
// HasPrefix("ignored ") check per note, break on the first miss, is exact.
allIgnored := true
for _, n := range notes {
	if !strings.HasPrefix(n, "ignored ") {
		allIgnored = false
		break
	}
}

// PATTERN (assembly): the suffix consts each begin with ONE leading space, so
// Join + "." + suffix yields the ". " gap with no extra concatenation.
//   "[web-search-prime-fixer] " + strings.Join(notes, "; ") + "." + suffix
// Trace (renamed only):
//   "...renamed to \"search_query\"" + "." + " Use \"search_query\" in future calls."
//   = "...renamed to \"search_query\". Use \"search_query\" in future calls."  ✓

// GOTCHA (restated): the "ignored " prefix includes the trailing space.
// HasPrefix(n, "ignored") (no space) would misclassify a hypothetical
// "ignored-something" note; the space makes the match exact to the §10 string.
```

### Integration Points

```yaml
FILES MODIFIED (append-only):
  - sse.go        (EXTEND: + warningText + doc comment; imports unchanged normally)
  - sse_test.go   (EXTEND: + TestWarningText_Table; imports unchanged normally)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only — needs only "strings", already present).
  - rewrite.go: the INPUT source (RewriteResult.Notes). Read its note strings; do not edit.
  - proxy.go / main.go / config.go / doc.go / *_test.go: zero edits.
  - testdata/*.sse: zero edits.
CONSUMER SEAM (this function feeds — keep the signature stable):
  - P1.M3.T3.S2 (injector): `text := warningText(res.Notes)` then prepend
        {"type":"text","text":text} into result.content (PRD §12.2). Depends on
        warningText([]string) string returning the exact §12.3 line + "" for empty.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# Format + vet (and confirm append-only — no collateral edits).
gofmt -w sse.go sse_test.go
go vet ./...
git diff --stat           # expect: sse.go + sse_test.go ONLY
git diff go.mod           # expect: EMPTY (zero new requires)

# Expected: gofmt produces no diffs; vet clean; only sse.go + sse_test.go changed;
# go.mod unchanged. If vet reports a duplicate import of "strings", REMOVE your
# added import (it already existed) and re-run.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Targeted: run ONLY the warningText tests, verbose.
go test -run 'TestWarningText' -v

# MUST PASS (the three that prove §12.3 fidelity):
#   renamed_only_future_calls          -> §12.3 example 1 byte-for-byte
#   renamed_and_dropped_future_calls   -> §12.3 example 2 byte-for-byte
#   ignored_only_avoid_notice          -> §12.3 ignored + avoid-notice suffix
#   multiple_ignored_avoid_notice      -> all-ignored branch with >1 note
#   mixed_ignored_and_renamed_future_calls -> a single non-ignored note flips branch
#   empty_returns_empty                -> defensive empty guard
# Expected: PASS, exit 0. If a §12.3 example mismatches, diff `got` vs `want`
# character-by-character (the test prints both with %q) — the error is almost
# always a missing/extra space, an inverted suffix, or a re-formatted note.
```

### Level 3: Integration Testing (System Validation)

```bash
# No service to start (pure unit). Confirm the module + full suite stay healthy
# and the package still compiles as `package main` with warningText present.
go build ./...      # must compile (warningText unused by non-test code so far — still must build)
go test ./...       # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . warningText  # sanity: doc comment present and shows both suffix rules (Mode A)

# Expected: build clean; full suite green; `go doc . warningText` prints the doc
# comment documenting the two suffix rules. NOTE: end-to-end injection (warningText
# actually prepended into a streaming tools/call result) is exercised by
# P1.M3.T3.S2 / P1.M4.T2 / P1.M5.T1; this item proves the format in isolation.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Round-trip coherence: feed warningText the EXACT Notes a real Rewrite would
#     produce, and confirm the §12.3 line. This cross-checks the rewrite.go ↔
#     warningText contract without running the proxy:
go test -run 'TestWarningText_Table/renamed_only_future_calls' -v
go test -run 'TestWarningText_Table/renamed_and_dropped_future_calls' -v
go test -run 'TestWarningText_Table/ignored_only_avoid_notice' -v

# (b) Confirm the note strings the table uses match rewrite.go's actual output:
#     grep the three Sprintf formats and compare to the table literals.
grep -n 'is not a valid parameter; renamed to' rewrite.go
grep -n 'dropped redundant' rewrite.go
grep -n 'ignored' rewrite.go
# expect: the three fmt.Sprintf lines whose %q-expanded forms equal the table inputs.

# Expected: all three targeted subtests PASS; the grep lines confirm the table
# inputs are byte-identical to what Rewrite emits (no drift between stages).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `sse.go` + `sse_test.go`; `go.mod` unchanged.
- [ ] Level 2 passes: `go test -run 'TestWarningText' -v` is green, including the
      three §12.3-fidelity cases and the empty/mixed/multiple-ignored edge cases.
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc .
      warningText` shows the suffix-rule doc comment (Mode A).

### Feature Validation

- [ ] `func warningText(notes []string) string` exists (unexported) in `sse.go`.
- [ ] Renamed-only input → `[web-search-prime-fixer] "query" is not a valid
      parameter; renamed to "search_query". Use "search_query" in future calls.`
- [ ] Renamed + dropped input → same prefix, `…; dropped redundant "q". Use
      "search_query" in future calls.`
- [ ] Ignored-only input → `[web-search-prime-fixer] ignored "query" (use only
      "search_query"). Use only "search_query" to avoid this notice.`
- [ ] All-ignored (any count) → avoid-notice suffix; ≥1 non-ignored → future-calls.
- [ ] Empty `notes` → `""`.

### Code Quality Validation

- [ ] `warningText` is APPENDED to `sse.go` (reader code untouched).
- [ ] Tests are APPENDED to `sse_test.go` (reader tests untouched; no redeclared
      `initSSE`).
- [ ] Imports unchanged unless `"strings"`/`"testing"` were genuinely absent (they
      are not); no duplicate imports.
- [ ] Doc comment cites PRD §12.3 and documents BOTH suffix rules (Mode A).
- [ ] Notes joined VERBATIM (no re-formatting); classifier is `HasPrefix("ignored ")`.
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . warningText` prints the two suffix rules.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't re-format the notes. `rewrite.go` emits the literal PRD §10 algorithm
  strings; `warningText` joins them VERBATIM with `"; "`. Re-quoting (e.g.
  wrapping each note in extra quotes) or substituting the §10 table shorthand
  (`renamed "query"`) will NOT reproduce the §12.3 examples. The table tests catch
  this byte-for-byte.
- ❌ Don't invert the suffix rule. ALL notes ignored ⇒ "avoid this notice";
  otherwise ⇒ "in future calls". A single renamed/dropped note forces the
  future-calls branch. Use an `allIgnored` flag that breaks false on the first
  non-ignored note — do not invert the condition.
- ❌ Don't classify "ignored" by `Contains` or by whole-string equality. Use
  `strings.HasPrefix(note, "ignored ")` WITH the trailing space. `Contains` could
  match the word elsewhere; whole-string equality fails because the alias name
  varies. The trailing space makes the prefix exact to the §10 string.
- ❌ Don't add an extra space in assembly. The suffix consts each start with one
  space, so `Join + "." + suffix` already yields `. `. Adding another space
  produces `.  ` (double space) and fails the byte-exact tests.
- ❌ Don't emit a stub for empty notes. Return `""`, not
  `[web-search-prime-fixer] . Use …`. The injector only calls with non-empty
  Notes, but the empty guard is cheap and correct.
- ❌ Don't export `warningText`. It is `package main`-internal; the injector calls
  it directly. An exported `WarningText` widens the API and diverges from the
  PRD §12.3 signature.
- ❌ Don't OVERWRITE `sse.go`/`sse_test.go` or duplicate `const initSSE`. APPEND
  only. The reader owns Event/Reader/Next and its tests; this item owns
  warningText and its table. A duplicate `initSSE` is a compile error.
- ❌ Don't blindly append `"strings"` to the import block. `sse.go` already imports
  it (reader). Verify first; a duplicate import is a compile error. Add only if
  the compiler reports it absent.
- ❌ Don't call `Rewrite` from the tests. The item contract says MOCKING: pure
  function — pass literal note slices matching `rewrite.go`'s output. Coupling the
  test to `Rewrite` would make a `rewrite.go` wording change silently break
  warningText tests (and vice versa); the byte-exact literals pin the seam.
- ❌ Don't modify `rewrite.go`, `go.mod`, `PRD.md`, or any other file. This item
  edits exactly `sse.go` + `sse_test.go` (append-only).
