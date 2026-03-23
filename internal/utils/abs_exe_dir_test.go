package utils

import (
	"path/filepath"
	"testing"
)

func TestAbsDirPathForExecutable(t *testing.T) {
	dir, err := AbsDirPathForExecutable()
	if err != nil {
		t.Fatalf("AbsDirPathForExecutable error: %v", err)
	}
	if filepath.IsAbs(dir) == false {
		t.Fatalf("expected absolute path, got %q", dir)
	}
}
