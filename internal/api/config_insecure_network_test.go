package api

// ST-3a (docs/reviews/internal-security-trust-boundary-audit.md), operator ruling 3
// (2026-08-16): server.allow_insecure_network is config-file/startup-only and must NOT
// be remotely writable. Server bind settings are intentionally absent from the
// /v1/config wire surface (ConfigResponse has no Server block), so a PUT can neither set
// the acknowledgement nor change the bind — the acknowledgement lives only in config.json.
//
// AC-4: a PUT /v1/config carrying a `server` block (and thus an attempt to flip
// allow_insecure_network / change socket_path) leaves the stored Server block unchanged
// and still returns 200, while a genuinely-writable field in the SAME body IS applied —
// proving the request reached the handler and the server fields were dropped, not that
// the whole PUT was rejected.

import (
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
)

func TestHandlePutConfig_AllowInsecureNetworkNotRemotelyWritable(t *testing.T) {
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Server.Protocol = "tcp"
		cfg.SocketPath = "127.0.0.1:8080"
		cfg.Server.AllowInsecureNetwork = false
	})

	// A body that tries to flip the acknowledgement and repoint the bind, alongside a
	// genuinely-writable field (station callsign) that must take effect.
	body := `{
		"server": {"allow_insecure_network": true, "protocol": "tcp"},
		"socket_path": "0.0.0.0:8080",
		"logging_station": {"station_callsign": "M0XYZ"}
	}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot()
	if got.Server.AllowInsecureNetwork {
		t.Error("PUT set server.allow_insecure_network; the acknowledgement must not be remotely writable")
	}
	if got.SocketPath != "127.0.0.1:8080" {
		t.Errorf("PUT changed socket_path to %q; bind settings must not be remotely writable", got.SocketPath)
	}
	// The writable field DID apply — so the PUT reached the handler and the server
	// fields were dropped, not the whole request rejected.
	if got.LoggingStation.StationCallsign != "M0XYZ" {
		t.Errorf("writable field not applied (station_callsign = %q); PUT may have been rejected wholesale",
			got.LoggingStation.StationCallsign)
	}
}
