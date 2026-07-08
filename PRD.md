# web-search-prime-fixer — Design Doc

This document is the combined product requirements (the *what* and *why*) and
technical design (the *how*) for `web-search-prime-fixer`. It is authoritative
and implementation-ready.

This is a **Design Doc**, not an RFC: it is not circulated to solicit feedback
before deciding. The only items still open are minor shipped-defaults in
section 23.

---

## 0. What changed since the first revision

The first revision specified a **transparent proxy** that forwarded `tools/list`
verbatim from z.ai and rewrote only the `arguments` of outbound `tools/call`
requests (`query` → `search_query`). That design was built and passed its tests,
and then **failed in production** on its first real use. Section 1 explains why,
with evidence from the live client. The fix required two changes, both reflected
throughout this document:

1. **Client side.** Stop the MCP client from prefixing tool names, so agents can
   call a tool by the obvious name. One config line (`toolPrefix: "none"`).
2. **Server side.** Stop being a transparent proxy. Become a real **MCP server**
   that advertises essentially one tool (`web_search`), accepts *any* arguments,
   extracts the intended search query from whatever the agent sent, delegates one
   clean call to z.ai, and — when usage was non-canonical — appends a warning
   (with a correct-usage example) **after** the results.

The earlier "rename one parameter, forward everything else verbatim" philosophy
is **superseded**. The new job is: *anything the agent sends at this server is a
web search; figure out the query, run it, return the results, and warn about
correct usage.*

## 1. Problem (revised, with evidence)

Agents routinely fail 3–4 times before successfully calling z.ai's
`web-search-prime` MCP tool. Two distinct failure layers cause this, and the
old design addressed neither usefully.

### 1.1 Layer 1 — the wrong tool NAME is rejected before any network hop

The MCP client (pi, via `pi-mcp-adapter`) exposes every server's tools under a
**prefixed** name by default. Verified in the installed adapter source:

```ts
// pi-mcp-adapter/init.ts
const prefix = config.settings?.toolPrefix ?? "server";      // default "server"

// pi-mcp-adapter/types.ts
function formatToolName(toolName, serverName, prefix) {
  const p = getServerPrefix(serverName, prefix);
  return p ? `${p}_${toolName}` : toolName;                  // "server" -> web_search_<tool>
}                                                            // "none"   -> <tool> (bare)
```

The tool-metadata cache stores the **bare** server name (`web_search_prime`),
but the gateway surfaces the **prefixed** form. With the operator's server named
`web_search`, the only tool the agent could call was `web_search_web_search_prime`.
Every common first guess (`web_search`, `search`, `query`) was rejected **inside
the client** — `Tool "web_search" not found. Use mcp({ search: "..." }) to
search.` — *before any request reached our process.* The old proxy never saw
these calls, so its argument rewrite never ran.

### 1.2 Layer 2 — the wrong parameter NAME and the wrong SCHEMA

When a call *does* reach a tool, the argument is wrong in two ways:

- **Wrong parameter name:** `query` instead of `search_query`; the query under
  `q`/`search`/`searchQuery`/`search_term`; extra junk params.
- **Wrong schema / structure:** the query nested one level deep
  (`{"input": {"query": "x"}}`), the value itself an object
  (`{"query": {"text": "x"}}`), a chat-shaped `messages` array, a bare array
  (`["x"]`), a bare string, a numeric value, etc.

z.ai's real schema wants `search_query` (required) plus a small set of optional
fields, and ignores or rejects the rest. Agents get both the name and the
structure wrong, repeatedly.

### 1.3 What the first revision got wrong

It scoped the fix to a narrow slice of layer 2 (renaming one top-level key) on
the assumption that a malformed `tools/call` would arrive at the proxy. In
practice the layer-1 rejection happens first and locally, so the proxy sat on a
path where its target failure almost never arrived. It also **forbade** (as a
non-goal) the one intervention that could have helped at its own layer: rewriting
`tools/list`. This revision inverts both mistakes and broadens the layer-2 fix
from "rename a key" to "extract a query from arbitrary structure."

