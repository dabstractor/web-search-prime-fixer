name: "P1.M5.T1.S1 — Fake upstream + reusable test harness (initialize SSE, session header, Accept assertion)"
description: |

  OWN and VERIFY a reusable end-to-end test harness for the proxy, consumed by
  P1.M5.T1.S2's five PRD §19.3 cases. The proxy handler (P1.M4.T2.S2 — DONE) and
  the golden testdata fixtures (P1.M3.T2.S1 — DONE) already exist; what is missing
  is a FORMALIZED, CONFIGURABLE fake upstream plus a request-sending helper.

  WHY A NEW FILE, NOT AN EDIT TO proxy_test.go: `proxy_test.go` already holds a
  "seed" harness (`fakeUpstream`, `newTestProxy`, `captureProxy`, `testSID`,
  `initSSE`, `toolsCallSSE`) grown incrementally through M4, and it carries 11
  passing end-to-end tests on top of it. The PARALLEL item P1.M4.T3.S1 (logging)
  creates `proxy_log_test.go` and REUSES those seed helpers BY NAME ("do NOT
  redeclare captureProxy/fakeUpstream/testSID"). Editing or appending to
  `proxy_test.go` now risks a merge collision with that parallel work and churns
  settled tests. So the formalized harness lives in a NEW file
  `proxy_harness_test.go` (package main), which sees all the existing unexported
  helpers (same package) without touching them.

  WHAT THE SEED LACKS (the gaps this item closes, per PRD §19.3 + the item contract):
    - The seed `fakeUpstream` returns the HARDCODED `initSSE` constant, not the
      `testdata/initialize.sse` fixture (they happen to be byte-identical —
      verified — but the contract is "returns canned SSE from testdata").
    - The seed records the request via `*got = *r` (a shallow `*http.Request`
      copy). That captures headers/URL/method but NOT the body — `r.Body` is an
      `io.ReadCloser` consumed by the handler and unreadable from the snapshot.
      PRD §19.3 case 1 ("upstream RECEIVES search_query") and the redaction case
      both need the RECORDED BODY. This item reads + stores the body.
    - There is no "send a JSON-RPC request and capture the client-side SSE
      response + the upstream-received request" helper — every existing test
      hand-rolls `http.Post` / `http.NewRequest`. This item ships `postRPC`.
    - There is no configurable fake: M4 used bespoke `httptest.NewServer(http.HandlerFunc(...))`
      closures per test. T1.S2 needs ONE type that can return initialize.sse OR
      tools_call.sse OR a non-200 status from a single constructor.

  SCOPE BOUNDARY (what this item does NOT do): it does NOT write PRD §19.3 cases
  1-5 (rewrite+warning, canonical passthrough, session round-trip, Auth
  forwarded+redacted, /healthz isolated). Those are P1.M5.T1.S2. Several already
  exist on the seed harness and stay put; T1.S2 adds the remaining gaps (notably
  redaction-in-logs) on TOP of this harness. This item ships ONLY: the harness
  types/helpers + two self-tests proving the harness records correctly and pins
  the item's explicit "Accept contains text/event-stream" assertion.

  MOCKING: the fake upstream IS the mock. No real z.ai call ever happens; the
  proxy's `Config.Upstream` points at the in-process `httptest.Server`.
  **Mode A docs** (PRD §19.3 / §19.4): the harness types + helpers carry doc
  comments naming the PRD §19.3 contract and the fixtures they load.

---

## Goal

**Feature Goal**: A reusable, configurable, testdata-driven fake-upstream harness
exists in `proxy_harness_test.go` and is proven correct by two self-tests. It
records the headers AND body of every request it receives; returns canned SSE
loaded from the `testdata/*.sse` golden fixtures (PRD §19.4); supports a
configurable HTTP status and `Mcp-Session-Id`; and ships a `postRPC` helper that
posts a JSON-RPC body to the proxy with optional extra headers and returns the
real client response (the upstream-received request is read via `recorded()`).
The harness is the substrate P1.M5.T1.S2's five PRD §19.3 end-to-end cases build on.

**Deliverable**: ONE new file `proxy_harness_test.go` (package main, stdlib-only
imports: `bytes`/`encoding/json`/`io`/`net/http`/`net/http/httptest`/`os`/
`strings`/`sync`/`testing`). It adds: `recordedRequest` struct; `fakeMCP` type
(embedding `*httptest.Server`, mutex-guarded recording, settable `sseBody`/
`status`/`session`); `newFakeMCP` + `newFakeMCPInit` constructors; `loadFixture`;
`postRPC`; and two self-tests (`TestHarness_InitializeAndAccept`,
`TestHarness_RecordsBodyAndHeaders`). NO production file is edited. NO existing
test or helper is touched or renamed. `go.mod` unchanged.

**Success Definition**: `go test -run 'TestHarness_' -v` passes and demonstrates:
(1) an `initialize` request sent with no `Accept` reaches the fake upstream with
`Accept` containing `text/event-stream` (the proxy fallback, PRD §8); (2) the
fake records that request's BODY (it contains `"method":"initialize"`); (3) the
client receives status 200, `Content-Type: text/event-stream…`,
`Mcp-Session-Id: <testSID>`, and a body byte-equal to `testdata/initialize.sse`;
(4) a tools/call with an alias + `Authorization` + a client `Mcp-Session-Id`
yields a `recorded()` whose headers carry the forwarded `Authorization` and the
client `Mcp-Session-Id`, and whose body shows the alias RENAMED to
`search_query` before it reached upstream. The full existing suite
(`go test ./...`) stays GREEN (the new file is additive; same package, no
collisions). `go vet ./...` clean. `git diff --stat` shows ONLY
`proxy_harness_test.go`.

## Hard Prerequisites

1. **The proxy handler is DONE** (P1.M4.T2.S1 + P1.M4.T2.S2 — Complete on disk):
   `newProxyHandler(cfg, log, client)` reads the body, `decideRewrite`s it,
   applies the Accept fallback (`proxy.go`: `if outReq.Header.Get("Accept") == ""`
   → `Set("Accept","application/json, text/event-stream")`), strips hop-by-hop,
   forwards via `*http.Client`, and `forward()` copies status+headers and either
   `io.Copy`s (passthrough) or `sse.Inject`s (rewrite). This harness EXERCISES
   that handler end-to-end through a real `http.DefaultClient` round-trip.
2. **The golden fixtures exist** (P1.M3.T2.S1 — Complete): `testdata/initialize.sse`
   (id:1 initialize result, trailing blank line) and `testdata/tools_call.sse`
   (id:2 tools/call result, `isError:false`, `content[0].text` = a stringified
   JSON array). Verified: `testdata/initialize.sse` is byte-identical to the
   `initSSE` constant in `proxy_test.go`.
3. **The seed harness exists and is reused, not replaced** (proxy_test.go):
   `newTestProxy(upstreamURL)` wires the real handler with a discard logger;
   `captureProxy(t, upstreamURL, buf, level)` wires it with a capturing logger;
   `testSID` (`11111111-…`) and `toolsCallSSE(t)` (reads `testdata/tools_call.sse`)
   are the shared constants/helpers. This item CALLS them; it does NOT redeclare
   or edit them (see Parallel Execution constraint below).

## User Persona

**Target User**: **P1.M5.T1.S2** (the next item) — it consumes this harness to
   write PRD §19.3 cases 1-5. Secondary: **P1.M5.T3.S1** (README) documents the
   test layout; the harness's doc comments are the source of truth for "how the
   e2e tests stand up a fake upstream".

**Use Case**: T1.S2 wants to assert "client sends `{"query":"x"}` → upstream
   receives `search_query` → client gets the warning block first" (PRD §19.3
   case 1). It writes:
   `up := newFakeMCP(t, toolsCallSSE(t), testSID); proxy := newTestProxy(up.URL);`
   `resp := postRPC(t, proxy.URL, aliasBody, nil); rec := up.recorded();`
   then asserts `rec.Body` was renamed and decodes `resp.Body` via
   `NewSSEReader`. No bespoke `httptest.NewServer(http.HandlerFunc(...))` closure
   per case.

