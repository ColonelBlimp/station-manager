package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ColonelBlimp/station-manager/frontend"
)

// testSPAFS is a synthetic SPA filesystem: an index.html plus a real assets/
// directory (so "assets" resolves to a dir). Self-contained so the 404 / no-
// listing behaviour is asserted independently of the committed dist/ contents.
func testSPAFS() fs.FS {
	return fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<html>Station Manager SPA</html>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
}

// TestSpaHandler_ServesIndexAtRoot confirms a GET / returns the
// embedded SPA's index.html. This catches regressions in the embed
// directive (e.g. dist/ stripped from git, package path renamed) and
// the //go:embed all: prefix.
func TestSpaHandler_ServesIndexAtRoot(t *testing.T) {
	h := spaHandler(frontend.LoggingFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Station Manager") {
		t.Fatalf("body missing expected SPA marker; got: %s", body)
	}
}

// TestSpaHandler_ServesLogbookIndex confirms the logbook SPA's embedded
// index.html is reachable through its filesystem. Guards the committed
// dist/index.html placeholder against being stripped from git (which
// would break the //go:embed all:logbook/dist directive at build time
// and 404 the SPA at runtime).
func TestSpaHandler_ServesLogbookIndex(t *testing.T) {
	h := spaHandler(frontend.LogbookFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Logbook") {
		t.Fatalf("body missing expected logbook SPA marker; got: %s", rec.Body.String())
	}
}

// TestSpaHandler_ApiPathReturns404 confirms a /v1/* path that reached the SPA
// catch-all (no API route matched it — a disabled subsystem or a typo) gets an
// honest 404, NOT an SPA-fallback 200 index.html. Otherwise a disabled
// GET /v1/rig/events would 200-HTML and mislead every curl/EventSource/fetch
// consumer (review 2026-07-05 internal/api finding 2).
func TestSpaHandler_ApiPathReturns404(t *testing.T) {
	h := spaHandler(testSPAFS())
	for _, path := range []string{"/v1/rig/events", "/v1/rig/command", "/v1/typo"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (API miss, not SPA fallback)", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Station Manager SPA") {
				t.Fatalf("served SPA index.html for an API path instead of 404")
			}
		})
	}
}

// TestSpaHandler_DirectoryServesIndexNotListing confirms a real directory in the
// embed FS (e.g. /assets) SPA-falls-through to index.html rather than an
// http.FileServer directory listing (a minor disclosure over LAN TCP) or a 301
// redirect (review 2026-07-05 internal/api nit).
func TestSpaHandler_DirectoryServesIndexNotListing(t *testing.T) {
	h := spaHandler(testSPAFS())
	for _, path := range []string{"/assets", "/assets/"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (SPA fallback, not a 301/listing)", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "Station Manager SPA") {
				t.Fatalf("did not serve index.html; body = %q", body)
			}
			if strings.Contains(body, "app.js") {
				t.Fatalf("rendered a directory listing (contains app.js): %q", body)
			}
		})
	}
}

// TestSpaHandler_FallsBackToIndexForUnknownPaths confirms client-side
// router paths like /log or /logbook return index.html instead of 404,
// which is the load-bearing behaviour for SPA refresh-on-deep-link.
func TestSpaHandler_FallsBackToIndexForUnknownPaths(t *testing.T) {
	h := spaHandler(frontend.LoggingFS())

	for _, path := range []string{"/log", "/logbook", "/config", "/some/nested/route"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (SPA fallback)", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), "Station Manager") {
				t.Fatalf("fallback did not serve index.html for %s", path)
			}
		})
	}
}
