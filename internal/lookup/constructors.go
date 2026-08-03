package lookup

import (
	"sort"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
Provider CONSTRUCTOR registry (ADR 0062), the companion to the descriptor
registry in internal/lookupdef.

Why two registries and not one. The descriptor has to be readable from
internal/config (it carries the endpoint defaults and credential rules the
validator applies), and internal/config cannot import THIS package —
internal/lookup pulls in internal/database/sqlite, which imports
internal/config. So descriptors live in a leaf (internal/lookupdef) that
everything can see, while constructors live here, where the provider
interfaces are defined. Each provider's init() registers into both.

The signature deliberately omits *config.Service: every provider's NewService
accepts one only to RESOLVE its own LookupConfig when none was supplied, and
buildEnrichment always supplies one. Leaving it out is what keeps this package
free of internal/config and the whole arrangement acyclic.
*/

// CallsignConstructor builds a callsign-class provider from its resolved
// config. Returning the interface (not the concrete type) is what lets
// buildEnrichment wire any registered provider without knowing its package.
type CallsignConstructor func(logger *logging.Service, cfg *types.LookupConfig, userAgent string) CallsignProvider

// CountryConstructor is the same for the single country-class provider.
type CountryConstructor func(logger *logging.Service, cfg *types.LookupConfig, userAgent string) CountryProvider

var (
	ctorMu        sync.RWMutex
	callsignCtors = map[string]CallsignConstructor{}
	countryCtors  = map[string]CountryConstructor{}
)

// RegisterCallsignProvider records a chain provider's constructor. Call from the
// provider package's init(); cmd/smd imports the package to trigger it.
// Panics on a duplicate or a nil constructor — both are binary bugs.
func RegisterCallsignProvider(name string, ctor CallsignConstructor) {
	if name == "" || ctor == nil {
		panic("lookup.RegisterCallsignProvider: empty name or nil constructor")
	}
	ctorMu.Lock()
	defer ctorMu.Unlock()
	if _, dup := callsignCtors[name]; dup {
		panic("lookup.RegisterCallsignProvider: duplicate provider " + name)
	}
	callsignCtors[name] = ctor
}

// RegisterCountryProvider records the country provider's constructor.
func RegisterCountryProvider(name string, ctor CountryConstructor) {
	if name == "" || ctor == nil {
		panic("lookup.RegisterCountryProvider: empty name or nil constructor")
	}
	ctorMu.Lock()
	defer ctorMu.Unlock()
	if _, dup := countryCtors[name]; dup {
		panic("lookup.RegisterCountryProvider: duplicate provider " + name)
	}
	countryCtors[name] = ctor
}

// CallsignConstructorFor returns a chain provider's constructor, or ok=false
// when this build has none. A miss is what buildEnrichment turns into a loud
// startup failure: the operator's config names a provider no binary can wire,
// and silently skipping it would degrade enrichment with no explanation.
func CallsignConstructorFor(name string) (CallsignConstructor, bool) {
	ctorMu.RLock()
	defer ctorMu.RUnlock()
	c, ok := callsignCtors[name]
	return c, ok
}

// CountryConstructorFor is the country-leg equivalent.
func CountryConstructorFor(name string) (CountryConstructor, bool) {
	ctorMu.RLock()
	defer ctorMu.RUnlock()
	c, ok := countryCtors[name]
	return c, ok
}

// RegisteredCallsignProviders lists the wirable chain providers, sorted. Used
// by the boundary test that keeps the two registries in step.
func RegisteredCallsignProviders() []string {
	ctorMu.RLock()
	defer ctorMu.RUnlock()
	out := make([]string, 0, len(callsignCtors))
	for n := range callsignCtors {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// RegisteredCountryProviders lists the wirable country providers, sorted.
func RegisteredCountryProviders() []string {
	ctorMu.RLock()
	defer ctorMu.RUnlock()
	out := make([]string, 0, len(countryCtors))
	for n := range countryCtors {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// ResetConstructorsForTests clears both constructor maps.
func ResetConstructorsForTests() {
	ctorMu.Lock()
	defer ctorMu.Unlock()
	callsignCtors = map[string]CallsignConstructor{}
	countryCtors = map[string]CountryConstructor{}
}
