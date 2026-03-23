package hamnut

// PrefixLookupResponse models the successful JSON response from the Hamnut
// /v1/call-signs/prefixes endpoint.
type PrefixLookupResponse struct {
	Status            string `json:"status"`
	Found             bool   `json:"found"`
	Timestamp         string `json:"_t"`
	Continent         string `json:"continent"`
	CountryName       string `json:"countryName"`
	CQZone            int    `json:"cqZone"`
	ITUZone           int    `json:"ituZone"`
	Prefix            string `json:"prefix"`
	PrimaryDXCCPrefix string `json:"primaryDXCCPrefix"`

	// Optional/forward-compatible fields.
	CountryCode string `json:"countryCode,omitempty"`
	TimeOffset  string `json:"timeOffset,omitempty"`
	LocalTime   string `json:"localTime,omitempty"`
}
