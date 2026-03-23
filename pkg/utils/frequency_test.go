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
		"999.999": "",
	}
	for in, want := range cases {
		if got := FrequencyToBand(in); got != want {
			t.Fatalf("FrequencyToBand(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestGetFrequencyRange(t *testing.T) {
	mini, maxi := GetFrequencyRange("14.074")
	if mini == 0 && maxi == 0 {
		t.Fatal("expected non-zero range for 20m band prefix")
	}
	min2, max2 := GetFrequencyRange("999.999")
	if min2 != 0 || max2 != 0 {
		t.Fatal("expected zero range for unknown prefix")
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
