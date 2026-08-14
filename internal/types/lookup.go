package types

// LookupConfig is one provider's configuration block — used by both
// CountryProvider implementations (hamnut) and CallsignProvider
// implementations (QRZ.com, HamQTH, QRZCQ, …) under ADR 0017.
//
// The operator config holds a slice of these (one per provider). The
// chain runner reads them in priority order; the hamnut leg reads
// the single `Name == HamNutLookupServiceName` entry.
//
// HttpTimeoutSec is in seconds (multiplied by time.Second by the
// provider). Username / Password / ViewURL are optional — empty for
// providers that don't need them (hamnut is anonymous).
//
// User-Agent is intentionally NOT a per-provider field. The daemon's
// global `Config.UserAgent` is used on every outbound HTTP call from
// every provider — there's no operational reason for QRZ vs hamnut
// to identify themselves differently to upstream services.
type LookupConfig struct {
	Name string `json:"name"`
	// Priority is the provider's exclusive authority/order in the callsign
	// chain (ADR 0068). It is unused on the single country-provider block.
	Priority int `json:"priority,omitempty"`
	// Label is the operator's own display name for this source, settable ONLY
	// in config.json — no API surface writes it. The friendly name otherwise
	// lives in the SPA's provider map, so changing it is a build + deploy and
	// the operator cannot do it at all; a source this build does not recognise
	// has no friendly name whatsoever and displays its raw id, which a label is
	// the only way to fix without shipping a new binary.
	//
	// Deliberately NOT Name: Name is the key mergeLookup matches on to carry a
	// provider's stored password across a save, and the key
	// LookupServiceConfig resolves at startup. Renaming it silently detaches
	// the credentials. Nothing joins on Label.
	Label          string `json:"label,omitempty"`
	Enabled        bool   `json:"enabled"`
	URL            string `json:"url"`
	Username       string `json:"username"`
	Password       string `json:"password"`
	HttpTimeoutSec int    `json:"timeout_sec"`
	ViewURL        string `json:"view_url,omitempty"`
}

// EnrichmentConfig is the daemon's enrichment-pipeline configuration
// per ADR 0017. Sits at the top level of the daemon Config under the
// `lookup` JSON key.
//
// Hamnut is a single block (one country provider per daemon — country
// source-of-truth doesn't fan out). The Chain is the operator-ordered
// list of callsign-class providers (QRZ.com, HamQTH, QRZCQ, …). Each entry's
// explicit Priority is authoritative; runtime sorts numerically, then filters
// disabled providers. ADR 0068's ContinueIfBlank policy decides whether a
// lower provider runs, and lower results fill blanks only. An empty Chain is
// valid — it simply means no callsign-class enrichment runs, and cold-station
// Tabs return empty station data.
//
// TTLs are in days (operator-friendly unit; the config Service
// converts to time.Duration via accessors).
//
// The two TTLs are POINTERS because absent and explicitly-zero are
// different instructions and only a pointer can carry both: nil means
// "use the default" (365 / 90, filled by config.Normalize), while an
// explicit 0 means "trust this cache indefinitely" — the reading
// lookup.Orchestrator.isStale has always applied to a non-positive
// TTL. Until 2026-08-03 the field was a plain int and applyDefaults
// stamped 365/90 over any zero, so an operator who set 0 got what they
// asked for until the next restart and then silently got a year.
//
// RefreshMaxInFlight stays a plain int on purpose: zero falls through
// to the refresher package default in BOTH the accessor and the
// defaults pass, so it has no absent-vs-zero conflict to express.
type EnrichmentConfig struct {
	Hamnut LookupConfig   `json:"hamnut"`
	Chain  []LookupConfig `json:"chain,omitempty"`
	// ContinueIfBlank names the callsign fields that justify consulting the
	// next provider. An explicit empty list retains ADR 0017's legacy
	// first-substantive-result behaviour; nil is normalised to ADR 0068's
	// initial name + gridsquare policy.
	ContinueIfBlank    []string `json:"continue_if_blank"`
	CountryTTLDays     *int     `json:"country_ttl_days,omitempty"`
	StationTTLDays     *int     `json:"station_ttl_days,omitempty"`
	RefreshMaxInFlight int      `json:"refresh_max_in_flight"`
}
