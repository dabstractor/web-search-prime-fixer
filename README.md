# web-search-prime-fixer

web-search-prime-fixer is a local proxy that fixes a common misspelling in calls to the z.ai `web-search-prime` MCP tool: it renames the `query` parameter (and a few other aliases) to the required `search_query`, forwards the corrected call, and prepends a one-line warning so the agent learns the right name. It is for one developer running a single local instance for their own MCP clients (pi, Claude Code, Cursor, and others).

The problem it solves: agents call the tool with `{"query": "..."}` instead of `{"search_query": "..."}`, get empty or wrong results, and retry. Point the client at this proxy and the next misspelled call returns the correct result on the first try.

## How it works

For an outbound `tools/call`, the proxy rewrites the arguments once when any configured alias is present:

- If `search_query` is absent, the first present alias (in config order) is promoted to `search_query`, and the other aliases are removed.
- If `search_query` is already present, all aliases are removed (the canonical value wins).
- Every other parameter is passed through untouched.

Default aliases: `query`, `q`, `search`, `searchQuery`, `search_term`. The target is always `search_query`. A correct call (`{"search_query": "..."}`) is forwarded byte-for-byte with no warning.

## What the agent sees

On a misspelled call, the proxy prepends a `text` content block to the real result. The agent gets the correct results on the first call and does not need to retry. A call that sends `{"query": "rust async runtime"}` produces this warning in the result:

```
[web-search-prime-fixer] "query" is not a valid parameter; renamed to "search_query". Use "search_query" in future calls.
```

When `search_query` was already present and only aliases were ignored, the suffix differs:

```
[web-search-prime-fixer] ignored "query" (use only "search_query"). Use only "search_query" to avoid this notice.
```

`isError` is never set by the proxy. A call that needed no rewrite is identical to talking to z.ai directly.

## Build

Requires Go 1.22 or newer (see `go.mod`). The module uses the Go standard library only: no external dependencies, no `go.sum`, and no network access is needed to build.

```
go build -o web-search-prime-fixer .
```

The produced binary is `./web-search-prime-fixer`.

To set the version reported by `/healthz`, pass it at build time. A plain build reports `dev`:

```
go build -ldflags "-X main.version=0.1.0" -o web-search-prime-fixer .
```

## Run

With no arguments and no config file, the proxy listens on `127.0.0.1:8787` and forwards to the z.ai upstream:

```
./web-search-prime-fixer
```

Change the bind address:

```
WSPF_LISTEN=127.0.0.1:9000 ./web-search-prime-fixer
```

Load a config file (`WSPF_CONFIG` is used verbatim; if the named file is missing, the proxy exits with an error rather than falling back to defaults):

```
WSPF_CONFIG=./my-config.json ./web-search-prime-fixer
```

The proxy binds `127.0.0.1` only (it is not an open proxy). It shuts down gracefully on `Ctrl-C` or `SIGTERM`.

## Configure your MCP client

Paste this block into your client's server config. It is identical to a direct z.ai config, except the `url` points at the proxy. The `Authorization` header stays on the client; the proxy forwards it verbatim and holds no key of its own.

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

Replace `your_api_key` with your z.ai API key. Only the URL changes from a direct configuration; nothing else in the block differs.

## Health check

Confirm the proxy is running:

```
curl http://127.0.0.1:8787/healthz
```

Response is `200` with body `{"ok":true,"version":"<version>"}`. `version` is `dev` for a plain build, or the value injected with `-ldflags`. `/healthz` does not call the upstream; it is a local liveness check.

## Configuration

The proxy runs from built-in defaults with no config file. Override with environment variables or a JSON config file. Env vars take precedence over the file; the file takes precedence over defaults.

| Env var | Overrides | Notes |
| --- | --- | --- |
| `WSPF_CONFIG` | config file path | Used verbatim. A missing file is a hard error, not a silent fallback to defaults. |
| `WSPF_UPSTREAM` | the z.ai MCP URL | Highest precedence. |
| `WSPF_LISTEN` | the bind address | |
| `WSPF_LOG_LEVEL` | the log level | `debug`, `info`, `warn`, or `error`. Default `info`. |

`path` and `aliases` have no env override. Empty env values are ignored.

Config file discovery order:

1. `WSPF_CONFIG`, if set (verbatim).
2. Otherwise, the first existing of:
   - `./web-search-prime-fixer.json`
   - `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json` (defaults to `~/.config/web-search-prime-fixer/config.json` on Linux/macOS)
3. Otherwise, no file. Built-in defaults are used.

JSON keys and defaults (from `config.go` `DefaultConfig`):

| Key | Default | Notes |
| --- | --- | --- |
| `upstream` | `https://api.z.ai/api/mcp/web_search_prime/mcp` | Must be an absolute URL. |
| `listen` | `127.0.0.1:8787` | Must parse as `host:port`. |
| `path` | `/mcp` | Informational. All non-`/healthz` paths forward to the upstream. |
| `aliases` | `["query","q","search","searchQuery","search_term"]` | Order matters: the first present alias is promoted. |
| `target_param` | `search_query` | Forced to `search_query` if empty. |
| `log_level` | `info` | |

Unknown JSON fields are ignored. See `config.example.json` for a ready-to-copy example.

## Non-goals

The proxy does one thing: rename a configured alias to `search_query`. It does not:

- infer or default `location`
- normalize `content_size`, `search_recency_filter`, or any enum
- truncate or shorten the query text
- drop, map, or warn about unsupported parameters (for example `max_results`, `safe_search`); they pass through untouched
- rewrite the tool schema or the `tools/list` response (forwarded verbatim)
- manage API keys or rotate credentials
- retry failed upstream calls, rate-limit, or cache results

Rule of thumb: if a behavior is not "rename a configured alias to `search_query`", it passes through unchanged.

## Tests

```
go test ./...
```

The suite uses `httptest` fakes and golden SSE fixtures. It makes no network calls and needs no credentials.
