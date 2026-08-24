package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func getNotifications(t *testing.T, srv *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/notifications"+query, nil)
	w := httptest.NewRecorder()
	srv.handleListNotifications(w, req)
	return w
}

func decodeNotificationItems(t *testing.T, w *httptest.ResponseRecorder) []types.OperatorEvent {
	t.Helper()
	var body struct {
		Items []types.OperatorEvent `json:"items"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode items: %v (%s)", err, w.Body.String())
	}
	return body.Items
}

// seedNotification records one notification straight through the store (bypassing
// the producing boundaries) so the read-path tests are independent of them.
func seedNotification(t *testing.T, srv *Server, kind, detail string) {
	t.Helper()
	if err := srv.db.RecordOperatorEvent(context.Background(), sqlite.OperatorEventInput{
		Category: "notification",
		Kind:     kind,
		Severity: "error",
		Build:    "v-test",
		Detail:   json.RawMessage(detail),
	}); err != nil {
		t.Fatalf("seed notification: %v", err)
	}
}

// An empty store returns 200 with an empty (non-null) items array.
func TestListNotifications_EmptyReturnsEmptyArray(t *testing.T) {
	srv := testServer(t)
	w := getNotifications(t, srv, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if items := decodeNotificationItems(t, w); len(items) != 0 {
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

// Newest-first, with the full structured fields intact through the wire.
func TestListNotifications_ReturnsNewestFirstWithFields(t *testing.T) {
	srv := testServer(t)
	seedNotification(t, srv, "export.adif_failed", `{"count":1,"outcome":"server"}`)
	seedNotification(t, srv, "forward.failed", `{"qso_id":7,"forwarder":"qrz","action":"insert","attempts":2}`)

	items := decodeNotificationItems(t, getNotifications(t, srv, ""))
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	// Newest first: the forward.failed seeded second comes back first.
	if items[0].Kind != "forward.failed" || items[1].Kind != "export.adif_failed" {
		t.Fatalf("order = [%q,%q], want [forward.failed, export.adif_failed]", items[0].Kind, items[1].Kind)
	}
	if items[0].Severity != "error" || items[0].Build == "" || items[0].OccurredAt.IsZero() {
		t.Errorf("first item missing structured fields: %+v", items[0])
	}
	if string(items[1].Detail) != `{"count":1,"outcome":"server"}` {
		t.Errorf("detail round-trip = %s", items[1].Detail)
	}
}

// Producer→store→GET seam (browser kind): a failure recorded via POST is
// returned by GET — durable in the store, independent of any transient toast.
// (The daemon forward.failed producer→store leg is proven in the forwarding
// worker's markFailed test; store→GET for that kind is covered above.)
func TestNotifications_PostThenGetReturnsIt(t *testing.T) {
	srv := testServer(t)

	pw := postNotification(t, srv, `{"kind":"export.adif_failed","count":4,"outcome":"invalid"}`)
	if pw.Code != http.StatusNoContent {
		t.Fatalf("POST status = %d, want 204; body=%s", pw.Code, pw.Body.String())
	}

	items := decodeNotificationItems(t, getNotifications(t, srv, ""))
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Kind != "export.adif_failed" || items[0].Severity != "error" || items[0].Build == "" {
		t.Errorf("event = kind %q severity %q build %q, want export.adif_failed/error/<stamped>",
			items[0].Kind, items[0].Severity, items[0].Build)
	}
	if string(items[0].Detail) != `{"count":4,"outcome":"invalid"}` {
		t.Errorf("detail = %s, want {count:4,outcome:invalid}", items[0].Detail)
	}
}

// ?limit bounds the window; an out-of-range limit is rejected, not clamped.
func TestListNotifications_LimitBoundsAndValidates(t *testing.T) {
	srv := testServer(t)
	for i := 0; i < 3; i++ {
		seedNotification(t, srv, "export.adif_failed", `{"count":1,"outcome":"server"}`)
	}

	if items := decodeNotificationItems(t, getNotifications(t, srv, "?limit=2")); len(items) != 2 {
		t.Errorf("limit=2 returned %d items, want 2", len(items))
	}
	for _, bad := range []string{"?limit=0", "?limit=-1", "?limit=501", "?limit=abc"} {
		if w := getNotifications(t, srv, bad); w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", bad, w.Code)
		}
	}
}
