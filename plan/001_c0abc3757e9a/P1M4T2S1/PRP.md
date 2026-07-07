name: "P1.M4.T2.S1 — Forward to upstream (headers, hop-by-hop strip, Accept fallback, ctx) + response header copy"
description: |

  OWN and VERIFY the upstream-forward + response-header-copy mechanics in
  `proxy.go`, and add the ONE behavioral delta the item contract names that the
  preceding items did not ship: **on a non-2xx upstream response, emit an
  `upstream_error` log line `{status, req_id}` while STILL copying status +
  non-hop-by-hop headers + body through to the client (do NOT synthesize)**. The
  forward mechanics themselves (POST to `cfg.Upstream`, request header copy minus
  hop-by-hop, Accept fallback, `r.Context()` propagation, response status +
  header copy, `io.Copy` + `Flush`) are ALREADY on disk — `newUpstreamClient` +
  `hopHeaders`/`isHopByHop`/`copyForwardHeaders` from P1.M1.T4.S2, and the
  `newProxyHandler` (read body → decideRewrite → `bytes.NewReader(dec.body)` →
  copy headers → Del Content-Length → Accept fallback) + `forward(client, rw,
  outReq, dec, log)` wiring from P1.M4.T1.S1 (implemented in parallel; treat its
  PRP as a contract). To carry `req_id` for the log, EXTEND `rewriteDecision`
  with a `reqID any` field and EXTEND `decideRewrite` to capture the first
  JSON-RPC `id` it already sees while iterating (additive — T1.S1's
  streamThrough/rewrittenIDs/warning behavior is untouched). Then VERIFY the
  whole forward + response-header contract with httptest: a fake upstream
  asserting it received `Authorization`, an `Accept` carrying the
  `text/event-stream` token, and `Mcp-Session-Id`; PLUS a non-2xx case asserting
  copy-through (not synthesis) AND the `upstream_error` log line. The response is
  left as `io.Copy` — the conditional `io.Copy`-vs-`Inject` branch is the
  EXPLICIT deliverable of P1.M4.T2.S2 (this item only ships the seam: `forward`
  already receives `dec`, so T2.S2 adds `if len(dec.rewrittenIDs) > 0 { Inject }
  else { io.Copy }`). `go.mod` gains ZERO requires. **Mode A docs**: a doc
  comment on `forward` naming the non-2xx copy-through rule + the T2.S2 seam.

---

## Goal

**Feature Goal**: The proxy forwards every client request to `cfg.Upstream`
correctly (POST, request headers minus hop-by-hop, Accept fallback, client
context cancellation, Authorization verbatim) and copies the upstream response
status + non-hop-by-hop headers + body back to the client — VERIFIED end-to-end
with httptest — AND, on a non-2xx upstream status, logs a structured
`upstream_error {status, req_id}` line while still passing the response through
unchanged (never synthesizing a 502 for an HTTP error status). The request id is
threaded through the decision so the log can name it.

**Deliverable**: NO new files. Three edits, all `package main` in `proxy.go` +
`proxy_test.go`:
- **EXTEND `rewriteDecision` in `proxy.go`** — add `reqID any` (the primary
  JSON-RPC request id, for logging). Additive field; T1.S1's other fields are
  byte-for-byte unchanged.
