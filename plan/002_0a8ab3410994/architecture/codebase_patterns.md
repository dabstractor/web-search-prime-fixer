# Codebase Patterns — web-search-prime-fixer v1 (reusable conventions)

Established conventions from the v1 codebase that the v2 rewrite must follow for
consistency. The v1 code is well-tested and well-documented; its **style and
patterns** carry over even though its **proxy architecture** is deleted.

## 1. Config patterns (`config.go`, `config_test.go`, `resolve_test.go`)

### Struct + defaults + overlay + validation
```go
type Config struct { ... }
func DefaultConfig() Config { return Config{...} }         // all fields, verbatim from PRD
func LoadConfig(path string) (Config, error)               // DefaultConfig base + json.Unmarshal overlay
func resolveConfigPath() string                            // WSPF_CONFIG > CWD > XDG > ""
func ResolveConfig() (Config, error)                       // discover + load + env-override + validate
```
- **JSON tags** on every field (snake_case). Unknown JSON fields ignored (default `json.Unmarshal` behavior).
- **Env overrides** applied AFTER file load: `WSPF_UPSTREAM`, `WSPF_LISTEN`, `WSPF_LOG_LEVEL`. Empty env values ignored.
- **Validation** in `ResolveConfig`: `Listen` must parse via `net.SplitHostPort`; `Upstream` must be absolute URL (`url.Parse` + `IsAbs`). v2 adds: `Tools` non-empty + contains `CanonicalTool`; no `Tools` entry == `TargetTool`.
- **No credential fields in Config** — Authorization is a request header, never owned.

### Test patterns
- `TestDefaultConfig`: `reflect.DeepEqual(def, want)` against the PRD-verbatim expected struct, plus belt-and-suspenders per-field asserts.
- `TestLoadConfig_FromFile`: table-driven subtests (partial override, unknown fields ignored, full file, invalid JSON error).
- `isolateConfigEnv(t)`: hermetic helper that clears `WSPF_*` env, sets `XDG_CONFIG_HOME` to temp, chdirs to temp. **Reusable** in v2 config tests.
- `writeConfig(t, body)`: writes JSON config to temp file, returns path. **Reusable.**

## 2. Logger patterns (`main.go` → `logger.go`)

```go
type logger struct { w io.Writer; level int }
func newLogger(w io.Writer, level string) *logger
func (l *logger) log(level string, msg string, fields map[string]any)
func redactHeaders(h http.Header) map[string]any
```
- One JSON line per log call, terminated by `\n`. Fields: `ts` (RFC3339 UTC), `level`, `msg`, then caller fields.
- Level filtering: `debug < info < warn < error`. Unrecognized level → `info`.
- **`redactHeaders`**: replaces `Authorization`, `Cookie`, `Set-Cookie`, `Proxy-Authorization` values with `"<redacted>"`.
- In tests, `newLogger(&buf, "info")` writes to a `bytes.Buffer` for assertion.
- v2 change: `logStartup` emits new fields (`tools`, `canonical_tool`, `query_aliases`) instead of `aliases`.

## 3. Health patterns (`main.go` → `health.go`)

```go
var version = "dev"  // set via -ldflags "-X main.version=..."
func healthHandler(w http.ResponseWriter, r *http.Request)
```
- GET only (405 on non-GET with `Allow: GET` header).
- `{"ok":true,"version":"<version>"}`, `Content-Type: application/json`.
- Pure local: no upstream call.
- **Unchanged in v2** — just moves to `health.go`.

## 4. main.go bootstrap pattern

Sequence:
1. `ResolveConfig()` → fail-fast on error (log "error"/"config" to stderr, exit 1).
2. `newLogger(os.Stderr, cfg.LogLevel)`.
3. `logStartup(log, cfg)`.
4. Build route table: `/healthz` → `healthHandler`; everything else → handler.
5. `&http.Server{Addr: cfg.Listen, Handler: mux}` — **NO ReadTimeout/WriteTimeout** (would truncate SSE).
6. Graceful shutdown goroutine: SIGINT/SIGTERM → log "shutdown" → `srv.Shutdown(ctx)` 10s deadline.
7. `srv.ListenAndServe()` → `http.ErrServerClosed` is normal exit.

v2 change: step 4 mounts `mcp.NewStreamableHTTPHandler(...)` (wrapped in auth middleware) instead of `newProxyHandler`.

## 5. Test harness conventions

- **httptest** for HTTP-level tests: `httptest.NewRecorder()`, `httptest.NewRequest()`, `httptest.NewServer()`.
- **io.Discard** for logger in tests that don't assert logs: `newLogger(io.Discard, "error")`.
- **Table-driven** with `t.Run(tt.name, ...)` subtests throughout.
- **reflect.DeepEqual** for struct comparisons.
- Tests are in `package main` (same package as implementation).

## 6. File deletion impact

When deleting `proxy.go`, `sse.go`, `rewrite.go` and their test files:
- `main.go` references `newProxyHandler` and `newUpstreamClient` (defined in proxy.go) → must be replaced.
- `health_test.go` `TestRouting_HealthzOnly` references `newProxyHandler` → must be updated to use the new handler.
- `config.go` has no dependency on deleted files (safe).
- `logger_test.go` tests `redactHeaders` which moves to `logger.go` → test moves/updates accordingly.

## 7. JSON key mapping (v1 → v2 config breaking change)

| v1 JSON key | v2 JSON key | Notes |
|---|---|---|
| `aliases` | `query_aliases` | **Renamed** — breaking config change, call out in README. |
| *(none)* | `tools` | New. Default `["web_search"]`. |
| *(none)* | `canonical_tool` | New. Default `"web_search"`. |
| *(none)* | `canonical_param` | New. Default `"query"`. |
| *(none)* | `optional_aliases` | New. Map of z.ai canonical → aliases. |
| *(none)* | `target_tool` | New. Always `"web_search_prime"`. |
| `target_param` | `target_param` | Unchanged. Always `"search_query"`. |
| `upstream` | `upstream` | Unchanged. |
| `listen` | `listen` | Unchanged. |
| `path` | `path` | Unchanged. |
| `log_level` | `log_level` | Unchanged. |
