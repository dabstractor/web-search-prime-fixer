name: "P1.M4.T2.S2 — Conditional response: passthrough io.Copy vs SSE injection + flush"
description: |

  OWN and VERIFY the conditional RESPONSE-BODY step in `forward` (proxy.go): after
  the headers are written (P1.M4.T2.S1 — parallel, treat its PRP as a contract),
  choose between `io.Copy(rw, resp.Body)` verbatim when `dec.streamThrough` is true
  (the common, zero-buffer case — PRD §6/§11.3) and `Inject(rw, resp.Body,
  dec.rewrittenIDs, dec.warning)` (sse.go, P1.M3.T3.S2 — DONE) when a tools/call
  argument was rewritten — and `http.Flusher.Flush()` after each write when the
  response Content-Type is `text/event-stream` so streamed events are not buffered
  (PRD §11.3). The forward mechanics themselves (client.Do, non-2xx log, response
  header copy, WriteHeader) are ALREADY on disk and owned by T2.S1 — this item
  replaces ONLY the body step + flush. Concretely: add an unexported `flushWriter`
  wrapper (http.Flusher-coupled, lives in proxy.go — sse.go's Inject deliberately
  stays Flusher-free so it remains unit-testable with a `*strings.Builder`), gate it
  on `strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")`, and
  branch the writer on `dec.streamThrough`. Then VERIFY the two load-bearing e2e
  flows with httptest + the `testdata/tools_call.sse` fixture (PRD §19.3 cases 1 & 2):
  (1) a canonical `{"search_query":...}` request streams the upstream SSE body back
  BYTE-FOR-BYTE (no injected block); (2) an aliased `{"query":...}` request makes the
  upstream receive `search_query` and the client receive the warning block FIRST then
  the original content (`content[1].text` byte-identical, `isError==false`). Add a
  focused `TestFlushWriter` unit test pinning "one Write → one Flush". `go.mod` gains
  ZERO requires; proxy.go adds ONE import (`strings`). **Mode A docs**: update
  `forward`'s doc comment to name the io.Copy-vs-Inject branch + the SSE flush rule
  (replacing T2.S1's "Until then the body is streamed through unchanged" placeholder).

---

## Goal

**Feature Goal**: The proxy's response path is conditionally correct: when no
argument was rewritten, the upstream body streams to the client byte-for-byte with
no parsing/buffering and no warning (PRD §11.3/§6); when a tools/call argument WAS
rewritten, the upstream SSE body is fed through `Inject` keyed on
`dec.rewrittenIDs` so exactly one `text` warning block is prepended into the
matching result and the original content is preserved; and in either case, when the
response is SSE, each write is flushed so events reach the client immediately.

**Deliverable**: NO new files. Two edits (both `package main`):
- **MODIFY `forward` body in `proxy.go`** — replace the single
  `io.Copy(rw, resp.Body)` + warn-on-copy-error + final-flush block with: (a) an
  `isSSE` detection; (b) a `flushWriter` wrap of `w` when SSE; (c) a
  `dec.streamThrough` branch choosing `io.Copy(w, resp.Body)` (passthrough) vs
  `Inject(w, resp.Body, dec.rewrittenIDs, dec.warning)` (rewrite); (d) the existing
  single final `http.Flusher.Flush()`. Plus an unexported `flushWriter` type
  (http.Flusher-coupled, proxy.go-local). Add `strings` to the import block. Update
  `forward`'s doc comment (Mode A).
- **APPEND to `proxy_test.go`** — `TestForward_PassthroughByteEqual` (canonical
  param → byte-equal upstream SSE, no block), `TestForward_RewriteWarningFirst`
  (alias param → upstream got `search_query`; client SSE has warning block first,
  original content second, `isError==false`), and `TestFlushWriter_FlushesPerWrite`
  (one `Write` → one `Flush`).

**Success Definition**: `go test -run 'TestForward|TestFlushWriter' -v` passes; in
particular a canonical-param tools/call returns the upstream SSE body byte-for-byte
(no extra block), and an aliased tools/call returns the warning block first with the
original `content[1].text` byte-identical and `isError==false`. The existing
`TestPassthrough_*` + `TestDecideRewrite` suites stay GREEN. `go vet ./...` and
`go test ./...` stay clean. `go.mod` unchanged. `go doc . forward` names the
io.Copy-vs-Inject branch + the SSE flush rule (Mode A).

## Hard Prerequisites

1. **`forward` exists** with the post-T2.S1 body: `client.Do` → (transport-error →
   502) → `defer resp.Body.Close()` → non-2xx `upstream_error {status, req_id}` log
   (copy-through, no synthesis) → non-hop-by-hop response-header copy →
   `rw.WriteHeader(resp.StatusCode)` → `io.Copy(rw, resp.Body)` + warn-on-error →
   final `http.Flusher.Flush()`. This item replaces ONLY the `io.Copy`+warn+
   final-flush block (everything ABOVE `rw.WriteHeader(resp.StatusCode)` is T2.S1's
   and MUST stay untouched). `forward`'s signature
   `forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, dec
   rewriteDecision, log *logger)` is UNCHANGED by this item.
2. **`Inject` exists** in `sse.go` (P1.M3.T3.S2 — DONE): `func Inject(w io.Writer,
   body io.Reader, rewrittenIDs map[any]bool, warning string) error`. Its doc states
   "It does not flush w (the caller owns http.Flusher — see P1.M4.T2.S2)." So THIS
   item owns flushing by WRAPPING `w` — do NOT add a Flush parameter to Inject.
3. **`dec` carries** `streamThrough bool`, `rewrittenIDs map[any]bool`,
   `warning string` (P1.M4.T1.S1 — DONE). `streamThrough` is `true` iff NO id was
   rewritten (equivalent to `len(dec.rewrittenIDs) == 0`); use the `streamThrough`
   field directly. `rewrittenIDs` keys are `float64(N)` (numeric) / `string`; `warning`
   is the formatted line from `warningText` (non-empty when streamThrough is false).
4. **`testdata/tools_call.sse` exists** (P1.M3.T2.S1 — DONE): a `tools/call` SSE
   result, JSON-RPC `id:2`, `result.content[0].text` = the stringified array
   `[{"title":"Example Search Result",...}]`, `isError:false`.
5. **`isHopByHop`, `newUpstreamClient`, `copyForwardHeaders`** are DONE (T4.S2 —
   stable); do not touch. `dcCfg` (decideRewrite test Config with alias order
   `["query","q","search","searchQuery","search_term"]` and target `search_query`)
   is defined in `proxy_test.go`; the e2e tests can reuse `DefaultConfig()` (same
   aliases) or `dcCfg`.

## User Persona

**Target User**: (1) the **MCP client**: receives verbatim SSE when nothing changed,
   and a clean warning-first result when it sent an alias — never a broken stream,
   never buffered until EOF. (2) **P1.M5.T1.S1/S2** (e2e harness): formalizes the
   fake-upstream + the rewrite/passthrough assertions this item ships as focused
   `forward` tests. (3) **P1.M4.T3.S1** (logging): the `debug forward` event it adds
   will observe the io.Copy-vs-Inject choice; this item's branch is where it logs.

**Use Case**: client POSTs a `tools/call` with `{"query":"x"}` (a configured alias);
the proxy rewrites the request to `{"search_query":"x"}` (T1.S1), forwards it, and
when z.ai streams back the `id:2` result, the client sees the terse warning block
first, then the real result content, intact and still valid JSON. When the client
already sent `{"search_query":"x"}`, the response streams through byte-for-byte.

**Pain Points Addressed**: (1) Without the conditional, the proxy either always
parses (breaking the zero-buffer PRD §6 requirement and altering unchanged bytes)
or never injects (the warning never appears). (2) Without per-event flushing, an SSE
stream is buffered by `net/http`'s writer and the client sees nothing until EOF —
defeating streaming. (3) Without a test, a future edit could swap the branch
predicate or drop the flush and silently break both FR-3 and the streaming contract.

## Why

- **PRD §11.3 (Write the response)** is the exact contract: "If `streamThrough`:
  `io.Copy` the upstream body to the client unchanged. Else: feed the upstream body
  through the SSE injector (section 12) keyed on `reqID` before writing to the
  client. Flush after writing when the content type is SSE." This item implements
  that branch + flush.
- **PRD §6 (Non-functional)**: "The common case (no alias present) must not buffer or
  parse the response body; it streams straight through." The `dec.streamThrough`
  branch guarantees the no-rewrite path is a raw `io.Copy` (no SSE parse).
- **PRD §3 / FR-3**: when a rewrite happens, prepend ONE warning block; the original
  result content is unchanged and remains valid JSON; the proxy never sets
  `isError:true`. `Inject` (DONE) enforces this; this item calls it.
- **PRD §19.3 cases 1 & 2** name the two assertions this item's tests encode.
- **Coherence across the chain**: T1.S1 decides + shapes the REQUEST; T2.S1 owns
  forward mechanics + the non-2xx log + reqID; THIS item owns the RESPONSE body
  transform + SSE flush; T3.S1 owns the `rewrite`/`debug forward` log events; M5
  owns the consolidated e2e suite. Clean, non-overlapping ownership.

## What

One structural addition (the `flushWriter` wrapper) and a conditional replacement of
the `forward` body step. Visible behavior: unchanged requests stream verbatim;
rewritten requests get the warning injected; SSE responses flush per write. No new
files; the only new import is `strings` (for `strings.Contains` on Content-Type).

### Success Criteria

- [ ] `flushWriter{w io.Writer; f http.Flusher}` exists (unexported) in `proxy.go`
      with a `Write` that writes then calls `f.Flush()` (only when `f != nil` and the
      write did not error).
- [ ] `forward`, after `rw.WriteHeader(resp.StatusCode)`, branches on
      `dec.streamThrough`: `io.Copy(w, resp.Body)` (passthrough) vs
      `Inject(w, resp.Body, dec.rewrittenIDs, dec.warning)` (rewrite). The non-2xx
      log, response-header copy, and `WriteHeader` ABOVE the branch are byte-for-byte
      T2.S1's (untouched).
- [ ] `w` is `rw` UNLESS `strings.Contains(resp.Header.Get("Content-Type"),
      "text/event-stream")`, in which case `w = flushWriter{w: rw, f: flusher}` (when
      `rw` implements `http.Flusher`).
- [ ] The single final `http.Flusher.Flush()` (T4.S2) is PRESERVED after the branch
      (covers non-SSE / non-Flusher writers).
- [ ] A canonical-param tools/call (`{"search_query":...}`) → client body is
      BYTE-IDENTICAL to the upstream SSE payload (no injected block).
- [ ] An aliased tools/call (`{"query":...}`, JSON-RPC `id:2`) → the fake upstream
      RECEIVED `search_query` (request side, T1.S1) AND the client's decoded SSE event
      has `result.content[0]` = the warning block (`type:text`, text starts with
      `[web-search-prime-fixer]`), `content[1].text` = the original stringified array,
      `isError == false`.
- [ ] `flushWriter.Write` calls `Flush()` exactly once per `Write` (pinned by
      `TestFlushWriter_FlushesPerWrite` using a recording Flusher).
- [ ] `forward`'s signature is UNCHANGED; only its BODY (the io.Copy→conditional
      replacement) + the doc comment change. `go vet ./...` clean; `go test ./...`
      green; `go.mod` unchanged; no `.go` file other than `proxy.go`/`proxy_test.go`
      is edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact post-T2.S1 `forward` body is quoted and the precise block to replace
