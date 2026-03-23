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
// Returns an error if the input cannot be parsed as a valid floating-point number.
func ConvertToXDDDMMM(input string, isLat bool) (string, error) {
	// Parse the input string to a float
	coord, err := strconv.ParseFloat(input, 64)
	if err != nil {
		return emptyString, err
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
