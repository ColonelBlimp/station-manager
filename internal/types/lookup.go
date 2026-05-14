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
	Name           string `json:"name"`
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
// list of callsign-class providers (QRZ.com, HamQTH, QRZCQ, …); the
// orchestrator iterates in slice order and applies first-non-empty-
// wins per ADR 0017 #8. An empty Chain is valid — it simply means no
// callsign-class enrichment runs, and cold-station Tabs return empty
// station data.
//
// TTLs are in days (operator-friendly unit; the config Service
// converts to time.Duration via accessors). RefreshMaxInFlight bounds
// the async-refresh worker (refresher.Service); zero falls through
// to the package default.
type EnrichmentConfig struct {
	Hamnut             LookupConfig   `json:"hamnut"`
	Chain              []LookupConfig `json:"chain,omitempty"`
	CountryTTLDays     int            `json:"country_ttl_days"`
	StationTTLDays     int            `json:"station_ttl_days"`
	RefreshMaxInFlight int            `json:"refresh_max_in_flight"`
}
