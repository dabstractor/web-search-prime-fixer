name: "P1.M4.T3.S1 — rewrite + upstream_error + debug forward log events"
description: |

  OWN and VERIFY the three observability events in `proxy.go`'s request handler that
  make the proxy fully observable per PRD §15. Two events are NEW (this item adds
  them); one is already on disk and this item merely VERIFIES it:
    - `rewrite` (info) — NEW. Emitted in `newProxyHandler` right after
      `decideRewrite` returns, ONLY when a tools/call argument was rewritten
      (`!dec.streamThrough`). Fields: `req_id`, `tool` (= `params.name` if present),
      `notes` (the per-alias array), and `present` (a per-configured-alias boolean
      map captured BEFORE the rewrite mutated args).
    - `forward` (debug) — NEW. Emitted in `newProxyHandler` right after `forward()`
      returns, for EVERY request. Fields: `method` (JSON-RPC method), `id`
      (= `dec.reqID`), `mode` (`"streamed"` when `dec.streamThrough`, else
      `"injected"`). Suppressed when the logger's level is above debug (PRD §15
      "Levels honored"; item MOCKING "info level suppresses debug forward lines").
    - `upstream_error` (warn) — ALREADY DONE by P1.M4.T2.S1 at `proxy.go:182`
      (`if resp.StatusCode >= 300` → `log.log("warn","upstream_error",{status,req_id})`).
      `>= 300` = non-2xx, which is PRD-§15-correct ("on non-2xx upstream responses").
      This item VERIFIES it is present and correct; it does NOT re-implement it.

  Because the `rewrite`/`forward` lines need fields that `rewriteDecision` (proxy.go)
  does not yet carry (`notes`, `tool`, per-alias `present`, and the JSON-RPC
  `method`), this item ADDS four additive fields to `rewriteDecision` and widens the
  unexported `rewriteObject` helper's return from `[]string` to a small struct so the
  single parse point also yields the tool name + presence map (presence MUST be
  captured BEFORE `Rewrite` deletes/renames the aliases — it cannot be re-derived
  from the re-serialized `dec.body`). `rewriteObject` is called only inside
  `decideRewrite` (verified: zero test callers), so the signature change is safe.

  Both new log emissions live in `newProxyHandler` (the handler closure) — NOT in
  `forward()`. `forward()`'s body is already at its post-P1.M4.T2.S2 state on disk
  (the `if dec.streamThrough { io.Copy } else { Inject }` branch + `flushWriter` are
  landed) and MUST stay untouched. Putting the `forward` debug line in the handler
  (not inside `forward`) keeps it next to the `rewrite` line and removes any risk of
  colliding with the parallel T2.S2 response-body edits. `go.mod` gains ZERO
  requires; `proxy.go` adds NO new imports (`strings` is already imported by T2.S2).
  Tests live in a NEW file `proxy_log_test.go` (package main) to avoid an append
  collision with T2.S2's concurrent `proxy_test.go` appends; it adds NO new imports.
  **Mode A docs**: `rewriteLogFields` + the two `log.log` calls carry doc comments
  naming the PRD §15 events + the "never log Authorization" invariant; the final
  README sweep (P1.M5.T3.S1) documents log levels/events to operators.

---

## Goal

**Feature Goal**: The proxy is fully observable per PRD §15. Every applied alias
rewrite produces one info-level structured `rewrite` JSON line naming the request,
the tool, the notes, and which configured aliases were present; every request
produces one debug-level `forward` line naming the JSON-RPC method, the id, and
whether the response was streamed or injected; and non-2xx upstream responses
already produce the warn-level `upstream_error` line (DONE). Level filtering is
honored throughout — an info-level logger keeps `rewrite`/`upstream_error`/`startup`
but suppresses the debug `forward` lines. No secret (Authorization/Cookie/…) is ever
logged.

