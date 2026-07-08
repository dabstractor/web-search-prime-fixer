# Research — teach.go append-after-results + no-results warning + isError invariant (PRD §12.3)

Item **P1.M3.T1.S2** in plan `002_0a8ab3410994`. All SDK facts below were verified
by reading the actual Go MCP SDK source at
`$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`.
File/line citations are exact.

This item **MODIFIES** the `teach.go` that P1.M3.T1.S1 creates (adds two functions +
the SDK import) and **EXTENDS** `teach_test.go` (adds two test functions). S1 and S2
run in parallel; this PRP treats S1's teach.go as a frozen contract (three pure
functions `shouldWarn`/`warningText`/`noQueryWarningText`, import `"fmt"` only).

---

## 1. The two SDK types this item introduces into teach.go

### mcp.CallToolResult — `protocol.go:71`
```go
type CallToolResult struct {
    Meta              `json:"_meta,omitempty"`
    Content           []Content `json:"content"`
    StructuredContent any       `json:"structuredContent,omitempty"`
    IsError           bool      `json:"isError,omitempty"` // NEVER set for warnings
    err               error     // unexported; set only by SetError
}
```
- `Content []Content` is the slice of result content blocks the client sees.
- `IsError bool` defaults to `false` (zero value). `json:"isError,omitempty"` → it is
  ABSENT from the wire when false. We never touch it for any normalization/warning
  case, so it stays false and is omitted from the JSON. This is exactly FR-6 /
  PRD §12.2 ("isError is never set for any normalization, guidance, or warning case").
- `err` is unexported and set ONLY by `SetError`. We never call `SetError`, so `err`
  stays nil. (Note for tests: `reflect.DeepEqual` on a `*CallToolResult` compares
  `err` too, but both sides nil → equal. We still prefer asserting observable fields.)

### mcp.Content interface — `content.go:17`
```go
type Content interface {
    MarshalJSON() ([]byte, error)
    fromWire(*wireContent)
}
```

### mcp.TextContent — `content.go:28`  (POINTER-RECEIVER methods!)
```go
type TextContent struct {
    Text        string
    Meta        Meta
    Annotations *Annotations
}
func (c *TextContent) MarshalJSON() ([]byte, error) { ... }   // pointer receiver
func (c *TextContent) fromWire(wire *wireContent) { ... }      // pointer receiver
```
**CRITICAL: both methods have POINTER receivers.** Therefore a `TextContent` VALUE does
NOT satisfy the `Content` interface — only `*TextContent` does. So every content block
we build MUST be `&mcp.TextContent{Text: "..."}`. A bare `mcp.TextContent{...}` will
not even compile where `[]mcp.Content` is expected (compile error: does not implement
interface). This is the #1 correctness trap and the compiler catches it for us, but
the doc comment + tests should pin the `&` form. Verified the SDK's own canonical
usage is always the `&` form: `mcp_test.go:43`, `content_nil_test.go:32`,
`tool_example_test.go:40`, `protocol.go:134` all write `&TextContent{Text: "..."}`.

---

## 2. The SetError trap — `protocol.go:131-140` (NEVER call it)

```go
func (r *CallToolResult) SetError(err error) {
    if len(r.Content) == 0 || seterroroverwrite == "1" {
        r.Content = []Content{&TextContent{Text: err.Error()}}
    }
    r.IsError = true
    r.err = err
}
```
- `SetError` sets `IsError = true` unconditionally.
- It also OVERWRITES `Content` with a single error-text block **whenever Content is
  empty** (the normal case for our no-results result). There is an escape hatch
  (`MCPGODEBUG=seterroroverwrite=1` restores the always-overwrite behavior), but that
  only makes it MORE destructive — it never makes it less.
- **Therefore we NEVER call `SetError`** for any normalization/guidance/warning case
  (PRD §12.2: "isError is never set for any normalization, guidance, or warning case").
  We build results by hand with `IsError` left at its zero value (false). The
  architecture doc `mcp_sdk_api.md` §2 states this verbatim. Confirmed against source.
- `GetError()` returns the unexported `err`; always nil for us (we never set it).

---

## 3. The two function contracts (item spec, verified against SDK)

### (a) appendWarning(result *mcp.CallToolResult, text string)
Appends a single `&mcp.TextContent{Text: text}` to `result.Content` AFTER the existing
blocks. Does NOT touch `result.IsError` (stays false in our flow). Does NOT replace or
prepend. This is the delegate-flow tail: upstream results come first, warning appended
last, so the model acts on the results, not the warning (PRD §12.3).

Body (the whole function):
```go
func appendWarning(result *mcp.CallToolResult, text string) {
    result.Content = append(result.Content, &mcp.TextContent{Text: text})
}
```
- `append` to a nil Content yields a single-element slice `[warning]` (robust edge case;
  normal flow always has upstream results present). The function is append-ONLY by
  construction — it cannot replace or reorder existing blocks.
