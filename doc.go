// Package main implements web-search-prime-fixer, a local normalizing MCP server
// for the z.ai web-search-prime endpoint. It is built on the official Go MCP SDK,
// binds to 127.0.0.1 (local only), advertises a single tool (web_search), and
// acts as an MCP client to z.ai rather than forwarding the agent's bytes. It is
// explicitly not a transparent or open proxy: it owns the JSON-RPC surface and
// rebuilds every call it forwards.
//
// On a tools/call it extracts the search query from whatever arguments the agent
// sent (any key, nested object, bare string, or array) and delegates one clean
// call to z.ai's web_search_prime, forwarding the client's Authorization header
// while holding no key of its own. When the call was non-canonical it appends a
// teaching warning (with a correct-usage example) after the results, never
// instead of them and never prepended; the canonical call (web_search with
// { "query": "..." }) gets no warning. When no query can be extracted, it
// returns the warning immediately with no upstream call.
//
// Non-goals: it is not a general search engine, not a general-purpose or open
// proxy, and it owns no credentials; it performs no retry, rate-limiting, or
// caching of upstream calls, no truncation of the query text, and no multi-tool
// semantics (every advertised tool performs the same one search). The z.ai name
// web_search_prime is never advertised to the agent, and the server is not
// responsible for other MCP servers' tool-name collisions when the client runs
// with toolPrefix set to "none".
package main
