name: "P1.M5.T1.S2 — E2E §19.3 cases: rewrite+warning, canonical passthrough, session round-trip, Auth forwarded+redacted, /healthz isolated"
description: |

  OWN and VERIFY the five end-to-end cases of PRD §19.3 (cases 1-5), built on the
  reusable harness shipped by P1.M5.T1.S1 (DONE on disk: `proxy_harness_test.go`).
  Each case maps 1:1 to a PRD §21 success criterion. Together they are the green
  `proxy_*_test.go` suite that proves "all PRD §21 success criteria" end to end
  through a real `http.DefaultClient` round-trip against an in-process fake z.ai
  upstream (no network, no credentials, no flakiness).

  WHY A NEW FILE (`proxy_e2e_test.go`), NOT EDITS TO `proxy_test.go`:
    - S1 established the additive-file precedent (`proxy_harness_test.go`).
    - `proxy_test.go`'s seed helpers (`fakeUpstream`, `newTestProxy`, `captureProxy`,
      `testSID`, `toolsCallSSE`, `jsonToContent0Text`) are reused BY NAME by the
      parallel `proxy_log_test.go` (P1.M4.T3.S1) and by this item. Editing or
      renaming them risks breakage. The new file is ADDITIVE: it CALLS the harness
      and seed helpers (same package), it never redeclares them.
    - A single coherent §19.3 case suite in one file matches the `proxy_*_test.go`
      family naming and keeps the formal harness-driven cases grouped.

  WHAT IS GENUINELY NEW (the gaps this item closes — see research/gap_analysis.md):
    - CASE 4 REDACTION HALF (THE highlight): no existing test both SENDS an
      `Authorization` header AND captures stderr. `TestPassthrough_AuthorizationForwarded`
      proves forwarding but uses a DISCARD logger; `TestLog_RewriteEvent` asserts
      no-secret-in-log but never SENDS an Authorization header (redaction is
      trivially true there). Case 4 does BOTH in one test: send Authorization,
      capture stderr at debug, assert (a) the header reached upstream UNCHANGED
      and (b) the secret value + "Bearer" marker are ABSENT from the log.
    - CASE 3 TWO-REQUEST FLOW: the contract literally requires initialize to HAND
      OUT the session id and a FOLLOW-UP request to RESEND it. The seed
      `TestForward_McpSessionIdRoundTrip` models one request; case 3 models the
      real two-step MCP session lifecycle.
    - CASE 5 isolation via the harness's `recorded()` zero value (no separate hit
      counter).

  CASES 1 & 2 re-prove behavior the seed already covers, on the FORMAL harness —
  the whole point of building it. The seed tests stay untouched as extra coverage.

  SCOPE BOUNDARY: this item writes ONLY `proxy_e2e_test.go` (5 test funcs, package
  main, stdlib imports only). It edits NO production file, NO existing test, NO
  seed helper, and NO `go.mod`. It does NOT add the harness (S1 did). It does NOT
  touch README/docs (P1.M5.T3 owns those). MOCKING: the fake upstream IS the mock.

---

## Goal

**Feature Goal**: Five end-to-end test functions exist in `proxy_e2e_test.go`, each
driven by the P1.M5.T1.S1 harness (`newFakeMCP`/`newFakeMCPInit` + `postRPC` +
`recorded()`) plus the seed helpers (`newTestProxy`, `captureProxy`, `testSID`,
`toolsCallSSE`, `jsonToContent0Text`) and the SSE reader (`NewSSEReader`). Together
they prove, through a real client→proxy→fake-upstream round-trip, every PRD §21
success criterion and all five PRD §19.3 cases.

