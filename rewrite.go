package main

import "fmt"

// RewriteResult describes the outcome of one Rewrite call. Notes carries the
// algorithm-literal per-alias messages that warningText joins into the SSE
// warning (PRD §12.3). Invariants: Changed==true implies len(Notes)>=1;
// Changed==false implies Notes==nil and args untouched (zero-value result).
type RewriteResult struct {
	Changed bool
	Notes   []string
}

// Rewrite applies the configured alias->target rename to args IN PLACE and
// returns what happened. aliases is walked in CONFIG ORDER: when target is
// absent, the first present alias is promoted. The args map is NEVER iterated
// (no "for k := range args"), because Go map iteration is randomized and would
// make the promoted-alias choice nondeterministic (architecture/external_deps.md
// §6). Duplicate alias entries are de-duped while building the present list, so
// a duplicate config alias produces no double note and no spurious drop.
//
// Non-alias keys are never touched (PRD §3: never normalize, truncate, or move
// values that are not configured aliases). When nothing matches, args is left
// byte-for-byte unchanged and a zero-value RewriteResult is returned, so callers
// may forward the original bytes without re-serialization.
//
// In-place mutation contract: args is the caller's map reference; Rewrite
// mutates it directly. Callers that need the original must clone first.
//
// Result invariants:
//   - Changed==true  implies len(Notes) >= 1.
//   - Changed==false implies Notes==nil AND args is untouched.
//
// Algorithm (PRD §10):
//  1. nil args, empty target, or empty aliases -> unchanged.
//  2. present = aliases (config order) that exist as keys in args, de-duped.
//  3. present empty -> unchanged.
//  4. target already a key: canonical value wins; drop every present alias,
//     noting `ignored "<alias>" (use only "<target>")`.
//  5. target absent: promote present[0] (rename note), drop present[1:]
//     (dropped-redundant notes).
func Rewrite(args map[string]any, aliases []string, target string) RewriteResult {
	// 1. Guard: nothing to do.
	if args == nil || target == "" || len(aliases) == 0 {
		return RewriteResult{}
	}

	// 2. Build present in CONFIG ORDER, de-duped. NEVER range over args.
	seen := make(map[string]bool)
	var present []string
	for _, a := range aliases {
		if seen[a] {
			continue
		}
		seen[a] = true
		if _, ok := args[a]; ok {
			present = append(present, a)
		}
	}

	// 3. Empty short-circuit: leave args byte-for-byte unchanged.
	if len(present) == 0 {
		return RewriteResult{}
	}

	notes := make([]string, 0, len(present))

	// 4. Target already present: canonical value wins; drop all aliases.
	if _, ok := args[target]; ok {
		for _, a := range present {
			delete(args, a)
			notes = append(notes, fmt.Sprintf("ignored %q (use only %q)", a, target))
		}
		return RewriteResult{Changed: true, Notes: notes}
	}

	// 5. Target absent: promote the first present alias, drop the rest.
	chosen := present[0]
	args[target] = args[chosen]
	delete(args, chosen)
	notes = append(notes, fmt.Sprintf("%q is not a valid parameter; renamed to %q", chosen, target))
	for _, a := range present[1:] {
		delete(args, a)
		notes = append(notes, fmt.Sprintf("dropped redundant %q", a))
	}
	return RewriteResult{Changed: true, Notes: notes}
}
