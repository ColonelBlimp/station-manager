package lookup

import (
	"testing"
	"time"
)

// parseOffsetDuration is a private orchestrator helper; this file is
// in the same package so it can hit it directly. The unit cases cover
// the two upstream-emitted formats (Go-duration "2h 0m" and RFC 3339
// zone "+02:00") plus malformed input that must NOT default to UTC —
// an unparseable offset is a data-quality signal, not a "use zero"
// trigger.
func TestParseOffsetDuration(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want time.Duration
		ok   bool
	}{
		// Go-duration shape (hamnut's standard format).
		{name: "2h 0m positive", in: "2h 0m", want: 2 * time.Hour, ok: true},
		{name: "5h 30m positive", in: "5h 30m", want: 5*time.Hour + 30*time.Minute, ok: true},
		{name: "negative 5h 30m", in: "-5h 30m", want: -(5*time.Hour + 30*time.Minute), ok: true},
		{name: "0h 0m UTC", in: "0h 0m", want: 0, ok: true},
		{name: "no spaces 2h0m", in: "2h0m", want: 2 * time.Hour, ok: true},
		{name: "hours only 2h", in: "2h", want: 2 * time.Hour, ok: true},

		// RFC 3339 zone shape (the deriveTimeOffset fallback emits this).
		{name: "+02:00 RFC zone", in: "+02:00", want: 2 * time.Hour, ok: true},
		{name: "-08:00 RFC zone", in: "-08:00", want: -8 * time.Hour, ok: true},
		{name: "+05:30 half-hour RFC zone", in: "+05:30", want: 5*time.Hour + 30*time.Minute, ok: true},
		{name: "+00:00 UTC RFC zone", in: "+00:00", want: 0, ok: true},

		// Boundary / unparseable cases.
		{name: "empty", in: "", want: 0, ok: false},
		{name: "whitespace only", in: "   ", want: 0, ok: false},
		{name: "garbage", in: "tomorrow", want: 0, ok: false},
		{name: "missing colon in zone shape", in: "+0200", want: 0, ok: false},
		{name: "wrong-length zone shape", in: "+2:00", want: 0, ok: false},
		{name: "bad sign", in: "*02:00", want: 0, ok: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseOffsetDuration(tc.in)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v (input %q)", ok, tc.ok, tc.in)
			}
			if got != tc.want {
				t.Errorf("duration = %v, want %v (input %q)", got, tc.want, tc.in)
			}
		})
	}
}
