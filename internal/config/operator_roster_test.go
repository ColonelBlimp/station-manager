package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// --- ADR 0055: operator roster — seed, normalise, validate ----------------

func TestApplyDefaults_SeedsOperatorRosterFromIdentity(t *testing.T) {
	cfg := &Config{LoggingStation: types.LoggingStation{
		Operator: "G0XYZ",
		MyName:   "Marc",
	}}
	applyDefaults(cfg, t.TempDir())
	Normalize(cfg)

	if len(cfg.Operators) != 1 {
		t.Fatalf("roster = %+v, want one seeded entry", cfg.Operators)
	}
	if cfg.Operators[0].Callsign != "G0XYZ" || cfg.Operators[0].Name != "Marc" {
		t.Errorf("seeded operator = %+v, want {G0XYZ, Marc}", cfg.Operators[0])
	}
	if cfg.DefaultOperator != "G0XYZ" {
		t.Errorf("default_operator = %q, want G0XYZ", cfg.DefaultOperator)
	}
}

func TestApplyDefaults_SeedFallsBackToStationCallsign(t *testing.T) {
	// No OPERATOR set → seed from STATION_CALLSIGN.
	cfg := &Config{LoggingStation: types.LoggingStation{StationCallsign: "M0ABC"}}
	applyDefaults(cfg, t.TempDir())
	Normalize(cfg)

	if len(cfg.Operators) != 1 || cfg.Operators[0].Callsign != "M0ABC" {
		t.Fatalf("roster = %+v, want single entry seeded from station_callsign M0ABC", cfg.Operators)
	}
	if cfg.DefaultOperator != "M0ABC" {
		t.Errorf("default_operator = %q, want M0ABC", cfg.DefaultOperator)
	}
}

func TestApplyDefaults_DoesNotClobberExistingRoster(t *testing.T) {
	cfg := &Config{
		LoggingStation: types.LoggingStation{Operator: "G0XYZ"},
		Operators:      []types.Operator{{Callsign: "7Q5MLV"}, {Callsign: "7Q8AC"}},
	}
	applyDefaults(cfg, t.TempDir())

	if len(cfg.Operators) != 2 {
		t.Fatalf("roster resized to %+v; an existing roster must not be reseeded", cfg.Operators)
	}
	// default_operator points at the first existing entry, not the identity.
	if cfg.DefaultOperator != "7Q5MLV" {
		t.Errorf("default_operator = %q, want first roster entry 7Q5MLV", cfg.DefaultOperator)
	}
}

func TestNormalize_UppercasesRoster(t *testing.T) {
	cfg := &Config{
		Operators:       []types.Operator{{Callsign: "  7q5mlv ", Name: " Marc "}},
		DefaultOperator: "7q5mlv",
	}
	Normalize(cfg)

	if got := cfg.Operators[0].Callsign; got != "7Q5MLV" {
		t.Errorf("roster callsign = %q, want canonicalised 7Q5MLV", got)
	}
	if got := cfg.Operators[0].Name; got != "Marc" {
		t.Errorf("roster name = %q, want trimmed Marc", got)
	}
	if got := cfg.DefaultOperator; got != "7Q5MLV" {
		t.Errorf("default_operator = %q, want canonicalised 7Q5MLV", got)
	}
}

func TestValidateOperators_Rules(t *testing.T) {
	t.Run("rejects malformed callsign", func(t *testing.T) {
		out := validateOperators(Config{Operators: []types.Operator{{Callsign: "ABCDEF"}}}) // no digit
		if !hasFinding(out, "operators[0]") {
			t.Errorf("expected a finding for operators[0]; got %+v", out)
		}
	})

	t.Run("rejects duplicate callsign", func(t *testing.T) {
		out := validateOperators(Config{Operators: []types.Operator{{Callsign: "7Q5MLV"}, {Callsign: "7Q5MLV"}}})
		if !hasFinding(out, "operators[1]") {
			t.Errorf("expected a duplicate finding for operators[1]; got %+v", out)
		}
	})

	t.Run("rejects default_operator not in roster", func(t *testing.T) {
		out := validateOperators(Config{
			Operators:       []types.Operator{{Callsign: "7Q5MLV"}},
			DefaultOperator: "7Q8AC",
		})
		if !hasFinding(out, "default_operator") {
			t.Errorf("expected a finding for default_operator; got %+v", out)
		}
	})

	t.Run("valid roster passes", func(t *testing.T) {
		out := validateOperators(Config{
			Operators:       []types.Operator{{Callsign: "7Q5MLV", Name: "Marc"}, {Callsign: "7Q8AC"}},
			DefaultOperator: "7Q5MLV",
		})
		if len(out) != 0 {
			t.Errorf("valid roster should produce no findings; got %+v", out)
		}
	})

	t.Run("empty roster passes", func(t *testing.T) {
		if out := validateOperators(Config{}); len(out) != 0 {
			t.Errorf("empty roster should produce no findings; got %+v", out)
		}
	})
}
