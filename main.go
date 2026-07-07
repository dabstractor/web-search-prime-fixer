package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Log level ranks. A message is emitted only when its rank is >= the logger's
// configured level (debug < info < warn < error).
const (
	levelDebug = iota // 0
	levelInfo         // 1
	levelWarn         // 2
	levelError        // 3
)

// levelNum maps a level string to its numeric rank. An unrecognized or empty
// level maps to levelInfo (the default; the logger never panics on a bad level).
func levelNum(level string) int {
	switch level {
	case "debug":
		return levelDebug
	case "info":
		return levelInfo
	case "warn":
		return levelWarn
	case "error":
		return levelError
	default:
		return levelInfo
	}
}

// logger is a structured JSON logger. Each call to log writes exactly one JSON
// object terminated by a newline to w, so the output is one log record per line.
//
// In production w is os.Stderr (PRD §15: structured JSON lines to stderr so
// stdout stays clean for any process supervisor); in tests w is a *bytes.Buffer.
//
// Level filtering (debug < info < warn < error): a message is emitted only when
// its level is >= the logger's configured level. For example, a logger created
// with level "info" drops "debug" messages and emits "info", "warn", and "error".
//
// Each line has the fields: ts (RFC3339, UTC), level, msg, followed by the
// caller-supplied context fields.
//
// SECURITY: request headers that carry secrets must never be logged verbatim.
// Pass them through redactHeaders first — any header named Authorization, Cookie,
// Set-Cookie, or Proxy-Authorization is replaced with the literal "<redacted>"
// (PRD §6 "No secrets in logs", PRD §13).
type logger struct {
	w     io.Writer
	level int
}

// newLogger returns a logger that writes JSON lines to w, honoring the given
// level. The level string is one of "debug", "info", "warn", "error"; an
// unrecognized or empty level is treated as "info".
func newLogger(w io.Writer, level string) *logger {
	return &logger{w: w, level: levelNum(level)}
}

// log writes one structured JSON line for the message if its level is enabled.
//
// The line is a JSON object with fields ts (RFC3339 UTC), level, and msg, plus
// every key in fields added on top. If the message's level is below the logger's
// configured level, nothing is written.
func (l *logger) log(level string, msg string, fields map[string]any) {
	if levelNum(level) < l.level {
		return // below threshold: drop silently
	}
	m := map[string]any{
		"ts":    time.Now().UTC().Format(time.RFC3339),
		"level": level,
		"msg":   msg,
	}
	for k, v := range fields {
		m[k] = v
	}
	b, err := json.Marshal(m)
	if err != nil {
		return // never emit malformed JSON; skip the line
	}
	_, _ = l.w.Write(append(b, '\n'))
}

// redactHeaders returns a copy of h safe to log. Every header name is preserved;
// the value of any header whose canonical name is Authorization, Cookie,
// Set-Cookie, or Proxy-Authorization is replaced with the literal "<redacted>"
// (PRD §13). All other headers keep their original []string value. The input h
// is not modified.
func redactHeaders(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		switch http.CanonicalHeaderKey(k) {
		case "Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization":
			out[k] = "<redacted>"
		default:
			out[k] = v
		}
	}
	return out
}

// version is the proxy's build version, surfaced by GET /healthz. It defaults to
// "dev" and is overridden at link time:
//
//	go build -ldflags "-X main.version=<value>" -o web-search-prime-fixer .
//
// It MUST be a package-level var (not local to main) for the linker -X flag to
// set it (PRD §16). Verified on-disk: default "dev"; ldflags injects any string.
var version = "dev"

// healthHandler serves GET /healthz. It writes 200 with a JSON body
// {"ok":true,"version":<version>} and Content-Type application/json.
//
// It is a PURE local handler: it performs NO upstream call and reads NO network
// resource, so it always answers quickly and never depends on z.ai health
// (PRD §16: "Does not touch the upstream").
//
// The body is produced with json.Marshal of two scalars (bool + string), which
// cannot fail for our inputs; the write to the ResponseWriter is best-effort and
// its error is ignored, matching net/http handler convention.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	// PATTERN: set headers BEFORE WriteHeader (once WriteHeader is called, later
	// Header mutations are ignored).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Marshals to {"ok":true,"version":"<version>"} (keys sorted by json.Marshal;
	// "ok" < "version"). HTML-escaping does not affect semver.
	body, _ := json.Marshal(map[string]any{"ok": true, "version": version})
	_, _ = w.Write(body)
}

// logStartup emits the "startup" log event: the resolved configuration the proxy
// is actually running with (PRD §15). Fields: aliases (as a JSON array), listen,
// upstream, log_level. NEVER credentials — Config carries no credential field
// (PRD §13: Authorization is forwarded as a request header, never owned), so
// logging cfg structurally cannot leak a secret.
//
// Extracted as a named function (rather than inlined in main) so it is unit-
// testable with an injected *bytes.Buffer writer via newLogger(&buf, level).
func logStartup(l *logger, cfg Config) {
	l.log("info", "startup", map[string]any{
		"aliases":   cfg.Aliases, // []string -> JSON array
		"listen":    cfg.Listen,
		"upstream":  cfg.Upstream,
		"log_level": cfg.LogLevel,
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
