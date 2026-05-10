package bridge_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageBoundary_NoStorageOrForwarderImports enforces ADR 0013's
// package-graph discipline at compile-time-adjacent. The narrow
// daemon scope invariant (log/forward subsystems must not couple
// with rig state) is now defended by the import graph rather than a
// process boundary; this test makes that defense explicit so a
// future refactor that imports the wrong package fails CI loudly.
//
// Forbidden imports FROM internal/bridge:
//   - internal/database/sqlite (storage)
//   - internal/forwarding (forwarder)
//   - internal/qsoservice (log-write orchestration)
//
// Allowed imports FROM internal/bridge:
//   - internal/types (canonical DTOs)
//   - internal/logging (cross-cutting, not log/forward-specific)
//   - internal/errors (cross-cutting)
//   - internal/cat, internal/serial (rig-side dependencies)
//   - stdlib + small leaf packages
//
// The test parses every non-test .go file in this package and walks
// the AST for ImportSpec nodes. Test files are excluded (they may
// import test helpers from anywhere; restricting them adds friction
// without architectural value).
func TestPackageBoundary_NoStorageOrForwarderImports(t *testing.T) {
	forbidden := []string{
		"github.com/ColonelBlimp/station-manager/internal/database/sqlite",
		"github.com/ColonelBlimp/station-manager/internal/forwarding",
		"github.com/ColonelBlimp/station-manager/internal/qsoservice",
	}

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	fset := token.NewFileSet()
	for _, path := range matches {
		// Skip test files — they're allowed to import anything for
		// integration testing setup.
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imp := range f.Imports {
			// imp.Path.Value is quoted; trim the quotes.
			pkg := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if pkg == bad || strings.HasPrefix(pkg, bad+"/") {
					t.Errorf("%s: forbidden import %q (ADR 0013 package boundary — bridge MUST NOT import storage / forwarder / qsoservice)", path, pkg)
				}
			}
		}
	}
}

// TestReverseBoundary_NoBridgeImportsFromOtherInternalPackages
// enforces the other direction of ADR 0013's discipline: no
// internal/* package outside the rig-side allowlist may import
// internal/bridge. Catches the failure mode where rig-state
// knowledge leaks into log/forward (or any other) code.
//
// Walks every directory under internal/ at depth 1, skipping
// internal/bridge itself + the rig-side allowlist (cat, serial —
// these are bridge dependencies but the dependency goes the other
// way: bridge imports them). Generalised from a hard-coded
// {sqlite, forwarding, qsoservice} list per the 2026-05-10 review
// (#5) so a future internal/X package gets enforcement for free.
//
// Allowlist entries should be rare; each one is a deliberate "this
// package is on the rig-side of the boundary, importing bridge is
// fine" decision. Add to the allowlist only when adding a real
// rig-side dependency, not as a workaround for an accidental
// import that should be refactored.
func TestReverseBoundary_NoBridgeImportsFromOtherInternalPackages(t *testing.T) {
	bridgePkg := "github.com/ColonelBlimp/station-manager/internal/bridge"

	// Packages that are intentionally on the rig-side of the
	// boundary. internal/api is allowed to import bridge because
	// it's the HTTP-server wiring layer that registers the
	// /v1/rig/events route — the one shared-imports exception ADR
	// 0013 names.
	allowlist := map[string]struct{}{
		"bridge": {}, // self
		"api":    {}, // HTTP wiring layer registers the SSE route
	}

	entries, err := filepath.Glob("../*")
	if err != nil {
		t.Fatalf("glob internal/*: %v", err)
	}

	fset := token.NewFileSet()
	for _, dir := range entries {
		base := filepath.Base(dir)
		if _, skip := allowlist[base]; skip {
			continue
		}
		// Walk recursively so sub-packages (e.g.
		// internal/forwarding/qrz) are covered too.
		matches, err := filepath.Glob(filepath.Join(dir, "**/*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
		}
		// Glob with ** isn't standard; fall back to manual walk.
		topMatches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s top: %v", dir, err)
		}
		matches = append(matches, topMatches...)
		// Sub-directories one level deep.
		subdirs, err := filepath.Glob(filepath.Join(dir, "*/"))
		if err == nil {
			for _, sub := range subdirs {
				subMatches, _ := filepath.Glob(filepath.Join(sub, "*.go"))
				matches = append(matches, subMatches...)
				// One more level (e.g. internal/forwarding/qrz/...)
				subSubdirs, _ := filepath.Glob(filepath.Join(sub, "*/"))
				for _, sub2 := range subSubdirs {
					sub2Matches, _ := filepath.Glob(filepath.Join(sub2, "*.go"))
					matches = append(matches, sub2Matches...)
				}
			}
		}

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
				if pkg == bridgePkg || strings.HasPrefix(pkg, bridgePkg+"/") {
					t.Errorf("%s: forbidden import %q (ADR 0013 package boundary — only the api wiring layer + rig-side packages may import internal/bridge)", path, pkg)
				}
			}
		}
	}
}
