name: "P1.M3.T1.S2 — Append-after-results logic + immediate no-results warning + isError invariant (PRD §12.3)"
description: |

  MODIFY `teach.go` (which P1.M3.T1.S1 created with three pure functions importing only
  `"fmt"`) to ADD the SDK import `github.com/modelcontextprotocol/go-sdk/mcp` and TWO
  MCP-result-assembly functions, and EXTEND `teach_test.go` with two table-driven tests.
  These are the bridge between S1's pure warning text and the SDK's `*mcp.CallToolResult`.
  (1) `func appendWarning(result *mcp.CallToolResult, text string)` — appends
  `&mcp.TextContent{Text: text}` to `result.Content` AFTER the existing blocks (PRD §12.3
  ordering invariant: upstream results first, warning last so the model acts on results).
  Append-ONLY: does NOT replace/prepend/reorder. Does NOT touch `result.IsError` (stays
  false). (2) `func noQueryResult(text string) *mcp.CallToolResult` — builds a FRESH
  result with `Content: []mcp.Content{&mcp.TextContent{Text: text}}` (the warning IS the
  only content) and `IsError` left at zero (false); returned when extraction found no
  query (no upstream call, PRD §10.1.5/§12.1/FR-6). CONTRACT (verified from SDK v1.6.1
  source): `TextContent`'s methods are POINTER receivers so only `*TextContent` satisfies
  the `Content` interface — always use `&mcp.TextContent{...}`. NEVER call
  `result.SetError(...)` — it sets IsError=true AND overwrites Content when empty
  (`protocol.go:131`). We build results by hand; IsError stays false. INPUT: the
  `*mcp.CallToolResult` type + already-built warning `text` from S1's `warningText` /
  `noQueryWarningText`. OUTPUT consumed by P1.M5.T2 (server dispatch: calls
  `appendWarning` after upstream results, `noQueryResult` when extraction fails).
  DOCS: [Mode A] doc comments on both functions documenting the append-only ordering, the
  no-results exception, the pointer-receiver/`&TextContent` rule, and that IsError is
  never set for any normalization/warning case. go.mod gains ZERO requires.

---

## Goal

**Feature Goal**: Ship the MCP-result-assembly layer of the teaching signal (PRD §12.3,
FR-6). After S1 produced the pure decision + warning-text functions, this item answers
the assembly questions: (a) given a successful upstream `*mcp.CallToolResult`, how do we
append the warning AFTER its content blocks (so results lead, warning trails) without
ever flipping `IsError`? and (b) when extraction found no query, how do we return a
fresh result whose ONLY content is the no-results warning (no upstream call, `IsError`
false)? The two functions are tiny, deterministic, and fully table-tested against PRD
§12.3's ordering invariant and FR-6's "isError never set" rule.

