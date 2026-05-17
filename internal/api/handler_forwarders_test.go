package api

import (
	"context"
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// forwarderCfg is a tiny builder for types.ForwarderConfig that the
// forwarder-wiring tests use to populate config before the server is
// constructed. Credentials are omitted — the ingest path doesn't
// parse them, the forwarder package's constructor does, which isn't
// reached here.
func forwarderCfg(name, typ string, enabled bool, actions ...string) types.ForwarderConfig {
	return types.ForwarderConfig{
		Name:         name,
		Type:         typ,
		Enabled:      enabled,
		ActionFilter: actions,
	}
}

// serverWithForwarders builds a testServer whose Config.Forwarders
// contains the given entries.
func serverWithForwarders(t *testing.T, fwds ...types.ForwarderConfig) *Server {
	t.Helper()
	return testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Forwarders = fwds
	})
}

func TestSubmit_EnqueuesRowsForEnabledForwarders(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
		forwarderCfg("clublog", "clublog", true, "insert", "update"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	qsoID, _ := submitAndGetID(t, srv, lbID, testQsoADIF)

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("len = %d, want 2 (one row per enabled forwarder)", len(uploads))
	}

	// Rows are ordered by (forwarder_name, action); both entries should
	// have action='insert' and status='pending' fresh from the tx.
	for _, u := range uploads {
		if u.Action != "insert" {
			t.Fatalf("row %+v: action = %q, want insert", u, u.Action)
		}
		if u.Status != "pending" {
			t.Fatalf("row %+v: status = %q, want pending", u, u.Status)
		}
		if u.Attempts != 0 {
			t.Fatalf("row %+v: attempts = %d, want 0", u, u.Attempts)
		}
	}
}

func TestSubmit_DisabledForwarder_StillEnqueues(t *testing.T) {
	// Per ADR 0022: presence in config.json gates enqueue; Enabled is
	// purely a worker-lifecycle signal. A disabled forwarder MUST still
	// receive a qso_upload row so the queue is drained on the worker's
	// first tick after re-enable + restart. Pre-ADR-0022 this test
	// pinned the opposite (and-was-the-bug) behaviour.
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("clublog", "clublog", false, "insert"), // enabled=false → STILL enqueues
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	qsoID, _ := submitAndGetID(t, srv, lbID, testQsoADIF)

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(uploads) != 2 {
		t.Fatalf("len = %d, want 2 (both enabled and disabled forwarders enqueue per ADR 0022)", len(uploads))
	}
	seen := map[string]bool{}
	for _, u := range uploads {
		seen[u.ForwarderName] = true
	}
	if !seen["qrz"] || !seen["clublog"] {
		t.Fatalf("forwarder_names = %v, want both qrz and clublog", seen)
	}
}

func TestSubmit_ActionFilter_Excludes(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("lotw", "lotw", true, "update"), // action_filter: only update
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	qsoID, _ := submitAndGetID(t, srv, lbID, testQsoADIF)

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("len = %d, want 1 (lotw's filter excludes insert)", len(uploads))
	}
	if uploads[0].ForwarderName != "qrz" {
		t.Fatalf("forwarder_name = %q, want qrz", uploads[0].ForwarderName)
	}
}