**Pain Points Addressed**: (1) Without body recording, "upstream receives
   `search_query`" cannot be asserted from the recorded request (the seed only
   has headers). (2) Without a configurable fake, every test re-opened a bespoke
   server closure, duplicating the Content-Type/session-header boilerplate. (3)
   Without `postRPC`, every test re-rolled `http.NewRequest` + header setting.

## Why

- **PRD §19.3 (proxy_test.go httptest end-to-end)** is the exact contract:
  "Stand up a fake upstream `httptest.Server` that returns the `initialize` SSE
  response with a `mcp-session-id` header, and asserts it receives a request with
  `Accept` containing `text/event-stream` … returns a canned `tools/call` SSE
  result." This item delivers that fake upstream + the assertion; the five
  numbered CASES are T1.S2.
- **PRD §19.4 (Golden fixtures)**: tests must not depend on the live z.ai server.
  The harness loads `testdata/*.sse`, so the fake upstream replays captured wire
  format — no network, no flakiness, no credentials.
- **PRD §8 (transport contract)**: the Accept-fallback assertion pins the
  load-bearing "`text/event-stream` token is REQUIRED or z.ai returns empty"
  rule at the integration level — the proxy must add it when the client omits it.
- **Coherence across the chain**: M4 shipped the handler + seed tests
  incrementally; this item formalizes the shared substrate so M5.T1.S2 and the
  final quality gate (M5.T2.S1) have one canonical, configurable fake. It does
  not alter request/response mechanics or any existing assertion.

## What

One new test file. Visible behavior: none (test-only). The harness can stand up
a fake z.ai upstream that returns any `testdata/*.sse` fixture at any status with
any session id, records the full request (headers + body), and is driven by a
single `postRPC` call. Two self-tests prove it.

### Success Criteria

- [ ] `proxy_harness_test.go` exists, `package main`, compiles, imports only
      stdlib (`bytes`/`encoding/json`/`io`/`net/http`/`net/http/httptest`/`os`/
      `strings`/`sync`/`testing`).
- [ ] `recordedRequest{Method, Path, Header, Body}` records Method, URL path,
      a CLONED `http.Header`, and the FULLY-READ body.
- [ ] `fakeMCP` embeds `*httptest.Server`; its handler reads `r.Body`, stores a
      `recordedRequest` under a mutex, then writes `Content-Type:
      text/event-stream;charset=UTF-8` + (if set) `Mcp-Session-Id` + the
      configured status + the `sseBody`.
- [ ] `newFakeMCP(t, sse, session)` returns a started `*fakeMCP` (status defaults
      to 200); `newFakeMCPInit(t)` returns one backed by
      `testdata/initialize.sse` + `testSID` + 200.
- [ ] `loadFixture(t, name)` reads a testdata file (generalizes `toolsCallSSE`).
- [ ] `postRPC(t, proxyURL, body, setHeaders) *http.Response` builds a POST with
      `Content-Type: application/json`, applies `setHeaders` if non-nil, and
      returns the real client response (caller closes `Body`).
- [ ] `TestHarness_InitializeAndAccept` passes: no-`Accept` `initialize` POST →
      `recorded().Header.Get("Accept")` contains `text/event-stream`; recorded
      body contains `"method":"initialize"`; client gets 200,
      `text/event-stream` Content-Type, `Mcp-Session-Id==testSID`, body byte-equal
      to `testdata/initialize.sse`.
- [ ] `TestHarness_RecordsBodyAndHeaders` passes: tools/call with alias +
      `Authorization` + client `Mcp-Session-Id` → `recorded()` has the forwarded
      `Authorization` and client `Mcp-Session-Id`; recorded body's
      `params.arguments` has `search_query` and NOT `query` (proves the body is
      captured POST-rewrite-by-the-proxy, PRE-upstream).
- [ ] `go test ./...` is GREEN; `go vet ./...` clean; `gofmt -l
      proxy_harness_test.go` prints nothing; `git diff --stat` shows ONLY
      `proxy_harness_test.go`; `git diff go.mod` empty.
