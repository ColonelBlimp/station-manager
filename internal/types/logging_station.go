package types

// LoggingStation carries the ADIF "MY_*" fields describing the station
// logging the QSO. It is one of three structs (alongside QsoDetails
// and ContactedStation) embedded in types.Qso, and the marshalled
// form lands inside the qso row's additional_data JSON blob.
//
// Every field is tagged `,omitempty` per ADR 0015: the blob omits
// empty values uniformly so it carries operator-set data only, not
// "field exists but empty" noise. Read-back via json.Unmarshal is
// unaffected (missing fields → zero value). StationCallsign retains
// its `validate:"required"` rule because validation runs on the
// in-memory struct, not on the marshalled blob.
type LoggingStation struct {
	AntennaAzimuth  string `json:"ant_az,omitempty"` // the bearing from the logging station to the contacted station. Calculated.
	MyAltitude      string `json:"my_altitude,omitempty"`
	MyAntenna       string `json:"my_antenna,omitempty"`
	MyCity          string `json:"my_city,omitempty"`
	MyCountry       string `json:"my_country,omitempty"`
	MyCqZone        string `json:"my_cq_zone,omitempty"`
	MyDXCC          string `json:"my_dxcc,omitempty"`
	MyGridsquare    string `json:"my_gridsquare,omitempty"`
	MyIota          string `json:"my_iota,omitempty"`
	MyIotaIslandID  string `json:"my_iota_island_id,omitempty"`
	MyITUZone       string `json:"my_itu_zone,omitempty"`
	MyLat           string `json:"my_lat,omitempty"`
	MyLon           string `json:"my_lon,omitempty"`
	MyMorseKeyInfo  string `json:"my_morse_key_info,omitempty"`
	MyMorseKeyType  string `json:"my_morse_key_type,omitempty"`
	MyName          string `json:"my_name,omitempty"`
	MyPostalCode    string `json:"my_postal_code,omitempty"`
	MyRig           string `json:"my_rig,omitempty"`
	MySig           string `json:"my_sig,omitempty"`
	MySigInfo       string `json:"my_sig_info,omitempty"`
	MyStreet        string `json:"my_street,omitempty"`
	MyWwffRef       string `json:"my_wwff_ref,omitempty"`
	Operator        string `json:"operator,omitempty"` // the logging operator's callsign if STATION_CALLSIGN is absent, OPERATOR shall be treated as both the logging station's callsign and the logging operator's callsign
	OwnerCallsign   string `json:"owner_callsign,omitempty"`
	StationCallsign string `json:"station_callsign,omitempty" validate:"required,min=3,max=30"`
}
