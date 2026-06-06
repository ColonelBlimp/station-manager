package qsoservice

// Callsign length bounds (inclusive). The error messages in submit.go /
// update.go quote "3-32" in prose; keep them in step with these if changed.
const (
	minCallsignLen = 3
	maxCallsignLen = 32
)

// IsValidCallsign checks that a callsign:
//   - is 3 to 32 characters long;
//   - contains only ASCII letters, ASCII digits, and the recognised
//     separators '/' and '-' (slash for portable/mobile/maritime
//     suffixes like G4ABC/P, dash occasionally seen in special-event
//     calls);
//   - contains at least one ASCII digit.
//
// The character-set restriction (review m6) defends downstream LIKE-
// based queries that interpolate the validated callsign without their
// own ESCAPE clause — notably FetchQsoSliceByCallsignWithContext, which
// builds `<callsign>/%` to match suffix-decorated variants. A typo'd
// callsign containing '%' or '_' would otherwise silently over-match.
//
// The input should already be trimmed and uppercased.
func IsValidCallsign(callsign string) bool {
	if len(callsign) < minCallsignLen || len(callsign) > maxCallsignLen {
		return false
	}
	hasDigit := false
	for i := 0; i < len(callsign); i++ {
		c := callsign[i]
		switch {
		case c >= '0' && c <= '9':
			hasDigit = true
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c == '/' || c == '-':
		default:
			return false
		}
	}
	return hasDigit
}
