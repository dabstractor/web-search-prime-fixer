# P1.M4.T2.S2 research — conditional response: io.Copy passthrough vs Inject + flush

RESEARCH ONLY. Verifies the seam T2.S1 leaves in `forward`, the `Inject` contract
(sse.go), and the `http.Flusher` streaming rules, so the conditional response path
can be implemented in one pass.

## 1. The seam T2.S1 leaves (contract — treat as already landed)

`proxy.go` `forward` after P1.M4.T2.S1 (and the parallel T2.S1) is, verbatim:

```go
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, dec rewriteDecision, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {                                  // T2.S1 (non-2xx log, copy-through)
		log.log("warn", "upstream_error", map[string]any{"status": resp.StatusCode, "req_id": dec.reqID})
	}
	for k, vs := range resp.Header {                             // copy non-hop-by-hop resp headers
		if isHopByHop(k) { continue }
		for _, v := range vs { rw.Header().Add(k, v) }
	}
	rw.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(rw, resp.Body); err != nil {           // <-- THE SEAM: replace this block
		log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
	}
	if f, ok := rw.(http.Flusher); ok { f.Flush() }             // <-- and this final flush
}
```

T2.S1's explicit deliverable note (from its PRP CONSUMER SEAM): "T2.S2 replaces the
body step with `if len(dec.rewrittenIDs) > 0 { Inject(rw, resp.Body, dec.rewrittenIDs,
dec.warning) } else { io.Copy(rw, resp.Body) }` then Flush." T2.S1 OWNS everything
ABOVE the seam (Do error, non-2xx log, header copy, WriteHeader). THIS item owns
ONLY the body step + flush. Do NOT touch anything above `rw.WriteHeader`.

NOTE on the gating predicate: `dec.streamThrough` (bool) and `len(dec.rewrittenIDs) == 0`
are EQUIVALENT — `decideRewrite` sets `streamThrough=true` iff no id was rewritten
(proxy.go, verified). Use `dec.streamThrough` (the field that exists for exactly this
decision and is documented on the struct); it reads clearer than re-deriving from the
map length and is robust if a future caller pre-populates `rewrittenIDs`.

## 2. Inject contract (sse.go — DONE, verified on disk)

`func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error`

