name: "P1.M3.T1.S1 — Canonical-pair detection + warning text generation (PRD §12.1, §12.2)"
description: |

  CREATE `teach.go` (`package main`, imports ONLY `"fmt"`) with THREE pure functions
  and CREATE `teach_test.go` for them. No MCP/SDK types, no I/O, no globals — these
  are the pure decision + text layer of the teaching signal (PRD §12).
  (1) `shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam
  string) bool` — the AFTER-RESULTS warning predicate (PRD §12.1). Returns false ONLY for
  the canonical call: calledTool==canonicalTool AND result.Source==canonicalParam AND no
  normalized optionals. Returns true for any wrong tool, any non-canonical Source
  (alias/scalar/nested/array/inferred/bare-string), or len(result.Optionals)>0.
  (2) `warningText(calledTool, source, canonicalTool, canonicalParam string) string` — the
  appended-after-results warning, byte-identical to PRD §12.2 (incl. the EM DASH U+2014).
  (3) `noQueryWarningText(canonicalTool, canonicalParam string) string` — the immediate
  no-results warning (PRD §12.2), byte-identical. INPUT: ExtractionResult (Query/Source/
  Ambiguous/Optionals/Found — frozen by P1.M2.T1) + canonical strings from Config
  (CanonicalTool="web_search", CanonicalParam="query"). teach functions take plain params
  (no Config import — pure, like extract.go). OUTPUT consumed by P1.M3.T1.S2 (append
  logic) + P1.M5.T2 (server dispatch). DOCS: Mode-A doc comments on all three (canonical-
  pair rule, exact text format, Source reflects ambiguous/inferred labels). go.mod gains
  zero requires.

---

## Goal

**Feature Goal**: Ship the pure decision + text layer of the teaching signal (PRD §12).
After P1.M2.T1 built extraction (`ExtractionResult`), this item answers the two
teaching questions the server must answer on every `tools/call`: (a) given a
successful extraction, was this the canonical call (no warning) or not (append a
warning)? and (b) what exact warning text do we emit — appended after results, or
immediate when there was no query at all? The three functions are deterministic,
side-effect-free, and fully table-tested against PRD §12.1/§12.2 byte-for-byte.

**Deliverable**: TWO NEW files (no modifications to existing files):
1. **CREATE** `teach.go` — `package main`, `import "fmt"`, three exported functions
   (`shouldWarn`, `warningText`, `noQueryWarningText`) each with a Mode-A doc comment.
2. **CREATE** `teach_test.go` — `package main`, `import ("reflect"; "testing")`,
   table-driven `TestShouldWarn`, `TestWarningText`, `TestNoQueryWarningText`.

