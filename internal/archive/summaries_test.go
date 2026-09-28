package archive

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// ADR 0084 slice 1: the archive summaries live in a discardable, versioned,
// UUID-keyed sidecar. These pin what "discardable" means — a missing, corrupt or
// unsupported file is a cache miss with a reason, never an error — and what the
// change signature can and cannot see.

func sampleSummaries() Summaries {
	return Summaries{
		"0199aaaa-0000-7000-8000-000000000001": {
			Logbooks: []LogbookSummary{
				{UUID: "lb-1", Name: "Default", Callsign: "7Q5MLV", QSOs: 7468},
				{UUID: "lb-2", Name: "Contest", Callsign: "7Q5MLV", QSOs: 1},
			},
			Signature: FileSignature{Dev: 1, Ino: 2, CtimeNs: 3, Size: 4, MtimeNs: 5},
			TakenAt:   time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC),
		},
	}
}

func TestSummaries_RoundTripIsOwnerOnlyAndAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SummariesFileName)
	want := sampleSummaries()
	if err := WriteSummaries(path, want); err != nil {
		t.Fatalf("write: %v", err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("dir holds %d entries after the write, want only the file (no temp left)", len(entries))
	}
	got, miss := ReadSummaries(path)
	if miss != "" {
		t.Fatalf("read: miss %q", miss)
	}
	id := "0199aaaa-0000-7000-8000-000000000001"
	if len(got) != 1 || len(got[id].Logbooks) != 2 || got[id].Logbooks[0].QSOs != 7468 ||
		got[id].Signature != want[id].Signature || !got[id].TakenAt.Equal(want[id].TakenAt) {
		t.Fatalf("round trip = %+v, want %+v", got, want)
	}
}

func TestSummaries_EveryUnreadableFileIsACacheMissWithAReason(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, body, wantMiss string
	}{
		{"missing", "", MissMissing},
		{"corrupt", "{not json", MissCorrupt},
		{"unsupported format", `{"format": 99, "archives": {}}`, MissUnsupported},
		{"no format", `{"archives": {}}`, MissUnsupported},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(dir, tc.name+".json")
			if tc.body != "" {
				if err := os.WriteFile(path, []byte(tc.body), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			got, miss := ReadSummaries(path)
			if miss != tc.wantMiss {
				t.Fatalf("miss = %q, want %q", miss, tc.wantMiss)
			}
			if got == nil || len(got) != 0 {
				t.Fatalf("a miss must be an EMPTY, usable set, got %#v", got)
			}
		})
	}
}

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

func TestSummariesPath_IsTheStationGlobalFileNeverBesideAnArchive(t *testing.T) {
	cfg := config.Config{DataDir: "/data"}
	if got, want := SummariesPath(cfg), filepath.Join("/data", "db", "qso-archive-summaries.json"); got != want {
		t.Fatalf("SummariesPath = %q, want %q", got, want)
	}
}

// Each part of the comparison, alone. The filesystem tests above cannot isolate
// the inode — the kernel's coarse ctime makes an equal-ctime replacement
// unconstructible on demand — so the rule itself is pinned here: device, inode or
// ctime differing reads stale; size or mtime differing alone does not (they are
// diagnostics, and under WAL the main file's mtime lags the data).
func TestCurrent_ComparesDeviceInodeAndCtimeOnly(t *testing.T) {
	base := FileSignature{Dev: 1, Ino: 2, CtimeNs: 3, Size: 4, MtimeNs: 5}
	s := ArchiveSummary{Signature: base}
	for _, tc := range []struct {
		name  string
		now   FileSignature
		fresh bool
	}{
		{"identical", base, true},
		{"other device", FileSignature{Dev: 9, Ino: 2, CtimeNs: 3, Size: 4, MtimeNs: 5}, false},
		{"other inode", FileSignature{Dev: 1, Ino: 9, CtimeNs: 3, Size: 4, MtimeNs: 5}, false},
		{"other ctime", FileSignature{Dev: 1, Ino: 2, CtimeNs: 9, Size: 4, MtimeNs: 5}, false},
		{"other size only", FileSignature{Dev: 1, Ino: 2, CtimeNs: 3, Size: 9, MtimeNs: 5}, true},
		{"other mtime only", FileSignature{Dev: 1, Ino: 2, CtimeNs: 3, Size: 4, MtimeNs: 9}, true},
	} {
		if got := s.Current(tc.now); got != tc.fresh {
			t.Errorf("%s: Current = %v, want %v", tc.name, got, tc.fresh)
		}
	}
}

// Durability: the rename must reach the disk, so the directory is synced AFTER
// the new file is in place (the same barrier archive creation uses).
func TestWriteSummaries_SyncsTheDirectoryAfterTheRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, SummariesFileName)
	var synced []string
	orig := syncDir
	syncDir = func(p string) error {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("directory synced before the file was in place: %v", err)
		}
		synced = append(synced, p)
		return nil
	}
	t.Cleanup(func() { syncDir = orig })
	if err := WriteSummaries(path, sampleSummaries()); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("synced %v, want exactly the sidecar's directory", synced)
	}
}
