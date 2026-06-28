package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validateFt8FieldDay: class is the syntactic rule (types); section is real ARRL/RAC
// membership via go-ft8 (v0.4.0). A bogus-but-well-formed section like "ZZ" must now
// be rejected — the whole point of the strict swap.
func TestValidateFt8FieldDay(t *testing.T) {
	cases := []struct {
		name    string
		fd      *types.Ft8FieldDayConfig
		wantErr bool
	}{
		{"nil → ok", nil, false},
		{"empty → ok", &types.Ft8FieldDayConfig{}, false},
		{"valid class + section", &types.Ft8FieldDayConfig{Class: "2A", Section: "EMA"}, false},
		{"DX section ok", &types.Ft8FieldDayConfig{Class: "1D", Section: "DX"}, false},
		{"class only ok", &types.Ft8FieldDayConfig{Class: "3F"}, false},
		{"section only ok", &types.Ft8FieldDayConfig{Section: "WI"}, false},
		{"bad class", &types.Ft8FieldDayConfig{Class: "99Z"}, true},
		{"bogus section rejected by go-ft8", &types.Ft8FieldDayConfig{Section: "ZZ"}, true},
		{"section too long", &types.Ft8FieldDayConfig{Section: "TOOLONG"}, true},
	}
	for _, c := range cases {
		findings := validateFt8FieldDay(c.fd)
		if c.wantErr && len(findings) == 0 {
			t.Errorf("%s: want a finding, got none", c.name)
		}
		if !c.wantErr && len(findings) != 0 {
			t.Errorf("%s: want no finding, got %+v", c.name, findings)
		}
	}
}
