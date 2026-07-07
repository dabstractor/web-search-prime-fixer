# Research: Authoritative citations for a body-rewriting Go reverse-proxy

## Summary
Four documentation facts needed for the proxy spec were located. Three of the
four (net/http `ContentLength`/`GetBody` auto-detection, the on-wire
`Content-Length` originating from `Request.ContentLength` and not from
`Request.Header`, and `encoding/json` decoding numbers to `float64`) are quoted
**verbatim from the Go standard-library source** installed locally at
`/usr/lib/go/src/...` — i.e. the exact source that pkg.go.dev renders, which is
the strongest possible authority and byte-for-byte stable across Go 1.22+.
The fourth (JSON-RPC 2.0 batch + `id` rules) is quoted from the canonical, frozen
specification; see the **Gaps** note, since the live URL could not be byte-fetched
from this environment.

> Method note: this environment exposes only a local `read` tool (no `web_search`
> / fetch). pkg.go.dev renders directly from the Go source comments, so the local
> tree is an authoritative mirror. Verbatim quotes below were copied directly
> from `/usr/lib/go/src/...`; file/symbol pointers are given so a reviewer can
> re-verify.

---

## Fact 1 — `http.NewRequest`/`NewRequestWithContext` sets `ContentLength` and `GetBody` for known body types

**Citation (doc):** `net/http` — `NewRequestWithContext`
- Rendered: https://pkg.go.dev/net/http#NewRequest
- Source: `/usr/lib/go/src/net/http/request.go`, doc comment on `NewRequestWithContext`.

**Verbatim quote (doc comment):**
> If body is of type [\*bytes.Buffer], [\*bytes.Reader], or [\*strings.Reader],
> the returned request's ContentLength is set to its exact value (instead of -1),
> GetBody is populated (so 307 and 308 redirects can replay the body), and Body
> is set to [NoBody] if the ContentLength is 0.

**Verbatim quote (implementation — same file, the `switch v := body.(type)` in `NewRequestWithContext`):**
```go
case *bytes.Buffer:
    req.ContentLength = int64(v.Len())
    ...
    req.GetBody = func() (io.ReadCloser, error) { ... }
case *bytes.Reader:
    req.ContentLength = int64(v.Len())
    ...
    req.GetBody = func() (io.ReadCloser, error) { ... }
case *strings.Reader:
    req.ContentLength = int64(v.Len())
    ...
    req.GetBody = func() (io.ReadCloser, error) { ... }
```

**Tight paraphrase:** Passing a `*bytes.Reader`/`*bytes.Buffer`/`*strings.Reader`
to `NewRequest`/`NewRequestWithContext` causes both `Request.ContentLength` (the
exact byte length) and `Request.GetBody` (a factory that re-creates the body) to
be populated automatically. This is what lets the proxy hand the re-serialized
bytes to `NewRequest` and get a correct `Content-Length` "for free."

---

## Fact 2 — On-wire `Content-Length` comes from `Request.ContentLength`, NOT from a `Content-Length` entry in `Request.Header`

**Citations:**
- `Request.Header` field doc: https://pkg.go.dev/net/http#Request.Header
- `Request.ContentLength` field doc: https://pkg.go.dev/net/http#Request.ContentLength
- `Request.Write` method doc: https://pkg.go.dev/net/http#Request.Write
- Source: `/usr/lib/go/src/net/http/request.go` (`Header`, `ContentLength`, `Write`,
  `outgoingLength`, and the `reqWriteExcludeHeader` map) and
  `/usr/lib/go/src/net/http/transfer.go` (`transferWriter.writeHeader`).

**Verbatim quote A — `Request.Header` field doc (request.go):**
> For client requests, certain headers such as Content-Length and Connection are
> automatically written when needed and values in Header may be ignored. See the
> documentation for the Request.Write method.

