package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Backend proofs for W-0001 / ADR 0076 Producer B — the browser notification
// ingestion endpoint. The handler must strictly reject hostile input and record
// only a canonical, server-stamped event.

func postNotification(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/notifications", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleRecordNotification(w, req)
	return w
}

func notificationRows(t *testing.T, srv *Server) int {
	t.Helper()
	evs, err := srv.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 500)
	if err != nil {
		t.Fatalf("fetch notifications: %v", err)
	}
	return len(evs)
}

// A valid export.adif_failed posts a 204 and records a canonical, server-stamped
// event whose detail is exactly {count, outcome}.
func TestRecordNotification_ValidExportFailureRecordsCanonicalEvent(t *testing.T) {
	srv := testServer(t)

	w := postNotification(t, srv, `{"kind":"export.adif_failed","count":5,"outcome":"server"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}

	evs, err := srv.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("rows = %d, want 1", len(evs))
	}
	ev := evs[0]
	if ev.Kind != "export.adif_failed" {
		t.Errorf("kind = %q, want export.adif_failed", ev.Kind)
	}
	if ev.Severity != "error" {
		t.Errorf("severity = %q, want error (daemon-stamped)", ev.Severity)
	}
	if ev.Build == "" {
		t.Errorf("build is empty — must be stamped server-side")
	}
	if string(ev.Detail) != `{"count":5,"outcome":"server"}` {
		t.Errorf("detail = %s, want canonical {count,outcome} built server-side", ev.Detail)
	}
}

// Every hostile shape is rejected 400 AND writes nothing. A rejected request must
// never reach the durable store.
func TestRecordNotification_StrictRejectionWritesNothing(t *testing.T) {
	cases := []struct{ name, body string }{
		{"unknown key message", `{"kind":"export.adif_failed","count":5,"outcome":"server","message":"boom"}`},
		{"unknown key reason", `{"kind":"export.adif_failed","count":5,"outcome":"server","reason":"secret"}`},
		{"unknown key code", `{"kind":"export.adif_failed","count":5,"outcome":"server","code":"E17"}`},
		{"unknown kind", `{"kind":"qso.deleted","count":5,"outcome":"server"}`},
		{"outcome aborted", `{"kind":"export.adif_failed","count":5,"outcome":"aborted"}`},
		{"outcome ok", `{"kind":"export.adif_failed","count":5,"outcome":"ok"}`},
		{"outcome garbage", `{"kind":"export.adif_failed","count":5,"outcome":"pwned"}`},
		{"count zero", `{"kind":"export.adif_failed","count":0,"outcome":"server"}`},
		{"count negative", `{"kind":"export.adif_failed","count":-1,"outcome":"server"}`},
		{"count missing", `{"kind":"export.adif_failed","outcome":"server"}`},
		{"count non-integral", `{"kind":"export.adif_failed","count":2.5,"outcome":"server"}`},
		{"count overflow", `{"kind":"export.adif_failed","count":99999999999999999999,"outcome":"server"}`},
		{"malformed json", `{"kind":`},
		{"trailing bracket", `{"kind":"export.adif_failed","count":5,"outcome":"server"}]`},
		{"trailing object", `{"kind":"export.adif_failed","count":5,"outcome":"server"}{"x":1}`},
		{"trailing garbage", `{"kind":"export.adif_failed","count":5,"outcome":"server"} nope`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t)
			w := postNotification(t, srv, tc.body)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body=%s", w.Code, w.Body.String())
			}
			if n := notificationRows(t, srv); n != 0 {
				t.Errorf("recorded %d rows, want 0 — a rejected request must write nothing", n)
			}
		})
	}
}

// A count above the export endpoint's own 10,000 cap is still a valid failure to
// record — a 10,001-QSO request may be exactly the invalid export being reported.
func TestRecordNotification_CountAboveExportCapIsValid(t *testing.T) {
	srv := testServer(t)

	w := postNotification(t, srv, `{"kind":"export.adif_failed","count":10001,"outcome":"invalid"}`)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204; body=%s", w.Code, w.Body.String())
	}
	evs, err := srv.db.FetchOperatorEventsByCategoryWithContext(context.Background(), "notification", 10)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(evs) != 1 || string(evs[0].Detail) != `{"count":10001,"outcome":"invalid"}` {
		t.Fatalf("want one row with count 10001; got %d rows, detail=%s", len(evs), func() string {
			if len(evs) > 0 {
				return string(evs[0].Detail)
			}
			return "<none>"
		}())
	}
}

// All four allowlisted outcomes are accepted.
func TestRecordNotification_AcceptsEachAllowedOutcome(t *testing.T) {
	for _, outcome := range []string{"no_qsos", "invalid", "server", "network"} {
		t.Run(outcome, func(t *testing.T) {
			srv := testServer(t)
			w := postNotification(t, srv, `{"kind":"export.adif_failed","count":3,"outcome":"`+outcome+`"}`)
			if w.Code != http.StatusNoContent {
				t.Fatalf("outcome %q: status = %d, want 204; body=%s", outcome, w.Code, w.Body.String())
			}
			if n := notificationRows(t, srv); n != 1 {
				t.Errorf("outcome %q: rows = %d, want 1", outcome, n)
			}
		})
	}
}
