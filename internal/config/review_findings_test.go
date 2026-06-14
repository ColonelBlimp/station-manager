package config

import (
	"sync"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// --- M2: operator / owner_callsign normalisation + validation -------------

func TestNormalize_CanonicalisesOperatorAndOwner(t *testing.T) {
	cfg := Config{LoggingStation: types.LoggingStation{
		StationCallsign: "  m0abc  ",
		Operator:        "  g0xyz ",
		OwnerCallsign:   "2e0pqr",
	}}
	Normalize(&cfg)

	if got := cfg.LoggingStation.Operator; got != "G0XYZ" {
		t.Errorf("Operator = %q, want canonicalised %q", got, "G0XYZ")
	}
	if got := cfg.LoggingStation.OwnerCallsign; got != "2E0PQR" {
		t.Errorf("OwnerCallsign = %q, want canonicalised %q", got, "2E0PQR")
	}
}

func TestValidateLoggingStation_OperatorAndOwnerCallsignRules(t *testing.T) {
	t.Run("rejects malformed operator", func(t *testing.T) {
		out := validateLoggingStation(types.LoggingStation{Operator: "ABCDEF"}) // no digit
		if !hasFinding(out, "logging_station.operator") {
			t.Errorf("expected a finding for logging_station.operator; got %+v", out)
		}
	})

	t.Run("rejects malformed owner_callsign", func(t *testing.T) {
		out := validateLoggingStation(types.LoggingStation{OwnerCallsign: "X1"}) // too short
		if !hasFinding(out, "logging_station.owner_callsign") {
			t.Errorf("expected a finding for logging_station.owner_callsign; got %+v", out)
		}
	})

	t.Run("empty operator and owner pass (optional, fall back)", func(t *testing.T) {
		out := validateLoggingStation(types.LoggingStation{StationCallsign: "M0ABC"})
		if len(out) != 0 {
			t.Errorf("empty operator/owner should produce no findings; got %+v", out)
		}
	})

	t.Run("valid operator and owner pass", func(t *testing.T) {
		out := validateLoggingStation(types.LoggingStation{
			StationCallsign: "M0ABC",
			Operator:        "G0XYZ",
			OwnerCallsign:   "2E0PQR",
		})
		if len(out) != 0 {
			t.Errorf("valid identity fields should produce no findings; got %+v", out)
		}
	})
}

func hasFinding(findings []Finding, field string) bool {
	for _, f := range findings {
		if f.Field == field {
			return true
		}
	}
	return false
}

// --- M1: Clone deep-copy independence + accessor/Update race --------------

// TestConfig_Clone_IsIndependent proves Clone returns a config that shares no
// backing storage with the receiver — the property the PUT candidate path
// relies on so Normalize's in-place edits can't corrupt the live config.
func TestConfig_Clone_IsIndependent(t *testing.T) {
	mode := "DATA-U"
	orig := Config{
		Rigs: []types.RigConfig{{
			ID:           1,
			Model:        "yaesu-ftdx10",
			Ft8Mode:      &mode,
			ModeMappings: map[string]types.ModeMapping{"DATA-U": {Mode: "RTTY"}},
		}},
		Forwarders: []types.ForwarderConfig{{
			Name:         "qrz",
			ActionFilter: []string{"insert"},
		}},
	}

	clone := orig.Clone()

	// Mutate every reference-typed field on the clone.
	clone.Rigs[0].Ft8Mode = nil
	clone.Rigs[0].ModeMappings["DATA-U"] = types.ModeMapping{Mode: "MUTATED"}
	clone.Rigs[0].Model = "mutated"
	clone.Forwarders[0].ActionFilter[0] = "mutated"
	clone.Forwarders[0].Name = "mutated"

	// The original must be untouched.
	if orig.Rigs[0].Ft8Mode == nil || *orig.Rigs[0].Ft8Mode != "DATA-U" {
		t.Errorf("orig Ft8Mode was mutated via clone: %v", orig.Rigs[0].Ft8Mode)
	}
	if orig.Rigs[0].ModeMappings["DATA-U"].Mode != "RTTY" {
		t.Errorf("orig ModeMappings was mutated via clone: %+v", orig.Rigs[0].ModeMappings)
	}
	if orig.Rigs[0].Model != "yaesu-ftdx10" {
		t.Errorf("orig Rigs Model was mutated via clone: %q", orig.Rigs[0].Model)
	}
	if orig.Forwarders[0].ActionFilter[0] != "insert" {
		t.Errorf("orig Forwarders ActionFilter was mutated via clone: %q", orig.Forwarders[0].ActionFilter[0])
	}
	if orig.Forwarders[0].Name != "qrz" {
		t.Errorf("orig Forwarders Name was mutated via clone: %q", orig.Forwarders[0].Name)
	}
}

// TestService_UpdateAccessorRace loops Update (write lock, whole-struct
// reassign) against the read-locked accessors. Run under `go test -race` it
// proves the accessors no longer race a concurrent PUT /v1/config. (Without
// the read locks added for review M1, the race detector flags s.Cfg here.)
func TestService_UpdateAccessorRace(t *testing.T) {
	svc := New(Config{
		Forwarders: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true}},
	})
	// Update needs a path to persist to; a temp file keeps the test hermetic.
	svc.SetPath(t.TempDir() + "/config.json")

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(3)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = svc.Update(func(cfg *Config) error {
				// Reassign the very slice the reader reads.
				cfg.Forwarders = []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: i%2 == 0}}
				return nil
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = svc.Forwarders()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = svc.Enrichment()
			_ = svc.CountryTTL()
		}
	}()

	wg.Wait()
}