**Deliverable**: NO new production files. Three edits, all `package main`:
- **MODIFY `proxy.go`** — (a) add four additive fields to `rewriteDecision`
  (`method string`, `tool string`, `notes []string`, `present map[string]bool`);
  (b) widen `rewriteObject`'s return to a struct `rewriteObjectResult{changed,notes,
  tool,present}` and populate it (capturing `present` + `tool` BEFORE `Rewrite`
  mutates args); (c) thread `method`/`tool`/`notes`/`present` through `decideRewrite`'s
  return paths; (d) add an unexported `rewriteLogFields(dec rewriteDecision)
  map[string]any` helper; (e) emit the `rewrite` (info) line after `decideRewrite`
  when `!dec.streamThrough` and the `forward` (debug) line after `forward()` returns.
  `forward()`'s body and signature are UNCHANGED.
- **CREATE `proxy_log_test.go`** (package main) — `TestLog_RewriteEvent`,
  `TestLog_NoRewriteWhenUnchanged`, `TestLog_ForwardDebugEvent` (covering injected
  mode, streamed mode, and the info-suppresses-debug level filter), plus a tiny
  `findMsg` JSONL helper. Reuses `captureProxy`/`fakeUpstream`/`testSID` from
  `proxy_test.go`; adds NO imports beyond stdlib already used.
- **VERIFY** (no edit) that `upstream_error` (`proxy.go:182`, `>= 300`) is present
  and that the existing `TestForward_Non2xxCopiedThroughAndLogged` still passes.

**Success Definition**: `go test -run 'TestLog_|TestForward_Non2xx|TestForward_2xx' -v`
passes; in particular: (1) an aliased tools/call emits exactly one info `rewrite`
line with `req_id`, `tool`, a non-empty `notes` array, and a `present` map, and NO
`Authorization` field; (2) a canonical/non-tools/call request emits NO `rewrite`
line; (3) at debug level every request emits a `forward` line with `method`, `id`,
and `mode` (`injected` for a rewrite, `streamed` otherwise); (4) at info level the
`forward` line is suppressed while `rewrite`/`upstream_error` still appear. The
existing `TestPassthrough_*`, `TestDecideRewrite*`, `TestForward_*`, `TestSSE*`
suites stay GREEN. `go vet ./...` and `go test ./...` stay clean. `go.mod`
unchanged; `proxy.go` import block unchanged.

## Hard Prerequisites

1. **`rewriteDecision` exists** with `body`, `streamThrough`, `rewrittenIDs`,
   `warning`, `reqID` (P1.M4.T1.S1 + P1.M4.T2.S1 — DONE on disk). This item ADDS
   `method`, `tool`, `notes`, `present` (all additive; existing readers —
   `forward()`, `newProxyHandler`, and every test — read only the original five, so
   nothing breaks).
2. **`forward()` is at post-T2.S2 state** (DONE on disk): its body branches on
   `dec.streamThrough` (`io.Copy` vs `Inject`) with the `flushWriter` SSE wrapper,
   and emits the non-2xx `upstream_error {status, req_id}` log at `proxy.go:182`.
   This item does NOT touch `forward()`'s body or signature. The `forward` debug
   line goes in the HANDLER (`newProxyHandler`), not inside `forward`.
3. **`logger` exists** (main.go, P1.M1.T3.S1 — DONE): `(l *logger).log(level, msg,
   fields map[string]any)` writes one JSON line and drops messages below the
   configured level. `newLogger(w, level)`, `redactHeaders(h)`, and the
   `levelDebug<levelInfo<levelWarn<levelError` ranks are all DONE. Filtering is the
   logger's job — this item just emits at the right level (`info` for `rewrite`,
   `debug` for `forward`).
4. **`Rewrite` mutates args in place** (rewrite.go, P1.M2.T1 — DONE): it deletes the
   renamed alias and any dropped aliases. Therefore per-alias presence flags CANNOT
   be re-derived from `dec.body` after the rewrite — they MUST be captured inside
   `rewriteObject` BEFORE `Rewrite(args, ...)` is called. This is the load-bearing
   reason `rewriteObject` (not the handler) owns capturing `present` + `tool`.
5. **`captureProxy(t, upstreamURL, buf, level)`** exists in `proxy_test.go` (T2.S1):
   builds a proxy whose logger writes to `buf` at `level`. It is the log-capture
   harness these tests reuse. `fakeUpstream(t, &got)`, `testSID`, `dcCfg`,
   `firstArgValue` are also present — reuse, do NOT redeclare.

## User Persona

**Target User**: (1) the **operator** running the proxy: tailing stderr shows, per
   request, whether a call was rewritten (info `rewrite`), whether the upstream
   errored (warn `upstream_error`), and — at debug level — every forward with its
   method/id and streamed-vs-injected mode. (2) **P1.M5.T1.S1/S2** (e2e harness):
   will assert on these log lines (e.g. "a rewrite produces a JSON line with notes").
   (3) **P1.M5.T3.S1** (README): documents the log levels/events to operators using
   the field set this item ships.

**Use Case**: a client sends `{"method":"tools/call","params":{"name":"web_search",
"arguments":{"query":"x"}},"id":2}`. The operator sees one info line
`{"msg":"rewrite","req_id":2,"tool":"web_search","notes":["\"query\" is not a valid
parameter; renamed to \"search_query\""],"present":{"query":true,"q":false,...}}`
and, at debug level, one `{"msg":"forward","method":"tools/call","id":2,"mode":
"injected"}` line. If z.ai returns 503, the operator ALSO sees a warn
`upstream_error` line (already shipped). No Authorization value ever appears.

**Pain Points Addressed**: (1) Without the `rewrite` line, an operator cannot tell
   from logs which calls were actually fixed (only the SSE warning reaches the
   client). (2) Without the `forward` debug line, debugging "did this request stream
   or get injected?" requires packet capture. (3) Without level filtering on these
   lines, debug noise would flood production stderr.

## Why

- **PRD §15 (Logging)** is the exact contract: "`rewrite`: `req_id`, `tool` (if
  present), `notes` (the array), param presence flags. Logged whenever FR-2 changes
  a call. … `debug` adds per-request `forward` lines with method/id and whether the
  response was streamed or injected. … Levels honored." This item implements the two
  unshipped events (`rewrite`, `forward`) and verifies the shipped one
  (`upstream_error`).
- **PRD §6 / §13 (No secrets in logs)**: "`Authorization` is never logged." The
  `rewrite`/`forward` events log NO headers, so the invariant holds trivially; the
  test pins it (asserts no `Authorization` field and no `Bearer` substring) as a
  regression guard. `redactHeaders` is the documented tool if any future field adds
  header context.
- **Item OUTPUT** ("Full observability per PRD §15; the proxy is feature-complete"):
  this is the last core-feature item before the end-to-end suite (M5) and the docs
  sweep. After it, every PRD §15 event is emitted and level-filtered.
- **Coherence across the chain**: T1.S1 decides + T2.S1/T2.S2 forward; THIS item
  only OBSERVES the decision via additive fields and emits two log lines in the
  handler. It does not alter request/response mechanics, does not touch `forward()`,
  and does not change any existing test's assertions.

## What

Four additive fields on one struct, one helper-signature widening, one small log-
field helper, two `log.log` calls in the handler, and a new test file. Visible
behavior: rewrites produce an info `rewrite` line; every request produces a debug
`forward` line (suppressed below debug); `upstream_error` is unchanged. No new
files in production; the only new file is `proxy_log_test.go`.

### Success Criteria

- [ ] `rewriteDecision` carries additive `method string`, `tool string`,
      `notes []string`, `present map[string]bool`; populated by `decideRewrite`
      (notes/tool/present only on the rewrite path; method on every valid object).
- [ ] `rewriteObject(obj, cfg)` returns `rewriteObjectResult{changed, notes, tool,
      present}`; `present` is a per-`cfg.Aliases` boolean map captured BEFORE
      `Rewrite` mutates args; `tool` is `params["name"]` (string, "" if absent).
- [ ] `newProxyHandler` emits `log.log("info","rewrite", rewriteLogFields(dec))`
      iff `!dec.streamThrough`, immediately after `dec := decideRewrite(body, cfg)`.
- [ ] `newProxyHandler` emits `log.log("debug","forward", {method, id, mode})`
      after `forward(...)` returns; `mode` is `"streamed"` iff `dec.streamThrough`.
- [ ] `upstream_error` remains at `proxy.go:182` (`>= 300`, `{status, req_id}`,
      warn) — VERIFIED unchanged (existing `TestForward_Non2xxCopiedThroughAndLogged`
      still passes).
- [ ] At logger level `info`: a rewrite emits the `rewrite` line and NO `forward`
      line; a non-2xx emits the `upstream_error` line.
- [ ] At logger level `debug`: every request emits a `forward` line with the correct
      `method`/`id`/`mode`.
- [ ] A `rewrite` line contains `req_id`, `tool`, a non-empty `notes` array, and a
      `present` map, and contains NO `Authorization` field / no `Bearer` substring.
- [ ] `forward()`'s body and signature are byte-for-byte unchanged; `proxy.go`'s
      import block is unchanged; `go.mod` unchanged; no `.go` file other than
      `proxy.go`/`proxy_log_test.go` is created or edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact additive fields + the `rewriteObject` struct + the `decideRewrite`
return-path edits are given verbatim, with the load-bearing "capture present BEFORE
Rewrite mutates args" rule justified (Rewrite deletes/renames aliases); (b) the two
`log.log` call sites are pinpointed in `newProxyHandler` ("right after decideRewrite"
and "right after forward() returns") so they cannot collide with `forward()`'s body;
(c) the field/level table is fixed (rewrite=info, forward=debug, upstream_error=warn)
and grounded in PRD §15; (d) the `rewriteLogFields` helper is given; (e) the three
tests are enumerated with literal request bodies, the `captureProxy` harness, decoded
JSONL assertions, and the level-filter case; (f) the new test file's package + import
set + non-collision rationale are stated; (g) `upstream_error` is confirmed DONE on
disk with the exact line number and threshold.

### Documentation & References

```yaml
# MUST READ — the logging contract this item implements.
- file: PRD.md
  section: "§15 Logging" + "§6 (No secrets in logs)" + "§13 (Headers, credentials, security)"
  why: §15 names every event + its fields + "Levels honored". §15 verbatim: "rewrite:
        req_id, tool (if present), notes (the array), param presence flags. Logged
        whenever FR-2 changes a call." and "debug adds per-request forward lines with
        method/id and whether the response was streamed or injected." §6/§13 = never
        log Authorization.
  critical: §15 "Levels honored" => the debug forward line MUST be suppressed at info
        level — rely on the logger's existing level filter (levelNum(level) < l.level
        drops it). Do NOT invent a second filter.

