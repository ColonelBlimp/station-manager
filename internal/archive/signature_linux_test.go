//go:build linux

package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Linux only: SignatureOf reads device, inode and ctime from Stat_t here, and
// deliberately reports "unsupported" on other platforms (signature_other.go).

// The signature is (device, inode, ctime); size and mtime ride along for
// diagnostics only. Unchanged reads current; a write reads stale; and — the ruled
// case — a same-size replacement with its mtime put back reads stale, because the
// replacement is a different inode.
func TestSignature_SeesChangeThatSizeAndMtimeCannot(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.db")
	if err := os.WriteFile(path, []byte("aaaaaaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	taken, err := SignatureOf(path)
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	s := ArchiveSummary{Signature: taken}

	now, _ := SignatureOf(path)
	if !s.Current(now) {
		t.Fatal("an untouched file reads stale")
	}

	// Same-size replacement, mtime restored: the file an operator restores from a
	// backup with its timestamps preserved.
	old, _ := os.Stat(path)
	repl := filepath.Join(dir, "replacement.db")
	if err := os.WriteFile(repl, []byte("bbbbbbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(repl, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(repl, path); err != nil {
		t.Fatal(err)
	}
	after, _ := SignatureOf(path)
	if after.Size != taken.Size || after.MtimeNs != taken.MtimeNs {
		t.Fatalf("fixture: size/mtime differ (%+v vs %+v) — the case needs them equal", after, taken)
	}
	if s.Current(after) {
		t.Fatal("a same-size replacement with a preserved mtime reads current")
	}
}

// In place, the inode stays; ctime is what moves. Ctime comes from the kernel's
// coarse clock, so wait (by observing ctime itself, not a fixed sleep) until a
// fresh metadata change would land on a later tick.
func TestSignature_InPlaceRewriteWithRestoredMtimeReadsStale(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "archive.db")
	if err := os.WriteFile(path, []byte("aaaaaaaa"), 0o600); err != nil {
		t.Fatal(err)
	}
	taken, err := SignatureOf(path)
	if err != nil {
		t.Fatalf("signature: %v", err)
	}
	probe := filepath.Join(dir, "probe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(2 * time.Second); ; {
		_ = os.Chmod(probe, 0o600)
		p, _ := SignatureOf(probe)
		if p.CtimeNs > taken.CtimeNs {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ctime never advanced")
		}
	}
	old, _ := os.Stat(path)
	if err := os.WriteFile(path, []byte("bbbbbbbb"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, old.ModTime(), old.ModTime()); err != nil {
		t.Fatal(err)
	}
	after, _ := SignatureOf(path)
	if after.Ino != taken.Ino || after.Size != taken.Size || after.MtimeNs != taken.MtimeNs {
		t.Fatalf("fixture: expected same inode, size and mtime (%+v vs %+v)", after, taken)
	}
	if (ArchiveSummary{Signature: taken}).Current(after) {
		t.Fatal("an in-place rewrite with a restored mtime reads current")
	}
}
