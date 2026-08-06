package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validateFt8Audio: the RX level meter's window bounds (dogfood 2026-08-06).
// The rule guards the RESOLVED window, not the sparse fields: a lone low_dbfs
// of -5 silently inverts the default window (-5 > the -10 default high) and
// the meter would then read "too low" and "too high" simultaneously — so the
// check runs after ResolveFt8Audio, exactly the values the SPA will classify
// against. Bounds: each within [-120, 0] (dBFS of int16 audio; the floor is
// the meter's silence value), and low strictly below high.
func TestValidateFt8Audio(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		name    string
		in      *types.Ft8AudioConfig
		wantErr bool
	}{
		{"nil → ok (defaults)", nil, false},
		{"empty → ok (defaults)", &types.Ft8AudioConfig{}, false},
		{"sane window", &types.Ft8AudioConfig{LowDbfs: f(-70), HighDbfs: f(-20)}, false},
		{"low above resolved high inverts the window", &types.Ft8AudioConfig{LowDbfs: f(-5)}, true},
		{"low == high is empty, not a window", &types.Ft8AudioConfig{LowDbfs: f(-30), HighDbfs: f(-30)}, true},
		{"below the silence floor", &types.Ft8AudioConfig{LowDbfs: f(-130)}, true},
		{"above full scale", &types.Ft8AudioConfig{HighDbfs: f(1)}, true},
	}
	for _, c := range cases {
		findings := validateFt8Audio(c.in)
		if c.wantErr && len(findings) == 0 {
			t.Errorf("%s: want a finding, got none", c.name)
		}
		if !c.wantErr && len(findings) != 0 {
			t.Errorf("%s: unexpected findings %v", c.name, findings)
		}
	}
}
