name: "P1.M3.T3.S2 — SSE Inject: prepend warning into tools/call result.content, re-emit with WHATWG framing"
description: |

  Implement the **warning-injection pipeline** for the SSE module: a streaming
  `func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error`
  plus a writer helper `func emitEvent(w io.Writer, ev Event) error`. It reads an
  upstream SSE body with the **Reader** (P1.M3.T1.S1, DONE on disk), and for each
  event whose JSON-RPC `id` is in `rewrittenIDs` and whose `result.content` is an
  array, **prepends** `{"type":"text","text":warning}` at index 0, re-serializes,
  and re-emits preserving the original `Event.ID` and `Event.Type` with WHATWG
  framing (internal newlines split back into multiple `data:` lines). Everything
  else — non-objects, ids not in the set, JSON-RPC `error` envelopes, MCP
  `isError:true` results, results with no `content` array — is re-emitted
  **byte-for-byte in its Data** (unchanged). `isError` is NEVER touched (PRD §3 /
  FR-3). `Inject` is the consumer of `warningText` (P1.M3.T3.S1, parallel) — it
  receives the formatted string as the `warning` arg; it does NOT call
  `warningText` itself. It is consumed by **P1.M4.T2.S2** (the conditional
  rewritten-response path: `io.Copy` when no rewrite, else `Inject`). APPEND
  `Inject`/`emitEvent`/`marshalJSON` to the existing `sse.go` (reader + warningText)
  and APPEND the §19.2 tests to the existing `sse_test.go`. go.mod gains ZERO
  requires (stdlib only — one new import: `encoding/json`). **Mode A docs**: a doc
  comment on `Inject` documenting the id-correlation, prepend-only,
  isError-untouched, and passthrough-on-no-content rules (PRD §12.2).

---

## Goal

**Feature Goal**: A streaming `Inject` that transforms an upstream SSE body into a
client-facing SSE body in which exactly one `text` content block — the warning — is
prepended into each rewritten `tools/call` result, with the original result content
preserved and still valid JSON, and every other event passed through with its
`Data` unchanged. `emitEvent` reverses the reader's `data:`-line join so the
output is well-formed WHATWG SSE.

**Deliverable**: NO new files. Two edits (both APPEND to files created by
P1.M3.T1.S1 / extended by P1.M3.T3.S1):
- **APPEND to `sse.go`** — `func Inject(...) error`, `func emitEvent(...) error`,
  and a private `func marshalJSON(v any) (string, error)` helper, plus a Mode-A
  doc comment on `Inject`. Add `"encoding/json"` to the import block (the only new
  import; `strings`/`io`/`bufio` already present).
- **APPEND to `sse_test.go`** — the six PRD §19.2 cases (inject-prepends-warning,
  id-not-in-set passthrough, initialize passthrough, error-result passthrough,
  multi-data-line round-trip, and an `emitEvent` framing unit test).

**Success Definition**: `go test -run 'TestSSE_Inject|TestSSE_EmitEvent' -v` passes;
in particular, injecting into `testdata/tools_call.sse` with
`rewrittenIDs={float64(2):true}` yields a re-decoded `result.content` whose `[0]`
is exactly `{type:text, text:<warning>}` and whose `[1].text` is byte-identical to
the original stringified-array string AND still `json.Valid`, with `isError==false`
unchanged. `go vet ./...` and `go test ./...` stay clean. `go doc . Inject` shows
the four documented rules (Mode A).

## Hard Prerequisites

1. **`sse.go` exists** with the Reader (P1.M3.T1.S1 — DONE on disk, verified:
   `Event`, `Reader`, `NewSSEReader`, `(*Reader).Next`, `dispatch`, `reset`, and
   imports `bufio`/`io`/`strings`). This item APPENDS to it. If `sse.go` is absent,
   STOP — the reader has not run; do NOT create a competing `sse.go`.
2. **`testdata/{initialize,tools_call,tools_call_multiline}.sse` exist**
   (P1.M3.T2.S1 — DONE on disk). `tools_call.sse` carries JSON-RPC `id:2`; the
   `content[0].text` is the stringified array
   `[{"title":"Example Search Result","url":"https://example.com/result","content":"Lorem ipsum dolor sit amet."}]`.
3. **`warningText(notes []string) string` exists OR will exist** (P1.M3.T3.S1,
   parallel). This item does NOT import/call `warningText`; it receives the
   formatted string via the `warning` parameter at call time. Tests pass a literal
   warning string (no coupling to `warningText`).
4. **`rewrittenIDs map[any]bool`** is provided at runtime by P1.M4.T1.S1. Its keys
   are the decoded JSON-RPC `id` values: `float64` for numeric ids, `string` for
   string ids (default `encoding/json` decode into `any` — see "id contract"
   below). This item treats it as an opaque set; tests build it inline.

## User Persona

**Target User**: the **next work item** that calls `Inject` — P1.M4.T2.S2
(conditional response writer): when at least one request id was rewritten, it
streams the upstream body through `Inject(w, resp.Body, rewrittenIDs,
warningText(res.Notes))`; otherwise it `io.Copy`s verbatim. Plus the **maintainer**,
who gets one audited place where "prepend the warning, leave everything else alone"
lives.

**Use Case**: `err := Inject(w, body, rewrittenIDs, warning)` → the client receives
a valid SSE stream whose rewritten tools/call results carry the warning first and
the real payload intact.

**User Journey**: implementer reads this PRP → appends `Inject`/`emitEvent`/
`marshalJSON` to `sse.go` → appends the six tests to `sse_test.go` →
`go test -run 'TestSSE_Inject|TestSSE_EmitEvent' -v` → green → P1.M4.T2.S2 wires
`Inject` into the rewritten-response path.

