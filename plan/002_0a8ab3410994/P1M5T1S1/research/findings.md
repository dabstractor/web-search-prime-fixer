# P1.M5.T1.S1 — Research Findings

## 1. What this item builds

`func buildTools(cfg Config) []*mcp.Tool` in a NEW file `tools.go` (package main).
Iterates `cfg.Tools`; the entry == `cfg.CanonicalTool` becomes the FULL canonical
tool (full Description + documented permissive InputSchema); every other entry is
a TERSE alias (one-line Description + minimal open schema). Consumed by P1.M5.T2.S1
which does `for _, t := range buildTools(cfg) { server.AddTool(t, dispatchHandler) }`.

## 2. The SDK `mcp.Tool` + `AddTool` contract (verified from v1.6.1 source)

- `type Tool struct` (protocol.go:1295): `Meta`, `Annotations *ToolAnnotations`,
  `Description string`, `InputSchema any`, `Name string`, `OutputSchema any`,
  `Title string`, `Icons []Icon`. We set Name, Description, InputSchema only.
- `InputSchema any` accepts `json.RawMessage` (recommended), `map[string]any`, or
  `*jsonschema.Schema`.
- `(*Server).AddTool(t *Tool, h ToolHandler)` (server.go:238): **PANICS** if
  `t.InputSchema == nil` (server.go:242-247); **PANICS** if, after remarshaling the
  schema to a map, `m["type"] != "object"` (server.go:253-260). `OutputSchema` left
  nil. Tool-name validity is a logged ERROR (tool.go:109 validateToolName), NOT a
  panic. => every schema buildTools emits MUST be `{"type":"object",...}` or the
  consumer (M5.T2) crashes at startup. This is the load-bearing constraint.
- The non-generic `AddTool(t, h)` is the form we use: the handler gets RAW arguments
  (`req.Params.Arguments` is `json.RawMessage`) — exactly what extract.go consumes.

## 3. PROVEN by throwaway PoC (this session)

Built the dynamic schema (map → json.Marshal → json.RawMessage) from DefaultConfig
and called `srv.AddTool` for the canonical tool + an alias tool. **PASS, no panic.**
Schema decode-back: `type=="object"`, `additionalProperties==true`, 17 properties
(query primary + 13 other QueryAliases + 3 OptionalAliases keys: location,
content_size, search_recency_filter). Both the dynamic canonical schema and the
literal alias schema `{"type":"object","additionalProperties":true}` are AddTool-safe.
This removes the single biggest risk (a schema that panics AddTool at M5.T2 startup).

## 4. Config fields consumed (config.go, P1.M1.T1.S1 — DONE)

- `Tools []string` — advertised names. Default `["web_search"]`.
- `CanonicalTool string` — default `"web_search"`.
- `CanonicalParam string` — default `"query"`.
- `QueryAliases []string` — default 14 entries (query, search_query, q, search,
  searchQuery, search_term, term, text, input, prompt, question, keywords, topic,
  searchString).
- `OptionalAliases map[string][]string` — keys: location, content_size,
  search_recency_filter (each with client-facing aliases).
- `TargetTool string` — `"web_search_prime"` (z.ai's real name; NEVER advertised).

## 5. CRITICAL design decision: select canonical by VALUE, not by Tools[0] index

The item says "Tools[0] (the canonical tool, must == CanonicalTool)". BUT config
validation (ResolveConfig, P1.M1.T1.S2 — DONE) only enforces
`slices.Contains(cfg.Tools, cfg.CanonicalTool)`, NOT `Tools[0]==CanonicalTool`. A
config like `Tools=["search","web_search"]` PASSES validation yet has
Tools[0]="search". Blindly treating Tools[0] as canonical would then build the
canonical tool with the WRONG name and build the real CanonicalTool as a terse
alias — a silent bug. => buildTools selects the canonical entry by VALUE
(`name == cfg.CanonicalTool`), iterating cfg.Tools IN ORDER so output order still
matches cfg.Tools (PRD §19.3 case 5: "tools/list advertises exactly Tools"). With
the default config Tools[0]==CanonicalTool, so behavior matches the item's
framing. Robust + faithful.

## 6. The permissive schema rationale (FR-3) — DOCUMENT, never ENFORCE

PRD FR-3 (verbatim): "declare query as the primary parameter, list the common
aliases as optional, and set additionalProperties: true. A client that validates
arguments against the schema must never reject { "query": ... } — or any other
shape — locally, because that would reproduce the exact layer-2 failure we are
eliminating. The schema's job is to *document* the canonical form, not to *enforce*
it." => canonical schema has NO `"required"` array (a required:["query"] would make
a strict client reject `{"q":"x"}`). Properties are pure documentation; runtime
acceptance is governed by extract.go + additionalProperties:true, not by the schema.

## 7. Description texts (item-specified; made dynamic with cfg)

- Canonical: with default CanonicalParam "query" reads EXACTLY the item's text —
  `Performs a web search via z.ai. Call with { "query": "..." }. Accepts
  alternative parameter names but canonical is query.`
  Built via `fmt.Sprintf("Performs a web search via z.ai. Call with { \"%s\": \"...\" }.
  Accepts alternative parameter names but canonical is %s.", CanonicalParam, CanonicalParam)`.
- Alias: `Performs a web search; alias of <CanonicalTool>.` (default → "alias of
  web_search."). Built via `fmt.Sprintf("Performs a web search; alias of %s.",
  CanonicalTool)`.

## 8. No-z.ai-names rule is NOT buildTools' enforcement job

PRD §9.3 / §3: "Never advertise z.ai-branded names (web_search_prime)." This is
ENFORCED upstream by ResolveConfig (config.go: `slices.Contains(cfg.Tools,
cfg.TargetTool)` → error). buildTools NEVER references cfg.TargetTool and trusts
the validated config. A defensive test still pins it (buildTools output contains
no tool named cfg.TargetTool).

## 9. Consistency with siblings (extract.go, teach.go)

- teach.shouldWarn(calledTool, result, canonicalTool, canonicalParam): the teaching
  predicate keys off the ACTUAL called tool vs CanonicalTool, and result.Source vs
  CanonicalParam — INDEPENDENT of which Tools index is canonical. So value-match
  selection has zero effect on the teaching signal. Confirms the design is safe.
- extract.go ToUpstreamArgs(targetParam): builds {targetParam: Query} + optionals
  for the upstream call. Independent of the advertised schema. buildTools is
  DOCUMENTATION-only; it does not change extraction.

## 10. Consumer seam + downstream testing

- M5.T2.S1: `for _, t := range buildTools(cfg) { server.AddTool(t, handler) }` —
  the SAME dispatch handler for every tool. buildTools returns []*mcp.Tool ready
  for this loop; each tool's InputSchema must be AddTool-safe (verified §3).
- PRD §19.3 case 5 ("tools/list advertises exactly Tools; only web_search carries
  a full description; never web_search_prime") is the E2E assertion in M5.T3.S1.
  This item ships a FOCUSED tools_test.go (buildTools unit tests + an AddTool-safe
  gate) — distinct from the e2e tools/list test.
- doc.go is STALE v1 text ("local reverse proxy", "Go standard library exclusively")
  — owned by P1.M5.T4.S2; this item does NOT touch it.

## 11. File plan

NEW `tools.go` + NEW `tools_test.go` (both package main). ZERO edits to existing
files. go.mod/go.sum unchanged (encoding/json, fmt, mcp all already imported
elsewhere). Imports for tools.go: `encoding/json`, `fmt`,
`github.com/modelcontextprotocol/go-sdk/mcp`. tools_test.go adds `context`,
`testing`.
