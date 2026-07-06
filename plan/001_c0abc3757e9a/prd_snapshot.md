# web-search-prime-fixer — Design Doc

This document combines the product requirements (the *what* and *why*) with the
technical design (the *how*) for `web-search-prime-fixer`. It is intended to be
read once and implemented in a single pass by a coding agent.

The common industry term for a combined requirements-plus-design document is a
**Design Doc** (also called a Technical Design Document, TDD). An **RFC** is the
related but different format used when a proposal is circulated *to solicit
feedback before deciding*; this document is authoritative and implementation
ready, so Design Doc is the accurate label.

---

## 1. Problem

The z.ai `web-search-prime` MCP tool exposes a single required parameter,
`search_query`. Agents routinely call it with a different parameter name instead
(most often `query`). When that happens, the query is not applied, the call
either returns empty or wrong results, and the agent retries, burning turns and
API budget.

Observed real failures (from local agent session logs):

- `{"query": "..."}` instead of `{"search_query": "..."}` (most frequent)
- `{"query": "...", ...}` mixed with correct fields

z.ai's schema already declares `search_query` correctly, but agents do not read
it carefully enough. This has one root cause and one fix.

## 2. Goal

Provide a small local proxy that sits between the agent's MCP client and z.ai.
It does exactly one thing to outbound `tools/call` requests: if the arguments
contain a known alias for `search_query`, rename it to `search_query`, forward
the corrected call, and return the real results with a terse warning prepended
so the agent learns the correct name and does not retry.

## 3. Non-goals (explicitly out of scope)

The proxy MUST NOT do any of the following, even if it looks helpful. These were
considered and rejected:

- Infer or default `location`.
- Normalize `content_size`, `search_recency_filter`, or any enum value.
- Truncate, shorten, or otherwise modify the query text.
- Drop, map, or warn about unsupported parameters (e.g. `max_results`,
  `safe_search`). They pass through untouched.
- Rewrite the tool schema or `tools/list` response. The schema is forwarded
  verbatim from z.ai.
- Manage API keys or rotate credentials.
- Retry failed upstream calls, rate-limit, or cache results.

If a behavior is not "rename a configured alias to `search_query`", it is out of
scope and must pass through unchanged.

## 4. Users and context

- Operator: a single developer running one local instance for their own agents.
- Clients: any MCP client that speaks Streamable HTTP (pi, Claude Code, Cursor,
  etc.). The client is configured exactly like the direct z.ai server, with only
  the `url` changed to the local proxy.
- Upstream: `https://api.z.ai/api/mcp/web_search_prime/mcp` (Streamable HTTP /
  SSE MCP server, protocol `2024-11-05`).

## 5. Functional requirements

### FR-1 Transparent proxy
The proxy speaks MCP Streamable HTTP / SSE. It forwards every JSON-RPC method
(`initialize`, `notifications/*`, `tools/list`, `tools/call`, `ping`, etc.) to
the upstream and streams the response back. Initialize negotiation, session IDs,
headers, and the SSE event framing are preserved end to end.

### FR-2 The one rewrite rule
For an outbound `tools/call` request, if `params.arguments` contains any key in
the configured alias list:

- If `search_query` is absent, the first alias (in config order) is promoted to
  `search_query`. Other aliases present are removed.
- If `search_query` is already present, all aliases are removed (the canonical
  value wins).
- Any non-alias parameters are left untouched.

The alias list is configuration-driven (see FR-5 and section 14). No other
argument mutation occurs.

### FR-3 Terse warning, results preserved
When FR-2 changes anything, the proxy prepends one `text` content block to the
matching `tools/call` result in the SSE response, ahead of the real result data.
The original result content is unchanged and remains valid JSON. The proxy never
sets `isError: true`. When nothing changed, the response is passed through
byte-for-byte and no warning is added.

### FR-4 Credentials forwarded, not owned
The client sends `Authorization: Bearer <key>` to the proxy exactly as it would
to z.ai. The proxy forwards that header verbatim and holds no key of its own.

### FR-5 Configuration
A small config file defines the alias list and a few operational values
(upstream URL, listen address, path, log level). Built-in defaults allow the
proxy to run with no config file at all.

## 6. Non-functional requirements

- **Single binary, no external runtime dependencies.** Go, stdlib only.
- **Local only.** Binds to `127.0.0.1`. Connects only to the configured upstream.
  It is not a general-purpose or open proxy.
- **Minimal overhead.** The common case (no alias present) must not buffer or
  parse the response body; it streams straight through.
