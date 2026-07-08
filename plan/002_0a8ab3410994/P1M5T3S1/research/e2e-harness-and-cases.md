# Research: server_test.go E2E harness + PRD §19.3 cases (P1.M5.T3.S1)

This item creates `server_test.go`: the END-TO-END suite that drives the
REGISTERED server through a real SDK client. It is distinct from
`dispatch_test.go` (P1.M5.T2.S1, parallel), which tests the handler closure in
isolation by hand-building a `*mcp.CallToolRequest`. The E2E suite goes
client -> our server (registered tools) -> handler -> UpstreamClient -> fake z.ai.

## 1. The E2E stack (what newE2E wires up)

Reuse, do NOT redefine:
- `newFakeZAI(t) (*httptest.Server, *fakeState)` from upstream_test.go — the fake
  z.ai. Records `st.lastTool/lastArgs/lastAuth` (under st.mu), `st.calls` (int32,
  atomic, ONE increment per successful tool-handler call), `st.expire()`,
  `st.setToolErr()`. Canned result Content[0].Text = `[{"title":"r","url":"u","content":"c"}]`.
- `newUpstreamClient(cfg, log) *UpstreamClient` + `registerTools(server, cfg,
  upstream, log)` + `makeDispatchHandler` from server.go (P1.M5.T2.S1, in flight —
  its PRP is the contract).
- `buildTools(cfg)` from tools.go (P1.M5.T1.S1, DONE).
- `authMiddleware(h http.Handler)` + `authHeaderKey{}` from main.go (DONE).
- `DefaultConfig()` from config.go (DONE) — override `cfg.Upstream = zaiSrv.URL`.

Harness shape:
```go
func newE2E(t *testing.T, logw io.Writer) (*mcp.ClientSession, *fakeState, Config) {
    zaiSrv, st := newFakeZAI(t)            // fake z.ai
    cfg := DefaultConfig()
    cfg.Upstream = zaiSrv.URL               // delegate to the fake, NOT real z.ai
    log := newLogger(logw, "debug")
    // OUR server + the wired dispatch handler (server.go from P1.M5.T2.S1).
    server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: "dev"}, nil)
    upstream := newUpstreamClient(cfg, log)
    registerTools(server, cfg, upstream, log)
    // Mount OUR server EXACTLY like main.go: SDK handler + authMiddleware.
    sdkHandler := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil)
    ourSrv := httptest.NewServer(authMiddleware(sdkHandler))
    t.Cleanup(ourSrv.Close)
    // SDK client carrying a fixed Authorization to OUR server (like a real agent).
    transport := &mcp.StreamableClientTransport{
        Endpoint:   ourSrv.URL,
        HTTPClient: &http.Client{Transport: &fixedAuthTripper{base: http.DefaultTransport, auth: "Bearer e2e-key"}},
    }
    client := mcp.NewClient(&mcp.Implementation{Name: "e2e-test-client", Version: "test"}, nil)
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    t.Cleanup(cancel)
    sess, err := client.Connect(ctx, transport, nil)
    if err != nil { t.Fatalf("client connect to our server: %v", err) }
    t.Cleanup(func() { _ = sess.Close() })
    return sess, st, cfg
}
```

