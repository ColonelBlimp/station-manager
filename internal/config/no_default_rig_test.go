package config

import (
	"path/filepath"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// No default rig with rigs configured (ruling 2026-09-28): adding creates a rig
// profile, and 'Set as default' is the one act that selects the rig Station
// Manager uses. So rigs with default_rig_id 0 is a valid, deliberate setup state:
// Validate accepts it, and a load does not quietly put rig 1 back (applyDefaults
// used to) — otherwise 'no default' would vanish at the next start.
func TestNoDefaultRig_IsValidAndSurvivesAReload(t *testing.T) {
	rigs := []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10"}, {ID: 2, Model: "icom-ic7300"}}
	for _, f := range Validate(Config{Rigs: rigs}) {
		if f.Field == "default_rig_id" {
			t.Fatalf("rigs with no default rejected: %+v", f)
		}
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	seedConfig(t, path, "OLD")
	svc := New(DefaultConfig(dir))
	svc.SetPath(path)
	if _, err := svc.Update(func(c *Config) error {
		c.Rigs = rigs
		c.DefaultRigID = 0
		return nil
	}); err != nil {
		t.Fatalf("save rigs with no default: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(loaded.Rigs) != 2 || loaded.DefaultRigID != 0 {
		t.Fatalf("after reload: %d rigs, default %d; want 2 rigs, default 0", len(loaded.Rigs), loaded.DefaultRigID)
	}
	if b := loaded.ActiveBridge(); b.Cat.Driver != "" || b.Serial.Port != "" {
		t.Fatalf("no default rig, yet ActiveBridge projects %+v", b)
	}
	if got := loaded.ResolveMyRig(); got != "" {
		t.Fatalf("no default rig, yet MY_RIG resolves to %q", got)
	}
}

// With CAT on, the connection needs the default rig's port and driver, so a
// config that switches CAT on with no default is refused rather than loaded.
func TestNoDefaultRig_RefusedWithCATOn(t *testing.T) {
	cfg := Config{Rigs: []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0"}}}
	cfg.Bridge.Enabled = true
	if err := validateBridge(cfg.ActiveBridge()); err == nil {
		t.Fatal("CAT on with no default rig accepted")
	}
}

// A catalogue with no default rig projects NO rig-owned identity, even when stale
// loose bridge/FT8 fields remain on disk from before the catalogue: those belong to
// a rig, and with rigs configured the catalogue is authoritative. Otherwise CAT
// could validate and bind the old hardware with no default rig at all (review of
// the no-default slice, P2).
func TestNoDefaultRig_StaleLooseFieldsProjectNothing(t *testing.T) {
	cfg := Config{Rigs: []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10", Port: "/dev/ttyUSB0"}}}
	cfg.Bridge.Serial = &types.BridgeSerialConfig{Port: "/dev/old", Overrides: types.RigOverrides{BaudRate: 4800}}
	cfg.Bridge.Cat = &types.BridgeCatConfig{Driver: "icom-ic7300"}
	cfg.Ft8.Device = "Old Codec In"
	cfg.Ft8.TX = &types.Ft8TXConfig{Device: "Old Codec Out", Mode: "USB-D"}

	if b := cfg.ActiveBridge(); b.Cat.Driver != "" || b.Serial.Port != "" || b.Serial.Overrides != (types.RigOverrides{}) {
		t.Fatalf("no default rig, yet ActiveBridge projects driver %q port %q", b.Cat.Driver, b.Serial.Port)
	}
	if f := cfg.ActiveFt8(); f.Device != "" || (f.TX != nil && (f.TX.Device != "" || f.TX.Mode != "")) {
		t.Fatalf("no default rig, yet ActiveFt8 projects device %q tx %+v", f.Device, f.TX)
	}

	cfg.Bridge.Enabled = true
	refused := false
	for _, f := range Validate(cfg) {
		if f.Field == "bridge" || f.Code == "invalid_bridge" {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("CAT on with no default rig validated through stale loose fields: %+v", Validate(cfg))
	}
	// The stored config is untouched: the projection never writes back.
	if cfg.Bridge.Serial.Port != "/dev/old" || cfg.Ft8.TX.Device != "Old Codec Out" {
		t.Fatal("the projection mutated the stored loose fields")
	}
}