**Deliverable**: MODIFY two existing files (no brand-new files; S1 created them in
parallel and this PRP treats S1's teach.go/teach_test.go as a frozen contract):
1. **MODIFY** `teach.go` — add `"github.com/modelcontextprotocol/go-sdk/mcp"` to the
   import block (alongside S1's `"fmt"`), and ADD two exported functions
   (`appendWarning`, `noQueryResult`) each with a Mode-A doc comment. S1's three
   functions are UNCHANGED.
2. **EXTEND** `teach_test.go` — add two table-driven tests `TestAppendWarning` and
   `TestNoQueryResult` (S1's three tests are UNCHANGED).

No other file is touched. `go.mod` gains zero `require`s (the SDK require was added in
P1.M1.T2.S1 and `main.go` already imports `mcp`). No README/config/doc.go change
(Mode A docs = doc comments only).

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` (and `go test -run 'TestAppendWarning|TestNoQueryResult' -v`) all exit
clean. `appendWarning` is append-only (existing blocks preserved first, in order;
warning added last; `IsError` untouched/false). `noQueryResult` returns a non-nil
result whose single content block is exactly the warning text and whose `IsError` is
false. teach.go imports `fmt` AND `mcp` (S1's `"fmt"` retained); `git diff --stat
go.mod` is empty. A wire-format check confirms `noQueryResult`/`appendWarning` output
marshals to `{"content":[{"type":"text","text":...}]}` with NO `isError` field.

## User Persona

**Target User**: (1) **P1.M5.T2** (server dispatch), which wires the handler:
`!Found` → `return noQueryResult(noQueryWarningText(...)), nil`; `Found` → delegate →
`if shouldWarn(...) { appendWarning(upstream, warningText(...)) }`; `return upstream, nil`.
S2's two function signatures + `IsError=false` invariant ARE the assembly contract M5.T2
calls. (2) **P1.M5.T3** (server_test.go e2e §19.3), whose case 4 (`web_search + {}` → no
upstream call; client gets the immediate no-results warning, `isError:false`) asserts
`noQueryResult`'s output, and whose cases 2/3 (results THEN warning) assert
`appendWarning`'s ordering — S2's wire format is what those tests compare against. (3)
the agent, which receives results first (acts on them) and the teaching warning last.

**Use Case**: Agent calls `web_search` with `{"q": "rust async runtime"}`. Extraction
yields `{Query:"rust async runtime", Source:"q", Found:true}`. The server delegates to
z.ai, gets back `upstream` (a `*mcp.CallToolResult` with the real search results in
`Content`). `shouldWarn(...)` returns true, so the server calls
`appendWarning(upstream, warningText("web_search","q","web_search","query"))` — the
warning is appended as the LAST content block, results stay first. If instead the agent
sent `{}` (no query), extraction is `Found==false`, the server makes NO upstream call,
and returns `noQueryResult(noQueryWarningText("web_search","query"))` immediately — a
fresh result whose only content is the no-results warning, `IsError` false.

**User Journey**: tools/call → extract → (caller's Found gate) → for `Found`: delegate to
upstream → `appendWarning(upstream, warningText(...))` (only if `shouldWarn`); for
`!Found`: `noQueryResult(noQueryWarningText(...))`, no upstream. In BOTH paths `IsError`
is false. The model always sees results-first-then-warning (delegate path) or the lone
no-results warning (no-query path) — never a bare warning over real results, never
`isError:true`.

**Pain Points Addressed**: (1) PRD §12.3: "A warning returned without results tempts the
model to retry." `appendWarning` guarantees results lead and the warning trails, so the
model acts on results. `noQueryResult` is the ONE sanctioned warning-without-results
case (there is genuinely nothing to return; a retry is exactly what we want). (2) The SDK
`SetError` trap: calling it sets `IsError=true` AND clobbers Content when empty. A single
source of truth (`appendWarning`/`noQueryResult` build results by hand, never touch
`IsError` or call `SetError`) encodes FR-6's "isError never set" once, tested.

## Why

- Implements **PRD §12.3** (append-after-results ordering; why results must accompany the
  warning; the single no-results exception) and the S2-owned half of **§19.2**
  (teach_test.go: "warning appended after results", "the immediate no-results warning",
  "isError never set").
- Supports **FR-6** ("append one text content block AFTER the real result content ...
  never returned without results when a search could be performed ... Never set
  isError:true"). `appendWarning` is the AFTER-results append; `noQueryResult` is the one
  no-results exception; both leave `IsError` false.
- **Decouples MCP assembly from the dispatch.** The server handler (M5.T2) calls two
  trivially-testable functions instead of inlining `&mcp.TextContent{...}` and `append`
  in the handler. This keeps the handler focused on the extract→delegate→teach flow.
- **Freezes the assembly seam** (signatures + IsError invariant + pointer-receiver
  `&TextContent` rule) so M5.T2 and the e2e suite (M5.T3) assert against one frozen
  result shape. S1 froze the decision+text seam; this item freezes the result seam.

## What

Two exported functions added to the existing `teach.go` (S1's three functions retained;
the import block gains `"github.com/modelcontextprotocol/go-sdk/mcp"`):

1. **`func appendWarning(result *mcp.CallToolResult, text string)`**
   — the after-results warning appender. Mutates `result` in place by appending a single
   `&mcp.TextContent{Text: text}` to `result.Content`. Append-ONLY: existing content
   blocks are preserved first and in their original order; the warning becomes the LAST
   block. Does NOT touch `result.IsError` (it stays false in our flow — we never set it).
   PRECONDITION: `result` must be non-nil (in the real dispatch it is always the non-nil
   upstream result; no defensive nil guard). Body is a single `append`.

2. **`func noQueryResult(text string) *mcp.CallToolResult`**
   — the immediate no-results warning result. Returns a FRESH, non-nil
   `*mcp.CallToolResult` whose `Content` is exactly
   `[]mcp.Content{&mcp.TextContent{Text: text}}` (the warning IS the only content) and
   whose `IsError` is left at its zero value (false — never set). `StructuredContent` and
   `Meta` stay nil. Body is a single struct literal return.

`teach_test.go` gains two table-driven tests: `TestAppendWarning` (append-only ordering
with one and multiple existing blocks, append-to-empty edge, double-append, `IsError`
stays false, whole-`Content`-slice equality via `reflect.DeepEqual`) and
`TestNoQueryResult` (single-block content, `IsError` false, non-nil, empty-text edge).

### Success Criteria

- [ ] `teach.go` imports both `"fmt"` (S1, retained) and
      `"github.com/modelcontextprotocol/go-sdk/mcp"` (S2, new); S1's three functions are
      unchanged; `appendWarning`/`noQueryResult` are added with Mode-A doc comments.
- [ ] `appendWarning` appends exactly one `&mcp.TextContent{Text: text}` as the LAST
      block; existing blocks are preserved first and in order; `IsError` is never set.
- [ ] `noQueryResult` returns a non-nil `*mcp.CallToolResult` with `Content` ==
      `[]mcp.Content{&mcp.TextContent{Text: text}}` and `IsError` == false.
- [ ] Neither function ever references `SetError` or sets `IsError`.
- [ ] `teach_test.go` has `TestAppendWarning` (ordering + invariant rows) and
      `TestNoQueryResult` (content + invariant rows); S1's three tests unchanged.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat go.mod` is empty.

## All Needed Context

### Context Completeness Check

_Pass._ The deliverable is fully pinned: (a) the two function bodies are each a single
line (`result.Content = append(...)` / `return &mcp.CallToolResult{Content: ...}`) — the
contract is the SDK type usage, which this PRP's research (`append-logic-contract.md`)
**verified line-by-line against the SDK v1.6.1 source**: `CallToolResult` fields
(`protocol.go:71`), the `Content` interface + pointer-receiver `TextContent`
(`content.go:17/28`), the `SetError` trap (`protocol.go:131`), and the canonical
result-building pattern `&CallToolResult{Content: []Content{&TextContent{Text:"..."}}}`
(cited at `mcp_test.go:43`, `tool_example_test.go:40`). (b) PRD §12.3/§12.2/FR-6 pin the
ordering invariant and the "isError never set" rule. (c) The item description pins the
exact signatures and the append-only/no-results semantics. (d) S1's PRP (frozen contract)
pins teach.go's existing content (three pure functions, import `"fmt"` only) and
teach_test.go's existing tests. (e) `go.mod` already requires the SDK (verified) and
`main.go` already imports `mcp` (verified), so adding the import to teach.go needs no
go.mod change. The one non-obvious trap — `TextContent` has POINTER-receiver methods, so
only `*TextContent` implements `Content` — is flagged in Known Gotchas and is caught by
the compiler AND by the wire-format test. An agent with no prior knowledge can implement
this from the PRP + the on-disk teach.go (S1) alone.

### Documentation & References

```yaml
# MUST READ — the ordering rule + the no-results exception + the isError invariant.
- file: PRD.md
  section: "§12.3 Why appended, and why results must accompany the warning"
  why: the ordering invariant this item enforces — run the search first, return results +
        warning together, warning TRAILING so results are what the model acts on; the ONE
        note that travels without results is the "could not find a query" warning.
  critical: appendWarning is the AFTER-results appender (warning last); noQueryResult is
        the single no-results exception. Neither sets isError.

- file: PRD.md
  section: "§12.2 Warning text (with example)" + "§12.1 When a warning is added"
  why: §12.2 fixes "isError is never set for any normalization, guidance, or warning
        case"; §12.1 pins the immediate no-results case (extraction found no usable
        query → no upstream call). S1 produces the warning TEXT; S2 wraps it in the result.
  critical: the warning TEXT this item wraps is already byte-frozen by S1's
        warningText/noQueryWarningText. S2 takes that text as a plain string param.

- file: PRD.md
  section: "FR-6 Teaching signal" + "§9.4 Canonical surface"
  why: FR-6 = "append one text content block AFTER the real result content ... Never set
        isError:true ... the one exception: extraction found no usable query → return the
        warning immediately and make no upstream call." This is verbatim the two functions.
  critical: appendWarning = the AFTER-results append; noQueryResult = the no-upstream-call
        exception; both leave IsError false.

- file: PRD.md
  section: "§19.2 teach_test.go"
  why: the test spec. S2 owns: "warning appended after results" (TestAppendWarning),
        "the immediate no-results warning" (TestNoQueryResult), "isError never set" (both).
        S1's tests own the canonical/no-warning/example-text assertions — UNCHANGED.

# MUST READ — the SDK types + the SetError trap, VERIFIED from source.
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§2 CLIENT SIDE ... CallToolResult" + "§3 CONTENT TYPES"
  why: §2 shows the `CallToolResult` struct (`Content []Content`, `IsError bool` omitempty,
        unexported `err` set only by SetError) and the append/warning pattern
        `result.Content = append(result.Content, &mcp.TextContent{Text: warningText})`,
        plus "Never call result.SetError(err)". §3 shows TextContent and the Content
        interface, and that results are built with `[]mcp.Content{&mcp.TextContent{Text:"..."}}`.
  critical: the architecture doc's `&mcp.TextContent{...}` form and "never SetError" rule
        were independently RE-VERIFIED against the SDK source in this PRP's research
        (append-logic-contract.md §1/§2). TextContent methods are POINTER receivers →
        only `*TextContent` implements Content.

# VERIFIED DESIGN — the exact function bodies + test design + wire-format check.
- docfile: plan/002_0a8ab3410994/P1M3T1S2/research/append-logic-contract.md
  why: §1 the verified CallToolResult/Content/TextContent facts (with source line cites);
        §2 the SetError trap; §3 the two function bodies + the M5.T2 dispatch pseudocode
        they plug into; §4 the teach.go evolution (fmt-only → +mcp import; go.mod
        unchanged); §5 test design (reflect.DeepEqual on []mcp.Content; the contentTexts
        helper); §6 wire-format fidelity check; §7 consumer map; §8 gotchas.

# FROZEN CONTRACT (read-only — S1, running in parallel): the file this item MODIFIES.
- file: teach.go   # CREATED by P1.M3.T1.S1 (treat as frozen contract)
  why: S1's teach.go is `package main`, `import "fmt"`, with shouldWarn / warningText /
        noQueryWarningText. S2 ADDS the `mcp` import and the two assembly functions
        alongside them; it does NOT change S1's functions. (If S1 is not yet on disk when
        S2 runs, S1 must land first — see Implementation Tasks Task 0.)
  pattern: S1's doc comments cite PRD sections and explain semantics; S2's two doc
        comments follow the same Mode-A style and additionally cite the SDK type facts.
  gotcha: do NOT remove or rewrite S1's functions or import; only ADD to the import block
        and append the two functions.

- file: teach_test.go   # CREATED by P1.M3.T1.S1 (treat as frozen contract)
  why: S1's teach_test.go is `package main`, `import (reflect, testing)`, with
        TestShouldWarn / TestWarningText / TestNoQueryWarningText and local decoupling
        constants teachCanonTool/teachCanonParam. S2 EXTENDS it with TestAppendWarning /
        TestNoQueryResult; it does NOT change S1's tests or constants.
  pattern: table-driven `tests := []struct{...}` + `t.Run(tc.name, ...)`; reflect.DeepEqual
        on whole values; `t.Errorf("... = ...\nwant ...")` message style.

# MUST READ — the test-file convention to mirror (and the project's reflect.DeepEqual style).
- file: extract_test.go
  why: the project's test conventions for a module: package main; import reflect+testing;
        table-driven + t.Run; reflect.DeepEqual on whole values; local decoupling
        constants. TestAppendWarning/TestNoQueryResult follow the same shape (the SDK types
        replace ExtractionResult, but the table+DeepEqual+t.Run style is identical).
  pattern: `t.Errorf` with got/want; one table per concern.

# CONFIG (read-only) — proves the import path is already in go.mod and used.
- file: go.mod
  why: already requires `github.com/modelcontextprotocol/go-sdk v1.6.1` (added in
        P1.M1.T2.S1). teach.go adding the `mcp` import needs NO go.mod change.
- file: main.go
  why: already imports `"github.com/modelcontextprotocol/go-sdk/mcp"` and uses the `mcp.`
        prefix (e.g. `mcp.NewServer`, `mcp.NewStreamableHTTPHandler`). Confirms the import
        path and the `mcp.` qualifier style teach.go should reuse.

# CONTRACTS (read-only — do not break):
- file: plan/002_0a8ab3410994/P1M3T1S1/PRP.md
  why: owns teach.go's three pure functions + teach_test.go's three tests. S2 ADDS to
        both files without modifying S1's code. If a parallel-write merge is needed,
        resolve by keeping all S1 code intact and adding S2 code alongside.
- file: plan/002_0a8ab3410994/P1M2T1S3/PRP.md
  why: owns extract.go / ExtractionResult (frozen). S2 does not touch extraction.

# CONSUMERS (forward references — what S2 must produce):
- file: plan/002_0a8ab3410994/prd_snapshot.md
  section: "§11.3" (result handling), "§12.3" (append-after-results dispatch),
        "§19.3" (server_test e2e cases 2/3/4 assert the warning content/order/isError).
  why: pins the dispatch M5.T2 builds on top of S2's two functions, and the e2e
        assertions that compare against S2's wire format.

# Go MCP SDK (stable v1.6.1 — verified from source, not just docs).
- url: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#CallToolResult
  why: the CallToolResult struct (Content []Content, IsError bool). Prefer the on-disk
        source (cited in research §1) over pkg.go.dev for exact-field accuracy.
```

### Current Codebase tree (the INPUT state — run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod          # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  go.sum
  doc.go          # package comment (rewritten in P1.M5.T4.S2 — NOT here)
  main.go         # bootstrap; ALREADY imports "github.com/modelcontextprotocol/go-sdk/mcp"
  config.go       # Config{CanonicalTool:"web_search",CanonicalParam:"query",...} — UNTOUCHED
  logger.go, health.go — UNTOUCHED
  extract.go      # ExtractionResult + extract (P1.M2.T1, COMPLETE) — UNTOUCHED
  extract_test.go # extract tests — UNTOUCHED (the test-style analog to mirror)
  config_test.go, resolve_test.go, health_test.go — owned by P1.M1.T3.S1 — DO NOT TOUCH
  logger_test.go  # green — UNTOUCHED
  teach.go        # CREATED by P1.M3.T1.S1 (parallel): package main, import "fmt",
                  #   shouldWarn / warningText / noQueryWarningText — S2 MODIFIES (adds mcp import + 2 fns)
  teach_test.go   # CREATED by P1.M3.T1.S1 (parallel): TestShouldWarn / TestWarningText /
                  #   TestNoQueryWarningText — S2 EXTENDS (adds 2 tests)
  testdata/, README.md, config.example.json, PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
teach.go          # MODIFIED. import block: "fmt" (S1, kept) + "github.com/modelcontextprotocol/
                  #   go-sdk/mcp" (S2, new). Functions:
                  #   shouldWarn / warningText / noQueryWarningText  (S1 — UNCHANGED)
                  #   appendWarning(result *mcp.CallToolResult, text string)   — S2 NEW (§12.3 append-only)
                  #   noQueryResult(text string) *mcp.CallToolResult           — S2 NEW (§12.3 no-results)
                  #   Each new fn with a Mode-A doc comment. No Config import; no I/O; no network.
teach_test.go     # EXTENDED. ADD (S1's tests + constants kept):
                  #   TestAppendWarning  (append-only ordering + IsError==false)
                  #   TestNoQueryResult  (single-block content + IsError==false)
# NO other files created or modified. go.mod unchanged.
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL — mcp.TextContent methods are POINTER receivers (content.go:MarshalJSON/fromWire).
// Therefore a TextContent VALUE does NOT satisfy the mcp.Content interface — only *TextContent
// does. Every content block MUST be &mcp.TextContent{Text: "..."}. A bare mcp.TextContent{...}
// in a []mcp.Context is a COMPILE ERROR (good — the compiler enforces this). Still, write the &.
// Verified: the SDK's own usage is always &TextContent{...} (mcp_test.go:43, protocol.go:134).

// CRITICAL — NEVER call result.SetError(err) (protocol.go:131-140). It sets IsError=true
// AND, when Content is empty, OVERWRITES Content with a single error-text block. (With the
// MCPGODEBUG=seterroroverwrite=1 escape hatch it ALWAYS overwrites — never less.) For every
// normalization/guidance/warning case we build results BY HAND and leave IsError at its zero
// value (false). appendWarning does not touch IsError; noQueryResult leaves it zero via the
// struct literal. Confirmed against source + architecture/mcp_sdk_api.md §2.

// CRITICAL — appendWarning is APPEND-ONLY. A single `result.Content = append(result.Content,
// &mcp.TextContent{Text: text})`. Do NOT prepend, do NOT rebuild/reorder the slice, do NOT
// allocate a new slice. Existing upstream blocks MUST remain first, in original order, and the
// warning MUST become the LAST block (PRD §12.3: results lead, warning trails). A test asserts
// the whole Content slice with reflect.DeepEqual so any reordering fails loudly.

// CRITICAL — IsError is NEVER set by either function. appendWarning does not reference it
// (leaves whatever it was — false in our flow); noQueryResult leaves it at zero via the literal.
// json:"isError,omitempty" means a false IsError is ABSENT from the wire. The wire-format test
// asserts "isError" is NOT present in the marshalled JSON.

// GOTCHA — do NOT give appendWarning or noQueryResult canonical-tool/canonical-param params.
// They take ALREADY-BUILT warning TEXT (a plain string). The caller (M5.T2) builds that text
// with S1's warningText(...) / noQueryWarningText(...). Keeping canonical values out keeps
// these functions pure MCP-assembly (no Config import, trivially unit-testable).

// GOTCHA — teach.go GAINS the "github.com/modelcontextprotocol/go-sdk/mcp" import but go.mod
// is UNCHANGED (the require line already exists from P1.M1.T2.S1; main.go already imports mcp).
// Do NOT run `go get` or edit go.mod. Verify with `git diff --stat go.mod` == empty.

// GOTCHA — do NOT modify S1's three functions (shouldWarn/warningText/noQueryWarningText) or
// S1's three tests / local constants. Only ADD the import line, the two functions, and the two
// tests. If S1 lands in parallel, resolve any merge by keeping all S1 code intact.

// GOTCHA — keep teach.go and teach_test.go gofmt-clean. The new doc comments are multi-line
// (Mode A) but must be valid Go comment blocks (// per line), citing PRD §12.3/§12.2/FR-6 and
// the SDK facts (pointer-receiver TextContent, never SetError).

// GOTCHA — reflect.DeepEqual on []mcp.Content works for TextContent blocks: it follows the
// interface dynamic type and the *TextContent pointer target, comparing the pointed-to struct
// ({Text, Meta:zero, Annotations:nil}). Two distinct &TextContent{Text:"a"} are DeepEqual.
// Prefer asserting the observable Content slice + IsError (not the whole *CallToolResult, which
// includes the unexported `err` field — both nil → equal, but observable-field asserts are clearer).

// GOTCHA — appendWarning assumes result is NON-NIL (precondition). In the real dispatch result
// is always the non-nil upstream *CallToolResult. No defensive nil guard (would mask caller
// bugs; matches shouldWarn's no-defensive-checks style). Document the precondition in the doc comment.
```

## Implementation Blueprint

### Data models and structure

No new data model. S2 consumes the SDK types `*mcp.CallToolResult`, `mcp.Content`, and
`*mcp.TextContent` (verified in research §1). S1's `ExtractionResult` is untouched and
NOT referenced by the two new functions (they take already-built warning text).

### The exact function bodies (copy verbatim)

```go
package main

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ... S1's shouldWarn / warningText / noQueryWarningText remain UNCHANGED here ...

// appendWarning appends the teaching warning to an upstream search result, AFTER its
// existing content blocks (PRD §12.3). In the delegate flow the server first obtains the
// real search results (a *mcp.CallToolResult from the upstream client), and only then —
// when shouldWarn reported a non-canonical call — appends the warning produced by
// warningText. The ordering matters: results lead, the warning trails, so the model acts
// on the results rather than retrying the tool call on seeing a lone warning.
//
// appendWarning is APPEND-ONLY: it adds a single &mcp.TextContent{Text: text} as the LAST
// element of result.Content. It does not replace, prepend, or reorder existing blocks.
// It does not touch result.IsError: per FR-6 and PRD §12.2 "isError is never set for any
// normalization, guidance, or warning case", and because we never call result.SetError
// (which would set IsError=true and, when Content is empty, overwrite it), IsError stays
// at its zero value (false). text is the already-built warning string (see warningText).
//
// result must be non-nil; in the dispatch it is always the non-nil upstream result.
func appendWarning(result *mcp.CallToolResult, text string) {
	result.Content = append(result.Content, &mcp.TextContent{Text: text})
}

// noQueryResult builds the IMMEDIATE warning result returned when extraction found no
// usable query (PRD §10.1.5, §12.1, FR-6): no upstream call is made and there is nothing
// to append after. The warning IS the only content. This is the single sanctioned case in
// which a warning travels without results (PRD §12.3) — a retry with correct input is
// exactly what we want.
//
// The returned *mcp.CallToolResult is fresh and non-nil, with Content set to a single
// &mcp.TextContent{Text: text} (the same canonical result shape the SDK itself uses). IsError
// is left at its zero value (false): per FR-6/PRD §12.2 it is never set for any warning
// case, and we build the result by hand rather than calling SetError (which would set
// IsError=true and overwrite Content). StructuredContent and Meta remain nil. text is the
// already-built no-results warning string (see noQueryWarningText).
func noQueryResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: CONFIRM S1's teach.go is present (parallel-execution gate)
  - RUN: test -f teach.go && grep -q 'func shouldWarn' teach.go && grep -q 'func noQueryWarningText' teach.go
  - EXPECT: exit 0. teach.go (created by P1.M3.T1.S1) must exist with shouldWarn,
        warningText, noQueryWarningText and `import "fmt"`. If absent, S1 has not landed
        yet — STOP and surface it (S2 modifies the file S1 creates). Do NOT create teach.go
        from scratch (that is S1's scope).

Task 1: MODIFY teach.go — add the mcp import + the two functions
  - FILE: teach.go (EXISTING). ADD `"github.com/modelcontextprotocol/go-sdk/mcp"` to the
        import block alongside S1's `"fmt"` (gofmt will group them: stdlib blank line then
        third-party). PASTE the two function bodies above verbatim (appendWarning /
        noQueryResult), each WITH its Mode-A doc comment, AFTER S1's three functions.
  - CONSTRAINT: use &mcp.TextContent{Text: text} (POINTER — see Known Gotchas). appendWarning
        is a single append call (append-only); noQueryResult is a single struct-literal
        return. Neither references IsError or SetError.
  - PRESERVE: S1's shouldWarn / warningText / noQueryWarningText and S1's `"fmt"` import
        are UNCHANGED. PLACEMENT: repo root (package main), functions after S1's.

Task 2: EXTEND teach_test.go — add two table-driven tests
  - FILE: teach_test.go (EXISTING). Ensure the import block has the SDK package; the tests
        reference mcp.CallToolResult / mcp.Content / mcp.TextContent, so add
        "github.com/modelcontextprotocol/go-sdk/mcp" to the imports alongside reflect/testing.
        (S1's imports are reflect+testing; keep them, add mcp.)
  - ADD a helper (so error messages are readable and the text-only assumption is explicit):
        func contentTexts(cs []mcp.Content) []string {
            out := make([]string, len(cs))
            for i, c := range cs { out[i] = c.(*mcp.TextContent).Text }
            return out
        }
  - ADD TestAppendWarning: table of {name, initial []mcp.Content, text string,
        wantContent []mcp.Content}. Build res := &mcp.CallToolResult{Content: tc.initial};
        call appendWarning(res, tc.text); assert reflect.DeepEqual(res.Content, tc.wantContent)
        AND res.IsError == false. Rows: single_block, multi_block_ordering (THE §12.3 test),
        append_to_empty, double_append (append twice → warning appears twice, still last,
        IsError false). See "Test rows" below.
  - ADD TestNoQueryResult: table of {name, text string, wantContent []mcp.Content}. Call
        res := noQueryResult(tc.text); assert reflect.DeepEqual(res.Content, tc.wantContent)
        AND res.IsError == false AND res != nil AND len(res.Content) == 1. Rows: nonempty,
        empty_text_edge. See "Test rows" below.
  - NAMING: TestAppendWarning / TestNoQueryResult; t.Run subtests with descriptive names.
        PLACEMENT: repo root alongside S1's tests. PRESERVE S1's tests + local constants.

Task 3: VALIDATE
  - RUN: gofmt -w teach.go teach_test.go; go vet ./...; go build ./...;
        go test -run 'TestAppendWarning|TestNoQueryResult' -v;
        go test ./...
  - CONFIRM: teach.go imports fmt + mcp; neither function references SetError/IsError;
        go.mod unchanged (git diff --stat go.mod empty); S1's tests still pass.
```

### Test rows (copy the wantContent values verbatim)

**TestAppendWarning** (the multi_block_ordering row IS the §12.3 invariant):

| name | initial Content | text | wantContent (after) | IsError |
|---|---|---|---|---|
| single_block | [&TextContent{Text:"result1"}] | "WARN" | [&TC{"result1"}, &TC{"WARN"}] | false |
| multi_block_ordering | [&TC{"result1"}, &TC{"result2"}] | "WARN" | [&TC{"result1"}, &TC{"result2"}, &TC{"WARN"}] | false |
| append_to_empty | nil | "WARN" | [&TC{"WARN"}] | false |
| double_append | [&TC{"result1"}] | "W1" then "W2" | [&TC{"result1"}, &TC{"W1"}, &TC{"W2"}] | false |

(&TC{x} is shorthand for &mcp.TextContent{Text: x}. Each row asserts
`reflect.DeepEqual(res.Content, wantContent)` — this single check proves append-only,
warning-last, and exact text — plus `res.IsError == false`.)

**TestNoQueryResult**:

| name | text | wantContent | IsError | non-nil | len(Content) |
|---|---|---|---|---|---|
| nonempty | "WARN TEXT" | [&mcp.TextContent{Text:"WARN TEXT"}] | false | true | 1 |
| empty_text_edge | "" | [&mcp.TextContent{Text:""}] | false | true | 1 |

### Implementation Patterns & Key Details

```go
// PATTERN (TestAppendWarning row — the §12.3 ordering + invariant in one assertion):
res := &mcp.CallToolResult{Content: tc.initial}
appendWarning(res, tc.text)
if !reflect.DeepEqual(res.Content, tc.wantContent) {
	t.Errorf("appendWarning Content =\n %v\nwant\n %v", contentTexts(res.Content), contentTexts(tc.wantContent))
}
if res.IsError {
	t.Errorf("appendWarning set IsError=true; must stay false")
}

// PATTERN (TestNoQueryResult row):
res := noQueryResult(tc.text)
if res == nil {
	t.Fatalf("noQueryResult returned nil")
}
if !reflect.DeepEqual(res.Content, tc.wantContent) {
	t.Errorf("noQueryResult Content =\n %v\nwant\n %v", contentTexts(res.Content), contentTexts(tc.wantContent))
}
if res.IsError {
	t.Errorf("noQueryResult IsError=true; must be false")
}
if len(res.Content) != 1 {
	t.Errorf("noQueryResult len(Content) = %d, want 1 (warning is the only content)", len(res.Content))
}

// PATTERN (helper — makes failures readable and pins the text-only assumption):
func contentTexts(cs []mcp.Content) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.(*mcp.TextContent).Text // panics if a non-text block appears (desired in a test)
	}
	return out
}

// GOTCHA: build expected slices with &mcp.TextContent{Text:...} (pointer), never a value —
// a value does not implement mcp.Content and won't compile in []mcp.Content.

// GOTCHA: do NOT assert on the whole *mcp.CallToolResult with reflect.DeepEqual expecting
// portability across SDK internals — it works (unexported `err` is nil on both sides) but
// asserting Content + IsError is clearer and decouples from the SDK's private fields.
```

### Integration Points

```yaml
FILES MODIFIED:
  - teach.go       (EXISTING, from S1): import block gains "github.com/modelcontextprotocol/
        go-sdk/mcp"; ADD appendWarning + noQueryResult with Mode-A doc comments.
  - teach_test.go  (EXISTING, from S1): import block gains the mcp package; ADD
        contentTexts helper + TestAppendWarning + TestNoQueryResult.
NO OTHER FILES TOUCHED:
  - extract.go/extract_test.go, config*.go, logger.go, health.go, main.go, doc.go: UNTOUCHED.
  - config_test.go/resolve_test.go/health_test.go: owned by P1.M1.T3.S1 — DO NOT TOUCH.
  - go.mod/go.sum: UNCHANGED (SDK require already present; main.go already imports mcp).
CONSUMER SEAMS (S2 freezes the result-assembly seam; M5.T2 builds the dispatch on it):
  - P1.M5.T2 (server dispatch): the handler does
        ext := extract(req.Params.Arguments, cfg.QueryAliases, cfg.OptionalAliases)
        if !ext.Found { return noQueryResult(noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam)), nil }
        upstream, err := delegate(...)
        if err != nil { return /* honest error via SDK error return, NOT SetError */ }
        if shouldWarn(req.Params.Name, ext, cfg.CanonicalTool, cfg.CanonicalParam) {
            appendWarning(upstream, warningText(req.Params.Name, ext.Source, cfg.CanonicalTool, cfg.CanonicalParam))
        }
        return upstream, nil
  - P1.M5.T3 (server_test.go e2e): §19.3 case 4 asserts noQueryResult's output (no upstream,
        immediate no-results warning, isError:false); cases 2/3 assert appendWarning's
        results-then-warning ordering.