is delimited; (b) the `flushWriter` type is given verbatim with the "one Write → one
Flush" semantics justified by `emitEvent` doing one `io.WriteString` per event; (c)
the `Inject` signature + its "does not flush w" contract are quoted so the implementer
wraps `w` rather than mutating Inject; (d) the isSSE detection + the symmetric
gating decision is stated with the byte-equality safety argument (Flush adds no
bytes); (e) the two e2e tests are enumerated with literal request bodies, the
`testdata/tools_call.sse` fixture, and decoded-field assertions; (f) the one new
import (`strings`) and the import-block ordering are stated.

### Documentation & References

```yaml
# MUST READ — the response contract this item implements.
- file: PRD.md
  section: "§11.3 Write the response" + "§6 (minimal overhead)" + "§3/FR-3" + "§19.3 cases 1&2"
  why: §11.3 is the exact branch ("If streamThrough: io.Copy ... Else: feed ... through the
        SSE injector ... Flush after writing when the content type is SSE"). §6 mandates the
        no-rewrite path must not buffer/parse. FR-3 = prepend one block, never isError. §19.3
        cases 1&2 are the two assertions the e2e tests encode.
  critical: §11.3 "Flush after writing when the content type is SSE (use an http.Flusher if
        the client supports it) so streamed events are not buffered." Gate the flushWriter on
        the SSE Content-Type, NOT on the Inject-vs-io.Copy choice.

# MUST READ — the previous PRP (contract for the parallel T2.S1 work).
- docfile: plan/001_c0abc3757e9a/P1M4T2S1/PRP.md
  why: defines the post-T2.S1 forward this item builds on: the non-2xx upstream_error log
        (copy-through, no synthesis) sits ABOVE the body step; reqID is threaded; the body is
        STILL io.Copy. Its "CONSUMER SEAM" section says T2.S2 "replaces the body step with
        `if len(dec.rewrittenIDs) > 0 { Inject(...) } else { io.Copy(...) }` then Flush."
  critical: T2.S1 OWNS everything above rw.WriteHeader (Do error, non-2xx log, response-header
        copy, WriteHeader). This item touches ONLY the io.Copy+warn+final-flush block. Do NOT
        alter the non-2xx log or the header copy.

# MUST READ — the Inject pipeline (DONE on disk).
- file: sse.go
  why: func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string)
        error. Loops NewSSEReader(body).Next(); per event, injectData prepends the warning into
        result.content iff obj["id"] in rewrittenIDs and content is []any; emitEvent writes ONE
        io.WriteString per whole event. Doc: "It does not flush w (the caller owns http.Flusher
        — see P1.M4.T2.S2)."
  pattern: emitEvent builds the whole event in a strings.Builder and does a single
        io.WriteString(w, b.String()) -> therefore one Write on w == one emitted event. That is
        why wrapping w with a flushWriter yields "Flush after each emitted event".
  gotcha: do NOT add a Flush parameter to Inject (it would break its strings.Builder
        unit tests). Wrap w in the caller (forward).

# MUST READ — the current proxy.go (the forward core this item extends).
- file: proxy.go
  why: contains forward (T4.S2 core + T2.S1 non-2xx log) + rewriteDecision/decideRewrite
        (T1.S1). rewriteDecision.streamThrough/rewrittenIDs/warning are the fields this item
        consumes. newProxyHandler already wires bytes.NewReader(dec.body) + forward(client, rw,
        outReq, dec, log).
  pattern: the block to REPLACE is exactly:
        `if _, err := io.Copy(rw, resp.Body); err != nil { log.log(...) }`
        `if f, ok := rw.(http.Flusher); ok { f.Flush() }`
        Everything above rw.WriteHeader(resp.StatusCode) is T2.S1's — leave it.
  gotcha: proxy.go imports bytes/encoding/json/io/net/http/time — `strings` is NOT yet
        imported. ADD `strings` for strings.Contains (gofmt sorts it: bytes, encoding/json,
        io, net/http, strings, time).

# MUST READ — verified Go facts (http.Flusher, io.Copy streaming, Content-Type).
- docfile: plan/001_c0abc3757e9a/architecture/external_deps.md
  section: "§2 Go stdlib SSE proxying" + "§4 Reverse-proxy header handling"
  why: §2 verifies http.Flusher exists (`type Flusher interface { Flush() }`) and that
        handlers MUST test for it at runtime; io.Copy streams without whole-body buffering.
        §4 confirms a manual http.Handler is correct because the proxy needs the conditional
        response path.
  critical: httptest.ResponseRecorder does NOT implement Flusher (buffers); httptest.NewServer's
        real handler DOES. The e2e tests use httptest.NewServer (real Flusher). The flushWriter
        unit test uses a hand-rolled recording Flusher.

# MUST READ — the e2e harness + decideRewrite test Config (reuse, do not redeclare).
- file: proxy_test.go
  why: defines fakeUpstream/newTestProxy/initSSE/testSID/dcCfg/firstArgValue. TestPassthrough_*
        already assert initialize SSE byte-equality + header copy. This item APPENDS the
        conditional-response e2e tests + the flushWriter unit test.
  pattern: table-driven + httptest.NewServer recording *got http.Request; PRD-§ comments. The
        canonical-param case can reuse newTestProxy (discard logger is fine — these paths do not
        emit upstream_error on success). The rewrite case needs a fake upstream that returns
        testdata/tools_call.sse, so it builds its own httptest.Server inline.

# MUST READ — the testdata fixture the e2e tests stream.
- file: testdata/tools_call.sse
  why: the canned tools/call SSE result (id:2, content[0].text = stringified array). The
        rewrite e2e uses it as the upstream response; the passthrough e2e uses it as the
        byte-equality reference. Reproduce: `data:{"jsonrpc":"2.0","id":2,"result":{"content":
        [{"type":"text","text":"[{\"title\":\"Example Search Result\",\"url\":\"https://
        example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],"isError":false}}\n\n`.

- url: https://pkg.go.dev/net/http#Flusher
  why: Flusher interface + "Handlers should always test for this ability at runtime."
- url: https://pkg.go.dev/io#Copy
  why: io.Copy streams dst<-src without buffering the whole body (PRD §6 passthrough).
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment — UNTOUCHED
  main.go           # bootstrap + logger — UNTOUCHED
  config.go         # Config + DefaultConfig — UNTOUCHED
  proxy.go          # forward + newProxyHandler + hop stuff (T4.S2) + decideRewrite (T1.S1)
                    #   + non-2xx log/reqID (T2.S1) — MODIFY forward BODY + ADD flushWriter
  rewrite.go        # Rewrite — UNTOUCHED
  sse.go            # Event/Reader + warningText + Inject/emitEvent — UNTOUCHED
  proxy_test.go     # TestPassthrough_* + TestDecideRewrite (T4.S2/T1.S1) — APPEND e2e + flushWriter tests
  *_test.go         # other tests — UNTOUCHED
  testdata/*.sse    # SSE fixtures — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy.go        # MODIFY: add unexported flushWriter type; in forward, replace the
                #   io.Copy+warn+final-flush block with isSSE-detect + flushWriter wrap +
                #   dec.streamThrough branch (io.Copy vs Inject) + preserved final Flush.
                #   Add "strings" import. Update forward doc comment (Mode A).
proxy_test.go   # APPEND: TestForward_PassthroughByteEqual + TestForward_RewriteWarningFirst
                #   + TestFlushWriter_FlushesPerWrite. Reuse fakeUpstream/initSSE/testSID/
                #   newTestProxy/dcCfg; do NOT redeclare them. Add "os" import for os.ReadFile
                #   of the fixture (verify not already imported — proxy_test.go currently
                #   imports bytes/encoding/json/io/net/http/httptest/strings/testing; ADD os).
```

No new files. No other file changes. `main.go` calls `newProxyHandler(cfg, log,
client)` — its signature is UNCHANGED, so no edits there.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — replace ONLY the body step. The block to swap is the io.Copy+warn-on-copy-error
  + the final http.Flusher.Flush(). Everything ABOVE rw.WriteHeader(resp.StatusCode) —
  the Do-error 502, the defer resp.Body.Close(), the non-2xx upstream_error log, the
  response-header copy loop — is T2.S1's. Leave it byte-for-byte intact.

CRITICAL — gate the flushWriter on SSE Content-Type, NOT on the Inject branch. PRD §11.3:
  "Flush after writing when the content type is SSE." Both the io.Copy passthrough (a
  streaming initialize / unchanged tools/call) AND the Inject path benefit from flushing
  SSE per write. Non-SSE responses (application/json) write straight to rw (no needless
  Flush). Use strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream").

CRITICAL — Flush adds NO bytes. Wrapping the io.Copy passthrough writer with a
  flushWriter does NOT break the byte-equality MOCKING: Flush() only pushes buffered
  bytes out, it writes nothing new. The passthrough e2e asserts exact byte-equality and
  MUST still pass with the flushWriter active.

CRITICAL — do NOT add a Flush parameter to Inject. sse.go's Inject deliberately does not
  flush w (its doc says so) so it stays unit-testable with a *strings.Builder. THIS item
  owns flushing by WRAPPING w in forward. Mutating Inject's signature would break the
  P1.M3.T3.S2 unit tests (TestSSE_Inject_*).

CRITICAL — one Write == one emitted event. emitEvent builds the whole event in a
  strings.Builder and calls io.WriteString(w, b.String()) ONCE. So wrapping w with a
  flushWriter that Flush()es after every Write yields exactly "Flush after each emitted
  event" on the Inject path (and one Flush per 32 KiB io.Copy chunk on the passthrough
  path). Do NOT flush inside emitEvent or Inject.

