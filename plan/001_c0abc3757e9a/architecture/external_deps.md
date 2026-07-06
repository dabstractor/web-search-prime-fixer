# External Dependencies & Technical Feasibility — web-search-prime-fixer

Validation that the PRD is buildable with **Go stdlib only**. This is a RESEARCH
brief (no code). Every **Go-side** claim was **verified by reading the installed
Go toolchain source** (`go1.26.4`, `GOROOT=/usr/lib/go`) — those are on-disk
verifications, not references. **Protocol-side** claims (MCP spec, JSON-RPC 2.0,
WHATWG SSE) are from authoritative knowledge of those *stable* specs; URLs are
**canonical references, NOT live-fetched** (this subagent had no web-access tool —
see Supervisor coordination and Gaps). Confidence is consolidated in the
**Verification** section.

---

## 1. MCP Streamable HTTP transport — CONFIRMED (protocol-side, High)
- A **single HTTP endpoint** accepts `POST` and returns responses as SSE
  (`text/event-stream`) or plain `application/json`. This **replaced** the older
  two-endpoint **HTTP+SSE** transport.
- `initialize` establishes a session; the server returns **`Mcp-Session-Id`** in
  the response header. The client **MUST** resend `Mcp-Session-Id` on subsequent
  requests.
- Each JSON-RPC 2.0 message is carried in **one SSE `data:` field** (UTF-8 JSON).
  JSON-RPC `id` is a number or a string on requests/responses and absent on
  notifications; the responder echoes `id`. **Proxy relevance:** an alias rename
  that changes a request `id` MUST also rewrite the matching response `id` in the
  SSE data.
- **Version correction:** the Streamable HTTP transport was introduced in revision
  **2025-03-26** and **deprecated** the original HTTP+SSE transport (which *is*
  the 2024-11-05 mechanism: `GET /sse` opens a long-lived stream and emits an
  `endpoint` event telling the client where to `POST`; POSTs return plain JSON).
  Confirms PRD §8 (transport contract) and §11.2 (header forwarding).
- Citation: https://spec.modelcontextprotocol.io/specification/2025-03-26/basic/transports
  — canonical reference, not live-fetched. https://www.jsonrpc.org/specification

## 2. Go stdlib SSE proxying — CONFIRMED; no stdlib SSE framer, must hand-roll
- `net/http` proxies a streaming body without buffering it whole:
  - **`io.Copy(dst, resp.Body)`** streams the upstream body straight through
    (PRD §11.3 passthrough path — "common case must not buffer or parse").
  - **`http.Flusher`** flushes each SSE event so the client sees streaming chunks.
    **VERIFIED** in `/usr/lib/go/src/net/http/server.go`: `type Flusher interface
    { Flush() }`; doc: *"The default HTTP/1.x and HTTP/2 ResponseWriter
    implementations support Flusher ... Handlers should always test for this
    ability at runtime."* → guard `if f, ok := w.(http.Flusher); ok { f.Flush() }`.
- **There is NO stdlib SSE framer.** No `sse` package in the standard library;
  `net/http` serves responses but does not parse `text/event-stream` framing. The
  proxy must hand-roll the parser per PRD §12 (confidence High from knowledge +
  prior GOROOT search).
- Citation (VERIFIED, on-disk): `/usr/lib/go/src/net/http/server.go` (`Flusher`);
  https://pkg.go.dev/net/http#Flusher , https://pkg.go.dev/io#Copy

## 3. SSE framing rules (WHATWG) — CONFIRMED (protocol-side, High)
PRD §12.1 framing is correct per the WHATWG Server-Sent Events spec:
- (a) Lines beginning with U+003A COLON (`:`) are comments → ignored.
- (b) `field: value` — strip exactly ONE leading space after the colon if present
  (split on the first colon; line with no colon = field name, empty value).
- (c) Multiple consecutive `data:` lines concatenate their values with `\n`.
- (d) A blank line dispatches the accumulated event and resets the accumulator.
- (e) **A final event with no trailing blank line must still be dispatched at EOF**
  (the parser must flush the accumulator when the reader returns EOF).
- Citation: https://html.spec.whatwg.org/multipage/server-sent-events.html#parsing-an-event-stream
  — canonical reference, not live-fetched.
- **"RFC 8895" (task) is a likely misattribution:** SSE is a WHATWG Living Standard,
  not an RFC. Treat the WHATWG spec as authoritative. (Confidence on the
  misattribution itself: Medium — could not web-verify.)

