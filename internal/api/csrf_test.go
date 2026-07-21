package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// requireSameOrigin rejects a cross-origin drive-by POST while letting the
// same-origin SPA, loopback dev proxy, non-browser clients, and all safe methods
// through (codex 088bdb84 P1).
func TestRequireSameOrigin(t *testing.T) {
	srv := testServer(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := srv.requireSameOrigin(next)

	do := func(method, origin, host string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/v1/restart", nil)
		req.Host = host
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		return w
	}
	pass := func(name string, w *httptest.ResponseRecorder) {
		if w.Code != http.StatusOK {
			t.Fatalf("%s: want pass-through 200, got %d (%s)", name, w.Code, w.Body.String())
		}
	}
	block := func(name string, w *httptest.ResponseRecorder) {
		if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "cross_origin") {
			t.Fatalf("%s: want 403 cross_origin, got %d (%s)", name, w.Code, w.Body.String())
		}
	}

	pass("GET cross-origin (safe method)", do(http.MethodGet, "https://evil.example", "127.0.0.1:8080"))
	pass("POST no Origin (curl/CLI)", do(http.MethodPost, "", "127.0.0.1:8080"))
	pass("POST same host:port", do(http.MethodPost, "http://127.0.0.1:8080", "127.0.0.1:8080"))
	pass("POST loopback dev proxy", do(http.MethodPost, "http://localhost:5173", "127.0.0.1:8080"))
	block("POST cross-origin", do(http.MethodPost, "https://evil.example", "127.0.0.1:8080"))
	block("POST malformed Origin", do(http.MethodPost, "not-a-url", "127.0.0.1:8080"))
	// DNS rebinding: the attacker rebinds their own name to us, so Host AND Origin
	// both read evil.example — an Origin==Host check would pass, the allowlist
	// rejects on the disallowed Host (codex 85997b79 P1).
	block("POST DNS-rebinding", do(http.MethodPost, "http://evil.example:8080", "evil.example:8080"))
}
