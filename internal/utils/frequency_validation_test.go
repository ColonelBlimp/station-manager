package utils

import "testing"

func TestIsValidFrequencyMHz(t *testing.T) {
	// Valid frequencies are 7-8 digit integer strings representing Hz
	// e.g., 7074000 = 7.074 MHz, 14074000 = 14.074 MHz
	valid := []string{
		"7074000",  // 7.074 MHz (40m)
		"7050000",  // 7.050 MHz (40m)
		"14074000", // 14.074 MHz (20m)
		"14250000", // 14.250 MHz (20m)
		"21074000", // 21.074 MHz (15m)
		"28074000", // 28.074 MHz (10m)
		"50313000", // 50.313 MHz (6m)
		"1840000",  // 1.840 MHz (160m) - 7 digits
		"3573000",  // 3.573 MHz (80m) - 7 digits
	}
	for _, v := range valid {
		if !IsValidFrequencyMHz(v) {
			t.Errorf("expected valid frequency: %q", v)
		}
	}

	invalid := []string{
		"",           // empty
		"707400",     // too short (6 digits)
		"140740000",  // too long (9 digits)
		"14ABC000",   // contains letters
		"14.074",     // decimal format not allowed
		"14.074.000", // dotted format not allowed
		"0000000",    // all zeros (zero value)
		"00000000",   // all zeros (zero value)
		"-7074000",   // negative
		"7,074,000",  // commas not allowed
		"7074000 ",   // trailing space (trimmed, but then valid - checking behavior)
		" 7074000",   // leading space (trimmed, but then valid - checking behavior)
	}
	for _, v := range invalid {
		// Note: strings with only whitespace around valid numbers will be trimmed and pass
		// The function trims spaces, so " 7074000" becomes "7074000" which is valid
		trimmed := v
		if trimmed == " 7074000" || trimmed == "7074000 " {
			// These become valid after trimming - skip them from invalid list
			continue
		}
		if IsValidFrequencyMHz(v) {
			t.Errorf("expected invalid frequency: %q", v)
		}
	}
}

func TestIsValidFrequencyMHz_EdgeCases(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"minimum 7 digits", "1800000", true},
		{"maximum 8 digits", "99999999", true},
		{"with leading spaces", " 7074000", true},  // trimmed
		{"with trailing spaces", "7074000 ", true}, // trimmed
		{"only spaces", "   ", false},
		{"7 zeros", "0000000", false},          // zero value
		{"8 zeros", "00000000", false},         // zero value
		{"starts with zero", "07074000", true}, // leading zero OK, value > 0
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsValidFrequencyMHz(tt.input)
			if got != tt.want {
				t.Errorf("IsValidFrequencyMHz(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
