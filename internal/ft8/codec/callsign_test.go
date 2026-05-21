package codec

import (
	"math/rand/v2"
	"testing"
)

// c28 zone tag. Per QEX paper Appendix A Table 7 every std-shape
// callsign lands in the std-call partition [stdCallOffset, 2^28);
// the hash range [nTokens, stdCallOffset) is reserved for non-std
// callsigns (compound calls, special-event calls) per Table 2.
const zoneStdCall = "std-call range (>= stdCallOffset)"

// CallsignC28 spec-vector regression suite. Vectors are computed
// analytically from CallsignC28's digit-position-3 alignment
// algorithm (see callsign.go); the algorithm itself is fixed by
// QEX paper Appendix A and Table 7. Values for 3-char, 4-char,
// and 5-char-2-prefix calls were REGENERATED for finding #1 —
// the earlier vectors were artefacts of the QEX ref [14]
// `std_call_to_c28.f90` reference program, which uses Fortran
// `adjustr` (right-justify only) and is incomplete: it doesn't
// handle the digit-alignment needed for short calls and 5-char-
// 2-prefix calls. The QEX paper Appendix A is the authoritative
// spec ("28 bits are enough to encode any standard call sign
// uniquely"), not the gap in the reference program.
//
// Coverage:
//
//   - 3-char (M1A, G3X) — minimum FT8 std-call length, 1-char prefix
//   - 4-char (K1JT) — 1-char prefix; Joe Taylor's call
//   - 5-char 1-prefix (G4ABC, K1ABC, W9XYZ, F5RXL) — common length
//   - 5-char 2-prefix (VK7MO, AB1CD) — needs digit alignment
//     distinct from adjustr; Phase 2C's spec-incorrect routing
//     sent these through HashedCallC28
//   - 6-char (7Q5MLV, 9V1ABC, PJ4ABC) — max length, 2-char prefix
//     including digit-led
//
// 7Q5MLV is a Malawian call (operator's country); 9V1ABC has a
// digit-led 2-char prefix; PJ4ABC produces the largest c28 in this
// set, near the top of the std-call range.
//
// All vectors land in zoneStdCall — the post-fix invariant is
// uniform: no std-shape call leaks into the hash range.
var callsignVectors = []struct {
	name string
	call string
	want uint32
	zone string
}{
	{"M1A_three_char", "M1A", 10608568, zoneStdCall},
	{"G3X_three_char", "G3X", 9483721, zoneStdCall},
	{"K1JT_four_char", "K1JT", 10222009, zoneStdCall},
	{"G4ABC_five_char", "G4ABC", 9486694, zoneStdCall},
	{"K1ABC_five_char", "K1ABC", 10214965, zoneStdCall},
	{"W9XYZ_five_char", "W9XYZ", 12751800, zoneStdCall},
	{"F5RXL_five_char", "F5RXL", 9322543, zoneStdCall},
	{"VK7MO_five_char_2prefix", "VK7MO", 237090319, zoneStdCall},
	{"AB1CD_five_char_2prefix", "AB1CD", 86389684, zoneStdCall},
	{"7Q5MLV_six_char_malawi", "7Q5MLV", 68170754, zoneStdCall},
	{"9V1ABC_six_char_digit_prefix", "9V1ABC", 83238895, zoneStdCall},
	{"PJ4ABC_six_char_letter_prefix", "PJ4ABC", 194310064, zoneStdCall},
}

func TestCallsignC28_SpecVectors(t *testing.T) {
	for _, tc := range callsignVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := CallsignC28(tc.call)
			if got != tc.want {
				t.Errorf("CallsignC28(%q): got %d (0x%08x), want %d (0x%08x)",
					tc.call, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestCallsignC28_OutputFitsIn28Bits(t *testing.T) {
	// Every result must fit in 28 bits for any valid std-call input.
	// Belt-and-braces against an arithmetic regression that no single
	// vector might catch.
	for _, tc := range callsignVectors {
		got := CallsignC28(tc.call)
		if got >= (1 << CallsignBits) {
			t.Errorf("%s: CallsignC28(%q)=%d (0x%08x) exceeds 28 bits", tc.name, tc.call, got, got)
		}
	}
}

func TestCallsignC28_OutputInStdCallRange(t *testing.T) {
	// Per QEX paper Appendix A Table 7, every std-shape callsign
	// packs into the std-call partition [stdCallOffset, 2^28).
	// The pre-finding-#1 invariant ("stays out of the special-token
	// space") was a weaker bound that admitted hash-range overlap
	// for short calls; the spec-correct invariant is tighter.
	for _, tc := range callsignVectors {
		got := CallsignC28(tc.call)
		if got < stdCallOffset {
			t.Errorf("%s: CallsignC28(%q)=%d below stdCallOffset %d — leaked out of std-call range, violates QEX Appendix A Table 7", tc.name, tc.call, got, stdCallOffset)
		}
	}
}

func TestCallsignC28_ZoneMatchesVector(t *testing.T) {
	// Per-vector zone assertion. Per finding #1 every std-shape
	// call lands in zoneStdCall; a vector that doesn't is either
	// mis-declared or a regression in the digit-alignment algorithm.
	for _, tc := range callsignVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := CallsignC28(tc.call)
			var actualZone string
			switch {
			case got >= stdCallOffset:
				actualZone = zoneStdCall
			case got >= nTokens:
				actualZone = "ILLEGAL (hash-range overlap; QEX Appendix A says std calls pack into [stdCallOffset, 2^28))"
			default:
				actualZone = "ILLEGAL (below nTokens — leaked into special-token range)"
			}
			if actualZone != tc.zone {
				t.Errorf("CallsignC28(%q)=%d landed in %q; vector declares %q",
					tc.call, got, actualZone, tc.zone)
			}
		})
	}
}