## 4. Reverse-proxy header handling — CONFIRMED; manual handler is correct (Go-side VERIFIED)
- **`httputil.ReverseProxy` DOES strip hop-by-hop headers.** **VERIFIED** in
  `/usr/lib/go/src/net/http/httputil/reverseproxy.go`: type doc — *"Hop-by-hop
  headers (see RFC 9110, section 7.6.1), including **Connection, Proxy-Connection,
  Keep-Alive, Proxy-Authenticate, Proxy-Authorization, TE, Trailer,
  Transfer-Encoding, and Upgrade**, are removed from client requests and backend
  responses."* (RFC **9110** HTTP Semantics supersedes RFC 7230 §6.1; list
  includes de-facto `Proxy-Connection`.) Customization: `Rewrite func(*ProxyRequest)`
  (request) + `ModifyResponse func(*http.Response) error` (response).
- **A manual `http.Handler` is the right choice** (PRD correctly does NOT use
  ReverseProxy), because the proxy needs:
  1. **Request-side body mutation** (conditional alias → search_query rename inside
     the JSON-RPC body) before forwarding. ReverseProxy exposes the request only
     via `Rewrite`/`Director`; by then the body is committed, and conditionally
     re-serializing a rewritten body is cleaner in a custom handler.
  2. **A conditional response path** keyed on whether a rewrite occurred:
     passthrough = `io.Copy` verbatim; rewritten = feed through the SSE injector.
     **VERIFIED:** ReverseProxy's response is documented as copied back
     *"unmodified"*, so its `ModifyResponse` operates on the whole response and
     **fights streaming** — it cannot cleanly express "stream verbatim unless I
     rewrote".
  3. **Per-response SSE injection keyed on the rewritten request-id set** — a
     stateful request↔response correlation that fits a custom handler.
- **Action:** in the manual handler, when copying upstream response headers
  forward, reuse the stdlib hop-by-hop set verbatim (the list above); forward a
  header allowlist including `Mcp-Session-Id` (see §8) and drop the hop-by-hop set
  (PRD §11.2/§11.3).
- Citation (VERIFIED, on-disk): `/usr/lib/go/src/net/http/httputil/reverseproxy.go`;
  https://pkg.go.dev/net/http/httputil#ReverseProxy

## 5. Context cancellation for long SSE streams — CONFIRMED (Go-side VERIFIED)
- Carry the inbound context upstream: `outReq = outReq.WithContext(r.Context())`
  (or `http.NewRequestWithContext`). On client disconnect the context cancels, the
  upstream `*http.Response.Body` read errors, the copy loop exits, and a deferred
  `resp.Body.Close()` runs (PRD §17).
- **Do NOT set `http.Client.Timeout`** — it is a whole-exchange deadline that
  **includes reading the response body**, so it cuts off long SSE streams. Use
  `&http.Client{}` with no `Timeout`; rely on context cancellation.
- **`http.Transport.ResponseHeaderTimeout`** is the correct dead-upstream knob.
  **VERIFIED** in `/usr/lib/go/src/net/http/transport.go` (Transport field), doc:
  *"ResponseHeaderTimeout ... specifies the amount of time to wait for a server's
  response headers after fully writing the request (including its body, if any).
  **This time does not include the time to read the response body.**"* → bounds
  time-to-first-byte on headers without bounding the SSE body. PRD §17 specifies
  30s; pair with a `Transport.DialContext` dial timeout.
- Citation (VERIFIED, on-disk): `/usr/lib/go/src/net/http/transport.go`;
  https://pkg.go.dev/net/http#Transport

## 6. Go map ordering is non-deterministic — CONFIRMED (drives the algorithm)
- Iterating a `map[K]V` yields keys in **randomized** order (runtime intentional
  randomization); the Go spec states the order is *"not specified and is not
  guaranteed to be the same from one iteration to the next."*
- **This is WHY the PRD §10 rewrite algorithm iterates the alias SLICE
  (`[]Alias`, config order) — NOT the args `map[string]any` — to pick a target.**
  "Promote the first alias" is deterministic only because the alias list is an
  ordered slice. The implementer must **never** range over the args map to pick an
  alias — that would make the choice nondeterministic run-to-run (correctness bug,
  not style).
- Citation: https://go.dev/ref/spec#For_statements ; https://go.dev/blog/maps

---

