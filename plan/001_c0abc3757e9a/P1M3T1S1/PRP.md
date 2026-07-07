name: "P1.M3.T2.S1 — testdata/initialize.sse + testdata/tools_call.sse (verified wire format)"
description: |

  Create the **golden SSE fixtures** in `testdata/` that stand in for the live
  z.ai MCP server, so every downstream SSE/proxy test (P1.M3.T1.S1 reader,
  P1.M3.T3.S2 injector, P1.M5.T1 e2e) is deterministic and offline. Three static
  files, matching **PRD §8 byte-for-byte**: (1) `testdata/initialize.sse` — the
  `initialize` response (`id:`/`event:`/`data:`, protocolVersion 2024-11-05,
  serverInfo mcp-web-search-prime 0.0.1); (2) `testdata/tools_call.sse` — a
  `tools/call` response whose `result.content[0].text` is a **JSON-encoded STRING
  (stringified array), NOT an object** (PRD §8); (3) `testdata/tools_call_multiline.sse`
  — the SAME message split across multiple `data:` lines (no indentation) to
  exercise the §8.10 / §19.2 multi-line round-trip. Hard rule: **NO space after
  `data`** (real z.ai wire), every event terminated by a **blank line** (last two
  bytes `0a 0a`). `testdata/initialize.sse` MUST be byte-identical to the existing
  inline `proxy_test.go` `initSSE` constant so P1.M5.T1.S1 can swap inline→fixture
  with zero behavior change. **No Go code ships** in this item (no edits to any
  `.go` file; fixtures only). Mode A docs (none — test fixtures, not user-facing).

---

## Goal

**Feature Goal**: Freeze the z.ai MCP wire format as on-disk golden fixtures so
that P1.M3.T1 (reader), P1.M3.T3 (injector), and P1.M5 (e2e) never touch the live
server and always assert against a deterministic, §8-faithful byte stream. The
fixtures ARE the mock (item §3: "these fixtures ARE the mock for the live
server").

**Deliverable**: THREE new static files under `testdata/`:
- `testdata/initialize.sse` — `initialize` response, single `data:` line.
- `testdata/tools_call.sse` — `tools/call` response, single `data:` line, stringified-array text payload.
- `testdata/tools_call_multiline.sse` — the tools/call message split across multiple `data:` lines (no indentation) for the round-trip test.

No `.go` file is created or modified. `go.mod` gains zero `require`s. No README,
no config.example.json (Mode A docs = none).

**Success Definition**: (a) all three files exist under `testdata/`; (b) each is
terminated by a blank line (last two bytes `0a 0a`); (c) no line starts with
`data: ` (space-after-data is FORBIDDEN — must be `data:{...}`); (d) every
`data:` payload (joined across lines, for the multi-line file) is **valid JSON**;
(e) `tools_call.sse`'s `result.content[0].text` decodes to a JSON **array** (not
an object); (f) `testdata/initialize.sse` is byte-identical to the existing
`proxy_test.go` `initSSE` constant (proven by a byte-diff assertion, not eyeballing).

## User Persona

**Target User**: (1) the **next three work items** that read these files —
P1.M3.T1.S1 (reader: parses initialize + tools_call + multi-line join),
P1.M3.T3.S2 (injector: prepends a warning into the tools/call `content` array,
re-emits with framing), P1.M5.T1.S1/S2 (e2e: fake upstream serves these,
replacing the inline `initSSE`). (2) the **maintainer**, who gets a single,
audited source of truth for "what does z.ai actually send" so a future probe of
the live server can be diffed against these files.

**Use Case**: A reader test opens `testdata/tools_call.sse`, feeds it to the SSE
parser, and asserts the decoded `Event.Data` equals the canonical JSON — without
spinning up z.ai or hand-typing a wire blob inline (which drifts). The multi-line
file proves the parser joins `data:` lines with `\n` and that re-emitting splits
them back identically.

**User Journey**: implementer writes the three files verbatim from the PRP's
"Implementation Patterns" block → runs the one-shot Go validator (parses each
`data:` payload, checks byte structure) → green → next work items load them via
`os.ReadFile("testdata/<name>.sse")`.

**Pain Points Addressed**: (1) Inline wire blobs (like the current `initSSE`)
drift from §8 and get copy-pasted inconsistently across tests — a single fixture
kills that. (2) The "text is a stringified array, not an object" rule is easy to
get wrong by hand; a frozen fixture encodes the correct escaping once. (3) The
multi-line `data:` form is rare on the wire but the parser MUST handle it — a
dedicated fixture makes that an explicit, visible requirement instead of an
undocumented parser detail.

## Why

