package utils

import "testing"

func TestFormatDate(t *testing.T) {
	cases := map[string]string{
		"20250102": "2025-01-02",
		"":         "YYYY-MM-DD",
		"2025":     "YYYY-MM-DD",
	}
	for in, want := range cases {
		if got := FormatDate(in); got != want {
			t.Fatalf("FormatDate(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	cases := map[string]string{
		"0930": "09:30",
		"":     "HH:MM",
		"12":   "HH:MM",
	}
	for in, want := range cases {
		if got := FormatTime(in); got != want {
			t.Fatalf("FormatTime(%q) = %q; want %q", in, got, want)
		}
	}
}
