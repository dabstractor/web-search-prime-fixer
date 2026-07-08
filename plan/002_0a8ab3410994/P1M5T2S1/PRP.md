name: "P1.M5.T2.S1 — Register tools via AddTool + wire the extract→delegate→teach dispatch handler (PRD §5.2, §11.3)"
description: |

  OWN and VERIFY the pipeline wiring of the v2.0 normalizing MCP server. THREE files:
  CREATE `server.go` (the dispatch handler + registration + the upstream constructor),
  MODIFY `main.go` (construct the UpstreamClient + call registerTools after NewServer),
  CREATE `dispatch_test.go` (focused handler-level tests reusing the newFakeZAI harness).

  THE DISPATCH HANDLER (the heart of this item). One `mcp.ToolHandler` is built by
  `makeDispatchHandler(cfg, upstream, log)` and registered for EVERY advertised tool
  (all tools share it). For each inbound `tools/call` it runs, in order (the item
  contract steps 1-8):
    (1) read `calledTool := req.Params.Name`, `rawArgs := req.Params.Arguments`.
    (2) `result := extract(rawArgs, cfg.QueryAliases, cfg.OptionalAliases)` (FR-4).
    (3) log "delegate" {called_tool, source, canonical(bool), optionals(names)}.
    (4) if `!result.Found`: log "extract_failed", return
        `noQueryResult(noQueryWarningText(...))` — NO upstream call, IsError stays false.
    (5) else `upstreamResult, err := upstream.callTool(ctx, result.ToUpstreamArgs(cfg.TargetParam))`
        — auth rides via ctx (authHeaderKey from main.go middleware); FR-5.
    (6) if err: log "upstream_error", return `(nil, err)` HONESTLY (SDK wraps as JSON-RPC).
    (7) if `shouldWarn(calledTool, result, CanonicalTool, CanonicalParam)`:
        `appendWarning(upstreamResult, warningText(...))` — warning AFTER results (FR-6).
    (8) return upstreamResult. IsError is NEVER set for any normalization/warning case.

  INVARIANTS this handler owns (PRD §11.3, §12, FR-6): results LEAD, warning TRAILS
  (so the model does not retry); the no-query case returns the warning with NO results
  and NO upstream call; `isError` is NEVER set (we build results by hand, never SetError);
  only `search_query` (+ normalized optionals) reaches z.ai (ToUpstreamArgs drops junk).

  SCOPE: CREATE server.go (`newUpstreamClient`, `registerTools`, `makeDispatchHandler`
  + a `sortedKeys` helper); MODIFY main.go (2-line wiring after NewServer — no new
  imports); CREATE dispatch_test.go (5-6 handler-level tests). ZERO edits to extract.go,
  teach.go, upstream.go, config.go, logger.go, health.go, tools.go (the parallel
  P1.M5.T1.S1 deliverable), or any other file. go.mod/go.sum unchanged. Mode A: doc
  comments on registerTools/makeDispatchHandler documenting the pipeline order, the
  no-results exception, the append-after ordering, and the IsError-never-set invariant.

---

## Goal

**Feature Goal**: A single `mcp.ToolHandler` (built by `makeDispatchHandler`) implements
  the full `extract → delegate → teach` pipeline per PRD §5.2/§11.3/FR-4/5/6, registered
  for every advertised tool via `registerTools(server, cfg, upstream, log)` which loops
  `buildTools(cfg)` calling `server.AddTool(tool, handler)` with ONE shared handler.
  main.go constructs the `UpstreamClient` and calls `registerTools` right after
  `mcp.NewServer`, so the running server actually answers `tools/call` end to end.

**Deliverable**:
  1. **CREATE** `server.go` (package main): `func newUpstreamClient(cfg Config, log *logger)
     *UpstreamClient`; `func registerTools(server *mcp.Server, cfg Config, upstream
     *UpstreamClient, log *logger)`; `func makeDispatchHandler(cfg Config, upstream
     *UpstreamClient, log *logger) mcp.ToolHandler`; private helper `sortedKeys`. Imports:
     `context`, `sort`, `github.com/modelcontextprotocol/go-sdk/mcp`.
  2. **MODIFY** `main.go`: after `server := mcp.NewServer(...)`, add
     `upstream := newUpstreamClient(cfg, log)` + `registerTools(server, cfg, upstream, log)`.
     NO new imports (both are same-package).
  3. **CREATE** `dispatch_test.go` (package main): handler-level tests reusing
     `newFakeZAI` from upstream_test.go — canonical(no warning), alias+junk(warning after),
     bare-string(extracted+warned), empty{}(no upstream, immediate warning, IsError false),
     upstream error(returned honestly), and the IsError-never-set invariant.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
  `go test ./...` all clean. With the default config, `makeDispatchHandler(cfg, upstream,
  log)` on `{"query":"x"}` returns z.ai's result with NO appended warning and IsError
  false; on `{"q":"x","junk":1}` returns z.ai's result THEN a warning block (IsError
  false); on `{}` makes NO upstream call and returns the immediate no-query warning
  (IsError false); on an upstream error returns `(nil, err)`. `registerTools` registers
  exactly `buildTools(cfg)` (one tool by default) without panicking. main.go boots and the
  server answers `tools/call` (the pipeline is wired). No file outside server.go /
  main.go / dispatch_test.go is created or edited.

## Hard Prerequisites

1. **buildTools exists** (P1.M5.T1.S1, parallel — its PRP is the contract): `func
   buildTools(cfg Config) []*mcp.Tool` returns AddTool-safe tools (valid object schemas).
   This task CONSUMES it in the registration loop and does NOT redefine it. If tools.go is
   absent at implementation time, STOP (prerequisite not met). Verify: `grep -q "func
   buildTools" tools.go`.
2. **extract / teach / upstream / config / logger are DONE** (read on disk — signatures
   in research/findings.md §1, all VERIFIED): `extract(raw, queryAliases, optionalAliases)
   ExtractionResult`; `ExtractionResult.{Query,Source,Optionals,Found}`; `(ExtractionResult)
   ToUpstreamArgs(targetParam) map[string]any`; `shouldWarn(calledTool, result,
   canonicalTool, canonicalParam) bool`; `warningText(calledTool, source, canonicalTool,
   canonicalParam) string`; `noQueryWarningText(canonicalTool, canonicalParam) string`;
   `appendWarning(*mcp.CallToolResult, text)`; `noQueryResult(text) *mcp.CallToolResult`;
   `(u *UpstreamClient) callTool(ctx, args) (*mcp.CallToolResult, error)`;
   `(l *logger) log(level, msg, fields)`.
3. **main.go already calls mcp.NewServer** (P1.M1.T2.S2 — DONE): `server := mcp.NewServer(
   &mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)`. This task
   inserts the wiring immediately AFTER that line. `version` is the health.go package var.
   `authMiddleware` (main.go) stores the inbound Authorization under `authHeaderKey{}`;
   `authInjector.RoundTrip` (upstream.go) reads it via `authFromContext` — so the handler
   only forwards `ctx`; auth is NOT passed explicitly.
4. **The newFakeZAI harness exists** (upstream_test.go — DONE): `newFakeZAI(t) (*httptest.
   Server, *fakeState)` stands up a real MCP "fake-zai" over httptest advertising
   `web_search_prime`, recording `st.lastTool/lastArgs/lastAuth/calls`, returning a canned
   `Content[0].Text = `[{"title":"r","url":"u","content":"c"}]``, with `st.setToolErr(err)`
   + `st.expire()`. dispatch_test.go REUSES it (same package) — do not redefine it.
