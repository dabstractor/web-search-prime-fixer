package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
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

		// bytes.NewReader lets net/http set ContentLength (and GetBody for safe
		// retries) from the actual forwarded length — even after re-serialization
		// changed the byte count. (Verified: Transport writes Content-Length from
		// outReq.ContentLength, ignoring a stale copied header.)
		outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.Upstream, bytes.NewReader(dec.body))
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
	}
}

// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.2/§11.3). It is the REUSABLE FORWARD CORE: copy status + non-hop-by-hop
// response headers, stream the body, flush for SSE.
//
// NON-2xx (P1.M4.T2.S1, PRD §15): when the upstream returns a non-2xx status, an
// upstream_error line {status, req_id} is logged at warn — but the response is
// STILL copied through verbatim (status + headers + body). The proxy NEVER
// synthesizes a 502 for an upstream HTTP error status; synthesis happens only on
// a transport failure (client.Do error) below.
//
// dec carries the rewrite decision. dec.body is already set as outReq.Body by the
// caller (newProxyHandler); dec.reqID is used for the upstream_error log here;
// dec.streamThrough/rewrittenIDs/warning select the RESPONSE body path.
// P1.M4.T2.S2 EXTENDS the body step: io.Copy (passthrough) when
// dec.streamThrough, otherwise feed the body through the SSE injector (sse.go
// Inject) keyed on dec.rewrittenIDs with dec.warning. Until then the body is
// streamed through unchanged.
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
	// Copy non-hop-by-hop response headers BEFORE WriteHeader (WriteHeader locks
	// the header map). Content-Type, Mcp-Session-Id, Cache-Control, Vary, X-Log-Id
	// pass through; Transfer-Encoding/Connection etc. are stripped (hop-by-hop).
	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	// io.Copy streams the body without whole-body buffering (PRD §11.3/§6). A
	// copy error here (client disconnect mid-stream) is logged at warn, not fatal.
	if _, err := io.Copy(rw, resp.Body); err != nil {
		log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
	}
	// Flush so SSE events reach the client immediately (external_deps.md §2).
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
func decideRewrite(body []byte, cfg Config) rewriteDecision {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Not valid JSON -> forward unchanged (never reject on a parse quirk).
		return rewriteDecision{body: body, streamThrough: true}
	}

	var rewrittenIDs map[any]bool
	var notes []string
	// addID records a rewritten object's id + notes. The id is the DECODED value
	// (float64 for numbers) — the key Inject matches on the response side.
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

	switch v := parsed.(type) {
	case map[string]any:
		reqID = v["id"] // capture the (single) request's id for logging
		if n := rewriteObject(v, cfg); n != nil {
			addID(v["id"], n)
		}
	case []any:
		// Defensive JSON-RPC batch (MCP does not use batches): process each element.
		for _, elem := range v {
			if obj, ok := elem.(map[string]any); ok {
				if reqID == nil { // first element's id wins (batch is defensive)
					reqID = obj["id"]
				}
				if n := rewriteObject(obj, cfg); n != nil {
					addID(obj["id"], n)
				}
			}
		}
	default:
		// Valid JSON but a scalar (number/string/null/bool) -> pass through.
		return rewriteDecision{body: body, streamThrough: true}
	}

	if len(rewrittenIDs) == 0 {
		// Nothing changed -> forward ORIGINAL bytes (no re-serialization). reqID is
		// still threaded so a non-2xx can name the request in the log.
		return rewriteDecision{body: body, streamThrough: true, reqID: reqID}
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
	}
}

// rewriteObject applies the alias rewrite to a single JSON-RPC object IN PLACE.
// It returns the RewriteResult.Notes (nil when nothing changed) so the caller can
// collect the object's id and join notes into the warning. A non-tools/call
// method, or a tools/call with absent/non-object params.arguments, is left
// untouched (PRD §11.1 point 3; §3 — only configured aliases are ever moved).
func rewriteObject(obj map[string]any, cfg Config) []string {
	if obj["method"] != "tools/call" {
		return nil
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return nil
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		return nil
	}
	if res := Rewrite(args, cfg.Aliases, cfg.TargetParam); res.Changed {
		return res.Notes
	}
	return nil
}
