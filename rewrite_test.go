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

// TestRewrite_NilArgs passes a nil map DIRECTLY (it must NOT go through cloneMap,
// which turns nil into {}). This proves rewrite.go's step-1 `if args == nil`
// guard fires before any write to the nil map (which would otherwise PANIC at
// runtime). PRD §19.1 (nil args edge case).
func TestRewrite_NilArgs(t *testing.T) {
	var args map[string]any // nil — do NOT cloneMap (it would turn this into {})
	got := Rewrite(args, rwAliases, rwTarget)
	if got.Changed {
		t.Errorf("Changed = true, want false for nil args")
	}
	if got.Notes != nil {
		t.Errorf("Notes = %#v, want nil for nil args", got.Notes)
	}
	if args != nil {
		t.Errorf("args = %#v, want nil (guard must fire before any nil-map write)", args)
	}
}

// TestRewrite_OrderingMatters pins config-order promotion (architecture/
// external_deps.md §6: Go map iteration is randomized, so the algorithm walks the
// aliases slice, never the args map). With aliases=[q,query] (reversed from the
// default config order), q MUST be promoted — a `range args` regression would
// flip q-vs-query run-to-run. PRD §19.1 (alias-list ordering).
func TestRewrite_OrderingMatters(t *testing.T) {
	aliases := []string{"q", "query"} // reversed from the default config order
	args := map[string]any{"q": "y", "query": "x"}
	got := Rewrite(args, aliases, rwTarget)
	if !got.Changed {
		t.Fatal("Changed = false, want true")
	}
	wantArgs := map[string]any{"search_query": "y"} // q's value promoted
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %#v, want %#v (q promoted, NOT query)", args, wantArgs)
	}
	wantNotes := []string{
		`"q" is not a valid parameter; renamed to "search_query"`,
		`dropped redundant "query"`,
	}
	if !reflect.DeepEqual(got.Notes, wantNotes) {
		t.Errorf("Notes = %#v, want %#v", got.Notes, wantNotes)
	}
}

// TestRewrite_DuplicateAliasDedup covers BOTH de-dup paths: target-absent
// (single rename note) and target-present (single ignored note). A duplicate
// alias entry in config MUST produce exactly ONE note, never a double note or a
// spurious drop. PRD §19.1 (duplicate alias entries de-duped).
func TestRewrite_DuplicateAliasDedup(t *testing.T) {
	aliases := []string{"query", "query"}
	t.Run("target_absent_single_rename", func(t *testing.T) {
		args := map[string]any{"query": "x"}
		got := Rewrite(args, aliases, rwTarget)
		if !got.Changed {
			t.Fatal("Changed = false, want true")
		}
		wantArgs := map[string]any{"search_query": "x"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %#v, want %#v", args, wantArgs)
		}
		wantNotes := []string{`"query" is not a valid parameter; renamed to "search_query"`}
		if !reflect.DeepEqual(got.Notes, wantNotes) {
			t.Errorf("Notes = %#v, want exactly one rename note (no double/drop): %#v", got.Notes, wantNotes)
		}
	})
	t.Run("target_present_single_ignore", func(t *testing.T) {
		args := map[string]any{"query": "x", "search_query": "y"}
		got := Rewrite(args, aliases, rwTarget)
		if !got.Changed {
			t.Fatal("Changed = false, want true")
		}
		wantArgs := map[string]any{"search_query": "y"}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %#v, want %#v", args, wantArgs)
		}
		wantNotes := []string{`ignored "query" (use only "search_query")`}
		if !reflect.DeepEqual(got.Notes, wantNotes) {
			t.Errorf("Notes = %#v, want exactly one ignored note: %#v", got.Notes, wantNotes)
		}
	})
}

// TestRewrite_NonStringValues pins PRD §3 (Rewrite moves keys without
// inspecting or coercing values). number/object/array values must be carried to
// search_query byte-for-byte unchanged. cloneMap's shallow copy is safe here
// because Rewrite never mutates nested values. PRD §19.1 (non-string alias
// values carried through as-is).
func TestRewrite_NonStringValues(t *testing.T) {
	cases := []struct {
		name string
		val  any
	}{
		{"number", 123},
		{"object", map[string]any{"k": 1}},
		{"array", []any{1, 2}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			args := map[string]any{"query": c.val}
			got := Rewrite(args, rwAliases, rwTarget)
			if !got.Changed {
				t.Fatal("Changed = false, want true")
			}
			wantArgs := map[string]any{"search_query": c.val}
			if !reflect.DeepEqual(args, wantArgs) {
				t.Errorf("args = %#v, want search_query to carry the value as-is: %#v", args, wantArgs)
			}
			if len(got.Notes) != 1 {
				t.Errorf("Notes = %#v, want exactly one rename note", got.Notes)
			}
		})
	}
}
