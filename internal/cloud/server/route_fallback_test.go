package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// AW-4: the SM Cloud API returns the JSON error envelope — not ServeMux's plain text — for
// an unmatched route, matching the daemon. An unknown path is 404 not_found; a method
// mismatch is 405 method_not_allowed with an accurate Allow. The cloud mux carries no SPA
// catch-all, so ServeMux already classifies 404 vs 405 correctly; only the body changes.
func TestCloudRouteFallback_JSON(t *testing.T) {
	h := quietServer().Handler()
	serve := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	t.Run("unknown path is 404 not_found JSON with no Allow, for every method", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
			w := serve(m, "/v1/no-such-route")
			if w.Code != http.StatusNotFound {
				t.Fatalf("%s: status = %d, want 404; body %s", m, w.Code, w.Body.String())
			}
			if code := errCode(t, w); code != "not_found" {
				t.Errorf("%s: code = %q, want not_found", m, code)
			}
			if allow := w.Header().Get("Allow"); allow != "" {
				t.Errorf("%s: Allow = %q, want empty (a 404 advertises no methods)", m, allow)
			}
		}
	})

	t.Run("method mismatch is 405 method_not_allowed JSON with accurate Allow", func(t *testing.T) {
		// /v1/qsos is PUT-only.
		w := serve(http.MethodGet, "/v1/qsos")
		if w.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405; body %s", w.Code, w.Body.String())
		}
		if code := errCode(t, w); code != "method_not_allowed" {
			t.Errorf("code = %q, want method_not_allowed", code)
		}
		if allow := w.Header().Get("Allow"); allow != http.MethodPut {
			t.Errorf("Allow = %q, want exactly PUT", allow)
		}
	})

	t.Run("matched route does not hit the fallback", func(t *testing.T) {
		// GET /v1/version is a real route; whatever it returns, it is never the
		// synthesized 404/405 fallback.
		w := serve(http.MethodGet, "/v1/version")
		if w.Code == http.StatusNotFound || w.Code == http.StatusMethodNotAllowed {
			t.Errorf("a registered route must not hit the fallback, got %d: %s", w.Code, w.Body.String())
		}
	})
}
