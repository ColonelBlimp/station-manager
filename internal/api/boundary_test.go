package api_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// modulePrefix is the import-path prefix for every package inside this
// module. Only module-internal imports are ratcheted; stdlib and external
// deps are unconstrained.
const modulePrefix = "github.com/ColonelBlimp/station-manager/"

// frozenInternalImports is the set of module-internal packages internal/api
// was allowed to import as of ADR 0043 (2026-07-03). It is a HIGH-WATER MARK,
// not a target: the api-split (ADR 0043) shrinks this set surface by surface,
// and TestPackageBoundary_ApiImportsAreFrozen only fails on a package NOT in
// this set — so removals are always fine, additions are the thing under guard.
//
// This is the anti-regression ratchet from ADR 0043's Consequences: internal/api
// already imports the whole system (efferent coupling ~22), and that breadth
// accreted one unopposed import at a time. Freezing the set means a handler
// that quietly grows a NEW subsystem dependency fails CI, forcing the conscious
// question: "should this cross a port instead?" (ADR 0043 principle 1) rather
// than fattening the god-package further.
//
// To add an entry you must edit this list deliberately — and the edit is the
// signal to ask whether the new coupling belongs behind an httpkit-style port.
var frozenInternalImports = map[string]struct{}{
	"frontend":             {},
	"internal/adif":        {},
	"internal/api/httpkit": {},
	"internal/bridge":      {},
	"internal/buildinfo":   {},
	// Added 2026-08-01 with intent (Diff B, docs/reviews/forwarding-logging-gaps.md
	// F1). POST /v1/forwarder/{name}/uploads IS the manual-backfill producer, so it
	// must name its own provenance when calling qsoservice.EnqueueUploads — that
	// function is shared with the smcloud reconciler and the two are exactly what
	// the origin field exists to tell apart.
	//
	// Considered and rejected: defaulting EnqueueUploads to `manual` so the handler
	// need not name it. That makes a caller who forgets acquire `manual` silently —
	// the fail-open shape deliberately rejected for the column's DB default, and it
	// would let a future producer mislabel itself by omission.
	//
	// A leaf enum package: constants, String and Parse, no behaviour, no further
	// internal imports — the same class of dependency as enums/upload/action.
	"internal/enums/upload/origin": {},
	"internal/cat":                 {},
	"internal/config":              {},
	"internal/database/sqlite":     {},
	"internal/email":               {},
	// Added 2026-08-10 with intent (spot-network §4.1 first capture slice).
	// GET /v1/evidence/status serves evidence.Status directly — the writer's
	// own honesty surface (state, physical usage, counts), which only the
	// writer can answer. A port would re-declare that struct verbatim for one
	// read-only getter; the service is otherwise injected via SetEvidence
	// (nil-safe) and api holds no evidence behaviour. Same class as the
	// bridge/ft8 status couplings already in this set.
	"internal/evidence":     {},
	"internal/enums/bands":  {},
	"internal/enums/modes":  {},
	"internal/enums/source": {},
	"internal/errors":       {},
	"internal/events":       {},
	"internal/forwarding":   {},
	// Added 2026-08-16 with intent (ST-6). The sent-ADIF archive holds full QSO
	// records, so its directory is tightened to 0700 via the ONE private-state
	// policy in internal/fsperm — which resolves symlinks so a symlinked archive
	// dir can't chmod an external target. A LEAF: it imports only the standard
	// library, so it adds no transitive breadth to internal/api.
	"internal/fsperm":   {},
	"internal/ft8":      {},
	"internal/hardware": {},
	"internal/logging":  {},
	// Added 2026-08-03 with intent (ADR 0062). GET /v1/lookup-types serves the
	// enrichment-provider descriptors, exactly as GET /v1/forwarder-types serves
	// forwarder ones from internal/forwarding — which this package has always
	// imported for the same reason. Without it the Settings section keeps a
	// hardcoded provider map and adding a lookup service stays an SPA change.
	//
	// A LEAF: descriptors, a registry, and nothing else — no behaviour, no
	// further internal imports beyond internal/types. Deliberately split out of
	// internal/lookup, which pulls in internal/database/sqlite and could not be
	// imported here or from internal/config without a cycle. Same class of
	// dependency as enums/upload/origin above.
	"internal/lookupdef":  {},
	"internal/lookup":     {},
	"internal/qsoservice": {},
	"internal/types":      {},
	"internal/utils":      {},
	"manual":              {},
}

// TestPackageBoundary_ApiImportsAreFrozen freezes internal/api's module-internal
// import set (ADR 0043). Any non-test .go file that imports a module package not
// in frozenInternalImports fails — the ratchet that stops the god-package from
// growing while the surface split proceeds. It also reports (without failing)
// allowlist entries that are no longer imported, so the list can be trimmed as
// the split sheds dependencies.
//
// Uses the same AST-import-scan technique as internal/bridge/boundary_test.go.
// Test files are excluded — they may import anything for integration setup.
func TestPackageBoundary_ApiImportsAreFrozen(t *testing.T) {
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	seen := map[string]struct{}{}
	fset := token.NewFileSet()
	for _, path := range matches {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			if !strings.HasPrefix(pkg, modulePrefix) {
				continue // stdlib / external — unconstrained
			}
			rel := strings.TrimPrefix(pkg, modulePrefix)
			seen[rel] = struct{}{}
			if _, ok := frozenInternalImports[rel]; !ok {
				t.Errorf("%s: new module-internal import %q is not in the frozen set "+
					"(ADR 0043 ratchet). internal/api's import breadth is frozen; before adding it, "+
					"ask whether this coupling belongs behind a port. If it genuinely does, add %q "+
					"to frozenInternalImports with intent.", path, rel, rel)
			}
		}
	}

	// Informational: allowlist entries no longer imported are candidates to
	// trim as the split sheds dependencies. Not a failure — the ratchet only
	// guards against growth.
	for rel := range frozenInternalImports {
		if _, ok := seen[rel]; !ok {
			t.Logf("frozenInternalImports entry %q is no longer imported — safe to remove as the split proceeds", rel)
		}
	}
}
