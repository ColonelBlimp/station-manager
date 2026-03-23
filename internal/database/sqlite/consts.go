package sqlite

import "time"

const (
	errMsgNotInitialized = "Database service is not initialized."
	errMsgNilService     = "Database service is nil."
	errMsgNilConfig      = "Database config is nil."
	errMsgNotOpen        = "Database service is not open."
	errMsgAlreadyOpen    = "Database service is already open."
	errMsgConfigInvalid  = "Database configuration is invalid."
	errMsgPingFailed     = "Failed to ping database."
	errMsgConnFailed     = "Database connection failed."
	errMsgFailedClose    = "Failed to close database connection."
	errMsgMigrateFailed  = "Failed to run database migrations."
	errMsgEmptyPath      = "SQLite path cannot be empty."
	errMsgDsnBuildError  = "Failed to build DSN."
)

const (
	errMsgInvalidId = "Invalid ID"
)

const (
	SqliteDriver = "sqlite"
	emptyString  = ""

	// pingRetryBackoff is the delay between ping retry attempts for transient errors.
	// Kept short since SQLite is local and busy_timeout PRAGMA handles most wait scenarios.
	pingRetryBackoff = 25 * time.Millisecond

	// uploadRetryCooldown is the minimum time that must pass before a failed upload
	// can be retried. This prevents rapid retry loops for persistently failing uploads.
	uploadRetryCooldown = 5 * time.Minute

	// defaultUploadBatchLimit is the default number of pending uploads to process per batch
	// when QsoForwardingRowLimit is not configured.
	defaultUploadBatchLimit = 5
)
