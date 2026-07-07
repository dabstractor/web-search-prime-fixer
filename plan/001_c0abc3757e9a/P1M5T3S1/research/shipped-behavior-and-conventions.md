# Research: README.md for web-search-prime-fixer (P1.M5.T3.S1)

MODE B (changeset-level doc sweep). The README must describe the SHIPPED
behavior, verified against the on-disk source, not aspirational text. Every fact
below was read directly from the production files at research time.

## 1. What the proxy does (one line)

Local reverse proxy for the z.ai `web-search-prime` MCP endpoint that renames
common misspellings of the required `search_query` parameter (e.g. `query`) to
`search_query`, forwards the corrected call, and prepends a one-line warning to
the result so the agent learns the right name and does not retry. Every other
request passes through unchanged. Source: `doc.go` package comment.

## 2. The one rewrite rule (rewrite.go Rewrite, PRD §10/FR-2)

For an outbound `tools/call`, if `params.arguments` contains any configured alias:

- If `search_query` is ABSENT: the first present alias (in CONFIG ORDER) is
  promoted to `search_query`; the other present aliases are removed.
- If `search_query` is PRESENT: all aliases are removed (canonical value wins).
- Non-alias parameters are never touched.

Default aliases (DefaultConfig): `["query", "q", "search", "searchQuery",
"search_term"]`. Target is always `search_query`.

## 3. Exact SHIPPED warning format (sse.go warningText)

Format string: `[web-search-prime-fixer] ` + notes joined by `"; "` + `"."` +
suffix.

Per-alias notes (rewrite.go):
- rename:   `%q is not a valid parameter; renamed to %q`
            e.g. `"query" is not a valid parameter; renamed to "search_query"`
- ignored:  `ignored %q (use only %q)`
            emitted when search_query was already present
- dropped:  `dropped redundant %q`
            emitted for aliases beyond the promoted one

Suffix rules (warningText):
- If at least one note is a rename or dropped: ` Use "search_query" in future calls.`
- If ALL notes are "ignored":                    ` Use only "search_query" to avoid this notice.`

So the PRD §7.2 example matches the code byte-for-byte:
```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
```

The warning is prepended as a `text` content block in the `tools/call` SSE
result, ahead of the real result. `isError` is never set. When nothing changes,
the response passes through byte-for-byte (no warning). (PRD §12/FR-3.)

## 4. Build / run / test (PRD §18)

- Build:  `go build -o web-search-prime-fixer .`
- Run:    `./web-search-prime-fixer`                  (defaults; 127.0.0.1:8787)
          `WSPF_LISTEN=127.0.0.1:9000 ./web-search-prime-fixer`
          `WSPF_CONFIG=./my-config.json ./web-search-prime-fixer`
- Tests:  `go test ./...`

go.mod: `module web-search-prime-fixer` + `go 1.22`, ZERO requires. No go.sum
(confirmed absent). Stdlib only.

## 5. Environment variables (config.go ResolveConfig, PRD §14.3)

- `WSPF_CONFIG`    config file path, used VERBATIM. Missing file = hard error
                   (does NOT fall back to defaults).
- `WSPF_UPSTREAM`  overrides Config.Upstream (highest precedence).
- `WSPF_LISTEN`    overrides Config.Listen.
- `WSPF_LOG_LEVEL` overrides Config.LogLevel (debug|info|warn|error).

Empty env values are ignored. Path and Aliases have NO env override.

## 6. Config file discovery (resolveConfigPath, PRD §14.3)

1. `WSPF_CONFIG` if set (verbatim).
2. Else first EXISTING of:
   `./web-search-prime-fixer.json`
   `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json`
   (portable via os.UserConfigDir; defaults to
    `~/.config/web-search-prime-fixer/config.json` on Linux/macOS)
3. Else: no file, built-in defaults.

## 7. Config schema (Config struct, PRD §14.1) — JSON keys

`upstream`, `listen`, `path`, `aliases`, `target_param`, `log_level`.

Defaults (DefaultConfig, PRD §14.2):
- upstream:     `https://api.z.ai/api/mcp/web_search_prime/mcp`
- listen:       `127.0.0.1:8787`
- path:         `/mcp`
- aliases:      `["query", "q", "search", "searchQuery", "search_term"]`
- target_param: `search_query`
- log_level:    `info`

Validation: Listen must parse as host:port; Upstream must be absolute URL;
TargetParam forced to `search_query` if empty; unknown JSON fields ignored.

## 8. Client MCP config (PRD §7.1, EXACT block)

Only the URL changes (points at the proxy). Headers stay identical so the client
still carries the key, which the proxy forwards verbatim:

```json
{
  "mcpServers": {
    "web-search-prime": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": {
        "Authorization": "Bearer your_api_key"
      }
    }
  }
}
```

## 9. /healthz (main.go healthHandler, PRD §16)

