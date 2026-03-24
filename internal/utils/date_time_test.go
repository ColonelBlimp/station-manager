package utils

import (
	"testing"
	"time"
)

func TestDateNowAsYYYYMMDD(t *testing.T) {
	got := DateNowAsYYYYMMDD()
	if !IsValidDateYYYYMMDD(got) {
		t.Fatalf("DateNowAsYYYYMMDD() = %q; not a valid YYYYMMDD date", got)
	}
}

func TestGenerateDateYYYYMMDD(t *testing.T) {
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC), "20250102"},
		{time.Date(2000, 2, 29, 12, 0, 0, 0, time.UTC), "20000229"},
		{time.Date(1999, 12, 31, 23, 59, 59, 0, time.UTC), "19991231"},
	}
	for _, c := range cases {
		got := GenerateDateYYYYMMDD(c.t)
		if got != c.want {
			t.Fatalf("GenerateDateYYYYMMDD(%v) = %q; want %q", c.t, got, c.want)
		}
	}
}

func TestFormatDate(t *testing.T) {
	cases := map[string]string{
		"20250102": "2025-01-02",
		"":         "",
		"2025":     "",
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
		"":     "",
		"12":   "",
	}
	for in, want := range cases {
		if got := FormatTime(in); got != want {
			t.Fatalf("FormatTime(%q) = %q; want %q", in, got, want)
		}
	}
}
