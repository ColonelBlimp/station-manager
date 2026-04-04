package adif

import "testing"

func Test_hzToMHz(t *testing.T) {
	cases := map[string]string{
		"14310000":  "14.310",
		"7050000":   "7.050",
		"14439000":  "14.439",
		"144.390":   "144.390",   // Already MHz — must not be corrupted
		"14.310":    "14.310",    // Already MHz — must not be corrupted
		"123456":    "123456",    // Too short
		"123456789": "123456789", // Too long
		"abcdefgh":  "abcdefgh",  // Not a number
	}
	for in, want := range cases {
		got := hzToMHz(in)
		if got != want {
			t.Fatalf("hzToMHz(%q) = %q; want %q", in, got, want)
		}
	}
}