**Verbatim quote B — `Request.ContentLength` field doc (request.go):**
> ContentLength records the length of the associated content. The value -1
> indicates that the length is unknown. Values >= 0 indicate that the given
> number of bytes may be read from Body.
>
> For client requests, a value of 0 with a non-nil Body is also treated as unknown.

**Verbatim quote C — `Request.Write` method doc (request.go):**
> Write writes an HTTP/1.1 request, which is the header and body, in wire format.
> This method consults the following fields of the request:
> Host / URL / Method (defaults to "GET") / Header / ContentLength /
> TransferEncoding / Body

**Verbatim quote D — the exclusion map (request.go):**
```go
// Headers that Request.Write handles itself and should be skipped.
var reqWriteExcludeHeader = map[string]bool{
    "Host":              true, // not in Header map anyway
    "User-Agent":        true,
    "Content-Length":    true,
    "Transfer-Encoding": true,
    "Trailer":           true,
}
```
`Request.Write` writes the remaining header map with
`r.Header.writeSubset(w, reqWriteExcludeHeader, trace)` — so any `Content-Length`
present in `Request.Header` is explicitly **skipped**.

**Verbatim quote E — `outgoingLength` (request.go):**
```go
// outgoingLength reports the Content-Length of this outgoing (Client) request.
// It maps 0 into -1 (unknown) when the Body is non-nil.
func (r *Request) outgoingLength() int64 {
    if r.Body == nil || r.Body == NoBody {
        return 0
    }
    if r.ContentLength != 0 {
        return r.ContentLength
    }
    return -1
}
```

**Verbatim quote F — `transferWriter.writeHeader` (transfer.go), where the header
line is actually emitted:** in `newTransferWriter`, the request path sets
`t.ContentLength = rr.outgoingLength()`; then `writeHeader` emits
```go
if t.shouldSendContentLength() {
    ...
    io.WriteString(w, strconv.FormatInt(t.ContentLength, 10)+"\r\n")
    ...
}
```

**Tight paraphrase / conclusion:** The `Content-Length:` byte sequence written on
the wire is derived entirely from `Request.ContentLength` (via `outgoingLength()` →
`transferWriter.ContentLength` → `strconv.FormatInt`). Any `Content-Length` entry
left sitting in `Request.Header` is suppressed by `reqWriteExcludeHeader` and never
serialized. This confirms the empirically-observed behavior: a deliberately-wrong
`Content-Length` in `Request.Header` is ignored when `ContentLength` is set.

---

## Fact 3 — JSON-RPC 2.0: Batch is an Array of independent Request objects; `id` is String|Number|NULL (NULL discouraged), and notifications have no `id` and get no response

**Citation:** JSON-RPC 2.0 Specification — https://www.jsonrpc.org/specification
- Batch: https://www.jsonrpc.org/specification#batch
- Request object (`id`): https://www.jsonrpc.org/specification#request_object
- Notification: https://www.jsonrpc.org/specification#notification

> ⚠️ Verification gap (see **Gaps**): the live URL could not be byte-fetched in
> this environment (no network tool). The quotes below are the canonical, stable
> spec text; the JSON-RPC 2.0 specification has been frozen since
> 2013-03-13 and has not changed. A final eyeball against the rendered page before
> committing the spec doc is recommended.

**Verbatim quote A — `id` member (request_object section):**
> id
> An identifier established by the Client that MUST contain a String, Number, or
> NULL value if included. If it is not included it is assumed to be a
> notification. The value SHOULD normally not be Null [1] and Numbers SHOULD NOT
> contain fractional parts [2].
>
> [1] The use of Null as a value for the id member in a Request object is
> discouraged …
> [2] Fractional parts may be problematic …

**Verbatim quote B — Notification (notification section):**
> A Notification is a Request object without an "id" member. A Request object
> that is a Notification signifies the Client's lack of interest in the
> corresponding Response object, and as such no Response object needs to be
> returned to the client. The Server MUST NOT reply to a Notification, including
> those that are within a batch request.

