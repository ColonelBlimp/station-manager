package api

// ST-4a (A5) — allow_insecure_http is config-file-only: a PUT /v1/config cannot set or
// clear it, and a PUT that omits it preserves the stored value. This is a strong
// behavioural test: the stored smcloud forwarder is valid ONLY because of the ack (its
// URL is remote http). A PUT that re-sends the forwarder without the ack must still
// succeed (200) and keep the ack — if mergeForwarders dropped it, the startup probe would
// refuse the now-unacknowledged remote-http forwarder and the PUT would 400.

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	_ "github.com/ColonelBlimp/station-manager/internal/forwarding/smcloud" // register "smcloud"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestHandlePutConfig_AllowInsecureHTTPPreservedNotWritable(t *testing.T) {
	creds, err := json.Marshal(map[string]string{"url": "http://cloud.example:8091", "token": "tok-123"})
	if err != nil {
		t.Fatal(err)
	}
	srv := testServerWithCfg(t, func(cfg *config.Config) {
		cfg.Forwarders = []types.ForwarderConfig{{
			Name: "cloud", Type: "smcloud", Enabled: true,
			Credentials: creds, AllowInsecureHTTP: true, // valid ONLY because of the ack
		}}
	})

	// A PUT that re-sends the forwarder (the wire type cannot carry allow_insecure_http)
	// and omits credentials (kept). It does NOT try to enable/disable the ack — it can't.
	body := `{"forwarders":[{"name":"cloud","type":"smcloud","enabled":true}]}`
	w := putConfigSmtp(t, srv, body)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s (ack should have been preserved, keeping the config valid)",
			w.Code, w.Body.String())
	}

	got := srv.cfg.Snapshot().Forwarders
	if len(got) != 1 {
		t.Fatalf("expected 1 forwarder, got %d", len(got))
	}
	if !got[0].AllowInsecureHTTP {
		t.Error("allow_insecure_http was cleared by a PUT that omitted it; it must be preserved")
	}
}
