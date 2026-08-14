package qrzcq_test

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/lookup"
	"github.com/ColonelBlimp/station-manager/internal/lookup/qrzcq"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestLiveQRZCQPremiumAccount is an explicit opt-in smoke test. Point
// QRZCQ_LIVE_CONFIG at an operator config containing the disabled QRZCQ entry
// and supply QRZCQ_PASSWORD through the environment. The test enables a copy
// in memory and never rewrites or prints credentials.
func TestLiveQRZCQPremiumAccount(t *testing.T) {
	path := os.Getenv("QRZCQ_LIVE_CONFIG")
	if path == "" {
		t.Skip("set QRZCQ_LIVE_CONFIG to opt into the premium-account smoke test")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read live config: %v", err)
	}
	var cfg config.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("parse live config: %v", err)
	}
	var providerCfg *types.LookupConfig
	for i := range cfg.Lookup.Chain {
		if cfg.Lookup.Chain[i].Name == qrzcq.ServiceName {
			copy := cfg.Lookup.Chain[i]
			providerCfg = &copy
			break
		}
	}
	if providerCfg == nil {
		t.Fatalf("config has no %q entry", qrzcq.ServiceName)
	}
	if providerCfg.Username == "" {
		t.Fatal("QRZCQ live config entry has no username")
	}
	password := os.Getenv("QRZCQ_PASSWORD")
	if password == "" {
		t.Fatal("QRZCQ_PASSWORD is not set")
	}
	providerCfg.Password = password
	providerCfg.Enabled = true
	userAgent := cfg.UserAgent
	if userAgent == "" {
		userAgent = "station-manager/live-test"
	}
	ctor, ok := lookup.CallsignConstructorFor(qrzcq.ServiceName)
	if !ok {
		t.Fatalf("constructor %q is not registered", qrzcq.ServiceName)
	}
	provider := ctor(logging.NewForWriter(io.Discard), providerCfg, userAgent)
	if err := provider.Initialize(context.Background()); err != nil {
		t.Fatalf("Initialize live provider: %v", err)
	}
	call := os.Getenv("QRZCQ_LIVE_CALL")
	if call == "" {
		call = "7Q8AC"
	}
	station, err := provider.LookupWithContext(context.Background(), call)
	if err != nil {
		t.Fatalf("live LookupWithContext(%s): %v", call, err)
	}
	if station.Call == "" || lookup.IsEmpty(station) {
		t.Fatalf("live lookup returned no substantive station data for %s", call)
	}
	t.Logf("QRZCQ live lookup succeeded for %s (name=%t grid=%t address=%t)",
		station.Call, station.Name != "", station.Gridsquare != "", station.Address != "")
}
