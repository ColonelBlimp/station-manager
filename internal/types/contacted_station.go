package types

import "time"

// ContactedStation carries the ADIF "contacted station" fields. It is
// one of three structs (alongside QsoDetails and LoggingStation)
// embedded in types.Qso, and the marshalled form lands inside the qso
// row's additional_data JSON blob (and, for the standalone enrichment
// row, inside contacted_station.additional_data).
//
// Every field is tagged `,omitempty` per ADR 0015: the blob omits
// empty values uniformly so it carries operator-set / enriched data
// only, not "field exists but empty" noise. Read-back via
// json.Unmarshal is unaffected (missing fields → zero value).
type ContactedStation struct {
	// ID is the primary key of the ContactedStation table. This is only used when updating the contacted station details.
	// Notice the JSON tag for this struct is "csid" so that it does not clash with the "id" field of the QSO struct.
	// See the models.ts file for more details.
	CSID         int64  `json:"csid,omitempty"`
	Address      string `json:"address,omitempty"`
	Age          string `json:"age,omitempty"`
	Altitude     string `json:"altitude,omitempty"`
	Call         string `json:"call,omitempty"`
	Cont         string `json:"cont,omitempty"` // the contacted station's Continent
	ContactedOp  string `json:"contacted_op,omitempty"`
	Country      string `json:"country,omitempty"`
	CQZ          string `json:"cqz,omitempty"`
	DXCC         string `json:"dxcc,omitempty"`
	Email        string `json:"email,omitempty"`
	EqCall       string `json:"eq_call,omitempty"` // the contacted station's owner's callsign (if different from call)
	Gridsquare   string `json:"gridsquare,omitempty"`
	Iota         string `json:"iota,omitempty"`
	IotaIslandId string `json:"iota_island_id,omitempty"`
	ITUZ         string `json:"ituz,omitempty"`
	Lat          string `json:"lat,omitempty"`
	Lon          string `json:"lon,omitempty"`
	Name         string `json:"name,omitempty"`
	QTH          string `json:"qth,omitempty"`
	Sig          string `json:"sig,omitempty"`      // the name of the contacted station's special activity or interest group
	SigInfo      string `json:"sig_info,omitempty"` // information associated with the contacted station's activity or interest group
	Web          string `json:"web,omitempty"`
	WwffRef      string `json:"wwff_ref,omitempty"`

	// LastRefreshedAt is the timestamp of the most recent write to
	// this row's storage table — either from a callsign-class
	// enrichment lookup (QRZ, HamQTH, …) or from the QSO-submit
	// upsert path (per ADR 0017 #10). Populated by the read helper;
	// used by the enrichment orchestrator to branch fresh / stale /
	// cold per ADR 0017's three-state read policy. Zero means "never
	// refreshed (NULL in DB), treat as stale on first read."
	LastRefreshedAt time.Time `json:"last_refreshed_at,omitempty"`
}