DATABASE / ROUTES / ENV: none. The two functions are pure MCP-struct assembly (no I/O, no
        network, no globals); appendWarning's only effect is mutating the passed result slice.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# After editing teach.go + teach_test.go — fix before running tests.
gofmt -w teach.go teach_test.go
go vet ./...
go build ./...                  # must compile; the mcp import resolves (already in go.mod)

# teach.go imports BOTH fmt (S1) and mcp (S2):
grep -A4 '^import' teach.go     # expect "fmt" and "github.com/modelcontextprotocol/go-sdk/mcp"

# Neither new function references SetError or sets IsError:
grep -n 'SetError\|IsError' teach.go   # MUST print nothing (appendWarning/noQueryResult never touch them)

# S1's three functions are still present and unchanged:
grep -n 'func shouldWarn\|func warningText\|func noQueryWarningText' teach.go   # 3 hits

# go.mod is UNCHANGED (the SDK require already existed; main.go already imported mcp):
git diff --stat go.mod          # expect EMPTY

# Confirm only the two files changed (no new files, no other edits):
git status --short              # expect:  M teach.go   M teach_test.go   (plus whatever S1 adds if parallel)

# Expected: zero errors; teach.go imports fmt+mcp; no SetError/IsError refs in the new fns;
# S1's functions intact; go.mod clean.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Run the new tests in isolation, verbose.
go test -run 'TestAppendWarning|TestNoQueryResult' -v