- **EXTEND `decideRewrite` in `proxy.go`** — capture the first JSON-RPC `id`
  encountered during the EXISTING body iteration into `dec.reqID` (numeric →
  `float64`, string → `string`, absent/notification/scalar/invalid → nil). ~2
  added lines inside the existing `switch`; the rewrite/streamThrough/
  rewrittenIDs/warning logic is untouched (T1.S1's `TestDecideRewrite` stays green).
- **MODIFY `forward` body in `proxy.go`** — after `client.Do`, if
  `resp.StatusCode >= 300`, emit `log.log("warn", "upstream_error",
  map[string]any{"status": resp.StatusCode, "req_id": dec.reqID})` and THEN fall
  through to the EXISTING copy-status + copy-headers + `io.Copy` + `Flush`
  (copy-through, no synthesis, no early return). Update `forward`'s doc comment
  to name the non-2xx rule + cite P1.M4.T2.S2. Signature UNCHANGED (`forward(
  client, rw, outReq, dec rewriteDecision, log)` — T1.S1 set it).
- **APPEND to `proxy_test.go`** — `TestForward_Non2xxCopiedThroughAndLogged`
  (fake upstream returns 503; assert client gets 503 + body verbatim AND the
  captured `upstream_error` log line has the right status + req_id, using a
  buffer-backed `warn`-level logger), `TestForward_McpSessionIdRoundTrip`
  (client sends `Mcp-Session-Id` → upstream received it AND upstream's response
  `mcp-session-id` reached the client), and a `reqID` assertion added to the
  existing `TestDecideRewrite` rows (or a focused `TestDecideRewrite_ReqID`).

**Success Definition**: `go test -run 'TestForward|TestDecideRewrite' -v` passes;
in particular a 503 from a fake upstream is copied to the client (status 503 +
body) AND produces exactly one `upstream_error` log line with `"status":503` and
the request's `req_id`; `Mcp-Session-Id` round-trips request→upstream→response→
client; `dec.reqID == float64(N)` for a numeric request id. The existing
`TestPassthrough_*` suite stays GREEN (reqID is additive; the new non-2xx log is
dropped by those tests' `error`-level discard logger). `go vet ./...` and
`go test ./...` stay clean. `go.mod` unchanged. `go doc . forward` names the
non-2xx copy-through rule and the T2.S2 seam (Mode A).

## Hard Prerequisites

1. **`proxy.go` exists** with the post-T1.S1 shape: `hopHeaders`, `isHopByHop`,
   `newUpstreamClient`, `copyForwardHeaders` (P1.M1.T4.S2 — DONE), plus
   `rewriteDecision`, `decideRewrite`, `rewriteObject`, and `forward(client, rw,
   outReq, dec rewriteDecision, log)` / `newProxyHandler(cfg, log, client)`
   wired to `bytes.NewReader(dec.body)` (P1.M4.T1.S1 — implemented in parallel;
   its PRP is the contract). This item EXTENDS `rewriteDecision` + `decideRewrite`
   + the `forward` BODY in place. If `forward`'s signature does NOT already carry
   `dec rewriteDecision`, STOP — T1.S1 has not landed; this item depends on it.
2. **`forward`'s current body** is the T4.S2 core: `client.Do` → copy non-hop-by-hop
   response headers → `WriteHeader(resp.StatusCode)` → `io.Copy(rw, resp.Body)`
   (warn-log on copy error) → `Flush`. This item inserts the non-2xx log BEFORE
   the header copy and leaves everything else byte-for-byte intact.
3. **The logger API** is `(*logger).log(level, msg string, fields map[string]any)`
   (main.go). Levels: debug<info<warn<error; a line is dropped when its level is
   below the logger's configured level. `newLogger(w io.Writer, level)` accepts
   any writer — tests inject `&bytes.Buffer` at `warn` to capture the line.
4. **`newUpstreamClient`** is DONE (Transport.ResponseHeaderTimeout=30s, NO
   Client.Timeout — external_deps.md §5). Do NOT touch it.
5. **`copyForwardHeaders`** is a DENYLIST (skips hop-by-hop only), so ALL
   non-hop-by-hop headers — Content-Type, Accept, Authorization, Mcp-Session-Id,
   Accept-Language, User-Agent — pass through VERBATIM. Do NOT convert it to an
   allowlist (PRD §11.2 lists examples, not an exhaustive set).

## User Persona

**Target User**: (1) **P1.M4.T2.S2** (next item, response path): consumes `dec`
(streamThrough/rewrittenIDs/warning) + the already-copied response headers to
choose `io.Copy` vs `Inject`. (2) **P1.M4.T3.S1** (logging): formalizes the
`rewrite` and `debug forward` events; this item ships the `upstream_error`
(non-2xx) emission it will consolidate. (3) the **operator**, who sees a
structured `upstream_error {status, req_id}` line when z.ai returns 4xx/5xx.
(4) the **MCP client**, whose request reaches z.ai with correct headers and whose
response — even a non-2xx — is passed through faithfully.

**Use Case**: client POSTs a tools/call; proxy forwards headers + body upstream;
upstream returns e.g. 429; proxy logs `upstream_error {status:429, req_id:2}` and
streams the 429 + headers + body to the client unchanged.

**Pain Points Addressed**: (1) Without the non-2xx log, a failing upstream is
silent in stderr (the existing logs only fire on transport/copy errors, not HTTP
statuses). (2) Without `req_id`, the operator cannot correlate the log to a
request. (3) The "do not synthesize" rule must be guarded by a test so a future
edit can't accidentally turn a 4xx into a 502.

## Why

- **PRD §11.2 (Forward to upstream)** is the request-side contract this item
  VERIFIES (POST, header allowlist via denylist, hop-by-hop strip, Accept
  fallback, context-bound client).
- **PRD §11.3 (Write the response)** is the response-side contract this item
  VERIFIES (status copy + non-hop-by-hop response header copy + `io.Copy` +
  `Flush`); the conditional `io.Copy`-vs-`Inject` is explicitly deferred to
  P1.M4.T2.S2 (PRD §11.3's "If streamThrough … Else …" branch).
- **PRD §13 (Headers/credentials/security)**: Authorization forwarded verbatim,
  never read/logged. The `upstream_error` log carries `{status, req_id}` ONLY —
  no request headers, so no secret can leak (matches `redactHeaders` discipline).
- **PRD §15 (Logging)**: the `upstream_error` event is `{status, req_id}` "on
  non-2xx upstream responses." This item emits it.
- **PRD §17 (Timeouts)**: per-request context cancellation (no Client.Timeout,
  ResponseHeaderTimeout 30s) — already correct; this item only verifies ctx
  propagation is wired (`NewRequestWithContext(r.Context())`).
- **Coherence across the chain**: `decideRewrite` (T1.S1) parses + rewrites;
  this item threads `req_id` and owns the forward response log; T2.S2 owns the
  conditional body transform; T3.S1 owns the `rewrite`/`debug forward` events.
  Clean, non-overlapping ownership.

## What

Three surgical edits: extend `rewriteDecision` with `reqID`; extend
`decideRewrite` to populate it (additive); insert the non-2xx `upstream_error`
log in `forward` (copy-through, no synthesis). Plus httptest verification of the
full forward + response-header contract and the non-2xx path. No new files; no
new imports (stdlib only; `bytes`/`encoding/json`/`io`/`net/http` already present
after T1.S1). The response body stays `io.Copy` — the conditional is T2.S2.

### Success Criteria

- [ ] `rewriteDecision` has a `reqID any` field in `proxy.go` (additive; T1.S1's
      `body`/`streamThrough`/`rewrittenIDs`/`warning` unchanged).
- [ ] `decideRewrite([]byte{'{"jsonrpc":"2.0","id":7,...}'}`) →
      `dec.reqID == float64(7)`; a string id `"abc"` → `dec.reqID == "abc"`;
      a notification (no `id`) / scalar / invalid body → `dec.reqID == nil`.
- [ ] T1.S1's `TestDecideRewrite` rows STILL pass (reqID does not alter
      streamThrough/rewrittenIDs/warning/body).
- [ ] `forward`, on a non-2xx upstream response (`resp.StatusCode >= 300`), emits
      EXACTLY one `log.log("warn", "upstream_error", {"status": <code>, "req_id":
      <id>})` and THEN copies status + non-hop-by-hop headers + body through
      (no `http.Error`, no early return, no synthesized status).
- [ ] A fake upstream returning 503 → the test client receives `StatusCode==503`
      and the original body verbatim; the captured log buffer contains one line
      whose JSON has `"msg":"upstream_error"`, `"status":503`, and the request id.
- [ ] A fake upstream returning 200 emits NO `upstream_error` line (the log fires
      only on non-2xx).
- [ ] `Mcp-Session-Id` round-trips: client request header reaches the fake
      upstream AND the upstream's response `mcp-session-id` reaches the client.
- [ ] `Authorization` is forwarded verbatim; `Accept` carries the
      `text/event-stream` token (fallback when omitted); hop-by-hop headers are
      stripped on BOTH request and response.
- [ ] `forward`'s signature is UNCHANGED from T1.S1 (`forward(client, rw, outReq,
      dec rewriteDecision, log)`); only its BODY gains the non-2xx log + an
      updated doc comment.
- [ ] `go vet ./...` clean; `go test ./...` green; `go.mod` unchanged; no `.go`
      file other than `proxy.go`/`proxy_test.go` is edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone
because: (a) the exact post-T1.S1 `forward`/`decideRewrite`/`rewriteDecision`
shapes are quoted and located; (b) the single behavioral delta (non-2xx log) is
given as exact Go with the copy-through guarantee stated; (c) the `req_id`
extension is a 2-line additive change shown verbatim; (d) the logger level
semantics are stated so the test injects the right level; (e) the boundary with
T2.S2 (leave `io.Copy`, only ship the seam) is explicit; (f) all test cases are
enumerated with literal assertions, including the buffer-backed logger capture.

### Documentation & References

```yaml
# MUST READ — the forward/response contract this item verifies + extends.
- file: PRD.md
  section: "§11.2 Forward to upstream" + "§11.3 Write the response" + "§13" + "§15" + "§17"
  why: §11.2 = request-side forward rules (POST, header copy, hop-by-hop strip, Accept
        fallback, context-bound client). §11.3 = response-side rules (status copy,
        non-hop-by-hop header copy, io.Copy passthrough, Flush) — and the conditional
        io.Copy-vs-Inject BRANCH that this item explicitly leaves to P1.M4.T2.S2.
        §13 = Authorization forwarded verbatim, never logged. §15 = upstream_error event
        is {status, req_id} on non-2xx. §17 = per-request ctx, no Client.Timeout.
  critical: §11.3 "If streamThrough: io.Copy ... Else: feed ... through the SSE
        injector" — the ELSE branch is P1.M4.T2.S2, NOT this item. This item keeps io.Copy.

