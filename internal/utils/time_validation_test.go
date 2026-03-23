package utils

import "testing"

func TestIsValidTimeADIF(t *testing.T) {
	valid := []string{
		"0000",
		"2359",
		"1230",
		"010203",
		"123045",
	}
	for _, v := range valid {
		if !IsValidTimeADIF(v) {
			t.Fatalf("expected valid ADIF time: %q", v)
		}
	}

	invalid := []string{
		"",
		"24:00",
		"2400",
		"2360",
		"126099",
		"ab:cd",
		"12:34",
		"7",
		"123",
		"246000",
	}
	for _, v := range invalid {
		if IsValidTimeADIF(v) {
			t.Fatalf("expected invalid ADIF time: %q", v)
		}
	}
}
