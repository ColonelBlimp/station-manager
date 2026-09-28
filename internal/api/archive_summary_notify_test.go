package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func createLogbook(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/logbook", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleCreateLogbook(w, req)
	return w
}

// ADR 0084 slice 2b: a logbook created, renamed or deleted changes what the
// active archive holds, so each committed change signals the archive summary —
// directly, through the injected notifier. A refused change signals nothing.
func TestLogbookWrites_NotifyTheArchiveSummary(t *testing.T) {
	srv := testServer(t)
	// Logbook 1 is the configured default (never deletable); the test works on a second.
	if w := createLogbook(t, srv, `{"name":"Main","callsign":"7Q5MLV"}`); w.Code != http.StatusCreated {
		t.Fatalf("seed default = %d %s", w.Code, w.Body.String())
	}
	var calls atomic.Int32
	srv.SetArchiveSummaryNotifier(func() { calls.Add(1) })

	w := createLogbook(t, srv, `{"name":"Contest","callsign":"7Q5MLV"}`)
	if w.Code != http.StatusCreated {
		t.Fatalf("create = %d %s", w.Code, w.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("after a create: %d notifications, want 1", calls.Load())
	}
	id := int64(0)
	lbs, err := srv.db.FetchAllLogbooksWithContext(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, lb := range lbs {
		if lb.Name == "Contest" {
			id = lb.ID
		}
	}

	if w := createLogbook(t, srv, `{"name":"Contest","callsign":"7Q5MLV"}`); w.Code != http.StatusConflict {
		t.Fatalf("duplicate create = %d, want 409", w.Code)
	}
	if calls.Load() != 1 {
		t.Fatalf("a refused create notified (%d)", calls.Load())
	}

	if w := patchLogbook(t, srv, id, `{"name":"CQWW"}`); w.Code != http.StatusOK {
		t.Fatalf("rename = %d %s", w.Code, w.Body.String())
	}
	if calls.Load() != 2 {
		t.Fatalf("after a rename: %d notifications, want 2", calls.Load())
	}

	if w := deleteLogbook(t, srv, id); w.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", w.Code, w.Body.String())
	}
	if calls.Load() != 3 {
		t.Fatalf("after a delete: %d notifications, want 3", calls.Load())
	}
	if w := deleteLogbook(t, srv, id); w.Code != http.StatusNotFound {
		t.Fatalf("second delete = %d, want 404", w.Code)
	}
	if calls.Load() != 3 {
		t.Fatalf("a refused delete notified (%d)", calls.Load())
	}
}

// First-run setup creates the Default logbook: the archive's contents changed.
func TestSetup_CreatingTheDefaultLogbookNotifiesTheArchiveSummary(t *testing.T) {
	srv := testServer(t)
	var calls atomic.Int32
	srv.SetArchiveSummaryNotifier(func() { calls.Add(1) })
	req := httptest.NewRequest(http.MethodPut, "/v1/config",
		strings.NewReader(`{"logging_station":{"station_callsign":"M0XYZ"}}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handlePutConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup PUT = %d %s", w.Code, w.Body.String())
	}
	if calls.Load() != 1 {
		t.Fatalf("setup created the Default logbook with %d notifications, want 1", calls.Load())
	}
}
