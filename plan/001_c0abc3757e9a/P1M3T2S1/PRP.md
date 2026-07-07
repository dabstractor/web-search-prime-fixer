name: "P1.M3.T1.S1 — SSE Event type + reader (WHATWG framing, unbounded lines, EOF flush)"
description: |

  Implement `sse.go`'s **reader half** (the injector/warningText are P1.M3.T3):
  a hand-rolled Server-Sent Events decoder that yields decoded `Event`s from an
  upstream SSE body. The proxy uses it to parse z.ai's `text/event-stream`
  responses so a warning can later be injected into a `tools/call` result. There
  is **no stdlib SSE framer** (`architecture/external_deps.md` §2), so this is
  framed by hand per the WHATWG algorithm. The single most load-bearing decision:
  frame with **`bufio.Reader.ReadString('\n')` (unbounded line length), NOT
  `bufio.Scanner`** — `bufio.Scanner`'s `ScanLines` has a hard 64 KB token cap
  (`bufio.MaxScanTokenSize`, VERIFIED in GOROOT `bufio/scan.go`) and z.ai
  high-context `tools/call` results can exceed 64 KB; a Scanner-based framer
  would `ErrTooLong` on exactly the inputs this proxy exists to serve. Implement
  `type Event struct{ID,Type,Data string}`, `type Reader`, `NewSSEReader(r
  io.Reader) *Reader`, and `(rd *Reader) Next() (Event, error)` returning
  `io.EOF` when done. Framing per WHATWG / PRD §8.10+§12.1: `:`-comment lines
  ignored; `field: value` splits on the first colon and strips ONE leading
  space; multiple `data:` lines join with `\n`; a blank line dispatches + resets;
  a final event with no trailing blank line is flushed at EOF. Ship `sse_test.go`
  decoding the golden fixtures from the sibling "testdata" item (initialize /
  tools_call / tools_call_multiline), a >64 KB data-line parse (proves the
  bufio.Reader choice), and the multi-line join round-trip. **Mode A docs**: the
  doc comments on `Event`/`Reader`/`NewSSEReader`/`Next` document the framing
  rules and the bufio.Reader-vs-Scanner rationale.

  > Directory note (do not be confused by the path): orchestrator slots are
  > cross-labeled in this plan — this reader item lives at `P1M3T2S1/`, while
  > the **testdata fixtures** item lives at `P1M3T1S1/` (its `PRP.md` `name:`
  > reads `P1.M3.T2.S1 — testdata/...`). The fixtures are a HARD PREREQUISITE
  # for the fixture-consuming tests; see "Hard Prerequisites" below.

---

## Goal

**Feature Goal**: A stdlib-only, streaming SSE decoder (`Event` + `SSEReader`) that
correctly frames z.ai's `text/event-stream` bodies — including arbitrarily long
(`>64 KB`) `data:` lines and multi-line `data:` events — yielding one decoded
`Event` per `Next()` call and flushing a trailing un-terminated event at EOF.

**Deliverable**: TWO new files in the repo root (`package main`):
- `sse.go` — `type Event`, `type Reader`, `func NewSSEReader(r io.Reader) *Reader`,
  `func (rd *Reader) Next() (Event, error)`, plus unexported `dispatch`/`reset`
  helpers. Reader-only; **no** inject/warningText code (that is P1.M3.T3).
- `sse_test.go` — unit tests covering fixture decode, the >64 KB line, EOF flush,
  multi-line join, and a table of WHATWG framing edge cases.

`go.mod` gains ZERO `require`s (stdlib only). No other `.go` file is modified.

**Success Definition**: `go test -run 'TestSSEReader|TestSSE' -v` passes (and
`go vet ./...` / `go test ./...` stay clean); in particular the >64 KB line test
passes (proving the `bufio.Reader` choice) and the EOF-flush test passes (proving
§12.1 final-event buffering); `testdata/initialize.sse` decodes to
`Event{ID:"1", Type:"message", Data:<init JSON>}`.

## Hard Prerequisites

1. **testdata fixtures (the sibling "P1.M3.T2.S1" item).** This reader's fixture
   tests `os.ReadFile` three files that MUST exist on disk:
   `testdata/initialize.sse`, `testdata/tools_call.sse`,
   `testdata/tools_call_multiline.sse`. They are fully specified at
   `plan/001_c0abc3757e9a/P1M3T1S1/PRP.md` (+ `.../P1M3T1S1/research/wire-format-and-fixtures.md`).
   As of this writing `testdata/` holds only `.gitkeep`. **If those files are
   absent, create them FIRST from the verbatim bytes in that PRP's "Implementation
   Patterns" heredocs** (this item does not own the fixtures, but it cannot be
   validated without them). The self-contained framing tests below
   (`strings.NewReader`) pass regardless.
2. `proxy_test.go`'s `const initSSE` (already present) — reused by reference in
   `sse_test.go`; do NOT redeclare it (Go forbids duplicate package-level names).

## User Persona

**Target User**: the **next two work items** that call this API —
P1.M3.T3.S2 (injector: loops `rd.Next()`, parses `Event.Data` as JSON, prepends a
warning into `result.content`, re-emits by splitting `Data` on `\n` back into
multiple `data:` lines) and P1.M4.T2.S2 (response path: chooses `io.Copy`
passthrough vs. feed-through-the-injector). Plus the **maintainer**, who gets an
audited, WHATWG-correct framer instead of an ad-hoc scanner.

