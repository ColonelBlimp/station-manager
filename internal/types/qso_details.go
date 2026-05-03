package types

// QsoDetails carries the ADIF QSO-detail fields. It is one of three
// structs (alongside ContactedStation and LoggingStation) embedded in
// types.Qso, and the marshalled form lands inside the qso row's
// additional_data JSON blob.
//
// Every field is tagged `,omitempty` per ADR 0015: the blob omits
// empty values uniformly so it carries operator-set data only,
// not "field exists but empty" noise. Read-back via json.Unmarshal
// is unaffected (missing fields → zero value).
type QsoDetails struct {
	AIndex      string `json:"a_index,omitempty"`
	AntPath     string `json:"ant_path,omitempty"` // ADIF, section II.B.1 - currently, we only use S and L
	Band        string `json:"band,omitempty" validate:"band"`
	BandRx      string `json:"band_rx,omitempty"` //in a split frequency QSO, the logging station's receiving band
	Comment     string `json:"comment,omitempty"`
	ContestId   string `json:"contest_id,omitempty"`
	Distance    string `json:"distance,omitempty"` // km
	Freq        string `json:"freq,omitempty"`
	FreqRx      string `json:"freq_rx,omitempty"`
	Mode        string `json:"mode,omitempty" validate:"mode"`
	Submode     string `json:"submode,omitempty"`
	Notes       string `json:"notes,omitempty"` // information of interest to the logging station's operator
	QsoDate     string `json:"qso_date,omitempty"`
	QsoDateOff  string `json:"qso_date_off,omitempty"`
	QsoRandom   string `json:"qso_random,omitempty"`
	QsoComplete string `json:"qso_complete,omitempty"`
	RstRcvd     string `json:"rst_rcvd,omitempty"`
	RstSent     string `json:"rst_sent,omitempty"`
	RxPwr       string `json:"rx_pwr,omitempty"` // the contacted station's transmitter power in Watts with a value greater than or equal to 0
	SRX         string `json:"srx,omitempty"`    // contest QSO received serial number with a value greater than or equal to 0
	STX         string `json:"stx,omitempty"`    // contest QSO transmitted serial number with a value greater than or equal to 0
	TimeOff     string `json:"time_off,omitempty"`
	TimeOn      string `json:"time_on,omitempty"`
	TxPwr       string `json:"tx_pwr,omitempty"` // the logging station's power in Watts with a value greater than or equal to 0
	Rig         string `json:"rig,omitempty"`
}
