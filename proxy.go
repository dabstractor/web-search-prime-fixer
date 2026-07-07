package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"
)

// hopHeaders is the exact stdlib hop-by-hop set, reproduced verbatim from
// net/http/httputil/reverseproxy.go:365 (external_deps.md §4). It is unexported
// in stdlib, so we own this copy. Stripped from BOTH the forwarded request and
// the copied upstream response. NOTE: this is the 9-ENTRY list (incl
// Proxy-Connection), a safe superset of PRD §11.2's 8-entry summary.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection", // non-standard but still sent by libcurl and rejected by e.g. google
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te", // canonicalized version of "TE"
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// isHopByHop reports whether name is a hop-by-hop header. Canonicalizes both
// sides so "TE"/"Te" match identically (http.Header already canonicalizes keys,
// but the request header map may carry non-canonical entries).
func isHopByHop(name string) bool {
	c := http.CanonicalHeaderKey(name)
	for _, h := range hopHeaders {
		if c == http.CanonicalHeaderKey(h) {
			return true
		}
	}
	return false
}

// newUpstreamClient builds the single *http.Client used to forward every request
// to cfg.Upstream (PRD §17). It clones http.DefaultTransport (sensible dial/TLS/
// h2 defaults: dial 30s, TLS 10s, ALPN h2, idle pooling) and sets
// Transport.ResponseHeaderTimeout to 30s so a dead upstream is detected quickly
// WITHOUT bounding the response-body read (verified: "does not include the time
// to read the response body").
//
// CRITICAL: http.Client.Timeout is left ZERO. A non-zero Timeout is a
// whole-exchange deadline that includes reading the body and would cut off long
// SSE streams (external_deps.md §5). Rely on per-request context cancellation.
func newUpstreamClient() *http.Client {
	// DefaultTransport is declared as RoundTripper; assert to *Transport to Clone.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.ResponseHeaderTimeout = 30 * time.Second
	return &http.Client{Transport: tr}
}

