package config

import (
	stderrs "errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

// ---- defaults ----

func TestDefaultConfig_LookupDefaults(t *testing.T) {
	cfg := DefaultConfig(t.TempDir())
	if cfg.Lookup.CountryTTLDays != 365 {
		t.Errorf("CountryTTLDays = %d, want 365", cfg.Lookup.CountryTTLDays)
	}
	if cfg.Lookup.StationTTLDays != 90 {
		t.Errorf("StationTTLDays = %d, want 90", cfg.Lookup.StationTTLDays)
	}
	if cfg.Lookup.RefreshMaxInFlight != 4 {
		t.Errorf("RefreshMaxInFlight = %d, want 4", cfg.Lookup.RefreshMaxInFlight)
	}
	if cfg.Lookup.Hamnut.Name != types.HamNutLookupServiceName {
		t.Errorf("Hamnut.Name = %q, want canonical %q", cfg.Lookup.Hamnut.Name, types.HamNutLookupServiceName)
	}
	if cfg.Lookup.Hamnut.HttpTimeoutSec != 10 {
		t.Errorf("Hamnut.HttpTimeoutSec = %d, want 10", cfg.Lookup.Hamnut.HttpTimeoutSec)
	}
	if len(cfg.Lookup.Chain) != 1 {
		t.Fatalf("default chain should have one prepopulated entry, got %d", len(cfg.Lookup.Chain))
	}
	qrz := cfg.Lookup.Chain[0]
	if qrz.Name != types.QRZLookupServiceName {
		t.Errorf("default chain[0].Name = %q, want %q", qrz.Name, types.QRZLookupServiceName)
	}
	if qrz.Enabled {
		t.Error("default chain[0].Enabled = true, want false (template entry, operator opts in)")
	}
	if qrz.URL != "https://xmldata.qrz.com/xml/current" {
		t.Errorf("default chain[0].URL = %q, want canonical QRZ XML endpoint", qrz.URL)
	}
	if qrz.HttpTimeoutSec != 10 {
		t.Errorf("default chain[0].HttpTimeoutSec = %d, want 10", qrz.HttpTimeoutSec)
	}
}

func TestLoad_PreservesOperatorTTLs(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	content := `{
		"lookup": {
			"country_ttl_days": 30,
			"station_ttl_days": 7,
			"refresh_max_in_flight": 8
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Lookup.CountryTTLDays != 30 {
		t.Errorf("CountryTTLDays = %d, want 30 (operator value)", cfg.Lookup.CountryTTLDays)
	}
	if cfg.Lookup.StationTTLDays != 7 {
		t.Errorf("StationTTLDays = %d, want 7 (operator value)", cfg.Lookup.StationTTLDays)
	}
	if cfg.Lookup.RefreshMaxInFlight != 8 {
		t.Errorf("RefreshMaxInFlight = %d, want 8 (operator value)", cfg.Lookup.RefreshMaxInFlight)
	}
}

func TestLoad_StampCanonicalHamnutName(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	// Operator omits the `name` field — applyDefaults stamps the
	// canonical service name so LookupServiceConfig() can find it.
	content := `{
		"lookup": {
			"hamnut": {
				"enabled": true,
				"url": "https://api.hamnut.example/v1/call-signs/prefixes",
				"useragent": "smd/test"
			}
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Lookup.Hamnut.Name != types.HamNutLookupServiceName {
		t.Errorf("Hamnut.Name = %q, want %q (defaults must stamp canonical)",
			cfg.Lookup.Hamnut.Name, types.HamNutLookupServiceName)
	}
}

// ---- validation ----

func TestValidateLookup_EmptyChain_OK(t *testing.T) {
	if err := validateLookup(types.EnrichmentConfig{}); err != nil {
		t.Errorf("empty Lookup config should be valid: %v", err)
	}
}

func TestValidateLookup_DisabledProvidersWithEmptyURL_OK(t *testing.T) {
	// Operator may have a partially-configured-but-disabled entry
	// (e.g., placeholder for a service they haven't signed up for
	// yet). Disabled = no validation.
	lc := types.EnrichmentConfig{
		Hamnut: types.LookupConfig{Name: "hamnutlookupservice", Enabled: false},
		Chain: []types.LookupConfig{
			{Name: "qrzlookupservice", Enabled: false},
		},
	}
	if err := validateLookup(lc); err != nil {
		t.Errorf("disabled providers should not validate: %v", err)
	}
}

func TestValidateLookup_EnabledHamnutMissingURL(t *testing.T) {
	lc := types.EnrichmentConfig{
		Hamnut: types.LookupConfig{
			Name:           "hamnutlookupservice",
			Enabled:        true,
			HttpTimeoutSec: 1,
		},
	}
	err := validateLookup(lc)
	if err == nil || !strings.Contains(err.Error(), "url is empty") {
		t.Errorf("expected url-empty error, got %v", err)
	}
}

// User-Agent is no longer a per-provider LookupConfig field; the
// global Config.UserAgent feeds every provider at construction time
// and the non-empty check fires inside each Service.Initialize. See
// hamnut + qrz lookup service tests for the corresponding coverage.

func TestValidateLookup_RejectsDuplicateChainName(t *testing.T) {
	lc := types.EnrichmentConfig{
		Chain: []types.LookupConfig{
			{Name: "qrzlookupservice", Enabled: false},
			{Name: "qrzlookupservice", Enabled: false},
		},
	}
	err := validateLookup(lc)
	if err == nil || !strings.Contains(err.Error(), "duplicate name") {
		t.Errorf("expected duplicate-name error, got %v", err)
	}
}

func TestValidateLookup_RejectsChainEntryCollidingWithHamnut(t *testing.T) {
	// A chain entry named "hamnutlookupservice" would shadow the
	// Hamnut block in LookupServiceConfig lookups — reject loud.
	lc := types.EnrichmentConfig{
		Hamnut: types.LookupConfig{Name: "hamnutlookupservice"},
		Chain: []types.LookupConfig{
			{Name: "hamnutlookupservice", Enabled: false},
		},
	}
	err := validateLookup(lc)
	if err == nil || !strings.Contains(err.Error(), "collides with hamnut") {
		t.Errorf("expected hamnut-collision error, got %v", err)
	}
}

func TestValidateLookup_RejectsNegativeTTL(t *testing.T) {
	lc := types.EnrichmentConfig{CountryTTLDays: -1}
	if err := validateLookup(lc); err == nil {
		t.Error("expected error for negative country_ttl_days")
	}

	lc = types.EnrichmentConfig{StationTTLDays: -1}
	if err := validateLookup(lc); err == nil {
		t.Error("expected error for negative station_ttl_days")
	}

	lc = types.EnrichmentConfig{RefreshMaxInFlight: -1}
	if err := validateLookup(lc); err == nil {
		t.Error("expected error for negative refresh_max_in_flight")
	}
}

// ---- accessors ----

func TestService_TTLAccessors(t *testing.T) {
	svc := New(Config{
		Lookup: types.EnrichmentConfig{
			CountryTTLDays: 365,
			StationTTLDays: 90,
		},
	})
	if got := svc.CountryTTL(); got != 365*24*time.Hour {
		t.Errorf("CountryTTL = %v, want 365 days", got)
	}
	if got := svc.StationTTL(); got != 90*24*time.Hour {
		t.Errorf("StationTTL = %v, want 90 days", got)
	}
}

func TestService_LookupServiceConfig_FindsHamnut(t *testing.T) {
	svc := New(Config{
		Lookup: types.EnrichmentConfig{
			Hamnut: types.LookupConfig{
				Name:    types.HamNutLookupServiceName,
				Enabled: true,
				URL:     "https://hamnut.example/v1",
			},
		},
	})
	got, err := svc.LookupServiceConfig(types.HamNutLookupServiceName)
	if err != nil {
		t.Fatalf("LookupServiceConfig(hamnut): %v", err)
	}
	if got.URL != "https://hamnut.example/v1" {
		t.Errorf("got URL = %q, want hamnut URL", got.URL)
	}
}

func TestService_LookupServiceConfig_FindsChainEntry(t *testing.T) {
	svc := New(Config{
		Lookup: types.EnrichmentConfig{
			Hamnut: types.LookupConfig{Name: types.HamNutLookupServiceName},
			Chain: []types.LookupConfig{
				{Name: "qrzlookupservice", URL: "https://qrz.com/xml"},
				{Name: "hamqthlookupservice", URL: "https://hamqth.example"},
			},
		},
	})
	got, err := svc.LookupServiceConfig("hamqthlookupservice")
	if err != nil {
		t.Fatalf("LookupServiceConfig(hamqth): %v", err)
	}
	if got.URL != "https://hamqth.example" {
		t.Errorf("got URL = %q, want hamqth URL", got.URL)
	}
}

func TestService_LookupServiceConfig_UnknownNameReturnsErrLookupConfigNotFound(t *testing.T) {
	svc := New(Config{
		Lookup: types.EnrichmentConfig{
			Hamnut: types.LookupConfig{Name: types.HamNutLookupServiceName},
		},
	})
	_, err := svc.LookupServiceConfig("nonexistentprovider")
	if !stderrs.Is(err, ErrLookupConfigNotFound) {
		t.Errorf("err = %v, want ErrLookupConfigNotFound", err)
	}
}

// ---- end-to-end via Load ----

func TestLoad_ValidLookupBlock(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	content := `{
		"lookup": {
			"hamnut": {
				"enabled": true,
				"url": "https://api.hamnut.example/v1/call-signs/prefixes",
				"useragent": "smd/test",
				"timeout_sec": 5
			},
			"chain": [
				{
					"name": "qrzlookupservice",
					"enabled": true,
					"url": "https://xmldata.qrz.com/xml/current",
					"useragent": "smd/test",
					"timeout_sec": 5,
					"username": "tester",
					"password": "secret"
				}
			],
			"country_ttl_days": 180,
			"station_ttl_days": 30
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	cfg, err := Load(cfgFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Lookup.Hamnut.Enabled {
		t.Error("Hamnut.Enabled lost on load")
	}
	if len(cfg.Lookup.Chain) != 1 {
		t.Fatalf("Chain length = %d, want 1", len(cfg.Lookup.Chain))
	}
	if cfg.Lookup.Chain[0].Username != "tester" {
		t.Errorf("chain[0].Username = %q, want tester", cfg.Lookup.Chain[0].Username)
	}
	if cfg.Lookup.CountryTTLDays != 180 {
		t.Errorf("CountryTTLDays = %d, want 180", cfg.Lookup.CountryTTLDays)
	}
}

func TestLoad_RejectsInvalidLookupBlock(t *testing.T) {
	// Empty URL on an enabled hamnut block — exercises the
	// Load → validateLookup → validateLookupProvider path
	// end-to-end. UserAgent is no longer a per-provider field, so
	// the previously-tested "empty useragent" case lives on the
	// Service.Initialize side now (see hamnut + qrz lookup tests).
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.json")
	content := `{
		"lookup": {
			"hamnut": {
				"enabled": true,
				"url": "",
				"timeout_sec": 5
			}
		}
	}`
	if err := os.WriteFile(cfgFile, []byte(content), 0o644); err != nil {
		t.Fatalf("writing config: %v", err)
	}
	if _, err := Load(cfgFile); err == nil {
		t.Fatal("expected validation error for empty URL on enabled hamnut")
	}
}

// TestValidateLookup_CredentialedProviderRequiresSecureTransport guards review
// 2026-06-19 M2: a provider carrying credentials (QRZ) must not send them over
// plain http to a remote host. https is required; http is allowed only to
// loopback (for local mocks); non-credentialed providers are unaffected.
func TestValidateLookup_CredentialedProviderRequiresSecureTransport(t *testing.T) {
	creds := func(rawURL string) types.EnrichmentConfig {
		return types.EnrichmentConfig{Chain: []types.LookupConfig{{
			Name: "qrzlookupservice", Enabled: true, URL: rawURL,
			HttpTimeoutSec: 1, Username: "abc", Password: "abcde",
		}}}
	}
	if err := validateLookup(creds("http://xml.qrz.com/")); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("credentialed http (remote) should be rejected, got %v", err)
	}
	if err := validateLookup(creds("https://xml.qrz.com/")); err != nil {
		t.Errorf("credentialed https should pass, got %v", err)
	}
	if err := validateLookup(creds("http://127.0.0.1:8080/")); err != nil {
		t.Errorf("credentialed http to loopback should pass, got %v", err)
	}
	// A provider with no credentials over plain http is fine (e.g. hamnut).
	noCreds := types.EnrichmentConfig{Chain: []types.LookupConfig{{
		Name: "someprovider", Enabled: true, URL: "http://example.com/", HttpTimeoutSec: 1,
	}}}
	if err := validateLookup(noCreds); err != nil {
		t.Errorf("non-credentialed http provider should pass, got %v", err)
	}
}
