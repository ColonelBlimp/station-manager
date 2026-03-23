package utils

import (
	"regexp"
	"strings"
	"time"
)

// FormatDate converts a raw date string in YYYYMMDD format into a formatted date string in YYYY-MM-DD format.
// Returns "YYYY-MM-DD" if the input does not have exactly 8 characters.
func FormatDate(rawDate string) string {
	if len(rawDate) != 8 {
		return "YYYY-MM-DD"
	}

	return rawDate[:4] + "-" + rawDate[4:6] + "-" + rawDate[6:]
}

// FormatTime converts a 4-digit string representing time in HHMM format to a string in HH:MM format. Returns "HH:MM" on error.
func FormatTime(rawTime string) string {
	if len(rawTime) != 4 {
		return "HH:MM"
	}
	return rawTime[:2] + ":" + rawTime[2:]
}

// IsValidDateYYYYMMDD validates a date string strictly in the format YYYYMMDD.
// Rules:
// - exactly 8 digits (no separators)
// - represents a real calendar date (UTC) including leap years
// - disallow all-zero date like 00000000
func IsValidDateYYYYMMDD(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) != 8 {
		return false
	}
	if s == "00000000" {
		return false
	}
	// time.Parse with layout 20060102 will validate month/day ranges and leap years.
	if _, err := time.Parse("20060102", s); err != nil {
		return false
	}
	return true
}

// IsValidTimeADIF validates an ADIF time string.
// Accepts:
// - HHMM (4 digits)
// - HHMMSS (6 digits)
// Where HH is 00-23, MM is 00-59, SS is 00-59. Spaces and separators are not allowed.
func IsValidTimeADIF(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if len(s) != 4 && len(s) != 6 {
		return false
	}
	// Use time.Parse to validate ranges; it will reject non-digits and out-of-range values
	layout := "1504"
	if len(s) == 6 {
		layout = "150405"
	}
	if _, err := time.Parse(layout, s); err != nil {
		return false
	}
	return true
}

func DateNowAsYYYYMMDD() string {
	return time.Now().UTC().Format("20060102")
}

func GenerateDateYYYYMMDD(t time.Time) string {
	return t.UTC().Format("20060102")
}

// SanitizeDateToYYYYMMDD converts date strings in formats YYYY-MM-DD or YYYY/MM/DD to YYYYMMDD.
// If the input is already in YYYYMMDD, it is returned unchanged. Leading/trailing spaces are ignored.
// Returns empty string if the input cannot be sanitized into a valid YYYYMMDD date.
func SanitizeDateToYYYYMMDD(s string) string {
	s = strings.TrimSpace(s)
	if s == emptyString {
		return emptyString
	}
	// Already no separators and valid
	if IsValidDateYYYYMMDD(s) {
		return s
	}
	// Replace common separators '-' and '/' with nothing if pattern matches YYYY[-|/]MM[-|/]DD
	re := regexp.MustCompile(`^(\d{4})[-\/]?(\d{2})[-\/]?(\d{2})$`)
	matches := re.FindStringSubmatch(s)
	if len(matches) == 4 {
		candidate := matches[1] + matches[2] + matches[3]
		if IsValidDateYYYYMMDD(candidate) {
			return candidate
		}
	}
	return emptyString
}

// SanitizeTimeToADIF converts a string time into compact ADIF formats HHMM or HHMMSS.
// Accepted inputs include:
// - HH:MM, H:MM, HH:MM:SS, H:MM:SS
// - Plain digits HHMM or HHMMSS
// - Separators ':', '-', '.', ' ' between parts will be ignored
// Returns empty string if it cannot be sanitized to a valid time.
func SanitizeTimeToADIF(s string) string {
	s = strings.TrimSpace(s)
	if s == emptyString {
		return emptyString
	}
	// Fast path: already valid 4 or 6 digits
	if IsValidTimeADIF(s) {
		return s
	}
	// If contains any non-digit separators, split into numeric parts
	sep := regexp.MustCompile(`[^0-9]+`)
	parts := sep.Split(s, -1)
	// Remove empty parts from edges or multiple separators
	filtered := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 2 || len(filtered) == 3 {
		pad2 := func(x string) string {
			if len(x) == 1 {
				return "0" + x
			}
			return x
		}
		hh := pad2(filtered[0])
		mm := pad2(filtered[1])
		if len(filtered) == 2 {
			candidate := hh + mm
			if IsValidTimeADIF(candidate) {
				return candidate
			}
			return emptyString
		}
		ss := pad2(filtered[2])
		candidate := hh + mm + ss
		if IsValidTimeADIF(candidate) {
			return candidate
		}
		return emptyString
	}
	// Otherwise, keep digits only and validate as 4 or 6 length
	digits := make([]rune, 0, len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			digits = append(digits, r)
		}
	}
	compact := string(digits)
	if len(compact) != 4 && len(compact) != 6 {
		return emptyString
	}
	if IsValidTimeADIF(compact) {
		return compact
	}
	return emptyString
}
