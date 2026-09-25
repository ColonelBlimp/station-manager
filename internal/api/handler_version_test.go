package api

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
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
	// currently 15 (0001_init + 0002_relax_rst_length + 0003_allow_time_seconds +
	// 0004_utc_timestamps + 0005_qso_revision + 0006_widen_mode_call +
	// 0007_qso_upload_origin + 0008_operator_event + 0009_operator_event_alarm +
	// 0010_qso_upload_upstream_id_generation + 0011_qso_upload_failure_class +
	// 0012_archive_identity + 0013_operator_event_archive + 0014_logbook_destination + 0015_logbook_destination_legacy_name).
	if !strings.Contains(body, `"schema":{"version":15,"dirty":false}`) {
		t.Fatalf("body = %q, want schema:{version:15,dirty:false}", body)
	}
	// No archive adopted in this server: the field is absent, never a fake identity.
	if strings.Contains(body, `"archive"`) {
		t.Fatalf("body = %q, want no archive field before adoption", body)
	}
}

// The active archive's identity (ADR 0071, W-0021 slice 1) is reported beside
// the schema so an operator can see WHICH file the daemon serves.
func TestVersion_ReportsTheActiveArchive(t *testing.T) {
	const id = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.QsoArchives = []types.QsoArchiveConfig{{ID: id, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/station-manager.db"}}
		cfg.ActiveQsoArchiveID = id
	})
	req := httptest.NewRequest(http.MethodGet, "/v1/version", nil)
	w := httptest.NewRecorder()
	srv.handleVersion(w, req)
	body := w.Body.String()
	want := `"archive":{"id":"` + id + `","label":"Home","ownership":"legacy"}`
	if !strings.Contains(body, want) {
		t.Fatalf("body = %q, want %s", body, want)
	}
	if strings.Contains(body, "/data/db") {
		t.Fatalf("body = %q leaks the file path; only id, label and ownership belong on the wire", body)
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
