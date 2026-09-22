package types

type Logbook struct {
	ID int64 `json:"id"`
	// UUID is the logbook's stable identity for cross-file and cloud use (ADR
	// 0071, migration 0012): a UUIDv7 minted by the store on insert or by the
	// daemon's one-time backfill, never accepted from a client. Empty only for
	// a file the daemon has not yet started. The numeric ID stays the local key.
	UUID        string `json:"uuid"`
	Name        string `json:"name"`     // Unique name (to the user) for the logbook
	Callsign    string `json:"callsign"` // The callsign associated with the logbook
	Description string `json:"description,omitempty"`
}

type LogbookList []Logbook
