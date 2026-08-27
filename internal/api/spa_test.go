package api

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ColonelBlimp/station-manager/frontend"
	"github.com/ColonelBlimp/station-manager/internal/config"
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

// TestRootServesShell confirms GET / now serves the consolidated app's index.html
// directly — the app moved to the canonical root (W-0003), so `/` is the shell,
// not a 302 to the retired /app/ mount. Driven through the full server handler so
// the route registration itself is covered.
func TestRootServesShell(t *testing.T) {
	srv := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "127.0.0.1:8080" // loopback: the Host allowlist now covers safe methods too (ST-1)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Station Manager") {
		t.Fatalf("GET /: body missing SPA marker; got %s", w.Body.String())
	}
}

// TestShellRoutesServeIndex confirms the former legacy-SPA paths are now shell
// routes served by the app's index.html — the 307 redirects to /app/config and
// /app/logbook were dropped when the app reached the canonical root (W-0003), so a
// deep-link reload of any shell route returns index.html for client-side routing.
func TestShellRoutesServeIndex(t *testing.T) {
	srv := testServer(t)
	for _, path := range []string{"/config", "/config/", "/logbook", "/logbook/", "/operate"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "127.0.0.1:8080" // loopback: satisfies the Host allowlist (ST-1)
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s: status = %d, want 200 (shell route; %s)", path, w.Code, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "Station Manager") {
				t.Fatalf("GET %s: body missing SPA marker", path)
			}
		})
	}
}

// TestAppPathsRedirectToRoot confirms the retired /app/ mount's URLs 301-redirect
// (permanent, so saved bookmarks survive) to their canonical-root equivalents,
// preserving the path suffix AND the query string. Registered only inside the
// SPA-serving block.
func TestAppPathsRedirectToRoot(t *testing.T) {
	srv := testServer(t)
	cases := []struct{ path, want string }{
		{"/app", "/"},
		{"/app/", "/"},
		{"/app/config", "/config"},
		{"/app/logbook?dest=clublog", "/logbook?dest=clublog"},
	}
	for _, c := range cases {
		t.Run(c.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, c.path, nil)
			req.Host = "127.0.0.1:8080"
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusMovedPermanently {
				t.Fatalf("GET %s: status = %d, want 301 (%s)", c.path, w.Code, w.Body.String())
			}
			if loc := w.Header().Get("Location"); loc != c.want {
				t.Fatalf("GET %s: Location = %q, want %q", c.path, loc, c.want)
			}
		})
	}
}

// TestSpaRoutesAbsentWhenSPADisabled confirms the SPA, its shell routes, and the
// /app compatibility redirects are ALL registered only inside the SPA-serving
// block: with ServeSPA off (a headless Unix-socket deployment, no browser), every
// browser-facing path is a plain 404 — not HTML, not a redirect.
func TestSpaRoutesAbsentWhenSPADisabled(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		serve := false
		cfg.Server.ServeSPA = &serve
	})
	for _, path := range []string{"/", "/config", "/logbook", "/operate", "/app", "/app/config"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req.Host = "127.0.0.1:8080"
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, req)
			if w.Code != http.StatusNotFound {
				t.Fatalf("GET %s with ServeSPA off: status = %d, want 404 (no SPA routes registered)", path, w.Code)
			}
		})
	}
}

// TestSpaHandler_ServesIndexAtRoot confirms a GET / returns the
// embedded SPA's index.html. This catches regressions in the embed
// directive (e.g. dist/ stripped from git, package path renamed) and
// the //go:embed all: prefix. Uses the app SPA fixture (the logging SPA
// was retired 2026-07-21).
func TestSpaHandler_ServesIndexAtRoot(t *testing.T) {
	h := spaHandler(frontend.AppFS())
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

// TestSpaHandler_ServesAppIndex confirms the consolidated app SPA's (ADR 0044)
// embedded index.html is reachable through its filesystem, and that it carries the
// ROOT base path its canonical-root mount depends on (W-0003 moved the app off the
// /app/ sub-path). Guards both the committed dist/index.html against being stripped
// from git (which would break //go:embed all:app/dist) and a regression of
// `base: '/'` in the app's vite.config.ts (which would 404 the bundle at runtime).
func TestSpaHandler_ServesAppIndex(t *testing.T) {
	h := spaHandler(frontend.AppFS())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "/assets/index.js") {
		t.Fatalf("body missing root base-path marker /assets/index.js; got: %s", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "/app/assets/index.js") {
		t.Fatalf("index.html still carries the retired /app/ base path")
	}
}

// TestSpaHandler_ApiPathReturns404 confirms a /v1/* path that reached the SPA
// catch-all (no API route matched it — a disabled subsystem or a typo) gets an
// honest 404, NOT an SPA-fallback 200 index.html. Otherwise a disabled
// GET /v1/rig/events would 200-HTML and mislead every curl/EventSource/fetch
// consumer (review 2026-07-05 internal/api finding 2).
func TestSpaHandler_ApiPathReturns404(t *testing.T) {
	h := spaHandler(testSPAFS())
	for _, path := range []string{"/v1", "/v1/rig/events", "/v1/rig/command", "/v1/typo"} {
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

// TestSpaHandler_GuardsDebugPprof confirms a /debug/pprof* path reaching the SPA
// catch-all gets an honest 404, never SPA HTML — the same server-namespace
// invariant as /v1/*. This is load-bearing once the app serves at the canonical
// root: with profiling OFF the pprof routes aren't registered, so /debug/pprof*
// falls through here, and it must NOT become a 200 index.html (which would break
// the full-server pprof-off 404 contract in handler_pprof_test.go).
func TestSpaHandler_GuardsDebugPprof(t *testing.T) {
	h := spaHandler(testSPAFS())
	for _, path := range []string{"/debug/pprof/", "/debug/pprof/heap", "/debug/pprof"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
			if rec.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (server namespace, not SPA fallback)", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "Station Manager SPA") {
				t.Fatalf("served SPA index.html for a /debug/pprof path")
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
	h := spaHandler(frontend.AppFS())

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