`fixedAuthTripper` is a tiny test-only RoundTripper that sets Authorization on
every outbound request (mirrors a real agent's MCP client). It is NOT authInjector
(authInjector reads from context for OUR upstream; the CLIENT side needs a fixed
header). Define it once in server_test.go.

## 2. SDK signatures (VERIFIED in go-sdk@v1.6.1 source)

- `(*ClientSession).Connect` via `(*Client).Connect(ctx, Transport, *ClientSessionOptions) (*ClientSession, error)` — client.go:255. Pass nil opts.
- `(*ClientSession).CallTool(ctx context.Context, params *mcp.CallToolParams) (*mcp.CallToolResult, error)` — client.go:990.
  `mcp.CallToolParams{Name string, Arguments any}` — Arguments is `any`, so any JSON shape (object, bare string, number, array) is accepted by the client and sent verbatim. The server handler receives it as `json.RawMessage`.
- `(*ClientSession).ListTools(ctx context.Context, params *mcp.ListToolsParams) (*mcp.ListToolsResult, error)` — client.go:983.
  `mcp.ListToolsResult{Tools []*mcp.Tool}` — protocol.go:866. Pass `&mcp.ListToolsParams{}` (zero-value; deterministic, avoids nil-params questions).
- `mcp.CallToolResult{Content []mcp.Content, IsError bool}` — Content[i] is `*mcp.TextContent` for our blocks; read via type assertion.

## 3. Warning text markers (teach.go — for case 4 / case-2 assertions)

- `warningText(...)` produces text STARTING with `[web-search-prime-fixer] Warning: this call used "<source>"/...`. Contains `Results are above.` (proves results LEAD) and a correct-usage EXAMPLE.
- `noQueryWarningText(...)` produces text containing `could not find a search query in the arguments; no search was run.` — this is the case-4 marker.
- Canonical (case 1) => NO warning block at all (len(Content)==1).

## 4. extract.go source labels (for case-3 warning-content assertions)

- bare-string arg -> Source = "bare-string".
- array arg -> Source = "array[0]".
- nested object -> Source = "nested:<...>" or "inferred:<...>".
- The warning text embeds the source (warningText uses `source`), so assert the
  warning CONTAINS the substring ("bare-string", "nested", "array").

## 5. The 6 PRD §19.3 cases — exact assertions

Case 1 — canonical: `web_search` + `{"query":"x"}`.
  client.CallTool(Name:"web_search", Arguments: map{"query":"x"}).
  - err==nil; res.IsError==false.
  - len(res.Content)==1 (NO warning); Content[0].(*TextContent).Text == canned.
  - st.lastTool=="web_search_prime"; st.lastArgs["search_query"]=="x"; st.lastArgs["query"]==nil.
  - st.calls==1.

Case 2 — alias+junk: `web_search` + `{"q":"x","junk":1}`.
  - err==nil; IsError false; len(Content)==2.
  - Content[0].Text == canned (results LEAD); Content[1] starts `[web-search-prime-fixer]` AND contains `Results are above.` AND contains `"q"`.
  - st.lastArgs["search_query"]=="x"; st.lastArgs["junk"]==nil (dropped).
  - st.calls==1.

Case 3 — bare/nested/array (sub-tests; each -> result + warning):
  - bare:    Arguments: "x"                                  -> search_query=="x"; len(Content)==2; warning contains "bare-string".
  - nested:  Arguments: map{"input": map{"query":"x"}}       -> search_query=="x"; len(Content)==2; warning contains "nested".
  - array:   Arguments: []any{"x"}                            -> search_query=="x"; len(Content)==2; warning contains "array".

Case 4 — empty: `web_search` + `{}`.
  - Arguments: map{} (empty object).
  - err==nil; IsError false; st.calls==0 (NO upstream call); len(Content)==1.
  - Content[0].Text contains "could not find a search query".

Case 5 — tools/list:
  - result, err := sess.ListTools(ctx, &mcp.ListToolsParams{}).
  - len(result.Tools)==len(cfg.Tools) (default 1); result.Tools[0].Name=="web_search";
    result.Tools[0].Description contains "query" (full canonical description).
  - NO tool named "web_search_prime" (iterate Tools, assert none).

Case 6 — upstream session-expiry -> transparent re-init + success (the E2E version):
  - 1st call {"query":"first"} -> err==nil, real result (establishes the upstream session).
  - st.expire() (evicts the fake z.ai session OUR UpstreamClient holds).
  - 2nd call {"query":"second"} -> err==nil, real result (re-init + retry transparent).
  - st.calls==2 (1st + retry; the 404'd attempt is rejected before the fake's tool handler, so not counted).

Auth assertion (fold into its own test): with a captured log buffer:
  - st.lastAuth == "Bearer e2e-key" (reached fake z.ai verbatim).
  - the log buffer does NOT contain the substring "e2e-key" (never logged). (redactHeaders
    would redact it if it ever were; the delegate log carries no headers.)

## 6. Auth threading (why fixedAuthTripper on the client works E2E)

- CLIENT transport (fixedAuthTripper) sets `Authorization: Bearer e2e-key` on every
  outbound request to OUR server.
- OUR authMiddleware reads that header from the inbound request and stores it under
  authHeaderKey{} in the request context. The SDK passes req.Context() into
  server.Connect (verified streamable.go:491), so the value reaches our handler.
- Our handler forwards ctx to upstream.callTool. upstream.authInjector reads
  authHeaderKey from ctx and sets Authorization on the outbound request to fake z.ai.
- fake z.ai records st.lastAuth. So `st.lastAuth == "Bearer e2e-key"` proves the
  full client->server->upstream auth path verbatim.

## 7. Gotchas / library quirks

- st.calls counts SUCCESSFUL tool-handler invocations (single atomic increment in
  the fake's handler). A 404'd session-expiry attempt is rejected by the recording
  wrapper BEFORE the SDK dispatches the tool, so it is NOT counted. => case 6
  asserts st.calls==2 (first + retry), NOT 3.
- Read st.lastTool/lastArgs/lastAuth UNDER st.mu (race-safe for `go test -race`).
  st.calls via atomic.LoadInt32. Define small helpers (zaiTool/zaiArg/zaiCalls).
- Do NOT redefine newFakeZAI/fakeState/newUpstreamClient/registerTools/buildTools/
  authMiddleware/authHeaderKey — reuse/consume them (same package => redeclare error).
- CallToolParams.Arguments is `any`: passing a Go string ("x") marshals to a bare
  JSON string; passing []any{"x"} marshals to a JSON array; the server receives each
  as the corresponding json.RawMessage. extract handles all (PRD §10, extract tests).
- Use a generous context timeout (30s) — E2E runs real (in-process) HTTP rounds:
  client initialize, tools/call, and for case 6 a re-init (another initialize).
- The OUR server mount MUST use authMiddleware(sdkHandler) (not the bare sdkHandler),
  or the handler ctx carries no auth and the auth test fails (and auth forwarding to
  z.ai silently does nothing). Mirror main.go exactly.
- DefaultConfig().Upstream is the REAL z.ai URL; the harness MUST override it with
  zaiSrv.URL, or tests hit the real network.
- ListTools params: pass `&mcp.ListToolsParams{}` (zero-value) — deterministic.
- Each test gets its OWN newE2E (own fake z.ai + own OUR server + own client session)
  for isolation. case 6 makes TWO calls within ONE newE2E so they share one
  UpstreamClient (the upstream session persists across them, enabling the re-init).

## 8. PRP-relevant file layout at implementation time

server.go (P1.M5.T2.S1, in flight) provides: newUpstreamClient, registerTools,
makeDispatchHandler, sortedKeys. tools.go provides buildTools + canonicalDescription
(contains "query"). main.go provides authMiddleware + authHeaderKey. upstream_test.go
provides newFakeZAI/fakeState. teach.go provides warningText/noQueryWarningText
markers. extract.go provides the source labels. config.go provides DefaultConfig.

Prerequisite gate: `grep -q "func registerTools" server.go` — if server.go or
registerTools is absent, STOP (the parallel P1.M5.T2.S1 has not landed).
