package types

// Country carries the country / DXCC / zone enrichment fields for the
// contacted station. It is referenced by types.Qso as the
// `country_details` member and travels inside the qso row's
// additional_data JSON blob.
//
// Every field is tagged `,omitempty` per ADR 0015 so an unenriched
// QSO emits `"country_details":{}` rather than 14 empty-string keys.
// Read-back via json.Unmarshal is unaffected (missing fields → zero
// value). Eliminating the `country_details` member entirely when zero
// would require changing the embed to `*Country` plus nil-checks at
// every assignment site; deferred per ADR 0015's "Alternatives
// considered" until the empty-object pattern becomes a real noise
// complaint.
type Country struct {
	ID                int64  `json:"id,omitempty"`
	Name              string `json:"name,omitempty" hamnut:"countryName"`
	Prefix            string `json:"prefix,omitempty" hamnut:"prefix"`
	Ccode             string `json:"ccode,omitempty" hamnut:"countryCode"`
	Continent         string `json:"continent,omitempty" hamnut:"continent"`
	CQZone            string `json:"cq_zone,omitempty" hamnut:"cqZone"`
	ITUZone           string `json:"itu_zone,omitempty" hamnut:"ituZone"`
	DXCCPrefix        string `json:"dxcc_prefix,omitempty" hamnut:"primaryDXCCPrefix"`
	TimeOffset        string `json:"time_offset,omitempty" hamnut:"timeOffset"`
	ShortPathDistance string `json:"short_path_distance,omitempty"`
	LongPathDistance  string `json:"long_path_distance,omitempty"`
	ShortPathBearing  string `json:"short_path_bearing,omitempty"`
	LongPathBearing   string `json:"long_path_bearing,omitempty"`
	IsNewEntity       bool   `json:"is_new_entity,omitempty"` // Indicates if this QSO is with a new country for the logging station
	LocalTime         string `json:"local_time,omitempty"`
}
