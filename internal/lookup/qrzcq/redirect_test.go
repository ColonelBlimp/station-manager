package qrzcq

// ST-4b — the QRZCQ lookup client (created lazily in Initialize) must carry the
// same-origin redirect policy, since credentials travel in the request URL. Pins the
// production construction path (service.go); the policy + no-leak Do sanitisation are
// proven in internal/securehttp.

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
	_ = s.Initialize(context.Background())
	if s.client == nil {
		t.Fatal("Initialize did not create the lookup client")
	}
	if s.client.CheckRedirect == nil {
		t.Error("Initialize did not install the same-origin redirect policy on the lookup client (ST-4b)")
	}
}
