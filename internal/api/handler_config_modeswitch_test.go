package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

// GET serves restore_rig_on_mode_switch RESOLVED: true when unset (the default).
func TestHandleGetConfig_RestoreRigOnModeSwitch_DefaultsTrue(t *testing.T) {
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
	if resp.RestoreRigOnModeSwitch == nil || !*resp.RestoreRigOnModeSwitch {
		t.Errorf("unset should resolve to true (default ON); got %v", resp.RestoreRigOnModeSwitch)
	}
}

// GET reflects an explicit false.
func TestHandleGetConfig_RestoreRigOnModeSwitch_ExplicitFalse(t *testing.T) {
	off := false
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.RestoreRigOnModeSwitch = &off
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
	w := httptest.NewRecorder()
	srv.handleGetConfig(w, req)

	var resp ConfigResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.RestoreRigOnModeSwitch == nil || *resp.RestoreRigOnModeSwitch {
		t.Errorf("explicit false should serve false; got %v", resp.RestoreRigOnModeSwitch)
	}
}

// PUT persists the flag; a PUT that omits it leaves the stored value untouched.
func TestHandlePutConfig_RestoreRigOnModeSwitch(t *testing.T) {
	srv := testServer(t)

	// Set false.
	w := putConfigSmtp(t, srv, `{"restore_rig_on_mode_switch":false}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := srv.cfg.Snapshot().RestoreRigOnModeSwitch; got == nil || *got {
		t.Errorf("flag not persisted as false: %v", got)
	}

	// A subsequent PUT that omits it must not clobber the stored false.
	w = putConfigSmtp(t, srv, `{"logging_station":{"station_callsign":"M0XYZ"}}`)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}
	if got := srv.cfg.Snapshot().RestoreRigOnModeSwitch; got == nil || *got {
		t.Errorf("omitted PUT clobbered the flag: %v", got)
	}
}
