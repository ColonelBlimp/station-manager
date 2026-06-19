package utils

import (
	"path/filepath"
	"testing"
)

// XDGDataDir had 0% direct coverage (review 2026-06-19 L2). Cover the
// XDG_DATA_HOME-set and HOME-fallback branches with isolated env.
func TestXDGDataDir(t *testing.T) {
	// XDG_DATA_HOME set → used directly.
	t.Setenv("XDG_DATA_HOME", "/custom/data")
	if got, want := XDGDataDir(), filepath.Join("/custom/data", xdgAppName); got != want {
		t.Errorf("XDGDataDir() = %q; want %q", got, want)
	}

	// XDG_DATA_HOME unset → ~/.local/share/<app>.
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("HOME", "/home/tester")
	if got, want := XDGDataDir(), filepath.Join("/home/tester", ".local", "share", xdgAppName); got != want {
		t.Errorf("XDGDataDir() with HOME fallback = %q; want %q", got, want)
	}
}
