package config

import (
	"encoding/json"
	stderrors "errors"
	"strings"
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
// TestValidateRigs_RejectsUnknownModel guards review 2026-06-19 M2: a rig whose
// Model isn't a known cat.Lookup driver must fail config validation at the
// single boundary, not slip through to a runtime unknown_driver bridge error.
// A real model validates; a typo does not.
func TestValidateRigs_RejectsUnknownModel(t *testing.T) {
	hasModelFinding := func(findings []Finding) bool {
		for _, f := range findings {
			if strings.Contains(f.Message, "not a known rig driver") {
				return true
			}
		}
		return false
	}

	good := Config{Rigs: []types.RigConfig{{ID: 1, Model: "yaesu-ftdx10"}}, DefaultRigID: 1}
	if hasModelFinding(Validate(good)) {
		t.Error("a real rig model was flagged as unknown")
	}

	bad := Config{Rigs: []types.RigConfig{{ID: 1, Model: "yaesu-typo"}}, DefaultRigID: 1}
	if !hasModelFinding(Validate(bad)) {
		t.Errorf("unknown rig model was not rejected; findings = %+v", Validate(bad))
	}
}

// TestValidateSmtp_RejectsMalformedFrom guards review 2026-06-19 M2 (config
// side): an enabled SMTP block with a non-mailbox smtp.from fails validation at
// startup/PUT, not only when the operator first sends. A valid mailbox passes.
func TestValidateSmtp_RejectsMalformedFrom(t *testing.T) {
	hasSmtpFinding := func(findings []Finding) bool {
		for _, f := range findings {
			if f.Field == "smtp" {
				return true
			}
		}
		return false
	}

	good := Config{Smtp: types.SmtpConfig{Enabled: true, Host: "smtp.x", From: "ops@example.com", Port: 587, TimeoutSec: 5}}
	if hasSmtpFinding(Validate(good)) {
		t.Error("a valid smtp.from was flagged")
	}

	bad := Config{Smtp: types.SmtpConfig{Enabled: true, Host: "smtp.x", From: "not a mailbox", Port: 587, TimeoutSec: 5}}
	if !hasSmtpFinding(Validate(bad)) {
		t.Errorf("malformed smtp.from was not rejected; findings = %+v", Validate(bad))
	}
}

// TestValidateFt8Occupancy_RejectsBadValues guards review 2026-06-19 M3 (FT8): a
// bad ft8.tx.occupancy block (inverted/over-Nyquist passband, negative weight,
// non-positive threshold) is rejected at config validation, since those values
// shape the clear-offset picker and — once selected — a real TX offset.
func TestValidateFt8Occupancy_RejectsBadValues(t *testing.T) {
	hasOcc := func(findings []Finding) bool {
		for _, f := range findings {
			if strings.HasPrefix(f.Field, "ft8.tx.occupancy") {
				return true
			}
		}
		return false
	}

	good := types.Ft8OccupancyConfig{PassbandLowHz: 300, PassbandHighHz: 2800, ThresholdFactor: 2.0, WeightEdge: 1.5}
	if hasOcc(Validate(Config{Ft8: types.Ft8Config{TX: &types.Ft8TXConfig{Occupancy: &good}}})) {
		t.Error("a valid occupancy block was flagged")
	}

	bads := []struct {
		name string
		o    types.Ft8OccupancyConfig
	}{
		{"inverted passband", types.Ft8OccupancyConfig{PassbandLowHz: 3000, PassbandHighHz: 200}},
		{"high above Nyquist", types.Ft8OccupancyConfig{PassbandHighHz: 7000}},
		{"negative low", types.Ft8OccupancyConfig{PassbandLowHz: -100, PassbandHighHz: 2000}},
		{"passband too narrow", types.Ft8OccupancyConfig{PassbandLowHz: 1000, PassbandHighHz: 1010}},
		{"negative weight", types.Ft8OccupancyConfig{WeightEdge: -1}},
		{"non-positive threshold", types.Ft8OccupancyConfig{ThresholdFactor: -2}},
	}
	for _, b := range bads {
		t.Run(b.name, func(t *testing.T) {
			o := b.o
			cfg := Config{Ft8: types.Ft8Config{TX: &types.Ft8TXConfig{Occupancy: &o}}}
			if !hasOcc(Validate(cfg)) {
				t.Errorf("bad occupancy %q not flagged; findings=%+v", b.name, Validate(cfg))
			}
		})
	}
}

// TestService_Update_NestedMutationRollback guards review 2026-06-19 M3: when an
// Update closure mutates a NESTED value (here, appending to Forwarders) and then
// returns an error, the live in-memory config must be untouched — Update works
// on a deep Clone, not a shallow copy that aliases the live slices.
func TestService_Update_NestedMutationRollback(t *testing.T) {
	svc := New(Config{
		Forwarders: []types.ForwarderConfig{{Name: "qrz", Type: "qrz", Enabled: true}},
	})
	svc.SetPath(t.TempDir() + "/config.json")

	wantErr := stderrors.New("abort")
	err := svc.Update(func(cfg *Config) error {
		cfg.Forwarders = append(cfg.Forwarders, types.ForwarderConfig{Name: "leak", Type: "clublog"})
		cfg.Forwarders[0].Enabled = false // also mutate an existing element in place
		return wantErr
	})
	if !stderrors.Is(err, wantErr) {
		t.Fatalf("Update err = %v, want the abort error", err)
	}

	got := svc.Snapshot().Forwarders
	if len(got) != 1 {
		t.Fatalf("Forwarders len = %d, want 1 (aborted append leaked)", len(got))
	}
	if !got[0].Enabled {
		t.Error("aborted in-place mutation of Forwarders[0].Enabled leaked into live config")
	}
}

// --- L2 (review 2026-06-19): accessor defensive copies are deep ----------

// Forwarders() promises callers can't mutate the live config. The copy must be
// deep: writing through a returned entry's nested Credentials / ActionFilter /
// Retry must not reach s.Cfg, or a future caller following the defensive-copy
// contract could race Update.
func TestForwarders_DeepCopyIsolatesNestedFields(t *testing.T) {
	svc := New(Config{
		Forwarders: []types.ForwarderConfig{{
			Name:         "qrz",
			Type:         "qrz",
			Credentials:  json.RawMessage(`{"key":"orig"}`),
			ActionFilter: []string{"insert"},
			Retry:        &types.RetryConfig{MaxAttempts: 3},
		}},
	})

	out := svc.Forwarders()
	out[0].ActionFilter[0] = "MUTATED"
	out[0].Credentials[2] = 'X'
	out[0].Retry.MaxAttempts = 99

	live := svc.Cfg.Forwarders[0]
	if live.ActionFilter[0] != "insert" {
		t.Errorf("live ActionFilter mutated: %q", live.ActionFilter[0])
	}
	if string(live.Credentials) != `{"key":"orig"}` {
		t.Errorf("live Credentials mutated: %s", live.Credentials)
	}
	if live.Retry.MaxAttempts != 3 {
		t.Errorf("live Retry.MaxAttempts mutated: %d", live.Retry.MaxAttempts)
	}
}

// Enrichment() returns the pipeline config by value; its Chain slice must be a
// copy so a caller can't mutate the live provider list under the lock.
func TestEnrichment_DeepCopyIsolatesChain(t *testing.T) {
	svc := New(Config{
		Lookup: types.EnrichmentConfig{
			Chain: []types.LookupConfig{{Name: "qrz", Enabled: true}},
		},
	})

	out := svc.Enrichment()
	out.Chain[0].Name = "MUTATED"
	out.Chain[0].Enabled = false

	live := svc.Cfg.Lookup.Chain[0]
	if live.Name != "qrz" || !live.Enabled {
		t.Errorf("live Chain entry mutated: %+v", live)
	}
}

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
