package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

type logbookCountBody struct {
	LogbookID int64 `json:"logbook_id"`
	Count     int64 `json:"count"`
}

func getLogbookCount(t *testing.T, srv *Server, lbID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/v1/logbook/%d/count", lbID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", lbID))
	w := httptest.NewRecorder()
	srv.handleLogbookCount(w, req)
	return w
}

func decodeLogbookCount(t *testing.T, w *httptest.ResponseRecorder) logbookCountBody {
	t.Helper()
	var b logbookCountBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode count body: %v (body=%s)", err, w.Body.String())
	}
	return b
}

func TestLogbookCount_Empty(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Empty", "G4ABC")

	w := getLogbookCount(t, srv, lbID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeLogbookCount(t, w)
	if got.LogbookID != lbID {
		t.Fatalf("logbook_id = %d, want %d", got.LogbookID, lbID)
	}
	if got.Count != 0 {
		t.Fatalf("count = %d, want 0", got.Count)
	}
}

func TestLogbookCount_AfterInserts(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	submitQsoAt(t, srv, lbID, "DL1AAA", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbID, "DL1BBB", "20250508", "0900", "7.100")
	submitQsoAt(t, srv, lbID, "DL1CCC", "20250508", "1000", "7.150")

	w := getLogbookCount(t, srv, lbID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeLogbookCount(t, w)
	if got.Count != 3 {
		t.Fatalf("count = %d, want 3", got.Count)
	}
}

func TestLogbookCount_UnknownLogbook(t *testing.T) {
	srv := testServer(t)
	_ = createTestLogbook(t, srv, "Real", "G4ABC")

	w := getLogbookCount(t, srv, 99999)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

func TestLogbookCount_IsolatedPerLogbook(t *testing.T) {
	srv := testServer(t)
	lbA := createTestLogbook(t, srv, "A", "G4ABC")
	lbB := createTestLogbook(t, srv, "B", "G4ABC")

	submitQsoAt(t, srv, lbA, "DL1AAA", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbA, "DL1BBB", "20250508", "0900", "7.100")
	submitQsoAt(t, srv, lbB, "DL1CCC", "20250508", "1000", "7.150")

	wA := getLogbookCount(t, srv, lbA)
	if wA.Code != http.StatusOK {
		t.Fatalf("A status = %d; body = %s", wA.Code, wA.Body.String())
	}
	if got := decodeLogbookCount(t, wA); got.Count != 2 {
		t.Fatalf("A count = %d, want 2", got.Count)
	}

	wB := getLogbookCount(t, srv, lbB)
	if wB.Code != http.StatusOK {
		t.Fatalf("B status = %d; body = %s", wB.Code, wB.Body.String())
	}
	if got := decodeLogbookCount(t, wB); got.Count != 1 {
		t.Fatalf("B count = %d, want 1", got.Count)
	}
}
