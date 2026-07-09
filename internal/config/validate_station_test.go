package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// validateStationPrefs.operating_bands: empty means "all bands" (the SPA
// defaults), a valid list passes, and an unknown or duplicated band is a config
// typo that must be rejected before it renders a dead selector button.
func TestValidateStationPrefs_OperatingBands(t *testing.T) {
	cases := []struct {
		name    string
		bands   []string
		wantErr bool
	}{
		{"nil → ok (means all bands)", nil, false},
		{"empty → ok", []string{}, false},
		{"valid subset", []string{"80m", "40m", "20m", "15m", "10m"}, false},
		{"valid incl VHF", []string{"20m", "2m", "70cm"}, false},
		{"unknown band rejected", []string{"20m", "11m"}, true},
		{"gibberish rejected", []string{"banana"}, true},
		{"duplicate rejected", []string{"20m", "40m", "20m"}, true},
	}
	for _, c := range cases {
		findings := validateStationPrefs(types.StationConfig{
			AmpMultiplier:  1, // keep the amp/power guards happy — isolate the band check
			OperatingBands: c.bands,
		})
		if c.wantErr && len(findings) == 0 {
			t.Errorf("%s: want a finding, got none", c.name)
		}
		if !c.wantErr && len(findings) != 0 {
			t.Errorf("%s: want no finding, got %+v", c.name, findings)
		}
	}
}
