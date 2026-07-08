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
//		for _, t := range buildTools(cfg) { server.AddTool(t, handler) }
//
//	  - the CANONICAL tool (the entry equal to cfg.CanonicalTool) gets a full
//	    Description (canonicalDescription) and a DOCUMENTED but PERMISSIVE
//	    InputSchema (canonicalInputSchema): the canonical param primary + the
//	    configured query aliases + the z.ai optionals as optional strings,
//	    type:object, additionalProperties:true, and NO "required" array. The schema
//	    DOCUMENTS the canonical form; it never ENFORCES it (FR-3), so a validating
//	    client must never reject any arguments shape locally — that would reproduce
//	    the exact layer-2 failure this server exists to eliminate.
//	  - every OTHER entry is a TERSE ALIAS (PRD §9.3): a one-line Description
//	    (aliasDescription: "Performs a web search; alias of <CanonicalTool>.") and
//	    the minimal open schema (aliasInputSchema). All advertised tools route to
//	    the SAME handler (registered by the caller); extraction normalizes whatever
//	    the agent sent.
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
