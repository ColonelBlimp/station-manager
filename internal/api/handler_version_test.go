package api

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestVersion_HappyPath(t *testing.T) {
	srv := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	w := httptest.NewRecorder()
	srv.handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", w.Code, w.Body.String())
	}

	body := w.Body.String()

	// daemon version: testServer passes "test" into api.New.
	if !strings.Contains(body, `"daemon":"test"`) {
		t.Fatalf("body = %q, want daemon:test", body)
	}

	// go version: runtime.Version() is always non-empty and starts with "go".
	if !strings.Contains(body, `"go":"`+runtime.Version()+`"`) {
		t.Fatalf("body = %q, want go:%q", body, runtime.Version())
	}

	// env: buildinfo.Env defaults to "dev" (the test binary isn't built with the
	// RPM's -X …buildinfo.Env=release), so the SPAs would flag this as a dev build.
	if !strings.Contains(body, `"env":"dev"`) {
		t.Fatalf("body = %q, want env:dev", body)
	}

	// schema version: migrations ran in testServer's setup, so schema should be
	// at the latest migration and not dirty. Bump this with each new migration —
	// currently 6 (0001_init + 0002_relax_rst_length + 0003_allow_time_seconds +
	// 0004_utc_timestamps + 0005_qso_revision + 0006_widen_mode_call).
	if !strings.Contains(body, `"schema":{"version":6,"dirty":false}`) {
		t.Fatalf("body = %q, want schema:{version:6,dirty:false}", body)
	}
}

func TestVersion_ReportsInjectedDaemonVersion(t *testing.T) {
	// Confirm the daemon version field reflects whatever api.New was
	// called with — future-proofs the ldflags-injection path.
	srv := testServer(t)
	srv.daemonVersion = "9.9.9-pretend-release"

	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	w := httptest.NewRecorder()
	srv.handleVersion(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"daemon":"9.9.9-pretend-release"`) {
		t.Fatalf("body = %q, want injected daemon version", w.Body.String())
	}
}
