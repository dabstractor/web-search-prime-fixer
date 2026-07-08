package main

import "fmt"

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