5. **CallToolRequest is directly constructible** (VERIFIED, research §3): `&mcp.
   CallToolRequest{Params: &mcp.CallToolParamsRaw{Name: ..., Arguments: json.RawMessage(...)}}`
   with nil Session/Extra — the handler reads ONLY Params. So makeDispatchHandler is
   unit-testable without SDK server routing.

## User Persona

**Target User**: (1) **P1.M5.T3.S1** (server_test.go e2e) — its §19.3 cases drive the
   REGISTERED server end to end (tools/list, session-expiry); this item gives it a server
   with tools + the handler wired so its cases 1-4 (canonical/alias/bare/empty) actually
   traverse the pipeline. (2) **main.go / the running server** — without registerTools the
   server advertises no tools and cannot answer tools/call. (3) **P1.M5.T4.S1** (README) —
   documents "the server normalizes then delegates then teaches"; this handler IS that flow.

**Use Case**: An agent calls `web_search` with `{"query":"rust async runtime"}` (canonical)
   → the handler extracts query, delegates to z.ai `web_search_prime`, returns the result
   with no warning. The same agent calls `web_search` with `{"q":"x","junk":1}` → the
   handler extracts from the alias, drops junk, delegates, and appends a warning teaching
   the canonical form AFTER the results. A garbled call with `{}` → the immediate no-query
   warning, no upstream call.

**Pain Points Addressed**: PRD §1 (the layer-2 failure — wrong param/schema) is fixed by
   extract+delegate; PRD §12 (teaching without inducing retry) is fixed by append-AFTER;
   FR-6 (never isError, never results-without-warning-except-no-query) is the handler's
   core invariant.

## Why

- Implements **PRD §5.2 Part B** (the normalizing server's tools/call row: extract →
  delegate to z.ai → return result, append warning if non-canonical) and **§11.3** (result
  handling: append warning after content, preserve order, never isError, skip on
  extract-failure).
- Implements **FR-4** (extract from any input — via `extract`), **FR-5** (delegate to z.ai
  — via `upstream.callTool`), **FR-6** (teaching signal appended after results, the
  no-results exception, isError never set).
- Implements **PRD §15 logging**: the `delegate`, `extract_failed`, and `upstream_error`
  events emitted at the right pipeline stages.
- **Completes the M5.T2 milestone** (server dispatch wiring): the single subtask. After it,
  M5.T3 can run the e2e suite against a server that actually handles tools/call, and M5.T4
  can document the wired behavior.

## What

One new file (server.go), one modified file (main.go), one new test file. Visible behavior:
after this item the server answers `tools/call` through the full pipeline (it previously
registered no tools). The five handler-level behaviors (canonical, alias+junk, bare-string,
empty, upstream-error) are pinned by dispatch_test.go.

### Success Criteria

- [ ] `server.go` exists, `package main`, imports only `context`, `sort`,
      `github.com/modelcontextprotocol/go-sdk/mcp`.
- [ ] `func newUpstreamClient(cfg Config, log *logger) *UpstreamClient` returns
      `&UpstreamClient{upstream: cfg.Upstream, targetTool: cfg.TargetTool, targetParam:
      cfg.TargetParam, log: log}` (mirrors the upstream_test.go struct-literal pattern; no
      credential field).
- [ ] `func registerTools(server *mcp.Server, cfg Config, upstream *UpstreamClient, log
      *logger)` builds ONE handler via makeDispatchHandler and calls `server.AddTool(tool,
      handler)` for every `tool` in `buildTools(cfg)` (all tools share the SAME handler).
- [ ] `func makeDispatchHandler(cfg Config, upstream *UpstreamClient, log *logger)
      mcp.ToolHandler` returns a closure executing steps 1-8 (extract → log delegate →
      no-query-or-delegate → teach).
- [ ] Canonical `{"query":"x"}` → upstream receives `search_query=="x"`; result has 1
      content block (no warning); IsError false.
- [ ] Alias `{"q":"x","junk":1}` → upstream receives `search_query=="x"` (junk dropped);
      result has 2 blocks (result THEN warning); warning text from warningText; IsError false.
- [ ] Bare-string `"x"` → extracted (Source="bare-string"), forwarded, result + warning.
- [ ] Empty `{}` → NO upstream call (fake.calls==0); result is the no-query warning (1
      block); IsError false.
- [ ] Upstream error → handler returns `(nil, err)` (no synthesized result).
- [ ] IsError is NEVER true for any path (canonical/alias/bare/empty/error-return).
- [ ] main.go calls `newUpstreamClient(cfg, log)` + `registerTools(server, cfg, upstream,
      log)` immediately after `mcp.NewServer(...)`; no new imports.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat` shows server.go (new), dispatch_test.go (new), main.go (modified)
      and NOTHING else; `git diff go.mod` empty.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because: (a)
the EXACT signatures of every consumed function (extract/teach/upstream/logger/config) are
given with file citations AND verified on disk (research §1); (b) the full reference
implementation for server.go (newUpstreamClient/registerTools/makeDispatchHandler/sortedKeys)
is given verbatim; (c) the EXACT main.go edit (oldText→newText) is given; (d) the SDK
AddTool/ToolHandler/CallToolRequest surface is verified (research §2) and the test seam
(direct CallToolRequest construction) is proven (research §3); (e) the log-event design is
specified field-by-field with the double-log reconciliation explained (research §4); (f)
the full dispatch_test.go (reusing newFakeZAI) is given; (g) the IsError-never-set and
append-after-results invariants are justified against the teach.go functions that enforce
them.

### Documentation & References

```yaml
# MUST READ — the pipeline this handler implements.
- file: PRD.md
  section: "§5.2 Part B (tools/call row)" + "§11.3 Result handling"
  why: §5.2 = extract → delegate to z.ai → return result, append warning if non-canonical.
        §11.3 = append warning AFTER content, preserve order, never isError, skip on
        extract-failure. The 8 handler steps map 1:1 to these.
  critical: the warning is APPENDED AFTER results (FR-6) — never returned alone when a
        search could run; the ONLY no-results case is extract-failure (§10.1.5).

# MUST READ — the teaching-signal rules.
- file: PRD.md
  section: "§12.1 When a warning is added" + "§12.2 Warning text" + "FR-6"
  why: §12.1 = no warning iff canonical (tool==web_search AND source==query AND no
        optionals); warning-after-results otherwise; immediate no-results warning on
        extract-failure. FR-6 = isError NEVER set for any normalization/warning case.
  critical: shouldWarn (teach.go) already encodes §12.1 exactly; the handler just calls it
        and appends. noQueryResult (teach.go) already builds the IsError=false no-results
        result. The handler must NEVER call SetError.

# MUST READ — the log events the handler emits.
- file: PRD.md
  section: "§15 Logging"
  why: delegate (called_tool, source, canonical, optionals), extract_failed (no upstream
        call), upstream_error (non-2xx/expiry). Levels honored; debug adds raw args shape.
  critical: the handler emits delegate per tools/call (step 3, before the Found check),
        extract_failed only when !Found (step 4), upstream_error when callTool errs (step 6).
        See research §4 for the (intentional, complementary) co-fire with callTool's
        session-lifecycle upstream_error logs — do NOT suppress the handler's log.

# MUST READ — the consumed signatures (VERIFIED on disk).
- file: extract.go
  section: "extract + ExtractionResult + ToUpstreamArgs"
  why: extract(raw, queryAliases, optionalAliases) → ExtractionResult{Query,Source,Ambiguous,
        Optionals,Found}. ToUpstreamArgs(targetParam) → {targetParam: Query, ...optionals}
        (drops aliases/junk → z.ai gets schema-valid input). Missing args → zero-length
        RawMessage → Found=false.
