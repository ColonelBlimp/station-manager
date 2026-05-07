package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestE2E_PatchWritesAuditRow verifies that a PATCH on /v1/qso/{uuid}
// appends one qso_history row tagged op=update / source=api with a
// before_image that round-trips back into the pre-edit QSO state
// (ADR 0016 prep #2).
func TestE2E_PatchWritesAuditRow(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	// Capture the pre-edit row so we can compare against before_image.
	preEdit, err := srv.db.FetchQsoByUUIDWithContext(context.Background(), qsoUUID)
	if err != nil {
		t.Fatalf("fetch pre-edit qso: %v", err)
	}

	w := patchQso(t, srv, qsoUUID, `{"comment":"audit test"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PATCH: status = %d; body = %s", w.Code, w.Body.String())
	}

	rows, err := srv.db.FetchQsoHistoryByUUIDWithContext(context.Background(), qsoUUID)
	if err != nil {
		t.Fatalf("fetch qso_history: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Op != "update" {
		t.Fatalf("op = %q, want update", r.Op)
	}
	if r.Source != "api" {
		t.Fatalf("source = %q, want api", r.Source)
	}
	if r.QsoUUID != qsoUUID {
		t.Fatalf("qso_uuid = %q, want %q", r.QsoUUID, qsoUUID)
	}

	// before_image must be the pre-edit snapshot, not the merged result.
	// Round-trip through types.Qso and check the field that the PATCH
	// changed: in the snapshot, comment must still be empty.
	var snapshot types.Qso
	if err := json.Unmarshal(r.BeforeImage, &snapshot); err != nil {
		t.Fatalf("unmarshal before_image: %v", err)
	}
	if snapshot.QsoDetails.Comment != preEdit.QsoDetails.Comment {
		t.Fatalf("before_image comment = %q, want pre-edit %q",
			snapshot.QsoDetails.Comment, preEdit.QsoDetails.Comment)
	}
	if snapshot.UUID != qsoUUID {
		t.Fatalf("before_image uuid = %q, want %q", snapshot.UUID, qsoUUID)
	}
	if snapshot.ContactedStation.Call != "M0CMC" {
		t.Fatalf("before_image call = %q, want M0CMC", snapshot.ContactedStation.Call)
	}
}

// TestE2E_DeleteWritesAuditRow verifies the symmetric path: a DELETE
// appends one row with op=delete / source=api and the snapshot
// reflects the QSO as it was just before soft-delete.
func TestE2E_DeleteWritesAuditRow(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, qsoUUID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("DELETE: status = %d; body = %s", w.Code, w.Body.String())
	}

	rows, err := srv.db.FetchQsoHistoryByUUIDWithContext(context.Background(), qsoUUID)
	if err != nil {
		t.Fatalf("fetch qso_history: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	r := rows[0]
	if r.Op != "delete" {
		t.Fatalf("op = %q, want delete", r.Op)
	}
	if r.Source != "api" {
		t.Fatalf("source = %q, want api", r.Source)
	}

	var snapshot types.Qso
	if err := json.Unmarshal(r.BeforeImage, &snapshot); err != nil {
		t.Fatalf("unmarshal before_image: %v", err)
	}
	if snapshot.UUID != qsoUUID {
		t.Fatalf("before_image uuid = %q, want %q", snapshot.UUID, qsoUUID)
	}
	if snapshot.ContactedStation.Call != "M0CMC" {
		t.Fatalf("before_image call = %q, want M0CMC", snapshot.ContactedStation.Call)
	}
}

// TestE2E_TwoEditsAccumulateHistory verifies that successive edits
// append rather than overwrite — the audit table must preserve each
// step a row passed through.
func TestE2E_TwoEditsAccumulateHistory(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	if w := patchQso(t, srv, qsoUUID, `{"comment":"first"}`); w.Code != http.StatusOK {
		t.Fatalf("first PATCH: status = %d", w.Code)
	}
	if w := patchQso(t, srv, qsoUUID, `{"comment":"second"}`); w.Code != http.StatusOK {
		t.Fatalf("second PATCH: status = %d", w.Code)
	}

	rows, err := srv.db.FetchQsoHistoryByUUIDWithContext(context.Background(), qsoUUID)
	if err != nil {
		t.Fatalf("fetch qso_history: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 (one per PATCH)", len(rows))
	}
	// Ordering is at ASC, id ASC — so rows[0] is the first edit's
	// snapshot (empty comment) and rows[1] is the second edit's
	// snapshot (comment="first").
	var first, second types.Qso
	if err := json.Unmarshal(rows[0].BeforeImage, &first); err != nil {
		t.Fatalf("unmarshal rows[0]: %v", err)
	}
	if err := json.Unmarshal(rows[1].BeforeImage, &second); err != nil {
		t.Fatalf("unmarshal rows[1]: %v", err)
	}
	if first.QsoDetails.Comment != "" {
		t.Fatalf("rows[0] (pre-first-edit) comment = %q, want empty", first.QsoDetails.Comment)
	}
	if second.QsoDetails.Comment != "first" {
		t.Fatalf("rows[1] (pre-second-edit) comment = %q, want \"first\"", second.QsoDetails.Comment)
	}
}
