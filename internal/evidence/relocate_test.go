package evidence

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func makeArchive(t *testing.T, path string, rows int) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE obs (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < rows; i++ {
		if _, err := db.Exec(`INSERT INTO obs VALUES (?)`, i); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
}

func archiveRows(t *testing.T, path string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM obs`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRelocateArchive_MovesFromLegacyPreservingData(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		t.Fatal(err)
	}
	oldPath := filepath.Join(root, "evidence.db")
	newPath := filepath.Join(dbDir, "evidence.db")
	makeArchive(t, oldPath, 7)

	if got := RelocateArchive(oldPath, newPath, nil); got != newPath {
		t.Fatalf("path = %q, want the db/ path %q", got, newPath)
	}
	if n := archiveRows(t, newPath); n != 7 {
		t.Errorf("relocated archive has %d rows, want 7 — fold + move must preserve committed data", n)
	}
	for _, suf := range []string{"", "-wal", "-shm"} {
		if _, err := os.Stat(oldPath + suf); !os.IsNotExist(err) {
			t.Errorf("legacy %q still present after move", "evidence.db"+suf)
		}
	}
	if fi, err := os.Stat(newPath); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 (owner-only)", fi.Mode().Perm())
	}
}

func TestRelocateArchive_AlreadyMigratedIsNoop(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	_ = os.MkdirAll(dbDir, 0o700)
	oldPath := filepath.Join(root, "evidence.db")
	newPath := filepath.Join(dbDir, "evidence.db")
	makeArchive(t, newPath, 3)
	makeArchive(t, oldPath, 99) // a stray legacy archive must be left alone, not touched

	if got := RelocateArchive(oldPath, newPath, nil); got != newPath {
		t.Fatalf("path = %q, want %q", got, newPath)
	}
	if n := archiveRows(t, newPath); n != 3 {
		t.Errorf("live archive rows = %d, want it untouched (3)", n)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("stray legacy archive should be left in place, not deleted: %v", err)
	}
}

func TestRelocateArchive_FreshInstallReturnsNewPath(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	_ = os.MkdirAll(dbDir, 0o700)
	newPath := filepath.Join(dbDir, "evidence.db")
	if got := RelocateArchive(filepath.Join(root, "evidence.db"), newPath, nil); got != newPath {
		t.Fatalf("path = %q, want %q", got, newPath)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("a fresh install must create nothing here")
	}
}

func TestRelocateArchive_MoveFailureKeepsLegacyPath(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "evidence.db")
	makeArchive(t, oldPath, 5)
	// The destination directory does not exist → the single-file rename fails; the
	// archive must be left where it is and the legacy path returned.
	newPath := filepath.Join(root, "nonexistent-dir", "evidence.db")
	if got := RelocateArchive(oldPath, newPath, nil); got != oldPath {
		t.Fatalf("path = %q, want the legacy path %q on move failure", got, oldPath)
	}
	if n := archiveRows(t, oldPath); n != 5 {
		t.Errorf("legacy archive rows = %d, want it intact (5) after a failed move", n)
	}
}

// If the WAL cannot be folded, the archive must NOT be moved — a single-file move
// would drop any unfolded records. A non-database file at the path stands in for an
// unfoldable archive (the checkpoint fails).
func TestRelocateArchive_UnfoldableArchiveKeepsLegacy(t *testing.T) {
	root := t.TempDir()
	dbDir := filepath.Join(root, "db")
	_ = os.MkdirAll(dbDir, 0o700)
	oldPath := filepath.Join(root, "evidence.db")
	newPath := filepath.Join(dbDir, "evidence.db")
	if err := os.WriteFile(oldPath, []byte("this is not a sqlite database"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RelocateArchive(oldPath, newPath, nil); got != oldPath {
		t.Fatalf("path = %q, want the legacy path %q when the WAL can't be folded", got, oldPath)
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Errorf("archive must stay at legacy when it can't be safely folded: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Errorf("nothing should be moved to the new path when the fold fails")
	}
}