`GET /healthz` -> `200` with body `{"ok":true,"version":"<version>"}`.
Does NOT touch the upstream. Version defaults to `dev`, injected at build via
`-ldflags "-X main.version=..."`.

## 10. Credentials (PRD §13 / FR-4)

Client sends `Authorization: Bearer <key>` to the proxy exactly as to z.ai. The
proxy forwards it verbatim and holds no key of its own. Authorization is never
logged (logger redacts Authorization/Cookie/Set-Cookie/Proxy-Authorization).

## 11. Non-goals (PRD §3) — the README MUST state these

The proxy does NOT:
- infer/default `location`
- normalize `content_size`, `search_recency_filter`, or any enum
- truncate/shorten query text
- drop/map/warn about unsupported params (e.g. `max_results`, `safe_search`)
- rewrite the tool schema or `tools/list` response (forwarded verbatim)
- manage API keys or rotate credentials
- retry, rate-limit, or cache

Rule of thumb: if a behavior is not "rename a configured alias to
search_query", it passes through unchanged.

## 12. Logging (PRD §15)

Structured JSON lines to stderr (stdout stays clean). Fields: ts, level, msg +
context. Events: `startup` (resolved config, no creds), `rewrite` (req_id, tool,
notes), `upstream_error` (status, req_id, on non-2xx), `shutdown` (on signal).
`debug` adds per-request `forward` lines.

## 13. Robustness / shutdown (PRD §16/§17)

- Binds 127.0.0.1 only (local). Not a general-purpose or open proxy.
- No hard Client.Timeout (would cut long SSE streams); relies on request-context
  cancellation. ResponseHeaderTimeout on the Transport (~30s) detects dead upstream.
- Graceful shutdown: SIGINT/SIGTERM -> server.Shutdown(ctx) with 10s deadline -> exit.

## 14. Test/build gate (cite the parallel PRP as contract)

The parallel item P1.M5.T2.S1 (final quality gate) is the contract that proves:
`go vet ./...` clean; `go test ./...` green (71 funcs / 10 files); both builds
exit 0; `-ldflags "-X main.version=0.1.0"` -> `/healthz` returns
`{"ok":true,"version":"0.1.0"}`; stdlib-only (zero requires, no go.sum). The
README can state "builds with the Go standard library only" and cite `go test
./...` as the test command with confidence.

## 15. write-tech-docs skill (item contract says to use it; "no marketing tone")

Skill rules enforced (hard, non-negotiable):
1. NO em dashes (U+2014). Use colon, parentheses, comma, or period.
2. NO marketing tell-words (powerful, robust, elegant, seamless, comprehensive,
   leverage, utilize, unlock, empower, etc.).
3. NO hedging/formulaic transitions (moreover, furthermore, "in conclusion",
   "it's worth noting", excess perhaps/maybe/likely).
4. Do NOT narrate the codebase. Document what code cannot show: what it is, why
   it exists, how to use it, the gotchas.
5. Run the linter before finishing.

Linter location (absolute, in the skill dir, NOT in the repo):
`/home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh`
Invocation: `bash /home/dustin/.pi/agent/skills/write-tech-docs/scripts/lint.sh README.md`
Must exit 0.

Concision: intros 1-2 sentences; features one line each; no prose paragraph over
~4 sentences / 100 words; front-load the answer; concrete task headings; real
runnable examples tested before publishing.

README checklist (skill): first 1-2 sentences say what it is + who it is for;
why before how; install copy-pasteable and tested; common usage first; features
specific/honest; no template-only sections.

## 16. SCOPE BOUNDARY (what this README owns vs neighbors)

- THIS item (P1.M5.T3.S1) writes ONLY `README.md` at repo root.
- P1.M5.T3.S2 (sibling) owns `config.example.json` AND the `doc.go` package
  comment polish. config.example.json does NOT exist yet at research time.
  => The README should DOCUMENT the config schema inline (it is stable, from
     config.go) and POINT to `config.example.json` as the ready-to-copy example,
     without depending on S2's exact wording. State "see config.example.json for
     a ready-to-copy example" so the cross-reference resolves once S2 lands.
- Do NOT modify doc.go, config.go, PRD.md, tasks.json, prd_snapshot.md, go.mod.
- Mode B: describe SHIPPED behavior. Every claim above was read from source.

## 17. Exact runnable smoke commands to publish in / verify the README

These were verified against the on-disk binary/source and are safe to publish:
- `go build -o web-search-prime-fixer .`
- `./web-search-prime-fixer &` then `curl -s http://127.0.0.1:8787/healthz`
  -> `{"ok":true,"version":"dev"}` (default build) or the ldflags value.
- `go test ./...`
- Client config block: the PRD §7.1 JSON verbatim.
- Warning example: the PRD §7.2 line verbatim (matches warningText output).