// copyForwardHeaders copies src's headers into dst EXCEPT the hop-by-hop set.
// Authorization, Content-Type, Accept, Mcp-Session-Id, Accept-Language,
// User-Agent and all other non-hop-by-hop headers pass through VERBATIM
// (PRD §11.2, §13). Authorization is forwarded unchanged and is never read here.
func copyForwardHeaders(dst, src http.Header) {
	for k, vs := range src {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// newProxyHandler returns the HTTP handler that transparently forwards every
// non-/healthz request to cfg.Upstream (PRD §9, §11.2). The client body streams
// through UNCHANGED (no buffering — PRD §6); headers are copied minus
// hop-by-hop; the Accept fallback is applied; the client context propagates
// upstream (disconnect cancellation). forward() writes the response.
//
// P1.M4.T1 EXTENDS this factory: it will read the body into bytes, detect
// tools/call, apply the alias rewrite, re-serialize, and set reqID — replacing
// the streamed r.Body with rewritten bytes and tracking ids. P1.M4.T2.S2
// EXTENDS forward() with a conditional SSE-injection response path.
func newProxyHandler(cfg Config, log *logger, client *http.Client) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		// PRD §11.1 point 1: read the FULL request body (MCP requests are small
		// JSON-RPC objects). This ends streaming on the REQUEST side; the RESPONSE
		// side stays streamed (forward io.Copy / Inject).
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"read body"}`, http.StatusBadRequest)
			return
		}
		// Decide the rewrite (pure). dec.body is the original bytes when nothing
		// changed (zero drift) or the re-serialized bytes when an alias was renamed.
		dec := decideRewrite(body, cfg)
		// P1.M4.T3.S1 (PRD §15): log the rewrite whenever FR-2 changed a call. Fires
		// on the REQUEST decision (before forward), so it logs even if the upstream
		// later errors or the SSE result id doesn't match. Carries NO headers, so
		// Authorization can never appear (PRD §13; redactHeaders is the documented
		// tool if a future field adds header context).
		if !dec.streamThrough {
			log.log("info", "rewrite", rewriteLogFields(dec))
		}

		// bytes.NewReader lets net/http set ContentLength (and GetBody for safe
		// retries) from the actual forwarded length — even after re-serialization
		// changed the byte count. (Verified: Transport writes Content-Length from
		// outReq.ContentLength, ignoring a stale copied header.)
		//
		// METHOD PRESERVED (PRD §11.2): forward the ORIGINAL request method, not a
		// hardcoded POST. MCP Streamable HTTP permits a client GET to open the
		// server-initiated notification SSE stream; converting it to POST would
		// prevent that channel from opening. POST JSON-RPC (the common path for
		// pi/Claude Code/Cursor) still works exactly as before because the client
		// method is POST. A GET carries no body, so bytes.NewReader(dec.body)
		// (typically empty for a GET) is the right source.
		outReq, err := http.NewRequestWithContext(r.Context(), r.Method, cfg.Upstream, bytes.NewReader(dec.body))
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"bad upstream request"}`, http.StatusBadGateway)
			return
		}
		copyForwardHeaders(outReq.Header, r.Header)
		// The copied Content-Length reflects the ORIGINAL (pre-rewrite) length;
		// after re-serialization it may be stale. Transport writes the correct
		// length from outReq.ContentLength (set above via bytes.NewReader), so
		// drop the stale copy.
		outReq.Header.Del("Content-Length")
		// Accept fallback (PRD §8): the text/event-stream token is REQUIRED or
		// z.ai returns empty. ONLY default when the client omitted Accept; a
		// client-provided Accept is passed through unmodified (PRD §11.2).
		if outReq.Header.Get("Accept") == "" {
			outReq.Header.Set("Accept", "application/json, text/event-stream")
		}
		// dec.body is already set as outReq.Body; dec.rewrittenIDs + dec.warning
		// are threaded for P1.M4.T2.S2 (response-side io.Copy-vs-Inject branch).
		forward(client, rw, outReq, dec, log)
		// P1.M4.T3.S1 (PRD §15): debug per-request forward line — JSON-RPC method,
		// id, and whether the response was streamed (io.Copy) or injected (SSE
		// Inject). Suppressed below debug by the logger's level filter (PRD §15
		// "Levels honored"; do NOT add a second filter). mode reflects the PATH
		// decision (dec.streamThrough) — exactly what forward() branched on.
		log.log("debug", "forward", map[string]any{
			"method": dec.method, // JSON-RPC method ("" for scalar/unparseable)
			"id":     dec.reqID,  // float64|string|nil
			"mode":   forwardMode(dec.streamThrough),
		})
	}
}

// flushWriter writes to w and, when f is non-nil, flushes after each successful
// Write. forward wraps the response writer with it for SSE responses so streamed
// events are not buffered (PRD §11.3). Because sse.emitEvent writes one whole event
// per io.WriteString, one Write here == one emitted event == one Flush. Flush adds no
// bytes, so wrapping the io.Copy passthrough path is byte-safe
// (TestForward_PassthroughByteEqual). f is nil for non-SSE or non-Flusher writers
// -> pass-through.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err == nil && fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}

// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.2/§11.3). It is the REUSABLE FORWARD CORE: copy status + non-hop-by-hop
// response headers, write the body, flush for SSE.
//
// RESPONSE BODY (PRD §11.3, P1.M4.T2.S2): after the status + headers are written,
// the body step branches on the rewrite decision:
//   - dec.streamThrough == true  -> io.Copy(rw, resp.Body) VERBATIM (zero parse, zero
//     buffering — PRD §6; the common case). No warning is injected.
//   - dec.streamThrough == false -> Inject(rw, resp.Body, dec.rewrittenIDs,
//     dec.warning) (sse.go, P1.M3.T3.S2): the upstream SSE body is decoded event by
//     event, and for each event whose JSON-RPC id is in dec.rewrittenIDs one text
//     warning block is prepended into result.content; every other event is re-emitted
//     unchanged. isError is never touched (FR-3).
//
// SSE FLUSH (PRD §11.3 "Flush after writing when the content type is SSE"): when
// the upstream Content-Type contains "text/event-stream", the writer is wrapped in
// a flushWriter so each Write flushes immediately — one Flush per emitted event on
// the Inject path, one per read chunk on the io.Copy path — so streamed events are
// not buffered. A trailing http.Flusher.Flush() covers any residual buffered bytes.
// On the rewrite path the upstream's Content-Length is dropped from the copied
// headers because Inject re-serializes the body and changes its byte count
// (mirrors the request-side Content-Length handling in newProxyHandler).
//
// NON-2xx (P1.M4.T2.S1, PRD §15): a non-2xx upstream status is logged at warn as
// upstream_error {status, req_id} but STILL copied through (status + headers + body);
// the proxy never synthesizes a 502 for an HTTP error status. dec.reqID names the
// request in that log. dec.body was set as outReq.Body by the caller (newProxyHandler).
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, dec rewriteDecision, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	// NON-2xx: log upstream_error {status, req_id} but DO NOT synthesize — copy
	// the response through below (PRD §15/§11.3). req_id may be nil (notification/
	// scalar/unparseable body) -> logged as JSON null, which is acceptable.
	if resp.StatusCode >= 300 {
		log.log("warn", "upstream_error", map[string]any{
			"status": resp.StatusCode,
			"req_id": dec.reqID,
		})
	}
	// isSSE keys on the ACTUAL RESPONSE media type, not on the rewrite decision,
	// so a non-SSE body (e.g. a 401 JSON error returned for an aliased tools/call)
	// is copied verbatim and never fed to the SSE injector — which would otherwise
	// decode it as an empty stream and drop it (FR-1/FR-3).
	isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	// injecting is true ONLY on the rewrite path AND for an SSE response: that is
	// the single case where the body is re-serialized by Inject and its byte count
	// (hence Content-Length) changes. Every other combination copies the body
	// verbatim, so the upstream Content-Length stays accurate and is preserved
	// (dropping it on a JSON error body can confuse a length-reading client).
	injecting := !dec.streamThrough && isSSE
	// Copy non-hop-by-hop response headers BEFORE WriteHeader (WriteHeader locks
	// the header map). Content-Type, Mcp-Session-Id, Cache-Control, Vary, X-Log-Id
	// pass through; Transfer-Encoding/Connection etc. are stripped (hop-by-hop).
	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		if injecting && http.CanonicalHeaderKey(k) == "Content-Length" {
			continue
		}
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)

	// P1.M4.T2.S2: choose the writer. Wrap with flushWriter for SSE so each Write
	// flushes (per emitted event on Inject; per read chunk on io.Copy). Flush adds
	// no bytes, so the passthrough path stays byte-for-byte identical (PRD §11.3/§6).
	var w io.Writer = rw
	if isSSE {
		if f, ok := rw.(http.Flusher); ok {
			w = flushWriter{w: rw, f: f}
		}
	}
	if dec.streamThrough || !isSSE {
		// Passthrough: stream the upstream body unchanged, no warning (PRD §11.3/§6).
		// This branch ALSO covers the rewrite path when the response is not SSE —
		// e.g. an upstream error body (401/429/5xx) returned as JSON, or a 200 JSON
		// result. The teaching warning is only meaningful INSIDE an SSE result, so
		// nothing is lost by copying such a body verbatim (FR-1: "transparently
		// forwards ... and streams the response back"; FR-3).
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
		}
	} else {
		// Rewrite path with an SSE response: feed the upstream body through the SSE
		// injector keyed on the rewritten request ids; matching tools/call results
		// get the warning prepended (PRD §11.3/§12). Non-matching events pass
		// through unchanged.
		if err := Inject(w, resp.Body, dec.rewrittenIDs, dec.warning); err != nil {
			log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
		}
	}
	// Final flush for any residual buffered bytes (non-SSE / non-Flusher writers).
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
}