**Pain Points Addressed**: (1) The "do I inject?" decision has FIVE passthrough
guards (not-object, id-not-in-set, error envelope, isError, no-content) that are
easy to get wrong; a pinned decision tree + end-to-end tests fix each. (2) The
JSON-RPC `id` (inside `Data`) is NOT the SSE `Event.ID` (the `id:` field) —
correlation must parse `Data`. (3) `json.Marshal` HTML-escapes `<>&` and sorts map
keys, which would silently alter result text; this PRP specifies
`SetEscapeHTML(false)` and value-identity tests. (4) The reader loses "was there an
`event:` line?", so byte-identity of framing is impossible for both initialize and
tools/call; this PRP resolves it (omit default `event:`, test data-identity).

## Why

- **PRD §12.2 (Inject)**: the exact algorithm this item implements (parse → guard →
  prepend → re-serialize → re-emit with original id/event and newline-split data).
- **PRD §3 / FR-3**: NEVER set `isError:true`; original result content unchanged and
  valid JSON; prepend exactly one `text` block; pass through byte-for-byte when
  nothing changed (the nothing-changed case is `io.Copy` in P1.M4.T2.S2; `Inject`
  itself is only called when something changed, but it still re-emits
  non-matching events unchanged).
- **PRD §8.10 (SSE framing rules)**: `data:` values join with `"\n"` on read and
  split back into multiple `data:` lines on write — the `emitEvent` round-trip.
- **PRD §19.2 (sse_test.go)**: names the five pipeline test cases (+ this PRP adds
  a focused `emitEvent` framing test).
- **Decouples injection from formatting and wiring.** `warningText` (S1) formats the
  line; `Inject` (this item) places it; the proxy (P1.M4.T2) chooses when to call
  it. Each stage's contract is fixed so the others don't change.
- **Coherence across the chain.** `rewrite.go` (DONE) decides Changed + records ids;
  `warningText` (S1) renders Notes; `Inject` (this) prepends into the matching
  result by id; the proxy (P1.M4) wires request↔response id correlation.

## What

`Inject`, `emitEvent`, and a private `marshalJSON` appended to `sse.go`. Visible
behavior: a rewritten tools/call result streams back with one extra `text` content
block first; the original payload string is unchanged and still valid JSON;
`isError` is untouched; all other events pass through with their `Data` unchanged.

### Success Criteria

