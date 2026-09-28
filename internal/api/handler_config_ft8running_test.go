package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// GET /v1/config serves ft8_running: whether THIS daemon registered the FT8
// routes at startup. It differs from the saved ft8_enabled between a save and the
// restart that applies it, and the SPA gates its FT links on it (clean-room
// review 3ac5dada P2). Each case pins the field to the real routes: running ⇔
// POST /v1/ft8/claim is served.
func TestHandleGetConfig_Ft8RunningFollowsTheServedRoutes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		svcEnabled  bool // the FT8 service this daemon started with
		savedOn     bool // ft8.enabled in the config, saved since start
		wantRunning bool
	}{
		{"saved off, still running until restart", true, false, true},
		{"saved on, not running until restart", false, true, false},
		{"on and running", true, true, true},
		{"off and not running", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ft8Svc := ft8.NewService(types.Ft8Config{Enabled: tc.svcEnabled}, logging.Noop(), t.TempDir())
			srv := testServerWithFt8(t, func(c *config.Config) { c.Ft8.Enabled = tc.savedOn }, ft8Svc)

			get := httptest.NewRequest(http.MethodGet, "/v1/config", nil)
			get.Host = "127.0.0.1:8080"
			w := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(w, get)
			if w.Code != http.StatusOK {
				t.Fatalf("GET /v1/config = %d: %s", w.Code, w.Body.String())
			}
			var body struct {
				Ft8Enabled *bool `json:"ft8_enabled"`
				Ft8Running *bool `json:"ft8_running"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if body.Ft8Running == nil {
				t.Fatalf("ft8_running missing: %s", w.Body.String())
			}
			if *body.Ft8Running != tc.wantRunning {
				t.Fatalf("ft8_running = %v, want %v", *body.Ft8Running, tc.wantRunning)
			}
			if body.Ft8Enabled == nil || *body.Ft8Enabled != tc.savedOn {
				t.Fatalf("ft8_enabled = %s, want the saved %v", w.Body.String(), tc.savedOn)
			}

			claim := httptest.NewRequest(http.MethodPost, "/v1/ft8/claim", strings.NewReader(`{"mode":"ft8"}`))
			claim.Host = "127.0.0.1:8080"
			claim.Header.Set("Content-Type", "application/json")
			cw := httptest.NewRecorder()
			srv.httpServer.Handler.ServeHTTP(cw, claim)
			if served := cw.Code != http.StatusNotFound; served != tc.wantRunning {
				t.Fatalf("POST /v1/ft8/claim = %d (served %v), but ft8_running = %v", cw.Code, served, tc.wantRunning)
			}
		})
	}
}