- [ ] No `.go` file other than `proxy_harness_test.go` is created or edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone
because: (a) the exact types (`recordedRequest`, `fakeMCP`), constructor
signatures, and the `serve`/`recorded`/`postRPC`/`loadFixture` bodies are given
verbatim below; (b) the two self-tests are given with literal request bodies and
assertions; (c) the reused seed helpers (`newTestProxy`, `testSID`,
`toolsCallSSE`, `DefaultConfig`) are named with their file + one-line behavior;
(d) the load-bearing "record the body by `io.ReadAll(r.Body)` BEFORE writing the
response, under a mutex" rule is justified (the seed's `*got = *r` shallow copy
cannot capture the body); (e) the "Accept contains text/event-stream" assertion
is justified (Go's client sets no default Accept → proxy fallback fires); (f) the
parallel-collision constraint (NEW file, do not touch `proxy_test.go` /
`proxy_log_test.go`) is stated with the exact reason.

### Documentation & References

```yaml
# MUST READ — the exact test contract this item implements the fake-upstream half of.
- file: PRD.md
  section: "§19.3 proxy_test.go (httptest end-to-end)" + "§19.4 Golden fixtures"
  why: §19.3 names the fake upstream's job verbatim — "Stand up a fake upstream
        httptest.Server that returns the initialize SSE response with a mcp-session-id
        header, and asserts it receives a request with Accept containing text/event-stream
        … returns a canned tools/call SSE result." §19.4 = the testdata fixtures exist
        so tests do not depend on the live z.ai server. The five numbered CASES in §19.3
        are T1.S2's scope, NOT this item's.
  critical: this item delivers the fake upstream + the Accept assertion (§19.3 preamble)
        and the harness helpers; it does NOT write cases 1-5. Keep that boundary.

# MUST READ — why the Accept assertion holds (the fallback rule).
- file: PRD.md
  section: "§8 Verified transport contract (z.ai upstream) — Request / Accept header"
  why: §8: "Accept: application/json, text/event-stream (the text/event-stream token is
        REQUIRED; z.ai returns empty results without it)". proxy.go implements the fallback:
        if the client omits Accept, the proxy sets exactly that value. Go's http.Post /
        http.DefaultClient.Do sets NO default Accept, so a Content-Type-only request reaches
        the upstream with Accept containing text/event-stream.
  critical: the self-test sends the request with ONLY Content-Type (no Accept) so the
        fallback fires. Do NOT set Accept in postRPC's default — only via setHeaders.

# MUST READ — the handler this harness exercises (real round-trip).
- file: proxy.go
  section: "newProxyHandler (line ~74) + forward (line ~146)"
  why: newProxyHandler reads the body, decideRewrite, applies the Accept fallback
        (line ~96: `if outReq.Header.Get("Accept") == ""`), copies headers minus
        hop-by-hop, and forward() copies status+headers and io.Copy/Injects the body.
        The harness hits this through a real http.DefaultClient round-trip, so ALL of
        that runs — this is why httptest.NewRequest is NOT used (it would skip the
        transport and bypass copyForwardHeaders / the Accept fallback).
  critical: do NOT use httptest.NewRequest+httptest.NewRecorder for the harness; use
        httptest.NewServer + http.DefaultClient (the existing M4 convention).

# MUST READ — the seed harness to REUSE (do NOT redeclare / edit).
- file: proxy_test.go
  why: defines newTestProxy(upstreamURL) [real handler + discard logger], captureProxy
        (t, upstreamURL, buf, level) [capturing logger], testSID, initSSE, toolsCallSSE(t)
        [reads testdata/tools_call.sse], dcCfg, firstArgValue. proxy_harness_test.go is
        package main so it sees all of these. CALL them; do not redefine.
  pattern: newTestProxy(up.URL) is the canonical "wire the real handler at a fake upstream"
        one-liner. Reuse it; do not write a second proxy-wiring helper.
  critical: do NOT edit proxy_test.go. The parallel P1.M4.T3.S1 created proxy_log_test.go
        reusing fakeUpstream/captureProxy/testSID BY NAME; renaming them now breaks it.
        proxy_harness_test.go is ADDITIVE only.

# MUST READ — the SSE reader T1.S2 will use to decode client responses (harness stays compatible).
- file: sse.go
  section: "NewSSEReader (line 59) + Reader.Next (line 66) + Event.Data (line 20)"
  why: T1.S2 decodes the client-side SSE response with `ev, err := NewSSEReader(body).Next()`
        then `json.Unmarshal([]byte(ev.Data), &obj)`. This harness must return SSE bodies
        that NewSSEReader parses — which the testdata fixtures already are (verified format).
        postRPC returns the raw *http.Response; T1.S2 reads + decodes it. No change needed.

# MUST READ — Config + DefaultConfig (how the harness points Upstream at the fake).
- file: config.go
  why: DefaultConfig() returns the production Config (Aliases, TargetParam="search_query",
        etc.). The seed newTestProxy/captureProxy already do `cfg := DefaultConfig();
        cfg.Upstream = upstreamURL`. The harness does NOT build its own Config — it reuses
        newTestProxy/captureProxy, which already wire Upstream correctly.

- url: https://pkg.go.dev/net/http/httptest#NewServer
  why: NewServer(handler) starts a real HTTP listener on a random local port and returns
        a *Server whose .URL is the base to hit. This is the fake upstream.
  critical: the handler closure is where recording + canned-response writing live. The
        server's listener is real, so http.DefaultClient.Do goes through the proxy's real
        transport (copyForwardHeaders, Accept fallback, Mcp-Session-Id forwarding all run).

- url: https://pkg.go.dev/net/http#Request
  why: *http.Request.Body is an io.ReadCloser; a shallow copy (*got = *r) does NOT preserve
        the body for later reading (it is consumed when the handler reads it). To record the
        body, io.ReadAll(r.Body) into []byte INSIDE the handler and store that []byte. Also
        clone r.Header (it is a map; storing the live map races concurrent requests).
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment — UNTOUCHED
  main.go           # logger + bootstrap — UNTOUCHED (newLogger reused)
  config.go         # Config + DefaultConfig — UNTOUCHED
  proxy.go          # newProxyHandler + forward + decideRewrite — UNTOUCHED (exercised, not edited)
  rewrite.go        # Rewrite — UNTOUCHED
  sse.go            # NewSSEReader/Inject/warningText — UNTOUCHED (T1.S2 will call NewSSEReader)
  proxy_test.go     # SEED harness + 11 e2e/unit tests — UNTOUCHED (newTestProxy/testSID/toolsCallSSE reused)
  proxy_log_test.go # (created by parallel P1.M4.T3.S1) — UNTOUCHED
  health_test.go    # /healthz + routing tests — UNTOUCHED
  logger_test.go / config_test.go / resolve_test.go / rewrite_test.go / sse_test.go — UNTOUCHED
  testdata/initialize.sse      # golden fixture (id:1 initialize) — READ by newFakeMCPInit
  testdata/tools_call.sse      # golden fixture (id:2 tools/call) — READ via toolsCallSSE
  testdata/tools_call_multiline.sse — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy_harness_test.go   # CREATE (package main): the formalized, reusable e2e harness.
                        #   - recordedRequest{Method,Path,Header,Body} — snapshot of what the
                        #     fake upstream received (body fully read; header cloned).
                        #   - fakeMCP — *httptest.Server embedding type; configurable sseBody
                        #     (canned SSE from a fixture), status, session id; mutex-guarded
                        #     recording of every received request; serves Content-Type
                        #     text/event-stream + Mcp-Session-Id.
                        #   - newFakeMCP(t, sse, session) / newFakeMCPInit(t) constructors.
                        #   - (f *fakeMCP) recorded() recordedRequest — accessor (copy under lock).
                        #   - loadFixture(t, name) string — reads a testdata file (generalizes toolsCallSSE).
                        #   - postRPC(t, proxyURL, body, setHeaders) *http.Response — the "send a
                        #     JSON-RPC request" helper; real http.DefaultClient round-trip.
                        #   - TestHarness_InitializeAndAccept + TestHarness_RecordsBodyAndHeaders
                        #     (self-tests: Accept assertion + initialize round-trip; body+header recording).
                        #   Imports: bytes/encoding/json/io/net/http/net/http/httptest/os/strings/sync/testing.
```

No other file changes.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — record the body, do not shallow-copy the request. The seed `fakeUpstream`
  does `*got = *r`, a shallow *http.Request copy. That captures Header/URL/Method but
  NOT the body: r.Body is an io.ReadCloser that the handler must read, and once read it
  is consumed. PRD §19.3 case 1 ("upstream receives search_query") and the redaction
  case need the RECORDED BODY. So the fakeMCP handler MUST io.ReadAll(r.Body) into a
  []byte and store that []byte in recordedRequest.Body — BEFORE writing the response.

CRITICAL — clone the header and guard with a mutex. r.Header is a live map; storing the
  reference races concurrent requests (and httptest may reuse). Store r.Header.Clone()
  and guard the recordedRequest swap with a sync.Mutex. recorded() returns a copy under
  the lock so callers can assert without racing a later request.

CRITICAL — NEW file; do NOT touch proxy_test.go or proxy_log_test.go. The parallel
  P1.M4.T3.S1 created proxy_log_test.go and reuses fakeUpstream/captureProxy/testSID/initSSE
  from proxy_test.go BY NAME. Renaming, removing, or behaviorally changing those (e.g.
  making fakeUpstream read a fixture instead of initSSE) risks breaking the parallel
  tests on merge. proxy_harness_test.go is ADDITIVE: it adds fakeMCP/loadFixture/postRPC
  and CALLS the existing newTestProxy/testSID/toolsCallSSE. It does not redeclare them.

CRITICAL — use the REAL listener, not httptest.NewRequest. PRD §19.3 offers both; pick
  "test *http.Client hitting the real listener". httptest.NewRequest + httptest.NewRecorder
  skips the client→proxy→upstream network hop, so the proxy's copyForwardHeaders, the
  Accept fallback, the hop-by-hop strip, and Mcp-Session-Id forwarding would NOT run —
  defeating an end-to-end harness. Every M4 test uses httptest.NewServer + http.Post/
  DefaultClient.Do; follow that. postRPC uses http.DefaultClient.Do.

GOTCHA — Go's http client sets NO default Accept header. http.Post(url, contentType, body)
  sets Content-Type only; http.DefaultClient.Do(req) sets only what you put on req. So a
  request built with Content-Type alone has Accept == "" at the proxy, which triggers the
  Accept fallback (proxy.go: Set "application/json, text/event-stream"). THAT is why the
  self-test's "upstream Accept contains text/event-stream" assertion holds. Do NOT set a
  default Accept inside postRPC — leave it to setHeaders if a test wants one.

GOTCHA — postRPC's setHeaders hook is how T1.S2 adds Authorization / Mcp-Session-Id /
  a custom Accept. Default nil. postRPC ALWAYS sets Content-Type: application/json first,
  then calls setHeaders (so a test can override Content-Type too if it ever needs to).

GOTCHA — the recording is captured at handler ENTRY (before WriteHeader), so it is already
  complete by the time postRPC returns (response headers received). But T1.S2 should still
  fully read/close resp.Body before asserting on side effects (logs, recording) so the
  handler + any log calls fully complete. The self-tests io.ReadAll(resp.Body)+Close.

GOTCHA — do NOT redeclare testSID/initSSE/toolsCallSSE/newTestProxy/captureProxy. They are
  in proxy_test.go, same package. newFakeMCPInit reuses testSID; the tools/call self-test
  reuses toolsCallSSE(t). Declaring a second copy is a compile error (redeclared in same
  package) — another reason the names must stay distinct (fakeMCP, not fakeUpstream).

GOTCHA — fakeMCP must embed *httptest.Server so `up.URL` and `up.Close()` work like the
  seed's return value (callers do `defer up.Close()`). Embed, don't wrap.

GOTCHA — status 0 means 200. If a test sets f.status = 0 (or leaves it), serve() must
  coerce to http.StatusOK (WriteHeader(0) panics). newFakeMCP defaults status to 200.
```

## Implementation Blueprint

### Data models and structure

One snapshot struct, one configurable fake-upstream type, and two small helpers. All
unexported (test files, package main — T1.S2 is the same package and sees them).

```go
// recordedRequest is a deep snapshot of the request the fake upstream received
// (PRD §19.3). Unlike the seed fakeUpstream's shallow `*got = *r` copy (which cannot
// capture the body — r.Body is a consumed io.ReadCloser), this stores the FULLY-READ
// body plus a CLONED header map, so PRD §19.3 case 1 ("upstream receives search_query")
// and the redaction case can assert on what actually reached upstream.
type recordedRequest struct {
	Method string      // always POST for MCP, but recorded for completeness
	Path   string      // r.URL.Path (the proxy forwards regardless of path; recorded anyway)
	Header http.Header // CLONED (live map races concurrent requests)
	Body   []byte      // io.ReadAll(r.Body) — the bytes the proxy forwarded
}

// fakeMCP is a configurable in-process stand-in for the z.ai MCP upstream
// (PRD §19.3 / §19.4, P1.M5.T1.S1). It records every request it receives (headers +
// body, under a mutex) and replies with a canned SSE body loaded from a testdata
// fixture, plus the headers a real z.ai response carries (Content-Type
// text/event-stream + Mcp-Session-Id). It is the formalized harness P1.M5.T1.S2's
// five PRD §19.3 cases build on; it complements (does NOT replace) proxy_test.go's
// seed fakeUpstream, which the existing unit tests and the parallel proxy_log_test.go
// still use.
//
// MOCKING: the fake upstream IS the mock. Config.Upstream points at f.URL; no real
// z.ai call is ever made.
type fakeMCP struct {
	*httptest.Server // embed so up.URL / up.Close() work like the seed

	mu  sync.Mutex
	rec recordedRequest

	sseBody string // canned SSE to return (loaded from a testdata fixture)
	status  int    // HTTP status (0 -> coerced to 200 in serve)
	session string // Mcp-Session-Id response header value ("" -> omit)
}
```

No production types. Imports: stdlib only (see file header).

### Reference implementation (CREATE `proxy_harness_test.go`)

> Run `gofmt -w proxy_harness_test.go` after. Whole file is new; nothing to match.

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
)

// recordedRequest — see Data models above.
type recordedRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// fakeMCP — see Data models above.
type fakeMCP struct {
	*httptest.Server

	mu  sync.Mutex
	rec recordedRequest

	sseBody string
	status  int
	session string
}

// newFakeMCP starts a fake upstream that replies with sse (a canned SSE body,
// typically loaded from a testdata fixture via loadFixture) and the given
// Mcp-Session-Id response header. status defaults to 200 (set f.status before the
// first request to override). Close with defer.
func newFakeMCP(t *testing.T, sse, session string) *fakeMCP {
	t.Helper()
	f := &fakeMCP{sseBody: sse, session: session, status: http.StatusOK}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// newFakeMCPInit starts a fake upstream that returns the initialize SSE loaded from
// testdata/initialize.sse + the canonical testSID (PRD §8 initialize response). This
// is the default z.ai-initialize stand-in T1.S2's session round-trip case builds on.
func newFakeMCPInit(t *testing.T) *fakeMCP {
	t.Helper()
	return newFakeMCP(t, loadFixture(t, "testdata/initialize.sse"), testSID)
}

// serve is the fake upstream handler: record the full request (headers + body), then
// write the canned SSE response with the configured status + session header.
func (f *fakeMCP) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.rec = recordedRequest{
		Method: r.Method,
		Path:   r.URL.Path,
		Header: r.Header.Clone(),
		Body:   body,
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
	if f.session != "" {
		w.Header().Set("Mcp-Session-Id", f.session)
	}
	status := f.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, f.sseBody)
}

// recorded returns a copy of the last request the fake upstream received. Safe to
// call after postRPC returns (the recording is captured at handler entry, before the
// response is written). PRD §19.3 cases assert on this.
func (f *fakeMCP) recorded() recordedRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.rec
}

// loadFixture reads a testdata SSE fixture (relative to the package main test CWD =
// repo root) and returns its contents as a string. Used to feed fakeMCP canned SSE
// from the golden fixtures (PRD §19.4). Generalizes proxy_test.go's toolsCallSSE.
func loadFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return string(b)
}

// postRPC posts a JSON-RPC body to the proxy at proxyURL and returns the real client
// response (PRD §19.3 — "send a JSON-RPC request and capture the client-side SSE
// response"). The caller owns resp.Body (close it). setHeaders, if non-nil, mutates
// the outbound request headers AFTER Content-Type is set (use it to add
// Authorization, Mcp-Session-Id, or a custom Accept). The upstream-received request
// is read via the fake's recorded() — postRPC returns ONLY the client response to
// stay decoupled from the fake type.
//
// CRITICAL: Go's http client sets NO default Accept, so a request built with
// Content-Type alone has Accept == "" at the proxy, which triggers the proxy's
// Accept fallback (proxy.go: "application/json, text/event-stream"). That is the
// behavior TestHarness_InitializeAndAccept asserts on. Do NOT add a default Accept
// here.
func postRPC(t *testing.T, proxyURL, body string, setHeaders func(http.Header)) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, proxyURL, strings.NewReader(body))
	if err != nil {
		t.Fatalf("build rpc request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if setHeaders != nil {
		setHeaders(req.Header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post rpc to %s: %v", proxyURL, err)
	}
	return resp
}

// (H1) Initialize round-trip + Accept assertion (PRD §8/§19.3 preamble). A JSON-RPC
// initialize sent with NO Accept reaches the fake upstream with Accept containing
// text/event-stream (the proxy fallback); the fake records the request BODY (proving
// body recording works); the client receives 200, text/event-stream, Mcp-Session-Id,
// and a body byte-equal to testdata/initialize.sse.
func TestHarness_InitializeAndAccept(t *testing.T) {
	up := newFakeMCPInit(t) // initialize.sse + testSID + 200
	defer up.Close()
	proxy := newTestProxy(up.URL) // reuse the seed: real handler + discard logger
	defer proxy.Close()

	// POST an initialize with Content-Type ONLY (no Accept) -> proxy fallback fires.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","method":"initialize","id":1}`, nil)
	defer resp.Body.Close()

	rec := up.recorded()

	// (a) The item's explicit assertion: upstream Accept contains text/event-stream.
	if accept := rec.Header.Get("Accept"); !strings.Contains(accept, "text/event-stream") {
		t.Errorf("upstream Accept = %q, want it to contain text/event-stream (proxy fallback)", accept)
	}
	// (b) The harness records the BODY (the seed's shallow copy could not).
	if !bytes.Contains(rec.Body, []byte(`"method":"initialize"`)) {
		t.Errorf("recorded upstream body missing initialize method: %q", rec.Body)
	}
	if rec.Method != http.MethodPost {
		t.Errorf("recorded Method = %q, want POST", rec.Method)
	}

	// (c) Client received the initialize SSE verbatim + the session header.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("client status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("client Content-Type = %q, want text/event-stream", ct)
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != testSID {
		t.Errorf("client Mcp-Session-Id = %q, want %q", sid, testSID)
	}
	got, _ := io.ReadAll(resp.Body)
	if want := loadFixture(t, "testdata/initialize.sse"); string(got) != want {
		t.Errorf("client body != initialize.sse fixture:\n got %q\nwant %q", got, want)
	}
}

