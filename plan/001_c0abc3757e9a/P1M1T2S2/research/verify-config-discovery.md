# Research — config discovery / env overrides / validation primitives (S2)

## Question
P1.M1.T2.S2 requires `ResolveConfig()` to (1) discover a config path per PRD
§14.3 precedence, (2) `LoadConfig` it, (3) apply `WSPF_UPSTREAM` /
`WSPF_LISTEN` / `WSPF_LOG_LEVEL` env overrides, (4) validate `Listen`
(parseable host:port) and `Upstream` (absolute URL), (5) force `TargetParam` to
`search_query` if empty. The whole feature rests on four Go stdlib behaviors.
This probe verifies them **on-disk** with the installed toolchain
(`go1.26.4-X:nodwarf5 linux/amd64`, stdlib only — zero deps).

The contract's research note says: use `os.UserConfigDir()` for the XDG base
(it resolves `$XDG_CONFIG_HOME` or `~/.config` portably).

## Method
`/tmp/wspf-s2-probe/main.go` (go.mod `probe`, no deps) exercises
`os.UserConfigDir`, `net.SplitHostPort`, `url.Parse`+`URL.IsAbs`, and `os.Stat`
across the realistic + edge inputs. Ran with `go run .`.

## Results

### 1. `os.UserConfigDir()` — drives the XDG candidate
| env state | return | err |
|-----------|--------|-----|
| `XDG_CONFIG_HOME=/tmp/fake-xdg`, `HOME=/home/dustin` | `/tmp/fake-xdg` | nil |
| `XDG_CONFIG_HOME` unset, `HOME=/home/dustin` | `/home/dustin/.config` | nil |
| both unset | `""` | `neither $XDG_CONFIG_HOME nor $HOME are defined` |

**Conclusions:**
- `os.UserConfigDir()` is the correct portable XDG resolver (Linux: `$XDG_CONFIG_HOME`
  else `$HOME/.config`). Do NOT hand-read `XDG_CONFIG_HOME`.
- **It can return an error** (when neither env var is set). `ResolveConfig` MUST
  tolerate this: on error, **skip the XDG candidate** and fall through to the
  "no file → defaults" branch. Do not crash.
- **Setting `XDG_CONFIG_HOME` via `t.Setenv(...)` is observed by `os.UserConfigDir`**
  → tests can drive the XDG candidate deterministically WITHOUT touching `$HOME`
  or doing `os.Chdir`. This is the clean, parallel-safe way to test XDG discovery.

### 2. `net.SplitHostPort` — the Listen validator
| input | host | port | err |
|-------|------|------|-----|
| `127.0.0.1:8787` (default) | `127.0.0.1` | `8787` | nil ✓ |
| `0.0.0.0:9000` | `0.0.0.0` | `9000` | nil ✓ |
| `:8787` (empty host) | `""` | `8787` | nil ✓ (binds all ifaces; acceptable) |
| `127.0.0.1` (no port) | `""` | `""` | `missing port in address` ✗ |
| `garbage` | `""` | `""` | `missing port in address` ✗ |
| `host:port:extra` | `""` | `""` | `too many colons in address` ✗ |
| `""` (empty) | `""` | `""` | `missing port in address` ✗ |

**Conclusions:**
- Validation = `if _, _, err := net.SplitHostPort(cfg.Listen); err != nil { return err }`.
- The default `127.0.0.1:8787` passes. An empty `Listen` (e.g. from
  `{"listen":""}` in a file) FAILS validation with a clear error — exactly the
  contract's "clear error on failure".
- `:8787` passes (empty host). This binds all interfaces, not loopback. The
  contract only requires "parseable host:port", so this is accepted. (Loopback is
  the *default*, not an enforced constraint — see PRD §13.)
- `net.SplitHostPort` requires exactly one `:` separating host and port; multiple
  colons (IPv6 literals need brackets, e.g. `[::1]:8787`) — `[::1]:8787` parses
  fine (not probed but well-known); bare `::1` would fail. Default + realistic
  inputs all behave as required.

### 3. `url.Parse` + `URL.IsAbs()` — the Upstream validator
| input | scheme | host | IsAbs | verdict |
|-------|--------|------|-------|---------|
| `https://api.z.ai/api/mcp/web_search_prime/mcp` (default) | `https` | `api.z.ai` | true | ✓ valid |
| `https://example.com/mcp` | `https` | `example.com` | true | ✓ valid |
| `api.z.ai/mcp` (no scheme) | `""` | `""` | false | ✗ rejected (not absolute) |
| `//host/path` (scheme-relative) | `""` | `host` | false | ✗ rejected |
| `http://` (empty host) | `http` | `""` | true | ✓ accepted (lenient) |
| `:foo` | — | — | — | parse error `missing protocol scheme` |

