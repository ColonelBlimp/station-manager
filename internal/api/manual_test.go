package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/manual"
)

// TestManualHandler_ServesIndexAtRoot confirms a GET / returns the embedded
// manual's index.html. Guards the //go:embed all:public directive and the
// committed public/index.html placeholder against being stripped from git
// (which would break the build / 404 the manual at runtime).
func TestManualHandler_ServesIndexAtRoot(t *testing.T) {
	h := manualHandler(manual.FS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Operator Manual") {
		t.Fatalf("body missing expected manual marker; got: %s", rec.Body.String())
	}
}

// TestManualHandler_404sUnknownPaths confirms the manual is plain static files
// with NO SPA-router fallback — an unresolved path is a genuine 404, not a
// rewrite to index.html (the key behavioural difference from spaHandler).
func TestManualHandler_404sUnknownPaths(t *testing.T) {
	h := manualHandler(manual.FS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/no-such-page", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (no SPA fallback for static manual)", rec.Code)
	}
}
