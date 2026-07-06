package main

import (
	"encoding/json"
	"os"
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