// rewriteDecision is the outcome of inspecting a request body for the alias
// rewrite (PRD §11.1). It is produced by decideRewrite and consumed by the
// forward path (P1.M4.T2.S1 — the body to POST) and the response path
// (P1.M4.T2.S2 — the rewritten ids + warning that select SSE injection).
type rewriteDecision struct {
	// body is the request body to forward upstream: the ORIGINAL input bytes when
	// streamThrough is true (no rewrite — zero formatting drift, PRD §11.1 point 4),
	// otherwise the re-serialized bytes carrying the renamed args.
	body []byte
	// streamThrough is true when NO tools/call argument was rewritten. When true,
	// body is the original bytes verbatim and the response is io.Copy'd unchanged
	// (P1.M4.T2.S2). When false, the response is run through the SSE injector keyed
	// on rewrittenIDs.
	streamThrough bool
	// rewrittenIDs is the set of JSON-RPC request ids whose tools/call arguments
	// were rewritten. Keys are the values encoding/json produced when decoding the
	// request id into any: float64 for numeric ids, string for string ids. This is
	// exactly what Inject (sse.go) finds when it decodes the RESPONSE id the same
	// way (see "id contract" on decideRewrite). Empty/nil when streamThrough.
	rewrittenIDs map[any]bool
	// warning is the formatted single-line warning (warningText, sse.go) to prepend
	// into each rewritten tools/call RESULT. Empty when streamThrough.
	warning string
	// reqID is the primary JSON-RPC request id of this request, captured for the
	// upstream_error log on non-2xx responses (PRD §15). It is the decoded value
	// (float64 for numeric ids, string for string ids, nil when the body is a
	// notification / scalar / unparseable). It is ADDITIVE: it does not affect the
	// rewrite/streamThrough/rewrittenIDs/warning behavior (P1.M4.T2.S1).
	reqID any
	// --- P1.M4.T3.S1 (observability, PRD §15) — additive; zero existing readers break ---
	// method is the JSON-RPC method (initialize/tools/call/tools/list/...), captured
	// for the `forward` debug log line. "" for a scalar / unparseable body. It is
	// ADDITIVE: it does not affect request/response mechanics.
	method string
	// tool is params.name of the rewritten tools/call (the `rewrite` line's "tool"
	// field). "" when absent/non-string. Set on the rewrite path only.
	tool string
	// notes is the per-alias note array (the `rewrite` line's "notes" field). It is
	// the SAME slice warningText joins into `warning`, so the log and the injected
	// SSE warning cannot drift. Set on the rewrite path only.
	notes []string
	// present is the per-configured-alias presence map (alias -> was-in-arguments),
	// captured BEFORE Rewrite mutated args (Rewrite deletes the renamed + dropped
	// aliases, so presence cannot be recomputed from dec.body). Set on the rewrite
	// path only.
	present map[string]bool
}

