package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

// Config is the resolved configuration for the web-search-prime-fixer proxy.
//
// Fields are populated by overlaying a JSON config file onto the built-in
// defaults (see DefaultConfig and LoadConfig). Every field has a snake_case JSON
// key used when reading a config file (PRD §14.1 "JSON form").
type Config struct {
	// Upstream is the z.ai MCP endpoint the proxy forwards to.
	// JSON key: "upstream".
	Upstream string `json:"upstream"`

	// Listen is the local bind address (host:port). Local only (127.0.0.1).
	// JSON key: "listen".
	Listen string `json:"listen"`

	// Path is reserved (informational; default "/mcp"). The proxy forwards all
	// non-/healthz paths to Upstream regardless of this value.
	// JSON key: "path".
	Path string `json:"path"`

	// Aliases is the ordered list of argument keys renamed to TargetParam when
	// present in a tools/call request. Order matters: the first present alias is
	// promoted when the target is absent.
	// JSON key: "aliases".
	Aliases []string `json:"aliases"`

	// TargetParam is the canonical parameter aliases are renamed to
	// (always "search_query").
	// JSON key: "target_param".
	TargetParam string `json:"target_param"`

	// LogLevel is one of debug | info | warn | error.
	// JSON key: "log_level".
	LogLevel string `json:"log_level"`
}

// DefaultConfig returns the built-in default configuration (PRD §14.2, verbatim).
//
// The proxy runs with no config file at all by starting from these defaults;
// LoadConfig("") yields this exact value, and LoadConfig(path) overlays a file's
// JSON on top of it, overriding only the fields the file names.
func DefaultConfig() Config {
	return Config{
		Upstream:    "https://api.z.ai/api/mcp/web_search_prime/mcp",
		Listen:      "127.0.0.1:8787",
		Path:        "/mcp",
		Aliases:     []string{"query", "q", "search", "searchQuery", "search_term"},
		TargetParam: "search_query",
		LogLevel:    "info",
	}
}

// LoadConfig reads the JSON config file at path and overlays it onto the
// built-in defaults, returning the merged Config.
//
// If path == "", the defaults are returned unchanged with a nil error (this is
// how the proxy runs with no config file). Otherwise the file is read with
// os.ReadFile and json.Unmarshal'd over the default struct, so fields present in
// the file override the defaults while omitted fields keep their default value.
// Unknown fields are ignored (json.Unmarshal default behavior).
//
// On a read or parse error the returned Config may be partial and must be
// ignored; callers should check the error first.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// resolveConfigPath returns the config file path to load, per PRD §14.3 discovery
// precedence (first match wins):
//  1. WSPF_CONFIG: if set (non-empty), used VERBATIM — even if the file is
//     missing, which surfaces as a load error from LoadConfig rather than a
//     silent fallback to defaults.
//  2. Otherwise the first EXISTING of:
//     ./web-search-prime-fixer.json                         (process CWD)
//     $XDG_CONFIG_HOME/web-search-prime-fixer/config.json   (portable: os.UserConfigDir)
//  3. If none of the above is usable, "" is returned (no file → built-in defaults).
//
// os.UserConfigDir resolves $XDG_CONFIG_HOME or ~/.config portably and is
// consulted at runtime; if it errors (no $HOME/$XDG_CONFIG_HOME) the XDG
// candidate is skipped, never fatal.
func resolveConfigPath() string {
	if p := os.Getenv("WSPF_CONFIG"); p != "" {
		return p // explicit override: used verbatim (missing file → load error)
	}
	const cwdCandidate = "web-search-prime-fixer.json" // "./web-search-prime-fixer.json" (PRD §14.3); bare name resolves against CWD
	if fileExists(cwdCandidate) {
		return cwdCandidate
	}
	if dir, err := os.UserConfigDir(); err == nil {
		xdgCandidate := filepath.Join(dir, "web-search-prime-fixer", "config.json")
		if fileExists(xdgCandidate) {
			return xdgCandidate
		}
	}
	return "" // no file found → DefaultConfig() base
}

// fileExists reports whether name exists and is stat-able. Used for the "first
// existing" CWD/XDG search only (NOT for WSPF_CONFIG, which is verbatim).
func fileExists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}

// ResolveConfig discovers, loads, env-overrides, and validates the proxy
// configuration, returning a fully-validated Config ready for startup (PRD §14.3).
//
// # Discovery precedence (resolveConfigPath)
//
//  1. WSPF_CONFIG: if set (non-empty), that path is loaded VERBATIM. A missing
//     file here is a hard error (the caller asked for a specific file), not a
//     silent fallback to defaults.
//  2. Otherwise the first EXISTING of:
//     ./web-search-prime-fixer.json
//     $XDG_CONFIG_HOME/web-search-prime-fixer/config.json
//     (resolved portably via os.UserConfigDir; defaults to
//     ~/.config/web-search-prime-fixer/config.json on Linux/macOS).
//  3. If none exist, no file is loaded and the built-in defaults (DefaultConfig)
//     form the base.
//
// # Environment overrides (applied AFTER the file load — highest precedence)
//
//	WSPF_UPSTREAM   -> Config.Upstream
//	WSPF_LISTEN     -> Config.Listen
//	WSPF_LOG_LEVEL  -> Config.LogLevel
//
// An empty env var is ignored (the file/default value is kept). Path and Aliases
// have NO env override.
//
// # Validation (returns a clear, wrapped error on failure)
//
//   - Listen must be a parseable host:port (net.SplitHostPort).
//   - Upstream must be an absolute URL (url.Parse succeeds and URL.IsAbs reports
//     a non-empty scheme).
//
// After validation, if TargetParam is empty it is forced to "search_query".
//
// ResolveConfig performs no I/O output. The returned Config contains no
// credential fields (Authorization is forwarded as a request header, never part
// of Config — see PRD §13), so the bootstrap logging it at startup (PRD §15)
// never exposes secrets.
func ResolveConfig() (Config, error) {
	path := resolveConfigPath()
	cfg, err := LoadConfig(path) // S1 primitive: DefaultConfig base, file overlay, unknown fields ignored
	if err != nil {
		return cfg, fmt.Errorf("load config %q: %w", path, err)
	}

	// Env overrides (highest precedence; empty values ignored).
	if v := os.Getenv("WSPF_UPSTREAM"); v != "" {
		cfg.Upstream = v
	}
	if v := os.Getenv("WSPF_LISTEN"); v != "" {
		cfg.Listen = v
	}
	if v := os.Getenv("WSPF_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
	}

	// Validate Listen: must parse as host:port.
	if _, _, err := net.SplitHostPort(cfg.Listen); err != nil {
		return cfg, fmt.Errorf("invalid listen address %q: %w", cfg.Listen, err)
	}
	// Validate Upstream: must be an absolute URL (non-empty scheme).
	u, err := url.Parse(cfg.Upstream)
	if err != nil {
		return cfg, fmt.Errorf("invalid upstream URL %q: %w", cfg.Upstream, err)
	}
	if !u.IsAbs() {
		return cfg, fmt.Errorf("upstream URL %q is not absolute (missing scheme)", cfg.Upstream)
	}

	// Force TargetParam if empty (PRD §14.3).
	if cfg.TargetParam == "" {
		cfg.TargetParam = "search_query"
	}

	return cfg, nil
}
