package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// filler streams n bytes of 'a' without allocating them, so an oversized-body test can
// drive MaxBytesReader past its cap cheaply.
type filler struct{ n int }

func (f *filler) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, io.EOF
	}
	k := len(p)
	if k > f.n {
		k = f.n
	}
	for i := 0; i < k; i++ {
		p[i] = 'a'
	}
	f.n -= k
	return k, nil
}

func ingestHandlers(s *Server) []struct {
	name string
	path string
	h    http.HandlerFunc
} {
	return []struct {
		name string
		path string
		h    http.HandlerFunc
	}{
		{"qsos", "/v1/qsos", s.handlePutQsos},
		{"evidence", "/v1/evidence", s.handlePutEvidence},
	}
}

func quietServer() *Server {
	return &Server{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// AW-5: SM Cloud ingest must map an oversized body to 413 body_too_large — matching the
// daemon's shared reader — with a GENERIC message, never the raw decoder text. Today
// both handlers collapse a *http.MaxBytesError into 400 invalid_body and concatenate
// err.Error(), so a transport-limit hit looks like malformed JSON and leaks
// "http: request body too large".
func TestCloudIngest_OversizedBody_Is413NoLeak(t *testing.T) {
	for _, hc := range ingestHandlers(quietServer()) {
		t.Run(hc.name, func(t *testing.T) {
			// An unterminated JSON string just past the 32 MiB cap: valid JSON start, so
			// the decoder keeps reading until MaxBytesReader trips (not a syntax error).
			body := io.MultiReader(strings.NewReader(`"`), &filler{n: maxBodyBytes + 100})
			req := httptest.NewRequest(http.MethodPut, hc.path, body)
			w := httptest.NewRecorder()
			hc.h(w, req)

			if w.Code != http.StatusRequestEntityTooLarge {
				t.Fatalf("oversized body: status = %d, want 413; body %s", w.Code, w.Body.String())
			}
			if code := errCode(t, w); code != "body_too_large" {
				t.Errorf("code = %q, want body_too_large", code)
			}
			if strings.Contains(w.Body.String(), "request body too large") {
				t.Errorf("response leaks the raw decoder message: %s", w.Body.String())
			}
		})
	}
}

// A malformed (but small) body is a generic 400 invalid_body with no decoder detail in
// the response — the detail is logged, not returned.
func TestCloudIngest_MalformedBody_Is400NoLeak(t *testing.T) {
	for _, hc := range ingestHandlers(quietServer()) {
		t.Run(hc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPut, hc.path, strings.NewReader(`{bad json`))
			w := httptest.NewRecorder()
			hc.h(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("malformed body: status = %d, want 400; body %s", w.Code, w.Body.String())
			}
			if code := errCode(t, w); code != "invalid_body" {
				t.Errorf("code = %q, want invalid_body", code)
			}
			if strings.Contains(w.Body.String(), "invalid character") {
				t.Errorf("response leaks the raw decoder message: %s", w.Body.String())
			}
		})
	}
}

func errCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var er struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &er); err != nil {
		t.Fatalf("error body is not JSON (%v): %s", err, w.Body.String())
	}
	return er.Code
}
