// Package main implements web-search-prime-fixer, a local reverse proxy for
// the z.ai web-search-prime MCP endpoint. It binds to 127.0.0.1 (local only)
// and uses the Go standard library exclusively.
//
// On a tools/call, argument keys found in the alias list are renamed to the
// canonical "search_query". When "search_query" is absent, the first present
// alias in config order is promoted and the remaining aliases are dropped.
// When "search_query" is present, all aliases are dropped and the canonical
// value wins. When a rewrite is applied, a one-line warning is injected into
// the tools/call result so the caller learns the correct name. Every other
// argument and request passes through untouched. The default alias list is
// query, q, search, searchQuery, search_term; the target is always
// "search_query".
//
// Non-goals: it does not normalize other parameters (location, content_size,
// search_recency_filter, or any enum), does not warn about or drop unsupported
// parameters, does not rewrite the tool schema, and does not manage API keys,
// retry, rate-limit, or cache.
package main
