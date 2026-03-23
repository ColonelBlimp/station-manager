package utils

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathExists(t *testing.T) {
	dir := t.TempDir()
	exists, err := PathExists(dir)
	if err != nil || !exists {
		t.Fatalf("expected existing dir, got exists=%v err=%v", exists, err)
	}

	non := filepath.Join(dir, "does-not-exist")
	exists, err = PathExists(non)
	if err != nil || exists {
		t.Fatalf("expected non-existing path, got exists=%v err=%v", exists, err)
	}

	// Permission error scenario (best-effort): create a file and remove read perms may still Stat
	f := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(f, []byte("x"), 0o000); err != nil {
		t.Fatalf("setup file: %v", err)
	}
	// Stat generally works even without read permission; so just ensure it doesn't error fatally
	_, _ = PathExists(f)
}
