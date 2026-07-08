package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
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
// (PRD §10 precedence). It is PURE and DETERMINISTIC: no I/O, no globals, no side
// effects; it dispatches on the Go type produced by json.Unmarshal and SORTS map
// keys wherever a result depends on order.
//
// Precedence (levels 1-5):
//  1. STRING   -> that is the query (source "bare-string").
//  2. NUMBER/BOOLEAN -> stringify via fmt.Sprint (source "scalar").
//  3. ARRAY    -> recurse on the FIRST element that yields a query; override the
//     nested Source with "array[0]" (PRD §10.1.3). If none yields, fail.
//     4a. OBJECT alias scan (PRD §10.1.4.1): walk queryAliases in INDEX order; the
//     first alias key that YIELDS wins:
//     - value is a STRING    -> Query=value, Source=<matched alias key verbatim>
//     - value is NUMBER/BOOL -> Query=fmt.Sprint(value), Source="scalar".
//     - value is a MAP/ARRAY -> drill in for the first reachable string (sub-key
//     priority [text,value,content,query,q,data,input], string values only;
//     then sorted recursive descent); on a hit Source="nested:<matched alias
//     key>". On a miss, CONTINUE to the next alias key.
//     4b. OBJECT inference (PRD §10.1.4.2): when NO alias key yields, recursively
//     collect every reachable NON-EMPTY string EXCLUDING recognized optional
//     keys (canonical names + their aliases); longest-wins (ties keep the first
//     in sorted collection order); Source="inferred:<dotted/bracket path>"
//     (e.g. inferred:foo, inferred:messages[0].content); Ambiguous = candidates > 1.
//     Empty strings are NOT collected (an empty string is not a usable query).
//  5. FAILURE (PRD §10.1.5): no usable string anywhere -> the zero value
//     (Found=false). Per FR-6 the caller makes NO upstream call on failure.
//
// Optional-parameter normalization (PRD §10.2): for OBJECT inputs, optionals are
// read SHALLOWLY from the top-level object — canonical name first, then its aliases
// in slice order; the first present key wins; the RAW value is stored under the
// canonical name and attached to EVERY object success return (alias-scan AND
// inference). Enum values are NOT validated/translated (forwarded as-is; §10.2
// out of scope). Optionals is nil on failure. Array-of-object inputs forward the
// object optionals through the unchanged array case.
//
// Clean payload (PRD §10.3): a Found result's ToUpstreamArgs(targetParam) emits
// EXACTLY {targetParam: Query} plus normalized optionals — no alias names, no
// unrecognized keys. Callers gate upstream calls on Found (FR-6).
//
// Determinism (architecture/external_deps.md §6): Go map iteration is randomized,
// so every map range that decides a result collects keys and immediately calls
// sort.Strings (the alias scan walks a slice; the recursive descent and the
// inference collection sort map keys; optional-normalization sorts canonical
// order). Set-building (optionalKeySet) and copy-into-map (ToUpstreamArgs) ranges
// are order-irrelevant.
//
// queryAliases and optionalAliases are read only by the object path and are
// accepted here for signature stability; they are threaded to recursive extraction
// so array-of-object inputs work.
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
		// Optional-parameter normalization (PRD §10.2): SHALLOW read of the
		// top-level object, canonical name first then aliases in slice order.
		// Attached to EVERY object success return below; nil on failure.
		opt := extractOptionals(x, optionalAliases)
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
				return ExtractionResult{Query: val.(string), Source: a, Optionals: opt, Found: true}
			case float64, bool:
				return ExtractionResult{Query: fmt.Sprint(val), Source: "scalar", Optionals: opt, Found: true}
			case map[string]any, []any:
				if s, found := drillIn(val); found {
					return ExtractionResult{Query: s, Source: "nested:" + a, Optionals: opt, Found: true}
				}
				// drill found nothing -> try the next alias key
			default:
				// nil (alias present but JSON null) -> skip to the next alias key
			}
		}
		// Level 4b (P1.M2.T1.S3): inference (PRD §10.1.4.2). No alias key
		// yielded, so recursively collect every reachable NON-EMPTY string,
		// EXCLUDING recognized optional keys. Longest wins (ties keep the
		// first in sorted collection order); Ambiguous = candidates > 1.
		excluded := optionalKeySet(optionalAliases)
		cands := collectReachableStrings(x, excluded)
		if len(cands) > 0 {
			picked := longestCandidate(cands)
			return ExtractionResult{
				Query:     picked.value,
				Source:    "inferred:" + picked.path,
				Ambiguous: len(cands) > 1,
				Optionals: opt,
				Found:     true,
			}
		}
		return ExtractionResult{} // §10.1.5 failure: no usable string anywhere (Optionals nil)
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

