name: "P1.M4.T1.S1 — Body parse + tools/call detection + Rewrite wiring + reqID tracking"
description: |

  Implement the **request-side rewrite decision** for the proxy: a pure
  `func decideRewrite(body []byte, cfg Config) rewriteDecision` plus the
  `rewriteDecision` struct `{body, streamThrough, rewrittenIDs, warning}`, plus a
  private `func rewriteObject(obj map[string]any, cfg Config) []string` helper.
  It reads the FULL request body, decodes JSON into `any` (object OR array),
  finds every `method=="tools/call"` whose `params.arguments` is an object, and
  calls the existing `Rewrite(args, cfg.Aliases, cfg.TargetParam)` (rewrite.go,
  DONE) on each. It collects the rewritten request `id`s (as decoded values —
  `float64` for numbers, the load-bearing contract with `sse.Inject`) and joins
  the per-object notes into one `warningText` string. When NOTHING changed it
  returns the ORIGINAL bytes with `streamThrough=true` (no re-serialization — zero
  formatting drift, PRD §11.1 point 4); when something changed it re-serializes
  the (possibly array) body and returns `streamThrough=false` with the id set +
  warning. Then WIRE it into `newProxyHandler` (read body → decide → set
  `outReq.Body` to `bytes.NewReader(dec.body)` → forward) and THREAD `dec` into
  `forward()` via a new `dec rewriteDecision` parameter (the response-side
  conditional that USES `dec` is the explicit deliverable of P1.M4.T2.S2 — this
  item only ships the seam). The decision function is PURE (no I/O) and is unit-
  tested in isolation against crafted request bodies with NO live upstream (the
  item's MOCKING requirement). APPEND the decision code to the existing
  `proxy.go`, MODIFY `newProxyHandler`/`forward` in place, and APPEND the decision
  tests to the existing `proxy_test.go`. `go.mod` gains ZERO requires (stdlib
  only — one new import: `bytes`). **Mode A docs**: a doc comment on
  `decideRewrite` documenting original-bytes passthrough, array handling, and the
  float64 id-tracking contract (PRD §11.1).

---

## Goal

**Feature Goal**: A pure, fully-unit-tested request-side decision function that
turns raw request body bytes into `{body, streamThrough, rewrittenIDs, warning}`,
wired into the proxy so an outbound `tools/call` carrying an alias (e.g. `query`)
is renamed to `search_query` before it is POSTed upstream, while every other
request is forwarded byte-for-byte unchanged. The rewritten ids + warning are
threaded to the response path for P1.M4.T2.S2 to consume.

**Deliverable**: NO new files. Three edits, all `package main`:
- **APPEND to `proxy.go`** — `type rewriteDecision`, `func decideRewrite(body
  []byte, cfg Config) rewriteDecision`, and `func rewriteObject(obj
  map[string]any, cfg Config) []string`, plus a Mode-A doc comment on
  `decideRewrite`. Add `"bytes"` to the import block (the only new import;
  `io`/`net/http` already present).
- **MODIFY `newProxyHandler` in `proxy.go`** — replace the streaming
  `http.NewRequestWithContext(ctx, POST, cfg.Upstream, r.Body)` with:
  `io.ReadAll(r.Body)` → `dec := decideRewrite(body, cfg)` →
  `http.NewRequestWithContext(ctx, POST, cfg.Upstream, bytes.NewReader(dec.body))`
  → `copyForwardHeaders` → `outReq.Header.Del("Content-Length")` → Accept
  fallback → `forward(client, rw, outReq, dec, log)`. The handler-factory
  signature is UNCHANGED (`newProxyHandler(cfg, log, client)`), so `main.go`,
  `health_test.go`, and `proxy_test.go` are untouched.
- **MODIFY `forward` signature in `proxy.go`** — `forward(client, rw, outReq, dec
  rewriteDecision, log)`. The body is UNCHANGED (still `io.Copy` passthrough);
  `dec` is threaded through and unused until P1.M4.T2.S2 adds the
  `if len(dec.rewrittenIDs) > 0 { Inject(...) } else { io.Copy }` branch. (Unused
  function parameters compile cleanly in Go; `go vet` does not flag them.)
- **APPEND to `proxy_test.go`** — a table-driven `TestDecideRewrite` (pure, no
  upstream) covering tools/call alias rename, canonical-param passthrough,
  non-alias passthrough, non-tools/call passthrough, JSON ARRAY batch, missing /
  non-object `params.arguments`, string + numeric (float64) ids, invalid JSON,
  valid-JSON scalar, and the **byte-identity of the unchanged body**.

**Success Definition**: `go test -run TestDecideRewrite -v` passes; in particular
a `tools/call` with `{"query":"x"}` yields a re-serialized body containing
`"search_query"` with `rewrittenIDs[float64(2)]==true` and a non-empty `warning`,
and a plain `initialize` yields `streamThrough==true` with `dec.body` byte-for-byte
identical to the input. The existing `TestPassthrough_*` suite (proxy_test.go)
STILL passes (it posts non-tools/call bodies, which now take the unchanged
passthrough path). `go vet ./...` and `go test ./...` stay clean. `go.mod`
unchanged. `go doc . decideRewrite` shows the original-bytes-passthrough + array +
id-tracking rules (Mode A).

## Hard Prerequisites

1. **`proxy.go` exists** with `newProxyHandler(cfg, log, client) http.HandlerFunc`
   and `forward(client, rw, outReq, log)` (P1.M1.T4.S2 — DONE on disk, verified:
   `hopHeaders`, `isHopByHop`, `newUpstreamClient`, `copyForwardHeaders`,
   `newProxyHandler`, `forward`). This item MODIFIES `newProxyHandler` and
   `forward` in place and APPENDS the decision code. If `proxy.go` is absent,
   STOP — the passthrough core has not run.
2. **`rewrite.go` exists** with `func Rewrite(args map[string]any, aliases
   []string, target string) RewriteResult` (P1.M2.T1.S1 — DONE on disk,
   verified). `decideRewrite` CALLS `Rewrite`; it does NOT reimplement the
   algorithm. `Rewrite` mutates `args` IN PLACE and returns `{Changed, Notes}`.
3. **`warningText(notes []string) string` exists** in `sse.go`
   (P1.M3.T3.S1 — DONE on disk, verified at `sse.go:145`). `decideRewrite` CALLS
   `warningText` to render the combined notes into the single warning line; it
   does NOT format the warning itself.
4. **`Inject(w, body, rewrittenIDs map[any]bool, warning string) error` exists** in
   `sse.go` (P1.M3.T3.S2 — DONE on disk, verified at `sse.go:187`). This item
   does NOT call `Inject`; it produces the `rewrittenIDs` + `warning` that
   P1.M4.T2.S2 will pass to `Inject`. The id-set MUST be keyed by the decoded
   JSON value (`float64` for numbers) so `Inject`'s response-side lookup matches
   (see "id contract" below — VERIFIED).

## User Persona

**Target User**: (1) the **next work item** that consumes `dec` — P1.M4.T2.S2
(response path): when `len(dec.rewrittenIDs) > 0` it streams the upstream body
through `Inject(w, resp.Body, dec.rewrittenIDs, dec.warning)`, else it
`io.Copy`s verbatim. (2) the **maintainer**, who gets one audited place where
"read body, detect tools/call, rewrite, remember the id" lives. (3) the **MCP
client**, whose mistaken `{"query":"x"}` call now reaches z.ai as
`{"search_query":"x"}` and gets back real results + a warning.

**Use Case**: `dec := decideRewrite(body, cfg)` → forward `dec.body` upstream;
when the response arrives, the proxy injects `dec.warning` into the results whose
ids are in `dec.rewrittenIDs`.

**User Journey**: implementer reads this PRP → appends `rewriteDecision` +
`decideRewrite` + `rewriteObject` to `proxy.go` → modifies `newProxyHandler` +
`forward` → appends `TestDecideRewrite` to `proxy_test.go` →
`go test -run TestDecideRewrite -v` → green → existing
`TestPassthrough_*` still green → P1.M4.T2.S2 wires the response branch.

**Pain Points Addressed**: (1) The body must be read in FULL to parse JSON, which
ends streaming — but MCP requests are tiny (PRD §11.1 point 1), so this is fine
and is the only way to detect `tools/call`. (2) Re-serializing EVERY request
would drift formatting (key sort, HTML-escape); the original-bytes passthrough on
the unchanged path (PRD §11.1 point 4) makes the common case byte-faithful. (3)
The `float64`-id hazard: keying `rewrittenIDs` by `int` would silently fail to
match `Inject`'s response lookup — this PRP pins the decoded-value contract.
(4) The Content-Length hazard: re-serialization changes the byte count, so the
copied client `Content-Length` header would be stale — this PRP specifies the
verified remedy (`bytes.NewReader` + `Header.Del`).

## Why

- **PRD §11.1 (Decide whether to rewrite)** is the exact algorithm: read full body
  → parse JSON → array ⇒ iterate elements / object ⇒ single → for each
  `tools/call` with object `params.arguments` call `Rewrite` → if nothing changed
  forward ORIGINAL bytes, else re-serialize and remember the rewritten `id`s.
- **PRD §10 (Rewrite)** / **FR-2**: the one rewrite rule this item invokes (it does
  not redefine it — `rewrite.go` owns it).
- **PRD §3 (Non-goals)**: `decideRewrite` only renames configured aliases to
  `search_query`. It never drops/normalizes/warns about other params; a
  non-alias tools/call (`{"foo":"bar"}`) is passed through unchanged with no
  warning.
- **PRD §8.1 research note**: MCP requests are small JSON-RPC objects, safe to
  read fully into memory (unlike the streamed SSE RESPONSE, which stays streamed).
- **Decouples decision from transport.** `decideRewrite` (this) is pure and tested
  with no network; `forward` (T4.S2) does the POST; `Inject` (P1.M3.T3.S2) does
  the response transform; P1.M4.T2.S2 chooses io.Copy vs Inject from `dec`. Each
  stage's contract is fixed so the others don't change.
- **Coherence across the chain.** `rewrite.go` (DONE) decides Changed + notes;
  `warningText` (DONE) renders notes; `decideRewrite` (this) drives request
  rewrite + collects ids; `Inject` (DONE) correlates response ids; P1.M4.T2.S2
  wires `dec` → response branch. This item is the request-side keystone.

## What

`rewriteDecision` + `decideRewrite` + `rewriteObject` appended to `proxy.go`;
`newProxyHandler` rewired to read the body, decide, and forward `dec.body`;
`forward` extended with a `dec` parameter (response branch deferred to
P1.M4.T2.S2). Visible behavior: a `tools/call` with a configured alias is
rewritten before it leaves the proxy; every other request is byte-faithfully
forwarded.

### Success Criteria

- [ ] `type rewriteDecision` with fields `body []byte`, `streamThrough bool`,
      `rewrittenIDs map[any]bool`, `warning string` exists in `proxy.go`.
- [ ] `func decideRewrite(body []byte, cfg Config) rewriteDecision` exists in
      `proxy.go`.
- [ ] `func rewriteObject(obj map[string]any, cfg Config) []string` exists in
      `proxy.go`.
- [ ] `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_search_prime","arguments":{"query":"x"}}}`
      → `streamThrough==false`; re-decoded `dec.body` has
      `params.arguments.search_query=="x"` and NO `query` key;
      `dec.rewrittenIDs[float64(2)]==true` AND `dec.rewrittenIDs[int(2)]==false`
      (proves the float64 contract); `dec.warning!=""` and contains `"renamed"`.
- [ ] A `tools/call` whose `params.arguments` is already `{"search_query":"y"}`
      (canonical only) → `streamThrough==true`; `dec.body` byte-for-byte == input;
      `dec.rewrittenIDs==nil`; `dec.warning==""`.
- [ ] A `tools/call` with `{"foo":"bar"}` (non-alias) → `streamThrough==true`;
      byte-identical body; no ids; no warning (PRD §3).
- [ ] A non-tools/call (`initialize`, `tools/list`) → `streamThrough==true`;
      byte-identical body.
- [ ] JSON ARRAY body `[initialize-obj, tools/call-with-alias(id=5)]` →
      `streamThrough==false`; `dec.body` is a valid JSON ARRAY; re-decoded
      element[1].params.arguments has `search_query`; `dec.rewrittenIDs` has
      exactly one key `float64(5)`; `dec.warning!=""`.
- [ ] `tools/call` with `params` absent, `params.arguments` absent, or
      `params.arguments` a non-object (string/array) → `streamThrough==true`.
- [ ] `tools/call` with a STRING id (`"abc"`) that is rewritten →
      `dec.rewrittenIDs["abc"]==true`.
- [ ] Invalid JSON body (`{not json`) → `streamThrough==true`; `dec.body` == input.
- [ ] Valid-JSON scalar body (`42`, `"x"`, `null`) → `streamThrough==true`.
- [ ] `newProxyHandler` builds `outReq` with `bytes.NewReader(dec.body)`; existing
      `TestPassthrough_*` (proxy_test.go) still PASS (they post non-tools/call
      bodies → unchanged path).
- [ ] `forward` signature is `forward(client, rw, outReq, dec rewriteDecision, log)`;
      `go vet ./...` clean; `go test ./...` green; `go.mod` unchanged; no `.go` file
      other than `proxy.go`/`proxy_test.go` edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact `Rewrite`/`warningText`/`Inject`/`forward`/`newProxyHandler` APIs are
quoted/located from the on-disk source; (b) the decision logic is given as exact
Go with the array-vs-object switch and the float64-id fact; (c) the
Content-Length gotcha is resolved with a VERIFIED remedy (`bytes.NewReader` sets
`ContentLength`; Transport writes the field and ignores a stale copied header —
empirically confirmed, see research); (d) a complete reference implementation is
provided; (e) all test cases are enumerated with literal inputs and exact
assertions; (f) the float64-id contract with `Inject` is stated and asserted
(`float64(2)` matches, `int(2)` does not); (g) the boundary with P1.M4.T2.S1/T2.S2
is explicit (this item threads `dec`; it does NOT add the response conditional).

### Documentation & References

```yaml
# MUST READ — the algorithm this item implements.
- file: PRD.md
  section: "§11.1 Decide whether to rewrite" + "§3 (non-goals)" + "§10 (Rewrite rule it invokes)"
  why: §11.1 is the step-by-step (read full body → parse → array⇒iterate/object⇒single
        → tools/call + object params.arguments → Rewrite → nothing-changed⇒forward
        ORIGINAL bytes, else re-serialize + remember id). §3 forbids touching
        non-alias params. §10 is the rule Rewrite (DONE) encodes.
  critical: §11.1 point 4 "forward the ORIGINAL body bytes (no re-serialization, to
        avoid any formatting drift)" — the unchanged path MUST return the input bytes.

# MUST READ — the existing forward core + handler factory this item MODIFIES.
- file: proxy.go
  why: newProxyHandler builds outReq from r.Body (streaming) and calls forward;
        forward does client.Do + response-header copy + io.Copy + flush. This item
        swaps the streaming body for bytes.NewReader(dec.body) and threads dec.
  pattern: copyForwardHeaders copies ALL non-hop-by-hop headers INCLUDING
        Content-Length (it is not hop-by-hop) — so the rewritten path MUST del it
        (see Known Gotchas). Accept fallback: only default when the client omitted Accept.
  gotcha: do NOT touch hopHeaders/isHopByHop/newUpstreamClient/copyForwardHeaders. MODIFY
        newProxyHandler + forward in place; APPEND the decision code.

# MUST READ — the Rewrite function this item calls (DONE).
- file: rewrite.go
  why: Rewrite(args, aliases, target) mutates args IN PLACE and returns
        {Changed bool, Notes []string}. Changed==false ⇒ args untouched, Notes==nil.
  pattern: caller passes cfg.Aliases (config order matters) + cfg.TargetParam.
  gotcha: Rewrite returns a ZERO-VALUE result when nothing matched; decideRewrite must
        treat Notes==nil as "no change" (not call warningText with it).

# MUST READ — warningText (DONE) which decideRewrite calls to render the warning.
- file: sse.go
  section: "func warningText(notes []string) string" (sse.go:145)
  why: renders the combined notes as the single-line SSE warning. decideRewrite calls
        it with the FLATTENED notes from all rewritten objects in the (batch) body.

# MUST READ — Inject (DONE, parallel P1.M3.T3.S2) which consumes rewrittenIDs/warning.
- file: sse.go
  section: "func Inject(w, body, rewrittenIDs map[any]bool, warning string) error" (sse.go:187)
  why: defines the EXACT type/keys of rewrittenIDs that this item must produce.
        Inject decodes each response event's JSON-RPC id into any (float64 for numbers)
        and looks it up in rewrittenIDs. So this item MUST key rewrittenIDs by the
        decoded request id (float64(N)) — else Inject never matches.

# This item's own research — VERIFIED Go facts (Content-Length, float64 id, re-marshal).
- docfile: plan/001_c0abc3757e9a/P1M4T1S1/research/go-proxy-body-rewrite.md
  why: empirically confirms (1) NewRequest(*bytes.Reader) sets ContentLength + GetBody;
        (2) Transport writes Content-Length from the FIELD, ignoring a stale copied
        header (so Del-ing it is safe and the wire length is correct); (3) numeric id
        decodes to float64 and map[any]bool keyed by float64(N) matches while int(N) does not;
        (4) re-marshaling a decoded tools/call object yields valid JSON z.ai accepts.
  section: "VERIFIED" sections (Content-Length probe + id probe).

# Predecessor context (background only — on-disk source is authoritative).
- docfile: plan/001_c0abc3757e9a/P1M3T3S2/PRP.md
  why: defines Inject + the id contract this item must satisfy. The "id contract"
        section there is the source of truth for float64-keyed rewrittenIDs.

# CODEBASE CONVENTIONS — follow these patterns.
- file: proxy_test.go
  why: httptest e2e harness (fakeUpstream, newTestProxy, initSSE const). This item
        APPENDS the pure decideRewrite tests to the SAME file (one _test.go per
        source file is the repo convention). Do NOT redeclare initSSE/testSID/
        fakeUpstream/newTestProxy (already defined).
  pattern: table-driven []struct{name,...} + t.Run(tc.name, ...); PRD-§ comments per row.
- file: rewrite_test.go
  why: table-driven style for a pure function (cloneMap helper) — mirror it for
        decideRewrite's pure tests.

- url: https://www.jsonrpc.org/specification
  why: JSON-RPC 2.0 batch = a JSON array of request objects processed independently;
        id is String/Number/NULL; a notification (no id) gets no response. Drives the
        array handling + the "track id only if present" detail.
  critical: §11.1 point 2 "MCP does not use batches" — array handling is DEFENSIVE;
        a single object is the normal case.

- url: https://pkg.go.dev/net/http#Request
  why: Request.ContentLength is set by NewRequest when the body is a *bytes.Reader/
        *bytes.Buffer/*strings.Reader; Transport writes the Content-Length header from
        this field. Justifies the Del("Content-Length") cleanup (verified empirically).
- url: https://pkg.go.dev/encoding/json#Unmarshal
  why: decoding JSON into any yields float64 for numbers — the float64-id contract.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment
  main.go           # bootstrap + logger + routes (calls newProxyHandler) — UNTOUCHED
  config.go         # Config + DefaultConfig + ResolveConfig (P1.M1.T2) — UNTOUCHED
  proxy.go          # forward + newProxyHandler + hop stuff (P1.M1.T4.S2) — MODIFY + EXTEND THIS
  rewrite.go        # Rewrite + RewriteResult (P1.M2.T1) — UNTOUCHED (called)
  sse.go            # Event/Reader + warningText (S1) + Inject (P1.M3.T3.S2) — UNTOUCHED (called)
  proxy_test.go     # TestPassthrough_* e2e harness (P1.M1.T4.S2) — EXTEND THIS
  *_test.go         # config/resolve/logger/health/rewrite/sse tests — UNTOUCHED
  testdata/*.sse    # SSE response fixtures (P1.M3.T2.S1) — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy.go        # EXTEND (append): rewriteDecision + decideRewrite + rewriteObject + doc.
                #   MODIFY: newProxyHandler (read body→decide→bytes.NewReader→forward),
                #           forward (+ dec param; body unchanged). Imports: add "bytes" (only new).
proxy_test.go   # EXTEND (append): TestDecideRewrite (pure, table-driven, no upstream).
                #   No new imports ("bytes","encoding/json","strings","testing" present;
                #   reuse existing helpers/constants — do not redeclare).
```

No new files. No other file changes. `main.go`/`health_test.go`/`proxy_test.go`
call `newProxyHandler(cfg, log, client)` — its signature is UNCHANGED, so they
need no edits.

### Verified Go facts (load-bearing — see research/go-proxy-body-rewrite.md)

```
VERIFIED (go1.26.4) — http.NewRequestWithContext(ctx, POST, url, bytes.NewReader(data)):
  outReq.ContentLength == int64(len(data))   // auto-set from the *bytes.Reader
  outReq.GetBody != nil                       // set for safe retries on idempotent methods

VERIFIED — Transport writes Content-Length from the FIELD, ignoring a stale header:
  outReq.Header.Set("Content-Length","999")   // deliberately wrong (simulating a copied
                                              //   pre-rewrite length)
  -> upstream received Content-Length "100" (correct) and the full 100-byte body.
  => Del-ing the copied Content-Length after copyForwardHeaders is SAFE and CLEAN:
     Transport re-writes Content-Length from outReq.ContentLength on the wire.

VERIFIED — json.Unmarshal([]byte(`{"id":2}`), &any) yields float64(2):
  map[any]bool keyed by float64(2) -> lookup[float64(2)]==true, lookup[int(2)]==false.
  => rewrittenIDs MUST be keyed by the decoded obj["id"] value (float64(N)), which is
     exactly what Inject finds when it decodes the response id the same way. Do NOT
     convert ids to int.

VERIFIED — json.Marshal of a decoded tools/call object yields VALID JSON:
  keys are sorted alphabetically and <,>,& are HTML-escaped (\u003c/\u003e/\u0026),
  but the VALUE is semantically identical (z.ai's JSON parser unescapes on read).
  => re-serialization is correct for the CHANGED path; the unchanged path avoids it
     entirely (original-bytes passthrough) so there is ZERO drift for the common case.
```

### id contract (with sse.Inject, the consumer of rewrittenIDs)

`rewrittenIDs` is `map[any]bool`. For a numeric JSON-RPC id `N`, the key MUST be
`float64(N)` — the value `encoding/json` produces when decoding `{"id":N}` into
`any`. This item obtains that key for free by indexing `rewrittenIDs[obj["id"]]`
using the DECODED value (never converting to int). `Inject` (sse.go) decodes each
response event's id the same way, so its `rewrittenIDs[obj["id"]]` lookup matches.
String ids decode to `string` and match identically. **Do not** build
`rewrittenIDs` from `int`/`json.Number` keys or numeric ids will silently fail to
match (the lookup returns `false` → no warning injected). A `tools/call` with no
`id` (a notification) adds `nil` to the map; harmless (no response to match) but
this item tracks it only if present.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — original-bytes passthrough. When NOTHING changed, return the ORIGINAL
  input bytes (PRD §11.1 point 4). Do NOT json.Marshal the decoded object on the
  unchanged path — that would sort keys + HTML-escape and drift the formatting.
  Only the CHANGED path re-serializes. The test asserts dec.body == input BYTES.

CRITICAL — float64 id, not int. json.Unmarshal into any yields float64 for JSON
  numbers. Key rewrittenIDs by obj["id"] (the decoded value). A test asserts
  rewrittenIDs[float64(2)]==true AND rewrittenIDs[int(2)]==false. (Verified.)

CRITICAL — Content-Length after re-serialization. copyForwardHeaders copies the
  client's Content-Length (it is NOT hop-by-hop). After re-serialization the byte
  count changed, so the copied value is STALE. REMEDY (verified): pass
  bytes.NewReader(dec.body) to NewRequest (auto-sets outReq.ContentLength), then
  outReq.Header.Del("Content-Length") after copyForwardHeaders. Transport writes
  the correct length from the FIELD; the stale header never reaches the wire.

CRITICAL — only tools/call + object params.arguments. rewriteObject returns nil
  (no change) unless method=="tools/call" AND params is an object AND
  params.arguments is an object. A tools/call with arguments as a string/array,
  or a missing params, is left untouched. PRD §3: never touch non-alias params.

CRITICAL — do NOT implement the response conditional. forward() receives dec but
  KEEPS doing io.Copy. The `if len(dec.rewrittenIDs) > 0 { Inject(...) } else
  { io.Copy }` branch is P1.M4.T2.S2's explicit deliverable. Threading dec is
  THIS item's seam; using it for the response is the next item. (An unused
  function parameter compiles cleanly; go vet does not flag it.)

