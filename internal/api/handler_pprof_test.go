package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// pprofProbe issues a GET against the supplied server and returns the
// recorder. Centralised so the per-test boilerplate stays readable.
func pprofProbe(t *testing.T, srv *Server, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8080" // loopback: the Host allowlist now covers safe methods too (ST-1)
	w := httptest.NewRecorder()
	srv.httpServer.Handler.ServeHTTP(w, req)
	return w
}

// TestPprof_DisabledByDefault pins the safe-by-default behaviour: a
// fresh server (no cfg.Server.EnableProfiling override) does NOT mount
// the pprof handlers. Since the logging SPA was retired (2026-07-21)
// there is no root catch-all, so /debug/pprof/* matches no route and
// returns 404 — a cleaner posture than the old SPA fallthrough.
// Critical: without this assertion, a future refactor that flips the
// default could expose pprof to anyone reachable on the daemon's port —
// leaking heap / goroutine state and offering a DoS surface via
// /debug/pprof/profile?seconds=N.
func TestPprof_DisabledByDefault(t *testing.T) {
	srv := testServer(t) // EnableProfiling stays at its zero value (false)

	w := pprofProbe(t, srv, "/debug/pprof/")
	if w.Code != http.StatusNotFound {
		t.Fatalf("/debug/pprof/ with profiling off: status = %d, want 404; body=%q", w.Code, w.Body.String())
	}
	// Body must not carry pprof markers — the disabled path must not
	// leak any pprof index links / profile descriptions even by
	// accident (e.g., a future bug where pprof handlers get
	// registered unconditionally and the gate only suppresses the
	// log line).
	if strings.Contains(w.Body.String(), "/debug/pprof/heap") {
		t.Errorf("/debug/pprof/ with profiling off leaked pprof markers; body=%q", w.Body.String())
	}
}

// TestPprof_DisabledRejectsAllSubroutes is the belt-and-braces sweep:
// every documented pprof subroute must 404 when EnableProfiling is
// false. Catches any future code path that registers a subset of the
// routes but forgets to gate them all.
func TestPprof_DisabledRejectsAllSubroutes(t *testing.T) {
	srv := testServer(t)

	paths := []string{
		"/debug/pprof/",
		"/debug/pprof/cmdline",
		"/debug/pprof/symbol",
		"/debug/pprof/heap",
		"/debug/pprof/goroutine",
	}
	for _, path := range paths {
		w := pprofProbe(t, srv, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("%s with profiling off: status = %d, want 404", path, w.Code)
		}
	}
}

// TestPprof_EnabledMountsIndex covers the opt-in path: with
// EnableProfiling=true, /debug/pprof/ returns the pprof index page
// (a list of available profile types, distinct from the SPA's HTML).
// The "Types of profiles available" string is stable across recent Go
// stdlib versions and a reliable marker; if a future Go release
// changes the wording, the dual-marker check still catches it via
// the /debug/pprof/heap link.
func TestPprof_EnabledMountsIndex(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.EnableProfiling = true
	})

	w := pprofProbe(t, srv, "/debug/pprof/")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d for /debug/pprof/ when profiling on; want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Types of profiles available") &&
		!strings.Contains(body, "/debug/pprof/heap") {
		t.Errorf("/debug/pprof/ body does not contain pprof index markers; body=%q", body)
	}
}

// TestPprof_EnabledMountsCmdline verifies the cmdline subroute is
// wired. pprof.Cmdline returns text/plain with the test binary's
// executable path — Content-Type is the cleanest discriminator from
// the SPA's text/html fallthrough.
func TestPprof_EnabledMountsCmdline(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.EnableProfiling = true
	})

	w := pprofProbe(t, srv, "/debug/pprof/cmdline")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d for /debug/pprof/cmdline; want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("Content-Type = %q for /debug/pprof/cmdline; want text/plain (pprof handler did not serve)", ct)
	}
}

// TestPprof_EnabledMountsHeapProfile covers the named-profile path
// (heap, goroutine, allocs, etc.). pprof.Index dispatches these via
// the URL suffix; verifying one of them confirms the dispatch is
// wired. Heap is the most operator-relevant during stress testing.
// Content-Type "application/octet-stream" is the gzipped pprof binary
// format — distinct from the SPA's text/html fallthrough.
func TestPprof_EnabledMountsHeapProfile(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.EnableProfiling = true
	})

	w := pprofProbe(t, srv, "/debug/pprof/heap")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d for /debug/pprof/heap; want 200", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Errorf("/debug/pprof/heap returned empty body")
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/octet-stream") {
		t.Errorf("Content-Type = %q for /debug/pprof/heap; want application/octet-stream", ct)
	}
}

// TestPprof_EnabledMountsSymbolGetAndPost verifies both methods on
// the symbol endpoint resolve to the pprof handler. Symbol is the one
// pprof route registered for both GET (the operator's curl path) and
// POST (the `go tool pprof` batched-address-resolution path); a
// mistake in either registration would silently fall through to the
// SPA on that method.
func TestPprof_EnabledMountsSymbolGetAndPost(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.EnableProfiling = true
	})

	for _, method := range []string{http.MethodGet, http.MethodPost} {
		req := httptest.NewRequest(method, "/debug/pprof/symbol", nil)
		// `go tool pprof` reaches the daemon on loopback; httptest defaults Host to
		// example.com, which the same-origin CSRF guard rejects for the POST.
		req.Host = "127.0.0.1:8080"
		w := httptest.NewRecorder()
		srv.httpServer.Handler.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("%s /debug/pprof/symbol: status = %d, want 200", method, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s /debug/pprof/symbol: Content-Type = %q, want text/plain", method, ct)
		}
	}
}
