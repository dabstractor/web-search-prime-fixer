# P1.M5.T1.S1 — Research Findings

## 1. The seed harness already exists (proxy_test.go)

`proxy_test.go` (package main) already contains a working-but-minimal harness, grown
incrementally through M4:

| Helper | What it does | Gap vs. T1.S1 contract |
|---|---|---|
| `fakeUpstream(t, &got)` | records the request via `*got = *r`; replies `initSSE` + `Mcp-Session-Id` | (a) returns the **hardcoded `initSSE` constant**, NOT `testdata/initialize.sse`; (b) does NOT record the **body** (shallow copy of `*http.Request` — `r.Body` is consumed and unreadable later) |
| `newTestProxy(upstreamURL)` | wires real `newProxyHandler` + `newUpstreamClient` + discard logger | — reused as-is |
| `captureProxy(t, upstreamURL, buf, level)` | same but logger writes to `buf` at `level` | — reused as-is |
| `testSID` / `initSSE` constants | session id + canned SSE | — reused as-is |
| `toolsCallSSE(t)` | reads `testdata/tools_call.sse` | generalizes into `loadFixture` |
| `dcCfg`, `firstArgValue`, `jsonToContent0Text` | decideRewrite-unit helpers | not relevant to harness |

Comment in source verbatim: *"This harness is the seed P1.M5.T1.S1 will formalize."*

## 2. CRITICAL — parallel-execution collision constraint

`P1.M4.T3.S1` (logging) is implemented IN PARALLEL right now. Its PRP:
- CREATES `proxy_log_test.go` (package main) and **reuses `fakeUpstream` / `captureProxy` /
  `testSID` / `initSSE` from `proxy_test.go` BY NAME** ("do NOT redeclare").
- Assumes `fakeUpstream` returns `initSSE` + `testSID` (its tests assert on the LOG, but
  the upstream must still return 200 + initSSE).

=> T1.S1 MUST NOT rename, remove, or behaviorally change the existing seed helpers, and
MUST NOT append to `proxy_test.go` (collision risk with that file's settled state). The
formalized harness goes in a **NEW file** `proxy_harness_test.go`.

## 3. testdata fixture equivalence (verified)

`testdata/initialize.sse` is **byte-identical** to the `initSSE` constant in proxy_test.go
(confirmed via python compare: `const == file: True`, file ends `...\n\n`). So a
fixture-driven fake returning `testdata/initialize.sse` behaves exactly like the seed.
`testdata/tools_call.sse` is a single `data:` line with id:2, `isError:false`, content[0].text
= a stringified JSON array.

## 4. What T1.S1 must ADD (the contract, PRD §19.3 + item desc)

A formalized, reusable harness in a new file:
1. `fakeMCP` — a **configurable** `httptest.Server` returning canned SSE loaded from a
   **testdata fixture** (not a constant), with a settable `status` and `session` header,
   that records **headers AND body** of every received request (mutex-guarded).
2. `recordedRequest` — a snapshot type `{Method, Path, Header, Body}`.
3. `loadFixture(t, path)` — reads a testdata file (generalizes `toolsCallSSE`).
4. `postRPC(t, proxyURL, body, setHeaders) *http.Response` — the "send a JSON-RPC request"
   helper (real `http.DefaultClient` hitting the real httptest listener — NOT
   `httptest.NewRequest`, which bypasses the proxy's header-copy transport).
5. Two self-tests proving the harness + pinning the item's explicit assertion:
   - **Accept assertion**: POST `initialize` with NO Accept → upstream recorded `Accept`
     contains `text/event-stream` (the proxy fallback, PRD §8). PLUS initialize round-trip
     (status 200, Content-Type, Mcp-Session-Id=testSID, body == initialize.sse fixture).
   - **Body+header recording**: POST a tools/call with alias + Authorization + client
     Mcp-Session-Id → recorded headers have them; recorded body shows the alias RENAMED.

## 5. The "Accept contains text/event-stream" assertion — why it holds

Go's `http.Post` / `http.DefaultClient.Do` does **not** set a default `Accept` header. The
proxy (`proxy.go` `newProxyHandler`) applies the fallback when `Accept == ""`:
`outReq.Header.Set("Accept", "application/json, text/event-stream")` (PRD §8). So a request
sent with only `Content-Type` reaches the upstream with `Accept` containing
`text/event-stream`. (Existing `TestPassthrough_AcceptFallback` already pins both the
omitted-fallback and provided-passthrough halves — T1.S1's self-test re-asserts the fallback
half on the formalized harness, proving the harness records headers correctly.)

## 6. httptest.NewRequest is NOT used (design note)

PRD §19.3 offers "httptest.NewRequest + the real handler (or a test *http.Client hitting the
real listener)". We use the **real listener** path (`httptest.NewServer` + `http.DefaultClient`)
because that is the only path that exercises the proxy's real transport: `copyForwardHeaders`,
hop-by-hop stripping, Accept fallback, and `Mcp-Session-Id` forwarding all run inside
`newProxyHandler`/`forward` on a real round-trip. `httptest.NewRequest` + `httptest.NewRecorder`
skips the client→proxy→upstream network hop and would NOT verify header forwarding end-to-end.
The existing M4 tests all use the real-listener path; T1.S1 follows that convention.

## 7. T1.S2 consumption seam (the harness's reason to exist)

T1.S2 ("E2E cases: rewrite+warning, correct-param passthrough, session round-trip, Auth
forwarded+redacted, /healthz isolated", PRD §19.3 cases 1-5) will build on this harness. The
configurable `fakeMCP` lets T1.S2 return `testdata/tools_call.sse` (rewrite/passthrough cases)
or `testdata/initialize.sse` (session case) or a non-200 status (error case) from one type.
`postRPC` + `recorded()` give T1.S2 the (client-response, upstream-request) pair each case
asserts on. Several of those cases already exist on the seed (`TestForward_RewriteWarningFirst`,
`TestForward_PassthroughByteEqual`, `TestForward_McpSessionIdRoundTrip`,
`TestPassthrough_AuthorizationForwarded`, `TestRouting_HealthzOnly`); T1.S2 fills gaps
(notably **redaction in logs** — assert a Bearer token never appears in the captured stderr —
which needs `captureProxy` + `fakeMCP` together). T1.S1 must NOT write those cases (scope
boundary) — only the harness + its self-tests.

## 8. APIs reused (stable, do not redeclare)

- `Config` / `DefaultConfig()` — config.go. `DefaultConfig()` sets Aliases/TargetParam; tests
  override only `Upstream`.
- `newProxyHandler(cfg, log, client) http.HandlerFunc` — proxy.go (the real handler).
- `newUpstreamClient() *http.Client` — proxy.go (Transport w/ 30s ResponseHeaderTimeout, no
  Client.Timeout — SSE-safe).
- `newLogger(w, level) *logger`; `(l).log(level, msg, fields)` — main.go.
- `NewSSEReader(r io.Reader) *Reader`; `(*Reader).Next() (Event, error)`; `Event.Data string`
  — sse.go (T1.S2 will decode client SSE responses with this).
- From proxy_test.go: `testSID`, `initSSE`, `toolsCallSSE`, `newTestProxy`, `captureProxy`.