CRITICAL — APPEND the decision code; MODIFY the handler/forward in place. Do not
  rewrite proxy.go. hopHeaders/isHopByHop/newUpstreamClient/copyForwardHeaders are
  STABLE and owned by T4.S2. Add "bytes" to imports only if absent (it is).

GOTCHA — never call warningText with nil notes. warningText returns "" for an empty
  slice, but decideRewrite only reaches the warning step when rewrittenIDs is
  non-empty, so notes is guaranteed non-empty there. For the unchanged path, do
  not call warningText at all (return warning:"").

GOTCHA — keep rewriteDecision/decideRewrite/rewriteObject UNEXPORTED. The handler
  (package main) calls decideRewrite directly; nothing outside the package needs it.
  An exported name needlessly widens the API (matches Inject/emitEvent convention).

GOTCHA — invalid JSON / valid-JSON-scalar bodies are passed through UNCHANGED, never
  rejected. The proxy must not block a request on a parse quirk; z.ai can decide.
  json.Unmarshal into any errors → streamThrough=true with original bytes; a decoded
  scalar (number/string/null/bool) hits the default switch arm → same.

GOTCHA — do NOT redeclare initSSE/testSID/fakeUpstream/newTestProxy in proxy_test.go.
  They are defined by the T4.S2 harness. The new TestDecideRewrite is PURE and does
  not need them, but if it references the e2e helpers it must reuse the existing ones.
