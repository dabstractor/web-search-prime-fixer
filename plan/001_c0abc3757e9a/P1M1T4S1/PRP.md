# PRP — P1.M1.T4.S1: main() bootstrap + /healthz + version + startup log

## Goal

**Feature Goal**: Wire the **`main()` bootstrap** of the proxy in `main.go`
(`package main`): resolve config, build the redacting logger, emit the structured
`startup` log event (resolved config, **never credentials**), expose
`GET /healthz` → `200 {"ok":true,"version":"<version>"}` (a pure local handler
that **never touches the upstream**), inject the build version via
`-ldflags "-X main.version=..."` (defaulting to `"dev"`), and register the route
table (`/healthz` intercepted, **everything else forwarded** — PRD §9) — booting a
single `http.Server` on the configured listen address. Graceful shutdown and the
real passthrough forwarder belong to **P1.M1.T4.S2** (which consumes this main/mux
and depends on this subtask); here they are a clearly-marked **placeholder stub**.

**Deliverable**: Two changes at the repo root
(`/home/dustin/projects/web-search-prime-fixer`), both `package main`:
1. **MODIFY** `main.go` — PRESERVE the T3.S1 logger (type/`newLogger`/`log`/
   `redactHeaders`/level machinery) **byte-for-byte**; ADD the `os` import; ADD
   package-level `var version = "dev"`; ADD `func healthHandler(w
   http.ResponseWriter, r *http.Request)`, `func logStartup(l *logger, cfg
   Config)`, and the placeholder `func passthroughHandler(w http.ResponseWriter, r
   *http.Request)`; and **REPLACE** the empty `func main() {}` stub with the real
   bootstrap (ResolveConfig → newLogger → logStartup → mux → `*http.Server` →
   ListenAndServe, with proper error handling).
2. **CREATE** `health_test.go` — stdlib-`testing`-only unit tests for the
   testable pure units: `healthHandler` (status 200, `Content-Type
   application/json`, body decodes to `{ok:true, version:"dev"}`; build-version
   override via the `version` package var; never touches upstream) and
   `logStartup` (emits one `info`/`startup` JSON line with `aliases`/`listen`/
   `upstream`/`log_level` and **no `authorization` field**), plus the
   `/healthz`-only routing invariant via `httptest.NewServer` + the stub.

**Success Definition**: `go build ./...`, `go vet ./...`, `gofmt -l .`, and
`go test ./...` all exit clean. A versioned build
`go build -ldflags "-X main.version=1.2.3" -o /tmp/wspf .` boots and
`curl /healthz` returns `200 {"ok":true,"version":"1.2.3"}`; a plain
`go build` yields `version:"dev"`. The `startup` event reaches stderr as one JSON
line containing `aliases` (array), `listen`, `upstream`, `log_level` and no
credential. Non-`/healthz` requests hit the 501 stub (real forwarding is T4.S2).
`config.go`/`config_test.go`/`resolve_test.go`/`logger_test.go` are untouched.
`go.mod` gains zero `require`s (only stdlib `os` is newly imported).

## User Persona

**Target User**: (1) the operator/process-supervisor starting the binary and
reading its stderr; (2) a health-checker/load-balancer probing `/healthz`; (3)
downstream subtask implementers (T4.S2 consumes this `main`/mux; P1.M4 wires the
real handler into the same mux; P1.M5.T1 asserts `/healthz` is isolated).

**Use Case**: At process start the supervisor needs (a) a fast, dependency-free
liveness probe (`GET /healthz`) and (b) a single structured line on stderr saying
what config the proxy actually resolved (so a misconfigured `WSPF_LISTEN`/upstream
is visible before traffic flows). Build/release tooling tags the binary with a
version that `/healthz` reports.

**User Journey**: `./web-search-prime-fixer` → stderr gets one JSON `startup`
line → `GET http://127.0.0.1:8787/healthz` → `200
{"ok":true,"version":"dev"}` (or the ldflags-injected version). Any other path
returns 501 until T4.S2 lands.

**Pain Points Addressed**: (1) operators had no liveness/version surface —
`/healthz` gives both without hitting the upstream (cheap, side-effect-free);
(2) a misconfigured proxy would boot silently — the `startup` log shows the
resolved `listen`/`upstream`/`log_level`/`aliases` so drift is obvious; (3) the
forward-dependency on T4.S2 (the real passthrough handler) is resolved with a
stub so this subtask is independently compilable, bootable, and testable.

## Why

- This is **PRD §20 step 1** (the bootstrap) and the integration point for all of
  Milestone 1: it stitches together `ResolveConfig` (P1.M1.T2.S2) and the
  redacting logger (P1.M1.T3.S1) into a running process, and defines the single
  `http.ServeMux` + `*http.Server` that every later handler plugs into.
- Implements **PRD §16 "Health and operations"**: `GET /healthz` →
  `200 {"ok":true,"version":"<version>"}`, never touching the upstream; version
  injected via `-ldflags "-X main.version=..."` or defaulting to `dev`. (Graceful
  shutdown in §16 is explicitly T4.S2 — NOT this subtask.)
- Implements **PRD §15 "Logging"** `startup` event: "resolved config (aliases,
  listen, upstream, log level). Never logs credentials." Because `Config` carries
  no credential fields (PRD §13: Authorization is forwarded as a header, never
  owned), this is structurally guaranteed; the test asserts it defensively.
- Implements **PRD §9 "Architecture"** route table: one listener, two routes —
  `/healthz` → health handler, everything else → proxy handler. Verified on-disk
  that Go's `ServeMux` gives exactly this (exact `/healthz`, `/` subtree
  catch-all; `/healthz/` falls through).
- Establishes the **`*http.Server{Addr, Handler}` shape** T4.S2 needs to add
  `signal.Notify` + `server.Shutdown` with zero restructuring, and the **stub
  registration line** T4.S2 swaps for the real handler factory.

## What

`main.go` gains (all `package main`, logger code from T3.S1 preserved):

- **Package-level `var version = "dev"`** — the build-version source. Overridden
  at link time by `-ldflags "-X main.version=<value>"` (verified on-disk:
  `dev`→`2.0.0`→`v1.0.0-rc1` all inject correctly). Read by `healthHandler`.
- **`func healthHandler(w http.ResponseWriter, r *http.Request)`** — a PURE local
  handler: sets `Content-Type: application/json`, writes `200`, then the body
  `json.Marshal(map[string]any{"ok": true, "version": version})`. Makes **no** HTTP
  client call and reads **no** upstream — it cannot fail on upstream health. (It
  cannot meaningfully error: `json.Marshal` of two scalars never errors; writes to
  an `http.ResponseWriter` are best-effort and their error is ignored, matching
  stdlib handler convention.)
