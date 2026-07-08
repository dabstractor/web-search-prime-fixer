# web-search-prime-fixer

web-search-prime-fixer is a **normalizing MCP server** (not a proxy), built on the
[official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk), that makes
an agent's **first** web-search attempt succeed — regardless of how it guessed the
tool name or mangled the arguments — while teaching it toward one canonical form.
It runs as one Go binary for a single developer running a single local instance
for their own MCP clients (pi, Claude Code, Cursor).

## What it solves

When an agent calls a web-search MCP tool, two layers of failure conspire to make
the first attempt come back empty or wrong (PRD §1/§2):

- **Layer 1 — wrong tool name.** The client wraps every tool in a prefixed name
  (for example `web_search_web_search_prime`), so the agent's guessed
  `web_search(...)` call names a tool that does not exist.
- **Layer 2 — wrong parameter / shape.** Even when the tool resolves, the agent
  sends `{"query": "..."}` or `{"q": "..."}` or a bare string when the upstream
  expects `{"search_query": "..."}`, gets empty or wrong results, and retries.

The fix is **two parts** (PRD §5): **Part A** unlocks the client so tools are
exposed by their bare names, and **Part B** (this server) advertises a canonical
tool, extracts the query from any input shape, delegates exactly one clean call to
z.ai, and appends a short warning after the results when usage was non-canonical.
The first attempt should just work.

## What it is (and is not)

- **It is** a real MCP server that owns the JSON-RPC surface and `tools/list`,
  advertising essentially one tool (`web_search`). To answer a call, it acts as an
  MCP **client** to z.ai: one lazily-initialized session, reused, and
  re-initialized on expiry.
- **It is not** a byte-level pass-through proxy or a byte forwarder, and it is
  not an open proxy. It rewrites and normalizes, then delegates exactly one call
  per inbound request.
- It binds `127.0.0.1` only and dials only the configured upstream, so it cannot
  be used as an open relay.

## The canonical surface

The server teaches exactly one form (PRD §9.4):

- **Tool:** `web_search`
- **Parameter:** `query` (string, required)
- **Optionals** (passed through to z.ai when provided): `location`,
  `content_size`, `search_recency_filter`

```json
{ "query": "rust async runtime" }
```

