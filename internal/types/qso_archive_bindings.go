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
	Reason string `json:"reason,omitempty"`
	// NewLogbookReason, when set, is why a logbook created now could not have
	// this destination turned on (SM Cloud in Home: the default logbook only
	// until per-binding identity lands, W-0021 5F.0).
	NewLogbookReason string               `json:"new_logbook_reason,omitempty"`
	Logbooks         []LogbookBindingView `json:"logbooks"`
}

// StationAccountView is the presence of a station account, never its values.
type StationAccountView struct {
	Configured bool   `json:"configured"`
	Label      string `json:"label,omitempty"`
	// FieldsSet lists the station-scoped credential keys that hold a value.
	FieldsSet []string `json:"fields_set,omitempty"`
	// BuildKey is "present" or "absent" for a type that authenticates as an
	// application with a key built into the daemon (ClubLog, ADR 0054);
	// omitted for a type that needs none. Without it the destination still
	// constructs, and its uploads wait in the queue for a keyed build.
	BuildKey string `json:"build_key,omitempty"`
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
	// Reason, when set, is why THIS row cannot be turned on although its
	// destination can (W-0021 5F.0); never set on a row that is enabled.
	Reason string `json:"reason,omitempty"`
	// LockedFields lists the logbook-scoped keys fixed by the binding's remote
	// adoption (SM Cloud's cloud logbook name, ADR 0090): not shown for
	// editing, and refused by the PUT. Absent when nothing is locked.
	LockedFields []string `json:"locked_fields,omitempty"`
	// Adoption is the remote adoption's status (ADR 0090, T4), on Home's
	// default SM Cloud row only, and only while it describes that row's
	// current binding, name and station account.
	Adoption *BindingAdoptionView `json:"adoption,omitempty"`
	// UploadsHeld is set when the running daemon started this binding held:
	// adopted, but not confirmed for the station account it started with, so
	// its uploads stay queued (ADR 0091, ruling C2). It reads "waiting for
	// confirmation" until a confirmation for the current account is recorded,
	// then "restart required".
	UploadsHeld *BindingAdoptionView `json:"uploads_held,omitempty"`
}

// BindingAdoptionView is one adoption status: a stable state and the sentence
// the Forwarding tab shows.
type BindingAdoptionView struct {
	State   string `json:"state"`
	Message string `json:"message"`
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
