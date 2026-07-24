package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/manual"
)

// TestManualHandler_ServesIndexAtRoot confirms a GET / returns the embedded
// manual's index.html — guarding the //go:embed all:public directive and the
// static-serve wiring.
//
// The generated manual is deliberately NOT committed (only public/.gitkeep is),
// so a bare checkout that hasn't run `hugo` / `task manual:build` embeds no
// index.html. Skip there rather than fail: every real build path (task build:smd,
// deploy, rpm, release, hosted CI, and `task ci:local`) runs Hugo first, so this
// still exercises the serve path wherever the manual actually exists.
func TestManualHandler_ServesIndexAtRoot(t *testing.T) {
	if _, err := fs.Stat(manual.FS(), "index.html"); err != nil {
		t.Skip("manual not built (only .gitkeep embedded); run `task manual:build` to exercise this test")
	}
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
