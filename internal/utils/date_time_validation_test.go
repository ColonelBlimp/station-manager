package utils

import "testing"

func TestIsValidDateYYYYMMDD(t *testing.T) {
	valid := []string{
		"20250101",
		"19990228",
		"20000229", // leap year
	}
	for _, v := range valid {
		if !IsValidDateYYYYMMDD(v) {
			t.Fatalf("expected valid date: %q", v)
		}
	}

	invalid := []string{
		"",
		"2025-01-01",
		"2025011",
		"202501011",
		"20251301", // month > 12
		"20250230", // invalid day
		"20010229", // not leap year
		"abcdefgh",
	}
	for _, v := range invalid {
		if IsValidDateYYYYMMDD(v) {
			t.Fatalf("expected invalid date: %q", v)
		}
	}
}