# MUST READ — the current proxy.go (forward core + decision wiring this item extends).
- file: proxy.go
  why: contains hopHeaders/isHopByHop/newUpstreamClient/copyForwardHeaders (T4.S2, STABLE —
        do not touch), rewriteDecision/decideRewrite/rewriteObject (T1.S1), newProxyHandler
        (T1.S1 wiring), forward (T4.S2 body + T1.S1 signature). This item EXTENDS
        rewriteDecision + decideRewrite + the forward BODY only.
  pattern: forward's response loop `for k, vs := range resp.Header { if isHopByHop(k) {
        continue } ... }` + `rw.WriteHeader(resp.StatusCode)` + `io.Copy` is the copy-through
        path the non-2xx log must NOT short-circuit.
  gotcha: insert the non-2xx log AFTER client.Do success and BEFORE/AROUND the header copy,
        but NEVER add an early `return` on non-2xx — the response must still be copied.

# MUST READ — the previous PRP (contract for the parallel T1.S1 work).
- docfile: plan/001_c0abc3757e9a/P1M4T1S1/PRP.md
  why: defines the post-T1.S1 proxy.go this item builds on: rewriteDecision fields,
        decideRewrite body, newProxyHandler wiring (bytes.NewReader(dec.body), Del
        Content-Length, Accept fallback), forward(client, rw, outReq, dec, log). Its
        "CONSUMER SEAM" section explicitly says T2.S1 "is largely verification."
  critical: forward's BODY is UNCHANGED by T1.S1 (only its signature gained `dec`). This
        item is the first to change forward's BODY since T4.S2. Keep the diff minimal:
        + the non-2xx log, + the doc comment.

# MUST READ — verified Go facts (header stripping, ctx, timeouts, logger).
- docfile: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§4 Reverse-proxy header handling" + "§5 Context cancellation"
  why: §4 confirms the 9-entry hop-by-hop list (incl Proxy-Connection) is the stdlib set,
        stripped on BOTH request and response — already implemented; this item verifies it.
        §5 confirms outReq.WithContext(r.Context()) + NO Client.Timeout +
        ResponseHeaderTimeout=30s — already implemented (newUpstreamClient).
  critical: a manual http.Handler (not httputil.ReverseProxy) is correct here precisely
        because we need the conditional response path (T2.S2). Do not "simplify" to ReverseProxy.

# MUST READ — the logger API + level semantics (for the non-2xx test).
- file: main.go
  section: "func (l *logger) log" + "func levelNum" + "func newLogger"
  why: log.log drops when levelNum(level) < l.level; newLogger(&buf, "warn") captures
        "warn" lines. The existing newTestProxy uses newLogger(io.Discard, "error"), which
        DROPS warn — so the non-2xx test MUST build its own buffer-backed warn logger.

# MUST READ — the existing httptest harness + header-assertion tests (do not redeclare).
- file: proxy_test.go
  why: fakeUpstream/newTestProxy/initSSE/testSID are defined (T4.S2). TestPassthrough_*
        already assert Authorization forwarded, Accept fallback, Mcp-Session-Id forwarded,
        response headers copied, hop-by-hop stripped. This item APPENDS the non-2xx +
        round-trip + reqID tests; it does NOT redeclare the harness helpers.
  pattern: table-driven + httptest.NewServer recording *got http.Request; PRD-§ comments.

# Verified facts — see this item's research note.
- docfile: plan/001_c0abc3757e9a/P1M4T2S1/research/forward-and-non2xx.md
  section: "§2 Verified Go/codebase facts" + "§3 The req_id decision"
  why: documents the overlap (T1.S1 already shipped the mechanics), the bilateral hop-by-hop
        strip, the logger-level gotcha for the non-2xx test, and the additive reqID choice.

- url: https://pkg.go.dev/net/http#HandlerFunc
  why: WriteHeader locks the header map — copy response headers BEFORE WriteHeader (already
        done in forward); the non-2xx log must not reorder this.
- url: https://pkg.go.dev/net/http/httptest
  why: httptest.NewServer fake upstream pattern used by the tests.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment
  main.go           # bootstrap + logger (log.log/levelNum/newLogger) + routes — UNTOUCHED
  config.go         # Config — UNTOUCHED
  proxy.go          # forward + newProxyHandler + hop stuff (T4.S2) + decideRewrite (T1.S1)
                    #   — EXTEND rewriteDecision + decideRewrite + forward BODY THIS ITEM
  rewrite.go        # Rewrite — UNTOUCHED
  sse.go            # Event/Reader + warningText + Inject — UNTOUCHED
  proxy_test.go     # TestPassthrough_* + TestDecideRewrite (T4.S2/T1.S1) — EXTEND THIS
  *_test.go         # other tests — UNTOUCHED
  testdata/*.sse    # SSE fixtures — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy.go        # EXTEND: rewriteDecision += reqID any; decideRewrite += capture-first-id
                #   (additive); forward BODY += non-2xx upstream_error log (copy-through,
                #   no synthesis) + updated doc comment. NO new imports.
proxy_test.go   # EXTEND (append): TestForward_Non2xxCopiedThroughAndLogged,
                #   TestForward_McpSessionIdRoundTrip, and reqID assertions (extend
                #   TestDecideRewrite or add TestDecideRewrite_ReqID). Reuse
                #   fakeUpstream/initSSE/testSID; do NOT redeclare them.
```

