package codec

import "testing"

// Grid4ToG15 test vectors generated 2026-05-17 using the public-
// domain `grid4_to_g15` reference program from QEX ref [14] (built
// from `grid4_to_g15.f90`). Per QEX paper §9 the reference is in
// the public domain; the g15 values below are facts produced by
// running it against chosen inputs.
//
// Coverage spans every input class the encoder handles:
//
//   - Maidenhead grid origin (AA00) and max (RR99)
//   - Common 4-char grids across the world (FN20, IO91, KN07, etc.)
//   - All four reserved tokens (empty, RRR, RR73, 73)
//   - Signed reports covering negative and positive ends, and the
//     border cases that exercise the +35 bias arithmetic
var grid4Vectors = []struct {
	name  string
	input string
	want  uint16
}{
	// Maidenhead grids
	{"grid_origin_AA00", "AA00", 0},
	{"grid_max_RR99", "RR99", 32399},
	{"grid_FN20_US_NE", "FN20", 10320},
	{"grid_IO91_UK", "IO91", 15891},
	{"grid_KN07_HU", "KN07", 19307},
	{"grid_JN94_HU", "JN94", 17594},
	{"grid_IN94_FR", "IN94", 15794},
	{"grid_JO22_NL", "JO22", 17622},
	{"grid_EM48_US_MO", "EM48", 8448},

	// Reserved tokens
	{"reserved_empty", "", 32401},
	{"reserved_RRR", "RRR", 32402},
	{"reserved_RR73", "RR73", 32403},
	{"reserved_73", "73", 32404},

	// Signed reports — bias is +35, so "+02" → 32400+2+35 = 32437,
	// "-11" → 32400-11+35 = 32424, etc.
	{"report_plus_2", "+02", 32437},
	{"report_minus_11", "-11", 32424},
	{"report_plus_30", "+30", 32465},
	{"report_minus_30", "-30", 32405},
	{"report_plus_99", "+99", 32534},
}

func TestGrid4ToG15_SpecVectors(t *testing.T) {
	for _, tc := range grid4Vectors {
		t.Run(tc.name, func(t *testing.T) {
			got := Grid4ToG15(tc.input)
			if got != tc.want {
				t.Errorf("Grid4ToG15(%q): got %d (0x%04x), want %d (0x%04x)",
					tc.input, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestGrid4ToG15_OutputFitsIn15Bits(t *testing.T) {
	// Property test: every result must fit in 15 bits. Belt-and-
	// braces against a regression in the +35-bias arithmetic that
	// no specific vector might catch.
	for _, tc := range grid4Vectors {
		got := Grid4ToG15(tc.input)
		if got >= (1 << G15Bits) {
			t.Errorf("%s: Grid4ToG15(%q)=%d exceeds 15 bits", tc.name, tc.input, got)
		}
	}
}

func TestGrid4ToG15_RR73ShortCircuit(t *testing.T) {
	// "RR73" matches the 4-char-grid shape (R-R-7-3) but must
	// encode as its reserved token, NOT as a Maidenhead locator.
	// If the short-circuit broke, "RR73" would encode as the
	// (impossible) grid "RR73" = 17*18*100 + 17*100 + 73 = 32373,
	// not the reserved 32403.
	got := Grid4ToG15("RR73")
	if got != g15RR73 {
		t.Errorf("Grid4ToG15(\"RR73\"): got %d, want %d (reserved token)", got, g15RR73)
	}
	// Sanity: confirm 32373 is what would have been produced if the
	// short-circuit failed. This pins the bug we're guarding against.
	const ifShortCircuitFailed = uint16(17*18*100 + 17*100 + 73)
	if ifShortCircuitFailed != 32373 {
		t.Errorf("test arithmetic wrong: expected 32373, got %d", ifShortCircuitFailed)
	}
}

func TestGrid4ToG15_RejectsInvalidInputs(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"grid_too_short", "FN2"},
		{"grid_too_long", "FN201"},
		{"grid_lowercase_letter", "fn20"},
		{"grid_letter_out_of_range_S", "SN20"}, // letters cap at R
		{"grid_letter_out_of_range_T", "TN20"},
		{"grid_digit_position_letter", "FN2X"},
		{"grid_letter_position_digit", "1N20"},
		{"unsigned_number", "11"}, // missing + or -
		{"random_garbage", "WAT"},
		{"single_sign", "+"},
		{"sign_with_garbage", "+ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Grid4ToG15(%q) should panic; did not", tc.input)
				}
			}()
			Grid4ToG15(tc.input)
		})
	}
}

