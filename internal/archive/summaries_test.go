package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0084 slice 1: the archive summaries live in a discardable, versioned,
// UUID-keyed sidecar. These pin what "discardable" means — a missing, corrupt or
// unsupported file is a cache miss with a reason, never an error — and what the
// change signature can and cannot see.

func sampleSummaries() Summaries {
	return Summaries{
		"0199aaaa-0000-7000-8000-000000000001": {
			Logbooks: []LogbookSummary{
				{UUID: "lb-1", Name: "Default", Callsign: "7Q5MLV", QSOCount: 7468},
				{UUID: "lb-2", Name: "Contest", Callsign: "7Q5MLV", QSOCount: 1},
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
	if len(got) != 1 || len(got[id].Logbooks) != 2 || got[id].Logbooks[0].QSOCount != 7468 ||
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

// ---- slice 2a: building a summary and merging it into the sidecar ----

type fakeSummarySource struct {
	logbooks []types.Logbook
	counts   map[int64]int64
	err      error
}

func (f fakeSummarySource) FetchAllLogbooksWithContext(context.Context) ([]types.Logbook, error) {
	return f.logbooks, f.err
}

func (f fakeSummarySource) FetchQsoCountByLogbookIdWithContext(_ context.Context, id int64, _ string, _ bool) (int64, error) {
	return f.counts[id], nil
}

func TestBuildLogbookSummaries_OneEntryPerLogbookWithItsCount(t *testing.T) {
	src := fakeSummarySource{
		logbooks: []types.Logbook{
			{ID: 1, UUID: "lb-1", Name: "Default", Callsign: "7Q5MLV"},
			{ID: 2, UUID: "lb-2", Name: "Contest", Callsign: "7Q5MLV"},
		},
		counts: map[int64]int64{1: 7468, 2: 1},
	}
	got, err := BuildLogbookSummaries(context.Background(), src)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	want := []LogbookSummary{
		{UUID: "lb-1", Name: "Default", Callsign: "7Q5MLV", QSOCount: 7468},
		{UUID: "lb-2", Name: "Contest", Callsign: "7Q5MLV", QSOCount: 1},
	}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("summaries = %+v, want %+v", got, want)
	}

	none, err := BuildLogbookSummaries(context.Background(), fakeSummarySource{})
	if err != nil || none == nil || len(none) != 0 {
		t.Fatalf("no logbooks = %#v (%v); want an empty, non-nil list", none, err)
	}
	if _, err := BuildLogbookSummaries(context.Background(), fakeSummarySource{err: errors.New("boom")}); err == nil {
		t.Fatal("a failed logbook read built a summary")
	}
}

func TestMergeSummary_KeepsTheOtherArchives(t *testing.T) {
	path := filepath.Join(t.TempDir(), SummariesFileName)
	if err := WriteSummaries(path, sampleSummaries()); err != nil {
		t.Fatal(err)
	}
	if err := MergeSummary(path, "other", ArchiveSummary{Logbooks: []LogbookSummary{{Name: "Drill", QSOCount: 3}}}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	got, miss := ReadSummaries(path)
	if miss != "" || len(got) != 2 || got["other"].Logbooks[0].Name != "Drill" ||
		got["0199aaaa-0000-7000-8000-000000000001"].Logbooks[0].QSOCount != 7468 {
		t.Fatalf("after merge: %+v (miss %q)", got, miss)
	}
}

// Derived state: an unreadable sidecar is replaced, not a reason to fail.
func TestMergeSummary_ReplacesAnUnreadableFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), SummariesFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := MergeSummary(path, "a", ArchiveSummary{}); err != nil {
		t.Fatalf("merge over a corrupt file: %v", err)
	}
	if got, miss := ReadSummaries(path); miss != "" || len(got) != 1 {
		t.Fatalf("after merge: %+v (miss %q)", got, miss)
	}
}

// Merges are read-modify-write; serialised, concurrent ones lose nothing.
func TestMergeSummary_ConcurrentMergesLoseNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), SummariesFileName)
	const n = 24
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := MergeSummary(path, fmt.Sprintf("archive-%02d", i), ArchiveSummary{}); err != nil {
				t.Errorf("merge %d: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
	if got, _ := ReadSummaries(path); len(got) != n {
		t.Fatalf("%d archives after %d concurrent merges; entries were lost", len(got), n)
	}
}
