package config

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	CONFIG READS THE PROVIDER REGISTRY, NOT A LIST OF NAMES (ADR 0062).

	CRITERION:

	    A lookup provider's endpoint defaults and credential rules come from the
	    provider itself. Adding one does not mean editing config.

	These replace two hardcoded sites: normalizeLookupURLs stamped QRZ's and
	hamnut's URLs by name, and validateLookupProvider asked
	types.LookupProviderNeedsCredentials — a predicate that existed only to dodge
	an import cycle and hardcoded QRZ.

	THE EMPTY REGISTRY IS THE CASE TO GET RIGHT, and it is why these tests
	register their own descriptors instead of importing a provider package. They
	CANNOT import one: internal/lookup/qrz imports internal/config, so the
	dependency only runs one way. In the real daemon cmd/smd imports the
	providers and the registry is populated; here it starts empty, which is
	exactly the state RG4 pins — an unregistered provider must be left alone
	rather than defaulted to nothing or refused.

	That is an improvement on what it replaces: each test now STATES which
	providers it assumes instead of silently inheriting two from a hardcoded
	list.
*/

// withProvider registers a descriptor for one test and clears it afterwards.
// The registry is process-global, so leaking a registration would make later
// tests depend on execution order.
func withProvider(t *testing.T, d lookupdef.ProviderDescriptor) {
	t.Helper()
	lookupdef.ResetForTests()
	lookupdef.RegisterProvider(d)
	t.Cleanup(lookupdef.ResetForTests)
}

func callsignDescriptor(name string) lookupdef.ProviderDescriptor {
	return lookupdef.ProviderDescriptor{
		Name:              name,
		DisplayName:       "Test Callsign Provider",
		Kind:              lookupdef.KindCallsign,
		NeedsCredentials:  true,
		MinUsernameLen:    3,
		MinPasswordLen:    5,
		DefaultURL:        "https://example.org/xml",
		DefaultViewURL:    "https://example.org/db/",
		DefaultTimeoutSec: 12,
	}
}

// RG1 — Normalize stamps a registered provider's endpoint defaults. The
// descriptor's values are deliberately NOT QRZ's, so a fix that kept the old
// hardcoded stamping would fail here.
func TestNormalize_StampsRegisteredProviderDefaults(t *testing.T) {
	withProvider(t, callsignDescriptor("testprov"))

	cfg := Config{Lookup: types.EnrichmentConfig{
		Chain: []types.LookupConfig{{Name: "testprov", Enabled: true, Username: "M0ABC", Password: "s3cret"}},
	}}
	Normalize(&cfg)

	got := cfg.Lookup.Chain[0]
	if got.URL != "https://example.org/xml" {
		t.Errorf("URL = %q, want the descriptor's default", got.URL)
	}
	if got.ViewURL != "https://example.org/db/" {
		t.Errorf("ViewURL = %q, want the descriptor's default", got.ViewURL)
	}
	if got.HttpTimeoutSec != 12 {
		t.Errorf("HttpTimeoutSec = %d, want the descriptor's 12", got.HttpTimeoutSec)
	}
}

// RG2 — an operator's own values are never overwritten by the descriptor.
func TestNormalize_KeepsOperatorProviderValues(t *testing.T) {
	withProvider(t, callsignDescriptor("testprov"))

	cfg := Config{Lookup: types.EnrichmentConfig{
		Chain: []types.LookupConfig{{
			Name: "testprov", Enabled: true, Username: "M0ABC", Password: "s3cret",
			URL: "https://mirror.example.net/xml", HttpTimeoutSec: 30,
		}},
	}}
	Normalize(&cfg)

	got := cfg.Lookup.Chain[0]
	if got.URL != "https://mirror.example.net/xml" || got.HttpTimeoutSec != 30 {
		t.Errorf("operator values overwritten: %+v", got)
	}
}

// RG3 — the credential rule comes from the descriptor. Same enabled-with-no-
// password case that used to be hardcoded to QRZ, now driven by a provider this
// build has never heard of.
func TestValidateLookup_UsesRegistryCredentialRules(t *testing.T) {
	withProvider(t, callsignDescriptor("testprov"))

	lc := types.EnrichmentConfig{
		Chain: []types.LookupConfig{{
			Name: "testprov", Enabled: true, Username: "M0ABC", Password: "",
			URL: "https://example.org/xml", HttpTimeoutSec: 12,
		}},
	}
	if err := validateLookup(lc); err == nil {
		t.Error("expected a rejection for an enabled credentialed provider with no password")
	}
}

// RG3b — and a provider the registry says is anonymous enables without one, so
// the rule is genuinely per-provider rather than "everything needs a login".
func TestValidateLookup_AnonymousRegisteredProviderNeedsNoCredentials(t *testing.T) {
	withProvider(t, lookupdef.ProviderDescriptor{
		Name: "anonprov", DisplayName: "Anon", Kind: lookupdef.KindCountry,
		NeedsCredentials: false,
		DefaultURL:       "https://anon.example.org/", DefaultTimeoutSec: 10,
	})

	lc := types.EnrichmentConfig{
		Hamnut: types.LookupConfig{
			Name: "anonprov", Enabled: true,
			URL: "https://anon.example.org/", HttpTimeoutSec: 10,
		},
	}
	if err := validateLookup(lc); err != nil {
		t.Errorf("an anonymous provider must enable without credentials, got: %v", err)
	}
}

// RG4 — THE EMPTY-REGISTRY / UNKNOWN-PROVIDER CASE. A provider config refers to
// that this build does not know must be left ALONE, not defaulted to empty and
// not refused: the operator may be running a config from a newer build, and
// refusing to load it would strand them on a daemon that will not start.
func TestNormalize_LeavesUnregisteredProviderUntouched(t *testing.T) {
	lookupdef.ResetForTests()
	t.Cleanup(lookupdef.ResetForTests)

	cfg := Config{Lookup: types.EnrichmentConfig{
		Chain: []types.LookupConfig{{
			Name: "unknownprov", Enabled: false,
			URL: "https://unknown.example.org/api", HttpTimeoutSec: 7,
		}},
	}}
	Normalize(&cfg)

	got := cfg.Lookup.Chain[0]
	if got.URL != "https://unknown.example.org/api" || got.HttpTimeoutSec != 7 {
		t.Errorf("unregistered provider's settings were disturbed: %+v", got)
	}
}

// RG4b — and validation does not invent credential requirements for it either.
// Guessing "probably needs a login" would refuse to save a perfectly good
// config for a provider the next build supports.
func TestValidateLookup_UnregisteredProviderIsNotAssumedCredentialed(t *testing.T) {
	lookupdef.ResetForTests()
	t.Cleanup(lookupdef.ResetForTests)

	lc := types.EnrichmentConfig{
		Chain: []types.LookupConfig{{
			Name: "unknownprov", Enabled: true,
			URL: "https://unknown.example.org/api", HttpTimeoutSec: 7,
		}},
	}
	if err := validateLookup(lc); err != nil {
		t.Errorf("an unregistered provider must not be assumed to need credentials, got: %v", err)
	}
}
