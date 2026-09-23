package api

import (
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// W-0021 slice 3A: with the archive-switch seal up, every FT8 admission route
// answers 409 archive_switch_pending — one code for the SPA to name the cause.
func TestFt8Routes_SealedAdmissionIs409ArchiveSwitchPending(t *testing.T) {
	ft8Svc := ft8.NewService(types.Ft8Config{Enabled: true}, logging.Noop(), t.TempDir())
	srv := testServerWithFt8(t, func(c *config.Config) { c.LoggingStation.StationCallsign = "G0TST" }, ft8Svc)
	if err := ft8Svc.SealTxAdmission(); err != nil {
		t.Fatalf("seal: %v", err)
	}
	cases := []struct {
		name, path, body string
		h                http.HandlerFunc
	}{
		{"arm", "/v1/ft8/tx/arm", `{"armed":true}`, srv.handleFt8TxArm},
		{"send", "/v1/ft8/tx/send", `{"message":"CQ G0XYZ IO91","offset_hz":1500}`, srv.handleFt8TxSend},
		{"claim", "/v1/ft8/claim", `{"mode":"ft4"}`, srv.handleFt8Claim},
		{"qso start", "/v1/ft8/qso/start",
			`{"their_call":"K1ABC","their_grid":"FN42","slot_utc":"2026-06-10T14:30:00Z","offset_hz":1500,"operating_freq_mhz":14.074}`, srv.handleFt8QsoStart},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := postFt8Qso(t, srv, c.path, c.body, c.h)
			if w.Code != http.StatusConflict || decodeErrCode(t, w) != "archive_switch_pending" {
				t.Fatalf("status=%d body=%s, want 409 archive_switch_pending", w.Code, w.Body.String())
			}
		})
	}
}