# Full teach-test suite (S1 + S2 together) + the whole module stay green.
go test -run 'TestShouldWarn|TestWarningText|TestNoQueryWarningText|TestAppendWarning|TestNoQueryResult' -v
go test ./...

# Expected: PASS. Spot-check the load-bearing assertions:
#   TestAppendWarning/multi_block_ordering: [result1,result2] + append "WARN"
#        -> Content == [result1, result2, WARN] (results first, warning LAST) and IsError==false.
#   TestAppendWarning/append_to_empty: append to nil Content -> [WARN], IsError==false.
#   TestAppendWarning/double_append: two appends -> warning appears twice, still last, IsError==false.
#   TestNoQueryResult/nonempty: Content == [WARN TEXT], IsError==false, len==1, non-nil.
#   TestNoQueryResult/empty_text_edge: Content == [""], IsError==false, len==1.
# Any failure means a prepend/reorder, a stray IsError=true, or a value (non-pointer)
# TextContent — the reflect.DeepEqual assertion pinpoints it via the contentTexts diff.
```

### Level 3: Integration Testing (System Validation)

```bash
# The two functions are pure MCP-struct assembly — the "integration" is the consumer-call-
# shape smoke: prove the real M5.T2 dispatch shape wires extract -> Found gate -> (noQueryResult
# | delegate+appendWarning) end-to-end with the REAL DefaultConfig and S1's text functions,
# producing results the SDK can marshal.

