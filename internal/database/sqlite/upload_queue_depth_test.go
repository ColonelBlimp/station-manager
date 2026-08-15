package sqlite

// L11-C3 (store half) — the periodic queue summary needs a point-in-time depth snapshot per
// forwarder: how many rows are pending, when the oldest pending row was enqueued, and the
// durable count of rows that have given up (status=failed). These are what let an operator
// tell a growing backlog from a draining one, and a genuinely-empty queue from a summary that
// did not run. Ruling 2026-08-15: "totals" is a durable DB count of failed rows, not a
// process-lifetime counter, so it survives a restart.

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestUploadQueueDepth_CountsScopeAndOldest(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	ctx := context.Background()

	// Three qrz uploads (distinct QSOs, so no (qso_id,forwarder,action) collision) and one
	// clublog upload that must NOT leak into qrz's counts.
	var qrzQsos []int64
	for i, call := range []string{"M0AAA", "M0BBB", "M0CCC"} {
		q := validTestQso(lbID, call, "40m", "SSB", "20250508", "084"+string(rune('0'+i)))
		id, err := svc.InsertQso(q)
		if err != nil {
			t.Fatalf("insert qso %d: %v", i, err)
		}
		enqueueUpload(t, svc, id, "qrz", "qrz", action.Insert)
		qrzQsos = append(qrzQsos, id)
	}
	other := validTestQso(lbID, "M0DDD", "20m", "SSB", "20250508", "0900")
	otherID, _ := svc.InsertQso(other)
	enqueueUpload(t, svc, otherID, "clublog", "clublog", action.Insert)

	// Make the third qrz row FAILED, so qrz has 2 pending + 1 failed. Set the status
	// directly: MarkUploadFailed is conditional on in_progress (a pending row re-arms), and
	// this is a store-query fixture, not a lifecycle test.
	rawExec(t, svc, `UPDATE qso_upload SET status = 'failed' WHERE qso_id = ? AND forwarder_name = 'qrz'`, qrzQsos[2])

	// Back-date the FIRST qrz row so "oldest" is unambiguous — an implementation returning
	// the newest, or the first-inserted regardless of time, fails here.
	first, err := svc.FetchUploadsByQsoIDWithContext(ctx, qrzQsos[0])
	if err != nil || len(first) != 1 {
		t.Fatalf("fetch first qrz upload: %v (rows=%d)", err, len(first))
	}
	rawExec(t, svc, `UPDATE qso_upload SET created_at = '2020-01-01 00:00:00' WHERE id = ?`, first[0].ID)

	got, err := svc.UploadQueueDepthWithContext(ctx, "qrz")
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	if got.Pending != 2 {
		t.Errorf("Pending = %d, want 2 (clublog must not leak; failed must not count)", got.Pending)
	}
	if got.Failed != 1 {
		t.Errorf("Failed = %d, want 1", got.Failed)
	}
	if got.OldestQueued.Year() != 2020 {
		t.Errorf("OldestQueued = %v, want the back-dated 2020 row (MIN of pending created_at)", got.OldestQueued)
	}
}

// C3: re-arm resets the backlog age. A row uploaded long ago and re-enqueued (operator edit,
// reconcile, stamp-sync — all UPSERT the same (qso,forwarder,action) triple) is NEW pending
// work, not a months-old backlog. OldestQueued must reflect the re-arm time, not the original
// created_at — otherwise the oldest-age signal is inflated after normal repeated edits.
func TestUploadQueueDepth_ReArmResetsOldestAge(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	ctx := context.Background()

	q := validTestQso(lbID, "M0AAA", "40m", "SSB", "20250508", "0845")
	qsoID, err := svc.InsertQso(q)
	if err != nil {
		t.Fatalf("insert qso: %v", err)
	}
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)
	// Age the row as if it were enqueued back in 2020.
	rawExec(t, svc, `UPDATE qso_upload SET created_at = '2020-01-01 00:00:00' WHERE qso_id = ? AND forwarder_name = 'qrz'`, qsoID)

	// Re-enqueue the SAME triple — the UPSERT re-arms the row in place.
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	got, err := svc.UploadQueueDepthWithContext(ctx, "qrz")
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	if got.OldestQueued.Year() == 2020 {
		t.Errorf("OldestQueued = %v, want the re-arm time — a re-enqueued row is new work, not a 2020 backlog", got.OldestQueued)
	}
}

// C3: an empty queue reports zero depth and a ZERO OldestQueued — the caller uses the zero
// value to know there is no oldest row, and must not read a bogus timestamp.
func TestUploadQueueDepth_EmptyQueueHasZeroOldest(t *testing.T) {
	svc := testService(t)
	got, err := svc.UploadQueueDepthWithContext(context.Background(), "qrz")
	if err != nil {
		t.Fatalf("queue depth on empty table: %v", err)
	}
	if got.Pending != 0 || got.Failed != 0 {
		t.Errorf("Pending/Failed = %d/%d, want 0/0", got.Pending, got.Failed)
	}
	if !got.OldestQueued.IsZero() {
		t.Errorf("OldestQueued = %v, want zero for an empty queue", got.OldestQueued)
	}
}