- **No secrets in logs.** The `Authorization` header is never logged.
- **Observability.** Structured JSON lines to stderr, including a line per
  applied rewrite. Configurable level.

## 7. User experience

### 7.1 Client configuration
Identical to the direct z.ai config except the URL points at the proxy. Headers
stay identical so the client still carries the key, which the proxy forwards:

```json
{
  "mcpServers": {
    "web-search-prime": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": {
        "Authorization": "Bearer your_api_key"
      }
    }
  }
}
```

### 7.2 What the agent sees
On a misconfigured call, the agent receives the real search results plus a
leading note, for example:

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
```

followed by the normal result payload. The agent does not need to retry.

## 8. Verified transport contract (z.ai upstream)

Established by direct probe of `https://api.z.ai/api/mcp/web_search_prime/mcp`.

### Request
- Method: `POST` (absolute path `/api/mcp/web_search_prime/mcp`).
- Headers:
  - `Content-Type: application/json`
  - `Accept: application/json, text/event-stream` (the `text/event-stream`
    token is REQUIRED; z.ai returns empty results without it)
  - `Authorization: Bearer <key>` (client-supplied)
  - `Mcp-Session-Id: <uuid>` on every request after `initialize`

### `initialize` response
- Status `200`.
- `content-type: text/event-stream;charset=UTF-8`.
- Response header `mcp-session-id: <uuid>` (new session; must be returned to the
  client and resent on subsequent requests).
- SSE body (note: real wire format has no space after `data`):

```
id:1
event:message
data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"tools":{"listChanged":true}},"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}
```

### `tools/call` response
SSE with one message of the shape:

```
data:{"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"<json string>"}],"isError":false}}
```

The result payload is a JSON-encoded string (a stringified array), not an
object. The proxy must keep it intact when injecting the warning.

### SSE framing rules (for the parser/writer)
- An event is a sequence of field lines (`id:`, `event:`, `data:`) terminated
  by a blank line.
- `data:` carries the JSON-RPC message. Preserve `id:` and `event:` fields when
  re-emitting.
- A `data:` value may itself contain newlines represented as multiple `data:`
  lines that must be concatenated with `\n` on read and split again on write.
  In practice z.ai emits a single `data:` line per message; the parser must
  still handle the multi-line form correctly.

### Session handling
The proxy uses identity pass-through: the `mcp-session-id` returned by upstream
on `initialize` is forwarded to the client unchanged, and the client's
`Mcp-Session-Id` request header is forwarded to upstream unchanged. No session
map is required. This is correct because each client receives and resends its
own id.

## 9. Architecture

```
MCP client (pi/Claude/Cursor)
   |  POST /mcp  (JSON-RPC over HTTP, SSE responses)
   v
web-search-prime-fixer   <-- 127.0.0.1:8787, Go stdlib http.Server
   |  - passthrough for everything
   |  - on tools/call: alias -> search_query rename + warning
   v
https://api.z.ai/api/mcp/web_search_prime/mcp
```

The proxy is a single Go binary with one HTTP listener and two routes:

- `/healthz` -> health handler.
- everything else -> MCP proxy handler.

All MCP traffic (regardless of path) is forwarded to the single configured
upstream URL. The incoming path is ignored for forwarding; only `/healthz` is
intercepted.

### File layout

```
web-search-prime-fixer/
  go.mod                 module web-search-prime-fixer; go 1.22+
  main.go                config load, server bootstrap, graceful shutdown
  config.go              Config struct, defaults, file + env loading
  proxy.go               HTTP handler: route passthrough vs rewrite
  rewrite.go             alias -> search_query rule (pure function)
  sse.go                 SSE reader: parse events; inject warning into result
  doc.go                 package comment
  config.example.json    documented example config
  README.md              install + run
  rewrite_test.go        table-driven rule tests
  sse_test.go            SSE inject/passthrough tests
  proxy_test.go          end-to-end handler tests (httptest)
  PRD.md                 this design doc
```

No third-party dependencies. `go.mod` requires only the standard library.

## 10. The rewrite rule (rewrite.go)

Pure function over a `map[string]any` arguments object.

```go
// RewriteResult describes what happened.
type RewriteResult struct {
    Changed bool
    Notes   []string // human-readable, one per affected alias
}

// Rewrite applies the alias->target rename to args in place.
// aliases order matters: the first present alias is promoted when target is
// absent. Non-alias keys are never touched.
func Rewrite(args map[string]any, aliases []string, target string) RewriteResult
```

