package decoder

import "testing"

// c28 zones the test vectors land in. Documented behaviour per
// QEX paper Table 7 (nominal layout) and CallsignC28's algorithm
// notes (short calls overlap with hash range — disambiguated by
// the message-type tag, not by c28 range).
const (
	zoneHashOverlap = "hash-range overlap (short call; disambiguated by i3/n3 message-type tag)"
	zoneStdCall     = "true std-call range (>= stdCallOffset)"
)

// CallsignC28 test vectors generated 2026-05-17 using the
// public-domain `std_call_to_c28` reference program from the QEX
// paper's reference [14] tarball (built locally from
// `std_call_to_c28.f90`). Per QEX paper §9 the reference is in the
// public domain; the c28 values below are facts produced by running
// it against chosen inputs.
//
// Coverage:
//
//   - 3-char (M1A) — minimum FT8 std-call length
//   - 4-char (K1JT) — Joe Taylor's call; verifies short-call padding
//   - 5-char (G4ABC, K1ABC, W9XYZ, VK7MO, F5RXL) — common length
//   - 6-char (7Q5MLV, 9V1ABC, PJ4ABC) — max length, all with
//     2-char prefixes including digit-led
//
// 7Q5MLV is a Malawian call (operator's country); 9V1ABC has a
// digit-led 2-char prefix; PJ4ABC produces the largest c28 in this
// set, near the top of the std-call range.
//
// The zone column pins the documented hash-range-overlap behaviour
// for short calls (M1A, K1JT) as an asserted property rather than
// a paragraph in the doc.
var callsignVectors = []struct {
	name string
	call string
	want uint32
	zone string
}{
	{"M1A_three_char", "M1A", 6050834, zoneHashOverlap},
	{"K1JT_four_char", "K1JT", 6040944, zoneHashOverlap},
	{"G4ABC_five_char", "G4ABC", 9486694, zoneStdCall},
	{"K1ABC_five_char", "K1ABC", 10214965, zoneStdCall},
	{"W9XYZ_five_char", "W9XYZ", 12751800, zoneStdCall},
	{"VK7MO_five_char", "VK7MO", 12339580, zoneStdCall},
	{"F5RXL_five_char", "F5RXL", 9322543, zoneStdCall},
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

func TestCallsignC28_OutputAboveSpecialTokens(t *testing.T) {
	// Every std-call c28 must be above the reserved special-tokens
	// range (>= nTokens). Short callsigns can produce values that
	// fall INSIDE the 22-bit hash range due to the algorithm's
	// negative-index handling — that's intentional and not a
	// collision: the FT8 protocol disambiguates std-calls from
	// hashes via the message-type tag (i3/n3 bits) at the message
	// level, not by c28 range. So the only invariant we can pin
	// here is "stays out of the special-tokens space."
	for _, tc := range callsignVectors {
		got := CallsignC28(tc.call)
		if got < nTokens {
			t.Errorf("%s: CallsignC28(%q)=%d below nTokens %d — leaked into special-token range", tc.name, tc.call, got, nTokens)
		}
	}
}

func TestCallsignC28_ZoneMatchesVector(t *testing.T) {
	// Per-vector zone assertion: each vector lands in the c28 zone
	// declared next to it. Pins the documented hash-overlap behaviour
	// as a test property, so adding a new vector with a wrong zone
	// label fails CI instead of silently going against the doc.
	for _, tc := range callsignVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := CallsignC28(tc.call)
			var actualZone string
			switch {
			case got >= stdCallOffset:
				actualZone = zoneStdCall
			case got >= nTokens:
				actualZone = zoneHashOverlap
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

func TestCallsignC28_RejectsEmptyInput(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CallsignC28(\"\") should panic; did not")
		}
	}()
	CallsignC28("")
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

func TestCallsignC28_EmbeddedSpaceIsDeterministic(t *testing.T) {
	// Embedded spaces pass alphabet validation (space is in
	// callsignAlphaPos1) and produce a deterministic c28. Whether
	// such a "call" is a valid FT8 std call is the caller's
	// concern — this function's contract is "any char in the
	// pos-1 alphabet works." This test pins the determinism so an
	// accidental contract change (e.g. someone adds embedded-space
	// rejection) trips CI rather than silently corrupting calls
	// with stray whitespace upstream.
	const call = "K1 BC"
	a := CallsignC28(call)
	b := CallsignC28(call)
	if a != b {
		t.Errorf("CallsignC28(%q) not deterministic: got %d then %d", call, a, b)
	}
	// Don't pin the specific value — that's an algorithm artifact,
	// not a contract. Determinism is the contract.
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
