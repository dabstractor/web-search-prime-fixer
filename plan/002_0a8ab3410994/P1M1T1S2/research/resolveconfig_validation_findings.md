# Research Notes — P1.M1.T1.S2: ResolveConfig validation extension

## 1. Goal of this subtask

Extend `ResolveConfig()` in `config.go` with three NEW validation rules that run
AFTER the existing Listen/Upstream checks and BEFORE the existing
force-TargetParam-if-empty logic. Edit ONLY `config.go` (function body + doc
comment). Consumed by `main.go` bootstrap (P1.M1.T2.S1) and the resolve_test.go
rewrite (P1.M1.T3.S1).

## 2. The exact current ResolveConfig (v1, post-S1 struct)

Read from `config.go` lines 211-end. Order today:
1. `resolveConfigPath()`  → discovery (UNCHANGED — S2 does NOT touch)
2. `LoadConfig(path)`     → defaults + file overlay (UNCHANGED)
3. env overrides          → WSPF_UPSTREAM/LISTEN/LOG_LEVEL, empty ignored (UNCHANGED)
4. Validate Listen        → `net.SplitHostPort` (UNCHANGED)
5. Validate Upstream      → `url.Parse` + `!u.IsAbs()` error (UNCHANGED)
6. force TargetParam=="search_query" if empty (UNCHANGED — KEEP)
7. return cfg, nil

**S2 inserts the 3 new Tools checks between step 5 and step 6.** That placement:
- is "AFTER the existing Listen/Upstream checks" (contract requirement) ✓
- keeps the force-TargetParam logic untouched (contract requirement) ✓
- is a minimal diff ✓
- is functionally correct: rule (c) compares against `TargetTool` (default
  "web_search_prime"), which force-TargetParam never touches (it only sets
  `TargetParam` == "search_query"). Different fields → order-independent.

## 3. The three new rules (authoritative source: PRD §18.3 lines 552-559)

PRD §18.3 verbatim: "Validation: `Listen` parses, `Upstream` is an absolute URL,
`Tools` is non-empty and contains `CanonicalTool`, and no entry equals
`TargetTool` (we never advertise z.ai's real name); else exit with a clear error."

Contract mapping:
- (a) `len(cfg.Tools) > 0`                       → else clear error
- (b) `slices.Contains(cfg.Tools, cfg.CanonicalTool)`  → else clear error
- (c) `!slices.Contains(cfg.Tools, cfg.TargetTool)`    → else clear error

Ordering rationale for (a)→(b)→(c): check non-empty first (membership on an empty
slice is trivially false anyway and the message should be distinct), then
canonical membership, then the z.ai-name leak guard.

## 4. slices.Contains availability

- `go.mod` declares `go 1.22`.
- `slices.Contains` shipped in Go 1.21 (stdlib `slices`). ✓ No third-party dep.
- Both (b) and (c) are membership tests → `slices.Contains` is the idiomatic fit
  and keeps the two checks symmetric.
- Alternative (no new import): a hand-rolled loop. Equally valid but less DRY.
  Recommend `slices.Contains`; must add `"slices"` to config.go's import block.
- Verified config.go's current imports: encoding/json, fmt, net, net/url, os,
  path/filepath. Adding "slices" is the only import change.

## 5. Error format — follow the existing wrapped-error convention

Existing v1 errors in ResolveConfig wrap a STDLIB error with `%w`:
- `fmt.Errorf("invalid listen address %q: %w", cfg.Listen, err)` (wraps net err)
- `fmt.Errorf("invalid upstream URL %q: %w", cfg.Upstream, err)` (wraps url err)
- `fmt.Errorf("upstream URL %q is not absolute ...", cfg.Upstream)` (no %w — pure logical check)

The 3 new rules are PURE logical checks with no underlying stdlib error to chain.
Therefore they use `fmt.Errorf` WITHOUT `%w` (there is nothing to wrap) — exactly
like the existing "not absolute" branch. The contract's phrase "wrapped error
(fmt.Errorf)" means "wrapped in a descriptive fmt.Errorf message", NOT literal
`%w` chaining. Messages quote the offending value with `%q` for clarity, matching
the existing style.

Proposed messages:
- (a) `fmt.Errorf("tools list must not be empty")`
- (b) `fmt.Errorf("tools list must contain the canonical tool %q", cfg.CanonicalTool)`
- (c) `fmt.Errorf("tools list must not contain the target tool %q (it would advertise z.ai's real name)", cfg.TargetTool)`

## 6. Default config passes all three (no behavior change for default users)

After S1, DefaultConfig() yields: Tools=["web_search"], CanonicalTool="web_search",
TargetTool="web_search_prime". Therefore:
- (a) len 1 > 0 ✓
- (b) contains "web_search" ✓
- (c) "web_search_prime" not in ["web_search"] ✓
=> Defaults still validate clean. `TestResolveConfig_Defaults` (once rewritten by
T3.S1) will still pass. No existing happy-path breaks.

## 7. Callers of ResolveConfig (impact of new validation)

- `main.go:174` — the ONLY production caller; bootstrap does fail-fast on the
  returned error (log + exit 1). The 3 new rules simply make misconfiguration
  fail-fast earlier with a clear message. No main.go change needed for S2.
- `resolve_test.go` (many call sites) — owned by P1.M1.T3.S1 rewrite. S2 must NOT
  edit resolve_test.go. Its current `TestResolveConfig_*` cases all use defaults
  or override only Listen/Upstream/LogLevel via env, which all still pass the new
  rules (defaults valid). The one TargetParam-forcing test uses a file with only
  `target_param` → Tools stays default → still valid.

## 8. Why the full package still won't build after S2 (expected, NOT a defect)

Same as S1's documented breakage. After S1+S2:
- `main.go:160`  `cfg.Aliases` undefined        → fixed in P1.M1.T2.S1
- `proxy.go:498/499/512/532` `cfg.Aliases`       → proxy.go DELETED in P1.M1.T2.S2
- `config_test.go`, `resolve_test.go`, `health_test.go`, `proxy_test.go` reference
  `.Aliases` / stale schema → rewritten/deleted in P1.M1.T3.S1 / T2.S2

=> S2 validates config.go with the SAME isolated temp-module gate S1 used
(copy config.go + main stub into /tmp, go vet + go build). The full `go build ./...`
stays red until P1.M1 completes. S2 MUST NOT "fix" that by editing out-of-scope files.

## 9. Doc comment deliverable ([Mode A])

ResolveConfig's doc comment has stale v1 wording S1 deliberately left for S2:
- "validates the proxy configuration" → "validates the normalizing MCP server configuration"
- "PRD §14.3" → "PRD §18.3" (PRD renumbered in v2; S1 already did §14.2→§18.2)
- "Path and Aliases have NO env override" → update for v2 fields (Aliases is now
  QueryAliases; also Tools/CanonicalTool/CanonicalParam/OptionalAliases/TargetTool/
  TargetParam have no env override)
- "# Validation" bullets: append the 3 new rules.

The primary [Mode A] deliverable is the 3 new validation bullets; the
stale-wording fixes ride along because they are in the same doc comment.

## 10. Validation approach for S2 itself

- Level 1: isolated temp-module `go vet . && go build .` on config.go (gofmt clean).
- Level 2: throwaway, NON-committed test in the isolated module proving all 3
  rules fire (empty / missing-canonical / contains-target all error; default
  passes). Delete after; do not commit (resolve_test.go is T3.S1's).
- Level 3: document expected full-build breakage (same set as S1) — not a gate.
- There is NO go.mod change in S2 (slices is stdlib). Confirmed no new require.
