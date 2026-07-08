# Research: MCP Streamable HTTP transport — session lifecycle, expiry & reconnection (go-sdk v1.6.1)

> **Sourcing note.** This brief was built primarily from the locally-vendored
> `github.com/modelcontextprotocol/go-sdk v1.6.1` source (`go.mod` confirms
> v1.6.1; module cache `~/go/pkg/mod/github.com/modelcontextprotocol/go-sdk@v1.6.1`).
> The Go SDK source **quotes the MCP spec verbatim** in its comments, so the
> spec quotes below are exact. The `web_search` tool was not available in this
> environment, so exact live page *anchors* could not be re-verified against
> modelcontextprotocol.io; section numbers and quoted text are reliable, and
> the URL fragments shown are taken directly from the SDK source. See **Gaps**.

## Summary

The MCP Streamable HTTP transport models sessions with a server-assigned
`Mcp-Session-Id`. A server MAY terminate a session at any time; afterwards it
MUST answer any request carrying that id with **HTTP 404** — there is **no
dedicated JSON-RPC error code** for "session not found." The official Go SDK
maps that 404 to the `ErrSessionMissing` sentinel (a terminal condition),
treats transient `500/502/503/504/429` as retryable (does not break the
connection), and exposes `StreamableClientTransport.MaxRetries` (default 5) for
SSE-reconnect attempts. Correctly re-initializing means building a **new**
transport + `Client.Connect` (the new `initialize` carries *no* session id and
the new `notifications/initialized` carries the *new* id), done **once**, then
surfacing any further failure honestly.

## Findings

### 1. Spec: session lifecycle / termination (the "MAY terminate → 404" rule)

1. **Exact spec text, quoted verbatim in go-sdk `mcp/streamable.go` (`checkResponse`):**
   > §2.5.3: "The server MAY terminate the session at any time, after which it
   > MUST respond to requests containing that session ID with HTTP 404 Not
   > Found."
   — `streamable.go`, comment in `streamableClientConn.checkResponse`. This is the canonical "session expiry" mechanism in the Streamable HTTP transport.
2. **Session ID is assigned by the server at initialization** via the `Mcp-Session-Id` response header on the message carrying the `InitializeResult`. — `streamable_client.go` design doc ("§2.5 … A server using the Streamable HTTP transport MAY assign a session ID at initialization time, by including it in a `Mcp-Session-Id` header on the HTTP response containing the InitializeResult"); server side sets it in `servePOST` (`if c.sessionID != "" && isInitialize { w.Header().Set(sessionIDHeader, c.sessionID) }`).
3. **Server-side implementation of the 404** (confirms the mechanism, not just the spec): when a request carries an `Mcp-Session-Id` that is not in the session table (non-stateless), the handler responds **404 with body "session not found"**: `if sessInfo == nil && !h.opts.Stateless { http.Error(w, "session not found", http.StatusNotFound); return }` — `streamable.go`, `StreamableHTTPHandler.ServeHTTP`. The DELETE (client-initiated termination) is `204 No Content`.
4. **Lifecycle as implemented by the client** (design doc): session begins after `InitializeResult` (id captured, standalone SSE started); it ends when the client `Close()`s (sends DELETE) **or the server returns 404**. — `streamable_client.go`, "Sessions" + "Connection Lifecycle".