No new files. No other file changes. `main.go` calls `newProxyHandler(cfg, log,
client)` — its signature is UNCHANGED, so no edits there.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — copy-through, do NOT synthesize. On non-2xx, log upstream_error and
  CONTINUE to the existing copy-status + copy-headers + io.Copy path. NEVER add
  http.Error / an early return / a synthesized 502 for an HTTP error status. The
  ONLY synthesized response is on client.Do transport FAILURE (already in T4.S2),
  which is correct (no upstream response to copy). A test pins: 503 → client 503.

CRITICAL — log BEFORE the header copy, but do not reorder WriteHeader. The
  existing forward copies resp.Header into rw.Header() THEN calls WriteHeader
  (WriteHeader locks the map). The non-2xx log must not be placed after WriteHeader
  (it would still work but muddies "log then serve"); place it right after the
  client.Do error check, before the header loop. Logging does not touch rw.

CRITICAL — logger level for the test. newTestProxy uses newLogger(io.Discard,
  "error"), which DROPS "warn". The non-2xx log is "warn". The non-2xx test MUST
  construct its own proxy with newLogger(&buf, "warn") (or "debug") to observe the
  line. (Verified: levelNum warn=2 < error=3 ⇒ dropped at error level.)

CRITICAL — req_id is ADDITIVE. Do NOT alter decideRewrite's streamThrough /
  rewrittenIDs / warning / body logic. Capture the first id in the EXISTING switch
  arms (map[string]any: reqID = v["id"]; []any: first element's obj["id"]). Numeric
  ids are float64 (same encoder contract as rewrittenIDs/Inject). T1.S1's
  TestDecideRewrite must stay GREEN.

CRITICAL — do NOT implement the response conditional. forward keeps io.Copy. The
  `if len(dec.rewrittenIDs) > 0 { Inject(...) } else { io.Copy }` branch is
  P1.M4.T2.S2. This item ships only: the non-2xx log + req_id + verification + the
  seam (dec is already in forward's signature from T1.S1).

CRITICAL — Authorization is never logged. The upstream_error fields are {status,
  req_id} ONLY. Do NOT add request/response headers to the log (PRD §13). The
  existing redactHeaders is a safety net, not an excuse to log secrets.

GOTCHA — copyForwardHeaders is a DENYLIST. It forwards ALL non-hop-by-hop headers
  (Content-Type, Accept, Authorization, Mcp-Session-Id, Accept-Language, User-Agent,
  and anything else). Do NOT convert it to an allowlist — PRD §11.2's header list is
  illustrative, and identity pass-through is the design (PRD §8 session handling).

GOTCHA — hop-by-hop strip is BILATERAL. isHopByHop is checked in BOTH
  copyForwardHeaders (request) and the response-header loop (forward). Do not add a
  second request-side strip; it is already done. The item's "strip on BOTH request
  and response" is already satisfied.

GOTCHA — ctx is already wired. NewRequestWithContext(r.Context(), ...) (T1.S1) IS
  outReq.WithContext(r.Context()) (external_deps.md §5). Do not call
  outReq.WithContext again. NO Client.Timeout is set (newUpstreamClient); do not
  add one (it would cut off SSE streams).

GOTCHA — do NOT redeclare fakeUpstream/initSSE/testSID/newTestProxy. They are
  defined by T4.S2. The new tests reuse them. The non-2xx test needs a custom
  upstream (returns 503) and a buffer-backed logger, so it builds its own
  httptest.Server + newProxyHandler inline rather than via newTestProxy.
```

## Implementation Blueprint

### Data models and structure

`rewriteDecision` gains one field (additive; no JSON tags — in-process value):

```go
type rewriteDecision struct {
	body          []byte
	streamThrough bool
	rewrittenIDs  map[any]bool
	warning       string
	reqID         any // NEW (P1.M4.T2.S1): primary JSON-RPC request id for the upstream_error log (PRD §15). float64 for numbers, string for strings, nil when absent/unparseable.
}
```

No new types. No new imports (`bytes`, `encoding/json`, `io`, `net/http`, `time`
already present after T1.S1).

### Reference implementation (EDIT in `proxy.go`)

> Assume the post-T1.S1 `proxy.go`. The three changes below are surgical and
> additive. Run `gofmt -w proxy.go` after.

**EDIT 1 — extend `rewriteDecision`** (add the `reqID` field after `warning`):

```go
type rewriteDecision struct {
	body          []byte
	streamThrough bool
	rewrittenIDs  map[any]bool
	warning       string
	// reqID is the primary JSON-RPC request id of this request, captured for the
	// upstream_error log on non-2xx responses (PRD §15). It is the decoded value
	// (float64 for numeric ids, string for string ids, nil when the body is a
	// notification / scalar / unparseable). It is ADDITIVE: it does not affect the
	// rewrite/streamThrough/rewrittenIDs/warning behavior (P1.M4.T2.S1).
	reqID any
}
```

**EDIT 2 — extend `decideRewrite`** (capture the first id in the existing
`switch`; the rest of the function is byte-for-byte T1.S1):

```go
func decideRewrite(body []byte, cfg Config) rewriteDecision {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		return rewriteDecision{body: body, streamThrough: true}
	}

	var rewrittenIDs map[any]bool
	var notes []string
	var reqID any // NEW (P1.M4.T2.S1): first JSON-RPC id seen, for the upstream_error log.
	addID := func(id any, n []string) {
		if rewrittenIDs == nil {
			rewrittenIDs = make(map[any]bool)
		}
		rewrittenIDs[id] = true
		notes = append(notes, n...)
	}

	switch v := parsed.(type) {
	case map[string]any:
		reqID = v["id"] // NEW: capture the (single) request's id for logging.
		if n := rewriteObject(v, cfg); n != nil {
			addID(v["id"], n)
		}
	case []any:
		for _, elem := range v {
			if obj, ok := elem.(map[string]any); ok {
				if reqID == nil { // NEW: first element's id wins (batch is defensive).
					reqID = obj["id"]
				}
				if n := rewriteObject(obj, cfg); n != nil {
					addID(obj["id"], n)
				}
			}
		}
	default:
		return rewriteDecision{body: body, streamThrough: true}
	}

	if len(rewrittenIDs) == 0 {
		return rewriteDecision{body: body, streamThrough: true, reqID: reqID} // NEW: thread reqID on the unchanged path too.
	}

	out, err := json.Marshal(parsed)
	if err != nil {
		return rewriteDecision{body: body, streamThrough: true}
	}
	return rewriteDecision{
		body:          out,
		streamThrough: false,
		rewrittenIDs:  rewrittenIDs,
		warning:       warningText(notes),
		reqID:         reqID, // NEW
	}
}
```

> The `err`-return and the `default`-return intentionally leave `reqID` zero
> (nil): an unparseable or scalar body has no id to log. Update `decideRewrite`'s
> doc comment to note `reqID` is captured for the `upstream_error` log.

**EDIT 3 — insert the non-2xx log in `forward`** (signature UNCHANGED from T1.S1;
add the log right after the `client.Do` error check, before the response-header
loop; update the doc comment):

```go
// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.2/§11.3). It is the REUSABLE FORWARD CORE: copy status + non-hop-by-hop
// response headers, stream the body, flush for SSE.
//
// NON-2xx (P1.M4.T2.S1, PRD §15): when the upstream returns a non-2xx status, an
// `upstream_error` line {status, req_id} is logged at warn — but the response is
// STILL copied through verbatim (status + headers + body). The proxy NEVER
// synthesizes a 502 for an upstream HTTP error status; synthesis happens only on a
// transport failure (client.Do error) below.
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
	if _, err := io.Copy(rw, resp.Body); err != nil {
		log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
	}
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
}
```

> The ONLY lines added to `forward`'s body are the `if resp.StatusCode >= 300 { ... }`
> block. Everything else is byte-for-byte the T4.S2 core (response-header loop,
> WriteHeader, io.Copy, Flush). The `client.Do` error branch is unchanged.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the post-T1.S1 inputs exist
  - RUN: grep -n "func forward\|func decideRewrite\|type rewriteDecision" proxy.go
  - EXPECT: forward signature carries "dec rewriteDecision" (T1.S1 landed). IF
        forward's signature is still "forward(client, rw, outReq, log)" with NO
        dec, STOP — T1.S1 has not completed; this item depends on it.
  - CONFIRM copyForwardHeaders/isHopByHop/newUpstreamClient exist (T4.S2).

Task 1: EXTEND rewriteDecision + decideRewrite in proxy.go
  - ADD the "reqID any" field to rewriteDecision (EDIT 1) + its doc line.
  - ADD the two "reqID =" capture lines + thread reqID into the three return
        statements (EDIT 2). Update decideRewrite's doc comment to mention reqID.
  - DO NOT alter streamThrough/rewrittenIDs/warning/body logic. T1.S1's
        TestDecideRewrite must stay GREEN.

Task 2: MODIFY forward body in proxy.go (non-2xx log)
  - INSERT the "if resp.StatusCode >= 300 { log.log(...) }" block after the
        client.Do error check and BEFORE the response-header loop (EDIT 3).
  - UPDATE forward's doc comment to name the non-2xx copy-through rule + cite
        P1.M4.T2.S2. Signature UNCHANGED.
  - DO NOT add an early return / http.Error on non-2xx (copy-through).

Task 3: APPEND the new tests to proxy_test.go
  - PACKAGE: main. APPEND at the END. Reuse fakeUpstream/initSSE/testSID; do NOT
        redeclare. Add no new imports ("bytes","encoding/json","io","net/http",
        "net/http/httptest","strings","testing" already present).
  - TestForward_Non2xxCopiedThroughAndLogged: fake upstream returns 503 + a body;
        buffer-backed warn logger; assert client.StatusCode==503, body verbatim,
        AND the buffer has one upstream_error line with status 503 + the req_id.
  - TestForward_2xxNoUpstreamError: fake upstream returns 200; assert NO
        upstream_error line in the buffer (log fires only on non-2xx).
  - TestForward_McpSessionIdRoundTrip: client sends Mcp-Session-Id; assert the
        fake upstream RECEIVED it AND the client received the response mcp-session-id.
  - reqID: add assertions to TestDecideRewrite (or a TestDecideRewrite_ReqID)
        that dec.reqID == float64(N) for numeric id and "abc" for string id.

Task 4: VALIDATE
  - gofmt -w proxy.go proxy_test.go
  - go vet ./...
  - go test -run 'TestForward|TestDecideRewrite' -v   # new + existing decision tests
  - go test -run TestPassthrough -v                    # T4.S2 e2e regression (still green)
  - go test ./...                                      # full suite green
  - git diff --stat  # expect ONLY proxy.go + proxy_test.go
  - git diff go.mod  # expect EMPTY
  - go doc . forward  # Mode A: prints the non-2xx copy-through rule + T2.S2 seam.
```

