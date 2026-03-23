package utils

import "testing"

func TestConvertToXDDDMMM_Positive(t *testing.T) {
	in := "12.3456"
	got, err := ConvertToXDDDMMM(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "N012 20.736"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Negative(t *testing.T) {
	in := "-7.5"
	got, err := ConvertToXDDDMMM(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "S007 30.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Zero(t *testing.T) {
	in := "0"
	got, err := ConvertToXDDDMMM(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// minutes are zero-padded to width 6 with 3 decimals
	want := "N000 00.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q) = %q; want %q", in, got, want)
	}
}

func TestConvertToXDDDMMM_Invalid(t *testing.T) {
	in := "abc"
	got, err := ConvertToXDDDMMM(in)
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
	got, err := ConvertToXDDDMMM(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Expect carry to N011 00.000 after normalization
	want := "N011 00.000"
	if got != want {
		t.Fatalf("ConvertToXDDDMMM(%q) = %q; want %q", in, got, want)
	}
}
