package adif

import "github.com/ColonelBlimp/station-manager/internal/types"

// emailMarkIgnored is the delivery bookkeeping the session-email mark ignores
// when it compares a QSO's current row with the content that was emailed
// (W-0010 outcome 11; the reviewed list in the inbox triage, item 1). Upload
// stamps, the QRZ log id and the email mark itself change a row's revision
// without changing the contact the recipient was sent; every other emitted
// field is contact content, the QSO's identity (APP_SM_QSO_ID) included.
// QRZCOM_QSO_DOWNLOAD_* is not emitted today and is listed prospectively.
var emailMarkIgnored = map[string]struct{}{
	"CLUBLOG_QSO_UPLOAD_DATE":    {},
	"CLUBLOG_QSO_UPLOAD_STATUS":  {},
	"QRZCOM_QSO_UPLOAD_DATE":     {},
	"QRZCOM_QSO_UPLOAD_STATUS":   {},
	"APP_QRZLOG_LOGID":           {},
	"SM_QSO_UPLOAD_DATE":         {},
	"SM_QSO_UPLOAD_STATUS":       {},
	"SM_FWRD_BY_EMAIL_DATE":      {},
	"SM_FWRD_BY_EMAIL_STATUS":    {},
	"QRZCOM_QSO_DOWNLOAD_DATE":   {},
	"QRZCOM_QSO_DOWNLOAD_STATUS": {},
}

// EmailMarkIgnoredTags lists the ignored tags (a fresh slice; order unspecified).
func EmailMarkIgnoredTags() []string {
	out := make([]string, 0, len(emailMarkIgnored))
	for tag := range emailMarkIgnored {
		out = append(out, tag)
	}
	return out
}

// EmailMarkContent is the part of q's emitted ADIF record the session-email mark
// compares: every emitted field except the ignored bookkeeping. Two rows with
// equal content carry the same contact as far as the attachment is concerned.
func EmailMarkContent(q types.Qso) map[string]string {
	fields := EmittedFields(QsoToRecord(q))
	for tag := range emailMarkIgnored {
		delete(fields, tag)
	}
	return fields
}
