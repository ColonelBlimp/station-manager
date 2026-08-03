package lookupdef

import (
	"sort"
	"sync"
)

// ProviderKind separates the two legs of the enrichment pipeline (ADR 0017):
// exactly one COUNTRY provider resolves DXCC / CQ / ITU zones from the prefix,
// while the CALLSIGN chain is operator-ordered and applies first-non-empty-wins.
// The config shape mirrors that split (EnrichmentConfig.Hamnut vs .Chain), so a
// descriptor has to say which leg it belongs to.
type ProviderKind string

const (
	KindCountry  ProviderKind = "country"
	KindCallsign ProviderKind = "callsign"
)

// ProviderDescriptor is everything the daemon and the Settings → Enrichment
// section need to know about a lookup provider WITHOUT hardcoding its name.
//
// It exists because those facts used to be spread across five sites in four
// packages and two languages — buildEnrichment's constructor switch, config's
// seed entry, config's URL defaulting, a credential-requirement predicate in
// internal/types, and a display map in the SPA (ADR 0062). Each answered a
// question only the provider can answer, nothing linked them, and nothing
// failed at compile time when one was missed.
//
// Registration happens in the provider package's init(), so the descriptor sits
// next to the implementation it describes.
type ProviderDescriptor struct {
	// Name is the canonical service name — the key in config.json, the key
	// mergeLookup matches on to carry a stored password across a save, and the
	// key LookupServiceConfig resolves. Renaming it detaches credentials.
	Name string `json:"name"`
	// DisplayName is the built-in human name. The operator's config.json
	// `label` overrides it for display; this is the fallback.
	DisplayName string `json:"display_name"`
	// Help is the one-line description shown under the provider's heading.
	Help string       `json:"help,omitempty"`
	Kind ProviderKind `json:"kind"`

	// NeedsCredentials is false for a provider that is anonymous BY DESIGN
	// (hamnut). When true the config validator refuses to save the provider
	// ENABLED without a usable login — because the provider's own Initialize
	// would reject it at the next start, and that aborts daemon startup long
	// after the PUT returned 200 (ADR 0062 / review 9732ab7914af).
	NeedsCredentials bool `json:"needs_credentials"`
	MinUsernameLen   int  `json:"min_username_len,omitempty"`
	MinPasswordLen   int  `json:"min_password_len,omitempty"`

	// Defaults stamped by config.Normalize when the operator left them blank,
	// so a new provider is frictionless to enable: credentials only, never a
	// URL. Empty DefaultURL means the provider has no canonical endpoint and
	// the operator must supply one.
	DefaultURL        string `json:"default_url,omitempty"`
	DefaultViewURL    string `json:"default_view_url,omitempty"`
	DefaultTimeoutSec int    `json:"default_timeout_sec,omitempty"`
}

var (
	registryMu  sync.RWMutex
	descriptors = map[string]ProviderDescriptor{}
)

// RegisterProvider records a provider's descriptor. Call it from the provider
// package's init(); cmd/smd imports the package to trigger it, exactly as it
// does for forwarder types.
//
// Panics on programmer errors — an empty name, an empty display name, an
// unknown kind, or a duplicate registration. All are binary bugs: two packages
// claiming one name means link order decides how the provider behaves, and a
// descriptor missing the fields every consumer reads would surface as a blank
// row in the operator's Settings list. Failing at startup beats either.
func RegisterProvider(d ProviderDescriptor) {
	if d.Name == "" {
		panic("lookupdef.RegisterProvider: empty provider name")
	}
	if d.DisplayName == "" {
		panic("lookupdef.RegisterProvider: empty display name for " + d.Name)
	}
	if d.Kind != KindCountry && d.Kind != KindCallsign {
		panic("lookupdef.RegisterProvider: bad kind " + string(d.Kind) + " for " + d.Name)
	}

	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := descriptors[d.Name]; dup {
		panic("lookupdef.RegisterProvider: duplicate provider " + d.Name)
	}
	descriptors[d.Name] = d
}

// Descriptor returns a provider's descriptor and whether it is registered.
//
// A MISS IS NORMAL, not a fault: internal/config reads this registry and its
// own tests never import a provider package, so the registry is empty there.
// Every caller must branch on ok rather than use a zero descriptor, which would
// read as a real provider with no name, no endpoint and no credential rules.
func Descriptor(name string) (ProviderDescriptor, bool) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	d, ok := descriptors[name]
	return d, ok
}

// Descriptors returns every registered provider, sorted by name — the surface
// GET /v1/lookup-types serves. Sorted rather than map order so the Settings
// list does not reshuffle between restarts.
func Descriptors() []ProviderDescriptor {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]ProviderDescriptor, 0, len(descriptors))
	for _, d := range descriptors {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ResetForTests clears the registry. Registration is init()-time and global, so
// a test that registers has to be able to put it back; RegisterProvider
// deliberately has no unregister.
//
// Exported because internal/config's tests need it: they cannot import a
// provider package (internal/lookup/qrz imports internal/config), so they
// register their own descriptors and must clean up after themselves.
func ResetForTests() {
	registryMu.Lock()
	defer registryMu.Unlock()
	descriptors = map[string]ProviderDescriptor{}
}