- **`func logStartup(l *logger, cfg Config)`** — emits exactly one `l.log("info",
  "startup", map[string]any{"aliases": cfg.Aliases, "listen": cfg.Listen,
  "upstream": cfg.Upstream, "log_level": cfg.LogLevel})`. Extracted as a named
  function (rather than inlined in `main`) **solely so it is unit-testable** with
  an injected `*bytes.Buffer` writer (the logger's test seam). No credential
  field exists on `Config`, so none is logged.
- **`func passthroughHandler(w http.ResponseWriter, r *http.Request)`** — a
  PLACEHOLDER registered for every non-`/healthz` path. Returns `http.Error(w,
  `{"error":"passthrough not implemented (P1.M1.T4.S2)"}`, 501)` so a partial
  build is obviously incomplete, never a silent success. T4.S2 replaces the
  registration `mux.HandleFunc("/", passthroughHandler)` with the real factory and
  adds graceful shutdown; `/healthz` and the `*http.Server` are unaffected.
- **`func main()`** — the bootstrap:
  1. `cfg, err := ResolveConfig()`; on error, log `"config"` at `error` level to
     `os.Stderr` via a throwaway `newLogger(os.Stderr, "error")` and `os.Exit(1)`.
     (The contract's `cfg,_ := ResolveConfig()` is shorthand — a robust bootstrap
     MUST fail fast on an invalid config rather than boot with e.g. a bad listen
     address. This is a deliberate, documented refinement.)
  2. `log := newLogger(os.Stderr, cfg.LogLevel)` (the contract variable name).
  3. `logStartup(log, cfg)`.
  4. `mux := http.NewServeMux(); mux.HandleFunc("/healthz", healthHandler);
     mux.HandleFunc("/", passthroughHandler)`.
  5. `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` (NO timeout fields —
     would break SSE streaming; see gotchas).
  6. `if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
     log.log("error", "listen", ...); os.Exit(1) }`. (`ErrServerClosed` is
     non-fatal: it is the normal return when `Shutdown` is called — T4.S2 will add
     that path; guarding it now makes T4.S2 a pure addition.)

`health_test.go` (package main) tests the pure units via `httptest.NewRecorder`/
`NewServer` + `json.Unmarshal` (never byte matching — `json.Marshal` HTML-escapes
`<`, so `"<redacted>"`/similar decode correctly but byte asserts fail).

### Success Criteria

- [ ] `var version = "dev"` exists at package level in `main.go`; a plain `go build`
      yields `/healthz` `version:"dev"`, and `go build -ldflags "-X main.version=X"`
      yields `version:"X"` (verified by `TestHealthHandler_VersionOverride` setting
      the package var, and by the Level-3 build+curl smoke).
- [ ] `GET /healthz` → `200`, `Content-Type: application/json`, body decodes to
      `{"ok": true, "version": <string>}`; the handler issues no upstream call.
- [ ] `func logStartup(l *logger, cfg Config)` emits one `info`/`startup` JSON line
      whose decoded map has `aliases` (matching `cfg.Aliases` as a JSON array),
      `listen`, `upstream`, `log_level`, and **no** `authorization` key.
- [ ] Route table: `/healthz` → `healthHandler`; every other path (`/mcp`,
      `/healthz/`, `/anything`) → the stub (non-`200`/non-health response). Only
      exact `/healthz` is intercepted.
- [ ] `func main()` wires ResolveConfig→newLogger→logStartup→mux→`*http.Server`→
      ListenAndServe; a config error exits non-zero; the server starts on
      `cfg.Listen`.
- [ ] `func passthroughHandler` is the documented 501 placeholder for T4.S2.
- [ ] The T3.S1 logger (`type logger`, `newLogger`, `log`, `redactHeaders`,
      `levelNum`, level consts) is unchanged.
- [ ] `main.go` imports only stdlib (`encoding/json`, `io`, `net/http`, `os`,
      `time`); `go.mod` gains zero `require`s.
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .`, `go test ./...` all clean.

## All Needed Context

### Context Completeness Check

_Pass._ The contract enumerates the exact bootstrap sequence, the `version`
injection mechanism (`-ldflags "-X main.version=..."`, default `"dev"`), the
`/healthz` body (`{"ok":true,"version":"<version>"}`, no upstream), and the
startup fields (`aliases`/`listen`/`upstream`/`log_level`, never credentials).
The four stdlib behaviors T4.S1 rests on (`-ldflags -X`, `ServeMux` exact+catch-all
routing, `json.Marshal` of `{"ok":true,...}` and of the startup map) are **verified
on-disk** against `go1.26.4` in `research/verify-bootstrap-stdlib.md`, including
the one sharp edge (ServeMux treats `/healthz` as exact; `/healthz/` falls through
— acceptable per contract). The INPUT state of `main.go` (the T3.S1 logger + empty
`func main() {}` stub) has been **read on-disk** (the logger is already present,
verbatim). The T3.S1 logger API (`newLogger(w io.Writer, level string) *logger`,
`(l *logger).log(level, msg string, fields map[string]any)`, `redactHeaders`) and
the T2.S2 config API (`ResolveConfig() (Config, error)`, `Config{Upstream, Listen,
Path, Aliases []string, TargetParam, LogLevel}`, `DefaultConfig()`) are **read
on-disk** and quoted in the references. The forward-dependency on T4.S2 (real
passthrough + graceful shutdown; T4.S2 INPUT is "main/mux from T4.S1") is resolved
with a documented stub. An agent with no prior knowledge of this codebase can
implement this from the PRP alone.

### Documentation & References

```yaml
# MUST READ — authoritative health/version/route/logging contract.
- file: PRD.md
  why: §16 "Health and operations" (GET /healthz -> 200
        {"ok":true,"version":"<version>"}; does NOT touch the upstream; version via
        -ldflags "-X main.version=..." or default "dev"; graceful shutdown is named
        here but is T4.S2, NOT this subtask); §9 "Architecture" (one HTTP listener,
        two routes: /healthz -> health, everything else -> proxy; "only /healthz is
        intercepted; everything else forwards regardless of path"); §15 "Logging"
        (startup event: resolved config — aliases, listen, upstream, log level;
        "Never logs credentials"; structured JSON lines to stderr so stdout stays
        clean); §18 "Building and running" (go build -o web-search-prime-fixer .;
        go test ./...).
  critical: /healthz MUST NOT touch the upstream (pure local handler). The startup
        log fields are EXACTLY aliases/listen/upstream/log_level — do NOT add others
        and do NOT log any header/credential (Config has none, so this is
        structural). Graceful shutdown (SIGINT/SIGTERM -> server.Shutdown 10s) is
        T4.S2 — do NOT implement signal handling here.

# INPUT (HARD DEPENDENCY) — the logger this subtask calls, already on disk.
- file: plan/001_c0abc3757e9a/P1M1T3S1/PRP.md
  why: Defines and ships the logger API this subtask consumes: type logger
        struct{w io.Writer; level int}; func newLogger(w io.Writer, level string)
        *logger; func (l *logger) log(level, msg string, fields map[string]any);
        func redactHeaders(h http.Header) map[string]any. T3.S1 leaves main.go as
        the logger + `func main() {}` stub.
  critical: newLogger takes a raw `level string` (NOT a Config) — T4.S1 bridges
        cfg.LogLevel -> newLogger(os.Stderr, cfg.LogLevel). Keep the logger code
        UNCHANGED. The injected io.Writer (os.Stderr here, *bytes.Buffer in tests)
        is the seam that makes logStartup unit-testable.

# INPUT (HARD DEPENDENCY) — the config resolver + Config shape, already on disk.
- file: plan/001_c0abc3757e9a/P1M1T2S2/PRP.md
  why: Defines ResolveConfig() (Config, error) — discovery + env overrides +
        validation (Listen must net.SplitHostPort; Upstream must be absolute URL);
        Config{Upstream, Listen, Path string; Aliases []string; TargetParam,
        LogLevel string}; DefaultConfig(). ResolveConfig can return a non-nil
        error (bad listen, missing WSPF_CONFIG file, non-absolute upstream).
  critical: ResolveConfig returns an ERROR that main() MUST handle (fail fast,
        exit 1) — the contract's `cfg,_` is shorthand. Config has NO credential
        field, so logging it cannot leak. cfg.Aliases is []string (logs as a JSON
        array); cfg.Listen is "host:port" (validated); cfg.LogLevel is one of
        debug|info|warn|error (NOT enum-validated by ResolveConfig, but newLogger
        tolerates any string -> info).

# FORWARD DEPENDENCY (THE SEAM) — what T4.S2 consumes from this subtask.
- file: plan/001_c0abc3757e9a/tasks.json   # READ-ONLY (do not modify)
  why: P1.M1.T4.S2.dependencies == ["P1.M1.T4.S1"]; T4.S2 INPUT is "main/mux from
        P1.M1.T4.S1". T4.S2 owns the REAL passthrough handler (manual forward, SSE
        stream, *http.Client with Transport.ResponseHeaderTimeout) AND graceful
        shutdown (signal.Notify SIGINT/SIGTERM -> server.Shutdown(ctx 10s)).
  critical: T4.S1 lands FIRST, so the real passthrough handler does not exist yet.
        T4.S1 MUST register a PLACEHOLDER stub for the / catch-all (returns 501)
        so the module compiles, boots, and /healthz is testable end-to-end. The
        registration line mux.HandleFunc("/", passthroughHandler) is the exact
        target T4.S2 swaps for the real factory (e.g.
        mux.HandleFunc("/", newProxyHandler(cfg, log))). The *http.Server
        constructed here (Addr+Handler only, no timeouts) is the object T4.S2
        calls .Shutdown() on — build it now so T4.S2 is a pure addition.

# VERIFIED GOTCHAS — on-disk proof of the four stdlib behaviors.
- file: plan/001_c0abc3757e9a/P1M1T4S1/research/verify-bootstrap-stdlib.md
  why: go1.26.4 on-disk runs confirming: -ldflags "-X main.version=..." injects
        the build version (dev default, any string override); ServeMux registers
        /healthz as an EXACT match and / as the subtree catch-all (/healthz/ and
        every other path fall through to /); json.Marshal({"ok":true,...}) and the
        startup map produce the exact PRD bodies; aliases []string -> JSON array.
  critical: /healthz is EXACT-match — /healthz/ (trailing slash) is forwarded,
        not intercepted. This is acceptable and matches the contract's literal
        mux.HandleFunc("/healthz", ...). Do NOT register /healthz/ (would be scope
        creep). Also: do NOT set http.Server WriteTimeout/ReadTimeout — a fixed
        write deadline kills the long-lived SSE responses (P1.M3/P1.M4); build the
        server with Addr+Handler ONLY.

# ARCHITECTURE — stdlib-only invariant.
- file: plan/001_c0abc3757e9a/architecture/system_context.md
  why: "No third-party dependencies are required or allowed (PRD §6, §9)."
        Confirms the bootstrap is hand-rolled with net/http + encoding/json (no
        gorilla/mux, no cobra, no log/slog).
  critical: Do NOT add a router framework or CLI flag library. Two
        mux.HandleFunc calls + a single *http.Server is the entire routing layer.

# Go stdlib refs — exact semantics relied upon (stable; verified on-disk above).
- url: https://pkg.go.dev/net/http#ServeMux
  why: ServeMux.HandleFunc("/healthz", h) registers an EXACT path; HandleFunc("/",
        h) registers the root SUBTREE (matches every path not matched by a more
        specific pattern). Together: only /healthz is intercepted, everything
        else forwards. This is PRD §9's two-route table.
  critical: A pattern without a trailing slash matches only that exact path;
        /healthz/ therefore falls through to /. No automatic redirect occurs
        (redirects are a subtree-with-trailing-slash behavior we don't trigger).
- url: https://pkg.go.dev/net/http#Server
  why: type Server struct{ Addr string; Handler Handler; ... }. ListenAndServe()
        listens on Addr and serves Handler; it blocks until error or Shutdown
        (which returns http.ErrServerClosed).
  critical: Construct srv := &http.Server{Addr: cfg.Listen, Handler: mux} so T4.S2
        can call srv.Shutdown(ctx). Do NOT set ReadTimeout/WriteTimeout/IdleTimeout
        — a write deadline truncates streamed SSE responses (PRD §8/§11.3, P1.M3).
- url: https://pkg.go.dev/net/http#HandlerFunc
  why: type HandlerFunc func(ResponseWriter, *Request) — the signature of
        healthHandler and passthroughHandler, and what HandleFunc accepts.
- url: https://pkg.go.dev/net/http/httptest#NewRecorder
  why: Returns a *ResponseRecorder; calling a handler with it captures status,
        headers, and body WITHOUT a real socket — the idiomatic way to unit-test
        healthHandler in isolation. (httptest.NewServer is used for the routing
        test.)
- url: https://pkg.go.dev/cmd/link
  why: -ldflags "-X importpath.name=value" sets a string variable at link time.
        For a package-main var, importpath is "main": -X main.version=1.2.3.
```

### Current Codebase tree (the INPUT state of this subtask)

```bash
# Run: ls -la /home/dustin/projects/web-search-prime-fixer
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22; NO requires (T1.S1)
  doc.go            # "Package main ..." comment (T1.S1)
  main.go           # T3.S1 logger (type logger/newLogger/log/redactHeaders/     ← THIS SUBTASK EXTENDS IT
                    #   level machinery) + `func main() {}` EMPTY STUB
  config.go         # Config/DefaultConfig/LoadConfig (S1) + ResolveConfig/helpers (S2) [T2 — DO NOT EDIT]
  config_test.go    # S1's tests (TestDefaultConfig ...)                          [T2.S1 — DO NOT EDIT]
  resolve_test.go   # S2's tests (ResolveConfig discovery/env/validation)         [T2.S2 — DO NOT EDIT]
  logger_test.go    # T3.S1's tests (redaction/level/JSON)                        [T3.S1 — DO NOT EDIT]
  testdata/.gitkeep # placeholder (T1.S1; P1.M3.T2 adds *.sse)
  PRD.md            # unchanged
  plan/...          # unchanged (READ-ONLY planning scaffolding)
# NOTE: no version var, no healthHandler, no real main() yet. This subtask adds them.
```

### Desired Codebase tree (after this subtask)

```bash
web-search-prime-fixer/
  go.mod            # UNCHANGED (zero requires; the 1 new import is stdlib `os`)
  doc.go            # UNCHANGED
  main.go           # MODIFIED — import block += "os";                                [THIS]
                    #            += var version = "dev"   (package level)             [THIS]
                    #            += func healthHandler(w, r)                          [THIS]
                    #            += func logStartup(l *logger, cfg Config)            [THIS]
                    #            += func passthroughHandler(w, r)   (501 STUB)        [THIS]
                    #            func main() {...}  (REPLACES the empty stub)         [THIS]
                    #            (T3.S1 logger symbols UNCHANGED)
  health_test.go    # NEW — TestHealthHandler / TestHealthHandler_VersionOverride /   [THIS]
                    #        TestHealthHandler_NoUpstream / TestLogStartup /
                    #        TestRouting_HealthzOnly (httptest.NewRecorder + NewServer;
                    #        asserts via json.Unmarshal, never byte matching)
  config.go         # UNCHANGED (T2 owns it)
  config_test.go    # UNCHANGED
  resolve_test.go   # UNCHANGED
  logger_test.go    # UNCHANGED
  testdata/.gitkeep # UNCHANGED
```

### Known Gotchas of our codebase & Library Quirks

```go
// CRITICAL (#1): -ldflags sets a PACKAGE-LEVEL var, so `var version = "dev"` MUST
// live in package main at file scope (NOT inside main()). The flag is
// `-X main.version=<value>` (importpath "main" for a package-main var). Verified
// on-disk: dev -> 2.0.0 -> v1.0.0-rc1 all inject correctly. A plain `go build`
// (no ldflags) leaves version == "dev" — the documented default.

// CRITICAL: /healthz is an EXACT ServeMux match. mux.HandleFunc("/healthz", h)
// matches ONLY "/healthz"; "/healthz/" (trailing slash), "/mcp", and everything
// else fall through to mux.HandleFunc("/", ...) (the subtree catch-all). This is
// EXACTLY PRD §9 ("only /healthz is intercepted"). Do NOT also register
// "/healthz/" — that is scope creep and the contract registers only "/healthz".

// CRITICAL: Do NOT set http.Server.ReadTimeout/WriteTimeout/IdleTimeout. A fixed
// write deadline would TRUNCATE the long-lived SSE responses the proxy streams
// (PRD §8/§11.3, P1.M3/P1.M4). Build srv := &http.Server{Addr: cfg.Listen,
// Handler: mux} with ONLY Addr+Handler. (The upstream *http.Client.Transport's
// ResponseHeaderTimeout — T4.S2's concern — is a different, correct timeout.)

// CRITICAL: Set the response header BEFORE WriteHeader. In healthHandler:
//   w.Header().Set("Content-Type", "application/json")
//   w.WriteHeader(http.StatusOK)
//   w.Write(body)
// Calling WriteHeader before Header().Set silently locks the default
// Content-Type (text/plain; charset=utf-8) — the Content-Type assertion in the
// test would then FAIL.

// CRITICAL: The contract's `cfg,_ := ResolveConfig()` is SHORTHAND. ResolveConfig
// returns a real error (bad listen address, missing WSPF_CONFIG file, non-absolute
// upstream). main() MUST handle it: log "config" at error level to os.Stderr via
// newLogger(os.Stderr, "error") and os.Exit(1). Booting with an invalid listen
// address is worse than failing fast. (On the error path we cannot trust
// cfg.LogLevel, so use a fixed "error" level logger — never fmt.Println, to keep
// all output structured JSON on stderr per PRD §15.)

// CRITICAL: Treat http.ErrServerClosed as NON-FATAL. srv.ListenAndServe() returns
// it when Shutdown() is called (T4.S2 adds that path). Guarding now
// (err != nil && err != http.ErrServerClosed) makes T4.S2 a pure addition —
// without it, T4.S2's graceful shutdown would log a spurious "listen" error and
// exit 1.

// CRITICAL: Name the logger variable `log` (the contract literally writes
// `log := newLogger(os.Stderr, cfg.LogLevel)`). Calling log.log("info", ...) is
// valid Go: the selector `log.log` resolves to "method log on the value held by
// variable log" (var name and method name are different namespaces). There is no
// conflict because we do NOT import the stdlib "log" package (T3.S1 forbade it).

// GOTCHA: json.Marshal HTML-escapes '<','>','&'. Health bodies and standard
// semver versions contain none of these, so /healthz output is unaffected. Only a
// pathological injected version (e.g. "1.0<x") would emit "\u003c" — out of scope
// (PRD's "<version>" is a placeholder, not literal brackets). Tests MUST still
// decode via json.Unmarshal (never byte/string matching), to stay consistent with
// logger_test.go and to be robust to future field additions.

// GOTCHA: cfg.Aliases is []string. In the startup log it marshals to a JSON ARRAY
// (["query","q",...]). The test compares it as a []any after Unmarshal, NOT as a
// string. cfg.LogLevel is a plain string; cfg.Listen is "host:port".

// SCOPE GUARD: Do NOT implement graceful shutdown (SIGINT/SIGTERM, signal.Notify,
// server.Shutdown, ctx with 10s deadline). That is T4.S2's explicit deliverable.
// Here main() blocks on srv.ListenAndServe(). T4.S2 will wrap signal handling
// around the *http.Server you construct here.

// SCOPE GUARD: Do NOT implement real upstream forwarding in passthroughHandler.
// The 501 placeholder is the entire point — it keeps the build green and
// /healthz testable while T4.S2 ships the real handler. The registration line
// mux.HandleFunc("/", passthroughHandler) is the documented swap target.

// SCOPE GUARD: Do NOT add flag parsing, CLI args, or a version flag. Version is
// injected at BUILD time via -ldflags, never parsed at runtime. (PRD §18 runs the
// binary with no args.)

// SCOPE GUARD: Do NOT touch config.go / config_test.go / resolve_test.go /
// logger_test.go / doc.go / go.mod. This subtask edits ONLY main.go (MODIFY) and
// creates health_test.go. Do NOT add a require to go.mod; `os` is stdlib.

// TEST GOTCHA: main() itself is NOT unit-tested (it blocks on ListenAndServe and
// touches os.Stderr/os.Exit). The testable units are the PURE functions:
// healthHandler (httptest.NewRecorder) and logStartup (injected *bytes.Buffer).
// main()'s wiring is validated by the Level-3 integration smoke (build, boot on
// an ephemeral port, curl /healthz, terminate).
```

## Implementation Blueprint

### Data models and structure

No persistent data model. The bootstrap introduces one package-level variable and
three package-level functions plus the real `main`:

```go
// version is the proxy's build version, reported by /healthz. It defaults to
// "dev" and is overridden at link time with:
//   go build -ldflags "-X main.version=<value>" -o web-search-prime-fixer .
// (PRD §16). It MUST live at package scope (not inside main) for -X to set it.
var version = "dev"
```

### Implementation Tasks (ordered by dependencies)

```yaml
Task 0: VERIFY INPUT (read-only, no edits)
  - RUN: `cd /home/dustin/projects/web-search-prime-fixer && sed -n '1,40p' main.go`
  - EXPECT: main.go begins `package main`, imports (encoding/json, io, net/http,
    time), and contains the T3.S1 logger symbols (type logger, newLogger,
    (l *logger).log, redactHeaders, levelNum) AND ends with `func main() {}` (the
    EMPTY stub). If main.go is ONLY the bare 3-line T1.S1 stub (`package main` +
    `func main() {}`) with NO logger, STOP — T3.S1 (hard dependency) has not landed;
    do not proceed.
  - RUN: `go build ./...` → MUST exit 0 (T3.S1 + T2.S2 have landed).
  - RUN: `grep -n 'func ResolveConfig' config.go` → MUST show ResolveConfig exists.
  - WHY: This subtask's INPUT is "T3.S1 main.go (logger + empty main stub)" and
    "T2.S2 ResolveConfig". Confirm both before editing.

Task 1: MODIFY main.go — add version, health, startup, stub passthrough, real main
  - FILE: /home/dustin/projects/web-search-prime-fixer/main.go
  - PRESERVE: the entire T3.S1 logger block (imports for encoding/json/io/net/http/
    time; level consts; levelNum; type logger; newLogger; (l *logger).log;
    redactHeaders) UNCHANGED.
  - ADD to the import block: `"os"` (alphabetical order; os goes after net/http).
  - ADD (after the logger symbols, before func main): package-level
    `var version = "dev"`; `func healthHandler(w http.ResponseWriter, r
    *http.Request)`; `func logStartup(l *logger, cfg Config)`; `func
    passthroughHandler(w http.ResponseWriter, r *http.Request)`.
  - REPLACE the empty `func main() {}` with the real bootstrap.
  - USE the EXACT code in "Implementation Patterns & Key Details" below (verbatim;
    it is small and fully specified).
  - WHY: All consumers (T4.S2 mux swap; P1.M4 handler wiring; P1.M5 /healthz
    isolation test) target these exact symbols. Keeping them in main.go (package
    main) matches PRD §9's single-binary, single-package layout.

Task 2: CREATE health_test.go
  - FILE: /home/dustin/projects/web-search-prime-fixer/health_test.go
  - PACKAGE: `package main`
  - IMPORTS: `"encoding/json"`, `"io"`, `"net/http"`, `"net/http/httptest"`,
    `"bytes"`, `"reflect"`, `"testing"` (stdlib only; mirror logger_test.go's
    decode-via-Unmarshal approach).
  - IMPLEMENT the tests in "Validation Loop → Level 2" verbatim: TestHealthHandler
    (200 + Content-Type + ok/version), TestHealthHandler_VersionOverride (set the
    package var, assert flows, restore), TestHealthHandler_NoUpstream
    (completes with no network — structural purity), TestLogStartup (one info/
    startup line; aliases as array; no authorization key), TestRouting_HealthzOnly
    (httptest.NewServer: /healthz -> health; /mcp and /healthz/ -> stub 501).
  - NAMING: TestHealthHandler, TestHealthHandler_VersionOverride,
    TestHealthHandler_NoUpstream, TestLogStartup, TestRouting_HealthzOnly.
  - WHY: Covers the contract's explicit testable unit ("healthHandler is a pure
    function — assert 200, ok:true, version") plus the startup-log shape, the
    no-credential invariant, the ldflags seam, and the PRD §9 routing table.

Task 3: VALIDATE
  - RUN (exact commands, repo root):
        gofmt -w main.go health_test.go
        go build ./...
        go vet ./...
        gofmt -l .
        go test ./...
        go test -run 'TestHealthHandler|TestLogStartup|TestRouting' -v ./...
  - EXPECT: gofmt silent; build/vet clean; all tests PASS. Re-run after any edit.

Task 4: INTEGRATION SMOKE (Level 3 — the version+health end-to-end proof)
  - RUN (repo root):
        go build -ldflags "-X main.version=1.2.3" -o /tmp/wspf .
        WSPF_LISTEN=127.0.0.1:0 ./tmp/wspf &  # NOTE: port 0 may not be supported by
        # ResolveConfig validation (needs host:port; use a real free port instead):
        # pick a free port, e.g. 127.0.0.1:18787, and run:
        WSPF_LISTEN=127.0.0.1:18787 /tmp/wspf & PID=$!
        sleep 0.3
        curl -sS -i http://127.0.0.1:18787/healthz   # expect 200 {"ok":true,"version":"1.2.3"}
        kill $PID; wait $PID 2>/dev/null
        # default build:
        go build -o /tmp/wspf-dev .
        WSPF_LISTEN=127.0.0.1:18788 /tmp/wspf-dev & PID=$!
        sleep 0.3
        curl -sS http://127.0.0.1:18788/healthz      # expect {"ok":true,"version":"dev"}
        kill $PID; wait $PID 2>/dev/null
        rm -f /tmp/wspf /tmp/wspf-dev
  - EXPECT: ldflags build reports version "1.2.3"; plain build reports "dev".
```

### Implementation Patterns & Key Details

```go
// main.go — the ADDITIONS to the T3.S1 file (the logger block above this is
// UNCHANGED; only the import block gains "os", and these symbols + the real main
// are added). Place these after the T3.S1 logger symbols, replacing the empty
// `func main() {}`.
//
// === IMPORT BLOCK (final, after edit) ===
// import (
// 	"encoding/json"
// 	"io"
// 	"net/http"
// 	"os"
// 	"time"
// )

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
	// Header mutations are ignored). See gotcha.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// CRITICAL: marshals to {"ok":true,"version":"<version>"} (keys sorted by
	// json.Marshal; "ok" < "version"). HTML-escaping does not affect semver.
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

// passthroughHandler is a PLACEHOLDER registered for every non-/healthz path.
// P1.M1.T4.S2 replaces this stub with the real transparent forwarder (a manual
// upstream POST that streams SSE responses) and adds graceful shutdown; the
// registration line `mux.HandleFunc("/", passthroughHandler)` in main is the
// exact swap target (e.g. -> mux.HandleFunc("/", newProxyHandler(cfg, log))).
//
// Until T4.S2 lands, non-health requests get 501 so a partial build can never
// silently masquerade as a working proxy. /healthz is unaffected and fully
// functional (PRD §9: only /healthz is intercepted).
func passthroughHandler(w http.ResponseWriter, r *http.Request) {
	http.Error(w, `{"error":"passthrough not implemented (P1.M1.T4.S2)"}`, http.StatusNotImplemented)
}

// main is the proxy bootstrap: resolve config, build the redacting logger, emit
// the startup event, register the two routes, and serve. Graceful shutdown
// (SIGINT/SIGTERM -> server.Shutdown, 10s deadline) is P1.M1.T4.S2 — NOT here.
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

	// Route table (PRD §9): /healthz intercepted; EVERYTHING else forwards.
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)       // exact match
	mux.HandleFunc("/", passthroughHandler)         // subtree catch-all (T4.S2 swaps this)

	// CRITICAL: NO ReadTimeout/WriteTimeout — a write deadline would truncate the
	// streamed SSE responses (PRD §8/§11.3, P1.M3/P1.M4). Addr+Handler only. T4.S2
	// calls srv.Shutdown(ctx) on this object.
	srv := &http.Server{Addr: cfg.Listen, Handler: mux}

	// ListenAndServe blocks until error or Shutdown. ErrServerClosed is the normal
	// return when Shutdown is called (T4.S2 adds that path); treat it non-fatal so
	// T4.S2 is a pure addition (no spurious "listen" error on graceful exit).
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.log("error", "listen", map[string]any{"err": err.Error(), "listen": cfg.Listen})
		os.Exit(1)
	}
}
```

### Integration Points

```yaml
PACKAGE:
  - name: "main"           # main.go + health_test.go are package main, repo root

