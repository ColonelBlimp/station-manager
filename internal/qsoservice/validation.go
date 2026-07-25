package qsoservice

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/internal/enums/modes"
)

// validateSubmodeMatchesMode rejects a SUBMODE that is KNOWN to belong to a
// different main mode: an inconsistent pair (MODE=CW, SUBMODE=USB) would
// otherwise be stored and forwarded to QRZ/ClubLog as contradictory ADIF. An
// UNKNOWN submode passes — the catalogue is operator-extendable, so an
// unlisted-but-valid ADIF submode must not block an import.
//
// Both create and edit call this, because the invariant is only as strong as its
// weaker path: a PATCH naming just MODE leaves the stored SUBMODE untouched and
// re-forms at edit time exactly the pair refused at creation. Keeping it in one
// function is what stops the two paths drifting apart again.
//
// Inputs should already be trimmed and uppercased (same contract as
// IsValidCallsign) — the lookup normalizes its own argument, but the parent it
// returns is canonical uppercase and is compared against mode directly.
func validateSubmodeMatchesMode(mode, submode string) error {
	if mode == "" || submode == "" {
		return nil
	}
	parent, ok := modes.GetModeBySubmode(submode)
	if !ok || parent.String() == mode {
		return nil
	}
	return &SubmitError{
		Code:    "invalid_field_value",
		Message: fmt.Sprintf("SUBMODE %q belongs to mode %q, not %q", submode, parent.String(), mode),
	}
}

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
