package api

import "github.com/ColonelBlimp/station-manager/internal/qsoservice"

// isValidCallsign delegates to the canonical validation in qsoservice.
func isValidCallsign(callsign string) bool {
	return qsoservice.IsValidCallsign(callsign)
}