**Use Case**: `rd := NewSSEReader(resp.Body)` then `for { ev, err := rd.Next();
if err == io.EOF { break }; ... }` — streaming, one event at a time, no whole-body
buffering (so the passthrough path stays zero-parse per PRD §6).

**User Journey**: implementer reads the PRP's "Reference implementation" block →
writes `sse.go` (reader only) → writes `sse_test.go` (fixtures + self-contained
cases) → `go test -run 'TestSSEReader|TestSSE' -v` → green → P1.M3.T3.S2 imports
`Event`/`SSEReader`.

**Pain Points Addressed**: (1) no stdlib framer exists, so a naive `bufio.Scanner`
attempt silently breaks on `>64 KB` lines — this PRP picks the right primitive and
proves it with a test. (2) The "final event with no trailing blank line must still
dispatch" rule is easy to forget — an explicit EOF-flush test pins it. (3) Multi-
line `data:` join/split symmetry is subtle — the fixture + round-trip test pins it.

## Why

- **PRD §12.1 (Reader)**: defines `Event` and the parsing rules this item implements.
- **PRD §8.10 (SSE framing rules)**: the wire contract — events are field lines
  terminated by a blank line; `data:` may span multiple lines joined with `\n`.
- **PRD §19.2 (sse_test.go)**: names the multi-`data:`-line round-trip + tools/call
  decode cases this item's tests cover.
- **architecture/external_deps.md §2 + §7**: no stdlib framer; `bufio.Scanner`'s
  64 KB cap (VERIFIED in GOROOT) → must use `bufio.Reader.ReadString`. This is the
  single most important technical finding driving the design.
- **Decouples the reader from the injector.** The reader is a pure, testable unit
  before P1.M3.T3 (inject) and P1.M4.T2 (wiring) exist.

## What

`sse.go` (reader only) implementing the WHATWG framing algorithm, plus
`sse_test.go`. Visible behavior: a `Reader` over any `io.Reader` yields decoded
`Event`s; `io.EOF` ends the stream; `>64 KB` lines parse without error; an event
not terminated by a blank line is still delivered at EOF.

### Success Criteria

- [ ] `sse.go` defines `Event{ID,Type,Data string}`, `Reader`, `NewSSEReader`,
      `Next()` returning `(Event, error)` with `io.EOF` sentinel.
- [ ] Framing matches WHATWG: comments ignored; one-leading-space strip after the
      first colon; `data:` lines joined with `\n`; blank line dispatches + resets
      (only when ≥1 `data:` line seen); unknown fields ignored.
- [ ] A `data:` line whose VALUE exceeds 64 KB parses **without error** (proves
      `bufio.Reader` over `Scanner`).
- [ ] A stream ending with a pending event and **no trailing blank line** still
      delivers that event, then `io.EOF`.
- [ ] `testdata/initialize.sse` decodes to `Event{ID:"1", Type:"message",
      Data:<the init JSON>}`; `testdata/tools_call.sse` decodes to a `Type ==
      "message"` (default) event whose `Data` is valid JSON.
- [ ] `testdata/tools_call_multiline.sse` decodes to an event whose `Data` is
      valid JSON (the per-line values joined with `\n`).
- [ ] `go vet ./...` clean; `go test ./...` green (no regression); `go.mod` has
      zero new `require`s; no other `.go` file changed.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge of this codebase can implement from this
PRP alone because: (a) the full reference implementation is given in
"Reference implementation" below; (b) the framing algorithm is pinned to WHATWG +
PRD §8.10/§12.1 with a per-rule mapping; (c) the `bufio.Reader`-not-`Scanner`
decision is justified against VERIFIED GOROOT facts; (d) the exact testdata bytes
the fixtures decode to are reproduced here; (e) the test table (input → assertion
→ PRD section) is enumerated; (f) codebase conventions (package, imports, doc
style, test helpers, the existing `initSSE` constant) are captured. The only
external dependency is the testdata fixtures, whose verbatim bytes are inlined in
the sibling fixtures PRP and summarized here.

### Documentation & References

