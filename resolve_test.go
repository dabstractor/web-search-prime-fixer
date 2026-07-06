package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// isolateConfigEnv makes a ResolveConfig test hermetic:
//   - clears the four WSPF_* env vars (so a dev shell can't leak in),
//   - points XDG_CONFIG_HOME at an EMPTY temp dir (so no real user config is
//     discovered via os.UserConfigDir),
//   - chdir's into a fresh temp dir (so no stray ./web-search-prime-fixer.json
//     in the repo root is discovered),
//   - restores cwd via t.Cleanup.
//
// Returns the CWD temp dir so a test can opt INTO cwd discovery by writing
// ./web-search-prime-fixer.json into it.
//
// MUST NOT be used with t.Parallel (env + chdir are process-global).
func isolateConfigEnv(t *testing.T) string {
	t.Helper()
	for _, k := range []string{"WSPF_CONFIG", "WSPF_UPSTREAM", "WSPF_LISTEN", "WSPF_LOG_LEVEL"} {
		t.Setenv(k, "")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // empty → XDG candidate never found unless a test writes into it
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })
	return dir
}

// writeConfig writes a JSON config file into a fresh t.TempDir() and returns
// its absolute path (for WSPF_CONFIG tests).
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// fieldByJSONName returns cfg.Upstream/Listen/LogLevel by the logical field name.
func fieldByJSONName(cfg *Config, field string) string {
	switch field {
	case "Upstream":
		return cfg.Upstream
	case "Listen":
		return cfg.Listen
	case "LogLevel":
		return cfg.LogLevel
	}
	return ""
}

// (a) Defaults — no file, no env.
func TestResolveConfig_Defaults(t *testing.T) {
	isolateConfigEnv(t) // empty cwd + empty XDG + cleared WSPF_*
	cfg, err := ResolveConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !reflect.DeepEqual(cfg, DefaultConfig()) {
		t.Errorf("got %+v\nwant %+v", cfg, DefaultConfig())
	}
}

// (b) WSPF_CONFIG explicit load + missing-file error (table).
func TestResolveConfig_WSPF_CONFIG(t *testing.T) {
	t.Run("explicit_path_loads", func(t *testing.T) {
		isolateConfigEnv(t)
		p := writeConfig(t, `{"listen":"0.0.0.0:9999","log_level":"warn"}`)
		t.Setenv("WSPF_CONFIG", p)
		cfg, err := ResolveConfig()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		want := DefaultConfig()
		want.Listen = "0.0.0.0:9999"
		want.LogLevel = "warn"
		if !reflect.DeepEqual(cfg, want) {
			t.Errorf("got %+v\nwant %+v", cfg, want)
		}
	})
	t.Run("missing_explicit_path_errors", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_CONFIG", filepath.Join(t.TempDir(), "nope.json"))
		if _, err := ResolveConfig(); err == nil {
			t.Fatal("want error for missing WSPF_CONFIG file; got nil")
		}
	})
	t.Run("env_beats_file", func(t *testing.T) {
		isolateConfigEnv(t)
		p := writeConfig(t, `{"upstream":"http://from-file.invalid/mcp"}`)
		t.Setenv("WSPF_CONFIG", p)
		t.Setenv("WSPF_UPSTREAM", "https://from-env.example.com/mcp")
		cfg, err := ResolveConfig()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if cfg.Upstream != "https://from-env.example.com/mcp" {
			t.Errorf("Upstream=%q want env value", cfg.Upstream)
		}
	})
}

