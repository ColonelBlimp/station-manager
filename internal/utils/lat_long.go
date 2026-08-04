package utils

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// ConvertToXDDDMMM converts a latitude or longitude string to the XDDD MMM.MMM format.
// isLat must be true for latitude (N/S) and false for longitude (E/W).
// Returns an error if the input cannot be parsed as a valid floating-point
// number, is non-finite (NaN/±Inf), or is out of geographic bounds — latitude
// must be within ±90°, longitude within ±180° (review 2026-06-19 L1). The
// daemon's only caller feeds Maidenhead-derived values that are already in
// range, but this is an exported helper on the shared utility surface, so it
// rejects impossible coordinates rather than formatting them (e.g. lat 91).
func ConvertToXDDDMMM(input string, isLat bool) (string, error) {
	// Parse the input string to a float
	coord, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return emptyString, err
	}
	if math.IsNaN(coord) || math.IsInf(coord, 0) {
		return emptyString, fmt.Errorf("coordinate is not finite: %q", input)
	}
	limit := 180.0
	kind := "longitude"
	if isLat {
		limit = 90.0
		kind = "latitude"
	}
	if math.Abs(coord) > limit {
		return emptyString, fmt.Errorf("%s out of range (|%g| > %g)", kind, coord, limit)
	}

	// Determine the directional character
	var direction string
	if isLat {
		if coord < 0 {
			direction = "S"
		} else {
			direction = "N"
		}
	} else {
		if coord < 0 {
			direction = "W"
		} else {
			direction = "E"
		}
	}
	coord = math.Abs(coord)

	// Extract degrees and minutes
	degrees := int(coord)
	minutes := (coord - float64(degrees)) * 60

	// Normalize rounding so that 59.9995 -> 60.000 carries into degrees
	minutes = math.Round(minutes*1000) / 1000
	if minutes >= 60.0 {
		degrees += 1
		minutes = 0
	}

	// Format degrees and minutes
	degreesStr := fmt.Sprintf("%03d", degrees)
	minutesStr := fmt.Sprintf("%06.3f", minutes)

	// Combine into the final format
	result := strings.TrimSpace(fmt.Sprintf("%s%s %s", direction, degreesStr, minutesStr))
	return result, nil
}

// IsXDDDMMM returns true if s matches the XDDD MMM.MMM latitude/longitude format.
// Acceptable directions: N, S, E, W.
// Degrees must be zero-padded to 3 digits (000–180), minutes must be zero-padded with exactly
// two digits before the decimal point and exactly three digits after (00.000–59.999).
// Note: When degrees = 180, minutes must be 00.000 to be a valid coordinate; this function enforces that.
func IsXDDDMMM(s string) bool {
	// Quick structural check: one direction letter, three digits, space, two digits, dot, three digits
	re := regexp.MustCompile(`^[NSEW][0-9]{3} [0-9]{2}\.[0-9]{3}$`)
	if !re.MatchString(s) {
		return false
	}

	// Split into parts
	degStr := s[1:4]
	minStr := s[5:]

	deg, err := strconv.Atoi(degStr)
	if err != nil {
		return false
	}
	// minutes as float (with exactly three decimals by regex)
	mi, err := strconv.ParseFloat(minStr, 64)
	if err != nil {
		return false
	}

	// Validate numeric bounds
	if deg < 0 || deg > 180 {
		return false
	}
	// Minutes must be in [0, 60); allow exactly 60.000 only when it would carry, but since
	// canonical format from ConvertToXDDDMMM never outputs 60.000 we disallow it here.
	if mi < 0.0 || mi >= 60.0 {
		return false
	}
	// If degrees is 180, minutes must be 0
	if deg == 180 && mi != 0.0 {
		return false
	}

	return true
}

// ConvertFromXDDDMMM is the inverse of ConvertToXDDDMMM: it parses an ADIF
// Location ("XDDD MM.MMM") back to signed decimal degrees, formatted to six
// decimal places to match what enrichment stores.
//
// isLat is not optional, and its absence was a defect (codex c3d99362 /
// fd3062b7, both P1). The forward direction validates the hemisphere against
// the axis and the degrees against that axis's limit; an inverse that did not
// accepted "E022 58.119" as a LATITUDE and "N180 00.000" as anything, turning
// malformed input into plausible decimals and admitting positions beyond the
// poles. An inverse must be exactly as strict as the function it inverts, or it
// is a hole in whichever boundary calls it.
//
// Anything that is not a well-formed Location for THIS axis is refused rather
// than guessed at, including a bare decimal: callers treat the error as "leave
// this value alone" or "do not admit it", depending on which side they guard.
func ConvertFromXDDDMMM(s string, isLat bool) (string, error) {
	if !IsXDDDMMM(s) {
		return emptyString, fmt.Errorf("not an ADIF Location: %q", s)
	}
	hemi := s[0]
	if isLat && hemi != 'N' && hemi != 'S' {
		return emptyString, fmt.Errorf("hemisphere %q is not a latitude: %q", string(hemi), s)
	}
	if !isLat && hemi != 'E' && hemi != 'W' {
		return emptyString, fmt.Errorf("hemisphere %q is not a longitude: %q", string(hemi), s)
	}
	deg, err := strconv.ParseFloat(s[1:4], 64)
	if err != nil {
		return emptyString, err
	}
	min, err := strconv.ParseFloat(s[5:], 64)
	if err != nil {
		return emptyString, err
	}
	val := deg + min/60.0
	limit := 180.0
	kind := "longitude"
	if isLat {
		limit = 90.0
		kind = "latitude"
	}
	if val > limit {
		return emptyString, fmt.Errorf("%s out of range (%g > %g): %q", kind, val, limit, s)
	}
	if hemi == 'S' || hemi == 'W' {
		val = -val
	}
	return strconv.FormatFloat(val, 'f', 6, 64), nil
}
