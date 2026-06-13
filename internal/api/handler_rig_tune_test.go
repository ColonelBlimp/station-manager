package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// tuneTestServer is testServer with an FTdx10 bridge injected but NOT started —
// the rigdef resolves (so tune is supported) but there's no active serial
// client and no observed rig state. That covers the handler paths reachable
// without hardware; the 202 start happy path needs a live client the bridge
// owns privately and is pinned at the bridge layer (bridge.TestStartTune_Happy).
func tuneTestServer(t *testing.T) *Server {
	t.Helper()
	srv := testServer(t)
	srv.bridge = bridge.New(types.BridgeConfig{
		Enabled: true,
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	return srv
}

func postRigTune(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/v1/rig/tune", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.handleRigTune(w, req)
	return w
}

// TestHandleRigTune covers the request routing + error mapping reachable
// without a live serial client. active:true reaches StartTune and, on a bridge
// that has observed no rig state, maps ErrTuneStateUnknown → 503
// rig_state_unknown. active:false reaches StopTune, an idempotent no-op when
// idle → 202. Bad JSON is a 400.
func TestHandleRigTune(t *testing.T) {
	srv := tuneTestServer(t)

	t.Run("bad json", func(t *testing.T) {
		w := postRigTune(t, srv, `{`)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "invalid_json" {
			t.Errorf("code = %q, want invalid_json", code)
		}
	})

	t.Run("start with unknown rig state", func(t *testing.T) {
		w := postRigTune(t, srv, `{"active":true}`)
		if w.Code != http.StatusServiceUnavailable {
			t.Fatalf("status = %d, want 503 (body %s)", w.Code, w.Body.String())
		}
		if code := decodeErrCode(t, w); code != "rig_state_unknown" {
			t.Errorf("code = %q, want rig_state_unknown", code)
		}
	})

	t.Run("stop is an idempotent no-op", func(t *testing.T) {
		w := postRigTune(t, srv, `{"active":false}`)
		if w.Code != http.StatusAccepted {
			t.Fatalf("status = %d, want 202 (body %s)", w.Code, w.Body.String())
		}
	})
}

// TestBridgeInfoFor_Tune pins the capability flag: BOTH shipped Yaesu rigs
// advertise tune support (rigdef has set_mode/set_power/tx_on/tx_off) so the
// SPA can show the Tune button (ADR 0027). The FT-710 gained tune support
// 2026-06-06; this asserts the API surface reports it the same as the
// bridge-level TuneSupported tests (closes the 2026-06-06 review's L3 drift,
// where only the FTdx10 was pinned here).
func TestBridgeInfoFor_Tune(t *testing.T) {
	for _, driver := range []string{"yaesu-ftdx10", "yaesu-ft710"} {
		cfg := config.DefaultConfig(t.TempDir())
		cfg.Bridge.Enabled = true
		cfg.Bridge.Cat = &types.BridgeCatConfig{Driver: driver}
		if info := bridgeInfoFor(cfg); !info.Tune {
			t.Errorf("BridgeInfo.Tune = false for %s; want true", driver)
		}
	}
}
