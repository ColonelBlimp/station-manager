package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// AW-1 alpha.2: the public QSO responses (GET, a successful PATCH, and the logbook-list
// items) go through a boundary projection that strips the server-internal identifiers
// dedupe_key, csid, and country_details.id, while the canonical external fields survive —
// uuid, the transitional local id, logbook_id, the ADIF/enrichment fields, and
// contact_history[].id. This pins the handler wiring; projectPublicQso's full field matrix
// is proven in qso_projection_test.go.
func TestPublicProjection_HandlersOmitInternalIDs(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Main", "G4ABC")
	_, uuid := submitAndGetID(t, srv, lbID, testQsoADIF)

	assertProjected := func(t *testing.T, body []byte) {
		t.Helper()
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(body, &obj); err != nil {
			t.Fatalf("body is not a JSON object: %v (%s)", err, body)
		}
		for _, k := range []string{"dedupe_key", "csid"} {
			if _, ok := obj[k]; ok {
				t.Errorf("%q must be absent from the public projection: %s", k, body)
			}
		}
		for _, k := range []string{"uuid", "id", "logbook_id"} {
			if _, ok := obj[k]; !ok {
				t.Errorf("%q must be present in the public projection", k)
			}
		}
		if cd, ok := obj["country_details"]; ok {
			var c map[string]json.RawMessage
			if err := json.Unmarshal(cd, &c); err == nil {
				if _, ok := c["id"]; ok {
					t.Errorf("country_details.id must be absent from the public projection: %s", cd)
				}
			}
		}
	}

	t.Run("GET", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/v1/qso/"+uuid, nil)
		req.SetPathValue("uuid", uuid)
		w := httptest.NewRecorder()
		srv.handleGetQso(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET status = %d: %s", w.Code, w.Body.String())
		}
		assertProjected(t, w.Body.Bytes())
	})

	t.Run("PATCH success", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/v1/qso/"+uuid, strings.NewReader(`{"comment":"projection edit"}`))
		req.SetPathValue("uuid", uuid)
		w := httptest.NewRecorder()
		srv.handleUpdateQso(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PATCH status = %d: %s", w.Code, w.Body.String())
		}
		assertProjected(t, w.Body.Bytes())
	})

	t.Run("logbook list items", func(t *testing.T) {
		id := strconv.FormatInt(lbID, 10)
		req := httptest.NewRequest(http.MethodGet, "/v1/logbook/"+id+"/qso", nil)
		req.SetPathValue("id", id)
		w := httptest.NewRecorder()
		srv.handleListQsoByLogbook(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("list status = %d: %s", w.Code, w.Body.String())
		}
		var resp struct {
			Items []json.RawMessage `json:"items"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("list body: %v (%s)", err, w.Body.String())
		}
		if len(resp.Items) == 0 {
			t.Fatal("list returned no items")
		}
		for _, it := range resp.Items {
			assertProjected(t, it)
		}
	})
}
