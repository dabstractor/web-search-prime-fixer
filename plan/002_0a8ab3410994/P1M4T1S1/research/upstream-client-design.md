# P1.M4.T1.S1 Research — UpstreamClient (lazy shared SDK session)

All signatures VERIFIED by reading the actual Go MCP SDK source at
`$(go env GOMODCACHE)/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`
(go1.25 toolchain, SDK v1.6.1 — exactly what go.mod requires). The
fake-z.ai test pattern is PROVEN by a throwaway PoC (`TestZZFakeZAIPoC`)
that connected a real `StreamableClientTransport` to a real MCP server over
`httptest`, completed the initialize handshake, and recorded a `tools/call`.

---

## §1 CLIENT-SIDE SDK API (source-verified)

```go
// client.go:44
func NewClient(impl *Implementation, options *ClientOptions) *Client

// client.go:255 — performs the FULL initialize handshake SYNCHRONOUSLY:
// connect() -> handleSend[*InitializeResult](initialize) [AWAITS response] ->
// check protocolVersion -> notify(initialized) -> return session.
func (c *Client) Connect(ctx context.Context, t Transport, opts *ClientSessionOptions) (cs *ClientSession, err error)

// client.go:990
func (cs *ClientSession) CallTool(ctx context.Context, params *CallToolParams) (*CallToolResult, error)

// client.go:341
func (cs *ClientSession) Close() error   // closes the connection; call for clean shutdown

// streamable.go:1505
type StreamableClientTransport struct {
    Endpoint             string
    HTTPClient           *http.Client      // custom client for RoundTripper auth injection (S2)
    MaxRetries           int               // default 5
    DisableStandaloneSSE bool              // true => no persistent GET; request-response only
    OAuthHandler         auth.OAuthHandler
    strict bool; logger *slog.Logger       // unexported
}

// protocol.go
type CallToolParams struct {
    Meta      `json:"_meta,omitempty"`
    Name      string `json:"name"`                 // cfg.TargetTool = "web_search_prime"
    Arguments any    `json:"arguments,omitempty"`  // map[string]any: {cfg.TargetParam: query, ...optionals}
}
```

`ClientSessionOptions` has only an unexported `protocolVersion` (testing only) →
pass `nil` to `Connect`. `ClientOptions` → pass `nil` to `NewClient`.

## §2 SERVER-SIDE API for the FAKE z.ai (source-verified — the test harness)

```go
// streamable.go:194 — getServer takes *http.Request (NOT *mcp.Request)
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler
// server.go:157
func NewServer(impl *Implementation, options *ServerOptions) *Server
// server.go:238 — NON-generic AddTool; handler gets raw Arguments as json.RawMessage
func (s *Server) AddTool(t *Tool, h ToolHandler)   // t.InputSchema MUST be non-nil, type "object"
```

## §3 PROVEN FAKE-z.ai TEST PATTERN (the de-risking PoC)

```go
// Build a fake z.ai as a REAL MCP server over httptest.
zai := mcp.NewServer(&mcp.Implementation{Name: "fake-zai", Version: "v1"}, nil)
zai.AddTool(&mcp.Tool{
    Name: "web_search_prime",
    InputSchema: json.RawMessage(`{"type":"object","properties":{"search_query":{"type":"string"}},"additionalProperties":true}`),
}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
    // record req.Params.Name + json.Unmarshal(req.Params.Arguments, &lastArgs)
    return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: `[...]`}}}, nil
})
h := mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return zai }, nil)
srv := httptest.NewServer(h)
defer srv.Close()

// The UpstreamClient under test points u.upstream at srv.URL; ensureSession
// builds a REAL StreamableClientTransport against it. Connect does the real
// initialize handshake; CallTool does a real tools/call the fake records.
```

PoC RESULT: `Connect` + `CallTool` succeeded against the fake in ~5ms; the fake
recorded `tool="web_search_prime"`, `args=map[search_query:lunar rover]`, and the
result `Content[0]` was a `*mcp.TextContent` with the canned payload, `IsError=false`.
This means the UpstreamClient can be tested end-to-end against an in-process fake
with NO real z.ai and NO network.

## §4 Connect is SYNCHRONOUS + the context is DETACHED (lazy session is safe)

`Connect` (client.go:256-292) calls `connect(ctx, t, ...)`, then
`handleSend[*InitializeResult](ctx, methodInitialize, req)` which AWAITS the
server's initialize response, then sends `notifications/initialized`. So
`ensureSession`'s `Connect` call returns only after the handshake fully
succeeds (or fails, calling `cs.Close()` and returning a non-nil error —
leaving `u.session` nil so the NEXT call retries).

