package sqlite

import (
	"os"
	"path/filepath"
	"testing"
)

// The db/ directory holds the operator's private databases (log, reference,
// evidence) whose WAL/SHM sidecars are created at the umask, so the DIRECTORY mode
// is the access boundary. SM secures the directory it CREATES by making it 0700; a
// pre-existing/operator-configured directory is deliberately left alone (it could
// be a shared or system path — codex 8c226f51). This pins the create-side guarantee.
func TestCheckDatabaseDir_CreatesOwnerOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "db") // does not exist yet
	if err := (&Service{}).checkDatabaseDir(filepath.Join(dir, "station-manager.db")); err != nil {
		t.Fatalf("checkDatabaseDir: %v", err)
	}
	fi, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o700 {
		t.Errorf("created db dir mode = %v, want 0700 (owner-only)", fi.Mode().Perm())
	}
}
