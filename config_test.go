package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestDefaultConfig verifies every field of DefaultConfig() matches PRD §14.2
// verbatim (exact strings and alias slice order).
func TestDefaultConfig(t *testing.T) {
	def := DefaultConfig()

	want := Config{
		Upstream:    "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:      "127.0.0.1:8787",
		Path:        "/mcp",
		Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"},
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
	if !reflect.DeepEqual(def.Aliases, []string{"query", "q", "search", "searchQuery", "search_term"}) {
		t.Errorf("Aliases = %#v, want the five-element default slice in order", def.Aliases)
	}
	if def.TargetParam != "search_query" {
		t.Errorf("TargetParam = %q, want search_query", def.TargetParam)
	}
	if def.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info", def.LogLevel)
	}
}

// TestLoadConfig_EmptyPath verifies LoadConfig("") returns the defaults with no
// error — this is how the proxy runs with no config file at all.
func TestLoadConfig_EmptyPath(t *testing.T) {
	cfg, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") returned error %v, want nil", err)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("LoadConfig(\"\") = %+v, want DefaultConfig() %+v", cfg, DefaultConfig())
	}
}

// TestLoadConfig_FromFile exercises the file-merge path with table-driven
// subtests: partial override keeps defaults, unknown fields are ignored, a full
// file sets every field, and invalid JSON yields an error.
func TestLoadConfig_FromFile(t *testing.T) {
	// Row 1 & 2 derive `want` from DefaultConfig() so the test tracks future
	// default changes automatically rather than spelling them out again.
	partial := DefaultConfig()
	partial.Listen = "0.0.0.0:9999"
	partial.Aliases = []string{"foo"}

	unknown := DefaultConfig()
	unknown.Upstream = "http://example.invalid/mcp"
	unknown.LogLevel = "warn"

	tests := []struct {
		name    string
		json    string
		wantErr bool
		want    Config
	}{
		{
			name:    "partial_override_keeps_defaults",
			json:    `{"listen":"0.0.0.0:9999","aliases":["foo"]}`,
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
			name:    "full_file_all_fields",
			json:    `{"upstream":"http://u","listen":"127.0.0.1:1","path":"/p","aliases":["a","b"],"target_param":"search_query","log_level":"debug"}`,
			wantErr: false,
			want: Config{
				Upstream:    "http://u",
				Listen:      "127.0.0.1:1",
				Path:        "/p",
				Aliases:     []string{"a", "b"},
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

// TestLoadConfig_MissingFile pins the S1/S2 boundary: a non-empty path to a
// missing file is an ERROR (the "no file → defaults" decision belongs to S2,
// which decides whether to even call LoadConfig with this path).
func TestLoadConfig_MissingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "does-not-exist.json")
	cfg, err := LoadConfig(p)
	if err == nil {
		t.Fatalf("LoadConfig(%q) returned nil error, want non-nil for a missing file", p)
	}
	// cfg may be partial/DefaultConfig on the error path; do not assert it.
	_ = cfg
}
