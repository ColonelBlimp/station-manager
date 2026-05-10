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

// TestReverseBoundary_NoBridgeImportsFromStorageOrForwarder is the
// other direction of ADR 0013's discipline: storage / forwarder /
// qsoservice MUST NOT import internal/bridge. Catches the failure
// mode where rig-state knowledge leaks into log/forward code.
//
// We walk the relevant package directories and look for
// `internal/bridge` in their imports. Test files excluded.
func TestReverseBoundary_NoBridgeImportsFromStorageOrForwarder(t *testing.T) {
	bridgePkg := "github.com/ColonelBlimp/station-manager/internal/bridge"

	dirs := []string{
		"../database/sqlite",
		"../forwarding",
		"../qsoservice",
	}

	fset := token.NewFileSet()
	for _, dir := range dirs {
		matches, err := filepath.Glob(filepath.Join(dir, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", dir, err)
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
					t.Errorf("%s: forbidden import %q (ADR 0013 package boundary — storage/forwarder/qsoservice MUST NOT import bridge)", path, pkg)
				}
			}
		}
	}
}
