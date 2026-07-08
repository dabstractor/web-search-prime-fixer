package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestDefaultConfig verifies every field of DefaultConfig() matches PRD §18.2
// verbatim (the v2 11-field schema: exact strings, the 14-entry QueryAliases slice
// in order, and the 3-key OptionalAliases map).
func TestDefaultConfig(t *testing.T) {
	def := DefaultConfig()

	want := Config{
		Upstream:       "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:         "127.0.0.1:8787",
		Path:           "/mcp",
		Tools:          []string{"web_search"},
		CanonicalTool:  "web_search",
		CanonicalParam: "query",
		QueryAliases: []string{
			"query", "search_query", "q", "search", "searchQuery",
			"search_term", "term", "text", "input", "prompt",
			"question", "keywords", "topic", "searchString",
		},
		OptionalAliases: map[string][]string{
			"location":              {"country", "region"},
			"content_size":          {"size", "contentSize", "detail"},
			"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
		},
		TargetTool:  "web_search_prime",
		TargetParam: "search_query",
		LogLevel:    "info",
	}
	if !reflect.DeepEqual(def, want) {
		t.Fatalf("DefaultConfig mismatch:\n got  %+v\n want %+v", def, want)
	}

	// Belt-and-suspenders per-field asserts so a typo names itself.
	if def.Upstream != "https://api.z.ai/api/mcp/web_search_prime/mcp" {
		t.Errorf("Upstream = %q, want the z.ai mcp URL", def.Upstream)
	}
	if def.Listen != "127.0.0.1:8787" {
		t.Errorf("Listen = %q, want 127.0.0.1:8787", def.Listen)
	}
	if def.Path != "/mcp" {
		t.Errorf("Path = %q, want /mcp", def.Path)
	}
	if !reflect.DeepEqual(def.Tools, []string{"web_search"}) {
		t.Errorf("Tools = %#v, want [\"web_search\"]", def.Tools)
	}
	if def.CanonicalTool != "web_search" {
		t.Errorf("CanonicalTool = %q, want web_search", def.CanonicalTool)
	}
	if def.CanonicalParam != "query" {
		t.Errorf("CanonicalParam = %q, want query", def.CanonicalParam)
	}
	wantAliases := []string{
		"query", "search_query", "q", "search", "searchQuery",
		"search_term", "term", "text", "input", "prompt",
		"question", "keywords", "topic", "searchString",
	}
	if !reflect.DeepEqual(def.QueryAliases, wantAliases) {
		t.Errorf("QueryAliases = %#v, want the 14-element default slice in order", def.QueryAliases)
	}
	wantOptionals := map[string][]string{
		"location":              {"country", "region"},
		"content_size":          {"size", "contentSize", "detail"},
		"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
	}
	if !reflect.DeepEqual(def.OptionalAliases, wantOptionals) {
		t.Errorf("OptionalAliases = %#v, want the 3-key default map", def.OptionalAliases)
	}
	if def.TargetTool != "web_search_prime" {
		t.Errorf("TargetTool = %q, want web_search_prime", def.TargetTool)
	}
	if def.TargetParam != "search_query" {
		t.Errorf("TargetParam = %q, want search_query", def.TargetParam)
	}
	if def.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", def.LogLevel)
	}
}

// TestLoadConfig_EmptyPath verifies LoadConfig("") returns the defaults with no
// error — this is how the server runs with no config file at all.
func TestLoadConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") returned error %v, want nil", err)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("LoadConfig(\"\") = %+v, want DefaultConfig() %+v", cfg, DefaultConfig())
	}
}

