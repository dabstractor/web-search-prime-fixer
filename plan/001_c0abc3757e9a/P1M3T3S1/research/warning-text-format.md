# Research — P1.M3.T3.S1 warningText(notes) format

Scope: pin the EXACT input strings (from `rewrite.go`), the EXACT output strings
(PRD §12.3), and the suffix rule, so the implementer can write bit-for-bit
assertions. All claims traced to on-disk sources.

## 1. INPUT contract: notes are the LITERAL algorithm strings, not §10 table shorthand

`RewriteResult.Notes` is populated by `rewrite.go` with the **literal algorithm
strings** (PRD §10 steps 4–5), confirmed by
`plan/001_c0abc3757e9a/P1M2T1S1/research/notes-and-contract.md` §1. Verified
verbatim against the on-disk `rewrite.go` `fmt.Sprintf` calls:

| note kind | rewrite.go source line                                   | example note string (target="search_query")            |
|-----------|----------------------------------------------------------|---------------------------------------------------------|
| renamed   | `fmt.Sprintf("%q is not a valid parameter; renamed to %q", chosen, target)` | `"query" is not a valid parameter; renamed to "search_query"` |
| dropped   | `fmt.Sprintf("dropped redundant %q", a)`                 | `dropped redundant "q"`                                 |
| ignored   | `fmt.Sprintf("ignored %q (use only %q)", a, target)`     | `ignored "query" (use only "search_query")`             |

**Decision (warningText does NOT re-format notes):** it joins them VERBATIM with
`"; "`. The notes are already final. `warningText` only adds the prefix, the
trailing period, and the suffix.

## 2. "ignored" detection — unambiguous prefix

A note is an "ignored" note **iff** it begins with the literal `ignored ` (lower-
case, one trailing space). The other two kinds never collide:

- renamed notes begin with `"` (the opening quote of the chosen alias).
- dropped notes begin with `dropped `.
- ignored notes begin with `ignored `.

So `strings.HasPrefix(note, "ignored ")` is the exact classifier. No substring
matching, no parsing — a prefix test on the literal algorithm string.

## 3. OUTPUT format (PRD §12.3) — bit-for-bit trace

Template:

```
[web-search-prime-fixer] <join(notes, "; ")>.<suffix>
```

where `<suffix>` is one of (note each begins with a single space):

- DEFAULT (any note is renamed or dropped):
  ` Use "search_query" in future calls.`
- ALL-IGNORED (every note is an ignored note):
  ` Use only "search_query" to avoid this notice.`

Assembly: `"[web-search-prime-fixer] " + strings.Join(notes, "; ") + "." + suffix`
→ the `"."` abuts the last note, and the leading space of `suffix` gives the
`. ` gap. Verified against all three PRD §12.3 examples:

### Example 1 — `{"query":"x"}` (renamed only)

notes = `[ "\"query\" is not a valid parameter; renamed to \"search_query\"" ]`
allIgnored = false → suffix = ` Use "search_query" in future calls.`

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
```
✓ matches PRD §12.3 example 1 byte-for-byte.

### Example 2 — `{"query":"x","q":"y"}` (renamed + dropped)

notes = `[ "\"query\" is not a valid parameter; renamed to \"search_query\"",
           "dropped redundant \"q\"" ]`
join = `"query" is not a valid parameter; renamed to "search_query"; dropped redundant "q"`
allIgnored = false → suffix = ` Use "search_query" in future calls.`

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query"; dropped redundant "q". Use "search_query" in future calls.
```
✓ matches PRD §12.3 example 2 byte-for-byte.

### Example 3 — `{"query":"x","search_query":"y"}` (ignored only)

notes = `[ "ignored \"query\" (use only \"search_query\")" ]`
allIgnored = true → suffix = ` Use only "search_query" to avoid this notice.`

```
[web-search-prime-fixer] ignored "query" (use only "search_query"). Use only "search_query" to avoid this notice.
```
✓ matches the item's third contract case (ignored + avoid-notice suffix).

## 4. Edge cases

- **Empty notes** → `""`. The injector (P1.M3.T3.S2) only calls `warningText`
  when `RewriteResult.Changed==true` (⟹ `len(Notes)>=1`, invariant on
  `rewrite.go`'s `RewriteResult` doc comment). Empty is defensive; returning `""`
  means "no warning to inject" rather than emitting a malformed
  `[web-search-prime-fixer] . Use …` stub.
- **Single ignored note** → avoid-notice suffix (Example 3).
- **Mixed ignored + renamed/dropped** → DEFAULT suffix (a single non-ignored
  note flips `allIgnored` to false).
- **Multiple ignored** → avoid-notice suffix (all-ignored branch).

## 5. Imports needed

`warningText` needs only the stdlib `"strings"` (`strings.Join`, `strings.HasPrefix`).
`sse.go` (created by P1.M3.T1.S1 reader) ALREADY imports `"strings"` (for
`strings.TrimRight`/`Join`/`IndexByte`/`HasPrefix`), so NO import change is
required in the normal case. GOTCHA: do NOT blindly append `"strings"` to the
import block — a duplicate import is a Go compile error. Verify it is present;
add it ONLY if genuinely absent.

## 6. Placement / coordination contract (cross-task)

- `warningText` is **unexported** (`func warningText(...)`) — the injector
  (P1.M3.T3.S2) lives in the SAME `package main` and calls it directly.
- It is **APPENDED to `sse.go`**, which is created by P1.M3.T1.S1 (reader, at
  `plan/.../P1M3T2S1/PRP.md`). That PRP explicitly carves out the extension
  point: "sse.go is READER-ONLY in this item … inject/warningText are P1.M3.T3,
  which EXTENDS sse.go."
- Tests are **APPENDED to `sse_test.go`** (same package). MUST NOT redeclare
  `const initSSE` (defined in `proxy_test.go`, reused by the reader's
  `sse_test.go`). Use uniquely-named test functions: `TestWarningText_*`.
- `go.mod` gains ZERO `require`s (stdlib only). No other `.go` file is modified.
- The reader's `Event{ID,Type,Data string}` type is NOT needed by `warningText`
  (it takes `[]string`, not an `Event`) — pure, no SSE dependency.

## 7. Test conventions (pinned from rewrite_test.go / config_test.go)

- `package main`, file `sse_test.go` (APPEND, do not overwrite).
- Table-driven: `tests := []struct{ name string; notes []string; want string }{...}`
  over `for _, tc := range tests { t.Run(tc.name, func(t *testing.T){...}) }`.
- Strings are `==`-comparable (no `reflect.DeepEqual` needed for a single string,
  but `reflect.DeepEqual(got, tc.want)` is also fine and matches the sibling
  tests' style). Prefer `got != tc.want` + `t.Errorf("got %q\nwant %q", got, tc.want)`.
- Per-case comments citing PRD § (§12.3).
- Pass literal note slices (matching the `rewrite.go` algorithm strings) — DO NOT
  call `Rewrite` (the item contract says MOCKING: pure function).
