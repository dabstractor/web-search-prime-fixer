name: "P1.M5.T3.S1 — server_test.go: E2E harness + all PRD §19.3 cases (canonical, alias/junk, bare/nested/array, empty, tools/list, session-expiry)"
description: |

  CREATE `server_test.go` (package main): the END-TO-END suite that drives the
  REGISTERED server through a real Go MCP SDK client, with a fake z.ai injected as
  the upstream. This is the primary success-criteria gate (PRD §22). TEST-ONLY: no
  user-facing surface change, no production code edits.

  THE E2E STACK (newE2E harness, all reused — do NOT redefine):
    fake z.ai (newFakeZAI)  ->  OUR server (mcp.NewServer + registerTools, upstream
    pointed at the fake)  ->  mounted via StreamableHTTPHandler + authMiddleware
    (exactly like main.go)  ->  a connected SDK client carrying a fixed
    Authorization header. The client calls web_search; the call traverses our
  extract→delegate→teach handler and reaches the fake z.ai, which records the exact
    CallToolParams it received.

  THE 6 CASES (PRD §19.3), each a test (case 3 has sub-tests):
    1. canonical {"query":"x"}  -> fake got web_search_prime/search_query exactly; client gets results, NO warning, IsError false.
    2. alias {"q":"x","junk":1} -> fake got clean search_query (junk dropped); client gets results THEN warning.
    3. bare-string / nested / array argument -> extracted + forwarded; results + warning.
    4. empty {}                  -> NO upstream call (st.calls==0); client gets the immediate no-results warning, IsError false.
    5. tools/list                -> advertises exactly cfg.Tools; only web_search has a full description; never web_search_prime.
    6. upstream session-expiry   -> one transparent re-init, then success.
  Plus an auth test: assert the Authorization header reached the fake z.ai verbatim
  AND was never logged (PRD §17, FR-7).

  INPUT (treat as CONTRACT — the parallel P1.M5.T2.S1 is in flight and lands first):
    server.go (P1.M5.T2.S1)  : newUpstreamClient(cfg, log), registerTools(server, cfg,
                              upstream, log), makeDispatchHandler. PRP: plan/002_.../P1M5T2S1/PRP.md.
    tools.go (P1.M5.T1.S1)   : buildTools(cfg); canonicalDescription contains "query".
    upstream_test.go          : newFakeZAI/fakeState (REUSE — do not redeclare).
    main.go                   : authMiddleware(h), authHeaderKey{} (REUSE).
    teach.go / extract.go     : warning markers + source labels (for assertions).
    config.go                 : DefaultConfig() (override cfg.Upstream).

  SCOPE: CREATE server_test.go ONLY. ZERO edits to any production file, to
  dispatch_test.go (the parallel handler-level suite), or to go.mod/go.sum. If
  server.go/registerTools is absent at implementation time, STOP (prerequisite not
  met). The E2E suite is ADDITIVE: it reuses the existing harness and asserts the
  shipped behavior end to end.

---

## Goal

**Feature Goal**: A single `server_test.go` whose `newE2E` harness stands up the
  full client→server→upstream→fake-z.ai stack in-process and whose 6 tests (case 3
  with 3 sub-tests) prove every PRD §19.3 success criterion green under
  `go test ./...` and `go test -race ./...`.

**Deliverable**: `server_test.go` (new file, package main). It defines the `newE2E`
  harness, the `fixedAuthTripper` client-auth helper, small race-safe fake-state
  readers, and the tests:
  `TestE2E_Canonical_NoWarning`, `TestE2E_AliasJunk_WarningAppendedAfter`,
  `TestE2E_NonCanonical_ArgShapes` (bare/nested/array sub-tests),
  `TestE2E_Empty_NoUpstreamImmediateWarning`, `TestE2E_ToolsList`,
  `TestE2E_SessionExpiry_TransparentReinit`, `TestE2E_AuthForwardedAndNotLogged`.

**Success Definition**:
  - `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`, and
    `go test -race -count=1 ./...` all clean (the new suite is additive; existing
    suites stay green).
  - Each of the 6 PRD §19.3 cases passes with the EXACT assertions in the
    research file (upstream receives clean `search_query`; results LEAD and warning
    TRAILS; the empty case makes no upstream call; tools/list advertises exactly
    cfg.Tools with no z.ai-branded name; session-expiry re-inits transparently).
  - The auth test proves `st.lastAuth == "Bearer e2e-key"` (reached fake z.ai
    verbatim) AND the captured log never contains the key string.
  - `git diff --stat` shows ONLY server_test.go added; nothing else changes.

## User Persona

**Target User**: (1) **The PRD §22 success-criteria gate** — this suite IS the
   executable proof of "an agent that sends web_search/query gets results on the
   first call, no retry" and "an agent that sends any other name/shape also
   succeeds on the first call with a warning appended after the results". (2) Any
   future maintainer who refactors extract/teach/upstream/wiring and needs a
   regression net that exercises the whole pipeline, not just units.

**Use Case**: A maintainer changes extract.go and runs `go test -run TestE2E_ ./...`
   to confirm the end-to-end pipeline still: normalizes alias/junk, preserves
   results-lead-warning-trail ordering, skips the upstream call on empty input, and
   recovers transparently from a z.ai session expiry.

**Pain Points Addressed**: Without an E2E suite, a wiring regression (e.g. the
   handler stops forwarding ctx for auth, or tools/list leaks web_search_prime)
   would pass every unit test and ship. This suite catches exactly those
   integration-level regressions by driving the registered server through a real
   SDK client.

## Why

- Implements **PRD §19.3** (the server_test.go end-to-end cases) and is the
  executable form of **PRD §22** (success criteria): canonical-no-retry, any-shape-
  succeeds-with-warning, no-query-immediate-warning, exactly-one-advertised-tool,
  schema-valid-z.ai-call, and session-expiry resilience.
