package utils

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
)

var (
	ErrFrequencyTooShort = errors.New("frequency string too short (minimum 9 characters)")
	ErrFrequencyTooLong  = errors.New("frequency string too long (maximum 10 characters)")
	ErrFrequencySyntax   = errors.New("invalid frequency string (must have 2 periods)")
	ErrFrequencyInvalid  = errors.New("invalid frequency string (must have 9 characters)")
)

// FrequencyRanges holds the mapping of frequency prefixes to their min and max ranges.
var FrequencyRanges = map[string][2]float64{
	"54.": {50.000000, 54.000000},
	"53.": {50.000000, 54.000000},
	"52.": {50.000000, 54.000000},
	"51.": {50.000000, 54.000000},
	"50.": {50.000000, 54.000000},
	"29.": {28.000000, 29.700000},
	"28.": {28.000000, 29.700000},
	"24.": {24.890000, 24.990000},
	"21.": {21.000000, 21.450000},
	"18.": {18.068000, 18.168000},
	"14.": {14.000000, 14.350000},
	"10.": {10.100000, 10.150000},
	"7.":  {7.000000, 7.200000},
	"5.":  {5.351500, 5.366500},
	"3.":  {3.500000, 3.800000},
	"2.":  {1.810000, 2.000000},
	"1.":  {1.810000, 2.000000},
}

var BandNames = map[string]string{
	"54.": "6m",
	"53.": "6m",
	"52.": "6m",
	"51.": "6m",
	"50.": "6m",
	"29.": "10m",
	"28.": "10m",
	"24.": "12m",
	"21.": "15m",
	"18.": "17m",
	"14.": "20m",
	"10.": "30m",
	"7.":  "40m",
	"5.":  "60m",
	"3.":  "80m",
	"2.":  "160m",
	"1.":  "160m",
}

// FormatFrequencyToKhz converts a 9-character raw frequency string into a formatted frequency string in kHz format.
// Returns an error if the input string length is invalid.
func FormatFrequencyToKhz(rawFreq string) (string, error) {
	if len(rawFreq) != 9 {
		return "0.000.000", ErrFrequencyInvalid
	}
	mhz := strings.TrimLeft(rawFreq[1:3], "0")
	khz := rawFreq[3:6]
	hz := rawFreq[6:]
	return mhz + dotString + khz + dotString + hz, nil
}

// GetFrequencyRange retrieves the min and max frequency range for a given frequency prefix.
// It returns the minimum and maximum frequency values if a match is found, or 0, 0 if no match exists.
func GetFrequencyRange(freq string) (float64, float64) {
	for prefix, ranges := range FrequencyRanges {
		if strings.HasPrefix(freq, prefix) {
			return ranges[0], ranges[1]
		}
	}
	return 0, 0
}

// FrequencyToBand determines the band corresponding to a given frequency string using predefined mappings.
// It returns the band name if a match is found or an empty string if no match exists.
func FrequencyToBand(freq string) string {
	for prefix, band := range BandNames {
		if strings.HasPrefix(freq, prefix) {
			return band
		}
	}
	return emptyString
}

// FormatFrequencyToMhz formats a raw frequency string (e.g., "014.074.000" or "14.074") into MHz format "14.074".
// It is lenient about length and focuses on dot-separated parts; returns an error if structure is clearly invalid.
func FormatFrequencyToMhz(rawFreq string) (string, error) {
	if rawFreq == emptyString {
		return emptyString, nil
	}
	parts := strings.Split(rawFreq, dotString)
	switch len(parts) {
	case 1:
		// No dot present, cannot infer MHz with decimals reliably
		return emptyString, ErrFrequencySyntax
	case 2:
		// Already MHz with decimals (e.g., "14.074" or "144.390")
		mhz := strings.TrimLeft(parts[0], "0")
		return mhz + dotString + parts[1], nil
	default:
		// Three or more parts: drop the last part (Hz) and keep MHz.KHz
		mhz := strings.TrimLeft(parts[0], "0")
		return mhz + dotString + parts[1], nil
	}
}

func IsValidFrequencyMHz(s string) bool {
	s = strings.TrimSpace(s)
	if s == emptyString {
		return false
	}
	re := regexp.MustCompile(`^\d{7,8}$`)
	if !re.MatchString(s) {
		return false
	}
	f, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return false
	}
	return f > 0
}