SYMBOLS INTRODUCED (consumed by later subtasks):
  - var version                                 # -> healthHandler (runtime);
        #                                    build/release via -ldflags "-X main.version=..."
  - func healthHandler(w http.ResponseWriter, r *http.Request)
        # -> registered at mux.HandleFunc("/healthz", healthHandler) in main();
        #    asserted isolated (no upstream call) by P1.M5.T1.S2 case 5
  - func logStartup(l *logger, cfg Config)
        # -> called once from main(); emits PRD §15 "startup"
  - func passthroughHandler(w http.ResponseWriter, r *http.Request)  # 501 STUB
        # -> registered at mux.HandleFunc("/", passthroughHandler) in main();
        #    T4.S2 REPLACES this registration with the real factory
  - func main()                                # the bootstrap wiring

CONSUMED FROM PRIOR SUBTASKS (read-only dependencies, already on disk):
  - func ResolveConfig() (Config, error)        # P1.M1.T2.S2 (config.go)
  - type Config { Upstream, Listen, Path string; Aliases []string; TargetParam, LogLevel string }
        # P1.M1.T2.S1 (config.go)
  - func newLogger(w io.Writer, level string) *logger   # P1.M1.T3.S1 (main.go)
  - func (l *logger) log(level, msg string, fields map[string]any)  # P1.M1.T3.S1
  - func DefaultConfig() Config                 # used by health_test.go (P1.M1.T2.S1)

