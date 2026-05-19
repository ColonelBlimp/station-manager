package codec

import (
	"testing"
)

// ---- TokenToC28 / C28ToToken: Table 7 boundaries ---------------------------

// TestTokenToC28_Singletons pins DE / QRZ / CQ to their fixed c28
// values per QEX paper Table 7.
func TestTokenToC28_Singletons(t *testing.T) {
	cases := []struct {
		s    string
		want uint32
	}{
		{"DE", 0},
		{"QRZ", 1},
		{"CQ", 2},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			got, ok := TokenToC28(tc.s)
			if !ok {
				t.Fatalf("TokenToC28(%q) = (_, false), want (%d, true)", tc.s, tc.want)
			}
			if got != tc.want {
				t.Errorf("TokenToC28(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

// TestTokenToC28_CQDigitsBoundaries pins the CQ NNN range to its
// Table 7 boundaries (3 = "CQ 000", 1002 = "CQ 999").
func TestTokenToC28_CQDigitsBoundaries(t *testing.T) {
	cases := []struct {
		s    string
		want uint32
	}{
		{"CQ 000", 3},
		{"CQ 001", 4},
		{"CQ 042", 45},
		{"CQ 100", 103},
		{"CQ 999", 1002},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			got, ok := TokenToC28(tc.s)
			if !ok {
				t.Fatalf("TokenToC28(%q) = (_, false), want (%d, true)", tc.s, tc.want)
			}
			if got != tc.want {
				t.Errorf("TokenToC28(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

// TestTokenToC28_CQLettersBoundaries pins each row of the CQ-letters
// partition to its Table 7 boundary values. Derived analytically
// from c28 = 1003 + base27(left-space-padded 4-char suffix).
func TestTokenToC28_CQLettersBoundaries(t *testing.T) {
	cases := []struct {
		s    string
		want uint32
	}{
		// Width 1: CQ A=1004 .. CQ Z=1029.
		{"CQ A", 1004},
		{"CQ M", 1016},
		{"CQ Z", 1029},
		// Width 2: CQ AA=1031 .. CQ ZZ=1731.
		{"CQ AA", 1031},
		{"CQ AZ", 1056},
		{"CQ BA", 1058},
		{"CQ ZZ", 1731},
		// Width 3: CQ AAA=1760 .. CQ ZZZ=20685.
		{"CQ AAA", 1760},
		{"CQ DXX", 1003 + 4*27*27 + 24*27 + 24}, // "DXX" = (4,24,24) in 4-char " DXX"
		{"CQ ZZZ", 20685},
		// Width 4: CQ AAAA=21443 .. CQ ZZZZ=532443.
		{"CQ AAAA", 21443},
		{"CQ POTA", 1003 + 16*27*27*27 + 15*27*27 + 20*27 + 1}, // P=16, O=15, T=20, A=1
		{"CQ ZZZZ", 532443},
	}
	for _, tc := range cases {
		t.Run(tc.s, func(t *testing.T) {
			got, ok := TokenToC28(tc.s)
			if !ok {
				t.Fatalf("TokenToC28(%q) = (_, false), want (%d, true)", tc.s, tc.want)
			}
			if got != tc.want {
				t.Errorf("TokenToC28(%q) = %d, want %d", tc.s, got, tc.want)
			}
		})
	}
}

// TestTokenToC28_Rejects covers the negative cases — inputs that
// don't match any token shape and must return (_, false) so the
// caller can route through the callsign path instead.
func TestTokenToC28_Rejects(t *testing.T) {
	rejects := []string{
		"",           // empty
		" ",          // whitespace only
		"K1JT",       // std callsign (has a digit, not a token)
		"cq",         // lowercase — caller's job to upper-case
		"CQ",         // bare CQ is at the singleton branch, NOT here — but covered to check this path returns OK
		"CQ ",        // CQ with empty suffix
		"CQ 1",       // 1 digit (need 3)
		"CQ 12",      // 2 digits
		"CQ 1234",    // 4 digits
		"CQ 12X",     // mixed digits + letter
		"CQ AAAAA",   // 5 letters (max 4)
		"CQ A1",      // letter + digit
		"CQ ABC1",    // letters + digit
		"FOO",        // non-token bareword
		"CQ DX K1JT", // multi-token string (caller pre-tokenises)
		"  CQ AB  ",  // leading/trailing whitespace (caller normalises)
	}
	for _, s := range rejects {
		t.Run(s, func(t *testing.T) {
			if s == "CQ" {
				// CQ should succeed via the singleton branch.
				if got, ok := TokenToC28(s); !ok || got != 2 {
					t.Errorf("TokenToC28(%q) = (%d, %v), want (2, true)", s, got, ok)
				}
				return
			}
			if _, ok := TokenToC28(s); ok {
				t.Errorf("TokenToC28(%q) = (_, true), want (_, false)", s)
			}
		})
	}
}

// ---- C28ToToken inverse ----------------------------------------------------

// TestC28ToToken_Singletons pins the c28 → string inverse for the
// three bare singletons.
func TestC28ToToken_Singletons(t *testing.T) {
	cases := []struct {
		c28  uint32
		want string
	}{
		{0, "DE"},
		{1, "QRZ"},
		{2, "CQ"},
	}
	for _, tc := range cases {
		t.Run(tc.want, func(t *testing.T) {
			got, ok := C28ToToken(tc.c28)
			if !ok {
				t.Fatalf("C28ToToken(%d) = (_, false), want (%q, true)", tc.c28, tc.want)
			}
			if got != tc.want {
				t.Errorf("C28ToToken(%d) = %q, want %q", tc.c28, got, tc.want)
			}
		})
	}
}

// TestC28ToToken_RoundTrip pins TokenToC28 → C28ToToken as identity
// across all four CQ-letters widths plus the digits range and the
// singletons.
func TestC28ToToken_RoundTrip(t *testing.T) {
	cases := []string{
		"DE", "QRZ", "CQ",
		"CQ 000", "CQ 042", "CQ 100", "CQ 999",
		"CQ A", "CQ M", "CQ Z",
		"CQ AA", "CQ AZ", "CQ ZZ",
		"CQ AAA", "CQ DXX", "CQ ZZZ",
		"CQ AAAA", "CQ POTA", "CQ DXDX", "CQ ZZZZ",
	}
	for _, s := range cases {
		t.Run(s, func(t *testing.T) {
			c28, ok := TokenToC28(s)
			if !ok {
				t.Fatalf("TokenToC28(%q) = (_, false)", s)
			}
			got, ok := C28ToToken(c28)
			if !ok {
				t.Fatalf("C28ToToken(%d) = (_, false), want (%q, true)", c28, s)
			}
			if got != s {
				t.Errorf("Round-trip %q → %d → %q", s, c28, got)
			}
		})
	}
}

// TestC28ToToken_InterRowGaps pins the documented gap c28 values
// between rows of Table 7 — must return (_, false).
func TestC28ToToken_InterRowGaps(t *testing.T) {
	// Per the encoding c28 = 1003 + base27(4-char suffix):
	//   1003 = "    "  (all space → would be bare CQ, but bare CQ
	//                    is at c28=2, so this slot is unused)
	//   1030 = "   A " (1*27 = "  A ", trailing space — invalid
	//                    encoding, NOT a 1-letter suffix)
	//   1732 = " A  "  (1*27^2 = " A  ", trailing 2 spaces)
	//   1759 = " AA "  (1*27^2 + 27 = " AA ", trailing space)
	//   20686 = "A   " (27^3, leading letter + 3 trailing spaces)
	//   21442 = "A ZZ" (27^3 + 26*27 + 26, embedded space)
	//   532444 = first c28 past the CQ-letters row (large reserved gap)
	gaps := []uint32{
		1003,
		1030,
		1732, 1759,
		20686, 21442,
		532444, 1000000, nTokens - 1,
	}
	for _, c28 := range gaps {
		if got, ok := C28ToToken(c28); ok {
			t.Errorf("C28ToToken(%d) = (%q, true), want (_, false) — c28 is a token-partition gap", c28, got)
		}
	}
}

// TestC28ToToken_IntraRowGaps spot-checks intra-row gaps inside the
// CQ-letters row. These are c28 values whose 4-char base-27 decode
// produces an embedded or trailing space — not a valid CQ-letter
// suffix.
func TestC28ToToken_IntraRowGaps(t *testing.T) {
	// "  B " = 2*27 + 0 = 54 → c28 = 1057. d=(0,0,2,0), trailing zero
	// after non-zero → invalid.
	// "  C " = 3*27 = 81 → c28 = 1084. Same shape.
	// " A B" = 1*729 + 0 + 2 = 731 → c28 = 1734. d=(0,1,0,2), embedded
	// zero → invalid.
	intraGaps := []uint32{
		1057,
		1084,
		1734,
	}
	for _, c28 := range intraGaps {
		if got, ok := C28ToToken(c28); ok {
			t.Errorf("C28ToToken(%d) = (%q, true), want (_, false) — c28 is a CQ-letters intra-row gap", c28, got)
		}
	}
}

// TestC28ToToken_OutOfTokenRange verifies c28 ≥ nTokens returns
// (_, false) — caller should route those through C28ToCallsign.
func TestC28ToToken_OutOfTokenRange(t *testing.T) {
	beyond := []uint32{
		nTokens,
		nTokens + 1,
		stdCallOffset,
		(1 << CallsignBits) - 1,
	}
	for _, c28 := range beyond {
		if got, ok := C28ToToken(c28); ok {
			t.Errorf("C28ToToken(%d) = (%q, true), want (_, false) — c28 is outside the token partition", c28, got)
		}
	}
}

// ---- Capacity assertions ---------------------------------------------------

// TestTokenSubrangeCapacities pins the analytical capacity of each
// Table 7 partition against the row's c28 span. Catches an off-by-
// one in the boundary constants without touching the encoder logic.
func TestTokenSubrangeCapacities(t *testing.T) {
	cases := []struct {
		name        string
		lo, hi      uint32
		wantSpan    uint32 // hi - lo + 1
		description string
	}{
		{"DE_singleton", 0, 0, 1, "DE singleton"},
		{"QRZ_singleton", 1, 1, 1, "QRZ singleton"},
		{"CQ_singleton", 2, 2, 1, "CQ singleton"},
		{"CQ_NNN", cqDigitsLo, cqDigitsHi, 1000, "CQ 000..999 = 1000 entries"},
		{"CQ_A_to_Z", 1004, 1029, 26, "Width-1 row: 26 entries (= 26^1)"},
		{"CQ_AA_to_ZZ", 1031, 1731, 701, "Width-2 row: 701 entries (= 26^2 + 25 intra-row gaps)"},
		{"CQ_AAA_to_ZZZ", 1760, 20685, 18926, "Width-3 row: 18926 entries (= 26^3 + 1350 intra-row gaps)"},
		{"CQ_AAAA_to_ZZZZ", 21443, 532443, 511001, "Width-4 row: 511001 entries (= 26^4 + 54025 intra-row gaps)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.hi - tc.lo + 1
			if got != tc.wantSpan {
				t.Errorf("%s: span = %d, want %d (%s)", tc.name, got, tc.wantSpan, tc.description)
			}
		})
	}
}

// TestCQLettersValidCounts pins the number of valid (gap-free) CQ-
// letters tokens per width against 26^K. The intra-row gap count is
// rowSpan - 26^K; this test verifies the analytical prediction holds
// when we iterate the whole row and count successful decodes.
//
// Width 4 has 511001 codepoints; iterating the whole row is fast
// enough at codec layer (each call is O(1)) but kept as one loop to
// avoid 500k separate sub-tests in the runner.
func TestCQLettersValidCounts(t *testing.T) {
	cases := []struct {
		name   string
		lo, hi uint32
		want   int
	}{
		{"width_1", 1004, 1029, 26},
		{"width_2", 1031, 1731, 676},       // 26^2
		{"width_3", 1760, 20685, 17576},    // 26^3
		{"width_4", 21443, 532443, 456976}, // 26^4
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid := 0
			for c28 := tc.lo; c28 <= tc.hi; c28++ {
				if _, ok := C28ToToken(c28); ok {
					valid++
				}
			}
			if valid != tc.want {
				t.Errorf("%s: %d valid CQ-letter codepoints, want %d (= 26^K)", tc.name, valid, tc.want)
			}
		})
	}
}
