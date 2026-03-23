package utils

import "testing"

func TestSanitizeDateToYYYYMMDD(t *testing.T) {
	cases := map[string]string{
		"2025-01-02":   "20250102",
		"2025/01/02":   "20250102",
		"20250102":     "20250102",
		" 2025-01-02 ": "20250102",
		"2025-13-01":   "", // invalid month
		"2025/02/30":   "", // invalid day
		"":             "",
	}
	for in, want := range cases {
		if got := SanitizeDateToYYYYMMDD(in); got != want {
			t.Fatalf("SanitizeDateToYYYYMMDD(%q) = %q; want %q", in, got, want)
		}
	}
}
