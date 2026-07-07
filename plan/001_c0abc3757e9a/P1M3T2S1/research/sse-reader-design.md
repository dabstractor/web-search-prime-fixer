# Research — SSE Event type + reader (WHATWG framing, unbounded lines, EOF flush)

Item: **P1.M3.T1.S1** (the SSE reader/`Event` type; written to the **P1M3T2S1/**
slot per orchestrator path, mirroring how the sibling fixtures item was written
to the P1M3T1S1/ slot — see §0). This is the SOURCE-OF-TRUTH brief for the PRP.
Every framing claim below is pinned to PRD §8.10/§12.1 and verified against the
WHATWG SSE algorithm and the installed Go toolchain source.

---

## 0. Directory cross-labeling (READ THIS FIRST)

The orchestrator paths are **swapped** relative to the work-item IDs:

| Physical dir under plan/001_c0abc3757e9a/ | Actual content            |
|-------------------------------------------|---------------------------|
| `P1M3T1S1/`                               | **P1.M3.T2.S1** — testdata fixtures (its `PRP.md` `name:` field says so) |
| `P1M3T2S1/`  ← **THIS item**              | **P1.M3.T1.S1** — SSE Event type + reader |

So: the **testdata fixtures are a sibling artifact**, fully specified at
`plan/001_c0abc3757e9a/P1M3T1S1/PRP.md` + `.../P1M3T1S1/research/wire-format-and-fixtures.md`.
This reader item **consumes** those fixtures (`testdata/initialize.sse`,
`testdata/tools_call.sse`, `testdata/tools_call_multiline.sse`). They are a
**HARD PREREQUISITE**: as of this writing `testdata/` contains only `.gitkeep`,
so the implementer must ensure the fixtures item has run first (or create the
three files from the verbatim bytes in that PRP). Self-contained framing tests
(strings.NewReader) do NOT depend on the fixtures.

---

## 1. The contract (from the work item + PRD §12.1)

Deliverables (all in **`sse.go`**, `package main`):

```go
type Event struct {
    ID   string // "id:" field value; "" if absent
    Type string // "event:" field value; defaults to "message" when no event: line
    Data string // "data:" field values joined with "\n"
}

type Reader struct { /* unexported: a *bufio.Reader + per-event accumulators */ }

