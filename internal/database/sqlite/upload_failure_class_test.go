package sqlite

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/enums/upload/action"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/failure"
	"github.com/ColonelBlimp/station-manager/internal/enums/upload/origin"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0010 outcome 9: a terminal failure's reason CLASS is durable beside its
// text, so recovery can pick the credential-stranded rows without parsing
// redacted provider output.
func TestMarkUploadFailed_PersistsFailureClass(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if err := svc.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "QRZ authentication rejected", failure.Auth); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if got := uploads[0].FailureClass; got != "auth" {
		t.Fatalf("FailureClass = %q, want auth", got)
	}
	if got := uploads[0].LastError; got != "QRZ authentication rejected" {
		t.Fatalf("LastError = %q — the class must not replace the readable reason", got)
	}
}

func TestMarkUploadFailed_UnclassifiedStoresNull(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if err := svc.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "wrong station callsign", ""); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	var isNull bool
	if err := svc.handle.QueryRow(`SELECT failure_class IS NULL FROM qso_upload WHERE id = ?`, claimed[0].ID).Scan(&isNull); err != nil {
		t.Fatalf("read failure_class: %v", err)
	}
	if !isNull {
		t.Fatal("failure_class stored non-NULL for an unclassified failure; want NULL, never ''")
	}
}

func TestMarkUploadFailed_RefusesUnknownClassBeforeSQL(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	err := svc.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "x", failure.Class("rejected"))
	if err == nil {
		t.Fatal("unknown class accepted; want a Go-side refusal naming the value")
	}
	uploads, _ := svc.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if got := uploads[0].Status; got != "in_progress" {
		t.Fatalf("Status = %q after refused mark, want in_progress (nothing written)", got)
	}
}

// A re-arm is a new attempt: the class of the failure it supersedes must not
// survive to describe a row that is now pending.
func TestInsertQsoUploadTx_RearmClearsFailureClass(t *testing.T) {
	svc := testService(t)
	lbID, _ := svc.InsertLogbook(types.Logbook{Name: "L", Callsign: "G4ABC"})
	qsoID, _ := svc.InsertQso(validTestQso(lbID, "M0CMC", "40m", "SSB", "20250508", "0845"))
	enqueueUpload(t, svc, qsoID, "qrz", "qrz", action.Insert)

	claimed, _ := svc.ClaimPendingUploadsWithContext(context.Background(), "qrz", 1)
	if err := svc.MarkUploadFailedWithContext(context.Background(), claimed[0].ID, "auth", failure.Auth); err != nil {
		t.Fatalf("mark failed: %v", err)
	}

	ctx := context.Background()
	tx, cancel, err := svc.BeginTxContext(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer cancel()
	if err := svc.InsertQsoUploadTx(ctx, tx, qsoID, action.Insert, "qrz", "qrz", origin.Manual); err != nil {
		t.Fatalf("re-arm: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	uploads, _ := svc.FetchUploadsByQsoIDWithContext(ctx, qsoID)
	if uploads[0].Status != "pending" {
		t.Fatalf("Status = %q, want pending after re-arm", uploads[0].Status)
	}
	if uploads[0].FailureClass != "" {
		t.Fatalf("FailureClass = %q after re-arm, want cleared", uploads[0].FailureClass)
	}
}