# MUST READ — the logger this item calls.
- file: main.go
  why: (l *logger).log(level, msg, fields map[string]any) writes one JSON line and
        drops below-level messages. levelDebug<levelInfo<levelWarn<levelError.
        redactHeaders(h http.Header) map[string]any replaces Authorization/Cookie/
        Set-Cookie/Proxy-Authorization with "<redacted>". newLogger(w, level).
  pattern: every existing event is ONE log.log call with a map[string]any: startup
        (main.go logStartup), upstream_error (proxy.go:182), shutdown (main.go).
        Follow that pattern EXACTLY — one call, one map, no bespoke writer.
  critical: the rewrite/forward events log NO headers, so redactHeaders is NOT needed
        for them — but NEVER add a header value to these events without routing it
        through redactHeaders first (PRD §13).

# MUST READ — the decision struct + decideRewrite this item extends (additively).
- file: proxy.go
  why: rewriteDecision (line ~234) carries body/streamThrough/rewrittenIDs/warning/
        reqID. decideRewrite (line ~291) is the SINGLE parse point; it already
        collects `notes` locally and discards them after warningText(notes).
        rewriteObject (line ~362) returns []string (Notes) and is the ONLY place
        that touches args before Rewrite mutates them.
  pattern: reqID is captured as "the FIRST JSON-RPC id seen while iterating" (batch =
        defensive). Mirror that for tool/present: capture on the first CHANGED object.
  gotcha: rewriteObject is called at proxy.go:318 (single object) and :328 (batch
        element) — BOTH must be updated for the new struct return. Verified: ZERO
        test files call rewriteObject directly, so the signature widening is safe.

# MUST READ — the handler this item edits (newProxyHandler) + the forward() it must NOT edit.
- file: proxy.go
  section: "newProxyHandler (line ~74) and forward (line ~146)"
  why: newProxyHandler is where the two log.log calls go: `dec := decideRewrite(body,
        cfg)` (line ~96) — emit rewrite right after, gated on !dec.streamThrough;
        `forward(client, rw, outReq, dec, log)` (line ~124) — emit forward right after.
        forward()'s body is DONE (T2.S2): conditional io.Copy/Inject + flushWriter +
        the non-2xx upstream_error log at line 182. DO NOT touch forward()'s body.
  critical: the rewrite line must fire on the REQUEST-side decision (!dec.streamThrough)
        regardless of the response — even if the upstream later errors or the SSE event
        id doesn't match, the rewrite happened and must be logged. Place it BEFORE
        forward(). The forward (debug) line is placed AFTER forward() returns so it
        represents a completed forward; its mode reflects the path decision
        (dec.streamThrough), which is exactly what forward() used.

# MUST READ — the test harness to reuse (do NOT redeclare).
- file: proxy_test.go
  why: defines captureProxy(t, upstreamURL, buf, level) [the log-capture proxy],
        fakeUpstream(t, &got), testSID, initSSE, dcCfg, firstArgValue. TestForward_
        Non2xxCopiedThroughAndLogged + TestForward_2xxNoUpstreamError already pin
        upstream_error — they stay GREEN and serve as the upstream_error verification.
  pattern: captureProxy writes logger output to a *bytes.Buffer at a chosen level;
        tests then bytes.Split(buf,"\n") + json.Unmarshal each line + assert on msg.
  critical: T2.S2 APPENDS to proxy_test.go concurrently (TestForward_* / TestFlushWriter_*
        + helpers). Put the log tests in a NEW file proxy_log_test.go (package main) to
        avoid an append collision. Do NOT redeclare captureProxy/fakeUpstream/testSID.

# MUST READ — Rewrite mutates args in place (why presence must be captured pre-mutation).
- file: rewrite.go
  why: Rewrite(args, aliases, target) deletes the chosen alias (args[target]=args[chosen];
        delete(args,chosen)) and drops the rest (delete(args,a)). So after Rewrite the
        aliases are GONE from args; dec.body (re-serialized) has only the target. Presence
        flags therefore CANNOT be recomputed from dec.body — capture inside rewriteObject.
  critical: capture present BEFORE calling Rewrite(args,...). Capture tool (params.name)
        from params (unaffected by Rewrite) at the same point for symmetry/DRY.