cat > /tmp/teach_asm_smoke_test.go <<'EOF'
package main

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTeachAsmSmoke_ConsumerShape(t *testing.T) {
	cfg := DefaultConfig() // the real canonical values the server passes
	ext := extract(json.RawMessage(`{"q":"rust async runtime","location":"US"}`),
		cfg.QueryAliases, cfg.OptionalAliases)
	if !ext.Found {
		t.Fatalf("alias call should be Found: %+v", ext)
	}

	// Delegate path: build a fake upstream result, append the warning, assert ordering + isError.
	upstream := &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: "result: rust async runtime"},
	}}
	if !shouldWarn("web_search", ext, cfg.CanonicalTool, cfg.CanonicalParam) {
		t.Fatalf("alias call should warn")
	}
	appendWarning(upstream, warningText("web_search", ext.Source, cfg.CanonicalTool, cfg.CanonicalParam))
	if len(upstream.Content) != 2 {
		t.Fatalf("expected 2 content blocks (result + warning), got %d", len(upstream.Content))
	}
	if got := upstream.Content[0].(*mcp.TextContent).Text; got != "result: rust async runtime" {
		t.Fatalf("first block (result) wrong: %q", got)
	}
	if upstream.IsError {
		t.Fatalf("delegate result must have IsError==false")
	}

	// No-query path: {} -> Found==false -> noQueryResult, no upstream call.
	noq := extract(json.RawMessage(`{}`), cfg.QueryAliases, cfg.OptionalAliases)
	if noq.Found {
		t.Fatalf("{} should be Found==false")
	}
	res := noQueryResult(noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam))
	if res == nil || len(res.Content) != 1 || res.IsError {
		t.Fatalf("noQueryResult bad shape: %+v", res)
	}

	// Canonical call: web_search + {"query":...} -> shouldWarn false -> NO appendWarning.
	canonical := extract(json.RawMessage(`{"query":"rust async runtime"}`),
		cfg.QueryAliases, cfg.OptionalAliases)
	if shouldWarn(cfg.CanonicalTool, canonical, cfg.CanonicalTool, cfg.CanonicalParam) {
		t.Fatalf("canonical call should NOT warn")
	}
}
EOF
cp /tmp/teach_asm_smoke_test.go ./zz_asm_smoke_test.go && go test -run TestTeachAsmSmoke_ConsumerShape -v && rm ./zz_asm_smoke_test.go

