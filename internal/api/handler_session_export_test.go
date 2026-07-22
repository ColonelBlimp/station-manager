package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// postExport drives handleSessionExport with a JSON body.
func postExport(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/session/export", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleSessionExport(w, req)
	return w
}

func TestSessionExport_MissingUUIDs_Returns400(t *testing.T) {
	srv := testServer(t)
	w := postExport(t, srv, `{"uuids":[]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "missing_required_field") {
		t.Errorf("body should carry missing_required_field; got %s", w.Body.String())
	}
}

func TestSessionExport_TooManyUUIDs_Returns400(t *testing.T) {
	srv := testServer(t)

	var b strings.Builder
	b.WriteString(`{"uuids":[`)
	for i := 0; i <= maxSessionQsoUUIDs; i++ { // one over the cap
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`"00000000-0000-0000-0000-000000000000"`)
	}
	b.WriteString(`]}`)

	w := postExport(t, srv, b.String())
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("body should carry invalid_field_value; got %s", w.Body.String())
	}
}

func TestSessionExport_AllUnknownUUIDs_Returns400NoQsos(t *testing.T) {
	srv := testServer(t)
	w := postExport(t, srv, `{"uuids":["00000000-0000-0000-0000-000000000000"]}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "no_qsos") {
		t.Errorf("body should carry no_qsos; got %s", w.Body.String())
	}
}

func TestSessionExport_Success_ReturnsAdifAttachmentAndArchives(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	w := postExport(t, srv, `{"uuids":["`+uuid+`"]}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	// Downloadable attachment with the ADIF content type.
	if ct := w.Header().Get("Content-Type"); ct != "application/x-adif" {
		t.Errorf("Content-Type = %q, want application/x-adif", ct)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.Contains(cd, "attachment") || !strings.Contains(cd, ".adi") {
		t.Errorf("Content-Disposition = %q, want an .adi attachment", cd)
	}

	// The rebuilt-from-DB body — the enriched record, not a client subset.
	body := w.Body.String()
	if !strings.Contains(body, "<EOH>") || !strings.Contains(body, "<EOR>") {
		t.Errorf("body is not a well-formed ADIF document; got %s", body)
	}
	if !strings.Contains(strings.ToUpper(body), "STATION_CALLSIGN") {
		t.Errorf("body should carry the daemon's back-filled station block; got %s", body)
	}

	// Backup-on-export: a copy was archived under exports/sent-adif/.
	entries := readArchiveDir(t, srv)
	if len(entries) != 1 {
		t.Fatalf("archive dir has %d entries, want 1 (the exported backup)", len(entries))
	}
}

func TestSessionExport_BadFilename_Returns400(t *testing.T) {
	srv := testServer(t)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	uuid := submitTestQsoUUID(t, srv, lbID)

	// A traversal filename must be rejected before any archive write.
	w := postExport(t, srv, `{"uuids":["`+uuid+`"],"filename":"../escape.adi"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_field_value") {
		t.Errorf("body should carry invalid_field_value; got %s", w.Body.String())
	}
}
