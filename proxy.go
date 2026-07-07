package main

import (
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
		// NewRequestWithContext(r.Context(), ..., r.Body) streams r.Body to
		// upstream without buffering AND sets the context in one step (≡ the
		// contract's outReq.WithContext(r.Context())). cfg.Upstream is the full
		// absolute z.ai URL; the incoming path is NOT spliced in (PRD §9).
		outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.Upstream, r.Body)
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"bad upstream request"}`, http.StatusBadGateway)
			return
		}
		copyForwardHeaders(outReq.Header, r.Header)
		// Accept fallback (PRD §8): the text/event-stream token is REQUIRED or
		// z.ai returns empty. ONLY default when the client omitted Accept; a
		// client-provided Accept is passed through unmodified (PRD §11.2).
		if outReq.Header.Get("Accept") == "" {
			outReq.Header.Set("Accept", "application/json, text/event-stream")
		}
		forward(client, rw, outReq, log)
	}
}

// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.3 passthrough path). It is the REUSABLE FORWARD CORE: copy status +
// non-hop-by-hop response headers, io.Copy the body verbatim, flush for SSE.
//
// P1.M4.T2.S2 will EXTEND the body step: io.Copy (passthrough) UNLESS the
// request was rewritten, in which case the body is fed through the SSE injector
// keyed on reqID. The request has already been built by the caller (T4.S2
// streams r.Body; M4 passes rewritten bytes), so forward is body-source agnostic.
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
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