- url: https://pkg.go.dev/encoding/json#Marshal
  why: json.Marshal(map[string]bool{"k":true}) -> {"k":true}; []string -> ["a","b"];
        any(float64(2)) -> 2; nil map -> null. Confirms the rewrite/forward field shapes.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package main comment — UNTOUCHED
  main.go           # logger + bootstrap — UNTOUCHED (logger + redactHeaders reused)
  config.go         # Config + DefaultConfig — UNTOUCHED
  proxy.go          # newProxyHandler + forward + decideRewrite + rewriteObject
                    #   + rewriteDecision — MODIFY (additive fields + 2 log calls +
                    #   rewriteObject struct + rewriteLogFields helper). forward() UNTOUCHED.
  rewrite.go        # Rewrite — UNTOUCHED
  sse.go            # Inject/emitEvent/warningText — UNTOUCHED
  proxy_test.go     # T4.S2/T1.S1/T2.S1/T2.S2 tests — UNTOUCHED (T2.S2 appends concurrently)
  logger_test.go    # logger unit tests — UNTOUCHED
  *_test.go / testdata/*.sse — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
proxy.go            # MODIFY: +4 additive fields on rewriteDecision; widen rewriteObject to
                    #   return rewriteObjectResult{changed,notes,tool,present}; thread the new
                    #   fields through decideRewrite's return paths; + rewriteLogFields helper;
                    #   + rewrite (info) log call after decideRewrite; + forward (debug) log
                    #   call after forward(). forward() body/signature UNCHANGED. NO new imports.
proxy_log_test.go   # CREATE (package main): TestLog_RewriteEvent + TestLog_NoRewriteWhenUnchanged
                    #   + TestLog_ForwardDebugEvent + findMsg helper. Reuses captureProxy/
                    #   fakeUpstream/testSID. Imports bytes/encoding/json/io/net/http/
                    #   net/http/httptest/strings/testing (all stdlib; NO `os`).
```

No other file changes. `main.go` calls `newProxyHandler(cfg, log, client)` — its
signature is UNCHANGED, so no edits there.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — capture `present` BEFORE Rewrite mutates args. Rewrite(args,...) deletes the
  renamed + dropped aliases (rewrite.go). After it, args (and the re-serialized dec.body)
  contain ONLY the target param. Per-alias presence flags therefore MUST be read inside
  rewriteObject before `Rewrite(args, cfg.Aliases, cfg.TargetParam)` runs. Re-deriving
  from dec.body in the handler would report every alias as absent (silent false log).

CRITICAL — put BOTH new log.log calls in newProxyHandler, NOT in forward(). forward()'s
  body is DONE (T2.S2) and must stay byte-for-byte intact; the parallel T2.S2 work
  touched exactly that body. The handler closure (newProxyHandler) is untouched by
  T2.S2, so the rewrite line (after decideRewrite) and the forward line (after the
  forward() call) land in a conflict-free region. This also co-locates the two events
  next to the decision they observe.

CRITICAL — rewrite fires on the REQUEST decision, not the response. Emit `rewrite`
  when `!dec.streamThrough` (a tools/call arg was renamed), BEFORE forward(). Even if
  the upstream later returns non-2xx (upstream_error also fires) or the SSE result id
  doesn't match rewrittenIDs (Inject re-emits unchanged), the rewrite still happened
  and must be logged. Do NOT gate rewrite on the response.

CRITICAL — `mode` reflects the PATH decision (dec.streamThrough), not whether a
  particular SSE event matched. A rewrite whose upstream result id doesn't match still
  reports mode="injected" because forward() ran the Inject branch. This matches PRD §15
  "whether the response was streamed or injected" at the path level and is correct for a
  debug line. The transport-error path (client.Do err -> 502) reports mode based on
  dec.streamThrough too; that case is separately covered by the error/warn upstream_error
  line, so the slight imprecision is acceptable.

CRITICAL — level filtering is the LOGGER's job. The logger already drops messages whose
  rank < configured level (main.go: log method). Emit `rewrite` at "info" and `forward`
  at "debug"; do NOT add a second filter. The item MOCKING "info level suppresses debug
  forward lines" is satisfied for free — pin it with a test (info logger -> no forward
  line; debug logger -> forward line present).

CRITICAL — widen rewriteObject's return, do NOT add a second function. rewriteObject is
  the single place that extracts params/args and calls Rewrite. Returning a struct
  {changed,notes,tool,present} keeps one parse point. Both call sites (proxy.go:318 and
  :328) must switch from `if n := rewriteObject(...); n != nil` to
  `if rr := rewriteObject(...); rr.changed`. Verified: no *_test.go calls rewriteObject.

GOTCHA — `present` is keyed by EVERY configured alias (cfg.Aliases), each -> bool, so an
  operator sees which aliases were checked (not just the present ones). Build it with a
  loop over cfg.Aliases (config order), NOT a range over args (randomized map iteration).
  Mirror rewrite.go's "NEVER range over args" discipline.

GOTCHA — `tool` is params["name"] as a STRING only. params["name"] may be absent or
  non-string; in those cases tool = "" (the PRD says "tool (if present)"). Use a type
  assertion: `if name, ok := params["name"].(string); ok { tool = name }`.

GOTCHA — `method` is the JSON-RPC method (initialize/tools/call/tools/list), NOT the
  HTTP method (always POST). Capture it in decideRewrite alongside reqID
  (`if m, ok := v["method"].(string); ok { method = m }`). It is "" for scalars /
  unparseable bodies (the forward line then logs method:"" — acceptable).

GOTCHA — rewriteDecision is also constructed on the unparseable-JSON and scalar paths
  (early returns). Those return BEFORE method/tool/notes/present are known, so they
  stay zero-value (streamThrough=true -> no rewrite line; forward line logs method:""
  id:nil mode:"streamed"). Do not try to populate them there.

GOTCHA — do NOT redeclare captureProxy/fakeUpstream/testSID/initSSE/dcCfg/firstArgValue.
  They live in proxy_test.go. proxy_log_test.go is `package main`, so it sees them. Do
  NOT add `os` to proxy_log_test.go (T2.S2 adds `os` to proxy_test.go — a separate file,
  so no clash, but proxy_log_test.go doesn't need it anyway).

GOTCHA — json round-trip: map[string]bool marshals to {"k":true} and unmarshals into
  map[string]any with bool values; []string marshals to ["a",...] and unmarshals into
  []any. So in tests assert present["query"] == true (bool) and notes.([]any).
```

## Implementation Blueprint

### Data models and structure

Additive fields on `rewriteDecision` (proxy.go), a widened `rewriteObject` return
struct, and one log-field helper. No new production types beyond these.

```go
// (ADD to rewriteDecision — additive; existing fields untouched)
type rewriteDecision struct {
	body          []byte
	streamThrough bool
	rewrittenIDs  map[any]bool
	warning       string
	reqID         any
	// --- P1.M4.T3.S1 (observability, PRD §15) — additive, zero existing readers break ---
	// method is the JSON-RPC method (initialize/tools/call/...). Captured for the
	// `forward` debug line. "" for a scalar / unparseable body.
	method string
	// tool is params.name of the rewritten tools/call (the `rewrite` line's "tool"
	// field). "" when absent/non-string. Set on the rewrite path only.
	tool string
	// notes is the per-alias note array (the `rewrite` line's "notes" field). It is
	// the same slice warningText joins into `warning`. Set on the rewrite path only.
	notes []string
	// present is the per-configured-alias presence map (alias -> was-in-arguments),
	// captured BEFORE Rewrite mutated args. Set on the rewrite path only.
	present map[string]bool
}

// (WIDEN rewriteObject's return — the single parse point also yields tool + presence)
type rewriteObjectResult struct {
	changed bool
	notes   []string
	tool    string
	present map[string]bool // per-cfg.Aliases presence, captured PRE-mutation
}

// rewriteLogFields builds the `rewrite` event payload from a rewrite decision
// (PRD §15). It is pulled out so the handler reads as one log.log call and the
// field set is testable in isolation. NEVER include header values here — the
// rewrite event carries no headers, so Authorization can never appear (PRD §13).
// If a future field adds header context, route it through redactHeaders first.
func rewriteLogFields(dec rewriteDecision) map[string]any {
	return map[string]any{
		"req_id": dec.reqID, // float64|string|nil — json.Marshal renders nil as null
		"tool":   dec.tool,  // "" renders as "" (PRD: "tool (if present)")
		"notes":  dec.notes, // []string -> JSON array
		"present": dec.present, // map[string]bool -> {"alias":bool,...}
	}
}
```

No other new types. Imports: **none added** to `proxy.go` (`strings` already
imported by T2.S2; `log.log`/`map`/`bool`/`string`/`[]string` are builtin).

### Reference implementation (EDITS in `proxy.go`)

> Run `gofmt -w proxy.go` after. All edits are additive or a local signature widen.

**EDIT 1 — `rewriteObject` returns the struct** (replace the `func rewriteObject(...)
[]string` body; capture `present` + `tool` BEFORE `Rewrite`):

```go
// rewriteObject applies the alias rewrite to a single JSON-RPC object IN PLACE and
// returns what happened plus the observability fields the `rewrite` log needs
// (P1.M4.T3.S1, PRD §15). A non-tools/call method, or a tools/call with absent/
// non-object params.arguments, is left untouched and returns a zero-value result.
//
// PRE-MUTATION CAPTURE (P1.M4.T3.S1): `present` (per-configured-alias presence) and
// `tool` (params.name) are read from args/params BEFORE Rewrite mutates args —
// Rewrite deletes the renamed + dropped aliases, so presence cannot be recomputed
// from the re-serialized body afterwards.
func rewriteObject(obj map[string]any, cfg Config) rewriteObjectResult {
	var res rewriteObjectResult
	if obj["method"] != "tools/call" {
		return res
	}
	params, ok := obj["params"].(map[string]any)
	if !ok {
		return res
	}
	if name, ok := params["name"].(string); ok {
		res.tool = name
	}
	args, ok := params["arguments"].(map[string]any)
	if !ok {
		return res
	}
	// Capture presence BEFORE Rewrite mutates args (Rewrite deletes/renames aliases).
	res.present = aliasPresence(args, cfg.Aliases)
	if rr := Rewrite(args, cfg.Aliases, cfg.TargetParam); rr.Changed {
		res.changed = true
		res.notes = rr.Notes
	} else {
		// Nothing changed: drop the captured present/tool so an unchanged request
		// does not carry stale observability data (the rewrite line won't fire anyway).
		res.present = nil
		res.tool = ""
	}
	return res
}

// aliasPresence reports, for each configured alias (in config order), whether it
// was a key in args. Iterates cfg.Aliases (NOT args) so the output is stable and
// covers every alias — mirroring rewrite.go's "NEVER range over args" discipline
// (Go map iteration is randomized). Captured pre-mutation by rewriteObject.
func aliasPresence(args map[string]any, aliases []string) map[string]bool {
	p := make(map[string]bool, len(aliases))
	for _, a := range aliases {
		_, p[a] = args[a]
	}
	return p
}
```

**EDIT 2 — thread the new fields through `decideRewrite`** (the local `var`s + the
two call sites + both return paths). Capture `method` like `reqID` (first object's
method); take `tool`/`present`/`notes` from the first CHANGED object:

```go
// Inside decideRewrite, alongside `var reqID any` add:
var (
	reqID   any
	method  string
	tool    string
	present map[string]bool
)
// `addID` already joins notes; keep it. (notes is the local []string already declared.)

// single-object case (was: if n := rewriteObject(v, cfg); n != nil { addID(v["id"], n) }):
reqID = v["id"]
if m, ok := v["method"].(string); ok {
	method = m
}
if rr := rewriteObject(v, cfg); rr.changed {
	addID(v["id"], rr.notes)
	tool = rr.tool
	present = rr.present
}

// array/batch case (was: if n := rewriteObject(obj, cfg); n != nil { addID(obj["id"], n) }):
for _, elem := range v {
	if obj, ok := elem.(map[string]any); ok {
		if reqID == nil {
			reqID = obj["id"]
		}
		if method == "" {
			if m, ok := obj["method"].(string); ok {
				method = m
			}
		}
		if rr := rewriteObject(obj, cfg); rr.changed {
			addID(obj["id"], rr.notes)
			if tool == "" { // first changed object's tool/present win (batch is defensive)
				tool = rr.tool
				present = rr.present
			}
		}
	}
}

// unchanged-path return (was: return rewriteDecision{body: body, streamThrough: true, reqID: reqID}):
return rewriteDecision{body: body, streamThrough: true, reqID: reqID, method: method}

// rewrite-path return (was: return rewriteDecision{body: out, streamThrough: false,
//   rewrittenIDs: rewrittenIDs, warning: warningText(notes), reqID: reqID}):
return rewriteDecision{
	body:          out,
	streamThrough: false,
	rewrittenIDs:  rewrittenIDs,
	warning:       warningText(notes),
	reqID:         reqID,
	method:        method,
	tool:          tool,
	notes:         notes,    // the SAME slice warningText joined — authoritative
	present:       present,
}
```

> The two EARLY returns (unparseable JSON, scalar) stay as-is: they return before
> `method`/`tool`/`notes`/`present` are known, so those fields stay zero-value
> (streamThrough=true -> no rewrite line; the forward line logs method:"" id:nil).

**EDIT 3 — the two `log.log` calls in `newProxyHandler`** (signature UNCHANGED;
`forward()` untouched). Place the `rewrite` line right after `dec := decideRewrite`
and the `forward` line right after the `forward(...)` call:

```go
// In newProxyHandler, after:  dec := decideRewrite(body, cfg)
dec := decideRewrite(body, cfg)
// P1.M4.T3.S1 (PRD §15): log the rewrite whenever FR-2 changed a call. Fires on the
// REQUEST decision (before forward), so it logs even if the upstream later errors or
// the SSE result id doesn't match. Carries NO headers -> Authorization can't appear.
if !dec.streamThrough {
	log.log("info", "rewrite", rewriteLogFields(dec))
}
// ... (existing outReq construction + Accept fallback, UNCHANGED) ...
forward(client, rw, outReq, dec, log)
// P1.M4.T3.S1 (PRD §15): debug per-request forward line — method, id, and whether the
// response was streamed (io.Copy) or injected (SSE Inject). Suppressed below debug by
// the logger's level filter. mode reflects the path decision (dec.streamThrough).
log.log("debug", "forward", map[string]any{
	"method": dec.method,           // JSON-RPC method ("" for scalar/unparseable)
	"id":     dec.reqID,            // float64|string|nil
	"mode":   forwardMode(dec.streamThrough),
})
```

with the tiny helper (keeps the ternary out of the call site):

```go
// forwardMode renders the streamed-vs-injected label for the debug `forward` line
// (PRD §15). "streamed" when no argument was rewritten (forward io.Copy'd verbatim);
// "injected" when a tools/call argument was rewritten (forward ran sse.Inject).
func forwardMode(streamThrough bool) string {
	if streamThrough {
		return "streamed"
	}
	return "injected"
}
```

> NET: `forward()` is untouched. `newProxyHandler` gains two `log.log` calls + uses
> the two helpers. `rewriteDecision` gains four fields. `rewriteObject` widens to a
> struct + an `aliasPresence` helper. `decideRewrite` threads the new fields.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the on-disk state
  - RUN: grep -n "func forward\|StatusCode >= 300\|func rewriteObject\|type rewriteDecision\|func decideRewrite" proxy.go
  - EXPECT: forward()'s body has the `if dec.streamThrough` branch (T2.S2 DONE) AND the
        non-2xx `if resp.StatusCode >= 300` upstream_error log (T2.S1 DONE). rewriteObject
        returns []string (pre-edit). rewriteDecision has body/streamThrough/rewrittenIDs/
        warning/reqID only.
  - CONFIRM rewriteObject has NO test callers: grep -rn "rewriteObject" *_test.go (expect empty).

Task 1: ADD the four additive fields to rewriteDecision (proxy.go)
  - APPEND method/tool/notes/present to the struct (EDIT 1's struct block). Document each.
  - DO NOT reorder or rename the existing five fields.

Task 2: WIDEN rewriteObject + add aliasPresence (proxy.go)
  - REPLACE `func rewriteObject(obj, cfg) []string` with the struct-returning version
        (EDIT 1). ADD `aliasPresence(args, aliases)`. Capture present + tool BEFORE Rewrite.
  - On the unchanged branch, nil out present/tool (stale-data hygiene; the rewrite line
        won't fire but keep dec clean).

Task 3: THREAD the new fields through decideRewrite (proxy.go)
  - ADD the `var method/tool/present` locals; capture method like reqID (first object).
  - UPDATE both rewriteObject call sites (single-object line ~318, batch line ~328) to use
        `rr := rewriteObject(...); rr.changed`.
  - UPDATE both return paths (unchanged + rewrite) to populate method/tool/notes/present.
  - Leave the unparseable/scalar early returns as-is (zero-value fields).

Task 4: ADD the log helpers + the two log.log calls (proxy.go)
  - ADD rewriteLogFields(dec) and forwardMode(bool) helpers.
  - In newProxyHandler: emit `log.log("info","rewrite", rewriteLogFields(dec))` right after
        `dec := decideRewrite(body, cfg)`, gated on `!dec.streamThrough`.
  - Emit `log.log("debug","forward", {method,id,mode})` right after `forward(client, rw,
        outReq, dec, log)`.
  - DO NOT touch forward()'s body. DO NOT change newProxyHandler's signature.

Task 5: CREATE proxy_log_test.go (package main)
  - TestLog_RewriteEvent: aliased tools/call -> exactly one info `rewrite` line with
        req_id==2, tool=="web_search_prime", non-empty notes, present["query"]==true, NO
        Authorization field, NO "Bearer" substring, level=="info".
  - TestLog_NoRewriteWhenUnchanged: canonical tools/call -> NO `rewrite` line in buf.
  - TestLog_ForwardDebugEvent: (a) debug logger + aliased request -> forward line with
        method=="tools/call", id==2, mode=="injected"; (b) debug logger + canonical request
        -> mode=="streamed"; (c) INFO logger -> NO forward line (level filter).
  - ADD findMsg(buf, want) helper (decodes JSONL, returns first line whose msg==want).
  - Reuse captureProxy/fakeUpstream/testSID; do NOT redeclare. Imports: bytes/
        encoding/json/io/net/http/net/http/httptest/strings/testing. NO `os`.

Task 6: VALIDATE
  - gofmt -w proxy.go proxy_log_test.go
  - go vet ./...
  - go test -run 'TestLog_' -v                # new log tests
  - go test -run 'TestForward_Non2xx|TestForward_2xx' -v   # upstream_error verification (unchanged)
  - go test -run 'TestDecideRewrite|TestPassthrough|TestSSE' -v   # regressions
  - go test ./...                             # full suite green
  - git diff --stat  # expect ONLY proxy.go + proxy_log_test.go
  - git diff go.mod  # expect EMPTY
  - git diff proxy.go | grep -E '^\+.*"import"|^\+\s*"'  # expect NO new import line
```

### Test block (proxy_log_test.go — CREATE, package main)

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// findMsg decodes buf's JSONL and returns the first line whose "msg" == want, or
// fatals if absent. (P1.M4.T3.S1 log-event tests.)
func findMsg(t *testing.T, buf *bytes.Buffer, want string) map[string]any {
	t.Helper()
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m["msg"] == want {
			return m
		}
	}
	t.Fatalf("no %q line in log output:\n%s", want, buf.String())
	return nil
}

// (L1) REWRITE event: an aliased tools/call emits exactly ONE info-level "rewrite"
// JSON line carrying req_id, tool (params.name), a non-empty notes array, and a
// per-alias presence map — and NO Authorization field / no Bearer token (PRD §15/§13;
// item MOCKING "a rewrite produces a JSON line with notes and NO Authorization field").
func TestLog_RewriteEvent(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got) // returns initSSE; we assert on the LOG, not the body
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "info") // info keeps rewrite, drops debug forward
	defer proxy.Close()

	// Alias "query" -> decideRewrite renames to search_query (rewrittenIDs={float64(2)}).
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"name":"web_search_prime","arguments":{"query":"lunar rover"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	// Drain so the handler (and its log calls) fully complete.
	_, _ = io.ReadAll(resp.Body)

	rewrite := findMsg(t, &buf, "rewrite")
	// Exactly one rewrite line.
	count := bytes.Count(buf.Bytes(), []byte(`"msg":"rewrite"`))
	if count != 1 {
		t.Fatalf("rewrite line count = %d, want 1:\n%s", count, buf.String())
	}
	if rewrite["level"] != "info" {
		t.Errorf("rewrite level = %#v, want info", rewrite["level"])
	}
	if rewrite["req_id"] != float64(2) {
		t.Errorf("rewrite req_id = %#v, want float64(2)", rewrite["req_id"])
	}
	if rewrite["tool"] != "web_search_prime" {
		t.Errorf("rewrite tool = %#v, want web_search_prime (params.name)", rewrite["tool"])
	}
	notes, ok := rewrite["notes"].([]any)
	if !ok || len(notes) == 0 {
		t.Errorf("rewrite notes = %#v, want a non-empty array", rewrite["notes"])
	} else {
		// The renamed note mentions the alias + target.
		joined := strings.ToLower(fmt.Sprint(notes))
		if !strings.Contains(joined, "query") || !strings.Contains(joined, "search_query") {
			t.Errorf("rewrite notes missing the renamed->target fact: %v", notes)
		}
	}
	present, ok := rewrite["present"].(map[string]any)
	if !ok {
		t.Errorf("rewrite present = %#v, want a map", rewrite["present"])
	} else if present["query"] != true {
		t.Errorf("present[query] = %#v, want true (alias was present)", present["query"])
	}

	// SECURITY (PRD §6/§13): no Authorization field, no Bearer token anywhere.
	if _, ok := rewrite["Authorization"]; ok {
		t.Error("rewrite line carries an Authorization field (must never log secrets)")
	}
	if bytes.Contains(buf.Bytes(), []byte("Bearer")) {
		t.Errorf("a bearer token leaked into the log:\n%s", buf.String())
	}
}

// (L2) NO rewrite event when nothing changed: a canonical-param tools/call emits no
// rewrite line (PRD §15 "Logged whenever FR-2 changes a call" -> only on change).
func TestLog_NoRewriteWhenUnchanged(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "info")
	defer proxy.Close()

	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	if bytes.Contains(buf.Bytes(), []byte(`"msg":"rewrite"`)) {
		t.Errorf("rewrite emitted for an unchanged (canonical) request:\n%s", buf.String())
	}
}

// (L3) FORWARD debug event: at debug level every request gets a "forward" line with
// method, id, and mode; a rewrite -> "injected", a passthrough -> "streamed". At INFO
// the forward line is SUPPRESSED (PRD §15 "Levels honored"; item MOCKING "info level
// suppresses debug forward lines").
func TestLog_ForwardDebugEvent(t *testing.T) {
	var got http.Request
	up := fakeUpstream(t, &got)
	defer up.Close()

	// (a) injected mode: aliased tools/call at DEBUG.
	var buf bytes.Buffer
	proxy := captureProxy(t, up.URL, &buf, "debug")
	defer proxy.Close()
	resp, err := http.Post(proxy.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/call",`+
			`"params":{"arguments":{"query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)

	fwd := findMsg(t, &buf, "forward")
	if fwd["method"] != "tools/call" {
		t.Errorf("forward method = %#v, want tools/call", fwd["method"])
	}
	if fwd["id"] != float64(2) {
		t.Errorf("forward id = %#v, want float64(2)", fwd["id"])
	}
	if fwd["mode"] != "injected" {
		t.Errorf("forward mode = %#v, want injected (alias was rewritten)", fwd["mode"])
	}

	// (b) streamed mode: canonical tools/call at DEBUG.
	var buf2 bytes.Buffer
	proxy2 := captureProxy(t, up.URL, &buf2, "debug")
	defer proxy2.Close()
	resp2, err := http.Post(proxy2.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","id":7,"method":"tools/call",`+
			`"params":{"arguments":{"search_query":"x"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	_, _ = io.ReadAll(resp2.Body)
	fwd2 := findMsg(t, &buf2, "forward")
	if fwd2["mode"] != "streamed" {
		t.Errorf("forward mode = %#v, want streamed (no rewrite)", fwd2["mode"])
	}
	if fwd2["method"] != "tools/call" {
		t.Errorf("forward method = %#v, want tools/call", fwd2["method"])
	}

	// (c) LEVEL FILTER: at INFO the forward line is dropped (rewrite/upstream_error stay).
	var buf3 bytes.Buffer
	proxy3 := captureProxy(t, up.URL, &buf3, "info")
	defer proxy3.Close()
	resp3, err := http.Post(proxy3.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	_, _ = io.ReadAll(resp3.Body)
	if bytes.Contains(buf3.Bytes(), []byte(`"msg":"forward"`)) {
		t.Errorf("forward debug line emitted at info level (must be suppressed):\n%s", buf3.String())
	}
}
```

> NOTE: `TestLog_RewriteEvent` uses `fmt.Sprint(notes)` for a substring check — ADD
> `"fmt"` to proxy_log_test.go's import block (stdlib; not added by T2.S2 to a
> different file, so no clash). If you prefer to avoid `fmt`, replace with a loop:
> `for _, n := range notes { s, _ := n.(string); joined += strings.ToLower(s) }`.

### Implementation Patterns & Key Details

```go
// PATTERN: one log.log call per event, map[string]any fields. Every existing event
// (startup, upstream_error, shutdown) is a single log.log(level, msg, map). The two
// new events follow EXACTLY this pattern — no bespoke writer, no buffered emitter.

// PATTERN: additive decision fields. rewriteDecision gains fields that decideRewrite
// already computes but discards (notes) or can trivially capture (method/tool/present).
// Existing readers (forward, newProxyHandler, all tests) read only the original five,
// so the addition is non-breaking. This is how reqID was added in T2.S1 — same shape.

// PATTERN: single parse point. rewriteObject is the only function that extracts
// params/arguments and calls Rewrite. Widening its return to a struct keeps ONE place
// that understands the tools/call shape; the handler and decideRewrite consume the
// struct. Do not re-parse dec.body in the handler (aliases are gone post-rewrite).

// PATTERN: observe in the handler, mutate in forward. The two log calls live in
// newProxyHandler (the request orchestrator), next to the decision they observe.
// forward() owns response mechanics and stays log-edit-free (its only log is T2.S1's
// upstream_error, already shipped). Clean separation: decide -> observe -> forward.

// GOTCHA (restated): present MUST be captured before Rewrite. Rewrite deletes the
// renamed + dropped aliases; after it, args (and dec.body) hold only the target. A
// presence map built post-mutation would show every alias absent — a silent false log.

// GOTCHA (restated): rewrite fires on the request decision (!dec.streamThrough), NOT
// on the response. Emit it before forward(). Even a later non-2xx or an id-mismatched
// SSE result does not un-rewrite the request.

// GOTCHA (restated): level filtering is the logger's job. Emit rewrite=info,
// forward=debug; do not add a filter. The info-suppresses-debug requirement is met
// for free and pinned by TestLog_ForwardDebugEvent case (c).

// GOTCHA (restated): widen rewriteObject, don't duplicate it. Both call sites
// (proxy.go ~318 and ~328) switch to `rr := rewriteObject(...); rr.changed`.
```

### Integration Points

```yaml
FILES MODIFIED:
  - proxy.go  (MODIFY: +4 additive fields on rewriteDecision; widen rewriteObject to
                return rewriteObjectResult; + aliasPresence + rewriteLogFields +
                forwardMode helpers; thread method/tool/notes/present through
                decideRewrite's returns; +2 log.log calls in newProxyHandler [rewrite
                after decideRewrite, forward after forward()]. forward() body/signature
                UNCHANGED. NO new imports.)
FILES CREATED:
  - proxy_log_test.go  (package main: TestLog_RewriteEvent + TestLog_NoRewriteWhenUnchanged
                + TestLog_ForwardDebugEvent + findMsg. Imports bytes/encoding/json/fmt/io/
                net/http/net/http/httptest/strings/testing. Reuses captureProxy/fakeUpstream/testSID.)
FILES NOT TOUCHED (contract):
  - go.mod: zero new requires.
  - forward()'s BODY: untouched (T2.S2 DONE on disk).
  - main.go / config.go / doc.go / rewrite.go / sse.go: untouched.
  - proxy_test.go / logger_test.go / other *_test.go / testdata/*.sse: untouched
        (log tests live in the new proxy_log_test.go to avoid T2.S2's concurrent
        proxy_test.go appends).
CONSUMER SEAM (keep stable):
  - P1.M5.T1.S1/S2 (e2e harness): will assert on these log lines (e.g. rewrite has
        notes, forward has mode). The field names (req_id/tool/notes/present;
        method/id/mode) and levels (info/debug/warn) are the stable contract.
  - P1.M5.T3.S1 (README): documents the log levels/events to operators using the
        field set + level table this item ships.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w proxy.go proxy_log_test.go
go vet ./...
git diff --stat              # expect: proxy.go + proxy_log_test.go ONLY
git diff go.mod              # expect: EMPTY (zero new requires)
git diff proxy.go | grep -E '^\+\s*"bytes"|^\+\s*"strings"|^\+\s*"os"' || true
                             # expect: EMPTY (no new import added to proxy.go)

# Expected: gofmt clean; vet clean; only proxy.go + proxy_log_test.go changed;
# go.mod unchanged; proxy.go import block unchanged.
```

### Level 2: Unit Tests (Component Validation)

```bash
# New log-event tests.
go test -run 'TestLog_' -v

# MUST PASS (the ones that prove the contract):
#   TestLog_RewriteEvent           -> one info rewrite line: req_id/tool/notes/present,
#                                     NO Authorization, NO Bearer.
#   TestLog_NoRewriteWhenUnchanged -> canonical request emits NO rewrite line.
#   TestLog_ForwardDebugEvent      -> debug: forward line w/ method/id/mode (injected for
#                                     rewrite, streamed for passthrough); info: NO forward line.
# Expected: PASS, exit 0. If RewriteEvent lacks notes, decideRewrite didn't thread notes.
#   If present[query]!=true, presence was captured post-mutation (Rewrite already ran).
#   If ForwardDebugEvent mode is wrong, forwardMode/dec.streamThrough wiring is off.
#   If the info case emits a forward line, the level is wrong (must be "debug").

# upstream_error VERIFICATION (unchanged T2.S1 code; must stay green).
go test -run 'TestForward_Non2xx|TestForward_2xx' -v

# Regression: the decision/passthrough/sse suites MUST stay green (additive fields only).
go test -run 'TestDecideRewrite|TestPassthrough|TestSSE' -v
```

### Level 3: Integration Testing (System Validation)

```bash
go build ./...          # compiles with the new fields + log calls
go test ./...           # config/resolve/logger/health/proxy/rewrite/sse — ALL green
go doc . rewriteLogFields   # prints the field set + the "no headers" invariant (Mode A)
go doc . forwardMode        # prints the streamed-vs-injected semantics

# Expected: build clean; full suite green; go doc shows the helpers + the PRD §15 events.
# The proxy is now FULLY OBSERVABLE per PRD §15 (item OUTPUT: "the proxy is feature-complete").
```

### Level 4: Creative & Domain-Specific Validation

```bash
# (a) Pin "rewrite has notes + NO Authorization" (the item MOCKING security assertion).
go test -run TestLog_RewriteEvent -v

# (b) Pin level filtering: info suppresses the debug forward line.
go test -run TestLog_ForwardDebugEvent -v

# (c) Confirm upstream_error is the T2.S1 implementation, unchanged.
grep -n 'StatusCode >= 300' proxy.go   # expect: the non-2xx upstream_error log

# (d) Confirm forward()'s body was NOT edited by this item (diff hygiene).
git diff proxy.go | grep -E '^\+|^\-' | grep -i 'streamThrough\|Inject\|flushWriter' || \
   echo "forward() body untouched (good)"

# Expected: (a) and (b) PASS; (c) shows the >= 300 log; (d) shows no Inject/flushWriter
# lines changed (only the additive fields + helpers + 2 handler log calls).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `gofmt` clean; `go vet ./...` clean; `git diff --stat` shows
      ONLY `proxy.go` + `proxy_log_test.go`; `go.mod` unchanged; `proxy.go` import
      block unchanged.
- [ ] Level 2 passes: `go test -run 'TestLog_' -v` green.
- [ ] Level 2 verification: `go test -run 'TestForward_Non2xx|TestForward_2xx' -v`
      green (upstream_error untouched).
- [ ] Level 2 regression: `go test -run 'TestDecideRewrite|TestPassthrough|TestSSE' -v`
      green (additive fields break nothing).
- [ ] Level 3 passes: `go build ./...` and `go test ./...` green; `go doc` shows the
      helpers + PRD §15 event names.

### Feature Validation

- [ ] `rewriteDecision` carries additive `method`/`tool`/`notes`/`present`, populated
      by `decideRewrite` (notes/tool/present on the rewrite path only).
- [ ] `rewriteObject` returns `rewriteObjectResult{changed,notes,tool,present}`;
      `present` captured BEFORE `Rewrite`; `aliasPresence` loops `cfg.Aliases`.
- [ ] `newProxyHandler` emits the info `rewrite` line iff `!dec.streamThrough` (after
      `decideRewrite`) and the debug `forward` line after `forward()` returns.
- [ ] `rewrite` line has `req_id`, `tool`, non-empty `notes`, `present` map, and NO
      `Authorization`/`Bearer`.
- [ ] `forward` line has `method` (JSON-RPC), `id`, `mode` (`injected` for a rewrite,
      `streamed` otherwise); suppressed at info level.
- [ ] `upstream_error` is unchanged at `proxy.go:182` (`>= 300`, `{status, req_id}`).
- [ ] `forward()`'s body + signature unchanged; `newProxyHandler` signature unchanged.

### Code Quality Validation

- [ ] Additive fields only — no existing `rewriteDecision` reader breaks.
- [ ] `rewriteObject` widened in place (not duplicated); both call sites updated.
- [ ] Both log calls in `newProxyHandler` (NOT in `forward()`); one `log.log` per event.
- [ ] Tests in a NEW `proxy_log_test.go` (no collision with T2.S2's `proxy_test.go`);
      reuse `captureProxy`/`fakeUpstream`/`testSID`; no redeclared helpers.
- [ ] No new `proxy.go` imports; `proxy_log_test.go` imports only stdlib.
- [ ] Anti-patterns avoided (see below).

### Documentation & Deployment

- [ ] Mode A docs honored: `rewriteLogFields` + the two `log.log` calls carry doc
      comments naming the PRD §15 events + the "never log Authorization" invariant.
      The README sweep (P1.M5.T3.S1) documents log levels/events to operators.
- [ ] No new env vars / config keys / routes. `go.mod` unchanged.

---

## Anti-Patterns to Avoid

- ❌ Don't emit `rewrite`/`forward` inside `forward()`. `forward()`'s body is DONE
  (T2.S2) and the parallel T2.S2 work touched exactly that body. Both new log calls
  go in `newProxyHandler` (the handler closure), which T2.S2 never touches. This also
  co-locates the two events next to the decision they observe.
- ❌ Don't re-derive `present` from `dec.body`. `Rewrite` deletes the renamed + dropped
  aliases, so the re-serialized body contains ONLY the target. A presence map built
  post-mutation shows every alias absent. Capture `present` inside `rewriteObject`
  BEFORE `Rewrite(args, ...)`.
- ❌ Don't gate `rewrite` on the response. Emit it when `!dec.streamThrough` (the
  REQUEST was rewritten), before `forward()`. A later non-2xx or an unmatched SSE
  result id does not un-rewrite the request — the event must still fire.
- ❌ Don't add a second level filter. The logger already drops below-level messages
  (`levelNum(level) < l.level`). Emit `rewrite` at "info" and `forward` at "debug";
  the "info suppresses debug forward" requirement is met for free. Adding `if
  cfg.LogLevel == "debug"` in the handler would double-filter and is wrong.
- ❌ Don't duplicate `rewriteObject`. It is the single tools/call parse point. Widen
  its return to a struct so tool/presence/notes all come from one place. Do not write
  a second "extract tool + presence" helper that re-parses params.
- ❌ Don't redeclare `captureProxy`/`fakeUpstream`/`testSID`/`initSSE`/`dcCfg`. They
  live in `proxy_test.go`; `proxy_log_test.go` is `package main` and sees them.
- ❌ Don't append the log tests to `proxy_test.go`. T2.S2 appends to that file
  concurrently. Use the new `proxy_log_test.go` to avoid an append collision.
- ❌ Don't log header values in `rewrite`/`forward`. These events carry no headers, so
  Authorization cannot appear. If a future field needs header context, route it
  through `redactHeaders` (PRD §13) — never log `Authorization`/`Cookie`/`Set-Cookie`/
  `Proxy-Authorization` verbatim.
- ❌ Don't change the `upstream_error` threshold. `>= 300` (non-2xx) is PRD-§15-
  correct. The item's ">= 400" phrasing is an alternative; "non-2xx per PRD" wins.
  T2.S1 already shipped it correctly — verify, don't touch.