# Expected: smoke compiles (real DefaultConfig + extract + S1 text + S2 assembly + the mcp
# types) and passes. This proves S2's seam slots into the dispatch M5.T2 will build: results
# lead and the warning trails on the delegate path; the no-query path returns a lone warning
# with IsError false; the canonical path appends nothing.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Wire-format fidelity: marshal the results and assert the JSON the client actually sees.
# This catches (a) a value TextContent (would not implement Content -> compile error earlier),
# (b) an accidental SetError/isError:true, (c) prepend-vs-append, (d) wrong element order.
# TextContent.MarshalJSON emits {"type":"text","text":"..."}; isError is omitempty (absent when false).

cat > /tmp/teach_wire_test.go <<'EOF'
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestTeachWireFidelity(t *testing.T) {
	// noQueryResult: single text block, NO isError field, NO structuredContent/_meta.
	res := noQueryResult("W")
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal noQueryResult: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := m["isError"]; ok {
		t.Errorf("noQueryResult must NOT emit isError (must stay false/omitempty): %s", b)
	}
	if _, ok := m["structuredContent"]; ok {
		t.Errorf("noQueryResult must NOT emit structuredContent: %s", b)
	}
	content, _ := m["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("noQueryResult content len = %d, want 1: %s", len(content), b)
	}
	block, _ := content[0].(map[string]any)
	if block["type"] != "text" || block["text"] != "W" {
		t.Errorf("noQueryResult block wrong: %s", b)
	}

	// appendWarning: results first, warning LAST; still NO isError.
	upstream := &mcp.CallToolResult{Content: []mcp.Content{
		&mcp.TextContent{Text: "result1"},
		&mcp.TextContent{Text: "result2"},
	}}
	appendWarning(upstream, "W")
	b2, _ := json.Marshal(upstream)
	if strings.Contains(string(b2), `"isError":true`) {
		t.Errorf("appendWarning must not set isError:true: %s", b2)
	}
	var m2 map[string]any
	_ = json.Unmarshal(b2, &m2)
	blocks, _ := m2["content"].([]any)
	if len(blocks) != 3 {
		t.Fatalf("appendWarning content len = %d, want 3: %s", len(blocks), b2)
	}
	last := blocks[2].(map[string]any)
	if last["text"] != "W" {
		t.Errorf("appendWarning warning must be LAST: %s", b2)
	}
	first := blocks[0].(map[string]any)
	if first["text"] != "result1" {
		t.Errorf("appendWarning must preserve first result: %s", b2)
	}
}
EOF
cp /tmp/teach_wire_test.go ./zz_wire_test.go && go test -run TestTeachWireFidelity -v && rm ./zz_wire_test.go