- Loops `NewSSEReader(body).Next()`; for each Event, `injectData` either prepends the
  warning into `result.content` (when `obj["id"]` ∈ rewrittenIDs AND result.content is
  `[]any`) or returns Data UNCHANGED; then `emitEvent(w, ev)` writes ONE `io.WriteString`
  per whole event (`id:`?`event:`?`data:`…``\n`). So **one Write call == one emitted
  event**. That fact is what makes "Flush after each emitted event" cheap to implement:
  flush after each `Write` on `w`.
- Returns `nil` at clean EOF, the reader's non-EOF error, or a write error. It NEVER
  touches `isError` and NEVER fails on a parse error (re-emits unchanged).
- **CRITICAL (doc comment on Inject): "It does not flush w (the caller owns
  http.Flusher — see P1.M4.T2.S2)."** So flushing is THIS item's job, done by wrapping
  `w`. Keeping Inject decoupled from `http.Flusher` is what lets it be unit-tested with a
  `*strings.Builder` (do NOT re-add a Flush parameter to Inject).

`dec.rewrittenIDs` keys are `float64(N)` for numeric ids, `string` for string ids
(`decideRewrite`/`encoding/json` contract). `dec.warning` is the already-formatted line
from `warningText` (non-empty when streamThrough is false).

## 3. http.Flusher + the flushWriter design (VERIFIED, GOROOT)

`net/http` `type Flusher interface { Flush() }` (verified `/usr/lib/go/src/net/http/
server.go`; external_deps.md §2): "Handlers should always test for this ability at
runtime" → `if f, ok := rw.(http.Flusher); ok`. `httptest.ResponseRecorder` does NOT
implement Flusher (it buffers); `httptest.NewServer`'s real handler DOES.

A write-through flusher wraps `w` so every `Write` flushes (== one Flush per emitted
event on the Inject path, one Flush per io.Copy chunk on the passthrough path):

```go
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
```

This lives in **proxy.go** (http.Flusher-coupled), NOT sse.go (which must stay
Flusher-free / strings.Builder-testable). Unexported; only `forward` uses it.

## 4. SSE detection + the symmetric gating (contract-faithful)

PRD §11.3: "Flush after writing when the content type is SSE." The item contract:
"Detect SSE via Content-Type contains 'text/event-stream'; if so … Flush() after each
emitted event." →

`isSSE := strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")`
(Content-Type is non-hop-by-hop, so it is ALREADY copied into `rw.Header()` by T2.S1
before WriteHeader; reading it from `resp.Header` is equivalent and avoids touching rw).

Design decision: when `isSSE`, wrap `w` (both paths) with the flushWriter; when not SSE,
write straight to `rw`. This is symmetric, gives true streaming on BOTH the Inject path
(per-event) and the SSE-passthrough io.Copy path (per-chunk), and — because Flush adds no
bytes — preserves the **byte-for-byte** equality the passthrough MOCKING requires. The
gating on isSSE (not on "is it the Inject path") is the literal contract ("when the
content type is SSE") and avoids flushing plain-JSON responses needlessly.

Edge (won't occur with z.ai, untested): a rewritten tools/call whose response is NOT SSE
would be fed to Inject and mis-framed. Acceptable: streamThrough=false only happens for a
rewritten tools/call, and z.ai tools/call responses ARE SSE (PRD §8). The isSSE gate only
controls flushing, not the Inject-vs-io.Copy choice (that choice is `dec.streamThrough`).

## 5. The two load-bearing e2e assertions (item MOCKING; PRD §19.3 cases 1 & 2)

- **PASSTHROUGH byte-equal (case 2):** client sends canonical `{"search_query":...}` →
  `dec.streamThrough=true` → `io.Copy`. Client body MUST be byte-identical to the
  upstream payload (no injected block). `testdata/tools_call.sse` (id:2) is a fine
  upstream payload: it must round-trip verbatim. (Flush adds no bytes → safe.)
- **REWRITE + warning first (case 1):** client sends `{"query":...}` (alias) with
  JSON-RPC `id:2` → upstream receives `search_query`; upstream returns
  `testdata/tools_call.sse` (id:2); `dec.streamThrough=false`,
  `dec.rewrittenIDs={float64(2):true}`. Client's SSE event: decode → `result.content[0]`
  is the warning block `{type:text, text:warningText}`, `content[1].text` is the ORIGINAL
  stringified array (byte-identical), `isError==false`. Assert content[0] starts with
  `[web-search-prime-fixer]` (marker) and content[1].text equals the pre-inject text.

Plus: assert the UPSTREAM received `search_query` (not `query`) in case 1 (request side
already correct from T1.S1; this verifies the response side keys on the right id).

## 6. What is NOT in scope

- Request-side rewrite (T1.S1 — DONE), warningText (M3.T3.S1 — DONE), Inject (M3.T3.S2 —
  DONE), forward mechanics + non-2xx log + reqID (T2.S1 — parallel). DO NOT touch them.
- `debug forward` / `rewrite` log events (T3.S1 — future). The single warn-on-copy-error
  log stays as T4.S2 left it; this item keeps it on BOTH paths.
- No new imports beyond what proxy.go already has after T2.S1: `io`, `net/http`,
  `bytes`, `encoding/json`, `time` (already present) + **add `strings`** (for
  `strings.Contains` on Content-Type). proxy_test.go already imports `strings`. Verify
  `strings` is not already imported by proxy.go before adding (it currently is NOT —
  proxy.go imports bytes/encoding/json/io/net/http/time).
