# Gap Analysis — P1.M5.T1.S2 (E2E §19.3 cases) vs existing on-disk coverage

## What already exists (seed tests, untouched by this item)

| §19.3 case | Existing seed test | File | What it proves |
|---|---|---|---|
| 1 rewrite+warning | `TestForward_RewriteWarningFirst` | proxy_test.go | upstream gets search_query; client content[0]=warning, [1]=orig, isError=false |
| 2 canonical passthrough | `TestForward_PassthroughByteEqual` | proxy_test.go | client body byte-equal to upstream (no block) |
| 3 session round-trip (1-req) | `TestForward_McpSessionIdRoundTrip` | proxy_test.go | req header→upstream AND resp header→client in ONE request |
| 4 Auth forwarded (half) | `TestPassthrough_AuthorizationForwarded` | proxy_test.go | Authorization reaches upstream — but uses DISCARD logger (no redaction proof) |
| 5 /healthz isolated | `TestRouting_HealthzOnly` | health_test.go | GET /healthz → 200 {ok}, upstream hit-count==0 |

## The genuine GAPS this item adds (on top of the S1 harness)

1. **Case 4 redaction half (THE highlight)** — NO existing test sends Authorization AND
   captures stderr. `TestLog_RewriteEvent` asserts no-secret-in-log but never SENDS an
   Authorization header (so redaction is trivially true). `TestPassthrough_AuthorizationForwarded`
   proves forwarding but with a DISCARD logger. T1.S2 must do BOTH in one test: send
   Authorization, capture stderr at debug, assert (a) header reached upstream unchanged
   AND (b) the secret value + "Bearer" marker are absent from the captured log.

2. **Case 3 two-request flow** — the contract literally says "initialize response reaches
   the client ... AND is then RESENT BY A FOLLOW-UP request (inspect the upstream-received
   Mcp-Session-Id)." The seed `TestForward_McpSessionIdRoundTrip` models a single request;
   T1.S2 models the real two-step MCP session lifecycle (initialize hands out sid →
   follow-up carries sid → upstream records sid).

3. **Case 5 isolation via the harness** — assert the fake upstream's `recorded()` stays the
   ZERO value (its handler never ran), not a separate hit counter.

## Cases that re-prove existing behavior on the formal harness (acceptable overlap)

Cases 1 & 2 already pass on the seed. T1.S2 re-proves them on the **formalized S1 harness**
(`newFakeMCP` + `postRPC` + `recorded()`) as the canonical §19.3 case suite. This is the
whole point of building the harness, and the contract explicitly lists all 5 as T1.S2's
deliverable. Seed tests stay untouched as additional coverage.

## Verified S1 harness API (proxy_harness_test.go, on disk, PASSING)

```
type recordedRequest struct { Method string; Path string; Header http.Header; Body []byte }
type fakeMCP struct { *httptest.Server; ...; sseBody string; status int; session string }
func newFakeMCP(t *testing.T, sse, session string) *fakeMCP
func newFakeMCPInit(t *testing.T) *fakeMCP   // initialize.sse + testSID + 200
func (f *fakeMCP) recorded() recordedRequest // last request (mutex-guarded)
func loadFixture(t *testing.T, name string) string
func postRPC(t *testing.T, proxyURL, body string, setHeaders func(http.Header)) *http.Response
```

## Verified seed helpers reused (proxy_test.go)

```
const testSID = "11111111-1111-1111-1111-111111111111"
func newTestProxy(upstreamURL string) *httptest.Server                       // discard logger
func captureProxy(t, upstreamURL, *bytes.Buffer, level) *httptest.Server     // capturing logger
func toolsCallSSE(t *testing.T) string                                       // reads testdata/tools_call.sse
func jsonToContent0Text(t *testing.T, sse string) string                     // result.content[0].text
```

## Verified production symbols

- `NewSSEReader(r io.Reader) *Reader` + `(*Reader).Next() (Event, error)` + `Event.Data` (sse.go)
- `healthHandler(w, r)`, `var version = "dev"`, `DefaultConfig() Config`, `newLogger(w, level) *logger`,
  `newProxyHandler(cfg, log, client)`, `newUpstreamClient()` (main.go / config.go / proxy.go)

## New-file rationale (do NOT edit proxy_test.go)

- S1 established the additive-file precedent (`proxy_harness_test.go`).
- proxy_test.go's seed helpers + proxy_log_test.go (parallel P1.M4.T3.S1) depend on those
  helpers BY NAME — editing them risks breakage.
- A single coherent §19.3 case suite in `proxy_e2e_test.go` matches the `proxy_*_test.go`
  family naming and keeps the formal harness-driven cases grouped.

## Symbol-name collision check (run on disk)

`grep -rn "TestE2E_\|decodeFirstEvent\|proxy_e2e" *_test.go` → empty. Safe to use
`TestE2E_RewriteAndWarningFirst`, `TestE2E_CanonicalPassthrough`, `TestE2E_SessionRoundTrip`,
`TestE2E_AuthForwardedAndRedacted`, `TestE2E_HealthzIsolated`.

## Full suite state at research time

`go vet ./...` clean; `go test ./...` → `ok web-search-prime-fixer` (green, including the
newly-landed `TestHarness_*` self-tests). No production file is touched by this item.