# Expected: noQueryResult marshals to {"content":[{"type":"text","text":"W"}]} with NO isError;
# appendWarning preserves [result1,result2] then appends "W" as the last block, no isError:true.
# This is the strongest guarantee S2 gives the e2e consumers (M5.T3) that compare the
# client-visible wire format.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: gofmt clean; `go vet ./...` and `go build ./...` exit 0; teach.go
      imports fmt + mcp; no `SetError`/`IsError` reference in the new functions; S1's three
      functions intact; `git diff --stat go.mod` empty.
- [ ] Level 2 passes: `go test -run 'TestAppendWarning|TestNoQueryResult' -v` and
      `go test ./...` both PASS.
- [ ] Level 3 passes: the consumer-shape smoke compiles with real DefaultConfig + S1 text
      + S2 assembly and passes (results-lead/warning-trail; no-query lone warning; canonical
      no-append).
- [ ] Level 4 passes: noQueryResult marshals to a single text block with NO isError field;
      appendWarning preserves results and appends the warning last with no isError:true.

### Feature Validation

- [ ] `appendWarning` appends exactly one `&mcp.TextContent{Text: text}` as the LAST block;
      existing blocks preserved first and in order; `IsError` never set (stays false).
- [ ] `noQueryResult` returns a non-nil `*mcp.CallToolResult` with Content ==
      `[]mcp.Content{&mcp.TextContent{Text: text}}` and `IsError` == false.