### Algorithm
1. If `args` is nil or `target == ""` or `len(aliases) == 0`, return unchanged.
2. Compute `present` = aliases (in config order) that exist as keys in `args`.
3. If `present` is empty, return unchanged (no warning).
4. If `target` is already a key in `args`:
   - For each alias in `present`: delete it from `args`; append note
     `ignored "<alias>" (use only "<target>")`.
   - Return `Changed: true`.
5. Else (target absent):
   - `chosen = present[0]`.
   - `args[target] = args[chosen]`; delete `args[chosen]`.
   - Append note `"<chosen>" is not a valid parameter; renamed to "<target>"`.
   - For each remaining alias in `present[1:]`: delete it; append note
     `dropped redundant "<alias>"`.
   - Return `Changed: true`.

Notes are joined into the warning text (see section 12).

### Examples

| Input args | Output args | Notes |
|---|---|---|
| `{"query":"x"}` | `{"search_query":"x"}` | `renamed "query"` |
| `{"searchQuery":"x"}` | `{"search_query":"x"}` | `renamed "searchQuery"` |
| `{"q":"x"}` | `{"search_query":"x"}` | `renamed "q"` |
| `{"query":"x","q":"y"}` | `{"search_query":"x"}` | `renamed "query"`, `dropped "q"` |
| `{"query":"x","search_query":"y"}` | `{"search_query":"y"}` | `ignored "query"` |
| `{"search_query":"x"}` | `{"search_query":"x"}` | (unchanged, no warning) |
| `{"foo":"bar"}` | `{"foo":"bar"}` | (unchanged, no warning) |
| `{}` | `{}` | (unchanged) |

## 11. Request handling (proxy.go)

### 11.1 Decide whether to rewrite
1. Read the full request body into memory (MCP requests are small JSON-RPC
   objects).
2. Parse as JSON. MCP does not use batches; if the body is a JSON array, process
   each element independently (defensive; re-serialize as an array).
3. For each JSON-RPC object, if `method == "tools/call"`:
   - Let `args = params.arguments` (object). If absent/non-object, skip.
   - Call `Rewrite(args, cfg.Aliases, cfg.TargetParam)`.
   - If `Changed`, re-serialize the object with the modified `params.arguments`
     and remember the request `id` for response correlation.
4. If nothing changed across all objects, set a per-request flag `streamThrough
   = true` and forward the ORIGINAL body bytes (no re-serialization, to avoid
   any formatting drift).
5. If something changed, forward the re-serialized body and set `reqID =
   <id>` (or the set of rewritten ids for a batch).

### 11.2 Forward to upstream
Build an outgoing `*http.Request`:
- Method `POST`, URL = `cfg.Upstream` (fixed; incoming path ignored).
- Body = original bytes (passthrough) or re-serialized bytes (rewrite).
- Copy these request headers from the client verbatim: `Content-Type`,
  `Accept`, `Authorization`, `Mcp-Session-Id`; also pass through
  `Accept-Language`/`User-Agent` if present.
- Do NOT forward hop-by-hop headers (`Connection`, `Keep-Alive`,
  `Transfer-Encoding`, `TE`, `Trailer`, `Upgrade`, `Proxy-Authorization`,
  `Proxy-Authenticate`).
- Set `Accept` to the client's value if present; if the client omitted it, fall
  back to `application/json, text/event-stream`. Do not otherwise modify a
  client-provided `Accept` (passthrough per scope).
- Use an `*http.Client` governed by per-request context cancellation rather than
  a hard timeout, so long SSE streams are not cut off (see section 17).

### 11.3 Write the response
Forward these response headers from upstream verbatim: `Content-Type`,
`Mcp-Session-Id`, `Cache-Control`, `Vary`, `X-Log-Id`, and any other non
hop-by-hop headers. Strip hop-by-hop headers. Copy the upstream status code.

Then:
- If `streamThrough`: `io.Copy` the upstream body to the client unchanged.
- Else: feed the upstream body through the SSE injector (section 12) keyed on
  `reqID` before writing to the client.

Flush after writing when the content type is SSE (use an `http.Flusher` if the
client supports it) so streamed events are not buffered.

## 12. SSE warning injection (sse.go)

### 12.1 Reader
A line scanner over the upstream body that yields decoded SSE events:

```go
type Event struct {
    ID    string
    Type  string // the "event:" field, default "message"
    Data  string // concatenated "data:" lines joined with "\n"
}
```

Parsing per RFC 8895 / the WHATWG SSE rules in summary:
- Lines starting with `:` are comments; ignore.
- `field: value` (strip one leading space after the colon if present).
- Accumulate `data` lines (append with `\n`).
- A blank line dispatches the accumulated event and resets the accumulator.
- Buffer a final event if the stream ends without a trailing blank line.

