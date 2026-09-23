package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/archive"
	"github.com/ColonelBlimp/station-manager/internal/config"
)

// W-0021 slice 2C: a `.creating` file an interrupted archive creation left in
// the managed directory is diagnosed at every start — named, never listed as
// an archive, never deleted — and the start still succeeds.
func TestLifecycle_StartDiagnosesCreatingArtefacts(t *testing.T) {
	var stray string
	d, orch := newOrchestratedDaemon(t, func(c *config.Config) {
		dir := archive.ManagedDir(*c)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		stray = filepath.Join(dir, "019fd5c5-efcc-7193-be4f-1fee532ee399.db.creating")
		if err := os.WriteFile(stray, []byte("partial"), 0o600); err != nil {
			t.Fatal(err)
		}
	})
	if err := orch.Start(d.workerCtx); err != nil {
		t.Fatalf("orchestrated start failed: %v", err)
	}
	if len(d.creatingArtefacts) != 1 || d.creatingArtefacts[0] != stray {
		t.Fatalf("diagnosed = %v, want [%s]", d.creatingArtefacts, stray)
	}
	if _, err := os.Stat(stray); err != nil {
		t.Fatalf("the artefact was removed at start; it must be left for the operator: %v", err)
	}
	for _, e := range d.cfgSvc.Snapshot().QsoArchives {
		if e.ID == "019fd5c5-efcc-7193-be4f-1fee532ee399" {
			t.Fatal("the artefact was listed as an archive")
		}
	}
}