HANDOFF TO P1.M1.T4.S2 (the forward-dependency seam):
  - T4.S2 INPUT is "main/mux from P1.M1.T4.S1". It will:
      (1) replace `mux.HandleFunc("/", passthroughHandler)` with the real handler
          factory (e.g. `mux.HandleFunc("/", newProxyHandler(cfg, log))`), creating
          proxy.go with the manual forward + SSE stream + *http.Client;
      (2) wrap the *http.Server constructed here with signal.Notify(SIGINT/SIGTERM)
          -> srv.Shutdown(ctx 10s) -> log "shutdown" -> exit.
  - This subtask constructs the *http.Server (Addr+Handler, no timeouts) and
    guards http.ErrServerClosed precisely so T4.S2 is a pure addition.

NO NEW ENV VARS / NO go.mod CHANGES / NO CONFIG SCHEMA CHANGES:
  - The only new import is stdlib `os`. go.mod gains zero requires.
  - WSPF_LISTEN/WSPF_CONFIG/WSPF_LOG_LEVEL (T2.S2) flow into cfg via ResolveConfig;
    nothing new here.
  - [Mode A] DOCS: no README/config.example.json/doc.go changes (those are
    P1.M5.T3). Inline doc comments on every new symbol are the documentation.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
cd /home/dustin/projects/web-search-prime-fixer