## 7. FINDING (robustness) — `bufio.Scanner` 64 KB token limit (Go-side VERIFIED)
**VERIFIED** in `/usr/lib/go/src/bufio/scan.go`: `const MaxScanTokenSize = 64 *
1024`; `NewScanner` defaults to the `ScanLines` split at that max. A `Scanner`
errors (`ErrTooLong`) / truncates on a single token > 64 KB unless
`Scanner.Buffer(buf, N)` raises the limit.

A `tools/call` result embedded as a stringified-JSON `data:` line (high context,
~2500 words, or multi-result) can **exceed 64 KB**, silently breaking a naive
`Scanner`-based framer.

**Recommendation for `sse.go`:** prefer `bufio.Reader` with
`ReadString('\n')` / `ReadBytes('\n')` (no line-length limit) over `Scanner`; if
`Scanner` is used, call `Scanner.Buffer(buf, bigN)`. Cover with a test fixture
whose `data:` line is > 64 KB.
- Citation (VERIFIED, on-disk): `/usr/lib/go/src/bufio/scan.go`;
  https://pkg.go.dev/bufio#Scanner , https://pkg.go.dev/bufio#Reader.ReadString

## 8. FINDING — `Mcp-Session-Id` header canonicalization (High)
Go `net/http` canonicalizes header keys via `textproto.CanonicalMIMEHeaderKey`,
so `Mcp-Session-Id` ↔ `mcp-session-id` are identical on the wire and through
`Header.Get`/`Set`. The proxy can read/forward either casing; no special handling
beyond forwarding the header verbatim on session-bearing requests (PRD §8/§11).

---

## Verification (consolidated)

| # | Point | Method | Confidence |
|---|-------|--------|------------|
| 1 | MCP Streamable HTTP transport | Knowledge + canonical URL (not web-fetched) | High (mechanism) / Medium (exact revision) |
| 2a | `io.Copy` + `http.Flusher` streaming | **VERIFIED, GOROOT** `net/http/server.go` | High |
| 2b | No stdlib SSE framer | Knowledge + prior GOROOT search (cannot enumerate) | High |
| 3 | WHATWG SSE framing edge cases | Knowledge + canonical URL (not web-fetched) | High |
| 3n | "RFC 8895" is a misattribution | Knowledge (could not web-verify) | Medium |
| 4a | ReverseProxy strips hop-by-hop (RFC 9110) | **VERIFIED, GOROOT** `net/http/httputil/reverseproxy.go` | High |
| 4b | Manual handler preferred (unmodified copy) | Inference from VERIFIED doc | High |
| 5 | `ResponseHeaderTimeout` ≠ body-time | **VERIFIED, GOROOT** `net/http/transport.go` | High |
| 6 | Map iteration randomized; iterate alias slice | Knowledge + Go spec (canonical URL) | High |
| 7 | `bufio.Scanner` 64 KB limit; prefer `bufio.Reader` | **VERIFIED, GOROOT** `bufio/scan.go` | High |
| 8 | `Mcp-Session-Id` canonicalization | Knowledge (`textproto`) | High |

**Net:** 4 of the 6 requested points have ≥1 on-disk source verification; points
1 & 3 are protocol-side (stable-spec knowledge + canonical, not live-fetched, URLs).

---

## Gaps
- **No live web verification** (no `web_search`/fetch tool). MCP / JSON-RPC / WHATWG
  URLs are canonical references, not retrieved. A later web-enabled pass should
  (a) confirm the exact `spec.modelcontextprotocol.io` Streamable HTTP section
  anchor, (b) confirm the revision designator (`2025-03-26` vs newer `2025-06-18`),
  (c) determine what `RFC 8895` actually is (suspected misattribution — Medium).
- **"No stdlib SSE framer"** is stated from knowledge + a prior GOROOT search; this
  subagent cannot enumerate directories with `read`, so absence is High-confidence
  from knowledge, not fresh enumeration.
- **MCP `initialize`/session lifecycle details** are High confidence from knowledge
  but not spec-quote-verified.

## Supervisor coordination
- One `need_decision` contact (no `web_search` tool); supervisor chose **Option A**
  (proceed from authoritative knowledge + on-disk Go-source verification) and
  additionally requested this brief at `plan/001_c0abc3757e9a/architecture/external_deps.md`.
  The identical brief was also written to the authoritative run-output path
  `.../outputs/58808913/research.md` to satisfy the run-harness contract.
- No code was written (RESEARCH ONLY). No further blocking decisions remain.