Canonical spec URLs referenced by the SDK source (use the version matching your negotiated protocol):
- 2025-06-18 base page: `https://modelcontextprotocol.io/specification/2025-06-18/basic/transports` (SDK cites `#sending-messages-to-the-server`, `#listening-for-messages-from-the-server`, `#protocol-version-header`)
- 2025-11-25 base page: `https://modelcontextprotocol.io/specification/2025-11-25/basic/transports` (SDK cites `#streamable-http`, `#sending-messages-to-the-server`, `#resumability-and-redelivery`)
- Legacy (2025-03-26) transport page, referenced by `StreamableHTTPHandler`: `https://modelcontextprotocol.io/2025/03/26/streamable-http-transport.html`
- "Session Management" subsection: best-guess fragment `…/basic/transports#session-management` (anchor **not** re-verified live; section text ≈ §2.5.x per SDK's internal numbering).

### 2. Client best practices on session-expiry (re-init once, then fail honestly)

1. **404 is terminal → re-initialize transparently once, then retry the original call.** The SDK never auto-reconnects a 404; it surfaces `ErrSessionMissing`. A delegating client/proxy should detect that, perform **one** fresh initialize, then replay the original `tools/call`. If it still fails, surface the error verbatim — **never fabricate a result.**
2. **Transient vs terminal distinction is load-bearing.** Transient HTTP statuses (`500, 502, 503, 504, 429`) are retried *without* breaking the connection (no re-init needed); only a 404/session-missing path requires re-init. Conflating them causes either spurious re-init storms or dead sessions that never recover. — `streamable.go`, `isTransientHTTPStatus` + `checkResponse`.
3. **Guard against reconnect storms / infinite loops.** The SDK itself added a "retries-without-progress" guard (issue #679) so SSE reconnects bail after `MaxRetries` attempts when no event advances. A re-init-once layer should add its own cap (e.g. re-init **at most once** per logical request) plus a small backoff, and must not retry 404 with the same id indefinitely.

### 3. JSON-RPC error code vs. HTTP 404

1. **There is no JSON-RPC error code for "session not found."** Session validity is enforced at the **HTTP layer** via 404 (body "session not found"). — server `ServeHTTP` (404 path) and client `checkResponse` (404 → `ErrSessionMissing`). Standard JSON-RPC codes (`-32700/-32600/-32601/-32602/-32603`) and MCP's transport errors cover other failures; an *invalid/expired session* is explicitly an HTTP 404 concern, not a JSON-RPC code.
2. **Practical implication:** a 404 can arrive before any JSON-RPC body exists, so clients must check the HTTP status (the SDK does this in `checkResponse`), not wait for a JSON-RPC error object.

### 4. Official Go SDK reconnection / session-expiry handling

1. **`MaxRetries` field** on `StreamableClientTransport`:
   > "MaxRetries is the maximum number of times to attempt a reconnect before giving up. It defaults to 5. To disable retries, use a negative number."
   — `streamable.go`, `StreamableClientTransport`. In `Connect`, `0` → 5, negative → 0. These retries apply to **SSE-stream reconnection**, with exponential backoff + full jitter (`reconnectGrowFactor 1.5`, `reconnectMaxDelay 30s`, `reconnectInitialDelay 1s`).
2. **404 is terminal** (not retried): `checkResponse` returns an error **wrapping `ErrSessionMissing`** on 404, and the connection is marked failed (`c.fail(...)` from the caller), so subsequent Reads/Writes fail. — `streamable.go`, `checkResponse` + `fail`.
3. **Transient 5xx/429 are retried, connection survives:** `isTransientHTTPStatus` returns true for `500, 502, 503, 504, 429`; such responses are wrapped with `jsonrpc2.ErrRejected` so they do **not** set a write error or break the logical session. — `streamable.go`, `checkResponse` + `isTransientHTTPStatus`.
4. **`ErrSessionMissing` sentinel** (package `mcp`):
   > `// ErrSessionMissing is returned when the session is known to not be present on the server.`
   > `var ErrSessionMissing = errors.New("session not found")`
   — `transport.go`. `Close()` uses `errors.Is(c.failure(), ErrSessionMissing)` to **skip the redundant DELETE** when the server has already discarded the session. — `streamable.go`, `streamableClientConn.Close`.

### 5. Gotchas specific to the re-initialize flow (critical for the PRP)

1. **Re-init = a brand-new transport + `Client.Connect`, not re-sending `initialize` on the existing session.** `Client.Connect` does the full handshake on a fresh `streamableClientConn`: `connect(...)` → `initialize` → `hc.sessionUpdated` (captures the NEW id) → `notifications/initialized`. — `client.go`, `Client.Connect`.
2. **Why you can't reuse the live connection:** `setMCPHeaders` always attaches `c.sessionID` when non-empty. After the first init, `c.sessionID` holds the **old** id, so a re-sent `initialize` would POST with the stale id and the server would 404 it; and if a response somehow carried a different id, `Write` errors `"mismatching session IDs"`. The fresh transport starts with `sessionID == ""`, so its `initialize` correctly carries **no** `Mcp-Session-Id`. — `streamable.go`, `setMCPHeaders`, `Write`.
3. **The `notifications/initialized` POST (and the retried `tools/call`) carry the NEW id.** After `sessionUpdated`, `c.sessionID` is the new value, so every subsequent request — including the trailing `notifications/initialized` of the handshake — is stamped with it. — `client.go` `Client.Connect` ordering + `streamable.go` `sessionUpdated`/`setMCPHeaders`.
4. **Test-harness consequence (directly answers your concern): a fake upstream must NOT blanket-404 every session id.** It must 404 only the *expired* id and, on a fresh `initialize`, accept the request and return a **new** `Mcp-Session-Id`; all later requests (the `notifications/initialized` + `tools/call`) arrive under the new id and must succeed. If the harness 404s the new id too, the client sees a permanent `ErrSessionMissing` and (correctly) gives up — making a re-init-once test impossible to pass. — derived from server `ServeHTTP` (404 only when id unknown) + `Client.Connect` (new id issued at initialize).

## Pitfalls checklist for a re-init-once implementation

- **Don't retry a 404 with the same id.** 404/`ErrSessionMissing` is terminal → tear down and re-init, don't loop.
- **Re-init exactly once per logical request** (cap = 1), then surface the real error. Never synthesize a `tools/call` result.
- **Build a new `StreamableClientTransport` and call `Client.Connect`**; never re-send `initialize` on the dead `ClientSession` (it would carry the stale `Mcp-Session-Id`).
- **Ensure the new `initialize` carries NO `Mcp-Session-Id`** (happens automatically on a fresh transport); capture the NEW id from the `InitializeResult` response; the retried `tools/call` must use the NEW id.
- **Serialize re-init under a lock.** Several in-flight `tools/call` on the same dead session will all see `ErrSessionMissing`; only one should perform the re-init, and the others must retry against the **new** session or fail honestly.
- **Distinguish transient (500/502/503/504/429 → just retry, session survives) from terminal (404 → re-init).** The two need different code paths.
- **Add backoff + jitter before re-init** to avoid a reconnect storm during an upstream-wide expiry/outage.
- **Test harness must 404 only the expired id**, accept a fresh `initialize`, issue a NEW `Mcp-Session-Id`, and allow the new session through — blanket-404 breaks re-init-once.
- **Watch the standalone SSE path separately:** MaxRetries=5 and the #679 no-progress guard govern SSE reconnect; a re-init at the application layer is independent.

## Sources

**Kept (primary, locally verified):**
- `mcp/streamable.go` (go-sdk v1.6.1) — `checkResponse` (§2.5.3 quote + 404→ErrSessionMissing + transient 5xx/429), `isTransientHTTPStatus`, `Close` (skip DELETE on ErrSessionMissing), `StreamableClientTransport.MaxRetries`, `Connect` default=5, `setMCPHeaders`, `Write` (mismatched-IDs), `handleSSE` (#679 no-progress guard), `StreamableHTTPHandler.ServeHTTP` (server 404 "session not found"; DELETE→204; session-id header on initialize).
- `mcp/streamable_client.go` (go-sdk v1.6.1) — client design doc: session lifecycle, `Mcp-Session-Id` capture, §2.5/§2.2.3 references, error taxonomy (terminal 404 vs transient).
- `mcp/transport.go` (go-sdk v1.6.1) — `ErrSessionMissing` declaration + doc.
- `mcp/client.go` (go-sdk v1.6.1) — `Client.Connect` handshake order (initialize → sessionUpdated → notifications/initialized).
- Spec pages cited inline by the SDK: `modelcontextprotocol.io/specification/2025-06-18/basic/transports` and `…/2025-11-25/basic/transports` (section numbers per SDK quotes).

**Dropped:** none excluded for cause; no web search results were available to filter (see Gaps). No secondary/blog sources were used.

## Gaps

- **Exact live spec URL fragments not re-verified.** The `web_search`/URL-fetch tool was unavailable in this environment. Section numbers (e.g. §2.5.3, §2.5) and all quoted spec text are exact (they appear verbatim in the go-sdk v1.6.1 source comments), but the precise `#session-management`-style anchor on modelcontextprotocol.io should be confirmed against the live page before finalizing the PRP's citation. Recommend a human one-click verify of `…/2025-06-18/basic/transports#session-management` (or the 2025-11-25 equivalent) and pinning the protocol version you negotiate.
- **Behavior across MCP spec versions:** this brief uses 2025-06-18 / 2025-11-25 numbering from the SDK. If your upstream negotiates 2025-03-26, anchor text differs slightly (`/2025/03/26/streamable-http-transport.html`); the 404-on-termination semantics are unchanged.
- **Did not independently load the live spec HTML** to confirm there is no JSON-RPC `-32xxx`-style code for invalid session beyond the 404 mechanism; conclusion (no such code) rests on the SDK's transport-level handling + the spec quotes, which is strong but not a direct read of the spec's "error codes" subsection.

## Supervisor coordination
None needed. No blocking decision; proceeded from locally-vendored primary sources. Disclosure of the web-tool limitation is recorded above under Gaps.