- Exercises the **full auth path** end to end (client header → middleware → handler
  ctx → upstream injector → fake z.ai) which no unit test covers in combination
  (PRD §17, FR-7). Unit tests cover each half; this proves they compose.
- Pins the **results-LEAD / warning-TRAIL** ordering and the **IsError-never-set**
  invariant at the protocol level (what the agent's client actually receives), not
  just at the handler level (which dispatch_test.go covers).
- Guards the **tools/list advertisement contract** (PRD §9.3: exactly cfg.Tools,
  full description only on web_search, never web_search_prime) which is invisible to
  handler-level tests.

## What

One new test file. No production change. The visible behavior under test is the
already-shipped pipeline (extract → delegate → teach); this item only asserts it
end to end. The 7 test functions map 1:1 to the 6 PRD §19.3 cases plus the auth
assertion called out in the item contract ("Assert auth header reached the fake
z.ai and was never logged").

### Success Criteria

- [ ] `server_test.go` exists, `package main`, reuses (does not redefine)
      newFakeZAI/fakeState/newUpstreamClient/registerTools/buildTools/authMiddleware/authHeaderKey/DefaultConfig.
- [ ] Case 1 (canonical): client gets 1 content block (canned), no warning; fake
      got `web_search_prime`/`search_query=="x"`, `query` absent; IsError false.
- [ ] Case 2 (alias+junk): client gets 2 blocks (canned THEN warning containing
      `"q"` and `Results are above.`); fake got `search_query=="x"`, `junk` absent;
      IsError false.
- [ ] Case 3 (bare/nested/array sub-tests): each extracted to `search_query`,
      forwarded, 2 blocks, warning names the source ("bare-string"/"nested"/"array").
- [ ] Case 4 (empty): `st.calls==0` (no upstream call); client gets 1 block
      containing `could not find a search query`; IsError false.
- [ ] Case 5 (tools/list): exactly `len(cfg.Tools)` tools; Tools[0].Name==
      `web_search` with a description containing `query`; no tool named
      `web_search_prime`.
- [ ] Case 6 (session-expiry): two calls both succeed; `st.calls==2`; the second
      call transparently re-inits after `st.expire()`.
- [ ] Auth: `st.lastAuth == "Bearer e2e-key"`; captured log does not contain
      `e2e-key`.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...`,
      `go test -race -count=1 ./...` all clean; `git diff --stat` = server_test.go only.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the full `newE2E` harness is given verbatim, wiring the reused pieces with the
exact cfg override (`cfg.Upstream = zaiSrv.URL`) and the exact main.go-style mount
(`authMiddleware(sdkHandler)`); (b) every consumed signature is cited and verified
on disk (research §2); (c) each of the 7 tests is specified with its exact client
call AND its exact assertions (research §5); (d) the SDK client signatures
(ListTools/CallTool/Connect) are verified against the SDK source; (e) the warning
markers and source labels to assert on are quoted from teach.go/extract.go; (f) the
race-safety rules (read st under st.mu / atomic) and the st.calls semantics are
spelled out; (g) the prerequisite gate (registerTools must exist) and the "reuse,
don't redeclare" rule are explicit.

### Documentation & References

```yaml
# MUST READ — the E2E design + exact assertions for every case.
- docfile: plan/002_0a8ab3410994/P1M5T3S1/research/e2e-harness-and-cases.md
  why: the full newE2E harness, the verified SDK signatures, the warning/source
        markers, and the exact per-case assertions. This is the single source of
        truth for the test bodies.
  critical: the harness MUST override cfg.Upstream with the fake z.ai URL, MUST
        mount authMiddleware(sdkHandler) (not the bare handler), and each test gets
        its own newE2E for isolation (case 6 makes 2 calls in one).

# MUST READ — the cases this suite implements (verbatim).
- file: PRD.md
  section: "§19.3 server_test.go (end-to-end)" + "§19.4 Golden fixtures" + "§22 Success criteria"
  why: §19.3 enumerates the 6 cases; §22 is the success-criteria gate this suite
        proves; §19.4 notes the reused testdata SSE fixtures (wire-format references;
        this E2E suite does not parse them directly — the SDK handles framing).

# MUST READ — the server-wiring contract (parallel, in flight — assume it lands).
- docfile: plan/002_0a8ab3410994/P1M5T2S1/PRP.md
  section: "registerTools + newUpstreamClient + makeDispatchHandler contract"
  why: defines the exact functions this suite consumes. registerTools(server, cfg,
        upstream, log) registers buildTools(cfg) with one shared handler;
        newUpstreamClient(cfg, log) builds the UpstreamClient (no credential field).
  critical: if server.go / registerTools is absent at implementation time, STOP
        (prerequisite not met). Gate: `grep -q "func registerTools" server.go`.

# MUST READ — the verified SDK client surface.
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§2 CLIENT SIDE" + "§4 TESTING (NewInMemoryTransports / httptest)"
  why: Connect(ctx, Transport, nil); CallTool(ctx, *CallToolParams); ListTools(ctx,
        *ListToolsParams); CallToolParams.Arguments is `any` (accepts any JSON
        shape — object, bare string, array). Confirms httptest is the E2E path.
  critical: ListTools params = `&mcp.ListToolsParams{}` (zero-value). CallTool
        Arguments = a Go value marshaled to JSON (string->bare, []any->array).

# READ — the reused fake-z.ai harness (recorded state, expire/setToolErr semantics).
- file: upstream_test.go
  section: "newFakeZAI + fakeState + the recording wrapper"
  why: newFakeZAI(t) returns the fake + state. st.calls is ONE atomic increment per
        successful tool-handler call; a 404'd expiry attempt is NOT counted (the
        recording wrapper rejects it before the handler). st.expire() evicts the
        live Mcp-Session-Id; st.lastAuth is the recorded Authorization.
  critical: REUSE newFakeZAI/fakeState — do NOT redeclare (same package => compile
        error). Read st.lastTool/lastArgs/lastAuth under st.mu; st.calls via atomic.

# READ — the auth seam this suite exercises end to end.
- file: main.go
  section: "authMiddleware + authHeaderKey"
  why: the E2E mount must wrap the SDK handler in authMiddleware (so the handler ctx
        carries the inbound Authorization for upstream forwarding). authHeaderKey is
        the context key. Mirror main.go exactly.
- file: upstream.go
  section: "authInjector + authFromContext + callTool session-expiry re-init"
  why: the upstream side of the auth path (reads authHeaderKey from ctx, sets it on
        the outbound z.ai request) and the re-init logic case 6 exercises.

# READ — assertion markers + source labels.
- file: teach.go
  section: "warningText (starts '[web-search-prime-fixer] Warning: this call used ...'; contains 'Results are above.') + noQueryWarningText (contains 'could not find a search query in the arguments; no search was run.')"
- file: extract.go
  section: "Source labels: 'bare-string', 'array[0]', 'nested:...'/ 'inferred:...'"
- file: tools.go
  section: "canonicalDescription contains the canonical param ('query') — for tools/list assertion"
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod / go.sum    # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  main.go            # authMiddleware + authHeaderKey + NewServer + StreamableHTTPHandler mount (REUSE authMiddleware/authHeaderKey)
  config.go          # DefaultConfig() + v2 Config (REUSE; override cfg.Upstream)
  extract.go         # extract + ExtractionResult + Source labels (consumed by handler)
  teach.go           # warningText/noQueryWarningText markers (for assertions)
  upstream.go        # UpstreamClient + callTool + authInjector + authFromContext (consumed)
  logger.go          # *logger + newLogger (REUSE for captured-log auth test)
  health.go          # healthHandler + var version (REUSE version="dev")
  tools.go           # buildTools + canonicalDescription (REUSE; tools/list assertions)
  server.go          # P1.M5.T2.S1 (in flight): newUpstreamClient/registerTools/makeDispatchHandler (REUSE)
  dispatch_test.go   # P1.M5.T2.S1 (in flight): handler-level tests (DO NOT TOUCH; this suite is E2E)
  *_test.go          # config/resolve/logger/health/extract/teach/upstream/tools tests (untouched)
  testdata/*.sse     # golden wire fixtures (PRD §19.4; SDK handles framing; not parsed here)
  README.md, config.example.json, PRD.md, doc.go — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
server_test.go   # CREATE (package main): the E2E suite.
                 #   - newE2E(t, logw) (*mcp.ClientSession, *fakeState, Config): wires fake z.ai
                 #     (newFakeZAI) -> OUR server (NewServer + registerTools, upstream=fake URL) ->
                 #     mounted via StreamableHTTPHandler + authMiddleware -> connected SDK client
                 #     carrying a fixed Authorization. t.Cleanup closes everything.
                 #   - fixedAuthTripper: test-only RoundTripper setting Authorization on every
                 #     outbound client request to OUR server.
                 #   - race-safe readers: zaiCalls(st) int32; zaiTool(st)/zaiArg(st,key)/zaiAuth(st)
                 #     (read st under st.mu).
                 #   - callWeb helper: sess.CallTool with a timeout ctx.
                 #   - 7 tests: TestE2E_Canonical_NoWarning, TestE2E_AliasJunk_WarningAppendedAfter,
                 #     TestE2E_NonCanonical_ArgShapes (bare/nested/array), TestE2E_Empty_NoUpstreamImmediateWarning,
                 #     TestE2E_ToolsList, TestE2E_SessionExpiry_TransparentReinit, TestE2E_AuthForwardedAndNotLogged.
                 #   Imports: bytes, context, io, net/http, net/http/httptest, strings, sync/atomic,
                 #     testing, time, github.com/modelcontextprotocol/go-sdk/mcp.
# No other file is created or modified by this item.
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — REUSE, do NOT redeclare. newFakeZAI/fakeState (upstream_test.go),
newUpstreamClient/registerTools/makeDispatchHandler (server.go),
buildTools/canonicalDescription (tools.go), authMiddleware/authHeaderKey (main.go),
DefaultConfig/newLogger (config.go/logger.go), version (health.go) are all in
package main. Redeclaring any => "redeclared in this block" compile error. The
dispatch_test.go file (parallel P1.M5.T2.S1) defines its OWN helpers
(newDispatchHandler/callDispatch/lastArg/callCount/contentText) — those ARE
distinct names from this suite's (newE2E/callWeb/zaiCalls/zaiArg/...), so no
collision, but do NOT copy them; write fresh E2E helpers.

CRITICAL — override cfg.Upstream. DefaultConfig().Upstream is the REAL z.ai URL.
newE2E MUST set cfg.Upstream = zaiSrv.URL or the tests hit the public network and
fail/hang.

CRITICAL — mount authMiddleware(sdkHandler), NOT the bare sdkHandler. Without the
middleware, the handler ctx carries no authHeaderKey, so the auth test fails AND
auth forwarding to z.ai silently no-ops (authInjector reads "" and sets nothing).
Mirror main.go's `mux.Handle("/", authMiddleware(sdkHandler))` exactly.

CRITICAL — st.calls counts successful tool-handler calls ONCE (single atomic
increment in newFakeZAI's handler). A session-expiry 404 is rejected by the
recording wrapper BEFORE the SDK dispatches the tool, so it is NOT counted. =>
case 6 asserts st.calls==2 (1st call + retry), NOT 3. case 4 asserts ==0.

CRITICAL — read fakeState under st.mu for lastTool/lastArgs/lastAuth (the handler
writes them under st.mu); `go test -race` flags an unlocked read. st.calls is int32
-> read via atomic.LoadInt32. Use the zaiTool/zaiArg/zaiAuth/zaiCalls helpers.

CRITICAL — CallToolParams.Arguments is `any`. To send a BARE STRING, pass a Go
string ("x") -> marshals to the JSON string "x" -> server receives json.RawMessage
"\"x\"". To send an ARRAY, pass []any{"x"} -> JSON array. To send nested, pass
map[string]any{"input": map[string]any{"query":"x"}}. extract handles all (PRD §10).
Passing a Go map[string]any{} (empty) for case 4 sends the empty object {}.

CRITICAL — the warning is APPENDED AFTER results (FR-6). For case 2/3 assert
len(Content)==2 AND Content[0] is the canned result (NOT the warning) AND
Content[1] starts with "[web-search-prime-fixer]". The warning text also contains
the literal "Results are above." — assert it to pin the results-lead invariant.

CRITICAL — IsError is NEVER true on any path (FR-6). Assert res.IsError==false in
cases 1-4 (case 6 too). The only path returning an error to the CLIENT is an
upstream failure (not exercised by these success-oriented cases); if you add an
upstream-error E2E case, expect a non-nil err, not IsError.

GOTCHA — generous context timeout (30s). E2E runs real (in-process) HTTP rounds:
client initialize, tools/call, and for case 6 a re-init (another initialize).
A 2s timeout can flake on a slow CI. Use context.WithTimeout(ctx, 30*time.Second).

GOTCHA — each test gets its OWN newE2E (own fake z.ai + own OUR server + own client
session) for isolation. case 6 MUST use ONE newE2E for its two calls so they share
ONE UpstreamClient (the upstream session persists across them, enabling re-init).

GOTCHA — ListTools params: pass &mcp.ListToolsParams{} (zero-value struct). Do not
pass nil unless you confirm the SDK accepts it; the zero struct is deterministic.

GOTCHA — the canonical tool's Description (canonicalDescription) contains the
canonical param name, e.g. 'canonical is query'. Assert
strings.Contains(desc, "query") (the default CanonicalParam) for the "full
description" check. tools_test.go pins the exact text if you need it.

GOTCHA — go test caches results. For a true run use `go test -count=1 ./...`; the
validation below uses -count=1 and -race explicitly.
```

## Implementation Blueprint

### Data models and structure

None new. The suite consumes `Config`, `*UpstreamClient`, `*mcp.Server`,
`*mcp.ClientSession`, `*fakeState`, and the SDK types `mcp.CallToolParams` /
`mcp.ListToolsParams` / `mcp.CallToolResult`. One test-only helper type
(`fixedAuthTripper`). Deps already in go.mod (go-sdk); no new imports beyond stdlib
+ go-sdk/mcp.

### Reference implementation (CREATE `server_test.go`)

> Run `gofmt -w server_test.go` after. Reuses every harness piece; defines only
> newE2E, fixedAuthTripper, small readers, and the 7 tests.

```go
package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fixedAuthTripper is a test-only http.RoundTripper that sets a fixed Authorization
// header on every outbound request — mirroring how a real agent's MCP client sends
// its API key to OUR server. (It is deliberately NOT authInjector: authInjector
// reads from context for OUR upstream side; the CLIENT side needs a fixed header.)
type fixedAuthTripper struct {
	base http.RoundTripper
	auth string
}

func (f *fixedAuthTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Authorization", f.auth)
	return f.base.RoundTrip(req)
}

// newE2E stands up the full client→server→upstream→fake-z.ai stack in-process.
// It reuses newFakeZAI (fake z.ai), builds OUR server with registerTools applied
// (the upstream pointed at the fake), mounts it via StreamableHTTPHandler +
// authMiddleware exactly like main.go, and connects an SDK client that carries a
// fixed Authorization. logw captures logs (for the auth-not-logged test); pass
// io.Discard otherwise. Returns the client session, the fake state, and cfg.
// Each call gets a fresh stack (own fake z.ai, own server, own session) for
// isolation; t.Cleanup closes the client session, the httptest servers, and the ctx.
func newE2E(t *testing.T, logw io.Writer) (*mcp.ClientSession, *fakeState, Config) {
	t.Helper()
	zaiSrv, st := newFakeZAI(t) // reuse from upstream_test.go

	cfg := DefaultConfig()
	cfg.Upstream = zaiSrv.URL // delegate to the fake z.ai, NOT the real one
	log := newLogger(logw, "debug")

	// OUR server + the wired dispatch handler (server.go from P1.M5.T2.S1).
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)
	upstream := newUpstreamClient(cfg, log)
	registerTools(server, cfg, upstream, log)

	// Mount OUR server EXACTLY like main.go: SDK handler wrapped in authMiddleware
	// so the inbound Authorization is threaded into the handler ctx.
	sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
	ourSrv := httptest.NewServer(authMiddleware(sdkHandler))
	t.Cleanup(ourSrv.Close)

	// SDK client carrying a fixed Authorization to OUR server (like a real agent).
	transport := &mcp.StreamableClientTransport{
		Endpoint: ourSrv.URL,
		HTTPClient: &http.Client{Transport: &fixedAuthTripper{
			base: http.DefaultTransport,
			auth: "Bearer e2e-key",
		}},
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test-client", Version: "test"}, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		t.Fatalf("client connect to our server: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess, st, cfg
}

// callWeb invokes sess.CallTool(web_search, arguments) with a timeout. arguments
// may be any JSON-able value (object, bare string, array, ...).
func callWeb(t *testing.T, sess *mcp.ClientSession, arguments any) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sess.CallTool(ctx, &mcp.CallToolParams{Name: "web_search", Arguments: arguments})
}

// contentText returns Content[i] as the *mcp.TextContent's Text.
func contentText(t *testing.T, res *mcp.CallToolResult, i int) string {
	t.Helper()
	if i >= len(res.Content) {
		t.Fatalf("Content[%d] out of range (len=%d)", i, len(res.Content))
	}
	tc, ok := res.Content[i].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[%d] is %T, want *mcp.TextContent", i, res.Content[i])
	}
	return tc.Text
}

// Race-safe fake-state readers (st fields are written under st.mu; calls is int32).
func zaiCalls(st *fakeState) int32    { return atomic.LoadInt32(&st.calls) }
func zaiTool(st *fakeState) string    { st.mu.Lock(); defer st.mu.Unlock(); return st.lastTool }
func zaiAuth(st *fakeState) string    { st.mu.Lock(); defer st.mu.Unlock(); return st.lastAuth }
func zaiArg(st *fakeState, k string) any {
	st.mu.Lock(); defer st.mu.Unlock(); return st.lastArgs[k]
}

// (1) Canonical: web_search + {"query":"x"} -> results only, no warning.
func TestE2E_Canonical_NoWarning(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{"query": "rust async runtime"})
	if err != nil {
		t.Fatalf("canonical call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d, want 1 (no warning for canonical)", len(res.Content))
	}
	if got := contentText(t, res, 0); got != `[{"title":"r","url":"u","content":"c"}]` {
		t.Errorf("Content[0]=%q, want the canned z.ai result", got)
	}
	if zaiTool(st) != "web_search_prime" {
		t.Errorf("upstream tool=%q, want web_search_prime", zaiTool(st))
	}
	if got := zaiArg(st, "search_query"); got != "rust async runtime" {
		t.Errorf("upstream search_query=%#v, want the verbatim query", got)
	}
	if zaiArg(st, "query") != nil {
		t.Errorf("upstream received alias 'query' (should be renamed to search_query)")
	}
	if n := zaiCalls(st); n != 1 {
		t.Errorf("upstream calls=%d, want 1", n)
	}
}

// (2) Alias + junk: web_search + {"q":"x","junk":1} -> results THEN warning.
func TestE2E_AliasJunk_WarningAppendedAfter(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{"q": "x", "junk": 1})
	if err != nil {
		t.Fatalf("alias call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content)=%d, want 2 (result THEN warning)", len(res.Content))
	}
	if got := contentText(t, res, 0); got != `[{"title":"r","url":"u","content":"c"}]` {
		t.Errorf("Content[0]=%q, want the canned result LEADING (FR-6 results lead)", got)
	}
	warn := contentText(t, res, 1)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("Content[1] is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, "Results are above.") {
		t.Errorf("warning lacks 'Results are above.' (results-lead invariant): %q", warn)
	}
	if !strings.Contains(warn, `"q"`) {
		t.Errorf("warning does not name the used source \"q\": %q", warn)
	}
	if got := zaiArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query=%#v, want \"x\"", got)
	}
	if zaiArg(st, "junk") != nil {
		t.Errorf("upstream received dropped key 'junk' (ToUpstreamArgs must drop it)")
	}
}

// (3) Bare-string / nested / array argument shapes -> extracted + warned.
func TestE2E_NonCanonical_ArgShapes(t *testing.T) {
	shapes := []struct {
		name      string
		arguments any
		wantSub   string // source label embedded in the warning
	}{
		{"bare-string", "x", "bare-string"},
		{"nested", map[string]any{"input": map[string]any{"query": "x"}}, "nested"},
		{"array", []any{"x"}, "array"},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			sess, st, _ := newE2E(t, io.Discard)
			res, err := callWeb(t, sess, s.arguments)
			if err != nil {
				t.Fatalf("%s call: %v", s.name, err)
			}
			if res.IsError {
				t.Errorf("%s: IsError=true, want false (FR-6)", s.name)
			}
			if got := zaiArg(st, "search_query"); got != "x" {
				t.Errorf("%s: upstream search_query=%#v, want \"x\"", s.name, got)
			}
			if len(res.Content) != 2 {
				t.Fatalf("%s: len(Content)=%d, want 2 (result + warning)", s.name, len(res.Content))
			}
			warn := contentText(t, res, 1)
			if !strings.Contains(warn, s.wantSub) {
				t.Errorf("%s: warning does not reflect source %q: %q", s.name, s.wantSub, warn)
			}
		})
	}
}

// (4) Empty {}: NO upstream call; immediate no-results warning; IsError false.
func TestE2E_Empty_NoUpstreamImmediateWarning(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)
	res, err := callWeb(t, sess, map[string]any{})
	if err != nil {
		t.Fatalf("empty call: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError=true, want false (FR-6)")
	}
	if n := zaiCalls(st); n != 0 {
		t.Errorf("upstream calls=%d, want 0 (no-query must NOT call upstream — §10.1.5)", n)
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content)=%d, want 1 (the immediate no-results warning)", len(res.Content))
	}
	if got := contentText(t, res, 0); !strings.Contains(got, "could not find a search query") {
		t.Errorf("Content[0] is not the no-results warning: %q", got)
	}
}

// (5) tools/list advertises exactly cfg.Tools; only web_search has a full
// description; never web_search_prime.
func TestE2E_ToolsList(t *testing.T) {
	sess, _, cfg := newE2E(t, io.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	res, err := sess.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(res.Tools) != len(cfg.Tools) {
		t.Fatalf("len(Tools)=%d, want %d (exactly cfg.Tools)", len(res.Tools), len(cfg.Tools))
	}
	for i, want := range cfg.Tools {
		if res.Tools[i].Name != want {
			t.Errorf("Tools[%d].Name=%q, want %q", i, res.Tools[i].Name, want)
		}
	}
	// The canonical tool (web_search) carries the full description (names "query").
	canonical := res.Tools[0] // default Tools=["web_search"]; canonical is first by value
	if canonical.Name != cfg.CanonicalTool {
		t.Errorf("canonical tool name=%q, want %q", canonical.Name, cfg.CanonicalTool)
	}
	if !strings.Contains(canonical.Description, cfg.CanonicalParam) {
		t.Errorf("canonical Description=%q lacks the canonical param %q", canonical.Description, cfg.CanonicalParam)
	}
	// Never advertise z.ai-branded names.
	for _, tl := range res.Tools {
		if tl.Name == cfg.TargetTool {
			t.Errorf("tools/list advertised z.ai-branded name %q (forbidden — PRD §9.3)", cfg.TargetTool)
		}
	}
}

// (6) Upstream session-expiry -> one transparent re-init, then success.
func TestE2E_SessionExpiry_TransparentReinit(t *testing.T) {
	sess, st, _ := newE2E(t, io.Discard)

	// First call: lazily establishes the upstream session and succeeds.
	if res, err := callWeb(t, sess, map[string]any{"query": "first"}); err != nil {
		t.Fatalf("first call: %v", err)
	} else if res.IsError {
		t.Fatal("first call: IsError=true")
	}

	// Evict the live z.ai session OUR UpstreamClient holds.
	st.expire()

	// Second call: transparently re-inits ONCE and retries; the client sees success.
	res2, err := callWeb(t, sess, map[string]any{"query": "second"})
	if err != nil {
		t.Fatalf("second call (after expiry): %v (expected transparent re-init + retry)", err)
	}
	if res2 == nil || res2.IsError {
		t.Fatal("second call: expected a real, non-error result after re-init")
	}
	// st.calls counts successful tool-handler calls: first + retry == 2.
	// The 404'd attempt is rejected before the fake's handler, so NOT counted.
	if n := zaiCalls(st); n != 2 {
		t.Errorf("upstream calls=%d, want 2 (first + retry; the 404'd attempt is not a tool call)", n)
	}
}

// (7) Auth: the Authorization header reached the fake z.ai verbatim AND was never
// logged. (PRD §17, FR-7; the item contract: "Assert auth header reached the fake
// z.ai and was never logged.")
func TestE2E_AuthForwardedAndNotLogged(t *testing.T) {
	var buf bytes.Buffer
	sess, st, _ := newE2E(t, &buf) // capture logs
	if _, err := callWeb(t, sess, map[string]any{"query": "x"}); err != nil {
		t.Fatalf("call: %v", err)
	}
	// Reached the fake z.ai verbatim.
	if got := zaiAuth(st); got != "Bearer e2e-key" {
		t.Errorf("fake z.ai received Authorization %q, want %q (verbatim — PRD §17, FR-7)", got, "Bearer e2e-key")
	}
	// Never logged: the captured log stream must not contain the credential.
	if strings.Contains(buf.String(), "e2e-key") {
		t.Errorf("credential 'e2e-key' appears in the log (must never be logged — PRD §15/§17):\n%s", buf.String())
	}
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the on-disk state
  - RUN: grep -q "func registerTools" server.go && grep -q "func newUpstreamClient" server.go \
        && grep -q "func buildTools" tools.go && grep -q "func newFakeZAI" upstream_test.go \
        && grep -q "func authMiddleware" main.go && ! -f server_test.go && echo OK
  - EXPECT: OK. IF ANY FAIL: STOP — a prerequisite (the parallel P1.M5.T2.S1 wiring,
        buildTools, newFakeZAI, or authMiddleware) is missing, or server_test.go
        already exists (collision).

Task 1: CREATE server_test.go — harness + helpers
  - FILE: server_test.go. IMPORTS: bytes, context, io, net/http, net/http/httptest,
        strings, sync/atomic, testing, time, github.com/modelcontextprotocol/go-sdk/mcp.
  - IMPLEMENT: fixedAuthTripper; newE2E(t, logw); callWeb; contentText;
        zaiCalls/zaiTool/zaiAuth/zaiArg.
  - NAMING: test helpers lowercase (unexported); tests TestE2E_*. Reuse (do NOT
        redefine) newFakeZAI/registerTools/newUpstreamClient/authMiddleware/etc.

Task 2: IMPLEMENT the 7 tests
  - TestE2E_Canonical_NoWarning (case 1); TestE2E_AliasJunk_WarningAppendedAfter (case 2);
        TestE2E_NonCanonical_ArgShapes (case 3, 3 sub-tests);
        TestE2E_Empty_NoUpstreamImmediateWarning (case 4); TestE2E_ToolsList (case 5);
        TestE2E_SessionExpiry_TransparentReinit (case 6); TestE2E_AuthForwardedAndNotLogged (auth).
  - ASSERTIONS: exactly as in the reference implementation / research §5. Pin:
        results-LEAD/warning-TRAIL ordering; IsError never true; st.calls semantics
        (case 4 ==0, case 6 ==2); warning markers ("Results are above.",
        "could not find a search query"); source labels in the warning.

Task 3: VALIDATE
  - gofmt -w server_test.go
  - go vet ./...
  - go build ./...
  - go test -run 'TestE2E_' -v -count=1 ./...        # the 7 tests (case 3 = 3 sub-tests)
  - go test -race -count=1 ./...                      # full suite green + race-clean (additive)
  - git diff --stat                                   # expect: server_test.go (new) ONLY
  - git diff go.mod go.sum                            # expect: EMPTY
```

### Implementation Patterns & Key Details

```go
// PATTERN: the E2E stack mirrors main.go's production mount. newE2E builds
//   sdkHandler := mcp.NewStreamableHTTPHandler(getServer, nil)
//   httptest.NewServer(authMiddleware(sdkHandler))
// and points cfg.Upstream at the fake z.ai. Do NOT skip authMiddleware (auth test
// fails + upstream auth silently no-ops) and do NOT leave cfg.Upstream as the real URL.

// PATTERN: client carries auth via a fixedAuthTripper on its transport's HTTPClient.
//   &mcp.StreamableClientTransport{Endpoint: ourSrv.URL,
//     HTTPClient: &http.Client{Transport: &fixedAuthTripper{base, auth}}}
// This mirrors a real agent sending Authorization on every request.

// PATTERN: CallToolParams.Arguments is `any` — pass Go values directly:
//   map[string]any{"query":"x"}  -> object
//   "x"                          -> bare JSON string
//   []any{"x"}                   -> JSON array
//   map[string]any{}             -> empty object (case 4)

// PATTERN: race-safe fake-state reads. st.lastTool/lastArgs/lastAuth under st.mu
// (use zaiTool/zaiArg/zaiAuth); st.calls via atomic (use zaiCalls). Required for
// `go test -race`.

// GOTCHA (restated): st.calls == successful tool-handler calls (one increment). A
//   session-expiry 404 is rejected before the handler -> not counted. case 6 ==2.
// GOTCHA (restated): results LEAD, warning TRAILS (assert Content[0]==canned,
//   Content[1]==warning, and warn contains "Results are above.").
// GOTCHA (restated): IsError NEVER true on cases 1-4,6. Do not assert IsError on an
//   error path (those return a non-nil err, not a result).
// GOTCHA (restated): do NOT redeclare newFakeZAI/registerTools/buildTools/etc.
// GOTCHA (restated): each test its own newE2E; case 6 uses ONE newE2E for both calls.
// GOTCHA (restated): override cfg.Upstream = zaiSrv.URL (never hit real z.ai).
```

### Integration Points

```yaml
FILES CREATED:
  - server_test.go  (package main: newE2E harness + fixedAuthTripper + 7 tests. Imports
        bytes, context, io, net/http, net/http/httptest, strings, sync/atomic, testing,
        time, go-sdk/mcp. Reuses every harness piece; redeclares nothing.)
FILES MODIFIED: none.
FILES NOT TOUCHED (contract):
  - go.mod / go.sum: unchanged (single SDK require; all imports already used elsewhere).
  - server.go / dispatch_test.go (parallel P1.M5.T2.S1): consumed / not touched.
  - extract.go / teach.go / upstream.go / config.go / logger.go / health.go / tools.go /
        main.go: consumed, not edited.
  - all other *_test.go / testdata / README.md / config.example.json / PRD.md / doc.go.
CONSUMER SEAM (keep stable for M5.T4):
  - The green E2E suite is P1.M5.T4.S3's (quality gate) proof that the server answers
        end to end, and P1.M5.T4.S1's (README) evidence that "an agent that sends
        web_search/query gets results on the first call". Keep the test names stable.
DATABASE / ROUTES / ENV / CONFIG: none (test-only; cfg.Upstream is overridden in-test).
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w server_test.go
go vet ./...          # must be clean (catches redeclared helpers, unused imports)
go build ./...        # must compile (server_test.go is a test file; `go build` ignores it,
                      #   but `go vet`/`go test` compile it — see Level 2)
git diff --stat       # expect: server_test.go (new) ONLY
git diff go.mod go.sum # expect: EMPTY (no new requires)
# Expected: zero errors. If `go vet` reports a redeclared symbol, you redefined a
# reused helper (newFakeZAI/registerTools/...) — delete your copy and reuse it.
```

### Level 2: The E2E suite (this IS the deliverable)

```bash
# The 7 tests (case 3 = 3 sub-tests = 9 leaf test invocations):
go test -run 'TestE2E_' -v -count=1 ./...
# Expected: all PASS. Each prints the case name. If a case fails, READ the assertion
# message: a Content-count mismatch => ordering/warning logic; an st.calls mismatch =>
# re-check the fake's counting; an auth mismatch => missing authMiddleware mount.

# Full suite, race-clean, no cache (the new tests are additive):
go test -race -count=1 ./...
# Expected: ok web-search-prime-fixer; ALL existing tests still pass; -race reports
# no data race (the st.mu reads in zaiTool/zaiArg/zaiAuth are required here).

# Expected: zero failures, zero race reports.
```

### Level 3: Targeted case re-runs (debugging a single case)

```bash
# Run one case at a time to isolate a regression:
go test -run 'TestE2E_Canonical_NoWarning' -v -count=1 ./...
go test -run 'TestE2E_AliasJunk_WarningAppendedAfter' -v -count=1 ./...
go test -run 'TestE2E_NonCanonical_ArgShapes' -v -count=1 ./...
go test -run 'TestE2E_Empty_NoUpstreamImmediateWarning' -v -count=1 ./...
go test -run 'TestE2E_ToolsList' -v -count=1 ./...
go test -run 'TestE2E_SessionExpiry_TransparentReinit' -v -count=1 ./...
go test -run 'TestE2E_AuthForwardedAndNotLogged' -v -count=1 ./...
# Expected: each PASSES individually. A case that passes alone but fails in the full
# suite indicates state leakage between tests — ensure each uses its OWN newE2E.
```

### Level 4: PRD §22 success-criteria cross-check (manual mapping)

```bash
# Map each success criterion (PRD §22) to the test that proves it:
#   "web_search/query gets results, no retry, no warning"   -> TestE2E_Canonical_NoWarning
#   "any other name/shape succeeds first call + warning"    -> TestE2E_AliasJunk_* , TestE2E_NonCanonical_*
#   "no usable query -> immediate warning, no upstream"     -> TestE2E_Empty_NoUpstreamImmediateWarning
#   "exactly one tool; no z.ai-branded names"               -> TestE2E_ToolsList
#   "z.ai receives schema-valid search_query"               -> zaiArg(st,"search_query") in cases 1/2/3
#   "session-expiry -> transparent re-init"                 -> TestE2E_SessionExpiry_TransparentReinit
#   (auth forwarded, never owned/logged)                    -> TestE2E_AuthForwardedAndNotLogged
# Expected: every §22 bullet has a green test. No bullet is uncovered.

# Sanity: no test hits the real network (cfg.Upstream is the in-process fake).
grep -n "api.z.ai" server_test.go   # expect: no matches (the real URL is never referenced)
# Expected: empty. If a match appears, cfg.Upstream was not overridden -> fix newE2E.
```

## Final Validation Checklist

### Technical Validation

- [ ] `gofmt -l .` empty (incl. server_test.go).
- [ ] `go vet ./...` exit 0 (no redeclared helpers, no unused imports).
- [ ] `go build ./...` exit 0.
- [ ] `go test -run 'TestE2E_' -v -count=1 ./...` — all 7 tests PASS.
- [ ] `go test -race -count=1 ./...` — full suite green + race-clean (additive).

### Feature Validation (PRD §19.3 / §22)

- [ ] Case 1: canonical -> 1 block, no warning; fake got search_query, query absent.
- [ ] Case 2: alias+junk -> 2 blocks (result THEN warning with "q" + "Results are above.");
      fake got search_query, junk absent.
- [ ] Case 3: bare/nested/array each -> search_query=="x", 2 blocks, warning names source.
- [ ] Case 4: empty -> st.calls==0; 1 block "could not find a search query"; IsError false.
- [ ] Case 5: tools/list -> exactly cfg.Tools; web_search has full desc; no web_search_prime.
- [ ] Case 6: two calls succeed; st.calls==2; re-init transparent.
- [ ] Auth: st.lastAuth=="Bearer e2e-key"; log never contains "e2e-key".

### Code Quality / Scope Validation

- [ ] ONLY server_test.go created. No other file touched.
- [ ] No edits to server.go/dispatch_test.go/extract.go/teach.go/upstream.go/tools.go/
      main.go/config.go/go.mod/go.sum or any other file.
- [ ] Reuses (does not redefine) newFakeZAI/fakeState/registerTools/newUpstreamClient/
      buildTools/authMiddleware/authHeaderKey/DefaultConfig/newLogger/version.
- [ ] Race-safe fake-state reads (st under st.mu; calls via atomic).
- [ ] Test names are TestE2E_* (stable for the M5.T4 gate + README consumers).

### Documentation & Deployment

- [ ] Test-only: no user-facing surface change, no production code change.
- [ ] `git diff --stat` shows server_test.go only; `git diff go.mod go.sum` empty.
- [ ] No network dependency (cfg.Upstream overridden to the in-process fake).

---

## Anti-Patterns to Avoid

- ❌ Don't redefine newFakeZAI/fakeState/registerTools/newUpstreamClient/buildTools/
  authMiddleware/authHeaderKey/DefaultConfig — reuse them (same package => redeclare error).
- ❌ Don't mount the bare sdkHandler — wrap it in authMiddleware, or the auth path is dead
  and the auth test fails (and forwarding to z.ai silently no-ops).
- ❌ Don't leave cfg.Upstream as the default — override it with the fake's URL or tests hit
  the real z.ai network and fail/hang.
- ❌ Don't assert exact st.calls for the expiry case as 3 — the 404'd attempt is rejected
  before the fake's handler and is NOT counted; it is 2 (first + retry).
- ❌ Don't read st.lastTool/lastArgs/lastAuth without st.mu, or read st.calls without atomic —
  `go test -race` will flag a data race. Use the zaiTool/zaiArg/zaiAuth/zaiCalls helpers.
- ❌ Don't assert Content ordering loosely — pin results-LEAD (Content[0]==canned) and
  warning-TRAIL (Content[1] starts "[web-search-prime-fixer]" and contains "Results are above.").
- ❌ Don't forget IsError==false on cases 1-4,6 (FR-6: never set for normalization/warning).
- ❌ Don't pass nil to ListTools if unsure — pass &mcp.ListToolsParams{} (zero-value, deterministic).
- ❌ Don't share one newE2E across tests — each test gets its own (isolation); case 6 alone
  uses one newE2E for its two calls (shared UpstreamClient enables re-init).
- ❌ Don't edit server.go, dispatch_test.go, go.mod, or any production file — this item is
  server_test.go only.
- ❌ Don't use a short context timeout — E2E runs real HTTP rounds incl. a re-init; use 30s.

---

## Confidence Score

**9/10** for one-pass success. Every consumed signature (registerTools/newUpstreamClient
from server.go; newFakeZAI/fakeState from upstream_test.go; buildTools from tools.go;
authMiddleware/authHeaderKey from main.go; the SDK's Connect/CallTool/ListTools) is
verified on disk or against the SDK source, and the full reference test file (harness +
helpers + all 7 tests with exact assertions) is given verbatim. The warning markers and
source labels to assert on are quoted directly from teach.go/extract.go, and the st.calls
semantics (one increment; 404-not-counted) are confirmed from the actual newFakeZAI code.
The residual 1 point reflects the dependency on the parallel P1.M5.T2.S1 wiring landing
first (mitigated by the Task 0 prerequisite gate) and the inherent fragility of any
in-process multi-round E2E test under a slow CI (mitigated by the 30s timeout and the
per-case targeted re-runs in Level 3).