### Test block (proxy_test.go — APPEND)

```go
// captureProxy builds a proxy at upstreamURL whose logger writes to buf at the
// given level (so warn-level upstream_error lines are observable, unlike
// newTestProxy's discard-error logger). (P1.M4.T2.S1)
func captureProxy(t *testing.T, upstreamURL string, buf *bytes.Buffer, level string) *httptest.Server {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Upstream = upstreamURL
	return httptest.NewServer(newProxyHandler(cfg, newLogger(buf, level), newUpstreamClient()))
}

// (6) Non-2xx upstream: log upstream_error {status, req_id} BUT copy status +
// headers + body through unchanged (PRD §15/§11.3; P1.M4.T2.S1 MOCKING).
func TestForward_Non2xxCopiedThroughAndLogged(t *testing.T) {
	const errBody = `{"error":"rate limited"}`
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusServiceUnavailable) // 503
		_, _ = io.WriteString(w, errBody)
	}))
	defer up.Close()

	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "warn")
	defer proxy.Close()

	// A tools/call with a numeric id so req_id is non-nil in the log.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	// Copy-through: the client gets 503 + the original body, not a synthesized 502.
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (copy-through, not synthesized)", resp.StatusCode)
	}
	got, _ := io.ReadAll(resp.Body)
	if string(got) != errBody {
		t.Errorf("body = %q, want %q (copied through verbatim)", got, errBody)
	}
	if resp.Header.Get("Mcp-Session-Id") != testSID {
		t.Error("non-hop-by-hop response header not copied on non-2xx")
	}

	// Exactly one upstream_error line, with status 503 and req_id 2.
	var saw bool
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if !bytes.Contains(line, []byte(`"msg":"upstream_error"`)) {
			continue
		}
		if saw {
			t.Fatalf("more than one upstream_error line:\n%s", buf.String())
		}
		saw = true
		if !bytes.Contains(line, []byte(`"status":503`)) {
			t.Errorf("upstream_error line missing status 503:\n%s", line)
		}
		// req_id 2 marshals as a JSON number (json.Marshal of float64(2) -> "2").
		if !bytes.Contains(line, []byte(`"req_id":2`)) {
			t.Errorf("upstream_error line missing req_id 2:\n%s", line)
		}
	}
	if !saw {
		t.Fatalf("no upstream_error line emitted for 503:\n%s", buf.String())
	}
}

// (7) A 2xx response emits NO upstream_error line (the log fires only on non-2xx).
func TestForward_2xxNoUpstreamError(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // replies 200 + initSSE
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug") // debug captures everything
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if bytes.Contains(buf.Bytes(), []byte(`"msg":"upstream_error"`)) {
		t.Errorf("upstream_error emitted on 200:\n%s", buf.String())
	}
}

// (8) Mcp-Session-Id round-trip: client request header -> upstream, and upstream
// response header -> client (PRD §8/§11; item MOCKING "Mcp-Session-Id round-trips").
func TestForward_McpSessionIdRoundTrip(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // echoes mcp-session-id=testSID in the response
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	const clientSID = "22222222-2222-2222-2222-222222222222"
	req, _ := http.NewRequest(http.MethodPost, proxy.URL, strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Mcp-Session-Id", clientSID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	// Request side: upstream received the client's Mcp-Session-Id verbatim.
	if got.Header.Get("Mcp-Session-Id") != clientSID {
		t.Errorf("upstream Mcp-Session-Id = %q, want %q", got.Header.Get("Mcp-Session-Id"), clientSID)
	}
	// Response side: the upstream's mcp-session-id reached the client.
	if resp.Header.Get("Mcp-Session-Id") != testSID {
		t.Errorf("client Mcp-Session-Id = %q, want %q", resp.Header.Get("Mcp-Session-Id"), testSID)
	}
}

// TestDecideRewrite_ReqID pins that decideRewrite threads the request id for the
// upstream_error log (P1.M4.T2.S1). Numeric -> float64; string -> string; absent
// -> nil. Additive to T1.S1's decision (does not affect rewrittenIDs/warning).
func TestDecideRewrite_ReqID(t *testing.T) {
	cases := []struct {
		name string
		body string
		want any
	}{
		{"numeric_id", `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`, float64(2)},
		{"string_id", `{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"arguments":{"query":"x"}}}`, "abc"},
		{"no_id_notification", `{"jsonrpc":"2.0","method":"tools/call","params":{"arguments":{"query":"x"}}}`, nil},
		{"initialize", `{"jsonrpc":"2.0","method":"initialize","id":1}`, float64(1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := decideRewrite([]byte(tc.body), dcCfg)
			if dec.reqID != tc.want {
				t.Errorf("reqID = %#v (want %#v); note numeric must be float64, not int", dec.reqID, tc.want)
			}
		})
	}
}
```

