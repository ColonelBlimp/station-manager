package archive

import (
	"errors"
	"fmt"
	"os"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
)

// Activation failure codes (ADR 0071, W-0021 activation follow-up). The daemon
// owns the classification: a code is what the catalogue entry persists, what
// the listing and the Station Event carry, and what the SPA turns into words —
// never the Go error chain or a filesystem path, which stay in the log.
const (
	FailFileMissing      = "archive_file_missing"
	FailFileUnreadable   = "archive_file_unreadable"
	FailNoIdentity       = "archive_no_identity"
	FailIdentityMismatch = "archive_identity_mismatch"
	FailPromotionPersist = "promotion_persist_failed"
	FailPendingUnclear   = "pending_unclear"
	// FailStart is every other reason a candidate generation did not come up.
	FailStart = "archive_start_failed"
)

// ActivationFailure is a classified activation failure: the stable Code and
// the diagnostic Err (paths, ids, the underlying error) for the log.
type ActivationFailure struct {
	Code string
	Err  error
}

func (f *ActivationFailure) Error() string { return f.Code + ": " + f.Err.Error() }
func (f *ActivationFailure) Unwrap() error { return f.Err }

// FailureCode classifies any activation error: a wrapped ActivationFailure
// yields its code, anything else is FailStart.
func FailureCode(err error) string {
	var f *ActivationFailure
	if errors.As(err, &f) {
		return f.Code
	}
	return FailStart
}

// FailureMessage is the operator's wording for a code — plain, stable, without
// paths. An unknown or legacy value (an entry written before codes existed
// held free text) reads as the generic failure.
func FailureMessage(code string) string {
	switch code {
	case "":
		return ""
	case FailFileMissing:
		return "The archive's file is missing."
	case FailFileUnreadable:
		return "The archive's file could not be read."
	case FailNoIdentity:
		return "The file at the archive's path is not a Station Manager archive."
	case FailIdentityMismatch:
		return "The file at the archive's path belongs to a different archive."
	case FailPromotionPersist:
		return "The switch could not be recorded in the station's configuration."
	case FailPendingUnclear:
		return "The restart could not be requested and the request could not be withdrawn; the next restart will try this archive."
	default:
		return "The daemon could not start on this archive."
	}
}

// NormalizeFailureCode maps what an entry holds to a stable code: a known code
// passes through; anything else (legacy free text) is FailStart.
func NormalizeFailureCode(stored string) string {
	switch stored {
	case "", FailFileMissing, FailFileUnreadable, FailNoIdentity, FailIdentityMismatch, FailPromotionPersist, FailPendingUnclear, FailStart:
		return stored
	}
	return FailStart
}

// VerifyIdentity proves that the file a catalogue entry names is that archive
// (the file's embedded identity is the authority). It is the one classifier
// both the activation preflight and the start-time open use, so a missing file
// is reported as missing on both paths — the stat comes first, before SQLite
// is asked to open anything (a read-only open beside stale sidecars can
// otherwise answer "no such table" for a file that is not there). A
// pre-adoption install (no entry) has nothing to verify.
func VerifyIdentity(paths Paths) error {
	if paths.Entry == nil {
		return nil
	}
	if _, err := os.Stat(paths.QSO); err != nil {
		if os.IsNotExist(err) {
			return &ActivationFailure{Code: FailFileMissing, Err: fmt.Errorf("archive %s (%s): no file at %s", paths.Entry.ID, paths.Entry.Label, paths.QSO)}
		}
		return &ActivationFailure{Code: FailFileUnreadable, Err: fmt.Errorf("archive %s (%s): stat %s: %w", paths.Entry.ID, paths.Entry.Label, paths.QSO, err)}
	}
	identity, found, err := sqlite.PeekArchiveIdentity(paths.QSO)
	switch {
	case err != nil:
		return &ActivationFailure{Code: FailFileUnreadable, Err: fmt.Errorf("archive %s (%s): read the identity of %s: %w", paths.Entry.ID, paths.Entry.Label, paths.QSO, err)}
	case !found:
		return &ActivationFailure{Code: FailNoIdentity, Err: fmt.Errorf("archive %s (%s) at %s carries no identity; it is not the provisioned or adopted file the catalogue names", paths.Entry.ID, paths.Entry.Label, paths.QSO)}
	case identity.ArchiveUUID != paths.Entry.ID:
		return &ActivationFailure{Code: FailIdentityMismatch, Err: fmt.Errorf("catalogue entry %s (%s) names %s, but that file holds archive %s; refusing to touch it", paths.Entry.ID, paths.Entry.Label, paths.QSO, identity.ArchiveUUID)}
	}
	return nil
}
