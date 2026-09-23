package types

// QsoArchiveOwnership says who manages a QSO archive's file (ADR 0071).
type QsoArchiveOwnership string

const (
	// QsoArchiveOwnershipManaged: created by the daemon under
	// ${working_dir}/db/qso-archives/<id>.db. The path derives from the id and
	// is never stored; the daemon owns the file's modes and lifecycle.
	QsoArchiveOwnershipManaged QsoArchiveOwnership = "managed"
	// QsoArchiveOwnershipLegacy: the installation's pre-archive database,
	// adopted in place at its recorded path (operator ruling (a), 2026-09-22).
	// Permissions are managed as before; the daemon never moves or renames it.
	QsoArchiveOwnershipLegacy QsoArchiveOwnership = "legacy"
	// QsoArchiveOwnershipExternal: an operator-owned file outside the working
	// directory, recorded at its canonical absolute path. Never chmod'd, moved,
	// renamed or deleted by the daemon; detach-only from the app.
	QsoArchiveOwnershipExternal QsoArchiveOwnership = "external"
)

// QsoArchiveConfig is one entry of the station-global archive catalogue in
// config.json (ADR 0071): the facts the daemon needs BEFORE it can open a QSO
// file. Everything that describes the archive itself — its default logbook,
// its logical logbooks and their identities — lives inside the file, whose
// archive_metadata row carries the same ID; the two must agree, and the file
// is the authority.
type QsoArchiveConfig struct {
	// ID is the archive's immutable UUIDv7, minted once and embedded in the file.
	ID string `json:"id"`
	// Label is the operator-facing, mutable name. It never renames the file.
	Label string `json:"label"`
	// Ownership selects the filesystem policy: managed | legacy | external.
	Ownership QsoArchiveOwnership `json:"ownership"`
	// Path is the canonical absolute path of a legacy or external file. Absent
	// for a managed archive, whose path derives from ID.
	Path string `json:"path,omitempty"`
	// LastActivationError is the diagnostic from the most recent failed
	// activation attempt, kept so a failed candidate never reads as active.
	LastActivationError string `json:"last_activation_error,omitempty"`
	// RequestKey is the creation request's idempotency key (config v5): a
	// retried create with the same key — through a restarted daemon too —
	// returns this archive instead of making another. Absent on adopted entries.
	RequestKey string `json:"request_key,omitempty"`
}

// QsoArchiveCreateRequest names a new managed archive by its semantics only —
// never a path (ADR 0071 path confinement). RequestKey makes creation
// idempotent: a retried request with the same key returns the archive the
// first one made. The wire shape of POST /v1/qso-archives.
type QsoArchiveCreateRequest struct {
	RequestKey      string `json:"request_key"`
	Label           string `json:"label"`
	LogbookName     string `json:"logbook_name"`
	LogbookCallsign string `json:"logbook_callsign"`
}

// QsoArchiveState is an archive's place in the catalogue as the daemon reports
// it: the one it serves, the candidate for the next start, or neither.
type QsoArchiveState string

const (
	QsoArchiveStateActive   QsoArchiveState = "active"
	QsoArchiveStatePending  QsoArchiveState = "pending"
	QsoArchiveStateInactive QsoArchiveState = "inactive"
)

// QsoArchiveView is one archive as the API lists it (GET /v1/qso-archives): the
// catalogue entry's operator-facing fields plus its state. A failed activation
// is an inactive archive with LastActivationError set — never "active".
type QsoArchiveView struct {
	ID                  string              `json:"id"`
	Label               string              `json:"label"`
	Ownership           QsoArchiveOwnership `json:"ownership"`
	State               QsoArchiveState     `json:"state"`
	LastActivationError string              `json:"last_activation_error,omitempty"`
}

// QsoArchiveCreated is the POST /v1/qso-archives response: the archive, and
// whether the request key found one already made.
type QsoArchiveCreated struct {
	Archive QsoArchiveView `json:"archive"`
	Reused  bool           `json:"reused"`
}

// QsoArchiveActivation is the POST /v1/qso-archives/{id}/activate response
// (202): the candidate the next start activates, and whether the pending
// selector's write is known durable ("durable") or only probably so
// ("uncertain": the rename landed but its directory sync could not be
// confirmed — startup is safe under either truth).
type QsoArchiveActivation struct {
	ID         string `json:"id"`
	Durability string `json:"durability"`
}

const (
	QsoArchiveDurabilityDurable   = "durable"
	QsoArchiveDurabilityUncertain = "uncertain"
)
