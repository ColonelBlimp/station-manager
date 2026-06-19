package utils

import "testing"

func TestParseFreqMHz_MHzFloat(t *testing.T) {
	cases := map[string]int64{
		"7.050":   7050,
		"14.074":  14074,
		"14.310":  14310,
		"144.390": 144390,
		"3.573":   3573,
		"1.840":   1840,
	}
	for in, want := range cases {
		got, err := ParseFreqMHz(in)
		if err != nil {
			t.Fatalf("ParseFreqMHz(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseFreqMHz(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseFreqMHz_IntegerPassthrough(t *testing.T) {
	// Bare integer input is accepted as already-kHz for legacy clients.
	cases := map[string]int64{
		"7050":   7050,
		"14074":  14074,
		"144390": 144390,
	}
	for in, want := range cases {
		got, err := ParseFreqMHz(in)
		if err != nil {
			t.Fatalf("ParseFreqMHz(%q) error: %v", in, err)
		}
		if got != want {
			t.Fatalf("ParseFreqMHz(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParseFreqMHz_InvalidInput(t *testing.T) {
	invalid := []string{"", "abc", "14.abc", "not-a-number"}
	for _, in := range invalid {
		if _, err := ParseFreqMHz(in); err == nil {
			t.Fatalf("ParseFreqMHz(%q) expected error, got nil", in)
		}
	}
}

// FREQ is required and no caller uses zero as a sentinel, so a non-positive
// frequency is an input error here rather than a value that reaches storage
// (review 2026-06-19 M3). Both the MHz-decimal and bare-integer-kHz forms
// reject it.
func TestParseFreqMHz_RejectsNonPositive(t *testing.T) {
	for _, in := range []string{"0", "0.000", "-1", "-14.074", "-7050"} {
		if got, err := ParseFreqMHz(in); err == nil {
			t.Errorf("ParseFreqMHz(%q) = %d, nil; want error", in, got)
		}
	}
}

func TestParseFreqMHz_Rounding(t *testing.T) {
	// 14.0745 MHz = 14074.5 kHz → rounds to 14075.
	got, err := ParseFreqMHz("14.0745")
	if err != nil {
		t.Fatalf("ParseFreqMHz(\"14.0745\") error: %v", err)
	}
	if got != 14075 {
		t.Fatalf("ParseFreqMHz(\"14.0745\") = %d, want 14075", got)
	}
}

func TestFormatFreqMHz(t *testing.T) {
	cases := map[int64]string{
		14074:  "14.074",
		7050:   "7.050",
		7000:   "7.000",
		144390: "144.390",
		1840:   "1.840",
		// Sub-kHz granularity is lost in storage, but the formatter
		// itself just reflects what the DB column holds.
		0: "0.000",
	}
	for in, want := range cases {
		if got := FormatFreqMHz(in); got != want {
			t.Fatalf("FormatFreqMHz(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFreqRoundTrip(t *testing.T) {
	// MHz string → int64 kHz → MHz string should be idempotent on the
	// canonical three-decimal form.
	cases := []string{"7.050", "14.074", "144.390", "3.573", "7.000"}
	for _, in := range cases {
		kHz, err := ParseFreqMHz(in)
		if err != nil {
			t.Fatalf("ParseFreqMHz(%q): %v", in, err)
		}
		if got := FormatFreqMHz(kHz); got != in {
			t.Fatalf("round-trip %q → %d → %q", in, kHz, got)
		}
	}
}
