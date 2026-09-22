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
}
