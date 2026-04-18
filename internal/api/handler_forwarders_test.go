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

	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

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

func TestSubmit_DisabledForwarder_Skipped(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("clublog", "clublog", false, "insert"), // enabled=false
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	uploads, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(uploads) != 1 {
		t.Fatalf("len = %d, want 1 (disabled forwarder should be skipped)", len(uploads))
	}
	if uploads[0].ForwarderName != "qrz" {
		t.Fatalf("forwarder_name = %q, want qrz", uploads[0].ForwarderName)
	}
}

func TestSubmit_ActionFilter_Excludes(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("lotw", "lotw", true, "update"), // action_filter: only update
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

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
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := patchQso(t, srv, qsoID, `{"comment":"updated"}`)
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
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, qsoID)
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

func TestDelete_NoForwarders_SoftDeletesCleanly(t *testing.T) {
	// No forwarders configured — delete should still soft-delete the
	// QSO and commit successfully. Verifies Stage 7 didn't make delete
	// depend on having forwarders.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := deleteQso(t, srv, qsoID)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d", w.Code)
	}

	if _, err := srv.db.FetchQsoByIdWithContext(context.Background(), qsoID); err == nil {
		t.Fatal("expected QSO to be soft-deleted")
	}
}
