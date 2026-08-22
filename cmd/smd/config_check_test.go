package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// AC8 — even default-path resolution is read-only. The daemon's ordinary
// WorkingDir resolver creates a first-run directory, but config-check must not:
// a missing live install should produce a not-found error and no filesystem
// artifact.
func TestRunConfigCheck_DefaultPathDoesNotCreateWorkingDir(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "missing-station-manager")
	t.Setenv(utils.EnvSmWorkingDir, workDir)

	err := runConfigCheck(nil)
	if err == nil {
		t.Fatal("config-check unexpectedly accepted a missing config")
	}
	if _, statErr := os.Stat(workDir); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("config-check created its default working directory; stat error = %v", statErr)
	}
}