- PRECONDITION (documented): `result` must be non-nil. In the real dispatch (M5.T2)
  `result` is always the non-nil upstream `*CallToolResult`. No defensive nil guard
  (would mask caller bugs; matches shouldWarn's no-defensive-checks style).

### (b) noQueryResult(text string) *mcp.CallToolResult
Builds a FRESH result whose ONLY content is the warning text, with `IsError` left false
(zero value — never set). Returned when extraction found no query (no upstream call made;
PRD §10.1.5, §12.1, FR-6).

Body (the whole function):
```go
func noQueryResult(text string) *mcp.CallToolResult {
    return &mcp.CallToolResult{
        Content: []mcp.Content{&mcp.TextContent{Text: text}},
    }
}
```
- This is byte-for-byte the SDK's canonical result-construction pattern
  (`&CallToolResult{Content: []Content{&TextContent{Text: "..."}}}`), verified at
  `mcp_test.go:43` and `tool_example_test.go:40`. `IsError` is omitted by construction.
- `StructuredContent` and `Meta` stay at zero values (nil) — we never populate them.

### Neither function takes canonical values.
Both take already-built warning `text`. The CALLER (M5.T2) produces that text via S1's
pure functions:
```go
// pseudocode for the dispatch S2 plugs into (owned by P1.M5.T2):
ext := extract(req.Params.Arguments, cfg.QueryAliases, cfg.OptionalAliases)
if !ext.Found {
    return noQueryResult(noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam)), nil
}
upstream, err := delegate(...)            // P1.M4 upstream
if err != nil { return /* honest error */ }
if shouldWarn(req.Params.Name, ext, cfg.CanonicalTool, cfg.CanonicalParam) {
    appendWarning(upstream, warningText(req.Params.Name, ext.Source, cfg.CanonicalTool, cfg.CanonicalParam))
}
return upstream, nil
```
S2 freezes ONLY the two assembly functions; the dispatch glue is M5.T2.

---

## 4. teach.go evolution: S1 (pure, fmt-only) → S2 (adds the SDK import + 2 functions)

S1's teach.go (frozen contract) is:
```go
package main
import "fmt"
// shouldWarn, warningText, noQueryWarningText  (pure; no SDK types)
```
S2 MODIFIES it to:
```go
package main
import (
    "fmt"
    "github.com/modelcontextprotocol/go-sdk/mcp"
)
// shouldWarn, warningText, noQueryWarningText  (S1 — UNCHANGED)
// appendWarning, noQueryResult                  (S2 — NEW; the MCP-assembly layer)
```
- `go.mod` is UNCHANGED: the `require github.com/modelcontextprotocol/go-sdk v1.6.1`
  line already exists (added in P1.M1.T2.S1) and `main.go` already imports the `mcp`
  package. teach.go just adds a second import to its own import block.
- The pure/text functions (S1) and the MCP-assembly functions (S2) coexist in one file:
  teach.go is the single home for the teaching signal — pure decision+text AND the
  MCP-result assembly that wraps it. The doc comments mark the layer split.

---

## 5. Test design (teach_test.go EXTEND — two new table-driven tests)

Mirror `extract_test.go` / S1's `teach_test.go`: `package main`, `import (reflect, testing)`,
table-driven `tests := []struct{...}` + `t.Run`. The new tests assert on the OBSERVABLE
fields of the result (Content slice + IsError), not the unexported `err`.

### reflect.DeepEqual on []mcp.Content works for TextContent blocks
`reflect.DeepEqual([]mcp.Content{&mcp.TextContent{Text:"a"}}, []mcp.Content{&mcp.TextContent{Text:"a"}})`
is TRUE: DeepEqual follows interface dynamic types and pointer targets, comparing the
pointed-to `TextContent` structs (both `{Text:"a", Meta:zero, Annotations:nil}`). So we
can assert the WHOLE Content slice in one call, which catches every bug at once: wrong
ordering, replaced/prepended content, missing block, wrong text. This is the project's
established convention (`reflect.DeepEqual` on whole values).

### TestAppendWarning — the §12.3 ordering + append-only + IsError-invariant test
Rows:
| name | initial Content | append text | want Content (after) | IsError |
|---|---|---|---|---|
| single_block | [result1] | WARN | [result1, WARN] | false |
| multi_block_ordering | [result1, result2] | WARN | [result1, result2, WARN] | false |
| append_to_empty | [] (nil) | WARN | [WARN] | false |
| preserves_non_text_unchecked | (we only ever see TextContent upstream; covered by the type-assert helper) | | | |

Key assertions per row:
- `reflect.DeepEqual(res.Content, wantContent)` — proves append-only AND warning-last.
- `res.IsError == false` — the invariant (never set).
- `len(res.Content) == len(initial)+1` — exactly one block added.
- (Robustness) appending twice yields the warning twice and still IsError false.

Use a tiny helper to read text out for error messages:
```go
func contentTexts(cs []mcp.Content) []string {
    out := make([]string, len(cs))
    for i, c := range cs { out[i] = c.(*mcp.TextContent).Text }
    return out
}
```
(The type-assert `.(*mcp.TextContent)` documents that we only ever produce/consume text
blocks in the teaching signal — and would panic loudly if a non-text block appeared,
which is what we want in a test.)

### TestNoQueryResult — the immediate no-results + IsError-invariant test
Rows:
| name | text | want Content | IsError | non-nil |
|---|---|---|---|---|
| nonempty | WARN TEXT | [WARN TEXT] | false | true |
| empty_text_edge | "" | [""] | false | true |

Key assertions:
- `reflect.DeepEqual(res.Content, []mcp.Content{&mcp.TextContent{Text: text}})` — exactly
  one block, the warning IS the only content.
- `res.IsError == false` — invariant (never set; also `"isError"` is omitempty so it is
  absent from the wire).
- `len(res.Content) == 1` — nothing else attached.
- `res != nil`.

### §19.2 coverage these tests provide
The two new tests own the S2 half of PRD §19.2: "warning appended after results"
(TestAppendWarning multi_block_ordering), "the immediate no-results warning"
(TestNoQueryResult), "isError never set" (both tests). S1's tests already cover "no
warning for canonical", "warning for every other case", "example text present and
correct".

---

## 6. Wire-format fidelity check (Level 4) — proves the client actually sees the bytes

The real proof that `&mcp.TextContent` (not a value) is correct: marshal the result and
assert the JSON. `TextContent.MarshalJSON` emits `{"type":"text","text":"..."}`. So:

- `noQueryResult("W")` marshals to `{"content":[{"type":"text","text":"W"}]}` — NO
  `isError` field (omitempty; it is false), NO `structuredContent`, NO `_meta`.
- After `appendWarning` on `[result1]` with "W": marshals to
  `{"content":[{"type":"text","text":"result1"},{"type":"text","text":"W"}]}`.

Asserting this JSON catches: (a) a value `TextContent` (would not implement Content →
compile error, caught earlier), (b) accidentally calling SetError (would add
`"isError":true` and on empty Content overwrite it), (c) prepending instead of appending
(wrong element order in the JSON array). This is the strongest guarantee S2 gives the
e2e consumers (M5.T3) that compare the client-visible wire format.

Implementation note: marshal via `json.Marshal(res)` — `*CallToolResult` has the SDK's
custom marshal path. To assert, unmarshal into `map[string]json.RawMessage` or
`map[string]any` and check `content` length/order/text and that `"isError"` is absent.

---

## 7. Consumer map (what S2 must produce for the rest of P1.M3/P1.M5)

| Consumer | What it calls | Why S2's contract matters |
|---|---|---|
| **P1.M5.T2** (server dispatch) | `appendWarning(upstream, warningText(...))`, `noQueryResult(noQueryWarningText(...))` | M5.T2 wires extract → Found gate → upstream → teach. S2's two functions ARE the MCP-result assembly; M5.T2 just calls them with S1's text. S2's signatures + IsError=false invariant are the contract. |
| **P1.M5.T3** (server_test.go e2e §19.3) | indirectly | E2E case 4 (`web_search + {}` → no upstream, immediate no-results warning, isError:false) asserts `noQueryResult`'s output. Cases 2/3 (results then warning) assert `appendWarning`'s ordering. S2's wire format is what those tests compare against. |

DATABASE / ROUTES / ENV: none. The two functions are pure MCP-struct assembly (no I/O,
no globals, no network) — the only "side effect" is mutating/appending the passed result
slice, which is the whole point.

---

## 8. Gotchas summary (anti-patterns)

- `TextContent` methods are POINTER receivers → only `*TextContent` implements `Content`.
  Always use `&mcp.TextContent{Text: ...}`. (A value form is a compile error in
  `[]mcp.Content` context; still, write the `&`.)
- NEVER call `result.SetError(...)`: sets IsError=true AND overwrites Content when empty.
  Build results by hand; leave IsError at zero (false).
- `appendWarning` is append-ONLY: a single `append` call. Do NOT prepend, do NOT rebuild
  the slice, do NOT reorder. The existing upstream blocks must remain first, in order.
- `appendWarning` must NOT set IsError. It must not even READ IsError — it leaves it
  untouched (stays false in our flow). (Testing: assert IsError==false before AND after.)
- `noQueryResult` must NOT use SetError or set IsError; the `&CallToolResult{...}` literal
  leaves IsError at its zero value. StructuredContent/Meta stay nil.
- Do NOT give either function canonical-tool/param params — they take already-built
  warning TEXT. The caller (M5.T2) builds text with S1's warningText/noQueryWarningText.
- teach.go gains the `mcp` import but go.mod is unchanged (require already present).
- Do NOT modify S1's three functions or S1's three tests — only ADD the two functions and
  the two tests. If a merge with S1 is needed (parallel execution), resolve by keeping
  S1's code intact and adding S2's code alongside.