GOTCHA — use dec.streamThrough, not len(dec.rewrittenIDs)==0. They are equivalent
  (decideRewrite sets streamThrough=true iff no id was rewritten), but streamThrough is
  the documented field and reads as the PRD §11.3 branch predicate directly.

GOTCHA — Content-Type is non-hop-by-hop, so it is ALREADY copied into rw.Header() by T2.S1
  before WriteHeader. Reading resp.Header.Get("Content-Type") is equivalent and avoids
  touching rw (the header map is locked after WriteHeader). Read from resp.Header.

GOTCHA — http.Flusher must be tested at runtime. `if f, ok := rw.(http.Flusher); ok`.
  httptest.NewServer's handler implements Flusher; httptest.ResponseRecorder does NOT.
  The flushWriter unit test uses a hand-rolled recording Flusher, NOT ResponseRecorder.

GOTCHA — proxy.go does NOT yet import `strings`. ADD it (gofmt sorts: bytes,
  encoding/json, io, net/http, strings, time). proxy_test.go DOES import strings but NOT
  `os`; the rewrite e2e reads testdata/tools_call.sse via os.ReadFile -> ADD `os`.

GOTCHA — keep the warn-on-copy-error log on BOTH paths. The existing
  `if _, err := io.Copy(...); err != nil { log.log("warn","upstream_error",{"err":...}) }`
  becomes the error check for io.Copy (passthrough) AND an analogous one for Inject
  (rewrite): `if err := Inject(...); err != nil { log.log("warn","upstream_error",
  {"err":...}) }`. A client disconnect mid-stream surfaces as a warn on both paths.

