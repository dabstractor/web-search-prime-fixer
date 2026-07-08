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
		// S2 flips the array-of-objects boundary: the object case now yields and
		// the array case overrides Source to "array[0]".
		{"array_of_objects", `[{"query":"x"}]`, ExtractionResult{Query: "x", Source: "array[0]", Found: true}},
		// Level 4a: object alias scan (shallow string -> Source=<matched alias key>).
		{"shallow_canonical", `{"query":"x"}`, ExtractionResult{Query: "x", Source: "query", Found: true}},
		{"shallow_noncanonical_q", `{"q":"x"}`, ExtractionResult{Query: "x", Source: "q", Found: true}},
		{"shallow_search", `{"search":"x"}`, ExtractionResult{Query: "x", Source: "search", Found: true}},
		// Level 4a: object alias scan (shallow scalar -> Source="scalar").
		{"shallow_number", `{"query":42}`, ExtractionResult{Query: "42", Source: "scalar", Found: true}},
		{"shallow_bool", `{"query":true}`, ExtractionResult{Query: "true", Source: "scalar", Found: true}},
		// Level 4a: nested drill-in (map/array value -> Source="nested:<alias key>").
		{"nested_wrapper", `{"input":{"query":"x"}}`, ExtractionResult{Query: "x", Source: "nested:input", Found: true}},
		{"nested_value_is_object", `{"query":{"text":"x"}}`, ExtractionResult{Query: "x", Source: "nested:query", Found: true}},
		{"nested_subkey_priority", `{"query":{"text":"first","value":"second"}}`, ExtractionResult{Query: "first", Source: "nested:query", Found: true}},
		{"nested_array_value", `{"query":["x","y"]}`, ExtractionResult{Query: "x", Source: "nested:query", Found: true}},
		{"nested_array_of_objects", `{"query":[{"text":"x"}]}`, ExtractionResult{Query: "x", Source: "nested:query", Found: true}},
		{"nested_descent_nonpriority", `{"query":{"foobar":"x"}}`, ExtractionResult{Query: "x", Source: "nested:query", Found: true}},
		{"nested_descent_sorted", `{"query":{"zebra":"z","apple":"a"}}`, ExtractionResult{Query: "a", Source: "nested:query", Found: true}},
		{"nested_descent_deep", `{"query":{"a":{"b":"deep"}}}`, ExtractionResult{Query: "deep", Source: "nested:query", Found: true}},
		// Determinism: config slice order decides which alias wins, never map order.
		{"config_order", `{"query":"a","q":"b"}`, ExtractionResult{Query: "a", Source: "query", Found: true}},
		{"map_order_irrelevant", `{"q":"b","query":"a"}`, ExtractionResult{Query: "a", Source: "query", Found: true}},
		{"drill_continuation", `{"query":{},"q":"x"}`, ExtractionResult{Query: "x", Source: "q", Found: true}},
		// S2<->S3 boundary: non-yielding objects return Found=false (inference is S3).
		{"alias_null", `{"query":null}`, ExtractionResult{}},
		{"nested_empty", `{"query":{}}`, ExtractionResult{}},
		{"nested_array_no_string", `{"query":[1,2,3]}`, ExtractionResult{}},
		{"nested_subkey_nonstring", `{"query":{"text":42}}`, ExtractionResult{}},
		{"no_alias_string_s3boundary", `{"foo":"bar"}`, ExtractionResult{Query: "bar", Source: "inferred:foo", Found: true}},
		// Level 4b (S3): inference — no alias key yielded, so collect reachable
		// non-empty strings (excluding optional keys), longest-wins, inferred:<path>.
		{"infer_single", `{"foo":"bar"}`, ExtractionResult{Query: "bar", Source: "inferred:foo", Found: true}},
		{"infer_multi_longest", `{"a":"short","b":"longest string"}`, ExtractionResult{Query: "longest string", Source: "inferred:b", Ambiguous: true, Found: true}},
		{"infer_messages", `{"messages":[{"role":"user","content":"rust async"}]}`, ExtractionResult{Query: "rust async", Source: "inferred:messages[0].content", Ambiguous: true, Found: true}},
		{"infer_nested_single", `{"a":{"b":"deep search text"}}`, ExtractionResult{Query: "deep search text", Source: "inferred:a.b", Found: true}},
		{"infer_tie", `{"a":"xx","b":"yy"}`, ExtractionResult{Query: "xx", Source: "inferred:a", Ambiguous: true, Found: true}},
		{"infer_skip_empty", `{"a":"","b":"real"}`, ExtractionResult{Query: "real", Source: "inferred:b", Found: true}},
		{"infer_exclude_optional", `{"location":"France","description":"search rust"}`, ExtractionResult{Query: "search rust", Source: "inferred:description", Optionals: map[string]any{"location": "France"}, Found: true}},
		{"infer_exclude_optalias", `{"country":"France","description":"search rust"}`, ExtractionResult{Query: "search rust", Source: "inferred:description", Optionals: map[string]any{"location": "France"}, Found: true}},
		// Level 4a + optional normalization (S3): optionals attached to alias-scan
		// success returns; shallow, canonical-first, nil when none.
		{"alias_plus_optional", `{"q":"rust","country":"France"}`, ExtractionResult{Query: "rust", Source: "q", Optionals: map[string]any{"location": "France"}, Found: true}},
		{"canon_wins_over_alias", `{"q":"rust","location":"US","country":"France"}`, ExtractionResult{Query: "rust", Source: "q", Optionals: map[string]any{"location": "US"}, Found: true}},
		{"multi_optionals", `{"q":"rust","country":"France","size":"large","recency":"day"}`, ExtractionResult{Query: "rust", Source: "q", Optionals: map[string]any{"location": "France", "content_size": "large", "search_recency_filter": "day"}, Found: true}},
		{"array_obj_optionals", `[{"q":"x","country":"FR"}]`, ExtractionResult{Query: "x", Source: "array[0]", Optionals: map[string]any{"location": "FR"}, Found: true}},
		// Level 5 (S3): failure — no usable string anywhere -> zero value.
		{"fail_no_strings", `{"a":1,"b":true}`, ExtractionResult{}},
		{"fail_only_optional", `{"location":"France"}`, ExtractionResult{}},
		{"fail_array_nons", `{"a":[1,2]}`, ExtractionResult{}},
		{"fail_all_empty", `{"a":"","b":""}`, ExtractionResult{}},
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

