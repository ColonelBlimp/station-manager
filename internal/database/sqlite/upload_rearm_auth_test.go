package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/failure"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 9, ruling (b): at worker start each enabled forwarder's
// failed rows of class `auth` return to pending — one attempt per daemon
// restart with whatever credential the restart loaded. Rows failed for any
// other reason, and other forwarders' rows, are untouched.
func TestRearmAuthFailedUploadsForForwarder(t *testing.T) {
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
	qData := seedFailed("EA1B", "qrz", "")
	qOther := seedFailed("F5ABC", "clublog", failure.Auth)

	// The fixture shape (ruling (d)): a pre-0011 failed row, class NULL, whose
	// last_error happens to read like an auth failure — must NOT be re-armed.
	qLegacy, _ := svc.InsertQso(validTestQso(lbID, "G3XYZ", "20m", "SSB", "20250508", "0900"))
	enqueueUpload(t, svc, qLegacy, "qrz", "qrz", action.Insert)
	claimOneAndMark(t, svc, "qrz", func(id int64) error {
		return svc.MarkUploadFailedWithContext(ctx, id, "QRZ authentication rejected: invalid api key [REDACTED]", "")
	})

	n, err := svc.RearmAuthFailedUploadsForForwarderWithContext(ctx, "qrz")
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	if n != 1 {
		t.Fatalf("rearmed = %d, want 1 (only qrz's auth-class row)", n)
	}

	get := func(qsoID int64) types.QsoUpload {
		t.Helper()
		ups, err := svc.FetchUploadsByQsoIDWithContext(ctx, qsoID)
		if err != nil || len(ups) != 1 {
			t.Fatalf("fetch uploads for %d: %v (%d rows)", qsoID, err, len(ups))
		}
		return ups[0]
	}
	auth := get(qAuth)
	if auth.Status != "pending" || auth.FailureClass != "" || auth.LastError != "" || auth.Attempts != 0 {
		t.Fatalf("auth row after rearm = status %q class %q last_error %q attempts %d; want pending, cleared, 0",
			auth.Status, auth.FailureClass, auth.LastError, auth.Attempts)
	}
	if auth.NextAttemptAt > time.Now().Unix() {
		t.Fatalf("next_attempt_at = %d is in the future; a re-armed row must be claimable now", auth.NextAttemptAt)
	}
	for name, id := range map[string]int64{"data-rejected": qData, "other-forwarder": qOther, "legacy-unclassified": qLegacy} {
		row := get(id)
		if row.Status != "failed" {
			t.Errorf("%s row status = %q after rearm, want failed (untouched)", name, row.Status)
		}
	}
	if get(qOther).FailureClass != "auth" {
		t.Error("other forwarder's auth row lost its class; it must be untouched")
	}

	// A re-armed row is claimable by the ordinary worker path.
	claimed, err := svc.ClaimPendingUploadsWithContext(ctx, "qrz", 10)
	if err != nil {
		t.Fatalf("claim after rearm: %v", err)
	}
	if len(claimed) != 1 || claimed[0].QsoID != qAuth {
		t.Fatalf("claimed %d rows after rearm, want exactly the re-armed auth row", len(claimed))
	}
}

func TestRearmAuthFailedUploadsForForwarder_NothingToRearm_ReturnsZero(t *testing.T) {
	svc := testService(t)
	n, err := svc.RearmAuthFailedUploadsForForwarderWithContext(context.Background(), "qrz")
	if err != nil {
		t.Fatalf("rearm: %v", err)
	}
	if n != 0 {
		t.Fatalf("rearmed = %d, want 0", n)
	}
	if _, err := svc.RearmAuthFailedUploadsForForwarderWithContext(context.Background(), " "); err == nil {
		t.Fatal("blank forwarder name accepted; want an error")
	}
}