## 2. Goal

Make an agent's first attempt at a web search succeed, regardless of how it
guessed the tool name or mangled the arguments, while keeping the agent's context
window clean and teaching it toward one canonical form.

Concretely, after this is deployed:

- An agent that calls `web_search` with `{ "query": "..." }` gets correct results
  on the **first** call, with no retry and no warning.
- An agent that calls it with `{ "q": "..." }`, a bare string, a nested object,
  a chat-shaped array, or extra junk params also succeeds on the first call.
- An agent that guesses a different *advertised* tool name (e.g. `search`, if
  configured) also succeeds.
- The agent's tool list contains essentially **one** tool (`web_search`).
- When the agent strays from the canonical form, it receives a **warning with a
  correct-usage example**, appended **after** the results — never instead of them.

## 3. Non-goals (revised)

The proxy-era non-goals that forbade normalizing arguments are **dropped**. The
new job is explicitly to normalize. The current non-goals are:

- **Not a general search engine.** It delegates every query to z.ai's
  `web_search_prime`. It does not synthesize, rank, cache, or invent results.
- **Not a general-purpose or open proxy.** It binds to `127.0.0.1` and dials only
  the configured z.ai upstream.
- **No credential ownership.** It forwards the client's `Authorization` header to
  z.ai and holds no key of its own.
- **No retry / rate-limiting / caching of upstream calls.** One delegated call per
  inbound call; surface upstream errors honestly.
- **No truncation of the query text.** The extracted query is forwarded verbatim.
- **No multi-tool semantics.** Every advertised tool does the same one thing (a
  web search). Additional names exist only to catch guesses, not to offer
  features.
- **No z.ai-branded names advertised.** `web_search_prime` is never exposed to the
  agent; models do not know it by default and it is not the surface we teach.
- **Not responsible for other MCP servers' tool-name collisions** when the client
  runs with `toolPrefix: "none"` (section 20); the operator owns that.

Anything not listed is in scope if it serves "make the agent's first try work."

## 4. Users and context

- **Operator:** a single developer running one local instance for their own
  agents.
- **Clients:** any MCP client that speaks Streamable HTTP (pi, Claude Code,
  Cursor). Configured exactly like the direct z.ai server, except the `url`
  points at this server **and** the client is set to expose bare tool names
  (`toolPrefix: "none"` for pi; section 20).
- **Upstream:** `https://api.z.ai/api/mcp/web_search_prime/mcp` (Streamable HTTP
  / SSE MCP server). The real tool there is named `web_search_prime` and requires
  `search_query`. That name is an internal implementation detail; the agent never
  sees it.

## 5. Solution overview (two parts)

### 5.1 Part A — client unlock: bare tool names

Set the client to stop prefixing. For pi, add `settings.toolPrefix` to
`~/.pi/agent/mcp.json` (section 20). Now whatever our server lists in
`tools/list` is exactly what the agent types. Combined with advertising a tool
named `web_search`, the most common first guess becomes directly callable with no
discovery hop.

This single change removes most layer-1 failures on its own, because the tool is
no longer hidden behind an unguessable `web_search_web_search_prime` name.

### 5.2 Part B — server rewrite: a normalizing MCP server

`web-search-prime-fixer` becomes a real MCP server (not a byte proxy). It owns
the JSON-RPC surface and delegates only the actual search:

| Client → us | Our response |
| --- | --- |
| `initialize` | our own `serverInfo` + `capabilities.tools` (no upstream call) |
| `tools/list` | our generated tool list: essentially one tool, `web_search` (section 9) |
| `tools/call` (any advertised tool) | extract query from arbitrary input (section 10) → delegate to z.ai `web_search_prime` (section 11) → return the result content, then append a warning+example if usage was non-canonical (section 12) |
| `ping`, `notifications/*`, `resources/list`, `prompts/list`, `logging/setLevel`, `completion/complete` | handled locally (pong / ack / empty / method-not-found as appropriate) |

