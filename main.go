package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// logStartup emits the "startup" log event: the resolved configuration the proxy
// is actually running with (PRD §15). Fields: tools (as a JSON array),
// canonical_tool, query_aliases (as a JSON array), listen, upstream, log_level.
// NEVER credentials — Config carries no credential field (PRD §13: Authorization
// is forwarded as a request header, never owned), so logging cfg structurally
// cannot leak a secret.
//
// Extracted as a named function (rather than inlined in main) so it is unit-
// testable with an injected *bytes.Buffer writer via newLogger(&buf, level).
func logStartup(l *logger, cfg Config) {
	l.log("info", "startup", map[string]any{
		"tools":          cfg.Tools, // []string -> JSON array
		"canonical_tool": cfg.CanonicalTool,
		"query_aliases":  cfg.QueryAliases, // []string -> JSON array
		"listen":         cfg.Listen,
		"upstream":       cfg.Upstream,
		"log_level":      cfg.LogLevel,
	})
}

// authHeaderKey is an unexported context-key type that carries the inbound
// Authorization header from the HTTP middleware down to the tool handler (and
// onward to the upstream z.ai client in P1.M4.T1.S2). An unexported struct type
// is the collision-free context-key convention (PRD §17; go.dev/blog/context).
type authHeaderKey struct{}

// authMiddleware wraps h so that each request's Authorization header is stored in
// the request context before h handles it. The SDK's StreamableHTTPHandler passes
// req.Context() into server.Connect (verified: streamable.go:491-493), so the
// value reaches the tool handler. The credential is NEVER logged or stored in
// Config (PRD §13/§17); it is only forwarded verbatim to z.ai later.
func authMiddleware(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), authHeaderKey{}, r.Header.Get("Authorization"))
		h.ServeHTTP(w, r.WithContext(ctx))
	})
}

// main is the MCP server bootstrap: resolve config, build the redacting logger,
// emit the startup event, build the SDK server + Streamable HTTP handler, wrap it
// in the auth-extraction middleware, register /healthz and /, arm the graceful-
// shutdown handler, and serve (PRD §16).
func main() {
	// Resolve config (discovery + env overrides + validation). The contract's
	// `cfg,_` is shorthand: a real error here MUST fail fast (a bad listen address
	// or missing WSPF_CONFIG file must not silently boot).
	cfg, err := ResolveConfig()
	if err != nil {
		// On the error path cfg.LogLevel is untrusted; log at a fixed "error"
		// level to os.Stderr (structured JSON, never fmt.Println — PRD §15) and
		// exit non-zero.
		newLogger(os.Stderr, "error").log("error", "config", map[string]any{"err": err.Error()})
		os.Exit(1)
	}

	// CRITICAL: variable named `log` per the contract. log.log(...) is valid Go
	// (selector resolves method-on-value; var name != method name namespace).
	log := newLogger(os.Stderr, cfg.LogLevel)
	logStartup(log, cfg)

	// MCP SDK server: owns the JSON-RPC surface (initialize, ping, notifications/*,
	// resources/list, prompts/list, tools/list, tools/call). NO tools are registered
	// in this milestone — AddTool calls arrive in P1.M5.T2. Pass nil ServerOptions
	// for defaults. version is the health.go package var ("dev", or set via -ldflags).
	server := mcp.NewServer(&mcp.Implementation{Name: "web-search-prime-fixer", Version: version}, nil)

	// [Mode A] The SDK's StreamableHTTPHandler OWNS all MCP transport framing —
	// initialize handshake, Mcp-Session-Id lifecycle, SSE framing, JSON-RPC dispatch,
	// and tools/list / tools/call routing (PRD §13, FR-1). We hand it the same
	// *Server for every request (getServer may return a shared server). Pass nil
	// options for stateful sessions + SSE responses (the Streamable HTTP transport).
	// This replaces the v1 byte-forwarding proxy + hand-rolled SSE framer.
	sdkHandler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)

	// Route table: /healthz is our local handler (no upstream); EVERYTHING else
	// goes to the SDK handler, wrapped so the inbound Authorization header is
	// threaded into the request context for later upstream forwarding (PRD §17).
	// The SDK passes req.Context() into server.Connect (verified streamable.go:491),
	// so the context value reaches the tool handler. We never log the credential.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)   // local health (no upstream); PRD §16
	mux.Handle("/", authMiddleware(sdkHandler)) // SDK owns MCP framing; subtree catch-all

	// CRITICAL: NO ReadTimeout/WriteTimeout — a write deadline would truncate the
	// streamed SSE responses (PRD §8/§11.3, P1.M3/P1.M4). Addr+Handler only. The
	// graceful-shutdown goroutine below calls srv.Shutdown(ctx) on this object.
	srv := &http.Server{Addr: cfg.Listen, Handler: mux}

	// T4.S2: graceful shutdown (PRD §16). Runs in its own goroutine so
	// ListenAndServe stays on the main goroutine. SIGINT (Ctrl-C) / SIGTERM
	// (kill) -> log one "shutdown" line -> Shutdown drains within 10s ->
	// ListenAndServe returns http.ErrServerClosed -> main falls through to exit 0.
	go func() {
		sigCh := make(chan os.Signal, 1) // buffered: don't drop a fast signal
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		sig := <-sigCh
		log.log("info", "shutdown", map[string]any{"signal": sig.String()})
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	// ListenAndServe blocks until error or Shutdown. ErrServerClosed is the normal
	// return when Shutdown is called (T4.S2 adds that path); treat it non-fatal so
	// T4.S2 is a pure addition (no spurious "listen" error on graceful exit).
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.log("error", "listen", map[string]any{"err": err.Error(), "listen": cfg.Listen})
		os.Exit(1)
	}
	// ErrServerClosed (from Shutdown) falls through here -> clean exit 0.
}