func TestGrid4ToG15_DocumentedCollisions(t *testing.T) {
	// grid.go documents that reports -34..-31 collide with the four
	// reserved tokens via the +35 bias arithmetic. Pin each pairing
	// explicitly so any future change to g15ReportBias is forced
	// through an explicit doc + test update. This also pins the
	// signed-zero collision (+0 and -0 both map to maxGrid4+35).
	cases := []struct {
		name     string
		report   string
		reserved uint16
	}{
		{"minus_34_collides_with_empty", "-34", g15Empty},
		{"minus_33_collides_with_RRR", "-33", g15RRR},
		{"minus_32_collides_with_RR73", "-32", g15RR73},
		{"minus_31_collides_with_73", "-31", g15_73},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Grid4ToG15(tc.report)
			if got != tc.reserved {
				t.Errorf("Grid4ToG15(%q) = %d; expected to collide with reserved %d", tc.report, got, tc.reserved)
			}
		})
	}

	// Signed-zero collision: +0 and -0 both encode to maxGrid4 + bias.
	if g1, g2 := Grid4ToG15("+0"), Grid4ToG15("-0"); g1 != g2 {
		t.Errorf("expected +0 and -0 to collide; got %d vs %d", g1, g2)
	}
}

func TestGrid4ToG15_RejectsNonCanonicalReports(t *testing.T) {
	// The strict report shape is sign + 1 or 2 digits. Looser forms
	// that Atoi alone would accept must be rejected so a buggy
	// caller can't slip in a non-canonical encoding.
	cases := []struct {
		name string
		in   string
	}{
		{"plus_002_leading_zero_padding", "+002"},
		{"plus_9999_overflow_magnitude", "+9999"},
		{"plus_only_no_digits", "+"},
		{"plus_sign_with_letter", "+A"},
		{"plus_sign_with_three_digits", "+123"},
		{"trailing_whitespace", "+02 "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("Grid4ToG15(%q) should panic on non-canonical report shape; did not", tc.in)
				}
			}()
			Grid4ToG15(tc.in)
		})
	}
}

func TestGrid4ToG15_AcceptsOneDigitReport(t *testing.T) {
	// "+2" is canonical (sign + 1 digit). Reference reports
	// gen_crc14 produces 32437 for "+2" (same as for "+02" because
	// Atoi reads both as the integer 2).
	if got, want := Grid4ToG15("+2"), uint16(32437); got != want {
		t.Errorf("Grid4ToG15(\"+2\") = %d, want %d", got, want)
	}
	if got, want := Grid4ToG15("-2"), uint16(32433); got != want {
		t.Errorf("Grid4ToG15(\"-2\") = %d, want %d", got, want)
	}
}

func TestIsGrid4(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"AA00", true},
		{"FN20", true},
		{"RR99", true},
		{"RR73", true}, // matches the shape; short-circuit handled by Grid4ToG15
		{"", false},
		{"FN2", false},
		{"FN201", false},
		{"fn20", false},
		{"SN20", false},
		{"FN2X", false},
		{"1N20", false},
	}
	for _, tc := range cases {
		if got := isGrid4(tc.in); got != tc.want {
			t.Errorf("isGrid4(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func BenchmarkGrid4ToG15(b *testing.B) {
	cases := []struct {
		name string
		in   string
	}{
		{"grid_FN20", "FN20"},
		{"reserved_RR73", "RR73"},
		{"report_minus_11", "-11"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = Grid4ToG15(tc.in)
			}
		})
	}
}
