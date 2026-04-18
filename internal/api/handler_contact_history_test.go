package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// contactHistory is a test helper that sends GET /v1/contact-history.
// Empty string values are omitted from the query so the handler can be
// exercised on missing-param paths.
func contactHistory(t *testing.T, srv *Server, call, logbook string) *httptest.ResponseRecorder {
	t.Helper()
	var q []string
	if call != "" {
		q = append(q, "call="+call)
	}
	if logbook != "" {
		q = append(q, "logbook="+logbook)
	}
	url := "/v1/contact-history"
	if len(q) > 0 {
		url += "?" + strings.Join(q, "&")
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	srv.handleContactHistory(w, req)
	return w
}

type contactHistoryBody struct {
	Items []types.ContactHistory `json:"items"`
}

func decodeContactHistory(t *testing.T, w *httptest.ResponseRecorder) contactHistoryBody {
	t.Helper()
	var b contactHistoryBody
	if err := unmarshalJSON(w.Body.String(), &b); err != nil {
		t.Fatalf("decode body: %v (body=%s)", err, w.Body.String())
	}
	return b
}

func TestContactHistory_NoPriorContactsReturnsEmptyList(t *testing.T) {
	srv := testServer(t)

	w := contactHistory(t, srv, "DL1ABC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(got.Items))
	}
	// Confirm the empty-list JSON is `[]`, not `null` — easier for
	// clients that don't want to special-case null.
	if !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatalf("body = %q, want items:[]", w.Body.String())
	}
}

func TestContactHistory_HitsAcrossLogbooks(t *testing.T) {
	srv := testServer(t)
	lbA := createTestLogbook(t, srv, "Primary", "G4ABC")
	lbB := createTestLogbook(t, srv, "Contest", "G4ABC")

	// Log M0CMC twice in lbA and once in lbB.
	submitQsoAt(t, srv, lbA, "M0CMC", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbA, "M0CMC", "20250508", "0900", "7.100")
	submitQsoAt(t, srv, lbB, "M0CMC", "20250509", "1000", "14.074")

	// No logbook filter → all three come back.
	w := contactHistory(t, srv, "M0CMC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3 (all logbooks)", len(got.Items))
	}
}

func TestContactHistory_LogbookFilter(t *testing.T) {
	srv := testServer(t)
	lbA := createTestLogbook(t, srv, "Primary", "G4ABC")
	lbB := createTestLogbook(t, srv, "Contest", "G4ABC")

	submitQsoAt(t, srv, lbA, "M0CMC", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbA, "M0CMC", "20250508", "0900", "7.100")
	submitQsoAt(t, srv, lbB, "M0CMC", "20250509", "1000", "14.074")

	// logbook=lbB → only the one in lbB.
	w := contactHistory(t, srv, "M0CMC", fmt.Sprintf("%d", lbB))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1 (logbook-scoped)", len(got.Items))
	}
}

func TestContactHistory_NewestFirst(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// Log in order — created_at strictly increases with each insert, so
	// the last insert should appear first in the response.
	submitQsoAt(t, srv, lbID, "M0CMC", "20250501", "0800", "7.050")
	submitQsoAt(t, srv, lbID, "M0CMC", "20250507", "0900", "7.100")
	submitQsoAt(t, srv, lbID, "M0CMC", "20250510", "1000", "14.074")

	w := contactHistory(t, srv, "M0CMC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(got.Items))
	}
	// Most recently inserted = 20250510 first.
	if got.Items[0].QsoDate != "20250510" {
		t.Fatalf("items[0].qso_date = %q, want 20250510 (newest first)", got.Items[0].QsoDate)
	}
	if got.Items[2].QsoDate != "20250501" {
		t.Fatalf("items[2].qso_date = %q, want 20250501", got.Items[2].QsoDate)
	}
}

func TestContactHistory_SoftDeletedHidden(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	keep := submitQsoAt(t, srv, lbID, "M0CMC", "20250508", "0800", "7.050")
	drop := submitQsoAt(t, srv, lbID, "M0CMC", "20250508", "0900", "7.100")

	if dw := deleteQso(t, srv, drop); dw.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", dw.Code)
	}

	w := contactHistory(t, srv, "M0CMC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1 (soft-deleted hidden)", len(got.Items))
	}
	if got.Items[0].ID != keep {
		t.Fatalf("items[0].id = %d, want %d", got.Items[0].ID, keep)
	}
}

func TestContactHistory_CappedByMaxResults(t *testing.T) {
	// The endpoint caps at Server.MaxContactHistoryResults (default 100).
	// Log more than the cap and confirm the response length is the cap.
	srv := testServer(t)
	// Lower the cap for this test to keep it fast.
	srv.maxContactHistoryResults = 3

	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	for i := 0; i < 5; i++ {
		submitQsoAt(t, srv, lbID, "M0CMC", "20250508",
			fmt.Sprintf("08%02d", i), "7.050")
	}

	w := contactHistory(t, srv, "M0CMC", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decodeContactHistory(t, w)
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3 (capped)", len(got.Items))
	}
}

func TestContactHistory_MissingCall(t *testing.T) {
	srv := testServer(t)

	w := contactHistory(t, srv, "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContactHistory_InvalidCall(t *testing.T) {
	srv := testServer(t)

	// No digit.
	w := contactHistory(t, srv, "ABC", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestContactHistory_InvalidLogbookID(t *testing.T) {
	srv := testServer(t)

	w := contactHistory(t, srv, "M0CMC", "abc")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContactHistory_LogbookNotFound(t *testing.T) {
	srv := testServer(t)

	w := contactHistory(t, srv, "M0CMC", "999")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
