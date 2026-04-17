package qsoservice

import "unicode"

// IsValidCallsign checks that a callsign is at least 3 characters,
// at most 32 characters, and contains at least one digit.
// The input should already be trimmed and uppercased.
func IsValidCallsign(callsign string) bool {
	if len(callsign) < 3 || len(callsign) > 32 {
		return false
	}
	for _, r := range callsign {
		if unicode.IsDigit(r) {
			return true
		}
	}
	return false
}