// inferredCandidate is a reachable non-empty string collected during inference
// (level 4b), paired with its dotted/bracket path from the root object.
type inferredCandidate struct {
	path  string
	value string
}

// extractOptionals performs shallow optional-parameter normalization (PRD §10.2).
// For each canonical optional (walked in sorted order for determinism) it checks
// the canonical name first, then its aliases in slice order; the first present
// key wins and its RAW value is stored under the canonical name. Enum values are
// NOT validated. Returns nil when nothing is found, so non-optional rows keep
// Optionals nil (backward compatible with reflect.DeepEqual assertions).
func extractOptionals(x map[string]any, optionalAliases map[string][]string) map[string]any {
	var out map[string]any
	canonOrder := make([]string, 0, len(optionalAliases))
	for c := range optionalAliases { // collect only; sorted immediately below for determinism
		canonOrder = append(canonOrder, c)
	}
	sort.Strings(canonOrder)
	for _, canon := range canonOrder {
		keys := append([]string{canon}, optionalAliases[canon]...) // canonical first, then aliases in slice order
		for _, k := range keys {
			if v, ok := x[k]; ok {
				if out == nil {
					out = map[string]any{}
				}
				out[canon] = v
				break // first present key for this canonical wins
			}
		}
	}
	return out
}

// optionalKeySet returns the set of every optional canonical name and alias to
// EXCLUDE from inference (level 4b). This is set-building: map iteration order is
// irrelevant (the result only drives membership tests, never an ordering).
func optionalKeySet(optionalAliases map[string][]string) map[string]bool {
	set := map[string]bool{}
	for canon, aliases := range optionalAliases { // set-building: order irrelevant
		set[canon] = true
		for _, a := range aliases {
			set[a] = true
		}
	}
	return set
}

// collectReachableStrings collects every reachable NON-EMPTY string from root,
// EXCLUDING keys in excluded (the optional canonical names + aliases). It is the
// DFS driver for level-4b inference; maps are sorted for determinism (§6).
func collectReachableStrings(root map[string]any, excluded map[string]bool) []inferredCandidate {
	out := []inferredCandidate{}
	collect(root, excluded, "", &out)
	return out
}

// collect is the DFS body of collectReachableStrings. path is the dotted/bracket
// JSON path from the root object (object entry -> parent+"."+key; array element ->
// parent+"["+i+"]"). Empty strings are skipped (not usable queries, §10.1.5).
func collect(v any, excluded map[string]bool, path string, out *[]inferredCandidate) {
	switch x := v.(type) {
	case string:
		if x != "" { // empty strings are not usable candidates
			*out = append(*out, inferredCandidate{path: path, value: x})
		}
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x { // collect only; sorted immediately below for determinism
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if excluded[k] {
				continue
			}
			cp := k
			if path != "" {
				cp = path + "." + k
			}
			collect(x[k], excluded, cp, out)
		}
	case []any:
		for i, e := range x {
			collect(e, excluded, path+"["+strconv.Itoa(i)+"]", out)
		}
	}
}

// longestCandidate returns the candidate with the longest value. Ties keep the
// FIRST in deterministic collection order (strictly-greater comparison).
func longestCandidate(cands []inferredCandidate) inferredCandidate {
	best := cands[0]
	for _, c := range cands[1:] {
		if len(c.value) > len(best.value) {
			best = c
		}
	}
	return best
}

// ToUpstreamArgs builds the clean arguments forwarded to z.ai (PRD §10.3):
// exactly targetParam -> Query plus any normalized optionals. Everything else
// (alias names, unrecognized keys) is dropped, so every upstream call is
// schema-valid by construction. Intended for Found==true results; callers gate
// upstream calls on Found (FR-6), so no Found check is made here.
func (r ExtractionResult) ToUpstreamArgs(targetParam string) map[string]any {
	args := map[string]any{targetParam: r.Query}
	for k, v := range r.Optionals { // copy into map: order irrelevant
		args[k] = v
	}
	return args
}
