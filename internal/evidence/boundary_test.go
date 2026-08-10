package evidence_test

import (
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
)

// TestPackageBoundary_EvidenceStaysNarrow enforces the §4.1 separation at
// the import graph, the same discipline as internal/bridge's boundary test:
// the evidence writer must never couple with the decode subsystem it
// observes (cmd/smd injects an adapter; go-ft8 is the shared vocabulary),
// with the log database whose writer serves the QSO commit (the separate
// file IS the invariant), or with the rig-side subsystems. A refactor that
// adds any of these imports fails CI loudly instead of dissolving the
// boundary quietly.
func TestPackageBoundary_EvidenceStaysNarrow(t *testing.T) {
	forbidden := []string{
		"github.com/ColonelBlimp/station-manager/internal/ft8",
		"github.com/ColonelBlimp/station-manager/internal/database/sqlite",
		"github.com/ColonelBlimp/station-manager/internal/qsoservice",
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
			got := strings.Trim(imp.Path.Value, `"`)
			for _, bad := range forbidden {
				if got == bad || strings.HasPrefix(got, bad+"/") {
					t.Errorf("%s imports %s — the evidence writer must stay decoupled (§4.1; see doc.go)", path, got)
				}
			}
		}
	}
}