func NewSSEReader(r io.Reader) *Reader
func (rd *Reader) Next() (Event, error) // io.EOF when the stream is exhausted
```

No other symbols ship in this item. `inject`/`warningText` are **P1.M3.T3**;
they EXTEND `sse.go` later. `sse.go` is CREATED here with the reader only.

---

## 2. THE load-bearing decision: `bufio.Reader.ReadString('\n')`, NOT `bufio.Scanner`

**VERIFIED** in the installed toolchain (`go1.26.4`, `GOROOT=/usr/lib/go`),
documented in `architecture/external_deps.md` §7:

- `bufio.Scanner` defaults to the `ScanLines` split with a **hard 64 KB token cap**
  (`const MaxScanTokenSize = 64*1024` in `bufio/scan.go`). A single line/token
  exceeding 64 KB makes `Scan()` return false and `Err()` report `bufio.ErrTooLong`
  (it does NOT silently truncate; it errors). Raising the cap requires an explicit
  `Scanner.Buffer(buf, bigN)` call.
- z.ai `tools/call` results carry a **stringified-array `text` payload** that, on a
  high-context response (~2500+ words, or multi-result), **can exceed 64 KB**.
  A `Scanner`-based framer would therefore break on exactly the inputs this proxy
  exists to serve.
- `bufio.Reader.ReadString('\n')` / `ReadBytes('\n')` have **no line-length limit**:
  they allocate a growing buffer and return the full line up to and including the
  delimiter (or the partial tail + `io.EOF` at end of stream). This is the correct
  primitive for SSE framing here.

**Decision:** `NewSSEReader` wraps the input in `bufio.NewReader(r)` and `Next()`
loops on `ReadString('\n')`. A dedicated test feeds a >64 KB `data:` line and
asserts it parses without error — that test exists *to prove this decision*.

`bufio.NewReader` default read buffer is 4096 bytes, but `ReadString`/`ReadBytes`
grow past it on demand (they append into a fresh `[]byte`/`strings.Builder`),
so arbitrary-length lines work without configuration.

---

## 3. WHATWG framing algorithm (PRD §8.10 + §12.1, verified vs the spec)

Authoritative algorithm: WHATWG "Parsing an event stream"
(https://html.spec.whatwg.org/multipage/server-sent-data.html#parsing-an-event-stream).
external_deps.md §3 confirms PRD §12.1 matches it. The rules `Next()` must encode:

1. **Line terminator.** `ReadString('\n')` splits on LF. CRLF is handled by
   trimming trailing `\r`/`\n` from each physical line (`strings.TrimRight(line,
   "\r\n")` is safe: a data VALUE never contains a raw LF on the wire — that LF
   would itself be a line terminator, i.e. the multi-line `data:` form).
2. **Comment lines.** A line beginning with `:` (U+003A) is a comment → ignored.
3. **Field/value split.** Split on the FIRST `:`. If no colon, the whole line is
   the field name with an empty value. Strip **exactly ONE** leading space from the
   value if present (`data: foo` → value `foo`; `data:  foo` → value ` foo`).
4. **`data` accumulation.** Append each `data:` value to a `[]string`. At dispatch,
   `Data = strings.Join(values, "\n")`. This is **bit-identical** to WHATWG's
   "append value + LF, then strip one trailing LF at dispatch" for ≥1 data lines
   (verified: Join(["a","b"],"\n")=="a\nb" == WHATWG "a\nb\n" minus one trailing LF).
5. **`id` / `event`.** `id:` sets the event ID; `event:` sets the event type. Other
   field names (`retry:`, unknown fields) are **ignored** (WHATWG).
6. **Blank line = dispatch + reset.** A blank line (empty after trim) dispatches the
   accumulated event **only if at least one `data:` line was seen** (WHATWG: an event
   with an empty data buffer is not dispatched). On dispatch, if `event` is "" set
   `Type = "message"` (the SSE default), else `Type = event`. Then reset id/event/data.
   Consecutive blank lines therefore produce **no** spurious empty events.
7. **EOF flush.** If the stream ends (`ReadString` returns `io.EOF`) with a pending
   event (≥1 accumulated `data:` line and no dispatching blank line), the final event
   MUST still be dispatched. The canonical fixtures always carry a trailing blank line
   (`0a 0a`), but real streams / truncated bodies may not, and PRD §12.1 requires the
   flush. Implementation: when `ReadString` returns `(partial, io.EOF)` with
   `partial != ""`, process the partial line first, then if a pending event exists,
   return it; the next `Next()` call returns `io.EOF`.

### Dispatch contract (the part that must be byte-stable for P1.M3.T3.S2)
- `Event.ID`  = the last `id:` value seen for the event ("" if none).
- `Event.Type` = the `event:` value, or `"message"` if no `event:` line.
- `Event.Data` = the `data:` values joined with `"\n"` (no trailing newline).

P1.M3.T3.S2 (injector) will re-emit an event by writing `id:`/`event:`/`data:`
fields and **splitting `Data` on `"\n"` back into multiple `data:` lines** (the
round-trip symmetry the multi-line fixture exists to prove). So the reader's
`Join("\n")` and the writer's `Split("\n")` must be exact inverses — keep `Data`
free of any extra trailing newline.

---

## 4. Reference implementation (the algorithm in Go — for the PRP's Patterns block)

```go
package main

import (
	"bufio"
	"io"
	"strings"
)

type Event struct {
	ID   string
	Type string // "message" when no event: line was seen
	Data string // data: values joined with "\n"
}

// Reader decodes an SSE stream. See NewSSEReader for the framing contract.
type Reader struct {
	r     *bufio.Reader
	id    string
	event string
	data  []string // accumulated data: values; len>0 means a pending event exists
}

// NewSSEReader returns a Reader that decodes a Server-Sent Events stream from r
// (typically an upstream *http.Response.Body) one Event at a time. ...
func NewSSEReader(r io.Reader) *Reader {
	return &Reader{r: bufio.NewReader(r)}
}

