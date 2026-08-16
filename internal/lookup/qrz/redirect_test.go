package qrz

// ST-4b — the QRZ lookup client (created lazily in Initialize) must carry the same-origin
// redirect policy, since the session key + password travel in the request URL. This pins
// the PRODUCTION construction path (service.go): a dead loopback URL lets Initialize build
// the client and fail only on the session-key fetch, after which the client must be
// hardened. The redirect POLICY itself and its no-leak Do sanitisation are proven in
// internal/securehttp; here we prove the wiring reaches this client.

import (
	"context"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

func TestInitialize_HardensLookupClient(t *testing.T) {
	s := &Service{
		LoggerService: &logging.Service{},
		Config: &types.LookupConfig{
			Name:           ServiceName,
			Enabled:        true,
			URL:            "http://127.0.0.1:1", // loopback (passes transport policy); refuses fast
			Username:       "tester",
			Password:       "secret",
			HttpTimeoutSec: 5,
		},
		UserAgent: "smd/test",
	}
	// Initialize builds the client BEFORE the session-key fetch, then that fetch fails
	// (nothing listens on :1). The client is left in place regardless (a startup
	// session-key failure does not disable the provider).
	_ = s.Initialize(context.Background())
	if s.client == nil {
		t.Fatal("Initialize did not create the lookup client")
	}
	if s.client.CheckRedirect == nil {
		t.Error("Initialize did not install the same-origin redirect policy on the lookup client (ST-4b)")
	}
}
