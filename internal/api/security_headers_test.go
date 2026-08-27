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
// through the real handler stack — the /app→root compat redirect (not followed), the root
// SPA document, a SPA fallback route, a real static JS asset, and a representative API
// response. (W-0003 moved the app to the canonical root, so `/` is now a 200 document and
// the 3xx probe is the permanent /app redirect instead of the old root redirect.)
func TestSecurityHeaders_OnEveryResponse(t *testing.T) {
	srv := testServer(t)

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "127.0.0.1:8080" // loopback: passes the Host allowlist (ST-1)
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		return w
	}

	// The /app→root compatibility redirect — assert on the 3xx itself, not followed.
	appRedirect := get("/app/config")
	if appRedirect.Code != http.StatusMovedPermanently {
		t.Fatalf("GET /app/config: status = %d, want 301 (%s)", appRedirect.Code, appRedirect.Body.String())
	}
	assertSecurityHeaders(t, appRedirect, "app→root redirect")

	assertSecurityHeaders(t, get("/"), "SPA document")
	assertSecurityHeaders(t, get("/some-client-route"), "SPA fallback")

	// A real static asset — nosniff is what makes an incorrect MIME type matter, so cover one.
	asset := get("/assets/index.js")
	if asset.Code != http.StatusOK {
		t.Fatalf("GET /assets/index.js: status = %d, want 200 (real asset)", asset.Code)
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