**Deliverable**: ONE new file `proxy_e2e_test.go` (package main; stdlib-only
imports: `bytes`/`encoding/json`/`io`/`net/http`/`net/http/httptest`/`strings`/
`testing`). Five test functions:
  - `TestE2E_RewriteAndWarningFirst`      (§19.3 case 1 / §21 #1)
  - `TestE2E_CanonicalPassthrough`        (§19.3 case 2 / §21 #2)
  - `TestE2E_SessionRoundTrip`            (§19.3 case 3 / FR-1)
  - `TestE2E_AuthForwardedAndRedacted`    (§19.3 case 4 / §13/§6/§21)
  - `TestE2E_HealthzIsolated`             (§19.3 case 5 / §16)
NO production file edited. NO existing test or helper touched. `go.mod` unchanged.

**Success Definition**: `go test -run 'TestE2E_' -v` passes all five; `go test ./...`
stays fully GREEN (additive file, same package, no redeclared symbols); `go vet ./...`
clean; `gofmt -l proxy_e2e_test.go` prints nothing; `git diff --stat` shows ONLY
`proxy_e2e_test.go`; `git diff go.mod` empty. The five cases collectively assert:
upstream-received renamed param + client warning-first result; canonical-param
byte-equal passthrough; two-request session id round-trip; Authorization forwarded
verbatim AND absent from captured stderr; `/healthz` 200 `{ok:true}` without calling
the upstream.

## Hard Prerequisites

1. **The S1 harness is DONE on disk** (P1.M5.T1.S1 — Complete): `proxy_harness_test.go`
   ships `recordedRequest{Method,Path,Header,Body}`, `fakeMCP` (embeds
   `*httptest.Server`; fields `sseBody`/`status`/`session`), `newFakeMCP(t,sse,session)`,
   `newFakeMCPInit(t)`, `(f *fakeMCP) recorded() recordedRequest`, `loadFixture(t,name)`,
   `postRPC(t,proxyURL,body,setHeaders)`, and the two `TestHarness_*` self-tests (green).
   Verified: `go test -run 'TestHarness_' -v` passes. This item CONSUMES these by name;
   it does not redeclare or edit them.
2. **The proxy handler is DONE** (P1.M4.T2.S1 + P1.M4.T2.S2 — Complete): `newProxyHandler`
   reads the body, `decideRewrite`s it, applies the Accept fallback, strips hop-by-hop,
   forwards via `*http.Client`, and `forward()` copies status+headers and either
   `io.Copy`s (passthrough) or `sse.Inject`s (rewrite). The five cases exercise this
   handler through a real `http.DefaultClient` round-trip.
3. **The seed helpers exist and are reused** (proxy_test.go): `newTestProxy(upstreamURL)`
   [real handler + discard logger], `captureProxy(t, upstreamURL, buf, level)`
   [capturing logger], `const testSID`, `toolsCallSSE(t)` [reads testdata/tools_call.sse],
   `jsonToContent0Text(t, sse)` [returns result.content[0].text]. This item CALLS them;
   same package, so no import needed.
4. **The golden fixtures exist** (P1.M3.T2.S1 — Complete): `testdata/initialize.sse`
   (id:1 initialize; `Mcp-Session-Id`-bearing response body) and `testdata/tools_call.sse`
   (id:2 tools/call; `isError:false`; `content[0].text` = a stringified JSON array).
5. **The redacting logger + routing exist** (P1.M1.T3.S1 + P1.M1.T4 — Complete):
   `newLogger(w, level)` writes JSONL; `healthHandler(w, r)` serves `/healthz` locally;
   `redactHeaders` exists (unit-tested). No log event in `proxy.go` currently includes
   header values — case 4 LOCKS that invariant at the integration level.

## User Persona

**Target User**: **P1.M5.T2.S1** (the final quality gate) — it runs `go vet ./...` +
   `go test ./...` and treats these five cases as the proof that §21 is met. Secondary:
   **P1.M5.T3.S1** (README) documents the test layout; these cases' doc comments are
   the source of truth for "what an end-to-end §19.3 case looks like".

**Use Case**: A maintainer changes the rewrite rule, the SSE injector, or the header
   forwarding. They run `go test -run TestE2E_ -v` and immediately see whether any of:
   warning-first ordering, canonical byte-equality, session round-trip, Auth
   forwarding, Auth redaction, or `/healthz` isolation regressed.

**Pain Points Addressed**: (1) Without case 4's combined forward+redact assertion, a
   regression that adds header logging (e.g. naively enriching `rewriteLogFields`)
   would leak `Authorization` into stderr with no test catching it. (2) Without case
   3's two-request flow, a regression that dropped `Mcp-Session-Id` forwarding on the
   SECOND request of a session would pass the single-request seed test.

## Why

- **PRD §19.3 (proxy_test.go httptest end-to-end)** is the exact contract: the five
  numbered cases. The item ships them as a named suite on the formal harness.
- **PRD §21 (success criteria)** — each case pins one bullet: case 1 ↔ "correct
  results on the first call, one-line correction, no retry"; case 2 ↔ "zero
  difference from z.ai directly"; case 4 ↔ `Authorization` forwarded verbatim +
  never logged (§13/§6); case 5 ↔ `/healthz` does not touch the upstream (§16).
- **PRD §13 (security)**: case 4 is the integration-level proof that `Authorization`
  reaches upstream unchanged and never reaches stderr. The unit-level `redactHeaders`
  test covers the helper; this covers the end-to-end invariant.
- **Coherence across the chain**: M4 shipped the handler + incremental seed tests; S1
  formalized the harness; this item closes the suite by adding the two genuinely-new
  assertions (redaction, two-request session) and re-proving the rest on the harness.
  It changes no production behavior and no existing assertion.

## What

One new test file. Visible behavior: none (test-only). The five cases, each a real
`http.DefaultClient → proxy → fakeMCP` round-trip:

### Success Criteria

- [ ] `proxy_e2e_test.go` exists, `package main`, compiles, imports ONLY stdlib
      (`bytes`/`encoding/json`/`io`/`net/http`/`net/http/httptest`/`strings`/`testing`).
- [ ] `TestE2E_RewriteAndWarningFirst`: client sends `{"query":"x"}` → `recorded()`
      body has `search_query` (not `query`); client SSE result has `content[0]`=warning
      (`[web-search-prime-fixer]…`), `content[1]`=original payload, `isError==false`,
      `id==2`.
- [ ] `TestE2E_CanonicalPassthrough`: client sends `{"search_query":"x"}` →
      `recorded()` body has `search_query=="x"`; client body is BYTE-EQUAL to
      `toolsCallSSE(t)` (no injected block).
- [ ] `TestE2E_SessionRoundTrip`: initialize → client `Mcp-Session-Id`==testSID; a
      follow-up request carrying that id → `recorded()` header
      `Mcp-Session-Id`== the id (two requests over one fake upstream).
- [ ] `TestE2E_AuthForwardedAndRedacted`: aliased tools/call + `Authorization` →
      `recorded()` header `Authorization`== the secret (forwarded); captured stderr
      contains NEITHER the secret value NOR `Bearer`; no decoded JSONL line has an
      `Authorization` field.
- [ ] `TestE2E_HealthzIsolated`: `GET /healthz` → 200, body `{"ok":true,...}`;
      `up.recorded()` is the ZERO value (Method=="", Header==nil, Body==nil) — the
      upstream handler never ran.
- [ ] `go test ./...` GREEN; `go vet ./...` clean; `gofmt -l proxy_e2e_test.go`
      empty; `git diff --stat` shows ONLY `proxy_e2e_test.go`; `git diff go.mod`
      empty. No `.go` file other than `proxy_e2e_test.go` is created or edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact five test function bodies are given verbatim below (the reference
implementation); (b) every consumed symbol (`newFakeMCP`, `newFakeMCPInit`,
`postRPC`, `recorded()`, `loadFixture`, `newTestProxy`, `captureProxy`, `testSID`,
`toolsCallSSE`, `jsonToContent0Text`, `NewSSEReader`, `healthHandler`,
`DefaultConfig`, `newLogger`, `newProxyHandler`, `newUpstreamClient`) is named with
its file and one-line behavior; (c) the load-bearing gotchas (drain resp.Body before
asserting on logs/recording; `recorded()` returns the LAST request so a two-request
flow inspects the follow-up; no log event currently includes headers so the redaction
assertion holds by construction and LOCKS the invariant; Go errors on unused imports
so the import list must be exact) are justified; (d) the new-file rationale and the
"do not touch proxy_test.go" rule are stated with reasons; (e) the §19.3↔case and
case↔§21 mappings are explicit.

### Documentation & References

```yaml
# MUST READ — the exact five-case contract this item implements.
- file: PRD.md
  section: "§19.3 proxy_test.go (httptest end-to-end) — Cases 1-5"
  why: §19.3 lists the five cases verbatim. Cases 1-2 prove §21 #1/#2; case 3 proves
        FR-1 (session preserved end-to-end); case 4 proves §13 (Authorization forwarded
        verbatim, never logged); case 5 proves §16 (/healthz local-only).
  critical: case 4 is the integration-level redaction proof — "Authorization header
        from the client reaches the upstream unchanged and is ABSENT from the stderr
        log capture." Both halves in ONE test.

# MUST READ — the success criteria these cases prove.
- file: PRD.md
  section: "§21 Success criteria"
  why: each case maps 1:1 to a §21 bullet. Case 1 ↔ "correct results first call, one-line
        correction, no retry"; case 2 ↔ "zero difference from z.ai directly (no warning,
        identical results)"; the stdlib-only binary criterion is proven by the build gate.

# MUST READ — the security/logging contract case 4 locks.
- file: PRD.md
  section: "§13 Headers, credentials, security" + "§6 Non-functional (No secrets in logs)"
  why: §13 "Forward Authorization verbatim. Never read, log, or store its value." §6
        "The Authorization header is never logged." Case 4 sends Authorization, captures
        stderr at debug (so rewrite + forward lines fire), and asserts the secret + the
        "Bearer" marker never appear. proxy.go's log events carry NO header values by
        construction (rewriteLogFields is documented "NEVER include header values"); this
        test is the regression guard if a future change adds header context.

# MUST READ — the harness this item builds on (DONE on disk).
- file: proxy_harness_test.go
  section: "recordedRequest + fakeMCP + newFakeMCP/newFakeMCPInit + serve + recorded + loadFixture + postRPC"
  why: the formal fake upstream + the request sender. newFakeMCP(t, toolsCallSSE(t), testSID)
        returns a fake that replays the canned tools/call SSE; newFakeMCPInit(t) returns one
        that replays initialize.sse + testSID. postRPC(t, url, body, setHeaders) does the real
        round-trip; up.recorded() returns the LAST request the fake saw (headers CLONED, body
        fully read). REUSE these — do not redeclare.
  critical: recorded() returns the LAST request. In the two-request session case, after the
        follow-up it holds the follow-up's headers — that is what case 3 inspects. In case 5,
        if /healthz were forwarded, recorded() would be non-zero; the zero value IS the
        isolation proof.

# MUST READ — the seed helpers reused (do not redeclare / edit).
- file: proxy_test.go
  why: newTestProxy(up.URL) wires the real handler + DISCARD logger (cases 1,2,3,5).
        captureProxy(t, up.URL, &buf, "debug") wires it with a CAPTURING logger at debug
        (case 4 — maximizes the log surface a leak could land in). toolsCallSSE(t) reads
        testdata/tools_call.sse. jsonToContent0Text(t, sse) returns result.content[0].text.
        testSID is the canonical session id.
  critical: SAME PACKAGE — redeclaring any of these is a compile error. Do not edit
        proxy_test.go (parallel proxy_log_test.go depends on these unchanged).

# MUST READ — the SSE reader used to decode client responses.
- file: sse.go
  section: "NewSSEReader (line ~59) + Reader.Next (line ~66) + Event.Data (line ~20)"
  why: case 1 decodes the client SSE response with `ev, err := NewSSEReader(bytes.NewReader(body)).Next()`
        then `json.Unmarshal([]byte(ev.Data), &obj)`. The fake replays the golden fixtures,
        which are already valid SSE — NewSSEReader parses them.

# MUST READ — the handler + the log call sites case 4 exercises.
- file: proxy.go
  section: "newProxyHandler (rewrite log at `if !dec.streamThrough`) + forward (forward debug log)"
  why: case 4 sends an ALIASED tools/call at debug, so BOTH the info "rewrite" line AND the
        debug "forward" line fire (buf has ≥2 lines). Neither logs headers; case 4 asserts
        the secret stays out of both.
  critical: the redaction assertion passes TODAY because no log event includes headers — it
        is a regression guard, not a test of redactHeaders (that is unit-tested in logger_test.go).

# MUST READ — /healthz + the routing mux case 5 rebuilds.
- file: main.go
  section: "healthHandler + main()'s mux (HandleFunc /healthz + /)"
  why: case 5 builds the SAME mux main() builds and asserts GET /healthz → 200 {ok:true}
        WITHOUT the upstream handler running (recorded() stays zero). newLogger(io.Discard,"error")
        + newUpstreamClient() mirror main()'s wiring.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod                  # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go                  # package main comment — UNTOUCHED
  main.go                 # logger + bootstrap + healthHandler + routing — UNTOUCHED
  config.go               # Config + DefaultConfig — UNTOUCHED
  proxy.go                # newProxyHandler + forward + decideRewrite + log call sites — UNTOUCHED (exercised)
  rewrite.go              # Rewrite — UNTOUCHED
  sse.go                  # NewSSEReader/Inject/warningText — UNTOUCHED (NewSSEReader called)
  proxy_test.go           # SEED: fakeUpstream/newTestProxy/captureProxy/testSID/toolsCallSSE/jsonToContent0Text + 11 tests — UNTOUCHED (helpers REUSED)
  proxy_harness_test.go   # (S1) recordedRequest/fakeMCP/newFakeMCP/newFakeMCPInit/postRPC/loadFixture + TestHarness_* — UNTOUCHED (REUSED)
  proxy_log_test.go       # (parallel P1.M4.T3.S1) log-event tests — UNTOUCHED
  health_test.go          # /healthz + routing tests — UNTOUCHED
  logger_test.go / config_test.go / resolve_test.go / rewrite_test.go / sse_test.go — UNTOUCHED
  testdata/initialize.sse      # golden fixture (id:1 initialize) — READ via newFakeMCPInit
  testdata/tools_call.sse      # golden fixture (id:2 tools/call) — READ via toolsCallSSE
  testdata/tools_call_multiline.sse — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy_e2e_test.go   # CREATE (package main): the formal §19.3 end-to-end case suite.
                    #   - TestE2E_RewriteAndWarningFirst   §19.3 case 1 / §21 #1:
                    #     alias "query" -> upstream receives search_query; client SSE
                    #     result has the warning block at content[0], original at [1],
                    #     isError==false, id==2.
                    #   - TestE2E_CanonicalPassthrough     §19.3 case 2 / §21 #2:
                    #     canonical "search_query" -> upstream unchanged; client body
                    #     BYTE-EQUAL to the canned upstream payload (no block).
                    #   - TestE2E_SessionRoundTrip         §19.3 case 3 / FR-1:
                    #     initialize -> client Mcp-Session-Id==testSID; follow-up
                    #     carrying it -> upstream recorded() header Mcp-Session-Id==it.
                    #   - TestE2E_AuthForwardedAndRedacted  §19.3 case 4 / §13/§6:
                    #     aliased tools/call + Authorization -> upstream received it
                    #     verbatim; captured stderr has NEITHER the secret NOR "Bearer";
                    #     no JSONL line has an Authorization field.
                    #   - TestE2E_HealthzIsolated           §19.3 case 5 / §16:
                    #     GET /healthz -> 200 {"ok":true}; up.recorded() is the zero
                    #     value (upstream handler never ran).
                    #   Imports: bytes/encoding/json/io/net/http/net/http/httptest/strings/testing.
                    #   Reuses: newFakeMCP/newFakeMCPInit/postRPC/recorded (harness);
                    #           newTestProxy/captureProxy/testSID/toolsCallSSE/jsonToContent0Text (seed);
                    #           NewSSEReader (sse.go); healthHandler/DefaultConfig/newLogger/newProxyHandler/newUpstreamClient (prod).
```

No other file changes.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — drain resp.Body before asserting on side effects. The proxy's log calls
  (rewrite/forward/upstream_error) and the fake's recording happen DURING the handler.
  postRPC returns as soon as the response HEADERS are received; the body may still be
  streaming. io.ReadAll(resp.Body)+Close ensures the handler fully completes before you
  assert on the captured stderr (case 4) or recorded() (cases 1-3). The seed does this;
  mirror it.

CRITICAL — recorded() returns the LAST request. fakeMCP.overwrites rec on every request
  (it does not append). In the two-request session case, after the follow-up postRPC,
  recorded() holds the FOLLOW-UP's headers — exactly what case 3 inspects. Do NOT read
  recorded() between the two requests expecting the initialize snapshot.

CRITICAL — NEW file; do NOT touch proxy_test.go or proxy_harness_test.go. Both ship
  helpers this item reuses (same package). Redeclaring any of testSID/initSSE/toolsCallSSE/
  newTestProxy/captureProxy/jsonToContent0Text/fakeMCP/newFakeMCP/newFakeMCPInit/postRPC/
  recordedRequest/loadFixture is a COMPILE ERROR (redeclared in this block). proxy_e2e_test.go
  is ADDITIVE: it only defines the five TestE2E_* funcs.

CRITICAL — Go errors on UNUSED IMPORTS (and unused locals). The import list MUST be
  exactly: bytes, encoding/json, io, net/http, net/http/httptest, strings, testing.
  Verify each is used: bytes (NewSSEReader(bytes.NewReader…), bytes.Contains, bytes.Split);
  encoding/json (Unmarshal); io (io.ReadAll, io.Discard); net/http (http.Header in setHeaders,
  http.NewServeMux, http.Get, http.CanonicalHeaderKey); net/http/httptest (httptest.NewServer);
  strings (strings.HasPrefix); testing. If you drop an assertion that used an import, drop the
  import too, or the build breaks.

CRITICAL — case 4 MUST use captureProxy at "debug" + an ALIASED request. "debug" emits the
  forward line; the alias fires the rewrite line — together they maximize the log surface a
  leaked Authorization could land in (the strongest possible redaction proof). A discard logger
  (newTestProxy) would make the redaction assertion vacuous.

GOTCHA — the redaction assertion holds by CONSTRUCTION today (no log event includes headers),
  so it is a REGRESSION GUARD, not a test of redactHeaders. redactHeaders is unit-tested in
  logger_test.go (TestRedactHeaders / TestRedactHeaders_CaseInsensitiveAndNonMutating). Case 4
  is the integration-level complement: a real round-trip + captured stderr.

GOTCHA — case 5 rebuilds main()'s mux (HandleFunc /healthz + /). newTestProxy only wires the
  "/" proxy handler; it does NOT register /healthz. So case 5 builds its own mux exactly like
  TestRouting_HealthzOnly (health_test.go) does, but points cfg.Upstream at a fakeMCP and
  asserts recorded() is the zero value (no separate hit counter needed).

GOTCHA — postRPC sets Content-Type: application/json and lets setHeaders add the rest. Go's
  http client sets NO default Accept, so the proxy's Accept fallback fires for cases 1-4
  (harmless — we do not assert on Accept here; TestHarness_InitializeAndAccept already pins it).

GOTCHA — type assertions on decoded JSON mirror the seed convention (no nil-guard). result :=
  res["result"].(map[string]any); content := result["content"].([]any). The golden fixtures are
  verified shapes; these assertions match TestForward_RewriteWarningFirst exactly. Do not add
  defensive guards the seed does not have — keep the suite stylistically consistent.
```

## Implementation Blueprint

### Data models and structure

No new types. The file defines only five `func TestE2E_*` test functions. All consumed
types (`recordedRequest`, `fakeMCP`, `http.Header`, `map[string]any`, SSE `Event`) come
from the harness, stdlib, or `sse.go`. Imports: stdlib only (see the import gotcha).

### Reference implementation (CREATE `proxy_e2e_test.go`)

> Run `gofmt -w proxy_e2e_test.go` after. Whole file is new; nothing to match. Every
> consumed symbol is verified on disk (see Hard Prerequisites + Documentation & References).

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The five PRD §19.3 end-to-end cases (P1.M5.T1.S2), built on the P1.M5.T1.S1
// harness (proxy_harness_test.go: newFakeMCP/newFakeMCPInit/postRPC/recorded) and
// the seed helpers (proxy_test.go: newTestProxy/captureProxy/testSID/toolsCallSSE/
// jsonToContent0Text). Each case is a real http.DefaultClient -> proxy -> fakeMCP
// round-trip; no real z.ai call is ever made (the fake IS the mock).
//
// Cases 1-2 re-prove behavior the seed covers (TestForward_RewriteWarningFirst /
// TestForward_PassthroughByteEqual) on the FORMAL harness; case 3 adds the real
// two-request session lifecycle; case 4 is the combined forward+redact proof;
// case 5 proves /healthz isolation via the harness's recorded() zero value.

// (E1) §19.3 case 1 / §21 #1: client sends {"query":"x"} -> upstream receives
// search_query; the client SSE result has the warning text block FIRST then the
// original payload (one-line correction, no retry). isError stays false (FR-3).
func TestE2E_RewriteAndWarningFirst(t *testing.T) {
	up := newFakeMCP(t, toolsCallSSE(t), testSID) // canned id:2 tools/call result
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Alias "query" -> proxy renames to search_query (rewrittenIDs={float64(2)}).
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		nil)
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// UPSTREAM side: the alias reached upstream renamed to search_query.
	rec := up.recorded()
	var req map[string]any
	if err := json.Unmarshal(rec.Body, &req); err != nil {
		t.Fatalf("recorded upstream body not JSON: %v (%q)", err, rec.Body)
	}
	args := req["params"].(map[string]any)["arguments"].(map[string]any)
	if _, ok := args["query"]; ok {
		t.Errorf("upstream received alias 'query' (should be renamed): %#v", args)
	}
	if got := args["search_query"]; got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\"", got)
	}

	// CLIENT side: one SSE event; result.content[0]=warning, [1]=original; isError=false.
	ev, err := NewSSEReader(bytes.NewReader(body)).Next()
	if err != nil {
		t.Fatalf("client response not a decodable SSE event: %v\n%s", err, body)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &res); err != nil {
		t.Fatalf("client event Data not JSON: %v\n%s", err, ev.Data)
	}
	if res["id"] != float64(2) {
		t.Errorf("event id = %#v, want float64(2)", res["id"])
	}
	result := res["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError = true, want false (FR-3: proxy never sets isError)")
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2 (warning + original)", len(content))
	}
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" {
		t.Errorf("content[0].type = %#v, want text", c0["type"])
	}
	warn, _ := c0["text"].(string)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[0].text = %q, want the warning marker first", warn)
	}
	// content[1] preserves the original stringified-array payload (fixture content[0]).
	orig := jsonToContent0Text(t, toolsCallSSE(t))
	if c1 := content[1].(map[string]any)["text"].(string); c1 != orig {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1, orig)
	}
}

// (E2) §19.3 case 2 / §21 #2: client sends {"search_query":"x"} -> upstream
// receives it unchanged; the client body is BYTE-EQUAL to the upstream payload
// (no injected block). Proves zero-overhead passthrough (identical results,
// identical schema, no warning).
func TestE2E_CanonicalPassthrough(t *testing.T) {
	want := toolsCallSSE(t) // the exact bytes the client must receive
	up := newFakeMCP(t, want, testSID)
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Canonical param -> decideRewrite streamThrough=true -> io.Copy verbatim.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
		nil)
	defer resp.Body.Close()

	// UPSTREAM: search_query received unchanged.
	rec := up.recorded()
	var req map[string]any
	if err := json.Unmarshal(rec.Body, &req); err != nil {
		t.Fatalf("recorded upstream body not JSON: %v (%q)", err, rec.Body)
	}
	args := req["params"].(map[string]any)["arguments"].(map[string]any)
	if got := args["search_query"]; got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (unchanged)", got)
	}

	// CLIENT: byte-equal to the upstream payload — no injected warning block.
	got, _ := io.ReadAll(resp.Body)
	if string(got) != want {
		t.Errorf("client body not byte-equal to upstream (no block should be added):\n got %q\nwant %q", got, want)
	}
}

// (E3) §19.3 case 3 / FR-1: the initialize response reaches the client with the
// mcp-session-id header intact, and a FOLLOW-UP request resends that same
// Mcp-Session-Id to the upstream (inspect the upstream-received header). Two
// requests over one fake upstream: the id handed out on initialize is the one
// carried on the follow-up.
func TestE2E_SessionRoundTrip(t *testing.T) {
	up := newFakeMCPInit(t) // initialize.sse + Mcp-Session-Id=testSID on every response
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// (1) initialize: client receives the upstream's Mcp-Session-Id response header.
	resp1 := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","method":"initialize","id":1}`, nil)
	defer resp1.Body.Close()
	sid := resp1.Header.Get("Mcp-Session-Id")
	if sid != testSID {
		t.Fatalf("initialize client Mcp-Session-Id = %q, want %q (header must reach client intact)", sid, testSID)
	}
	_, _ = io.ReadAll(resp1.Body) // drain so the handler fully completes

	// (2) follow-up request resends the session id; upstream RECEIVES it verbatim.
	resp2 := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
		func(h http.Header) { h.Set("Mcp-Session-Id", sid) })
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)

	rec := up.recorded() // the LAST request == the follow-up
	if got := rec.Header.Get("Mcp-Session-Id"); got != sid {
		t.Errorf("follow-up upstream Mcp-Session-Id = %q, want %q (resend verbatim)", got, sid)
	}
}

