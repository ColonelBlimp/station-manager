package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

func listNotEmailed(t *testing.T, srv *Server, lbID int64, raw string) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/logbook/%d/qso?not_emailed=%s", lbID, raw)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", strconv.FormatInt(lbID, 10))
	w := httptest.NewRecorder()
	srv.handleListQsoByLogbook(w, req)
	return w
}

func countNotEmailed(t *testing.T, srv *Server, lbID int64, raw string) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/logbook/%d/count?not_emailed=%s", lbID, raw)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", strconv.FormatInt(lbID, 10))
	w := httptest.NewRecorder()
	srv.handleLogbookCount(w, req)
	return w
}

// TestListQsoByLogbook_NotEmailedFiltersEmailed proves the server-side
// not_emailed filter (the "Not emailed only" logbook toggle) restricts BOTH the
// page and the count to QSOs whose sm_fwrd_by_email_status stamp isn't "Y" —
// across the whole logbook, not just the loaded page (the page-local bug this
// fixes). Opt-in: not_emailed=false leaves everything visible.
func TestListQsoByLogbook_NotEmailedFiltersEmailed(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	emailedID, _ := submitAndGetID(t, srv, lbID, testQsoADIF)
	_, unsentUUID := submitAndGetID(t, srv, lbID, testQsoADIF2)

	// Stamp the first QSO "forwarded by email" (the durable additional_data flag
	// the filter keys on — the same stamp a real session email writes). A fresh
	// submit is at revision 0, which the revision-guarded stamp matches.
	if _, err := srv.db.MarkSessionEmailedAtRevisionWithContext(t.Context(),
		[]sqlite.SessionEmailTarget{{ID: emailedID, Revision: 0}}, "20260808"); err != nil {
		t.Fatalf("mark emailed: %v", err)
	}

	// not_emailed=true → only the unsent QSO on the page.
	w := listNotEmailed(t, srv, lbID, "true")
	if w.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Items []struct {
			UUID string `json:"uuid"`
		} `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].UUID != unsentUUID {
		t.Fatalf("items = %+v, want only the unsent QSO %s", got.Items, unsentUUID)
	}

	// Count reflects the same filter (so the SPA's "of N" matches the page).
	cw := countNotEmailed(t, srv, lbID, "true")
	if cw.Code != http.StatusOK {
		t.Fatalf("count status = %d, want 200; body = %s", cw.Code, cw.Body.String())
	}
	var cgot struct {
		Count int64 `json:"count"`
	}
	if err := unmarshalJSON(cw.Body.String(), &cgot); err != nil {
		t.Fatalf("decode count: %v", err)
	}
	if cgot.Count != 1 {
		t.Fatalf("filtered count = %d, want 1", cgot.Count)
	}

	// not_emailed=false → both QSOs count (opt-in, no default filtering).
	cw2 := countNotEmailed(t, srv, lbID, "false")
	var cgot2 struct {
		Count int64 `json:"count"`
	}
	if err := unmarshalJSON(cw2.Body.String(), &cgot2); err != nil {
		t.Fatalf("decode count (false): %v", err)
	}
	if cgot2.Count != 2 {
		t.Fatalf("unfiltered count = %d, want 2", cgot2.Count)
	}
}

// TestListQsoByLogbook_NotEmailedInvalidValue rejects a non-boolean not_emailed
// with a 400 rather than silently ignoring it — matching the strict validation
// the other list filters use.
func TestListQsoByLogbook_NotEmailedInvalidValue(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := listNotEmailed(t, srv, lbID, "maybe")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	var e struct {
		Code string `json:"code"`
	}
	if err := unmarshalJSON(w.Body.String(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "invalid_not_emailed" {
		t.Fatalf("code = %q, want invalid_not_emailed", e.Code)
	}
}
