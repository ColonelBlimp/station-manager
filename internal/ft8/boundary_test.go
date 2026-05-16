package ft8_test

import (
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageBoundary_NoStorageOrForwarderOrBridgeImports enforces
// ADR 0021's package-graph discipline at compile-time-adjacent. The
// narrow daemon scope invariant (log/forward subsystems must not
// couple with FT8) is defended by the import graph; this test makes
// the defense explicit so a refactor that imports the wrong package
// fails CI loudly.
//
// Forbidden imports FROM internal/ft8:
//   - internal/database/sqlite (storage — FT8 doesn't write to DB
//     directly; it goes through internal/qsoservice.Submit so the
//     one-fails-all-fail invariant applies to FT8-logged QSOs the
//     same way it does to SPA-logged QSOs).
//   - internal/forwarding (different concern — forwarding ships
//     QSOs upstream; FT8 produces them).
//   - internal/bridge (sibling subsystem — both consume cat/serial.
//     If FT8 ever needs rig state from bridge, the cleanest answer
//     is a shared event channel exposed from cat or a new neutral
//     package, not a direct bridge → ft8 dependency. ADR-level
//     decision when it surfaces).
//
// Allowed imports FROM internal/ft8 (per ADR 0021):
//   - internal/types (canonical DTOs)
//   - internal/logging (cross-cutting)
//   - internal/errors (cross-cutting, op-tagged)
//   - internal/cat, internal/serial (rig-side dependencies)
//   - internal/qsoservice (FT8 logs decoded QSOs through it)
//   - internal/config (read snapshot via the existing pattern)
//   - stdlib + small leaf packages
//
// Test files are excluded (they may import test helpers from
// anywhere; restricting them adds friction without architectural
// value).
func TestPackageBoundary_NoStorageOrForwarderOrBridgeImports(t *testing.T) {
	forbidden := []string{
		"github.com/ColonelBlimp/station-manager/internal/database/sqlite",
		"github.com/ColonelBlimp/station-manager/internal/forwarding",
		"github.com/ColonelBlimp/station-manager/internal/bridge",
	}

	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

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
			for _, bad := range forbidden {
				if pkg == bad || strings.HasPrefix(pkg, bad+"/") {
					t.Errorf("%s: forbidden import %q (ADR 0021 package boundary — ft8 MUST NOT import storage / forwarding / bridge)", path, pkg)
				}
			}
		}
	}
}

// TestReverseBoundary_NoFt8ImportsFromOtherInternalPackages enforces
// the other direction of ADR 0021's discipline: no internal/*
// package outside the rig-side allowlist may import internal/ft8.
// Catches the failure mode where FT8 internals leak into log /
// forward / bridge code.
//
// Walks every directory under internal/ to unbounded depth via
// filepath.WalkDir. The allowlist mirrors the bridge's reverse-
// boundary check — the api wiring layer may import any subsystem
// to register HTTP routes against it; the subsystem itself is
// self-importing.
func TestReverseBoundary_NoFt8ImportsFromOtherInternalPackages(t *testing.T) {
	ft8Pkg := "github.com/ColonelBlimp/station-manager/internal/ft8"

	// Packages intentionally on the FT8-side of the boundary.
	// internal/api is allowed because it's the HTTP wiring layer
	// that registers any future FT8-facing routes — the
	// shared-imports exception ADR 0013 names also applies here.
	allowlist := map[string]struct{}{
		"ft8": {}, // self
		"api": {}, // HTTP wiring layer
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
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			f, perr := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
			if perr != nil {
				t.Errorf("parse %s: %v", path, perr)
				return nil
			}
			for _, imp := range f.Imports {
				pkg := strings.Trim(imp.Path.Value, `"`)
				if pkg == ft8Pkg || strings.HasPrefix(pkg, ft8Pkg+"/") {
					t.Errorf("%s: forbidden import %q (ADR 0021 package boundary — only the api wiring layer + ft8-internal packages may import internal/ft8)", path, pkg)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}
