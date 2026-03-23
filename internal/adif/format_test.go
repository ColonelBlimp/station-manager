package adif

import "testing"

func Test_normalizeFreqMHz(t *testing.T) {
	cases := map[string]string{
		"7.050.000": "7.050",
		"14.074":    "14.074",
		"144.390":   "144.390",
		"144.":      "144",
		"":          "",
	}
	for in, want := range cases {
		got := normalizeFreqMHz(in)
		if got != want {
			t.Fatalf("normalizeFreqMHz(%q) = %q; want %q", in, got, want)
		}
	}
}

func Test_khzToMHz(t *testing.T) {
	cases := map[string]string{
		"14310000":  "14.310",
		"7050000":   "7.050",
		"14439000":  "14.439",
		"123456":    "123456",    // Too short
		"123456789": "123456789", // Too long
		"abcdefgh":  "abcdefgh",  // Not a number
	}
	for in, want := range cases {
		got := khzToMHz(in)
		if got != want {
			t.Fatalf("khzToMHz(%q) = %q; want %q", in, got, want)
		}
	}
}