```

## Implementation Blueprint

### Data models and structure

One new struct (no JSON tags — it is an in-process value, never serialized):

```go
// rewriteDecision is the outcome of inspecting a request body for the alias
// rewrite (PRD §11.1), produced by decideRewrite.
type rewriteDecision struct {
	body          []byte        // bytes to POST upstream (original if streamThrough, else re-serialized)
	streamThrough bool          // true ⇒ forward original bytes unchanged; response io.Copy'd
	rewrittenIDs  map[any]bool  // JSON-RPC ids rewritten (float64 for numbers) — response correlation
	warning       string        // formatted warning (warningText) to inject; "" when streamThrough
}
```

Depends only on stdlib `"bytes"`, `"encoding/json"`, `"io"`, `"net/http"` (all
present except `bytes`, which this item adds). Calls `Rewrite` (rewrite.go),
`warningText` (sse.go).

### Reference implementation (APPEND/MODIFY in `proxy.go`)

> The existing `proxy.go` imports `io`, `net/http`, `time`. After this change the
> import block also contains `bytes` and `encoding/json` (gofmt sorts:
> `bytes`, `encoding/json`, `io`, `net/http`, `time`). Run `gofmt -w proxy.go`.

**APPEND** (after the existing `forward` func):

```go
// rewriteDecision is the outcome of inspecting a request body for the alias
// rewrite (PRD §11.1). It is produced by decideRewrite and consumed by the forward
// path (P1.M4.T2.S1 — the body to POST) and the response path (P1.M4.T2.S2 — the
// rewritten ids + warning that select SSE injection).
type rewriteDecision struct {
	// body is the request body to forward upstream: the ORIGINAL input bytes when
	// streamThrough is true (no rewrite — zero formatting drift, PRD §11.1 point 4),
	// otherwise the re-serialized bytes carrying the renamed args.
	body []byte
	// streamThrough is true when NO tools/call argument was rewritten. When true,
	// body is the original bytes verbatim and the response is io.Copy'd unchanged
	// (P1.M4.T2.S2). When false, the response is run through the SSE injector keyed
	// on rewrittenIDs.
	streamThrough bool
	// rewrittenIDs is the set of JSON-RPC request ids whose tools/call arguments
	// were rewritten. Keys are the values encoding/json produced when decoding the
	// request id into any: float64 for numeric ids, string for string ids. This is
	// exactly what Inject (sse.go) finds when it decodes the RESPONSE id the same
	// way (see "id contract" on decideRewrite). Empty/nil when streamThrough.
	rewrittenIDs map[any]bool
	// warning is the formatted single-line warning (warningText, sse.go) to prepend
	// into each rewritten tools/call RESULT. Empty when streamThrough.
	warning string
}

