package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// AW-4: the /v1/ API namespace lives on its own ServeMux (apiRouter/registerRoutes), so an
// unmatched or method-mismatched route classifies cleanly — unlike the shared mux, where
// the SPA catch-all (GET /) poisoned it (a non-GET unknown path 405'd with a spurious
// Allow: GET; real mismatches gained a phantom GET). Every /v1/ fallback is now the stable
// JSON envelope: an unknown path is 404 not_found with NO Allow for any method; a real
// method mismatch is 405 method_not_allowed carrying only the methods that work.
func TestRouteFallback_UnmatchedV1RoutesReturnJSON(t *testing.T) {
	srv := testServer(t)
	serve := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist (ST-1)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		return w
	}
	wantEnvelope := func(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		if w.Code != status {
			t.Fatalf("status = %d, want %d; body %s", w.Code, status, w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		if got := decodeErrCode(t, w); got != code {
			t.Errorf("code = %q, want %q", got, code)
		}
	}

	t.Run("unknown /v1/ path is 404 not_found with no Allow, for every method", func(t *testing.T) {
		for _, m := range []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodOptions,
		} {
			w := serve(m, "/v1/no-such-route")
			wantEnvelope(t, w, http.StatusNotFound, "not_found")
			if allow := w.Header().Get("Allow"); allow != "" {
				t.Errorf("%s unknown path: Allow = %q, want empty (a 404 advertises no methods)", m, allow)
			}
		}
	})

	t.Run("method mismatch is 405 with an accurate Allow (no spurious SPA GET)", func(t *testing.T) {
		// /v1/restart is POST-only. Under the old shared mux a GET fell through the SPA
		// catch-all to 404, and a PUT reported Allow: GET, POST. On the API mux both are a
		// clean 405 whose Allow is exactly POST — the phantom GET is gone.
		for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
			w := serve(m, "/v1/restart")
			wantEnvelope(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
			if allow := w.Header().Get("Allow"); allow != "POST" {
				t.Errorf("%s /v1/restart: Allow = %q, want exactly \"POST\"", m, allow)
			}
		}
	})

	t.Run("multi-method route reports every real method and nothing else", func(t *testing.T) {
		// /v1/qso/{uuid} is GET+PATCH+DELETE. A POST is 405 advertising those three — and
		// not POST, and no phantom GET beyond the real one.
		w := serve(http.MethodPost, "/v1/qso/abc123")
		wantEnvelope(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
		allow := w.Header().Get("Allow")
		for _, want := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
			if !strings.Contains(allow, want) {
				t.Errorf("Allow = %q, want it to include %s", allow, want)
			}
		}
		if strings.Contains(allow, http.MethodPost) {
			t.Errorf("Allow = %q, must not advertise the mismatched POST", allow)
		}
	})

	t.Run("HEAD on an unknown /v1/ path is a 404 envelope with no body", func(t *testing.T) {
		// A real server (not a recorder) so net/http's transport-level HEAD body-drop
		// applies; the handler still writes the envelope, so the status/headers are the
		// 404 contract while the body is empty. httptest serves on loopback, passing the
		// Host allowlist.
		ts := httptest.NewServer(srv.httpServer.Handler)
		defer ts.Close()
		req, err := http.NewRequest(http.MethodHead, ts.URL+"/v1/no-such-route", nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
		if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "application/json") {
			t.Errorf("content-type = %q, want application/json", ct)
		}
		if body, _ := io.ReadAll(resp.Body); len(body) != 0 {
			t.Errorf("HEAD response carries a body (%q); net/http must drop it", body)
		}
	})

	t.Run("OPTIONS on a real route is 405 (the API does not implement OPTIONS)", func(t *testing.T) {
		w := serve(http.MethodOptions, "/v1/version") // GET-only
		wantEnvelope(t, w, http.StatusMethodNotAllowed, "method_not_allowed")
		if allow := w.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) {
			t.Errorf("Allow = %q, want it to include GET", allow)
		}
	})

	t.Run("/v1 and /v1/ and a trailing slash are all 404 not_found JSON", func(t *testing.T) {
		for _, p := range []string{"/v1", "/v1/", "/v1/version/"} {
			wantEnvelope(t, serve(http.MethodGet, p), http.StatusNotFound, "not_found")
		}
	})

	t.Run("matched /v1/ route is unaffected", func(t *testing.T) {
		w := serve(http.MethodGet, "/v1/forwarder-queues")
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("a registered route must not hit the fallback, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("non-/v1/ paths keep their SPA behavior, not the JSON envelope", func(t *testing.T) {
		// A client route the SPA owns returns index.html (200), never a 404 envelope.
		w := serve(http.MethodGet, "/logbook")
		if w.Code == http.StatusNotFound {
			t.Errorf("SPA client route 404'd: %s", w.Body.String())
		}
		if ct := w.Header().Get("Content-Type"); strings.Contains(ct, "application/json") {
			t.Errorf("SPA route served JSON (%q); it must not reach the API fallback", ct)
		}
	})

	t.Run("origin gate still fronts the API fallback", func(t *testing.T) {
		// requireSameOrigin sits OUTSIDE apiRouter: a disallowed host on an unknown /v1/
		// path is a 403 cross_origin, not the 404 fallback (middleware placement AC).
		req := httptest.NewRequest(http.MethodGet, "/v1/no-such-route", nil)
		req.Host = "evil.example:8080"
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (origin gate fronts the fallback)", w.Code)
		}
		if code := decodeErrCode(t, w); code != "cross_origin" {
			t.Errorf("code = %q, want cross_origin", code)
		}
	})
}
