package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/failure"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/status"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 9, ruling (a): the operator's "Retry failed" re-arms EVERY
// failed row of the named forwarder, whatever its failure class — an explicit
// action, unlike the class-scoped boot recovery. Rows in any other status, and
// other forwarders' rows, are untouched.
func TestRearmFailedUploadsForForwarder(t *testing.T) {
	svc := testService(t)
	ctx := context.Background()
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})

	seedFailed := func(call, fwd string, class failure.Class) int64 {
		t.Helper()
		qsoID, _ := svc.InsertQso(validTestQso(lbID, call, "40m", "SSB", "20250508", "0845"))
		enqueueUpload(t, svc, qsoID, fwd, "qrz", action.Insert)
		claimOneAndMark(t, svc, fwd, func(id int64) error {
			return svc.MarkUploadFailedWithContext(ctx, id, "reason", class)
		})
		return qsoID
	}
	qAuth := seedFailed("M0CMC", "qrz", failure.Auth)
	qData := seedFailed("EA1B", "qrz", "") // unclassified — the 2026-08-06 fixture shape
	qOther := seedFailed("F5ABC", "clublog", failure.Auth)

	// qrz rows in the non-failed states must survive a retry unchanged.
	qUploaded := seedUploadInState(t, svc, lbID, "qrz", "G3XYZ", "0900", status.Uploaded)
	qInFlight := seedUploadInState(t, svc, lbID, "qrz", "G4AAA", "0905", status.InProgress)

	n, err := svc.RearmFailedUploadsForForwarderWithContext(ctx, "qrz")
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	if n != 2 {
		t.Fatalf("rearmed = %d, want 2 (both of qrz's failed rows, any class)", n)
	}

	get := func(qsoID int64) types.QsoUpload {
		t.Helper()
		ups, err := svc.FetchUploadsByQsoIDWithContext(ctx, qsoID)
		if err != nil || len(ups) != 1 {
			t.Fatalf("fetch uploads for %d: %v (%d rows)", qsoID, err, len(ups))
		}
		return ups[0]
	}
	for name, id := range map[string]int64{"auth": qAuth, "unclassified": qData} {
		row := get(id)
		if row.Status != "pending" || row.FailureClass != "" || row.LastError != "" || row.Attempts != 0 {
			t.Errorf("%s row after rearm = status %q class %q last_error %q attempts %d; want pending, cleared, 0",
				name, row.Status, row.FailureClass, row.LastError, row.Attempts)
		}
		if row.NextAttemptAt > time.Now().Unix() {
			t.Errorf("%s row next_attempt_at = %d is in the future; a re-armed row must be claimable now", name, row.NextAttemptAt)
		}
	}
	if row := get(qOther); row.Status != "failed" || row.FailureClass != "auth" {
		t.Errorf("other forwarder's row = status %q class %q; must be untouched", row.Status, row.FailureClass)
	}
	if row := get(qUploaded); row.Status != "uploaded" {
		t.Errorf("uploaded row status = %q after rearm, want uploaded (history is never re-sent)", row.Status)
	}
	if row := get(qInFlight); row.Status != "in_progress" {
		t.Errorf("in-flight row status = %q after rearm, want in_progress (the claimed batch is left alone)", row.Status)
	}

	// Both re-armed rows are claimable by the ordinary worker path.
	claimed, err := svc.ClaimPendingUploadsWithContext(ctx, "qrz", 10)
	if err != nil {
		t.Fatalf("claim after rearm: %v", err)
	}
	if len(claimed) != 2 {
		t.Fatalf("claimed %d rows after rearm, want the 2 re-armed rows", len(claimed))
	}
}

func TestRearmFailedUploadsForForwarder_NothingToRearm_ReturnsZero(t *testing.T) {
	svc := testService(t)
	n, err := svc.RearmFailedUploadsForForwarderWithContext(context.Background(), "qrz")
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	if n != 0 {
		t.Fatalf("rearmed = %d, want 0", n)
	}
	if _, err := svc.RearmFailedUploadsForForwarderWithContext(context.Background(), " "); err == nil {
		t.Fatal("blank forwarder name accepted; want an error")
	}
}