```yaml
# MUST READ — the framing contract this item implements.
- file: PRD.md
  section: "§8 SSE framing rules (for the parser/writer)" + "§12 SSE warning injection / §12.1 Reader"
  why: the Event struct fields, the per-line framing rules (comment, field:value
        one-space-strip, data join with \n, blank-line dispatch, EOF flush), and
        the multi-line `data:` form.
  critical: §12.1 "Type defaults to message"; "Buffer a final event if the stream
        ends without a trailing blank line." §8.10: z.ai emits one `data:` line per
        message but the parser MUST handle the multi-line form.

- docfile: plan/001_c0abc3757e9a/architecture/external_deps.md
  why: §2 (no stdlib SSE framer → hand-roll), §3 (WHATWG framing rules, verified),
        §7 (bufio.Scanner 64 KB cap VERIFIED in GOROOT → use bufio.Reader.ReadString).
  section: "§7 FINDING — bufio.Scanner 64 KB token limit" and "§3 SSE framing rules (WHATWG)"
  critical: the bufio.Reader-not-Scanner decision is THE load-bearing technical
        choice; cite it in the SSEReader doc comment (Mode A docs requirement).

- docfile: plan/001_c0abc3757e9a/P1M3T2S1/research/sse-reader-design.md
  why: this item's own research — the full algorithm, the reference Go
        implementation, the test→PRD mapping, gotchas, consumer contracts.
  section: "§2 bufio.Reader vs Scanner", "§3 WHATWG framing algorithm", "§4 Reference implementation".

# HARD PREREQUISITE — the testdata fixtures (sibling item, cross-labeled dir).
- docfile: plan/001_c0abc3757e9a/P1M3T1S1/PRP.md
  why: defines + inlines the verbatim bytes for testdata/initialize.sse,
        testdata/tools_call.sse, testdata/tools_call_multiline.sse (the inputs
        this reader's fixture tests consume). CREATE THESE FILES FIRST if absent.
  section: "The exact bytes to write (copy verbatim)" and "Known Gotchas".

- url: https://html.spec.whatwg.org/multipage/server-sent-data.html#parsing-an-event-stream
  why: WHATWG "Parsing an event stream" — authoritative framing (comment lines,
        field/value split on first colon + one leading space strip, data buffer
        append-value-then-LF with one trailing LF stripped at dispatch, blank-line
        dispatch, default event type "message").
  critical: confirms `data:` may be delivered across multiple lines joined by LF;
        an event with an empty data buffer is NOT dispatched (blank line is a no-op).

- url: https://pkg.go.dev/bufio#Reader.ReadString
  why: `ReadString('\n')` returns the line up to+including the delimiter, or the
        partial tail + io.EOF — unbounded line length (grows its buffer). This is
        the framing primitive.
  critical: at EOF without a delimiter it returns `(partial, io.EOF)` — process the
        partial line FIRST, then flush the pending event (see Reference implementation).

- url: https://pkg.go.dev/bufio#Scanner
  why: documents `MaxScanTokenSize = 64*1024` and `ErrTooLong` — the trap this
        design explicitly avoids.

# CODEBASE CONVENTIONS — follow these patterns.
- file: rewrite.go
  why: doc-comment style (cite PRD/architecture inline), guard-clauses-first,
        unexported helpers, in-place/no-surprise API.
  pattern: top-of-type doc comment listing invariants + algorithm steps referencing PRD sections.

- file: rewrite_test.go
  why: test conventions — `package main`, table-driven `tests := []struct{...}{...}`
        with `t.Run(tc.name, ...)`, `reflect.DeepEqual`, per-case comments citing PRD §.

- file: proxy_test.go
  why: contains `const initSSE` (the exact initialize bytes this reader's fixture
        test asserts against) and the test-helper style (`fakeUpstream`, `newTestProxy`).
  pattern: REUSE `initSSE` by reference; do NOT redeclare it.
  gotcha: `initSSE` is string-concatenated across 4 raw-string lines ending in "\n\n";
        its DATA segment is the exact `Event.Data` initialize.sse must decode to.

- file: doc.go
  why: confirms `package main`, flat layout, stdlib-only.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment
  main.go           # bootstrap + `type logger`/`newLogger`/`(*logger).log` (P1.M1.T4)
  config.go         # Config + DefaultConfig + LoadConfig (P1.M1.T2)
  proxy.go          # passthrough forward core + hop-by-hop set (P1.M1.T4.S2) — UNTOUCHED
  rewrite.go        # Rewrite + RewriteResult (P1.M2.T1) — UNTOUCHED
  *_test.go         # config/resolve/logger/health/proxy/rewrite tests — UNTOUCHED
  testdata/.gitkeep # empty placeholder — the 3 .sse fixtures are added by the sibling item
  PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
sse.go              # NEW — Event, Reader, NewSSEReader, Next, dispatch, reset (READER ONLY).
                     #        P1.M3.T3.S2 will EXTEND this file with inject + warningText later.
sse_test.go         # NEW — fixture decode tests + self-contained framing/64KB/EOF tests.
                     #        Reuses proxy_test.go's `const initSSE` by reference.
testdata/*.sse      # PROVIDED by the sibling "testdata" item (hard prerequisite) —
                     #        initialize.sse, tools_call.sse, tools_call_multiline.sse.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — USE bufio.Reader.ReadString('\n'), NOT bufio.Scanner. bufio.Scanner's
  ScanLines has a hard 64 KB token cap (bufio.MaxScanTokenSize, VERIFIED in
  /usr/lib/go/src/bufio/scan.go); it returns bufio.ErrTooLong (errors, does not
  truncate) on a >64 KB line. z.ai tools/call stringified-array payloads on
  high-context results EXCEED 64 KB. A Scanner framer breaks on exactly the
  inputs this proxy serves. bufio.Reader.ReadString grows its buffer with no line-
  length limit. The >64 KB test exists to PROVE this choice — do not weaken it.

CRITICAL — EOF FLUSH (PRD §12.1). ReadString('\n') at end-of-stream returns
  (partialLine, io.EOF). You MUST process partialLine FIRST (it is the last data:
  line), THEN dispatch the pending event; the NEXT Next() call returns io.EOF. A
  reader that returns io.EOF while a data line is still buffered LOSES the final
  event. The TestSSEReader_EOFFlushNoTrailingBlank test pins this.

CRITICAL — Data has NO trailing newline. Event.Data = strings.Join(dataLines, "\n").
  P1.M3.T3.S2 re-emits by splitting Data on "\n" back into data: lines; the
  reader's Join and the writer's Split must be EXACT inverses. Do not append a
  trailing "\n" (that would break round-trip symmetry). This is bit-identical to
  WHATWG's "append value+LF per data line, strip ONE trailing LF at dispatch".

GOTCHA — dispatch ONLY when ≥1 data: line seen. WHATWG: a blank line with an empty
  data buffer does NOT dispatch (it is a no-op that just resets). So consecutive
  blank lines produce no spurious empty events, and an id:/event:-only block with
  no data: line is dropped. Track pending state with len(data) > 0.

GOTCHA — strip EXACTLY ONE leading space after the colon. "data: foo" -> "foo";
  "data:  foo" -> " foo" (one space remains). Split on the FIRST colon
  (strings.IndexByte), then `if strings.HasPrefix(value, " ") { value = value[1:] }`.
  A field with no colon (bare line) has value "". The z.ai fixtures use NO space
  after the colon (data:{...}); the strip is a no-op for them but is required for
  WHATWG correctness and other servers.

GOTCHA — line trimming. strings.TrimRight(line, "\r\n") removes all trailing
  \r/\n. Safe here because ReadString stops at the FIRST \n, so a physical line
  carries at most one \n (terminator) and at most one preceding \r (CRLF). A data
  VALUE never contains a raw \n on the wire (that \n would be a line terminator —
  i.e. the multi-line data: form). Lone-CR (pre-OSX Mac) line endings are NOT
  split by ReadString('\n'); z.ai/HTTP use LF or CRLF only — acceptable, document it.

GOTCHA — reset data to nil, not []string{}. After dispatch, rd.data must be nil
  (or empty) so len(rd.data) > 0 correctly reads "no pending event". reset() sets
  rd.id, rd.event = "", "" and rd.data = nil.

GOTCHA — DO NOT redeclare `const initSSE`. proxy_test.go already defines it in
  package main; sse_test.go is the SAME package and references it directly
  (initSSE is byte-identical to testdata/initialize.sse). A second declaration is
  a compile error.

GOTCHA — sse.go is READER-ONLY in this item. Do NOT add inject/warningText/
  warning formatting here (that is P1.M3.T3.S1/S2, which EXTENDS sse.go). Keep
  the public surface to Event + Reader + NewSSEReader + Next so P1.M3.T3 has a
  clean extension point.

GOTCHA — no whole-body buffering. The reader must stream line-by-line from r so
  the P1.M4.T2.S2 passthrough path can stay io.Copy (zero-parse) when no rewrite
  occurred (PRD §6 minimal overhead). Do NOT read the entire body up front.

GOTCHA — `value[1:]` after HasPrefix(" ") is safe; value == " " becomes "".
```

