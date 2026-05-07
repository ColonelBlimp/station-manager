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
type LookupConfig struct {
	Name           string `json:"name"`
	Enabled        bool   `json:"enabled"`
	URL            string `json:"url"`
	Username       string `json:"username,omitempty"`
	Password       string `json:"password,omitempty"`
	UserAgent      string `json:"useragent"`
	HttpTimeoutSec int    `json:"timeout_sec"`
	ViewURL        string `json:"view_url,omitempty"`
}
