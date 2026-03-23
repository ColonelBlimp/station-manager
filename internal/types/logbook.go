package types

type Logbook struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"user_id,omitempty"` // FK to users.id - only for postgres
	Name        string `json:"name"`              // Unique name (to the user) for the logbook
	Callsign    string `json:"callsign"`          // The callsign associated with the logbook
	APIKey      string `json:"api_key,omitempty"`
	Description string `json:"description,omitempty"`
}

type LogbookList []Logbook
