package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Deleting the LAST rig is one PUT carrying an empty `rigs` list and
// default_rig_id 0 (fresh-install ruling 2026-09-26). With CAT off the daemon
// accepts it — the rig-less exception in validateRigs. With CAT on it refuses
// (validateBridge needs a port and driver from the active rig), so the SPA must
// not offer the delete until CAT is turned off.
func TestHandlePutConfig_DeleteLastRig(t *testing.T) {
	for _, tc := range []struct {
		name     string
		catOn    bool
		wantCode int
	}{
		{"CAT off clears rigs and default", false, http.StatusOK},
		{"CAT on is refused, nothing written", true, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testServer(t)
			if _, err := srv.cfg.Update(func(c *config.Config) error {
				c.Rigs = []types.RigConfig{{ID: 1, Model: "icom-ic7300", Port: "/dev/ttyUSB0"}}
				c.DefaultRigID = 1
				c.Bridge.Enabled = tc.catOn
				return nil
			}); err != nil {
				t.Fatalf("seed one rig: %v", err)
			}
			req := httptest.NewRequest(http.MethodPut, "/v1/config",
				strings.NewReader(`{"rigs":[],"default_rig_id":0}`))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			srv.handlePutConfig(w, req)
			if w.Code != tc.wantCode {
				t.Fatalf("PUT status = %d, want %d; body = %s", w.Code, tc.wantCode, w.Body.String())
			}
			if !tc.catOn == strings.Contains(w.Body.String(), `"invalid_bridge"`) {
				t.Fatalf("refusal reason: body = %s, want invalid_bridge only when CAT is on", w.Body.String())
			}
			cfg := srv.cfg.Snapshot()
			wantRigs, wantDefault := 0, int64(0)
			if tc.wantCode != http.StatusOK {
				wantRigs, wantDefault = 1, 1
			}
			if len(cfg.Rigs) != wantRigs || cfg.DefaultRigID != wantDefault {
				t.Fatalf("after PUT: %d rigs, default %d; want %d rigs, default %d",
					len(cfg.Rigs), cfg.DefaultRigID, wantRigs, wantDefault)
			}
		})
	}
}