// (c) CWD discovery.
func TestResolveConfig_CwdDiscovery(t *testing.T) {
	dir := isolateConfigEnv(t) // dir is the current CWD now
	if err := os.WriteFile(filepath.Join(dir, "web-search-prime-fixer.json"),
		[]byte(`{"upstream":"https://cwd.example.com/mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Upstream != "https://cwd.example.com/mcp" {
		t.Errorf("Upstream=%q want cwd file value", cfg.Upstream)
	}
}

// (d) XDG discovery.
func TestResolveConfig_XDGDiscovery(t *testing.T) {
	isolateConfigEnv(t)
	// isolateConfigEnv already pointed XDG_CONFIG_HOME at a temp dir; write into it.
	xdg := os.Getenv("XDG_CONFIG_HOME")
	dir := filepath.Join(xdg, "web-search-prime-fixer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"),
		[]byte(`{"upstream":"https://xdg.example.com/mcp"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := ResolveConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Upstream != "https://xdg.example.com/mcp" {
		t.Errorf("Upstream=%q want xdg file value", cfg.Upstream)
	}
}

// (e) Precedence: CWD before XDG.
func TestResolveConfig_Precedence_CwdBeforeXDG(t *testing.T) {
	cwd := isolateConfigEnv(t)
	// XDG file
	xdg := os.Getenv("XDG_CONFIG_HOME")
	xdgDir := filepath.Join(xdg, "web-search-prime-fixer")
	os.MkdirAll(xdgDir, 0o755)
	os.WriteFile(filepath.Join(xdgDir, "config.json"),
		[]byte(`{"upstream":"https://xdg.example.com/mcp"}`), 0o644)
	// CWD file (should win)
	os.WriteFile(filepath.Join(cwd, "web-search-prime-fixer.json"),
		[]byte(`{"upstream":"https://cwd.example.com/mcp"}`), 0o644)
	cfg, err := ResolveConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.Upstream != "https://cwd.example.com/mcp" {
		t.Errorf("Upstream=%q want cwd (precedence)", cfg.Upstream)
	}
}

// (f) Env overrides (table) + empty-ignored + scope guard.
func TestResolveConfig_EnvOverrides(t *testing.T) {
	cases := []struct {
		name, key, val, field string
		want                  string
	}{
		{"upstream", "WSPF_UPSTREAM", "https://env.example.com/mcp", "Upstream", "https://env.example.com/mcp"},
		{"listen", "WSPF_LISTEN", "0.0.0.0:7000", "Listen", "0.0.0.0:7000"},
		{"log_level", "WSPF_LOG_LEVEL", "debug", "LogLevel", "debug"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv(c.key, c.val)
			cfg, err := ResolveConfig()
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			got := fieldByJSONName(&cfg, c.field) // tiny helper, see below
			if got != c.want {
				t.Errorf("%s=%q want %q", c.field, got, c.want)
			}
		})
	}
	t.Run("empty_env_ignored", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_LOG_LEVEL", "") // empty must NOT override the default "info"
		cfg, err := ResolveConfig()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("LogLevel=%q want default info", cfg.LogLevel)
		}
	})
	t.Run("path_and_aliases_not_overridable", func(t *testing.T) {
		isolateConfigEnv(t)
		t.Setenv("WSPF_PATH", "/should/be/ignored")
		t.Setenv("WSPF_ALIASES", "ignored") // no such override; DefaultConfig.Aliases kept
		cfg, err := ResolveConfig()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if cfg.Path != "/mcp" {
			t.Errorf("Path=%q want /mcp (no WSPF_PATH override)", cfg.Path)
		}
		if !reflect.DeepEqual(cfg.Aliases, DefaultConfig().Aliases) {
			t.Errorf("Aliases changed by non-existent WSPF_ALIASES: %+v", cfg.Aliases)
		}
	})
}

// (g) Validation (table: good + bad for Listen and Upstream).
func TestResolveConfig_Validation(t *testing.T) {
	cases := []struct {
		name, key, val string
		wantErr        bool
	}{
		{"good_listen", "WSPF_LISTEN", "0.0.0.0:9000", false},
		{"default_listen_ok", "WSPF_LISTEN", "127.0.0.1:8787", false},
		{"bad_listen_no_port", "WSPF_LISTEN", "127.0.0.1", true},
		{"bad_listen_garbage", "WSPF_LISTEN", "not an addr", true},
		{"bad_listen_empty", "WSPF_LISTEN", "", false}, // empty → ignored → default (valid) — NOT an error
		{"good_upstream_https", "WSPF_UPSTREAM", "https://example.com/mcp", false},
		{"bad_upstream_no_scheme", "WSPF_UPSTREAM", "api.z.ai/mcp", true},
		{"bad_upstream_scheme_relative", "WSPF_UPSTREAM", "//host/path", true},
		{"bad_upstream_empty", "WSPF_UPSTREAM", "", false}, // empty → ignored → default (valid)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			isolateConfigEnv(t)
			t.Setenv(c.key, c.val)
			_, err := ResolveConfig()
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

// (h) TargetParam forcing.
func TestResolveConfig_TargetParamForced(t *testing.T) {
	isolateConfigEnv(t)
	p := writeConfig(t, `{"target_param":""}`) // file forces TargetParam empty
	t.Setenv("WSPF_CONFIG", p)
	cfg, err := ResolveConfig()
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if cfg.TargetParam != "search_query" {
		t.Errorf("TargetParam=%q want forced \"search_query\"", cfg.TargetParam)
	}
}