GOTCHA — do NOT redeclare fakeUpstream/initSSE/testSID/newTestProxy/dcCfg/firstArgValue.
  They are defined by T4.S2/T1.S1 in proxy_test.go. The rewrite e2e builds its own
  httptest.Server (returns testdata/tools_call.sse) inline; the passthrough e2e reuses
  newTestProxy with a tools/call-returning upstream OR builds its own server.

GOTCHA — the rewrite e2e must use a JSON-RPC id that the fixture carries. testdata/
  tools_call.sse has id:2, so the client request must use "id":2 and an alias (e.g.
  "query") so decideRewrite produces rewrittenIDs={float64(2):true}. A mismatched id (or
  an int key) would silently NOT inject (id-not-in-set passthrough).
```

## Implementation Blueprint

### Data models and structure

One new unexported type (proxy.go-local, http.Flusher-coupled):

```go
// flushWriter writes to w and, when f is non-nil, flushes after each successful
// Write. It is how forward flushes SSE responses per write: because sse.emitEvent
// writes one whole event per io.WriteString, wrapping w with flushWriter yields
// exactly "Flush after each emitted event" (PRD §11.3). For the io.Copy passthrough
// path it flushes each read chunk. f is nil for non-SSE / non-Flusher writers, in
// which case flushWriter is a plain pass-through (Flush adds no bytes, so wrapping
// the passthrough path is byte-safe — see TestForward_PassthroughByteEqual).
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err == nil && fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}
```

No other new types. Imports: add `strings` to proxy.go (for the Content-Type check).

### Reference implementation (EDIT in `proxy.go`)

> Assume the post-T2.S1 `proxy.go` (the `forward` body quoted in Hard Prerequisites).
> Run `gofmt -w proxy.go` after. The two changes below are surgical: replace the
> io.Copy+warn+final-flush block, and add the flushWriter type above forward.

**EDIT 1 — update `forward`'s doc comment + replace the body step** (signature
UNCHANGED; everything from `client.Do` through `rw.WriteHeader(resp.StatusCode)` is
T2.S1's, untouched; only the trailing io.Copy+warn+flush block is replaced):

```go
// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.2/§11.3). It is the REUSABLE FORWARD CORE: copy status + non-hop-by-hop
// response headers, write the body, flush for SSE.
//
// RESPONSE BODY (PRD §11.3, P1.M4.T2.S2): after the status + headers are written,
// the body step branches on the rewrite decision:
//   - dec.streamThrough == true  -> io.Copy(rw, resp.Body) VERBATIM (zero parse, zero
//     buffering — PRD §6; the common case). No warning is injected.
//   - dec.streamThrough == false -> Inject(rw, resp.Body, dec.rewrittenIDs,
//     dec.warning) (sse.go, P1.M3.T3.S2): the upstream SSE body is decoded event by
//     event, and for each event whose JSON-RPC id is in dec.rewrittenIDs one text
//     warning block is prepended into result.content; every other event is re-emitted
//     unchanged. isError is never touched (FR-3).
//
// SSE FLUSH (PRD §11.3 "Flush after writing when the content type is SSE"): when the
// upstream Content-Type contains "text/event-stream", the writer is wrapped in a
// flushWriter so each Write flushes immediately — one Flush per emitted event on the
// Inject path, one per read chunk on the io.Copy path — so streamed events are not
// buffered. A trailing http.Flusher.Flush() covers any residual buffered bytes.
//
// NON-2xx (P1.M4.T2.S1, PRD §15): a non-2xx upstream status is logged at warn as
// upstream_error {status, req_id} but STILL copied through (status + headers + body);
// the proxy never synthesizes a 502 for an HTTP error status. dec.reqID names the
// request in that log. dec.body was set as outReq.Body by the caller (newProxyHandler).
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, dec rewriteDecision, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 { // T2.S1: copy-through, no synthesis.
		log.log("warn", "upstream_error", map[string]any{"status": resp.StatusCode, "req_id": dec.reqID})
	}
	for k, vs := range resp.Header { // copy non-hop-by-hop resp headers BEFORE WriteHeader.
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)

	// P1.M4.T2.S2: choose the writer. Wrap with flushWriter for SSE so each Write
	// flushes (per emitted event on Inject; per read chunk on io.Copy). Flush adds no
	// bytes, so the passthrough path stays byte-for-byte identical (PRD §11.3/§6).
	var w io.Writer = rw
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		if f, ok := rw.(http.Flusher); ok {
			w = flushWriter{w: rw, f: f}
		}
	}
	if dec.streamThrough {
		// Passthrough: stream the upstream body unchanged, no warning (PRD §11.3/§6).
		if _, err := io.Copy(w, resp.Body); err != nil {
			log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
		}
	} else {
		// Rewrite path: feed the upstream body through the SSE injector keyed on the
		// rewritten request ids; matching tools/call results get the warning prepended
		// (PRD §11.3/§12). Non-matching events pass through with Data unchanged.
		if err := Inject(w, resp.Body, dec.rewrittenIDs, dec.warning); err != nil {
			log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
		}
	}
	// Final flush for any residual buffered bytes (non-SSE / non-Flusher writers).
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
}
```

**EDIT 2 — add the `flushWriter` type** (place it ABOVE `forward`, near the other
proxy helpers; it is proxy.go-local because it is http.Flusher-coupled and sse.go's
Inject must stay Flusher-free):

```go
// flushWriter writes to w and, when f is non-nil, flushes after each successful
// Write. forward wraps the response writer with it for SSE responses so streamed
// events are not buffered (PRD §11.3). Because sse.emitEvent writes one whole event
// per io.WriteString, one Write here == one emitted event == one Flush. Flush adds no
// bytes, so wrapping the io.Copy passthrough path is byte-safe (TestForward_
// PassthroughByteEqual). f is nil for non-SSE or non-Flusher writers -> pass-through.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func (fw flushWriter) Write(p []byte) (int, error) {
	n, err := fw.w.Write(p)
	if err == nil && fw.f != nil {
		fw.f.Flush()
	}
	return n, err
}
```

> The ONLY net change to `forward`'s body vs T2.S1 is: the single `io.Copy`+warn
> becomes the `var w io.Writer` setup + the `if dec.streamThrough { io.Copy } else
> { Inject }` branch. The non-2xx log, header copy, WriteHeader, and final Flush are
> byte-for-byte T2.S1's. Add `strings` to the import block.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the post-T2.S1 forward exists
  - RUN: grep -n "func forward\|StatusCode >= 300\|io.Copy(rw, resp.Body)" proxy.go
  - EXPECT: forward signature carries "dec rewriteDecision" (T1.S1/T2.S1 landed) AND
        a non-2xx `if resp.StatusCode >= 300` log block ABOVE the io.Copy (T2.S1).
        IF forward's body still lacks the non-2xx log, note it — T2.S1 is parallel;
        implement against the body quoted in Hard Prerequisites regardless (the
        non-2xx log is additive and above the block you replace).
  - CONFIRM Inject exists: grep -n "func Inject" sse.go.

Task 1: ADD flushWriter to proxy.go
  - ADD the flushWriter struct + Write method (EDIT 2) above forward.
  - ADD "strings" to proxy.go's import block (gofmt sorts it). Verify it is not a
        duplicate (proxy.go currently imports bytes/encoding/json/io/net/http/time).

Task 2: MODIFY forward body in proxy.go (the conditional branch)
  - REPLACE the `io.Copy(rw, resp.Body)` + warn-on-error + final-flush block with
        EDIT 1's `var w io.Writer` setup + `if dec.streamThrough { io.Copy(w,...) }
        else { Inject(w,...) }` + preserved final Flush.
  - UPDATE forward's doc comment to name the io.Copy-vs-Inject branch + the SSE flush
        rule (replace T2.S1's "Until then the body is streamed through unchanged").
  - DO NOT touch the Do-error branch, the non-2xx log, the header copy, or WriteHeader.
  - DO NOT change forward's signature.

Task 3: APPEND the e2e + unit tests to proxy_test.go
  - PACKAGE: main. APPEND at the END. Reuse fakeUpstream/initSSE/testSID/newTestProxy/
        dcCfg; do NOT redeclare. ADD "os" import (for os.ReadFile of the fixture) if
        not present (verify: proxy_test.go imports bytes/encoding/json/io/net/http/
        httptest/strings/testing — ADD os).
  - TestForward_PassthroughByteEqual: canonical-param tools/call (id:2,
        {"search_query":...}) -> upstream returns testdata/tools_call.sse; assert the
        client body is BYTE-IDENTICAL to the fixture bytes (no injected block).
  - TestForward_RewriteWarningFirst: aliased tools/call (id:2, {"query":...}) ->
        assert (a) upstream RECEIVED search_query (request side); (b) client decoded
        SSE: content[0]=warning block (type:text, text has "[web-search-prime-fixer]"),
        content[1].text == original stringified array, isError==false.
  - TestFlushWriter_FlushesPerWrite: a recording Flusher counts Flush() calls == Write
        calls; f==nil -> no Flush (pass-through), error on write -> no Flush.

Task 4: VALIDATE
  - gofmt -w proxy.go proxy_test.go
  - go vet ./...
  - go test -run 'TestForward|TestFlushWriter' -v   # new tests
  - go test -run TestPassthrough -v                  # T4.S2/T2.S1 e2e regression (still green)
  - go test -run TestDecideRewrite -v                # T1.S1 decision regression (still green)
  - go test ./...                                    # full suite green
  - git diff --stat  # expect ONLY proxy.go + proxy_test.go
  - git diff go.mod  # expect EMPTY
  - go doc . forward  # Mode A: prints the io.Copy-vs-Inject branch + SSE flush rule.
```