> NOTE: `dcCfg` is defined by T1.S1's `TestDecideRewrite` (a minimal Config). If
> T1.S1 named it differently, reuse whatever constant it defined; do not shadow it.
> The `reqID` assertions can alternatively be folded into T1.S1's table rows —
> either form is acceptable as long as they run.

### Implementation Patterns & Key Details

```go
// PATTERN: copy-through on non-2xx. The proxy is transparent: an upstream 4xx/5xx
// is the upstream's answer, forwarded verbatim. Only a TRANSPORT failure (can't
// reach upstream) is synthesized into a 502. The non-2xx log is observability,
// not a control-flow branch — no early return after it.

// PATTERN: log fields are {status, req_id} ONLY (PRD §15). No headers, no body —
// Authorization must never appear in a log line. status is an int (marshals as a
// JSON number); req_id is the decoded JSON-RPC id (float64/string/nil).

// PATTERN: additive struct extension. reqID joins rewriteDecision without
// touching the T1.S1 fields. decideRewrite captures it in the EXISTING iteration
// (no second parse). Numeric ids stay float64 (the encoding/json contract that
// rewrittenIDs and Inject already rely on).

// GOTCHA (restated): the non-2xx log is "warn". newTestProxy's logger is "error"
// (drops warn). The non-2xx test uses captureProxy with a buffer-backed "warn"
// logger. The existing TestPassthrough_* tests are unaffected (they use the
// discard-error logger, so the new warn line is silently dropped there too).

// GOTCHA (restated): WriteHeader locks rw.Header(). Copy response headers BEFORE
// WriteHeader (already the order in forward). The non-2xx log does not touch rw,
// so its placement (after the Do check, before the header loop) is safe.
```