The relationship to z.ai is inverted versus a proxy: instead of forwarding the
client's request, we **act as an MCP client to z.ai** — one lazily-initialized
session, reused across calls, re-initialized on expiry (section 11).

## 6. Functional requirements

### FR-1 MCP server (Streamable HTTP), built on the Go MCP SDK
Speak MCP Streamable HTTP / SSE using the official
`github.com/modelcontextprotocol/go-sdk` (decision locked, section 13).
`NewStreamableHTTPHandler` owns initialize / `Mcp-Session-Id` / SSE framing /
JSON-RPC dispatch / `tools/list` / `tools/call`; we provide the tool handlers.

### FR-2 One advertised tool (plus optional alias names)
Advertise essentially one tool — `web_search` — which carries the full
description and schema we want the agent to learn. Additional names (e.g.
`search`) may be configured to catch common guesses; they route to the identical
handler and carry a terse description + minimal schema. **Never** advertise
z.ai-branded names (`web_search_prime`). Defaults to a single entry
(section 9.2).

### FR-3 Permissive schema (so the client never rejects locally)
`web_search`'s `inputSchema` must accept anything: declare `query` as the primary
parameter, list the common aliases as optional, and set `additionalProperties:
true`. A client that validates arguments against the schema (pi and others may)
must never reject `{ "query": ... }` — or any other shape — locally, because that
would reproduce the exact layer-2 failure we are eliminating. The schema's job is
to *document* the canonical form, not to *enforce* it; enforcement is impossible
to do helpfully (the client would just reject the call).

### FR-4 Extract a query from ANY input
For every `tools/call`, extract a search query string from `params.arguments`
**regardless of structure** (section 10). The directive is: *whatever the model
passed is treated as a search query somehow.* Accept any alias key at any depth;
a bare string; a numeric/boolean (coerced); a single-string object; an array
(first usable element); a nested wrapper; and, as a last resort, the longest
reachable string. Recognized optional z.ai parameters (`location`,
`content_size`, `search_recency_filter` and their aliases) are normalized and
forwarded. **All other keys are dropped** so z.ai receives a schema-valid request.

### FR-5 Upstream delegation
Maintain one MCP client session to z.ai (via the SDK's `StreamableClientTransport`
+ `client.Connect`). On each inbound `tools/call`, send a single `tools/call` to
z.ai's `web_search_prime` with `{search_query, ...}`, read the result content, and
return it. Re-initialize the upstream session on session-expiry signals. Thread
the client's `Authorization` header to z.ai.

### FR-6 Teaching signal — a warning, appended after results, never instead of them
When the agent used anything other than the canonical `web_search` / `query`,
**append** one `text` content block — a **warning** that includes a short
correct-usage **example** — **after** the real result content. The real results
are always present in the same response, so the model does not retry. The warning
is **never** returned without results when a search could be performed. The one
exception: if extraction found no usable query at all (section 10.1.5), return
the warning immediately and make no upstream call (there is nothing to return).
**Never set `isError: true.** When usage was canonical, return results with no
added warning.

### FR-7 Credentials forwarded, not owned
The client sends `Authorization: Bearer <key>` to us; we forward it verbatim to
z.ai and hold no key. It is never logged (section 15).

### FR-8 Configuration
A config file defines the advertised tools, the extraction alias order, the
canonical names, the upstream URL, the listen address, and the log level. Built-in
defaults allow running with no config file.

## 7. Non-functional requirements

- **Single binary.** One Go binary. Depends on the official Go MCP SDK
  (`github.com/modelcontextprotocol/go-sdk`); this is the sole external dependency.
- **Local only.** Binds to `127.0.0.1`, dials only the configured upstream.
- **Minimal context footprint.** The common case adds exactly one tool
  (`web_search`) to the agent's context.
- **No secrets in logs.** `Authorization` is never logged.
- **Observability.** Structured JSON lines to stderr, including a line per
  delegated call (which tool/param/structure was used, whether canonical, the
  extraction source).
- **Streaming preserved.** Long SSE result streams from z.ai are forwarded to the
  client without a hard timeout; client cancellation propagates upstream.

## 8. Verified transport contract (z.ai upstream)

Established by direct probe (carried over from the first revision; still
authoritative). With the SDK adopted, its `StreamableClientTransport` handles the
framing, the `Accept` negotiation, and `Mcp-Session-Id` lifecycle; this section
documents what our code must still account for:

- **Endpoint:** `POST https://api.z.ai/api/mcp/web_search_prime/mcp`.
- **Request headers:** `Content-Type: application/json`; `Accept` must contain
  `text/event-stream` (z.ai returns empty results without it); `Authorization:
  Bearer <key>` (client-supplied); `Mcp-Session-Id: <uuid>` after `initialize`.