### Test block (proxy_test.go — APPEND)

```go
// toolsCallSSE loads the canned tools/call SSE result (id:2) the e2e tests stream.
// (P1.M4.T2.S2; mirrors sse_test.go's fixture read, but proxy_test.go owns its own.)
func toolsCallSSE(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("testdata/tools_call.sse")
	if err != nil {
		t.Skipf("testdata/tools_call.sse missing: %v", err)
	}
	return string(b)
}

// (9) PASSTHROUGH byte-equal: a canonical-param tools/call (no alias) streams the
// upstream SSE body back BYTE-FOR-BYTE — no injected warning block (PRD §11.3/§19.3
// case 2; P1.M4.T2.S2 MOCKING "passthrough case asserts client body is BYTE-EQUAL").
func TestForward_PassthroughByteEqual(t *testing.T) {
	want := toolsCallSSE(t) // upstream payload the client must receive verbatim
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, want)
	}))
	defer up.Close()
	proxy := newTestProxy(up.URL) // discard logger is fine — success path logs nothing
	defer proxy.Close()

	// Canonical param -> decideRewrite streamThrough=true -> io.Copy verbatim.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	got, _ := io.ReadAll(resp.Body)
	if string(got) != want {
		t.Errorf("passthrough body not byte-equal to upstream (no block should be added):\n got %q\nwant %q", got, want)
	}
}

// (10) REWRITE + warning first: an aliased tools/call ({"query":...}, id:2) ->
// upstream RECEIVES search_query; client SSE event has the warning block at
// content[0] and the ORIGINAL content at content[1], isError==false (PRD §11.3/§3/
// FR-3/§19.3 case 1; P1.M4.T2.S2 MOCKING "rewrite case asserts warning block first").
func TestForward_RewriteWarningFirst(t *testing.T) {
	upPayload := toolsCallSSE(t) // id:2, content[0].text = stringified array
	var got http.Request
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		// Record the upstream-received request for the search_query assertion.
		got.Method, got.URL = r.Method, r.URL
		got.Header = r.Header.Clone()
		got.Body = io.NopCloser(bytes.NewReader(body))

		var obj map[string]any
		_ = json.Unmarshal(body, &obj)
		args, _ := obj["params"].(map[string]any)["arguments"].(map[string]any)
		if _, ok := args["query"]; ok {
			t.Errorf("upstream received alias 'query' (should be renamed): %#v", args)
		}
		if args["search_query"] == nil {
			t.Errorf("upstream did NOT receive 'search_query': %#v", args)
		}

		w.Header().Set("Content-Type", "text/event-stream;charset=UTF-8")
		w.Header().Set("Mcp-Session-Id", testSID)
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, upPayload)
	}))
	defer up.Close()
	proxy := newTestProxy(up.URL)
	defer proxy.Close()

	// Alias "query" -> decideRewrite renames to search_query, rewrittenIDs={float64(2):true}.
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// Decode the (single) SSE event the client received and assert on the JSON-RPC object.
	ev, err := NewSSEReader(bytes.NewReader(body)).Next()
	if err != nil {
		t.Fatalf("client response is not a decodable SSE event: %v\n%s", err, body)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("client event Data is not valid JSON: %v\n%s", err, ev.Data)
	}
	if obj["id"] != float64(2) {
		t.Errorf("event id = %v, want float64(2)", obj["id"])
	}
	result := obj["result"].(map[string]any)
	if isErr, _ := result["isError"].(bool); isErr {
		t.Errorf("isError = true, want false (FR-3: never set isError)")
	}
	content := result["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("len(content) = %d, want 2 (warning + original)", len(content))
	}
	// content[0] is the injected warning block.
	c0 := content[0].(map[string]any)
	if c0["type"] != "text" {
		t.Errorf("content[0].type = %v, want text", c0["type"])
	}
	warn, _ := c0["text"].(string)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[0].text = %q, want it to start with the warning marker", warn)
	}
	// content[1].text is the ORIGINAL stringified array (the fixture's content[0].text).
	origContent := jsonToContent0Text(t, upPayload)
	c1text := content[1].(map[string]any)["text"].(string)
	if c1text != origContent {
		t.Errorf("content[1].text changed:\n got %q\nwant %q", c1text, origContent)
	}
}

// jsonToContent0Text decodes an SSE payload's first event and returns
// result.content[0].text (the original stringified array) so the rewrite test can
// compare pre- vs post-inject.
func jsonToContent0Text(t *testing.T, sse string) string {
	t.Helper()
	ev, err := NewSSEReader(strings.NewReader(sse)).Next()
	if err != nil {
		t.Fatalf("decode upstream payload: %v", err)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(ev.Data), &obj); err != nil {
		t.Fatalf("upstream payload not JSON: %v", err)
	}
	return obj["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
}

// (11) flushWriter flushes once per Write and not on error; f==nil is a pass-through
// (P1.M4.T2.S2; pins "Flush after each emitted event" since emitEvent does one Write
// per event).
func TestFlushWriter_FlushesPerWrite(t *testing.T) {
	// recordingFlusher counts Flush() calls.
	var flushes int
	rw := struct {
		io.Writer
		*recordingFlusher
	}{bytes.NewBuffer(nil), &recordingFlusher{&flushes}}

	fw := flushWriter{w: rw, f: rw.recordingFlusher}
	if _, err := fw.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if flushes != 2 {
		t.Errorf("flushes = %d, want 2 (one per Write)", flushes)
	}

	// f == nil -> pass-through, no Flush.
	buf := bytes.NewBuffer(nil)
	fwNil := flushWriter{w: buf, f: nil}
	if _, err := fwNil.Write([]byte("c")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "c" {
		t.Errorf("nil-flusher pass-through wrote %q, want c", buf.String())
	}

	// erroringWriter -> Write errors, Flush must NOT be called.
	fwErr := flushWriter{w: erroringWriter{}, f: rw.recordingFlusher}
	before := flushes
	if _, err := fwErr.Write([]byte("x")); err == nil {
		t.Error("erroringWriter Write returned nil error")
	}
	if flushes != before {
		t.Errorf("Flush called after a write error (flushes %d -> %d)", before, flushes)
	}
}

type recordingFlusher struct{ n *int }

func (r *recordingFlusher) Flush() { *r.n++ }

type erroringWriter struct{}

func (erroringWriter) Write(p []byte) (int, error) { return 0, io.ErrShortWrite }
```

