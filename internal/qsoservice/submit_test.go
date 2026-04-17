package qsoservice

import (
	"testing"
)

// ---- FreqMHzToKHzString tests ----

func TestFreqMHzToKHzString_MHzFloat(t *testing.T) {
	cases := map[string]string{
		"7.050":   "7050",
		"14.074":  "14074",
		"14.310":  "14310",
		"144.390": "144390",
		"3.573":   "3573",
		"1.840":   "1840",
	}
	for in, want := range cases {
		got, err := FreqMHzToKHzString(in)
		if err != nil {
			t.Fatalf("FreqMHzToKHzString(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("FreqMHzToKHzString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFreqMHzToKHzString_IntegerPassthrough(t *testing.T) {
	// Already integer kHz — should pass through unchanged.
	cases := map[string]string{
		"7050":   "7050",
		"14074":  "14074",
		"144390": "144390",
	}
	for in, want := range cases {
		got, err := FreqMHzToKHzString(in)
		if err != nil {
			t.Fatalf("FreqMHzToKHzString(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("FreqMHzToKHzString(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFreqMHzToKHzString_InvalidInput(t *testing.T) {
	invalid := []string{"", "abc", "14.abc", "not-a-number"}
	for _, in := range invalid {
		_, err := FreqMHzToKHzString(in)
		if err == nil {
			t.Fatalf("FreqMHzToKHzString(%q) expected error, got nil", in)
		}
	}
}

func TestFreqMHzToKHzString_Rounding(t *testing.T) {
	// 14.0745 MHz = 14074.5 kHz → should round to 14075
	got, err := FreqMHzToKHzString("14.0745")
	if err != nil {
		t.Fatalf("FreqMHzToKHzString(\"14.0745\") error: %v", err)
	}
	if got != "14075" {
		t.Fatalf("FreqMHzToKHzString(\"14.0745\") = %q, want %q", got, "14075")
	}
}
