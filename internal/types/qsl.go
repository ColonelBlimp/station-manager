package types

type Qsl struct {
	QslMsg       string `json:"qslmsg"`
	QslMsgRcvd   string `json:"qslmsg_rcvd"`
	QslRDate     string `json:"qslrdate"`
	QslSDate     string `json:"qslsdate"`
	QslRcvd      string `json:"qsl_rcvd"`
	QslRcvdVia   string `json:"qsl_rcvd_via"`
	QslRcvdNotes string `json:"qsl_rcvd_notes"`
	QslSent      string `json:"qsl_sent"`
	QslSendVia   string `json:"qsl_sent_via"`
	QslVia       string `json:"qsl_via"`
}
