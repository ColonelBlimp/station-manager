package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// enqueueUploads fires POST /v1/forwarder/{name}/uploads against the test server.
func enqueueUploads(t *testing.T, srv *Server, name, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/forwarder/"+name+"/uploads", strings.NewReader(body))
	req.SetPathValue("name", name)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleEnqueueForwarderUploads(w, req)
	return w
}

func TestEnqueueForwarderUploads_HappyPath(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert", "update", "delete"))
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	qsoID, uuid := submitAndGetID(t, srv, lbID, testQsoADIF)

	// The submit already queued a row to the enabled qrz forwarder, so drain it
	// out of the way: the point here is that the manual endpoint re-arms it.
	w := enqueueUploads(t, srv, "qrz", `{"uuids":["`+uuid+`"],"force":true}`)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got struct {
		Enqueued        int `json:"enqueued"`
		SkippedUploaded int `json:"skipped_uploaded"`
	}
	if err := unmarshalJSON(w.Body.String(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enqueued != 1 {
		t.Fatalf("enqueued = %d, want 1", got.Enqueued)
	}

	rows, err := srv.db.FetchUploadsByQsoIDWithContext(context.Background(), qsoID)
	if err != nil {
		t.Fatalf("fetch uploads: %v", err)
	}
	if len(rows) != 1 || rows[0].ForwarderName != "qrz" || rows[0].Status != "pending" {
		t.Fatalf("rows = %+v, want one pending qrz row", rows)
	}
}

func TestEnqueueForwarderUploads_Validation(t *testing.T) {
	srv := serverWithForwarders(t,
		forwarderCfg("qrz", "qrz", true, "insert"),
		forwarderCfg("clublog", "clublog", false, "insert"),
	)
	lbID := createTestLogbook(t, srv, "My Log", "G4ABC")
	_, uuid := submitAndGetID(t, srv, lbID, testQsoADIF)

	cases := []struct {
		name      string
		dest      string
		body      string
		wantCode  int
		wantCode2 string // error code in body
	}{
		{"empty uuids", "qrz", `{"uuids":[]}`, http.StatusBadRequest, "missing_required_field"},
		{"malformed json", "qrz", `{`, http.StatusBadRequest, ""},
		{"disabled forwarder", "clublog", `{"uuids":["` + uuid + `"]}`, http.StatusBadRequest, "forwarder_unavailable"},
		{"unknown forwarder", "lotw", `{"uuids":["` + uuid + `"]}`, http.StatusBadRequest, "forwarder_unavailable"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := enqueueUploads(t, srv, c.dest, c.body)
			if w.Code != c.wantCode {
				t.Fatalf("status = %d, want %d; body = %s", w.Code, c.wantCode, w.Body.String())
			}
			if c.wantCode2 != "" {
				var e struct {
					Code string `json:"code"`
				}
				if err := unmarshalJSON(w.Body.String(), &e); err != nil {
					t.Fatalf("decode err body: %v", err)
				}
				if e.Code != c.wantCode2 {
					t.Fatalf("error code = %q, want %q", e.Code, c.wantCode2)
				}
			}
		})
	}
}

func TestEnqueueForwarderUploads_BatchTooLarge(t *testing.T) {
	srv := serverWithForwarders(t, forwarderCfg("qrz", "qrz", true, "insert"))

	uuids := make([]string, maxEnqueueUploadsBatch+1)
	for i := range uuids {
		// Distinct strings so dedupe doesn't collapse them below the cap.
		uuids[i] = `"x` + strings.Repeat("0", 4) + itoa(i) + `"`
	}
	body := `{"uuids":[` + strings.Join(uuids, ",") + `]}`

	w := enqueueUploads(t, srv, "qrz", body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body len = %d", w.Code, w.Body.Len())
	}
	var e struct {
		Code string `json:"code"`
	}
	if err := unmarshalJSON(w.Body.String(), &e); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if e.Code != "batch_too_large" {
		t.Fatalf("code = %q, want batch_too_large", e.Code)
	}
}

// itoa avoids an strconv import for the one numeric-suffix use above.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
