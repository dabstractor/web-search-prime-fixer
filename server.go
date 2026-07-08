package main

import (
	"context"
	"sort"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// newUpstreamClient builds the shared z.ai delegation client from cfg (PRD §11.1,
// FR-5). It is the constructor main.go uses; tests may also use it or the bare
// struct literal (&UpstreamClient{...}) as upstream_test.go does. It sets the four
// configuration-driven fields and the logger; the session itself is created LAZILY
// on the first callTool (UpstreamClient.ensureSession), so a server with no
// tools/call traffic never opens an upstream connection.
//
// NO CREDENTIAL FIELD (PRD §17, FR-7): Authorization is forwarded as a request
// header via the request context (main.go's authMiddleware stores it under
// authHeaderKey; upstream.authInjector reads it via authFromContext inside
// RoundTrip). The credential is never assigned to a field of UpstreamClient and
// never logged (redactHeaders would redact it if it ever were).
func newUpstreamClient(cfg Config, log *logger) *UpstreamClient {
	return &UpstreamClient{
		upstream:    cfg.Upstream,
		targetTool:  cfg.TargetTool,
		targetParam: cfg.TargetParam,
		log:         log,
	}
}

// registerTools advertises the server's tools and wires the shared dispatch handler
// (PRD §5.2, §9). It builds ONE handler via makeDispatchHandler and registers it for
// EVERY tool returned by buildTools(cfg) — all advertised tools route to the SAME
// handler, because the handler keys its behavior off the ACTUAL called tool name
// (req.Params.Name) vs cfg.CanonicalTool, independent of which tool entry routed the
// call (PRD §9.3: additionally-configured names are terse aliases of the canonical).
//
// This is the single point where tool DEFINITION (buildTools, P1.M5.T1.S1) meets
// tool DISPATCH (this milestone): buildTools produces AddTool-safe *mcp.Tool values;
// registerTools hands each one to (*Server).AddTool with the shared handler. main.go
// calls registerTools(server, cfg, upstream, log) immediately after mcp.NewServer.
func registerTools(server *mcp.Server, cfg Config, upstream *UpstreamClient, log *logger) {
	handler := makeDispatchHandler(cfg, upstream, log)
	for _, tool := range buildTools(cfg) {
		server.AddTool(tool, handler)
	}
}

// makeDispatchHandler returns the shared ToolHandler that implements the
// extract → delegate → teach pipeline for every advertised tool (PRD §5.2, §11.3,
// FR-4/5/6). For each inbound tools/call it runs, in order:
//
//	(1) read calledTool = req.Params.Name and rawArgs = req.Params.Arguments.
//	(2) result := extract(rawArgs, cfg.QueryAliases, cfg.OptionalAliases) — recover a
//	    query from ANY input shape (PRD §10, FR-4).
//	(3) log "delegate" {called_tool, source, canonical(bool), optionals(names)} per
//	    tools/call (PRD §15).
//	(4) if !result.Found: log "extract_failed", return the IMMEDIATE no-results
//	    warning (noQueryResult) and make NO upstream call (PRD §10.1.5, FR-6). This
//	    is the single sanctioned warning-without-results case.
//	(5) else upstreamResult, err := upstream.callTool(ctx, result.ToUpstreamArgs(
//	    cfg.TargetParam)) — delegate to z.ai web_search_prime (PRD §11, FR-5).
//	    ToUpstreamArgs drops aliases/junk so z.ai receives a schema-valid
//	    {search_query, ...optionals}. Auth rides via ctx (authHeaderKey).
//	(6) if err: log "upstream_error", return (nil, err) HONESTLY — the SDK wraps it
//	    as a JSON-RPC error. Never synthesize a result (PRD §11.1 honest-error rule).
//	(7) if shouldWarn: appendWarning(upstreamResult, warningText(...)) — the warning
//	    is APPENDED AFTER the real results (PRD §12, FR-6) so the model acts on the
//	    results rather than retrying on a lone warning.
//	(8) return upstreamResult.
//
// INVARIANTS (PRD §11.3, §12, FR-6): results LEAD and the warning TRAILS; the
// no-query case returns the warning with NO results and NO upstream call; IsError is
// NEVER set for any normalization/warning case — results are built only via
// teach.noQueryResult / teach.appendWarning (which never call SetError) and the
// upstream result is returned UNCHANGED. log must be non-nil in production (main.go
// passes the bootstrap logger); the nil-guards keep a future odd test from panicking.
func makeDispatchHandler(cfg Config, upstream *UpstreamClient, log *logger) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		calledTool := req.Params.Name
		rawArgs := req.Params.Arguments

		// (2) EXTRACT a query from whatever the agent sent (PRD §10, FR-4).
		result := extract(rawArgs, cfg.QueryAliases, cfg.OptionalAliases)

		// (3) "delegate" log event (PRD §15): per tools/call, emitted before the Found
		// check so every call is recorded. canonical = the call is exactly the
		// canonical tool/param with no normalized optionals (== !shouldWarn).
		canonical := calledTool == cfg.CanonicalTool &&
			result.Source == cfg.CanonicalParam &&
			len(result.Optionals) == 0
		if log != nil {
			log.log("info", "delegate", map[string]any{
				"called_tool": calledTool,
				"source":      result.Source,
				"canonical":   canonical,
				"optionals":   sortedKeys(result.Optionals),
			})
		}

		// (4) No usable query (PRD §10.1.5, FR-6 exception): return the immediate
		// no-results warning and make NO upstream call. IsError stays false.
		if !result.Found {
			if log != nil {
				log.log("info", "extract_failed", map[string]any{
					"called_tool": calledTool,
				})
			}
			return noQueryResult(noQueryWarningText(cfg.CanonicalTool, cfg.CanonicalParam)), nil
		}

		// (5) DELEGATE to z.ai (PRD §11, FR-5). ToUpstreamArgs drops aliases/junk so
		// z.ai gets a schema-valid {search_query, ...optionals}. Auth rides via ctx.
		upstreamResult, err := upstream.callTool(ctx, result.ToUpstreamArgs(cfg.TargetParam))
		if err != nil {
			// (6) Honest error (PRD §11.1): surface it verbatim; the SDK wraps it as a
			// JSON-RPC error. Never synthesize a result; never set IsError. (callTool
			// already logs session_expired/reinit_failed internally for those cases;
			// this is the call-level outcome log — complementary, not a duplicate.)
			if log != nil {
				log.log("warn", "upstream_error", map[string]any{
					"called_tool": calledTool,
					"err":         err.Error(),
				})
			}
			return nil, err
		}

		// (7)(8) TEACH (PRD §12, FR-6): append the warning AFTER the real results when
		// usage was non-canonical. Results lead, warning trails -> no retry. IsError is
		// NEVER set (appendWarning appends a TextContent and touches nothing else).
		if shouldWarn(calledTool, result, cfg.CanonicalTool, cfg.CanonicalParam) {
			appendWarning(upstreamResult, warningText(calledTool, result.Source, cfg.CanonicalTool, cfg.CanonicalParam))
		}
		return upstreamResult, nil
	}
}

// sortedKeys returns the keys of m in sorted order (nil when m is empty). It is used
// to log the normalized optional NAMES forwarded to z.ai as a deterministic JSON
// array (PRD §15 delegate "optionals"). Map iteration is randomized in Go, so the
// sort makes the log line stable. The VALUES are not logged (only the names matter
// for "which optionals were normalized"); nil/empty -> JSON null.
func sortedKeys(m map[string]any) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
