package main

import (
	"encoding/json"
	"fmt"
	"sort"
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
// Precedence:
//  1. STRING   -> that is the query (source "bare-string").
//  2. NUMBER/BOOLEAN -> stringify via fmt.Sprint (source "scalar").
//  3. ARRAY    -> recurse on the FIRST element that yields a query; override the
//     nested Source with "array[0]" (PRD §10.1.3). If none yields, fail.
//     4a. OBJECT  -> alias scan over queryAliases in INDEX order (deterministic).
//     The first alias key that YIELDS wins:
//     - value is a STRING    -> Query=value, Source=<matched alias key verbatim>
//     (e.g. "query", "q", "search"; PRD §15 lists the bare key as a source peer
//     of "bare-string"/"nested:…").
//     - value is NUMBER/BOOL -> Query=fmt.Sprint(value), Source="scalar".
//     - value is a MAP/ARRAY -> drill in for the first reachable string
//     (sub-key priority [text,value,content,query,q,data,input], string values
//     only; then sorted recursive descent); on a hit Source="nested:<matched
//     alias key>" (the TOP-LEVEL alias key, NOT the inner sub-key). On a miss,
//     CONTINUE to the next alias key (so {"query":{},"q":"x"} yields "q").
//
// Determinism (architecture/external_deps.md §6): Go map iteration is randomized,
// so the alias scan walks the queryAliases SLICE by index and the recursive-descent
// fallback SORTS map keys before recursing. extract never ranges a map to pick a
// query; the only for-range over a map collects keys immediately followed by
// sort.Strings.
//
// Failure (Found==false, zero value): nil/zero-length raw (no arguments field),
// invalid JSON, JSON null, an empty array, or an object where no alias key yields.
// The no-alias / no-yield object case (including {"foo":"bar"}) returns Found=false
// here and is the handoff point for P1.M2.T1.S3: inference (§10.1.4.2), optional-
// parameter normalization (§10.2), and the §10.1.5 failure path / §10.3 clean
// payload are S3. S2 never sets Optionals or Ambiguous.
//
// queryAliases and optionalAliases are read only by the object path (S2/S3) and
// are accepted here for signature stability; they are threaded to recursive
// extraction so array-of-object inputs work.
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
		// Level 4a (P1.M2.T1.S2): alias scan, shallow then nested drill-in.
		// Walk queryAliases BY INDEX (deterministic); the first alias that
		// YIELDS wins. A present but non-yielding alias (null, empty nested,
		// array of non-strings) is skipped and the scan continues.
		for _, a := range queryAliases {
			val, ok := x[a]
			if !ok {
				continue
			}
			switch val.(type) {
			case string:
				return ExtractionResult{Query: val.(string), Source: a, Found: true}
			case float64, bool:
				return ExtractionResult{Query: fmt.Sprint(val), Source: "scalar", Found: true}
			case map[string]any, []any:
				if s, found := drillIn(val); found {
					return ExtractionResult{Query: s, Source: "nested:" + a, Found: true}
				}
				// drill found nothing -> try the next alias key
			default:
				// nil (alias present but JSON null) -> skip to the next alias key
			}
		}
		return ExtractionResult{} // no alias yielded -> S3 inference (Found=false here)
	}
	return ExtractionResult{} // unreachable for any valid JSON value
}

// drillIn extracts a string from a nested alias value (a map or array). It is
// used by the object alias scan (level 4a) when an alias key's value is itself a
// map or array rather than a usable scalar.
//
// STEP 1 (maps only): walk the fixed sub-key priority list
// [text,value,content,query,q,data,input] and return the value of the first
// present sub-key whose value is a STRING (non-string values are skipped; drill-in
// extracts strings only, unlike the top-level shallow path which coerces numbers).
// STEP 2: fall back to firstReachableString (sorted recursive descent).
func drillIn(val any) (string, bool) {
	if m, ok := val.(map[string]any); ok {
		for _, sk := range []string{"text", "value", "content", "query", "q", "data", "input"} {
			if sv, present := m[sk]; present {
				if str, isStr := sv.(string); isStr {
					return str, true
				}
			}
		}
	}
	return firstReachableString(val)
}

// firstReachableString returns the first reachable STRING via depth-first search.
// Map keys are SORTED lexicographically before recursion so the result is
// deterministic despite Go's randomized map iteration (external_deps.md §6).
func firstReachableString(v any) (string, bool) {
	switch x := v.(type) {
	case string:
		return x, true
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x { // collect only; sorted immediately below for determinism
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if s, ok := firstReachableString(x[k]); ok {
				return s, true
			}
		}
	case []any:
		for _, e := range x {
			if s, ok := firstReachableString(e); ok {
				return s, true
			}
		}
	}
	return "", false
}
