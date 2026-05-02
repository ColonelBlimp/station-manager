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
