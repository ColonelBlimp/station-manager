package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestHandleGetConfig_DecodeLog: GET serves the ft8_decode_log block for the FT8
// tab. A configured block round-trips; a nil block (never enabled) is served as a
// disabled zero value so the SPA form still binds.
func TestHandleGetConfig_DecodeLog(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Ft8.DecodeLog = &types.Ft8DecodeLogConfig{Enabled: true, Path: "/tmp/all.txt"}
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
	if resp.Ft8DecodeLog == nil {
		t.Fatal("ft8_decode_log block missing from GET")
	}
	if !resp.Ft8DecodeLog.Enabled || resp.Ft8DecodeLog.Path != "/tmp/all.txt" {
		t.Errorf("ft8_decode_log not round-tripped: %+v", resp.Ft8DecodeLog)
	}
}

// TestHandleGetConfig_DecodeLogNilServedAsZero: a config with no decode_log block
// still serves a (disabled) block so the SPA form binds.
func TestHandleGetConfig_DecodeLogNilServedAsZero(t *testing.T) {
	srv := testServer(t)

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
	if resp.Ft8DecodeLog == nil {
		t.Fatal("ft8_decode_log should be served as a zero block, got nil")
	}
	if resp.Ft8DecodeLog.Enabled {
		t.Errorf("ft8_decode_log should be disabled by default: %+v", resp.Ft8DecodeLog)
	}
}

// TestHandlePutConfig_DecodeLogRoundTrip: a PUT persists the block; the GET in the
// PUT response reflects it.
func TestHandlePutConfig_DecodeLogRoundTrip(t *testing.T) {
	srv := testServer(t)

	body := `{"ft8_decode_log":{"enabled":true,"path":"/var/log/ft8.txt"}}`
	w := putConfigSmtp(t, srv, body) // shared PUT helper (handler_config_smtp_test.go)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Ft8.DecodeLog
	if got == nil || !got.Enabled || got.Path != "/var/log/ft8.txt" {
		t.Errorf("ft8_decode_log not persisted: %+v", got)
	}

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Ft8DecodeLog == nil || !resp.Ft8DecodeLog.Enabled {
		t.Errorf("ft8_decode_log not in PUT response: %+v", resp.Ft8DecodeLog)
	}
}

// TestHandlePutConfig_DecodeLogPresenceAware: a PUT that omits ft8_decode_log
// leaves the stored block untouched.
func TestHandlePutConfig_DecodeLogPresenceAware(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Ft8.DecodeLog = &types.Ft8DecodeLogConfig{Enabled: true, Path: "/tmp/all.txt"}
	})

	body := `{"logging_station":{"station_callsign":"M0XYZ"}}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Ft8.DecodeLog
	if got == nil || !got.Enabled || got.Path != "/tmp/all.txt" {
		t.Errorf("ft8_decode_log clobbered by a decode-log-less PUT: %+v", got)
	}
}
