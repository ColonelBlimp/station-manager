package config

import (
	"strings"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/lookupdef"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
	AN ENABLED CREDENTIALED PROVIDER MUST BE USABLE, OR THE PUT MUST REFUSE IT.

	Acceptance criterion:

	    When I remove or shorten the credentials of a lookup source that needs
	    them and leave it switched on, the save is refused and I am told why —
	    and I can tell that apart from a save that succeeded, which is what used
	    to happen right up until the daemon failed to start again.

	THE DEFECT (clean-room review 9732ab7914af, P1). Every link verified:

	  1. `password_clear` stores an empty password while the provider stays
	     enabled, and the PUT returns 200.
	  2. validateLookupProvider checked only URL / timeout / https-when-
	     credentialed — nothing about the credentials being USABLE.
	  3. At the next start, qrz's own check (internal/lookup/qrz/internal.go:285)
	     rejects a username under 3 or a password under 5 characters.
	  4. buildEnrichment propagates that, and cmd/smd's run() returns on it — so
	     the DAEMON DOES NOT START. The operator's next restart is a dead
	     station, hours after the save that caused it.

	This is the same shape forwarding.svelte.ts already warns about ("emptying a
	required credential is not a reset — the forwarder's New() rejects it,
	aborting spawnForwarderWorkers at the next restart, with the PUT long since
	returned 200"). The Remove control was carried over from SMTP, where the
	precondition is genuinely different: validateSmtp has never required a
	password, because unauthenticated submission is a legitimate setup. QRZ's is
	not. The lesson is that "the same masked-credential pattern" says nothing
	about whether the credential is OPTIONAL.

	WHERE THE LIMITS LIVE (revised by ADR 0062). They were briefly a pair of
	constants in internal/types, purely to dodge the import cycle
	(internal/lookup/qrz imports internal/config, so config cannot import qrz).
	They now live in QRZ's own registered descriptor, next to the Initialize
	check that enforces the same numbers — one declaration, so the save-time gate
	and the startup gate cannot drift.

	C3/C4 are what stop this becoming "credentials are mandatory": a DISABLED
	provider must still be storable half-configured (that is how an operator
	stages one), and an anonymous provider must never be asked for credentials at
	all.
*/

// qrzDescriptor is what the real QRZ package registers (internal/lookup/qrz's
// init). These tests declare it explicitly because internal/config CANNOT
// import that package — internal/lookup/qrz imports internal/config, so the
// dependency runs one way only and the registry is empty here (ADR 0062).
// Stating it beats inheriting it from a hardcoded list, which is what this
// replaced.
func qrzDescriptor() lookupdef.ProviderDescriptor {
	return lookupdef.ProviderDescriptor{
		Name:              types.QRZLookupServiceName,
		DisplayName:       "QRZ.com",
		Kind:              lookupdef.KindCallsign,
		NeedsCredentials:  true,
		MinUsernameLen:    3,
		MinPasswordLen:    5,
		DefaultURL:        types.QRZLookupDefaultURL,
		DefaultViewURL:    types.QRZLookupDefaultViewURL,
		DefaultTimeoutSec: 10,
	}
}

func qrzProvider(username, password string, enabled bool) types.EnrichmentConfig {
	return types.EnrichmentConfig{
		Hamnut: types.LookupConfig{
			Name: types.HamNutLookupServiceName, Enabled: false,
		},
		Chain: []types.LookupConfig{{
			Name: types.QRZLookupServiceName, Enabled: enabled,
			URL: "https://xmldata.qrz.com/xml/current/", HttpTimeoutSec: 10,
			Username: username, Password: password,
		}},
	}
}

// C1 — the exact path the Remove control creates: enabled, username intact,
// password gone.
func TestValidateLookup_RejectsEnabledQrzWithClearedPassword(t *testing.T) {
	withProvider(t, qrzDescriptor())
	err := validateLookup(qrzProvider("M0ABC", "", true))
	if err == nil {
		t.Fatal("expected an error for an enabled QRZ with no password")
	}
	if !strings.Contains(err.Error(), "password") {
		t.Errorf("error should name the missing password, got: %v", err)
	}
}

// C2 — and the length minimums, so a 2-character username cannot brick startup
// either. Both are asserted because they are separate checks in qrz's own code
// and a fix that mirrored only one would leave the other reachable.
func TestValidateLookup_RejectsEnabledQrzWithShortCredentials(t *testing.T) {
	withProvider(t, qrzDescriptor())
	if err := validateLookup(qrzProvider("M0", "longenough", true)); err == nil {
		t.Error("expected an error for a too-short QRZ username")
	}
	if err := validateLookup(qrzProvider("M0ABC", "abcd", true)); err == nil {
		t.Error("expected an error for a too-short QRZ password")
	}
}

// C3 — a DISABLED provider stays storable with nothing filled in. This is the
// existing "stash a partially-configured entry" behaviour (validateLookupProvider's
// own doc comment) and the fix must not take it away.
func TestValidateLookup_AllowsDisabledQrzWithNoCredentials(t *testing.T) {
	withProvider(t, qrzDescriptor())
	if err := validateLookup(qrzProvider("", "", false)); err != nil {
		t.Errorf("a disabled QRZ with no credentials must remain valid, got: %v", err)
	}
}

// C4 — an anonymous provider is never asked for credentials. Without this the
// rule would be "every enabled provider needs a login", which would make hamnut
// — free and anonymous by design — impossible to enable.
func TestValidateLookup_AllowsEnabledHamnutWithoutCredentials(t *testing.T) {
	withProvider(t, lookupdef.ProviderDescriptor{
		Name: types.HamNutLookupServiceName, DisplayName: "Hamnut",
		Kind: lookupdef.KindCountry, NeedsCredentials: false,
		DefaultURL: types.HamNutLookupDefaultURL, DefaultTimeoutSec: 10,
	})
	lc := types.EnrichmentConfig{
		Hamnut: types.LookupConfig{
			Name: types.HamNutLookupServiceName, Enabled: true,
			URL: types.HamNutLookupDefaultURL, HttpTimeoutSec: 10,
		},
	}
	if err := validateLookup(lc); err != nil {
		t.Errorf("hamnut is anonymous by design and must enable without credentials, got: %v", err)
	}
}

// C5 — a fully-credentialed enabled QRZ still passes, so the guard cannot be
// satisfied by rejecting everything.
func TestValidateLookup_AllowsEnabledQrzWithGoodCredentials(t *testing.T) {
	withProvider(t, qrzDescriptor())
	if err := validateLookup(qrzProvider("M0ABC", "s3cret", true)); err != nil {
		t.Errorf("a properly configured QRZ must validate, got: %v", err)
	}
}