Per architecture/mcp_sdk_api.md §2 (verified at streamable.go:223): "Connection
context is detached from Connect context using xcontext.Detach to preserve
context values." → the lazy shared session SURVIVES the first inbound request's
context cancellation (the per-request ctx dies when the response is sent, but the
session/connection persists). Context VALUES (e.g. S2's auth) are preserved on the
connection. So:
  - `ensureSession(firstReqCtx)` → Connect detaches → session lives across requests.
  - `callTool(eachReqCtx, ...)` → CallTool uses THAT request's ctx for cancellation,
    but reuses the shared session. Correct.

## §5 RACE-SAFE MUTEX DESIGN (the contract's ensureSession returns only `error`)

The contract: `func (u *UpstreamClient) ensureSession(ctx) error` (returns error,
not the session) and `callTool(ctx, args) (*mcp.CallToolResult, error)`. Holding the
mutex during `Connect` (network I/O) is REQUIRED to prevent double-init (two
concurrent first-calls would both Connect). But `callTool` must NOT hold the mutex
during `CallTool` (that would serialize all calls). The race-safe read of `u.session`
in `callTool` (since `ensureSession` writes it under the mutex) is:

```go
func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error) {
    if err := u.ensureSession(ctx); err != nil {
        return nil, err
    }
    u.mu.Lock()
    sess := u.session   // read under the mutex (race-safe; ensureSession writes under it)
    u.mu.Unlock()
    return sess.CallTool(ctx, &mcp.CallToolParams{Name: u.targetTool, Arguments: args})
}
```

`-race` clean: the only write of `u.session` is in `ensureSession` under `u.mu`;
the only read is in `callTool` under `u.mu`; `CallTool` runs on a local copy
outside the lock so concurrent calls proceed in parallel.

## §6 HTTPClient config (external_deps.md §5, carried from v1 proxy.go)

PRD §11.2 / external_deps §5: do NOT set `http.Client.Timeout` (a whole-exchange
deadline that includes reading the body → cuts off long SSE result streams). Use
`Transport.ResponseHeaderTimeout` (~30s) so a dead upstream is detected quickly
WITHOUT bounding the body read. Verified: ResponseHeaderTimeout "does not include
the time to read the response body". Pattern (mirrors the deleted v1
`newUpstreamClient`):

```go
func newUpstreamHTTPClient() *http.Client {
    tr := http.DefaultTransport.(*http.Transport).Clone()
    tr.ResponseHeaderTimeout = 30 * time.Second
    return &http.Client{Transport: tr}   // Timeout stays 0 (no hard deadline)
}
```

This `Transport` is the `base` S2 (P1.M4.T1.S2) will wrap with an auth
`RoundTripper` (the HTTPClient is passed as `StreamableClientTransport.HTTPClient`).

## §7 ARGS SHAPE callTool receives (from extract.go, frozen)

`ExtractionResult.ToUpstreamArgs(targetParam string) map[string]any` (extract.go:315)
emits `{targetParam: Query}` plus normalized optionals. So `callTool`'s `args` is
`{cfg.TargetParam: query, ...}` — built by the M5.T2 server via
`extract(...).ToUpstreamArgs(cfg.TargetParam)`. callTool just forwards it as
`CallToolParams.Arguments` (any). No transformation in S1.

## §8 CONSUMER + AUTH SEAMS (what S1 must NOT do)

- `authHeaderKey struct{}` + `authMiddleware` already exist in main.go (P1.M1.T2.S2):
  the inbound `Authorization` header is stored in the request context under
  `authHeaderKey{}`. S2 (P1.M4.T1.S2) reads `ctx.Value(authHeaderKey{})` and injects
  it onto the outbound z.ai request via a RoundTripper wrapping
  `newUpstreamHTTPClient()`'s Transport. **S1 builds the HTTPClient WITHOUT auth.**
  S1's UpstreamClient therefore connects to a NO-AUTH fake z.ai (tests) but would be
  REJECTED by real z.ai until S2 lands. This is the deliberate S1/S2 split.
- Consumer: M5.T2 (server dispatch) builds `args := extract(...).ToUpstreamArgs(...)`,
  calls `res, err := upstream.callTool(ctx, args)`, then `appendWarning(res, ...)`
  (teach.go) when `shouldWarn`. callTool returns the raw `*mcp.CallToolResult`;
  S1 does NOT append warnings (that's M5.T2 + teach.go).

## §9 Standalone-SSE note (recorded decision — follow the contract)

`StreamableClientTransport` default `DisableStandaloneSSE=false` sends a persistent
GET after initialize (server-initiated messages). z.ai is request-response (PRD §8)
and we never need server-initiated messages; a server that mishandles the GET could
trigger background reconnect churn (MaxRetries=5). The PoC used the DEFAULT against
an SDK fake server and worked. DECISION for S1: follow the item contract literally
(`mcp.StreamableClientTransport{Endpoint: u.upstream, HTTPClient: ...}` — two fields);
document `DisableStandaloneSSE: true` as a robustness lever for S3 (re-init) if
real-z.ai testing shows reconnect churn. (One justified field addition; reversible.)