// decideRewrite inspects a JSON-RPC request body and, for every tools/call whose
// params.arguments contains a configured alias, applies the alias->target rename
// in place via Rewrite (rewrite.go). It returns the bytes to forward upstream and
// the per-request state the response path needs (rewritten ids + warning)
// (PRD §11.1). decideRewrite is PURE (no I/O) and is unit-tested in isolation.
//
// RULES:
//   - ORIGINAL-BYTES PASSTHROUGH (PRD §11.1 point 4): when NO object changed, the
//     returned body is the ORIGINAL input bytes — no re-serialization — so a request
//     that needs no fixup is forwarded byte-for-byte and cannot drift in formatting
//     (key order, whitespace, HTML-escaping). Only a request that was actually
//     rewritten is re-serialized.
//   - ARRAY HANDLING (PRD §11.1 point 2): MCP does not use batches, but if the body
//     decodes to a JSON ARRAY, each element is inspected independently and, if any
//     element changed, the whole body is re-serialized as an array. A non-array,
//     non-object body (or unparseable JSON) is passed through UNCHANGED rather than
//     rejected — the proxy never blocks a request on a parse quirk.
//   - ID TRACKING: a rewritten object's JSON-RPC id is added to rewrittenIDs using
//     the decoded value directly (float64 for numbers — see the id contract below).
//     This is the correlation key the SSE injector matches against in the response
//     (sse.go Inject, P1.M3.T3.S2).
//   - reqID (P1.M4.T2.S1, PRD §15): the FIRST JSON-RPC id seen while iterating is
//     captured into rewriteDecision.reqID purely for the upstream_error log on a
//     non-2xx response. It is ADDITIVE — it does not alter streamThrough/
//     rewrittenIDs/warning/body (the log just needs ONE id to name the request).
//
// id contract: numeric JSON-RPC ids decode to float64 in any (id:2 -> float64(2));
// rewrittenIDs is therefore keyed by float64(2), which is exactly what Inject finds
// when it decodes the response id the same way. String ids decode to string and
// match identically. Never build rewrittenIDs from an int key —
// map[any]bool{int(2):true} would NOT match float64(2) (verified).
//
// PRECISION (validation ISSUE 3): decoding into any maps EVERY JSON number to
// float64, which silently corrupts integer ids whose magnitude exceeds 2^53
// (e.g. 12345678901234567 -> 12345678901234568 on the re-serialized rewrite
// path). To preserve full precision, decode with json.Decoder.UseNumber(): numeric
// values then become json.Number (a string-backed type) whose original digits are
// preserved through re-serialization. Inject decodes the response id the SAME way
// (UseNumber), so json.Number matches json.Number by string equality. reqID is
// therefore a json.Number for numeric ids (string for string ids, nil otherwise);
// it still marshals to the correct JSON number in logs.
func decideRewrite(body []byte, cfg Config) rewriteDecision {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber() // preserve full numeric precision (validation ISSUE 3)
	var parsed any
	if err := dec.Decode(&parsed); err != nil {
		// Not valid JSON -> forward unchanged (never reject on a parse quirk).
		return rewriteDecision{body: body, streamThrough: true}
	}

	var rewrittenIDs map[any]bool
	var notes []string
	// addID records a rewritten object's id + notes. The id is the DECODED value
	// (json.Number for numbers under UseNumber) — the key Inject matches on the
	// response side (which also decodes with UseNumber, so the types agree).
	addID := func(id any, n []string) {
		if rewrittenIDs == nil {
			rewrittenIDs = make(map[any]bool)
		}
		rewrittenIDs[id] = true
		notes = append(notes, n...)
	}

	// reqID is the first JSON-RPC id seen while iterating, threaded for the
	// upstream_error log on non-2xx responses (P1.M4.T2.S1, PRD §15). It stays nil
	// for a notification / scalar / unparseable body.
	var reqID any
	// P1.M4.T3.S1 (PRD §15): additive observability fields captured alongside
	// reqID. `method` is captured on every valid object (for the debug `forward`
	// line); tool/present/notes are taken from the first CHANGED object (for the
	// info `rewrite` line) — batch is defensive, so first-changed wins.
	var (
		method  string
		tool    string
		present map[string]bool
	)

	switch v := parsed.(type) {
	case map[string]any:
		reqID = v["id"] // capture the (single) request's id for logging
		if m, ok := v["method"].(string); ok {
			method = m
		}
		if rr := rewriteObject(v, cfg); rr.changed {
			addID(v["id"], rr.notes)
			tool = rr.tool
			present = rr.present
		}
	case []any:
		// Defensive JSON-RPC batch (MCP does not use batches): process each element.
		for _, elem := range v {
			if obj, ok := elem.(map[string]any); ok {
				if reqID == nil { // first element's id wins (batch is defensive)
					reqID = obj["id"]
				}
				if method == "" {
					if m, ok := obj["method"].(string); ok {
						method = m
					}
				}
				if rr := rewriteObject(obj, cfg); rr.changed {
					addID(obj["id"], rr.notes)
					if tool == "" { // first changed object's tool/present win (batch is defensive)
						tool = rr.tool
						present = rr.present
					}
				}
			}
		}
	default:
		// Valid JSON but a scalar (number/string/null/bool) -> pass through.
		return rewriteDecision{body: body, streamThrough: true}
	}

	if len(rewrittenIDs) == 0 {
		// Nothing changed -> forward ORIGINAL bytes (no re-serialization). reqID +
		// method are still threaded so a non-2xx can name the request in the log
		// and the debug `forward` line reports the method. tool/notes/present stay
		// zero-value (the rewrite line won't fire).
		return rewriteDecision{body: body, streamThrough: true, reqID: reqID, method: method}
	}

	// Something changed -> re-serialize the (possibly array) body. json.Marshal
	// sorts keys and HTML-escapes <>&, both semantically harmless for JSON-RPC
	// (z.ai parses the value, not the bytes); the original-bytes path above is
	// what guards formatting fidelity for the common unchanged case.
	out, err := json.Marshal(parsed)
	if err != nil {
		// Cannot happen for a value we just decoded; defensive: pass through.
		return rewriteDecision{body: body, streamThrough: true}
	}
	return rewriteDecision{
		body:          out,
		streamThrough: false,
		rewrittenIDs:  rewrittenIDs,
		warning:       warningText(notes),
		reqID:         reqID,
		method:        method,
		tool:          tool,
		notes:         notes, // the SAME slice warningText joined — authoritative
		present:       present,
	}
}

