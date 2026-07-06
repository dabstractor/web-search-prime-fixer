# PRP — P1.M1.T3.S1: Structured JSON logger to stderr with header redaction

## Goal

**Feature Goal**: Provide the **redacting structured logger** that every other
runtime component emits through — `type logger`, `func newLogger(w io.Writer,
level string) *logger`, method `func (l *logger) log(level string, msg string,
fields map[string]any)`, and helper `func redactHeaders(h http.Header)
map[string]any` — living in `main.go` (`package main`). It writes **one JSON
object per line** to an injectable `io.Writer` (`os.Stderr` in production; a
`bytes.Buffer` in tests), honors level filtering (`debug < info < warn < error`:
a message is emitted only when its level ≥ the logger's configured level), stamps
every line with an RFC3339 `ts`, and guarantees the four sensitive headers
(`Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization`) are never logged
verbatim (PRD §6 "No secrets in logs", PRD §13, PRD §15).

**Deliverable**: Two changes at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`:
1. **MODIFY** `main.go` — add the import block (`encoding/json`, `io`, `net/http`,
   `time`), the `type logger` struct, the `func newLogger` constructor, the
   `func (l *logger) log` method, the `func redactHeaders` helper, and the small
   level machinery (an unexported `levelNum` helper + unexported level constants).
   **Preserve `func main() {}` byte-for-byte** (the bootstrap that wires the logger
   in is P1.M1.T4.S1 — NOT this subtask).
2. **CREATE** `logger_test.go` — stdlib-`testing`-only unit tests (write to a
   `bytes.Buffer`, never real stderr) covering: `redactHeaders` redacts all four
   sensitive headers and preserves the rest; level filtering drops a `debug`
   message at `level=info` while keeping `info`/`warn`/`error`; each emitted line
   is valid JSON with `ts`/`level`/`msg` and the merged `fields`; the RFC3339 `ts`
   round-trips through `time.Parse`; exactly one line (trailing `'\n'`) is written
   per `log` call.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. `logger_test.go` passes every case above. No code
calls the logger yet (the bootstrap is P1.M1.T4); `config.go`/`resolve_test.go`
(the parallel P1.M1.T2.S2 work) are untouched; `main()` stays the empty stub. The
four imported packages are stdlib only — `go.mod` gains zero `require`s.

## User Persona

**Target User**: Downstream subtask implementers (P1.M1.T4.S1 bootstrap; P1.M4.T3.S1
proxy logging) and, ultimately, the operator reading the proxy's stderr.

**Use Case**: The bootstrap calls `newLogger(os.Stderr, cfg.LogLevel)` once and
emits `startup`/`shutdown` (P1.M1.T4); the proxy handler calls `l.log(...)` for
`rewrite` / `upstream_error` and (at debug) `forward` events (P1.M4.T3), passing
`redactHeaders(req.Header)` whenever request headers enter a log line.

**User Journey**: process supervisor starts the binary → stderr receives JSON
lines → a tool (or human) tailing stderr parses each line as one JSON object; no
line ever contains a raw `Authorization`/`Cookie` value.

**Pain Points Addressed**: (1) operators need structured (machine-parseable) logs
on stderr while stdout stays clean for the supervisor; (2) the `Authorization`
header is forwarded verbatim to the upstream (PRD §13) and must NEVER appear in a
log line — a single redaction helper used everywhere eliminates the "forgot to
redact here" footgun; (3) noisy `debug`/`forward` lines must be suppressible
without code changes (config-driven level).

## Why

- This is the **sole logging primitive** for the whole proxy. P1.M1.T4.S1
  (`startup`/`shutdown`) and P1.M4.T3.S1 (`rewrite`/`upstream_error`/debug
  `forward`) both import `*logger`, `newLogger`, and `redactHeaders` from `main.go`.
  Centralizing the JSON shape, the `ts` stamp, and the redaction list here means
  every event is consistent and the four sensitive headers can only ever be
  redacted — there is one place that knows how.
- Implements the **security invariant** end-to-end at the logging layer: PRD §6
  ("No secrets in logs. The `Authorization` header is never logged") and PRD §13
  ("Any header named `Authorization`, `Cookie`, `Set-Cookie`, or
  `Proxy-Authorization` is printed as `<redacted>`"). Because the redaction helper
  is the documented way to put headers into a log line, secrets cannot leak even
  if a caller forgets — but more importantly, callers have a one-call path that is
  obviously correct.
- Implements **PRD §15 "Logging"**: "Structured JSON lines to stderr (so stdout
  stays clean for any process supervisor). Fields: `ts`, `level`, `msg`, plus
  context." and "Levels honored". This subtask delivers the emitter + level
  filter; the four named events (`startup`/`rewrite`/`upstream_error`/`shutdown`,
  plus the debug-only `forward`) are *emitted by* the later subtasks through this
  emitter.
- Establishes the **injectable-writer testing pattern** (`newLogger(&buf, level)`)
  that P1.M4/P1.M5 will reuse to assert exact log content without touching real
  stderr or capturing process output.

## What

`main.go` gains (all `package main`):

- **`type logger struct { w io.Writer; level int }`** — `w` is where JSON lines
  go (production: `os.Stderr`; tests: `*bytes.Buffer`); `level` is the numeric
  threshold (see level machinery). Unexported fields; callers build it via
  `newLogger`.
- **`func newLogger(w io.Writer, level string) *logger`** — maps the level
  **string** (`"debug"`/`"info"`/`"warn"`/`"error"`) to its numeric rank and
  returns `&logger{w: w, level: <rank>}`. An **unrecognized or empty** level
  string is treated as `"info"` (matches `Config.LogLevel`'s default; the logger
  never panics on a bad level — `ResolveConfig` deliberately does NOT validate the
  level enum, see P1.M1.T2.S2's scope guard).
- **`func (l *logger) log(level string, msg string, fields map[string]any)`** —
  **(1)** level filter: if the message's level rank `< l.level`, return (emit
  nothing). **(2)** build `m := map[string]any{"ts": ..., "level": level, "msg":
  msg}` then range `fields` adding each key (caller fields are added last; none of
  our own events reuse `ts`/`level`/`msg`). **(3)** `ts` =
  `time.Now().UTC().Format(time.RFC3339)`. **(4)** `b, err := json.Marshal(m)`; on
  error write nothing (avoids emitting malformed JSON); on success
  `l.w.Write(append(b, '\n'))` — exactly one JSON object + one newline per call.
- **`func redactHeaders(h http.Header) map[string]any`** — returns a new map
  copying every header name from `h`; for a header whose `http.CanonicalHeaderKey`
  is `Authorization`/`Cookie`/`Set-Cookie`/`Proxy-Authorization` the value is the
  literal string `"<redacted>"`, otherwise the original `[]string` value is kept.

`logger_test.go` writes to a `bytes.Buffer`, asserts via `json.Unmarshal` (NOT byte
comparison — see the HTML-escaping gotcha), and covers redaction, level filtering,
valid-JSON output, RFC3339 `ts` round-trip, and the one-line-per-call invariant.

### Success Criteria

- [ ] `type logger struct { w io.Writer; level int }` exists in `main.go`.
- [ ] `func newLogger(w io.Writer, level string) *logger` exists; `"debug"`→lowest,
      `"error"`→highest; unknown/empty → `"info"` rank.
- [ ] `func (l *logger) log(level, msg string, fields map[string]any)` emits
      exactly one JSON line (`{...}\n`) when the message level ≥ the configured
      level, and emits nothing when below.
- [ ] Every emitted line is valid JSON (passes `json.Valid` after trimming the
      trailing newline) and decodes to a map containing `ts`, `level`, `msg`, plus
      the merged `fields`.
- [ ] `ts` is RFC3339: `time.Parse(time.RFC3339, line["ts"])` succeeds.
- [ ] `func redactHeaders(h http.Header) map[string]any` maps each of
      `Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization` to the string
      `"<redacted>"` and leaves every other header's value untouched (as its
      original `[]string`).
- [ ] `redactHeaders` is case-insensitive on the key (a hand-built
      `http.Header{"authorization": ...}` is still redacted), and preserves the
      original key casing in the output.
- [ ] Level filter is verified: at `level="info"`, a `log("debug", ...)` call
      writes nothing; `log("info"/"warn"/"error", ...)` each write one line.
- [ ] `func main() {}` is unchanged (still the empty stub); the logger is NOT yet
      called from `main` or anywhere else.
- [ ] `main.go` imports only `encoding/json`, `io`, `net/http`, `time` (all
      stdlib); `go.mod` gains zero `require`s.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.

## All Needed Context

### Context Completeness Check

_Pass._ The four symbols, their exact signatures, the level ordering, the field
set (`ts`/`level`/`msg`+fields), the four redacted headers, and the
"bytes.Buffer, not real stderr" mocking rule are all stated verbatim in the work
item's contract. The four stdlib primitives they rest on (`json.Marshal` of a
`map[string]any`, `time.RFC3339` UTC, `http.Header` as `map[string][]string`, and
`http.CanonicalHeaderKey`) are **verified on-disk** against the installed
`go1.26.4` toolchain in `research/verify-logger-stdlib.md`, including the **one
sharp edge** (`json.Marshal` HTML-escapes `<` → `\u003c`, so `"<redacted>"` is
emitted as `"\u003credacted\u003e"` in raw bytes — valid JSON, but tests MUST
compare decoded values via `json.Unmarshal`). The on-disk `main.go` INPUT (the
T1.S1 stub) has been read. The boundary with the parallel P1.M1.T2.S2 work
(edits `config.go`/creates `resolve_test.go`; does NOT touch `main.go`) and with
the future P1.M1.T4.S1 (fills in `main()`) is stated explicitly. An agent with no
prior knowledge of this codebase can implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative logging + security contract.
- file: PRD.md
  why: §15 "Logging" (structured JSON lines to stderr; fields ts/level/msg plus
        context; "Levels honored"; debug adds per-request forward lines); §13
        "Headers, credentials, security" (the redacting logger wraps stderr;
        Authorization/Cookie/Set-Cookie/Proxy-Authorization → "<redacted>";
        Authorization is forwarded verbatim and NEVER read/logged/stored); §6
        "Non-functional requirements" ("No secrets in logs. The Authorization
        header is never logged.").
  critical: The redaction list is EXACTLY four headers — Authorization, Cookie,
        Set-Cookie, Proxy-Authorization (PRD §13). Do NOT add others (e.g.
        X-Api-Key) — that would be scope creep against the contract. Output goes
        to stderr so stdout stays clean for a process supervisor; production wires
        os.Stderr, tests inject a bytes.Buffer.

# SCOPE BOUNDARY — what the module skeleton already provides (the INPUT).
- file: plan/001_c0abc3757e9a/P1M1T1S1/PRP.md
  why: go.mod (module web-search-prime-fixer, go 1.22, zero requires), doc.go,
        main.go (currently just `package main` + `func main() {}`), testdata/.
  critical: This subtask MODIFIES main.go (adds the logger) but MUST leave
        `func main() {}` as the empty stub — the bootstrap (ResolveConfig →
        newLogger(os.Stderr, cfg.LogLevel) → server) is P1.M1.T4.S1. Do NOT add a
        require to go.mod; all four new imports are stdlib.

# PARALLEL WORK — what P1.M1.T2.S2 is doing at the same time (do NOT collide).
- file: plan/001_c0abc3757e9a/P1M1T2S2/PRP.md
  why: T2.S2 MODIFIES config.go (adds ResolveConfig + helpers) and CREATES
        resolve_test.go. It does NOT touch main.go. T3.S1 MODIFIES main.go and
        CREATES logger_test.go. The two subtasks share NO source file, so they
        can land in either order / in parallel without merge conflict.
  critical: Do NOT edit config.go or resolve_test.go here. Do NOT call
        ResolveConfig or reference Config from the logger — newLogger takes a raw
        `level string` precisely so the logger is decoupled from Config (T4.S1 is
        what bridges cfg.LogLevel → newLogger). The logger must compile and test
        on its own, independent of whether T2.S2 has landed.

# CONSUMERS — what later subtasks expect from these symbols (forward contract).
- file: PRD.md
  why: §15 events: `startup`/`shutdown` (P1.M1.T4.S1) and
        `rewrite`/`upstream_error`/debug `forward` (P1.M4.T3.S1) are all emitted
        via `l.log(level, msg, fields)`. Request headers entering a log line go
        through `redactHeaders(req.Header)`.
  critical: Keep `log`'s signature EXACTLY `log(level string, msg string, fields
        map[string]any)` and `newLogger`'s EXACTLY `newLogger(w io.Writer, level
        string) *logger` — downstream call sites are written against these. A
        variadic-fields or struct-fields redesign would break the contract.

# VERIFIED GOTCHAS — on-disk proof of the four stdlib primitives' behavior.
- file: plan/001_c0abc3757e9a/P1M1T3S1/research/verify-logger-stdlib.md
  why: go1.26.4 on-disk run confirming: json.Marshal(map[string]any) sorts keys
        (output is valid JSON, deterministic); time.RFC3339 UTC → "...Z" and
        round-trips through time.Parse; http.Header is map[string][]string with
        canonical keys; CanonicalHeaderKey("authorization")=="Authorization".
  critical: json.Marshal HTML-ESCAPES '<','>','&' by default, so "<redacted>" is
        emitted as the bytes "\u003credacted\u003e". This is valid JSON (decodes
        to "<redacted>") but a byte/string-Contains assertion FAILS. Every test
        MUST json.Unmarshal the line and compare the decoded Go value. This is
        the #1 gotcha; it is why the PRP mandates Unmarshal-based assertions and
        keeps default escaping ON (simplest literal reading of "marshals to one
        JSON line"; no json.Encoder/SetEscapeHTML wiring or double-newline risk).

# ARCHITECTURE — stdlib-only invariant.
- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: "No third-party dependencies are required or allowed (PRD §6, §9)."
        Confirms the logger is hand-rolled in main.go with encoding/json (no
        log/slog, zerolog, etc.).
  critical: Do NOT import golang.org/x/... or any third-party logger. The four
        stdlib imports (encoding/json, io, net/http, time) are the complete set.

# Go stdlib refs — exact semantics relied upon (stable; verified on-disk above).
- url: https://pkg.go.dev/encoding/json#Marshal
  why: Marshals a map[string]any to compact JSON (keys sorted). Returns ([]byte,
        error); errors only for unsupported types (chan/func/recursive). We
        append '\n' for one-line-per-object.
- url: https://pkg.go.dev/time#pkg-constants
  why: time.RFC3339 == "2006-01-02T15:04:05Z07:00". time.Now().UTC().Format(...)
        yields a parseable, timezone-stamped ts. time.Parse(time.RFC3339, s)
        validates it in tests.
- url: https://pkg.go.dev/net/http#Header
  why: type Header map[string][]string. Ranging yields canonical keys; values are
        []string. http.CanonicalHeaderKey normalizes for case-insensitive
        comparison.
- url: https://pkg.go.dev/io#Writer
  why: type Writer interface { Write([]byte) (int, error) }. os.Stderr and
        *bytes.Buffer both satisfy it — this is the seam that makes the logger
        testable without real stderr.
```

### Current Codebase tree (the INPUT state of this subtask)

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22; NO requires (T1.S1)
  doc.go            # "Package main ..." comment (T1.S1)
  main.go           # package main + func main() {} STUB  ← THIS SUBTASK EXTENDS IT
  config.go         # package main; Config/DefaultConfig/LoadConfig (S1)         [T2.S1 — DO NOT EDIT]
                    #   (T2.S2, in parallel, APPENDS ResolveConfig + helpers)
  config_test.go    # S1's tests                                  [T2.S1 — DO NOT EDIT]
  resolve_test.go   # (created by parallel T2.S2, if it has landed)            [T2.S2 — DO NOT EDIT]
  testdata/.gitkeep # placeholder (T1.S1; P1.M3.T2 adds *.sse)
  PRD.md            # unchanged
  plan/...          # unchanged (READ-ONLY planning scaffolding)
# NOTE: no logger, no logger_test.go yet. This subtask adds both.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod            # UNCHANGED (zero requires; the 4 new imports are stdlib)
  doc.go            # UNCHANGED
  main.go           # MODIFIED — import block += encoding/json, io, net/http, time;
                    #            += level constants + levelNum (unexported)       [THIS]
                    #            += type logger struct{...}                       [THIS]
                    #            += func newLogger(w io.Writer, level string)*logger [THIS]
                    #            += func (l *logger) log(level,msg string, fields map[string]any) [THIS]
                    #            += func redactHeaders(h http.Header) map[string]any [THIS]
                    #            (func main() {} UNCHANGED — still the empty stub)
  logger_test.go    # NEW — TestRedactHeaders / TestLogger_LevelFiltering /      [THIS]
                    #        TestLogger_JSONOutput / TestLogger_OneLinePerCall /
                    #        TestLogger_RFC3339Ts / TestNewLogger_UnknownLevel
                    #        (writes to a *bytes.Buffer; asserts via json.Unmarshal)
  config.go         # UNCHANGED (T2.S1/T2.S2 own it)
  config_test.go    # UNCHANGED
  resolve_test.go   # UNCHANGED (T2.S2 owns it, if present)
  testdata/.gitkeep # UNCHANGED
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL (#1): json.Marshal HTML-ESCAPES '<','>','&' by default. The redaction
// marker "<redacted>" is therefore emitted as the bytes "\u003credacted\u003e".
// This is VALID JSON — json.Unmarshal of those bytes yields the Go string
// "<redacted>". But a byte-level check FAILS:
//   strings.Contains(buf.String(), "<redacted>")   // ❌ raw bytes have \u003c
//   bytes.Contains(buf.Bytes(), []byte("<redacted>")) // ❌ same
// EVERY test MUST decode first: json.Unmarshal(buf.Bytes(), &m); m["Authorization"] == "<redacted>"
// Decision: keep default escaping ON (json.Marshal + append '\n'). It is the most
// literal reading of the contract and avoids json.Encoder's double-newline hazard.

// CRITICAL: The level filter is `messageRank >= thresholdRank`, NOT `>`. With
// debug=0,info=1,warn=2,error=3: a logger at "info"(1) emits info(1≥1✓),
// warn(2≥1✓), error(3≥1✓) and drops debug(0≥1✗). Using `>` would wrongly drop
// messages AT the configured level (an "info" logger would drop "info" messages).

// CRITICAL: newLogger takes a level STRING but the struct stores an int. Map the
// string→int ONCE in newLogger (via levelNum); log() compares ints. Do NOT
// re-parse the string on every log call.

// CRITICAL: An unrecognized or empty level string must NOT panic. Map it to the
// "info" rank (matches Config.LogLevel's default). ResolveConfig (P1.M1.T2.S2)
// deliberately does NOT validate LogLevel against the enum, so newLogger can
// receive e.g. "trace" or "" — both must be tolerated as "info".

// CRITICAL: redactHeaders compares via http.CanonicalHeaderKey(k), NOT a raw
// string switch on k. A hand-built http.Header{"authorization": ...} (lowercase)
// or {"AUTHORIZATION": ...} must still be redacted. Real http.Header keys are
// already canonical, but the helper must be robust to odd casing. The STORED
// output key is the original k (preserve the map's casing — "copies header
// names/values").

// CRITICAL: redactHeaders returns a NEW map; it MUST NOT mutate the input h. The
// input http.Header is the live request header map (proxy.go will pass
// req.Header); mutating it would corrupt the forwarded request. Build
// `out := map[string]any{}` and range-copy.

// GOTCHA: http.Header values are []string. A NON-redacted header therefore
// marshals to a JSON ARRAY (e.g. "Content-Type":["application/json"]); a REDACTED
// header marshals to the plain STRING "<redacted>" (we replace the whole value).
// This is correct and intended — the redaction collapses one-or-many sensitive
// values to a single marker.

// GOTCHA: Set-Cookie can have MULTIPLE values (http.Header.Add). redactHeaders
// collapses all of them to the single string "<redacted>" — exactly the security
// behavior we want (never log any cookie value).

// GOTCHA: json.Marshal(map[string]any) sorts keys alphabetically. The contract's
// "builds a map with ts, level, msg, then fields" describes Go build order
// (which governs overwrite precedence for duplicate keys), NOT JSON key order.
// Output is deterministic and fine — JSON consumers key by name. Do NOT try to
// enforce ts/level/msg ordering in the output (would require a struct or
// json.Encoder; out of scope and unnecessary).

// GOTCHA: If json.Marshal fails (e.g. a field value of an unsupported type like
// func/chan — should not happen with our controlled events, but be defensive),
// write NOTHING. Never emit a partial/malformed line. The simplest correct body:
//   b, err := json.Marshal(m); if err != nil { return }; l.w.Write(append(b, '\n'))

// SCOPE GUARD: Do NOT call the logger from main() or anywhere else this subtask.
// The bootstrap (ResolveConfig → newLogger(os.Stderr, cfg.LogLevel) → server) is
// P1.M1.T4.S1. main() stays `func main() {}`. Do NOT add flag parsing.

// SCOPE GUARD: Do NOT implement the four named EVENTS (startup/rewrite/
// upstream_error/shutdown/forward). This subtask delivers the EMITTER + redaction
// helper only. The events are emitted by P1.M1.T4 and P1.M4.T3 via l.log(...).

// SCOPE GUARD: Do NOT touch config.go / resolve_test.go (parallel T2.S2). Do NOT
// add a go.mod require. Do NOT import log (the stdlib "log" package) or log/slog
// or any third-party logger — hand-roll with encoding/json as the contract says.

// TEST GOTCHA: No t.Parallel() needed (no shared global state — each test makes
// its own *bytes.Buffer and logger). It is harmless to omit; do not add it.
```

## Implementation Blueprint

### Data models and structure

No persistent data model. The logger is a small in-memory struct:

```go
// logger writes structured JSON log lines (one object per line) to an io.Writer.
// See the doc comment on `type logger` below for the full contract.
type logger struct {
	w     io.Writer // destination: os.Stderr in production, *bytes.Buffer in tests
	level int       // numeric threshold (see levelNum); messages with rank < level are dropped
}
```

Level machinery (unexported):

```go
const (
	levelDebug = iota // 0 — lowest
	levelInfo         // 1
	levelWarn         // 2
	levelError        // 3 — highest
)

// levelNum maps a level string to its numeric rank. debug < info < warn < error.
// An unrecognized or empty string maps to levelInfo (matches Config.LogLevel's
// default; the logger never panics on a bad level).
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
```

`redactHeaders` output type: `map[string]any` where sensitive keys → `"<redacted>"`
(string) and all other keys → the original `[]string` value.

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT (read-only, no edits)
  - RUN: `cd /home/dustin/projects/web-search-prime-fixer && cat main.go go.mod`
  - EXPECT: main.go == `package main\n\nfunc main() {}\n`; go.mod has
    `module web-search-prime-fixer` + `go 1.22` with NO require block.
  - RUN: `go build ./...` → MUST already exit 0 (T1.S1 landed; T2.S2 may or may
    not have landed yet — both leave the module building).
  - WHY: This subtask's INPUT is "the T1.S1 main.go stub". If main.go is not the
    bare stub, STOP — reconcile before editing. Do NOT recreate main.go.

Task 1: MODIFY main.go — replace the bare stub with stub + logger
  - FILE: /home/dustin/projects/web-search-prime-fixer/main.go
  - CURRENT CONTENT (the entire file):
        package main

        func main() {}
  - REPLACE WITH: package main + the import block + level machinery + type logger
    + newLogger + log + redactHeaders + `func main() {}` — EXACTLY the code in
    "Implementation Patterns & Key Details" below (verbatim; it is small and fully
    specified).
  - WHY: The contract says "Define in main.go a logger type". Keep everything in
    main.go (the consumers — bootstrap in main.go, proxy in proxy.go — are all
    package main and share these symbols). PRESERVE `func main() {}` unchanged at
    the end.

Task 2: CREATE logger_test.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/logger_test.go
  - PACKAGE: `package main`
  - IMPORTS: `"bytes"`, `"encoding/json"`, `"net/http"`, `"testing"`, `"time"`
    (stdlib only). (Do NOT import "strings" for redaction checks — see gotcha #1:
    use json.Unmarshal, not string matching.)
  - IMPLEMENT the tests in "Validation Loop → Level 2" (each spelled out with
    setup + assertion). Every test builds its own `var buf bytes.Buffer` and
    `newLogger(&buf, "<level>")`; every assertion decodes JSON via
    `json.Unmarshal` rather than byte matching.
  - NAMING: TestRedactHeaders, TestLogger_LevelFiltering, TestLogger_JSONOutput,
    TestLogger_OneLinePerCall, TestLogger_RFC3339Ts, TestNewLogger_UnknownLevel.
  - WHY: Covers the contract's three explicit cases (redaction, level filtering,
    valid JSON) plus the RFC3339 ts, the one-line-per-call invariant, and the
    unknown-level tolerance.

Task 3: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w main.go logger_test.go
        go build ./...
        go vet ./...
        gofmt -l .
        go test ./...
        go test -run 'TestRedactHeaders|TestLogger|TestNewLogger' -v ./...
  - EXPECT: gofmt silent; build/vet clean; all tests PASS including every
    logger test. Re-run after any edit until all clean.
```

### Implementation Patterns & Key Details

```go
// main.go — FULL INTENDED CONTENT (replace the bare stub with this; verbatim).
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

func main() {}
```

```go
// logger_test.go — shared helper (put near the top).

// decodeLines splits a buffer of newline-terminated JSON log lines into decoded
// maps. Fails the test if any line is not valid JSON. Skips a trailing empty line
// (from the final newline). Use this instead of byte/string matching because
// json.Marshal HTML-escapes '<' to "\u003c" (see research/verify-logger-stdlib.md).
func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue // trailing newline / blank line
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"           # main.go + logger_test.go are package main, repo root

SYMBOLS INTRODUCED (consumed by later subtasks):
  - type logger                            # -> P1.M1.T4.S1 holds one in main()
  - func newLogger(w io.Writer, level string) *logger
        # -> P1.M1.T4.S1: newLogger(os.Stderr, cfg.LogLevel)
  - func (l *logger) log(level, msg string, fields map[string]any)
        # -> P1.M1.T4.S1 (startup/shutdown), P1.M4.T3.S1 (rewrite/upstream_error/
        #    debug forward)
  - func redactHeaders(h http.Header) map[string]any
        # -> P1.M4.T3.S1: pass req.Header through it before putting headers in a
        #    log line (e.g. l.log("debug","forward", map[string]any{"headers":
        #    redactHeaders(req.Header)}))
  - func levelNum(level string) int        # UNEXPORTED — implementation detail
  - const levelDebug/levelInfo/levelWarn/levelError  # UNEXPORTED

NO INTEGRATION IN main() THIS SUBTASK:
  - Do NOT call newLogger or log from main(). Do NOT add flag parsing or server
    setup. main() stays `func main() {}`. P1.M1.T4.S1 is where cfg.LogLevel is
    bridged to newLogger(os.Stderr, ...).

NO CONFIG COUPLING THIS SUBTASK:
  - newLogger takes a raw `level string` (NOT a Config). This keeps the logger
    compilable/testable independently of whether P1.M1.T2.S2 (ResolveConfig) has
    landed. T4.S1 bridges cfg.LogLevel → newLogger.

NO NEW ENV VARS / NO go.mod CHANGES:
  - encoding/json, io, net/http, time are all stdlib. go.mod gains zero requires.
  - The WSPF_LOG_LEVEL env var (already introduced by T2.S2) feeds cfg.LogLevel,
    which T4.S1 passes to newLogger — NOT this subtask's concern.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -w main.go logger_test.go             # format in place
gofmt -l .                                  # MUST print nothing
go vet ./...                                # MUST exit 0, no output
go build ./...                              # MUST exit 0, no output

# Dependency-free invariant (PRD §6/§9) — grep for stray imports:
grep -E '^\s*"github\.com|^\s*"gopkg\.in|^\s*"golang\.org/x|^\s*"log"' main.go logger_test.go || true
# Expected: empty. main.go may import only: encoding/json, io, net/http, time.
# logger_test.go: bytes, encoding/json, net/http, testing, time.

# Expected: gofmt silent; vet/build clean; no third-party or stdlib-"log" imports.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...                                                    # MUST pass
go test -run 'TestRedactHeaders|TestLogger|TestNewLogger' -v ./...  # see these tests
```

Required `logger_test.go` test cases (each builds its own `var buf bytes.Buffer`;
every assertion decodes JSON via `decodeLines` / `json.Unmarshal`, never byte
matching — see gotcha #1):

```go
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

func decodeLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	out := []map[string]any{}
	for _, line := range bytes.Split(buf.Bytes(), []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(line, &m); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

// (a) redactHeaders: the four sensitive headers → "<redacted>"; others preserved.
func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer hunter2")
	h.Set("Cookie", "session=abc123")
	h.Set("Set-Cookie", "a=1")
	h.Add("Set-Cookie", "b=2") // multiple cookies all collapse to one marker
	h.Set("Proxy-Authorization", "Basic dXNlcjpwYXNz")
	h.Set("Content-Type", "application/json")
	h.Set("Mcp-Session-Id", "sess-42")

	got := redactHeaders(h)

	for _, sensitive := range []string{"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization"} {
		if got[sensitive] != "<redacted>" {
			t.Errorf("%s = %#v, want \"<redacted>\"", sensitive, got[sensitive])
		}
	}
	// Non-sensitive headers keep their []string value.
	if ct, ok := got["Content-Type"].([]string); !ok || len(ct) != 1 || ct[0] != "application/json" {
		t.Errorf("Content-Type = %#v, want []string{\"application/json\"}", got["Content-Type"])
	}
	if sid, ok := got["Mcp-Session-Id"].([]string); !ok || len(sid) != 1 || sid[0] != "sess-42" {
		t.Errorf("Mcp-Session-Id = %#v, want []string{\"sess-42\"}", got["Mcp-Session-Id"])
	}
}

// (a.2) redactHeaders is case-insensitive on the key and does not mutate the input.
func TestRedactHeaders_CaseInsensitiveAndNonMutating(t *testing.T) {
	h := http.Header{"authorization": []string{"secret"}, "SET-COOKIE": []string{"c=v"}}
	got := redactHeaders(h)
	if got["authorization"] != "<redacted>" {
		t.Errorf("lowercase authorization = %#v, want \"<redacted>\"", got["authorization"])
	}
	if got["SET-COOKIE"] != "<redacted>" {
		t.Errorf("SET-COOKIE = %#v, want \"<redacted>\"", got["SET-COOKIE"])
	}
	// Input must be untouched (it's the live request header map in production).
	if h.Get("authorization") != "secret" {
		t.Errorf("input header mutated: %v", h)
	}
}

// (b) Level filtering: at level=info, debug is dropped; info/warn/error kept.
func TestLogger_LevelFiltering(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")

	l.log("debug", "should be dropped", nil)
	if buf.Len() != 0 {
		t.Errorf("debug message written at info level: %q", buf.String())
	}

	for _, lvl := range []string{"info", "warn", "error"} {
		buf.Reset()
		l.log(lvl, "kept", nil)
		if buf.Len() == 0 {
			t.Errorf("%s message dropped at info level", lvl)
		}
	}
}

// (b.2) At level=error, only error is kept.
func TestLogger_LevelFiltering_ErrorOnly(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "error")
	for _, lvl := range []string{"debug", "info", "warn"} {
		buf.Reset()
		l.log(lvl, "dropped", nil)
		if buf.Len() != 0 {
			t.Errorf("%s written at error level: %q", lvl, buf.String())
		}
	}
	buf.Reset()
	l.log("error", "kept", nil)
	if buf.Len() == 0 {
		t.Error("error message dropped at error level")
	}
}

// (c) Output is valid JSON with ts/level/msg + merged fields; fields are added.
func TestLogger_JSONOutput(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "debug")
	l.log("info", "forward", map[string]any{
		"req_id":  "r-1",
		"method":  "POST",
		"headers": redactHeaders(http.Header{"Authorization": []string{"Bearer x"}}),
	})

	lines := decodeLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	m := lines[0]
	if m["level"] != "info" {
		t.Errorf("level = %#v, want \"info\"", m["level"])
	}
	if m["msg"] != "forward" {
		t.Errorf("msg = %#v, want \"forward\"", m["msg"])
	}
	if m["req_id"] != "r-1" {
		t.Errorf("req_id = %#v, want \"r-1\"", m["req_id"])
	}
	// Nested redacted headers decode back to "<redacted>".
	hdrs, ok := m["headers"].(map[string]any)
	if !ok {
		t.Fatalf("headers = %#v, want a JSON object", m["headers"])
	}
	if hdrs["Authorization"] != "<redacted>" {
		t.Errorf("headers.Authorization = %#v, want \"<redacted>\"", hdrs["Authorization"])
	}
}

// (c.2) One JSON object per log call, each terminated by a newline.
func TestLogger_OneLinePerCall(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	l.log("info", "first", nil)
	l.log("warn", "second", nil)
	l.log("error", "third", nil)

	lines := decodeLines(t, &buf)
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3", len(lines))
	}
	wantMsgs := []string{"first", "second", "third"}
	for i, want := range wantMsgs {
		if lines[i]["msg"] != want {
			t.Errorf("line %d msg = %#v, want %q", i, lines[i]["msg"], want)
		}
	}
	// Buffer must end with a newline (one object per line).
	if !bytes.HasSuffix(buf.Bytes(), []byte("\n")) {
		t.Error("buffer does not end with a newline")
	}
}

// (d) ts is RFC3339 — round-trips through time.Parse.
func TestLogger_RFC3339Ts(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	l.log("info", "x", nil)
	lines := decodeLines(t, &buf)
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1", len(lines))
	}
	ts, ok := lines[0]["ts"].(string)
	if !ok {
		t.Fatalf("ts = %#v, want a string", lines[0]["ts"])
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		t.Errorf("ts %q is not RFC3339: %v", ts, err)
	}
}

// (e) newLogger tolerates an unknown/empty level (treated as info).
func TestNewLogger_UnknownLevel(t *testing.T) {
	for _, lvl := range []string{"", "trace", "verbose", "INFO"} {
		var buf bytes.Buffer
		l := newLogger(&buf, lvl) // must NOT panic
		// Treated as info: debug dropped, info kept.
		l.log("debug", "dropped", nil)
		if buf.Len() != 0 {
			t.Errorf("level=%q: debug written (expected info-rank behavior): %q", lvl, buf.String())
		}
		l.log("info", "kept", nil)
		if buf.Len() == 0 {
			t.Errorf("level=%q: info dropped (expected info-rank behavior)", lvl)
		}
	}
}
```

```bash
# Expected: every test PASS. If TestLogger_JSONOutput fails on the redacted
# header, the most likely cause is a byte/string assertion instead of
# json.Unmarshal (see gotcha #1) — the raw bytes contain "\u003c", not '<'.
# If TestRedactHeaders_CaseInsensitive fails, the switch is comparing the raw key
# k instead of http.CanonicalHeaderKey(k). If TestNewLogger_UnknownLevel panics,
# the default case of levelNum is missing.
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# Smoke: the module still builds a binary with the extended main.go.
go build -o /tmp/wspf-logger-smoke . && /tmp/wspf-logger-smoke; echo "exit=$?"
rm -f /tmp/wspf-logger-smoke
# Expected: builds; runs and exits 0 with NO output on stdout or stderr (main()
# is still the empty stub — the logger is not yet wired in; this is correct).

# Decoupling check: logger_test.go must compile/pass even if resolve_test.go /
# ResolveConfig (parallel T2.S2) have NOT landed. (They share package main but
# are independent symbols.)
go test -run 'TestRedactHeaders|TestLogger|TestNewLogger' ./...   # expect PASS
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# godoc check — the "[Mode A] doc comment" deliverable:
go doc . logger            # the type's doc (stderr output, levels, redaction note)
go doc . redactHeaders     # the helper's doc (the four redacted headers)
# Expected: both print their godoc; the redactHeaders doc names all four headers
# and notes "<redacted>"; the logger doc notes output goes to stderr.

# Contract-shape check: the four exported symbols exist with the exact signatures.
grep -nE 'type logger struct|func newLogger\(w io\.Writer, level string\) \*logger|func \(l \*logger\) log\(level string, msg string, fields map\[string\]any\)|func redactHeaders\(h http\.Header\) map\[string\]any' main.go
# Expected: four matches (one per line).

# No-secrets guard: the redaction list is exactly the four contract headers.
grep -oE '"Authorization", "Cookie", "Set-Cookie", "Proxy-Authorization"' main.go
# Expected: one match (the redaction switch). Adding/removing a header here is a
# contract violation.

# main()-untouched guard:
grep -c '^func main() {}' main.go   # expect 1

# No-coupling guard: the logger does not import "os" (no os.Stderr hard-coding —
# the writer is injected) and does not reference Config.
! grep -nE '\bos\.|"web-search-prime-fixer/config"|Config\{' main.go
# Expected: the negated grep succeeds (exit 0) — the logger is decoupled from
# os.Stderr (injected via newLogger) and from Config.
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0, no output.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` passes (exit 0), including all `logger_test.go` tests.
- [ ] `main.go` imports only `encoding/json`, `io`, `net/http`, `time`.
- [ ] `logger_test.go` imports only `bytes`, `encoding/json`, `net/http`, `testing`, `time`.
- [ ] `go.mod` is unchanged (zero `require`s).

### Feature Validation

- [ ] `type logger struct { w io.Writer; level int }` exists in `main.go`.
- [ ] `func newLogger(w io.Writer, level string) *logger` exists; debug<info<warn<error; unknown/empty → info.
- [ ] `func (l *logger) log(level, msg string, fields map[string]any)` emits one JSON line per call when the message level ≥ the configured level, and nothing when below.
- [ ] Every emitted line is valid JSON with `ts`, `level`, `msg`, and the merged `fields`.
- [ ] `ts` parses as RFC3339 (`time.Parse(time.RFC3339, ts)` succeeds).
- [ ] `func redactHeaders(h http.Header) map[string]any` maps Authorization/Cookie/Set-Cookie/Proxy-Authorization → `"<redacted>"`; other headers keep their `[]string` value.
- [ ] `redactHeaders` is case-insensitive (canonical comparison) and does not mutate the input.
- [ ] `go doc . logger` and `go doc . redactHeaders` print the Mode-A doc comments (redaction list + stderr note).

### Code Quality Validation

- [ ] Level filter uses `>=` (not `>`); messages AT the configured level are emitted.
- [ ] `json.Marshal`'s HTML-escaping is accounted for: tests decode via `json.Unmarshal`, never byte/string matching.
- [ ] `newLogger` maps the level string to an int ONCE; `log` compares ints (no per-call string parse).
- [ ] `redactHeaders` builds a new map (never mutates the input `http.Header`).
- [ ] `func main() {}` is unchanged (empty stub); no code calls the logger yet.
- [ ] No third-party dependencies; no `log`/`log/slog` import.
- [ ] `config.go`/`resolve_test.go`/`config_test.go` untouched (parallel T2.S2 / prior S1 own them).

### Documentation & Deployment

- [ ] Doc comments on `type logger` and `redactHeaders` note: output goes to stderr (production), the four redacted headers, the `<redacted>` marker, and the level ordering (contract "[Mode A]").
- [ ] No README change here (the final README is P1.M5.T3.S1 — explicitly deferred).

---

## Anti-Patterns to Avoid

- ❌ Don't assert log content with `strings.Contains`/`bytes.Contains` against `"<redacted>"` — `json.Marshal` HTML-escapes `<` to `\u003c`, so the raw bytes contain `"\u003credacted\u003e"`. Decode with `json.Unmarshal` first, then compare the Go string value.
- ❌ Don't use `>` for the level comparison — an "info" logger would then drop "info" messages. The filter is `messageRank >= thresholdRank`.
- ❌ Don't re-parse the level string on every `log` call — map it to an int once in `newLogger` (via `levelNum`) and compare ints.
- ❌ Don't panic on an unknown/empty level — map it to `levelInfo` (the default). `ResolveConfig` deliberately does NOT validate the level enum (P1.M1.T2.S2 scope guard).
- ❌ Don't compare header keys with a raw string switch — use `http.CanonicalHeaderKey(k)`. A hand-built `http.Header{"authorization": ...}` must still be redacted.
- ❌ Don't mutate the input `http.Header` in `redactHeaders` — build a new map. The input is the live request header map in production; mutating it corrupts the forwarded request.
- ❌ Don't add a 5th redacted header (e.g. `X-Api-Key`) — the contract's list is EXACTLY Authorization, Cookie, Set-Cookie, Proxy-Authorization (PRD §13).
- ❌ Don't hard-code `os.Stderr` inside the logger — the writer is injected via `newLogger(w, ...)`. Hard-coding would make the logger untestable with a `bytes.Buffer`. (`os.Stderr` is wired by the bootstrap, P1.M1.T4.S1.)
- ❌ Don't couple the logger to `Config` — `newLogger` takes a raw `level string` precisely so it compiles/tests independently of the parallel config work. T4.S1 bridges `cfg.LogLevel → newLogger`.
- ❌ Don't implement the named events (startup/rewrite/upstream_error/shutdown/forward) here — this subtask delivers the EMITTER + redaction helper only. The events are emitted by P1.M1.T4 / P1.M4.T3.
- ❌ Don't wire the logger into `main()` or add flag parsing — bootstrap is P1.M1.T4.S1. `main()` stays `func main() {}`.
- ❌ Don't emit malformed/partial JSON if `json.Marshal` errors — write nothing instead (skip the line).
- ❌ Don't edit `config.go` / `resolve_test.go` / `config_test.go` — those are owned by T2.S1 / the parallel T2.S2. This subtask touches only `main.go` (MODIFY) and `logger_test.go` (CREATE).
- ❌ Don't add a `require` to `go.mod` or import `log`/`log/slog`/any third-party logger — hand-roll with `encoding/json` (PRD §6/§9 stdlib-only invariant).

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is small and fully specified — `main.go`'s entire
intended content is given verbatim above (logger type, newLogger, log, redactHeaders,
level machinery, with `func main() {}` preserved), and every test case is spelled
out with setup, assertion, and the expected decoded value. The four stdlib
primitives (`json.Marshal`, `time.RFC3339`, `http.Header`, `http.CanonicalHeaderKey`)
are **verified on-disk** against `go1.26.4` in
`research/verify-logger-stdlib.md`, including the one sharp edge (`json.Marshal`
HTML-escapes `<` → `\u003c`, so tests MUST compare decoded values via
`json.Unmarshal`). The on-disk `main.go` INPUT (the bare T1.S1 stub) has been read,
so the MODIFY instruction targets real, stable anchor text (the whole 3-line file is
replaced). The logger is decoupled from `Config` and from `os.Stderr` (both injected
by T4.S1), so it compiles/tests whether or not the parallel T2.S2 has landed — no
file collision (T2.S2 edits `config.go`/creates `resolve_test.go`; this edits
`main.go`/creates `logger_test.go`). Residual risks: (a) an agent writes a
`strings.Contains(buf.String(), "<redacted>")` assertion that fails on the escaped
bytes — mitigated by gotcha #1, the verbatim `decodeLines` helper, and the explicit
Anti-Pattern; (b) an agent uses `>` instead of `>=` for the level filter — mitigated
by the gotcha, the verbatim `log` code, and `TestLogger_LevelFiltering`; (c) an agent
mutates the input header in `redactHeaders` — mitigated by the non-mutating gotcha
and `TestRedactHeaders_CaseInsensitiveAndNonMutating`. Hence 9, not 10.
