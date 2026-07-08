# MCP SDK API Surface — VERIFIED from v1.6.1 source

All signatures below were **verified by reading the actual Go source** at
`/home/dustin/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1/mcp/`.
File paths and line numbers are cited. The package is `mcp` (subdirectory of the
module root); import path is `github.com/modelcontextprotocol/go-sdk/mcp`.

---

## 1. SERVER SIDE

### mcp.NewServer — `server.go:157`
```go
func NewServer(impl *Implementation, options *ServerOptions) *Server
```
- `impl` must be non-nil (panics otherwise).
- `mcp.Implementation` struct (`protocol.go`): `Name string`, `Title string`, `Version string`, `WebsiteURL string`, `Icons []Icon`.
- Example: `mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)`.
- `ServerOptions` (`server.go`) has fields: `Instructions`, `Logger *slog.Logger`, `InitializedHandler`, `PageSize`, `KeepAlive`, etc. Pass `nil` for defaults.

### (*Server).AddTool — `server.go:238`
```go
func (s *Server) AddTool(t *Tool, h ToolHandler)
```
- **This is the non-generic form** (we use this; NOT `mcp.AddTool[In,Out]` which does typed schema validation).
- With this form, the handler receives **raw arguments** (`json.RawMessage`); we do our own unmarshaling/validation. This is exactly what we need for extraction.
- **VALIDATION:** `t.InputSchema` must be non-nil (panics if nil) and must have `"type": "object"` (panics otherwise).
- `t.OutputSchema` if set must also have type "object". We leave it nil.

### mcp.Tool struct — `protocol.go:1295`
```go
type Tool struct {
    Meta         `json:"_meta,omitempty"`
    Annotations  *ToolAnnotations `json:"annotations,omitempty"`
    Description  string           `json:"description,omitempty"`
    InputSchema  any              `json:"inputSchema"`     // MUST be non-nil, type "object"
    Name         string           `json:"name"`
    OutputSchema any              `json:"outputSchema,omitempty"`
    Title        string           `json:"title,omitempty"`
    Icons        []Icon           `json:"icons,omitempty"`
}
```
- `InputSchema any` accepts: `json.RawMessage`, `map[string]any`, or `*jsonschema.Schema`. **Recommended: `json.RawMessage`** for precise control of `additionalProperties: true`.

### mcp.ToolHandler type — `tool.go:30`
```go
type ToolHandler func(context.Context, *CallToolRequest) (*CallToolResult, error)
```
- `CallToolRequest = ServerRequest[*CallToolParamsRaw]` (`requests.go:10`).
- Handler accesses: `req.Params.Name` (string), `req.Params.Arguments` (**`json.RawMessage`** — the raw bytes of whatever the agent sent).

### CallToolParamsRaw — `protocol.go`
```go
type CallToolParamsRaw struct {
    Meta      `json:"_meta,omitempty"`
    Name      string          `json:"name"`
    Arguments json.RawMessage `json:"arguments,omitempty"`
}
```
- **This is what extraction receives.** `Arguments` is raw JSON bytes. For our `extract.go`, we `json.Unmarshal(req.Params.Arguments, &v)` into `any`, then apply the precedence algorithm.

### mcp.NewStreamableHTTPHandler — `streamable.go:194`
```go
func NewStreamableHTTPHandler(getServer func(*http.Request) *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler
```
- `getServer` is called per request; returns the `*Server` to handle it. OK to return the same server every time.
- If `getServer` returns nil → 400 Bad Request.
- Returns `*StreamableHTTPHandler` which implements `http.Handler` (`ServeHTTP` at `streamable.go:255`).

### mcp.StreamableHTTPOptions — `streamable.go:127`
```go
type StreamableHTTPOptions struct {
    Stateless              bool           // default false (we want stateful sessions)
    JSONResponse           bool           // default false (SSE responses, which we want)
    Logger                 *slog.Logger
    EventStore             *EventStore    // for stream resumption (optional)
    CrossOriginProtection  *http.CrossOriginProtection
}
```
- For our use: pass `nil` or `&mcp.StreamableHTTPOptions{}` for defaults (stateful sessions, SSE responses).

