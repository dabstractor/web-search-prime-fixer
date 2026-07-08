name: "P1.M5.T1.S1 — Canonical web_search tool + permissive schema + terse alias tool definitions (PRD §9)"
description: |

  OWN and VERIFY `func buildTools(cfg Config) []*mcp.Tool` in a NEW file
  `tools.go` (package main): the advertised MCP tool definitions, ready for
  `(*Server).AddTool` registration by P1.M5.T2.S1.

  WHAT IT BUILDS (PRD §9, FR-2, FR-3): iterate `cfg.Tools` IN ORDER. The entry
  equal to `cfg.CanonicalTool` becomes the CANONICAL tool — Name=CanonicalTool, a
  full Description, and a DOCUMENTED but PERMISSIVE InputSchema (`type:object`;
  the canonical param primary + the configured query aliases + the z.ai optionals
  all as optional strings; `additionalProperties:true`; NO `required` array). Every
  OTHER entry is a TERSE ALIAS — Name=<entry>, a one-line Description ("Performs a
  web search; alias of <CanonicalTool>."), and the minimal open schema
  (`{"type":"object","additionalProperties":true}`). All advertised tools route to
  the SAME handler (M5.T2 registers them); extraction (extract.go) normalizes
  whatever the agent sent.

  CRITICAL SDK FACT (verified from go-sdk v1.6.1 source + PROVEN by a throwaway
  PoC this session): `(*Server).AddTool` PANICS if `t.InputSchema == nil` and
  PANICS if the schema's `type` is not `"object"` (server.go:242-260). Every schema
  buildTools emits MUST therefore be a valid JSON-schema object, or the consumer
  (M5.T2) crashes at startup. The PoC confirmed both the dynamic canonical schema
  (map → json.Marshal → json.RawMessage) and the literal alias schema are
  AddTool-safe (PASS, no panic; canonical schema decoded to type:object,
  additionalProperties:true, 17 properties).

  KEY DESIGN DECISION (load-bearing): select the canonical tool by VALUE
  (`name == cfg.CanonicalTool`), NOT by `Tools[0]` index. config validation
  (ResolveConfig, P1.M1.T1.S2 — DONE) only enforces
  `slices.Contains(cfg.Tools, cfg.CanonicalTool)`, NOT `Tools[0]==CanonicalTool`,
  so a reordered config like `["search","web_search"]` passes validation with
  Tools[0]="search". Treating Tools[0] as canonical would then mislabel the
  canonical tool. Value-match is robust; output order still follows cfg.Tools (PRD
  §19.3 case 5). With the default config Tools[0]==CanonicalTool, so behavior
  matches the item's "Tools[0] is canonical" framing.

  NO-Z.AI-NAMES RULE (PRD §9.3/§3): buildTools NEVER advertises cfg.TargetTool
  ("web_search_prime"). This is ENFORCED upstream by config validation (DONE);
  buildTools never references TargetTool and trusts the validated config. A
  defensive test still pins it.

  SCOPE: two NEW files — `tools.go` (buildTools + 3 private helpers + 1 package
  var) and `tools_test.go` (focused unit tests + an AddTool-safe integration gate).
  ZERO edits to existing files. go.mod/go.sum unchanged (all imports already used
  elsewhere). Mode A: doc comments on buildTools + canonicalInputSchema
  documenting the canonical tool, the permissive-schema rationale (document not
  enforce — FR-3), and the no-z.ai-names rule.

---

## Goal

**Feature Goal**: `buildTools(cfg Config) []*mcp.Tool` produces, for every entry in
`cfg.Tools` (in order), an `*mcp.Tool` whose `InputSchema` is a valid JSON-schema
object that `(*Server).AddTool` accepts WITHOUT panicking. The canonical entry
(== `cfg.CanonicalTool`) carries a full Description and a documented-but-permissive
schema (canonical param primary + configured aliases + z.ai optionals as optional
strings, `additionalProperties:true`, no `required`); every other entry is a terse
alias (one-line Description + minimal open schema). The result is ready for the
M5.T2 registration loop and advertises exactly `cfg.Tools`, never `cfg.TargetTool`.

**Deliverable**: TWO new files at the repo root (both `package main`):
1. **CREATE** `tools.go` — `func buildTools(cfg Config) []*mcp.Tool`; private
   helpers `canonicalDescription(cfg)`, `aliasDescription(cfg)`,
   `canonicalInputSchema(cfg)`; package var `aliasInputSchema` (the shared minimal
   open schema). Imports: `encoding/json`, `fmt`,
   `github.com/modelcontextprotocol/go-sdk/mcp`.
2. **CREATE** `tools_test.go` — `TestBuildTools_DefaultCanonical`,
   `TestBuildTools_AliasTool`, `TestBuildTools_NoTargetToolAdvertised`,
   `TestBuildTools_OrderFollowsConfig`, `TestBuildTools_AddToolSafe`, plus a
   `mustDecodeSchema` helper.

No existing file is edited. `go.mod`/`go.sum` unchanged.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` are all clean. With the default config, `buildTools(DefaultConfig())`
returns exactly one tool named `web_search` whose Description reads verbatim
`Performs a web search via z.ai. Call with { "query": "..." }. Accepts alternative
parameter names but canonical is query.` and whose InputSchema decodes to
`type:object`, `additionalProperties:true`, a `query` property, the three z.ai
optionals, and NO `required` array. A config with `Tools=["web_search","search"]`
yields two tools: the canonical (full) + a terse alias whose schema is exactly
`{"type":"object","additionalProperties":true}`. Registering every built tool via
`(*Server).AddTool` does not panic (the integration gate). No built tool is ever
named `cfg.TargetTool`.

## Hard Prerequisites

1. **The MCP SDK is required** (go.mod — DONE): `github.com/modelcontextprotocol/
   go-sdk v1.6.1`. `mcp.Tool` (protocol.go:1295) has fields `Name string`,
   `Description string`, `InputSchema any`. `(*Server).AddTool(t, h)` (server.go:238)
   PANICS on a nil or non-object `InputSchema`. `json.RawMessage` is the
   recommended `InputSchema` type for precise control of `additionalProperties:true`
   (architecture/mcp_sdk_api.md §5). All VERIFIED + PROVEN by PoC (research §2-§3).
2. **`Config` carries the v2 fields** (config.go, P1.M1.T1.S1 — DONE): `Tools`,
   `CanonicalTool`, `CanonicalParam`, `QueryAliases`, `OptionalAliases`,
   `TargetTool`, `TargetParam`. `DefaultConfig()` returns
   `Tools:["web_search"]`, `CanonicalTool:"web_search"`, `CanonicalParam:"query"`,
   `QueryAliases:[14 entries]`, `OptionalAliases:{location,content_size,
   search_recency_filter}`, `TargetTool:"web_search_prime"`.
3. **Config validation enforces the advertising invariants** (ResolveConfig,
   P1.M1.T1.S2 — DONE): Tools non-empty; `slices.Contains(cfg.Tools,
   cfg.CanonicalTool)`; `!slices.Contains(cfg.Tools, cfg.TargetTool)`. NOTE
   (load-bearing): it does NOT enforce `Tools[0]==CanonicalTool` — see "Key Design
   Decision" in the description and the gotcha below. buildTools trusts these
   invariants (validated cfg in production) but never references TargetTool itself.

## User Persona

**Target User**: (1) **P1.M5.T2.S1** (server dispatch) — consumes
   `buildTools(cfg)` in its registration loop:
   `for _, t := range buildTools(cfg) { server.AddTool(t, dispatchHandler) }`,
   wiring the SAME extract→delegate→teach handler to every advertised tool. The
   ONLY contract M5.T2 relies on is that each `*mcp.Tool` is AddTool-safe and the
   slice advertises exactly cfg.Tools. (2) **P1.M5.T3.S1** (server_test.go e2e) —
   PRD §19.3 case 5 ("tools/list advertises exactly Tools; only web_search carries
   a full description; never web_search_prime") asserts on the tools buildTools
   produced, via the registered server's tools/list. (3) **P1.M5.T4.S2** (doc.go
   rewrite) — may reference buildTools' doc comment for the advertised-surface
   description.

**Use Case**: The MCP client calls `tools/list` and sees the canonical `web_search`
   tool with a full description teaching `{ "query": "..." }` and a schema listing
   the accepted aliases/optionals. The agent calls `web_search` with
   `{"query":"rust async runtime"}` (canonical — no warning) or with `{"q":"..."}`
   (alias — extracted, forwarded, then a warning is appended by teach.go). If the
   operator configured a second name `search`, the client also sees a terse
   `search` tool pointing at `web_search`; both route to the same handler.

**Pain Points Addressed**: (1) PRD §9.1 — a wrong tool NAME is rejected
   client-side before any network hop, so to be catchable a name MUST be
   advertised; buildTools advertises exactly cfg.Tools. (2) PRD FR-3 — a strict
   client that validates against the schema must never reject any shape locally
   (that reproduces the layer-2 failure); the permissive schema (no `required`,
   `additionalProperties:true`) guarantees this. (3) PRD §9.3 — only the canonical
   tool pays the full context cost; aliases are terse (one-line desc + minimal
   schema).

## Why

- Implements **PRD §9** (Advertised tools and context minimization): §9.3 default
  `["web_search"]`, the canonical tool carries the full description + documented
  schema, additionally-configured tools are terse aliases, never z.ai-branded names.
- Implements **PRD FR-2** (one advertised tool + optional alias names) and **FR-3**
  (permissive schema: document, never enforce — `additionalProperties:true`, no
  `required`).
- Implements **PRD §9.4** (canonical surface = `web_search`/`query` + optionals
  `location`/`content_size`/`search_recency_filter`) as the documented schema.
- **Decouples tool DEFINITION from tool DISPATCH.** M5.T2 owns the handler
  (extract→delegate→teach); this item owns ONLY the advertised definitions. The two
  meet at the AddTool registration loop, whose contract (AddTool-safe schemas,
  exactly-cfg.Tools names) this item guarantees.
- **Completes the M5.T1 milestone** (advertised tool definitions): the single
  subtask. After it, M5.T2 can wire the handler and M5.T3 can run the e2e suite.

## What

Two new files. Visible behavior: none until M5.T2 registers the tools; then
`tools/list` advertises exactly `cfg.Tools`, with `web_search` carrying the full
description + permissive schema and any alias carrying a terse description + minimal
schema.

### Success Criteria

- [ ] `tools.go` exists, `package main`, imports only `encoding/json`, `fmt`,
      `github.com/modelcontextprotocol/go-sdk/mcp`.
- [ ] `func buildTools(cfg Config) []*mcp.Tool` iterates `cfg.Tools` IN ORDER; for
      the entry `== cfg.CanonicalTool` it appends a canonical `*mcp.Tool`
      (Name=CanonicalTool, full Description, `canonicalInputSchema(cfg)`); for every
      other entry it appends an alias `*mcp.Tool` (Name=entry, terse Description,
      `aliasInputSchema`).
- [ ] Canonical Description (default CanonicalParam "query") reads VERBATIM:
      `Performs a web search via z.ai. Call with { "query": "..." }. Accepts
      alternative parameter names but canonical is query.`
- [ ] Alias Description (default CanonicalTool "web_search") reads VERBATIM:
      `Performs a web search; alias of web_search.`
- [ ] `canonicalInputSchema(cfg)` returns `json.RawMessage` that decodes to
      `type:"object"`, `additionalProperties:true`, a `properties.<CanonicalParam>`
      (with a description), every other `cfg.QueryAliases` entry (≠ CanonicalParam)
      and every `cfg.OptionalAliases` key as optional string properties, and NO
      `"required"` key.
- [ ] `aliasInputSchema` is the package var
      `json.RawMessage(`{"type":"object","additionalProperties":true}`)` — shared by
      all alias tools, declaring NO `properties`.
- [ ] `buildTools` NEVER references `cfg.TargetTool` and NEVER emits a tool named
      `cfg.TargetTool`.
- [ ] Output order matches `cfg.Tools` (value-match selection preserves iteration
      order; PRD §19.3 case 5).
- [ ] Registering every built tool via `(*Server).AddTool` does not panic
      (`TestBuildTools_AddToolSafe`).
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean;
      `git diff --stat` shows ONLY `tools.go` + `tools_test.go`; `git diff go.mod`
      empty.
- [ ] No file other than `tools.go`/`tools_test.go` is created or edited.

## All Needed Context

### Context Completeness Check

_Pass._ An agent with no prior knowledge can implement from this PRP alone because:
(a) the exact SDK facts (`mcp.Tool` fields; `AddTool` panics on nil/non-object
schema; `json.RawMessage` recommended) are given with file:line citations AND
proven by a PoC (research §2-§3); (b) the full reference implementation for
`buildTools`, `canonicalDescription`, `aliasDescription`, `canonicalInputSchema`,
and the `aliasInputSchema` var is given verbatim; (c) the load-bearing design
decision (value-match canonical selection, with the exact reason config validation
does not enforce Tools[0]) is stated; (d) the exact Description strings (with the
default-config expected output) are given; (e) the permissive-schema rationale
(no `required`, `additionalProperties:true` — document never enforce, FR-3) is
justified; (f) the full test file with an AddTool-safe gate is given; (g) the
consumer seam (M5.T2's registration loop) and the e2e assertion (M5.T3 §19.3 case
5) are named so the implementer knows the downstream contract.

### Documentation & References

```yaml
# MUST READ — the rules this item implements.
- file: PRD.md
  section: "§9 Advertised tools and context minimization (9.3 Default and rules, 9.4 Canonical surface)"
  why: §9.3 = default ["web_search"]; canonical tool carries full description +
        documented schema; additionally-configured tools are terse aliases
        (one-line desc + minimal open schema {"type":"object","additionalProperties":true});
        NEVER advertise z.ai-branded names. §9.4 = canonical surface is web_search/query
        + optionals location/content_size/search_recency_filter.
  critical: the canonical tool is the ONLY one that pays the full context cost;
        aliases are terse. The no-z.ai-names rule is enforced by config validation
        upstream (DONE); buildTools trusts it.

- file: PRD.md
  section: "FR-2 One advertised tool (plus optional alias names)" + "FR-3 Permissive schema"
  why: FR-2 = advertise essentially one tool (web_search) with full description/schema;
        additional names route to the identical handler with a terse desc + minimal
        schema; never web_search_prime. FR-3 = declare query primary, list common
        aliases as optional, additionalProperties:true; a validating client must
        NEVER reject any shape locally; "The schema's job is to document the
        canonical form, not to enforce it."
  critical: FR-3 => the canonical schema has NO "required" array (required:["query"]
        would make a strict client reject {"q":"x"} — the exact failure we eliminate).

# MUST READ — the verified SDK API surface (the load-bearing AddTool contract).
- docfile: plan/002_0a8ab3410994/architecture/mcp_sdk_api.md
  section: "§1 SERVER SIDE (AddTool, Tool struct)" + "§5 INPUTSCHEMA"
  why: §1 = (*Server).AddTool(t *Tool, h ToolHandler) PANICS if InputSchema==nil
        and PANICS if type!="object" (server.go:242-260); the non-generic AddTool
        gives the handler RAW arguments (json.RawMessage) — exactly what extract.go
        consumes. mcp.Tool fields: Name, Description, InputSchema any (+ optional
        Annotations/Title/OutputSchema we leave unset). §5 = use json.RawMessage for
        precise control of additionalProperties:true; canonical schema example +
        alias schema `{"type":"object","additionalProperties":true}`.
  critical: EVERY schema buildTools emits MUST be a valid JSON-schema object or M5.T2
        crashes at startup. Proven AddTool-safe by PoC (research §3).

# MUST READ — the Config fields buildTools consumes (DONE, P1.M1.T1.S1).
- file: config.go
  section: "type Config struct" + "DefaultConfig"
  why: Config.Tools ([]string; Tools[0] conventionally canonical), CanonicalTool
        ("web_search"), CanonicalParam ("query"), QueryAliases (14-entry default),
        OptionalAliases (map: location/content_size/search_recency_filter -> aliases),
        TargetTool ("web_search_prime"; never advertised).
  pattern: DefaultConfig() is the canonical example config the tests build on.
  critical: ResolveConfig validates Tools non-empty + Contains(CanonicalTool) +
        !Contains(TargetTool), but does NOT enforce Tools[0]==CanonicalTool. Hence
        buildTools matches canonical by VALUE, not index.

# MUST READ — the config-validation that owns the no-z.ai-names rule (DONE, P1.M1.T1.S2).
- file: config.go
  section: "ResolveConfig — Tools validation block"
  why: the three Tools rules live here (non-empty; contains CanonicalTool; not
        contains TargetTool). buildTools does NOT re-validate; it trusts the
        validated cfg and never references TargetTool. A defensive test still pins
        the output invariant.
  gotcha: do NOT add a second Tools-validation pass in tools.go — that is config.go's
        job and is DONE. Duplication risks divergence.

# MUST READ — the teaching predicate (confirms value-match selection is safe).
- file: teach.go
  section: "shouldWarn(calledTool, result, canonicalTool, canonicalParam)"
  why: the warning predicate keys off the ACTUAL called tool (req.Params.Name) vs
        CanonicalTool, and result.Source vs CanonicalParam — INDEPENDENT of which
        Tools index is canonical. So selecting the canonical tool by value (not
        Tools[0]) has ZERO effect on the teaching signal. Confirms the design.

# MUST READ — the consumer (the registration loop this item feeds).
- file: plan/002_0a8ab3410994/P1M5T1S1/research/findings.md
  section: "§10 Consumer seam" + "§3 PROVEN PoC"
  why: M5.T2 does `for _, t := range buildTools(cfg) { server.AddTool(t, handler) }`
        with the SAME handler for every tool. The PoC (§3) proved both schemas are
        AddTool-safe. §5 (value-match rationale) and §6 (permissive-schema
        rationale) are the load-bearing design notes.

# SDK source (cited facts — no need to re-read; verified).
- url: https://pkg.go.dev/encoding/json#Marshal
  why: json.Marshal(map[string]any{...}) emits sorted keys + the bool true for
        additionalProperties; the top-level "type":"object" survives AddTool's
        remarshal check. Confirmed by PoC.
  critical: json.RawMessage is []byte — declare aliasInputSchema as a package VAR
        (not const). Marshal of map[string]any never errors for these primitive
        values (the defensive fallback is belt-and-suspenders).
```

### Current Codebase tree (run `ls` at repo root)

```bash
web-search-prime-fixer/
  go.mod / go.sum    # module web-search-prime-fixer; go 1.25.0; 1 require: go-sdk v1.6.1
  main.go            # authHeaderKey{} + authMiddleware + NewServer+StreamableHTTPHandler mount
                     #   (P1.M1.T2.S2) — UNTOUCHED (M5.T2 will wire buildTools here)
  config.go          # Config + DefaultConfig + ResolveConfig (v2 fields) — UNTOUCHED (consumed)
  extract.go         # extract + ToUpstreamArgs (P1.M2.T1) — UNTOUCHED (sibling; independent)
  teach.go           # shouldWarn + warningText + appendWarning (P1.M3.T1) — UNTOUCHED (sibling)
  upstream.go        # UpstreamClient.callTool (P1.M4.T1, parallel S3) — UNTOUCHED (sibling)
  logger.go          # *logger — UNTOUCHED
  health.go          # healthHandler — UNTOUCHED
  *_test.go (config/resolve/logger/health/extract/teach/upstream) — UNTOUCHED
  testdata/*.sse, README.md, config.example.json, PRD.md — UNTOUCHED
  doc.go             # STALE v1 text — UNTOUCHED (P1.M5.T4.S2 owns the rewrite)
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
tools.go            # CREATE (package main): func buildTools(cfg Config) []*mcp.Tool — the
                    #   advertised MCP tool definitions. Private helpers canonicalDescription,
                    #   aliasDescription, canonicalInputSchema. Package var aliasInputSchema.
                    #   Imports encoding/json, fmt, github.com/modelcontextprotocol/go-sdk/mcp.
                    #   Selects the canonical tool by VALUE (== cfg.CanonicalTool), iterates
                    #   cfg.Tools in order. Canonical tool: full desc + permissive documented
                    #   schema (no required). Alias tools: terse desc + minimal open schema.
                    #   Never references cfg.TargetTool.
tools_test.go       # CREATE (package main): TestBuildTools_DefaultCanonical,
                    #   TestBuildTools_AliasTool, TestBuildTools_NoTargetToolAdvertised,
                    #   TestBuildTools_OrderFollowsConfig, TestBuildTools_AddToolSafe (the
                    #   integration gate: AddTool does not panic), + mustDecodeSchema helper.
                    #   Imports context, encoding/json, testing, github.com/modelcontextprotocol/go-sdk/mcp.
```

No other file changes. `go.mod`/`go.sum` unchanged.

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — AddTool PANICS on a bad schema. (*Server).AddTool (server.go:238) panics if
  t.InputSchema == nil (server.go:242-247) and panics if the schema's type != "object"
  after remarshaling (server.go:253-260). buildTools is the ONLY producer of these
  schemas, and the consumer (M5.T2) calls AddTool at startup — a malformed schema would
  crash the server. => every schema MUST be {"type":"object",...}. PROVEN AddTool-safe by
  the PoC (research §3) for BOTH the dynamic canonical schema and the literal alias
  schema. TestBuildTools_AddToolSafe pins it at this layer so M5.T2 cannot regress it.

CRITICAL — select the canonical tool by VALUE, not by Tools[0]. ResolveConfig (config.go,
  DONE) enforces slices.Contains(cfg.Tools, cfg.CanonicalTool) but NOT Tools[0]==CanonicalTool.
  A config like Tools=["search","web_search"] passes validation with Tools[0]="search";
  treating Tools[0] as canonical would then build the canonical tool with the WRONG name and
  build the real CanonicalTool as a terse alias. => `if name == cfg.CanonicalTool` inside the
  loop. Output order still follows cfg.Tools (iterate in order). teach.shouldWarn keys off
  the actual called tool, so this is safe for the teaching signal (research §5, §9).

CRITICAL — the canonical schema must NOT have a "required" array. FR-3: "The schema's job
  is to document the canonical form, not to enforce it." A required:["query"] would make a
  strict client reject {"q":"x"} or {} locally — reproducing the exact layer-2 failure this
  server eliminates. additionalProperties:true + no required => any shape passes client
  validation; extract.go recovers the query at runtime.

CRITICAL — aliasInputSchema is a package VAR, not a const. json.RawMessage is a []byte;
  Go const cannot hold it. Declare `var aliasInputSchema = json.RawMessage(...)`. Reuse the
  SAME value for every alias tool (it is immutable in practice; sharing is fine).

GOTCHA — do NOT re-validate Tools in tools.go. The no-z.ai-names rule
  (!Contains(Tools, TargetTool)) and the CanonicalTool-in-Tools rule are config.go's job
  (DONE). buildTools trusts the validated cfg. Re-validating risks divergence. A defensive
  TEST (TestBuildTools_NoTargetToolAdvertised) pins the output invariant without adding
  production re-validation.

GOTCHA — json.Marshal sorts map keys. The canonical schema's properties are built as a
  map[string]any; json.Marshal emits them in sorted key order. This is fine: JSON-schema
  property order is semantically irrelevant, and tools/list is consumed by clients that
  treat properties as an unordered map. Do NOT try to force key order.

GOTCHA — the canonical description uses escaped quotes. Build it with a double-quoted Go
  string: fmt.Sprintf("Performs a web search via z.ai. Call with { \"%s\": \"...\" }.
  Accepts alternative parameter names but canonical is %s.", CanonicalParam, CanonicalParam).
  Backtick strings cannot embed the needed quotes cleanly. Verify the default output reads
  EXACTLY the item-quoted text (the test pins it byte-for-byte).

GOTCHA — InputSchema stays json.RawMessage after AddTool. AddTool's validation remarshals a
  COPY (server.go:254-260) and stores the tool as-is (server.go:277). So tests can decode
  tool.InputSchema.(json.RawMessage) both before AND after AddTool and assert on it.

GOTCHA — doc.go is STALE (v1 text). It says "local reverse proxy" and "Go standard library
  exclusively" — both now false. Do NOT touch it here; P1.M5.T4.S2 owns the rewrite.
```

## Implementation Blueprint

### Data models and structure

No new domain types. `buildTools` returns `[]*mcp.Tool` (SDK type, unchanged). One
package var (`aliasInputSchema`) and three private helpers. Deps: `encoding/json`,
`fmt`, `github.com/modelcontextprotocol/go-sdk/mcp` (all already transitively used
elsewhere; go.mod unchanged).

### Reference implementation (CREATE `tools.go`)

> Run `gofmt -w tools.go` after. Whole file is new.

```go
package main

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// aliasInputSchema is the minimal open JSON schema shared by every terse alias
// tool (PRD §9.3). It is type:object with additionalProperties:true so a
// validating client never rejects any arguments shape locally (FR-3); extraction
// (extract.go) recovers the query from whatever the agent actually sent. It is a
// package-level value (not a const) because json.RawMessage is a []byte. Declaring
// NO properties keeps the alias tool's advertised context minimal (PRD §9.1).
var aliasInputSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`)

// buildTools builds the advertised MCP tool definitions from cfg (PRD §9, FR-2,
// FR-3). It returns one *mcp.Tool per cfg.Tools entry, in cfg.Tools order, ready
// for (*Server).AddTool registration by the caller (P1.M5.T2):
//
//	for _, t := range buildTools(cfg) { server.AddTool(t, handler) }
//
//   - the CANONICAL tool (the entry equal to cfg.CanonicalTool) gets a full
//     Description (canonicalDescription) and a DOCUMENTED but PERMISSIVE
//     InputSchema (canonicalInputSchema): the canonical param primary + the
//     configured query aliases + the z.ai optionals as optional strings,
//     type:object, additionalProperties:true, and NO "required" array. The schema
//     DOCUMENTS the canonical form; it never ENFORCES it (FR-3), so a validating
//     client must never reject any arguments shape locally — that would reproduce
//     the exact layer-2 failure this server exists to eliminate.
//   - every OTHER entry is a TERSE ALIAS (PRD §9.3): a one-line Description
//     (aliasDescription: "Performs a web search; alias of <CanonicalTool>.") and
//     the minimal open schema (aliasInputSchema). All advertised tools route to
//     the SAME handler (registered by the caller); extraction normalizes whatever
//     the agent sent.
//
// NO-Z.AI-NAMES RULE (PRD §9.3, §3): buildTools never advertises cfg.TargetTool
// (z.ai's real name, "web_search_prime"). This is ENFORCED upstream by config
// validation (ResolveConfig, P1.M1.T1.S2 — DONE), which rejects any Tools entry
// equal to TargetTool; buildTools therefore never references TargetTool and trusts
// the validated config. A defensive test (TestBuildTools_NoTargetToolAdvertised)
// still pins the output invariant.
//
// CANONICAL SELECTION BY VALUE: the canonical tool is the entry equal to
// cfg.CanonicalTool, NOT necessarily Tools[0]. ResolveConfig guarantees
// slices.Contains(cfg.Tools, cfg.CanonicalTool) but does NOT enforce
// Tools[0]==CanonicalTool, so a reordered config (e.g. ["search","web_search"])
// would otherwise mislabel the canonical tool. Matching by value is robust while
// preserving cfg.Tools order in the output (PRD §19.3 case 5: tools/list
// advertises exactly Tools, in order). With the default config (Tools=["web_search"])
// Tools[0] is canonical, exactly as PRD §9.3 specifies.
func buildTools(cfg Config) []*mcp.Tool {
	tools := make([]*mcp.Tool, 0, len(cfg.Tools))
	for _, name := range cfg.Tools {
		if name == cfg.CanonicalTool {
			tools = append(tools, &mcp.Tool{
				Name:        cfg.CanonicalTool,
				Description: canonicalDescription(cfg),
				InputSchema: canonicalInputSchema(cfg),
			})
		} else {
			tools = append(tools, &mcp.Tool{
				Name:        name,
				Description: aliasDescription(cfg),
				InputSchema: aliasInputSchema,
			})
		}
	}
	return tools
}

// canonicalDescription is the full Description carried by the canonical tool
// (PRD §9.4). It names the canonical parameter so the agent learns the correct
// form. With the default CanonicalParam "query" it reads verbatim:
//
//	Performs a web search via z.ai. Call with { "query": "..." }. Accepts
//	alternative parameter names but canonical is query.
func canonicalDescription(cfg Config) string {
	return fmt.Sprintf(
		"Performs a web search via z.ai. Call with { \"%s\": \"...\" }. Accepts alternative parameter names but canonical is %s.",
		cfg.CanonicalParam, cfg.CanonicalParam,
	)
}

// aliasDescription is the one-line Description carried by every terse alias tool
// (PRD §9.3). It points the agent at the canonical tool. With the default
// CanonicalTool "web_search" it reads verbatim:
//
//	Performs a web search; alias of web_search.
func aliasDescription(cfg Config) string {
	return fmt.Sprintf("Performs a web search; alias of %s.", cfg.CanonicalTool)
}

// canonicalInputSchema builds the DOCUMENTED, permissive InputSchema for the
// canonical tool (PRD §9.4, FR-3). It returns json.RawMessage (recommended by
// architecture/mcp_sdk_api.md §5 for precise control of additionalProperties:true)
// declaring a JSON-schema object with:
//   - the canonical parameter (cfg.CanonicalParam) as the PRIMARY property — a
//     string with a description;
//   - each configured query alias (cfg.QueryAliases, excluding the canonical
//     param) as an OPTIONAL string;
//   - each z.ai optional parameter (the keys of cfg.OptionalAliases: location,
//     content_size, search_recency_filter) as an OPTIONAL string;
//   - type:"object" and additionalProperties:true.
//
// There is NO "required" array: per FR-3 the schema DOCUMENTS the canonical form
// but must never ENFORCE it, so a validating client never rejects {"q":"x"} or {}
// locally (that would reproduce the layer-2 failure this server eliminates).
//
// The result is a valid JSON-schema object, so (*Server).AddTool's validation
// (which panics on a nil or non-object InputSchema) accepts it — proven by a PoC
// (research §3). The exact property set is documentation only — extraction
// (extract.go) accepts any shape — so operators may trim cfg.QueryAliases to
// shrink the advertised schema (context minimization, PRD §9.1) without changing
// runtime behavior. The defensive fallback returns aliasInputSchema if Marshal
// somehow fails (it cannot for these primitive values).
func canonicalInputSchema(cfg Config) json.RawMessage {
	props := map[string]any{
		cfg.CanonicalParam: map[string]any{
			"type":        "string",
			"description": "The search query.",
		},
	}
	for _, a := range cfg.QueryAliases {
		if a == cfg.CanonicalParam {
			continue
		}
		if _, dup := props[a]; !dup {
			props[a] = map[string]any{"type": "string"}
		}
	}
	for opt := range cfg.OptionalAliases {
		if _, dup := props[opt]; !dup {
			props[opt] = map[string]any{"type": "string"}
		}
	}
	b, err := json.Marshal(map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": true,
	})
	if err != nil {
		return aliasInputSchema // defensive; unreachable for these primitive values
	}
	return json.RawMessage(b)
}
```

### Reference test file (CREATE `tools_test.go`)

> Run `gofmt -w tools_test.go` after. The AddTool-safe gate is the critical one.

```go
package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mustDecodeSchema decodes a tool's InputSchema (json.RawMessage) into out.
func mustDecodeSchema(t *testing.T, in any, out *map[string]any) {
	t.Helper()
	raw, ok := in.(json.RawMessage)
	if !ok {
		t.Fatalf("InputSchema is %T, want json.RawMessage", in)
	}
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode InputSchema: %v (%s)", err, raw)
	}
}

// (1) Default config -> exactly one tool, web_search, canonical (full desc +
// permissive documented schema). Pins the exact canonical Description text.
func TestBuildTools_DefaultCanonical(t *testing.T) {
	cfg := DefaultConfig()
	tools := buildTools(cfg)
	if len(tools) != 1 {
		t.Fatalf("len(tools) = %d, want 1 (default Tools=[web_search])", len(tools))
	}
	tl := tools[0]
	if tl.Name != "web_search" {
		t.Errorf("Name = %q, want web_search", tl.Name)
	}
	const wantDesc = `Performs a web search via z.ai. Call with { "query": "..." }. Accepts alternative parameter names but canonical is query.`
	if tl.Description != wantDesc {
		t.Errorf("Description = %q\nwant %q", tl.Description, wantDesc)
	}
	var schema map[string]any
	mustDecodeSchema(t, tl.InputSchema, &schema)
	if schema["type"] != "object" {
		t.Errorf("schema.type = %#v, want object", schema["type"])
	}
	if schema["additionalProperties"] != true {
		t.Errorf("schema.additionalProperties = %#v, want true (FR-3)", schema["additionalProperties"])
	}
	props, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema.properties = %#v, want a map", schema["properties"])
	}
	if props["query"] == nil {
		t.Error("schema missing primary 'query' property")
	}
	// z.ai optionals are documented as optional strings.
	for _, opt := range []string{"location", "content_size", "search_recency_filter"} {
		if props[opt] == nil {
			t.Errorf("schema missing z.ai optional %q", opt)
		}
	}
	// FR-3: the schema DOCUMENTS, never ENFORCES -> no "required" array.
	if _, hasReq := schema["required"]; hasReq {
		t.Error("schema has a 'required' array (FR-3 forbids enforcement)")
	}
}

// (2) A config with an extra alias tool -> that tool is terse (one-line desc +
// minimal open schema); the canonical tool stays full. Order follows cfg.Tools.
func TestBuildTools_AliasTool(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tools = []string{"web_search", "search"}
	tools := buildTools(cfg)
	if len(tools) != 2 {
		t.Fatalf("len(tools) = %d, want 2", len(tools))
	}
	if tools[0].Name != "web_search" || tools[1].Name != "search" {
		t.Fatalf("order/names = %q, %q; want web_search, search", tools[0].Name, tools[1].Name)
	}
	// canonical (index 0) keeps the full description (not the alias one-liner).
	if tools[0].Description == aliasDescription(cfg) {
		t.Errorf("canonical description collapsed to the alias one-liner: %q", tools[0].Description)
	}
	// alias (index 1) is terse.
	if tools[1].Description != "Performs a web search; alias of web_search." {
		t.Errorf("alias description = %q", tools[1].Description)
	}
	var alias map[string]any
	mustDecodeSchema(t, tools[1].InputSchema, &alias)
	if alias["type"] != "object" || alias["additionalProperties"] != true {
		t.Errorf("alias schema = %#v, want {type:object, additionalProperties:true}", alias)
	}
	if _, hasProps := alias["properties"]; hasProps {
		t.Error("alias schema should declare NO properties (minimal open schema, PRD §9.3)")
	}
}

// (3) buildTools NEVER advertises cfg.TargetTool (web_search_prime). Defensive
// guard for the no-z.ai-names rule (PRD §9.3) even though config validation
// enforces it upstream.
func TestBuildTools_NoTargetToolAdvertised(t *testing.T) {
	cfg := DefaultConfig()
	for _, tl := range buildTools(cfg) {
		if tl.Name == cfg.TargetTool {
			t.Errorf("buildTools advertised the target tool %q (z.ai's real name)", cfg.TargetTool)
		}
	}
}

// (4) Output order follows cfg.Tools even when the canonical tool is NOT first,
// AND the canonical tool is still the FULL one wherever it sits (value-match).
func TestBuildTools_OrderFollowsConfig(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tools = []string{"search", "web_search"} // canonical NOT first; passes ResolveConfig
	tools := buildTools(cfg)
	if len(tools) != 2 {
		t.Fatalf("len = %d, want 2", len(tools))
	}
	if tools[0].Name != "search" || tools[1].Name != "web_search" {
		t.Fatalf("order = %q,%q; want search,web_search (cfg.Tools order)", tools[0].Name, tools[1].Name)
	}
	// canonical (web_search, at index 1) is the FULL tool.
	var schema map[string]any
	mustDecodeSchema(t, tools[1].InputSchema, &schema)
	if schema["additionalProperties"] != true || schema["properties"] == nil {
		t.Errorf("canonical tool (index 1) missing full schema: %#v", schema)
	}
	// alias (search, at index 0) is terse.
	var alias map[string]any
	mustDecodeSchema(t, tools[0].InputSchema, &alias)
	if alias["properties"] != nil {
		t.Errorf("alias tool (index 0) got a non-minimal schema: %#v", alias)
	}
}

// (5) INTEGRATION GATE: the built tools are accepted by (*Server).AddTool without
// panicking. AddTool panics on a nil or non-object InputSchema (server.go:242-260),
// so this proves buildTools' schemas are well-formed — the contract M5.T2 relies on.
func TestBuildTools_AddToolSafe(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Tools = []string{"web_search", "search"}
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "v"}, nil)
	noop := func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	}
	for _, tl := range buildTools(cfg) {
		srv.AddTool(tl, noop) // a panic fails the test
	}
}
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: (PREREQUISITE) verify the SDK + Config state
  - RUN: grep -q "modelcontextprotocol/go-sdk v1.6.1" go.mod \
        && grep -q "CanonicalTool" config.go \
        && grep -q "TargetTool" config.go \
        && grep -q "OptionalAliases" config.go \
        && ! -f tools.go \
        && echo OK
  - EXPECT: OK. IF ANY FAIL: STOP — a prerequisite (SDK dep, P1.M1.T1.S1 config) has
        not run, or tools.go already exists (collision).

Task 1: CREATE tools.go (package main + imports + aliasInputSchema var)
  - FILE: tools.go. IMPORTS: encoding/json, fmt, github.com/modelcontextprotocol/go-sdk/mcp.
  - ADD: var aliasInputSchema = json.RawMessage(`{"type":"object","additionalProperties":true}`).
  - NAMING: buildTools (exported; consumed by M5.T2); canonicalDescription /
        aliasDescription / canonicalInputSchema (private).

Task 2: IMPLEMENT buildTools (value-match canonical selection)
  - FILE: tools.go. Loop cfg.Tools IN ORDER; entry == cfg.CanonicalTool -> canonical
        *mcp.Tool (Name=CanonicalTool, canonicalDescription, canonicalInputSchema);
        else alias *mcp.Tool (Name=entry, aliasDescription, aliasInputSchema).
  - CONSTRAINTS: NEVER reference cfg.TargetTool. Output order == cfg.Tools order.

Task 3: IMPLEMENT the three helpers (canonicalDescription, aliasDescription, canonicalInputSchema)
  - canonicalDescription: fmt.Sprintf with two %s = cfg.CanonicalParam (escaped quotes).
  - aliasDescription: fmt.Sprintf with cfg.CanonicalTool.
  - canonicalInputSchema: build props map (CanonicalParam primary + desc; each QueryAlias
        ≠CanonicalParam; each OptionalAliases key), Marshal {type:object, properties,
        additionalProperties:true}, return json.RawMessage; defensive fallback aliasInputSchema.
  - CONSTRAINTS: NO "required" array in the canonical schema (FR-3).

Task 4: CREATE tools_test.go + the five tests + mustDecodeSchema
  - FILE: tools_test.go. IMPORTS: context, encoding/json, testing, go-sdk/mcp.
  - TESTS: TestBuildTools_DefaultCanonical (exact canonical desc text + schema shape, no
        required), TestBuildTools_AliasTool (terse alias + minimal schema), 
        TestBuildTools_NoTargetToolAdvertised, TestBuildTools_OrderFollowsConfig (value-match
        robustness), TestBuildTools_AddToolSafe (the integration gate).

Task 5: VALIDATE
  - gofmt -w tools.go tools_test.go
  - go vet ./...
  - go build ./...
  - go test -run 'BuildTools' -v          # the five new tests
  - go test ./...                         # full suite green (no regressions; additive)
  - git diff --stat                       # expect ONLY tools.go + tools_test.go
  - git diff go.mod                       # expect EMPTY
  - go doc . buildTools                   # Mode A: canonical tool, permissive-schema, no-z.ai-names
```

### Implementation Patterns & Key Details

```go
// PATTERN: pure builder, no I/O. buildTools is a pure function of cfg; it builds
// []*mcp.Tool and returns them. It does NOT register (M5.T2 owns AddTool), does NOT
// validate (config.go owns that, DONE), and does NOT touch cfg.TargetTool.

// PATTERN: value-match canonical selection. The canonical tool is the entry equal to
// cfg.CanonicalTool, found while iterating cfg.Tools in order. This is robust to a
// reordered Tools list (which ResolveConfig permits) and preserves cfg.Tools order in
// the output (PRD §19.3 case 5). Default config: Tools[0]==CanonicalTool, so behavior
// matches the item's "Tools[0] is canonical" framing.

// PATTERN: json.RawMessage for precise schema control (architecture §5). Build the
// canonical schema as a map[string]any, json.Marshal it, wrap in json.RawMessage.
// AddTool's remarshal (server.go:254-260) re-decodes it and confirms type=="object".
// Proven AddTool-safe by PoC. The alias schema is a shared literal RawMessage var.

// PATTERN: document, never enforce (FR-3). The canonical schema declares properties
// (query primary + aliases + optionals) for DOCUMENTATION but sets additionalProperties:
// true and omits "required", so a validating client never rejects any shape. Runtime
// acceptance is extract.go's job, not the schema's.

// GOTCHA (restated): AddTool PANICS on nil/non-object schema. Every schema MUST be
// {"type":"object",...}. TestBuildTools_AddToolSafe pins this at the buildTools layer.

// GOTCHA (restated): no "required" array in the canonical schema. required:["query"]
// would make a strict client reject {"q":"x"} — the exact layer-2 failure we eliminate.

// GOTCHA (restated): aliasInputSchema is a VAR (json.RawMessage is []byte), shared by
// all alias tools.

// GOTCHA (restated): do NOT re-validate Tools in tools.go (config.go owns it, DONE).
// A defensive test pins the no-TargetTool output invariant without production duplication.
```

### Integration Points

```yaml
FILES CREATED:
  - tools.go        (package main: buildTools + canonicalDescription + aliasDescription +
                canonicalInputSchema + aliasInputSchema var. Imports encoding/json, fmt,
                go-sdk/mcp. Value-match canonical selection; permissive documented schema;
                terse alias schema; never references TargetTool.)
  - tools_test.go   (package main: 5 tests + mustDecodeSchema. Imports context,
                encoding/json, testing, go-sdk/mcp.)
FILES NOT TOUCHED (contract):
  - go.mod / go.sum: unchanged (encoding/json, fmt, mcp all already used elsewhere).
  - config.go / main.go / extract.go / teach.go / upstream.go / logger.go / health.go:
        untouched (consumed, not edited).
  - all *_test.go / testdata / README.md / config.example.json / PRD.md / doc.go: untouched.
CONSUMER SEAM (keep stable for M5.T2):
  - func buildTools(cfg Config) []*mcp.Tool — the registration-loop input. Contract:
        each *mcp.Tool is AddTool-safe; the slice advertises exactly cfg.Tools in order;
        no tool is named cfg.TargetTool. M5.T2 does `for _, t := range buildTools(cfg) {
        server.AddTool(t, dispatchHandler) }` with ONE shared handler.
DATABASE / ROUTES / ENV / CONFIG: none.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
gofmt -w tools.go tools_test.go
go vet ./...
go build ./...
git diff --stat     # expect: tools.go + tools_test.go ONLY (both new)
git diff go.mod     # expect: EMPTY (no new requires; encoding/json+fmt+mcp already used)

# Expected: gofmt clean; vet clean; build clean; only the two new files; go.mod unchanged.
```

### Level 2: Unit Tests (buildTools component validation)

```bash
go test -run 'BuildTools' -v

# MUST PASS (prove the contract):
#   TestBuildTools_DefaultCanonical      -> 1 tool web_search; exact canonical Description
#                                           text; schema type:object + additionalProperties:true
#                                           + query + 3 optionals + NO required.
#   TestBuildTools_AliasTool             -> 2 tools; canonical full, alias terse (one-line desc
#                                           + {"type":"object","additionalProperties":true}, no
#                                           properties).
#   TestBuildTools_NoTargetToolAdvertised-> no built tool named cfg.TargetTool.
#   TestBuildTools_OrderFollowsConfig    -> output order == cfg.Tools; canonical is full even
#                                           when not first (value-match).
#   TestBuildTools_AddToolSafe           -> registering every built tool via (*Server).AddTool
#                                           does not panic (the integration gate).
# Expected: PASS, exit 0. If AddToolSafe panics, a schema is malformed (nil or non-object) —
#   fix canonicalInputSchema/aliasInputSchema before anything else. If DefaultCanonical fails
#   on the Description text, the escaped-quote Sprintf is wrong. If it fails on "required",
#   you accidentally added a required array (FR-3 violation).
```

### Level 3: Integration Testing (regression + full suite)

```bash
# Full suite — buildTools is additive (new file, new symbols); nothing else changes.
go test ./...

# Expected: ALL green, exit 0. If a "redeclared in this block" compile error appears, a new
# symbol collided with an existing one (rename it). The sibling suites (extract/teach/upstream)
# must be unaffected — buildTools is a new, independent module.

# Confirm the consumer contract is real (M5.T2 will use exactly this loop):
grep -n "func buildTools" tools.go
go doc . buildTools     # Mode A: prints the canonical-tool / permissive-schema / no-z.ai-names doc
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Prove the permissive schema never rejects a non-canonical shape at the schema layer:
# decode the canonical schema and assert there is no "required" + additionalProperties==true.
go test -run 'TestBuildTools_DefaultCanonical' -v

# Prove no z.ai-branded name leaks into tools/list's advertised surface:
go test -run 'TestBuildTools_NoTargetToolAdvertised' -v

# (Optional) Confirm the schema a client would SEE over tools/list by marshaling one tool:
# this is exactly what M5.T3's §19.3 case 5 asserts end-to-end.
go test -run 'TestBuildTools_AliasTool' -v

# Expected: the canonical schema documents query + aliases + optionals with additionalProperties:
# true and no required (FR-3); no tool is named web_search_prime (§9.3); aliases are terse.
# buildTools is ready for M5.T2 to register and M5.T3 to assert via tools/list.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 clean: `gofmt -l .`, `go vet ./...`, `go build ./...`, `git diff --stat`
      (only tools.go + tools_test.go), `git diff go.mod` (empty).
- [ ] Level 2 passes: `go test -run 'BuildTools' -v` (all five tests green).
- [ ] Level 3 passes: `go test ./...` fully green (additive; no regression).
- [ ] Level 4 passes: canonical schema has no `required` + `additionalProperties:true`; no
      `web_search_prime` advertised.

### Feature Validation

- [ ] Default config → exactly one tool, `web_search`, canonical (full desc + permissive
      documented schema); canonical Description matches the item-quoted text byte-for-byte.
- [ ] A `Tools=["web_search","search"]` config → canonical (full) + terse alias (minimal
      open schema, no properties).
- [ ] Canonical tool selected by VALUE (robust to Tools[0] != CanonicalTool); output order
      follows cfg.Tools.
- [ ] No built tool is ever named `cfg.TargetTool`.
- [ ] Every built tool is `AddTool`-safe (no panic).

### Code Quality Validation

- [ ] Follows existing conventions: pure function in its own file (like extract.go's pure
      `extract`, teach.go's pure `warningText`); unexported helpers; doc comments citing PRD
      §9/FR-2/FR-3.
- [ ] File placement matches the desired tree (new files `tools.go` + `tools_test.go`).
- [ ] Anti-patterns avoided (see below): no schema enforcement (`required`); no Tools[0]
      assumption; no TargetTool reference; no production re-validation of config invariants.
- [ ] No new dependencies (`go.mod` unchanged).

### Documentation & Deployment

- [ ] Doc comments on `buildTools` (canonical tool, terse aliases, no-z.ai-names rule,
      value-match selection) and `canonicalInputSchema` (document-not-enforce rationale, FR-3).
- [ ] `go doc . buildTools` prints the contract M5.T2 + M5.T4 consume.
- [ ] **Mode A**: this item ships its doc comments inline; no README/doc.go change required
      (doc.go is owned by P1.M5.T4.S2; README by P1.M5.T4.S1).

---

## Anti-Patterns to Avoid

- ❌ Don't add a `"required"` array to the canonical schema — FR-3 forbids enforcement; a
  strict client must never reject `{"q":"x"}` or `{}` locally.
- ❌ Don't select the canonical tool by `Tools[0]` index — config validation does not enforce
  `Tools[0]==CanonicalTool`; match by value (`name == cfg.CanonicalTool`).
- ❌ Don't emit a schema that is not `{"type":"object",...}` — `AddTool` PANICS on nil or
  non-object `InputSchema`, crashing the server at M5.T2 startup. The PoC + the AddTool-safe
  test guard this.
- ❌ Don't reference `cfg.TargetTool` in `buildTools` — the no-z.ai-names rule is config
  validation's job (DONE); buildTools trusts the validated config.
- ❌ Don't re-validate `Tools` in `tools.go` (non-empty / contains CanonicalTool / not
  contains TargetTool) — that is `config.go`'s responsibility and is DONE; duplicating it
  risks divergence. A defensive TEST is enough.
- ❌ Don't declare `aliasInputSchema` as a `const` — `json.RawMessage` is a `[]byte`; use a
  package `var`.
- ❌ Don't build the canonical Description with a backtick string — the embedded quotes need
  escaping; use a double-quoted `fmt.Sprintf` with `\"%s\"`.
- ❌ Don't touch `doc.go` (stale v1 text) or `config.example.json` (stale v1 keys) — those
  rewrites are P1.M5.T4.S2's and P1.M5.T4.S2's scope respectively.
- ❌ Don't add any third-party dependency — `go.mod` stays at the single SDK require.

---

## Confidence Score

**9/10** for one-pass implementation success. The whole deliverable is two new files whose
reference implementation + test file are given verbatim; the single biggest risk (a schema
that panics `AddTool`) was PROVEN AddTool-safe by a throwaway PoC this session (PASS) and is
pinned by `TestBuildTools_AddToolSafe`; the load-bearing design decision (value-match
canonical selection) is justified against the actual config-validation behavior; and the
exact Description strings + schema shape are pinned byte-for-byte by tests. Deducted 1 point
for the residual ambiguity in PRD §9.3 "the common aliases" (interpreted as the full
configured `QueryAliases` set per FR-3's "list the common aliases as optional" — a defensible
reading, but an operator preferring a terser schema could trim `QueryAliases`, which this
design supports transparently since the property list is pure documentation).