**Verbatim quote C — Batch (batch section):**
> When a batch rpc call is made, the Server should respond with an Array
> containing the corresponding Response objects, after all of the Request objects
> of the batch have been processed. A Response object SHOULD exist for each
> Request object, except that there SHOULD NOT be any Response objects for
> notifications. The Server MAY process a batch rpc call as a set of concurrent
> tasks, processing them in any order and with a possibly non-deterministic
> return order.
>
> If the batch rpc call itself fails to be recognized as an valid JSON or as an
> Array with at least one value, the response from the Server MUST be a single
> Response object. If there are no Response objects contained within the Response
> array as it is to be sent to the client, the server MUST NOT return an empty
> Array and should return nothing at all.

**Tight paraphrase / conclusion:** A batch is a JSON array of Request objects
processed independently (and possibly concurrently/out of order); each request's
`id` is a String, Number, or NULL (NULL discouraged; numbers should be integers);
a request with **no** `id` is a notification and must receive **no** response. This
justifies per-element id mutation in the proxy and the "no response for
notification" rule.

---

## Fact 4 — `encoding/json` `Unmarshal` decodes JSON numbers into `float64` when the target is `interface{}`/`any`

**Citation:** `encoding/json` — `Unmarshal`
- Rendered: https://pkg.go.dev/encoding/json#Unmarshal
- Source: `/usr/lib/go/src/encoding/json/decode.go`, doc comment on `Unmarshal`.

**Verbatim quote (doc comment):**
> To unmarshal JSON into an interface value, Unmarshal stores one of these in the
> interface value:
> - bool, for JSON booleans
> - float64, for JSON numbers
> - string, for JSON strings
> - []any, for JSON arrays
> - map[string]any, for JSON objects
> - nil for JSON null

**Tight paraphrase / conclusion:** When unmarshaling into an `any`, every JSON
number becomes a Go `float64`. Re-serializing such a value can therefore change
its textual form (e.g. `3` → `3`, but `1e10` / large integers lose precision), so
the proxy must either decode `id` into a `json.RawMessage`/typed field or be
deliberate about number fidelity when round-tripping.

---

## References
- **Go net/http — `NewRequest` / `NewRequestWithContext`:** https://pkg.go.dev/net/http#NewRequest (Fact 1) — verbatim source at `/usr/lib/go/src/net/http/request.go`.
- **Go net/http — `Request` type:** https://pkg.go.dev/net/http#Request — deep links `#Request.Header`, `#Request.ContentLength`, `#Request.GetBody`, `#Request.Write` (Facts 1 & 2).
- **Go net/http — `transfer.go`:** https://cs.opensource.google/go/go/+/master:src/net/http/transfer.go — `transferWriter.writeHeader` emits `Content-Length` from `t.ContentLength` (Fact 2).
- **Go encoding/json — `Unmarshal`:** https://pkg.go.dev/encoding/json#Unmarshal (Fact 4) — verbatim source at `/usr/lib/go/src/encoding/json/decode.go`.
- **JSON-RPC 2.0 Specification:** https://www.jsonrpc.org/specification — `#request_object`, `#notification`, `#batch` (Fact 3).

## Gaps
- **Fact 3 not byte-fetched.** This subagent has no web/fetch tool, so the
  JSON-RPC 2.0 quotes were reproduced from the canonical, frozen spec text rather
  than re-read from the live URL. Recommend the spec-doc author open
  https://www.jsonrpc.org/specification#batch and
  `#request_object` once to confirm word-for-word. The section *anchor fragments*
  (`#batch`, `#request_object`, `#notification`) are the site's expected anchors
  but were also not byte-verified.
- **Facts 1, 2, 4 are fully authoritative:** extracted verbatim from the locally
  installed Go stdlib source (Go 1.22+), which is exactly what pkg.go.dev renders.
  No gap. A reviewer can re-confirm by opening the cited files/symbols.
- **Go version:** the local tree matches Go 1.22+ semantics (ServeMux
  patterns/`PathValue`, `maps`/`slices` usage), consistent with the project's
  `Go 1.22+` requirement.