// decideRewrite inspects a JSON-RPC request body and, for every tools/call whose
// params.arguments contains a configured alias, applies the alias->target rename
// in place via Rewrite (rewrite.go). It returns the bytes to forward upstream and
// the per-request state the response path needs (rewritten ids + warning)
// (PRD §11.1). decideRewrite is PURE (no I/O) and is unit-tested in isolation.
//
// RULES:
//   - ORIGINAL-BYTES PASSTHROUGH (PRD §11.1 point 4): when NO object changed, the
//     returned body is the ORIGINAL input bytes — no re-serialization — so a request
//     that needs no fixup is forwarded byte-for-byte and cannot drift in formatting
//     (key order, whitespace, HTML-escaping). Only a request that was actually
//     rewritten is re-serialized.
//   - ARRAY HANDLING (PRD §11.1 point 2): MCP does not use batches, but if the body
//     decodes to a JSON ARRAY, each element is inspected independently and, if any
//     element changed, the whole body is re-serialized as an array. A non-array,
//     non-object body (or unparseable JSON) is passed through UNCHANGED rather than
//     rejected — the proxy never blocks a request on a parse quirk.
//   - ID TRACKING: a rewritten object's JSON-RPC id is added to rewrittenIDs using
//     the decoded value directly (float64 for numbers — see the id contract below).
//     This is the correlation key the SSE injector matches against in the response
//     (sse.go Inject, P1.M3.T3.S2).
//
// id contract: numeric JSON-RPC ids decode to float64 in any (id:2 -> float64(2));
// rewrittenIDs is therefore keyed by float64(2), which is exactly what Inject finds
// when it decodes the response id the same way. String ids decode to string and
// match identically. Never build rewrittenIDs from an int key —
// map[any]bool{int(2):true} would NOT match float64(2) (verified).
func decideRewrite(body []byte, cfg Config) rewriteDecision {
	var parsed any
	if err := json.Unmarshal(body, &parsed); err != nil {
		// Not valid JSON -> forward unchanged (never reject on a parse quirk).
		return rewriteDecision{body: body, streamThrough: true}
	}

	var rewrittenIDs map[any]bool
	var notes []string
	// addID records a rewritten object's id + notes. The id is the DECODED value
	// (float64 for numbers) — the key Inject matches on the response side.
	addID := func(id any, n []string) {
		if rewrittenIDs == nil {
			rewrittenIDs = make(map[any]bool)
		}
		rewrittenIDs[id] = true
		notes = append(notes, n...)
	}

	switch v := parsed.(type) {
	case map[string]any:
		if n := rewriteObject(v, cfg); n != nil {
			addID(v["id"], n)
		}
	case []any:
		// Defensive JSON-RPC batch (MCP does not use batches): process each element.
		for _, elem := range v {
			if obj, ok := elem.(map[string]any); ok {
				if n := rewriteObject(obj, cfg); n != nil {
					addID(obj["id"], n)
				}
			}
		}
	default:
		// Valid JSON but a scalar (number/string/null/bool) -> pass through.
		return rewriteDecision{body: body, streamThrough: true}
	}

	if len(rewrittenIDs) == 0 {
		// Nothing changed -> forward ORIGINAL bytes (no re-serialization).
		return rewriteDecision{body: body, streamThrough: true}
	}

	// Something changed -> re-serialize the (possibly array) body. json.Marshal
	// sorts keys and HTML-escapes <>&, both semantically harmless for JSON-RPC
	// (z.ai parses the value, not the bytes); the original-bytes path above is
	// what guards formatting fidelity for the common unchanged case.
	out, err := json.Marshal(parsed)
	if err != nil {
		// Cannot happen for a value we just decoded; defensive: pass through.
		return rewriteDecision{body: body, streamThrough: true}
	}
	return rewriteDecision{
		body:          out,
		streamThrough: false,
		rewrittenIDs:  rewrittenIDs,
		warning:       warningText(notes),
	}
}

