package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// contestDupe is a test helper that sends GET /v1/contest-dupe with the
// given query params. Empty strings are omitted from the query so tests
// can exercise missing-param validation paths.
func contestDupe(t *testing.T, srv *Server, logbook, call, band, mode string) *httptest.ResponseRecorder {
	t.Helper()
	var q []string
	if logbook != "" {
		q = append(q, "logbook="+logbook)
	}
	if call != "" {
		q = append(q, "call="+call)
	}
	if band != "" {
		q = append(q, "band="+band)
	}
	if mode != "" {
		q = append(q, "mode="+mode)
	}
	url := "/v1/contest-dupe"
	if len(q) > 0 {
		url += "?" + strings.Join(q, "&")
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()
	srv.handleContestDupe(w, req)
	return w
}

func TestContestDupe_HitBandOnly(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC on 40m SSB

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatalf("body = %q, want duplicate:true", w.Body.String())
	}
}

func TestContestDupe_HitBandMode(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC on 40m SSB

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "SSB")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"duplicate":true`) {
		t.Fatalf("body = %q, want duplicate:true", w.Body.String())
	}
}

func TestContestDupe_MissDifferentMode(t *testing.T) {
	// Band+mode contest: same call+band but different mode should NOT be
	// a dupe. This is the scenario that drove the mode param.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC on 40m SSB

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "CW")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false", w.Body.String())
	}
}

func TestContestDupe_MissDifferentBand(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC on 40m

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "20m", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false", w.Body.String())
	}
}

func TestContestDupe_MissDifferentCall(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")
	submitQso(t, srv, lbID, testQsoADIF, false) // M0CMC on 40m

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "DL1ABC", "40m", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false", w.Body.String())
	}
}

func TestContestDupe_EmptyLogbookIsNoDupe(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false", w.Body.String())
	}
}

func TestContestDupe_SoftDeletedNotCounted(t *testing.T) {
	// If the prior contact was soft-deleted, it should NOT count as a
	// dupe — the underlying sqlboiler query filters deleted_at IS NULL.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := submitQso(t, srv, lbID, testQsoADIF, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d", w.Code)
	}
	var r qsoservice.SubmitResult
	if err := unmarshalJSON(w.Body.String(), &r); err != nil || r.UUID == "" {
		t.Fatalf("decode uuid from %s (err=%v)", w.Body.String(), err)
	}

	if dw := deleteQso(t, srv, r.UUID); dw.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", dw.Code)
	}

	cw := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "")
	if cw.Code != http.StatusOK {
		t.Fatalf("status = %d", cw.Code)
	}
	if !strings.Contains(cw.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false after soft-delete", cw.Body.String())
	}
}

func TestContestDupe_LogbookScoping(t *testing.T) {
	// A hit in a different logbook must NOT count — the whole point of
	// the logbook-per-contest pattern. Log M0CMC 40m SSB in logbook A,
	// dupe-check in logbook B → miss.
	srv := testServer(t)
	lbA := createTestLogbook(t, srv, "Contest A", "G4ABC")
	lbB := createTestLogbook(t, srv, "Contest B", "G4ABC")
	submitQso(t, srv, lbA, testQsoADIF, false)

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbB), "M0CMC", "40m", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"duplicate":false`) {
		t.Fatalf("body = %q, want duplicate:false (other-logbook hit must not count)", w.Body.String())
	}
}

func TestContestDupe_MissingLogbook(t *testing.T) {
	srv := testServer(t)

	w := contestDupe(t, srv, "", "M0CMC", "40m", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContestDupe_MissingCall(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "", "40m", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContestDupe_MissingBand(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContestDupe_InvalidBand(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "not-a-band", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestContestDupe_InvalidMode(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "M0CMC", "40m", "not-a-mode")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestContestDupe_InvalidCall(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Contest", "G4ABC")

	w := contestDupe(t, srv, fmt.Sprintf("%d", lbID), "ABC", "40m", "")
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestContestDupe_LogbookNotFound(t *testing.T) {
	srv := testServer(t)

	w := contestDupe(t, srv, "999", "M0CMC", "40m", "")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}
