package api

import (
	"context"
	"encoding/json"
	stderr "errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 3C: the archive routes over the injected port. Every refusal the
// port classifies keeps its code and message on the wire, with the status this
// package assigns; a nil port is 503; an unclassified failure is a 500 that
// serves no detail.

type codedErr struct{ code, msg string }

func (e *codedErr) Error() string       { return e.code + ": " + e.msg }
func (e *codedErr) RequestCode() string { return e.code }

type fakeArchives struct {
	bindings    types.ArchiveBindingsView
	bindingsErr error
	applied     []types.ArchiveBindingsRequest
	views       []types.QsoArchiveView
	created     types.QsoArchiveCreated
	createEr    error
	activate    types.QsoArchiveActivation
	actErr      error
	gotReq      types.QsoArchiveCreateRequest
	gotID       string
}

func (f *fakeArchives) List() []types.QsoArchiveView { return f.views }
func (f *fakeArchives) Bindings(_ context.Context, id string) (types.ArchiveBindingsView, error) {
	if f.bindingsErr != nil {
		return types.ArchiveBindingsView{}, f.bindingsErr
	}
	return f.bindings, nil
}
func (f *fakeArchives) ApplyBindings(_ context.Context, id string, req types.ArchiveBindingsRequest) (types.ArchiveBindingsView, error) {
	f.applied = append(f.applied, req)
	if f.bindingsErr != nil {
		return types.ArchiveBindingsView{}, f.bindingsErr
	}
	return f.bindings, nil
}
func (f *fakeArchives) CreateArchive(_ context.Context, req types.QsoArchiveCreateRequest) (types.QsoArchiveCreated, error) {
	f.gotReq = req
	return f.created, f.createEr
}
func (f *fakeArchives) Activate(_ context.Context, id string) (types.QsoArchiveActivation, error) {
	f.gotID = id
	return f.activate, f.actErr
}

func archivesServer(t *testing.T, f *fakeArchives) *Server {
	t.Helper()
	srv := testServer(t)
	srv.SetArchiveManager(f)
	return srv
}

func TestQsoArchives_UnwiredIs503(t *testing.T) {
	srv := testServer(t)
	for _, h := range []http.HandlerFunc{srv.handleListQsoArchives, srv.handleCreateQsoArchive, srv.handleActivateQsoArchive} {
		w := httptest.NewRecorder()
		h(w, httptest.NewRequest(http.MethodGet, "/v1/qso-archives", nil))
		if w.Code != http.StatusServiceUnavailable || decodeErrCode(t, w) != "archives_unavailable" {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	}
}

func TestQsoArchives_ListServesStates(t *testing.T) {
	f := &fakeArchives{views: []types.QsoArchiveView{
		{ID: "a", Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, State: types.QsoArchiveStateActive},
		{ID: "b", Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged, State: types.QsoArchiveStatePending, LastActivationError: "x"},
	}}
	srv := archivesServer(t, f)
	w := httptest.NewRecorder()
	srv.handleListQsoArchives(w, httptest.NewRequest(http.MethodGet, "/v1/qso-archives", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	var body qsoArchivesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Archives) != 2 || body.Archives[0].State != "active" || body.Archives[1].State != "pending" || body.Archives[1].LastActivationError != "x" {
		t.Fatalf("body = %+v", body)
	}
}

func postJSON(t *testing.T, h http.HandlerFunc, path, body string, pathValues map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range pathValues {
		req.SetPathValue(k, v)
	}
	w := httptest.NewRecorder()
	h(w, req)
	return w
}

func TestQsoArchives_Create(t *testing.T) {
	const body = `{"request_key":"k1","label":"Contest","logbook_name":"Contest","logbook_callsign":"g4abc"}`
	t.Run("created is 201 with the archive", func(t *testing.T) {
		f := &fakeArchives{created: types.QsoArchiveCreated{Archive: types.QsoArchiveView{ID: "b", Label: "Contest", State: types.QsoArchiveStateInactive}}}
		srv := archivesServer(t, f)
		w := postJSON(t, srv.handleCreateQsoArchive, "/v1/qso-archives", body, nil)
		if w.Code != http.StatusCreated || !strings.Contains(w.Body.String(), `"state":"inactive"`) {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
		if f.gotReq.RequestKey != "k1" || f.gotReq.LogbookCallsign != "g4abc" {
			t.Fatalf("port received %+v", f.gotReq)
		}
	})
	t.Run("reused is 200", func(t *testing.T) {
		srv := archivesServer(t, &fakeArchives{created: types.QsoArchiveCreated{Reused: true}})
		if w := postJSON(t, srv.handleCreateQsoArchive, "/v1/qso-archives", body, nil); w.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("unknown keys are rejected before the port", func(t *testing.T) {
		f := &fakeArchives{}
		srv := archivesServer(t, f)
		w := postJSON(t, srv.handleCreateQsoArchive, "/v1/qso-archives", `{"request_key":"k1","path":"/tmp/x.db"}`, nil)
		if w.Code != http.StatusBadRequest || f.gotReq.RequestKey != "" {
			t.Fatalf("status=%d body=%s port called=%v", w.Code, w.Body.String(), f.gotReq.RequestKey != "")
		}
	})
	t.Run("a field refusal is 400 with the port's code and message", func(t *testing.T) {
		srv := archivesServer(t, &fakeArchives{createEr: &codedErr{"missing_required_field", "label is required"}})
		w := postJSON(t, srv.handleCreateQsoArchive, "/v1/qso-archives", body, nil)
		if w.Code != http.StatusBadRequest || decodeErrCode(t, w) != "missing_required_field" || !strings.Contains(w.Body.String(), "label is required") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
	t.Run("an unclassified failure is 500 without its detail", func(t *testing.T) {
		srv := archivesServer(t, &fakeArchives{createEr: stderr.New("sync /secret/dir: disk full")})
		w := postJSON(t, srv.handleCreateQsoArchive, "/v1/qso-archives", body, nil)
		if w.Code != http.StatusInternalServerError || decodeErrCode(t, w) != "archive_create_failed" || strings.Contains(w.Body.String(), "/secret") {
			t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
		}
	})
}

func TestQsoArchives_Activate(t *testing.T) {
	t.Run("accepted is 202 with the durability", func(t *testing.T) {
		f := &fakeArchives{activate: types.QsoArchiveActivation{ID: "b", Durability: types.QsoArchiveDurabilityUncertain}}
		srv := archivesServer(t, f)
		w := postJSON(t, srv.handleActivateQsoArchive, "/v1/qso-archives/b/activate", "", map[string]string{"uuid": "b"})
		if w.Code != http.StatusAccepted || !strings.Contains(w.Body.String(), `"durability":"uncertain"`) || f.gotID != "b" {
			t.Fatalf("status=%d body=%s id=%q", w.Code, w.Body.String(), f.gotID)
		}
	})
	for code, status := range map[string]int{
		"archive_not_found": 404, "archive_active": 409, "activation_in_progress": 409, "tx_busy": 409,
		"archive_file_missing": 409, "archive_file_unreadable": 409, "archive_no_identity": 409,
		"archive_identity_mismatch": 409, "restart_unavailable": 503, "activation_persist_failed": 500,
		"restart_failed": 500, "pending_unclear": 500, "something_new": 500,
	} {
		t.Run(code, func(t *testing.T) {
			srv := archivesServer(t, &fakeArchives{actErr: &codedErr{code, "why"}})
			w := postJSON(t, srv.handleActivateQsoArchive, "/v1/qso-archives/b/activate", "", map[string]string{"uuid": "b"})
			if w.Code != status || decodeErrCode(t, w) != code || !strings.Contains(w.Body.String(), "why") {
				t.Fatalf("status=%d body=%s, want %d %s with the message", w.Code, w.Body.String(), status, code)
			}
		})
	}
}
