package api

// ST-6 — the sent-ADIF archive holds full exported QSO records, so its directory is 0700
// and each file 0600 (was 0755/0644). A pre-existing permissive dir is tightened too.

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArchiveSessionAdif_OwnerPrivateModes(t *testing.T) {
	srv := testServer(t)
	wd := srv.cfg.WorkingDir()
	if wd == "" {
		t.Skip("no working dir resolved")
	}
	dir := filepath.Join(wd, sessionAdifArchiveDir)

	// Pre-create the archive dir at a permissive 0755 to prove it is TIGHTENED, not just
	// created private.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	srv.archiveSessionAdif("session.adi", "ADIF BODY")

	if fi, err := os.Lstat(dir); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o700 {
		t.Errorf("archive dir mode = %04o, want 0700 (tightened)", fi.Mode().Perm())
	}

	f := filepath.Join(dir, "session.adi")
	if fi, err := os.Lstat(f); err != nil {
		t.Fatalf("archive file not written: %v", err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("archive file mode = %04o, want 0600", fi.Mode().Perm())
	}
}

// P2 (codex 52b6943a): if the archive dir is a SYMLINK to an external directory, the daemon
// must not chmod through it (mutating an external/shared target) nor write QSO data there —
// it skips the archive.
func TestArchiveSessionAdif_SkipsSymlinkedDir(t *testing.T) {
	srv := testServer(t)
	wd := srv.cfg.WorkingDir()
	if wd == "" {
		t.Skip("no working dir resolved")
	}
	external := t.TempDir() // outside the working dir, deliberately group-accessible
	if err := os.Chmod(external, 0o755); err != nil {
		t.Fatal(err)
	}
	// Make wd/exports/sent-adif a symlink to `external`.
	link := filepath.Join(wd, sessionAdifArchiveDir)
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, link); err != nil {
		t.Fatal(err)
	}

	srv.archiveSessionAdif("s.adi", "body")

	if fi, err := os.Lstat(external); err == nil && fi.Mode().Perm() == 0o700 {
		t.Error("chmod followed the symlink and tightened the external target")
	}
	if _, err := os.Stat(filepath.Join(external, "s.adi")); err == nil {
		t.Error("wrote QSO data through a symlinked archive dir into an external target")
	}
}
