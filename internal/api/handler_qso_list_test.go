package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// submitQsoAt is a helper that submits a single QSO with a caller-chosen
// date and time so tests can control the newest-first sort order. Returns
// the stored QSO's UUIDv7 (the canonical external identifier per ADR
// 0016) — call sites that need the int PK can resolve it via the DB,
// but for handler-level deletes/patches the UUID is what the route
// expects.
func submitQsoAt(t *testing.T, srv *Server, lbID int64, call, qsoDate, timeOn, freq string) string {
	t.Helper()
	body := fmt.Sprintf(
		"<CALL:%d>%s<BAND:3>40m<MODE:3>SSB<FREQ:%d>%s<QSO_DATE:8>%s<TIME_ON:4>%s<TIME_OFF:4>%s<RST_SENT:2>59<RST_RCVD:2>59<STATION_CALLSIGN:5>G4ABC<COUNTRY:7>England<EOR>",
		len(call), call, len(freq), freq, qsoDate, timeOn, timeOn,
	)
	w := submitQso(t, srv, lbID, body, false)
	if w.Code != http.StatusCreated {
		t.Fatalf("submit: status = %d; body = %s", w.Code, w.Body.String())
	}
	var r qsoservice.SubmitResult
	if err := unmarshalJSON(w.Body.String(), &r); err != nil || r.UUID == "" {
		t.Fatalf("decode uuid from %s (err=%v)", w.Body.String(), err)
	}
	return r.UUID
}

// listQsoByLogbook is a test helper that sends GET /v1/logbook/{id}/qso
// with optional ?after and ?limit params. Passing "" for after omits the
// param; passing limit=0 omits it too (so the server's default applies).
func listQsoByLogbook(t *testing.T, srv *Server, lbID int64, after string, limit int) *httptest.ResponseRecorder {
	t.Helper()
	url := fmt.Sprintf("/v1/logbook/%d/qso", lbID)
	var q []string
	if after != "" {
		q = append(q, "after="+after)
	}
	if limit > 0 {
		q = append(q, fmt.Sprintf("limit=%d", limit))
	}
	if len(q) > 0 {
		url += "?" + strings.Join(q, "&")
	}
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.SetPathValue("id", fmt.Sprintf("%d", lbID))
	w := httptest.NewRecorder()
	srv.handleListQsoByLogbook(w, req)
	return w
}

type qsoListBody struct {
	Items      []types.Qso `json:"items"`
	NextCursor *string     `json:"next_cursor"`
}

func decodeQsoList(t *testing.T, w *httptest.ResponseRecorder) qsoListBody {
	t.Helper()
	var b qsoListBody
	if err := json.Unmarshal(w.Body.Bytes(), &b); err != nil {
		t.Fatalf("decode list body: %v (body=%s)", err, w.Body.String())
	}
	return b
}

func TestListQso_EmptyLogbook(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Empty", "G4ABC")

	w := listQsoByLogbook(t, srv, lbID, "", 0)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeQsoList(t, w)
	if len(got.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(got.Items))
	}
	if got.NextCursor != nil {
		t.Fatalf("next_cursor = %v, want nil", *got.NextCursor)
	}
}

func TestListQso_SinglePage(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	submitQsoAt(t, srv, lbID, "DL1AAA", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbID, "DL1BBB", "20250508", "0900", "7.100")
	submitQsoAt(t, srv, lbID, "DL1CCC", "20250508", "1000", "7.150")

	w := listQsoByLogbook(t, srv, lbID, "", 10)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeQsoList(t, w)
	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(got.Items))
	}
	if got.NextCursor != nil {
		t.Fatalf("next_cursor should be nil when everything fit in one page")
	}
	// Newest first: CCC (1000), BBB (0900), AAA (0800).
	if got.Items[0].ContactedStation.Call != "DL1CCC" {
		t.Fatalf("items[0].call = %q, want DL1CCC (newest first)", got.Items[0].ContactedStation.Call)
	}
	if got.Items[2].ContactedStation.Call != "DL1AAA" {
		t.Fatalf("items[2].call = %q, want DL1AAA", got.Items[2].ContactedStation.Call)
	}
}

