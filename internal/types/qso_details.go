package types

// QsoDetails represents the details of a QSO and is compatible with the ADI format.
// Some fields are marked as 'omitempty'. This is for the importer tool, which marshalls the additional data using
// json.Marshal. Fields that are not set will be omitted from the JSON output, and this will not trigger the duplicate
// check at the database level.
type QsoDetails struct {
	AIndex      string `json:"a_index"`
	AntPath     string `json:"ant_path"` // ADIF, section II.B.1 - currently, we only use S and L
	Band        string `json:"band,omitempty" validate:"band"`
	BandRx      string `json:"band_rx"` //in a split frequency QSO, the logging station's receiving band
	Comment     string `json:"comment"`
	ContestId   string `json:"contest_id"`
	Distance    string `json:"distance"` // km
	Freq        string `json:"freq,omitempty"`
	FreqRx      string `json:"freq_rx"`
	Mode        string `json:"mode,omitempty" validate:"mode"`
	Submode     string `json:"submode"`
	Notes       string `json:"notes"` // information of interest to the logging station's operator
	QsoDate     string `json:"qso_date,omitempty"`
	QsoDateOff  string `json:"qso_date_off"`
	QsoRandom   string `json:"qso_random"`
	QsoComplete string `json:"qso_complete"`
	RstRcvd     string `json:"rst_rcvd,omitempty"`
	RstSent     string `json:"rst_sent,omitempty"`
	RxPwr       string `json:"rx_pwr"` // the contacted station's transmitter power in Watts with a value greater than or equal to 0
	SRX         string `json:"srx"`    // contest QSO received serial number with a value greater than or equal to 0
	STX         string `json:"stx"`    // contest QSO transmitted serial number with a value greater than or equal to 0
	TimeOff     string `json:"time_off,omitempty"`
	TimeOn      string `json:"time_on,omitempty"`
	TxPwr       string `json:"tx_pwr"` // the logging station's power in Watts with a value greater than or equal to 0
	Rig         string `json:"rig"`
}
