package api

// ST-2 (docs/reviews/internal-security-trust-boundary-audit.md) — the embedded operator SPAs
// were frameable (no frame-ancestors / X-Frame-Options), so a hostile page could iframe the
// loopback UI and clickjack same-origin operator actions. Operator rulings 2026-08-16: one
// OUTERMOST middleware (outside requireSameOrigin, so even ST-1 403s carry the headers) sets,
// on EVERY response, Content-Security-Policy: frame-ancestors 'none' + X-Frame-Options: DENY +
// X-Content-Type-Options: nosniff + Referrer-Policy: no-referrer. CSP here is frame-ancestors
// only; a full script/style CSP is separate work.

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func assertSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder, where string) {
	t.Helper()
	want := map[string]string{
		"Content-Security-Policy": "frame-ancestors 'none'",
		"X-Frame-Options":         "DENY",
		"X-Content-Type-Options":  "nosniff",
		"Referrer-Policy":         "no-referrer",
	}
	for k, v := range want {
		if got := w.Header().Get(k); got != v {
			t.Errorf("%s: header %s = %q, want %q (status %d)", where, k, got, v, w.Code)
		}
	}
}

// C1 + C2: the frame-denial + browser-trust headers are present on every response served
// through the real handler stack — the root redirect (not followed), a SPA document, a SPA
// fallback route, a real static JS asset, and a representative API response.
func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	srv := testServer(t)

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist (ST-1)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		return w
	}

	// Root redirect — assert on the 3xx itself, without following it.
	root := get("/")
	if root.Code != http.StatusFound {
		t.Fatalf("GET /: status = %d, want 302 (%s)", root.Code, root.Body.String())
	}
	assertSecurityHeaders(t, root, "root redirect")

	assertSecurityHeaders(t, get("/app/"), "SPA document")
	assertSecurityHeaders(t, get("/app/some-client-route"), "SPA fallback")

	// A real static asset — nosniff is what makes an incorrect MIME type matter, so cover one.
	asset := get("/app/assets/index.js")
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /app/assets/index.js: status = %d, want 200 (real asset)", asset.Code)
	}
	assertSecurityHeaders(t, asset, "static JS asset")

	// A representative API response.
	assertSecurityHeaders(t, get("/v1/hardware"), "API response")
}

// The headers must also ride an ST-1 rejection (403), since the middleware is OUTSIDE
// requireSameOrigin.
func TestSecurityHeaders_OnRejected403(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	req.Host = "evil.example:8080" // rebound → 403 from requireSameOrigin
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("rebound GET: status = %d, want 403", w.Code)
	}
	assertSecurityHeaders(t, w, "ST-1 403")
}