// rewriteObject applies the alias rewrite to a single JSON-RPC object IN PLACE.
// It returns the RewriteResult.Notes (nil when nothing changed) so the caller can
// collect the object's id and join notes into the warning. A non-tools/call
// method, or a tools/call with absent/non-object params.arguments, is left
// untouched (PRD §11.1 point 3; §3 — only configured aliases are ever moved).
func rewriteObject(obj map[string]any, cfg Config) []string {
	if obj["method"] != "tools/call" {
		return nil
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return nil
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		return nil
	}
	if res := Rewrite(args, cfg.Aliases, cfg.TargetParam); res.Changed {
		return res.Notes
	}
	return nil
}
```

**MODIFY `newProxyHandler`** — replace the body of the returned closure. The
factory signature stays `newProxyHandler(cfg Config, log *logger, client *http.Client) http.HandlerFunc`:

```go
func newProxyHandler(cfg Config, log *logger, client *http.Client) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		// PRD §11.1 point 1: read the FULL request body (MCP requests are small
		// JSON-RPC objects). This ends streaming on the REQUEST side; the RESPONSE
		// side stays streamed (forward io.Copy / Inject).
		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"read body"}`, http.StatusBadRequest)
			return
		}
		// Decide the rewrite (pure). dec.body is the original bytes when nothing
		// changed (zero drift) or the re-serialized bytes when an alias was renamed.
		dec := decideRewrite(body, cfg)

		// bytes.NewReader lets net/http set ContentLength (and GetBody for safe
		// retries) from the actual forwarded length — even after re-serialization
		// changed the byte count. (Verified: Transport writes Content-Length from
		// outReq.ContentLength, ignoring a stale copied header.)
		outReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, cfg.Upstream, bytes.NewReader(dec.body))
		if err != nil {
			log.log("error", "upstream_error", map[string]any{"err": err.Error()})
			http.Error(rw, `{"error":"bad upstream request"}`, http.StatusBadGateway)
			return
		}
		copyForwardHeaders(outReq.Header, r.Header)
		// The copied Content-Length reflects the ORIGINAL (pre-rewrite) length; after
		// re-serialization it may be stale. Transport writes the correct length from
		// outReq.ContentLength (set above via bytes.NewReader), so drop the stale copy.
		outReq.Header.Del("Content-Length")
		// Accept fallback (PRD §8/§11.2): default ONLY when the client omitted Accept.
		if outReq.Header.Get("Accept") == "" {
			outReq.Header.Set("Accept", "application/json, text/event-stream")
		}
		// dec.body is already set as outReq.Body; dec.rewrittenIDs + dec.warning are
		// threaded for P1.M4.T2.S2 (response-side io.Copy-vs-Inject branch).
		forward(client, rw, outReq, dec, log)
	}
}
```

**MODIFY `forward` signature** (body UNCHANGED — `dec` is threaded, unused until
P1.M4.T2.S2; update the doc comment to name `dec`):

```go
// forward sends outReq via client and streams the upstream response to rw
// (PRD §11.3 passthrough path). It is the REUSABLE FORWARD CORE: copy status +
// non-hop-by-hop response headers, copy the body, flush for SSE.
//
// dec carries the rewrite decision for this request: dec.body is already set as
// outReq.Body by the caller (newProxyHandler); dec.streamThrough/rewrittenIDs/
// warning select the RESPONSE path. P1.M4.T2.S2 EXTENDS the body step:
// io.Copy (passthrough) when dec.streamThrough, otherwise feed the body through
// the SSE injector (sse.go Inject) keyed on dec.rewrittenIDs with dec.warning.
// Until then the body is streamed through unchanged (the rewrite already shaped
// the REQUEST); dec is received but not yet used here.
func forward(client *http.Client, rw http.ResponseWriter, outReq *http.Request, dec rewriteDecision, log *logger) {
	resp, err := client.Do(outReq)
	if err != nil {
		log.log("error", "upstream_error", map[string]any{"err": err.Error()})
		http.Error(rw, `{"error":"upstream"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		if isHopByHop(k) {
			continue
		}
		for _, v := range vs {
			rw.Header().Add(k, v)
		}
	}
	rw.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(rw, resp.Body); err != nil {
		log.log("warn", "upstream_error", map[string]any{"err": err.Error()})
	}
	if f, ok := rw.(http.Flusher); ok {
		f.Flush()
	}
}
```

> The ONLY line that changes in `forward`'s body is its signature (add `dec
> rewriteDecision`). Keep the rest byte-for-byte as T4.S2 wrote it. `dec` is an
> unused parameter until P1.M4.T2.S2 — that compiles and vets cleanly.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the inputs exist
  - RUN: grep -n "func Rewrite(" rewrite.go && grep -n "func warningText" sse.go \
         && grep -n "func Inject" sse.go && grep -n "func newProxyHandler\|func forward" proxy.go
  - EXPECT: Rewrite (rewrite.go), warningText + Inject (sse.go), newProxyHandler +
        forward (proxy.go). IF ANY ABSENT: STOP — a prerequisite has not run.
  - CONFIRM "bytes" is NOT yet in proxy.go's import block (it imports io/net/http/time).
        It is added in Task 1.

Task 1: APPEND rewriteDecision + decideRewrite + rewriteObject to proxy.go
  - APPEND the "Reference implementation" block (the three symbols + their docs)
        to the END of proxy.go (after forward).
  - ADD "bytes" AND "encoding/json" to the import block (gofmt sorts: bytes,
        encoding/json, io, net/http, time). Do NOT duplicate io/net/http/time.
  - KEEP the Mode-A doc comment on decideRewrite (original-bytes passthrough +
        array handling + id tracking) — it IS the docs deliverable.
  - DO NOT touch hopHeaders/isHopByHop/newUpstreamClient/copyForwardHeaders.

Task 2: MODIFY newProxyHandler + forward signature in proxy.go
  - REPLACE newProxyHandler's closure body with the "Reference implementation"
        version (io.ReadAll -> decideRewrite -> bytes.NewReader(dec.body) ->
        copyForwardHeaders -> Del("Content-Length") -> Accept fallback ->
        forward(client, rw, outReq, dec, log)). Keep the factory signature.
  - CHANGE forward's signature to add "dec rewriteDecision" (between outReq and
        log). Update forward's doc comment to name dec + cite P1.M4.T2.S2. Leave
        forward's BODY byte-for-byte as-is (still io.Copy; dec unused for now).
  - The single existing forward() call site (proxy.go, in newProxyHandler) is the
        ONLY caller — no other file calls forward() directly.

Task 3: APPEND TestDecideRewrite to proxy_test.go
  - PACKAGE: main (same package; do NOT redeclare initSSE/testSID/fakeUpstream/
        newTestProxy — they are defined by the T4.S2 harness; the pure tests do
        not need them but must not shadow them).
  - APPEND at the END of proxy_test.go. IMPORTS: "bytes","encoding/json","strings",
        "testing" are already imported — add nothing (verify; do not duplicate).
  - TESTS: see the test block below. PURE — no httptest, no upstream. Each row
        feeds []byte to decideRewrite and asserts on the returned fields.
  - COVERAGE: tools/call alias rename (float64 id), canonical passthrough,
        non-alias passthrough, non-tools/call passthrough, JSON ARRAY batch,
        missing/non-object params.arguments, string id, invalid JSON, valid-JSON
        scalar, and BYTE-IDENTITY of the unchanged body.

Task 4: VALIDATE
  - gofmt -w proxy.go proxy_test.go
  - go vet ./...
  - go test -run TestDecideRewrite -v        # the new pure tests
  - go test -run TestPassthrough -v          # the existing e2e tests STILL pass
  - go test ./...                            # full suite green
  - ALL green. git diff --stat must show ONLY proxy.go + proxy_test.go.
  - git diff go.mod must be EMPTY.
  - go doc . decideRewrite                   # Mode A: prints the three rules.
```