### Integration Points

```yaml
FILES MODIFIED:
  - proxy.go       (EXTEND: rewriteDecision += reqID; decideRewrite += capture-first-id
                      + thread reqID into returns; forward BODY += non-2xx upstream_error
                      log + updated doc comment. Signature unchanged. NO new imports.)
  - proxy_test.go  (EXTEND: + captureProxy helper + TestForward_Non2xxCopiedThroughAndLogged
                      + TestForward_2xxNoUpstreamError + TestForward_McpSessionIdRoundTrip
                      + TestDecideRewrite_ReqID. Reuse fakeUpstream/initSSE/testSID.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only).
  - main.go: calls newProxyHandler(cfg, log, client) — signature UNCHANGED.
  - rewrite.go / sse.go / config.go / doc.go: untouched.
  - other *_test.go / testdata/*.sse: untouched.
CONSUMER SEAM (keep stable for the next item):
  - P1.M4.T2.S2 (response): forward() still does io.Copy. T2.S2 replaces the
        body step with `if len(dec.rewrittenIDs) > 0 { Inject(rw, resp.Body,
        dec.rewrittenIDs, dec.warning) } else { io.Copy(rw, resp.Body) }` then
        Flush. dec.rewrittenIDs (map[any]bool, float64 keys) + dec.warning are
        guaranteed by decideRewrite (T1.S1). The non-2xx log + header copy this
        item added stay ABOVE that branch (unchanged by T2.S2).
  - P1.M4.T3.S1 (logging): formalizes the `rewrite` + `debug forward` events and
        may consolidate the three upstream_error emission sites (Do-error, non-2xx,
        Copy-error). This item ships the non-2xx emission in its natural place.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w proxy.go proxy_test.go
go vet ./...
git diff --stat           # expect: proxy.go + proxy_test.go ONLY
git diff go.mod           # expect: EMPTY (zero new requires)

# Expected: gofmt clean; vet clean; only proxy.go + proxy_test.go changed; go.mod
# unchanged. No new imports were needed (bytes/encoding/json/io/net/http already
# present after T1.S1; proxy_test.go already imports bytes/encoding/json/io/
# net/http/httptest/strings/testing).
```

### Level 2: Unit Tests (Component Validation)

```bash
# New + existing decision tests.
go test -run 'TestForward|TestDecideRewrite' -v

# MUST PASS (the ones that prove the contract):
#   TestForward_Non2xxCopiedThroughAndLogged -> client 503 + body verbatim + ONE
#       upstream_error line {status:503, req_id:2}; response header copied on non-2xx.
#   TestForward_2xxNoUpstreamError            -> NO upstream_error line on 200.
#   TestForward_McpSessionIdRoundTrip         -> request header reached upstream AND
#       response header reached client.
#   TestDecideRewrite_ReqID                   -> float64(2) for numeric, "abc" for
#       string, nil for notification.
# Expected: PASS, exit 0. If Non2xx shows a SYNTHESIZED 502 or no body, an early
# return was accidentally added — remove it (copy-through).

# Regression: the T4.S2 e2e harness MUST still pass.
go test -run TestPassthrough -v
# Expected: PASS — reqID is additive; the new non-2xx warn log is dropped by these
# tests' discard-error logger, so their behavior is identical to T4.S2/T1.S1.
```

### Level 3: Integration Testing (System Validation)

