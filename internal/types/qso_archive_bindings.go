package types

// ArchiveBindingsView is GET /v1/qso-archives/{uuid}/bindings (ADR 0082, W-0021
// 5D): the active archive's destination bindings as the Forwarding tab renders
// them — one card per registered destination type, an aggregate switch state
// over the archive's live logbooks, and one row per logbook. Credentials are
// never on this wire: each row lists the keys that hold a value.
type ArchiveBindingsView struct {
	ArchiveID    string `json:"archive_id"`
	ArchiveLabel string `json:"archive_label"`
	// RestartRequired is true when the saved bindings differ from the set the
	// running daemon started with: edits apply at the next start (ADR 0082
	// part 5).
	RestartRequired bool                     `json:"restart_required"`
	Destinations    []DestinationBindingView `json:"destinations"`
}

// DestinationBindingView is one destination type across the archive.
type DestinationBindingView struct {
	Type        string `json:"type"`
	DisplayName string `json:"display_name"`
	// Account describes the station account config.json holds for this type.
	Account StationAccountView `json:"account"`
	// State is the aggregate switch: "on" when every live logbook's binding is
	// enabled, "off" when none is, "mixed" otherwise.
	State string `json:"state"`
	// Reason, when set, is why the switch cannot be turned on here (no station
	// account; SM Cloud outside the adopted archive until the identity-aware
	// server lands) — with where it is fixed.
	Reason   string               `json:"reason,omitempty"`
	Logbooks []LogbookBindingView `json:"logbooks"`
}

// StationAccountView is the presence of a station account, never its values.
type StationAccountView struct {
	Configured bool   `json:"configured"`
	Label      string `json:"label,omitempty"`
	// FieldsSet lists the station-scoped credential keys that hold a value.
	FieldsSet []string `json:"fields_set,omitempty"`
}

// LogbookBindingView is one logbook's binding to a destination; Bound is false
// when the logbook has no row yet (absent counts as off).
type LogbookBindingView struct {
	LogbookID       int64  `json:"logbook_id"`
	LogbookUUID     string `json:"logbook_uuid"`
	LogbookName     string `json:"logbook_name"`
	LogbookCallsign string `json:"logbook_callsign"`
	Bound           bool   `json:"bound"`
	Enabled         bool   `json:"enabled"`
	// ForwarderName is the opaque routing handle the queue endpoints take;
	// never a setting.
	ForwarderName string `json:"forwarder_name,omitempty"`
	// CredentialsSet lists the logbook-scoped keys that hold a value.
	CredentialsSet []string          `json:"credentials_set,omitempty"`
	Queue          BindingQueueCount `json:"queue"`
}

// BindingQueueCount mirrors the queue readout for one binding name.
type BindingQueueCount struct {
	Waiting  int64 `json:"waiting"`
	Failed   int64 `json:"failed"`
	InFlight int64 `json:"in_flight"`
}

// ArchiveBindingsRequest is PUT /v1/qso-archives/{uuid}/bindings: only the
// rows the operator changed, merged onto the stored rows. The whole candidate
// is validated first and every affected row commits in one transaction.
type ArchiveBindingsRequest struct {
	Destinations []DestinationBindingEdit `json:"destinations"`
}

// DestinationBindingEdit carries a destination type's edited logbook rows.
type DestinationBindingEdit struct {
	Type     string               `json:"type"`
	Logbooks []LogbookBindingEdit `json:"logbooks"`
}

// LogbookBindingEdit is one row's edit. Enabled is the row's new state.
// Credentials carries only the fields typed (blank keeps the stored value);
// CredentialsClear removes named stored fields and is accepted only when the
// row ends disabled.
type LogbookBindingEdit struct {
	LogbookID        int64             `json:"logbook_id"`
	Enabled          bool              `json:"enabled"`
	Credentials      map[string]string `json:"credentials,omitempty"`
	CredentialsClear []string          `json:"credentials_clear,omitempty"`
}
