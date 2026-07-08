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