// (E4) §19.3 case 4 / §13/§6/§21: the Authorization header reaches the upstream
// UNCHANGED and is ABSENT from the captured stderr log. Uses captureProxy at debug
// + an aliased tools/call so BOTH the rewrite and forward log lines fire (the
// largest log surface a leaked secret could land in). proxy.go's log events carry
// NO header values by construction; this locks that invariant at the integration level.
func TestE2E_AuthForwardedAndRedacted(t *testing.T) {
	const secret = "Bearer test-secret-hunter2-token"
	up := newFakeMCP(t, toolsCallSSE(t), testSID)
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug") // debug -> rewrite + forward lines fire
	defer proxy.Close()

	// Aliased tools/call WITH Authorization -> rewrite path logs; header forwarded.
	resp := postRPC(t, proxy.URL,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"query":"x"}}}`,
		func(h http.Header) { h.Set("Authorization", secret) })
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body) // drain so all log calls complete

	// (a) FORWARD: the Authorization header reached upstream verbatim.
	rec := up.recorded()
	if got := rec.Header.Get("Authorization"); got != secret {
		t.Errorf("upstream Authorization = %q, want forwarded verbatim", got)
	}

	// (b) REDACT: the secret value (and the Bearer marker) never appear in stderr.
	if bytes.Contains(buf.Bytes(), []byte(secret)) {
		t.Errorf("secret token leaked into the log:\n%s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("Bearer")) {
		t.Errorf("a Bearer marker leaked into the log (Authorization value logged):\n%s", buf.String())
	}
	// Belt-and-suspenders: no decoded JSONL line carries an Authorization field.
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		for k := range m {
			if http.CanonicalHeaderKey(k) == "Authorization" {
				t.Errorf("log line carries an Authorization field: %s", line)
			}
		}
	}
}

// (E5) §19.3 case 5 / §16: GET /healthz returns 200 {"ok":true,...} and does NOT
// call the upstream. Built on the SAME mux main() builds; the fake upstream's
// recorded() stays the ZERO value (its handler never ran) -> isolation proof via
// the harness, no separate hit counter.
func TestE2E_HealthzIsolated(t *testing.T) {
	up := newFakeMCPInit(t) // would record any request if /healthz leaked through
	defer up.Close()
	cfg := DefaultConfig()
	cfg.Upstream = up.URL
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", newProxyHandler(cfg, newLogger(io.Discard, "error"), newUpstreamClient()))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("/healthz body not JSON: %v (raw=%q)", err, raw)
	}
	if body["ok"] != true {
		t.Errorf("/healthz ok = %#v, want true", body["ok"])
	}

	// ISOLATION: the upstream handler never ran -> recorded() is the zero value.
	rec := up.recorded()
	if rec.Method != "" || rec.Header != nil || rec.Body != nil {
		t.Errorf("/healthz called the upstream (should be local-only): recorded=%+v", rec)
	}
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the on-disk state
  - RUN: grep -n "func newFakeMCP\|func newFakeMCPInit\|func postRPC\|func.*recorded\|func loadFixture" proxy_harness_test.go
  - EXPECT: all present (S1 DONE). RUN: grep -n "func newTestProxy\|func captureProxy\|const testSID\|func toolsCallSSE\|func jsonToContent0Text" proxy_test.go
  - EXPECT: all present (seed). CONFIRM no collision: grep -rn "TestE2E_\|proxy_e2e" *_test.go (expect empty).
  - CONFIRM fixtures + green baseline: ls testdata/initialize.sse testdata/tools_call.sse && go test ./... (ok).

Task 1: CREATE proxy_e2e_test.go scaffolding
  - FILE: proxy_e2e_test.go, package main.
  - IMPORTS (exact — Go errors on unused): bytes/encoding/json/io/net/http/net/http/httptest/strings/testing.
  - Doc comment naming the five §19.3 cases + the harness/seed reuse.

Task 2: WRITE TestE2E_RewriteAndWarningFirst (case 1)
  - newFakeMCP(t, toolsCallSSE(t), testSID) + newTestProxy(up.URL).
  - postRPC alias tools/call {"query":"x"} id:2; io.ReadAll(resp.Body).
  - Assert recorded().Body decoded args has search_query=="x" and NOT query.
  - Assert NewSSEReader(body).Next() → id==float64(2); result.isError==false;
        content len==2; content[0].type=="text" + HasPrefix "[web-search-prime-fixer]";
        content[1].text == jsonToContent0Text(t, toolsCallSSE(t)).

Task 3: WRITE TestE2E_CanonicalPassthrough (case 2)
  - want := toolsCallSSE(t); newFakeMCP(t, want, testSID) + newTestProxy(up.URL).
  - postRPC canonical {"search_query":"x"} id:2.
  - Assert recorded().Body args search_query=="x"; client body string(got)==want (byte-equal).

Task 4: WRITE TestE2E_SessionRoundTrip (case 3)
  - newFakeMCPInit(t) + newTestProxy(up.URL).
  - (1) postRPC initialize, nil → resp1.Header Mcp-Session-Id==testSID; save sid; drain.
  - (2) postRPC tools/call, setHeaders Mcp-Session-Id=sid → drain; recorded().Header
        Mcp-Session-Id==sid.

Task 5: WRITE TestE2E_AuthForwardedAndRedacted (case 4 — the highlight)
  - const secret = "Bearer test-secret-hunter2-token"; newFakeMCP(t, toolsCallSSE(t), testSID).
  - var buf bytes.Buffer; captureProxy(t, up.URL, &buf, "debug").
  - postRPC ALIASED tools/call, setHeaders Authorization=secret; drain.
  - Assert recorded().Header Authorization==secret (forwarded).
  - Assert !bytes.Contains(buf, secret) AND !bytes.Contains(buf, "Bearer") (redacted).
  - Assert no decoded JSONL line has a canonical "Authorization" key.

Task 6: WRITE TestE2E_HealthzIsolated (case 5)
  - newFakeMCPInit(t); cfg := DefaultConfig(); cfg.Upstream = up.URL.
  - Build main()'s mux: mux.HandleFunc("/healthz", healthHandler); mux.HandleFunc("/", newProxyHandler(cfg, newLogger(io.Discard,"error"), newUpstreamClient())); httptest.NewServer(mux).
  - http.Get(ts.URL+"/healthz") → 200; body ok==true.
  - Assert up.recorded() is zero: Method=="" && Header==nil && Body==nil.

Task 7: VALIDATE
  - gofmt -w proxy_e2e_test.go
  - go vet ./...
  - go test -run 'TestE2E_' -v     # the five new cases
  - go test ./...                  # full suite green (additive; no redeclared symbols)
  - git diff --stat               # expect ONLY proxy_e2e_test.go
  - git diff go.mod               # expect EMPTY
```

