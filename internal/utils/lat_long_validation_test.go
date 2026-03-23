package utils

import "testing"

func TestIsXDDDMMM_Valid(t *testing.T) {
	cases := []string{
		"N012 20.736",
		"S090 00.000",
		"E180 00.000",
		"W000 59.999",
	}
	for _, c := range cases {
		if !IsXDDDMMM(c) {
			t.Errorf("expected %q to be valid XDDDMMM", c)
		}
	}
}

func TestIsXDDDMMM_Invalid(t *testing.T) {
	cases := []string{
		"X012 20.736", // invalid direction
		"N12 20.736",  // degrees not 3 digits
		"N01220.736",  // missing space
		"N012 2.736",  // minutes not two digits before decimal
		"N181 00.000", // degrees > 180
		"N180 00.001", // 180 deg with non-zero minutes
		"N000 60.000", // minutes >= 60
		"N000 59.99",  // not 3 decimals
	}
	for _, c := range cases {
		if IsXDDDMMM(c) {
			t.Errorf("expected %q to be invalid XDDDMMM", c)
		}
	}
}
