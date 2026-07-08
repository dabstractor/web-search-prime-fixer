package main

import (
	"reflect"
	"testing"
)

// Local decoupling constants mirror DefaultConfig() so teach_test.go stays
// decoupled from config.go (teach functions are pure, taking plain params).
// The Level 3 smoke test asserts these match the real DefaultConfig values.
const (
	teachCanonTool  = "web_search" // mirrors DefaultConfig().CanonicalTool
	teachCanonParam = "query"      // mirrors DefaultConfig().CanonicalParam
)

func TestShouldWarn(t *testing.T) {
	tests := []struct {
		name       string
		calledTool string
		src        string
		opt        map[string]any
		canonTool  string
		canonParam string
		want       bool
	}{
		{"canonical", "web_search", "query", nil, "web_search", "query", false},
		{"canonical_empty_opt_map", "web_search", "query", map[string]any{}, "web_search", "query", false},
		{"wrong_tool_case", "Web_search", "query", nil, "web_search", "query", true},
		{"wrong_tool_other", "search", "query", nil, "web_search", "query", true},
		{"source_alias_q", "web_search", "q", nil, "web_search", "query", true},
		{"source_alias_search", "web_search", "search", nil, "web_search", "query", true},
		{"source_nested", "web_search", "nested:query", nil, "web_search", "query", true},
		{"source_inferred", "web_search", "inferred:messages[0].content", nil, "web_search", "query", true},
		{"source_bare_string", "web_search", "bare-string", nil, "web_search", "query", true},
		{"source_scalar", "web_search", "scalar", nil, "web_search", "query", true},
		{"source_array", "web_search", "array[0]", nil, "web_search", "query", true},
		{"optionals_present", "web_search", "query", map[string]any{"location": "US"}, "web_search", "query", true},
		{"optionals_wrong_tool", "search", "q", map[string]any{"location": "US"}, "web_search", "query", true},
		{"custom_canonical_match", "find", "q", nil, "find", "q", false},
		{"custom_canonical_mismatch", "find", "query", nil, "find", "q", true},
		// empty_source_boundary: a Found==false result has Source==""; shouldWarn
		// returns true (Source != canonicalParam), but the caller never reaches
		// shouldWarn for !Found — it takes the noQueryWarningText path (§12.3).
		{"empty_source_boundary", "web_search", "", nil, "web_search", "query", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := ExtractionResult{Source: tc.src, Optionals: tc.opt}
			if got := shouldWarn(tc.calledTool, res, tc.canonTool, tc.canonParam); got != tc.want {
				t.Errorf("shouldWarn(%q, %+v, %q, %q) = %v, want %v", tc.calledTool, res, tc.canonTool, tc.canonParam, got, tc.want)
			}
		})
	}
}

func TestWarningText(t *testing.T) {
	tests := []struct {
		name       string
		calledTool string
		source     string
		canonTool  string
		canonParam string
		want       string
	}{
		{
			"defaults_q",
			"web_search", "q", "web_search", "query",
			`[web-search-prime-fixer] Warning: this call used "web_search"/"q" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).`,
		},
		{
			"defaults_inferred",
			"web_search", "inferred:messages[0].content", "web_search", "query",
			`[web-search-prime-fixer] Warning: this call used "web_search"/"inferred:messages[0].content" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).`,
		},
		{
			"wrong_tool_name",
			"Web_search", "query", "web_search", "query",
			`[web-search-prime-fixer] Warning: this call used "Web_search"/"query" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).`,
		},
		{
			"custom_canonical",
			"search", "q", "search_tool", "q",
			`[web-search-prime-fixer] Warning: this call used "search"/"q" rather than the canonical form. Results are above. Next time call: search_tool with { "q": "..." } — e.g. search_tool({ "q": "rust async runtime" }).`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := warningText(tc.calledTool, tc.source, tc.canonTool, tc.canonParam)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("warningText =\n %q\nwant\n %q", got, tc.want)
			}
		})
	}
}

func TestNoQueryWarningText(t *testing.T) {
	tests := []struct {
		name       string
		canonTool  string
		canonParam string
		want       string
	}{
		{
			"defaults",
			"web_search", "query",
			`[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).`,
		},
		{
			"custom_canonical",
			"search_tool", "q",
			`[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: search_tool with { "q": "..." } — e.g. search_tool({ "q": "rust async runtime" }).`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := noQueryWarningText(tc.canonTool, tc.canonParam)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("noQueryWarningText =\n %q\nwant\n %q", got, tc.want)
			}
		})
	}
}
