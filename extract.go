package main

import (
	"encoding/json"
	"fmt"
)

// ExtractionResult describes the outcome of extracting a search query from a
// tools/call arguments payload. S1 (this subtask) populates Query, Source, and
// Found for precedence levels 1-3; Ambiguous and Optionals are populated by S3
// (inference fallback and optional-parameter normalization) for object inputs.
//
// Invariants: Found==true implies Query != "". Found==false implies the zero
// value (Query=="", Source=="", Ambiguous==false, Optionals==nil).
type ExtractionResult struct {
	Query     string
	Source    string
	Ambiguous bool
	Optionals map[string]any
	Found     bool
}

// extract recovers a search query string from a raw tools/call arguments payload
// (PRD §10.1 precedence). It is PURE and DETERMINISTIC: no I/O, no globals, no
// side effects; it dispatches on the Go type produced by json.Unmarshal.
//
// Precedence (levels handled in this subtask):
//  1. arguments is a STRING  -> that is the query (source "bare-string").
//  2. arguments is a NUMBER/BOOLEAN -> stringify via fmt.Sprint (source "scalar").
//  3. arguments is an ARRAY  -> recurse on the FIRST element that yields a query
//     (source "array[0]"); if none yields, extraction fails.
//
// Failure (Found==false, zero value): nil/zero-length raw (no arguments field),
// invalid JSON, JSON null, or an empty array with no yielding element. An OBJECT
// input (level 4) is handled by P1.M2.T1.S2 (alias scan + inference); until then
// it returns Found==false.
//
// queryAliases and optionalAliases are read only by the object path (S2/S3) and
// are accepted here for signature stability; they are threaded to recursive
// extraction so array-of-object inputs work once S2 lands.
func extract(raw json.RawMessage, queryAliases []string, optionalAliases map[string][]string) ExtractionResult {
	if len(raw) == 0 { // missing "arguments" field arrives as a zero-length RawMessage
		return ExtractionResult{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil { // invalid JSON
		return ExtractionResult{}
	}
	return extractValue(v, queryAliases, optionalAliases)
}

// extractValue dispatches on the decoded value's Go type. It is the extension
// seam: P1.M2.T1.S2 replaces the `case map[string]any` body with the level-4
// alias scan + nested drill-in; P1.M2.T1.S3 adds inference, optionals, failure.
func extractValue(v any, queryAliases []string, optionalAliases map[string][]string) ExtractionResult {
	switch x := v.(type) {
	case nil:
		return ExtractionResult{}
	case string:
		return ExtractionResult{Query: x, Source: "bare-string", Found: true}
	case float64, bool:
		return ExtractionResult{Query: fmt.Sprint(v), Source: "scalar", Found: true}
	case []any:
		for _, elem := range x {
			if r := extractValue(elem, queryAliases, optionalAliases); r.Found {
				return ExtractionResult{
					Query:     r.Query,
					Source:    "array[0]", // PRD §10.1.3: array source is always "array[0]"
					Ambiguous: r.Ambiguous,
					Optionals: r.Optionals,
					Found:     true,
				}
			}
		}
		return ExtractionResult{} // no element yielded -> fail
	case map[string]any:
		// Level 4 (object alias scan + nested drill-in + inference) — P1.M2.T1.S2.
		return ExtractionResult{}
	}
	return ExtractionResult{} // unreachable for any valid JSON value
}
