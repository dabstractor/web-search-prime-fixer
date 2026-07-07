package main

import (
	"bufio"
	"encoding/json"
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
//
// COMMENT (validation ISSUE 2): a WHATWG comment line (one beginning with ':')
// is surfaced as an Event with Comment set to the raw line text (without its
// trailing newline) and every other field zero. emitEvent re-emits a comment
// event VERBATIM followed by a terminating newline, so heartbeat comments
// (":keepalive") survive the rewrite path instead of being dropped — a dropped
// heartbeat on a long aliased stream could let an intermediary time the
// connection out. Comment events are produced ONLY by the reader; Inject never
// synthesizes one.
type Event struct {
	ID      string
	Type    string
	Data    string
	Comment string // raw comment line (incl. leading ':') when this event is a comment; else ""
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
				// Surface the comment as its own event so the writer can re-emit it
				// verbatim in stream order (validation ISSUE 2: heartbeats must survive
				// the rewrite path). A comment carries no data/id/event, so dispatch
				// does not flush any accumulated event here — the next blank line does.
				return Event{Comment: line}, nil
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
//
// ID MATCHING: decodes with json.Decoder.UseNumber() so numeric ids are
// json.Number (string-backed), matching exactly how decideRewrite keyed
// rewrittenIDs. The two MUST use the same number decoding strategy, otherwise a
// numeric request id (json.Number on the request side) would never match a
// float64 response id (validation ISSUE 3 fix).
func injectData(data string, rewrittenIDs map[any]bool, warning string) string {
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(data))
	dec.UseNumber() // match decideRewrite's numeric decoding (json.Number)
	if err := dec.Decode(&obj); err != nil {
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
//
// COMMENT events (Event.Comment != "") are written VERBATIM: the raw comment
// line (including its leading ':') followed by a single newline, and NO blank
// line (WHATWG comments are not events and need no dispatch terminator). This
// preserves heartbeat lines on the rewrite path (validation ISSUE 2).
func emitEvent(w io.Writer, ev Event) error {
	if ev.Comment != "" {
		_, err := io.WriteString(w, ev.Comment+"\n")
		return err
	}
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