gofmt -w main.go health_test.go             # format in place
gofmt -l .                                  # MUST print nothing
go vet ./...                                # MUST exit 0, no output
go build ./...                              # MUST exit 0, no output

# Dependency-free invariant (PRD §6/§9) — grep for stray imports / router libs:
grep -nE '^\s*"github\.com|^\s*"gopkg\.in|^\s*"golang\.org/x|^\s*"log"|"log/slog"|gorilla|cobra' main.go health_test.go || true
# Expected: empty. main.go imports only: encoding/json, io, net/http, os, time.
# health_test.go: bytes, encoding/json, io, net/http, net/http/httptest, reflect, testing.

# Expected: gofmt silent; vet/build clean; no third-party or stdlib-"log" imports.
```

### Level 2: Unit Tests (Component Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

go test ./...                                                # MUST pass
go test -run 'TestHealthHandler|TestLogStartup|TestRouting' -v ./...  # see these tests
```

Required `health_test.go` test cases (every assertion decodes JSON via
`json.Unmarshal` / `reflect.DeepEqual`, never byte/string matching — see gotcha):

```go
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// (a) GET /healthz -> 200, application/json, {"ok":true,"version":"dev"}.
func TestHealthHandler(t *testing.T) {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)

	healthHandler(rr, req)

	if got := rr.Code; got != http.StatusOK {
		t.Fatalf("status = %d, want 200", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("body not valid JSON: %v (raw=%q)", err, rr.Body.String())
	}
	if body["ok"] != true {
		t.Errorf("ok = %#v, want true", body["ok"])
	}
	if body["version"] != version {
		t.Errorf("version = %#v, want %#v", body["version"], version)
	}
}

// (b) Build-version override: setting the package var flows to /healthz. This
// proves the -ldflags "-X main.version=..." seam (the linker sets this same var).
func TestHealthHandler_VersionOverride(t *testing.T) {
	old := version
	version = "1.2.3"
	defer func() { version = old }() // restore for other tests

	rr := httptest.NewRecorder()
	healthHandler(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	var body map[string]any
	json.Unmarshal(rr.Body.Bytes(), &body)
	if body["version"] != "1.2.3" {
		t.Fatalf("version = %#v, want 1.2.3 (ldflags seam)", body["version"])
	}
}

// (c) healthHandler is pure: it completes without any network. (Structurally
// guaranteed — no http.Client call in the body — but asserted to lock the
// invariant that /healthz "does not touch the upstream", PRD §16.)
func TestHealthHandler_NoUpstream(t *testing.T) {
	rr := httptest.NewRecorder()
	// No upstream server is started; if the handler tried to dial, the test would
	// hang or fail. It must return 200 immediately.
	healthHandler(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (handler must be pure/local)", rr.Code)
	}
}

// (d) logStartup emits one info/startup line with the four config fields and NO
// authorization field (PRD §15 "Never logs credentials").
func TestLogStartup(t *testing.T) {
	var buf bytes.Buffer
	l := newLogger(&buf, "info")
	cfg := DefaultConfig()

	logStartup(l, cfg)

	// Exactly one line.
	lines := bytes.Split(bytes.TrimRight(buf.Bytes(), "\n"), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("got %d lines, want 1 (raw=%q)", len(lines), buf.String())
	}
	var m map[string]any
	if err := json.Unmarshal(lines[0], &m); err != nil {
		t.Fatalf("line not valid JSON: %v (raw=%q)", err, lines[0])
	}
	if m["level"] != "info" {
		t.Errorf("level = %#v, want info", m["level"])
	}
	if m["msg"] != "startup" {
		t.Errorf("msg = %#v, want startup", m["msg"])
	}
	// aliases is a JSON array matching cfg.Aliases ([]any after decode).
	aliases, ok := m["aliases"].([]any)
	if !ok {
		t.Fatalf("aliases = %#v, want a JSON array", m["aliases"])
	}
	want := make([]any, len(cfg.Aliases))
	for i, a := range cfg.Aliases {
		want[i] = a
	}
	if !reflect.DeepEqual(aliases, want) {
		t.Errorf("aliases = %#v, want %#v", aliases, want)
	}
	if m["listen"] != cfg.Listen {
		t.Errorf("listen = %#v, want %q", m["listen"], cfg.Listen)
	}
	if m["upstream"] != cfg.Upstream {
		t.Errorf("upstream = %#v, want %q", m["upstream"], cfg.Upstream)
	}
	if m["log_level"] != cfg.LogLevel {
		t.Errorf("log_level = %#v, want %q", m["log_level"], cfg.LogLevel)
	}
	// Security invariant: no credential field (Config has none; assert defensively).
	if _, present := m["authorization"]; present {
		t.Errorf("startup line contains an authorization field: %#v", m["authorization"])
	}
}

// (e) Routing table (PRD §9): only /healthz is intercepted; everything else hits
// the passthrough stub (501). Built via the SAME mux main() builds.
func TestRouting_HealthzOnly(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler)
	mux.HandleFunc("/", passthroughHandler) // the 501 stub
	ts := httptest.NewServer(mux)
	defer ts.Close()

	// /healthz -> health (200, ok+version).
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("/healthz status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var m map[string]any
	json.Unmarshal(body, &m)
	if m["ok"] != true {
		t.Errorf("/healthz body ok = %#v, want true (routed to healthHandler)", m["ok"])
	}

	// /mcp and /healthz/ -> the stub (501), proving they are NOT intercepted.
	for _, p := range []string{"/mcp", "/healthz/", "/initialize", "/foo/bar"} {
		resp, err := http.Get(ts.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotImplemented {
			t.Errorf("%s status = %d, want 501 (must route to stub, not health)", p, resp.StatusCode)
		}
	}
}
```