### **CRITICAL — Context propagation for Authorization (streamable.go:491-494)**
The `ServeHTTP` method passes `req.Context()` into `server.Connect`:
```go
// Pass req.Context() here, to allow middleware to add context values.
session, err := server.Connect(req.Context(), transport, connectOpts)
```
**This means middleware that wraps the StreamableHTTPHandler can inject values
(e.g., the Authorization header) into the request context, and those values
propagate through to the tool handler's context.** Pattern:
```go
// main.go: wrap the SDK handler with auth-extraction middleware
sdkHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
    sdkHandler.ServeHTTP(w, r.WithContext(ctx))
}))
```
Then the tool handler reads `ctx.Value(authHeaderKey{})` and passes it to
`upstream.go`, which injects it onto the outbound z.ai request via a
`RoundTripper` wrapping the `StreamableClientTransport.HTTPClient`.

---

## 2. CLIENT SIDE (upstream to z.ai)

### mcp.NewClient — `client.go:44`
```go
func NewClient(impl *Implementation, options *ClientOptions) *Client
```
- Example: `mcp.NewClient(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)`.

### (*Client).Connect — `client.go:255`
```go
func (c *Client) Connect(ctx context.Context, t Transport, opts *ClientSessionOptions) (*ClientSession, error)
```
- Performs the `initialize` handshake. Returns a `*ClientSession`.
- `ClientSessionOptions` (`client.go`) has an unexported `protocolVersion` field (testing only); pass `nil`.

### (*ClientSession).CallTool — `client.go:990`
```go
func (cs *ClientSession) CallTool(ctx context.Context, params *CallToolParams) (*CallToolResult, error)
```
- Returns the same `*CallToolResult` type the server handler returns. We read `.Content` and forward it.

### mcp.CallToolParams — `protocol.go`
```go
type CallToolParams struct {
    Meta      `json:"_meta,omitempty"`
    Name      string `json:"name"`           // cfg.TargetTool = "web_search_prime"
    Arguments any    `json:"arguments,omitempty"`  // map: {cfg.TargetParam: query, ...optionals}
}
```

### mcp.StreamableClientTransport — `streamable.go:1505`
```go
type StreamableClientTransport struct {
    Endpoint             string       // z.ai MCP URL
    HTTPClient           *http.Client // optional custom client (for RoundTripper auth injection)
    MaxRetries           int          // default 5
    DisableStandaloneSSE bool         // set true if we don't need server-initiated messages
    OAuthHandler         auth.OAuthHandler
    strict               bool         // unexported
    logger               *slog.Logger // unexported
}
```
- Construct: `&mcp.StreamableClientTransport{Endpoint: cfg.Upstream, HTTPClient: <custom>}`.
- **Auth injection:** wrap `HTTPClient.Transport` with a `RoundTripper` that sets `Authorization` on outbound requests (from context or a stored field).
- **Context preservation note** (`streamable.go:223`): "Connection context is detached from Connect context using xcontext.Detach to preserve context values (for auth middleware)." So context values from `Connect(ctx, ...)` are preserved for the connection lifetime.

### CallToolResult (returned by both client and server) — `protocol.go:71`
```go
type CallToolResult struct {
    Meta              `json:"_meta,omitempty"`
    Content           []Content `json:"content"`
    StructuredContent any       `json:"structuredContent,omitempty"`
    IsError           bool      `json:"isError,omitempty"`  // NEVER set this for warnings
    err               error     // unexported; set by SetError
}
```
- `Content []Content` — the result content blocks.
- **To append a warning:** `result.Content = append(result.Content, &mcp.TextContent{Text: warningText})`.
- **Never call `result.SetError(err)`** for normalization/guidance cases — it sets `IsError=true` AND overwrites Content if empty.

---

## 3. CONTENT TYPES — `content.go`

### mcp.TextContent — `content.go:28`
```go
type TextContent struct {
    Text        string
    Meta        Meta
    Annotations *Annotations
}
```
- Marshals to `{"type":"text","text":"..."}` via custom MarshalJSON.
- Implements the `Content` interface: `MarshalJSON() ([]byte, error)` + `fromWire(*wireContent)`.

### mcp.Content interface — `content.go:17`
```go
type Content interface {
    MarshalJSON() ([]byte, error)
    fromWire(*wireContent)
}
```
- `TextContent`, `ImageContent`, `AudioContent`, `ResourceLink`, `EmbeddedResource`, `ToolUseContent`, `ToolResultContent` implement it.
- **Reading text from a result:** `result.Content[0].(*mcp.TextContent).Text`.
- **Building results:** `[]mcp.Content{&mcp.TextContent{Text: "..."}}`.

---

## 4. TESTING

