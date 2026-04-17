package qsoservice

import (
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const ServiceName = "qsoservice"

// Service is the daemon's domain service for QSO ingest. It coordinates
// parsing, validation, deduplication, and atomic storage.
type Service struct {
	DB     *sqlite.Service  `di.inject:"sqliteservice"`
	Logger *logging.Service `di.inject:"loggingservice"`
}

// Initialize satisfies the iocdi.Initializer interface.
func (s *Service) Initialize() error {
	return nil
}

// SubmitResult is the outcome of a QSO submission.
type SubmitResult struct {
	Status string `json:"status"` // "stored" or "duplicate"
	ID     int64  `json:"id"`
}

// SubmitError is a domain validation error returned by Submit. The handler
// layer translates it into the appropriate HTTP status and error envelope.
type SubmitError struct {
	Code    string // machine-readable code (e.g. "missing_required_field")
	Message string // human-readable detail
}

func (e *SubmitError) Error() string {
	return e.Code + ": " + e.Message
}

// IsSubmitError returns the SubmitError if err is one (or wraps one), or nil.
func IsSubmitError(err error) *SubmitError {
	var se *SubmitError
	if stderr.As(err, &se) {
		return se
	}
	return nil
}
