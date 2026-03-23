package types

type Qso struct {
	ID int64 `json:"id"`

	// LogbookID represents the foreign key to the logbook associated with a QSO entry.
	// Every QSO entry MUST have a logbook associated with it.
	LogbookID int64 `json:"logbook_id" validate:"required"`

	// SessionID represents the foreign key to the session associated with a QSO entry.
	// Every QSO entry MUST have a session associated with it.
	SessionID int64 `json:"session_id" validate:"required"`

	SmQsoUploadDate     string `json:"sm_qso_upload_date"`
	SmQsoUploadStatus   string `json:"sm_qso_upload_status"`
	SmFwrdByEmailDate   string `json:"sm_fwrd_by_email_date"`
	SmFwrdByEmailStatus string `json:"sm_fwrd_by_email_status"`

	QrzComUploadDate   string `json:"qrzcom_qso_upload_date"`
	QrzComUploadStatus string `json:"qrzcom_qso_upload_status"`
	/*
		All the below fields are compatible with the ADI format and are populated by the adapter.
		The only exception to this is the [xx]ID/ID fields, which are required by database functions.
	*/
	QsoDetails
	ContactedStation
	LoggingStation
	Qsl

	CountryDetails Country          `json:"country_details" adapter:"ignore"` // More detailed information about the contacted station's country
	ContactHistory []ContactHistory `json:"contact_history" adapter:"ignore"`
}

type QsoSlice []Qso