func TestCallsignC28_RejectsTooShortInput(t *testing.T) {
	// FT8 std calls are 3-6 chars (prefix + digit + suffix).
	// Lengths 0..2 are rejected because the c28 they'd produce
	// doesn't correspond to any real callsign.
	cases := []struct {
		name string
		in   string
	}{
		{"empty", ""},
		{"one_char", "K"},
		{"two_char_letter_letter", "MO"},
		{"two_char_letter_digit", "K1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CallsignC28(%q) should panic; did not", tc.in)
				}
			}()
			CallsignC28(tc.in)
		})
	}
}

func TestCallsignC28_RejectsOversizedInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CallsignC28(7-char) should panic; did not")
		}
	}()
	CallsignC28("K1ABCDE")
}

func TestCallsignC28_RejectsLowercase(t *testing.T) {
	// Case-sensitivity is documented contract — caller is responsible
	// for upper-casing before invocation.
	defer func() {
		if r := recover(); r == nil {
			t.Error("CallsignC28(lowercase) should panic; did not")
		}
	}()
	CallsignC28("g4abc")
}

func TestCallsignC28_RejectsNonAlphabetChars(t *testing.T) {
	cases := []struct {
		name string
		call string
	}{
		{"punctuation_slash", "K1/AB"}, // compound calls go through c58, not c28
		{"hyphen", "K-1AB"},
		{"non_ascii", "K1ÉBC"},
		{"control_char", "K1\x01AB"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CallsignC28(%q) should panic; did not", tc.call)
				}
			}()
			CallsignC28(tc.call)
		})
	}
}

func TestCallsignC28_RejectsEmbeddedSpace(t *testing.T) {
	// Std-shape callsigns per QEX §A don't contain embedded spaces.
	// Under digit-position-3 alignment (finding #1), CallsignC28's
	// alphabet check rejects space along with other non-[0-9A-Z]
	// chars; callers must strip whitespace upstream. The old "pos-1
	// alphabet includes space" laxness was a side-effect of the
	// pre-fix negative-index handling and produced indistinguishable
	// c28 values for spaced and unspaced variants — a bug in the
	// caller's input now surfaces as a panic instead of silent
	// corruption.
	defer func() {
		if r := recover(); r == nil {
			t.Error("CallsignC28(\"K1 BC\") should panic on embedded space; did not")
		}
	}()
	CallsignC28("K1 BC")
}

// TestCallsignC28_PropertyAllStdShapesInStdRange is the property
// version of TestCallsignC28_OutputInStdCallRange: it generates
// random valid std-shape callsigns per QEX paper §A (prefix 1-2
// chars with ≥1 letter + 1 digit + suffix 1-3 letters) and pins
// that EVERY such call lands in the std-call range [stdCallOffset,
// 2^28). A regression that re-introduces hash-range overlap for
// any std-shape input fails here.
//
// Deterministic seed so failures are reproducible across runs.
func TestCallsignC28_PropertyAllStdShapesInStdRange(t *testing.T) {
	r := rand.New(rand.NewPCG(0xDEAD7E57, 0xBABE7A55))
	const trials = 5000
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const digits = "0123456789"
	const alnum = letters + digits

	randByte := func(s string) byte { return s[r.IntN(len(s))] }

	for trial := range trials {
		// Prefix: 1 or 2 chars, at least one letter.
		var prefix []byte
		if r.IntN(2) == 0 {
			// 1-char prefix: must be letter.
			prefix = []byte{randByte(letters)}
		} else {
			// 2-char prefix: alnum, retry if both are digits.
			for {
				c1, c2 := randByte(alnum), randByte(alnum)
				if (c1 >= 'A' && c1 <= 'Z') || (c2 >= 'A' && c2 <= 'Z') {
					prefix = []byte{c1, c2}
					break
				}
			}
		}
		digit := randByte(digits)
		// Suffix: 1, 2, or 3 letters.
		suffixLen := 1 + r.IntN(3)
		suffix := make([]byte, suffixLen)
		for j := range suffix {
			suffix[j] = randByte(letters)
		}
		call := string(prefix) + string(digit) + string(suffix)

		got := CallsignC28(call)
		if got < stdCallOffset {
			t.Errorf("trial %d: CallsignC28(%q) = %d below stdCallOffset %d — std-shape call leaked into hash partition, violates QEX Appendix A Table 7",
				trial, call, got, stdCallOffset)
		}
	}
}

func BenchmarkCallsignC28(b *testing.B) {
	// Multiple shapes: padding loop length varies by call length,
	// so a regression that short-circuits padding for one length
	// (e.g. an optimisation that skips the loop when len(call)==6)
	// would be visible in the per-shape numbers.
	cases := []struct {
		name string
		call string
	}{
		{"3char_M1A", "M1A"},
		{"4char_K1JT", "K1JT"},
		{"5char_K1ABC", "K1ABC"},
		{"6char_7Q5MLV", "7Q5MLV"},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				_ = CallsignC28(tc.call)
			}
		})
	}
}