- [ ] The multi_block_ordering test row proves PRD §12.3 (results lead, warning trails).
- [ ] The append_to_empty and double_append rows prove robustness without breaking the
      invariant.

### Code Quality Validation

- [ ] teach.go gains the `mcp` import; S1's `"fmt"` and three functions are unchanged.
- [ ] Doc comments are Mode-A (cite PRD §12.3/§12.2/FR-6; the append-only ordering; the
      no-results exception; the pointer-receiver `&TextContent` rule; that IsError is never
      set and SetError is never called).
- [ ] teach_test.go mirrors extract_test.go / S1's conventions (package main; reflect+
      testing+mcp; table-driven + t.Run; reflect.DeepEqual on the Content slice).
- [ ] No anti-patterns (see below); no value (non-pointer) TextContent; no SetError; no
      IsError set; no prepend/reorder; no canonical-value params; no Config import.

### Documentation & Deployment

- [ ] Mode A docs honored: doc comments on both functions; NO README/config.example.json/
      doc.go changes (the teaching signal is internal, surfaced only via the MCP response
      that M5.T2 assembles with these functions).
- [ ] No new env vars / config keys / routes / go.mod requires introduced.

---

## Anti-Patterns to Avoid

- ❌ Don't use a value `mcp.TextContent{...}` in `[]mcp.Content`. `TextContent`'s methods are
  POINTER receivers, so only `*TextContent` implements `Content`. Always write
  `&mcp.TextContent{Text: ...}`. (A value form is a compile error in this context — the
  compiler enforces it — but write the `&` deliberately.)
- ❌ Don't call `result.SetError(...)` anywhere. It sets `IsError=true` AND overwrites
  `Content` when empty (`protocol.go:131`). Build results by hand and leave `IsError` at
  zero (false). Verified against the SDK source.
- ❌ Don't make `appendWarning` do anything but a single `append`. No prepend, no slice
  rebuild, no reordering, no length pre-check that rebuilds. Existing upstream blocks must
  stay first, in order; the warning must be LAST (PRD §12.3).
- ❌ Don't set or read `IsError` in `appendWarning`. It must not touch the field at all
  (it stays false in our flow). `noQueryResult` must leave `IsError` at zero via the struct
  literal — never assign it, never call `SetError`.
- ❌ Don't give `appendWarning`/`noQueryResult` canonical-tool/param parameters. They take
  already-built warning TEXT (a plain string). The caller (M5.T2) builds text with S1's
  `warningText`/`noQueryWarningText`. Keeping canonical values out keeps these functions
  pure MCP-assembly and unit-testable without config.
- ❌ Don't import `config` in teach.go. Neither S1 nor S2 needs it. (S2 needs only `fmt` (S1)
  + `mcp` (S2).)
- ❌ Don't edit `go.mod`/`go.sum`. The SDK `require` already exists (P1.M1.T2.S1) and
  `main.go` already imports `mcp`. Adding the import to teach.go needs no module change.
- ❌ Don't modify S1's `shouldWarn`/`warningText`/`noQueryWarningText`, S1's tests, or S1's
  local constants. S2 only ADDS the import line, the two functions, and the two tests. If
  S1 lands in parallel, resolve any conflict by keeping all S1 code intact.
- ❌ Don't build the delegate dispatch or call the upstream client here. That is M5.T2's
  scope (and M4 for the client). S2 freezes ONLY the two result-assembly functions.
- ❌ Don't assert on the whole `*mcp.CallToolResult` with `reflect.DeepEqual` as the primary
  check — it works (unexported `err` is nil on both sides) but couples the test to SDK
  internals. Assert the observable `Content` slice + `IsError` instead.