- **`initialize` response:** `200`, `content-type: text/event-stream`, response
  header `mcp-session-id: <uuid>`.
- **`tools/call` response:** SSE, one message of shape
  `{"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"<json string>"}],"isError":false}}`.
  The result payload is a JSON-encoded **string** (a stringified array), not an
  object. We keep it intact when returning it to the client.

## 9. Advertised tools and context minimization

### 9.1 The hard constraint
**It is impossible to catch a tool name the client has not been told about.**
pi-mcp-adapter resolves names from the cached `tools/list` and rejects unknown
names client-side before any network call (section 1.1). Therefore, to catch a
wrong name we *must* advertise it, and advertising costs context. Fully "hiding
alternatives until misuse" is not achievable.

### 9.2 Why one tool is enough now
Under `toolPrefix: "none"` (Part A) plus advertising a tool named `web_search`,
the canonical name is itself the obvious, guessable name. Most agents will see
`web_search` in their tool list and use it directly. We therefore advertise
**one** tool by default. The layer-2 failures (wrong param, wrong schema) are
handled entirely by extraction (section 10), which needs no extra advertised
tools. An optional second name (`search`) may be configured if logs show agents
guessing it; it is not in the default set.

### 9.3 Default and rules
- **Default advertised tools:** `["web_search"]` (single entry).
- `web_search` carries the full description + documented schema (FR-3).
- Any additionally-configured tool is a **terse alias**: one-line description
  ("Performs a web search; alias of web_search."), minimal open schema
  (`{"type":"object","additionalProperties":true}`), same handler.
- **Never** advertise z.ai-branded names (`web_search_prime`, etc.).

### 9.4 Canonical surface (what we document and teach)
- **Tool:** `web_search`
- **Parameter:** `query` (string, required)
- **Optional:** `location`, `content_size`, `search_recency_filter` (passed
  through to z.ai when provided).

Canonical is `web_search` / `query`, exactly. Anything else triggers the warning.

## 10. Query extraction (`extract.go`)

Pure function over the inbound `params.arguments`. Deterministic; table-tested.
The directive: *whatever the model passed is treated as a search query somehow.*
Only when there is genuinely no usable string anywhere do we give up (10.1.5).

### 10.1 Precedence
1. `arguments` is a **string** → that is the query (source `bare-string`).
2. `arguments` is a **number/boolean** → stringify (source `scalar`).
3. `arguments` is an **array** → recurse on the first element that yields a query
   (source `array[0]`); else fall through to 10.1.5.
4. `arguments` is an **object** (the common case):
   1. **Alias scan (shallow, then nested).** Walk the alias priority list
      (config-driven; default `query`, `search_query`, `q`, `search`,
      `searchQuery`, `search_term`, `term`, `text`, `input`, `prompt`,
      `question`, `keywords`, `topic`, `searchString`). For the first present
      key, resolve its value to a string: if the value is itself a string/number/
      boolean, use it (coerced); if it is an object/array, **drill in** — look for
      a sub-key in `text/value/content/query/q/data/input`, else the first
      reachable string inside it (source `nested:<key>`).
   2. **No alias matched → infer.** Recursively collect every reachable string
      value, **excluding** keys that are recognized optionals or their aliases
      (10.2). If exactly one candidate → use it (source `inferred:<key>`). If
      several → use the **longest** (source `inferred:<key>`, `ambiguous=true`).
