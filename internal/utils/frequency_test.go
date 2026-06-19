package utils

import "testing"

func TestFormatFrequencyToKhz(t *testing.T) {
	got, err := FormatFrequencyToKhz("014074000")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "14.074.000" {
		t.Fatalf("got %q; want %q", got, "14.074.000")
	}
	// invalid length
	if _, err := FormatFrequencyToKhz("01407400"); err == nil {
		t.Fatal("expected error for invalid length")
	}
}

func TestFrequencyToBand(t *testing.T) {
	cases := map[string]string{
		"14.074":  "20m",
		"7.050":   "40m",
		"50.313":  "6m",
		"1.900":   "160m",
		"144.174": "2m",
		"999.999": "",
	}
	for in, want := range cases {
		if got := FrequencyToBand(in); got != want {
			t.Fatalf("FrequencyToBand(%q) = %q; want %q", in, got, want)
		}
	}
}

// In-prefix-out-of-band values used to be mislabelled by the old prefix scheme
// (review 2026-06-19 M1): 14.999 → 20m, 7.999 → 40m, 5.000 → 60m, 24.100 → 12m.
// They must now classify as out of band (empty), and the band edges + just-past
// values must land correctly.
func TestFrequencyToBand_RangeEdges(t *testing.T) {
	cases := map[string]string{
		// Old false positives — now out of band.
		"14.999": "",
		"7.999":  "",
		"5.000":  "",
		"24.100": "",
		// Edges (inclusive low/high) and just-outside.
		"14.350": "20m",
		"14.351": "",
		"7.200":  "40m", // inside the 7.0–7.3 allocation
		"7.300":  "40m",
		"7.301":  "",
		"5.250":  "60m",
		"5.357":  "60m",
		"24.890": "12m",
		"24.889": "",
		// Unparseable → empty, never a panic.
		"":     "",
		"abc":  "",
		"-1.0": "",
	}
	for in, want := range cases {
		if got := FrequencyToBand(in); got != want {
			t.Errorf("FrequencyToBand(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestGetFrequencyRange(t *testing.T) {
	mini, maxi := GetFrequencyRange("14.074")
	if mini != 14.000 || maxi != 14.350 {
		t.Fatalf("GetFrequencyRange(14.074) = %g,%g; want 14.000,14.350", mini, maxi)
	}
	// In-prefix-out-of-band and unknown both yield the zero range.
	for _, in := range []string{"14.999", "999.999", "5.000", ""} {
		if mn, mx := GetFrequencyRange(in); mn != 0 || mx != 0 {
			t.Errorf("GetFrequencyRange(%q) = %g,%g; want 0,0", in, mn, mx)
		}
	}
}

func TestFormatFrequencyToMhz(t *testing.T) {
	cases := []struct {
		in  string
		out string
		err bool
	}{
		{"7.050.000", "7.050", false},
		{"14.074", "14.074", false},
		{"144.390", "144.390", false},
		{"", "", false},
		{"7050000", "", true},
	}
	for _, c := range cases {
		got, err := FormatFrequencyToMhz(c.in)
		if c.err {
			if err == nil {
				t.Fatalf("expected error for %q, got none", c.in)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", c.in, err)
		}
		if got != c.out {
			t.Fatalf("FormatFrequencyToMhz(%q) = %q; want %q", c.in, got, c.out)
		}
	}
}
