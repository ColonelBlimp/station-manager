package qsoservice

import (
	stderr "errors"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const ServiceName = "qsoservice"

// Service is the daemon's domain service for QSO ingest. It coordinates
// parsing, validation, deduplication, atomic storage, and forwarder
// queue fan-out (per docs/v2-design/forwarding.md §6).
type Service struct {
	DB     *sqlite.Service  `di.inject:"sqliteservice"`
	Logger *logging.Service `di.inject:"loggingservice"`
	Config *config.Service  `di.inject:"configservice"`
	Hub    *events.Hub      `di.inject:"eventhub"`
}

// Initialize satisfies the iocdi.Initializer interface. It fails fast when a
// required dependency wasn't injected, so a DI-tag change or partial test
// harness surfaces a clear startup error rather than a nil-deref panic on the
// first QSO submit/update/delete (review 2026-06-19 L1).
func (s *Service) Initialize() error {
	const op errors.Op = "qsoservice.Service.Initialize"
	switch {
	case s.DB == nil:
		return errors.New(op).WithMsg("DB dependency not injected")
	case s.Config == nil:
		return errors.New(op).WithMsg("Config dependency not injected")
	case s.Logger == nil:
		return errors.New(op).WithMsg("Logger dependency not injected")
	case s.Hub == nil:
		return errors.New(op).WithMsg("Hub dependency not injected")
	}
	return nil
}

// SubmitResult is the outcome of a QSO submission.
//
// UUID is the canonical external identifier per ADR 0016 (UUIDv7,
// time-ordered, daemon-generated at create time). ID is the local
// SQLite primary key kept for transitional compatibility while
// callers migrate to UUID-keyed lookups; future surface-area work
// (PUT/DELETE by UUID, ADIF emission of APP_SM_QSO_ID) is phase 2.
type SubmitResult struct {
	Status string `json:"status"` // "stored" or "duplicate"
	UUID   string `json:"uuid"`
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