Anything else — a different tool name, a different parameter name, a nested/bare
string/array input, or an inferred optional — triggers a warning (see
[What the agent sees](#what-the-agent-sees)). The z.ai-branded tool name
`web_search_prime` is the upstream tool the server delegates to internally, but it
is **never** advertised to the agent (and is rejected if it appears in the
configured `tools` list).

## How it works

For each inbound `tools/call`:

1. **Extract** the query from any input shape (`extract.go`) — any of the
   configured `query_aliases`, or a bare string / array / nested object.
2. **If no query is found** anywhere, return the immediate warning right away and
   **do not** touch the upstream.
3. **Otherwise**, delegate exactly **one** clean call to z.ai using the canonical
   `query` parameter, mapping the optional aliases (`location`,
   `content_size`, `search_recency_filter`) where the agent used a non-canonical
   name.
4. **Return the results**, and if the call was non-canonical, **append** the
   warning after the results.

The two parts of the fix:

- **Part A — client unlock:** configure the MCP client to expose tools by their
  bare names (`toolPrefix: "none"`). Without this, the agent still cannot call
  `web_search` by its bare name.
- **Part B — this server:** advertise `web_search`, extract the query from any
  input, delegate one clean call, and teach the canonical form.

## Configure your MCP client

This is the **Part A** unlock — required for the agent to reach `web_search` by
its bare name. Drop this block into `~/.pi/agent/mcp.json` (PRD §20):

```json
{
  "settings": { "toolPrefix": "none" },
  "mcpServers": {
    "web_search": {
      "type": "http",
      "url": "http://127.0.0.1:8787/mcp",
      "headers": { "Authorization": "Bearer ${Z_AI_API_KEY}" }
    }
  }
}
```

- **`toolPrefix: "none"`** makes pi expose each tool by its bare name, so
  `web_search` is callable as `web_search` rather than
  `web_search_web_search_prime`. It applies **globally** across all of the
  operator's MCP servers. It is safe for the current `web_search` + `zread` set;
  it only matters if two servers ever advertise the same tool name (the operator
  owns that collision, see [Non-goals](#non-goals)).
- **`Authorization` stays on the client.** The server forwards it verbatim to z.ai
  and holds no key of its own (PRD §17, FR-7). `${Z_AI_API_KEY}` is the
  operator's own z.ai API key.
- **Other clients (Claude Code, Cursor):** use whatever makes them expose bare
  tool names (document per-client as needed); the URL
  `http://127.0.0.1:8787/mcp` is the same.

## What the agent sees

Three cases (FR-6: `isError` is **never** set for any normalization, guidance, or
warning case):

1. **Canonical call** (`web_search` + `query`) → results only. No warning, no
   retry.
2. **Non-canonical call** (any other tool name / parameter name / nested /
   bare-string / array / inferred or normalized optional) → **results first**,
   then the warning appended **after** the results:
```
[web-search-prime-fixer] Warning: this call used "<tool>"/"<source>" rather than the canonical form. Results are above. Next time call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```
3. **Empty input** (no usable query anywhere) → the immediate warning below, and
   **no upstream call** is made:
```
[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: web_search with { "query": "..." } — e.g. web_search({ "query": "rust async runtime" }).
```

The example query `"rust async runtime"` is a fixed literal, and both warnings
use a Unicode EM DASH (`—`, U+2014) before `e.g.`.

## Build

Requires Go — see the `go` directive in `go.mod` for the toolchain (currently
`go 1.25.0`). The server depends on the official Go MCP SDK
(`github.com/modelcontextprotocol/go-sdk` v1.6.1); resolve it, then build:

```
go mod tidy
go build -o web-search-prime-fixer .
```

The output binary is `./web-search-prime-fixer`. To set the version reported by
`/healthz`, inject it at build time (a plain build reports `dev`):

```
go build -ldflags "-X main.version=0.2.0" -o web-search-prime-fixer .
```

## Run

With no arguments and no config file, the server listens on `127.0.0.1:8787` and
delegates to the z.ai upstream:

```
./web-search-prime-fixer
```

Change the bind address:

```
WSPF_LISTEN=127.0.0.1:9000 ./web-search-prime-fixer
```

Load a config file (`WSPF_CONFIG` is used verbatim; if the named file is missing,
the server exits with an error rather than falling back to defaults):

```
WSPF_CONFIG=./my-config.json ./web-search-prime-fixer
```

The server binds `127.0.0.1` only (it is not an open proxy). TLS is unnecessary
(loopback HTTP to a TLS upstream). It shuts down gracefully on `Ctrl-C`
(`SIGINT`) or `SIGTERM`, draining in-flight upstream calls within 10s.

## Health check

Confirm the server is running:

```
curl http://127.0.0.1:8787/healthz
```

`GET /healthz` returns `200` with body `{"ok":true,"version":"<version>"}`
(`Content-Type: application/json`). A non-GET request returns `405`. `/healthz`
does **not** touch the upstream; it is a local liveness check. `version` defaults
to `dev` and is set via `-ldflags "-X main.version=..."` at build time.

## Configuration

The server runs from built-in defaults with no config file. Override with
environment variables or a JSON config file. Env vars (when set and non-empty)
take the highest precedence, applied after the file is loaded.

### JSON keys and defaults (from `config.go` `DefaultConfig`)

| JSON key | Default | Notes |
| --- | --- | --- |
| `upstream` | `https://api.z.ai/api/mcp/web_search_prime/mcp` | Must be an absolute URL. |
| `listen` | `127.0.0.1:8787` | Must parse as `host:port`. Local only. |
| `path` | `/mcp` | Informational / reserved. |
| `tools` | `["web_search"]` | Advertised tool names; `tools[0]` is canonical. Never `web_search_prime`. |
| `canonical_tool` | `web_search` | Must be present in `tools`. |
| `canonical_param` | `query` | The parameter we teach. |
| `query_aliases` | `["query","search_query","q","search","searchQuery","search_term","term","text","input","prompt","question","keywords","topic","searchString"]` | Ordered slice; index order = extraction priority. **RENAMED from v1 `aliases`** — see breaking change below. |
| `optional_aliases` | `{"location":["country","region"],"content_size":["size","contentSize","detail"],"search_recency_filter":["recency","freshness","time_filter","date_filter"]}` | Map of z.ai canonical optional → alias priority slice. |
| `target_tool` | `web_search_prime` | z.ai tool called internally; never advertised. |
| `target_param` | `search_query` | z.ai parameter sent upstream; forced to `search_query` if empty. |
| `log_level` | `info` | One of `debug`, `info`, `warn`, `error`. |

### Environment variables

| Env var | Overrides | Notes |
| --- | --- | --- |
| `WSPF_CONFIG` | config file path | Used verbatim. A missing named file is a **hard error** (exit non-zero), not a silent fallback to defaults. |
| `WSPF_UPSTREAM` | `upstream` | Empty values are ignored. |
| `WSPF_LISTEN` | `listen` | Empty values are ignored. |
| `WSPF_LOG_LEVEL` | `log_level` | Empty values are ignored. |

There is **no** env override for `path`, `tools`, `canonical_tool`,
`canonical_param`, `query_aliases`, `optional_aliases`, `target_tool`, or
`target_param` — set those in a config file.

### Config file discovery

1. `WSPF_CONFIG`, if set (used verbatim; a missing file is a hard error).
2. Otherwise, the first **existing** file of:
   - `./web-search-prime-fixer.json`
   - `$XDG_CONFIG_HOME/web-search-prime-fixer/config.json` (defaults to
     `~/.config/web-search-prime-fixer/config.json` on Linux/macOS, resolved
     portably via `os.UserConfigDir`)
3. Otherwise, no file — the server runs with built-in defaults (zero
   configuration needed).

### Breaking change: `aliases` → `query_aliases`

The v1 JSON key **`aliases`** was renamed to **`query_aliases`** in v2 (same
`[]string` semantics). **Operators with a v1 config file MUST rename the key
`aliases` → `query_aliases`** — otherwise the key is silently ignored (unknown
fields are dropped) and the operator's custom alias list is lost (extraction falls
back to the built-in default list). New v2 fields with no v1 equivalent: `tools`,
`canonical_tool`, `canonical_param`, `optional_aliases`, `target_tool`. (The
on-disk `config.example.json` is updated to the v2 schema by a parallel change.)

### Validation

On any validation failure, the server logs and exits non-zero. It checks that:

- `listen` parses as `host:port` (`net.SplitHostPort`).
- `upstream` is an absolute URL (`url.Parse` + `IsAbs`).
- `tools` is non-empty **and** contains `canonical_tool`.
- No `tools` entry equals `target_tool` (never advertise z.ai's real name).
- Unknown JSON fields are ignored.
- `Config` holds **no credential fields** (Authorization is a request header
  owned by the client, never by the server — so startup logging cannot leak a
  secret).

See `config.example.json` for a ready-to-copy example.

## Non-goals

The server's job is explicitly to **normalize** toward one canonical form. It
does not:

- act as a general search engine (it delegates to z.ai's `web_search_prime`; no
  synthesis, ranking, caching, or invention of results);
- act as a general-purpose or open proxy (it binds `127.0.0.1` and dials only the
  configured upstream);
- own credentials (it forwards the client's `Authorization` header and holds no
  key of its own);
- retry, rate-limit, or cache upstream calls (one delegated call per inbound;
  upstream errors are surfaced honestly);
- truncate the query text (the extracted query is forwarded verbatim);
- provide multi-tool semantics (every advertised tool does the same one thing);
- advertise z.ai-branded names (`web_search_prime` is never exposed to the agent);
- take responsibility for other MCP servers' tool-name collisions under
  `toolPrefix: "none"` (the operator owns that).

## Tests

```
go test ./...
```

The suite uses in-process `httptest` fakes and golden SSE fixtures. It makes no
network calls and needs no credentials.