```bash
# Expected: every test PASS. Common failures & fixes:
#  - Content-Type != "application/json": WriteHeader was called before
#    Header().Set (gotcha) — set the header first.
#  - TestHealthHandler_VersionOverride fails: version is local to main() instead
#    of package-level (the -X flag and this test both need package scope).
#  - TestRouting: a non-health path returns 200: the stub was omitted, or "/"
#    was not registered (only "/healthz"). Both /healthz AND / must be registered.
#  - TestLogStartup aliases mismatch: cfg.Aliases compared as a string instead of
#    a []any (after json.Unmarshal it is []any, not []string).
```

### Level 3: Integration Testing (System Validation)

```bash
cd /home/dustin/projects/web-search-prime-fixer

# (1) Default build boots and /healthz reports "dev".
go build -o /tmp/wspf-dev .
WSPF_LISTEN=127.0.0.1:18787 /tmp/wspf-dev >/tmp/wspf.stderr 2>&1 &
PID=$!
sleep 0.5
echo "--- /healthz (default build) ---"
curl -sS -i http://127.0.0.1:18787/healthz        # expect: HTTP/1.1 200 ... {"ok":true,"version":"dev"}
echo "--- non-health path (stub) ---"
curl -sS -i http://127.0.0.1:18787/mcp            # expect: HTTP/1.1 501 ... {"error":"passthrough not implemented (P1.M1.T4.S2)"}
echo "--- startup log on stderr ---"
cat /tmp/wspf.stderr                              # expect: one JSON line {"ts":..,"level":"info","msg":"startup","aliases":[...],"listen":..,"upstream":..,"log_level":..}
kill $PID 2>/dev/null; wait $PID 2>/dev/null

# (2) Versioned build via -ldflags reports the injected version.
go build -ldflags "-X main.version=1.2.3" -o /tmp/wspf .
WSPF_LISTEN=127.0.0.1:18788 /tmp/wspf >/tmp/wspf2.stderr 2>&1 &
PID=$!
sleep 0.5
echo "--- /healthz (ldflags version=1.2.3) ---"
curl -sS http://127.0.0.1:18788/healthz           # expect: {"ok":true,"version":"1.2.3"}
kill $PID 2>/dev/null; wait $PID 2>/dev/null

# (3) Config error fails fast, exits non-zero, logs structured error.
WSPF_CONFIG=/does/not/exist.json /tmp/wspf-dev; echo "exit=$?"
# expect: exit=1 and a JSON {"level":"error","msg":"config","err":"load config ..."} line on stderr.

rm -f /tmp/wspf /tmp/wspf-dev /tmp/wspf.stderr /tmp/wspf2.stderr
# Expected: (1) "dev", 501 on /mcp, one startup JSON line; (2) "1.2.3"; (3) exit 1.
```

