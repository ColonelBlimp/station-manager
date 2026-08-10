package config

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validateEvidence: the evidence block's cap floor (spot-network §4.1,
// operator 2026-08-10). The rule fires only when capture is ENABLED — a
// disabled block is inert consent state, and flagging its cap would nag
// operators who never opted in. The floor is types.EvidenceMinCapBytes:
// the writer reserves 16 MiB headroom below the cap, so anything at or
// under the floor would leave capture nowhere to write before dropping.
func TestValidateEvidence(t *testing.T) {
	cases := []struct {
		name    string
		in      types.EvidenceConfig
		wantErr bool
	}{
		{"disabled, zero cap → inert", types.EvidenceConfig{}, false},
		{"disabled, absurd cap → still inert", types.EvidenceConfig{CapBytes: 12}, false},
		{"enabled at the default", types.EvidenceConfig{Capture: true, CapBytes: 524288000}, false},
		{"enabled exactly at the floor", types.EvidenceConfig{Capture: true, CapBytes: types.EvidenceMinCapBytes}, false},
		{"enabled below the floor", types.EvidenceConfig{Capture: true, CapBytes: types.EvidenceMinCapBytes - 1}, true},
		{"enabled with a hand-typed tiny cap", types.EvidenceConfig{Capture: true, CapBytes: 1000}, true},
		{"enabled negative", types.EvidenceConfig{Capture: true, CapBytes: -1}, true},
	}
	for _, c := range cases {
		findings := validateEvidence(c.in)
		if c.wantErr && len(findings) == 0 {
			t.Errorf("%s: want a finding, got none", c.name)
		}
		if !c.wantErr && len(findings) != 0 {
			t.Errorf("%s: unexpected findings %v", c.name, findings)
		}
		for _, f := range findings {
			if f.Warning {
				t.Errorf("%s: finding must be an error, got warning", c.name)
			}
			if !strings.Contains(f.Field, "evidence") {
				t.Errorf("%s: finding field %q must name the evidence block", c.name, f.Field)
			}
		}
	}
}

// The rule must be WIRED into Validate — a correct validateEvidence that
// nothing calls protects nobody (Load and PUT /v1/config both go through
// Validate).
func TestValidate_CarriesEvidenceFinding(t *testing.T) {
	cfg := Config{Evidence: types.EvidenceConfig{Capture: true, CapBytes: 1000}}
	found := false
	for _, f := range Validate(cfg) {
		if f.Code == "evidence_cap_too_small" {
			found = true
		}
	}
	if !found {
		t.Fatal("Validate must surface the evidence cap finding")
	}
}

// The default cap fills only when unset, is the exact 500 MiB byte count,
// and NEVER touches the consent flag itself.
func TestEvidenceDefaults(t *testing.T) {
	cfg := Config{}
	applyDefaults(&cfg, t.TempDir())
	if cfg.Evidence.CapBytes != 524288000 {
		t.Fatalf("default cap = %d, want 524288000 (500 MiB exactly)", cfg.Evidence.CapBytes)
	}
	if cfg.Evidence.Capture {
		t.Fatal("defaults must never enable capture (consent layer)")
	}

	cfg = Config{Evidence: types.EvidenceConfig{Capture: true, CapBytes: 1 << 30}}
	applyDefaults(&cfg, t.TempDir())
	if cfg.Evidence.CapBytes != 1<<30 || !cfg.Evidence.Capture {
		t.Fatalf("operator values overwritten: %+v", cfg.Evidence)
	}
}