func TestUpdate_EnqueuesUpdateRows(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update"),
		forwarderCfg("lotw", "lotw", true, "insert"), // excludes update
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, qsoUUID, `{"comment":"updated"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("patch status = %d; body = %s", w.Code, w.Body.String())
	}

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	// Expected: qrz+insert, qrz+update, lotw+insert. No lotw+update.
	if len(uploads) != 3 {
		t.Fatalf("len = %d, want 3; got %+v", len(uploads), uploads)
	}

	var sawQrzUpdate, sawLotwUpdate bool
	for _, u := range uploads {
		if u.ForwarderName == "qrz" && u.Action == "update" {
			sawQrzUpdate = true
		}
		if u.ForwarderName == "lotw" && u.Action == "update" {
			sawLotwUpdate = true
		}
	}
	if !sawQrzUpdate {
		t.Fatal("expected qrz+update row, not present")
	}
	if sawLotwUpdate {
		t.Fatal("lotw+update row should NOT exist (filter excludes update)")
	}
}

func TestDelete_EnqueuesDeleteRows(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
		forwarderCfg("lotw", "lotw", true, "insert"), // no delete in filter
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, qsoUUID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d; body = %s", w.Code, w.Body.String())
	}

	// QSO itself should be gone (soft-deleted).
	if _, err := srv.db.FetchQsoByIdWithContext(context.Background(), qsoID); err == nil {
		t.Fatal("expected ErrNotFound after delete, got nil")
	}

	// Queue: qrz+insert, qrz+delete, lotw+insert. No lotw+delete.
	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	var sawQrzDelete, sawLotwDelete bool
	for _, u := range uploads {
		if u.ForwarderName == "qrz" && u.Action == "delete" {
			sawQrzDelete = true
		}
		if u.ForwarderName == "lotw" && u.Action == "delete" {
			sawLotwDelete = true
		}
	}
	if !sawQrzDelete {
		t.Fatal("expected qrz+delete row, not present")
	}
	if sawLotwDelete {
		t.Fatal("lotw+delete row should NOT exist (filter excludes delete)")
	}
}

// TestUpdate_TwicePatchesRearm covers the regression that a second PATCH on
// the same QSO would hit the qso_upload UNIQUE (qso_id, forwarder_name, action)
// constraint and bubble as 500. Re-arm semantics: each PATCH represents a new
// state needing forwarding, so the existing row is reset to status='pending'
// with cleared retry state — the row count stays the same, the row's status
// returns to pending, and attempts goes back to zero.
func TestUpdate_TwicePatchesRearm(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	for i, body := range []string{`{"comment":"first"}`, `{"comment":"second"}`} {
		w := patchQso(t, srv, qsoUUID, body)
		if w.Code != http.StatusOK {
			t.Fatalf("patch #%d status = %d; body = %s", i+1, w.Code, w.Body.String())
		}
	}

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	// Expect exactly two rows: qrz+insert (from initial submit) and qrz+update
	// (re-armed by the second PATCH, not duplicated).
	if len(uploads) != 2 {
		t.Fatalf("len = %d, want 2; got %+v", len(uploads), uploads)
	}
	var update *types.QsoUpload
	for i := range uploads {
		if uploads[i].Action == "update" {
			update = &uploads[i]
			break
		}
	}
	if update == nil {
		t.Fatal("expected qrz+update row")
	}
	if update.Status != "pending" {
		t.Fatalf("update row status = %q, want pending after re-arm", update.Status)
	}
	if update.Attempts != 0 {
		t.Fatalf("update row attempts = %d, want 0 after re-arm", update.Attempts)
	}
}

// TestDelete_TwiceDeletesIsRejected covers the second-DELETE path. The handler
// returns 404 because the QSO is soft-deleted on the first call; the upload-
// row constraint should NOT be reached.
func TestDelete_TwiceIsRejectedAt404(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "delete"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	if w := deleteQso(t, srv, qsoUUID); w.Code != http.StatusNoContent {
		t.Fatalf("first delete status = %d", w.Code)
	}
	if w := deleteQso(t, srv, qsoUUID); w.Code != http.StatusNotFound {
		t.Fatalf("second delete status = %d, want 404", w.Code)
	}
}

func TestDelete_NoForwarders_SoftDeletesCleanly(t *testing.T) {
	// No forwarders configured — delete should still soft-delete the
	// QSO and commit successfully. Verifies Stage 7 didn't make delete
	// depend on having forwarders.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID, qsoUUID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, qsoUUID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}

	if _, err := srv.db.FetchQsoByIdWithContext(context.Background(), qsoID); err == nil {
		t.Fatal("expected QSO to be soft-deleted")
	}
}
