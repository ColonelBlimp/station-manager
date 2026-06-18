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

// QslDefaults is the operator's standing outgoing-QSL preferences, persisted in
// config.json under `qsl` and stamped onto a logged QSO when the corresponding
// per-QSO field is empty. It is deliberately a SUBSET of [Qsl] — only the fields
// that make sense as a station-wide default. The per-QSO confirmation status
// fields (QSL_SENT/RCVD, dates, received notes/msg) are NOT here: they change per
// contact and over time, so they belong on the QSO record, never a config default.
//
// JSON tags match [Qsl]'s so the ADIF field names round-trip. Empty fields are
// omitted — and therefore never stamped: an empty default leaves the QSO's field
// empty, which the ADIF emitter omits.
type QslDefaults struct {
	// QslVia — ADIF QSL_VIA: the QSL route / manager (e.g. "via M0XXX", "LoTW").
	QslVia string `json:"qsl_via,omitempty"`
	// QslMsg — ADIF QSLMSG: a standing message for outgoing cards / uploads.
	QslMsg string `json:"qslmsg,omitempty"`
	// QslSendVia — ADIF QSL_SENT_VIA: default send method (B/D/E/M —
	// bureau/direct/electronic/manager).
	QslSendVia string `json:"qsl_sent_via,omitempty"`
}

// ApplyTo stamps the standing QSL defaults onto q: it fills only the fields q
// leaves empty (so any per-QSO value wins) and skips empty defaults (so an unset
// default never adds an empty ADIF tag). Used by the FT8 logging path; Phone/CW
// applies the same defaults SPA-side at submit. nil-safe.
func (d QslDefaults) ApplyTo(q *Qso) {
	if q == nil {
		return
	}
	if q.QslVia == "" {
		q.QslVia = d.QslVia
	}
	if q.QslMsg == "" {
		q.QslMsg = d.QslMsg
	}
	if q.QslSendVia == "" {
		q.QslSendVia = d.QslSendVia
	}
}
