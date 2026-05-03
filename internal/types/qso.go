package types

// Qso is the ADIF-shaped QSO record. It is the canonical in-memory
// representation: handlers, services, the DB adapter, and the
// additional_data JSON blob all share this single struct. The
// embedded sub-structs (QsoDetails, ContactedStation, LoggingStation,
// Qsl) carry the bulk of ADIF fields; CountryDetails carries DXCC /
// zone enrichment; ContactHistory carries prior contacts with the
// same callsign.
//
// Per ADR 0015, every field on this struct and its embeds is tagged
// `,omitempty` so the marshalled JSON blob carries operator-set /
// enriched data only, not "field exists but empty" noise.
type Qso struct {
	ID int64 `json:"id,omitempty"`

	// LogbookID represents the foreign key to the logbook associated with a QSO entry.
	// Every QSO entry MUST have a logbook associated with it.
	LogbookID int64 `json:"logbook_id,omitempty" validate:"required"`

	// DedupeKey is a SHA-256 hex hash of
	// CALL|BAND|MODE|FREQ|QSO_DATE|TIME_ON (uppercased, pipe-separated;
	// FREQ is the normalized integer-kHz string so MHz decimal variants
	// of the same frequency hash identically). Used by the submit
	// endpoint to detect duplicate QSOs within the same logbook.
	// Computed by qsoservice, not set by callers.
	DedupeKey string `json:"dedupe_key,omitempty"`

	SmQsoUploadDate     string `json:"sm_qso_upload_date,omitempty"`
	SmQsoUploadStatus   string `json:"sm_qso_upload_status,omitempty"`
	SmFwrdByEmailDate   string `json:"sm_fwrd_by_email_date,omitempty"`
	SmFwrdByEmailStatus string `json:"sm_fwrd_by_email_status,omitempty"`

	QrzComUploadDate   string `json:"qrzcom_qso_upload_date,omitempty"`
	QrzComUploadStatus string `json:"qrzcom_qso_upload_status,omitempty"`
	/*
		All the below fields are compatible with the ADI format and are populated by the adapter.
		The only exception to this is the [xx]ID/ID fields, which are required by database functions.
	*/
	QsoDetails
	ContactedStation
	LoggingStation
	Qsl

	CountryDetails Country          `json:"country_details,omitempty"` // Enrichment: contacted station's country details
	ContactHistory []ContactHistory `json:"contact_history,omitempty"` // Prior QSOs with this callsign
}

type QsoSlice []Qso
