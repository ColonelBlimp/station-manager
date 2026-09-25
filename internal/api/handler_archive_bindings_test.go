package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ADR 0082 part 9 (W-0021 5D): the bindings routes go through the archive
// port; the port's refusals map to their statuses, a PUT's body reaches the
// port as decoded, and no route is served without a manager. The rules
// themselves (whole-candidate validation, atomic write, masking, the SM Cloud
// remnant gate) are proven in internal/archive.

func bindingsReq(t *testing.T, srv *Server, method, uuid, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/v1/qso-archives/"+uuid+"/bindings", strings.NewReader(body))
	req.SetPathValue("uuid", uuid)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	if method == http.MethodGet {
		srv.handleGetArchiveBindings(w, req)
	} else {
		srv.handlePutArchiveBindings(w, req)
	}
	return w
}

func TestArchiveBindings_PortMappingAndPassthrough(t *testing.T) {
	srv := testServer(t)
	if w := bindingsReq(t, srv, http.MethodGet, "a", ""); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("no manager: status %d; want 503", w.Code)
	}
	f := &fakeArchives{bindings: types.ArchiveBindingsView{ArchiveID: "a", ArchiveLabel: "Home", RestartRequired: true,
		Destinations: []types.DestinationBindingView{{Type: "qrz", State: "mixed"}}}}
	srv.SetArchiveManager(f)

	w := bindingsReq(t, srv, http.MethodGet, "a", "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"restart_required":true`) || !strings.Contains(w.Body.String(), `"state":"mixed"`) {
		t.Fatalf("GET: status %d body %s", w.Code, w.Body.String())
	}
	body := `{"destinations":[{"type":"qrz","logbooks":[{"logbook_id":1,"enabled":true,"credentials":{"api_key":"k"}}]}]}`
	if w := bindingsReq(t, srv, http.MethodPut, "a", body); w.Code != http.StatusOK {
		t.Fatalf("PUT: status %d body %s", w.Code, w.Body.String())
	}
	if len(f.applied) != 1 || f.applied[0].Destinations[0].Logbooks[0].Credentials["api_key"] != "k" || !f.applied[0].Destinations[0].Logbooks[0].Enabled {
		t.Fatalf("the port did not receive the decoded request: %+v", f.applied)
	}
	for code, want := range map[string]int{
		"archive_not_active":     http.StatusConflict,
		"archive_not_found":      http.StatusNotFound,
		"binding_field_required": http.StatusBadRequest,
		"binding_not_enableable": http.StatusBadRequest,
		"logbook_not_found":      http.StatusNotFound,
		"bindings_unavailable":   http.StatusServiceUnavailable,
	} {
		f.bindingsErr = &codedErr{code, "why"}
		if w := bindingsReq(t, srv, http.MethodPut, "a", body); w.Code != want || decodeErrCode(t, w) != code {
			t.Fatalf("%s: status %d code %q; want %d", code, w.Code, decodeErrCode(t, w), want)
		}
	}
	if w := bindingsReq(t, srv, http.MethodPut, "", body); w.Code != http.StatusBadRequest {
		t.Fatalf("empty id: status %d; want 400", w.Code)
	}
}
