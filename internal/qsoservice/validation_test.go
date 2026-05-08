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

		// Review m6 — callsign goes into LIKE patterns downstream;
		// any wildcard / SQL meta or other non-callsign character
		// must be rejected here so the LIKE path stays well-formed.
		"K1%",         // SQL LIKE wildcard
		"K1_",         // SQL LIKE single-char wildcard
		"K1*",         // shell wildcard, not a callsign character
		"K1\\A",       // backslash (LIKE escape char) not allowed
		"K1 A",        // whitespace not allowed
		"K1.A",        // period not allowed
		"K1+A",        // plus not allowed
		"K1\nA",       // newline not allowed
		"K1\tA",       // tab not allowed
		"Käse1",       // non-ASCII letter not allowed
		"K1ÆABC",      // non-ASCII not allowed
		"K1@ABC",      // @ not allowed
		"K1'OR'1='1",  // SQL injection attempt
		"K1'-DROP--A", // SQL injection attempt
	}
	for _, cs := range invalid {
		if IsValidCallsign(cs) {
			t.Fatalf("expected %q to be invalid", cs)
		}
	}
}
