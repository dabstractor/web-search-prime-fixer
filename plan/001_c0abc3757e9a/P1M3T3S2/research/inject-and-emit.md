# Research — P1.M3.T3.S2: SSE Inject + emitEvent

Verification of the load-bearing facts for the warning-injection pipeline. All
Go-side claims below were **verified by running a program against the installed
toolchain** (go1.26.4) — outputs reproduced verbatim. Protocol-side claims
(WHATWG SSE) are cross-referenced to `architecture/external_deps.md §3` (already
on-disk research) and the PRD.

---

## 1. INPUTS that exist when this item runs (contract with upstream items)

| Symbol | Producer | Where | Shape |
|--------|----------|-------|-------|
| `Event` / `Reader` / `NewSSEReader(r)` / `(*Reader).Next()` | **P1.M3.T1.S1** (reader) | `sse.go` (DONE, on disk) | `Next()` returns `(Event, io.EOF)` at end; `(Event, err)` otherwise. `Event{ID,Type,Data}`; `Data` = `data:` values joined with `"\n"` (no trailing newline). |
| `warningText(notes []string) string` | **P1.M3.T3.S1** (parallel) | appended to `sse.go` | returns the PRD §12.3 line (or `""` for empty). This item receives the RESULT as the `warning` arg — it does NOT call `warningText` itself. |
| `testdata/tools_call.sse` | **P1.M3.T2.S1** (fixtures) | `testdata/` (DONE) | `data:{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"[{...}]"}],"isError":false}}\n\n` (no `id:`/`event:` line; single `data:` line). |
| `testdata/tools_call_multiline.sse` | **P1.M3.T2.S1** | `testdata/` (DONE) | same payload split across **8 `data:` lines** (the round-trip fixture). |
| `testdata/initialize.sse` | **P1.M3.T2.S1** | `testdata/` (DONE) | `id:1\nevent:message\ndata:{...}\n\n` (non-tools/call result; id=1). |
| `rewrittenIDs map[any]bool` | **P1.M4.T1.S1** (runtime, future) | passed to `Inject` at call time | set of rewritten JSON-RPC request ids (the `id` field inside the request body). |