- [ ] `func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error` exists in `sse.go`.
- [ ] `func emitEvent(w io.Writer, ev Event) error` exists in `sse.go`.
- [ ] Inject into `testdata/tools_call.sse` with `rewrittenIDs={float64(2):true}` → re-decoded `result.content[0] == {type:"text", text:<warning>}`; `content[1].text` byte-identical to the original stringified array AND `json.Valid`; `result.isError == false`; top-level `id == float64(2)`, `jsonrpc == "2.0"`.
- [ ] Event whose JSON-RPC `id` is NOT in `rewrittenIDs` → re-emitted with `Data` unchanged (no warning).
- [ ] `initialize` event (non-tools/call result; `id:1`) → `Data` unchanged.
- [ ] JSON-RPC `error` envelope (id in set) → `Data` unchanged; MCP `result.isError:true` (id in set) → `Data` unchanged.
- [ ] Multi-`data:`-line input (`tools_call_multiline.sse`) injects correctly: decoded `content[0]` is the warning, `content[1].text` is valid JSON.
- [ ] `emitEvent` produces `id:<ID>\n` (iff `ID!=""), `event:<Type>\n` (iff `Type!="" && Type!="message"`), one `data:<line>\n` per `\n`-split piece, then `\n`; round-trips through `NewSSEReader`.
- [ ] `go vet ./...` clean; `go test ./...` green; `go.mod` unchanged; no `.go` file other than `sse.go`/`sse_test.go` edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact `Event`/`Reader`/`Next` API and `Data` join semantics are quoted from
the on-disk `sse.go`; (b) the five-guard decision tree is enumerated with the exact
Go type assertions and the verified `float64`-id fact; (c) the `json.Marshal`
HTML-escape + key-sort GOTCHAs are stated with the verified remedy
(`SetEscapeHTML(false)`); (d) the `emitEvent` framing is given as exact bytes with
the `event:message`-omission decision and its justification; (e) a complete
reference implementation is provided; (f) all six tests are enumerated with literal
inputs and byte/value-exact assertions; (g) the testdata fixture contents are
reproduced verbatim; (h) the parallel-append coordination with `warningText` (S1)
and the import delta (`encoding/json` only) are explicit.

### Documentation & References

```yaml
# MUST READ — the algorithm this item implements.
- file: PRD.md
  section: "§12.2 Inject" + "§8.10 SSE framing rules" + "§3/FR-3 (never set isError)"
  why: §12.2 is the step-by-step (parse→guard→prepend→re-serialize→re-emit with
        original id/event, newline-split data); §8.10 the join/split round-trip;
        FR-3 the "never set isError:true, original content unchanged & valid" rule.
  critical: §12.2 "its `id`" = the JSON-RPC id INSIDE Data, NOT the SSE Event.ID.
        "Leave isError and all other fields untouched." "re-emit the event with the
        original id and event fields."

# MUST READ — the Reader this item consumes (DONE on disk). Quote of its API below.
- file: sse.go
  why: Event{ID,Type,Data}; NewSSEReader(r io.Reader) *Reader; (*Reader).Next()
        (Event, error) returns io.EOF at end. Data = data: values Join("\n"), no
        trailing newline. Type defaults to "message" when no event: line.
  pattern: dispatch() sets Type="message" default — so "no event: line" and
        "event:message" are indistinguishable after decode (drives emitEvent §5).
  gotcha: do NOT touch Event/Reader/NewSSEReader/Next/dispatch/reset — APPEND only.

# MUST READ — the testdata fixtures (DONE on disk). Contents reproduced verbatim below.
- file: testdata/tools_call.sse
  why: the inject target. JSON-RPC id=2; result.content[0].text = stringified array.
- file: testdata/tools_call_multiline.sse
  why: the 8-data:-line round-trip fixture (same payload, whitespace-split).
- file: testdata/initialize.sse
  why: the non-tools/call passthrough case (id:1, event:message).

# The warning formatter (parallel) — this item receives its OUTPUT as `warning`.
- docfile: plan/001_c0abc3757e9a/P1M3T3S1/PRP.md
  why: defines warningText([]string) string. This item does NOT call it; the proxy
        (P1.M4.T2.S2) calls warningText and passes the result as `warning`.
  section: "Reference implementation" (signature + Mode-A doc).

# The reader PRP (DONE) — defines the Event/Reader contract this item builds on.
- docfile: plan/001_c0abc3757e9a/P1M3T1S1/PRP.md
  why: confirms Next() EOF semantics, Data join, Type default. (sse.go on disk is
        authoritative; this PRP is background.)
  section: "Reference implementation".

# This item's own research — verified Go-json facts + the decision tree + framing.
- docfile: plan/001_c0abc3757e9a/P1M3T3S2/research/inject-and-emit.md
  why: §2 verifies float64-id, content is []any of map[string]any, json.Marshal
        HTML-escapes <>& (use SetEscapeHTML(false)), re-marshal sorts keys (test
        value-identity). §3 the 10-step decision tree. §4-5 emitEvent framing +
        the event:message-omission decision. §6 import/coordination with S1.
  section: "§2 VERIFIED Go json behavior" + "§3 DECISION TREE" + "§5 emit event:".

# CODEBASE CONVENTIONS — follow these patterns.
- file: sse_test.go
  why: table-driven + fixture style; `initJSON` already defined (derived from
        proxy_test.go's initSSE); tests use NewSSEReader + reflect.DeepEqual.
  pattern: read fixture with os.ReadFile (t.Skipf if missing); compare Event fields.
  gotcha: do NOT redeclare `initJSON` or `initSSE` (already in sse_test.go /
        proxy_test.go). APPEND new Test* funcs only.

- file: rewrite_test.go
  why: table-driven t.Run subtest style with per-case PRD-§ comments.

- url: https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream
  why: WHATWG SSE — data: values join with "\n" on read; the writer must split on
        "\n" back into multiple data: lines (the round-trip emitEvent performs).
  critical: a data: value containing "\n" is transmitted as N consecutive data:
        lines; emitEvent MUST split (strings.Split) and emit one data: per piece.

- url: https://pkg.go.dev/encoding/json#Encoder
  why: json.NewEncoder + SetEscapeHTML(false) preserves <>& in re-serialized text
        (json.Marshal HTML-escapes them — verified, see research §2c).
  critical: Encoder.Encode appends a trailing "\n" — TrimRight it before using as
        Event.Data (valid JSON never ends in a bare "\n").

- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: default decode into map[string]any: numbers → float64, objects →
        map[string]any, arrays → []any. Drives the type assertions in the guards.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment
  main.go           # bootstrap + logger (P1.M1.T4)
  config.go         # Config + DefaultConfig + LoadConfig (P1.M1.T2)
  proxy.go          # passthrough forward core (P1.M1.T4.S2)  — UNTOUCHED
  rewrite.go        # Rewrite + RewriteResult (P1.M2.T1)      — UNTOUCHED
  sse.go            # Event/Reader/... (P1.M3.T1.S1) + warningText (P1.M3.T3.S1) — EXTEND THIS
  sse_test.go       # reader + warningText tests               — EXTEND THIS
  *_test.go         # config/resolve/logger/health/proxy/rewrite tests — UNTOUCHED
  testdata/*.sse    # fixtures (P1.M3.T2.S1)                   — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
sse.go        # EXTEND (append): Inject(...) + emitEvent(...) + marshalJSON(...) + doc comment.
               #   Imports: add "encoding/json" (only new import). Reader + warningText untouched.
sse_test.go   # EXTEND (append): TestSSE_Inject_* (5 cases) + TestSSE_EmitEvent_Framing.
               #   No new imports ("encoding/json","os","reflect","strings","io","testing" present).
```

No new files. No other file changes.

### Verified Go-json facts (load-bearing — see research/inject-and-emit.md §2)

```
VERIFIED (go1.26.4) — json.Unmarshal of {"jsonrpc":"2.0","id":2,"result":{...}} into map[string]any:
  obj["id"]            -> float64(2)            // numeric id is float64, NOT int
  obj["result"]        -> map[string]any
  result["content"]    -> []any                  // each element is map[string]any
  result["isError"]    -> bool

VERIFIED — map[any]bool keyed by float64(2) indexes true via the decoded obj["id"]:
  rewrittenIDs[obj["id"]] -> true   // match ✓  (iff producer used default decode)

VERIFIED — json.Marshal HTML-ESCAPES < > & :
  "a<b>&c" -> "a\u003cb\u003e\u0026c"   // bytes altered (semantically same after client unescape)
  Encoder{SetEscapeHTML(false)}        -> "a<b>&c"  // bytes preserved  ✓ USE THIS

VERIFIED — re-marshaling map[string]any SORTS keys alphabetically:
  {jsonrpc,id,result} -> {"id":2,"jsonrpc":"2.0","result":{...}}   // outer bytes change, value identical
  => tests assert VALUE-identity of content[1].text (decoded ==), NOT byte-identity of the object.
```

### id contract (with P1.M4.T1.S1, the producer of rewrittenIDs)

`rewrittenIDs` is `map[any]bool`. For a numeric JSON-RPC id `N`, the key MUST be
`float64(N)` — the value `encoding/json` produces when decoding `{"id":N}` into
`any`. This item decodes `ev.Data` the same way, so `obj["id"]` is `float64(N)` and
`rewrittenIDs[obj["id"]]` matches. String ids decode to `string` and also match.
**Do not** build `rewrittenIDs` from `int`/`json.Number` keys or numeric ids will
silently fail to match (the lookup returns `false` → passthrough, no warning).
JSON-RPC ids are number/string/null — all comparable as `any` map keys (no panic).

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — JSON-RPC id ≠ SSE Event.ID. The correlation id is obj["id"] INSIDE Data
  (e.g. tools_call.sse has Event.ID=="" but JSON-RPC id==2). Always parse Data and
  read obj["id"]; never key off Event.ID.

CRITICAL — numeric id is float64, not int. json.Unmarshal into any yields float64
  for JSON numbers. rewrittenIDs must be keyed by float64(N). Documented above.

CRITICAL — json.Marshal HTML-escapes < > & and would alter z.ai result text (search
  results may contain HTML). Use json.NewEncoder + SetEscapeHTML(false), then
  TrimRight the trailing "\n" that Encode appends. (Verified; see marshalJSON below.)

CRITICAL — NEVER touch isError. Do not read it to decide injection EXCEPT the
  isError:true => passthrough guard (step 6); never set/clear it. FR-3.

CRITICAL — APPEND, do not OVERWRITE. sse.go already holds the Reader (P1.M3.T1.S1)
  and warningText (P1.M3.T3.S1, parallel). Add Inject/emitEvent/marshalJSON at the
  END. Do not touch Event/Reader/NewSSEReader/Next/dispatch/reset/warningText.

CRITICAL — do NOT redeclare initJSON or initSSE. sse_test.go defines initJSON;
  proxy_test.go defines initSSE. A duplicate package-level name is a compile error.
  The Inject tests reuse initJSON (for the initialize-passthrough case).

GOTCHA — re-marshaling sorts map keys (outer object bytes change). Tests must assert
  VALUE-identity (decode content[1].text, compare to original decoded text), not
  byte-identity of the whole Data string.

GOTCHA — the reader cannot tell "no event: line" from "event:message" (both decode
  to Type=="message"). So byte-identity of framing is impossible for BOTH initialize
  (has event:message) and tools_call (no event: line). emitEvent OMITS event: for the
  default "message" type (matches z.ai tools/call wire form). Verbatim tests assert
  DATA-identity (decode output, compare Event.Data), not byte-identity of the event.

GOTCHA — keep Inject/emitEvent/marshalJSON UNEXPORTED. The proxy (P1.M4.T2.S2) is
  package main and calls Inject directly. An exported name needlessly widens the API.

GOTCHA — "verbatim"/"passthrough" in Inject means Data unchanged, NOT that Inject is
  skipped. Inject processes EVERY event in the body; non-matching events are re-emitted
  with their Data untouched. (When NOTHING was rewritten, the proxy uses io.Copy
  instead of Inject — P1.M4.T2.S2's decision; Inject itself is only called when
  len(rewrittenIDs) > 0, but still re-emits non-matching events correctly.)
```

## Implementation Blueprint

### Data models and structure

No new types. `Inject` takes `(io.Writer, io.Reader, map[any]bool, string)`; the
per-event JSON is decoded into `map[string]any` (stdlib default). `emitEvent` takes
`(io.Writer, Event)`. `marshalJSON` takes `(any)` and returns `(string, error)`.
Depends only on stdlib `"encoding/json"`, `"io"`, `"strings"` (all present except
`encoding/json`, which this item adds).

### Reference implementation (APPEND this to `sse.go`)

> The reader's `sse.go` imports `bufio`, `io`, `strings`. After appending, the
> import block must also contain `encoding/json` (gofmt sorts: `bufio`,
> `encoding/json`, `io`, `strings`). If `warningText` (P1.M3.T3.S1) is already
> appended, place these AFTER it; ordering is irrelevant. Run `gofmt -w sse.go`.

```go
// Inject reads a Server-Sent Events stream from body (an upstream response body)
// and writes a transformed stream to w. For each event whose JSON-RPC id is in
// rewrittenIDs and whose result.content is an array, it PREPENDS one content block
// {"type":"text","text":warning} at index 0, re-serializes, and re-emits the event
// with its original Event.ID and Event.Type, splitting any internal newlines in the
// data back into multiple "data:" lines (WHATWG round-trip, PRD §8.10/§12.2).
//
// RULES (PRD §12.2 + §3/FR-3):
//   - ID CORRELATION: the matching id is the JSON-RPC "id" INSIDE the event's Data
//     (obj["id"]), NOT the SSE Event.ID (the "id:" field). An event whose id is
//     absent or not in rewrittenIDs is re-emitted UNCHANGED.
//   - PREPEND-ONLY: the original result.content elements are preserved in order
//     (appended after the warning); isError and every other field are untouched.
//   - isError UNTOUCHED: the proxy NEVER sets isError:true (FR-3). An MCP
//     isError:true result, a JSON-RPC "error" envelope, or any result without a
//     content array is re-emitted UNCHANGED (nothing to prepend to / an error).
//   - PASSTHROUGH-ON-NO-CONTENT: if Data is not a JSON object, has no result, or
//     result.content is not an array, the event is re-emitted UNCHANGED.
//
// Inject returns nil at clean EOF, the reader's non-EOF error, or a write error.
// It does not flush w (the caller owns http.Flusher — see P1.M4.T2.S2). A nil or
// empty rewrittenIDs re-emits every event unchanged (defensive; the proxy uses
// io.Copy instead in that case). warning is the formatted line from warningText
// (P1.M3.T3.S1); Inject does not call warningText itself.
func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error {
	rd := NewSSEReader(body)
	for {
		ev, err := rd.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		ev.Data = injectData(ev.Data, rewrittenIDs, warning)
		if err := emitEvent(w, ev); err != nil {
			return err
		}
	}
}

// injectData returns data with the warning prepended into result.content if data is
// a rewritten tools/call result; otherwise it returns data UNCHANGED (PRD §12.2
// guards 1–7). It never touches isError and never fails (a parse/re-serialize
// error means "not a result we can inject into" → return the original data).
func injectData(data string, rewrittenIDs map[any]bool, warning string) string {
	var obj map[string]any
	if err := json.Unmarshal([]byte(data), &obj); err != nil {
		return data // 1. not JSON -> unchanged
	}
	if _, isJSONRPCError := obj["error"]; isJSONRPCError {
		return data // 2. JSON-RPC error envelope (no result) -> unchanged
	}
	id, ok := obj["id"]
	if !ok || !rewrittenIDs[id] {
		return data // 3. id absent or not rewritten -> unchanged
	}
	result, ok := obj["result"].(map[string]any)
	if !ok {
		return data // 4. no result object -> unchanged
	}
	if isErr, ok := result["isError"].(bool); ok && isErr {
		return data // 5. MCP isError:true result -> unchanged
	}
	content, ok := result["content"].([]any)
	if !ok {
		return data // 6. no content array -> unchanged
	}
	// 7. PREPEND the warning; original content elements are preserved in order.
	result["content"] = append(
		[]any{map[string]string{"type": "text", "text": warning}},
		content...,
	)
	out, err := marshalJSON(obj)
	if err != nil {
		return data // re-serialization failed -> unchanged (defensive; shouldn't happen)
	}
	return out
}

// emitEvent writes one SSE event to w with WHATWG framing (PRD §8.10): an "id:"
// line iff Event.ID != "", an "event:" line iff Event.Type is a non-default type
// (the "message" default is omitted on the wire, matching z.ai's tools/call form),
// one "data:<line>" per "\n"-separated piece of Event.Data (the reverse of the
// reader's Join), and a terminating blank line.
func emitEvent(w io.Writer, ev Event) error {
	var b strings.Builder
	if ev.ID != "" {
		b.WriteString("id:")
		b.WriteString(ev.ID)
		b.WriteByte('\n')
	}
	if ev.Type != "" && ev.Type != "message" {
		b.WriteString("event:")
		b.WriteString(ev.Type)
		b.WriteByte('\n')
	}
	for _, line := range strings.Split(ev.Data, "\n") {
		b.WriteString("data:")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	b.WriteByte('\n') // blank line terminates the event
	_, err := io.WriteString(w, b.String())
	return err
}

// marshalJSON encodes v as compact JSON WITHOUT HTML-escaping (<, >, & are
// preserved), so re-serializing a z.ai result does not alter text that may contain
// HTML from search results. json.Marshal would escape those to \u003c/\u003e/\u0026
// (verified). The trailing "\n" that Encoder.Encode appends is trimmed (valid JSON
// never ends in a bare "\n").
func marshalJSON(v any) (string, error) {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify sse.go + sse_test.go + testdata exist
  - RUN: ls sse.go sse_test.go testdata/tools_call.sse testdata/tools_call_multiline.sse testdata/initialize.sse
  - IF ABSENT: STOP — the reader (P1.M3.T1.S1) / fixtures (P1.M3.T2.S1) have not run.
        Do NOT create competing files. Flag the blocker.
  - IF PRESENT: proceed. CONFIRM "encoding/json" is NOT yet in sse.go's import
        block (reader imports bufio/io/strings). It will be added in Task 1.

Task 1: APPEND Inject + injectData + emitEvent + marshalJSON to sse.go
  - APPEND the "Reference implementation" block above to the END of sse.go (after
        the reader's reset() AND after warningText if P1.M3.T3.S1 has appended it).
  - ADD "encoding/json" to the import block (gofmt sorts it between bufio and io).
        Do NOT duplicate bufio/io/strings (already present).
  - KEEP the doc comment on Inject — it IS the Mode A deliverable (four rules).
  - DO NOT touch Event/Reader/NewSSEReader/Next/dispatch/reset/warningText.

Task 2: APPEND the six tests to sse_test.go
  - PACKAGE: main (same package; do NOT redeclare initJSON/initSSE).
  - APPEND at the END of sse_test.go, after the reader's + warningText's tests.
  - IMPORTS: "encoding/json","os","io","reflect","strings","testing" are already
        imported by sse_test.go — add nothing (verify; do not duplicate).
  - TESTS: see the test block below. Go through Inject end-to-end (reader → decide
        → emit → re-decode with NewSSEReader) so the whole pipeline is exercised.
  - COVERAGE: the five PRD §19.2 cases + one emitEvent framing unit test.
  - DO NOT call warningText — pass a LITERAL warning string (no coupling to S1).

Task 3: VALIDATE
  - gofmt -w sse.go sse_test.go
  - go vet ./...
  - go test -run 'TestSSE_Inject|TestSSE_EmitEvent' -v
  - go test ./...
  - ALL green. git diff --stat must show ONLY sse.go + sse_test.go (append-only).
  - git diff go.mod must be EMPTY.
```

### Test block (sse_test.go — APPEND)

```go
// injectWarning is a fixed warning string for Inject tests (decoupled from
// warningText, which is exercised by TestWarningText_*). PRD §19.2.
const injectWarning = `[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.`

// injectAll runs Inject over the SSE stream in raw, with the given rewrittenIDs and
// warning, and returns the re-emitted bytes. Helper for the §19.2 cases.
func injectAll(t *testing.T, raw string, rewrittenIDs map[any]bool, warning string) string {
	t.Helper()
	var out strings.Builder
	if err := Inject(&out, strings.NewReader(raw), rewrittenIDs, warning); err != nil {
		t.Fatalf("Inject err=%v, want nil", err)
	}
	return out.String()
}

// firstEventData decodes the first event of an SSE stream and returns it (for
// asserting on the injected/unchanged Data).
func firstEventData(t *testing.T, raw string) Event {
	t.Helper()
	ev, err := NewSSEReader(strings.NewReader(raw)).Next()
	if err != nil {
		t.Fatalf("re-decode first event err=%v, want nil", err)
	}
	return ev
}

// TestSSE_Inject_ToolCallPrependsWarning: inject into testdata/tools_call.sse with
// id 2 in the set -> content[0] is the warning; content[1].text is the ORIGINAL
// stringified array (byte-identical) and still valid JSON; isError untouched
// (PRD §19.2, §12.2, §3/FR-3).
func TestSSE_Inject_ToolCallPrependsWarning(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	// Capture the ORIGINAL content[0].text (the stringified array) BEFORE inject.
	origEv := firstEventData(t, string(raw))
	origResult := origEv.Data // full JSON-RPC object string
	var origObj map[string]any
	json.Unmarshal([]byte(origResult), &origObj)
	origText := origObj["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)

	out := injectAll(t, string(raw), map[any]bool{float64(2): true}, injectWarning)
	ev := firstEventData(t, out)

	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("injected Data is not valid JSON: %v\n%s", err, ev.Data)
	}
	if obj["jsonrpc"] != "2.0" || obj["id"] != float64(2) {
		t.Errorf("envelope changed: jsonrpc=%v id=%v", obj["jsonrpc"], obj["id"])
	}
	result := obj["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError=%v, want false (FR-3: never set isError)", isErr)
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content)=%d, want 2 (warning + original)", len(content))
	}
	// content[0] is the warning block.
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" || c0["text"] != injectWarning {
		t.Errorf("content[0]=%v, want {type:text text:%q}", c0, injectWarning)
	}
	// content[1].text is the ORIGINAL stringified array, byte-identical + valid JSON.
	c1text := content[1].(map[string]any)["text"].(string)
	if c1text != origText {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1text, origText)
	}
	if !json.Valid([]byte(c1text)) {
		t.Errorf("content[1].text is not valid JSON: %q", c1text)
	}
}

// TestSSE_Inject_IdNotInSetPassthrough: a tools/call result whose id is NOT in the
// set is re-emitted with Data UNCHANGED (no warning) (PRD §19.2).
func TestSSE_Inject_IdNotInSetPassthrough(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	wantData := firstEventData(t, string(raw)).Data
	out := injectAll(t, string(raw), map[any]bool{float64(99): true}, injectWarning)
	gotData := firstEventData(t, out).Data
	if gotData != wantData {
		t.Errorf("id-not-in-set Data changed:\n got %q\nwant %q", gotData, wantData)
	}
}

// TestSSE_Inject_InitializePassthrough: a non-tools/call result (initialize; id 1)
// is re-emitted with Data UNCHANGED (PRD §19.2).
func TestSSE_Inject_InitializePassthrough(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	out := injectAll(t, string(raw), map[any]bool{float64(2): true}, injectWarning)
	ev := firstEventData(t, out)
	if ev.Data != initJSON {
		t.Errorf("initialize Data changed:\n got %q\nwant %q", ev.Data, initJSON)
	}
}

// TestSSE_Inject_ErrorResultPassthrough: a JSON-RPC error envelope (id in set) and
// an MCP isError:true result are re-emitted UNCHANGED (PRD §19.2 "error result (no
// content array) -> verbatim"; §12.2).
func TestSSE_Inject_ErrorResultPassthrough(t *testing.T) {
	cases := []struct {
		name, sse string
	}{
		{
			// JSON-RPC error response: has "error", no "result" (hence no content).
			"jsonrpc_error_envelope",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"error\":{\"code\":-32603,\"message\":\"boom\"}}\n\n",
		},
		{
			// MCP tools/call error: result.isError=true (still has content) -> unchanged.
			"mcp_isError_true",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"isError\":true,\"content\":[{\"type\":\"text\",\"text\":\"err\"}]}}\n\n",
		},
		{
			// result present but no content array -> unchanged.
			"result_no_content",
			"data:{\"jsonrpc\":\"2.0\",\"id\":2,\"result\":{\"isError\":false}}\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantData := firstEventData(t, tc.sse).Data
			out := injectAll(t, tc.sse, map[any]bool{float64(2): true}, injectWarning)
			gotData := firstEventData(t, out).Data
			if gotData != wantData {
				t.Errorf("error-result Data changed:\n got %q\nwant %q", gotData, wantData)
			}
		})
	}
}

// TestSSE_Inject_MultilineRoundTrip: a multi-data:-line event (8 lines) is parsed,
// injected, and re-emitted with content preserved. The joined Data parses to valid
// JSON; content[0] is the warning, content[1].text is valid JSON (PRD §19.2, §8.10).
func TestSSE_Inject_MultilineRoundTrip(t *testing.T) {
	raw, err := os.ReadFile("testdata/tools_call_multiline.sse")
	if err != nil {
		t.Skipf("testdata fixture missing: %v", err)
	}
	out := injectAll(t, string(raw), map[any]bool{float64(2): true}, injectWarning)
	ev := firstEventData(t, out)
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("multiline injected Data not valid JSON: %v\n%s", err, ev.Data)
	}
	content := obj["result"].(map[string]any)["content"].([]any)
	if c0 := content[0].(map[string]any); c0["type"] != "text" || c0["text"] != injectWarning {
		t.Errorf("multiline content[0]=%v, want the warning block", c0)
	}
	c1text := content[1].(map[string]any)["text"].(string)
	if !json.Valid([]byte(c1text)) {
		t.Errorf("multiline content[1].text not valid JSON: %q", c1text)
	}
}

// TestSSE_EmitEvent_Framing: emitEvent produces id:/event:/data: framing per PRD
// §8.10 — id: iff ID!=""; event: iff a non-default type; internal "\n" in Data
// split into multiple data: lines; terminated by a blank line. Round-trips through
// NewSSEReader (PRD §19.2 multi-line round-trip).
func TestSSE_EmitEvent_Framing(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		want string
	}{
		{
			// message default + multi-line data -> no event: line; 2 data: lines.
			"message_multiline_data",
			Event{Type: "message", Data: "a\nb"},
			"data:a\ndata:b\n\n",
		},
		{
			// id present, message default -> id: line, no event: line.
			"id_and_message",
			Event{ID: "1", Type: "message", Data: "x"},
			"id:1\ndata:x\n\n",
		},
		{
			// custom event type -> event: line emitted.
			"custom_event_type",
			Event{Type: "ping", Data: "x"},
			"event:ping\ndata:x\n\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := emitEvent(&b, tc.ev); err != nil {
				t.Fatalf("emitEvent err=%v", err)
			}
			if got := b.String(); got != tc.want {
				t.Errorf("emitEvent framing:\n got %q\nwant %q", got, tc.want)
			}
			// Round-trip: re-decoding the emitted bytes yields the original Event
			// (Data joined back with "\n", Type defaulted to "message" if absent).
			rt, err := NewSSEReader(strings.NewReader(b.String())).Next()
			if err != nil {
				t.Fatalf("round-trip re-decode err=%v", err)
			}
			wantRT := tc.ev
			if wantRT.Type == "" {
				wantRT.Type = "message"
			}
			if !reflect.DeepEqual(rt, wantRT) {
				t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", rt, wantRT)
			}
		})
	}
}
```

### Implementation Patterns & Key Details

```go
// PATTERN: streaming pipeline — Inject loops Next(); each event is either mutated
// (injectData) or passed through, then framed (emitEvent). Low memory: only one
// Event.Data in flight at a time (it may be large for tools/call, but that's
// unavoidable — we must parse it to inject). Mirrors io.Copy's "don't buffer the
// whole body" intent (architecture/external_deps.md §2).

// PATTERN: guards-first decision tree. injectData returns `data` unchanged on the
// FIRST failing guard (not-JSON / error-envelope / id-not-in-set / no-result /
// isError / no-content). Only the last step mutates. This makes the passthrough
// cases obvious and the inject case the single fall-through.

// PATTERN: prepend via append([]any{warningBlock}, content...). The ORIGINAL
// content slice elements are reused (not copied element-by-element); append
// allocates a new backing array for the result so the original is not aliased.
// Order is preserved: warning at [0], originals at [1:].

// GOTCHA (restated): marshalJSON uses json.NewEncoder + SetEscapeHTML(false) so
// <, >, & in z.ai result text are preserved on re-serialization (json.Marshal
// would emit \u003c/\u003e/\u0026). TrimRight the trailing "\n" Encode appends.

// GOTCHA (restated): emitEvent splits Data on "\n" (strings.Split) and emits one
// "data:" line per piece — the reverse of the reader's Join("\n"). After inject,
// Data is compact JSON (no internal "\n") so typically ONE data: line results;
// but if Data ever contains "\n" (e.g. a pre-injection multi-line event that was
// NOT matched and re-emitted verbatim), the split reproduces the multi-line form.

// GOTCHA (restated): the JSON-RPC id is obj["id"] (float64 for numbers), keyed
// against map[any]bool. NEVER use Event.ID for correlation.
```

### Integration Points

```yaml
FILES MODIFIED (append-only):
  - sse.go        (EXTEND: + Inject + injectData + emitEvent + marshalJSON + doc;
                    import block + "encoding/json")
  - sse_test.go   (EXTEND: + TestSSE_Inject_* (5) + TestSSE_EmitEvent_Framing +
                    injectWarning const + injectAll/firstEventData helpers)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only — needs "encoding/json", stdlib).
  - rewrite.go: the input source (RewriteResult.Notes). Untouched.
  - proxy.go / main.go / config.go / doc.go / *_test.go: zero edits.
  - testdata/*.sse: zero edits.
CONSUMER SEAM (this function feeds — keep signatures stable):
  - P1.M4.T2.S2 (conditional response): `if len(rewrittenIDs) > 0 { err = Inject(w,
        resp.Body, rewrittenIDs, warningText(res.Notes)) } else { _, err =
        io.Copy(w, resp.Body) }`; then `http.Flusher.Flush()`. Depends on Inject's
        exact signature and on rewrittenIDs being map[any]bool keyed by float64(N).
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

# Expected: gofmt clean; vet clean; only sse.go + sse_test.go changed; go.mod
# unchanged. If vet reports a duplicate import, REMOVE the duplicate (bufio/io/
# strings already present; only encoding/json is new).
```

### Level 2: Unit Tests (Component Validation)

```bash
# Targeted: run ONLY the Inject + emitEvent tests, verbose.
go test -run 'TestSSE_Inject|TestSSE_EmitEvent' -v

# MUST PASS (the ones that prove §12.2/§19.2 fidelity):
#   TestSSE_Inject_ToolCallPrependsWarning   -> content[0]=warning, content[1] valid+unchanged
#   TestSSE_Inject_IdNotInSetPassthrough     -> Data unchanged
#   TestSSE_Inject_InitializePassthrough     -> Data == initJSON
#   TestSSE_Inject_ErrorResultPassthrough    -> 3 sub-cases unchanged
#   TestSSE_Inject_MultilineRoundTrip        -> multi-line parses, injects, content valid
#   TestSSE_EmitEvent_Framing                -> exact framing bytes + round-trip
# Expected: PASS, exit 0. If ToolCallPrependsWarning fails on content[1].text, the
# cause is almost always HTML-escaping (forgot SetEscapeHTML(false)) or a guard
# that wrongly mutated content. Diff got vs want with %q.
```

### Level 3: Integration Testing (System Validation)

```bash
# No service to start (pure unit over fixtures). Confirm the module + full suite
# stay healthy and the package still compiles as `package main` with Inject present.
go build ./...        # must compile (Inject unused by non-test code so far — still must build)
go test ./...         # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . Inject       # sanity: doc comment present, shows the four rules (Mode A)
go doc . emitEvent    # sanity: framing doc present

# Expected: build clean; full suite green; go doc . Inject prints the id-correlation,
# prepend-only, isError-untouched, and passthrough-on-no-content rules. NOTE: actual
# end-to-end injection through a live upstream is exercised by P1.M4.T2 / P1.M5.T1;
# this item proves the pipeline over the golden fixtures.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Round-trip coherence: confirm emitEvent reverses the reader's Join exactly.
#     Feed a Data with internal newlines; the emitted bytes must re-decode to the
#     same Event (the TestSSE_EmitEvent_Framing round-trip subtests cover this).
go test -run 'TestSSE_EmitEvent_Framing' -v

# (b) Confirm the float64-id contract holds: the test keys rewrittenIDs by
#     float64(2) and the decoded obj["id"] is float64(2) — if a future producer
#     used int(2), this test would fail (id-not-in-set -> passthrough, no warning).
go test -run 'TestSSE_Inject_ToolCallPrependsWarning' -v

# (c) Confirm no HTML-escaping drift: grep marshalJSON for SetEscapeHTML(false).
grep -n 'SetEscapeHTML' sse.go
# expect: one line in marshalJSON setting it false.

# Expected: all targeted subtests PASS; SetEscapeHTML(false) is present (without it,
# a result whose text contains <>& would be byte-altered — not caught by the
# HTML-free fixtures, but required for production correctness).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `sse.go` + `sse_test.go`; `go.mod` unchanged.
- [ ] Level 2 passes: `go test -run 'TestSSE_Inject|TestSSE_EmitEvent' -v` green,
      including all five §19.2 cases and the emitEvent framing test.
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc . Inject`
      shows the four documented rules (Mode A).

### Feature Validation

- [ ] `func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error` exists (unexported) in `sse.go`.
- [ ] `func emitEvent(w io.Writer, ev Event) error` exists (unexported) in `sse.go`.
- [ ] Inject into `tools_call.sse` ({float64(2):true}) → `content[0]=={type:text,text:warning}`, `content[1].text` byte-identical to original AND `json.Valid`, `isError==false`.
- [ ] id not in set → Data unchanged; initialize → Data == initJSON; error envelope / isError:true / no-content → Data unchanged.
- [ ] Multi-line fixture injects correctly (content[0]=warning, content[1] valid JSON).
- [ ] emitEvent framing exact (`id:` iff ID; `event:` iff non-message; `data:` per `\n`-piece; blank line) and round-trips through NewSSEReader.

### Code Quality Validation

- [ ] `Inject`/`injectData`/`emitEvent`/`marshalJSON` are APPENDED to `sse.go` (reader + warningText untouched).
- [ ] Tests are APPENDED to `sse_test.go` (reader + warningText tests untouched; no redeclared `initJSON`/`initSSE`).
- [ ] Imports: only `encoding/json` added; no duplicate `bufio`/`io`/`strings`.
- [ ] Doc comment on `Inject` cites PRD §12.2 and documents id-correlation, prepend-only, isError-untouched, passthrough-on-no-content (Mode A).
- [ ] `marshalJSON` uses `SetEscapeHTML(false)` (no `<>&` alteration); trailing `\n` trimmed.
- [ ] Correlation uses `obj["id"]` (float64), NOT `Event.ID`.
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . Inject` prints the four rules; `go doc . emitEvent` prints the framing rules.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't correlate on `Event.ID`. The matching id is the JSON-RPC `id` INSIDE
  `Data` (`obj["id"]`), e.g. `2` for `tools_call.sse` whose `Event.ID == ""`. Keying
  on `Event.ID` would never match a tools/call result and the warning would never
  inject. Parse `Data` and read `obj["id"]`.
- ❌ Don't key `rewrittenIDs` by `int`. `encoding/json` decodes JSON numbers into
  `float64` in `any`; `map[any]bool{int(2): true}` would NOT match `obj["id"]`
  (`float64(2)`) and the warning would silently not inject. Key by `float64(N)` for
  numeric ids (and document the contract to the producer, P1.M4.T1.S1).
- ❌ Don't use plain `json.Marshal` to re-serialize. It HTML-escapes `<`, `>`, `&`
  to `\u003c`/`\u003e`/`\u0026` (verified), altering z.ai result text that may carry
  HTML from search results. Use `json.NewEncoder` + `SetEscapeHTML(false)` and trim
  the trailing `\n`. (The golden fixtures have no HTML chars, so this is invisible
  in tests but required for production.)
- ❌ Don't touch `isError`. FR-3: the proxy NEVER sets `isError:true`. Read it only
  for the isError:true → passthrough guard; never write it. A test asserts
  `isError==false` is preserved after inject.
- ❌ Don't mutate the original `content` elements. Prepend ONLY: `append([]any{
  warning}, content...)` leaves the originals in place at indices `[1:]`. Do not
  re-encode, re-order, or filter them. The test checks `content[1].text` is
  byte-identical to the original.
- ❌ Don't fail `Inject` on a parse error. A `Data` that isn't JSON (or isn't a
  matching result) is re-emitted UNCHANGED — `injectData` returns the original
  string. `Inject` itself returns `nil` at EOF, a reader error, or a WRITE error
  only. Bubbling a JSON parse error would break a stream that has one non-JSON
  event among many.
- ❌ Don't add an `event:message` line on re-emit. The reader normalizes a missing
  `event:` line to `Type=="message"`, indistinguishable from an explicit
  `event:message`. emitEvent OMITS `event:` for the default type (matching z.ai's
  tools/call wire form); emit it only for non-default types. Tests assert
  data-identity (decode output, compare `Event.Data`), not byte-identity of framing.
- ❌ Don't flush in `Inject`. `Inject` writes to `w`; flushing is the caller's job
  (P1.M4.T2.S2 via `http.Flusher`). Coupling Inject to `http.Flusher` would break
  its testability with a `*strings.Builder`.
- ❌ Don't OVERWRITE `sse.go`/`sse_test.go`, redeclare `initJSON`/`initSSE`, or
  duplicate imports. APPEND only. The reader owns `Event`/`Reader`/`Next`;
  `warningText` (S1) owns the formatter; this item owns `Inject`/`emitEvent`/
  `marshalJSON`. Add `encoding/json` to imports only if absent (it is).
- ❌ Don't call `warningText` from the tests. Pass a LITERAL warning string
  (`injectWarning`). Coupling to `warningText` would make an S1 wording change
  silently break Inject tests; the literal pins the seam. (The proxy wires
  `warningText` → `Inject` at runtime.)
- ❌ Don't modify `rewrite.go`, `go.mod`, `PRD.md`, `testdata/*`, or any other file.
  This item edits exactly `sse.go` + `sse_test.go` (append-only).
