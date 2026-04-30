package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/frontend"
)

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
