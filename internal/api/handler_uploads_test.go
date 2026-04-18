package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// listUploads fires GET /v1/qso/{id}/uploads against the test server.
func listUploads(t *testing.T, srv *Server, qsoID int64) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/v1/qso/%d/uploads", qsoID), nil)
	req.SetPathValue("id", fmt.Sprintf("%d", qsoID))
	w := httptest.NewRecorder()
	srv.handleListQsoUploads(w, req)
	return w
}

func TestListUploads_ReturnsRowsForStoredQso(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
		forwarderCfg("clublog", "clublog", true, "insert"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := listUploads(t, srv, qsoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got struct {
		Items []types.QsoUpload `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items len = %d, want 2; got %+v", len(got.Items), got.Items)
	}

	// DB layer orders by (forwarder_name, action); assert stable order.
	if got.Items[0].ForwarderName != "clublog" {
		t.Fatalf("items[0].forwarder_name = %q, want clublog", got.Items[0].ForwarderName)
	}
	if got.Items[1].ForwarderName != "qrz" {
		t.Fatalf("items[1].forwarder_name = %q, want qrz", got.Items[1].ForwarderName)
	}

	for _, it := range got.Items {
		if it.Action != "insert" {
			t.Fatalf("row %+v: action = %q, want insert", it, it.Action)
		}
		if it.Status != "pending" {
			t.Fatalf("row %+v: status = %q, want pending", it, it.Status)
		}
	}
}

func TestListUploads_EmptyWhenNoForwardersConfigured(t *testing.T) {
	// No forwarders → ingest inserts no qso_upload rows. The handler
	// should return {"items":[]}, not 404.
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	w := listUploads(t, srv, qsoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var got struct {
		Items []types.QsoUpload `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 0 {
		t.Fatalf("items len = %d, want 0; got %+v", len(got.Items), got.Items)
	}
	// Canary: the JSON must be a real empty array, not null.
	if body := w.Body.String(); !containsExact(body, `"items":[]`) {
		t.Fatalf("body = %q, want an empty array for items (not null)", body)
	}
}

func TestListUploads_SoftDeletedQsoStillReturnsRows(t *testing.T) {
	// A soft-deleted QSO's delete-action upload row is still legitimate
	// forwarding work. The endpoint must surface it so clients can show
	// "delete pending → delete uploaded" for the operator.
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID := submitAndGetID(t, srv, lbID, testQsoADIF)

	dw := deleteQso(t, srv, qsoID)
	if dw.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d; body = %s", dw.Code, dw.Body.String())
	}

	w := listUploads(t, srv, qsoID)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s (soft-deleted QSO should not 404)", w.Code, http.StatusOK, w.Body.String())
	}

	var got struct {
		Items []types.QsoUpload `json:"items"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 2 {
		t.Fatalf("items len = %d, want 2 (insert + delete); got %+v", len(got.Items), got.Items)
	}

	var sawInsert, sawDelete bool
	for _, it := range got.Items {
		if it.ForwarderName != "qrz" {
			t.Fatalf("row %+v: forwarder_name = %q, want qrz", it, it.ForwarderName)
		}
		switch it.Action {
		case "insert":
			sawInsert = true
		case "delete":
			sawDelete = true
		}
	}
	if !sawInsert || !sawDelete {
		t.Fatalf("missing insert or delete row: %+v", got.Items)
	}
}

func TestListUploads_UnknownQsoReturns404(t *testing.T) {
	srv := testServer(t)

	w := listUploads(t, srv, 99999)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}
	if !containsExact(w.Body.String(), `"code":"not_found"`) {
		t.Fatalf("body = %q, want not_found code", w.Body.String())
	}
}

func TestListUploads_InvalidIdReturns400(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/qso/abc/uploads", nil)
	req.SetPathValue("id", "abc")
	w := httptest.NewRecorder()
	srv.handleListQsoUploads(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
	if !containsExact(w.Body.String(), `"code":"invalid_id"`) {
		t.Fatalf("body = %q, want invalid_id code", w.Body.String())
	}
}

// containsExact is a tiny local helper to keep these tests readable —
// strings.Contains would do, but an explicit name documents intent
// ("canary-match on a known fragment, not a fuzzy substring check").
func containsExact(body, fragment string) bool {
	for i := 0; i+len(fragment) <= len(body); i++ {
		if body[i:i+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
