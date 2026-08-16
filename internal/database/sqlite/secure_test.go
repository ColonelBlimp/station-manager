package sqlite

// ST-6 — the log + reference databases (and their WAL/SHM sidecars + directory) must be
// owner-private when application-owned; an operator-supplied path outside the working dir is
// warned about, never mutated; in-memory paths are skipped. Assert EFFECTIVE modes.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestSecureDataFiles_TightensAppOwned(t *testing.T) {
	work := t.TempDir()
	dir := filepath.Join(work, "db")
	if err := os.MkdirAll(dir, 0o755); err != nil { // permissive dir
		t.Fatal(err)
	}
	logdb := filepath.Join(dir, "log.db")
	for _, f := range []string{logdb, logdb + "-wal"} { // no -shm: missing sidecar is a no-op
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	log := logging.NewForWriter(&bytes.Buffer{})
	if err := SecureDataFiles(work, log, logdb); err != nil {
		t.Fatalf("SecureDataFiles (app-owned): %v", err)
	}
	if mode(t, dir) != 0o700 {
		t.Errorf("db dir mode = %04o, want 0700", mode(t, dir))
	}
	if mode(t, logdb) != 0o600 {
		t.Errorf("db mode = %04o, want 0600", mode(t, logdb))
	}
	if mode(t, logdb+"-wal") != 0o600 {
		t.Errorf("db-wal mode = %04o, want 0600", mode(t, logdb+"-wal"))
	}
}

func TestSecureDataFiles_ExternalWarnsNotFatal(t *testing.T) {
	work := t.TempDir()
	ext := filepath.Join(t.TempDir(), "log.db") // a different tree — operator-supplied
	if err := os.WriteFile(ext, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	buf := &bytes.Buffer{}
	log := logging.NewForWriter(buf)

	if err := SecureDataFiles(work, log, ext); err != nil {
		t.Fatalf("external group-accessible DB must warn, not fail: %v", err)
	}
	if mode(t, ext) != 0o644 {
		t.Errorf("external DB was mutated to %04o; must stay 0644", mode(t, ext))
	}
	if !strings.Contains(buf.String(), "ST-6") {
		t.Errorf("no ST-6 warning for a group-accessible external DB; log = %s", buf.String())
	}
}

func TestSecureDataFiles_InMemoryAndEmptySkipped(t *testing.T) {
	log := logging.NewForWriter(&bytes.Buffer{})
	if err := SecureDataFiles(t.TempDir(), log, "", ":memory:", "file::memory:?cache=shared"); err != nil {
		t.Fatalf("in-memory/empty paths must be skipped: %v", err)
	}
}
