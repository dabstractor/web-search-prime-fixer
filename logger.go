package main

import (
	"encoding/json"
	"io"
	"net/http"
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