- **PRD §19.4 (Golden fixtures)**: "Include `testdata/initialize.sse` and
  `testdata/tools_call.sse` captured from the verified wire format (section 8) so
  tests do not depend on the live z.ai server." This item IS §19.4.
- **PRD §8** is the single source of truth for the wire format; the fixtures
  crystallize it as bytes the tests assert against, so a parser/injector bug shows
  up as a test diff, not a live-server heisenbug.
- **Decouples P1.M3 from P1.M5.** The reader/injector can be built and unit-tested
  against these fixtures before the e2e harness (P1.M5.T1) exists.
- **Eliminates drift.** The existing `proxy_test.go` `initSSE` inline constant
  already carries a TODO ("P1.M3.T2 later extracts shared fixtures"); this item is
  that extraction's source side. Keeping them byte-identical makes the future
  inline→fixture swap a no-op (P1.M5.T1.S1).

## What

Three static files under `testdata/`. Each is a single SSE event: one or more
field lines (`id:`/`event:`/`data:`) each LF-terminated, followed by a blank line
that dispatches the event (PRD §8.10). Field names have **NO space** after the
colon (matches §8 real wire; the parser strips an optional leading space for
other servers, but the FIXTURE is the z.ai form).

1. `testdata/initialize.sse` — `id:1` + `event:message` + one `data:` line carrying
   the §8 initialize JSON (protocolVersion `2024-11-05`, capabilities
   `logging`/`tools.listChanged`, serverInfo `mcp-web-search-prime`/`0.0.1`).
2. `testdata/tools_call.sse` — a single `data:` line carrying a §8 tools/call
   JSON-RPC result whose `result.content[0].text` value is a **JSON string** that
   itself encodes an **array** (stringified `[{"title":...,"url":...,"content":...}]`),
   `isError:false`. (No `id:`/`event:` line — §8's tools/call block has only `data:`.)
3. `testdata/tools_call_multiline.sse` — the SAME logical tools/call message, but
   the JSON is broken into **multiple `data:` lines** (newlines only between JSON
   tokens, **zero indentation**) so joining the per-line values with `\n` yields
   valid JSON and re-splitting reproduces the lines exactly (§19.2 round-trip).

### Success Criteria

- [ ] `testdata/initialize.sse`, `testdata/tools_call.sse`, and
      `testdata/tools_call_multiline.sse` all exist.
- [ ] No line in any fixture begins with `data: ` (space after `data`); all use
      `data:{...}` (and `id:`/`event:` likewise have no space).
- [ ] Each file's last two bytes are `0a 0a` (data line's LF + blank-line LF).
- [ ] The `data:` payload of `initialize.sse` and `tools_call.sse` is valid JSON;
      the JOINED `data:` lines of `tools_call_multiline.sse` are valid JSON.
- [ ] `tools_call.sse`'s `result.content[0].text` decodes to a JSON **array**
      (list), not an object.
- [ ] `testdata/initialize.sse` is byte-identical to the `proxy_test.go` `initSSE`
      constant (asserted by byte-diff, not visual).
- [ ] `git diff` shows ONLY additions under `testdata/`; zero `.go` files touched;
      `go.mod` unchanged.

## All Needed Context

### Context Completeness Check

_Pass._ The deliverable is fully pinned: (a) PRD §8 gives the literal wire bytes
for both responses (initialize + tools/call) plus the framing rules (§8.10); (b)
PRD §19.4 names the two canonical fixtures; (c) PRD §19.2 names the multi-line
round-trip test the third fixture serves; (d) the existing `proxy_test.go`
`initSSE` constant pins the exact initialize bytes this item must reproduce; (e)
this PRP's research (`wire-format-and-fixtures.md`) validates every payload as
JSON (§3), derives a drift-free multi-line form (§4), and maps every consumer
(§6). The one non-obvious trap — the multi-line fixture must use **zero
indentation** or the §12.1 "strip one leading space" rule breaks round-trip
symmetry — is flagged in Known Gotchas. An agent with no prior knowledge of this
codebase can write the three files from this PRP alone.

### Documentation & References

