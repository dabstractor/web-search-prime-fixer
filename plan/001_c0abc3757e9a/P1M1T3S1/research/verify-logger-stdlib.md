# Verification — Logger stdlib behavior on go1.26.4

On-disk verification of the four stdlib primitives the logger
(`type logger` + `newLogger` + `log` + `redactHeaders`) rests on, run against the
installed toolchain (`go1.26.4-X:nodwarf5 linux/amd64`, `GOROOT=/usr/lib/go`).
All findings below come from a throwaway `go run` program in a temp module
(no source written in the project repo).

## Method
```bash
cd "$(mktemp -d)"; go mod init verify; cat > main.go <<'EOF'  # ... see PRP research notes
EOF
go run .
```

## Findings (verified)

### 1. `json.Marshal(map[string]any)` — keys are SORTED, output is valid JSON
```
input  map: {ts, level, msg, req_id}
output : {"level":"info","msg":"hi","req_id":"r1","ts":"2026-07-06T21:23:13Z"}
```
Keys are emitted in **alphabetical order** (Go map JSON encoding sorts keys).
The output object has **no insertion-order guarantee** — the contract's "builds a
map with ts, level, msg, then fields" describes the *build order in Go* (which
dictates overwrite precedence for duplicate keys), NOT the JSON key order. This is
deterministic and fine for structured logging (JSON consumers key by name).
`json.Valid(line)` → **true**. Append `'\n'` for one-object-per-line.

### 2. RFC3339 UTC → `"2026-07-06T12:30:45Z"` (the `Z` suffix)
```
time.Date(2026,7,6,12,30,45,0,UTC).Format(time.RFC3339) == "2026-07-06T12:30:45Z"
```
Round-trips: `time.Parse(time.RFC3339, s)` succeeds and `.Equal(original)` is true.
So `ts` = `time.Now().UTC().Format(time.RFC3339)` is the correct, parseable value.
(UTC chosen for timezone-independent logs; RFC3339 permits either a `Z` or a
`+HH:MM` offset — UTC is the safe, conventional choice for a logger.)

### 3. `http.Header` is `map[string][]string`; keys are canonical
```
h.Set("Authorization","Bearer secret")
h.Set("Set-Cookie","a=1"); h.Add("Set-Cookie","b=2")
h.Set("Content-Type","application/json")
→ map[string][]string{
    "Authorization":[]string{"Bearer secret"},
    "Content-Type":[]string{"application/json"},
    "Set-Cookie":[]string{"a=1","b=2"},
  }
http.CanonicalHeaderKey("authorization")  == "Authorization"
http.CanonicalHeaderKey("Set-Cookie")     == "Set-Cookie"
```
- Values are **always `[]string`**. A non-redacted header marshals to a JSON array
  (`["application/json"]`); a redacted header marshals to the plain string
  `"<redacted>"` (we replace the whole value with a single string). This matches
  the contract: the redaction collapses a sensitive header's value(s) to one
  marker.
- `Set`/`Add` store **canonical** keys, so a real `http.Header` from a request has
  keys already in `Authorization`/`Cookie`/`Set-Cookie`/`Proxy-Authorization` form.
  `redactHeaders` still canonicalizes its *comparison* key via
  `http.CanonicalHeaderKey(k)` so a hand-built map with odd casing (e.g.
  `"authorization"`, `"AUTHORIZATION"`) is redacted correctly; the *stored* output
  key is the original `k` (preserves the map's casing — matches "copies header
  names/values").

### 4. ⚠️ CRITICAL — `json.Marshal` HTML-escapes `<`, `>`, `&` by default
```
redacted map JSON: {"Authorization":"\u003credacted\u003e","Content-Type":["application/json"],"Set-Cookie":"\u003credacted\u003e"}
log line JSON     : {"headers":{"Authorization":"\u003credacted\u003e",...},"level":"debug","msg":"forward","ts":"T"}
```
The string `"<redacted>"` is emitted as the bytes `"\u003credacted\u003e"` in the
JSON text. **This is valid JSON** — `json.Unmarshal` of those bytes yields the Go
string `"<redacted>"` exactly. But a **byte-level** assertion will fail:
```go
// ❌ FAILS — the raw bytes contain \u003c, not '<':
strings.Contains(buf.String(), "<redacted>")
bytes.Contains(buf.Bytes(), []byte("<redacted>"))
// ✅ PASSES — compare the decoded Go value, not the raw text:
var m map[string]any
json.Unmarshal(buf.Bytes(), &m)
m["Authorization"] == "<redacted>"
```
**Design decision (kept simple, matches the contract literally):** `log` uses
`b, _ := json.Marshal(m); l.w.Write(append(b, '\n'))` — i.e. default
HTML-escaping is left ON. Rationale: (a) it is the most literal reading of the
contract's "marshals to one JSON line"; (b) the output is still valid JSON and
decodes to `<redacted>`; (c) no extra `json.Encoder`/`SetEscapeHTML(false)` wiring
or double-newline hazard. **Every test MUST compare via `json.Unmarshal`, never
via `strings.Contains`/byte comparison.** This is documented as the #1 gotcha in
the PRP.

### 5. Nested `fields` (e.g. `headers: redactHeaders(h)`) marshal correctly
A `log` call with `fields={"headers": <redacted map>}` produces a single JSON
object with a nested `headers` object. Confirmed valid JSON. The caller (proxy.go,
P1.M4.T3.S1) will call `redactHeaders(req.Header)` and pass the result as the
value of a `"headers"` field — it is NOT spread into the top-level map, so there
is no risk of a field key colliding with `ts`/`level`/`msg`.

## Net
All four primitives behave as expected. The **only** sharp edge is #4 (HTML
escaping of `<redacted>`), and it is handled by mandating `json.Unmarshal`-based
test assertions. No on-disk surprise invalidates the contract's `type logger`,
`newLogger`, `log`, or `redactHeaders` shapes. No third-party dependency needed —
`encoding/json`, `io`, `net/http`, `time` (all stdlib) suffice.
