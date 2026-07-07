# Research — testdata/initialize.sse + testdata/tools_call.sse (verified wire format)

Item: **P1.M3.T2.S1** (testdata golden fixtures; written to the P1M3T1S1 slot per
orchestrator path — content is the fixtures, not the SSE reader). Source of truth
for the PRP. All byte sequences below were validated by parsing them as JSON and
checking the SSE framing rules (see §8 + §12.1 of PRD.md).

---

## 1. The PRD §8 verified wire format (literal bytes)

Established by direct probe of `https://api.z.ai/api/mcp/web_search_prime/mcp`
(PRD §8). The defining quirk: **real wire has NO space after `data`** — i.e.
`data:{...}`, not `data: {...}`. The §8 code fence shows the same compactness for
the other field names (`id:1`, `event:message` — no space). The PRP's fixtures
match this literally (see §2).

### initialize (PRD §8 "initialize response")
```
id:1
event:message
data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"tools":{"listChanged":true}},"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}
```
- protocolVersion **2024-11-05**.
- serverInfo **mcp-web-search-prime / 0.0.1**.
- Has all three field lines: `id:`, `event:`, `data:`.

### tools/call (PRD §8 "tools/call response")
```
data:{"jsonrpc":"2.0","id":N,"result":{"content":[{"type":"text","text":"<json string>"}],"isError":false}}
```
- **Only** a `data:` line (no `id:`/`event:` line in the §8 block).
- `<json string>` is a **JSON-encoded STRING — a stringified ARRAY, not an object**
  (PRD §8: "The result payload is a JSON-encoded string (a stringified array), not
  an object. The proxy must keep it intact when injecting the warning.")
- This is the single most load-bearing detail for P1.M3.T3.S2 (inject must NOT
  parse-and-reserialize the text payload as an object; it must keep it as a string
  and only prepend a new content block).

---

## 2. SSE framing byte-structure rules (PRD §8.10 + §12.1)

1. **Line terminator is LF (`\n`).** No CR. (z.ai emits LF; gofixed files match.)
2. **No space after `data`.** Matches §8 exactly. (The parser strips ONE leading
   space *if present* — §12.1 — so it tolerates `data: {...}` from other servers,
   but the FIXTURE must be the z.ai form `data:{...}`.)
   - Decision: `id:`, `event:`, `data:` are ALL written with **no space** after the
     colon, matching the §8 literal block and the item shorthand
     `id:1/event:message/data:{...}`.
3. **Blank line dispatches the event** (§8.10: "An event is a sequence of field
   lines terminated by a blank line"). So every fixture ends with the data line's
   `\n` immediately followed by a second `\n` (the blank line). Net: **the file's
   last two bytes are `0a 0a`** (`\n\n`). Verified: the existing `proxy_test.go`
   `initSSE` constant ends in `"\n\n"` (§5).
4. **EOF flush** (§12.1 "Buffer a final event if the stream ends without a trailing
   blank line") is a parser concern; the canonical FIXTURES always carry the
   trailing blank line so they look like real wire. (The multi-line variant also
   carries it.)

---

## 3. tools_call text payload = stringified ARRAY (verified)

The canonical payload chosen for `testdata/tools_call.sse`:

```json
[{"title":"Example Search Result","url":"https://example.com/result","content":"Lorem ipsum dolor sit amet."}]
```

Stringified into the MCP `text` field (a JSON string value, inner quotes escaped):

```
data:{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],"isError":false}}
```

Verified (python3): the data line is valid JSON; `result.content[0].text` is a
string; that string decodes to a JSON **array** (not an object). This is the
contract P1.M3.T3.S2 depends on (prepend a warning block, leave the text string
byte-intact). JSON-RPC `"id"` is `2` (initialize used `1`).

---

## 4. Multi-line `data:` variant (for the §19.2 round-trip test)

Requirement (item §1/§3 + §19.2): "include at least one multi-line `data:`
variant." §8.10: multiple `data:` lines are joined with `\n` on read and split
again on write. So a multi-line data payload's VALUE must contain real newlines
that split cleanly back into lines.

### Where the newlines can legally live
JSON forbids raw newlines inside string values, but **allows them between tokens**
(whitespace). Therefore the multi-line fixture encodes the SAME tools/call message
as **pretty JSON with newlines only between tokens and NO indentation**, then
breaks each logical line into its own `data:` line. Using **no leading spaces**
avoids the §12.1 "strip one leading space after the colon" rule altering the
content, guaranteeing a clean round-trip (join(split(x)) == x).

```
data:{
data:"jsonrpc": "2.0",
data:"id": 2,
data:"result": {
data:"content": [{"type": "text", "text": "[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],
data:"isError": false
data:}
data:}
```
(no trailing blank line shown above for layout; the FILE ends with a blank line,
last two bytes `0a 0a`, same as the canonical fixtures.)

Verified: joining the per-line values with `\n` yields valid JSON that decodes to
the same logical message as `testdata/tools_call.sse`. This is the third file
(`testdata/tools_call_multiline.sse`) — see §6 for why it is a separate file.

### Why "no indentation" matters (gotcha)
If a line were `data:  "jsonrpc": "2.0",` (2 spaces after the colon), §12.1 strips
exactly ONE, leaving ` "jsonrpc": "2.0",` — still valid JSON, but the
round-trip wouldn't be byte-symmetric and a strict equality test could mislead.
Zero-indentation lines (`data:"jsonrpc": ...`) have no leading space to strip, so
the join is exactly the stored value. Keep indentation OUT of the multi-line
fixture.

---

## 5. Compatibility with the existing proxy_test.go `initSSE` constant (load-bearing)

`proxy_test.go` (written by P1.M1.T4.S2) already contains:

```go
// Canned initialize SSE — PRD §8 wire format, NO space after `data`, trailing
// blank line dispatches the event. (P1.M3.T2 later extracts shared fixtures.)
const initSSE = "id:1\nevent:message\n" +
	`data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",` +
	`"capabilities":{"logging":{},"tools":{"listChanged":true}},` +
	`"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}` + "\n\n"
```

That comment names this very work item ("P1.M3.T2 later extracts shared fixtures").
**`testdata/initialize.sse` MUST be byte-identical to this constant** so that
P1.M5.T1.S1 (e2e harness formalization) can later replace the inline `initSSE`
with `os.ReadFile("testdata/initialize.sse")` with zero behavior change. The
literal bytes in §1 above are identical to `initSSE` (same fields, same JSON,
`\n\n` terminator, no space after `data`). This is a hard correctness gate.

This work item does NOT modify `proxy_test.go` — it only creates the testdata
files. P1.M5.T1.S1 performs the inline→fixture swap. (Confirmed: no `.go` file
currently references `testdata/` — grep is empty.)

---

## 6. File inventory + consumer map

| File | Form | Lines | Consumed by |
|---|---|---|---|
| `testdata/initialize.sse` | canonical §8 initialize, single `data:` line | 3 fields + blank | P1.M3.T1.S1 (reader init test), P1.M5.T1.S1 (e2e harness), replaces inline `initSSE` |
| `testdata/tools_call.sse` | canonical §8 tools/call, single `data:` line, stringified-array text | 1 field + blank | P1.M3.T1.S1 (reader parse test), P1.M3.T3.S2 (inject test), P1.M5.T1.S2 (e2e rewrite+warning) |
| `testdata/tools_call_multiline.sse` | same message, multi-line `data:` (no indent) | 8 `data:` lines + blank | P1.M3.T1.S1 (multi-line join test), P1.M3.T3.S2 (multi-line re-emit test) |

The contract output names only the first two; the third is **required** by the
"include at least one multi-line `data:` variant" clause and by §19.2
("Multi-`data:`-line event round-trips"). Keeping the canonical two byte-faithful
to §8 (single `data:` line) forces the multi-line case into its own file. This
does not conflict with the inline `initSSE` identity rule (§5), which only
constrains `initialize.sse`.

---

## 7. SSE framing spec references (for the consuming reader/writer, P1.M3.T1/T3)

These pin the framing the fixtures exercise; cite in the PRP's documentation block:
- WHATWG Server-sent events (authoritative framing — "Interpreting an event
  stream", data-line join with `\n`, blank-line dispatch):
  https://html.spec.whatwg.org/multipage/server-sent-data.html#parsing-an-event-stream
- RFC 8895 (IETF SSE, same algorithm): https://www.rfc-editor.org/rfc/rfc8895
- MCP Streamable HTTP transport (protocolVersion 2024-11-05, SSE responses,
  `mcp-session-id`): https://modelcontextprotocol.io/specification/2024-11-05/basic/transports

---

## 8. Validation approach (no code ships in this item)

This item produces ONLY testdata files — there is no Go code to compile/test yet
(`sse.go`/`sse_test.go` do not exist; §20 builds them in the next step using these
fixtures). So validation is: (a) byte-structure assertions on the files, and
(b) JSON-well-formedness of each `data:` payload (joined, for the multi-line
file), and (c) the stringified-array invariant on `tools_call.sse`. Full semantic
validation is DEFERRED to the consuming tests (sse_test.go, proxy_test.go), which
will FAIL loudly if a fixture is malformed. A one-shot Go validator script (run
from /tmp, not committed) encodes (a)+(b)+(c) — see PRP Validation Loop.
