package types

type Qso struct {
	ID int64 `json:"id"`

	// LogbookID represents the foreign key to the logbook associated with a QSO entry.
	// Every QSO entry MUST have a logbook associated with it.
	LogbookID int64 `json:"logbook_id" validate:"required"`

	// DedupeKey is a SHA-256 hex hash of
	// CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON (uppercased, pipe-separated;
	// FREQ is the normalized integer-kHz string so MHz decimal variants
	// of the same frequency hash identically). Used by the submit
	// endpoint to detect duplicate QSOs within the same logbook.
	// Computed by qsoservice, not set by callers.
	DedupeKey string `json:"dedupe_key,omitempty"`

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

	CountryDetails Country          `json:"country_details"` // Enrichment: contacted station's country details
	ContactHistory []ContactHistory `json:"contact_history"` // Prior QSOs with this callsign
}

type QsoSlice []Qso
