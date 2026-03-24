package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkingDir_WithArg(t *testing.T) {
	dir := t.TempDir()
	got, err := WorkingDir(dir)
	if err != nil || got != dir {
		t.Fatalf("WorkingDir(dir) = %q, %v", got, err)
	}
}

func TestWorkingDir_WithEnv(t *testing.T) {
	dir := t.TempDir()
	os.Setenv(EnvSmWorkingDir, dir)
	t.Cleanup(func() { os.Unsetenv(EnvSmWorkingDir) })
	got, err := WorkingDir()
	if err != nil || got != dir {
		t.Fatalf("WorkingDir() env = %q, %v", got, err)
	}
}

func TestWorkingDir_ErrorForMissing(t *testing.T) {
	non := filepath.Join(t.TempDir(), "missing")
	_, err := WorkingDir(non)
	if err == nil {
		t.Fatal("expected error for non-existing working directory")
	}
}

func TestWorkingDir_Default(t *testing.T) {
	// Ensure no env var is set so the fallback uses the executable directory.
	os.Unsetenv(EnvSmWorkingDir)
	got, err := WorkingDir()
	if err != nil {
		t.Fatalf("WorkingDir() default = error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("WorkingDir() default = %q; expected absolute path", got)
	}
}