func TestToUpstreamArgs(t *testing.T) {
	// ToUpstreamArgs builds the clean z.ai payload (PRD §10.3): exactly
	// targetParam -> Query plus normalized optionals; alias names and
	// unrecognized keys are dropped. Built from real extract() results so the
	// source-label/optional path is exercised end-to-end.
	tests := []struct {
		name       string
		raw        string
		target     string
		wantArgs   map[string]any
		wantCalled bool // false = Found==false, ToUpstreamArgs would not be called upstream
	}{
		{
			"no_optionals",
			`{"q":"rust"}`,
			"search_query",
			map[string]any{"search_query": "rust"},
			true,
		},
		{
			"with_optional",
			`{"q":"rust","country":"France"}`,
			"search_query",
			map[string]any{"search_query": "rust", "location": "France"},
			true,
		},
		{
			"dropped_junk",
			`{"q":"rust","junk":1,"country":"France"}`,
			"search_query",
			map[string]any{"search_query": "rust", "location": "France"},
			true,
		},
		{
			"multi_optionals",
			`{"q":"rust","country":"France","size":"large","recency":"day"}`,
			"search_query",
			map[string]any{"search_query": "rust", "location": "France", "content_size": "large", "search_recency_filter": "day"},
			true,
		},
		{
			"inferred_no_optionals",
			`{"foo":"bar"}`,
			"search_query",
			map[string]any{"search_query": "bar"},
			true,
		},
		{
			"inferred_with_optional",
			`{"location":"France","description":"search rust"}`,
			"search_query",
			map[string]any{"search_query": "search rust", "location": "France"},
			true,
		},
		{
			"array_obj_optionals",
			`[{"q":"x","country":"FR"}]`,
			"search_query",
			map[string]any{"search_query": "x", "location": "FR"},
			true,
		},
		{
			"custom_target_param",
			`{"q":"rust"}`,
			"q_param",
			map[string]any{"q_param": "rust"},
			true,
		},
		{
			"failure_not_called_upstream",
			`{}`,
			"search_query",
			map[string]any{"search_query": ""},
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := extract(json.RawMessage(tc.raw), extractQA, extractOpt)
			if res.Found != tc.wantCalled {
				t.Fatalf("Found=%v want %v", res.Found, tc.wantCalled)
			}
			got := res.ToUpstreamArgs(tc.target)
			if !reflect.DeepEqual(got, tc.wantArgs) {
				t.Errorf("ToUpstreamArgs(%q) = %#v\nwant             %#v", tc.target, got, tc.wantArgs)
			}
		})
	}
}
