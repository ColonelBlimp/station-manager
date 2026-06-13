package api

import (
	"github.com/ColonelBlimp/station-manager/internal/qsoservice"
)

// isValidCallsign delegates to the canonical validation in qsoservice. Used by
// the QSO / logbook / contact-history / enrich / contest-dupe handlers. The
// config station_callsign rule lives separately in internal/config (it can't
// import qsoservice — cycle — so the rule is mirrored there; config.md §12).
func isValidCallsign(callsign string) bool {
	return qsoservice.IsValidCallsign(callsign)
}