- file: teach.go
  section: "shouldWarn + warningText + noQueryWarningText + appendWarning + noQueryResult"
  why: shouldWarn(calledTool, result, canonicalTool, canonicalParam) — false ONLY for
        canonical. warningText/noQueryWarningText are byte-fixed. appendWarning appends a
        TextContent LAST (never IsError). noQueryResult builds the fresh IsError=false result.
- file: upstream.go
  section: "UpstreamClient + callTool"
  why: callTool(ctx, args) → (*mcp.CallToolResult, error); lazy session, re-init-on-expiry,
        returns z.ai's result UNCHANGED on success, (nil, err) on failure (never synthesizes).
        Auth rides via ctx (authHeaderKey) — the handler just forwards ctx.
  critical: there is NO newUpstreamClient constructor; tests use &UpstreamClient{...}. This
        task ADDS newUpstreamClient to server.go for main.go's use (it does NOT edit upstream.go).

# MUST READ — the consumer registration seam (parallel P1.M5.T1.S1).
- file: plan/002_0a8ab3410994/P1M5T1S1/PRP.md
  section: "buildTools contract + the registration loop"
  why: buildTools(cfg) []*mcp.Tool returns AddTool-safe tools; registerTools does
        `for _, t := range buildTools(cfg) { server.AddTool(t, handler) }` with ONE handler.
  critical: registerTools trusts buildTools' schemas (AddTool panics on bad ones — that is
        buildTools' contract, pinned by its TestBuildTools_AddToolSafe).

# MUST READ — the verified SDK surface.
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§1 SERVER SIDE (AddTool, ToolHandler, CallToolRequest)" + "§3 CallToolResult"
  why: AddTool(t, h); ToolHandler func(ctx, *CallToolRequest)(*CallToolResult,error);
        req.Params.{Name, Arguments(json.RawMessage)}; CallToolResult.{Content, IsError}.
  critical: NEVER call result.SetError (sets IsError=true + overwrites Content). Build
        results via teach.noQueryResult / appendWarning only.

