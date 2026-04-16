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

func TestWorkingDir_CreatesDir(t *testing.T) {
	// WorkingDir should auto-create a missing directory (covers first-run after package install).
	dir := filepath.Join(t.TempDir(), "newdir")
	got, err := WorkingDir(dir)
	if err != nil {
		t.Fatalf("WorkingDir(newdir) error: %v", err)
	}
	if got != dir {
		t.Fatalf("WorkingDir(newdir) = %q; want %q", got, dir)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("expected directory to be created at %q", dir)
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

func TestIsSystemPath(t *testing.T) {
	tests := []struct {
		dir  string
		want bool
	}{
		{"/usr/bin", true},
		{"/usr/local/bin", true},
		{"/usr/sbin", true},
		{"/usr/bin/extra", true},
		{"/home/user/bin", false},
		{"/tmp", false},
		{"/usr/share", false},
	}
	for _, tt := range tests {
		if got := isSystemPath(tt.dir); got != tt.want {
			t.Msgf("isSystemPath(%q) = %v; want %v", tt.dir, got, tt.want)
		}
	}
}

func TestXdgDataDir_Default(t *testing.T) {
	os.Unsetenv("XDG_DATA_HOME")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", xdgAppName)
	got := xdgDataDir()
	if got != want {
		t.Fatalf("xdgDataDir() = %q; want %q", got, want)
	}
}

func TestXdgDataDir_Custom(t *testing.T) {
	custom := t.TempDir()
	os.Setenv("XDG_DATA_HOME", custom)
	t.Cleanup(func() { os.Unsetenv("XDG_DATA_HOME") })
	want := filepath.Join(custom, xdgAppName)
	got := xdgDataDir()
	if got != want {
		t.Fatalf("xdgDataDir() = %q; want %q", got, want)
	}
}
