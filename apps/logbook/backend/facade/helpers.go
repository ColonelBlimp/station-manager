package facade

import "unicode"

// isAllNumbers checks if a string contains only numeric digits.
func isAllNumbers(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return len(s) > 0
}