**Key**: the SSE `Event.ID` (the `id:` field) is NOT the JSON-RPC `id`. For
`tools_call.sse`, `Event.ID == ""` (no `id:` line) but the JSON-RPC `id` is `2`
(inside `Data`). Correlation is on the **JSON-RPC `id`**, per PRD §12.2 ("its
`id`"). This item must parse `Data` as JSON and read `obj["id"]`.

---

## 2. VERIFIED — Go `encoding/json` behavior (the load-bearing facts)

Ran a program (go1.26.4); outputs reproduced verbatim.

### 2a. JSON-RPC numeric `id` decodes to `float64` in `map[string]any`
```
input : {"jsonrpc":"2.0","id":2,...}
obj["id"] -> type=float64 value=2
```
**Implication**: `rewrittenIDs` keys MUST be `float64` for numeric ids to match.
`map[any]bool` keyed by `float64(2)` indexes `true` via the decoded `obj["id"]`:
```
m := map[any]bool{float64(2): true}
m[obj["id"]] -> true   // match ✓
```
This holds **iff** the producer (P1.M4.T1.S1) decodes the request `id` with the
same `encoding/json` default semantics (`json.Unmarshal` into `any` → `float64`).
String ids decode to `string` and also match. JSON-RPC ids are number/string/null
— all comparable as `any` map keys (no panic). **Contract to document**: the
producer must build `rewrittenIDs` from a default `encoding/json` decode into
`any` (do NOT use `json.Number` or `int`, or numeric ids won't match).

### 2b. `result.content` is `[]any` whose elements are `map[string]any`
```
obj["result"].(map[string]any)["content"].([]any)[0] -> map[string]interface {}
```
**Implication**: the type assertions `obj["result"].(map[string]any)` and
`...["content"].([]any)` are the correct detection; a failed assertion ⇒ not a
tools/call result ⇒ re-emit unchanged (PRD §12.2 "has no content array").

### 2c. GOTCHA — `json.Marshal` HTML-escapes `<`, `>`, `&`
```
input : {"t":"a<b>&c"}
Marshal -> {"t":"a\u003cb\u003e\u0026c"}        // bytes altered!
Encoder{SetEscapeHTML(false)}.Encode -> "a<b>&c"  // bytes preserved
```
**Implication**: z.ai result text (a stringified JSON array whose values may
contain `<`/`&` from HTML in search results) would be byte-altered on re-marshal
with plain `json.Marshal`. Although the MCP client un-escapes to the same value,
FR-3 ("results preserved") favors minimal alteration. **Use
`json.NewEncoder` + `SetEscapeHTML(false)`**, then trim the trailing `"\n"` that
`Encode` appends. (Verified: `Encode` adds exactly one trailing `"\n"`; valid
JSON never ends in a bare `"\n"`, so `TrimRight(..., "\n")` is safe.)

### 2d. GOTCHA — re-marshaling a `map[string]any` SORTS keys alphabetically
```
input order : jsonrpc, id, result
re-serialized: {"id":2,"jsonrpc":"2.0","result":{...}}   // sorted
```
**Implication**: the OUTER object's key order changes (byte-different) but the
JSON value is semantically identical. MCP clients parse JSON, so this is fine.
The TEXT values inside `content[1:]` are preserved (with SetEscapeHTML(false)).
**Tests must assert value-identity of `content[1].text`** (decoded string ==),
NOT byte-identity of the whole object.

---

## 3. DECISION TREE for `Inject` (PRD §12.2, mapped to verified Go types)

For each `Event` from the reader over `body`:
```
1. json.Unmarshal(ev.Data, &obj)                -> err      => re-emit UNCHANGED
2. obj must be map[string]any                   => else     => re-emit UNCHANGED
3. obj["error"] present (JSON-RPC error resp.)              => re-emit UNCHANGED
4. id := obj["id"]; id absent OR !rewrittenIDs[id]          => re-emit UNCHANGED
5. result, ok := obj["result"].(map[string]any); !ok        => re-emit UNCHANGED
6. isErr,_ := result["isError"].(bool); ok && isErr         => re-emit UNCHANGED
7. content, ok := result["content"].([]any); !ok            => re-emit UNCHANGED
8. result["content"] = append(
       []any{map[string]string{"type":"text","text":warning}},
       content...)
9. ev.Data = marshalJSON(obj)   // Encoder + SetEscapeHTML(false), trimmed
10. emitEvent(w, ev)
```
Steps 3 and 6 both realize "result is an error result" (PRD §12.2): step 3 is the
JSON-RPC `error` envelope (no `result`, hence no `content` — this is the
"error result (no content array)" test case); step 6 is the MCP `isError:true`
result (has `content` but is an error result → still do not inject). `isError` is
NEVER set or cleared by us (FR-3).

**Passthrough is the common path**: the very first failing guard (steps 1–7)
short-circuits to `emitEvent(w, ev)` with `ev.Data` untouched. Only step 8 mutates.

---

## 4. `emitEvent` framing (WHATWG; reverses the reader's `Join("\n")`)

WHATWG "Interpreting an event stream" (architecture/external_deps.md §3): the
`data:` buffer's values are joined with `"\n"` on READ; on WRITE, a `Data`
containing `"\n"` MUST be split back into one `data:` line per `\n`-separated
piece. This is the round-trip the reader's `dispatch()` (Join) and this writer
(Split) form.

Framing produced (order: `id:`, `event:`, `data:`(s), blank line — PRD §8.10):
```
id:<ID>\n            iff ev.ID != ""              (initialize has "1"; tools/call has "")
event:<Type>\n       iff ev.Type != "" && ev.Type != "message"   (see §5)
data:<line>\n        for each line in strings.Split(ev.Data, "\n")
\n                   (blank line terminates the event)
```
**Verified shape for tools_call (after inject)**: `ev.ID==""` (no `id:` line),
`ev.Type=="message"` (no `event:` line — see §5), `ev.Data` is compact JSON with
no internal `"\n"` (Marshal/Encode output) ⇒ exactly ONE `data:` line, then a
blank line. Matches z.ai's single-`data:`-line wire form (PRD §8.9).

---

## 5. DECISION — emit `event:` line only for NON-default types

The reader **cannot distinguish** "no `event:` line" from "`event:message`": both
decode to `Event.Type == "message"` (`dispatch()` sets the default). Therefore
byte-identity of framing is **not simultaneously achievable** for
`initialize.sse` (has `event:message`) and `tools_call.sse` (no `event:` line) —
they decode to the same `Type`. Resolution:

- **emit `event:` iff `ev.Type != "" && ev.Type != "message"`** (omit the default).
- Rationale: (a) matches z.ai's tools/call wire form (no `event:` line) — the
  production-critical path; (b) `message` is the SSE default, conventionally
  omitted on the wire; (c) `initialize` is served via passthrough `io.Copy`
  (P1.M4.T2.S2), NOT `Inject`, in production, so its dropped `event:message` is
  test-only and harmless.
- **Consequence for tests**: "verbatim" cases assert **data-identity** (decode
  the output with `NewSSEReader`, compare `Event.Data` to the input's decoded
  `Data`) — NOT byte-identity of the whole event. This is the meaningful contract
  (the JSON-RPC message is unchanged; framing cosmetics are not). A dedicated
  `emitEvent` unit test checks the exact framing bytes (including newline-split).

---

## 6. Imports & coordination with P1.M3.T3.S1 (parallel append to sse.go)

Both S1 (`warningText`) and S2 (`Inject`/`emitEvent`/`marshalJSON`) **APPEND** to
the same `sse.go`. Coordination:

- **S1 adds NO new import** (`warningText` uses `strings.HasPrefix`/`Join` —
  already imported by the reader).
- **S2 adds exactly ONE new import: `encoding/json`** (for Unmarshal/Marshal).
  `marshalJSON` uses `strings.Builder` + `json.NewEncoder` (strings already
  imported; no `bytes`/`fmt` needed).
- Final import block (gofmt-sorted): `bufio`, `encoding/json`, `io`, `strings`.
- **Rule**: APPEND functions to the END of `sse.go`; add `encoding/json` to the
  import block only if absent (it is — the reader imports bufio/io/strings). Do
  NOT touch the reader's `Event`/`Reader`/`NewSSEReader`/`Next`/`dispatch`/
  `reset`, and do NOT redeclare anything. If `warningText` is already present,
  append after it; ordering of the two appends is irrelevant.

---

## 7. Consumer seam — P1.M4.T2.S2 (conditional response writer)

```go
if len(rewrittenIDs) > 0 {
    err = Inject(w, resp.Body, rewrittenIDs, warning)   // warning = warningText(res.Notes)
} else {
    _, err = io.Copy(w, resp.Body)                       // byte-for-byte passthrough
}
if f, ok := w.(http.Flusher); ok { f.Flush() }
```
`Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error`
is the contract. `Inject` does NOT flush (the caller owns `http.Flusher`).
`Inject` returns `nil` at clean EOF, the reader's non-EOF error, or a write error.
A nil/empty `rewrittenIDs` ⇒ every event hits guard step 4 ⇒ all re-emitted
unchanged (defensive; the caller uses `io.Copy` instead in that case).

---

## 8. Test plan mapping (PRD §19.2 → this item's sse_test.go APPEND)

| PRD §19.2 case | Test | Asserts |
|---|---|---|
| inject into tools_call.sse | `TestSSE_Inject_ToolCallPrependsWarning` | decode output → `content[0]=={type:text,text:warning}`; `content[1].text` == original stringified array AND `json.Valid`; `isError==false`; `id==2`. |
| id not in set → verbatim | `TestSSE_Inject_IdNotInSetPassthrough` | `rewrittenIDs={99}`; decode output → `Data` == decoded input `Data` (no warning). |
| initialize → verbatim | `TestSSE_Inject_InitializePassthrough` | `rewrittenIDs={2}`; decode output → `Data==initJSON` (unchanged). |
| error result (no content) → verbatim | `TestSSE_Inject_ErrorResultPassthrough` | JSON-RPC `error` envelope (id in set) → unchanged; `isError:true` result → unchanged. |
| multi-data-line round-trip | `TestSSE_Inject_MultilineRoundTrip` | feed `tools_call_multiline.sse`; decode output → `content[0]==warning`, `content[1].text` valid JSON. |
| (emit framing) | `TestSSE_EmitEvent_Framing` | `Event{ID:"1",Type:"message",Data:"a\nb"}` → `id:1\ndata:a\ndata:b\n\n`; custom `Type:"ping"` → `event:ping` line present; round-trips through `NewSSEReader`. |

All inject tests go through `Inject` end-to-end (reader → decide → emit → re-decode),
so the pipeline is exercised, not just the mutation helper.