// TestLoadConfig_FromFile exercises the v2 file-merge path with table-driven
// subtests: partial override keeps defaults, unknown fields are ignored, v2-only
// fields override, a full v2 file sets every field, and invalid JSON yields an error.
func TestLoadConfig_FromFile(t *testing.T) {
	// Rows 1/2/4 derive `want` from DefaultConfig() so the test tracks future
	// default changes automatically rather than spelling them out again.
	partial := DefaultConfig()
	partial.Listen = "0.0.0.0:9999"
	partial.QueryAliases = []string{"foo"}

	unknown := DefaultConfig()
	unknown.Upstream = "http://example.invalid/mcp"
	unknown.LogLevel = "warn"

	v2fields := DefaultConfig()
	v2fields.Tools = []string{"web_search", "search"}
	v2fields.CanonicalTool = "web_search"
	// json.Unmarshal merges a map field KEY-BY-KEY (it does not replace the whole
	// map), so a file with optional_aliases={"location":["country"]} keeps the
	// default content_size / search_recency_filter keys and overrides only
	// "location". The want map therefore reflects that merged result.
	v2fields.OptionalAliases = map[string][]string{
		"location":              {"country"},
		"content_size":          {"size", "contentSize", "detail"},
		"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
	}

	tests := []struct {
		name    string
		json    string
		wantErr bool
		want    Config
	}{
		{
			name:    "partial_override_keeps_defaults",
			json:    `{"listen":"0.0.0.0:9999","query_aliases":["foo"]}`,
			wantErr: false,
			want:    partial,
		},
		{
			name:    "unknown_fields_ignored",
			json:    `{"upstream":"http://example.invalid/mcp","banana":42,"nested":{"a":1},"log_level":"warn"}`,
			wantErr: false,
			want:    unknown,
		},
		{
			name:    "v2_fields_override",
			json:    `{"tools":["web_search","search"],"canonical_tool":"web_search","optional_aliases":{"location":["country"]}}`,
			wantErr: false,
			want:    v2fields,
		},
		{
			name:    "full_file_all_fields",
			json:    `{"upstream":"http://u","listen":"127.0.0.1:1","path":"/p","tools":["web_search"],"canonical_tool":"web_search","canonical_param":"query","query_aliases":["a","b"],"optional_aliases":{"location":["country"]},"target_tool":"web_search_prime","target_param":"search_query","log_level":"debug"}`,
			wantErr: false,
			want: Config{
				Upstream:       "http://u",
				Listen:         "127.0.0.1:1",
				Path:           "/p",
				Tools:          []string{"web_search"},
				CanonicalTool:  "web_search",
				CanonicalParam: "query",
				QueryAliases:   []string{"a", "b"},
				// Map merge: file's {"location":["country"]} overrides only that key;
				// the default content_size / search_recency_filter keys are retained.
				OptionalAliases: map[string][]string{
					"location":              {"country"},
					"content_size":          {"size", "contentSize", "detail"},
					"search_recency_filter": {"recency", "freshness", "time_filter", "date_filter"},
				},
				TargetTool:  "web_search_prime",
				TargetParam: "search_query",
				LogLevel:    "debug",
			},
		},
		{
			name:    "invalid_json_returns_error",
			json:    `{not valid json`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "config.json")
			if err := os.WriteFile(p, []byte(tt.json), 0o644); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			got, err := LoadConfig(p)
			if (err != nil) != tt.wantErr {
				t.Fatalf("LoadConfig(%q) err = %v, wantErr = %v", p, err, tt.wantErr)
			}
			if tt.wantErr {
				// On error cfg may be partial; do not assert its contents.
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("LoadConfig mismatch:\n got  %+v\n want %+v", got, tt.want)
			}
		})
	}
}

// TestLoadConfig_MissingFile pins the LoadConfig/ResolveConfig boundary: a
// non-empty path to a missing file is an ERROR (the "no file → defaults" decision
// belongs to ResolveConfig, which decides whether to even call LoadConfig).
func TestLoadConfig_MissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does-not-exist.json")
	cfg, err := LoadConfig(p)
	if err == nil {
		t.Fatalf("LoadConfig(%q) returned nil error, want non-nil for a missing file", p)
	}
	// cfg may be partial/DefaultConfig on the error path; do not assert it.
	_ = cfg
}
