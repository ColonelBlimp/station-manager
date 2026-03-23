package adif

import (
	"github.com/Station-Manager/errors"
	"github.com/Station-Manager/types"
)

// Record represents a single ADIF (Amateur Data Interchange Format) record.
type Record struct {
	types.QsoDetails
	types.ContactedStation
	types.LoggingStation
	QslSection
	UserDef
}

type QslSection struct {
	QslMsg     string `adif:"qslmsg,omitempty"`
	QslMsgIntl string `adif:"qslmsg_intl,omitempty"`
	QslRDate   string `adif:"qslrdate,omitempty"`
	QslSDate   string `adif:"qsl_sdate,omitempty"`
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
	SmQsoUploadDate     string `adif:"sm_qso_upload_date"`      // Values: "[date-time-stamp]" or empty string
	SmQsoUploadStatus   string `adif:"sm_qso_upload_status"`    // Values: "Y" = Uploaded, "N" = Not Uploaded
	SmFwrdByEmailDate   string `adif:"sm_fwrd_by_email_date"`   // Values: "[date-time-stamp]" or empty string
	SmFwrdByEmailStatus string `adif:"sm_fwrd_by_email_status"` // Values: "Y" = Forwarded by email, "N" = Not forwarded

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
		QslMsgIntl:            q.Qsl.QslMsgRcvd, // confirm your desired mapping here
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
		//		QslWanted:           q.Misc.QslWanted,
	}
	return r
}

func ConvertQsoToAdifNoHeader(q types.Qso) (string, error) {
	rec := QsoToRecord(q)
	return (&rec).String(), nil
}

func ComposeToAdifString(slice types.QsoSlice) (string, error) {
	const op errors.Op = "adif.ComposeToAdifString"
	if len(slice) == 0 {
		return emptyString, errors.New(op).Msg("QSO slice is empty")
	}
	recs := make([]Record, 0, len(slice))
	for _, q := range slice {
		recs = append(recs, QsoToRecord(q))
	}
	return (&Adif{HeaderSection: HeaderSection{}, Records: recs}).String(), nil
}