No existing `.go` file is touched. `go.mod` gains zero `require`s (only stdlib `fmt`).
No README/config/doc.go change (Mode A docs = doc comments only).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run 'TestShouldWarn|TestWarningText|TestNoQueryWarningText' -v`)
all exit clean. `shouldWarn` returns false ONLY for the canonical triple
(canonicalTool + Source==canonicalParam + no optionals) and true otherwise (verified
by a truth table covering wrong tool, every non-canonical Source label, and the
optionals trigger). `warningText`/`noQueryWarningText` produce byte-identical output
to PRD §12.2 (EM DASH U+2014 preserved) for the default and custom canonical values.
`teach.go` imports only `"fmt"`; `git diff --stat go.mod` is empty.

## User Persona

**Target User**: (1) **P1.M3.T1.S2** (the append-logic sibling), which assembles the
dispatch: `!Found` → `noQueryWarningText` immediate (FR-6, no upstream call);
`Found && shouldWarn(...)` → append `warningText(...)` after the result content;
`Found && !shouldWarn` → no warning; `isError` always false. S1's exact byte format
and boolean semantics ARE the contract S2 assembles. (2) **P1.M5.T2** (server
dispatch), which passes `cfg.CanonicalTool`/`cfg.CanonicalParam` to these functions.
(3) **P1.M5.T3** (server_test.go e2e), whose §19.3 cases assert the warning text the
client sees — S1's bytes are what those e2e tests compare against. (4) the agent,
who gets a correct, copy-pasteable usage example when it strays from canonical.

**Use Case**: Agent calls `web_search` with `{"q": "rust async runtime"}`. Extraction
yields `{Query:"rust async runtime", Source:"q", Found:true}`. The server runs the
search, then `shouldWarn("web_search", result, "web_search", "query")` returns true
(Source "q" != canonicalParam "query"), so `warningText("web_search", "q", "web_search",
"query")` is appended after the results — teaching the agent to use `query` next time,
with a concrete example. If instead the agent sent `{}` (no query), extraction is
`Found==false`, the server makes NO upstream call, and returns
`noQueryWarningText("web_search","query")` immediately.

**User Journey**: tools/call → extract → (Found gate in caller) → for Found, run
upstream → `shouldWarn(...)` → if true, `warningText(...)` appended after result
content blocks; for !Found, `noQueryWarningText(...)` returned with no results. The
warning text always ends in the literal example `web_search({ "query": "rust async
runtime" })` so the agent can copy the exact canonical form.

**Pain Points Addressed**: (1) Without a single source of truth for the warning
decision, every call site would re-derive "is this canonical?" inconsistently.
`shouldWarn` encodes PRD §12.1 once. (2) Without byte-faithful warning text, the
teaching signal drifts from the PRD's vetted wording; a frozen `warningText`/
`noQueryWarningText` keeps it exact (incl. the em dash and the concrete example).
(3) The "normalized optionals → warn" rule (PRD §12.1) is easy to forget;
`shouldWarn` makes it an explicit, tested branch.

## Why

- Implements **PRD §12.1** (when a warning is added) and **§12.2** (warning text with
  example) as pure functions, and the S1-owned half of **§19.2** (teach_test.go:
  "no warning for canonical; ... example text present and correct; the immediate
  no-results warning").
- Supports **FR-6** (teaching signal appended after results, never instead of them;
  the immediate no-results warning is the one exception). `noQueryWarningText` is the
  text for that exception; `shouldWarn` is the append predicate; the Found gate that
  chooses between them lives in the caller (S2 / P1.M5.T2), per **§12.3**.
- **Decouples the decision from the dispatch.** The server/append logic (S2, M5) can
  call three trivially-testable pure functions instead of embedding string templates
  and boolean logic in the MCP handler.
- **Freezes the teaching-signal seam** (signatures + exact text) so S2, M5.T2, and
  the e2e suite all assert against one frozen format. `ExtractionResult` is already
  frozen (P1.M2.T1); this item adds the teach seam without touching extraction.

## What

Three exported functions in a new `teach.go` (`package main`, `import "fmt"`):

1. **`shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam string) bool`**
   — the after-results warning predicate. Reads `result.Source` and
   `result.Optionals` (NOT `Found` — the caller gates on Found first, per §12.3).
   Returns `false` ONLY when `calledTool == canonicalTool` AND
   `result.Source == canonicalParam` AND `len(result.Optionals) == 0`. Returns
   `true` for: any non-canonical `calledTool`; any `result.Source` other than
   `canonicalParam` (alias key like `"q"`, `"scalar"`, `"nested:..."`, `"array[0]"`,
   `"inferred:..."`, `"bare-string"`); OR `len(result.Optionals) > 0` (normalized
   optionals were forwarded — a warning trigger per PRD §12.1 even when tool+source
   are canonical).

2. **`warningText(calledTool, source, canonicalTool, canonicalParam string) string`**
   — the appended-after-results warning, byte-identical to PRD §12.2. Built with
   `fmt.Sprintf` and literal `"` characters (NOT `%q`). Contains the EM DASH `—`
   (U+2014). `<tool>`=`calledTool`, `<source>`=`source`, and the example's
   `web_search`/`query`/`"rust async runtime"` come from `canonicalTool`/
   `canonicalParam`/a fixed literal respectively.

3. **`noQueryWarningText(canonicalTool, canonicalParam string) string`**
   — the immediate no-results warning (PRD §12.2), byte-identical, same em-dash
   and literal-quote construction.

`teach_test.go` table-tests all three: a `shouldWarn` truth table (canonical, wrong
tool, every non-canonical Source label, the optionals trigger, custom canonical),
and exact-byte assertions for `warningText`/`noQueryWarningText` (default + custom
canonical values, incl. an inferred Source).

### Success Criteria

- [ ] `teach.go` exists, `package main`, imports ONLY `"fmt"`, defines the three
      exported functions with Mode-A doc comments.
- [ ] `shouldWarn` returns `false` ONLY for canonicalTool + Source==canonicalParam +
      no optionals; `true` for wrong tool, any non-canonical Source, and
      len(Optionals)>0.
- [ ] `warningText` / `noQueryWarningText` are byte-identical to PRD §12.2 (EM DASH
      U+2014 preserved) for default and custom canonical values.
- [ ] `teach_test.go` exists with `TestShouldWarn` (truth table), `TestWarningText`
      (exact bytes), `TestNoQueryWarningText` (exact bytes).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat` shows ONLY the two new files; `go.mod` unchanged.

## All Needed Context

### Context Completeness Check

_Pass._ The deliverable is fully pinned: (a) `ExtractionResult` is read from the
on-disk `extract.go` (frozen by P1.M2.T1): fields `Query/Source/Ambiguous/Optionals/
Found` and the full Source-label enumeration; (b) PRD §12.1/§12.2 give the exact
decision rule and the exact warning text (em dash confirmed U+2014); (c) the item
description pins the exact signatures and the `shouldWarn` logic (Source + Optionals,
NOT Found/Ambiguous); (d) `config.go` pins `CanonicalTool`="web_search" /
`CanonicalParam`="query"; (e) this PRP's research (`teach-signal-contract.md`)
**validates** both warning strings byte-for-byte against PRD §12.2 in a throwaway Go
program and lists the exact expected `want` strings for every test row. The one
non-obvious trap — the EM DASH must be U+2014, not a hyphen, and a byte-exact test
catches a wrong dash — is flagged in Known Gotchas. An agent with no prior knowledge
can implement this from the PRP + the on-disk `extract.go` alone.

### Documentation & References

```yaml
# MUST READ — the decision rule and the exact text this item reproduces.
- file: PRD.md
  section: "§12.1 When a warning is added" + "§12.2 Warning text (with example)"
  why: §12.1 is the shouldWarn truth table (no warning iff canonical; warn on wrong
        tool/param/nested/inferred/bare-string/array/optionals; immediate no-results
        only on extraction failure). §12.2 is the byte-exact warning text (both the
        after-results and the no-results forms), incl. the EM DASH and the fixed
        example string "rust async runtime".
  critical: the §12.2 code blocks are LITERAL — reproduce them verbatim with the four
        substitutions (<tool>, <source>, canonicalTool, canonicalParam). The dash
        between `"..." }` and `e.g.` is an EM DASH (U+2014), NOT a hyphen.

- file: PRD.md
  section: "§12.3 Why appended, and why results must accompany the warning"
  why: explains the dispatch boundary shouldWarn depends on — the immediate
        no-results warning (noQueryWarningText) is the ONLY note that travels without
        results; everything else is appended AFTER results. This is why shouldWarn is
        the AFTER-RESULTS predicate and the Found gate is the caller's job.
  critical: do NOT make shouldWarn also handle the no-query case — that is
        noQueryWarningText's path, selected by the caller's Found gate.

- file: PRD.md
  section: "§9.4 Canonical surface" + "FR-6 Teaching signal"
  why: §9.4 fixes the canonical pair the functions compare against (tool web_search,
        param query); FR-6 fixes "appended after results, never instead of them" and
        "isError never set" (the isError part is S2's, but the text/decision is S1's).

- file: PRD.md
  section: "§19.2 teach_test.go"
  why: the test spec. S1 owns the assertions that map to the pure functions: "no
        warning for canonical" (shouldWarn false), "warning ... for every other case"
        (shouldWarn true), "the immediate no-results warning" (noQueryWarningText),
        "example text present and correct" (exact bytes). The "appended after results"
        + "isError never set" assertions are S2's append-logic tests.

# MUST READ — the struct this item consumes (frozen).
- file: extract.go
  why: defines ExtractionResult EXACTLY as teach.go reads it: Source (string — the
        canonical-pair key; non-canonical labels listed in the doc comment), Optionals
        (map[string]any — len()>0 is a warning trigger), plus Query/Ambiguous/Found
        (Found/Ambiguous are NOT read by shouldWarn; see research §2). teach.go and
        extract.go are the same package (main), so no import is needed.
  pattern: pure-function style (plain params, no Config import, no I/O); doc comments
        citing PRD sections; gofmt-clean. teach.go mirrors this discipline.
  gotcha: Source has MANY possible values (bare-string/scalar/array[0]/<alias>/
        nested:<alias>/inferred:<path>/""). ONLY the bare canonical alias key
        (default "query") equals canonicalParam. Every other value -> warn.

# MUST READ — the test-file analog to mirror.
- file: extract_test.go
  why: the project's test conventions for a pure-function module: package main;
        import reflect+testing; table-driven `tests := []struct{...}` + `t.Run`;
        reflect.DeepEqual on whole values; LOCAL decoupling constants that MIRROR
        DefaultConfig (extractQA/extractOpt) "so the test stays decoupled from
        config.go". teach_test.go follows the same decoupling (local teachCanonTool/
        teachCanonParam mirroring DefaultConfig's CanonicalTool/CanonicalParam).
  pattern: `t.Errorf("got = ...\nwant ...")` message style; one table per concern.

# VERIFIED DESIGN — the exact logic + the validated want strings.
- docfile: plan/002_0a8ab3410994/P1M3T1S1/research/teach-signal-contract.md
  why: §1 the ExtractionResult contract + Source-label enumeration; §2 the shouldWarn
        logic + the Found-boundary resolution (caller gates; shouldWarn reads only
        Source+Optionals); §3 the VALIDATED warningText/noQueryWarningText bytes (em
        dash confirmed) + why literal-quotes+%s not %q; §4 Config fields + pure design;
        §5 file scope (teach.go + teach_test.go; S2 extends later); §6 test conventions;
        §7 consumer map.

# CONFIG (read-only) — the canonical values the functions are called with.
- file: config.go
  why: shows DefaultConfig().CanonicalTool == "web_search" and CanonicalParam ==
        "query" (the values teach_test.go's local constants mirror, and the real
        caller passes). teach.go does NOT import config (pure; plain params).

# CONTRACTS (read-only — do not break):
- file: plan/002_0a8ab3410994/P1M2T1S3/PRP.md
  why: finalizes the extract module and ExtractionResult. Its consumer note
        ("P1.M3.T1.S1 reads Source/Ambiguous/Found") was SPECULATIVE; the AUTHORITATIVE
        item spec reads Source + Optionals only. Follow the item spec.
        ExtractionResult's fields are frozen — teach.go must not change them.
- file: plan/002_0a8ab3410994/P1M1T3S1/PRP.md
  why: owns config_test.go/resolve_test.go/health_test.go — DO NOT TOUCH. teach.go
        shares no symbols with them; both keep `go test ./...` green.

# CONSUMERS (forward references — what S1 must produce):
- file: plan/002_0a8ab3410994/prd_snapshot.md
  section: "§11.3" (result handling), "§12.3" (append-after-results dispatch),
        "§19.3" (server_test e2e cases 1-4 assert the warning text).
  why: pins the dispatch S2/M5.T2 build on top of S1's three functions, and the e2e
        assertions that compare against S1's exact bytes.

# Go stdlib (stable).
- url: https://pkg.go.dev/fmt#Sprintf
  why: builds the warning strings with ordered %s args + literal quote characters.
```

### Current Codebase tree (the INPUT state — run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap — UNTOUCHED
  config.go       # Config{CanonicalTool,CanonicalParam,...} — UNTOUCHED (read-only ref)
  logger.go, health.go — UNTOUCHED
  extract.go      # ExtractionResult + extract/extractValue (P1.M2.T1, COMPLETE) — UNTOUCHED
  extract_test.go # extract tests — UNTOUCHED (the test-style analog to mirror)
  config_test.go, resolve_test.go, health_test.go — owned by P1.M1.T3.S1 — DO NOT TOUCH
  logger_test.go  # green — UNTOUCHED
  testdata/, README.md, config.example.json, PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
teach.go          # NEW. package main; import "fmt". Three exported pure functions:
                  #   shouldWarn(...) bool            — canonical-pair predicate (PRD §12.1)
                  #   warningText(...) string          — appended-after-results warning (§12.2)
                  #   noQueryWarningText(...) string   — immediate no-results warning (§12.2)
                  #   Each with a Mode-A doc comment. No Config import; no I/O; no SDK types.
teach_test.go     # NEW. package main; import reflect+testing. Table-driven:
                  #   TestShouldWarn (truth table), TestWarningText (exact bytes),
                  #   TestNoQueryWarningText (exact bytes). Local teachCanonTool/Param
                  #   constants mirroring DefaultConfig (decoupling convention).
# NO other files created or modified. go.mod unchanged.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL — EM DASH (U+2014), not a hyphen. Both PRD §12.2 warning templates contain
// "—" (UTF-8 E2 80 94) between `"..." }` and `e.g.`. Do NOT use "-" or "--". Copy the
// character from the PRD. A byte-exact test (== / reflect.DeepEqual on the string)
// catches a wrong dash instantly. (Validated: present at byte 168 in default warningText.)

// CRITICAL — literal `"` + %s, NOT %q. Build the strings with fmt.Sprintf using literal
// double-quote characters in the format string (e.g. `used "%s"/"%s"`), so the format
// string looks identical to the PRD block and the output is unambiguous. %q would also
// work for these simple identifiers but escapes unusually-named values differently than
// the PRD; literal quotes remove all ambiguity.

// CRITICAL — shouldWarn reads ONLY result.Source and result.Optionals, NOT Found or
// Ambiguous. The item spec is explicit. The caller (S2 / P1.M5.T2) gates on Found FIRST
// (PRD §12.3): !Found -> noQueryWarningText (immediate, no upstream); Found -> run search
// -> shouldWarn decides the after-results warning. Do NOT add a Found check inside
// shouldWarn (it would mask caller bugs and deviate from spec). Ambiguity is already
// encoded in the Source label (inferred:<path>), so Ambiguous is not needed.

// CRITICAL — "normalized optionals" is a warning trigger EVEN WHEN tool+source are
// canonical (PRD §12.1: "or normalized optionals"). shouldWarn("web_search",
// {Source:"query", Optionals:{location:"US"}}, "web_search","query") must return TRUE.
// len(Optionals)>0 handles both nil and empty-non-nil maps (len==0 in both -> no trigger).

// CRITICAL — the ONE canonical Source value is the bare canonical alias key (default
// "query"). Every other Source label warns: "q"/"search" (alias), "scalar", "bare-string",
// "array[0]", "nested:query", "inferred:messages[0].content", "". Source=="" (a Found==false
// result) is != canonicalParam -> shouldWarn returns true, but the caller never reaches
// shouldWarn for !Found (it takes the noQueryWarningText path). Document this boundary.

// GOTCHA — the example string "rust async runtime" is a FIXED LITERAL in both warnings,
// NOT a parameter. Only <tool>, <source>, canonicalTool, canonicalParam are substituted.

// GOTCHA — do NOT import config in teach.go. The functions take canonicalTool/canonicalParam
// as plain string params (pure, unit-testable without config) — exactly like extract.go
// takes queryAliases/optionalAliases as plain params. The real caller passes
// cfg.CanonicalTool/cfg.CanonicalParam.

// GOTCHA — keep teach.go and teach_test.go gofmt-clean. The doc comments are multi-line
// (Mode A) but must be valid Go comment blocks (// per line). The format-string literals
// are raw string literals (backticks) so the literal " and the em dash need no escaping.

// GOTCHA — teach.go shares package main with extract.go/config.go; ExtractionResult needs
// NO import (same package). The only import is "fmt".

// GOTCHA — S1 owns ONLY the three pure functions + their unit tests. Do NOT build the
// append-after-results / MCP content-block logic here (that is P1.M3.T1.S2). Do NOT set
// isError anywhere (S2). teach.go must compile and test green with zero MCP/SDK types.
```

## Implementation Blueprint

### Data models and structure

No data model is introduced. `ExtractionResult` (P1.M2.T1) is consumed read-only. The
three functions are pure: `shouldWarn` is comparisons; `warningText`/`noQueryWarningText`
are `fmt.Sprintf` over plain strings.

### The exact function bodies (copy verbatim)

```go
package main

import "fmt"

// shouldWarn is the AFTER-RESULTS teaching-warning predicate (PRD §12.1). It reports
// whether the warning produced by warningText must be appended after a successful
// (Found==true) search's results.
//
// It returns false ONLY for the canonical call: the agent invoked the canonical tool
// name (calledTool == canonicalTool) AND the query came from the canonical parameter
// key (result.Source == canonicalParam) AND no optional parameters were normalized
// and forwarded (len(result.Optionals) == 0). Every other case returns true: a wrong
// tool name; any non-canonical Source (an alias key like "q", "scalar", "bare-string",
// "array[0]", "nested:<key>", or "inferred:<path>"); OR normalized optionals present
// (PRD §12.1 lists "normalized optionals" as a warning trigger even when the tool and
// source are canonical).
//
// shouldWarn reads result.Source and result.Optionals only. It is the AFTER-RESULTS
// predicate: the caller gates on result.Found first (PRD §12.3) — a Found==false result
// takes the immediate no-results warning (noQueryWarningText, §10.1.5) and does NOT
// call shouldWarn. Source reflects how the query was extracted (including
// inferred:<path> for ambiguous/inferred extraction, §12.2) so the agent can confirm
// it searched the right thing; Ambiguity is encoded in Source and is not read here.
func shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam string) bool {
	if len(result.Optionals) > 0 {
		return true
	}
	if calledTool != canonicalTool {
		return true
	}
	if result.Source != canonicalParam {
		return true
	}
	return false
}

// warningText returns the warning appended AFTER successful results (PRD §12.2). It is
// a single line: a notice naming the tool/source actually used, followed by a concrete
// correct-usage example built from the canonical tool and parameter. The format is
// byte-fixed (including the EM DASH before "e.g.") and must match PRD §12.2 exactly.
//
// source is the extraction Source label (e.g. "q", "scalar", "nested:input",
// "inferred:messages[0].content"); when extraction was ambiguous or inferred it
// reflects that so the agent can confirm it searched the right thing (§12.2). The
// example query "rust async runtime" is a fixed literal, not a parameter.
func warningText(calledTool string, source string, canonicalTool, canonicalParam string) string {
	return fmt.Sprintf(
		`[web-search-prime-fixer] Warning: this call used "%s"/"%s" rather than the canonical form. Results are above. Next time call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
		calledTool, source, canonicalTool, canonicalParam, canonicalTool, canonicalParam,
	)
}

// noQueryWarningText returns the IMMEDIATE warning returned when extraction found no
// usable query (PRD §12.2, §10.1.5): no upstream call is made and there are no results
// to append after. The format is byte-fixed (including the EM DASH) and matches PRD
// §12.2 exactly. The example query "rust async runtime" is a fixed literal.
func noQueryWarningText(canonicalTool, canonicalParam string) string {
	return fmt.Sprintf(
		`[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
		canonicalTool, canonicalParam, canonicalTool, canonicalParam,
	)
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: CREATE teach.go — package, import, and the three functions
  - FILE: teach.go (NEW). `package main`; `import "fmt"` (the ONLY import).
  - PASTE the three function bodies above verbatim (shouldWarn / warningText /
        noQueryWarningText), each WITH its Mode-A doc comment.
  - CONSTRAINT: literal `"` + %s in the Sprintf format strings (NOT %q); preserve the
        EM DASH — (U+2014) copied from the PRD; "rust async runtime" is a fixed literal.
  - CONSTRAINT: shouldWarn reads result.Source + result.Optionals ONLY (no Found, no
        Ambiguous). Do NOT import config; canonicalTool/canonicalParam are plain params.
  - PLACEMENT: repo root alongside extract.go (package main).

Task 2: CREATE teach_test.go — local constants + three table-driven tests
  - FILE: teach_test.go (NEW). `package main`; `import ("reflect"; "testing")`.
  - DECLARE local decoupling constants (mirror DefaultConfig; teach is pure, plain params):
        const teachCanonTool = "web_search"
        const teachCanonParam = "query"
  - ADD TestShouldWarn: table of {name, calledTool, src string, opt map[string]any,
        canonTool, canonParam string, want bool}. Use the truth table in "Test rows"
        below. Build result := ExtractionResult{Source: tc.src, Optionals: tc.opt};
        assert shouldWarn(tc.calledTool, result, tc.canonTool, tc.canonParam) == tc.want.
  - ADD TestWarningText: table of {name, calledTool, source, canonTool, canonParam, want}.
        Assert reflect.DeepEqual(warningText(...), tc.want). Copy want strings verbatim
        from "Exact expected strings" below (they include the EM DASH).
  - ADD TestNoQueryWarningText: table of {name, canonTool, canonParam, want}. Assert
        reflect.DeepEqual(noQueryWarningText(...), tc.want). Copy want verbatim.
  - NAMING: TestShouldWarn / TestWarningText / TestNoQueryWarningText; t.Run subtests
        with descriptive names. PLACEMENT: repo root alongside extract_test.go.

Task 3: VALIDATE
  - RUN: gofmt -w teach.go teach_test.go; go vet ./...; go build ./...;
        go test -run 'TestShouldWarn|TestWarningText|TestNoQueryWarningText' -v;
        go test ./...
  - CONFIRM: teach.go imports only "fmt" (grep '^import' / grep 'fmt'); go.mod unchanged;
        git diff --stat shows ONLY teach.go + teach_test.go added.
```

### Test rows (copy the want values verbatim — they were validated against PRD §12.2)

**TestShouldWarn** truth table (canonTool/canonParam default to teachCanonTool/Param
unless a `custom` row overrides):

| name | calledTool | src (Source) | opt (Optionals) | canonTool | canonParam | want |
|---|---|---|---|---|---|---|
| canonical | web_search | query | nil | web_search | query | false |
| canonical_empty_opt_map | web_search | query | {} (non-nil empty) | web_search | query | false |
| wrong_tool_case | Web_search | query | nil | web_search | query | true |
| wrong_tool_other | search | query | nil | web_search | query | true |
| source_alias_q | web_search | q | nil | web_search | query | true |
| source_alias_search | web_search | search | nil | web_search | query | true |
| source_nested | web_search | nested:query | nil | web_search | query | true |
| source_inferred | web_search | inferred:messages[0].content | nil | web_search | query | true |
| source_bare_string | web_search | bare-string | nil | web_search | query | true |
| source_scalar | web_search | scalar | nil | web_search | query | true |
| source_array | web_search | array[0] | nil | web_search | query | true |
| optionals_present | web_search | query | {location:US} | web_search | query | true |
| optionals_wrong_tool | search | q | {location:US} | web_search | query | true |
| custom_canonical_match | find | q | nil | find | q | false |
| custom_canonical_mismatch | find | query | nil | find | q | true |
| empty_source_boundary | web_search | "" | nil | web_search | query | true |

(The `empty_source_boundary` row documents that a Found==false result (Source=="")
returns true from shouldWarn — but the caller never reaches shouldWarn for !Found; it
takes the noQueryWarningText path. Include it with a comment so the boundary is pinned.)

**Exact expected strings** (copy verbatim into TestWarningText / TestNoQueryWarningText
`want` fields; each contains the EM DASH — U+2014):

```
# TestWarningText
defaults_q:                 warningText("web_search","q","web_search","query")
  -> [web-search-prime-fixer] Warning: this call used "web_search"/"q" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).

defaults_inferred:          warningText("web_search","inferred:messages[0].content","web_search","query")
  -> [web-search-prime-fixer] Warning: this call used "web_search"/"inferred:messages[0].content" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).

wrong_tool_name:            warningText("Web_search","query","web_search","query")
  -> [web-search-prime-fixer] Warning: this call used "Web_search"/"query" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).