> NOTE: `recordingFlusher` is a pointer receiver (`*recordingFlusher`) so the Flush
> counter mutates through the shared `&flushes`. The anonymous struct in the first
> case embeds both `io.Writer` and `*recordingFlusher` so `fw.f = rw.recordingFlusher`
> satisfies `http.Flusher`. If go vet complains about the goroutine/field tag, simplify
> to a named type. Verify `bytes`/`os`/`io`/`strings`/`encoding/json` are imported.

### Implementation Patterns & Key Details

```go
// PATTERN: conditional writer, conditional body. The writer choice (flushWriter vs rw)
// is gated on the SSE Content-Type; the BODY choice (io.Copy vs Inject) is gated on
// dec.streamThrough. Two orthogonal concerns, two independent checks — do not conflate.

// PATTERN: caller-owned flush via a write-through wrapper. sse.Inject takes an io.Writer
// and stays Flusher-free (testable with strings.Builder). forward wraps w with
// flushWriter so "one emitted event -> one Write -> one Flush" without Inject knowing
// about http.Flusher. This is the Go idiom for streaming SSE through a transform.

// PATTERN: byte-safe flushing. Flush() pushes buffered bytes; it writes nothing. So
// wrapping the io.Copy passthrough with flushWriter cannot alter the bytes the client
// receives — the byte-equality MOCKING (case 2) holds with the flushWriter active.

// PATTERN: symmetric gating. The flushWriter wraps BOTH paths when SSE, because PRD §11.3
// says "Flush after writing when the content type is SSE" (not "when injecting"). This
// also streams an unchanged initialize / unchanged tools/call SSE response correctly.

// GOTCHA (restated): everything above rw.WriteHeader(resp.StatusCode) is T2.S1's. The
// non-2xx upstream_error log, the response-header copy loop, and WriteHeader must be
// left untouched. Only the trailing io.Copy+warn+flush block is replaced.

// GOTCHA (restated): dec.streamThrough, not len(dec.rewrittenIDs)==0. Equivalent, but
// streamThrough is the documented field and matches the PRD §11.3 branch predicate.

// GOTCHA (restated): the rewrite e2e MUST use JSON-RPC id:2 to match testdata/
// tools_call.sse; a different id (or an int key in a hand-built rewrittenIDs) would not
// match and Inject would re-emit unchanged (no warning) — a silent false-pass risk.
```

