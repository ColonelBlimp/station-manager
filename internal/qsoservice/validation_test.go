package qsoservice

import "testing"

func TestIsValidCallsign(t *testing.T) {
	valid := []string{
		"K1A",      // minimum: 3 chars, 1 digit
		"G4ABC",    // typical UK
		"7Q5MLV",   // Malawi prefix
		"VK3ABC",   // Australia
		"JA1ABC",   // Japan
		"DL1ABC",   // Germany
		"W1AW",     // ARRL HQ
		"7Q5MLV/T", // portable suffix
		"ZL1ABC",   // New Zealand
	}
	for _, cs := range valid {
		if !IsValidCallsign(cs) {
			t.Fatalf("expected %q to be valid", cs)
		}
	}

	invalid := []string{
		"",     // empty
		"K",    // too short
		"KA",   // too short
		"ABC",  // no digit
		"ABCD", // no digit, 4 chars
	}
	for _, cs := range invalid {
		if IsValidCallsign(cs) {
			t.Fatalf("expected %q to be invalid", cs)
		}
	}
}
