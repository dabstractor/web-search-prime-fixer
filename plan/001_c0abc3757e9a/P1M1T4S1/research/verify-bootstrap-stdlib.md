# T4.S1 Bootstrap — on-disk verification (go1.26.4, linux/amd64)

Verification of the four stdlib behaviors T4.S1 (main() bootstrap + /healthz +
version + startup log) depends on. All confirmed against the installed toolchain
in throwaway `/tmp` scratch modules (no repo files were touched).

## 1. `-ldflags "-X main.version=..."` injects the build version (PRD §16)

A package-level `var version = "dev"` in `package main` is overridden at link
time by `-ldflags "-X main.version=<value>"`. Confirmed end-to-end:

```
# main.go: package main; var version = "dev"; func main(){ fmt.Println(version) }
go build -o v1 .                            && ./v1   # -> dev
go build -ldflags "-X main.version=2.0.0" -o v2 . && ./v2   # -> 2.0.0
go build -ldflags "-X main.version=v1.0.0-rc1" -o v3 . && ./v3 # -> v1.0.0-rc1
```

- The import path for a `package main` var is `main`, hence `-X main.version=...`.
- Works for any string (semver, rc tags, etc.).
- A plain `go build` (no ldflags) leaves `version == "dev"`. **This is the
  contract default.**

Implication: the recipe to ship a versioned binary is
`go build -ldflags "-X main.version=$(git describe --tags)" -o web-search-prime-fixer .`.

## 2. ServeMux routing: `/healthz` exact + `/` catch-all (PRD §9 / §16)

```
mux := http.NewServeMux()
mux.HandleFunc("/healthz", healthHandler)   // exact match
mux.HandleFunc("/", passthroughHandler)      // subtree catch-all
```

Verified via `httptest.NewServer(mux)` (test PASSED):

| Request path | Routed to   | Notes |
|---|---|---|
| `/healthz`   | healthHandler | exact match — the ONLY intercepted path |
| `/mcp`       | `/` catch-all | forwarded (real impl in T4.S2) |
| `/healthz/`  | `/` catch-all | trailing slash is NOT `/healthz`; falls through |
| `/anything`  | `/` catch-all | everything else forwards regardless of path |

This is exactly PRD §9 ("only `/healthz` is intercepted; everything else
forwards regardless of path"). **Key gotcha**: `/healthz` (no trailing slash) is
an EXACT pattern in Go's ServeMux; `/healthz/` does NOT match it and is forwarded.
That is acceptable and matches the contract's `mux.HandleFunc("/healthz", ...)`.
No automatic redirect occurs (redirects only happen for subtree patterns with a
trailing slash, which we do not register).

(Module is `go 1.22`, so the enhanced ServeMux pattern matcher is available, but
the classic exact-vs-subtree semantics above are unchanged for these two
patterns.)

## 3. Startup log field shape (PRD §15)

`json.Marshal(map[string]any{...})` of the four startup fields produces:

```json
{"aliases":["query","q","search"],"listen":"127.0.0.1:8787","log_level":"info","upstream":"https://api.z.ai/api/mcp/web_search_prime/mcp"}
```

- `aliases` (a Go `[]string`, i.e. `cfg.Aliases`) marshals to a **JSON array**.
- Keys are sorted alphabetically by `json.Marshal` (deterministic; consumers key
  by name, so order is irrelevant).
- **No `authorization` key** — `Config` carries no credential fields (PRD §13:
  Authorization is forwarded as a request header, never part of Config). The
  startup log therefore structurally cannot leak a credential. The defensive
  test asserts the decoded line has no `authorization` field.

## 4. Health JSON body (PRD §16)

`json.Marshal(map[string]any{"ok": true, "version": version})` for `version="dev"`:

```json
{"ok":true,"version":"dev"}
```

- `ok` is a Go `bool` → JSON `true`.
- Keys sorted (`ok` < `version`), matching PRD §16's literal
  `{"ok":true,"version":"<version>"}`.
- `json.Marshal` HTML-escapes `<`,`>`,`&`. Standard semver versions contain none
  of these, so escaping is a non-issue for `/healthz`. (Only a pathological
  injected version like `1.0<x` would emit `\u003c`; out of scope — PRD's
  `<version>` is a placeholder, not literal angle brackets.)

## 5. The T4.S2 seam (forward dependency — DESIGN DECISION)

`tasks.json` shows **T4.S2 depends on T4.S1** (`P1.M1.T4.S2.dependencies ==
["P1.M1.T4.S1"]`) and T4.S2's INPUT is explicitly *"main/mux from P1.M1.T4.S1"*.
T4.S2 owns the REAL transparent passthrough handler (manual forward, SSE stream,
`*http.Client` with `Transport.ResponseHeaderTimeout`) PLUS graceful shutdown
(`signal.Notify(SIGINT/SIGTERM)` → `server.Shutdown(ctx 10s)`).

Because T4.S1 lands FIRST, the passthrough handler does not exist yet. Resolution:
**T4.S1 registers a clearly-marked PLACEHOLDER stub for the `/` catch-all** so the
binary compiles, boots, and `/healthz` is fully testable end-to-end in isolation.
The stub returns HTTP 501 with a JSON body naming T4.S2, so a partial build can
never silently masquerade as a working proxy. T4.S2's PRP will instruct replacing
the stub registration `mux.HandleFunc("/", passthroughHandler)` with the real
factory (e.g. `mux.HandleFunc("/", newProxyHandler(cfg, log))`) and adding the
signal/shutdown wiring — `/healthz` and the `*http.Server` constructed here are
untouched by that change.

This is NOT the "halt — fundamentally impossible" case; it is the standard
forward-dependency seam, handled with a stub.

## 6. Out-of-scope guard: NO server timeouts (SSE-safe)

PRD §17 ("Timeouts and robustness") and the SSE streaming design (P1.M3 / P1.M4)
require that the proxy NOT set `http.Server.WriteTimeout`/`ReadTimeout`, because a
fixed write deadline would KILL the long-lived SSE responses the proxy streams.
T4.S1 constructs `srv := &http.Server{Addr: cfg.Listen, Handler: mux}` with ONLY
`Addr` and `Handler` — no `Timeout` fields. (Response-header timeout lives on the
upstream `*http.Client.Transport`, set by T4.S2, not on the listener.)
