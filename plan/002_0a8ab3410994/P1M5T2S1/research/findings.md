# Research: P1.M5.T2.S1 — Register tools via AddTool + wire the extract→delegate→teach dispatch handler

This task is the **pipeline wiring** of the v2.0 normalizing MCP server: it builds the
single `mcp.ToolHandler` that every advertised tool routes to, implementing the
`extract → delegate → teach` flow (PRD §5.2, §11.3, FR-4/5/6), and registers it on the
SDK server. Research focused on (a) the EXACT signatures of every function it wires
together, (b) the SDK `AddTool`/`ToolHandler`/`CallToolRequest` surface, (c) the test
seam (can the handler be unit-tested directly?), (d) the log-event design, and (e) the
main.go wiring scope.

---

## 1. Exact signatures consumed (all VERIFIED by reading the on-disk source)

### extract.go (P1.M2.T1 — DONE)
```go
type ExtractionResult struct {
	Query     string
	Source    string          // "query" | "q" | "bare-string" | "scalar" | "array[0]" | "nested:<k>" | "inferred:<path>"
	Ambiguous bool
	Optionals map[string]any  // canonical-optional-name -> raw value; nil when none
	Found     bool
}
func extract(raw json.RawMessage, queryAliases []string, optionalAliases map[string][]string) ExtractionResult
func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any  // {targetParam: Query, ...optionals}
```
- `extract` is PURE/DETERMINISTIC. Missing `arguments` arrives as a zero-length
  RawMessage → returns zero value (Found=false). Bare string → Source="bare-string".
  Object with no alias/inference → Found=false (§10.1.5).
- `ToUpstreamArgs` drops everything except the query + normalized optionals → z.ai gets
  a schema-valid `{search_query: ...}`.

### teach.go (P1.M3.T1 — DONE)
```go
func shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam string) bool
func warningText(calledTool, source, canonicalTool, canonicalParam string) string
func noQueryWarningText(canonicalTool, canonicalParam string) string
func appendWarning(result *mcp.CallToolResult, text string)   // APPEND-ONLY; never sets IsError
func noQueryResult(text string) *mcp.CallToolResult           // fresh result, 1 TextContent, IsError=false
```
- `shouldWarn` returns false ONLY for canonical (tool==canonical AND Source==canonicalParam
  AND no optionals). EVERYTHING else (wrong tool, alias source, bare-string, nested,
  inferred, OR optionals present) → true.
- WARNING TEXTS are byte-fixed (EM DASH before "e.g."; example "rust async runtime" is a
  fixed literal). `appendWarning` appends `&mcp.TextContent{Text: text}` LAST. Neither
  teach function EVER sets IsError (built by hand, never SetError).

### upstream.go (P1.M4.T1 — Implementing, callTool WITH re-init is on disk)
```go
type UpstreamClient struct {            // ALL fields unexported; no constructor exists
	mu          sync.Mutex
	session     *mcp.ClientSession
	upstream    string
	targetTool  string
	targetParam string
	log         *logger                  // nil-safe: logUpstreamError is a no-op when nil
}
func (u *UpstreamClient) callTool(ctx context.Context, args map[string]any) (*mcp.CallToolResult, error)
```
- `callTool` lazily ensures the shared session, calls z.ai's `web_search_prime` with
  `args`, returns z.ai's `*mcp.CallToolResult` UNCHANGED on success. On
  `mcp.ErrSessionMissing` it re-inits ONCE + retries once. NEVER synthesizes a result;
  all failure paths return `(nil, err)`.
- **NO `newUpstreamClient` constructor exists.** Tests build it via struct literal:
  `&UpstreamClient{upstream: srv.URL, targetTool: "web_search_prime", targetParam: "search_query"}`
  (optionally `log: ...`). => this task ADDS a `newUpstreamClient(cfg, log)` for main.go.
- Auth is threaded via CONTEXT (authHeaderKey{}), read by `authFromContext` inside
  `authInjector.RoundTrip`. The handler does NOT pass auth explicitly — it passes `ctx`,
  which already carries authHeaderKey from main.go's middleware. **So the handler just
  forwards `ctx` to `upstream.callTool(ctx, ...)` — auth rides for free.**

### logger.go (DONE)
```go
type logger struct { ... }
func (l *logger) log(level string, msg string, fields map[string]any)   // level: "debug"|"info"|"warn"|"error"
```

### config.go (DONE) — fields consumed by the handler
`cfg.QueryAliases`, `cfg.OptionalAliases` (→ extract); `cfg.CanonicalTool`,
`cfg.CanonicalParam` (→ shouldWarn/warningText/noQueryWarningText); `cfg.TargetParam`
(→ ToUpstreamArgs). `cfg.TargetTool`/`cfg.Upstream` (→ UpstreamClient).

## 2. SDK surface (VERIFIED from go-sdk v1.6.1 source + architecture/mcp_sdk_api.md)

```go
func (s *Server) AddTool(t *Tool, h ToolHandler)            // server.go:238; panics on nil/non-object InputSchema
type ToolHandler func(context.Context, *CallToolRequest) (*mcp.CallToolResult, error)
type CallToolRequest = ServerRequest[*CallToolParamsRaw]     // requests.go:10
type ServerRequest[P Params] struct { Session *ServerSession; Params P; Extra *RequestExtra }  // shared.go:477
type CallToolParamsRaw struct { Meta; Name string; Arguments json.RawMessage }  // protocol.go
type CallToolResult struct { Content []Content; StructuredContent any; IsError bool; err error }  // protocol.go:71
```
- The handler reads ONLY `req.Params.Name` (the called tool) and `req.Params.Arguments`
  (json.RawMessage — whatever the agent sent). It does NOT touch Session/Extra → they may
  be nil in tests.