custom_canonical:           warningText("search","q","search_tool","q")
  -> [web-search-prime-fixer] Warning: this call used "search"/"q" rather than the canonical form. Results are above. Next time call: search_tool with { "q": "..." } — e.g. search_tool({ "q": "rust async runtime" }).

# TestNoQueryWarningText
defaults:                   noQueryWarningText("web_search","query")
  -> [web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).

custom_canonical:           noQueryWarningText("search_tool","q")
  -> [web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: search_tool with { "q": "..." } — e.g. search_tool({ "q": "rust async runtime" }).
```

### Implementation Patterns & Key Details

```go
// PATTERN (decoupling convention from extract_test.go): declare LOCAL constants that
// mirror DefaultConfig so the pure-function test does not import config.go.
const (
	teachCanonTool  = "web_search" // mirrors DefaultConfig().CanonicalTool
	teachCanonParam = "query"      // mirrors DefaultConfig().CanonicalParam
)

// PATTERN (shouldWarn truth-table row): build the ExtractionResult minimally — shouldWarn
// reads only Source + Optionals, so Query/Found/Ambiguous are irrelevant to it.
res := ExtractionResult{Source: tc.src, Optionals: tc.opt}
if got := shouldWarn(tc.calledTool, res, tc.canonTool, tc.canonParam); got != tc.want {
	t.Errorf("shouldWarn(%q,%+v,%q,%q) = %v, want %v", tc.calledTool, res, tc.canonTool, tc.canonParam, got, tc.want)
}

// PATTERN (exact-byte text test): compare the whole string with reflect.DeepEqual so a
// wrong dash, missing space, or stray capitalization fails loudly.
if got := warningText(tc.calledTool, tc.source, tc.canonTool, tc.canonParam); !reflect.DeepEqual(got, tc.want) {
	t.Errorf("warningText =\n %q\nwant\n %q", got, tc.want)
}

// GOTCHA: the format strings are RAW string literals (backticks), so the literal " and
// the EM DASH — are written verbatim with no escaping. If you switch to a double-quoted
// literal you must escape the " as \" — do not; keep backticks.

// GOTCHA: never range over result.Optionals in shouldWarn — len() is enough and is
// deterministic. (There is no map iteration anywhere in teach.go.)
```

### Integration Points

```yaml
FILES CREATED:
  - teach.go       (NEW): package main; import "fmt"; shouldWarn/warningText/noQueryWarningText.
  - teach_test.go  (NEW): package main; import reflect+testing; three table-driven tests.
NO OTHER FILES TOUCHED:
  - extract.go/extract_test.go: the ExtractionResult provider — UNTOUCHED (read-only).
  - config*.go, logger.go, health.go, main.go, doc.go: UNTOUCHED.
  - config_test.go/resolve_test.go/health_test.go: owned by P1.M1.T3.S1 — DO NOT TOUCH.
CONSUMER SEAMS (S1 freezes the teach decision+text seam; S2 builds the dispatch on it):
  - P1.M3.T1.S2 (append logic): !Found -> noQueryWarningText (immediate, no upstream);
        Found && shouldWarn -> append warningText after result content; Found && !shouldWarn
        -> no warning; isError always false. S1's signatures + exact bytes are the contract.
  - P1.M5.T2 (server dispatch): passes cfg.CanonicalTool/cfg.CanonicalParam to these fns.
  - P1.M5.T3 (server_test.go e2e): §19.3 cases assert the warning text == S1's bytes.
DATABASE / ROUTES / ENV: none. Pure functions, no I/O, no globals, no SDK types.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# After creating teach.go + teach_test.go — fix before running tests.
gofmt -w teach.go teach_test.go
go vet ./...
go build ./...                  # must compile; only "fmt" added (stdlib)

# teach.go imports ONLY fmt (no config/reflect/encoding/json/SDK):
grep -A3 '^import' teach.go     # expect a single "fmt" import
grep -n 'config\.\|Config' teach.go   # MUST print nothing (no config coupling)
grep -n 'range ' teach.go       # MUST print nothing (no map iteration; len() only)

# EM DASH present in both warning builders (U+2014 = E2 80 94):
grep -n '—' teach.go            # expect 2 hits (warningText + noQueryWarningText)

# Confirm only the two new files; go.mod untouched:
git status --short              # expect: ?? teach.go  ?? teach_test.go
git diff --stat go.mod          # expect EMPTY

# Expected: zero errors; teach.go imports only fmt; em dash present; 2 new files; go.mod clean.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the teach tests in isolation, verbose.
go test -run 'TestShouldWarn|TestWarningText|TestNoQueryWarningText' -v

# Full module suite (teach + all existing tests stay green).
go test ./...

# Expected: PASS. Spot-check the load-bearing rows:
#   shouldWarn: canonical(web_search/query/no-opt)        -> false
#               wrong tool / every non-canonical Source   -> true
#               optionals present (even canonical t+s)     -> true
#               custom canonical match (find/q)           -> false
#   warningText(defaults, source="q")        == PRD §12.2 after-results template (em dash)
#   warningText(defaults, source="inferred:messages[0].content") == template w/ inferred source
#   noQueryWarningText(defaults)             == PRD §12.2 no-results template (em dash)
# Any mismatch on the text rows means a wrong dash, missing space, or bad substitution —
# diff the got/want strings byte-by-byte (the %q format in t.Errorf makes this visible).
```

### Level 3: Integration Testing (System Validation)

```bash
# teach functions are PURE — the "integration" is the consumer-call-shape smoke: prove
# the real caller (P1.M5.T2) can wire extract -> Found gate -> shouldWarn -> warningText
# end-to-end with the REAL DefaultConfig values, and that the local test constants match.

cat > /tmp/teach_smoke_test.go <<'EOF'
package main
import ("encoding/json"; "testing")
func TestTeachSmoke_ConsumerShape(t *testing.T) {
	cfg := DefaultConfig() // the real values the server passes
	// Canonical call: web_search + {"query":"..."} -> no warning.
	canonical := extract(json.RawMessage(`{"query":"rust async runtime"}`),
		cfg.QueryAliases, cfg.OptionalAliases)
	if shouldWarn(cfg.CanonicalTool, canonical, cfg.CanonicalTool, cfg.CanonicalParam) {
		t.Fatalf("canonical call should NOT warn: %+v", canonical)
	}
	// Alias call: web_search + {"q":"..."} -> warn, warningText cites source "q".
	alias := extract(json.RawMessage(`{"q":"rust async runtime"}`),
		cfg.QueryAliases, cfg.OptionalAliases)
	if !shouldWarn(cfg.CanonicalTool, alias, cfg.CanonicalTool, cfg.CanonicalParam) {
		t.Fatalf("alias call should warn: %+v", alias)
	}
	w := warningText(cfg.CanonicalTool, alias.Source, cfg.CanonicalTool, cfg.CanonicalParam)
	if alias.Source != "q" || w == "" {
		t.Fatalf("warningText for alias: source=%q warn=%q", alias.Source, w)
	}
	// No query: {} -> Found==false -> caller takes the noQueryWarningText path (no shouldWarn).
	noq := extract(json.RawMessage(`{}`), cfg.QueryAliases, cfg.OptionalAliases)
	if noq.Found {
		t.Fatalf("{} should be Found==false: %+v", noq)
	}
	nq := noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam)
	if nq == "" {
		t.Fatalf("noQueryWarningText empty")
	}
	// Local test constants must match the real config (decoupling invariant).
	if teachCanonTool != cfg.CanonicalTool || teachCanonParam != cfg.CanonicalParam {
		t.Fatalf("local constants %q/%q != config %q/%q", teachCanonTool, teachCanonParam,
			cfg.CanonicalTool, cfg.CanonicalParam)
	}
}
EOF
cp /tmp/teach_smoke_test.go ./zz_smoke_test.go && go test -run TestTeachSmoke_ConsumerShape -v && rm ./zz_smoke_test.go