**Conclusions:**
- Validation = `u, err := url.Parse(cfg.Upstream); err != nil → error; !u.IsAbs() → error`.
- `IsAbs()` is true iff `u.Scheme != ""`. The default upstream is valid.
- **`url.Parse` rarely errors** (it is lenient). The decisive check for "absolute"
  is `IsAbs()` (scheme present), NOT parse success. Missing-scheme inputs
  (`api.z.ai/mcp`, `//host/path`) are correctly rejected as non-absolute.
- **Leniency noted:** `http://` (empty host) passes `IsAbs()`. The contract only
  requires an absolute URL (has a scheme), so this is accepted. A realistic
  misconfiguration like `api.z.ai/mcp` (the common "forgot https://" mistake) IS
  caught — the primary intent of the check.

### 4. `os.Stat` — the "first existing" existence check
| input | err | `err == nil` (exists) |
|-------|-----|----------------------|
| existing file | nil | true |
| missing file | `stat ...: no such file or directory` | false |

**Conclusions:**
- "First existing of [cwd, XDG]" = `if _, err := os.Stat(p); err == nil { return p }`.
- `err == nil` is the correct "file is present and stat-able" predicate. A
  permission error would be treated as "not usable, skip" (fall through). This
  matches the contract's "first existing" wording and avoids a confusing
  mid-resolve permission failure. (Edge case; documented in PRP gotchas.)

## Conclusions for the implementation
1. **Discovery is a 4-line ladder** (`resolveConfigPath()`):
   `WSPF_CONFIG` (verbatim if set) → `web-search-prime-fixer.json` in CWD (if
   exists) → `<UserConfigDir>/web-search-prime-fixer/config.json` (if UserConfigDir
   succeeds AND file exists) → `""` (defaults). No manual `XDG_CONFIG_HOME` read.
2. **Env overrides** are exactly three (`WSPF_UPSTREAM`, `WSPF_LISTEN`,
   `WSPF_LOG_LEVEL`), applied only when non-empty, AFTER `LoadConfig`. `WSPF_PATH`
   and `WSPF_ALIASES` do NOT exist (scope boundary — do not implement them).
3. **Validation** = `net.SplitHostPort(Listen)` + `url.Parse(Upstream)` +
   `IsAbs()`. Both produce clear stdlib error messages; wrap with `fmt.Errorf("…: %w", err)`.
4. **Order matters for correctness only at the discovery layer** (precedence).
   Override → validate → force-`TargetParam` is independent of ordering (validation
   never inspects `TargetParam`), but implement in the contract's stated order
   (override, validate, force) to match the spec verbatim.
5. **`ResolveConfig` does NOT log.** No logger exists yet (P1.M1.T3), and the
   contract's "Logs resolved config at startup (no creds ever present)" describes
   the bootstrap's `startup` event (PRD §15), emitted in P1.M1.T4 from the Config
   this function returns. `Config` has no credential fields (Authorization is a
   forwarded header, PRD §13), so logging the returned Config is inherently safe.
6. **Testing strategy (all stdlib, no subagents, no web):**
   - `t.Setenv("WSPF_CONFIG"/"WSPF_UPSTREAM"/"WSPF_LISTEN"/"WSPF_LOG_LEVEL", …)`
     drives explicit + override cases (auto-restored by the test framework).
   - `t.Setenv("XDG_CONFIG_HOME", t.TempDir())` drives the XDG candidate — NO
     `os.Chdir`, NO `$HOME` mutation, fully deterministic.
   - The CWD candidate (`./web-search-prime-fixer.json`) requires `os.Chdir` to a
     temp dir with manual save/restore via `t.Cleanup`; those tests MUST be serial
     (no `t.Parallel`) because env + cwd are process-global.
   - A "no file / defaults" test must isolate BOTH cwd (chdir to empty temp dir)
     AND XDG (point `XDG_CONFIG_HOME` at a separate empty temp dir) so no real
     user config on the dev machine leaks into the result.

## Go stdlib references (canonical, stable; behavior verified on-disk above)
- `os.UserConfigDir`: https://pkg.go.dev/os#UserConfigDir — "On Unix, it returns
  `$XDG_CONFIG_HOME` or `$HOME/.config`." Verified errors when neither is set.
- `os.Stat`: https://pkg.go.dev/os#Stat — returns `*PathError` (IsNotExist) if
  missing. `err == nil` ⇒ exists & stat-able.
- `net.SplitHostPort`: https://pkg.go.dev/net#SplitHostPort — splits `host:port`;
  errors on missing port / too many colons.
- `url.Parse` / `URL.IsAbs`: https://pkg.go.dev/net/url#URL.IsAbs — "reports
  whether the URL has a non-empty scheme." Absolute ⇔ has scheme.
- `testing.T.Setenv`: https://pkg.go.dev/testing#T.Setenv — "records [the] value
  to be restored ... after the test"; "cannot be used in parallel tests."
