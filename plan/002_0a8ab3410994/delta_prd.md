# web-search-prime-fixer — Delta PRD (v1.0 → v2.0)

**Transparent proxy → normalizing MCP server.** This delta captures ONLY what
changes relative to the completed v1.0 (session `001_c0abc3757e9a`). The
authoritative new PRD is `plan/002_0a8ab3410994/prd_snapshot.md` (also repo
`PRD.md`); section references below point at it.

## Sizing note

This is a **large** delta (a full architectural pivot), not a tweak. The v1
transparent byte-forwarding proxy **failed in production** on first real use
(new PRD §0/§1, with evidence): the client rejected wrong tool names locally
*before* any request reached the proxy, so the proxy's narrow "rename one key"
rule never ran. v2 inverts the architecture — stop being a transparent proxy,
become a real MCP server that owns `initialize`/`tools/list`/`tools/call`,
extracts a query from arbitrary input, and delegates one clean call to z.ai.

Magnitude against the existing tree (verified):
- **Removed:** `proxy.go` (559 lines), `sse.go` (314), `rewrite.go` (87), plus
  their six test files (`proxy_test.go`, `proxy_e2e_test.go`,
  `proxy_harness_test.go`, `proxy_log_test.go`, `sse_test.go`, `rewrite_test.go`).
- **Modified:** `config.go` (schema grows from 6 → 11 fields), `main.go` (mount
  the SDK handler instead of the proxy handler), `config.example.json`, `doc.go`,
  `README.md`.
- **New:** `extract.go`, `upstream.go`, `teach.go`, `server.go`, `tools.go`,
  `logger.go`, `health.go` (the last two extract existing code out of `main.go`),
  plus `extract_test.go`, `teach_test.go`, `server_test.go`, `upstream_test.go`.
- **New dependency:** official Go MCP SDK (one `require`).