# Expected: smoke compiles (real DefaultConfig call shape + extract wiring) and passes;
# the local constants match the real CanonicalTool/CanonicalParam; the no-query path is
# taken via Found (shouldWarn is NOT consulted for !Found). This proves S1's seam slots
# into the dispatch S2/M5.T2 will build.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Byte-fidelity invariant: both warning strings must be byte-identical to the PRD §12.2
# literals. A wrong character (esp. the em dash) is the #1 failure mode. Assert the exact
# PRD default strings directly (no function call) so a copy-paste error in the format
# string is caught independently of the substitution logic.

cat > /tmp/teach_bytefidelity.go <<'EOF'
package main
import "testing"
func TestTeachByteFidelity_PRD(t *testing.T) {
	// PRD §12.2 literals (defaults), copied verbatim from the design doc:
	prdAfter := "[web-search-prime-fixer] Warning: this call used \"web_search\"/\"q\" rather than the canonical form. Results are above. Next time call: web_search with { \"query\": \"...\" } \u2014 e.g. web_search({ \"query\": \"rust async runtime\" })."
	prdNoQuery := "[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { \"query\": \"...\" } \u2014 e.g. web_search({ \"query\": \"rust async runtime\" })."
	if got := warningText("web_search", "q", "web_search", "query"); got != prdAfter {
		t.Errorf("warningText != PRD §12.2 after-results literal:\ngot:  %q\nwant: %q", got, prdAfter)
	}
	if got := noQueryWarningText("web_search", "query"); got != prdNoQuery {
		t.Errorf("noQueryWarningText != PRD §12.2 no-results literal:\ngot:  %q\nwant: %q", got, prdNoQuery)
	}
	// Belt-and-suspenders: the em dash (U+2014) is literally present in both outputs.
	for _, s := range []string{prdAfter, prdNoQuery} {
		if !containsDash(s) { t.Errorf("missing em dash in: %q", s) }
	}
}
func containsDash(s string) bool { for _, r := range s { if r == '\u2014' { return true } }; return false }
EOF
cp /tmp/teach_bytefidelity.go ./zz_bf_test.go && go test -run TestTeachByteFidelity_PRD -v && rm ./zz_bf_test.go