### mcp.NewInMemoryTransports — `transport.go:147`
```go
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport)
```
- Returns a pair of transports connected by `net.Pipe()`. First is for the client side, second for the server side (or vice versa — symmetric).
- Usage pattern for in-memory server+client test:
```go
cTransport, sTransport := mcp.NewInMemoryTransports()
// server side
server := mcp.NewServer(&mcp.Implementation{Name: "fake-zai", Version: "v1"}, nil)
server.AddTool(&mcp.Tool{Name: "web_search_prime", InputSchema: ...}, handler)
serverSession, _ := server.Connect(ctx, sTransport, nil)
// client side
client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v1"}, nil)
clientSession, _ := client.Connect(ctx, cTransport, nil)
res, _ := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "web_search_prime", Arguments: map[string]any{"search_query": "x"}})
```

### Alternative: httptest for end-to-end server tests
For `server_test.go` end-to-end (PRD §19.3), the fake z.ai can be a full MCP
server over httptest, OR we use `NewInMemoryTransports` to test the upstream
client in isolation. The `server_test.go` tests the OUR server's handler
end-to-end, which means: SDK client → our server (with a fake upstream injected).

---

## 5. INPUTSCHEMA — permissive schema construction

The `web_search` tool needs `InputSchema` that:
- Has `type: "object"` (required by AddTool validation).
- Declares `query` as the primary property (string, required for documentation).
- Lists common aliases as optional properties.
- Sets `additionalProperties: true` (so the client never rejects any shape).

**Recommended approach** — use `json.RawMessage` for exact control:
```go
canonicalSchema := json.RawMessage(`{
    "type": "object",
    "properties": {
        "query": {"type": "string", "description": "The search query."},
        "q": {"type": "string"},
        "search": {"type": "string"},
        "location": {"type": "string"},
        "content_size": {"type": "string"},
        "search_recency_filter": {"type": "string"}
    },
    "additionalProperties": true
}`)
```

**For terse alias tools** (minimal open schema):
```go
aliasSchema := json.RawMessage(`{"type":"object","additionalProperties":true}`)
```

---

## 6. AUTH/CONTEXT — Authorization header threading

The inbound client sends `Authorization: Bearer <key>` to our server. We forward
it verbatim to z.ai and never read/log/store it.

**Verified approach:**
1. **Middleware extracts Authorization from the inbound request** and stores it in context (see §1 CRITICAL context propagation — the SDK passes `req.Context()` into the session, so it reaches the tool handler).
2. **Tool handler reads Authorization from context** and passes it to the upstream caller.
3. **Upstream caller injects Authorization** onto the outbound z.ai HTTP request. Two options:
   - **(A) RoundTripper wrapper:** Create `&http.Client{Transport: &authInjector{base: http.DefaultTransport, auth: authHeader}}` and pass it as `StreamableClientTransport.HTTPClient`. The `authInjector.RoundTrip(req)` sets `req.Header.Set("Authorization", authHeader)` before delegating.
   - **(B) Per-call transport:** Since the session is lazily initialized with the first request's Authorization, and re-init happens on expiry, store the current auth value on the upstream client struct (guarded by mutex) and rebuild the transport when it changes.

Option (A) is cleanest but requires the transport to be created per-auth-value
(or the RoundTripper to read auth from a mutable field). Since the session is
shared and the auth comes from the first call, a struct field updated on each
call (before the CallTool) is the pragmatic choice. **Key risk:** concurrent calls
with different auth values — but the PRD says single operator, one key, so this
is acceptable.

---

## 7. SERVER OPTIONS — HasInitializedHandler etc.

The SDK's `ServerOptions` supports `InitializedHandler func(context.Context, *InitializedRequest)` for the `notifications/initialized` notification. Not required for our use but available.

Local MCP server methods handled by the SDK automatically:
- `ping` → SDK responds with pong.
- `notifications/*` → SDK acks.
- `resources/list`, `prompts/list` → SDK returns empty (we register none).
- `logging/setLevel` → SDK handles.
- `completion/complete` → SDK handles (or returns method-not-found).

**We do NOT need to manually handle these** — the SDK's `NewStreamableHTTPHandler`
+ `NewServer` dispatch handles them. We only provide tool handlers via `AddTool`.

---

## Summary of verified imports needed
```go
import (
    "github.com/modelcontextprotocol/go-sdk/mcp"
)
```
Everything (server, client, transports, content types, tool/result structs) is in
the `mcp` package. No other SDK subpackage is needed for our scope.