A full milestone structure is therefore justified (sizing rule: "Large new
feature → Full PRD structure").

## What carries over unchanged (REUSE — do not re-implement)

Established by session `001`; the prior research at
`plan/001_c0abc3757e9a/architecture/` remains authoritative for these:

- **z.ai transport contract** (new PRD §8) — verified by direct probe in session
  001. With the SDK adopted, its `StreamableClientTransport` now owns framing /
  `Accept` negotiation / `Mcp-Session-Id` lifecycle; the contract is retained
  only as background.
- **Config discovery + precedence** (`resolveConfigPath`, `ResolveConfig`,
  `WSPF_CONFIG` / CWD / XDG, `WSPF_UPSTREAM|LISTEN|LOG_LEVEL` env overrides) —
  reused as-is. Only the *schema* and *validation* change (FR-S1).
- **Redacting structured JSON logger** (`logger`, `newLogger`, `redactHeaders`,
  currently in `main.go`) — reused; move to its own file `logger.go` (new PRD §14
  layout).
- **`/healthz`** (`healthHandler`, `var version = "dev"`, `-ldflags` injection) —
  reused; move to its own file `health.go`.
- **Graceful shutdown** (SIGINT/SIGTERM → `srv.Shutdown(ctx)` 10s) — reused.
- **`testdata/initialize.sse`, `testdata/tools_call.sse`** — reused by the new
  upstream-client tests as golden fixtures (new PRD §19.4).

## What is removed

The entire v1 core is deleted (new PRD §14 "Removed"):

- `proxy.go` — the byte-forwarding transparent handler (`newProxyHandler`,
  `forward`, `decideRewrite`, `rewriteObject`, `newUpstreamClient`,
  `copyForwardHeaders`, `isHopByHop`, `flushWriter`). The proxy's
  streamThrough-vs-rewrite response split no longer exists: we now own the
  response and build it ourselves.
- `sse.go` — the hand-rolled SSE reader/injector (`SSEReader`, `Event`,
  `Inject`, `emitEvent`, `warningText`). The SDK owns SSE framing on both sides;
  we manipulate SDK result objects, not raw SSE bytes.
- `rewrite.go` — the narrow alias-rename (`Rewrite`, `RewriteResult`). Replaced
  by the far broader query-extraction in `extract.go`.
- The six matching test files above.
- The v1 non-goals that **forbade** normalizing arguments / rewriting
  `tools/list` — explicitly **dropped** (new PRD §3). The new job IS to
  normalize and to own `tools/list`.
- The v1 "prepend warning before results" timing — replaced by **append after
  results** (new PRD §6 FR-6, §12.3) so results always accompany any warning.

These are noted for awareness; they produce no tasks of their own (their
replacements are the new requirements below).

---

## Functional requirements (the delta)

### FR-S1 — Config schema extension (modifies `config.go`, P1.M1.T2)

The `Config` struct is extended to the new PRD §18.1 schema. Discovery and
env-override logic are unchanged; only fields, defaults, and validation change.

New/changed fields (new PRD §18.1/§18.2):
- `Tools []string` (JSON `"tools"`) — advertised tool names; `Tools[0]` is
  canonical. Default `["web_search"]`.
- `CanonicalTool string` (`"canonical_tool"`) — default `"web_search"`.
- `CanonicalParam string` (`"canonical_param"`) — default `"query"`.
- `QueryAliases []string` (`"query_aliases"`) — **replaces** the old `Aliases`
  (`"aliases"`). Extraction key priority order. Default the 13-entry list in
  new PRD §18.2.
- `OptionalAliases map[string][]string` (`"optional_aliases"`) — new. z.ai
  canonical optional ← aliases (new PRD §18.2 / §10.2).
- `TargetTool string` (`"target_tool"`) — new. Always `"web_search_prime"`.
- `TargetParam string` (`"target_param"`) — retained, still always
  `"search_query"`.
- `Upstream`, `Listen`, `Path`, `LogLevel` — unchanged.
- **Note the JSON-key rename** `aliases` → `query_aliases` (a breaking config
  change; call out in README).

New/changed validation (new PRD §18.3), added to `ResolveConfig`:
- `Tools` is non-empty and contains `CanonicalTool`.
- No `Tools` entry equals `TargetTool` (we never advertise z.ai's real name).

*Docs (Mode A):* update the doc comment on `type Config` to document the new
fields and the dropped `aliases`/renamed `query_aliases` JSON key.

### FR-S2 — Adopt the official Go MCP SDK (new dependency)

`go.mod` gains a single `require`: `github.com/modelcontextprotocol/go-sdk`
(verified: v1.6.1 is stable and present in the local module cache; the prior
"stdlib-only / no `go.sum`" constraint from session 001 is explicitly
relinquished per new PRD §13). Keep the `go 1.22` directive unless the SDK
demands a higher floor (verify at build).

Confirmed SDK API surface (verified by reading the cached v1.6.1 source):
- Server: `mcp.NewServer(*mcp.Implementation, *mcp.ServerOptions) *mcp.Server`;
  `(*Server).AddTool(*mcp.Tool, mcp.ToolHandler)`.
- Server HTTP transport: `mcp.NewStreamableHTTPHandler(getServer func(*http.Request)
  *mcp.Server, *mcp.StreamableHTTPOptions)` — owns initialize / `Mcp-Session-Id`
  / SSE framing / JSON-RPC dispatch / `tools/list` / `tools/call`.
- Client: `mcp.NewClient(...)`; `(*Client).Connect(ctx, Transport,
  *ClientSessionOptions)`; `(*mcp.ClientSession).CallTool(ctx,
  *mcp.CallToolParams) (*mcp.CallToolResult, error)`.
- Client transport: `mcp.StreamableClientTransport{Endpoint, HTTPClient}`.
- Tests: `mcp.NewInMemoryTransports()` for an in-memory fake z.ai.

`main.go` (modifies P1.M1.T4) mounts the SDK's `StreamableHTTPHandler` as the
catch-all route (everything except `/healthz`), replacing `newProxyHandler`.
Pass the inbound `*http.Request` into the SDK's `getServer` closure so the
per-call Authorization header reaches the upstream client (see FR-S4).

*Docs (Mode A):* `go.mod` `require` block is self-documenting; add a one-line
comment on the handler-mount in `main.go` noting the SDK owns transport framing.

### FR-S3 — Query extraction from ANY input (new `extract.go`)

Replaces `rewrite.go` entirely. A pure, table-tested function over the inbound
tool input (delivered by the SDK). Directive: *whatever the model passed is
treated as a search query somehow* (new PRD §10).

Returns the extracted query string, a `source` label (e.g. `query`,
`bare-string`, `scalar`, `array[0]`, `nested:<key>`, `inferred:<key>`,
`ambiguous`), the normalized optionals map, and a `found bool`. Algorithm per new
PRD §10.1 precedence:

1. string → query (`bare-string`).
2. number/boolean → stringify (`scalar`).
3. array → recurse first usable element (`array[0]`), else fall through to 5.
4. object: (a) alias scan over `QueryAliases` in config order, drilling into
   object/array values via sub-keys `text/value/content/query/q/data/input`;
   (b) else infer — collect every reachable string excluding recognized optionals
   and their aliases (new PRD §10.2), pick the single one or the **longest**
   (`ambiguous=true`).
5. nothing usable → extraction **fails** (no upstream call; warning returned per
   FR-S5, new PRD §10.1.5).

Optional normalization (new PRD §10.2/§10.3): recognized optionals
(`location`←`country,region`; `content_size`←`size,contentSize,detail`;
`search_recency_filter`←`recency,freshness,time_filter,date_filter`) are
forwarded under z.ai's canonical name, read shallowly from the top level.
**Everything else is dropped** so z.ai always receives a schema-valid call.

*Docs (Mode A):* doc comment on the extraction function documenting precedence,
source labels, the "drop everything not recognized" guarantee, and that enum
*values* are not validated (out of scope).

### FR-S4 — Upstream delegation (new `upstream.go`)

`web-search-prime-fixer` acts as an MCP **client** to z.ai (new PRD §11) via the
SDK. The session is lazily initialized on first `tools/call` and reused across
calls (guarded by a mutex).

- Build a `mcp.StreamableClientTransport{Endpoint: cfg.Upstream, HTTPClient:
  <injected>}` and connect via `mcp.NewClient(...).Connect(...)`.
- **Authorization threading (key integration risk):** the SDK transport is
  context-value-aware (verified: v1.6.1 source comment "preserve context values
  for auth middleware"). Wrap the client transport's `*http.Client` `Transport`
  with a `RoundTripper` that injects the inbound `Authorization` header **from
  the per-request context** onto the outbound z.ai request. Put the header into
  the context in the tool handler (FR-S6). Forward it verbatim; never read/log it.
- On `tools/call`, send one `session.CallTool(ctx, &CallToolParams{Name:
  cfg.TargetTool, Arguments: {cfg.TargetParam: query, ...optionals}})`, read the
  result content, return it. Beyond a single transparent session-expiry re-init
  (new PRD §11.1), never retry: on a 404 / invalid-session signal, re-run
  `initialize` once and retry; if it still fails, surface the upstream error
  honestly (do not synthesize results).
- Cancellation: the outbound request lifetime follows the client context (no hard
  body timeout); a `ResponseHeaderTimeout` (~30s) detects a dead upstream
  quickly (new PRD §11.2 — same conclusion as session 001's `external_deps.md
  §5`, now applied on the SDK client's `HTTPClient.Transport`).

*Docs (Mode A):* doc comment on the upstream client documenting the lazy-shared
session, the re-init-once rule, and the auth-via-context/RoundTripper pattern.

### FR-S5 — Teaching signal: append-after, with example (new `teach.go`)

Replaces `sse.go`'s `warningText`/`Inject` with logic over SDK result objects
(not raw SSE bytes). Rules (new PRD §12):

- **No warning** when called tool == `CanonicalTool` AND the query came from
  `CanonicalParam` (the canonical call). Results only.
- **Warning appended AFTER results** otherwise (wrong tool, wrong param,
  nested/inferred/bare-string/array/scalar source, or normalized optionals).
  Append one `text` content block to the tail of the result's `content` array;
  never replace or prepend results; never set `isError: true`.
- **Warning returned immediately, no upstream call** only when extraction failed
  (FR-S3 step 5 / new PRD §10.1.5).

Warning text format (new PRD §12.2) — a single warning line plus a concrete
correct-usage example (both the after-results and the no-results variants are
specified verbatim in new PRD §12.2).

*Docs (Mode A):* doc comment on the teaching functions documenting the
canonical-pair rule, append-only ordering, the no-results exception, and that
`isError` is never set.

### FR-S6 — Server surface: tools/list, one tool, permissive schema (new `tools.go`, `server.go`)

- `tools.go` defines the advertised tools (new PRD §9): `web_search` carries the
  full description + a **permissive** `inputSchema` (`query` primary + common
  aliases as optional + `additionalProperties: true`) so a validating client
  never rejects any shape locally (new PRD FR-3). Optionally-configured alias
  tools (e.g. `search`) get a one-line description + minimal open schema and the
  same handler. **Never** advertise `web_search_prime` (new PRD §9.3) — enforced
  by config validation (FR-S1).
- `server.go` registers the tools (`AddTool`) and implements the dispatch handler:
  extract (FR-S3) → if no query found, return the failure warning (FR-S5); else
  delegate to z.ai (FR-S4) → return result content, then append a warning if
  non-canonical (FR-S5). It also handles the local methods (`ping`,
  `notifications/*`, `resources/list`, `prompts/list`, `logging/setLevel`,
  `completion/complete`) as pong/ack/empty/method-not-found (new PRD §5.2 table).

*Docs (Mode A):* doc comment on the tool-registration function documenting the
canonical tool, the permissive-schema rationale, and the no-z.ai-names rule.

### Observability delta (modifies logging usage, not the logger)

The logger itself is unchanged. The **events** change (new PRD §15): the v1
`rewrite`/`forward` events become a per-`tools/call` `delegate` event
(`called_tool`, `source`, `canonical`, `optionals`, `warning`, upstream status,
latency) plus `extract_failed` (no upstream call) and `upstream_error`
(non-2xx / session-expiry / re-init). `startup` now logs `tools`/`canonical`/
`query_aliases` instead of `aliases`. `debug` adds the raw inbound arguments
shape (never headers).

*Docs (Mode A):* the per-event field lists are documented via inline comments
where each event is emitted.

---

## Mode B — changeset-level documentation (final task)

The user-facing behavior changes wholesale, so the changeset-level docs need a
full sync after all implementing work lands:

- **`README.md`** — rewrite: describe a normalizing MCP server (not a proxy);
  the `web_search` / `query` canonical surface; the **Part A client config** with
  `settings.toolPrefix: "none"` and the `web_search` server entry (new PRD §20);
  the append-after-results warning-with-example the agent sees; the new config
  fields + env vars (call out the `aliases` → `query_aliases` breaking change);
  the SDK build (`go mod tidy` / `go build`); /healthz; and the revised
  non-goals. The v1 README's "proxy that renames query→search_query" framing is
  stale and must be replaced.
- **`config.example.json`** — update to the new schema (`tools`,
  `canonical_tool`, `canonical_param`, `query_aliases`, `optional_aliases`,
  `target_tool`, `target_param`, `upstream`, `listen`, `path`, `log_level`)
  matching `DefaultConfig()`. Valid JSON, no comments.
- **`doc.go`** — rewrite the package comment: it currently describes the
  transparent proxy and its non-goals (do-not-normalize). Rewrite to describe the
  normalizing MCP server, the one-tool surface, and the append-after-results
  teaching signal (new PRD §0/§3/§9).

---

## Implementation order (single phase, 5 milestones)

Mirrors new PRD §21. Reuse the carried-over infra; rebuild only the core.

### M1 — Foundation pivot
Extend `config.go` to the new schema + defaults + validation (FR-S1). Add the
SDK `require` and mount `mcp.NewStreamableHTTPHandler` as the catch-all route in
`main.go` with a no-op tool set (FR-S2). Extract `logger`→`logger.go` and
`healthHandler`→`health.go` (new PRD §14 layout), keeping behavior. **Delete**
`proxy.go`, `sse.go`, `rewrite.go` and their six test files. Boot + `/healthz`
green; `/healthz` still does not touch the upstream. Update `config_test.go` /
`resolve_test.go` to the new schema + validation.

### M2 — Query extraction (`extract.go` + `extract_test.go`)
Pure, table-tested (FR-S3, new PRD §19.1): every structure shape — each alias
shallow; nested wrapper; value-is-object; bare string; numeric/boolean; single-
and multi-string (longest/ambiguous); array + array-of-objects; chat-shaped
`messages`; empty object; `null`; and the failure path.

### M3 — Teaching signal (`teach.go` + `teach_test.go`)
Canonical-pair rule; append-after-results ordering; the immediate no-results
warning; `isError` never set; example text correct (FR-S5, new PRD §19.2).

### M4 — Upstream delegation (`upstream.go` + `upstream_test.go`)
SDK client to z.ai: lazy shared session, re-init-once on expiry, auth via
context+RoundTripper, one `CallTool`, honest error surfacing (FR-S4, new PRD
§11). Test with `mcp.NewInMemoryTransports()` and/or `httptest` fake z.ai using
the reused `testdata` fixtures; assert `Authorization` reaches the fake and is
never logged.

### M5 — Server wiring, end-to-end, quality gate, doc sweep
`tools.go` + `server.go` (FR-S6): register tools, wire extract → delegate →
return-results-then-append-warning. `server_test.go` end-to-end (new PRD §19.3):
canonical call (no warning); alias/junk (clean upstream call + trailing
warning); bare/nested/array argument; `{}` (no upstream call, immediate warning,
`isError:false`); `tools/list` advertises exactly `Tools` and never
`web_search_prime`; session-expiry transparent re-init. Final gate:
`go vet ./...`, `go test ./...`, `go build` clean; confirm exactly one
`require` (the SDK). **Mode B doc sweep** (README, `config.example.json`,
`doc.go`) as the final task depending on everything above.