## Implementation Blueprint

### Data models and structure

```go
// Event is one decoded Server-Sent Events event (PRD §12.1).
type Event struct {
	ID   string // the "id:" field value; "" if the event had no id: line
	Type string // the "event:" field value, or "message" (SSE default) if none
	Data string // the "data:" field values joined with "\n"; "" if none
}

// Reader decodes an SSE stream from an io.Reader one Event at a time.
type Reader struct {
	r     *bufio.Reader // unbounded line reads (NOT bufio.Scanner — see doc)
	id    string        // accumulated "id:"     for the in-progress event
	event string        // accumulated "event:"  for the in-progress event
	data  []string      // accumulated "data:" values; len>0 <=> pending event
}
```

No ORM/pydantic (this is Go). No config/DB/routes touched. The "model" is just
`Event` + the unexported `Reader` state.

### Reference implementation (write `sse.go` from this — reader only)

```go
package main

import (
	"bufio"
	"io"
	"strings"
)

// Event is one decoded Server-Sent Events event (PRD §12.1). ID is the "id:"
// field value ("" if absent). Type is the "event:" field value, defaulting to
// "message" when the event had no "event:" line (the SSE default event type).
// Data is the event's "data:" field values joined with "\n" ("" if absent);
// it carries the JSON-RPC message on the wire and must be free of any trailing
// newline so the writer (P1.M3.T3.S2) can split it back into "data:" lines and
// reproduce the original bytes (round-trip symmetry, PRD §8.10).
type Event struct {
	ID   string
	Type string
	Data string
}

// Reader decodes a Server-Sent Events stream (RFC/WHATWG framing) from an
// upstream body — typically an *http.Response.Body — yielding one Event per
// Next() call and io.EOF when the stream is exhausted.
//
// Framing (WHATWG "Parsing an event stream"; PRD §8.10 + §12.1):
//   - Lines beginning with ':' are comments and are ignored.
//   - "field: value" splits on the FIRST colon; exactly ONE leading space of the
//     value is stripped if present. A line with no colon is a bare field name
//     with an empty value.
//   - "data:" values are accumulated; the event's Data is their Join("\n").
//   - "id:" sets the event ID; "event:" sets the event type; other field names
//     (e.g. "retry") are ignored.
//   - A blank line dispatches the accumulated event (only if at least one
//     "data:" line was seen) and resets the accumulators. If no "data:" line was
//     seen the blank line is a no-op (no empty event is emitted).
//   - A final event not terminated by a blank line is still flushed at EOF.
//
// LINE FRAMING PRIMITIVE: this uses bufio.Reader.ReadString('\n'), NOT
// bufio.Scanner. bufio.Scanner's ScanLines split has a hard 64 KiB token cap
// (bufio.MaxScanTokenSize) and errors with bufio.ErrTooLong on a longer line;
// z.ai tools/call results (stringified-array payloads on high-context responses)
// can exceed 64 KiB, so a Scanner-based framer would break on exactly the inputs
// this proxy serves. ReadString grows its buffer with no line-length limit
// (verified against the installed Go toolchain, GOROOT bufio/scan.go + bufio.go;
// see architecture/external_deps.md §7).
type Reader struct {
	r     *bufio.Reader
	id    string
	event string
	data  []string
}

// NewSSEReader returns a Reader that decodes the SSE stream from r. r is wrapped
// in a bufio.NewReader (4 KiB read buffer; ReadString grows past it on demand).
// The Reader does not buffer the whole body — it reads line-by-line so callers
// may choose a zero-parse passthrough (io.Copy) on the non-rewrite path.
func NewSSEReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

// Next decodes and returns the next Event. It returns io.EOF when the stream is
// fully consumed. A pending event with no terminating blank line is flushed
// before io.EOF is reported (PRD §12.1).
func (rd *Reader) Next() (Event, error) {
	for {
		line, err := rd.r.ReadString('\n')
		if line != "" { // a complete line, or a partial final line before EOF
			line = strings.TrimRight(line, "\r\n")
			if line == "" { // blank line: dispatch + reset
				if len(rd.data) > 0 {
					return rd.dispatch(), nil
				}
				rd.reset()
				continue
			}
			if strings.HasPrefix(line, ":") { // comment line
				continue
			}
			field, value := line, ""
			if i := strings.IndexByte(line, ':'); i >= 0 {
				field, value = line[:i], line[i+1:]
				if strings.HasPrefix(value, " ") { // strip ONE leading space
					value = value[1:]
				}
			}
			switch field {
			case "data":
				rd.data = append(rd.data, value)
			case "id":
				rd.id = value
			case "event":
				rd.event = value
			default:
				// unknown field (retry, etc.) -> ignored per WHATWG
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(rd.data) > 0 { // flush the final un-terminated event
					return rd.dispatch(), nil
				}
				return Event{}, io.EOF
			}
			return Event{}, err // unexpected I/O error (propagate)
		}
	}
}

// dispatch builds the Event from the accumulators, applies the "message" default,
// resets the accumulators, and returns the Event.
func (rd *Reader) dispatch() Event {
	ev := Event{ID: rd.id, Type: rd.event, Data: strings.Join(rd.data, "\n")}
	if ev.Type == "" {
		ev.Type = "message"
	}
	rd.reset()
	return ev
}

// reset clears the per-event accumulators (id, event, data) for the next event.
func (rd *Reader) reset() {
	rd.id, rd.event, rd.data = "", "", nil
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) ensure testdata fixtures exist
  - RUN: ls testdata/initialize.sse testdata/tools_call.sse testdata/tools_call_multiline.sse
  - IF ANY IS MISSING: create all three from the verbatim heredocs in
        plan/001_c0abc3757e9a/P1M3T1S1/PRP.md ("The exact bytes to write").
        (This item does not OWN the fixtures, but it cannot be validated without them.)
  - DO NOT modify any .go file in this task; fixtures are byte files under testdata/.

Task 1: CREATE sse.go (READER ONLY)
  - WRITE the "Reference implementation" block above verbatim (Event, Reader,
        NewSSEReader, Next, dispatch, reset). Keep the doc comments — they ARE the
        Mode A deliverable (framing rules + bufio.Reader rationale).
  - IMPORTS: bufio, io, strings ONLY (stdlib). No third-party imports.
  - PUBLIC SURFACE: Event, Reader, NewSSEReader, Next. NOTHING ELSE (inject/
        warningText are P1.M3.T3 — do not stub them here).
  - DO NOT touch any other .go file.

Task 2: CREATE sse_test.go
  - PACKAGE: main (same package; reuse proxy_test.go's `const initSSE`).
  - IMPORTS: encoding/json, io, os, reflect, strings, testing.
  - TESTS (see the test table below for inputs/assertions):
      TestSSEReader_DecodeInitializeFixture
      TestSSEReader_DecodeToolsCallFixture
      TestSSEReader_MultilineJoinFixture
      TestSSEReader_LargeLineOver64KB
      TestSSEReader_EOFFlushNoTrailingBlank
      TestSSE_Framing_Table          (t.Run subtests)
      TestSSEReader_MultipleEvents
      TestSSEReader_InitializeFixtureEqualsInitSSE  (belt+suspenders byte check)
  - NAMING: TestSSEReader_<Scenario> / TestSSE_<Area>; table cases via t.Run.
  - COVERAGE: comments, one-space-strip, default+explicit event type, id, unknown
        field ignored, multi-data join, CRLF tolerance, >64KB, EOF flush, multi-event,
        fixture decode + JSON validity, fixture==initSSE byte identity.
  - PLACEMENT: repo-root sse_test.go (alongside the code it tests).

Task 3: VALIDATE
  - gofmt -w sse.go sse_test.go
  - go vet ./...
  - go test -run 'TestSSEReader|TestSSE' -v
  - go test ./...
  - ALL must be green; git diff must show ONLY sse.go + sse_test.go (+ any fixtures
        created in Task 0, which are owned by the sibling item).
```