// (H2) Body + header recording (PRD §19.3 case 1/4 substrate). A tools/call with an
// ALIAS ("query") + Authorization + a client Mcp-Session-Id yields a recorded()
// whose headers carry the forwarded Authorization and the client Mcp-Session-Id, and
// whose body shows the alias RENAMED to search_query before it reached upstream
// (proving the body is captured as the PROXY forwarded it). This is the capability
// T1.S2's rewrite + Auth-forwarded cases assert on.
func TestHarness_RecordsBodyAndHeaders(t *testing.T) {
	up := newFakeMCP(t, toolsCallSSE(t), testSID) // canned tools/call SSE
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		func(h http.Header) {
			h.Set("Authorization", "Bearer secret-token")
			h.Set("Mcp-Session-Id", "client-session-id")
		})
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body) // drain so the handler fully completes

	rec := up.recorded()

	// Header recording: Authorization forwarded verbatim; client Mcp-Session-Id forwarded.
	if got := rec.Header.Get("Authorization"); got != "Bearer secret-token" {
		t.Errorf("recorded Authorization = %q, want forwarded verbatim", got)
	}
	if got := rec.Header.Get("Mcp-Session-Id"); got != "client-session-id" {
		t.Errorf("recorded Mcp-Session-Id = %q, want the client value forwarded", got)
	}

	// Body recording: the alias was RENAMED by the proxy before reaching upstream.
	var obj map[string]any
	if err := json.Unmarshal(rec.Body, &obj); err != nil {
		t.Fatalf("recorded body not valid JSON: %v (%q)", err, rec.Body)
	}
	args, ok := obj["params"].(map[string]any)["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("recorded body missing params.arguments: %#v", obj)
	}
	if _, ok := args["query"]; ok {
		t.Errorf("recorded body still has alias 'query' (proxy should have renamed it): %#v", args)
	}
	if args["search_query"] == nil {
		t.Errorf("recorded body missing 'search_query' (rename did not reach upstream): %#v", args)
	}
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the on-disk state
  - RUN: grep -n "func newTestProxy\|func captureProxy\|const testSID\|func toolsCallSSE\|func newProxyHandler" proxy_test.go proxy.go
  - EXPECT: newTestProxy(upstreamURL), captureProxy(t, upstreamURL, buf, level), testSID,
        toolsCallSSE(t) all present in proxy_test.go; newProxyHandler in proxy.go. These are
        REUSED — do not redeclare.
  - CONFIRM no name collision: grep -n "fakeMCP\|recordedRequest\|loadFixture\|postRPC" *_test.go
        (expect empty — these are the new names).
  - CONFIRM fixtures: ls testdata/initialize.sse testdata/tools_call.sse (both present, M3.T2.S1 DONE).