5. **Nothing usable** (no strings anywhere: `{}`, `null`, a number/bool that
   cannot be coerced to anything meaningful, an array of non-strings): extraction
   **fails**. Per FR-6, return the warning immediately and make **no upstream
   call**.

### 10.2 Optional-parameter normalization
Recognized optionals and their aliases (config-driven):
- `location` ← `country`, `region`.
- `content_size` ← `size`, `contentSize`, `detail`.
- `search_recency_filter` ← `recency`, `freshness`, `time_filter`, `date_filter`.

Values are forwarded under z.ai's canonical name. We do **not** validate or
translate enum *values* (out of scope; if z.ai rejects a value, the error
surfaces honestly). Optionals are read shallowly from the top-level object.

### 10.3 What we send to z.ai
Exactly: `search_query` (extracted) plus any recognized optionals that were
provided. **Everything else is dropped.** This guarantees z.ai receives a
schema-valid `tools/call` regardless of what the agent sent. (This inverts the
first revision's "pass unknown params through untouched" stance — that stance is
incompatible with "always send z.ai a clean call.")

### 10.4 Extraction source is logged and surfaced
The `source` (which key/structure the query came from, and `ambiguous`) is logged
on every delegated call (section 15) and, when non-canonical, summarized in the
warning so the agent can verify it guessed right.

## 11. Upstream delegation (`upstream.go`)

### 11.1 Session lifecycle
- One shared MCP client session to z.ai (SDK `ClientSession`), initialized lazily
  on first `tools/call`, using the inbound request's `Authorization` header.
- The session (`mcp-session-id`) is reused across calls. Guarded by a mutex;
  concurrent inbound calls may share it (z.ai keys are expected to tolerate
  concurrent use).
- On a session-expiry signal from z.ai (404 / invalid-session), re-run
  `initialize` once transparently and retry the call. If it still fails, surface
  the upstream error.

