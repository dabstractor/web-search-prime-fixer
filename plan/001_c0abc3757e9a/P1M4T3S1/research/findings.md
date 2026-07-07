# P1.M4.T3.S1 — Research Findings (rewrite / upstream_error / forward log events)

## 1. On-disk state (verified 2026-07-07)

- `proxy.go` is **post-T2.S2**: `forward()` already has the conditional
  `if dec.streamThrough { io.Copy } else { Inject }` branch, the `flushWriter`
  wrapper, the SSE-flush gating, and the updated doc comment. The
  `var w io.Writer = rw` / `strings.Contains(...,"text/event-stream")` block is
  present. **`forward()`'s body is DONE — do NOT edit it.**
- `upstream_error` is **DONE (T2.S1)**: `proxy.go:182` `if resp.StatusCode >= 300`
  → `log.log("warn","upstream_error",{"status":..., "req_id":dec.reqID})`.
  `>= 300` = non-2xx, which is PRD-§15-correct ("on non-2xx upstream responses").
  The item's ">= 400" phrasing is an alternative; "non-2xx per PRD" is authoritative.
  → **No code change needed for upstream_error; verify only.**
- `proxy.go` imports already include `strings` (T2.S2 added it). **My work adds NO
  new import to `proxy.go`.**
- `rewriteDecision` (proxy.go:234) carries: `body []byte`, `streamThrough bool`,
  `rewrittenIDs map[any]bool`, `warning string`, `reqID any`. It does **NOT** carry
  `notes`, `tool`, per-alias presence, or JSON-RPC `method`. The T3.S1 contract
  *assumes* the decision carries `notes` — reality: only `warning` (= `warningText(notes)`)
  is there. → These are **additive fields** (see §3).

## 2. The logger (main.go, P1.M1.T3.S1)

- `type logger struct{ w io.Writer; level int }`; `(l *logger).log(level, msg string,
  fields map[string]any)` writes ONE JSON line `{"ts":RFC3339,"level","msg",...fields}`.
- Level filtering: `levelDebug(0) < levelInfo(1) < levelWarn(2) < levelError(3)`.
  A message below the configured level is dropped silently. → a logger at "info"
  DROPS "debug" (the `forward` line); at "debug" it emits it. **Filtering is already
  implemented by the logger** — just emit at the right level.
- `json.Marshal(map[string]any)` handles nested `map[string]bool` → `{"k":true}` and
  `[]string` → `["a","b"]`, and `any` (float64/string/nil) for ids. nil map/slice →
  JSON null (acceptable; the rewrite event is only emitted on the non-nil path).
- `redactHeaders(h http.Header) map[string]any` (main.go) replaces
  Authorization/Cookie/Set-Cookie/Proxy-Authorization with `<redacted>`. The
  `rewrite`/`forward` events log **no headers**, so redactHeaders is not needed
  there — but the invariant "NEVER log Authorization" holds trivially.

## 3. What must change (additive, conflict-free)

### 3a. `rewriteDecision` gains 4 additive fields (no existing reader breaks)
- `method string`  — JSON-RPC method (for the `forward` debug line); "" for scalars/unparseable.
- `tool string`    — `params.name` of the rewritten tools/call (for the `rewrite` line).
- `notes []string` — the per-alias notes (currently discarded after `warningText`).
- `present map[string]bool` — per-**configured-alias** presence captured BEFORE Rewrite
  mutates args (Rewrite deletes/renames aliases, so presence CANNOT be re-derived
  from `dec.body` post-rewrite — **must be captured at decision time**).

### 3b. `rewriteObject` signature change (safe: only 2 callers, both in decideRewrite; zero test callers)
- Currently `func rewriteObject(obj, cfg) []string`. Change to return a struct
  `{changed bool; notes []string; tool string; present map[string]bool}` so the
  single parse point also yields tool + presence. Verified: `grep -rn rewriteObject`
  shows callers only at proxy.go:318,328 (inside decideRewrite); no `*_test.go` calls it.

### 3c. New log emissions in `newProxyHandler` (the handler closure — NOT `forward`)
- **`rewrite`** (info) — right after `dec := decideRewrite(body, cfg)`, gated on
  `!dec.streamThrough`. Fields: `req_id`, `tool`, `notes`, `present`.
- **`forward`** (debug) — right after `forward(client, rw, outReq, dec, log)` returns.
  Fields: `method`, `id`, `mode` (`"streamed"` if dec.streamThrough else `"injected"`).
- **Why the handler, not forward():** keeps both new events together near the
  decision; `forward()`'s body is DONE (T2.S2) and must stay untouched; placing the
  `forward` debug line in the handler avoids any risk of colliding with T2.S2 edits.

## 4. Test harness reuse + parallel-safety
- `captureProxy(t, upstreamURL, buf, level)` (proxy_test.go, T2.S1) builds a proxy
  whose logger writes to `buf` at `level` — exactly the log-capture harness needed.
- `fakeUpstream(t, &got)`, `testSID`, `initSSE`, `dcCfg` all exist to reuse.
- **T2.S2 APPENDS to `proxy_test.go`** (TestForward_*, TestFlushWriter_*, helpers
  toolsCallSSE/jsonToContent0Text/recordingFlusher). To avoid a same-file append
  collision with the parallel T2.S2 work, put the log tests in a **NEW file
  `proxy_log_test.go` (package main)**. Go compiles all `*_test.go` in package main
  together; logger_test.go already proves separate test files are the convention.
  The new file needs only stdlib already-imported-everywhere imports
  (bytes/encoding/json/io/net/http/net/http/httptest/strings/testing) — **add NO
  import that T2.S2 also adds** (notably not `os`), so even a shared import block
  cannot clash.

## 5. Field/level decisions (grounded in PRD §15)
| event          | level | when                              | fields                          |
|----------------|-------|-----------------------------------|---------------------------------|
| startup        | info  | boot (DONE, main.go)              | aliases, listen, upstream, log_level |
| rewrite        | info  | `!dec.streamThrough` (NEW)        | req_id, tool, notes, present    |
| upstream_error | warn  | `resp.StatusCode >= 300` (DONE)   | status, req_id                  |
| forward        | debug | every request (NEW)               | method, id, mode(streamed/injected) |
| shutdown       | info  | signal (DONE, main.go)            | signal                          |

- `rewrite` = info (it is a real semantic event; survives at info; the item says
  "info level suppresses debug forward lines" ⇒ rewrite is NOT debug).
- `forward` = debug (per PRD §15 "debug adds per-request forward lines").
- `mode` reflects the **path decision** (`dec.streamThrough`), which is exactly what
  `forward()` uses. Minor imprecision on the transport-error path (a 502) is already
  covered by the `upstream_error` line — acceptable for a debug line.

## 6. "No secrets" MOCKING requirement
- The item: "assert a rewrite produces a JSON line with notes and NO Authorization
  field." Since the rewrite event logs no headers, Authorization never appears — but
  the test PINS it (regression guard) and also asserts no `Bearer` token substring
  leaks. redactHeaders is the tool to use IF any future field adds header context.