Task 1: CREATE proxy_harness_test.go scaffolding (package main + imports)
  - FILE: proxy_harness_test.go.
  - IMPORTS: bytes/encoding/json/io/net/http/net/http/httptest/os/strings/sync/testing (ALL stdlib).
  - NAMING: every new symbol lowercase (fakeMCP, recordedRequest, loadFixture, postRPC,
        newFakeMCP, newFakeMCPInit) — distinct from the seed's fakeUpstream/newTestProxy/etc.

Task 2: IMPLEMENT recordedRequest + fakeMCP + serve + recorded + constructors
  - TYPES: recordedRequest{Method,Path,Header,Body}; fakeMCP{*httptest.Server, mu, rec,
        sseBody, status, session}.
  - serve(): io.ReadAll(r.Body) -> store recordedRequest (Header CLONED, under mu) -> set
        Content-Type text/event-stream -> set Mcp-Session-Id if session != "" -> coerce
        status 0 -> 200 -> WriteHeader -> WriteString(sseBody).
  - recorded(): lock, copy, unlock, return.
  - newFakeMCP(t, sse, session): default status 200, httptest.NewServer(http.HandlerFunc(f.serve)).
  - newFakeMCPInit(t): newFakeMCP(t, loadFixture(t,"testdata/initialize.sse"), testSID).
  - EMBED *httptest.Server so up.URL / up.Close() work.

