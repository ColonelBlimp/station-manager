package types

// ContactedStation represents details of the station contacted during a QSO and is compatible with the ADI format.
// Some fields are marked as 'omitempty'. This is for the importer tool, which marshalls the additional data using
// json.Marshal. Fields that are not set will be omitted from the JSON output, and this will not trigger the duplicate
// check at the database level.
type ContactedStation struct {
	// ID is the primary key of the ContactedStation table. This is only used when updating the contacted station details.
	// Notice the JSON tag for this struct is "csid" so that it does not clash with the "id" field of the QSO struct.
	// See the models.ts file for more details.
	CSID         int64  `json:"csid"`
	Address      string `json:"address"`
	Age          string `json:"age"`
	Altitude     string `json:"altitude"`
	Call         string `json:"call,omitempty"`
	Cont         string `json:"cont"` // the contacted station's Continent
	ContactedOp  string `json:"contacted_op"`
	Country      string `json:"country,omitempty"`
	CQZ          string `json:"cqz"`
	DXCC         string `json:"dxcc"`
	Email        string `json:"email"`
	EqCall       string `json:"eq_call"` // the contacted station's owner's callsign (if different from call)
	Gridsquare   string `json:"gridsquare"`
	Iota         string `json:"iota"`
	IotaIslandId string `json:"iota_island_id"`
	ITUZ         string `json:"ituz"`
	Lat          string `json:"lat"`
	Lon          string `json:"lon"`
	Name         string `json:"name"`
	QTH          string `json:"qth"`
	// These belong to a QSO, not necessarily a ContactedStation
	//Rig          string `json:"rig"`
	Sig     string `json:"sig"`      // the name of the contacted station's special activity or interest group
	SigInfo string `json:"sig_info"` // information associated with the contacted station's activity or interest group
	Web     string `json:"web"`
	WwffRef string `json:"wwff_ref"`
}