### 12.2 Inject
For each decoded event:
1. Parse `event.Data` as JSON. If it is not an object or its `id` is not in the
   set of rewritten request ids, re-emit the event unchanged.
2. Otherwise inspect `result`. If `result.content` is an array, prepend:
   ```go
   map[string]string{"type": "text", "text": warningText(notes)}
   ```
   Leave `isError` and all other fields untouched. If `result` is an error result
   or has no `content` array, still log the rewrite but do not inject (nothing
   to prepend to); re-emit unchanged.
3. Re-serialize the modified JSON and re-emit the event with the original `id`
   and `event` fields, encoding `data` with any internal newlines split back
   into multiple `data:` lines.

### 12.3 Warning text format
`warningText(notes []string)` returns a single line:

```
[web-search-prime-fixer] <note[0]>; <note[1]>; ... . Use "search_query" in future calls.
```

Example for `{"query":"x"}`:

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
```

Example for `{"query":"x","q":"y"}`:

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query"; dropped redundant "q". Use "search_query" in future calls.
```

The `Use "search_query" in future calls.` suffix is omitted when every note is
an "ignored" note (target was already correct), replaced by:
`... Use only "search_query" to avoid this notice.`

## 13. Headers, credentials, security

- Forward `Authorization` verbatim. Never read, log, or store its value.
- A redacting logger wraps stderr. Any header named `Authorization`,
  `Cookie`, `Set-Cookie`, or `Proxy-Authorization` is printed as `<redacted>`.
- The proxy dials only `cfg.Upstream`. There is no path or host forwarding; it
  cannot be used as an open proxy.
- Listen address defaults to `127.0.0.1` (loopback only).
- TLS termination is unnecessary (loopback HTTP to a TLS upstream).

## 14. Configuration (config.go)

### 14.1 Schema
```go
type Config struct {
    Upstream    string   // z.ai MCP endpoint
    Listen      string   // bind address
    Path        string   // reserved (informational; default "/mcp")
    Aliases     []string // keys renamed to TargetParam
    TargetParam string   // always "search_query"
    LogLevel    string   // debug | info | warn | error
}
```

JSON form:

```json
{
  "upstream":    "https://api.z.ai/api/mcp/web_search_prime/mcp",
  "listen":      "127.0.0.1:8787",
  "path":        "/mcp",
  "aliases":     ["query", "q", "search", "searchQuery", "search_term"],
  "target_param":"search_query",
  "log_level":   "info"
}
```

### 14.2 Defaults (used when a field is empty or no file exists)
- `Upstream`: `https://api.z.ai/api/mcp/web_search_prime/mcp`
- `Listen`: `127.0.0.1:8787`
- `Path`: `/mcp`
- `Aliases`: `["query", "q", "search", "searchQuery", "search_term"]`
- `TargetParam`: `search_query`
- `LogLevel`: `info`

With these defaults the proxy runs with no config file at all.

### 14.3 Discovery and precedence
1. Config path from env `WSPF_CONFIG`, else first existing of:
   `./web-search-prime-fixer.json`,
   `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json` (default
   `~/.config/web-search-prime-fixer/config.json`).
2. If no file is found, use defaults.
3. Env overrides (highest precedence): `WSPF_UPSTREAM`, `WSPF_LISTEN`,
   `WSPF_LOG_LEVEL`.
4. Parse with `encoding/json`. Unknown fields are ignored. Validation: `Listen`
   must parse, `Upstream` must be an absolute URL; otherwise exit with a clear
   error. `TargetParam` is forced to `search_query` if empty.

`Path` is reserved for documentation/health routing sanity; the proxy forwards
all non-`/healthz` paths to upstream regardless.

## 15. Logging

Structured JSON lines to stderr (so stdout stays clean for any process
supervisor). Fields: `ts`, `level`, `msg`, plus context.

Events:
- `startup`: resolved config (aliases, listen, upstream, log level). Never logs
  credentials.
- `rewrite`: `req_id`, `tool` (if present), `notes` (the array), param presence
  flags. Logged whenever FR-2 changes a call.
- `upstream_error`: `status`, `req_id`, on non-2xx upstream responses.
- `shutdown`: on signal.

Levels honored; `debug` adds per-request `forward` lines with method/id and
whether the response was streamed or injected.

## 16. Health and operations

- `GET /healthz` returns `200` with body `{"ok":true,"version":"<version>"}`.
  Does not touch the upstream.
- Version is injected at build via `-ldflags "-X main.version=..."` or defaults
  to `dev`.