### Test table (sse_test.go)

```go
// Reuse proxy_test.go's initSSE (byte-identical to testdata/initialize.sse).
// The initialize Data segment == initSSE with the "id:1\nevent:message\ndata:" prefix
// and trailing "\n\n" removed:
var initJSON = strings.TrimSuffix(           // drop trailing blank-line "\n"
	strings.TrimPrefix(initSSE, "id:1\nevent:message\ndata:"),
	"\n\n")
```

| Test | Input | Asserts | PRD |
|------|-------|---------|-----|
| `TestSSEReader_DecodeInitializeFixture` | `os.ReadFile("testdata/initialize.sse")` → `NewSSEReader` | `ev.ID=="1"`, `ev.Type=="message"`, `ev.Data==initJSON`; next `Next()`→`io.EOF` | §8 init, §12.1 |
| `TestSSEReader_DecodeToolsCallFixture` | `os.ReadFile("testdata/tools_call.sse")` | `ev.ID==""`, `ev.Type=="message"` (default), `json.Valid([]byte(ev.Data))==true` | §8 tools/call, §19.2 |
| `TestSSEReader_MultilineJoinFixture` | `os.ReadFile("testdata/tools_call_multiline.sse")` | `json.Valid([]byte(ev.Data))==true`; `strings.Count(ev.Data,"\n")+1 == 8` (joined the 8 data: values) | §8.10, §19.2 |
| `TestSSEReader_LargeLineOver64KB` | `strings.NewReader("data:"+strings.Repeat("x",70000)+"\n\n")` | `ev, err := rd.Next()` → `err==nil`, `len(ev.Data)==70000`; next → `io.EOF` | external_deps §7 |
| `TestSSEReader_EOFFlushNoTrailingBlank` | `strings.NewReader("data:hello")` (no `\n\n`) | 1st `Next()`→`Event{Data:"hello"}`; 2nd → `io.EOF` | §12.1 EOF flush |
| `TestSSE_Framing_Table` (t.Run) | various `strings.NewReader(...)` | see sub-cases below | §12.1 / WHATWG |
| `TestSSEReader_MultipleEvents` | `strings.NewReader("data:a\n\ndata:b\n\n")` | `Next()`→`{Data:"a"}`, `Next()`→`{Data:"b"}`, `Next()`→`io.EOF` | §8.10 dispatch+reset |
| `TestSSEReader_InitializeFixtureEqualsInitSSE` | `os.ReadFile("testdata/initialize.sse")` | `string(raw)==initSSE` | fixtures item byte-identity gate |

