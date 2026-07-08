package main

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// shouldWarn is the AFTER-RESULTS teaching-warning predicate (PRD §12.1). It reports
// whether the warning produced by warningText must be appended after a successful
// (Found==true) search's results.
//
// It returns false ONLY for the canonical call: the agent invoked the canonical tool
// name (calledTool == canonicalTool) AND the query came from the canonical parameter
// key (result.Source == canonicalParam) AND no optional parameters were normalized
// and forwarded (len(result.Optionals) == 0). Every other case returns true: a wrong
// tool name; any non-canonical Source (an alias key like "q", "scalar", "bare-string",
// "array[0]", "nested:<key>", or "inferred:<path>"); OR normalized optionals present
// (PRD §12.1 lists "normalized optionals" as a warning trigger even when the tool and
// source are canonical).
//
// shouldWarn reads result.Source and result.Optionals only. It is the AFTER-RESULTS
// predicate: the caller gates on result.Found first (PRD §12.3) — a Found==false result
// takes the immediate no-results warning (noQueryWarningText, §10.1.5) and does NOT
// call shouldWarn. Source reflects how the query was extracted (including
// inferred:<path> for ambiguous/inferred extraction, §12.2) so the agent can confirm
// it searched the right thing; Ambiguity is encoded in Source and is not read here.
func shouldWarn(calledTool string, result ExtractionResult, canonicalTool, canonicalParam string) bool {
	if len(result.Optionals) > 0 {
		return true
	}
	if calledTool != canonicalTool {
		return true
	}
	if result.Source != canonicalParam {
		return true
	}
	return false
}

// warningText returns the warning appended AFTER successful results (PRD §12.2). It is
// a single line: a notice naming the tool/source actually used, followed by a concrete
// correct-usage example built from the canonical tool and parameter. The format is
// byte-fixed (including the EM DASH before "e.g.") and must match PRD §12.2 exactly.
//
// source is the extraction Source label (e.g. "q", "scalar", "nested:input",
// "inferred:messages[0].content"); when extraction was ambiguous or inferred it
// reflects that so the agent can confirm it searched the right thing (§12.2). The
// example query "rust async runtime" is a fixed literal, not a parameter.
func warningText(calledTool string, source string, canonicalTool, canonicalParam string) string {
	return fmt.Sprintf(
		`[web-search-prime-fixer] Warning: this call used "%s"/"%s" rather than the canonical form. Results are above. Next time call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
		calledTool, source, canonicalTool, canonicalParam, canonicalTool, canonicalParam,
	)
}

// noQueryWarningText returns the IMMEDIATE warning returned when extraction found no
// usable query (PRD §12.2, §10.1.5): no upstream call is made and there are no results
// to append after. The format is byte-fixed (including the EM DASH) and matches PRD
// §12.2 exactly. The example query "rust async runtime" is a fixed literal.
func noQueryWarningText(canonicalTool, canonicalParam string) string {
	return fmt.Sprintf(
		`[web-search-prime-fixer] Warning: could not find a search query in the arguments; no search was run. Call: %s with { "%s": "..." } — e.g. %s({ "%s": "rust async runtime" }).`,
		canonicalTool, canonicalParam, canonicalTool, canonicalParam,
	)
}

// appendWarning appends the teaching warning to an upstream search result, AFTER its
// existing content blocks (PRD §12.3). In the delegate flow the server first obtains the
// real search results (a *mcp.CallToolResult from the upstream client), and only then —
// when shouldWarn reported a non-canonical call — appends the warning produced by
// warningText. The ordering matters: results lead, the warning trails, so the model acts
// on the results rather than retrying the tool call on seeing a lone warning.
//
// appendWarning is APPEND-ONLY: it adds a single &mcp.TextContent{Text: text} as the LAST
// element of result.Content. It does not replace, prepend, or reorder existing blocks.
// It does not touch result.IsError: per FR-6 and PRD §12.2 "isError is never set for any
// normalization, guidance, or warning case", and because we never call result.SetError
// (which would set IsError=true and, when Content is empty, overwrite it), IsError stays
// at its zero value (false). text is the already-built warning string (see warningText).
//
// The TextContent block is built with & (pointer) because mcp.TextContent's methods are
// POINTER receivers — only *mcp.TextContent implements the mcp.Content interface.
//
// result must be non-nil; in the dispatch it is always the non-nil upstream result.
func appendWarning(result *mcp.CallToolResult, text string) {
	result.Content = append(result.Content, &mcp.TextContent{Text: text})
}

// noQueryResult builds the IMMEDIATE warning result returned when extraction found no
// usable query (PRD §10.1.5, §12.1, FR-6): no upstream call is made and there is nothing
// to append after. The warning IS the only content. This is the single sanctioned case in
// which a warning travels without results (PRD §12.3) — a retry with correct input is
// exactly what we want.
//
// The returned *mcp.CallToolResult is fresh and non-nil, with Content set to a single
// &mcp.TextContent{Text: text} (the same canonical result shape the SDK itself uses). IsError
// is left at its zero value (false): per FR-6/PRD §12.2 it is never set for any warning
// case, and we build the result by hand rather than calling SetError (which would set
// IsError=true and overwrite Content). StructuredContent and Meta remain nil. text is the
// already-built no-results warning string (see noQueryWarningText).
//
// The TextContent block is built with & (pointer) because mcp.TextContent's methods are
// POINTER receivers — only *mcp.TextContent implements the mcp.Content interface.
func noQueryResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}
