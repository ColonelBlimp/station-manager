package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Read proofs for W-0020 slice 4 — GET /v1/station-events, the one read surface
// of the operator_event store across categories, replacing GET /v1/notifications
// (ruling 4; the browser-ingestion POST stays).

func getStationEvents(t *testing.T, srv *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/station-events"+query, nil)
	w := httptest.NewRecorder()
	srv.handleListStationEvents(w, req)
	return w
}

func decodeEventItems(t *testing.T, w *httptest.ResponseRecorder) []types.OperatorEvent {
	t.Helper()
	var body struct {
		Items []types.OperatorEvent `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode items: %v (%s)", err, w.Body.String())
	}
	return body.Items
}

// seedEvent records one event straight through the store (bypassing the
// producing boundaries) so the read-path tests are independent of them.
func seedEvent(t *testing.T, srv *Server, category, kind, severity, detail string) {
	t.Helper()
	if err := srv.db.RecordOperatorEvent(context.Background(), sqlite.OperatorEventInput{
		Category: category,
		Kind:     kind,
		Severity: severity,
		Build:    "v-test",
		Detail:   json.RawMessage(detail),
	}); err != nil {
		t.Fatalf("seed %s/%s: %v", category, kind, err)
	}
}

func seedNotification(t *testing.T, srv *Server, kind, detail string) {
	t.Helper()
	seedEvent(t, srv, "notification", kind, "error", detail)
}

// An empty store returns 200 with an empty (non-null) items array.
func TestListStationEvents_EmptyReturnsEmptyArray(t *testing.T) {
	srv := testServer(t)
	w := getStationEvents(t, srv, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if items := decodeEventItems(t, w); len(items) != 0 {
		t.Fatalf("items = %d, want 0", len(items))
	}
	// Must be [] not null so the SPA can render without a nil guard.
	var raw struct {
		Items json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &raw)
	if string(raw.Items) != "[]" {
		t.Errorf("items serialized as %s, want []", raw.Items)
	}
}

// AC5 + AC2: both categories on one page, newest first by arrival across
// categories, with the structured fields intact through the wire.
func TestListStationEvents_ReturnsBothCategoriesNewestFirstWithFields(t *testing.T) {
	srv := testServer(t)
	seedNotification(t, srv, "export.adif_failed", `{"count":1,"outcome":"server"}`)
	seedEvent(t, srv, "alarm", "tx_alarm.raised", "error", `{"code":"tx_unconfirmed"}`)
	seedEvent(t, srv, "alarm", "tx_alarm.cleared", "info", `{"code":"tx_unconfirmed","active_ms":700}`)
	seedNotification(t, srv, "forward.failed", `{"qso_id":7,"forwarder":"qrz","action":"insert","attempts":2}`)

	items := decodeEventItems(t, getStationEvents(t, srv, ""))
	if len(items) != 4 {
		t.Fatalf("items = %d, want 4", len(items))
	}
	want := []string{"forward.failed", "tx_alarm.cleared", "tx_alarm.raised", "export.adif_failed"}
	for i, k := range want {
		if items[i].Kind != k {
			t.Fatalf("order[%d] = %q, want %q (newest first across categories)", i, items[i].Kind, k)
		}
	}
	if items[1].Category != "alarm" || items[1].Severity != "info" || items[1].Build == "" || items[1].OccurredAt.IsZero() {
		t.Errorf("alarm item missing structured fields: %+v", items[1])
	}
	if string(items[3].Detail) != `{"count":1,"outcome":"server"}` {
		t.Errorf("detail round-trip = %s", items[3].Detail)
	}
}

// AC6: category and severity narrow the list; an unknown value is a 400, never
// an empty list that could read as "nothing happened".
func TestListStationEvents_FiltersByCategoryAndSeverityAndRejectsUnknownValues(t *testing.T) {
	srv := testServer(t)
	seedNotification(t, srv, "export.adif_failed", `{"count":1,"outcome":"server"}`)
	seedEvent(t, srv, "alarm", "tx_alarm.raised", "error", `{"code":"tx_unconfirmed"}`)
	seedEvent(t, srv, "alarm", "tx_alarm.cleared", "info", `{"code":"tx_unconfirmed","active_ms":700}`)
	seedEvent(t, srv, "alarm", "tx.disarmed", "warn", `{"cause":"cat_lost"}`)

	if items := decodeEventItems(t, getStationEvents(t, srv, "?category=alarm")); len(items) != 3 {
		t.Errorf("category=alarm returned %d items, want 3", len(items))
	}
	if items := decodeEventItems(t, getStationEvents(t, srv, "?category=notification")); len(items) != 1 || items[0].Category != "notification" {
		t.Errorf("category=notification returned %+v, want the one notification", items)
	}
	if items := decodeEventItems(t, getStationEvents(t, srv, "?severity=error")); len(items) != 2 {
		t.Errorf("severity=error returned %d items, want 2 (one per category)", len(items))
	}
	if items := decodeEventItems(t, getStationEvents(t, srv, "?category=alarm&severity=warn")); len(items) != 1 || items[0].Kind != "tx.disarmed" {
		t.Errorf("category=alarm&severity=warn returned %+v, want the disarm row", items)
	}
	for _, bad := range []string{"?category=daemon", "?category=ALARM", "?severity=fatal", "?severity=Error"} {
		if w := getStationEvents(t, srv, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (an unknown filter must not read as an empty history)", bad, w.Code)
		}
	}
}

// Producer→store→GET seam (browser kind): a failure recorded via the surviving
// POST /v1/notifications is returned by the new read surface.
func TestStationEvents_PostNotificationThenGetReturnsIt(t *testing.T) {
	srv := testServer(t)

	pw := postNotification(t, srv, `{"kind":"export.adif_failed","count":4,"outcome":"invalid"}`)
	if pw.Code != http.StatusNoContent {
		t.Fatalf("POST status = %d, want 204; body=%s", pw.Code, pw.Body.String())
	}
	items := decodeEventItems(t, getStationEvents(t, srv, "?category=notification"))
	if len(items) != 1 || items[0].Kind != "export.adif_failed" || items[0].Severity != "error" || items[0].Build == "" {
		t.Fatalf("items = %+v, want the one export.adif_failed", items)
	}
	if string(items[0].Detail) != `{"count":4,"outcome":"invalid"}` {
		t.Errorf("detail = %s, want {count:4,outcome:invalid}", items[0].Detail)
	}
}

// ?limit bounds the window at the store's whole ring (500 per category × the
// categories it holds); an out-of-range limit is rejected, not clamped.
func TestListStationEvents_LimitBoundsAndValidates(t *testing.T) {
	srv := testServer(t)
	for i := 0; i < 3; i++ {
		seedNotification(t, srv, "export.adif_failed", `{"count":1,"outcome":"server"}`)
	}
	if items := decodeEventItems(t, getStationEvents(t, srv, "?limit=2")); len(items) != 2 {
		t.Errorf("limit=2 returned %d items, want 2", len(items))
	}
	if w := getStationEvents(t, srv, "?limit=1000"); w.Code != http.StatusOK {
		t.Errorf("limit=1000 (the whole two-category ring): status = %d, want 200", w.Code)
	}
	for _, bad := range []string{"?limit=0", "?limit=-1", "?limit=1001", "?limit=abc"} {
		if w := getStationEvents(t, srv, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, w.Code)
		}
	}
}

// Ruling 4: GET /v1/notifications is retired at the router; the POST survives.
func TestStationEvents_OldGetRouteIsRetiredAndThePostSurvives(t *testing.T) {
	srv := testServer(t)
	viaRouter := func(path string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist (ST-1) that wraps the mux
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		return w.Code
	}
	if code := viaRouter("/v1/station-events"); code != http.StatusOK {
		t.Fatalf("GET /v1/station-events via the router: status = %d, want 200", code)
	}
	if code := viaRouter("/v1/notifications"); code != http.StatusNotFound && code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /v1/notifications still served (status %d); it is retired in favour of /v1/station-events", code)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications",
		strings.NewReader(`{"kind":"export.adif_failed","count":1,"outcome":"server"}`))
	req.Host = "127.0.0.1:8080"
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("POST /v1/notifications via the router: status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
}
