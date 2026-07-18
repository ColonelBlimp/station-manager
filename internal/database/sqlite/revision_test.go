package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// ADR 0050 characterization: the qso.revision counter is the sync protocol's
// version marker. The stamp trigger owns the bump (INSERT leaves 0, every
// trigger-stamped UPDATE increments), the manifest carries it, and restore
// preserves it so a restored row resumes its edit sequence.

func fetchRevision(t *testing.T, svc *Service, id int64) int64 {
	t.Helper()
	q, err := svc.FetchQsoById(id)
	if err != nil {
		t.Fatalf("fetch qso %d: %v", id, err)
	}
	return q.Revision
}

func TestQsoRevision_TriggerBumps(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	logbookID, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Test", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	q := validTestQso(logbookID, "M0CMC", "20m", "SSB", "20240615", "1253")
	id, err := svc.InsertQsoWithContext(ctx, q)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	if got := fetchRevision(t, svc, id); got != 0 {
		t.Fatalf("fresh row revision = %d, want 0", got)
	}

	// Each edit bumps by exactly one — including two edits within the same
	// wall-clock second, which is the whole point: modified_at cannot tell
	// them apart, revision can.
	for want := int64(1); want <= 2; want++ {
		edit, err := svc.FetchQsoByIdWithContext(ctx, id)
		if err != nil {
			t.Fatalf("fetch for edit: %v", err)
		}
		edit.ContactedStation.Name = "Edit " + time.Now().Format("150405.000")
		if err := svc.UpdateQsoWithContext(ctx, edit); err != nil {
			t.Fatalf("update %d: %v", want, err)
		}
		if got := fetchRevision(t, svc, id); got != want {
			t.Fatalf("revision after edit %d = %d, want %d", want, got, want)
		}
	}

	// The manifest — the reconcile diff basis — carries the same value.
	manifest, err := svc.FetchQsoManifestWithContext(ctx, logbookID)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	if len(manifest) != 1 || manifest[0].Revision != 2 {
		t.Fatalf("manifest = %+v, want one entry with revision 2", manifest)
	}
}

func TestQsoRevision_RestorePreservesAndResumes(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	logbookID, err := svc.InsertLogbookWithContext(ctx, types.Logbook{Name: "Test", Callsign: "G4ABC"})
	if err != nil {
		t.Fatalf("insert logbook: %v", err)
	}

	q := validTestQso(logbookID, "DL9UW", "40m", "CW", "20260101", "1200")
	q.UUID = utils.NewUUIDv7()
	q.ModifiedAt = time.Date(2026, 1, 1, 12, 0, 5, 0, time.UTC)
	q.Revision = 7 // the cloud-exported edit sequence position
	id, err := svc.InsertRestoredQsoWithContext(ctx, q)
	if err != nil {
		t.Fatalf("insert restored qso: %v", err)
	}
	if got := fetchRevision(t, svc, id); got != 7 {
		t.Fatalf("restored revision = %d, want 7 (preserved, not reset)", got)
	}

	// A post-restore edit RESUMES the sequence — restarting at 1 would push
	// as "stale" against a cloud already holding revision 7.
	edit, err := svc.FetchQsoByIdWithContext(ctx, id)
	if err != nil {
		t.Fatalf("fetch restored: %v", err)
	}
	edit.ContactedStation.Name = "Uwe"
	if err := svc.UpdateQsoWithContext(ctx, edit); err != nil {
		t.Fatalf("update restored: %v", err)
	}
	if got := fetchRevision(t, svc, id); got != 8 {
		t.Fatalf("revision after post-restore edit = %d, want 8", got)
	}
}