- `buildTools(cfg) []*mcp.Tool` (P1.M5.T1.S1, parallel) produces AddTool-safe tools; this
  task just iterates them: `for _, t := range buildTools(cfg) { server.AddTool(t, handler) }`
  with ONE shared handler.
- `mcp.NewServer` is ALREADY called in main.go (P1.M1.T2.S2). This task registers tools
  on THAT server.

## 3. Test seam — makeDispatchHandler is DIRECTLY unit-testable (KEY FINDING)

`CallToolRequest = ServerRequest[*CallToolParamsRaw]` has only Session/Params/Extra, and
the dispatch handler reads ONLY Params. So a unit test constructs the request directly:
```go
req := &mcp.CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: "web_search", Arguments: json.RawMessage(`{"query":"x"}`)}}
res, err := makeDispatchHandler(cfg, upstream, log)(ctx, req)
```
No SDK server routing needed. The upstream is a REAL UpstreamClient pointed at the
`newFakeZAI(t)` httptest harness ALREADY in upstream_test.go (same package), which:
- stands up a real MCP server ("fake-zai") over httptest advertising `web_search_prime`;
- records each tools/call (`st.lastTool`, `st.lastArgs`, `st.lastAuth`, `st.calls`);
- returns a canned result `Content[0] = &[{"title":"r","url":"u","content":"c"}]`;
- supports `st.setToolErr(err)` (tool returns error) and `st.expire()` (session 404).

=> dispatch_test.go reuses newFakeZAI + constructs UpstreamClient the same way
upstream_test.go does. This tests the FULL pipeline (extract→delegate→teach) in isolation,
leaving the SDK-routing e2e (tools/list, session-expiry §19.3 cases 5-6) to P1.M5.T3.S1.

## 4. Log-event design (PRD §15 vs. the item contract)

The item contract's LOGIC is the authority; it prescribes 3 events from the handler:
- **delegate** (step 3, BEFORE the Found check — fires for EVERY tools/call): fields
  `called_tool`, `source`, `canonical` (bool), `optionals` (normalized names provided).
- **extract_failed** (step 4, only when `!result.Found`): no upstream call made.
- **upstream_error** (step 6, when callTool returns err): the call failed upstream.

`canonical` (bool) = the call IS canonical = `calledTool==CanonicalTool && result.Source==CanonicalParam
&& len(result.Optionals)==0` (i.e. `!shouldWarn(...)`). `optionals` = the sorted keys of
`result.Optionals` (the canonical optional NAMES forwarded; nil/empty when none).

**Double-log reconciliation (IMPORTANT):** upstream.callTool internally logs
`upstream_error{status:"session_expired"}` and `{status:"reinit_failed"}` via its own
nil-safe `logUpstreamError`. The handler's step-6 `upstream_error` (call-level) therefore
co-fires for those two session-lifecycle cases. This is INTENTIONAL and complementary:
callTool's event carries session-lifecycle detail (status, reinit_attempts); the handler's
event carries the call outcome. They have different fields, not duplicates. Plain non-expiry
errors (ensureSession fail, retry fail, JSON-RPC tool error) are logged ONLY by the handler
(callTask surfaces them verbatim without its own log). => the handler logs upstream_error on
EVERY err per the contract; do NOT try to suppress it to "avoid double-logging" — the contract
mandates it. (See PRP gotcha.)

## 5. main.go wiring scope (this task MODIFIES main.go)

main.go (P1.M1.T2.S2) currently calls `mcp.NewServer(...)` but registers NO tools
("AddTool calls arrive in P1.M5.T2"). The contract: "Consumed by ... main.go (calls
registerTools after NewServer)." Since registerTools takes `upstream *UpstreamClient` as a
PARAM, main.go must construct it. This task therefore:
1. CREATE server.go: `newUpstreamClient(cfg, log)`, `registerTools(server, cfg, upstream, log)`,
   `makeDispatchHandler(cfg, upstream, log) mcp.ToolHandler`.
2. MODIFY main.go: after `server := mcp.NewServer(...)`, add
   `upstream := newUpstreamClient(cfg, log); registerTools(server, cfg, upstream, log)`.
   No new main.go imports (newUpstreamClient/registerTools are same-package).
3. CREATE dispatch_test.go: focused makeDispatchHandler unit tests (reuse newFakeZAI).

main.go is NOT touched by the parallel P1.M5.T1.S1 (tools.go) or by any other in-flight
task, so this edit is conflict-free. "The server now has tools registered and the full
pipeline wired" is only true once main.go calls registerTools — hence it is in scope.

## 6. Boundary vs. neighbors

- CONSUMES: buildTools (P1.M5.T1.S1, parallel — its PRP is the contract for the tools);
  extract (P1.M2.T1), teach (P1.M3.T1), upstream.callTool (P1.M4.T1); config/logger (DONE).
- DOES NOT: write server_test.go e2e / §19.3 cases 5-6 (tools/list, session-expiry) —
  P1.M5.T3.S1 owns those. This task's tests are `dispatch_test.go` (handler-level).
- DOES NOT: touch README/doc.go/config.example.json (P1.M5.T4 owns docs); modify
  upstream.go (M4-owned — adds the constructor to server.go instead).
- DOES NOT: modify go.mod (single SDK require is fixed; all imports already used).
