# Research — P1.M4.T2.S1: Forward to upstream + response header copy

## 1. Scope-overlap analysis (the load-bearing finding)

P1.M4.T1.S1 (implemented in parallel, treated as a contract) ALREADY modified
`proxy.go` to wire the rewrite decision into the forward path. Concretely, after
T1.S1 the on-disk `proxy.go` contains:

- `rewriteDecision{body, streamThrough, rewrittenIDs, warning}` (no req_id yet).
- `decideRewrite(body, cfg)` + `rewriteObject` (pure; sets rewrittenIDs/warning).
- `newProxyHandler` MODIFIED to: `io.ReadAll(r.Body)` → `dec := decideRewrite(...)`
  → `http.NewRequestWithContext(r.Context(), POST, cfg.Upstream,
  bytes.NewReader(dec.body))` → `copyForwardHeaders` → `outReq.Header.Del(
  "Content-Length")` → Accept fallback → `forward(client, rw, outReq, dec, log)`.
- `forward(client, rw, outReq, dec rewriteDecision, log)` — signature carries
  `dec`; BODY unchanged from T4.S2 (`client.Do` → copy non-hop-by-hop response
  headers → `WriteHeader(resp.StatusCode)` → `io.Copy` → `Flush`).

T1.S1's own PRP states this explicitly (CONSUMER SEAM section):
> "P1.M4.T2.S1 (forward): consumes dec.body (already set as outReq.Body) + the
>  forward mechanics (headers, hop-by-hop strip, Accept fallback, ctx) — these
>  are ALREADY in newProxyHandler/forward from T4.S2; T2.S1 is largely verification."

Cross-checking the item's LOGIC bullets against the post-T1.S1 code:

| Item LOGIC bullet | Post-T1.S2/T1.S1 state | T2.S1 action |
|---|---|---|
| POST to cfg.Upstream w/ decision body | DONE (`bytes.NewReader(dec.body)`) | verify |
| Copy req headers verbatim except hop-by-hop | DONE (`copyForwardHeaders`, denylist) | verify |
| Forward Content-Type/Accept/Auth/Mcp-Session-Id/Accept-Language/User-Agent | DONE (denylist passes all non-hop-by-hop) | verify |
| Accept fallback (`application/json, text/event-stream`) | DONE | verify |
| `outReq.WithContext(r.Context())` | DONE (`NewRequestWithContext`) | verify |
| Send (`client.Do`) | DONE | verify |
| **Non-2xx: log `upstream_error {status, req_id}` but copy through (no synthesize)** | **MISSING** | **IMPLEMENT** |
| Copy resp status + non-hop-by-hop headers verbatim | DONE (forward) | verify |
| Hand `*http.Response` to P1.M4.T2.S2 | seam — forward still `io.Copy` (T2.S2 adds Inject branch) | seam only |

**Conclusion:** the ONLY behavioral code deltas for T2.S1 are (a) threading a
`req_id` for the log and (b) emitting the `upstream_error` line on non-2xx. The
rest is VERIFICATION via httptest + documenting the T2.S2 seam.

## 2. Verified Go / codebase facts (from on-disk source)

### 2a. Non-2xx is ALREADY copied through — only the log is missing
`forward()` does `rw.WriteHeader(resp.StatusCode)` then copies non-hop-by-hop
response headers and `io.Copy`s the body. So a 4xx/5xx from upstream already
reaches the client verbatim. The "do not synthesize" guarantee holds today. The
single gap is: no `upstream_error` log line is emitted for a non-2xx STATUS
(only for `client.Do` error [level "error"] and `io.Copy` error [level "warn"]).
T2.S1 adds the third emission on `resp.StatusCode >= 300`.

### 2b. Logger level semantics (verified in main.go)
`levelNum`: debug=0 < info=1 < warn=2 < error=3. `log.log(level,...)` drops when
`levelNum(level) < l.level`. The existing `newTestProxy` uses
`newLogger(io.Discard, "error")`, which DROPS "warn". Therefore the non-2xx test
MUST inject a buffer-backed logger at level "warn" (or "debug") to observe the
emitted line. Confirmed: `newLogger(w io.Writer, level string)` accepts any
`io.Writer`, so `newLogger(&buf, "warn")` captures the JSON line.