// rewriteObject applies the alias rewrite to a single JSON-RPC object IN PLACE
// and returns what happened plus the observability fields the `rewrite` log needs
// (P1.M4.T3.S1, PRD §15). A non-tools/call method, or a tools/call with absent/
// non-object params.arguments, is left untouched and returns a zero-value result.
//
// PRE-MUTATION CAPTURE (P1.M4.T3.S1): `present` (per-configured-alias presence)
// and `tool` (params.name) are read from args/params BEFORE Rewrite mutates args —
// Rewrite deletes the renamed + dropped aliases, so presence cannot be recomputed
// from the re-serialized body afterwards. On the unchanged branch present/tool are
// nil'd out so an unchanged request carries no stale observability data (the
// rewrite line won't fire anyway).
func rewriteObject(obj map[string]any, cfg Config) rewriteObjectResult {
	var res rewriteObjectResult
	if obj["method"] != "tools/call" {
		return res
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return res
	}
	if name, ok := params["name"].(string); ok {
		res.tool = name
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		return res
	}
	// Capture presence BEFORE Rewrite mutates args (Rewrite deletes/renames aliases).
	res.present = aliasPresence(args, cfg.Aliases)
	if rr := Rewrite(args, cfg.Aliases, cfg.TargetParam); rr.Changed {
		res.changed = true
		res.notes = rr.Notes
	} else {
		// Nothing changed: drop the captured present/tool so an unchanged request
		// does not carry stale observability data (the rewrite line won't fire).
		res.present = nil
		res.tool = ""
	}
	return res
}

// aliasPresence reports, for each configured alias (in config order), whether it
// was a key in args. It iterates cfg.Aliases (NOT args) so the output is stable
// across runs and covers every configured alias — mirroring rewrite.go's "NEVER
// range over args" discipline (Go map iteration is randomized). Captured
// pre-mutation by rewriteObject (PRD §15 presence flags).
func aliasPresence(args map[string]any, aliases []string) map[string]bool {
	p := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		_, p[a] = args[a]
	}
	return p
}

// rewriteObjectResult is the widened return of rewriteObject (P1.M4.T3.S1): the
// single tools/call parse point now also yields the per-alias note array, the
// tool name, and the pre-mutation presence map that the `rewrite` log line needs.
// `changed` mirrors the former non-nil []string return (nil == unchanged).
type rewriteObjectResult struct {
	changed bool
	notes   []string
	tool    string
	present map[string]bool // per-cfg.Aliases presence, captured PRE-mutation
}

// rewriteLogFields builds the `rewrite` event payload from a rewrite decision
// (P1.M4.T3.S1, PRD §15): req_id, tool (if present), notes (the array), and the
// per-alias presence map. Pulled out so the handler reads as one log.log call and
// the field set is testable in isolation. NEVER include header values here — the
// rewrite event carries no headers, so Authorization can never appear (PRD §13).
// If a future field adds header context, route it through redactHeaders first.
func rewriteLogFields(dec rewriteDecision) map[string]any {
	return map[string]any{
		"req_id":  dec.reqID,   // float64|string|nil — json.Marshal renders nil as null
		"tool":    dec.tool,    // "" renders as "" (PRD: "tool (if present)")
		"notes":   dec.notes,   // []string -> JSON array
		"present": dec.present, // map[string]bool -> {"alias":bool,...}
	}
}

// forwardMode renders the streamed-vs-injected label for the debug `forward` log
// line (PRD §15). "streamed" when no argument was rewritten (forward io.Copy'd
// the body verbatim); "injected" when a tools/call argument was rewritten
// (forward ran sse.Inject). Reflects the path decision (dec.streamThrough).
func forwardMode(streamThrough bool) string {
	if streamThrough {
		return "streamed"
	}
	return "injected"
}
