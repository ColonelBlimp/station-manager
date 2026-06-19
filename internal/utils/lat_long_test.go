package utils

import "testing"

func TestConvertToXDDDMMM_Lat_Positive(t *testing.T) {
	in := "12.3456"
	got, err := ConvertToXDDDMMM(in, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "N012 20.736"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, true) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Lat_Negative(t *testing.T) {
	in := "-7.5"
	got, err := ConvertToXDDDMMM(in, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "S007 30.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, true) = %q; want %q", in, got, want)
	}
}

// Out-of-range and non-finite coordinates are rejected rather than formatted
// into a syntactically-valid-but-impossible string (review 2026-06-19 L1).
func TestConvertToXDDDMMM_RejectsOutOfRange(t *testing.T) {
	cases := []struct {
		in    string
		isLat bool
	}{
		{"91", true}, // latitude > 90
		{"-90.001", true},
		{"181", false}, // longitude > 180
		{"-200", false},
		{"NaN", true},
		{"Inf", false},
		{"-Inf", true},
	}
	for _, c := range cases {
		if got, err := ConvertToXDDDMMM(c.in, c.isLat); err == nil {
			t.Errorf("ConvertToXDDDMMM(%q, %v) = %q, nil; want error", c.in, c.isLat, got)
		}
	}

	// The exact bounds are still accepted.
	for _, c := range []struct {
		in    string
		isLat bool
	}{{"90", true}, {"-90", true}, {"180", false}, {"-180", false}} {
		if _, err := ConvertToXDDDMMM(c.in, c.isLat); err != nil {
			t.Errorf("ConvertToXDDDMMM(%q, %v) rejected a boundary value: %v", c.in, c.isLat, err)
		}
	}
}

func TestConvertToXDDDMMM_Lon_Positive(t *testing.T) {
	in := "12.3456"
	got, err := ConvertToXDDDMMM(in, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "E012 20.736"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, false) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Lon_Negative(t *testing.T) {
	in := "-7.5"
	got, err := ConvertToXDDDMMM(in, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "W007 30.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, false) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Zero(t *testing.T) {
	in := "0"
	got, err := ConvertToXDDDMMM(in, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// minutes are zero-padded to width 6 with 3 decimals
	want := "N000 00.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, true) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Invalid(t *testing.T) {
	in := "abc"
	got, err := ConvertToXDDDMMM(in, true)
	if err == nil {
		t.Fatalf("expected error for input %q, got none and result %q", in, got)
	}
	if got != emptyString {
		t.Fatalf("expected empty string on error, got %q", got)
	}
}

func TestConvertToXDDDMMM_RoundingCarry(t *testing.T) {
	// This value produces minutes that round to 60.000; expect degree to carry by 1 and minutes to 00.000
	in := "10.9999917" // 0.9999917*60 = 59.999502 -> rounds to 60.000
	got, err := ConvertToXDDDMMM(in, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect carry to N011 00.000 after normalization
	want := "N011 00.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q, true) = %q; want %q", in, got, want)
	}
}