### Test block (proxy_test.go — APPEND)

```go
// dcCfg is a minimal Config for decideRewrite tests (decoupled from config.go
// defaults so the unit is stable if defaults change). PRD §10 alias order.
var dcCfg = Config{
	Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"},
	TargetParam: "search_query",
}

// dcArgs is a canonical tools/call arguments skeleton used across rows.
const dcArgsQueryBody = `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
	`"params":{"name":"web_search_prime","arguments":{"query":"lunar rover"}}}`

// firstArgValue re-decodes dec.body and returns params.arguments[key] (or "" if
// absent), so tests assert on the SEMANTIC value, not on marshaled key order.
func firstArgValue(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var obj map[string]any
	if err := json.Unmarshal(b, &obj); err != nil {
		t.Fatalf("dec.body not valid JSON: %v\n%s", err, b)
	}
	params, _ := obj["params"].(map[string]any)
	args, _ := params["arguments"].(map[string]any)
	return args
}

func TestDecideRewrite(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		wantStream     bool   // expect streamThrough
		wantBodyIdent  bool   // expect dec.body == input bytes (unchanged path)
		wantArgKey     string // expected canonical arg key after rewrite ("" => unchanged)
		wantIDKey      any    // expected single rewritten id key (nil => no ids)
		wantWarningHas string // substring the warning must contain ("" => empty)
	}{
		// tools/call with alias "query" -> renamed to search_query (PRD §11.1/§10 row 1).
		{
			"toolscall_query_renamed",
			dcArgsQueryBody,
			false, false, "search_query", float64(2), "renamed",
		},
		// tools/call already canonical -> unchanged (PRD §10 row 6). BYTE-IDENTITY.
		{
			"toolscall_canonical_unchanged",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"search_query":"x"}}}`,
			true, true, "", nil, "",
		},
		// tools/call with a NON-alias param -> unchanged (PRD §3). BYTE-IDENTITY.
		{
			"toolscall_nonalias_unchanged",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":{"foo":"bar"}}}`,
			true, true, "", nil, "",
		},
		// non-tools/call (initialize) -> unchanged. BYTE-IDENTITY.
		{
			"initialize_unchanged",
			`{"jsonrpc":"2.0","method":"initialize","id":1}`,
			true, true, "", nil, "",
		},
		// non-tools/call (tools/list) -> unchanged. BYTE-IDENTITY.
		{
			"toolslist_unchanged",
			`{"jsonrpc":"2.0","method":"tools/list","id":3}`,
			true, true, "", nil, "",
		},
		// JSON ARRAY (defensive batch): element[1] tools/call with alias -> rewrite;
		// body re-serialized as an array; one rewritten id (float64(5)) (PRD §11.1 point 2).
		{
			"array_batch_one_rewrite",
			`[{"jsonrpc":"2.0","method":"initialize","id":1},` +
				`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"arguments":{"q":"hi"}}}]`,
			false, false, "search_query", float64(5), "renamed",
		},
		// JSON ARRAY with NO tools/call rewrite -> unchanged, BYTE-IDENTITY.
		{
			"array_batch_no_rewrite",
			`[{"jsonrpc":"2.0","method":"initialize","id":1}]`,
			true, true, "", nil, "",
		},
		// tools/call with params absent -> skip, unchanged.
		{
			"toolscall_no_params",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call"}`,
			true, true, "", nil, "",
		},
		// tools/call with params.arguments absent -> skip, unchanged.
		{
			"toolscall_no_arguments",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"web_search_prime"}}`,
			true, true, "", nil, "",
		},
		// tools/call with params.arguments a NON-object (array) -> skip, unchanged (PRD §11.1 point 3).
		{
			"toolscall_arguments_array",
			`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"arguments":["a","b"]}}`,
			true, true, "", nil, "",
		},
		// tools/call with a STRING id that is rewritten -> id keyed by the string.
		{
			"toolscall_string_id",
			`{"jsonrpc":"2.0","id":"abc","method":"tools/call","params":{"arguments":{"query":"x"}}}`,
			false, false, "search_query", "abc", "renamed",
		},
		// invalid JSON -> pass through unchanged, BYTE-IDENTITY (never reject).
		{
			"invalid_json_passthrough",
			`{not valid json`,
			true, true, "", nil, "",
		},
		// valid JSON scalar -> pass through unchanged (default switch arm).
		{
			"scalar_json_passthrough",
			`42`,
			true, true, "", nil, "",
		},
		// multiple aliases present -> query promoted, q dropped; combined notes.
		{
			"toolscall_query_and_q",
			`{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"arguments":{"query":"x","q":"y"}}}`,
			false, false, "search_query", float64(9), "dropped",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			in := []byte(tc.body)
			dec := decideRewrite(in, dcCfg)

			if dec.streamThrough != tc.wantStream {
				t.Errorf("streamThrough=%v, want %v", dec.streamThrough, tc.wantStream)
			}
			if tc.wantBodyIdent {
				// Unchanged path: body must be byte-for-byte identical to input.
				if !bytes.Equal(dec.body, in) {
					t.Errorf("unchanged body drifted:\n got %q\nwant %q", dec.body, in)
				}
			}
			if tc.wantIDKey != nil {
				if len(dec.rewrittenIDs) != 1 {
					t.Fatalf("rewrittenIDs len=%d, want 1: %#v", len(dec.rewrittenIDs), dec.rewrittenIDs)
				}
				if !dec.rewrittenIDs[tc.wantIDKey] {
					t.Errorf("rewrittenIDs missing key %#v (float64 contract): %#v", tc.wantIDKey, dec.rewrittenIDs)
				}
			} else if len(dec.rewrittenIDs) != 0 {
				t.Errorf("expected no rewritten ids, got %#v", dec.rewrittenIDs)
			}
			if tc.wantArgKey != "" {
				args := firstArgValue(t, dec.body)
				if args[tc.wantArgKey] == nil {
					t.Errorf("dec.body missing params.arguments.%s", tc.wantArgKey)
				}
				// The renamed alias must be GONE (only the canonical key remains).
				if args["query"] != nil || args["q"] != nil {
					t.Errorf("alias key leaked into rewritten args: %#v", args)
				}
			}
			if tc.wantWarningHas != "" {
				if dec.warning == "" {
					t.Errorf("warning empty, want it to contain %q", tc.wantWarningHas)
				} else if !strings.Contains(dec.warning, tc.wantWarningHas) {
					t.Errorf("warning=%q, want it to contain %q", dec.warning, tc.wantWarningHas)
				}
			} else if dec.warning != "" {
				t.Errorf("expected empty warning, got %q", dec.warning)
			}
		})
	}
}

// TestDecideRewrite_Float64IDContract pins the load-bearing contract with
// sse.Inject: numeric ids are keyed by float64(N), and an int(N) key does NOT
// match (the producer and consumer both decode via encoding/json -> float64).
func TestDecideRewrite_Float64IDContract(t *testing.T) {
	dec := decideRewrite([]byte(dcArgsQueryBody), dcCfg) // id:2
	if dec.streamThrough {
		t.Fatal("expected a rewrite, got streamThrough")
	}
	if !dec.rewrittenIDs[float64(2)] {
		t.Errorf("rewrittenIDs[float64(2)]=false, want true (Inject matches this)")
	}
	if dec.rewrittenIDs[int(2)] {
		t.Errorf("rewrittenIDs[int(2)]=true, want false — int keys must NOT match")
	}
}

// TestDecideRewrite_UnchangedBodyIsOriginalBytes guards PRD §11.1 point 4:
// nothing-changed => forward the ORIGINAL bytes (no re-serialization, no drift).
func TestDecideRewrite_UnchangedBodyIsOriginalBytes(t *testing.T) {
	// Non-canonical key order + extra whitespace: a re-serialization would SORT keys
	// and drop this formatting; the original-bytes path must preserve it verbatim.
	in := []byte(`{  "id" : 1 , "method":"initialize" , "jsonrpc":"2.0" }`)
	dec := decideRewrite(in, dcCfg)
	if !dec.streamThrough {
		t.Fatal("expected streamThrough for a non-tools/call body")
	}
	if !bytes.Equal(dec.body, in) {
		t.Errorf("unchanged body drifted (should be byte-identical):\n got %q\nwant %q", dec.body, in)
	}
}
```