# Expected: both functions produce byte-identical output to the PRD §12.2 literals; the
# em dash is present. This is the strongest guarantee S1 gives its e2e consumers (M5.T3).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: gofmt clean; `go vet ./...` and `go build ./...` exit 0; teach.go
      imports only "fmt"; em dash present (2 hits); git status shows only 2 new files;
      go.mod unchanged.
- [ ] Level 2 passes: `go test -run 'TestShouldWarn|TestWarningText|TestNoQueryWarningText' -v`
      and `go test ./...` both PASS.
- [ ] Level 3 passes: the consumer-shape smoke compiles with real DefaultConfig and
      passes; local constants match the real CanonicalTool/CanonicalParam.
- [ ] Level 4 passes: warningText/noQueryWarningText are byte-identical to the PRD §12.2
      literals; em dash present.

### Feature Validation

- [ ] `shouldWarn` returns false ONLY for canonical tool + Source==canonicalParam + no
      optionals; true for wrong tool, every non-canonical Source label, and optionals>0.
- [ ] `warningText` reproduces PRD §12.2 after-results text byte-for-byte (em dash,
      fixed example "rust async runtime", literal quotes around tool/source/param).
- [ ] `noQueryWarningText` reproduces PRD §12.2 no-results text byte-for-byte.
- [ ] The truth table covers: canonical, wrong tool (case + other name), each
      non-canonical Source (alias/scalar/bare-string/array/nested/inferred), optionals
      trigger, custom canonical, and the empty-source boundary.