### Integration Points

```yaml
FILES MODIFIED:
  - proxy.go       (MODIFY forward BODY: io.Copy -> conditional io.Copy|Inject + flushWriter
                      wrap on SSE + preserved final Flush; ADD flushWriter type; ADD
                      "strings" import; UPDATE forward doc comment. Signature unchanged.)
  - proxy_test.go  (APPEND: + toolsCallSSE + jsonToContent0Text helpers + recordingFlusher/
                      erroringWriter + TestForward_PassthroughByteEqual +
                      TestForward_RewriteWarningFirst + TestFlushWriter_FlushesPerWrite;
                      ADD "os" import.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only — `strings`/`os` are stdlib).
  - sse.go: Inject/emitEvent/Reader/warningText UNCHANGED (do NOT add a Flush param).
  - main.go / config.go / doc.go / rewrite.go: untouched.
  - other *_test.go / testdata/*.sse: untouched.
CONSUMER SEAM (keep stable):
  - P1.M5.T1.S1/S2 (e2e harness): formalizes the fake-upstream + rewrite/passthrough
        assertions as a full suite. This item's TestForward_* are the seed cases; M5
        may extract a shared fake-upstream-that-returns-tools-call helper.
  - P1.M4.T3.S1 (logging): the `debug forward` event it adds observes the
        io.Copy-vs-Inject choice at this exact branch; dec.streamThrough is the predicate.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w proxy.go proxy_test.go
go vet ./...
git diff --stat           # expect: proxy.go + proxy_test.go ONLY
git diff go.mod           # expect: EMPTY (zero new requires)
grep -n '"strings"' proxy.go          # expect: present (added)
grep -n '"os"' proxy_test.go          # expect: present (added)

# Expected: gofmt clean; vet clean; only proxy.go + proxy_test.go changed; go.mod
# unchanged; strings (proxy.go) and os (proxy_test.go) imports present.
```

### Level 2: Unit Tests (Component Validation)

```bash
# New conditional-response + flushWriter tests.
go test -run 'TestForward|TestFlushWriter' -v

# MUST PASS (the ones that prove the contract):
#   TestForward_PassthroughByteEqual -> client body BYTE-IDENTICAL to upstream SSE
#       (no injected block) despite the flushWriter being active.
#   TestForward_RewriteWarningFirst  -> upstream got search_query; client content[0] is
#       the warning (marker prefix), content[1].text == original, isError==false.
#   TestFlushWriter_FlushesPerWrite  -> one Write -> one Flush; nil f -> pass-through;
#       write error -> no Flush.
# Expected: PASS, exit 0. If PassthroughByteEqual fails, either an extra block was
# injected (streamThrough wrongly false) or bytes drifted (should be impossible with
# io.Copy). If RewriteWarningFirst has no warning, the id mismatched (use id:2) or
# rewrittenIDs was keyed by int (must be float64).

# Regression: T4.S2/T2.S1/T1.S1 MUST still pass.
go test -run TestPassthrough -v      # initialize SSE byte-equality + headers (flushWriter is byte-safe)
go test -run TestDecideRewrite -v    # decision logic untouched
go test -run TestSSE -v              # Inject unit tests untouched (no signature change)
```

### Level 3: Integration Testing (System Validation)