`TestSSE_Framing_Table` sub-cases (each: build a `Reader`, call `Next()`, assert):

```go
cases := []struct{
	name string
	in   string
	want Event
}{
	{"comment_ignored", ": a comment\ndata:foo\n\n", Event{Type:"message", Data:"foo"}},
	{"one_leading_space_stripped", "data: foo\n\n", Event{Type:"message", Data:"foo"}},
	{"two_leading_spaces_keep_one", "data:  foo\n\n", Event{Type:"message", Data:" foo"}},
	{"default_event_type_message", "data:foo\n\n", Event{Type:"message", Data:"foo"}},
	{"explicit_event_type", "event:custom\ndata:foo\n\n", Event{Type:"custom", Data:"foo"}},
	{"id_field", "id:42\ndata:foo\n\n", Event{ID:"42", Type:"message", Data:"foo"}},
	{"unknown_field_ignored", "retry:10000\ndata:foo\n\n", Event{Type:"message", Data:"foo"}},
	{"multiple_data_joined", "data:a\ndata:b\n\n", Event{Type:"message", Data:"a\nb"}},
	{"crlf_tolerated", "data:foo\r\n\r\n", Event{Type:"message", Data:"foo"}},
	{"no_space_after_colon", "data:foo\n\n", Event{Type:"message", Data:"foo"}}, // z.ai wire form
}
```
For each, also assert the subsequent `Next()` returns `io.EOF` (single-event streams).
Use `reflect.DeepEqual(got, tc.want)` for the Event (all three fields compared).

### Implementation Patterns & Key Details