### Code Quality Validation

- [ ] teach.go follows extract.go's pure-function discipline (plain params, no Config
      import, no I/O, no globals, no SDK types); only `"fmt"` imported.
- [ ] Doc comments are Mode-A (cite PRD §12.1/§12.2/§12.3, the canonical-pair rule, the
      exact text format, and that Source reflects ambiguous/inferred labels).
- [ ] teach_test.go mirrors extract_test.go conventions (package main; reflect+testing;
      table-driven + t.Run; local decoupling constants; reflect.DeepEqual).
- [ ] No anti-patterns (see below); no `%q` (literal quotes + %s); no Found/Ambiguous
      read in shouldWarn; no map iteration.

### Documentation & Deployment

- [ ] Mode A docs honored: doc comments on all three functions; NO README/config.example
      .json/doc.go changes (the teaching signal is internal, surfaced only via the MCP
      response that S2/M5 assemble).
- [ ] No new env vars / config keys / routes introduced.

---

## Anti-Patterns to Avoid

- ❌ Don't use `%q` in the format strings. Use literal `"` characters with `%s` so the
  format string is visually identical to the PRD §12.2 block and the output is
  unambiguous. (`%q` would also work for these identifiers but escapes edge cases
  differently than the PRD; literal quotes remove all doubt.)
