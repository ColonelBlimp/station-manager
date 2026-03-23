package types

// QsoAdditionalData represents the fields stored in the additional_data JSON column of the QSO table.
// These are fields that are not stored in dedicated database columns but are part of the full QSO record.
// All fields use omitempty to minimize storage and avoid triggering duplicate checks at the database level.
type QsoAdditionalData struct {
	// Upload status fields
	SmQsoUploadDate     string `json:"sm_qso_upload_date,omitempty"`
	SmQsoUploadStatus   string `json:"sm_qso_upload_status,omitempty"`
	SmFwrdByEmailDate   string `json:"sm_fwrd_by_email_date,omitempty"`
	SmFwrdByEmailStatus string `json:"sm_fwrd_by_email_status,omitempty"`
	QrzComUploadDate    string `json:"qrzcom_qso_upload_date,omitempty"`
	QrzComUploadStatus  string `json:"qrzcom_qso_upload_status,omitempty"`

	// QsoDetails fields (excluding those in dedicated columns: band, mode, freq, qso_date, time_on, time_off, rst_sent, rst_rcvd)
	AIndex      string `json:"a_index,omitempty"`
	AntPath     string `json:"ant_path,omitempty"`
	BandRx      string `json:"band_rx,omitempty"`
	Comment     string `json:"comment,omitempty"`
	ContestId   string `json:"contest_id,omitempty"`
	Distance    string `json:"distance,omitempty"`
	FreqRx      string `json:"freq_rx,omitempty"`
	Submode     string `json:"submode,omitempty"`
	Notes       string `json:"notes,omitempty"`
	QsoDateOff  string `json:"qso_date_off,omitempty"`
	QsoRandom   string `json:"qso_random,omitempty"`
	QsoComplete string `json:"qso_complete,omitempty"`
	RxPwr       string `json:"rx_pwr,omitempty"`
	SRX         string `json:"srx,omitempty"`
	STX         string `json:"stx,omitempty"`
	TxPwr       string `json:"tx_pwr,omitempty"`
	Rig         string `json:"rig,omitempty"`

	// ContactedStation fields (excluding those in dedicated columns: call, country)
	Address      string `json:"address,omitempty"`
	Age          string `json:"age,omitempty"`
	Altitude     string `json:"altitude,omitempty"`
	Cont         string `json:"cont,omitempty"`
	ContactedOp  string `json:"contacted_op,omitempty"`
	CQZ          string `json:"cqz,omitempty"`
	DXCC         string `json:"dxcc,omitempty"`
	Email        string `json:"email,omitempty"`
	EqCall       string `json:"eq_call,omitempty"`
	Gridsquare   string `json:"gridsquare,omitempty"`
	Iota         string `json:"iota,omitempty"`
	IotaIslandId string `json:"iota_island_id,omitempty"`
	ITUZ         string `json:"ituz,omitempty"`
	Lat          string `json:"lat,omitempty"`
	Lon          string `json:"lon,omitempty"`
	Name         string `json:"name,omitempty"`
	QTH          string `json:"qth,omitempty"`
	Sig          string `json:"sig,omitempty"`
	SigInfo      string `json:"sig_info,omitempty"`
	Web          string `json:"web,omitempty"`
	WwffRef      string `json:"wwff_ref,omitempty"`

	// LoggingStation fields
	AntennaAzimuth  string `json:"ant_az,omitempty"`
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
	Operator        string `json:"operator,omitempty"`
	OwnerCallsign   string `json:"owner_callsign,omitempty"`
	StationCallsign string `json:"station_callsign,omitempty"`
}

// ContactedStationAdditionalData represents the fields stored in the additional_data JSON column
// of the contacted_station table. These are fields not stored in dedicated columns (id, name, call, country).
type ContactedStationAdditionalData struct {
	Address      string `json:"address,omitempty"`
	Age          string `json:"age,omitempty"`
	Altitude     string `json:"altitude,omitempty"`
	Cont         string `json:"cont,omitempty"`
	ContactedOp  string `json:"contacted_op,omitempty"`
	CQZ          string `json:"cqz,omitempty"`
	DXCC         string `json:"dxcc,omitempty"`
	Email        string `json:"email,omitempty"`
	EqCall       string `json:"eq_call,omitempty"`
	Gridsquare   string `json:"gridsquare,omitempty"`
	Iota         string `json:"iota,omitempty"`
	IotaIslandId string `json:"iota_island_id,omitempty"`
	ITUZ         string `json:"ituz,omitempty"`
	Lat          string `json:"lat,omitempty"`
	Lon          string `json:"lon,omitempty"`
	QTH          string `json:"qth,omitempty"`
	Rig          string `json:"rig,omitempty"`
	Sig          string `json:"sig,omitempty"`
	SigInfo      string `json:"sig_info,omitempty"`
	Web          string `json:"web,omitempty"`
	WwffRef      string `json:"wwff_ref,omitempty"`
}
