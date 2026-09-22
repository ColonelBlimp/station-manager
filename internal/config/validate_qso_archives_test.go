package config

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// The catalogue rules (ADR 0071 identity model): every entry has a UUIDv7 id,
// unique in the catalogue, a non-empty label and a known ownership; managed
// entries carry NO path (it derives from the id), legacy and external entries
// carry an absolute one; the active and pending selectors name an entry or are
// empty. A rule that let a wrong id through would make the daemon open the wrong
// file, so each is refused as an error finding.
func TestValidate_QsoArchives(t *testing.T) {
	const idA = "019fd5c5-efcc-7193-be4f-1fee532ee315"
	const idB = "019fd5c5-efcc-7193-be4f-1fee532ee316"
	legacy := types.QsoArchiveConfig{ID: idA, Label: "Home", Ownership: types.QsoArchiveOwnershipLegacy, Path: "/data/db/station-manager.db"}
	managed := types.QsoArchiveConfig{ID: idB, Label: "Contest", Ownership: types.QsoArchiveOwnershipManaged}

	cases := []struct {
		name    string
		mutate  func(c *Config)
		wantErr string // substring of the finding message; "" = valid
	}{
		{"valid: legacy + managed, active named", func(c *Config) {
			c.QsoArchives = []types.QsoArchiveConfig{legacy, managed}
			c.ActiveQsoArchiveID = idA
		}, ""},
		{"valid: empty catalogue and empty selectors (pre-adoption)", func(c *Config) {}, ""},
		{"active id names no entry", func(c *Config) {
			c.QsoArchives = []types.QsoArchiveConfig{legacy}
			c.ActiveQsoArchiveID = idB
		}, "active_qso_archive_id"},
		{"pending id names no entry", func(c *Config) {
			c.QsoArchives = []types.QsoArchiveConfig{legacy}
			c.ActiveQsoArchiveID = idA
			c.PendingQsoArchiveID = idB
		}, "pending_qso_archive_id"},
		{"duplicate id", func(c *Config) {
			c.QsoArchives = []types.QsoArchiveConfig{legacy, legacy}
		}, "duplicate"},
		{"id is not a UUIDv7", func(c *Config) {
			bad := legacy
			bad.ID = "not-a-uuid"
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "UUIDv7"},
		{"empty label", func(c *Config) {
			bad := legacy
			bad.Label = " "
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "label"},
		{"unknown ownership", func(c *Config) {
			bad := legacy
			bad.Ownership = "borrowed"
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "ownership"},
		{"managed entry with a path", func(c *Config) {
			bad := managed
			bad.Path = "/somewhere.db"
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "path"},
		{"legacy entry without a path", func(c *Config) {
			bad := legacy
			bad.Path = ""
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "path"},
		{"external entry with a relative path", func(c *Config) {
			bad := legacy
			bad.Ownership = types.QsoArchiveOwnershipExternal
			bad.Path = "db/station-manager.db"
			c.QsoArchives = []types.QsoArchiveConfig{bad}
		}, "absolute"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := DefaultConfig(t.TempDir())
			tc.mutate(&cfg)
			var got []Finding
			for _, f := range Validate(cfg) {
				if !f.Warning && f.Code == "invalid_qso_archive" {
					got = append(got, f)
				}
			}
			if tc.wantErr == "" {
				if len(got) != 0 {
					t.Fatalf("valid catalogue refused: %+v", got)
				}
				return
			}
			if len(got) == 0 {
				t.Fatalf("no invalid_qso_archive finding; want one mentioning %q", tc.wantErr)
			}
			if !strings.Contains(got[0].Message, tc.wantErr) {
				t.Fatalf("finding %q does not mention %q", got[0].Message, tc.wantErr)
			}
		})
	}
}
