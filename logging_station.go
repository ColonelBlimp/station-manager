package types

// LoggingStation represents the details of the station logging the QSO, including location, equipment, and operator info.
// It is compatible with the ADI format.
type LoggingStation struct {
	AntennaAzimuth  string `json:"ant_az,omitempty"` // the bearing from the logging station to the contacted station. Calculated.
	MyAltitude      string `json:"my_altitude"`
	MyAntenna       string `json:"my_antenna"`
	MyCity          string `json:"my_city"`
	MyCountry       string `json:"my_country"`
	MyCqZone        string `json:"my_cq_zone"`
	MyDXCC          string `json:"my_dxcc"`
	MyGridsquare    string `json:"my_gridsquare"`
	MyIota          string `json:"my_iota"`
	MyIotaIslandID  string `json:"my_iota_island_id"`
	MyITUZone       string `json:"my_itu_zone"`
	MyLat           string `json:"my_lat"`
	MyLon           string `json:"my_lon"`
	MyMorseKeyInfo  string `json:"my_morse_key_info"`
	MyMorseKeyType  string `json:"my_morse_key_type"`
	MyName          string `json:"my_name"`
	MyPostalCode    string `json:"my_postal_code"`
	MyRig           string `json:"my_rig"`
	MySig           string `json:"my_sig"`
	MySigInfo       string `json:"my_sig_info"`
	MyStreet        string `json:"my_street"`
	MyWwffRef       string `json:"my_wwff_ref"`
	Operator        string `json:"operator"` // the logging operator's callsign if STATION_CALLSIGN is absent, OPERATOR shall be treated as both the logging station's callsign and the logging operator's callsign
	OwnerCallsign   string `json:"owner_callsign"`
	StationCallsign string `json:"station_callsign" validate:"required,min=3,max=30"`
}