### 11.2 Timeouts and cancellation
- The outbound request lifetime is governed by the **client context** (the
  agent's request cancellation propagates to z.ai), not a hard body timeout, so
  long SSE result streams are not cut off.
- A response-header timeout (~30s) detects a dead upstream quickly.

### 11.3 Result handling
Read z.ai's `tools/call` result content (the stringified-array `text` payload).
If usage was non-canonical, **append** the warning+example text block **after**
the result content (FR-6). Preserve the original content in order. Never set
`isError`. If extraction failed (10.1.5), this step is skipped entirely — the
warning was already returned and no upstream call was made.

## 12. Teaching signal (`teach.go`)

### 12.1 When a warning is added
- **No warning** when: called tool == `web_search` **and** the query came from
  the `query` parameter (the canonical call).
- **Warning appended after results** otherwise: wrong tool name, wrong param
  name, nested/inferred/bare-string/array extraction, or normalized optionals.
- **Warning returned immediately (no results, no upstream call)** only when
  extraction found no usable query (section 10.1.5).

### 12.2 Warning text (with example)
Appended after successful results — a **warning** plus a concrete correct-usage
example:

```
[web-search-prime-fixer] Warning: this call used "<tool>"/"<source>" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```

When extraction was ambiguous or inferred, `<source>` reflects that (e.g.
`inferred:messages[0].content`) so the agent can confirm it searched the right
thing.

When extraction failed entirely (the immediate, no-results case):

```
[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```

`isError` is never set for any normalization, guidance, or warning case.

### 12.3 Why appended, and why results must accompany the warning
A warning returned **without** results tempts the model to retry the tool call.
We therefore run the search first and return results + warning together, with the
warning trailing so the results are what the model acts on. The only note that
travels without results is the "I could not find a query" warning, where there is
genuinely nothing to return and a retry with correct input is exactly what we
want.

## 13. MCP SDK (decided: adopt the official SDK)

We adopt the official **`github.com/modelcontextprotocol/go-sdk`**. Evaluated
alternatives and rationale (recorded for context, not for re-decision):

| | Official | Community |
| --- | --- | --- |
| Module | `github.com/modelcontextprotocol/go-sdk` | `github.com/mark3labs/mcp-go` |
| Latest | **v1.6.1** (stable, post-1.0) | v0.55.1 (pre-1.0) |
| Churn | low; ships a conformance suite | very high (~55 versions, frequent betas) |

The community SDK was rejected (pre-1.0, heavy churn). Stdlib-only was rejected
because the scope is now a full MCP **server** *and* an MCP **client** to z.ai,
both over Streamable HTTP — exactly the surface the SDK exists to handle. The SDK
owns the riskiest code: `NewStreamableHTTPHandler` (server transport:
initialize, `Mcp-Session-Id`, SSE framing, JSON-RPC dispatch, `tools/list`,
`tools/call`) and `StreamableClientTransport` + `client.Connect` +
`session.CallTool` (client to z.ai). This retires the bug classes that bit the
first attempt (SSE multi-line `data:` rejoining, comment/heartbeat preservation,
HTML escaping, the >64 KiB line-length scanner limit, the required
`Accept: text/event-stream` token, `Mcp-Session-Id` lifecycle). Our
normalize → delegate → teach logic sits cleanly above the SDK in the tool handler
we pass to `AddTool`. The cost is one dependency (acceptable); the original
"build fully offline / no `go.sum`" purity is explicitly relinquished.

## 14. Architecture and file layout

```
MCP client (pi / Claude Code / Cursor)  -- toolPrefix: "none" --
   |  POST /mcp  (JSON-RPC over HTTP, SSE responses)
   v
web-search-prime-fixer   <-- 127.0.0.1:8787, Go binary (Go MCP SDK)
   |  - owns tools/list (one tool: web_search)
   |  - on tools/call: extract query -> delegate -> return results (+ trailing warning)
   v
https://api.z.ai/api/mcp/web_search_prime/mcp   (one client session)
```

Routes: `/healthz` → health handler; everything else → the SDK's
`StreamableHTTPHandler`.

```
web-search-prime-fixer/
  go.mod                 module web-search-prime-fixer; go 1.22+; requires github.com/modelcontextprotocol/go-sdk
  main.go                config load, server bootstrap (mount StreamableHTTPHandler), graceful shutdown
  config.go              Config struct, defaults, file + env loading
  server.go              register advertised tools (AddTool), dispatch handler: extract -> delegate -> teach
  tools.go               tool definitions: canonical schema + (optional) terse aliases
  extract.go             query extraction from arbitrary structure (pure, table-tested)
  upstream.go            z.ai MCP client (SDK): lazy session, re-init, CallTool
  teach.go               canonical set + warning text (with example) + append-after-results rule
  logger.go              redacting structured JSON logger
  health.go              /healthz handler (or inline in main.go)
  doc.go                 package comment
  config.example.json    documented example config
  README.md              install + run + client config (incl. toolPrefix:none)
  extract_test.go        table-driven extraction tests (every structure shape)
  teach_test.go          canonical/warning + append-order + no-results-case tests
  server_test.go         end-to-end via SDK in-memory transport with a fake z.ai
  upstream_test.go       session lifecycle + re-init tests
  testdata/              golden fixtures (initialize, tools_call)
  PRD.md                 this design doc
```

Kept from the first revision: `config.go` (extended), the redacting JSON logger,
`/healthz`, graceful shutdown. Removed: the byte-forwarding `proxy.go` and the
verbatim-`tools/list` passthrough (and, with the SDK, the hand-rolled SSE
reader/writer). Added: `server.go`, `tools.go`, `extract.go`, `upstream.go`,
`teach.go`.

## 15. Logging

Structured JSON lines to stderr (stdout stays clean). Redacting logger wraps
output; any header named `Authorization`, `Cookie`, `Set-Cookie`, or
`Proxy-Authorization` prints as `<redacted>`.

Events:
- `startup`: resolved config (advertised tools, alias order, canonical, listen,
  upstream, log level). Never credentials.
- `delegate`: per `tools/call` — `called_tool`, `source` (where the query came
  from: `query`/`bare-string`/`nested:...`/`inferred:...`/`ambiguous`), `canonical`
  (bool), `optionals` (normalized names provided), `warning` (bool), upstream
  status, latency.
- `extract_failed`: when section 10.1.5 triggered (no upstream call made).
- `upstream_error`: non-2xx / session-expiry / re-init attempts.
- `shutdown`: on signal.

Levels honored; `debug` adds the raw inbound `arguments` shape (never headers).

## 16. Health and operations

- `GET /healthz` → `200` with `{"ok":true,"version":"<version>"}`. Does not touch
  the upstream. Version via `-ldflags "-X main.version=..."`, default `dev`.
- Graceful shutdown: on `SIGINT`/`SIGTERM`, `server.Shutdown(ctx)` with a 10s
  deadline, then exit. In-flight upstream calls are cancelled via context.

## 17. Headers, credentials, security

- Forward `Authorization` verbatim to z.ai; never read, log, or store it.
- Dial only `cfg.Upstream`. No path/host forwarding; not usable as an open proxy.
- Bind `127.0.0.1` only. TLS termination unnecessary (loopback HTTP to a TLS
  upstream).

## 18. Configuration (`config.go`)

### 18.1 Schema
```go
type Config struct {
    Upstream        string              // z.ai MCP endpoint
    Listen          string              // bind address
    Path            string              // reserved (informational; default "/mcp")
    Tools           []string            // advertised tool names; Tools[0] is canonical
    CanonicalTool   string              // default "web_search"
    CanonicalParam  string              // default "query"
    QueryAliases    []string            // extraction key priority order
    OptionalAliases map[string][]string // z.ai canonical optional <- aliases
    TargetTool      string              // z.ai tool to call (always "web_search_prime")
    TargetParam     string              // z.ai param (always "search_query")
    LogLevel        string              // debug | info | warn | error
}
```

### 18.2 Defaults
- `Upstream`: `https://api.z.ai/api/mcp/web_search_prime/mcp`
- `Listen`: `127.0.0.1:8787`
- `Path`: `/mcp`
- `Tools`: `["web_search"]` (single entry; `search` may be added to catch that
  guess; never `web_search_prime`)
- `CanonicalTool`: `web_search`; `CanonicalParam`: `query`
- `QueryAliases`: `["query","search_query","q","search","searchQuery","search_term","term","text","input","prompt","question","keywords","topic","searchString"]`
- `OptionalAliases`: `{ "location": ["country","region"], "content_size": ["size","contentSize","detail"], "search_recency_filter": ["recency","freshness","time_filter","date_filter"] }`
- `TargetTool`: `web_search_prime`; `TargetParam`: `search_query`
- `LogLevel`: `info`

With these defaults the server runs with no config file at all.

### 18.3 Discovery and precedence
1. `WSPF_CONFIG`, else first existing of `./web-search-prime-fixer.json`,
   `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json`.
2. If none found, use defaults.
3. Env overrides (highest): `WSPF_UPSTREAM`, `WSPF_LISTEN`, `WSPF_LOG_LEVEL`.
4. Unknown JSON fields ignored. Validation: `Listen` parses, `Upstream` is an
   absolute URL, `Tools` is non-empty and contains `CanonicalTool`, and no entry
   equals `TargetTool` (we never advertise z.ai's real name); else exit with a
   clear error.

## 19. Test plan

### 19.1 `extract_test.go`
Table-driven over every structure: each alias key (shallow); nested wrapper
(`{"input":{"query":"x"}}`); value-is-object (`{"query":{"text":"x"}}`);
bare-string `arguments`; numeric/boolean coercion; single-string object;
multi-string (longest-wins, `ambiguous`); array (`["x"]`, array-of-objects with a
string inside); chat-shaped `messages`; empty object; `null`. Assert extracted
query, source label, forwarded optionals, dropped keys, and the failure path
(10.1.5).

### 19.2 `teach_test.go`
Assert: no warning for canonical; warning **appended after** results for every
other case; the immediate no-results warning when extraction fails; `isError`
never set; example text present and correct.

### 19.3 `server_test.go` (end-to-end)
Fake z.ai via the SDK's in-memory transport (or `httptest`) that records the
`tools/call` it receives. Cases:
1. `web_search` + `{ "query": "x" }` → upstream gets `web_search_prime`/
   `search_query` exactly; client gets results, no warning.
2. `web_search` + `{ "q": "x", "junk": 1 }` → upstream gets clean `search_query`;
   junk dropped; client gets results **then** warning.
3. `web_search` + bare-string / nested / array argument → extracted and forwarded;
   results + warning.
4. `web_search` + `{}` → no upstream call; client gets the immediate no-results
   warning, `isError:false`.
5. `tools/list` advertises exactly `Tools`; only `web_search` carries a full
   description; never `web_search_prime`.
6. Upstream session-expiry → one transparent re-init, then success.

### 19.4 Golden fixtures
`testdata/initialize.*` and `testdata/tools_call.*` captured from the verified
wire format (section 8) for the upstream-client tests.

## 20. Client configuration (the Part A unlock)

`~/.pi/agent/mcp.json` — note the top-level `settings.toolPrefix`:

```json
{
  "settings": { "toolPrefix": "none" },
  "mcpServers": {
    "web_search": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${Z_AI_API_KEY}" }
    }
  }
}
```

`toolPrefix: "none"` makes pi expose each tool by its bare name
(`pi-mcp-adapter/types.ts` `formatToolName`), so `web_search` is callable as
`web_search` rather than `web_search_web_search_prime`. This is global across all
of the operator's MCP servers; it is safe for the operator's current
`web_search` + `zread` set, and only matters if two servers ever advertise the
same tool name.

Equivalent configuration for other clients (Claude Code, Cursor) is whatever
makes them expose bare tool names; document per-client in the README as needed.

## 21. Implementation order

1. `config.go` (extended) + `main.go` serving `/healthz` and a no-op SDK handler.
2. `extract.go` + `extract_test.go`; table green.
3. `teach.go` + `teach_test.go`.
4. `upstream.go` + `upstream_test.go`: z.ai session via `StreamableClientTransport`,
   `CallTool`, re-init.
5. `tools.go` + `server.go`: register advertised tools (`AddTool`), wire the
   dispatch handler (extract → delegate → return results, then append warning).
6. `server_test.go` end-to-end with the fake upstream.
7. `README.md` + `config.example.json` (including the Part A client config).
8. `go vet ./...` and `go test ./...` clean.

## 22. Success criteria

- An agent that sends `web_search` / `{ "query": "x" }` gets correct results on
  the first call, no retry, no warning.
- An agent that sends any other parameter name or any mangled structure also
  succeeds on the first call; a warning with a correct-usage example is appended
  after the results.
- When the input contains no usable query, the warning is returned immediately
  (no upstream call) and tells the agent exactly how to call correctly.
- The agent's tool list shows exactly one tool (`web_search`); no z.ai-branded
  names are ever advertised.
- z.ai always receives a schema-valid `web_search_prime` / `search_query` call.
- Runs as one Go binary built on the official Go MCP SDK.

## 23. Open decisions (minor shipped-defaults)

1. **Teaching-note verbosity.** Keep the recommended single warning line with
   example, or shorten further? (Agents reportedly read little of this.)
2. **Whether to ship `search` as a second advertised tool** by default, or keep
   the single `web_search` entry until logs justify adding it. (Configurable
   regardless; this sets the shipped default.)

The SDK decision, the single-tool default, the no-z.ai-branded-names rule, and
the append-warning-after-results timing are all **decided** and reflected above.