```bash
go build ./...          # compiles with the conditional branch + flushWriter
go test ./...           # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . forward        # prints the io.Copy-vs-Inject branch + SSE flush rule (Mode A)
go doc . flushWriter    # prints the flush-per-write / byte-safe / nil-f semantics

# Expected: build clean; full suite green; go doc . forward names both the branch and
# the flush rule. The end-to-end "rewrite request -> warning in response" flow is NOW
# COMPLETE (this item is the final piece of the core feature per the item OUTPUT).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Pin byte-equality on the passthrough path WITH the flushWriter active (the
#     load-bearing "Flush adds no bytes" guarantee).
go test -run TestForward_PassthroughByteEqual -v

# (b) Pin the warning-first + original-preserved structure on the rewrite path
#     (FR-3: prepend one block, content intact, isError false).
go test -run TestForward_RewriteWarningFirst -v

# (c) Pin "one Write -> one Flush" (the per-event flush mechanism).
go test -run TestFlushWriter_FlushesPerWrite -v

# (d) Confirm Inject is still called with its original 4-arg signature (no Flush param
#     was bolted on — keeps the P1.M3.T3.S2 unit tests valid).
grep -n 'func Inject' sse.go   # expect: func Inject(w io.Writer, body io.Reader, rewrittenIDs map[any]bool, warning string) error

# Expected: all three targeted tests PASS; Inject signature unchanged.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `proxy.go` + `proxy_test.go`; `go.mod` unchanged; `strings`/`os` imports added.
- [ ] Level 2 passes: `go test -run 'TestForward|TestFlushWriter' -v` green.
- [ ] Level 2 regression: `go test -run 'TestPassthrough|TestDecideRewrite|TestSSE' -v`
      green (no collateral damage to Inject / decision / passthrough).
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc . forward`
      names the io.Copy-vs-Inject branch + the SSE flush rule (Mode A).

### Feature Validation

- [ ] `flushWriter{w io.Writer; f http.Flusher}` exists in `proxy.go`; `Write` flushes
      after a successful write when `f != nil` and not on error.
- [ ] `forward` branches on `dec.streamThrough`: `io.Copy(w, resp.Body)` (passthrough)
      vs `Inject(w, resp.Body, dec.rewrittenIDs, dec.warning)` (rewrite).
- [ ] `w` is wrapped in `flushWriter` when `Content-Type` contains
      `text/event-stream` AND `rw` implements `http.Flusher`; otherwise `w == rw`.
- [ ] The non-2xx `upstream_error` log + response-header copy + `WriteHeader` ABOVE the
      branch are byte-for-byte T2.S1's (untouched); the final `http.Flusher.Flush()` is
      preserved after the branch.
- [ ] Canonical-param tools/call → client body BYTE-IDENTICAL to upstream SSE (no block).
- [ ] Aliased tools/call → upstream received `search_query`; client SSE has warning block
      at `content[0]` (marker prefix), original at `content[1]`, `isError == false`.
- [ ] `forward`'s signature UNCHANGED; only its BODY (the conditional) + the doc comment
      changed; `Inject`'s signature UNCHANGED (no Flush param added).

### Code Quality Validation

- [ ] The conditional is the ONLY behavioral change to `forward`'s body since T2.S1; the
      non-2xx log / header copy / WriteHeader / final Flush are byte-for-byte T2.S1's.
- [ ] The flushWriter is http.Flusher-coupled and lives in proxy.go (NOT sse.go); Inject
      remains Flusher-free and strings.Builder-testable.
- [ ] Tests APPENDED to `proxy_test.go`; no redeclared `fakeUpstream`/`initSSE`/`testSID`/
      `newTestProxy`/`dcCfg`/`firstArgValue`.
- [ ] Imports: only `strings` (proxy.go) + `os` (proxy_test.go) added; no duplicates.
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . forward` prints the io.Copy-vs-Inject branch and the
      SSE flush rule (replacing T2.S1's "Until then the body is streamed through unchanged").
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't conflate the writer choice with the body choice. The flushWriter is gated on
  the SSE Content-Type ("Flush after writing when the content type is SSE" — PRD §11.3);
  the io.Copy-vs-Inject choice is gated on `dec.streamThrough`. Two independent checks.
  Gating the flush on "is it the Inject path" would leave an unchanged SSE
  initialize/tools/call response un-flushed (buffered until EOF — defeats streaming).
- ❌ Don't add a Flush parameter to `Inject`. sse.go's Inject deliberately does not flush
  (`"the caller owns http.Flusher — see P1.M4.T2.S2"`). Wrapping `w` in forward is the
  Go idiom; mutating Inject would break the P1.M3.T3.S2 unit tests (`TestSSE_Inject_*`
  pass a `*strings.Builder`).
- ❌ Don't worry that flushing breaks byte-equality. `Flush()` pushes buffered bytes out;
  it writes nothing. The passthrough e2e asserts exact byte-equality and MUST pass with
  the flushWriter active (TestForward_PassthroughByteEqual pins this). If it fails, the
  cause is an injected block (streamThrough wrongly false) or io.Copy drift, NOT flushing.
- ❌ Don't touch anything above `rw.WriteHeader(resp.StatusCode)`. The Do-error 502, the
  non-2xx `upstream_error` log, the response-header copy, and WriteHeader are T2.S1's.
  Replace ONLY the trailing `io.Copy`+warn+final-flush block.
- ❌ Don't use `len(dec.rewrittenIDs) == 0` as the branch predicate. It is equivalent to
  `dec.streamThrough`, but `streamThrough` is the documented field and the exact PRD
  §11.3 wording ("If `streamThrough`"). Use the field.
- ❌ Don't gate the flush on a missing `http.Flusher` by silently skipping it — DO test
  for it at runtime (`if f, ok := rw.(http.Flusher); ok`). The proxy still works without
  flushing (just buffered); the test is for robustness (ResponseRecorder, h2c, etc.).
- ❌ Don't drop the warn-on-copy-error log on either path. The io.Copy error check
  becomes the passthrough error log; add an analogous `if err := Inject(...); err != nil
  { log... }` for the rewrite path. A client disconnect mid-stream must surface on both.
- ❌ Don't OVERWRITE `proxy.go`/`proxy_test.go` or redeclare `fakeUpstream`/`initSSE`/
  `testSID`/`newTestProxy`/`dcCfg`/`firstArgValue`. EDIT `forward` in place + add
  `flushWriter`; APPEND the new tests + helpers.
- ❌ Don't mismatch the JSON-RPC id in the rewrite e2e. `testdata/tools_call.sse` has
  `id:2`; the client request must use `"id":2` so `decideRewrite` produces
  `rewrittenIDs={float64(2):true}` and `Inject` matches. A different id → no injection
  (false pass). Never hand-build `rewrittenIDs` with an `int` key in production paths.
- ❌ Don't modify `sse.go`, `main.go`, `config.go`, `rewrite.go`, `go.mod`, `PRD.md`,
  `testdata/*`, or any file other than `proxy.go` + `proxy_test.go`.