// Next decodes and returns the next Event. It returns io.EOF when the stream is
// fully consumed. Framing follows the WHATWG SSE algorithm (see package doc) ...
func (rd *Reader) Next() (Event, error) {
	for {
		line, err := rd.r.ReadString('\n')
		if line != "" { // a full line, or a partial final line before EOF
			line = strings.TrimRight(line, "\r\n")
			if line == "" { // blank line: dispatch + reset
				if len(rd.data) > 0 {
					return rd.dispatch(), nil
				}
				rd.reset()
				continue
			}
			if strings.HasPrefix(line, ":") { // comment
				continue
			}
			field, value := line, ""
			if i := strings.IndexByte(line, ':'); i >= 0 {
				field, value = line[:i], line[i+1:]
				if strings.HasPrefix(value, " ") {
					value = value[1:] // strip ONE leading space
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
				// unknown field (retry, etc.) -> ignore per WHATWG
			}
		}
		if err != nil {
			if err == io.EOF {
				if len(rd.data) > 0 { // flush final event at EOF
					return rd.dispatch(), nil
				}
				return Event{}, io.EOF
			}
			return Event{}, err // unexpected I/O error
		}
	}
}

func (rd *Reader) dispatch() Event {
	ev := Event{ID: rd.id, Type: rd.event, Data: strings.Join(rd.data, "\n")}
	if ev.Type == "" {
		ev.Type = "message"
	}
	rd.reset()
	return ev
}

func (rd *Reader) reset() {
	rd.id, rd.event, rd.data = "", "", nil
}
```

Notes the PRP must flag as gotchas:
- `strings.TrimRight(line, "\r\n")` removes **all** trailing `\r`/`\n` — correct here
  because `ReadString('\n')` stops at the first `\n`, so a physical line carries at
  most one `\n` (its terminator) and at most one preceding `\r` (CRLF). It does NOT
  eat data content because raw LF never appears inside a data value on the wire.
- Lone-CR line endings (pre-OSX Mac) are not split by `ReadString('\n')`. z.ai and
  HTTP SSE use LF or CRLF only, so this is acceptable; document it.
- `value[1:]` is safe after `strings.HasPrefix(value, " ")`; a value of exactly `" "`
  becomes `""` (no panic).
- `dispatch()` resets `data` to `nil` (not `[]string{}`) so `len(rd.data) > 0`
  correctly reads "no pending event" after a dispatch.
- Do **not** pre-read whole body into memory; the reader streams line-by-line so the
  passthrough path (P1.M4.T2.S2) can choose `io.Copy` and only the injection path
  pays for parsing (PRD §6 minimal-overhead).

---

## 5. The testdata fixtures this reader consumes (hard prerequisite; verbatim bytes)

From `P1M3T1S1/research/wire-format-and-fixtures.md` (the fixtures item):

- `testdata/initialize.sse` — `id:1` / `event:message` / one `data:{...}` line +
  trailing blank line. **Byte-identical** to `proxy_test.go`'s existing
  `const initSSE` (string-concatenated across 4 raw-string lines). Parsed →
  `Event{ID:"1", Type:"message", Data:<init JSON>}`.
- `testdata/tools_call.sse` — a single `data:{...}` line (NO `id:`/`event:` line) +
  trailing blank line; `result.content[0].text` is a stringified ARRAY. Parsed →
  `Event{ID:"", Type:"message", Data:<tools/call JSON>}` (Type defaults to message).
- `testdata/tools_call_multiline.sse` — the SAME tools/call message split across 8
  `data:` lines with **zero indentation**. Parsed → `Event{Data: <joined pretty
  JSON, valid>}`. Joining the 8 values with `"\n"` yields valid JSON; the round-trip
  (split `Data` on `"\n"` → reproduce the 8 lines) is the writer's symmetry test.

The exact `initialize.sse` Data value (== the `initSSE` data segment) is:
```
{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"tools":{"listChanged":true}},"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}
```

---

## 6. Test plan → PRD §19.2 + the item's three explicit requirements

`sse_test.go` (`package main`). Reuses the existing `proxy_test.go` `initSSE`
constant (same package — do NOT redeclare). Tests:

| # | Test | Input | Asserts | Source |
|---|------|-------|---------|--------|
| 1 | `TestSSEReader_DecodeInitializeFixture` | `testdata/initialize.sse` | `Event{ID:"1",Type:"message",Data==initSSE data}`; 2nd `Next()`→`io.EOF` | item req "decode initialize.sse" |
| 2 | `TestSSEReader_DecodeToolsCallFixture` | `testdata/tools_call.sse` | `ID==""`, `Type=="message"` (default), `Data` is valid JSON | §19.2 tools/call decode |
| 3 | `TestSSEReader_MultilineJoinFixture` | `testdata/tools_call_multiline.sse` | `json.Valid(Data)`; splitting `Data` on `"\n"` reproduces the 8 per-line values | item "multi-line round-trip" + §19.2 |
| 4 | `TestSSEReader_LargeLineOver64KB` | `strings.NewReader("data:"+repeat("x",70000)+"\n\n")` | `Next()` returns event, NO error; `len(Data)==70000` | item "large (>64KB) line parses" — proves bufio.Reader choice |
| 5 | `TestSSEReader_EOFFlushNoTrailingBlank` | `"data:hello"` (no trailing blank line) | `Next()`→`Event{Data:"hello"}`; then `io.EOF` | §12.1 "buffer a final event" |
| 6 | `TestSSFRaming_Table` | table of `strings.NewReader(...)` cases | comment ignored; one-leading-space stripped; default `Type=="message"`; explicit `event:` honored; `id:` honored; unknown field ignored; multi-data join `a\nb`; CRLF tolerated | §12.1 / WHATWG |
| 7 | `TestSSEReader_MultipleEvents` | `"data:a\n\ndata:b\n\n"` | two events then `io.EOF` | §8.10 dispatch+reset |
| 8 | (belt+suspenders) `TestSSEReader_InitializeFixtureEqualsInitSSE` | fixture bytes | `string(raw)==initSSE` | cross-checks the fixtures item's byte-identity gate |

Tests 4–7 are fully self-contained (`strings.NewReader`) and pass even if the
testdata fixtures are absent. Tests 1–3 + 8 require the fixtures (prerequisite).

---

## 7. Codebase conventions to follow (captured from existing files)

- `package main`, flat single-directory layout, **stdlib only** (`go.mod` has zero
  `require`s; module `web-search-prime-fixer`, go 1.22).
- Doc comments cite PRD sections and architecture findings inline (see `rewrite.go`,
  `proxy.go`). THIS item's doc must explain the WHATWG framing rules and the
  bufio.Reader-not-Scanner rationale (the item's Mode A docs requirement).
- Guard clauses first; unexported helpers with clear doc; in-place/no-surprise APIs.
- Tests: table-driven with `t.Run` subtests; `reflect.DeepEqual` for structs/maps;
  helper funcs (`cloneMap`, `fakeUpstream`, `newTestProxy`) at file top; comments
  reference the PRD section each case proves (see `rewrite_test.go`, `config_test.go`).
- `proxy_test.go` already defines `const initSSE` and `testSID` — reuse, do not
  redeclare (Go forbids duplicate package-level identifiers).

---

## 8. Consumer map (do not break these later)

- **P1.M3.T3.S2 (injector)**: `for { ev, err := rd.Next(); ... }`, `json.Unmarshal(
  []byte(ev.Data), ...)`, prepend a warning into `result.content`, re-emit by
  splitting `ev.Data` on `"\n"` into multiple `data:` lines (round-trip symmetry).
  Depends on `Event.ID/Type/Data` shape and the `Join("\n")`-no-trailing-newline rule.
- **P1.M4.T2.S2 (response path)**: passthrough = `io.Copy`; rewrite path = feed
  `resp.Body` to `NewSSEReader` then the injector. The reader must stream (no whole-
  body buffering) so the passthrough path stays zero-parse.

---

## 9. Validation approach

`go test ./...` (specifically `go test -run 'TestSSEReader|TestSSE' -v`). No service
to start (pure unit). Gates: (a) the >64KB test passes (proves bufio.Reader); (b)
EOF-flush test passes (proves §12.1 final-event buffering); (c) fixture decode tests
pass (proves the reader matches §8 wire format); (d) `go vet ./...` clean; (e) full
suite still green (no regression to config/health/logger/proxy/rewrite).

---

## 10. Spec references (cite in the PRP's docs block)
- WHATWG Server-sent events, "Parsing an event stream":
  https://html.spec.whatwg.org/multipage/server-sent-data.html#parsing-an-event-stream
- Go `bufio.Reader.ReadString` (unbounded lines): https://pkg.go.dev/bufio#Reader.ReadString
- Go `bufio.Scanner` 64 KB cap (`MaxScanTokenSize`): https://pkg.go.dev/bufio#Scanner
- VERIFIED on-disk GOROOT sources: `/usr/lib/go/src/bufio/scan.go` (64 KB const),
  `/usr/lib/go/src/bufio/bufio.go` (`ReadString` grows).
