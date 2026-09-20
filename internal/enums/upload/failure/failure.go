// Package failure defines the durable classification of a terminal upload
// failure — WHY a qso_upload row settled `failed`, in a form recovery code can
// act on without parsing provider-owned, redacted error text.
//
// The class is orthogonal to last_error (the operator-readable reason) and is
// meaningful only on a `failed` row. It exists so that a credential the
// operator has since corrected can re-arm exactly the rows that credential
// stranded (W-0010 outcome 9, ruling 2026-09-20 (c)): a station-callsign
// mismatch or a malformed record is not made retryable by a new key.
package failure

import "fmt"

// Class identifies why an upload failed terminally.
type Class string

const (
	// Auth — the destination rejected the configured credential (QRZ
	// RESULT=AUTH or HTTP 401, SM Cloud HTTP 401, ClubLog HTTP 403). Rows in
	// this class are re-armed at worker start, once per daemon restart.
	Auth Class = "auth"
)

func (c Class) String() string {
	return string(c)
}

// Parse converts a string to a Class. Returns an error for unknown values.
//
// The Go-side guard exists alongside the column's CHECK constraint, not instead
// of it: this one fails at the call site with the offending value named, before
// a statement is issued. The empty string is NOT a Class — an unclassified
// failure is stored as NULL, never as "".
func Parse(s string) (Class, error) {
	switch s {
	case "auth":
		return Auth, nil
	default:
		return "", fmt.Errorf("unknown failure class %q", s)
	}
}