Task 3: IMPLEMENT loadFixture + postRPC
  - loadFixture(t, name): os.ReadFile(name) -> string; t.Fatalf on error. (Mirrors toolsCallSSE.)
  - postRPC(t, proxyURL, body, setHeaders): http.NewRequest POST -> Set Content-Type
        application/json -> if setHeaders != nil { setHeaders(req.Header) } -> DefaultClient.Do
        -> return resp. NO default Accept.

Task 4: WRITE the two self-tests
  - TestHarness_InitializeAndAccept: newFakeMCPInit + newTestProxy(up.URL) + postRPC(initialize,
        nil) -> assert recorded Accept contains text/event-stream; recorded Body contains
        "method":"initialize"; client 200 + text/event-stream + Mcp-Session-Id==testSID + body
        == loadFixture(initialize.sse).
  - TestHarness_RecordsBodyAndHeaders: newFakeMCP(toolsCallSSE, testSID) + newTestProxy +
        postRPC(alias tools/call, setHeaders=Authorization+Mcp-Session-Id) -> assert recorded
        Authorization == "Bearer secret-token"; recorded Mcp-Session-Id == "client-session-id";
        recorded body args has search_query and NOT query.

Task 5: VALIDATE
  - gofmt -w proxy_harness_test.go
  - go vet ./...
  - go test -run 'TestHarness_' -v                 # new self-tests
  - go test -run 'TestPassthrough|TestForward|TestDecideRewrite|TestLog_|TestSSE|TestHealth|TestRouting' -v
                                                   # regressions incl. parallel proxy_log_test.go
  - go test ./...                                  # full suite green
  - git diff --stat   # expect ONLY proxy_harness_test.go
  - git diff go.mod   # expect EMPTY