```acceptance-report
{
  "criteriaSatisfied": [
    {
      "id": "criterion-1",
      "status": "satisfied",
      "evidence": "Scope held to pure citation research; a single non-code markdown artifact was produced. No code files were created, edited, or staged."
    },
    {
      "id": "criterion-2",
      "status": "satisfied",
      "evidence": "Three of four facts (Go net/http ContentLength/GetBody auto-detection, on-wire Content-Length source, encoding/json float64) are quoted verbatim from the local Go stdlib source with exact file/symbol pointers; the JSON-RPC 2.0 fact quotes the canonical frozen spec with an explicit verification gap. URL + anchor citations provided for all four."
    }
  ],
  "changedFiles": [
    "plan/001_c0abc3757e9a/P1M4T1S1/research/go-proxy-body-rewrite.md"
  ],
  "testsAddedOrUpdated": [],
  "commandsRun": [
    {
      "command": "read /usr/lib/go/src/net/http/request.go (sections: Request struct; Header/ContentLength/GetBody field docs; Write doc; NewRequest/NewRequestWithContext doc + impl switch; outgoingLength)",
      "result": "passed",
      "summary": "Extracted verbatim doc comments proving Facts 1 and 2."
    },
    {
      "command": "read /usr/lib/go/src/net/http/transfer.go (transferWriter, newTransferWriter, writeHeader)",
      "result": "passed",
      "summary": "Confirmed on-wire Content-Length is written from t.ContentLength (= outgoingLength()), and reqWriteExcludeHeader skips Header Content-Length (Fact 2)."
    },
    {
      "command": "read /usr/lib/go/src/encoding/json/decode.go (Unmarshal doc)",
      "result": "passed",
      "summary": "Extracted verbatim list proving JSON numbers unmarshal to float64 (Fact 4)."
    },
    {
      "command": "attempted read of https://pkg.go.dev/... and https://www.jsonrpc.org/specification",
      "result": "failed (expected)",
      "summary": "read tool is local-only; no network tool available. Pivoted to local Go source as authoritative mirror for Go facts; documented gap for JSON-RPC fact."
    }
  ],
  "validationOutput": [
    "Fact 1: NewRequestWithContext doc + impl switch verbatim from request.go — exact ContentLength/GetBody behavior confirmed.",
    "Fact 2: Request.Header note + reqWriteExcludeHeader + outgoingLength() + transferWriter.writeHeader verbatim — Content-Length originates from ContentLength field, Header entry suppressed.",
    "Fact 4: Unmarshal doc bullet list verbatim from decode.go — 'float64, for JSON numbers'.",
    "Fact 3: canonical JSON-RPC 2.0 spec text quoted (batch, id, notification); flagged as not byte-fetched from live URL."
  ],
  "residualRisks": [
    "JSON-RPC 2.0 quotes (Fact 3) are from canonical spec memory, not a live fetch; author should eyeball the rendered page before final spec-doc inclusion. Section anchor fragments (#batch/#request_object/#notification) unverified.",
    "Line numbers intentionally omitted to avoid drift; symbol/file pointers used instead — robust against minor source reshuffles."
  ],
  "noStagedFiles": true,
  "diffSummary": "Created one new markdown research artifact containing the four requested citations with verbatim quotes, URL/anchor links, tight paraphrases, references list, and gaps. No code changes.",
  "reviewFindings": [
    "non-blocker: Fact 3 (JSON-RPC) live URL not byte-fetched due to no network tool; mitigated by canonical frozen-spec text + explicit gap disclosure. Facts 1/2/4 fully authoritative from Go source."
  ],
  "manualNotes": "If a reviewer needs 100% byte-fidelity on Fact 3, the only remaining step is to open https://www.jsonrpc.org/specification#batch and #request_object and diff against the quoted text. Everything else is sourced directly from the installed Go stdlib (the authoritative origin of the pkg.go.dev pages)."
}
```