### 2c. Hop-by-hop strip is already bilateral
`isHopByHop` is consulted by BOTH `copyForwardHeaders` (request side) and the
response-header loop in `forward()` (response side). The 9-entry `hopHeaders`
(Connection, Proxy-Connection, Keep-Alive, Proxy-Authenticate,
Proxy-Authorization, Te, Trailer, Transfer-Encoding, Upgrade) matches
external_deps.md §4 / stdlib `httputil` verbatim. "Strip on BOTH request and
response" (item RESEARCH NOTE §4) is already satisfied.

### 2d. Client / Transport already correct (external_deps.md §5)
`newUpstreamClient()`: clones `http.DefaultTransport`, sets
`Transport.ResponseHeaderTimeout = 30s`, leaves `Client.Timeout` ZERO. Matches
the item's "NO Client.Timeout; ResponseHeaderTimeout=30s (already set in
P1.M1.T4.S2)". T2.S1 does NOT touch this.

### 2e. Authorization never read/logged
`copyForwardHeaders` forwards `Authorization` by copying the header value
without inspecting it. `redactHeaders` (main.go) replaces it with `<redacted>`
if ever logged. T2.S1's `upstream_error` log MUST NOT include request headers —
only `{status, req_id}` (PRD §15). Verified: the planned log fields contain no
secret.

## 3. The req_id decision

PRD §15 `upstream_error` event carries `status` + `req_id`. `req_id` = the
JSON-RPC request `id`. T1.S1's `rewriteDecision` tracks only the REWRITTEN ids
(`rewrittenIDs`), not the primary request id. A non-rewritten request (e.g.
`initialize`) can still get a non-2xx and needs its id logged.

**Decision:** T2.S1 EXTENDS `rewriteDecision` with `reqID any` and EXTENDS
`decideRewrite` to capture the first JSON-RPC `id` encountered while it already
iterates the parsed body (additive — does not alter streamThrough/rewrittenIDs/
warning). Numeric ids decode to `float64` (same encoder contract as
`rewrittenIDs`/`Inject`); string ids to `string`; a notification / scalar /
invalid body leaves `reqID` nil (logged as JSON null — acceptable).

Why decideRewrite (not a separate newProxyHandler parse): (1) it already parses
the body — no re-parse; (2) T1.S1's title includes "reqID tracking", so this
completes its stated scope rather than contradicting it; (3) T1.S1's
`TestDecideRewrite` asserts only on streamThrough/rewrittenIDs/warning/body, so
it stays GREEN when reqID is added. `forward()` reads `dec.reqID` for the log.

## 4. Test plan (MOCKING requirement)

Item MOCKING: "httptest.Server fake upstream asserting received headers
(Authorization present, Accept has text/event-stream, Mcp-Session-Id
round-trips)." The existing `proxy_test.go` (T4.S2) already covers most header
assertions (TestPassthrough_HopByHopStripped/AuthorizationForwarded/
AcceptFallback/ResponseHeadersCopied). T2.S1's GENUINELY new tests:

- **Non-2xx copy-through + upstream_error log**: fake upstream returns 503 with a
  body; assert the CLIENT receives 503 + body verbatim (not a synthesized 502)
  AND the captured log buffer contains an `upstream_error` line with the right
  `status` and `req_id`. Requires buffer-backed `warn`-level logger.
- **Mcp-Session-Id round-trip** (item-explicit): client sends `Mcp-Session-Id` →
  assert upstream RECEIVED it AND upstream's response `mcp-session-id` reached the
  CLIENT (full round-trip in one test).
- **reqID threading** (extends TestDecideRewrite or a new subtest): assert
  `dec.reqID == float64(N)` for numeric id, `"abc"` for string id, nil when absent.

Existing TestPassthrough_* MUST stay green (reqID additive; forward body
unchanged except the new non-2xx log, which is dropped by the discard-error
logger those tests use).
