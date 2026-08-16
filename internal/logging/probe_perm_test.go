package logging

// ST-6 — a new log file is created 0600, but OpenFile(0600) does NOT tighten an EXISTING
// looser one (a legacy 0644 smd.log). probeLogFileWritable must chmod it to owner-only, so
// a readable log — and, via lumberjack's mode-copy, its rotations — never persists.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProbeLogFileWritable_TightensExistingFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "smd.log")
	if err := os.WriteFile(p, []byte("legacy readable log"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := probeLogFileWritable(p); err != nil {
		t.Fatalf("probeLogFileWritable: %v", err)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("existing log mode = %04o, want 0600 (tightened)", fi.Mode().Perm())
	}
}

func TestProbeLogFileWritable_CreatesOwnerOnly(t *testing.T) {
	p := filepath.Join(t.TempDir(), "new.log")
	if err := probeLogFileWritable(p); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("new log mode = %04o, want 0600", fi.Mode().Perm())
	}
}