```

### Implementation Patterns & Key Details

```go
// PATTERN: embed *httptest.Server so the fake is usable exactly like the seed's return
// value — `up := newFakeMCPInit(t); defer up.Close(); proxy := newTestProxy(up.URL)`.
// No wrapper methods needed for URL/Close.

// PATTERN: record by io.ReadAll, not by shallow copy. The seed `fakeUpstream` does
// `*got = *r`; that captures headers but NOT the body (r.Body is a consumed ReadCloser).
// fakeMCP.serve reads the body into []byte and stores it, so PRD §19.3 case 1
// ("upstream receives search_query") is assertable from recorded().Body.

// PATTERN: clone the header + mutex-guard. r.Header is a live map; storing the
// reference races concurrent requests. r.Header.Clone() + sync.Mutex make recorded()
// safe. This also future-proofs against T1.S2 running concurrent cases.

// PATTERN: real-listener round-trip, not httptest.NewRequest. postRPC uses
// http.DefaultClient.Do against the proxy's httptest URL, so the proxy's real
// transport runs (copyForwardHeaders, Accept fallback, hop-by-hop strip, Mcp-Session-Id
// forwarding). httptest.NewRequest+NewRecorder would bypass all of that.

// PATTERN: postRPC returns *http.Response only; the upstream view comes from
// up.recorded(). Decoupling keeps postRPC reusable for cases that hit captureProxy
// (log capture) instead of newTestProxy, and for future fakes. The (resp, recorded())
// pair is the canonical T1.S2 pattern: `resp := postRPC(...); rec := up.recorded()`.

// GOTCHA (restated): Go's client sets no default Accept. A Content-Type-only request
// has Accept == "" at the proxy -> fallback -> upstream gets text/event-stream. That
// is the assertion. Do NOT set a default Accept in postRPC.

// GOTCHA (restated): NEW file, additive. Do not touch proxy_test.go / proxy_log_test.go.
// Do not redeclare testSID/initSSE/toolsCallSSE/newTestProxy/captureProxy (same package
// => compile error if redeclared).

