package types

// DefaultChangeRequest asks to make another logbook of the active archive the
// default at an attended restart (ADR 0097). BelievedDefaultUUID is the default
// the requesting tab believes is in effect: a stale belief is refused, never
// retargeted.
type DefaultChangeRequest struct {
	LogbookUUID         string `json:"logbook_uuid"`
	BelievedDefaultUUID string `json:"believed_default_uuid"`
}

// DefaultChangeAction names the request a Cancel, Confirm or Retry cleanup acts
// on; a request already resolved, or superseded, is refused.
type DefaultChangeAction struct {
	RequestID string `json:"request_id"`
}

// DefaultChangeAccepted is a change accepted and its restart requested.
type DefaultChangeAccepted struct {
	RequestID  string `json:"request_id"`
	Durability string `json:"durability"`
}

// DefaultChangeView is a default change as the status reports it.
type DefaultChangeView struct {
	RequestID   string `json:"request_id"`
	LogbookUUID string `json:"logbook_uuid"`
	State       string `json:"state"`
	Removed     string `json:"removed,omitempty"`
	FailureCode string `json:"failure_code,omitempty"`
	// Unconfirmed: config still holds the record, which blocks another change
	// or an activation and protects its target from deletion.
	Unconfirmed bool `json:"unconfirmed"`
}

// DefaultChangeStatus is the effective default and any change in play.
type DefaultChangeStatus struct {
	Effective Logbook            `json:"effective"`
	Change    *DefaultChangeView `json:"change,omitempty"`
}