```yaml
# MUST READ — the wire format the fixtures crystallize.
- file: PRD.md
  section: "§8 Verified transport contract (z.ai upstream)"
  why: the literal initialize + tools/call bytes, the "no space after data" note,
        and the "result payload is a JSON-encoded STRING (stringified array), not
        an object" contract. This is the byte-level source of truth.
  critical: the §8 code fence shows `id:1`, `event:message`, `data:{...}` — ALL
        with NO space after the colon. The tools/call block shows ONLY a `data:`
        line (no id:/event:). Reproduce both literally.

- file: PRD.md
  section: "§8 SSE framing rules (for the parser/writer)"
  why: event = field lines terminated by a blank line; multiple `data:` lines
        join with `\n` on read and split on write. Pins the trailing-blank-line
        terminator and the multi-line variant's design.
  critical: "In practice z.ai emits a single `data:` line per message; the parser
        must still handle the multi-line form correctly." — that is exactly why
        the third fixture exists.

- file: PRD.md
  section: "§12.1 Reader" (Event struct + parsing rules)
  why: the consumer's parsing rules — comments (`:` prefix), `field: value` with
        ONE optional leading space stripped, `data` lines accumulated with `\n`,
        blank line dispatches + resets, EOF flush. Defines what "correct framing"
        means for the fixtures.
  critical: the "strip one leading space after the colon IF PRESENT" rule is why
        the multi-line fixture uses ZERO indentation (see Known Gotchas).

- file: PRD.md
  section: "§19.2 sse_test.go" + "§19.4 Golden fixtures"
  why: §19.4 names the two canonical fixtures; §19.2 enumerates the reader tests
        that consume them, including "Multi-`data:`-line event round-trips
        (split/rejoin preserves content)" — the third fixture's reason for being.

- file: proxy_test.go
  why: contains the inline `initSSE` constant whose comment reads "(P1.M3.T2
        later extracts shared fixtures)." `testdata/initialize.sse` MUST equal it
        byte-for-byte so the future inline→fixture swap is a no-op.
  pattern: `const initSSE = "id:1\nevent:message\n" + 'data:{...}' + "\n\n"`.
  gotcha: do NOT edit proxy_test.go in this item — only create testdata files.
        P1.M5.T1.S1 performs the swap.

- url: https://html.spec.whatwg.org/multipage/server-sent-data.html#parsing-an-event-stream
  why: WHATWG SSE "parsing an event stream" — authoritative framing (data-line
        join with U+000A, blank-line dispatch, field-value = after colon with one
        optional leading space removed). The rules the fixtures must satisfy.
  critical: confirms a `data:` value may be delivered across multiple `data:`
        lines joined by `\n` — the multi-line fixture's basis.

- url: https://www.rfc-editor.org/rfc/rfc8895
  why: RFC 8895 (IETF SSE) restates the same framing; cite for reviewers who
        prefer an RFC over the WHATWG living standard.

- docfile: plan/001_c0abc3757e9a/P1M3T1S1/research/wire-format-and-fixtures.md
  why: the byte-level implementation cheat-sheet — exact payloads (§1, §3),
        framing rules (§2), the drift-free multi-line design + why zero-indent
        (§4), the initSSE identity proof (§5), and the consumer map (§6).
  section: "§4 Multi-line `data:` variant" and "§5 Compatibility with initSSE".

# CONTRACTS (read-only — do not break):
- file: plan/001_c0abc3757e9a/P1M2T1S1/PRP.md
  why: RewriteResult{Changed bool; Notes []string} is FROZEN. The warning this
        fixture's consumer (P1.M3.T3.S2) will inject is built from Rewrite.Notes;
        the fixtures themselves do not reference Rewrite, but their tools/call
        `content` array is the structure a warning block gets prepended INTO, so
        its shape (content[].{type,text}) must stay exactly as §8 specifies.
- file: plan/001_c0abc3757e9a/P1M1T4S2/PRP.md
  why: defines proxy_test.go + the inline initSSE this item must reproduce
        byte-for-byte (the swap target).
```

### Current Codebase tree (run `ls` / `tree` at repo root)

```bash
web-search-prime-fixer/
  go.mod            # module web-search-prime-fixer; go 1.22  (stdlib only, zero requires)
  doc.go            # package comment (package main)
  main.go           # bootstrap (P1.M1.T4) — UNTOUCHED
  config.go         # Config (P1.M1.T2) — UNTOUCHED
  config_test.go    # test-convention reference — UNTOUCHED
  resolve_test.go   # test-convention reference — UNTOUCHED
  logger_test.go    # UNTOUCHED
  health_test.go    # UNTOUCHED
  proxy.go          # passthrough forward core (P1.M1.T4.S2) — UNTOUCHED
  proxy_test.go     # contains inline `initSSE` constant — UNTOUCHED (fixture equals it)
  rewrite.go        # Rewrite (P1.M2.T1) — UNTOUCHED
  rewrite_test.go   # rewrite tests (P1.M2.T1) — UNTOUCHED
  testdata/.gitkeep # empty dir placeholder — STAYS (fixtures added alongside)
  PRD.md
```

### Desired Codebase tree with files to be added and responsibility of file

