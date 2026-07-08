# Research — teach.go: canonical-pair detection + warning text (PRD §12.1, §12.2)

Item **P1.M3.T1.S1** in plan `002_0a8ab3410994` (v2.0 normalizing MCP server). All
three function bodies below were validated byte-for-byte against PRD §12.2 in a
throwaway Go program (em dash U+2014 confirmed).

---

## 1. The input contract — ExtractionResult (read from on-disk extract.go)

`extract.go` (S1+S2+S3 merged on disk) ships this struct, which teach.go consumes:

```go
type ExtractionResult struct {
    Query     string
    Source    string         // the label shouldWarn + warningText key off
    Ambiguous bool           // NOT used by shouldWarn (ambiguity is encoded in Source)
    Optionals map[string]any // len(.)>0 is a warning trigger (normalized optionals forwarded)
    Found     bool           // the CALLER gates on this; shouldWarn does NOT read it (see §2)
}
```

**Source label enumeration** (all values extract can produce; the canonical one is
the literal canonicalParam, default `"query"`):

| extraction path | Source value |
|---|---|
| bare-string `arguments` | `"bare-string"` |
| number/boolean | `"scalar"` |
| array (first yielding element) | `"array[0]"` |
| object, alias key `a` is a string | `a` (e.g. `"query"`, `"q"`) |
| object, alias key `a` is a stringified number/bool | `"scalar"` |
| object, alias key `a` is a map/array (drill-in hit) | `"nested:"+a` |
| object, no alias yielded (inference) | `"inferred:"+<dotted/bracket path>` (e.g. `"inferred:foo"`, `"inferred:messages[0].content"`) |
| failure (no usable string) | `""` (zero value; Found==false) |

Only ONE Source value equals canonicalParam: the bare alias key `"query"` (when the
agent wrote `{"query": "..."}`). Every other Source → non-canonical → warn.

---

## 2. shouldWarn — the canonical-pair rule (PRD §12.1) + the Found boundary

PRD §12.1, verbatim decision table:
- **No warning** when calledTool == `web_search` AND query came from `query`.
- **Warning after results** otherwise: wrong tool, wrong param,
  nested/inferred/bare-string/array extraction, **or normalized optionals**.
- **Immediate no-results warning** (no upstream call) only when extraction found no
  query (§10.1.5) — that path uses `noQueryWarningText`, NOT shouldWarn.

### Logic (item spec, faithful — no extra fields)
```go
func shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam string) bool {
    if len(result.Optionals) > 0 { return true } // normalized optionals forwarded -> warn (PRD §12.1)
    if calledTool != canonicalTool { return true } // wrong tool name
    if result.Source != canonicalParam { return true } // any non-canonical source (alias/scalar/nested/array/inferred/bare-string)
    return false // the ONE canonical case
}
```
- `len(result.Optionals) > 0` is true for both a nil map and an empty non-nil map
  (len==0 in both) — so an accidental empty Optionals map does NOT spuriously warn.
  A populated Optionals (e.g. `{location:"US"}`) DOES warn even when tool+source are
  canonical, because "normalized optionals" is an explicit warning trigger (PRD §12.1).

### The Found boundary (RESOLUTION — important)
The item's shouldWarn signature takes the full ExtractionResult but its logic uses
only `Source` and `Optionals` — **NOT `Found`**. This is deliberate and correct:
- The dispatch (PRD §12.3; consumer P1.M5.T2 / P1.M3.T1.S2) gates on `Found` FIRST:
  `!Found` → immediate `noQueryWarningText` (FR-6, no upstream call); `Found` → run
  the search, THEN decide the after-results warning via `shouldWarn`.