### Implementation Patterns & Key Details

```go
// PATTERN: one fake + one proxy per case via the harness one-liners:
//   up := newFakeMCP(t, toolsCallSSE(t), testSID); defer up.Close()
//   proxy := newTestProxy(up.URL); defer proxy.Close()
//   resp := postRPC(t, proxy.URL, body, setHeaders)
//   rec := up.recorded()   // the upstream's view of what the proxy forwarded

// PATTERN: (resp, recorded()) pair. postRPC returns ONLY the client *http.Response;
// the upstream view comes from up.recorded(). This mirrors the S1 harness contract
// and the seed TestForward_RewriteWarningFirst / TestPassthrough_AuthorizationForwarded.

// PATTERN: decode the client SSE with NewSSEReader(body).Next() + json.Unmarshal
// (case 1). Matches the seed TestForward_RewriteWarningFirst exactly — same type
// assertions, same fixture-driven expectations.

// PATTERN: redaction via captured stderr. captureProxy(t, up.URL, &buf, level) wires
// the capturing logger (the seed helper); at "debug" the forward line fires too. Assert
// the secret + "Bearer" are absent from buf (case 4). This is the integration complement
// to the unit-level TestRedactHeaders in logger_test.go.

// GOTCHA (restated): drain resp.Body (io.ReadAll + Close) BEFORE asserting on buf or
// recorded() — the handler/log calls complete during the body read.

// GOTCHA (restated): recorded() is the LAST request; the two-request session case reads
// it AFTER the follow-up.

// GOTCHA (restated): NEW file, additive. Do not touch proxy_test.go / proxy_harness_test.go.
// Do not redeclare any reused symbol (same package => compile error).

// GOTCHA (restated): exact import list — drop an import if you drop the assertion that
// used it, or the build breaks.
```

