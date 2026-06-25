package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestHandleGetConfig_PskReporter: GET serves the psk_reporter block for the FT8
// tab. Served raw — a configured host/port round-trips; an unset block reports
// the zero value (the SPA renders the production default as a placeholder).
func TestHandleGetConfig_PskReporter(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.PskReporter = types.PskReporterConfig{Enabled: true, Port: 14739}
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp ConfigResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.PskReporter == nil {
		t.Fatal("psk_reporter block missing from GET")
	}
	if !resp.PskReporter.Enabled || resp.PskReporter.Port != 14739 {
		t.Errorf("psk_reporter not round-tripped: %+v", resp.PskReporter)
	}
}

// TestHandlePutConfig_PskReporterRoundTrip: a PUT persists the block; the GET
// that follows reflects it.
func TestHandlePutConfig_PskReporterRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"psk_reporter":{"enabled":true,"host":"report.pskreporter.info","port":14739}}`
	w := putConfigSmtp(t, srv, body) // shared PUT helper (handler_config_smtp_test.go)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().PskReporter
	if !got.Enabled || got.Host != "report.pskreporter.info" || got.Port != 14739 {
		t.Errorf("psk_reporter not persisted: %+v", got)
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.PskReporter == nil || !resp.PskReporter.Enabled || resp.PskReporter.Port != 14739 {
		t.Errorf("psk_reporter not in PUT response: %+v", resp.PskReporter)
	}
}

// TestHandlePutConfig_PskReporterBadPortReturns400: an out-of-range port is
// rejected by validatePskReporter.
func TestHandlePutConfig_PskReporterBadPortReturns400(t *testing.T) {
	srv := testServer(t)

	body := `{"psk_reporter":{"enabled":true,"port":99999}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s; want 400", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "invalid_psk_reporter") {
		t.Errorf("body = %s, want invalid_psk_reporter code", w.Body.String())
	}
}

// TestHandlePutConfig_PskReporterPresenceAware: a PUT that omits psk_reporter
// leaves the stored block untouched.
func TestHandlePutConfig_PskReporterPresenceAware(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.PskReporter = types.PskReporterConfig{Enabled: true, Port: 14739}
	})

	body := `{"logging_station":{"station_callsign":"M0XYZ"}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().PskReporter
	if !got.Enabled || got.Port != 14739 {
		t.Errorf("psk_reporter clobbered by a psk-less PUT: %+v", got)
	}
}
