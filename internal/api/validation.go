package api

import (
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// isValidCallsign delegates to the canonical validation in qsoservice.
func isValidCallsign(callsign string) bool {
	return qsoservice.IsValidCallsign(callsign)
}

// isValidZone reports whether s is a base-10 integer in [minVal,
// maxVal]. Used by CQ Zone (1-40), ITU Zone (1-90), and DXCC entity
// (0-522) validation. Trims nothing — caller is expected to have
// trimmed already, since these validators are called from the PUT
// handler which already trims per-field. Returns false for empty
// input; callers gate on empty separately to allow operators to
// clear the field.
func isValidZone(s string, minVal, maxVal int) bool {
	n, err := strconv.Atoi(s)
	if err != nil {
		return false
	}
	return n >= minVal && n <= maxVal
}
