package truth

import "testing"

// TestNormalizeText pins the manifest-vs-decoder formatting variances
// the matcher absorbs. Each case is a real observed mismatch from the
// 2026-05-28 Session 102 asymmetric-channelizer corpus A/B; the test
// is the guard that future changes don't drop coverage.
func TestNormalizeText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// 20m_slot2 truth: jt9-oracle confidence annotation "a1" leaked
		// into the manifest text field, blocking text-equality match
		// against the sandbox's clean decode.
		{
			"strips trailing jt9 a1 annotation",
			"CQ IZ1HJU JN44                        a1",
			"CQ IZ1HJU JN44",
		},
		{
			"strips trailing jt9 b2 annotation",
			"CQ K1JT FN20   b2",
			"CQ K1JT FN20",
		},
		{
			"strips trailing jt9 annotation with no extra trailing space",
			"PA9R SV9TLU -13 a1",
			"PA9R SV9TLU -13",
		},
		// live_slot2 truth: "DN9BT UR5WAI R-09" (no space), sandbox decode
		// "DN9BT UR5WAI R -09" (with space).
		{
			"fuses R-prefixed negative report",
			"DN9BT UR5WAI R -09",
			"DN9BT UR5WAI R-09",
		},
		{
			"fuses R-prefixed positive report",
			"PD0LBY OH7NW R +08",
			"PD0LBY OH7NW R+08",
		},
		{
			"already-fused R-prefixed report passes through",
			"DN9BT UR5WAI R-09",
			"DN9BT UR5WAI R-09",
		},
		// Whitespace housekeeping.
		{
			"collapses internal whitespace",
			"CQ  K1JT   FN20",
			"CQ K1JT FN20",
		},
		{
			"trims leading + trailing whitespace",
			"   CQ K1JT FN20   ",
			"CQ K1JT FN20",
		},
		// Pass-through: well-formed text must not be altered.
		{
			"clean grid message passes through",
			"CQ K1JT FN20",
			"CQ K1JT FN20",
		},
		{
			"clean ack passes through",
			"F1RCQ YO3OBB RR73",
			"F1RCQ YO3OBB RR73",
		},
		{
			"signed report (no R prefix) passes through",
			"PA9R SV9TLU -13",
			"PA9R SV9TLU -13",
		},
		{
			"portable suffix passes through",
			"RA6AR EX8ABR/P +01",
			"RA6AR EX8ABR/P +01",
		},
		// Defensive: empty/whitespace-only input doesn't panic.
		{
			"empty string",
			"",
			"",
		},
		{
			"whitespace only",
			"   ",
			"",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := NormalizeText(tc.in)
			if got != tc.want {
				t.Errorf("NormalizeText(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
