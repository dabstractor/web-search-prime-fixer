package main

import (
	"reflect"
	"testing"
)

// aliases/target are defined locally so the rewrite unit is decoupled from
// config.go defaults and remains unit-testable in isolation.
var rwAliases = []string{"query", "q", "search", "searchQuery", "search_term"}

const rwTarget = "search_query"

// cloneMap returns a shallow copy of m so each table row starts from a fresh
// map (maps are reference types: Rewrite mutates in place).
func cloneMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func TestRewrite_Table(t *testing.T) {
	tests := []struct {
		name       string
		in         map[string]any
		wantArgs   map[string]any
		wantNotes  []string
		wantChange bool
	}{
		// Row 1: {"query":"x"} -> renamed.
		{
			"query_renamed",
			map[string]any{"query": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"query" is not a valid parameter; renamed to "search_query"`},
			true,
		},
		// Row 2: {"searchQuery":"x"} -> renamed.
		{
			"searchQuery_renamed",
			map[string]any{"searchQuery": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"searchQuery" is not a valid parameter; renamed to "search_query"`},
			true,
		},
		// Row 3: {"q":"x"} -> renamed.
		{
			"q_renamed",
			map[string]any{"q": "x"},
			map[string]any{"search_query": "x"},
			[]string{`"q" is not a valid parameter; renamed to "search_query"`},
			true,
		},
		// Row 4: {"query":"x","q":"y"} -> query promoted (config-first), q dropped.
		{
			"query_promoted_q_dropped",
			map[string]any{"query": "x", "q": "y"},
			map[string]any{"search_query": "x"},
			[]string{
				`"query" is not a valid parameter; renamed to "search_query"`,
				`dropped redundant "q"`,
			},
			true,
		},
		// Row 5: {"query":"x","search_query":"y"} -> target wins, query ignored.
		{
			"target_present_query_ignored",
			map[string]any{"query": "x", "search_query": "y"},
			map[string]any{"search_query": "y"},
			[]string{`ignored "query" (use only "search_query")`},
			true,
		},
		// Row 6: {"search_query":"x"} -> unchanged (target only, no alias).
		{
			"target_only_unchanged",
			map[string]any{"search_query": "x"},
			map[string]any{"search_query": "x"},
			nil,
			false,
		},
		// Row 7: {"foo":"bar"} -> unchanged (no alias present; PRD §3).
		{
			"non_alias_unchanged",
			map[string]any{"foo": "bar"},
			map[string]any{"foo": "bar"},
			nil,
			false,
		},
		// Row 8: {} -> unchanged (empty).
		{
			"empty_unchanged",
			map[string]any{},
			map[string]any{},
			nil,
			false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			args := cloneMap(tc.in)
			got := Rewrite(args, rwAliases, rwTarget)
			if !reflect.DeepEqual(args, tc.wantArgs) {
				t.Errorf("args = %#v\nwant    %#v", args, tc.wantArgs)
			}
			if !reflect.DeepEqual(got.Notes, tc.wantNotes) {
				t.Errorf("Notes = %#v\nwant     %#v", got.Notes, tc.wantNotes)
			}
			if got.Changed != tc.wantChange {
				t.Errorf("Changed = %v, want %v", got.Changed, tc.wantChange)
			}
		})
	}
}

func TestRewrite_InPlaceMutation(t *testing.T) {
	// Proves the caller's map reference is mutated, not copied.
	args := map[string]any{"query": "x"}
	Rewrite(args, rwAliases, rwTarget)
	if _, ok := args["query"]; ok {
		t.Errorf(`args still has "query"; expected it deleted in place`)
	}
	if v, ok := args["search_query"]; !ok || v != "x" {
		t.Errorf(`args["search_query"] = %v, ok=%v; want "x", true (in-place move)`, v, ok)
	}
}
