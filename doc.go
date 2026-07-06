// Package main implements web-search-prime-fixer, a local MCP
// alias-fixing proxy. It is a transparent reverse proxy for the z.ai
// web-search-prime MCP endpoint that rewrites commonly misspelled
// tools/call argument aliases (e.g. "query") to the canonical
// "search_query" parameter and injects a one-line SSE warning when a
// rewrite is applied. Every other request passes through unchanged.
//
// The proxy binds to 127.0.0.1 (local only) and uses the Go standard
// library exclusively.
package main