### Implementation Patterns & Key Details

```go
// PATTERN: pure decision function. decideRewrite([]byte, Config) -> struct. No
// *http.Request, no network, no logging. This makes it trivially unit-testable
// (the item's MOCKING requirement) and lets forward()/newProxyHandler own all I/O.

// PATTERN: array-vs-object switch over a decoded `any`. json.Unmarshal into any
// yields map[string]any for an object and []any for an array; the default arm
// catches scalars. This is the defensive batch handling (PRD §11.1 point 2) and
// the "never reject on a parse quirk" guarantee in one place.

// PATTERN: track the id by the DECODED value. rewrittenIDs[obj["id"]] uses the
// float64/string the decoder produced — exactly what Inject's response-side
// lookup uses. Do not int()-convert; the test pins float64==true, int==false.

// PATTERN: original-bytes passthrough on the unchanged path. Return the input
// []byte verbatim (not a re-marshal) so key order / whitespace / escaping are
// preserved. Only the CHANGED path json.Marshal's. The test asserts byte-identity.

// GOTCHA (restated): Content-Length. bytes.NewReader(dec.body) auto-sets
// outReq.ContentLength; copyForwardHeaders copies the client's (now-stale)
// Content-Length; Del it. Transport writes the correct length from the field.
// (Verified — a deliberately-wrong copied header was ignored on the wire.)

// GOTCHA (restated): forward()'s body is UNCHANGED. Only its signature gains
// `dec rewriteDecision`. The response conditional (io.Copy vs Inject) is
// P1.M4.T2.S2. dec is an unused parameter here — that compiles and vets cleanly.
```

### Integration Points

```yaml
FILES MODIFIED:
  - proxy.go       (EXTEND: + rewriteDecision + decideRewrite + rewriteObject + docs;
                      imports += bytes, encoding/json. MODIFY: newProxyHandler closure
                      body + forward signature; forward body byte-for-byte unchanged.)
  - proxy_test.go  (EXTEND: + TestDecideRewrite + TestDecideRewrite_Float64IDContract
                      + TestDecideRewrite_UnchangedBodyIsOriginalBytes + dcCfg const
                      + firstArgValue helper. No new imports.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires (stdlib only — needs bytes + encoding/json, both stdlib).
  - main.go / health_test.go: call newProxyHandler(cfg, log, client) — signature
        UNCHANGED, so no edits.
  - rewrite.go: the rule decideRewrite invokes. Untouched.
  - sse.go: warningText (called) + Inject (downstream consumer). Untouched.
  - config.go / doc.go / *_test.go (other) / testdata/*.sse: zero edits.
CONSUMER SEAM (this decision feeds — keep the struct/func stable):
  - P1.M4.T2.S1 (forward): consumes dec.body (already set as outReq.Body) + the
        forward mechanics (headers, hop-by-hop strip, Accept fallback, ctx) — these
        are ALREADY in newProxyHandler/forward from T4.S2; T2.S1 is largely verification.
  - P1.M4.T2.S2 (response): `if len(dec.rewrittenIDs) > 0 {
        Inject(rw, resp.Body, dec.rewrittenIDs, dec.warning) } else {
        _, err = io.Copy(rw, resp.Body) }`; then http.Flusher.Flush(). Depends on
        dec.rewrittenIDs being map[any]bool keyed by float64(N) and dec.warning
        being the warningText output — both guaranteed by decideRewrite.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# Format + vet (and confirm the only edited files are proxy.go + proxy_test.go).
gofmt -w proxy.go proxy_test.go
go vet ./...
git diff --stat           # expect: proxy.go + proxy_test.go ONLY
git diff go.mod           # expect: EMPTY (zero new requires)

# Expected: gofmt clean; vet clean; only proxy.go + proxy_test.go changed; go.mod
# unchanged. If vet reports a duplicate import, REMOVE the duplicate (io/net/http/
# time already present; only bytes + encoding/json are new).
```

### Level 2: Unit Tests (Component Validation)

```bash
# Targeted: run ONLY the new pure decision tests, verbose.
go test -run TestDecideRewrite -v

# MUST PASS (the ones that prove §11.1 fidelity):
#   TestDecideRewrite/toolscall_query_renamed  -> search_query present, query gone, id float64(2)
#   TestDecideRewrite/toolscall_canonical_..   -> unchanged, byte-identical, no ids, empty warning
#   TestDecideRewrite/toolscall_nonalias_..    -> unchanged (PRD §3)
#   TestDecideRewrite/initialize_unchanged     -> unchanged, byte-identical
#   TestDecideRewrite/array_batch_one_rewrite  -> re-serialized as array, id float64(5)
#   TestDecideRewrite/toolscall_arguments_array-> skip (non-object arguments), unchanged
#   TestDecideRewrite/invalid_json_passthrough  -> unchanged, byte-identical
#   TestDecideRewrite_Float64IDContract         -> float64(2) matches, int(2) does NOT
#   TestDecideRewrite_UnchangedBodyIsOriginalBytes -> non-canonical formatting preserved
# Expected: PASS, exit 0. If toolscall_query_renamed fails on the id, the cause is
# almost always an int() conversion (keyed by int, not float64) — re-check addID.

# Regression: the existing e2e harness MUST still pass (non-tools/call bodies).
go test -run TestPassthrough -v
# Expected: PASS — these post initialize/{}/Accept-test bodies that take the
# unchanged path, so behavior is identical to T4.S2 (only the body source changed
# from streaming r.Body to bytes.NewReader(originalBytes)).
```

### Level 3: Integration Testing (System Validation)

