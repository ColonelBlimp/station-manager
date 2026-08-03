package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	THE PROVIDER LIST IS NON-SPARSE AND SEEDED FROM THE REGISTRY (ADR 0062).

	CRITERION:

	    A provider compiled into the daemon shows up in Settings → Enrichment,
	    switched off, ready to be given credentials and enabled. I never have to
	    hand-edit config.json to make a new source appear.

	WHY THIS EXISTS (clean-room review 83d595f88838, P1). The registry landed
	without it, and the result defeated its own purpose: a newly registered
	provider appeared in GET /v1/lookup-types but nowhere in config, so the
	section had no row for it and it could not be enabled. "Adding a provider is
	a package plus an import" was only true if the operator ALSO hand-wrote the
	config.json entry.

	It also removes the last hardcoded provider name in the daemon. DefaultConfig
	seeded a QRZ chain entry BY NAME — the one site of the original five that the
	registry commit missed, while claiming all five were gone.

	Same shape as forwarders (ADR 0039): add-missing, DISABLED, so the config is
	non-sparse and enabling a source is a toggle rather than a JSON edit. Seeding
	is a no-op on an empty registry, which is the normal state in this package's
	own tests.
*/

// S1 — a registered provider absent from config is seeded, disabled, with its
// declared defaults.
func TestApplyDefaults_SeedsRegisteredCallsignProvider(t *testing.T) {
	withProvider(t, callsignDescriptor("newprov"))

	cfg := DefaultConfig(t.TempDir())

	var seeded *types.LookupConfig
	for i := range cfg.Lookup.Chain {
		if cfg.Lookup.Chain[i].Name == "newprov" {
			seeded = &cfg.Lookup.Chain[i]
		}
	}
	if seeded == nil {
		t.Fatalf("registered provider was not seeded into the chain: %+v", cfg.Lookup.Chain)
	}
	if seeded.Enabled {
		t.Error("seeded provider is enabled; it must be opt-in like a forwarder seed")
	}
	if seeded.URL != "https://example.org/xml" || seeded.HttpTimeoutSec != 12 {
		t.Errorf("seeded provider did not take its descriptor's defaults: %+v", *seeded)
	}
}

// S2 — an entry the operator already has is neither duplicated nor overwritten.
// The fixture's stored values differ from the descriptor's, so "kept" and
// "re-seeded" cannot look the same.
func TestApplyDefaults_DoesNotDisturbConfiguredProvider(t *testing.T) {
	withProvider(t, callsignDescriptor("newprov"))

	cfg := Config{Lookup: types.EnrichmentConfig{
		Chain: []types.LookupConfig{{
			Name: "newprov", Enabled: true, Username: "M0ABC", Password: "s3cret",
			URL: "https://mirror.example.net/xml", HttpTimeoutSec: 30,
		}},
	}}
	applyDefaults(&cfg, t.TempDir())

	if n := len(cfg.Lookup.Chain); n != 1 {
		t.Fatalf("chain has %d entries, want 1 — the seed duplicated a configured provider", n)
	}
	got := cfg.Lookup.Chain[0]
	if !got.Enabled || got.URL != "https://mirror.example.net/xml" || got.HttpTimeoutSec != 30 {
		t.Errorf("configured provider was overwritten by the seed: %+v", got)
	}
}

// S3 — the country leg has exactly one slot, so a registered country provider
// fills it only when the operator has not named one.
func TestApplyDefaults_SeedsCountryProviderIntoTheSingleSlot(t *testing.T) {
	withProvider(t, lookupdef.ProviderDescriptor{
		Name: "anoncountry", DisplayName: "Anon Country", Kind: lookupdef.KindCountry,
		DefaultURL: "https://anon.example.org/", DefaultTimeoutSec: 9,
	})

	cfg := DefaultConfig(t.TempDir())

	if cfg.Lookup.Hamnut.Name != "anoncountry" {
		t.Errorf("country slot = %q, want the registered provider", cfg.Lookup.Hamnut.Name)
	}
	if cfg.Lookup.Hamnut.Enabled {
		t.Error("seeded country provider is enabled; it must be opt-in")
	}
	// ...and it must not ALSO land in the callsign chain.
	for _, c := range cfg.Lookup.Chain {
		if c.Name == "anoncountry" {
			t.Error("country provider was also seeded into the callsign chain")
		}
	}
}

// S3b — and an operator-named country provider is left alone.
func TestApplyDefaults_KeepsConfiguredCountryProvider(t *testing.T) {
	withProvider(t, lookupdef.ProviderDescriptor{
		Name: "anoncountry", DisplayName: "Anon Country", Kind: lookupdef.KindCountry,
		DefaultURL: "https://anon.example.org/", DefaultTimeoutSec: 9,
	})

	cfg := Config{Lookup: types.EnrichmentConfig{
		Hamnut: types.LookupConfig{Name: "othercountry", Enabled: true, URL: "https://other.example.org/"},
	}}
	applyDefaults(&cfg, t.TempDir())

	if cfg.Lookup.Hamnut.Name != "othercountry" {
		t.Errorf("country slot = %q, want the operator's own", cfg.Lookup.Hamnut.Name)
	}
}

// S4 — an EMPTY registry seeds nothing and must not panic. This is the normal
// state in this package's own tests, and it is also what a build compiled
// without provider packages looks like.
func TestApplyDefaults_EmptyRegistrySeedsNothing(t *testing.T) {
	lookupdef.ResetForTests()
	t.Cleanup(lookupdef.ResetForTests)

	cfg := DefaultConfig(t.TempDir())

	if len(cfg.Lookup.Chain) != 0 {
		t.Errorf("empty registry seeded %d chain entries, want 0: %+v", len(cfg.Lookup.Chain), cfg.Lookup.Chain)
	}
}

// S5 — seeding is idempotent: loading twice must not grow the chain. applyDefaults
// runs on every Load, and the daemon rewrites config.json, so a seed that
// appended unconditionally would add a duplicate entry per restart until
// validateLookup's duplicate-name check refused to start.
func TestApplyDefaults_SeedingIsIdempotent(t *testing.T) {
	withProvider(t, callsignDescriptor("newprov"))

	cfg := DefaultConfig(t.TempDir())
	first := len(cfg.Lookup.Chain)
	applyDefaults(&cfg, t.TempDir())

	if got := len(cfg.Lookup.Chain); got != first {
		t.Errorf("chain grew from %d to %d on a second pass — seeding is not idempotent", first, got)
	}
	if err := validateLookup(cfg.Lookup); err != nil {
		t.Errorf("re-seeded config no longer validates: %v", err)
	}
}
