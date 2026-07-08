package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

// Local aliases mirror DefaultConfig() so the test stays decoupled from
// config.go (extract is a pure function taking plain params).
var extractQA = []string{
	"query", "search_query", "q", "search", "searchQuery",
	"search_term", "term", "text", "input", "prompt",
	"question", "keywords", "topic", "searchString",
}
var extractOpt = map[string][]string{
	"location":              {"country", "region"},
	"content_size":          {"size", "contentSize", "detail"},
	"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
}

func TestExtract(t *testing.T) {
	tests := []struct {
		name string
		raw  string // string form of json.RawMessage; "" means pass a nil RawMessage
		want ExtractionResult
	}{
		// Level 1: bare string.
		{"bare_string", `"hello"`, ExtractionResult{Query: "hello", Source: "bare-string", Found: true}},
		// Level 2: scalar (number/bool) via fmt.Sprint.
		{"scalar_int", `42`, ExtractionResult{Query: "42", Source: "scalar", Found: true}},
		{"scalar_float", `3.14`, ExtractionResult{Query: "3.14", Source: "scalar", Found: true}},
		{"scalar_bool_true", `true`, ExtractionResult{Query: "true", Source: "scalar", Found: true}},
		{"scalar_bool_false", `false`, ExtractionResult{Query: "false", Source: "scalar", Found: true}},
		// Level 3: array -> first element that yields, source "array[0]".
		{"array_string", `["x"]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_first_yields", `[42,"x"]`, ExtractionResult{Query: "42", Source: "array[0]", Found: true}},
		{"array_skips_nonyielding_first", `[[],"x"]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_nested", `[["x"]]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		{"array_scalar_elem", `[true]`, ExtractionResult{Query: "true", Source: "array[0]", Found: true}},
		// Failure paths (Found=false -> zero value).
		{"null", `null`, ExtractionResult{}},
		{"empty_array", `[]`, ExtractionResult{}},
		{"empty_raw", ``, ExtractionResult{}},
		{"invalid_json", `{bad`, ExtractionResult{}},
		// S1<->S2 boundary: object level-4 is STUBBED (Found=false) until S2.
		{"empty_object", `{}`, ExtractionResult{}},
		{"array_of_objects", `[{"query":"x"}]`, ExtractionResult{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			got := extract(raw, extractQA, extractOpt)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("extract = %+v\nwant     %+v", got, tc.want)
			}
		})
	}
}

func TestExtract_PureAndDeterministic(t *testing.T) {
	// Same input twice -> identical result (pure/deterministic).
	raw := json.RawMessage(`["x"]`)
	a := extract(raw, extractQA, extractOpt)
	b := extract(raw, extractQA, extractOpt)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("nondeterministic: %+v vs %+v", a, b)
	}
	// Nil raw (arguments field omitted entirely).
	if got := extract(nil, extractQA, extractOpt); got.Found {
		t.Fatalf("nil raw -> Found=true, want false: %+v", got)
	}
}
