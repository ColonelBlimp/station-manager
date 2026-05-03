package types

// Qsl carries the ADIF QSL-confirmation fields. It is embedded in
// types.Qso and the marshalled form lands inside the qso row's
// additional_data JSON blob.
//
// Every field is tagged `,omitempty` per ADR 0015: the blob omits
// empty values uniformly so it carries operator-set data only, not
// "field exists but empty" noise. Read-back via json.Unmarshal is
// unaffected (missing fields → zero value).
type Qsl struct {
	QslMsg       string `json:"qslmsg,omitempty"`
	QslMsgRcvd   string `json:"qslmsg_rcvd,omitempty"`
	QslRDate     string `json:"qslrdate,omitempty"`
	QslSDate     string `json:"qslsdate,omitempty"`
	QslRcvd      string `json:"qsl_rcvd,omitempty"`
	QslRcvdVia   string `json:"qsl_rcvd_via,omitempty"`
	QslRcvdNotes string `json:"qsl_rcvd_notes,omitempty"`
	QslSent      string `json:"qsl_sent,omitempty"`
	QslSendVia   string `json:"qsl_sent_via,omitempty"`
	QslVia       string `json:"qsl_via,omitempty"`
}