```bash
testdata/
  .gitkeep                    # UNCHANGED (keep the dir tracked even if a later
                              #            cleanup removes it; harmless).
  initialize.sse              # NEW — §8 initialize response (1 event: id+event+data,
                              #        blank-terminated). Byte-identical to proxy_test.go
                              #        `initSSE`. Consumed by P1.M3.T1.S1 + P1.M5.T1.S1.
  tools_call.sse              # NEW — §8 tools/call response (1 event: single data: line,
                              #        blank-terminated). text payload = stringified ARRAY.
                              #        Consumed by P1.M3.T1.S1 + P1.M3.T3.S2 + P1.M5.T1.S2.
  tools_call_multiline.sse    # NEW — SAME message split across multiple data: lines
                              #        (zero indent) for the §19.2 round-trip test.
                              #        Consumed by P1.M3.T1.S1 + P1.M3.T3.S2.
# NO .go files created or modified. No README, no config.example.json (Mode A docs=none).
```

### Known Gotchas of our codebase & Library Quirks

```
CRITICAL — NO SPACE AFTER `data`: real z.ai wire is `data:{...}`, NOT `data: {...}`.
  The PRD §8 note says so explicitly; the existing proxy_test.go `initSSE` raw
  string literal confirms it (`data:{...}` with no space). A fixture with a space
  would still PARSE (§12.1 strips one optional leading space) but would NOT match
  real wire and would break the byte-identity check against `initSSE`. Write NO
  space after `id:`, `event:`, or `data:`.

CRITICAL — BLANK-LINE TERMINATOR (last two bytes = 0a 0a): an SSE event is a run
  of field lines terminated by a blank line (§8.10). So every fixture is:
    <field lines, each ending in \n> + \n   (the blank line)
  i.e. the file ends with the data line's `\n` immediately followed by another
  `\n`. The existing `initSSE` ends in `"\n\n"` — match it. A file ending in a
  single `\n` (no blank line) would rely on EOF-flush (§12.1) and would NOT look
  like real wire; the canonical fixtures always carry the blank line.

CRITICAL — tools_call text payload is a STRINGIFIED ARRAY, not an object (PRD §8):
  result.content[0].text must be a JSON STRING whose value is a JSON array
  ("[{...}]"), with inner quotes escaped (\"...). It must NOT be a bare object or
  a bare array token. P1.M3.T3.S2 (inject) prepends a warning block and leaves
  this string byte-intact; if the fixture shipped an object, the injector's
  "keep the text string intact" contract would be untestable. Verified array.

GOTCHA — the multi-line fixture MUST use ZERO indentation. §12.1 strips exactly
  ONE leading space after the colon IF PRESENT. A line `data:  "x"` (2 spaces)
  yields value ` "x"` (1 space) — still valid JSON, but the round-trip
  join(split(x))==x would be off and a strict equality test could mislead. Lines
  like `data:"x"` (no leading space) have nothing to strip, so the join is
  exactly the stored value. Keep newlines BETWEEN tokens only; never indent.

GOTCHA — JSON newlines may live ONLY between tokens, never inside string values.
  Raw U+000A inside a JSON string is invalid JSON. The multi-line fixture breaks
  the JSON at token boundaries (after `{`, after `,`, etc.), never inside the
  stringified-array text. The escaped `\"` inside the text payload are ordinary
  characters to SSE framing (no special meaning) and ride along fine.

GOTCHA — LF only, no CR. z.ai emits LF (`\n`). Do not let an editor add CRLF;
  that changes the byte stream and the `initSSE` identity check. If in doubt,
  verify with `file testdata/*.sse` (expect "ASCII text", not "with CRLF line
  terminators") and `od -c`.

GOTCHA — do NOT edit any .go file. This item is fixtures-only. In particular do
  NOT touch proxy_test.go's `initSSE` (P1.M5.T1.S1 swaps it later) and do NOT
  create sse.go/sse_test.go (that is P1.M3.T1.S1, the next item). The fixtures
  must stand alone and parse without any new code.

GOTCHA — initialize.sse byte-identity with `initSSE` is a HARD gate. The proxy_test.go
  constant is built by string concatenation across 4 raw-string lines; retype it
  EXACTLY (same JSON key order, same compact spacing, same `\n\n` end). Validate
  with a byte diff, not by eye (see Validation Loop Level 2).
```

## Implementation Blueprint

### Data models and structure

None. These are static byte files (SSE event streams), not Go types. The structure
they encode is the MCP JSON-RPC envelope the rest of the project consumes:

- `initialize` result: `{protocolVersion, capabilities, serverInfo}` (PRD §8).
- `tools/call` result: `{content:[{type:"text", text:<stringified array>}], isError:false}`
  (PRD §8). The `text` value is a JSON string, not a sub-object.

### The exact bytes to write (copy verbatim)

Use a heredoc / raw write per file so the editor cannot reflow the bytes. The
three blocks below ARE the file contents (each block's final blank line is the
event terminator; the files end with `\n\n`).

```
# ---- testdata/initialize.sse (== proxy_test.go initSSE, byte-for-byte) ----
id:1
event:message
data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"tools":{"listChanged":true}},"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}

# ---- testdata/tools_call.sse (text = stringified ARRAY) ----
data:{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],"isError":false}}

