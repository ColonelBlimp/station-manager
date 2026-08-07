package errors

import stderr "errors"

// ErrNotFound is a non-domain-specific sentinel error for when a value is not found.
var ErrNotFound = stderr.New("Not found")

// ErrDuplicateName signals that an INSERT or UPDATE would violate a
// uniqueness constraint on a human-meaningful name (e.g. logbook
// name). Handlers map this to 409 Conflict. Distinguishing it from a
// generic UNIQUE-constraint failure lets the daemon return a clear
// "duplicate_name" error code instead of a raw SQL string match.
var ErrDuplicateName = stderr.New("duplicate name")

// ErrLogbookHasQsos signals an attempt to delete a logbook whose
// child rows are still present. Handlers map this to 409 Conflict
// with the "has_qsos" code. Promoting this from a string-match in
// the handler layer lets the constraint message change without
// breaking the response shape.
var ErrLogbookHasQsos = stderr.New("logbook contains QSOs")

// ErrUploadReArmed signals that an upload-completion write (mark
// success/failed/transient-retry) matched no row because a concurrent
// operator edit re-armed the claimed row from 'in_progress' back to
// 'pending' mid-send. It is NOT a failure: the re-armed row stays
// pending to be re-claimed and re-forwarded with the latest state.
// The forwarder worker matches this (errors.Is) to SKIP publishing a
// terminal forward.succeeded/forward.failed event and the ADIF-stamp
// mirror hook — the transition did not actually commit, so consumers
// must not be told a terminal state was reached (review 2026-07-20
// internal/forwarding #4). Distinct from ErrNotFound (the row is gone
// — a genuine bug).
var ErrUploadReArmed = stderr.New("upload row re-armed by concurrent edit")

// ErrStaleRevision signals an optimistic-concurrency refusal on the QSO edit
// path (review 2026-08-07 #2): the row's trigger-maintained revision counter
// (ADR 0050) moved between the caller's fetch and its UPDATE, so the
// revision-guarded write matched zero rows. Distinct from ErrNotFound (the
// row is gone or soft-deleted): here the row EXISTS at a newer revision, and
// the caller must refuse the stale edit — silently re-applying it would
// overwrite the newer one (the lost-update shape) and write a duplicate
// before-image into the audit chain. Handlers map this to 409 edit_conflict.
var ErrStaleRevision = stderr.New("stale revision")
