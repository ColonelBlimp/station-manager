package utils

import (
	"fmt"
	"regexp"
	"strings"
)

// Maidenhead Locator System helpers for the daemon-side derivation of
// MY_LAT / MY_LON from MY_GRIDSQUARE on types.LoggingStation.
//
// Resolutions (canonical / ADIF-accepted):
//   - 4 chars: AA00       — 2°×1°  field+square         (~100×200 km)
//   - 6 chars: AA00aa     — 5'×2.5' subsquare           (~5×10 km)
//   - 8 chars: AA00aa00   — 0.5'×0.25' extended         (~0.5×1 km)
//
// Field (chars 1-2): A-R uppercase, 18 zones each 20° / 10° wide.
// Square (chars 3-4): 0-9, 10 zones each 2° / 1° wide.
// Subsquare (chars 5-6): a-x lowercase by spec; case-folded on input.
// Extended (chars 7-8): 0-9.
//
// The TypeScript-side mirror lives at
// frontend/app/src/lib/validators/maidenhead.ts; behaviour is kept
// in sync because both sides validate the same wire field.

var maidenheadPattern = regexp.MustCompile(`^[A-R]{2}[0-9]{2}([A-X]{2}([0-9]{2})?)?$`)

// IsValidMaidenhead reports whether s parses as a valid Maidenhead
// locator at the 4 / 6 / 8-char resolutions. Whitespace is trimmed and
// the input is case-folded before matching. Empty input is treated as
// "not invalid" — presence is a separate concern enforced by callers
// (matches the frontend validator's contract).
func IsValidMaidenhead(s string) bool {
	trimmed := strings.ToUpper(strings.TrimSpace(s))
	if trimmed == "" {
		return true
	}
	return maidenheadPattern.MatchString(trimmed)
}

// NormalizeMaidenhead returns the canonical mixed-case form of a valid
// Maidenhead locator: upper field, digits, lower subsquare, digits.
// Returns empty for empty/whitespace input. Returns the trimmed
// uppercase form unchanged when the input doesn't validate — callers
// should call IsValidMaidenhead first if they want a hard reject.
func NormalizeMaidenhead(s string) string {
	trimmed := strings.TrimSpace(s)
	if trimmed == "" {
		return ""
	}
	upper := strings.ToUpper(trimmed)
	if !maidenheadPattern.MatchString(upper) {
		return upper
	}
	switch len(upper) {
	case 4:
		return upper
	case 6:
		return upper[:4] + strings.ToLower(upper[4:6])
	case 8:
		return upper[:4] + strings.ToLower(upper[4:6]) + upper[6:8]
	}
	return upper
}

// MaidenheadToDecimal returns the latitude and longitude of the
// centre of the supplied locator's cell, in decimal degrees. ok is
// false when the input is empty or fails validation.
func MaidenheadToDecimal(s string) (lat, lon float64, ok bool) {
	lat, lon, _, _, ok = MaidenheadToCell(s)
	return lat, lon, ok
}

// MaidenheadToCell returns the locator's cell — its centre AND its extent,
// which is what a locator actually declares.
//
// The extent was always computed here to place the centre; it is returned
// because a consumer needs to know whether some OTHER coordinate pair falls
// inside the locator a station claims. That makes "these coordinates
// contradict this grid" answerable from the data's own precision rather than
// from an invented distance threshold. Mirrors gridToCell() in the SPA
// (frontend/app/src/lib/utils/bearing.ts) deliberately: both sides must agree
// on what "contradicts" means, or the map and the stored row disagree.
//
// ok is false when the input is empty or fails validation.
func MaidenheadToCell(s string) (lat, lon, latSpan, lonSpan float64, ok bool) {
	trimmed := strings.ToUpper(strings.TrimSpace(s))
	if trimmed == "" || !maidenheadPattern.MatchString(trimmed) {
		return 0, 0, 0, 0, false
	}

	lon = -180.0 + 20.0*float64(trimmed[0]-'A')
	lat = -90.0 + 10.0*float64(trimmed[1]-'A')
	lon += 2.0 * float64(trimmed[2]-'0')
	lat += 1.0 * float64(trimmed[3]-'0')

	// Cell size at this resolution; halved at the end for the centre.
	cellLon := 2.0
	cellLat := 1.0

	if len(trimmed) >= 6 {
		lon += (5.0 / 60.0) * float64(trimmed[4]-'A')
		lat += (2.5 / 60.0) * float64(trimmed[5]-'A')
		cellLon = 5.0 / 60.0
		cellLat = 2.5 / 60.0
	}
	if len(trimmed) == 8 {
		lon += (5.0 / 600.0) * float64(trimmed[6]-'0')
		lat += (2.5 / 600.0) * float64(trimmed[7]-'0')
		cellLon = 5.0 / 600.0
		cellLat = 2.5 / 600.0
	}

	lon += cellLon / 2.0
	lat += cellLat / 2.0

	return lat, lon, cellLat, cellLon, true
}

// MaidenheadToADIFLatLon converts a locator to ADIF "XDDD MM.MMM"
// strings for MY_LAT / MY_LON. Returns ok=false when the input is
// empty or invalid; the caller should clear MyLat/MyLon in that case.
func MaidenheadToADIFLatLon(s string) (myLat, myLon string, ok bool) {
	lat, lon, decOk := MaidenheadToDecimal(s)
	if !decOk {
		return "", "", false
	}

	latStr, err := ConvertToXDDDMMM(fmt.Sprintf("%.10f", lat), true)
	if err != nil {
		return "", "", false
	}
	lonStr, err := ConvertToXDDDMMM(fmt.Sprintf("%.10f", lon), false)
	if err != nil {
		return "", "", false
	}
	return latStr, lonStr, true
}
