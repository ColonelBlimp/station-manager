package api

// Old pages after the saved-QSO recovery's removal (ADR 0087, operator ruling
// 2026-10-05).
//
//   Q1  POST /v1/qso carrying ANY expect_-prefixed query key — one of the
//       three a recovered submit sent, all three, or an unknown one — is
//       409 reload_required and writes nothing — no QSO row and no upload-queue
//       row for an enabled insert forwarder: an old page must reload, its
//       expectation is never silently dropped. Its message says this request
//       stored nothing and never tells the page to log again (an earlier
//       attempt may have succeeded). The same submit without them stores AND
//       queues an upload (the fixture can do both).
//   Q2  GET /v1/submit-attribution is gone: 404 at the router.
//   Q3  A malformed query is still a 400 that stores nothing: a parser that
//       drops what it cannot read would turn a refused submit into a stored
//       one (review 2026-10-01).

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/forwarding"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

const reloadADIF = `<CALL:5>M0CMC<BAND:3>40m<MODE:3>SSB<FREQ:5>7.050<QSO_DATE:8>20250508<TIME_ON:4>0845<OPERATOR:5>G4ABC<EOR>`

const reloadForwarder = "qrz"

// reloadServer has one logbook bound to an enabled insert forwarder, so a stored
// QSO also queues an upload row.
func reloadServer(t *testing.T) (*Server, int64) {
	t.Helper()
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "Main", "G4ABC")
	srv.qso.SetDestinationRoutes([]forwarding.BoundForwarder{{
		LogbookID: lbID,
		Config: types.ForwarderConfig{
			Name: reloadForwarder, Type: "qrz", Enabled: true, ActionFilter: []string{"insert"},
		},
	}})
	return srv, lbID
}

func queuedUploads(t *testing.T, srv *Server) int64 {
	t.Helper()
	d, err := srv.db.UploadQueueDepthWithContext(context.Background(), reloadForwarder)
	if err != nil {
		t.Fatalf("queue depth: %v", err)
	}
	return d.Pending + d.Failed
}

func postQsoRaw(srv *Server, rawQuery string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/qso?"+rawQuery, strings.NewReader(reloadADIF))
	req.Header.Set("Content-Type", "application/x-adif")
	w := httptest.NewRecorder()
	srv.handleSubmitQso(w, req)
	return w
}

func storedQsos(t *testing.T, srv *Server, lbID int64) int64 {
	t.Helper()
	n, err := srv.db.FetchQsoCountByLogbookIdWithContext(context.Background(), lbID, "", false)
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestSubmitQso_AnyExpectKeyIsReloadRequiredAndStoresNothing(t *testing.T) {
	cases := map[string]map[string]string{
		"my_rig only":   {"expect_my_rig": "FTdx10"},
		"operator only": {"expect_operator": "G4ABC"},
		"my_name only":  {"expect_my_name": "Marc"},
		"all three":     {"expect_my_rig": "FTdx10", "expect_operator": "G4ABC", "expect_my_name": "Marc"},
		"empty value":   {"expect_my_name": ""},
		"unknown key":   {"expect_x": "1"},
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			srv, lbID := reloadServer(t)
			q := url.Values{}
			q.Set("logbook", fmt.Sprint(lbID))
			for k, v := range params {
				q.Set(k, v)
			}
			w := postQsoRaw(srv, q.Encode())
			var body struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v (body %s)", err, w.Body.String())
			}
			if w.Code != http.StatusConflict || body.Code != "reload_required" {
				t.Fatalf("status = %d, code = %q; want 409 reload_required", w.Code, body.Code)
			}
			msg := strings.ToLower(body.Message)
			if !strings.Contains(msg, "reload") || !strings.Contains(msg, "this request stored nothing") {
				t.Fatalf("message %q must say to reload and that this request stored nothing", body.Message)
			}
			if strings.Contains(msg, "log the qso again") || strings.Contains(msg, "log it again") {
				t.Fatalf("message %q must not tell the page to log again", body.Message)
			}
			if n := storedQsos(t, srv, lbID); n != 0 {
				t.Fatalf("stored %d QSOs, want 0", n)
			}
			if n := queuedUploads(t, srv); n != 0 {
				t.Fatalf("queued %d uploads, want 0", n)
			}
		})
	}
	// Control: the same submit without them stores.
	srv, lbID := reloadServer(t)
	if w := postQsoRaw(srv, fmt.Sprintf("logbook=%d", lbID)); w.Code != http.StatusCreated {
		t.Fatalf("plain submit: status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := storedQsos(t, srv, lbID); n != 1 {
		t.Fatalf("plain submit stored %d QSOs, want 1", n)
	}
	if n := queuedUploads(t, srv); n != 1 {
		t.Fatalf("plain submit queued %d uploads, want 1 (the fixture must be able to queue)", n)
	}
}

func TestSubmitAttribution_RouteIsGone(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/submit-attribution", nil)
	req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist (ST-1) that wraps the mux
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GET /v1/submit-attribution: status = %d, want 404 (ADR 0087 removes it)", w.Code)
	}
}

func TestSubmitQso_MalformedQueryIs400AndStoresNothing(t *testing.T) {
	srv, lbID := reloadServer(t)
	w := postQsoRaw(srv, fmt.Sprintf("logbook=%d&force=a;b", lbID))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"invalid_query_param"`) {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if n := storedQsos(t, srv, lbID); n != 0 {
		t.Fatalf("stored %d QSOs, want 0", n)
	}
}