```go
// PATTERN: load a fixture once, assert the decoded Event, assert EOF after.
func TestSSEReader_DecodeInitializeFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/initialize.sse")
	if err != nil { t.Skipf("testdata fixture missing — run the P1.M3.T2.S1 fixtures item first: %v", err) }
	rd := NewSSEReader(strings.NewReader(string(raw))) // raw is []byte; keep imports minimal
	ev, err := rd.Next()
	if err != nil { t.Fatalf("first Next() err=%v, want nil", err) }
	if ev.ID != "1" { t.Errorf("ID=%q want 1", ev.ID) }
	if ev.Type != "message" { t.Errorf("Type=%q want message", ev.Type) }
	if ev.Data != initJSON { t.Errorf("Data mismatch:\n got %q\nwant %q", ev.Data, initJSON) }
	if _, err := rd.Next(); err != io.EOF { t.Errorf("second Next() err=%v, want io.EOF", err) }
}

// PATTERN: the >64KB test PROVES the bufio.Reader choice (Scanner would ErrTooLong).
func TestSSEReader_LargeLineOver64KB(t *testing.T) {
	big := strings.Repeat("x", 70000) // 70000 > 65536 (bufio.MaxScanTokenSize)
	rd := NewSSEReader(strings.NewReader("data:" + big + "\n\n"))
	ev, err := rd.Next()
	if err != nil { t.Fatalf("Next() on >64KB line err=%v — did you use bufio.Scanner? want nil", err) }
	if len(ev.Data) != 70000 { t.Errorf("len(Data)=%d want 70000", len(ev.Data)) }
}

// PATTERN: EOF flush — stream with NO trailing blank line still delivers the event.
func TestSSEReader_EOFFlushNoTrailingBlank(t *testing.T) {
	rd := NewSSEReader(strings.NewReader("data:hello")) // no \n\n
	ev, err := rd.Next()
	if err != nil { t.Fatalf("Next() err=%v, want nil (EOF flush)", err) }
	if ev.Data != "hello" { t.Errorf("Data=%q want hello", ev.Data) }
	if _, err := rd.Next(); err != io.EOF { t.Errorf("second Next() err=%v, want io.EOF", err) }
}

// GOTCHA (restated): ReadString returns (partial, io.EOF) at end-of-stream —
// process partialLine THEN flush. Next() must NEVER lose a buffered data line.
// GOTCHA (restated): dispatch() resets data to nil so len(data)>0 means "pending".
// GOTCHA (restated): do NOT append a trailing "\n" to Data (round-trip symmetry).
```

### Integration Points

```yaml
FILES CREATED:
  - sse.go       (NEW — Event, Reader, NewSSEReader, Next, dispatch, reset)
  - sse_test.go  (NEW — fixture + framing/64KB/EOF tests)
FILES PROVIDED BY SIBLING ITEM (hard prerequisite; create in Task 0 if missing):
  - testdata/initialize.sse, testdata/tools_call.sse, testdata/tools_call_multiline.sse
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only).
  - proxy_test.go: keep `const initSSE`; sse_test.go REFERENCES it (no redeclare).
  - main.go / config.go / proxy.go / rewrite.go / doc.go: zero edits.
CONSUMER SEAMS (this API feeds — keep the shape stable):
  - P1.M3.T3.S2 (injector): `for ev,err := rd.Next(); ...`, json.Unmarshal(ev.Data),
        prepend warning into result.content, re-emit by splitting ev.Data on "\n"
        into multiple "data:" lines. Depends on Event{ID,Type,Data} + Join-no-trailing-newline.
  - P1.M4.T2.S2 (response path): passthrough=io.Copy; rewrite=NewSSEReader(resp.Body)
        then injector. Reader must stream (no whole-body buffering) for §6 overhead.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# Format + vet the two new files (and confirm no collateral edits).
gofmt -w sse.go sse_test.go
go vet ./...
git status --short     # expect: ?? sse.go  ?? sse_test.go  (+ testdata/*.sse if Task 0 created them)
git diff go.mod        # expect: EMPTY (zero new requires)

# Expected: gofmt produces no diffs (already formatted); vet clean; only the two
# new .go files (+ any fixtures) are untracked; go.mod unchanged.
```

### Level 2: Unit Tests (Component Validation)

```bash
# Targeted: run ONLY the SSE reader tests, verbose.
go test -run 'TestSSEReader|TestSSE' -v

# MUST PASS (the three that prove the design):
#   TestSSEReader_LargeLineOver64KB         -> proves bufio.Reader over Scanner
#   TestSSEReader_EOFFlushNoTrailingBlank   -> proves §12.1 final-event buffering
#   TestSSEReader_DecodeInitializeFixture   -> proves §8 wire-format decode
#   TestSSEReader_MultilineJoinFixture      -> proves §8.10 multi-line join
#   TestSSE_Framing_Table                   -> proves WHATWG framing edge cases

# Expected: PASS, exit 0. If TestSSEReader_LargeLineOver64KB fails with an error,
# the implementer used bufio.Scanner — switch to bufio.Reader.ReadString('\n').
# If the fixture tests SKIP with "testdata fixture missing", run the P1.M3.T2.S1
# fixtures item (or create the three files from P1M3T1S1/PRP.md verbatim bytes).
```

### Level 3: Integration Testing (System Validation)

