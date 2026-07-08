package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
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

// main is the proxy bootstrap: resolve config, build the redacting logger, emit
// the startup event, build the upstream client, register the two routes, arm the
// graceful-shutdown handler, and serve (PRD §16).
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

	// T4.S2: build the shared upstream client ONCE (PRD §17).
	client := newUpstreamClient()

	// Route table (PRD §9): /healthz intercepted; EVERYTHING else forwards.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)              // exact match
	mux.HandleFunc("/", newProxyHandler(cfg, log, client)) // subtree catch-all (was the T4.S1 501 stub)

	// CRITICAL: NO ReadTimeout/WriteTimeout — a write deadline would truncate the
	// streamed SSE responses (PRD §8/§11.3, P1.M3/P1.M4). Addr+Handler only. T4.S2
	// calls srv.Shutdown(ctx) on this object.
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
