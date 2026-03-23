package errors

import stderr "errors"

// ErrNotFound is a non-domain-specific sentinel error for when a value is not found.
var ErrNotFound = stderr.New("Not found")