```bash
# No service to start (pure unit). Confirm the module + full suite stay healthy
# with the new files present, and that the package compiles as `package main`.
go build ./...      # must compile (sse.go is used only by tests so far — still must build)
go test ./...       # config/resolve/logger/health/proxy/rewrite + new sse tests — ALL green
go doc . Event      # sanity: doc comment present and readable (Mode A docs)
go doc . Reader     # sanity: framing + bufio.Reader rationale visible

# Expected: build clean; full suite green; `go doc` shows the doc comments that
# document the framing rules and the bufio.Reader-not-Scanner choice.
# NOTE: the streaming behavior (Reader over a real *http.Response.Body) is
# exercised end-to-end by P1.M4.T2.S2 / P1.M5.T1; this item's Level 2/3 prove
# the framing logic in isolation.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Hand-prove the >64KB claim against the GOROOT const (documents the decision):
grep -n 'MaxScanTokenSize' "$(go env GOROOT)/src/bufio/scan.go"
# expect: a line like `MaxScanTokenSize = 64 * 1024` — confirms the cap this design avoids.

# (b) Multi-line round-trip symmetry proof (reader join == writer split input):
#     decode tools_call_multiline.sse, then re-split Data on "\n" and confirm it
#     reproduces the per-line values (this is also asserted in
#     TestSSEReader_MultilineJoinFixture via strings.Count).

# (c) WHATWG fidelity spot-check: feed a known WHATWG example and confirm the
#     decoded Data/Type match (covered by TestSSE_Framing_Table cases).

# Expected: MaxScanTokenSize line found; round-trip and WHATWG cases pass.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git status` shows only
      `sse.go` + `sse_test.go` (+ fixtures if Task 0 ran); `go.mod` unchanged.
- [ ] Level 2 passes: `go test -run 'TestSSEReader|TestSSE' -v` is green, including
      the >64 KB, EOF-flush, multi-line-join, and framing-table tests.
- [ ] Level 3 passes: `go build ./...` and `go test ./...` are green; `go doc . Reader`
      shows the framing + bufio.Reader rationale doc comment (Mode A docs).

### Feature Validation

- [ ] `Event{ID,Type,Data string}` exists; `Reader` + `NewSSEReader(r io.Reader)*Reader`
      + `(rd *Reader) Next() (Event, error)` exist and return `io.EOF` at end.
- [ ] `testdata/initialize.sse` → `Event{ID:"1", Type:"message", Data:<init JSON>}`;
      `testdata/tools_call.sse` → `Type=="message"`, valid-JSON `Data`.
- [ ] `testdata/tools_call_multiline.sse` → valid-JSON `Data` (8 data: lines joined).
- [ ] A `>64 KB` data line parses with NO error (bufio.Reader proven).
- [ ] A pending event with no trailing blank line is flushed at EOF.
- [ ] Comments ignored; one-leading-space stripped; default Type "message";
      unknown fields ignored; multi-data join `a\nb`; CRLF tolerated.

### Code Quality Validation

- [ ] `sse.go` is READER-ONLY (no inject/warningText — those are P1.M3.T3).
- [ ] Imports are `bufio`, `io`, `strings` only (stdlib; zero new `require`s).
- [ ] Doc comments cite PRD §8.10/§12.1 + the bufio.Reader-vs-Scanner rationale.
- [ ] `sse_test.go` is `package main` and reuses `proxy_test.go`'s `initSSE`
      (no redeclaration); table-driven with `t.Run` and `reflect.DeepEqual`.
- [ ] No whole-body buffering (the reader streams line-by-line).
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: doc comments on `Event`/`Reader`/`NewSSEReader`/`Next`
      document the framing rules and the bufio.Reader-not-Scanner choice.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't use `bufio.Scanner` / `ScanLines`. Its 64 KB `MaxScanTokenSize` errors on
  the >64 KB lines z.ai returns; the whole point of this item is to avoid that.
  Use `bufio.Reader.ReadString('\n')` (unbounded). The >64 KB test exists to catch
  this regression — do not weaken or skip it.
- ❌ Don't lose the final event at EOF. `ReadString` returns `(partial, io.EOF)` at
  end-of-stream; process the partial line THEN flush the pending event before
  reporting `io.EOF`. Returning `io.EOF` while a `data:` line is buffered drops data.
- ❌ Don't append a trailing newline to `Event.Data`. `Data = strings.Join(lines, "\n")`
  only. A trailing `\n` breaks the writer's split-on-`\n` round-trip (P1.M3.T3.S2).
- ❌ Don't dispatch on a blank line when no `data:` line was seen (WHATWG: empty data
  buffer → no-op reset, not an empty event). Track pending state with `len(data) > 0`.
- ❌ Don't strip more than ONE leading space after the colon. `data:  foo` → ` foo`
  (one space remains). Split on the FIRST colon only (`strings.IndexByte`).
- ❌ Don't read the whole body into memory up front. Stream line-by-line so the
  passthrough path (P1.M4.T2.S2) can stay `io.Copy` (PRD §6 minimal overhead).
- ❌ Don't redeclare `const initSSE`. It already exists in `proxy_test.go` (same
  `package main`); reference it. A duplicate is a compile error.
- ❌ Don't add `inject`/`warningText`/warning formatting here. This item ships the
  READER only; P1.M3.T3.S2 extends `sse.go` with the injector. Keep the public
  surface to `Event`/`Reader`/`NewSSEReader`/`Next`.
- ❌ Don't modify any other `.go` file, `go.mod`, or `PRD.md`. This item adds exactly
  `sse.go` + `sse_test.go` (and ensures the testdata fixtures exist).
- ❌ Don't forget Mode A docs. The doc comments documenting the framing rules and the
  bufio.Reader-vs-Scanner rationale are an explicit deliverable, not optional prose.