- Graceful shutdown: on `SIGINT`/`SIGTERM`, call `server.Shutdown(ctx)` with a
  10s deadline, then exit.

## 17. Timeouts and robustness

- Outgoing `*http.Client` uses a `Transport` with sensible dial/TLS/h2 defaults.
- The overall request lifetime is governed by the client context (request
  cancellation propagates to the upstream request via `req.WithContext`).
- Avoid a hard `Client.Timeout` that would cut off long SSE streams; rely on
  context cancellation instead. Use `ResponseHeaderTimeout` on the Transport
  (default 30s) so a dead upstream is detected quickly.

## 18. Building and running

```bash
go build -o web-search-prime-fixer .
./web-search-prime-fixer                    # uses defaults, listens 127.0.0.1:8787
WSPF_LISTEN=127.0.0.1:9000 ./web-search-prime-fixer
WSPF_CONFIG=./my-config.json ./web-search-prime-fixer
```

Tests:

```bash
go test ./...
```

## 19. Test plan

### 19.1 rewrite_test.go (table-driven)
Each row: input args, expected args, expected notes substrings, expected
`Changed`. Cover every row of the table in section 10, plus:
- nil args, empty args.
- args without any alias (no change).
- alias list ordering (`q` before `query` changes which is promoted).
- duplicate alias entries in config (de-duped, no double note).
- non-string alias values (numbers, objects) are carried through as-is.

### 19.2 sse_test.go
- Decode a real-shape `tools/call` SSE event, inject a warning, assert the
  result `content` array has the warning first and the original payload string
  unchanged and still valid JSON.
- Multi-`data:`-line event round-trips (split/rejoin preserves content).
- Event whose `id` is not in the rewritten set is emitted verbatim.
- Non-`tools/call` result (e.g. `initialize`) is emitted verbatim.
- Error result (no `content` array) is emitted verbatim, rewrite still logged.

### 19.3 proxy_test.go (httptest end-to-end)
Stand up a fake upstream `httptest.Server` that:
- Returns the `initialize` SSE response with a `mcp-session-id` header, and
  asserts it receives a request with `Accept` containing `text/event-stream`.
- Returns a canned `tools/call` SSE result.
Cases:
1. Client sends `{"query":"x"}`: upstream receives `search_query`; client
   receives the result with the warning text block first.
2. Client sends `{"search_query":"x"}`: upstream receives it unchanged; client
   receives the result with NO extra block (byte-equal to upstream payload).
3. `initialize` response reaches the client with the `mcp-session-id` header
   intact and is then resent by a follow-up request (simulate by inspecting the
   upstream-received `Mcp-Session-Id`).
4. `Authorization` header from the client reaches the upstream unchanged and is
   absent from the stderr log capture.
5. `/healthz` returns 200 and does not call the upstream.

### 19.4 Golden fixtures
Include `testdata/initialize.sse` and `testdata/tools_call.sse` captured from the
verified wire format (section 8) so tests do not depend on the live z.ai server.

## 20. Implementation order (for the coding agent)

1. `config.go` + defaults + a tiny `main.go` that serves `/healthz` and a
   no-op passthrough handler. Verify it boots and proxies `initialize`.
2. `rewrite.go` + `rewrite_test.go`; get the table green.
3. `sse.go` + `sse_test.go` with the golden fixtures.
4. `proxy.go`: wire rewrite into the request path and SSE injection into the
   response path; add the redacting logger.
5. `proxy_test.go` end-to-end with the fake upstream.
6. `README.md` + `config.example.json`.
7. `go vet ./...` and `go test ./...` clean.

Do not add any normalization beyond the alias rename. If tempted to "also fix"
location, recency, content_size, or unsupported params: do not. That is
explicitly out of scope (see section 3).

## 21. Success criteria

- An agent that sends `{"query": "x"}` receives correct results on the first
  call, with a one-line correction, and no retry.
- An agent that sends `{"search_query": "x"}` sees zero difference from talking
  to z.ai directly (no warning, identical results, identical schema).
- No other parameter is ever modified.
- The proxy runs as one Go binary with stdlib only and a one-line config change
  on the client.

## 22. Open defaults chosen (flag for review)

These were picked to keep the implementation minimal. Override if any feel wrong:

- Default alias list: `["query", "q", "search", "searchQuery", "search_term"]`.
- Default listen address: `127.0.0.1:8787`, path `/mcp`.
- Default upstream: the z.ai MCP endpoint in section 4.
- Warning fires whenever an alias was present, including when `search_query` was
  already correct (worded as "ignored"). This keeps the teaching signal
  consistent. If you prefer silence when `search_query` is already correct, say
  so.