// GOTCHA: coerce status 0 -> 200 in serve (WriteHeader(0) panics). newFakeMCP sets
// status = http.StatusOK by default; the coerce is belt-and-suspenders.
```

### Integration Points

```yaml
FILES CREATED:
  - proxy_harness_test.go  (package main: recordedRequest + fakeMCP + newFakeMCP/
                newFakeMCPInit + serve + recorded + loadFixture + postRPC +
                TestHarness_InitializeAndAccept + TestHarness_RecordsBodyAndHeaders.
                Imports bytes/encoding/json/io/net/http/net/http/httptest/os/strings/
                sync/testing. Reuses newTestProxy/testSID/toolsCallSSE from proxy_test.go.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only).
  - proxy.go / main.go / config.go / doc.go / rewrite.go / sse.go: untouched (exercised,
        not edited).
  - proxy_test.go: untouched (seed harness reused by NAME; parallel proxy_log_test.go
        depends on it unchanged).
  - proxy_log_test.go (parallel P1.M4.T3.S1): untouched.
  - all other *_test.go / testdata/*.sse: untouched.
CONSUMER SEAM (keep stable for T1.S2):
  - newFakeMCP(t, sse, session) / newFakeMCPInit(t): the constructors T1.S2 uses.
  - (f *fakeMCP) recorded(): the upstream-view accessor.
  - postRPC(t, proxyURL, body, setHeaders): the request sender.
  - loadFixture(t, name): the fixture loader.
  - The settable f.status field: how T1.S2 simulates a non-200 upstream.
  - Composes with existing captureProxy (log capture) and newTestProxy (discard logger).
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w proxy_harness_test.go
go vet ./...
git diff --stat     # expect: proxy_harness_test.go ONLY (new file)
git diff go.mod     # expect: EMPTY (zero new requires; stdlib only)

# Expected: gofmt clean; vet clean; only proxy_harness_test.go added; go.mod unchanged.
```

### Level 2: Unit Tests (the harness self-tests)

```bash
# The two self-tests proving the harness + the item's Accept assertion.
go test -run 'TestHarness_' -v

# MUST PASS (prove the contract):
#   TestHarness_InitializeAndAccept     -> no-Accept initialize POST: upstream Accept
#                                          contains text/event-stream; body recorded;
#                                          client 200 + text/event-stream + Mcp-Session-Id
#                                          + body == testdata/initialize.sse.
#   TestHarness_RecordsBodyAndHeaders   -> alias tools/call + Auth + client Mcp-Session-Id:
#                                          recorded Authorization forwarded; recorded
#                                          Mcp-Session-Id forwarded; recorded body has
#                                          search_query (renamed), NOT query.
# Expected: PASS, exit 0. If Accept assertion fails, postRPC set a default Accept (bug) OR
#   the proxy fallback regressed. If body recording is empty, serve() did not io.ReadAll.
#   If recorded body still has "query", you recorded PRE-decideRewrite (record inside the
#   FAKE handler, which runs after the proxy forwarded — the proxy already renamed it).
```

### Level 3: Integration Testing (regression + full suite)

```bash
# Regression: the existing e2e/unit suites + the parallel log tests MUST stay green
# (the new file is additive; same package, no redeclared names, no edited helpers).
go test -run 'TestPassthrough|TestForward|TestDecideRewrite|TestLog_|TestSSE|TestHealth|TestRouting' -v

# Full suite.
go test ./...

# Expected: ALL green, exit 0. If a "redeclared in this block" compile error appears, a
# new symbol collided with the seed (rename it). If TestLog_* fails, proxy_test.go's seed
# helpers were accidentally edited (revert them — do NOT touch proxy_test.go).

# Harness usability smoke check (optional, confirms the seam T1.S2 will use):
go doc . fakeMCP         # (unexported -> may be empty in `go doc`; that's fine for test types)
grep -n "func newFakeMCP\|func postRPC\|func.*recorded\|func loadFixture" proxy_harness_test.go
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Fixture-driven proof: confirm the harness replays the EXACT golden bytes (no drift,
# no live z.ai dependency). Diff the bytes the client received vs. the fixture on disk.
go test -run 'TestHarness_InitializeAndAccept' -v

# Confirm NO real network call is made: the proxy's Config.Upstream is the fake's
# 127.0.0.1:<random-port> URL (httptest.NewServer binds loopback). Grep the wiring:
grep -n "cfg.Upstream = upstreamURL" proxy_test.go   # the seed line newTestProxy/captureProxy use

# Confirm the Accept assertion is the PROXY's doing (not the client's): a request that
# DOES set Accept must be forwarded UNCHANGED (the existing TestPassthrough_AcceptFallback
# pins the provided half; this harness pins the omitted/fallback half):
go test -run 'TestPassthrough_AcceptFallback' -v

# Expected: fixture byte-equality holds; Upstream is loopback (no DNS, no z.ai); the
# fallback assertion passes. The harness is ready for T1.S2.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 clean: `gofmt`, `go vet ./...`, `git diff --stat` (only proxy_harness_test.go),
      `git diff go.mod` (empty).
- [ ] Level 2 passes: `go test -run 'TestHarness_' -v` (both self-tests green).
- [ ] Level 3 passes: `go test ./...` fully green (no regression, no collision with parallel
      proxy_log_test.go).
- [ ] Level 4 passes: client body byte-equal to `testdata/initialize.sse`; Upstream is
      loopback; fallback + provided Accept both behave.

### Feature Validation

- [ ] `fakeMCP` records headers AND body (seed recorded only headers).
- [ ] `fakeMCP` returns canned SSE from a testdata fixture (seed returned a hardcoded constant).
- [ ] `postRPC` sends a JSON-RPC request and returns the real client response.
- [ ] The item's explicit assertion holds: upstream `Accept` contains `text/event-stream`.
- [ ] Initialize round-trip intact: status 200, Content-Type text/event-stream,
      Mcp-Session-Id=testSID, body == `testdata/initialize.sse`.
- [ ] Body recording proves the proxy's rewrite reached upstream (`search_query`, not `query`).

### Code Quality Validation

- [ ] Follows existing conventions: `httptest.NewServer` + real `http.DefaultClient` round-trip
      (matches every M4 test); unexported test symbols; doc comments citing PRD §19.3/§19.4/§8.
- [ ] File placement matches the desired tree (new file `proxy_harness_test.go`, nothing else).
- [ ] Anti-patterns avoided (see below): no `httptest.NewRequest` for the harness, no shallow
      body copy, no edits to `proxy_test.go`, no redeclared seed helpers.
- [ ] No new dependencies (`go.mod` unchanged; stdlib only).

### Documentation & Deployment

- [ ] Doc comments on `recordedRequest`, `fakeMCP`, `newFakeMCP`, `newFakeMCPInit`, `loadFixture`,
      `postRPC` name the PRD §19.3/§19.4 contract and the "fake IS the mock" invariant.
- [ ] The "record body via io.ReadAll, not shallow copy" rationale is documented at `serve`.
- [ ] The "Go sets no default Accept -> proxy fallback" rationale is documented at `postRPC`.
- [ ] **Mode A**: this is test infra — no README change required (P1.M5.T3.S1 owns the doc sweep).

---

## Anti-Patterns to Avoid

- ❌ Don't record the request via a shallow `*http.Request` copy — `r.Body` is a consumed
  `io.ReadCloser`; the body is unreadable later. `io.ReadAll(r.Body)` inside `serve` and store
  the `[]byte`.
- ❌ Don't store the live `r.Header` map reference — clone it (`r.Header.Clone()`) and guard
  with a mutex (concurrent requests race a shared map).
- ❌ Don't use `httptest.NewRequest` + `httptest.NewRecorder` for the harness — it skips the
  client→proxy→upstream transport, so header forwarding / Accept fallback / hop-by-hop strip
  never run. Use `httptest.NewServer` + `http.DefaultClient.Do`.
- ❌ Don't set a default `Accept` in `postRPC` — that would mask the proxy fallback the
  self-test exists to verify. Leave `Accept` to the `setHeaders` hook.
- ❌ Don't edit `proxy_test.go` or `proxy_log_test.go` — the parallel P1.M4.T3.S1 depends on
  the seed helpers unchanged. New file only.
- ❌ Don't redeclare `testSID`/`initSSE`/`toolsCallSSE`/`newTestProxy`/`captureProxy` — they
  are in the same package (`proxy_test.go`); redeclaring is a compile error. Reuse them.
- ❌ Don't write PRD §19.3 cases 1-5 here — that is T1.S2's scope. Ship the harness + its
  self-tests only.
- ❌ Don't forget to coerce `status == 0 -> 200` in `serve` — `WriteHeader(0)` panics.
- ❌ Don't add any third-party dependency — `go.mod` stays stdlib-only.

---

## Confidence Score

**9/10** for one-pass implementation success. The whole deliverable is a single new
test file whose types, helpers, and two self-tests are given verbatim above; every
reused symbol is named with its file; the two load-bearing gotchas (record the body
via `io.ReadAll`, not shallow copy; Go sets no default Accept so the proxy fallback
fires) are justified and pinned by assertions. The only residual risk is a name
collision with the parallel `proxy_log_test.go` (mitigated by the distinct lowercase
names `fakeMCP`/`recordedRequest`/`loadFixture`/`postRPC` and the "do not touch
proxy_test.go" rule, both enforced by Task 0's grep and Level 3's compile + full
suite). Deducted 1 point for that parallel-merge uncertainty.