- So shouldWarn is the AFTER-RESULTS predicate, meaningful only for `Found==true`.
  It is NOT called on a no-query result. The caller owns the Found gate; shouldWarn
  stays a clean, spec-faithful predicate (do NOT add a `Found` check inside it —
  that would mask caller bugs and deviate from the item's stated logic).
- Note: the P1.M2.T1.S3 PRP's forward-note said teach "reads Source/Ambiguous/Found".
  That was speculative. The AUTHORITATIVE item spec reads **Source + Optionals only**
  (Ambiguous is encoded in the Source label; Found is the caller's gate). Follow the
  item spec, not the forward-note.

---

## 3. warningText / noQueryWarningText — exact bytes (validated against PRD §12.2)

PRD §12.2 templates (the `web_search`/`query` in the example ARE the canonical
values — substitute canonicalTool/canonicalParam). `<tool>` = the tool actually
called; `<source>` = the extraction Source label.

### warningText(calledTool, source, canonicalTool, canonicalParam)
```
[web-search-prime-fixer] Warning: this call used "<tool>"/"<source>" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```
Implement with literal `"` + `%s` (NOT `%q` — makes the byte layout self-evident and
the test a plain string compare):
```go
fmt.Sprintf(
    `[web-search-prime-fixer] Warning: this call used "%s"/"%s" rather than the canonical form. Results are above. Next time call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
    calledTool, source, canonicalTool, canonicalParam, canonicalTool, canonicalParam,
)
```
Validated: for `("web_search","q","web_search","query")` → byte-identical to the PRD
template; for inferred source `("web_search","inferred:messages[0].content",...)`
→ matches PRD's "`<source>` reflects that" clause.

### noQueryWarningText(canonicalTool, canonicalParam)
```
[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```
```go
fmt.Sprintf(
    `[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
    canonicalTool, canonicalParam, canonicalTool, canonicalParam,
)
```
Validated byte-identical to PRD §12.2 for defaults.

### CRITICAL — the em dash (U+2014)
Both templates contain `—` (EM DASH, UTF-8 `E2 80 94`) between `"..." }` and `e.g.`.
Do NOT replace it with `-`, `--`, or `—`-lookalikes. Copy the character from the PRD.
Validated present at byte offset 168 in the default warningText. A byte-exact test
(any `==` or `reflect.DeepEqual` on the string) catches a wrong dash immediately.

### Why literal `"` + `%s`, not `%q`
`%q` would also produce `"web_search"` for these simple identifiers, but it would
escape any future unusual char differently than the PRD. Using literal `"` chars in
the format string makes the output layout identical to the PRD block at a glance and
removes all ambiguity. `calledTool`/`source`/`canonicalTool`/`canonicalParam` are
simple identifiers/paths with no quotes/backslashes/control chars, so no escaping
concern either way.

---

## 4. Config fields + the pure-function design

`config.go` ships `CanonicalTool` (default `"web_search"`) and `CanonicalParam`
(default `"query"`). The teach functions take these as **plain string params** —
they do NOT import `config`/`Config`, mirroring `extract.go` (extract takes
`queryAliases`/`optionalAliases` as plain params). This keeps teach.go pure and
unit-testable without spinning up config. The real caller passes
`cfg.CanonicalTool`/`cfg.CanonicalParam` (see consumer map §7).

teach.go imports ONLY `"fmt"` (for Sprintf). No reflect, no encoding/json, no SDK,
no third-party. `go.mod` gains zero `require`s.

---

## 5. File scope — teach.go (CREATE) + teach_test.go (CREATE)

- `teach.go` does NOT exist yet (confirmed: `ls teach*.go` empty). S1 CREATES it with
  the three exported functions + Mode-A doc comments.
- S1 owns the THREE PURE FUNCTIONS + their unit tests. P1.M3.T1.S2 (next) adds the
  append-after-results logic + isError invariant to teach.go and EXTENDS teach_test.go.
- S1's teach_test.go covers the §19.2 assertions that map to S1: "no warning for
  canonical" (shouldWarn==false), "warning ... for every other case"
  (shouldWarn==true for wrong tool/source/optionals), "the immediate no-results
  warning" (noQueryWarningText exact bytes), "example text present and correct"
  (warningText/noQueryWarningText exact bytes). The "warning appended after results"
  + "isError never set" assertions are S2's append-logic domain — S1 does NOT build
  the append integration (no MCP content-block manipulation here).

This matches the established pattern: rewrite_test.go + extract_test.go are created
alongside their .go file in the first subtask and extended later.

---

## 6. Test conventions (read from extract_test.go — the closest analog)

- `package main`; `import ("reflect"; "testing")`.
- Table-driven `tests := []struct{...}` + `for _, tc := range tests { t.Run(tc.name, ...) }`.
- Assert whole values via `reflect.DeepEqual`, with `t.Errorf("... = ...\nwant ...")`.
- **Decoupling convention**: extract_test.go declares LOCAL `extractQA`/`extractOpt`
  "to mirror DefaultConfig() so the test stays decoupled from config.go (extract is a
  pure function taking plain params)". teach_test.go follows suit: declare LOCAL
  constants `teachCanonTool = "web_search"`, `teachCanonParam = "query"` mirroring
  DefaultConfig's CanonicalTool/CanonicalParam (teach functions take plain params).
  The Level-3 smoke test then uses the REAL `DefaultConfig()` values to prove the
  local constants match production.

---

## 7. Consumer map (what S1 must produce for the rest of P1.M3/P1.M5)

| Consumer | What it calls | Why S1's contract matters |
|---|---|---|
| **P1.M3.T1.S2** (append logic) | `shouldWarn(...)`, `warningText(...)`, `noQueryWarningText(...)` | S2 builds the dispatch: `!Found` → noQueryWarningText (immediate); `Found && shouldWarn` → append warningText after result content; `Found && !shouldWarn` → no warning. S2 sets `isError:false` always. S1's exact byte format + boolean semantics are the contract S2 assembles. |
| **P1.M5.T2** (server dispatch) | all three | The handler wires extract → (Found gate) → upstream → teach. It passes `cfg.CanonicalTool`/`cfg.CanonicalParam` to the teach functions. |
| **P1.M5.T3** (server_test.go e2e) | indirectly | E2E cases 1-4 (§19.3) assert the warning text the client sees; S1's byte format is what those tests compare against. |

DATABASE / ROUTES / ENV: none. teach.go is pure (fmt.Sprintf + comparisons), no I/O,
no globals, no SDK types.
