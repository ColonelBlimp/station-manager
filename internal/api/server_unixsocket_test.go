package api

import (
	"os"
	"path/filepath"
	"testing"
)

// A Unix-socket startup must NOT delete a non-socket file that socket_path
// happens to point at (review 2026-06-19 H1): a typo aimed at config.json, an
// ADIF export, or any user file would otherwise be unlinked before bind. The
// daemon must refuse to start and leave the file untouched.
func TestListenAndServe_UnixRefusesNonSocketPath(t *testing.T) {
	srv := testServer(t)
	srv.protocol = "unix"

	path := filepath.Join(t.TempDir(), "important.json")
	const content = `{"secret":"do-not-delete"}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}

	err := srv.ListenAndServe(path)
	if err == nil {
		t.Fatal("ListenAndServe should refuse to bind over a non-socket file")
	}

	// The file must still be present and unchanged.
	got, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("file was removed or unreadable after refused startup: %v", readErr)
	}
	if string(got) != content {
		t.Errorf("file content changed: got %q, want %q", got, content)
	}
}
