package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	AN EXPLICIT TTL OF ZERO MEANS "NEVER GOES STALE" AND MUST SURVIVE A RESTART.

	Acceptance criterion (operator, 2026-08-03):

	    When I set a cache TTL to 0 — the documented way to say "trust this cache
	    indefinitely" — it still means that after the daemon restarts, and I can
	    tell it apart from having left the field unset, which takes the default.

	THE DEFECT. Two parts of the daemon disagreed about what 0 means:

	  - orchestrator.isStale (internal/lookup/orchestrator.go:613) treats
	    `ttl <= 0` as "trust the cache indefinitely" — deliberate, documented, and
	    what the config SPA's Enrichment tab tells the operator ("0 = never goes
	    stale").
	  - applyDefaults treated 0 as UNSET and stamped 365/90 over it on every Load.

	So setting 0 worked until the next restart and then silently became a year.
	The operator's setting was not rejected, not warned about, just quietly
	reinterpreted — the worst of the three.

	WHY A POINTER AND NOT A SENTINEL. "Absent" and "explicitly zero" are different
	facts and JSON already distinguishes them; a *int carries that distinction for
	free. The alternative — reading -1 as "never stale" — needs validate to start
	accepting a negative it currently rejects as a typo, and puts a magic number in
	the operator's config file. RefreshMaxInFlight deliberately stays a plain int:
	0 means "package default" there in BOTH the accessor and applyDefaults, so it
	has no conflict to resolve.

	WHERE THE RESOLUTION LIVES. Normalize, not applyDefaults — the SMTP lesson
	(normalizeSmtpDefaults) applied again. applyDefaults runs only on Load, so a
	PUT that omits a TTL would otherwise store nil and nil-deref in the accessor.
	Normalize runs on both paths and before Validate.

	T1/T3 are the defect. T2/T4 are what stops the fix becoming "never default
	anything" — the fixture values differ from the defaults so the two cannot
	agree.
*/

func intPtr(v int) *int { return &v }

func writeCfg(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return p
}

// T1 — the defect: an explicit 0 survives Load instead of being stamped to 365.
func TestLoad_ExplicitZeroCountryTTLSurvives(t *testing.T) {
	cfg, err := Load(writeCfg(t, `{"lookup":{"country_ttl_days":0}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Lookup.CountryTTLDays == nil {
		t.Fatal("CountryTTLDays is nil after Load; want an explicit 0")
	}
	if got := *cfg.Lookup.CountryTTLDays; got != 0 {
		t.Errorf("CountryTTLDays = %d, want the operator's explicit 0 (never stale)", got)
	}
}

// T2 — and an ABSENT one still takes the default, so T1 can't be satisfied by
// simply never defaulting.
func TestLoad_AbsentCountryTTLTakesDefault(t *testing.T) {
	cfg, err := Load(writeCfg(t, `{"lookup":{}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Lookup.CountryTTLDays == nil {
		t.Fatal("CountryTTLDays is nil after Load; Normalize should have filled it")
	}
	if got := *cfg.Lookup.CountryTTLDays; got != defaultCountryTTLDays {
		t.Errorf("CountryTTLDays = %d, want the default %d", got, defaultCountryTTLDays)
	}
}

// T3 — the same pair for the station TTL, whose default (90) differs from the
// country one, so a fix that hard-wired a single default would fail here.
func TestLoad_StationTTLZeroSurvivesAndAbsentDefaults(t *testing.T) {
	zero, err := Load(writeCfg(t, `{"lookup":{"station_ttl_days":0}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if zero.Lookup.StationTTLDays == nil {
		t.Fatal("StationTTLDays is nil after Load; want an explicit 0")
	}
	if got := *zero.Lookup.StationTTLDays; got != 0 {
		t.Errorf("StationTTLDays = %d, want an explicit 0", got)
	}

	absent, err := Load(writeCfg(t, `{"lookup":{}}`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if absent.Lookup.StationTTLDays == nil {
		t.Fatal("StationTTLDays is nil after Load; Normalize should have filled it")
	}
	if got := *absent.Lookup.StationTTLDays; got != defaultStationTTLDays {
		t.Errorf("StationTTLDays = %d, want the default %d", got, defaultStationTTLDays)
	}
}

// T4 — the resolution runs in Normalize, the transform the PUT path shares.
// Without this the daemon would nil-deref on a save that omitted a TTL.
func TestNormalize_ResolvesAbsentTTLsAndKeepsExplicitZero(t *testing.T) {
	cfg := Config{Lookup: types.EnrichmentConfig{CountryTTLDays: intPtr(0)}}
	Normalize(&cfg)

	if cfg.Lookup.CountryTTLDays == nil {
		t.Fatal("explicit 0 became nil")
	}
	if got := *cfg.Lookup.CountryTTLDays; got != 0 {
		t.Errorf("explicit 0 was overwritten with %d", got)
	}
	if cfg.Lookup.StationTTLDays == nil {
		t.Fatal("absent StationTTLDays left nil; Normalize should have filled it")
	}
	if got := *cfg.Lookup.StationTTLDays; got != defaultStationTTLDays {
		t.Errorf("absent StationTTLDays = %d, want the default %d", got, defaultStationTTLDays)
	}
}

// T5 — the accessor turns an explicit 0 into a zero Duration, which is what
// orchestrator.isStale reads as "trust the cache indefinitely". Asserting the
// stored int alone would not prove the behaviour the operator asked for.
func TestService_CountryTTLZeroMeansNeverStale(t *testing.T) {
	svc := New(Config{Lookup: types.EnrichmentConfig{
		CountryTTLDays: intPtr(0),
		StationTTLDays: intPtr(0),
	}})
	if got := svc.CountryTTL(); got != 0 {
		t.Errorf("CountryTTL = %v, want 0 (never stale)", got)
	}
	if got := svc.StationTTL(); got != 0 {
		t.Errorf("StationTTL = %v, want 0 (never stale)", got)
	}
}

// T5b — and a nil (a config built in code, never Normalized) still yields the
// default rather than panicking. The accessor is reachable from a Service
// constructed directly, so it cannot assume Normalize has run.
func TestService_TTLAccessorsNilSafe(t *testing.T) {
	svc := New(Config{Lookup: types.EnrichmentConfig{}})
	if got := svc.CountryTTL(); got != time.Duration(defaultCountryTTLDays)*24*time.Hour {
		t.Errorf("CountryTTL = %v, want the default", got)
	}
	if got := svc.StationTTL(); got != time.Duration(defaultStationTTLDays)*24*time.Hour {
		t.Errorf("StationTTL = %v, want the default", got)
	}
}

// T6 — a negative is still a typo, not a meaning.
func TestValidateLookup_StillRejectsNegativeTTLPointer(t *testing.T) {
	if err := validateLookup(types.EnrichmentConfig{CountryTTLDays: intPtr(-1)}); err == nil {
		t.Error("expected an error for a negative country_ttl_days")
	}
	if err := validateLookup(types.EnrichmentConfig{StationTTLDays: intPtr(-1)}); err == nil {
		t.Error("expected an error for a negative station_ttl_days")
	}
}