```bash
go build ./...          # compiles with the extended rewriteDecision + forward body
go test ./...           # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . forward        # prints the non-2xx copy-through rule + the T2.S2 seam (Mode A)
go doc . rewriteDecision

# Expected: build clean; full suite green; go doc . forward names the non-2xx rule.
# NOTE: the end-to-end "rewrite request -> warning in response" flow is NOT yet
# complete — the response-side Inject branch lands in P1.M4.T2.S2. After THIS item,
# a non-2xx upstream response is logged AND passed through; a 2xx is passed through
# (still io.Copy, no warning injection yet).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Pin copy-through on non-2xx (the load-bearing "do not synthesize" rule).
go test -run TestForward_Non2xxCopiedThroughAndLogged -v
# A failure here means either (i) an early return/http.Error was added on non-2xx,
# or (ii) the upstream_error log was not emitted. Both are contract violations.

# (b) Pin "log fires only on non-2xx" (no spurious upstream_error on success).
go test -run TestForward_2xxNoUpstreamError -v

# (c) Pin the Mcp-Session-Id round-trip (item MOCKING requirement).
go test -run TestForward_McpSessionIdRoundTrip -v

# (d) Pin req_id threading (float64 contract for the log correlation key).
go test -run TestDecideRewrite_ReqID -v

# Expected: all four PASS.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `proxy.go` + `proxy_test.go`; `go.mod` unchanged.
- [ ] Level 2 passes: `go test -run 'TestForward|TestDecideRewrite' -v` green.
- [ ] Level 2 regression: `go test -run TestPassthrough -v` green.
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc . forward`
      names the non-2xx copy-through rule + the T2.S2 seam (Mode A).

### Feature Validation

- [ ] `rewriteDecision` has `reqID any`; `decideRewrite` populates it (float64 for
      numeric ids, string for string ids, nil when absent) WITHOUT altering T1.S1's
      streamThrough/rewrittenIDs/warning/body.
- [ ] `forward` logs `upstream_error {status, req_id}` on `resp.StatusCode >= 300`
      and STILL copies status + non-hop-by-hop headers + body through (no synthesis).
- [ ] Non-2xx (503) reaches the client as 503 + original body; 200 emits no
      `upstream_error` line.
- [ ] `Mcp-Session-Id` round-trips request→upstream and response→client.
- [ ] `Authorization` forwarded verbatim; `Accept` carries `text/event-stream`
      (fallback when omitted); hop-by-hop stripped on both sides.
- [ ] `forward`'s signature UNCHANGED from T1.S1; only its BODY + doc comment changed.

### Code Quality Validation

- [ ] The `reqID` extension is ADDITIVE (T1.S1's `TestDecideRewrite` rows stay green).
- [ ] The non-2xx log is the ONLY behavioral change to `forward`'s body; the
      response-header loop / WriteHeader / io.Copy / Flush are byte-for-byte T4.S2.
- [ ] Tests APPENDED to `proxy_test.go`; no redeclared `fakeUpstream`/`initSSE`/
      `testSID`/`newTestProxy`/`dcCfg`.
- [ ] No new imports in either file.
- [ ] The `upstream_error` log fields are `{status, req_id}` ONLY (no headers/body;
      Authorization can never appear in a log line — PRD §13).
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . forward` prints the non-2xx copy-through rule
      and cites P1.M4.T2.S2 for the response conditional.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't synthesize a response on non-2xx. PRD §11.3/§15: a non-2xx upstream
  status is copied through (status + headers + body); the `upstream_error` log is
  observability only. Adding `http.Error` / an early `return` on non-2xx would
  hide the real upstream answer. Synthesis is reserved for a `client.Do` transport
  failure (already in T4.S2 — no upstream response exists to copy).
- ❌ Don't alter T1.S1's decision logic. `reqID` is ADDITIVE: capture the first id
  in the EXISTING `switch` and thread it into the returns. Do not touch
  `streamThrough`/`rewrittenIDs`/`warning`/`body` — T1.S1's `TestDecideRewrite`
  must stay green.
- ❌ Don't log request/response headers. The `upstream_error` fields are
  `{status, req_id}` (PRD §15). Authorization is forwarded verbatim and never
  read/logged (PRD §13); `redactHeaders` is a safety net, not a license.
- ❌ Don't convert `copyForwardHeaders` to an allowlist. It is a DENYLIST (skip
  hop-by-hop only) so ALL non-hop-by-hop headers pass through — that is identity
  pass-through by design (PRD §8). PRD §11.2's header list is illustrative.
- ❌ Don't implement the response conditional. `forward` keeps `io.Copy`. The
  `if len(dec.rewrittenIDs) > 0 { Inject } else { io.Copy }` branch is
  P1.M4.T2.S2's explicit deliverable. This item ships the non-2xx log + req_id +
  the seam (dec already in the signature from T1.S1).
- ❌ Don't reorder WriteHeader relative to the header copy. Copy non-hop-by-hop
  response headers into `rw.Header()` BEFORE `WriteHeader` (WriteHeader locks the
  map). The non-2xx log does not touch `rw`, so place it after the `client.Do`
  check and before the header loop — do not move WriteHeader earlier.
- ❌ Don't use the discard-error logger to assert the non-2xx log.
  `newTestProxy`'s `newLogger(io.Discard, "error")` DROPS `warn` lines. The non-2xx
  test MUST use a buffer-backed `warn`-level logger (`captureProxy`). Existing
  `TestPassthrough_*` tests are intentionally unaffected (their logger drops the
  new warn line).
- ❌ Don't set `http.Client.Timeout` or re-call `WithContext`. `newUpstreamClient`
  already sets ResponseHeaderTimeout=30s and leaves Timeout ZERO (external_deps.md
  §5 — a Timeout would cut off SSE streams). `NewRequestWithContext(r.Context())`
  (T1.S1) already propagates cancellation. Do not "fix" what is already correct.
- ❌ Don't OVERWRITE `proxy.go`/`proxy_test.go` or redeclare existing helpers.
  EDIT `rewriteDecision`/`decideRewrite`/`forward` in place; APPEND the new tests.
  Reuse `fakeUpstream`/`initSSE`/`testSID`/`newTestProxy`/`dcCfg`.
- ❌ Don't modify `rewrite.go`, `sse.go`, `main.go`, `config.go`, `go.mod`,
  `PRD.md`, `testdata/*`, or any file other than `proxy.go` + `proxy_test.go`.
