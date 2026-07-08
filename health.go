package main

import (
	"encoding/json"
	"net/http"
)

// version is the proxy's build version, surfaced by GET /healthz. It defaults to
// "dev" and is overridden at link time:
//
//	go build -ldflags "-X main.version=<value>" -o web-search-prime-fixer .
//
// It MUST be a package-level var (not local to main) for the linker -X flag to
// set it (PRD §16). Verified on-disk: default "dev"; ldflags injects any string.
var version = "dev"

// healthHandler serves GET /healthz. It writes 200 with a JSON body
// {"ok":true,"version":<version>} and Content-Type application/json.
//
// It is a PURE local handler: it performs NO upstream call and reads NO network
// resource, so it always answers quickly and never depends on z.ai health
// (PRD §16: "Does not touch the upstream").
//
// METHOD (validation NOTE 6): PRD §16 specifies GET /healthz. A non-GET method
// (POST/HEAD/DELETE/...) is answered with 405 Method Not Allowed so a health
// probe that accidentally POSTs cannot masquerade as a liveness check.
//
// The body is produced with json.Marshal of two scalars (bool + string), which
// cannot fail for our inputs; the write to the ResponseWriter is best-effort and
// its error is ignored, matching net/http handler convention.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	// PATTERN: set headers BEFORE WriteHeader (once WriteHeader is called, later
	// Header mutations are ignored).
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	// Marshals to {"ok":true,"version":"<version>"} (keys sorted by json.Marshal;
	// "ok" < "version"). HTML-escaping does not affect semver.
	body, _ := json.Marshal(map[string]any{"ok": true, "version": version})
	_, _ = w.Write(body)
}