### Level 4: Creative & Domain-Specific Validation

```bash
cd /home/dustin/projects/web-search-prime-fixer

# godoc — the [Mode A] doc-comment deliverable:
go doc . version             # "var version ... overridden at link time via -ldflags"
go doc . healthHandler       # 200, application/json, does NOT touch the upstream
go doc . logStartup          # the four startup fields, never credentials
go doc . passthroughHandler  # PLACEHOLDER for P1.M1.T4.S2
# Expected: each prints its godoc; healthHandler names "does not touch the upstream".

# Contract-shape check: the five new symbols exist.
grep -nE '^var version = "dev"|func healthHandler\(|func logStartup\(|func passthroughHandler\(|^func main\(\)' main.go
# Expected: five matches.

# No-secrets guard: the startup log never references Authorization.
! grep -niE 'authorization' main.go || true
# Expected: zero hits in main.go (Authorization appears only in redactHeaders' redaction
# list inside the T3.S1 logger block — that is correct; logStartup/main add none).

# SSE-safety guard: the *http.Server is built WITHOUT timeouts (would truncate SSE).
grep -n 'WriteTimeout\|ReadTimeout\|IdleTimeout' main.go && echo "FAIL: timeout set" || echo "OK: no server timeouts"
# Expected: OK (no hits).

# ldflags seam: the package var is at file scope (required for -X).
grep -nE '^var version = "dev"' main.go   # expect exactly one match at column 0 (file scope).

# No graceful-shutdown scope creep: no signal handling this subtask.
grep -nE 'signal\.|os\.Signal|Shutdown\(' main.go && echo "FAIL: shutdown code present (T4.S2 scope)" || echo "OK: no shutdown code"
# Expected: OK (no hits).
```

## Final Validation Checklist

### Technical Validation

- [ ] `go build ./...` exits 0, no output.
- [ ] `go vet ./...` exits 0, no output.
- [ ] `gofmt -l .` prints nothing.
- [ ] `go test ./...` passes (exit 0), including all `health_test.go` tests.
- [ ] `main.go` imports only `encoding/json`, `io`, `net/http`, `os`, `time`.
- [ ] `health_test.go` imports only stdlib (`bytes`, `encoding/json`, `io`,
      `net/http`, `net/http/httptest`, `reflect`, `testing`).
- [ ] `go.mod` is unchanged (zero `require`s).

### Feature Validation

- [ ] `var version = "dev"` exists at package scope; plain build →
      `/healthz` `version:"dev"`; ldflags build → injected version.
- [ ] `GET /healthz` → `200`, `Content-Type: application/json`, body
      `{"ok":true,"version":<string>}`; handler issues no upstream call.