# MUST READ — the reusable test harness.
- file: upstream_test.go
  section: "newFakeZAI + fakeState"
  why: newFakeZAI(t) (*httptest.Server, *fakeState) — real fake z.ai over httptest;
        st.lastArgs/lastTool/lastAuth/calls; st.setToolErr(err); st.expire(). Canned result
        Content[0].Text = `[{"title":"r","url":"u","content":"c"}]`.
  critical: REUSE it (same package) — do NOT redefine newFakeZAI/fakeState in dispatch_test.go
        (compile error: redeclared). Read st fields UNDER st.mu to pass `go test -race`.
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod / go.sum    # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  main.go            # authHeaderKey + authMiddleware + NewServer + StreamableHTTPHandler mount
                     #   (P1.M1.T2.S2) — MODIFIED by this item (insert wiring after NewServer)
  config.go          # Config + DefaultConfig + ResolveConfig (v2 fields) — UNTOUCHED (consumed)
  extract.go         # extract + ExtractionResult + ToUpstreamArgs (P1.M2.T1) — UNTOUCHED (called)
  teach.go           # shouldWarn + warningText + noQueryWarningText + appendWarning + noQueryResult
                     #   (P1.M3.T1) — UNTOUCHED (called)
  upstream.go        # UpstreamClient + callTool + authInjector + authFromContext (P1.M4.T1)
                     #   — UNTOUCHED (called; newUpstreamClient added to server.go, NOT here)
  logger.go          # *logger + log(level,msg,fields) — UNTOUCHED (called)
  health.go          # healthHandler + var version — UNTOUCHED
  tools.go           # buildTools (P1.M5.T1.S1, parallel) — UNTOUCHED (consumed by registerTools)
  *_test.go (config/resolve/logger/health/extract/teach/upstream + tools_test) — UNTOUCHED
                     #   upstream_test.go ships newFakeZAI/fakeState — REUSED by dispatch_test.go
  testdata/*.sse, README.md, config.example.json, PRD.md, doc.go — UNTOUCHED
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
server.go          # CREATE (package main): the dispatch wiring.
                   #   - newUpstreamClient(cfg, log) *UpstreamClient — the constructor main.go uses
                   #     (mirrors upstream_test.go's struct literal; no credential field).
                   #   - registerTools(server, cfg, upstream, log) — loops buildTools(cfg), AddTool
                   #     each with ONE shared handler.
                   #   - makeDispatchHandler(cfg, upstream, log) mcp.ToolHandler — the 8-step
                   #     extract→delegate→teach closure (the heart of this item).
                   #   - sortedKeys(map) []string — deterministic optionals list for the delegate log.
                   #   Imports: context, sort, github.com/modelcontextprotocol/go-sdk/mcp.
dispatch_test.go   # CREATE (package main): handler-level tests reusing newFakeZAI.
                   #   - TestDispatch_Canonical_NoWarning
                   #   - TestDispatch_AliasJunk_WarningAppendedAfter
                   #   - TestDispatch_BareString_ExtractedAndWarned
                   #   - TestDispatch_Empty_NoUpstreamImmediateWarning
                   #   - TestDispatch_UpstreamError_ReturnedHonestly
                   #   - TestDispatch_IsErrorNeverSet (sweep across shapes)
                   #   + helpers newDispatchHandler/callDispatch/lastArg/callCount.
                   #   Imports: bytes, context, encoding/json, io, strings, sync/atomic, testing,
                   #             time, github.com/modelcontextprotocol/go-sdk/mcp.
main.go            # MODIFY: insert `upstream := newUpstreamClient(cfg, log)` +
                   #   `registerTools(server, cfg, upstream, log)` after `mcp.NewServer(...)`.
                   #   NO new imports (same-package calls).
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — IsError is NEVER set by this handler (FR-6/PRD §12.2). Build results ONLY via
  teach.noQueryResult (fresh, IsError=false) and teach.appendWarning (append-only, never
  IsError), and return upstream.callTool's result UNCHANGED on success. NEVER call
  result.SetError (it sets IsError=true AND overwrites Content when empty). The
  TestDispatch_IsErrorNeverSet sweep pins this across every shape.

CRITICAL — the warning is APPENDED AFTER results, never instead of them (FR-6/§11.3). The
  only path that returns a warning WITHOUT results is the extract-failure path (no-query),
  which makes NO upstream call. For every Found result, results are returned first and the
  warning (if any) trails — so the model acts on results, not a lone warning that tempts a
  retry. appendWarning (teach.go) enforces append-LAST; do not prepend or reorder.

CRITICAL — auth is NOT passed explicitly to callTool. The handler forwards `ctx` (the
  SDK-provided request context), which already carries authHeaderKey{} from main.go's
  authMiddleware. upstream.authInjector reads it via authFromContext inside RoundTrip. So
  step 5 is literally `upstream.callTool(ctx, result.ToUpstreamArgs(cfg.TargetParam))` — no
  auth argument. Do NOT add one.

CRITICAL — registerTools builds ONE handler and shares it across all tools (the item
  contract: "all tools share the SAME handler"). Do NOT build a per-tool handler. The
  handler keys behavior off req.Params.Name (the ACTUAL called tool) vs cfg.CanonicalTool,
  independent of which tool entry routed it — so one handler serves every advertised tool.

CRITICAL — REUSE newFakeZAI/fakeState from upstream_test.go in dispatch_test.go; do NOT
  redeclare them (same package => "redeclared in this block" compile error). Likewise do not
  redefine buildTools/extract/teach/UpstreamClient — consume them.

CRITICAL — read fakeState fields UNDER st.mu in tests (st.lastArgs/lastTool/lastAuth are
  written under st.mu by the fake). Reading without the lock is a data race that `go test
  -race` flags. st.calls is int32 — read via atomic.LoadInt32. Use the lastArg/callCount
  helpers (defined in dispatch_test.go) for this.

CRITICAL — newUpstreamClient goes in server.go, NOT upstream.go. upstream.go is owned by
  P1.M4.T1; editing it risks conflict. Same-package Go lets the constructor live anywhere;
  server.go (the wiring file) is the right home. It mirrors upstream_test.go's struct
  literal exactly (upstream/targetTool/targetParam/log; NO credential field — PRD §17).

GOTCHA — the delegate log fires for EVERY tools/call (step 3 is before the Found check),
  including the empty-{} case (source="", canonical=false, optionals=nil), which THEN also
  fires extract_failed. This matches PRD §15 "delegate: per tools/call" + the separate
  extract_failed event. Do not gate delegate on Found.

GOTCHA — canonical(bool) in the delegate log = the call IS canonical = calledTool==CanonicalTool
  && result.Source==CanonicalParam && len(result.Optionals)==0 (equivalently !shouldWarn).
  optionals = sortedKeys(result.Optionals) — the canonical optional NAMES forwarded (nil when
  none). Compute canonical directly (do not couple the log to shouldWarn).

GOTCHA — the handler's upstream_error log (step 6) co-fires with upstream.callTool's
  internal session-lifecycle logs (session_expired/reinit_failed) for those 2 cases. This is
  INTENTIONAL and complementary (different fields: callTool logs status+reinit_attempts; the
  handler logs the call outcome + err). Do NOT suppress the handler's log to "avoid double-
  logging" — the contract mandates it; plain non-expiry errors are logged ONLY by the handler.

GOTCHA — logger.log panics on a nil receiver (it dereferences l.level). Guard each log call
  with `if log != nil { ... }` (matches upstream.go's nil-safe logUpstreamError style), so a
  future test that constructs the handler oddly cannot nil-panic. Production always passes the
  non-nil bootstrap logger.

GOTCHA — CallToolRequest is directly constructible in tests with nil Session/Extra (the
  handler reads ONLY req.Params). So unit-test makeDispatchHandler by calling the returned
  closure with a hand-built *mcp.CallToolRequest — no SDK server routing needed (that's
  P1.M5.T3.S1's e2e job: tools/list, session-expiry §19.3 cases 5-6).

GOTCHA — fakeState.calls is double-incremented on the SUCCESS path inside newFakeZAI (a
  pre-existing quirk in upstream_test.go's fake). So assert on st.lastArgs (search_query
  present, query absent) rather than an exact st.calls for the Found cases; for the empty
  case assert st.calls==0 (no upstream call, clean). Use callCount(st) (atomic) for the 0
  assertion.
```

## Implementation Blueprint

### Data models and structure

No new domain types. The handler consumes `Config`, `*UpstreamClient`, `*logger`, and the
SDK types `mcp.Server`/`mcp.ToolHandler`/`mcp.CallToolRequest`/`mcp.CallToolResult`. One
private helper (`sortedKeys`). Deps: `context`, `sort`, `github.com/modelcontextprotocol/
go-sdk/mcp` (all already transitively used elsewhere; go.mod unchanged).

### Reference implementation (CREATE `server.go`)

> Run `gofmt -w server.go` after. Whole file is new.

```go
package main

import (
	"context"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newUpstreamClient builds the shared z.ai delegation client from cfg (PRD §11.1,
// FR-5). It is the constructor main.go uses; tests may also use it or the bare
// struct literal (&UpstreamClient{...}) as upstream_test.go does. It sets the four
// configuration-driven fields and the logger; the session itself is created LAZILY
// on the first callTool (UpstreamClient.ensureSession), so a server with no
// tools/call traffic never opens an upstream connection.
//
// NO CREDENTIAL FIELD (PRD §17, FR-7): Authorization is forwarded as a request
// header via the request context (main.go's authMiddleware stores it under
// authHeaderKey; upstream.authInjector reads it via authFromContext inside
// RoundTrip). The credential is never assigned to a field of UpstreamClient and
// never logged (redactHeaders would redact it if it ever were).
func newUpstreamClient(cfg Config, log *logger) *UpstreamClient {
	return &UpstreamClient{
		upstream:    cfg.Upstream,
		targetTool:  cfg.TargetTool,
		targetParam: cfg.TargetParam,
		log:         log,
	}
}

// registerTools advertises the server's tools and wires the shared dispatch handler
// (PRD §5.2, §9). It builds ONE handler via makeDispatchHandler and registers it for
// EVERY tool returned by buildTools(cfg) — all advertised tools route to the SAME
// handler, because the handler keys its behavior off the ACTUAL called tool name
// (req.Params.Name) vs cfg.CanonicalTool, independent of which tool entry routed the
// call (PRD §9.3: additionally-configured names are terse aliases of the canonical).
//
// This is the single point where tool DEFINITION (buildTools, P1.M5.T1.S1) meets
// tool DISPATCH (this milestone): buildTools produces AddTool-safe *mcp.Tool values;
// registerTools hands each one to (*Server).AddTool with the shared handler. main.go
// calls registerTools(server, cfg, upstream, log) immediately after mcp.NewServer.
func registerTools(server *mcp.Server, cfg Config, upstream *UpstreamClient, log *logger) {
	handler := makeDispatchHandler(cfg, upstream, log)
	for _, tool := range buildTools(cfg) {
		server.AddTool(tool, handler)
	}
}

// makeDispatchHandler returns the shared ToolHandler that implements the
// extract → delegate → teach pipeline for every advertised tool (PRD §5.2, §11.3,
// FR-4/5/6). For each inbound tools/call it runs, in order:
//
//	(1) read calledTool = req.Params.Name and rawArgs = req.Params.Arguments.
//	(2) result := extract(rawArgs, cfg.QueryAliases, cfg.OptionalAliases) — recover a
//	    query from ANY input shape (PRD §10, FR-4).
//	(3) log "delegate" {called_tool, source, canonical(bool), optionals(names)} per
//	    tools/call (PRD §15).
//	(4) if !result.Found: log "extract_failed", return the IMMEDIATE no-results
//	    warning (noQueryResult) and make NO upstream call (PRD §10.1.5, FR-6). This
//	    is the single sanctioned warning-without-results case.
//	(5) else upstreamResult, err := upstream.callTool(ctx, result.ToUpstreamArgs(
//	    cfg.TargetParam)) — delegate to z.ai web_search_prime (PRD §11, FR-5).
//	    ToUpstreamArgs drops aliases/junk so z.ai receives a schema-valid
//	    {search_query, ...optionals}. Auth rides via ctx (authHeaderKey).
//	(6) if err: log "upstream_error", return (nil, err) HONESTLY — the SDK wraps it
//	    as a JSON-RPC error. Never synthesize a result (PRD §11.1 honest-error rule).
//	(7) if shouldWarn: appendWarning(upstreamResult, warningText(...)) — the warning
//	    is APPENDED AFTER the real results (PRD §12, FR-6) so the model acts on the
//	    results rather than retrying on a lone warning.
//	(8) return upstreamResult.
//
// INVARIANTS (PRD §11.3, §12, FR-6): results LEAD and the warning TRAILS; the
// no-query case returns the warning with NO results and NO upstream call; IsError is
// NEVER set for any normalization/warning case — results are built only via
// teach.noQueryResult / teach.appendWarning (which never call SetError) and the
// upstream result is returned UNCHANGED. log must be non-nil in production (main.go
// passes the bootstrap logger); the nil-guards keep a future odd test from panicking.
func makeDispatchHandler(cfg Config, upstream *UpstreamClient, log *logger) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calledTool := req.Params.Name
		rawArgs := req.Params.Arguments

		// (2) EXTRACT a query from whatever the agent sent (PRD §10, FR-4).
		result := extract(rawArgs, cfg.QueryAliases, cfg.OptionalAliases)

		// (3) "delegate" log event (PRD §15): per tools/call, emitted before the Found
		// check so every call is recorded. canonical = the call is exactly the
		// canonical tool/param with no normalized optionals (== !shouldWarn).
		canonical := calledTool == cfg.CanonicalTool &&
			result.Source == cfg.CanonicalParam &&
			len(result.Optionals) == 0
		if log != nil {
			log.log("info", "delegate", map[string]any{
				"called_tool": calledTool,
				"source":      result.Source,
				"canonical":   canonical,
				"optionals":   sortedKeys(result.Optionals),
			})
		}

		// (4) No usable query (PRD §10.1.5, FR-6 exception): return the immediate
		// no-results warning and make NO upstream call. IsError stays false.
		if !result.Found {
			if log != nil {
				log.log("info", "extract_failed", map[string]any{
					"called_tool": calledTool,
				})
			}
			return noQueryResult(noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam)), nil
		}

		// (5) DELEGATE to z.ai (PRD §11, FR-5). ToUpstreamArgs drops aliases/junk so
		// z.ai gets a schema-valid {search_query, ...optionals}. Auth rides via ctx.
		upstreamResult, err := upstream.callTool(ctx, result.ToUpstreamArgs(cfg.TargetParam))
		if err != nil {
			// (6) Honest error (PRD §11.1): surface it verbatim; the SDK wraps it as a
			// JSON-RPC error. Never synthesize a result; never set IsError. (callTool
			// already logs session_expired/reinit_failed internally for those cases;
			// this is the call-level outcome log — complementary, not a duplicate.)
			if log != nil {
				log.log("warn", "upstream_error", map[string]any{
					"called_tool": calledTool,
					"err":         err.Error(),
				})
			}
			return nil, err
		}

		// (7)(8) TEACH (PRD §12, FR-6): append the warning AFTER the real results when
		// usage was non-canonical. Results lead, warning trails -> no retry. IsError is
		// NEVER set (appendWarning appends a TextContent and touches nothing else).
		if shouldWarn(calledTool, result, cfg.CanonicalTool, cfg.CanonicalParam) {
			appendWarning(upstreamResult, warningText(calledTool, result.Source, cfg.CanonicalTool, cfg.CanonicalParam))
		}
		return upstreamResult, nil
	}
}

// sortedKeys returns the keys of m in sorted order (nil when m is empty). It is used
// to log the normalized optional NAMES forwarded to z.ai as a deterministic JSON
// array (PRD §15 delegate "optionals"). Map iteration is randomized in Go, so the
// sort makes the log line stable. The VALUES are not logged (only the names matter
// for "which optionals were normalized"); nil/empty -> JSON null.
func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
```

### Reference edit (MODIFY `main.go`)

> Insert the wiring immediately AFTER the `mcp.NewServer(...)` line and BEFORE the
> `// [Mode A] The SDK's StreamableHTTPHandler ...` comment. No imports change.

oldText (exact, unique in main.go):
```go
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)

	// [Mode A] The SDK's StreamableHTTPHandler OWNS all MCP transport framing —
```
newText:
```go
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)

	// P1.M5.T2: advertise the tools (buildTools) on the SDK server, each wired to the
	// shared extract→delegate→teach dispatch handler (PRD §5.2, §11.3). The UpstreamClient
	// owns the lazy shared z.ai session; its outbound Authorization is threaded via ctx
	// (authMiddleware → authHeaderKey → authInjector), so the handler just forwards ctx.
	upstream := newUpstreamClient(cfg, log)
	registerTools(server, cfg, upstream, log)

	// [Mode A] The SDK's StreamableHTTPHandler OWNS all MCP transport framing —
```

### Reference test file (CREATE `dispatch_test.go`)

> Run `gofmt -w dispatch_test.go` after. Reuses newFakeZAI/fakeState from upstream_test.go
> (same package — do NOT redeclare them). Reads fakeState under st.mu / atomic for race-safety.

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newDispatchHandler builds the handler against a fresh fake z.ai (reusing
// upstream_test.go's newFakeZAI). When buf != nil it captures logs at debug; else it
// discards. Returns the handler and the fake's recorded state. The UpstreamClient is
// constructed exactly as upstream_test.go does (struct literal; no credential field).
func newDispatchHandler(t *testing.T, buf *bytes.Buffer) (mcp.ToolHandler, *fakeState) {
	t.Helper()
	srv, st := newFakeZAI(t)
	cfg := DefaultConfig()
	log := newLogger(io.Discard, "info")
	if buf != nil {
		log = newLogger(buf, "debug")
	}
	upstream := &UpstreamClient{
		upstream:    srv.URL,
		targetTool:  cfg.TargetTool,
		targetParam: cfg.TargetParam,
	}
	return makeDispatchHandler(cfg, upstream, log), st
}

// callDispatch invokes h with a constructed CallToolRequest (nil Session/Extra — the
// handler reads only Params). argsJSON is the raw arguments payload (a JSON object,
// string, etc.).
func callDispatch(t *testing.T, h mcp.ToolHandler, tool, argsJSON string) (*mcp.CallToolResult, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return h(ctx, &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: tool, Arguments: json.RawMessage(argsJSON)},
	})
}

// lastArg reads a single recorded upstream argument under the fake's mutex (race-safe).
func lastArg(st *fakeState, key string) any {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.lastArgs[key]
}

// callCount atomically reads the fake's tools/call count.
func callCount(st *fakeState) int32 {
	return atomic.LoadInt32(&st.calls)
}

// contentText returns Content[i] as a *mcp.TextContent's Text, failing if the shape is wrong.
func contentText(t *testing.T, res *mcp.CallToolResult, i int) string {
	t.Helper()
	if i >= len(res.Content) {
		t.Fatalf("Content[%d] out of range (len=%d)", i, len(res.Content))
	}
	tc, ok := res.Content[i].(*mcp.TextContent)
	if !ok {
		t.Fatalf("Content[%d] is %T, want *mcp.TextContent", i, res.Content[i])
	}
	return tc.Text
}

// (1) Canonical call: web_search + {"query":...} -> upstream gets search_query verbatim;
// result is the single canned block with NO appended warning; IsError false.
func TestDispatch_Canonical_NoWarning(t *testing.T) {
	h, st := newDispatchHandler(t, nil)
	res, err := callDispatch(t, h, "web_search", `{"query":"rust async runtime"}`)
	if err != nil {
		t.Fatalf("canonical call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (canonical never errors — FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "rust async runtime" {
		t.Errorf("upstream search_query = %#v, want the verbatim query", got)
	}
	if lastArg(st, "query") != nil {
		t.Errorf("upstream received alias 'query' (should be renamed to search_query)")
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1 (no warning for canonical)", len(res.Content))
	}
	if strings.Contains(contentText(t, res, 0), "[web-search-prime-fixer]") {
		t.Errorf("canonical result unexpectedly contains a warning block")
	}
}

// (2) Alias + junk: web_search + {"q":"x","junk":1} -> upstream gets search_query=="x"
// (junk dropped); result is the canned block THEN a warning; IsError false.
func TestDispatch_AliasJunk_WarningAppendedAfter(t *testing.T) {
	h, st := newDispatchHandler(t, nil)
	res, err := callDispatch(t, h, "web_search", `{"q":"x","junk":1}`)
	if err != nil {
		t.Fatalf("alias call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (warnings never set IsError — FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (extracted from alias 'q')", got)
	}
	if lastArg(st, "junk") != nil {
		t.Errorf("upstream received dropped key 'junk' (ToUpstreamArgs must drop it)")
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2 (result THEN warning)", len(res.Content))
	}
	// Results LEAD: content[0] is the canned upstream result, NOT the warning.
	if strings.Contains(contentText(t, res, 0), "[web-search-prime-fixer]") {
		t.Errorf("content[0] is the warning (results must LEAD, warning TRAILS — §11.3)")
	}
	// content[1] is the appended warning; it names the source actually used ("q").
	warn := contentText(t, res, 1)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("content[1] is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, `"q"`) {
		t.Errorf("warning does not name the used source \"q\": %q", warn)
	}
}

// (3) Bare-string argument: web_search + "x" -> extracted (Source="bare-string"),
// forwarded, result + warning; IsError false.
func TestDispatch_BareString_ExtractedAndWarned(t *testing.T) {
	h, st := newDispatchHandler(t, nil)
	res, err := callDispatch(t, h, "web_search", `"x"`)
	if err != nil {
		t.Fatalf("bare-string call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (FR-6)")
	}
	if got := lastArg(st, "search_query"); got != "x" {
		t.Errorf("upstream search_query = %#v, want \"x\" (bare-string extracted)", got)
	}
	if len(res.Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2 (result + warning for non-canonical)", len(res.Content))
	}
	if !strings.Contains(contentText(t, res, 1), "bare-string") {
		t.Errorf("warning does not reflect the bare-string source: %q", contentText(t, res, 1))
	}
}

// (4) Empty object: web_search + {} -> NO upstream call (callCount==0); the result is
// the immediate no-results warning (1 block); IsError false. (PRD §10.1.5, FR-6)
func TestDispatch_Empty_NoUpstreamImmediateWarning(t *testing.T) {
	h, st := newDispatchHandler(t, nil)
	res, err := callDispatch(t, h, "web_search", `{}`)
	if err != nil {
		t.Fatalf("empty call errored: %v", err)
	}
	if res.IsError {
		t.Errorf("IsError = true, want false (FR-6)")
	}
	if n := callCount(st); n != 0 {
		t.Errorf("upstream calls = %d, want 0 (no-query must NOT call upstream — §10.1.5)", n)
	}
	if len(res.Content) != 1 {
		t.Fatalf("len(Content) = %d, want 1 (the immediate no-results warning)", len(res.Content))
	}
	warn := contentText(t, res, 0)
	if !strings.HasPrefix(warn, "[web-search-prime-fixer]") {
		t.Errorf("result is not the warning marker: %q", warn)
	}
	if !strings.Contains(warn, "could not find a search query") {
		t.Errorf("result is not the no-results warning: %q", warn)
	}
}

// (5) Upstream error: the fake tool returns an error -> the handler returns (nil, err)
// honestly (no synthesized result). PRD §11.1 honest-error rule.
func TestDispatch_UpstreamError_ReturnedHonestly(t *testing.T) {
	h, st := newDispatchHandler(t, nil)
	st.setToolErr(errors.New("z.ai blew up"))
	res, err := callDispatch(t, h, "web_search", `{"query":"x"}`)
	if err == nil {
		t.Fatalf("upstream error not surfaced: err == nil, res=%v", res)
	}
	if res != nil {
		t.Errorf("handler synthesized a result on upstream error (must return nil): %+v", res)
	}
}

// (6) IsError-never-set sweep: across every normalization/warning shape, IsError stays
// false. (The error-return path is excluded — it returns nil, not a result.)
func TestDispatch_IsErrorNeverSet(t *testing.T) {
	shapes := []struct{ name, args string }{
		{"canonical", `{"query":"x"}`},
		{"alias", `{"q":"x"}`},
		{"bare-string", `"x"`},
		{"number", `42`},
		{"nested", `{"input":{"query":"x"}}`},
		{"empty", `{}`},
		{"no-arguments", ``},
	}
	for _, s := range shapes {
		t.Run(s.name, func(t *testing.T) {
			h, _ := newDispatchHandler(t, nil)
			res, err := callDispatch(t, h, "web_search", s.args)
			if err != nil {
				t.Fatalf("%s: unexpected error %v", s.name, err)
			}
			if res.IsError {
				t.Errorf("%s: IsError = true (FR-6: never set for normalization/warning)", s.name)
			}
		})
	}
}
```

> NOTE on imports for dispatch_test.go: `errors` must be added to the import block
> (used by TestDispatch_UpstreamError_ReturnedHonestly). Final import list:
> `bytes`, `context`, `encoding/json`, `errors`, `io`, `strings`, `sync/atomic`,
> `testing`, `time`, `github.com/modelcontextprotocol/go-sdk/mcp`. Drop `bytes` if no
> test uses a capture buffer (all tests above pass nil, so `bytes` is unused — REMOVE
> it or Go errors on the unused import). Decision: since no test above captures a
> buffer, either (a) remove the `buf *bytes.Buffer` param + `bytes` import, or (b) add
> one log-capturing assertion. The reference keeps the param for reuse but YOU MUST
> remove the unused `bytes` import if no test passes a non-nil buf. Simplest: change
> `newDispatchHandler(t, nil)` helpers to take no buf, drop `bytes`. (See Task 4.)

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the on-disk state
  - RUN: grep -q "func buildTools" tools.go && grep -q "func extract" extract.go \
        && grep -q "func shouldWarn" teach.go && grep -q "func (u \*UpstreamClient) callTool" upstream.go \
        && grep -q "func newFakeZAI" upstream_test.go && ! -f server.go && echo OK
  - EXPECT: OK. IF ANY FAIL: STOP — a prerequisite (buildTools from the parallel
        P1.M5.T1.S1, or extract/teach/upstream/newFakeZAI) is missing, or server.go
        already exists (collision).

Task 1: CREATE server.go — newUpstreamClient + registerTools + sortedKeys
  - FILE: server.go. IMPORTS: context, sort, github.com/modelcontextprotocol/go-sdk/mcp.
  - IMPLEMENT: newUpstreamClient(cfg, log) (struct literal; no credential field);
        registerTools (one handler, loop buildTools, AddTool each); sortedKeys.
  - NAMING: newUpstreamClient/registerTools/makeDispatchHandler exported (main.go +
        future tests consume them); sortedKeys private.

Task 2: IMPLEMENT makeDispatchHandler (the 8-step pipeline)
  - FILE: server.go. RETURN mcp.ToolHandler closure. Steps: read calledTool/rawArgs;
        extract; log delegate (nil-guarded); if !Found log extract_failed + return
        noQueryResult(noQueryWarningText); callTool(ctx, ToUpstreamArgs(TargetParam));
        if err log upstream_error + return (nil,err); if shouldWarn appendWarning(
        warningText); return upstreamResult.
  - INVARIANTS: never SetError; warning appended AFTER results; auth via ctx only;
        nil-guard every log.log.

Task 3: MODIFY main.go (the wiring)
  - EDIT: insert `upstream := newUpstreamClient(cfg, log)` + `registerTools(server,
        cfg, upstream, log)` between the NewServer line and the [Mode A] comment.
  - VERIFY no import change: `go build ./...` (main.go already imports mcp; the two
        new calls are same-package).

Task 4: CREATE dispatch_test.go (reuse newFakeZAI)
  - FILE: dispatch_test.go. IMPORTS: context, encoding/json, errors, io, strings,
        sync/atomic, testing, time, mcp. (NO bytes unless a test captures a buffer —
        see the NOTE above; drop the buf param + bytes import if unused.)
  - TESTS: TestDispatch_Canonical_NoWarning, TestDispatch_AliasJunk_WarningAppendedAfter,
        TestDispatch_BareString_ExtractedAndWarned, TestDispatch_Empty_NoUpstreamImmediateWarning,
        TestDispatch_UpstreamError_ReturnedHonestly, TestDispatch_IsErrorNeverSet.
  - HELPERS: newDispatchHandler, callDispatch, lastArg, callCount, contentText.
  - CONSTRAINTS: do NOT redeclare newFakeZAI/fakeState (reuse from upstream_test.go);
        read st under st.mu / atomic (race-safe).

Task 5: VALIDATE
  - gofmt -w server.go dispatch_test.go
  - go vet ./...                  # incl. -race-clean (the st.mu reads matter)
  - go build ./...
  - go test -run 'TestDispatch_' -v        # the six new tests
  - go test -race -count=1 ./...           # full suite green + race-clean (additive)
  - git diff --stat                        # expect server.go (new), dispatch_test.go (new), main.go (modified)
  - git diff go.mod                        # expect EMPTY
  - go doc . makeDispatchHandler           # Mode A: pipeline order, no-results exception, append-after, IsError invariant
```

### Implementation Patterns & Key Details

```go
// PATTERN: one shared handler for all tools. registerTools builds ONE handler and
// registers it for every buildTools entry. The handler keys off req.Params.Name (the
// ACTUAL called tool), so it is independent of which tool entry routed the call.
//   handler := makeDispatchHandler(cfg, upstream, log)
//   for _, tool := range buildTools(cfg) { server.AddTool(tool, handler) }

// PATTERN: forward ctx for auth, do not pass auth explicitly. The SDK-provided ctx
// already carries authHeaderKey (main.go authMiddleware); upstream.authInjector reads
// it. So step 5 is upstream.callTool(ctx, result.ToUpstreamArgs(cfg.TargetParam)).

// PATTERN: build results only via teach.go (never SetError). noQueryResult for the
// no-query case; appendWarning for the after-results warning; return upstream.callTool's
// result UNCHANGED on success. IsError stays false everywhere; errors return (nil, err).

// PATTERN: log under nil-guard (matches upstream.go's logUpstreamError). `if log != nil
// { log.log(...) }` per event. Production passes the non-nil bootstrap logger.

// PATTERN: read fakeState under st.mu / atomic in tests (race-safety for `go test -race`).
// st.lastArgs/lastTool/lastAuth under st.mu (use lastArg helper); st.calls via atomic
// (use callCount helper).

// GOTCHA (restated): IsError NEVER set — never call SetError; use noQueryResult/appendWarning.
// GOTCHA (restated): warning APPENDED AFTER results (appendWarning is append-LAST).
// GOTCHA (restated): the no-query path makes NO upstream call (gate on result.Found).
// GOTCHA (restated): delegate log fires for EVERY call (before the Found check); the
//   empty case ALSO fires extract_failed right after. Do not gate delegate on Found.
// GOTCHA (restated): newUpstreamClient lives in server.go, NOT upstream.go (M4-owned).
// GOTCHA (restated): do NOT redeclare newFakeZAI/fakeState/buildTools — reuse/consume them.
// GOTCHA (restated): drop the `bytes` import from dispatch_test.go if no test captures a
//   buffer (Go errors on unused imports).
```

### Integration Points

```yaml
FILES CREATED:
  - server.go         (package main: newUpstreamClient + registerTools + makeDispatchHandler +
                sortedKeys. Imports context, sort, go-sdk/mcp. The 8-step pipeline; one shared
                handler; IsError never set; warning after results; auth via ctx.)
  - dispatch_test.go  (package main: 6 tests + helpers. Reuses newFakeZAI/fakeState. Race-safe
                fakeState reads. Imports context, encoding/json, errors, io, strings,
                sync/atomic, testing, time, go-sdk/mcp — drop `bytes` if unused.)
FILES MODIFIED:
  - main.go           (insert `upstream := newUpstreamClient(cfg, log)` + `registerTools(server,
                cfg, upstream, log)` after mcp.NewServer. NO import change.)
FILES NOT TOUCHED (contract):
  - go.mod / go.sum: unchanged (single SDK require; context/sort/mcp already used).
  - extract.go / teach.go / upstream.go / config.go / logger.go / health.go / tools.go:
        consumed, not edited (newUpstreamClient is in server.go, NOT upstream.go).
  - all other *_test.go / testdata / README.md / config.example.json / PRD.md / doc.go: untouched.
CONSUMER SEAM (keep stable for M5.T3 / M5.T4):
  - registerTools(server, cfg, upstream, log) — main.go calls it once after NewServer.
  - makeDispatchHandler(cfg, upstream, log) mcp.ToolHandler — M5.T3.S1's server_test.go e2e
        exercises the REGISTERED handler end to end (§19.3 cases 1-4); this item's
        dispatch_test.go covers the handler-level logic. tools/list + session-expiry
        (§19.3 cases 5-6) are M5.T3.S1's.
DATABASE / ROUTES / ENV / CONFIG: none (the handler reads cfg fields; no new config).
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w server.go dispatch_test.go
go vet ./...
go build ./...
git diff --stat     # expect: server.go (new), dispatch_test.go (new), main.go (modified)
git diff go.mod     # expect: EMPTY (no new requires; context+sort+mcp already used)
git diff main.go    # expect: ONLY the 2-line wiring insertion after NewServer

# Expected: gofmt clean; vet clean; build clean; exactly the three files; go.mod unchanged;
# main.go diff is the minimal insertion. If vet reports "imported and not used" in
# dispatch_test.go, an import drifted (e.g. unused bytes) — trim it.
```

### Level 2: Handler-level tests (the pipeline logic)

```bash
go test -run 'TestDispatch_' -v

# MUST PASS (prove the extract→delegate→teach contract):
#   TestDispatch_Canonical_NoWarning            -> upstream got search_query; 1 block; no warning; IsError false.
#   TestDispatch_AliasJunk_WarningAppendedAfter -> upstream got search_query (junk dropped); 2 blocks
#                                                  (result THEN warning); warning names "q"; IsError false.
#   TestDispatch_BareString_ExtractedAndWarned  -> extracted bare-string; forwarded; 2 blocks; IsError false.
#   TestDispatch_Empty_NoUpstreamImmediateWarning -> 0 upstream calls; 1 block (no-results warning); IsError false.
#   TestDispatch_UpstreamError_ReturnedHonestly -> err != nil, res == nil (no synthesis).
#   TestDispatch_IsErrorNeverSet (sweep)        -> IsError false for every shape.
# Expected: PASS, exit 0. If Empty fails with calls>0, the handler called upstream on a
#   no-query result (gate on result.Found). If AliasJunk has 1 block, appendWarning was
#   skipped (shouldWarn/appendWarning wiring). If any IsError is true, SetError was called
#   somewhere (it must never be).
```

### Level 3: Full suite + race (regression)

```bash
go test -race -count=1 ./...

# Expected: ALL green, exit 0, race-clean. dispatch_test.go is additive (new file, new
# symbols); it REUSES newFakeZAI/fakeState without redeclaring them. If a "redeclared in
# this block" compile error appears, a helper name collided with upstream_test.go (rename
# it). The sibling suites (extract/teach/upstream/tools) must be unaffected. The race
# detector confirms the st.mu-guarded fakeState reads are correct.

# Confirm the consumer wiring is real (main.go calls registerTools after NewServer):
grep -A1 "mcp.NewServer" main.go | grep -q "registerTools" && echo "wired OK"
go doc . makeDispatchHandler   # Mode A: prints the pipeline-order / IsError-invariant doc
```

### Level 4: End-to-end smoke (the wired server actually answers tools/call)

```bash
# Boot the real server (defaults) and confirm it now answers tools/call through the
# pipeline. (No real z.ai call — point WSPF_UPSTREAM at a throwaway or just confirm
# tools/list advertises web_search, which proves registerTools ran at boot.)
go build -o /tmp/wspf .
WSPF_LISTEN=127.0.0.1:18791 /tmp/wspf > /tmp/wspf.log 2>&1 & PID=$!
sleep 1.2
# initialize -> get a session, then tools/list should advertise exactly web_search.
# (Minimal smoke: the startup log lists the tools; tools/list is asserted fully in M5.T3.)
grep -q '"msg":"startup"' /tmp/wspf.log && echo "booted OK"
kill $PID; wait $PID 2>/dev/null
# NOTE: a full tools/call + tools/list e2e is P1.M5.T3.S1's job (server_test.go §19.3).
# This smoke only proves the server BOOTS with tools registered (registerTools ran).

# Expected: the server boots (startup log present) with no panic — i.e. AddTool accepted
# every buildTools tool and registerTools completed. A panic here means a buildTools schema
# is malformed (buildTools' contract — its TestBuildTools_AddToolSafe should have caught it).
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 clean: `gofmt -l .`, `go vet ./...`, `go build ./...`, `git diff --stat`
      (server.go + dispatch_test.go new, main.go modified, nothing else), `git diff go.mod`
      (empty), `git diff main.go` (only the 2-line insertion).
- [ ] Level 2 passes: `go test -run 'TestDispatch_' -v` (all six green).
- [ ] Level 3 passes: `go test -race -count=1 ./...` fully green + race-clean.
- [ ] Level 4 passes: the server boots with tools registered (no AddTool panic).

### Feature Validation (PRD §5.2/§11.3/§12/FR-4/5/6)

- [ ] Canonical `{"query":"x"}` → upstream `search_query` verbatim; 1 block; no warning.
- [ ] Alias `{"q":"x","junk":1}` → upstream `search_query` (junk dropped); 2 blocks
      (result THEN warning); warning names the used source.
- [ ] Bare-string `"x"` → extracted + forwarded; result + warning.
- [ ] Empty `{}` → 0 upstream calls; immediate no-results warning; IsError false.
- [ ] Upstream error → `(nil, err)` returned honestly (no synthesized result).
- [ ] IsError NEVER true for any normalization/warning shape (the sweep).
- [ ] All advertised tools route to ONE shared handler (registerTools builds it once).

### Code Quality Validation

- [ ] Follows existing conventions: pure-ish handler closure (like extract/warningText);
      unexported helper (sortedKeys); doc comments citing PRD §5.2/§11.3/§12/FR-4/5/6/§15;
      newUpstreamClient mirrors upstream_test.go's struct-literal construction.
- [ ] File placement matches the desired tree (server.go + dispatch_test.go new; main.go
      minimal edit).
- [ ] Anti-patterns avoided (see below): no SetError; no prepend; no upstream call on
      no-query; no auth argument; no per-tool handler; no redeclared harness; no upstream.go
      edit; no unused import.
- [ ] No new dependencies (`go.mod` unchanged).

### Documentation & Deployment

- [ ] Doc comments on makeDispatchHandler (the 8-step pipeline order, the no-results
      exception, the append-after ordering, the IsError-never-set invariant) and
      registerTools (one shared handler; buildTools meets dispatch).
- [ ] `go doc . makeDispatchHandler` prints the contract M5.T3 / M5.T4 consume.
- [ ] **Mode A**: this item ships its doc comments inline + the main.go wiring; no
      README/doc.go change required (doc.go is P1.M5.T4.S2; README is P1.M5.T4.S1).

---

## Anti-Patterns to Avoid

- ❌ Don't call `result.SetError` (or set `IsError = true`) for ANY normalization/warning
  case — FR-6/PRD §12.2 forbid it. Build results only via `noQueryResult`/`appendWarning`
  and return upstream's result unchanged; errors return `(nil, err)`.
- ❌ Don't PREPEND the warning or return it WITHOUT results when a search could run —
  FR-6/§11.3 require results-then-warning (appendWarning is append-LAST). The only
  warning-without-results case is extract-failure (no upstream call).
- ❌ Don't make an upstream call when `!result.Found` — PRD §10.1.5/FR-6: return the
  immediate no-query warning and skip delegation entirely.
- ❌ Don't pass Authorization explicitly to callTool — it rides via `ctx` (authHeaderKey).
  Adding an auth argument would duplicate the context-seam and break the verbatim-forward
  design.
- ❌ Don't build a per-tool handler — registerTools builds ONE handler shared by all tools
  (the handler keys off req.Params.Name). Per-tool handlers would duplicate the pipeline.
- ❌ Don't redeclare `newFakeZAI`/`fakeState`/`buildTools`/`extract`/`teach`/`UpstreamClient`
  in server.go or dispatch_test.go — consume/reuse them (same package; redeclaring is a
  compile error).
- ❌ Don't edit `upstream.go` to add `newUpstreamClient` — it is M4-owned; put the
  constructor in server.go (same-package Go allows it).
- ❌ Don't gate the `delegate` log on `result.Found` — PRD §15 fires it per tools/call
  (step 3 is before the Found check); the empty case also fires `extract_failed` after it.
- ❌ Don't suppress the handler's `upstream_error` log to "avoid double-logging" — the
  contract mandates it; callTool's session-lifecycle logs are complementary (different fields).
- ❌ Don't read `fakeState` fields without `st.mu` / atomic in tests — `go test -race` flags
  it (use the lastArg/callCount helpers).
- ❌ Don't leave an unused import (e.g. `bytes`) in dispatch_test.go — Go fails the build.
- ❌ Don't touch `go.mod`, `tools.go`, `upstream.go`, or any doc/test file outside the three
  named files — this item owns server.go + dispatch_test.go + the main.go wiring, nothing else.

---

## Confidence Score

**9/10** for one-pass implementation success. The whole deliverable is one new file
(server.go) whose reference implementation is given verbatim, one minimal main.go edit
(exact oldText→newText), and one new test file (dispatch_test.go) reusing an existing
harness. Every consumed signature (extract/teach/upstream/logger/config) was VERIFIED by
reading the on-disk source; the SDK surface (AddTool/ToolHandler/CallToolRequest/
CallToolResult) is verified from the v1.6.1 source + the architecture doc; and the test
seam (direct CallToolRequest construction with nil Session/Extra) is proven from the
ServerRequest struct definition. The five handler behaviors map 1:1 to PRD §19.3 cases 1-4
(which M5.T3 will assert e2e) and are pinned here at the handler level. Deducted 1 point
for two residual risks: (a) the `bytes` import must be dropped if no dispatch test captures
a buffer (Go fails on unused imports — flagged in Task 4 + the gotcha); (b) the parallel
P1.M5.T1.S1 (buildTools/tools.go) must have landed for registerTools to compile (Task 0's
prerequisite check guards this). Both are caught by Level 1's `go build`/`go vet`.