func TestListQso_PaginationWalk(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// 7 QSOs, spaced one minute apart so the sort key is deterministic.
	const total = 7
	calls := make([]string, total)
	for i := range total {
		c := fmt.Sprintf("DL1A%02d", i)
		calls[i] = c
		submitQsoAt(t, srv, lbID, c, "20250508",
			fmt.Sprintf("08%02d", i), "7.050")
	}

	// Page 1: first 3 (newest first, so reverse order of insertion).
	w := listQsoByLogbook(t, srv, lbID, "", 3)
	if w.Code != http.StatusOK {
		t.Fatalf("page1 status = %d; body = %s", w.Code, w.Body.String())
	}
	p1 := decodeQsoList(t, w)
	if len(p1.Items) != 3 {
		t.Fatalf("page1 items = %d, want 3", len(p1.Items))
	}
	if p1.NextCursor == nil {
		t.Fatal("page1 next_cursor should be set — more rows to come")
	}

	// Page 2.
	w = listQsoByLogbook(t, srv, lbID, *p1.NextCursor, 3)
	if w.Code != http.StatusOK {
		t.Fatalf("page2 status = %d", w.Code)
	}
	p2 := decodeQsoList(t, w)
	if len(p2.Items) != 3 {
		t.Fatalf("page2 items = %d, want 3", len(p2.Items))
	}
	if p2.NextCursor == nil {
		t.Fatal("page2 next_cursor should be set — one row remaining")
	}

	// Page 3 — last one item, no more.
	w = listQsoByLogbook(t, srv, lbID, *p2.NextCursor, 3)
	if w.Code != http.StatusOK {
		t.Fatalf("page3 status = %d", w.Code)
	}
	p3 := decodeQsoList(t, w)
	if len(p3.Items) != 1 {
		t.Fatalf("page3 items = %d, want 1", len(p3.Items))
	}
	if p3.NextCursor != nil {
		t.Fatalf("page3 next_cursor should be nil — last page")
	}

	// Concatenation of the three pages should equal the full set in
	// newest-first order.
	var walked []string
	for _, p := range []qsoListBody{p1, p2, p3} {
		for _, it := range p.Items {
			walked = append(walked, it.ContactedStation.Call)
		}
	}
	// Expected: calls reversed.
	for i := range total {
		want := calls[total-1-i]
		if walked[i] != want {
			t.Fatalf("walked[%d] = %q, want %q (walk = %v)", i, walked[i], want, walked)
		}
	}
}

func TestListQso_LogbookNotFound(t *testing.T) {
	srv := testServer(t)

	w := listQsoByLogbook(t, srv, 999, "", 0)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestListQso_InvalidCursor(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	w := listQsoByLogbook(t, srv, lbID, "not-a-real-cursor!!", 10)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_cursor") {
		t.Fatalf("body = %q, want invalid_cursor", w.Body.String())
	}
}

func TestListQso_NegativeLimitRejected(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/v1/logbook/%d/qso?limit=-5", lbID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", lbID))
	w := httptest.NewRecorder()
	srv.handleListQsoByLogbook(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestListQso_LimitClampedToMax(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// Insert a few QSOs and ask for a crazy-large limit. Should succeed
	// (clamped silently to the server max) and return everything.
	submitQsoAt(t, srv, lbID, "DL1AAA", "20250508", "0800", "7.050")
	submitQsoAt(t, srv, lbID, "DL1BBB", "20250508", "0900", "7.100")

	w := listQsoByLogbook(t, srv, lbID, "", 1_000_000)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeQsoList(t, w)
	if len(got.Items) != 2 {
		t.Fatalf("items = %d, want 2", len(got.Items))
	}
}

func TestListQso_SoftDeletedHidden(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	keepID := submitQsoAt(t, srv, lbID, "DL1AAA", "20250508", "0800", "7.050")
	deleteID := submitQsoAt(t, srv, lbID, "DL1BBB", "20250508", "0900", "7.100")

	// Soft-delete the second one.
	if w := deleteQso(t, srv, deleteID); w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", w.Code)
	}

	w := listQsoByLogbook(t, srv, lbID, "", 10)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeQsoList(t, w)
	if len(got.Items) != 1 {
		t.Fatalf("items = %d, want 1 (soft-deleted hidden)", len(got.Items))
	}
	if got.Items[0].UUID != keepID {
		t.Fatalf("items[0].uuid = %q, want %q", got.Items[0].UUID, keepID)
	}
}

func TestListQso_DefaultLimitApplied(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")

	// Insert more QSOs than any reasonable default — but small enough for
	// the test to be fast. We just want to confirm that omitting ?limit
	// doesn't fail and the server returns some consistent count.
	const n = 5
	for i := range n {
		submitQsoAt(t, srv, lbID, fmt.Sprintf("DL1A%02d", i), "20250508",
			fmt.Sprintf("08%02d", i), "7.050")
	}

	w := listQsoByLogbook(t, srv, lbID, "", 0)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}
	got := decodeQsoList(t, w)
	if len(got.Items) != n {
		t.Fatalf("items = %d, want %d (under default page limit)", len(got.Items), n)
	}
}