# ---- testdata/tools_call_multiline.sse (multi-line data:, ZERO indent) ----
data:{
data:"jsonrpc": "2.0",
data:"id": 2,
data:"result": {
data:"content": [{"type": "text", "text": "[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],
data:"isError": false
data:}
data:}

```

(The `# ----` headers above are for THIS document only — do not write them into the
files. Each file contains exactly the lines under its header, ending with one
blank line so the last two bytes are `0a 0a`.)

### Implementation Tasks (ordered by dependencies)

```yaml
Task 1: CREATE testdata/initialize.sse
  - WRITE the 4-line block above verbatim (id:1 / event:message / data:{...} / <blank>).
  - BYTE RULES: no space after id:/event:/data:; LF line endings; file ends \n\n.
  - EQUALITY GATE: must equal proxy_test.go's `initSSE` constant byte-for-byte
        (validated in Task 5 Level 2). Same protocolVersion 2024-11-05, same
        serverInfo mcp-web-search-prime/0.0.1, same capabilities.
  - PLACEMENT: testdata/initialize.sse.

Task 2: CREATE testdata/tools_call.sse
  - WRITE the 2-line block above verbatim (one data: line + <blank>). NO id:/event:
        line (§8 tools/call block has only data:).
  - PAYLOAD RULE: result.content[0].text is a JSON STRING encoding an ARRAY:
        "[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"
        (inner quotes escaped; isError:false). JSON-RPC "id":2.
  - BYTE RULES: no space after data:; LF; file ends \n\n.
  - PLACEMENT: testdata/tools_call.sse.

Task 3: CREATE testdata/tools_call_multiline.sse
  - WRITE the 9-line block above verbatim: 8 data: lines (the tools/call message
        split at token boundaries, ZERO indentation) + a blank line.
  - ROUND-TRIP RULE: joining the 8 per-line values with "\n" MUST yield valid JSON
        that decodes to the same message as tools_call.sse; re-splitting at "\n"
        reproduces the 8 lines. (Validated in Task 5 Level 2.)
  - ZERO-INDENT RULE: every data: line's value has NO leading space (so §12.1's
        "strip one leading space" is a no-op). The only spaces are INSIDE the JSON
        tokens (e.g. "jsonrpc": "2.0").
  - PLACEMENT: testdata/tools_call_multiline.sse.

Task 4: SANITY-CHECK no collateral edits
  - RUN: git status --short ; expect ONLY three new untracked files under testdata/.
  - RUN: git diff --stat ; expect EMPTY (no .go edits, go.mod untouched).
  - RUN: ls testdata/ ; expect .gitkeep + the three new files.

Task 5: VALIDATE byte structure + JSON well-formedness + array invariant + initSSE identity
  - RUN the one-shot Go validator in "Validation Loop Level 2" (writes nothing to
        the repo; runs from /tmp). All four assertions must pass.
```

### Implementation Patterns & Key Details

```bash
# Write each file with a heredoc that preserves bytes exactly (no editor reflow,
# no trailing-space stripping). Note the literal blank line before EOF — that is
# the event terminator and makes the file end in \n\n.

cat > testdata/initialize.sse <<'EOF'
id:1
event:message
data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{"logging":{},"tools":{"listChanged":true}},"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}

EOF

cat > testdata/tools_call.sse <<'EOF'
data:{"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],"isError":false}}

EOF

cat > testdata/tools_call_multiline.sse <<'EOF'
data:{
data:"jsonrpc": "2.0",
data:"id": 2,
data:"result": {
data:"content": [{"type": "text", "text": "[{\"title\":\"Example Search Result\",\"url\":\"https://example.com/result\",\"content\":\"Lorem ipsum dolor sit amet.\"}]"}],
data:"isError": false
data:}
data:}

EOF
```

```go
// PATTERN (for the CONSUMERS — not built in this item, shown so the fixture shape
// is unambiguous): how P1.M3.T1.S1's reader will load + assert these. The reader
// joins multiple `data:` lines with "\n", strips one optional leading space per
// field, and dispatches on a blank line. So:
//   - initialize.sse     -> Event{ID:"1", Type:"message", Data:`{"jsonrpc":...}`}
//   - tools_call.sse     -> Event{ID:"", Type:"", Data:`{"jsonrpc":"2.0","id":2,...}`}
//   - tools_call_multiline.sse -> Event{Data: <joined pretty JSON, valid>}
// The fixture is correct iff, after that parse, Data is valid JSON whose
// result.content[0].text (tools_call) decodes to an array.

// GOTCHA: a `data:` line whose VALUE is empty (e.g. a stray blank between two
// data: lines) would inject a stray "\n". The multi-line fixture has NO empty
// data: lines — every one carries a JSON token. Keep it that way.
```

### Integration Points

```yaml
FILES CREATED:
  - testdata/initialize.sse             (NEW)
  - testdata/tools_call.sse             (NEW)
  - testdata/tools_call_multiline.sse   (NEW)

FILES NOT TOUCHED (contract):
  - proxy_test.go: the inline `initSSE` constant STAYS until P1.M5.T1.S1 swaps it
        for os.ReadFile("testdata/initialize.sse"). This item only guarantees the
        fixture equals that constant, so the swap is behavior-neutral.
  - go.mod / doc.go / *.go: zero edits (Mode A docs = none; no code ships here).
  - testdata/.gitkeep: leave in place (harmless; keeps the dir tracked).

CONSUMER SEAMS (these fixtures feed — do not change their bytes later):
  - P1.M3.T1.S1 (reader): asserts parse(initialize.sse).Data == canonical init JSON;
        parse(tools_call.sse).Data == canonical tools/call JSON; parse(multiline)
        joins to valid JSON equal to the round-trip.
  - P1.M3.T3.S2 (inject): opens tools_call.sse, prepends a warning block into
        result.content, re-emits — asserts the original text string is byte-intact.
        The stringified-array shape is what makes "byte-intact" meaningful.
  - P1.M5.T1.S1 (e2e harness): fake upstream serves initialize.sse + tools_call.sse;
        replaces the inline `initSSE` (byte-identical, so a no-op).

DATABASE / ROUTES / ENV / CONFIG: none. Static files only.
```

## Validation Loop

### Level 1: Syntax & Style (Immediate Feedback)

```bash
# (a) No accidental CRLF; expect "ASCII text" (NOT "with CRLF line terminators").
file testdata/initialize.sse testdata/tools_call.sse testdata/tools_call_multiline.sse

# (b) NO line starts with "data: " (space after data is FORBIDDEN).
grep -n '^data: ' testdata/*.sse && echo "FAIL: space after data" || echo "OK: no space after data"

# (c) Every file ends with the blank-line terminator (last two bytes = 0a 0a).
for f in testdata/initialize.sse testdata/tools_call.sse testdata/tools_call_multiline.sse; do
  last=$(tail -c2 "$f" | od -An -tx1 | tr -d ' \n')
  [ "$last" = "0a0a" ] && echo "$f OK (ends \\n\\n)" || echo "$f FAIL (ends $last, want 0a0a)"
done

# (d) No collateral edits: only the three new files under testdata/ are untracked;
#     no tracked file changed; go.mod untouched.
git status --short        # expect: ?? testdata/initialize.sse  ?? testdata/tools_call.sse  ?? testdata/tools_call_multiline.sse
git diff --stat           # expect: EMPTY
git diff go.mod           # expect: EMPTY

# Expected: file says ASCII text (no CRLF); grep finds no "data: "; every file ends
# 0a0a; git status shows only the 3 new files; all diffs empty.
```

### Level 2: JSON Well-Formedness + Array Invariant + initSSE Identity

```bash
# One-shot Go validator. Writes nothing to the repo (runs from /tmp). Exercises the
# exact framing a correct SSE reader applies: collect data: lines per event (first
# blank line ends the event), join with "\n", parse. Then checks the stringified-
# array invariant and byte-identity with proxy_test.go's initSSE.
cat > /tmp/validate_sse_fixtures.go <<'EOF'
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// firstEventData reads the first SSE event from raw and returns its joined data
// (multiple data: lines concatenated with "\n"), mirroring §12.1.
func firstEventData(raw string) string {
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if l == "" {
			break // blank line dispatches the (first) event
		}
		if strings.HasPrefix(l, "data:") {
			v := strings.TrimPrefix(l, "data:")
			if strings.HasPrefix(v, " ") { // strip ONE optional leading space
				v = v[1:]
			}
			lines = append(lines, v)
		}
	}
	return strings.Join(lines, "\n")
}

func main() {
	files := []string{
		"testdata/initialize.sse",
		"testdata/tools_call.sse",
		"testdata/tools_call_multiline.sse",
	}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			fmt.Println("FAIL read", f, err)
			os.Exit(1)
		}
		data := firstEventData(string(raw))
		var v any
		if err := json.Unmarshal([]byte(data), &v); err != nil {
			fmt.Printf("FAIL %s: data is not valid JSON: %v\n", f, err)
			os.Exit(1)
		}
		fmt.Printf("OK   %s: data is valid JSON\n", f)
	}

	// tools_call.sse: result.content[0].text must decode to an ARRAY, not an object.
	raw, _ := os.ReadFile("testdata/tools_call.sse")
	var msg map[string]any
	if err := json.Unmarshal([]byte(firstEventData(string(raw))), &msg); err != nil {
		fmt.Println("FAIL tools_call.sse: envelope not JSON:", err)
		os.Exit(1)
	}
	res := msg["result"].(map[string]any)
	content := res["content"].([]any)
	text := content[0].(map[string]any)["text"].(string)
	var arr any
	if err := json.Unmarshal([]byte(text), &arr); err != nil {
		fmt.Println("FAIL tools_call.sse: text payload is not valid JSON:", err)
		os.Exit(1)
	}
	if _, ok := arr.([]any); !ok {
		fmt.Println("FAIL tools_call.sse: text payload is NOT an array:", text)
		os.Exit(1)
	}
	fmt.Println("OK   tools_call.sse: result.content[0].text decodes to a JSON ARRAY")

	// initialize.sse must be byte-identical to proxy_test.go's initSSE constant.
	initSSE := "id:1\nevent:message\n" +
		`data:{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05",` +
		`"capabilities":{"logging":{},"tools":{"listChanged":true}},` +
		`"serverInfo":{"name":"mcp-web-search-prime","version":"0.0.1"}}}` + "\n\n"
	disk, _ := os.ReadFile("testdata/initialize.sse")
	if string(disk) != initSSE {
		fmt.Println("FAIL initialize.sse: NOT byte-identical to proxy_test.go initSSE")
		os.Exit(1)
	}
	fmt.Println("OK   initialize.sse: byte-identical to proxy_test.go initSSE")
}
EOF
go run /tmp/validate_sse_fixtures.go

# Expected: five "OK" lines (3 valid-JSON + array + initSSE-identity), exit 0.
# If any line says FAIL, the fixture is wrong — re-read the exact bytes in
# "Implementation Patterns" and rewrite the file with the heredoc (do not hand-edit).
```

### Level 3: Integration Testing (System Validation)

```bash
# No Go code ships in this item, so there is no service to start. The "integration"
# is the consumer-readiness check: confirm the module still builds/tests cleanly
# with the new fixtures present (they must not break the existing suite), and that
# the fixtures are discoverable from a test's working directory (repo root).

go build ./...     # must still compile (it will — no code changed)
go vet ./...       # must still be clean
go test ./...      # existing suite (config/health/logger/proxy/rewrite) must still PASS
ls testdata/       # .gitkeep + initialize.sse + tools_call.sse + tools_call_multiline.sse

# Expected: build/vet/test all clean; the three fixtures listed under testdata/.
# NOTE: full semantic validation (parse→inject→re-emit) is DEFERRED to the
# consuming tests sse_test.go (P1.M3.T1.S1 / P1.M3.T3.S2) and proxy_test.go
# (P1.M5.T1). Those tests will FAIL loudly if any fixture is malformed; this
# item's Level 2 validator is the front-line guarantee they can rely on.
```

### Level 4: Creative & Domain-Specific Validation

```bash
# Round-trip symmetry check for the multi-line fixture: prove that splitting the
# JOINED data back at "\n" reproduces the original per-line values (the §19.2
# round-trip the fixture exists to test). One-shot, from /tmp.
cat > /tmp/roundtrip_multiline.go <<'EOF'
package main

import (
	"fmt"
	"os"
	"strings"
)

func firstEventData(raw string) string {
	var lines []string
	for _, l := range strings.Split(raw, "\n") {
		if l == "" {
			break
		}
		if strings.HasPrefix(l, "data:") {
			v := strings.TrimPrefix(l, "data:")
			if strings.HasPrefix(v, " ") {
				v = v[1:]
			}
			lines = append(lines, v)
		}
	}
	return strings.Join(lines, "\n")
}

func main() {
	raw, _ := os.ReadFile("testdata/tools_call_multiline.sse")
	joined := firstEventData(string(raw))
	// Re-emit: split the joined data at "\n", prefix each with "data:".
	reemit := "data:" + strings.Join(strings.Split(joined, "\n"), "\ndata:") + "\n\n"
	// The re-emitted data: lines must equal the original file's data: lines.
	orig := string(raw)
	if orig != reemit {
		fmt.Println("FAIL multiline round-trip: re-emit != original")
		fmt.Println("ORIG:\n" + orig)
		fmt.Println("REEMIT:\n" + reemit)
		os.Exit(1)
	}
	fmt.Println("OK multiline round-trip: split/rejoin reproduces the original bytes")
}
EOF
go run /tmp/roundtrip_multiline.go

# Expected: "OK multiline round-trip", exit 0. This is the exact invariant
# P1.M3.T3.S2's writer must satisfy for multi-line events; the fixture proves the
# reader+writer pair is identity on this input.
```

## Final Validation Checklist

### Technical Validation

- [ ] Level 1 passes: `file` reports ASCII text (no CRLF); no `data: ` line; every
      file ends `0a 0a`; `git status` shows only the 3 new files; all diffs empty.
- [ ] Level 2 passes: the Go validator prints five `OK` lines (3 valid-JSON +
      tools_call array + initialize.sse==initSSE) and exits 0.
- [ ] Level 3 passes: `go build ./...`, `go vet ./...`, `go test ./...` all clean;
      the three fixtures are listed under `testdata/`.
- [ ] Level 4 passes: the multi-line round-trip validator prints `OK` and exits 0.

### Feature Validation

- [ ] `testdata/initialize.sse` exists, has `id:1`/`event:message`/`data:{...}`,
      protocolVersion `2024-11-05`, serverInfo `mcp-web-search-prime`/`0.0.1`.
- [ ] `testdata/tools_call.sse` exists, has a single `data:` line (no `id:`/`event:`),
      and `result.content[0].text` decodes to a JSON ARRAY (not an object).
- [ ] `testdata/tools_call_multiline.sse` exists, has multiple `data:` lines with
      ZERO indentation, and joins to valid JSON equal-on-re-split to the file.
- [ ] `testdata/initialize.sse` is byte-identical to `proxy_test.go`'s `initSSE`.

### Code Quality Validation

- [ ] Zero `.go` files created or modified (fixtures-only).
- [ ] `go.mod` unchanged (no new `require`s — none possible, no code added).
- [ ] `testdata/.gitkeep` left in place (harmless).
- [ ] No anti-patterns (see below); no space after `data`; no CRLF; no indentation
      in the multi-line fixture; no object-as-text payload.

### Documentation & Deployment

- [ ] Mode A docs honored: NO README / config.example.json / doc.go changes (test
      fixtures are not user-facing).
- [ ] No new env vars / config keys / routes introduced.

---

## Anti-Patterns to Avoid

- ❌ Don't write `data: {...}` (space after `data`). Real z.ai wire is `data:{...}`
  with NO space (PRD §8 note; confirmed by `proxy_test.go`'s `initSSE` raw literal).
  A space still parses (§12.1 strips one optional space) but breaks real-wire
  fidelity AND the byte-identity check against `initSSE`.
- ❌ Don't omit the trailing blank line. An SSE event is field lines TERMINATED BY A
  BLANK LINE (§8.10). Files ending in a single `\n` (no blank line) rely on
  EOF-flush and do not match real wire; every fixture must end `0a 0a`.
- ❌ Don't make `tools_call.sse`'s text payload an object or a bare array token. It
  MUST be a JSON STRING that encodes an array (`"[{...}]"`), per PRD §8. The
  injector (P1.M3.T3.S2) keeps this string byte-intact; an object payload would
  make that contract untestable.
- ❌ Don't indent the multi-line fixture. §12.1 strips ONE leading space after the
  colon; indented lines (`data:  "x"`) lose a space on read and the round-trip is
  not byte-symmetric. Use zero-indent lines (`data:"x"`).
- ❌ Don't put a raw newline INSIDE a JSON string value. Raw U+000A in a JSON string
  is invalid JSON. Break the multi-line JSON only BETWEEN tokens.
- ❌ Don't edit any `.go` file — especially not `proxy_test.go`'s `initSSE`. This
  item creates fixtures only; P1.M5.T1.S1 performs the inline→fixture swap. The
  fixture must EQUAL `initSSE`, not replace it.
- ❌ Don't add `id:`/`event:` lines to `tools_call.sse`. §8's tools/call block shows
  ONLY a `data:` line; reproduce that (the reader's default Type is "message" when
  no `event:` line is present — §12.1).
- ❌ Don't let an editor save CRLF, strip trailing blank lines, or reformat the JSON
  (e.g. Prettier). Write the files with the heredoc in "Implementation Patterns"
  and validate bytes with Level 1/2 before committing.
- ❌ Don't hand-type the initialize JSON from memory. Copy it verbatim from the
  "exact bytes" block (it is the `initSSE` literal) and prove byte-identity with
  the Level 2 validator.