### Integration Points

```yaml
FILES CREATED:
  - proxy_e2e_test.go  (package main: the five TestE2E_* funcs. Imports
        bytes/encoding/json/io/net/http/net/http/httptest/strings/testing. Reuses the
        S1 harness + seed helpers + NewSSEReader + healthHandler/DefaultConfig/newLogger/
        newProxyHandler/newUpstreamClient.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only).
  - all production .go (main.go/config.go/proxy.go/rewrite.go/sse.go/doc.go): untouched.
  - proxy_test.go / proxy_harness_test.go / proxy_log_test.go / health_test.go / all
    other *_test.go / testdata/*.sse: untouched.
CONSUMER SEAM (keep stable for P1.M5.T2.S1 / P1.M5.T3.S1):
  - The five TestE2E_* names — `go test -run TestE2E_` is the "§21 proof" selector the
    quality gate and README will reference.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w proxy_e2e_test.go
go vet ./...
git diff --stat     # expect: proxy_e2e_test.go ONLY (new file)
git diff go.mod     # expect: EMPTY (zero new requires; stdlib only)

# Expected: gofmt clean; vet clean; only proxy_e2e_test.go added; go.mod unchanged.
# If vet reports "declared and not used" or "imported and not used", an import/local
# drifted — trim it (see the import gotcha).
```

