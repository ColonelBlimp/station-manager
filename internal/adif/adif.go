package adif

import (
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// Record represents a single ADIF (Amateur Data Interchange Format) record.
//
// AppSmQsoID rides as an APP_<programid>_<fieldname> field — the ADIF
// mechanism for application-defined data that does not require a
// header-side USERDEFx declaration. Per ADR 0016 it carries the QSO's
// UUIDv7 so re-imports and forwarder uploads round-trip the canonical
// external identifier.
//
// AppSmRequestQsl rides via the same APP_ extension mechanism. The
// field is a string (parser/emitter only handle strings via
// reflection) holding "Y" when the operator wants to request a QSL
// card; the bool ↔ string conversion happens at the QsoToRecord /
// RecordToQso boundary so types.Qso keeps the bool semantic. Empty
// is the absence-equivalent — omitempty drops it from emitted ADIF.
type Record struct {
	types.QsoDetails
	types.ContactedStation
	types.LoggingStation
	QslSection
	UserDef
	AppSmQsoID      string `adif:"app_sm_qso_id,omitempty"`
	AppSmRequestQsl string `adif:"app_sm_request_qsl,omitempty"`
}

type QslSection struct {
	QslMsg     string `adif:"qslmsg,omitempty"`
	QslMsgIntl string `adif:"qslmsg_intl,omitempty"`
	QslRDate   string `adif:"qslrdate,omitempty"`
	QslSDate   string `adif:"qslsdate,omitempty"`
	QslRcvd    string `adif:"qsl_rcvd,omitempty"` // QslRcvd: the QSL received status
	QslSent    string `adif:"qsl_sent,omitempty"` // QslSent: the QSL sent status
	QslSentVia string `adif:"qsl_sent_via,omitempty"`
	QslVia     string `adif:"qsl_via,omitempty"`

	QrzComQsoDownloadDate   string `adif:"qrzcom_qso_download_date,omitempty"`
	QrzComQsoDownloadStatus string `adif:"qrzcom_qso_download_status,omitempty"`
	QrzComQsoUploadDate     string `adif:"qrzcom_qso_upload_date,omitempty"`
	QrzComQsoUploadStatus   string `adif:"qrzcom_qso_upload_status,omitempty"`
}

type HeaderSection struct {
	ADIFVer          string // ADIF version number
	CreatedTimestamp string // timestamp when the ADIF file was created
	ProgramID        string // name of the logging program
	ProgramVersion   string // version of the logging program
}

type Adif struct {
	HeaderSection HeaderSection
	Records       []Record
}

type UserDef struct {
	SmQsoUploadDate     string `adif:"sm_qso_upload_date"`     // Values: "[date-time-stamp]" or empty string
	SmQsoUploadStatus   string `adif:"sm_qso_upload_status"`   // Values: "Y" = Uploaded, "N" = Not Uploaded
	SmFwrdByEmailDate   string `adif:"sm_fwd_by_email_date"`   // Values: "[date-time-stamp]" or empty string
	SmFwrdByEmailStatus string `adif:"sm_fwd_by_email_status"` // Values: "Y" = Forwarded by email, "N" = Not forwarded

	// Indicates if a QSL (physical card) is wanted for this QSO. This allows for tracking if a QSL card is required for
	// this qso. 'qsl_rcvd' should be then used to track the status: 'R' = Requested, 'Y' = QSL received.
	QslWanted string `adif:"qsl_wanted"`
}

func QsoToRecord(q types.Qso) Record {
	r := Record{}
	// QsoDetails, ContactedStation, LoggingStation are already flat and compatible
	r.QsoDetails = q.QsoDetails
	r.ContactedStation = q.ContactedStation
	r.LoggingStation = q.LoggingStation
	// Map QSL
	r.QslSection = QslSection{
		QslMsg:                q.Qsl.QslMsg,
		QslRDate:              q.Qsl.QslRDate,
		QslSDate:              q.Qsl.QslSDate,
		QslRcvd:               q.Qsl.QslRcvd,
		QslSent:               q.Qsl.QslSent,
		QslSentVia:            q.Qsl.QslSendVia,
		QslVia:                q.Qsl.QslVia,
		QrzComQsoUploadDate:   q.QrzComUploadDate,
		QrzComQsoUploadStatus: q.QrzComUploadStatus,
	}
	// Map user-defined fields
	r.UserDef = UserDef{
		SmQsoUploadDate:     q.SmQsoUploadDate,
		SmQsoUploadStatus:   q.SmQsoUploadStatus,
		SmFwrdByEmailDate:   q.SmFwrdByEmailDate,
		SmFwrdByEmailStatus: q.SmFwrdByEmailStatus,
	}
	r.AppSmQsoID = q.UUID
	if q.AppSmRequestQsl {
		// "Y" is the project's encoding for the operator's "request a
		// QSL" reminder flag. Parser checks for this exact value on
		// the way back; an empty / absent field decodes to false.
		r.AppSmRequestQsl = "Y"
	}
	return r
}

// RecordToQso converts a parsed ADIF Record into a types.Qso, setting the
// given logbookID. QSL and user-defined fields are mapped where possible.
//
// AppSmRequestQsl maps "Y" → true, anything else → false. The strict
// equality means a malformed value ("y", "1", "true") decodes to false
// — defensive default that matches the project's "operator-set true
// only when the field is exactly Y" convention used by the existing
// SPA emitter.
func RecordToQso(rec Record, logbookID int64) types.Qso {
	q := types.Qso{
		LogbookID:        logbookID,
		QsoDetails:       rec.QsoDetails,
		ContactedStation: rec.ContactedStation,
		LoggingStation:   rec.LoggingStation,
		Qsl: types.Qsl{
			QslMsg:   rec.QslSection.QslMsg,
			QslRDate: rec.QslSection.QslRDate,
			QslSDate: rec.QslSection.QslSDate,
			QslRcvd:  rec.QslSection.QslRcvd,
			QslSent:  rec.QslSection.QslSent,
			QslVia:   rec.QslSection.QslVia,
		},
		QrzComUploadDate:    rec.QslSection.QrzComQsoUploadDate,
		QrzComUploadStatus:  rec.QslSection.QrzComQsoUploadStatus,
		SmQsoUploadDate:     rec.UserDef.SmQsoUploadDate,
		SmQsoUploadStatus:   rec.UserDef.SmQsoUploadStatus,
		SmFwrdByEmailDate:   rec.UserDef.SmFwrdByEmailDate,
		SmFwrdByEmailStatus: rec.UserDef.SmFwrdByEmailStatus,
		AppSmRequestQsl:     rec.AppSmRequestQsl == "Y",
	}
	return q
}

func ConvertQsoToAdifNoHeader(q types.Qso) string {
	rec := QsoToRecord(q)
	return (&rec).String()
}

func ComposeToAdifString(slice types.QsoSlice) (string, error) {
	const op errors.Op = "adif.ComposeToAdifString"
	if len(slice) == 0 {
		return emptyString, errors.New(op).WithMsg("QSO slice is empty")
	}
	recs := make([]Record, 0, len(slice))
	for _, q := range slice {
		recs = append(recs, QsoToRecord(q))
	}
	return (&Adif{HeaderSection: HeaderSection{}, Records: recs}).String(), nil
}