- [ ] `logStartup` emits one `info`/`startup` JSON line with `aliases` (array),
      `listen`, `upstream`, `log_level`, and no `authorization` key.
- [ ] Route table: `/healthz` → health; every other path → stub 501; only exact
      `/healthz` is intercepted.
- [ ] `func main()` boots on `cfg.Listen`; a `ResolveConfig` error exits non-zero
      with a structured `error`/`config` log line.
- [ ] `func passthroughHandler` is the documented 501 placeholder for T4.S2.
- [ ] Level-3 smoke: default build → "dev"; `go build -ldflags "-X main.version=1.2.3"`
      → "1.2.3"; `WSPF_CONFIG=/missing` → exit 1.

### Code Quality Validation

- [ ] T3.S1 logger symbols (`type logger`, `newLogger`, `log`, `redactHeaders`,
      `levelNum`, level consts) are unchanged.
- [ ] Response headers are set BEFORE `WriteHeader` in `healthHandler`.
- [ ] `*http.Server` is built with `Addr`+`Handler` only (no timeouts — SSE-safe).
- [ ] `http.ErrServerClosed` is treated as non-fatal (forward-compat for T4.S2).
- [ ] The logger variable is named `log` per the contract; `log.log(...)` is used.
- [ ] `config.go`/`config_test.go`/`resolve_test.go`/`logger_test.go`/`doc.go`
      untouched.
- [ ] No graceful-shutdown code (T4.S2 scope); no real forwarding (T4.S2 scope).

### Documentation & Deployment

- [ ] Doc comments on `version`, `healthHandler`, `logStartup`, `passthroughHandler`,
      and `main` (the [Mode A] deliverable): version injection mechanism, "does not
      touch the upstream", the four startup fields / no credentials, the T4.S2
      placeholder + swap target.
- [ ] No README / config.example.json / doc.go changes (those are P1.M5.T3 —
      explicitly deferred).

---

## Anti-Patterns to Avoid

- ❌ Don't declare `version` inside `main()` — `-ldflags "-X main.version=..."` only
  sets a PACKAGE-LEVEL var. It must be `var version = "dev"` at file scope.
- ❌ Don't let `/healthz` touch the upstream — it is a pure local handler (PRD §16).
  No `http.Client`, no `http.Get`, no upstream URL read in `healthHandler`.
- ❌ Don't call `WriteHeader` before `Header().Set` — the `Content-Type` would lock
  to the default `text/plain; charset=utf-8` and the test would fail. Set headers
  first.
- ❌ Don't ignore the `ResolveConfig` error (`cfg,_`) — a bad listen address or
  missing `WSPF_CONFIG` must fail fast (`os.Exit(1)`), not boot broken.
- ❌ Don't set `http.Server.WriteTimeout`/`ReadTimeout` — a write deadline truncates
  streamed SSE responses (PRD §8/§11.3, P1.M3/P1.M4). `Addr`+`Handler` only.
- ❌ Don't treat `http.ErrServerClosed` as fatal — it is the normal return when
  `Shutdown` is called (T4.S2). Guard `err != http.ErrServerClosed`.
- ❌ Don't implement graceful shutdown (`signal.Notify`, `server.Shutdown`) or real
  forwarding here — both are T4.S2. The `/` handler is a 501 placeholder.
- ❌ Don't register `/healthz/` (trailing slash) — `/healthz` is an exact match and
  that is the contract; `/healthz/` correctly falls through to the catch-all.
- ❌ Don't add flag parsing or a `--version` flag — version is BUILD-time only via
  `-ldflags`, never parsed at runtime (PRD §18 runs the binary with no args).
- ❌ Don't compare log/health JSON with `strings.Contains`/`bytes.Contains` —
  `json.Marshal` HTML-escapes `<`,`>`,`&`. Decode with `json.Unmarshal` first.
- ❌ Don't compare decoded `aliases` to a `[]string` — after `json.Unmarshal` it is
  `[]any`. Build a `[]any` want-slice or compare element-by-element.
- ❌ Don't rename the logger variable away from `log` — the contract literally writes
  `log := newLogger(os.Stderr, cfg.LogLevel)`. `log.log(...)` is valid Go.
- ❌ Don't touch `config.go`/`config_test.go`/`resolve_test.go`/`logger_test.go`/
  `doc.go`/`go.mod` — this subtask edits only `main.go` (MODIFY) and creates
  `health_test.go`. Don't add a `require` (the only new import is stdlib `os`).
- ❌ Don't unit-test `main()` directly — it blocks on `ListenAndServe` and calls
  `os.Exit`. Test the PURE units (`healthHandler`, `logStartup`) and validate
  `main()`'s wiring via the Level-3 integration smoke.
- ❌ Don't add a router framework (gorilla/mux) or CLI lib (cobra) — two
  `mux.HandleFunc` calls + one `*http.Server` is the whole layer (PRD §6/§9
  stdlib-only).

---

## Confidence Score

**9/10** — One-pass implementation success likelihood.

Rationale: The deliverable is small and fully specified — the exact additions to
`main.go` (import `os`; `var version = "dev"`; `healthHandler`; `logStartup`;
`passthroughHandler` 501 stub; the real `main` with ResolveConfig-error handling,
startup log, mux registration, and `*http.Server`+`ListenAndServe`) are given
verbatim, and every `health_test.go` case is spelled out with setup, assertion,
and decoded expected value. The four stdlib behaviors (`-ldflags -X` version
injection; `ServeMux` exact-`/healthz` + `/` catch-all routing; `json.Marshal` of
the health body and the startup map) are **verified on-disk** against `go1.26.4`
in `research/verify-bootstrap-stdlib.md`, including the routing sharp edge
(`/healthz/` falls through — acceptable per contract) and the SSE-safety rule
(no server timeouts). The INPUT state of `main.go` (T3.S1 logger + empty `main`
stub) and the consumed APIs (`ResolveConfig`/`Config` from T2; `newLogger`/`log`
from T3) are **read on-disk** and quoted, so the MODIFY targets real, stable text.
The forward-dependency on T4.S2 (real passthrough + graceful shutdown) is resolved
with a documented 501 stub + a constructed `*http.Server` + an `ErrServerClosed`
guard, making T4.S2 a pure addition with a single swap-target registration line —
no file collision (T4.S2 will create `proxy.go` and edit one `mux.HandleFunc`
line; this subtask edits `main.go` and creates `health_test.go`). Residual risks:
(a) an agent calls `WriteHeader` before `Header().Set` — mitigated by the gotcha,
the verbatim `healthHandler`, and `TestHealthHandler`'s Content-Type assertion;
(b) an agent declares `version` inside `main` (breaking `-X`) — mitigated by the
gotcha, the verbatim package-level decl, and `TestHealthHandler_VersionOverride`;
(c) an agent sets a server timeout (breaking future SSE) — mitigated by the
SSE-safety gotcha and the Level-4 guard; (d) an agent implements graceful shutdown
or real forwarding (T4.S2 scope creep) — mitigated by the scope guards and the
Level-4 `signal./Shutdown(` grep guard. Hence 9, not 10.