### Level 2: The five cases

```bash
go test -run 'TestE2E_' -v

# MUST PASS (prove the §19.3 / §21 contract):
#   TestE2E_RewriteAndWarningFirst   -> upstream received search_query; client content[0]
#                                       = warning, [1] = original, isError=false, id=2.
#   TestE2E_CanonicalPassthrough     -> upstream received search_query=="x"; client body
#                                       byte-equal to toolsCallSSE(t) (no block).
#   TestE2E_SessionRoundTrip         -> initialize client Mcp-Session-Id==testSID; follow-up
#                                       recorded Mcp-Session-Id== that id.
#   TestE2E_AuthForwardedAndRedacted -> recorded Authorization==secret; buf has NEITHER the
#                                       secret NOR "Bearer"; no JSONL Authorization field.
#   TestE2E_HealthzIsolated          -> GET /healthz → 200 {ok:true}; recorded() is zero.
# Expected: PASS, exit 0. If case 4's redaction half fails, a log event started including
#   header values — route it through redactHeaders (proxy.go rewriteLogFields doc) or stop
#   logging headers. If case 3 fails, Mcp-Session-Id forwarding regressed on the 2nd request.
```

### Level 3: Full suite (regression)

```bash
go test ./...

# Expected: ALL green, exit 0 (additive file; same package; no redeclared symbols; no
# edited helpers). If a "redeclared in this block" error appears, a TestE2E_ name or a
# helper collided with the seed/harness — rename it. If a pre-existing test fails, the
# new file accidentally shadowed a seed helper (revert; do NOT edit proxy_test.go).

# Symbol-name sanity (expect exactly five hits, all in proxy_e2e_test.go):
grep -rn "func TestE2E_" *_test.go
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Prove the §21 "zero difference" criterion is BYTE-LEVEL (not just JSON-semantic) by
# diffing the case-2 client bytes against the fixture on disk:
go test -run 'TestE2E_CanonicalPassthrough' -v

# Prove case 4's redaction is not vacuous: confirm the debug logger DID emit lines
# (rewrite + forward) for the aliased request, i.e. there WAS log surface to inspect.
# (Run the case, then temporarily print buf; or trust the assertion design: at "debug"
#  with an alias, both lines fire by construction — see proxy.go log call sites.)

# Prove case 5 isolation is real (not a false-zero): hit a forwarded path through the
# same mux and confirm recorded() becomes NON-zero (sanity, not a committed test):
#   (manual) curl -s the proxy's "/" with a JSON-RPC body -> up.recorded().Method == "POST".

# Expected: case-2 byte-equality holds against the on-disk fixture; case 4 emitted ≥2 log
# lines (rewrite + forward) yet the secret is absent; case 5's zero recording reflects
# true isolation (a forwarded path would populate it).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 clean: `gofmt`, `go vet ./...`, `git diff --stat` (only proxy_e2e_test.go),
      `git diff go.mod` (empty).
- [ ] Level 2 passes: `go test -run 'TestE2E_' -v` (all five green).
- [ ] Level 3 passes: `go test ./...` fully green (no regression, no collision).
- [ ] Level 4 passes: case-2 byte-equality vs the on-disk fixture; case 4 emitted log
      lines yet the secret is absent; case 5's zero recording reflects true isolation.

### Feature Validation (the §19.3 / §21 contract)

- [ ] Case 1: alias renamed before upstream; client warning-first, original preserved,
      isError false.
- [ ] Case 2: canonical param unchanged at upstream; client body byte-equal (no block).
- [ ] Case 3: initialize hands out the session id; follow-up resends it; upstream records it.
- [ ] Case 4: Authorization forwarded verbatim AND absent from captured stderr.
- [ ] Case 5: `/healthz` 200 `{ok:true}`; upstream handler never ran (recorded() zero).

### Code Quality Validation

- [ ] Follows existing conventions: `httptest.NewServer` + real `http.DefaultClient`
      round-trip; (resp, recorded()) pair; SSE decode via NewSSEReader; unexported test
      funcs; doc comments citing PRD §19.3/§21/§13/§16.
- [ ] File placement matches the desired tree (new file `proxy_e2e_test.go`, nothing else).
- [ ] Anti-patterns avoided (see below): no edits to seed/harness files, no redeclared
      symbols, no unused imports, no discard-logger for the redaction case.
- [ ] No new dependencies (`go.mod` unchanged; stdlib only).

### Documentation & Deployment

- [ ] Each TestE2E_* doc comment names its §19.3 case + §21 criterion + the reused helpers.
- [ ] The "redaction is a regression guard, not a redactHeaders test" rationale is on case 4.
- [ ] The "recorded() returns the LAST request" rationale is on case 3.
- [ ] **Mode A** (per item contract): this is test code — no README change required
      (P1.M5.T3.S1 owns the doc sweep; the case doc comments are the source of truth).

---

## Anti-Patterns to Avoid

- ❌ Don't edit `proxy_test.go`, `proxy_harness_test.go`, or any other existing file —
  this item is ADDITIVE. Redeclaring a seed/harness symbol is a compile error.
- ❌ Don't assert case 4's redaction with a DISCARD logger (`newTestProxy`) — that makes
  the assertion vacuous. Use `captureProxy(t, up.URL, &buf, "debug")` so log lines fire.
- ❌ Don't read `up.recorded()` between the two requests in case 3 expecting the
  initialize snapshot — it holds the LAST request; read it after the follow-up.
- ❌ Don't forget to `io.ReadAll(resp.Body)` + `Close` before asserting on `buf` or
  `recorded()` — the handler/log calls complete during the body read.
- ❌ Don't rebuild the `/healthz` routing ad hoc in case 5 — mirror main()'s mux
  (`HandleFunc "/healthz"` + `"/"`) exactly like `TestRouting_HealthzOnly`.
- ❌ Don't leave an unused import — Go fails the build. The import list is exactly
  `bytes/encoding/json/io/net/http/net/http/httptest/strings/testing`.
- ❌ Don't add defensive nil-guards the seed lacks (e.g. around `result["content"].([]any)`)
  — keep the suite stylistically consistent with `TestForward_RewriteWarningFirst`.
- ❌ Don't add a third-party dependency or new helper type — `go.mod` stays stdlib-only
  and the file ships only the five test funcs.
- ❌ Don't write the harness cases here — the harness (S1) is DONE. CONSUME it.

---

## Confidence Score

**9/10** for one-pass implementation success. The whole deliverable is a single new
test file whose five function bodies are given verbatim above; every consumed symbol is
verified on disk (S1 harness green, seed helpers present, golden fixtures present, full
suite green at research time); each assertion mirrors an existing passing seed test
(`TestForward_RewriteWarningFirst`, `TestForward_PassthroughByteEqual`,
`TestForward_McpSessionIdRoundTrip`, `TestPassthrough_AuthorizationForwarded`,
`TestRouting_HealthzOnly`) whose exact decode/type-assertion patterns are reused; and the
two genuinely-new assertions (case 3 two-request flow, case 4 combined forward+redact)
are traced against the verified `recorded()`-returns-last semantics and the verified
"no log event includes headers" construction. The only residual risk is a typo'd import
or a type-assertion shape mismatch on a fixture (mitigated by Task 0's baseline check,
the exact import list, Level 1's `go vet`, and Level 2's per-case run). Deducted 1 point
for that implementation-time surface.
