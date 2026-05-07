// Package source defines the audit-trail source enum used by qso_history.
//
// A Source identifies which subsystem of the daemon performed a QSO
// mutation. The qso_history table stores this as freetext (so an
// out-of-band operator action via sqlite3 can be recorded with whatever
// label fits), but daemon-internal callers should use a typed constant.
//
// Only constants for sources the daemon actually emits today are
// declared. New sources (worker, importer, etc.) are added one line at
// a time as they appear — no speculative entries.
package source

import "fmt"

// Source identifies the daemon subsystem responsible for a QSO mutation.
type Source string

const (
	// API is the standard HTTP API surface — PATCH/DELETE on /v1/qso/{uuid}.
	API Source = "api"
)

func (s Source) String() string {
	return string(s)
}

// Parse converts a string to a Source. Returns an error for unknown values.
// The qso_history.source column is freetext, so values read back from the
// DB may be outside the typed set; callers that need to round-trip through
// this enum should be ready to handle the error.
func Parse(s string) (Source, error) {
	switch s {
	case "api":
		return API, nil
	default:
		return "", fmt.Errorf("unknown source: %q", s)
	}
}