- ❌ Don't replace the EM DASH `—` (U+2014) with `-`, `--`, or any look-alike. Both PRD
  §12.2 templates use U+2014. Copy it from the PRD. A byte-exact test catches a wrong
  dash immediately (validated at byte 168 in the default warningText).
- ❌ Don't read `result.Found` or `result.Ambiguous` inside `shouldWarn`. The item spec
  is Source + Optionals only. The caller gates on Found first (PRD §12.3); ambiguity is
  encoded in the Source label. Adding a Found check would mask caller bugs and deviate
  from spec.
- ❌ Don't forget the optionals trigger. `shouldWarn` MUST return true when
  `len(result.Optionals) > 0` even if the tool and source are canonical (PRD §12.1:
  "or normalized optionals"). A canonical call with a forwarded optional still warns.
- ❌ Don't import `config` in teach.go. The functions take canonicalTool/canonicalParam
  as plain string params (pure, unit-testable) — exactly like extract.go takes plain
  alias params. The real caller passes cfg.CanonicalTool/cfg.CanonicalParam.
- ❌ Don't build the append-after-results / MCP content-block logic or set `isError` in
  S1. That is P1.M3.T1.S2's scope. teach.go must compile and test green with ZERO
  MCP/SDK types.
- ❌ Don't parameterize the example query "rust async runtime". It is a FIXED LITERAL in
  both warnings (PRD §12.2). Only <tool>, <source>, canonicalTool, canonicalParam vary.
- ❌ Don't range over `result.Optionals`. `len()` is enough, deterministic, and is all
  the spec requires. (No map iteration exists anywhere in teach.go.)
- ❌ Don't treat Source=="" as canonical. An empty Source (a Found==false result) is
  != canonicalParam, so shouldWarn returns true — but the caller never reaches
  shouldWarn for !Found (it uses noQueryWarningText). Pin this boundary with a test row
  + comment; do not special-case it in shouldWarn.
