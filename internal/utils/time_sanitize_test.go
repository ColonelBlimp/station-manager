package utils

import "testing"

func TestSanitizeTimeToADIF(t *testing.T) {
	cases := map[string]string{
		"09:30":     "0930",
		"9:3":       "0903",
		"23:59:58":  "235958",
		"7:5:9":     "070509",
		"2359":      "2359",
		"123045":    "123045",
		"12-34":     "1234",
		"12.34.56":  "123456",
		"  01:02  ": "0102",
	}
	for in, want := range cases {
		if got := SanitizeTimeToADIF(in); got != want {
			t.Fatalf("SanitizeTimeToADIF(%q) = %q; want %q", in, got, want)
		}
	}

	invalid := []string{
		"",
		"24:00",
		"2400",
		"23:60",
		"126099",
		"ab:cd",
		"7",
		"123",
		"246000",
	}
	for _, in := range invalid {
		if got := SanitizeTimeToADIF(in); got != "" {
			t.Fatalf("SanitizeTimeToADIF(%q) = %q; want empty", in, got)
		}
	}
}

// TimeToHHMM narrows an HHMMSS time to HHMM for storage; everything else is
// returned unchanged (review 2026-06-19 M2).
func TestTimeToHHMM(t *testing.T) {
	cases := map[string]string{
		"235958": "2359", // seconds dropped
		"084500": "0845",
		"0845":   "0845", // already HHMM — unchanged
		"":       "",     // empty — unchanged (caller validates)
	}
	for in, want := range cases {
		if got := TimeToHHMM(in); got != want {
			t.Errorf("TimeToHHMM(%q) = %q; want %q", in, got, want)
		}
	}
}