```bash
# Confirm the module + full suite stay healthy and the package still compiles with
# the new forward() signature and the threaded dec.
go build ./...        # must compile (dec unused by non-test code so far — still builds)
go test ./...         # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . decideRewrite  # sanity: doc comment present, shows the three rules (Mode A)
go doc . rewriteDecision

# Expected: build clean; full suite green; go doc . decideRewrite prints the
# original-bytes-passthrough, array-handling, and id-tracking rules. NOTE: the
# end-to-end "rewrite request -> warning appears in response" flow is NOT yet
# complete — the response-side Inject branch lands in P1.M4.T2.S2. After THIS
# item, a tools/call with `query` reaches z.ai as `search_query` (request fixed)
# but the response is still io.Copy'd verbatim (no warning injected yet).
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Pin the float64-id contract (the load-bearing seam with sse.Inject).
go test -run TestDecideRewrite_Float64IDContract -v
# If this fails, rewrittenIDs was built from int keys and Inject will NEVER match —
# the warning would silently not inject downstream. Must be green.

# (b) Pin original-bytes passthrough (PRD §11.1 point 4 formatting-drift guard).
go test -run TestDecideRewrite_UnchangedBodyIsOriginalBytes -v
# Feeds non-canonical key order + whitespace; asserts byte-identity. A failure
# means the unchanged path is re-serializing (drift) — fix the early-return.

# (c) Confirm no stale Content-Length reaches a (fake) upstream after rewrite.
#     Reuses the T4.S2 fakeUpstream harness; asserts the upstream body parses and
#     its Content-Length matches the RE-SERIALIZED length (not the original).
cat >> /tmp/cl.go <<'EOF'  # illustrative — not committed; run ad hoc if desired
EOF
# (A committed regression for (c) is OPTIONAL; the empirical probe in research/
# go-proxy-body-rewrite.md already verifies Transport honors outReq.ContentLength.
# The existing TestPassthrough suite exercises the bytes.NewReader path indirectly.)

# Expected: (a) and (b) PASS. (c) is verified by the research probe; a committed
# e2e Content-Length assertion is a candidate for P1.M5.T1.S2 (the e2e suite).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `proxy.go` + `proxy_test.go`; `go.mod` unchanged.
- [ ] Level 2 passes: `go test -run TestDecideRewrite -v` green (all rows +
      Float64IDContract + UnchangedBodyIsOriginalBytes).
- [ ] Level 2 regression: `go test -run TestPassthrough -v` green (T4.S2 e2e intact).
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc .
      decideRewrite` shows the three rules (Mode A).

### Feature Validation

- [ ] `type rewriteDecision{body, streamThrough, rewrittenIDs map[any]bool, warning}` exists in `proxy.go`.
- [ ] `func decideRewrite(body []byte, cfg Config) rewriteDecision` exists in `proxy.go`.
- [ ] `func rewriteObject(obj map[string]any, cfg Config) []string` exists in `proxy.go`.
- [ ] tools/call with `query` → `search_query` present + `query` gone; `rewrittenIDs[float64(2)]==true`, `[int(2)]==false`; `warning` contains "renamed".
- [ ] canonical-only / non-alias / non-tools/call / invalid-JSON / scalar → `streamThrough==true`, `dec.body` byte-identical to input, no ids, empty warning.
- [ ] JSON ARRAY batch → re-serialized as array; rewritten id keyed by `float64(5)`.
- [ ] tools/call with non-object `params.arguments` (array) or missing params → unchanged.
- [ ] `newProxyHandler` reads body → `decideRewrite` → `bytes.NewReader(dec.body)` → `Del("Content-Length")` → `forward(..., dec, log)`; factory signature unchanged.
- [ ] `forward` signature is `forward(client, rw, outReq, dec rewriteDecision, log)`; body unchanged (io.Copy; dec threaded, unused until P1.M4.T2.S2).

### Code Quality Validation

- [ ] `rewriteDecision`/`decideRewrite`/`rewriteObject` are APPENDED to `proxy.go` (hop/forward helpers untouched except the forward signature).
- [ ] `newProxyHandler` closure MODIFIED in place; factory signature preserved (main.go/health_test.go/proxy_test.go need no edits).
- [ ] Tests are APPENDED to `proxy_test.go`; no redeclared `initSSE`/`testSID`/`fakeUpstream`/`newTestProxy`.
- [ ] Imports: only `bytes` + `encoding/json` added; no duplicate `io`/`net/http`/`time`.
- [ ] Doc comment on `decideRewrite` cites PRD §11.1 and documents original-bytes passthrough, array handling, and the float64 id-tracking contract (Mode A).
- [ ] `rewrittenIDs` keyed by decoded `obj["id"]` (float64 for numbers), never int.
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `go doc . decideRewrite` prints the three rules.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't re-serialize the UNCHANGED body. PRD §11.1 point 4: when nothing
  changed, forward the ORIGINAL bytes. `json.Marshal` would sort keys + HTML-escape
  and silently drift the formatting. The test feeds non-canonical formatting and
  asserts byte-identity.
- ❌ Don't key `rewrittenIDs` by `int`. `encoding/json` decodes JSON numbers into
  `float64` in `any`; `map[any]bool{int(2):true}` would NOT match `Inject`'s
  response-side `float64(2)` lookup, so the warning would silently never inject.
  Key by the decoded `obj["id"]` value (float64(N)). The Float64IDContract test
  pins this.
- ❌ Don't leave a stale `Content-Length` on the rewritten request.
  `copyForwardHeaders` copies the client's `Content-Length` (not hop-by-hop), but
  after re-serialization the byte count changed. `bytes.NewReader(dec.body)` sets
  `outReq.ContentLength` correctly; `outReq.Header.Del("Content-Length")` drops the
  stale copy. Transport writes the correct length from the field (verified). Forgetting
  the Del is not fatal (Transport ignores the stale header) but is sloppy — Del it.
- ❌ Don't touch non-alias params. PRD §3: only configured aliases are renamed. A
  tools/call with `{"foo":"bar"}` or `{"max_results":10}` passes through UNCHANGED
  with no warning. `Rewrite` (DONE) already enforces this; `decideRewrite` just
  reports `Changed==false`.
- ❌ Don't reject invalid JSON or scalar bodies. The proxy must never block a
  request on a parse quirk — forward it unchanged and let z.ai decide. A decode
  error OR a decoded scalar returns `streamThrough=true` with the original bytes.
- ❌ Don't implement the response conditional in `forward`. Threading `dec` into
  `forward` is THIS item's seam; the `if len(dec.rewrittenIDs) > 0 { Inject } else
  { io.Copy }` branch is P1.M4.T2.S2's explicit deliverable. Leaving `dec` unused
  in `forward` (for now) compiles and vets cleanly.
- ❌ Don't change `newProxyHandler`'s factory signature. `main.go`, `health_test.go`,
  and `proxy_test.go` all call `newProxyHandler(cfg, log, client)`. Keep it; only
  modify the closure body. (`forward`'s signature DOES change, but it has exactly
  one caller — inside `newProxyHandler` — so the blast radius is `proxy.go` only.)
- ❌ Don't reformat `forward`'s body. Only its signature changes (add `dec`). The
  rest stays byte-for-byte as T4.S2 wrote it (response-header copy, io.Copy, flush).
- ❌ Don't call `warningText` on the unchanged path. It returns `""` for empty
  notes, but `decideRewrite` returns `warning:""` directly on the unchanged path
  without calling it (notes is nil there). Only the CHANGED path calls
  `warningText(notes)` where notes is guaranteed non-empty.
- ❌ Don't OVERWRITE `proxy.go`/`proxy_test.go` or redeclare existing helpers.
  APPEND the decision symbols + tests; MODIFY `newProxyHandler`/`forward` in place.
  `rewrite.go` owns `Rewrite`; `sse.go` owns `warningText`/`Inject`; this item owns
  `decideRewrite`/`rewriteObject`/`rewriteDecision`. Add `bytes`+`encoding/json` to
  imports only if absent.
- ❌ Don't modify `rewrite.go`, `sse.go`, `go.mod`, `PRD.md`, `testdata/*`, or any
  file other than `proxy.go` + `proxy_test.go`.
