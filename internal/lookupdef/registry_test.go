package lookupdef

import "testing"

/*
	LOOKUP PROVIDER REGISTRY (ADR 0062).

	CRITERION:

	    Adding an enrichment provider is a package plus a registration — not an
	    edit to buildEnrichment, config's URL defaults, config's credential
	    rules, and the SPA's provider map. What the daemon and the Settings
	    section know about a provider comes from ONE declaration.

	WHY A REGISTRY AND NOT A LOOKUP TABLE SOMEWHERE. The five hardcoded sites
	(ADR 0062's table) all answered questions only the provider itself can
	answer: what is it called, does it need a login, what is its endpoint. Each
	site had to be edited in step, nothing linked them, and nothing failed at
	compile time when one was missed.

	REGISTRATION IS init()-TIME AND PANICS. Same call as
	forwarding.RegisterForwarderType: a bad or duplicate registration is a
	programmer error in the binary, not an operator mistake, so it fails loudly
	at startup rather than producing a provider that half-works.

	R5 IS THE ONE THAT IS EASY TO SKIP. internal/config reads this registry, and
	its own unit tests never import the provider packages — so the registry is
	EMPTY there. Every consumer must therefore behave sanely on a miss rather
	than assuming a descriptor exists; Descriptor returns ok=false and the
	caller decides. See the note in ADR 0062's consequences.
*/

func reset() { ResetForTests() }

func desc(name string) ProviderDescriptor {
	return ProviderDescriptor{
		Name:              name,
		DisplayName:       "Test Provider",
		Kind:              KindCallsign,
		DefaultURL:        "https://example.org/api",
		DefaultTimeoutSec: 10,
	}
}

// R1 — a registered provider is retrievable by name.
func TestRegistry_RegisterAndLookup(t *testing.T) {
	reset()
	RegisterProvider(desc("testprovider"))

	got, ok := Descriptor("testprovider")
	if !ok {
		t.Fatal("Descriptor reported the provider as unknown after registration")
	}
	if got.DisplayName != "Test Provider" || got.DefaultURL != "https://example.org/api" {
		t.Errorf("descriptor not stored intact: %+v", got)
	}
}

// R2 — an UNregistered name is a clean miss, not a zero descriptor that reads
// as a real provider with empty fields. Callers branch on ok.
func TestRegistry_UnknownProviderIsAMiss(t *testing.T) {
	reset()

	if _, ok := Descriptor("nosuchprovider"); ok {
		t.Error("Descriptor reported an unregistered provider as known")
	}
}

// R3 — Descriptors() enumerates everything registered, which is what the
// /v1/lookup-types endpoint serves. Order is by name so the SPA's list is
// stable across restarts rather than map-iteration order.
func TestRegistry_DescriptorsAreSortedByName(t *testing.T) {
	reset()
	// SIX entries, not three: Go randomises map iteration, so with three there
	// is a 1-in-6 chance of accidentally-sorted order and the rule passes by
	// luck that often against an unsorted implementation (measured — 1 pass in
	// 12 runs). Six makes it 1-in-720.
	for _, n := range []string{"zulu", "alpha", "mike", "papa", "bravo", "tango"} {
		RegisterProvider(desc(n))
	}

	got := Descriptors()
	if len(got) != 6 {
		t.Fatalf("Descriptors returned %d entries, want 6", len(got))
	}
	for i, want := range []string{"alpha", "bravo", "mike", "papa", "tango", "zulu"} {
		if got[i].Name != want {
			t.Errorf("Descriptors()[%d].Name = %q, want %q (sorted)", i, got[i].Name, want)
		}
	}
}

// R4 — a duplicate registration is a binary bug and must not be silently
// tolerated: two packages claiming one name means whichever init() ran last
// decides how the provider behaves, which is a coin toss at link order.
func TestRegistry_DuplicateRegistrationPanics(t *testing.T) {
	reset()
	RegisterProvider(desc("dupe"))

	defer func() {
		if recover() == nil {
			t.Error("expected a panic on duplicate registration")
		}
	}()
	RegisterProvider(desc("dupe"))
}

// R4b — and so is a registration missing the fields every consumer reads. A
// nameless or unlabelled provider would reach the SPA as a blank row.
func TestRegistry_IncompleteRegistrationPanics(t *testing.T) {
	for _, tc := range []struct {
		what string
		d    ProviderDescriptor
	}{
		{"empty name", ProviderDescriptor{DisplayName: "X", Kind: KindCallsign}},
		{"empty display name", ProviderDescriptor{Name: "x", Kind: KindCallsign}},
		{"unknown kind", ProviderDescriptor{Name: "x", DisplayName: "X", Kind: "sideways"}},
	} {
		t.Run(tc.what, func(t *testing.T) {
			reset()
			defer func() {
				if recover() == nil {
					t.Errorf("expected a panic for %s", tc.what)
				}
			}()
			RegisterProvider(tc.d)
		})
	}
}

// R5 — THE EMPTY-REGISTRY CASE. internal/config reads this registry and its
// tests never import a provider package, so "no descriptors at all" is a normal
// state, not a fault. Enumerating must yield an empty slice rather than nil-
// panicking, and a miss must stay a miss.
func TestRegistry_EmptyIsUsable(t *testing.T) {
	reset()

	if got := Descriptors(); len(got) != 0 {
		t.Errorf("Descriptors() on an empty registry = %v, want empty", got)
	}
	if _, ok := Descriptor("anything"); ok {
		t.Error("empty registry claimed to know a provider")
	}
}

// R6 — credential requirements travel WITH the provider. This is the fact that
// was in internal/types purely to dodge an import cycle (ADR 0062), and the
// whole point of the registry is that it now lives where it is true.
func TestRegistry_CarriesCredentialRequirements(t *testing.T) {
	reset()
	d := desc("credentialed")
	d.NeedsCredentials = true
	d.MinUsernameLen = 3
	d.MinPasswordLen = 5
	RegisterProvider(d)

	got, ok := Descriptor("credentialed")
	if !ok {
		t.Fatal("provider not registered")
	}
	if !got.NeedsCredentials || got.MinUsernameLen != 3 || got.MinPasswordLen != 5 {
		t.Errorf("credential requirements not carried: %+v", got)
	}
}
