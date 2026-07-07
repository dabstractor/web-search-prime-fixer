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
