package codec

import "testing"

// CallsignC58 test vectors generated 2026-05-17 using the public-
// domain `nonstd_to_c58` reference program from QEX ref [14] (built
// from `nonstd_to_c58.f90`). Per QEX paper §9 the reference is in
// the public domain; the c58 values below are facts produced by
// running it against chosen inputs.
//
// Coverage:
//
//   - Compound call with '/' separator (PJ4/K1ABC, M/K1ABC,
//     VK7ABC/P, K1ABC/QRP) — the canonical non-standard case
//     c58 exists for
//   - Special-event call YW18FIFA (8 chars)
//   - Standard calls (K1ABC, K1JT, G4ABC, 7Q5MLV) — c58 encodes
//     standard calls too; in practice the standard slot uses c28,
//     but c58 is the universal fallback
//   - Empty string — every char padded to space; n58 = 0
var c58Vectors = []struct {
	name string
	call string
	want uint64
}{
	{"compound_PJ4_K1ABC", "PJ4/K1ABC", 166563865821947300},
	{"special_event_YW18FIFA", "YW18FIFA", 225199321060198248},
	{"std_call_K1ABC", "K1ABC", 132222118852965952},
	{"std_call_K1JT", "K1JT", 132263269320526080},
	{"std_call_G4ABC", "G4ABC", 107604919764801600},
	{"std_call_7Q5MLV_malawi", "7Q5MLV", 54715316605359104},
	{"compound_VK7ABC_P", "VK7ABC/P", 204408395410529952},
	{"compound_M_K1ABC", "M/K1ABC", 150603434814757136},
	{"empty_all_spaces", "", 0},
	{"compound_K1ABC_QRP", "K1ABC/QRP", 132222121842539800},
}

func TestCallsignC58_SpecVectors(t *testing.T) {
	for _, tc := range c58Vectors {
		t.Run(tc.name, func(t *testing.T) {
			got := CallsignC58(tc.call)
			if got != tc.want {
				t.Errorf("CallsignC58(%q): got %d (0x%015x), want %d (0x%015x)",
					tc.call, got, got, tc.want, tc.want)
			}
		})
	}
}

func TestCallsignC58_OutputFitsIn58Bits(t *testing.T) {
	// Every result must fit in 58 bits (38^11 < 2^58 by construction).
	// Belt-and-braces invariant.
	for _, tc := range c58Vectors {
		got := CallsignC58(tc.call)
		if got >= (1 << C58Bits) {
			t.Errorf("%s: CallsignC58(%q)=%d exceeds 58 bits", tc.name, tc.call, got)
		}
	}
}

func TestCallsignC58_EmptyInputZero(t *testing.T) {
	// All 11 chars pad to space (alphabet index 0); n58 stays 0
	// across all iterations.
	if got := CallsignC58(""); got != 0 {
		t.Errorf("CallsignC58(\"\") = %d, want 0", got)
	}
}

func TestCallsignC58_RejectsOversizedInput(t *testing.T) {
	cases := []struct {
		name string
		call string
	}{
		{"12char_in_alphabet", "ABCDEFGHIJKL"},
		{"50char_X", "XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CallsignC58(%q) should panic on length; did not", tc.call)
				}
			}()
			CallsignC58(tc.call)
		})
	}
}

func TestCallsignC58_RejectsNonAlphabetChars(t *testing.T) {
	cases := []struct {
		name string
		call string
	}{
		{"lowercase", "k1abc"},
		{"hyphen", "K1-ABC"},
		{"colon", "K1:ABC"},
		{"plus", "K1+ABC"},
		{"period", "K1.ABC"},
		{"non_ascii", "K1ÉBC"},
		{"control_char", "K1\x01BC"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("CallsignC58(%q) should panic on alphabet; did not", tc.call)
				}
			}()
			CallsignC58(tc.call)
		})
	}
}

// TestCallsignC58_AcceptsAllAlphabetChars mirrors the same positive
// invariant in HashCodes / FreeTextToF71 tests — every position of
// the shared 38-char hashAlphabet must survive the encoder.
func TestCallsignC58_AcceptsAllAlphabetChars(t *testing.T) {
	for i, c := range hashAlphabet {
		input := string(c)
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("alphabet char %d (%q) caused panic: %v", i, c, r)
				}
			}()
			_ = CallsignC58(input)
		}()
	}
}

// TestCallsignC58_LeftJustifyAsymmetry pins the documented padding
// behaviour: CallsignC58 left-justifies (trailing spaces), so a
// short input like "K1JT" produces the SAME value as "K1JT" followed
// by trailing spaces up to 11 chars. Leading spaces in the input,
// by contrast, are NOT absorbed — they're treated as content (each
// leading space multiplies the accumulator by 38 before the real
// chars contribute, so the result differs).
//
// This is the opposite asymmetry from FreeTextToF71 (which right-
// justifies — leading spaces absorbed, trailing preserved).
// Captures the doc's *Padding asymmetry across the codec's string
// primitives* note as a test property.
func TestCallsignC58_LeftJustifyAsymmetry(t *testing.T) {
	// Trailing spaces are equivalent to omitting them (padding does
	// the same thing internally).
	a := CallsignC58("K1JT")
	b := CallsignC58("K1JT   ") // 4 chars + 3 trailing spaces (still ≤ 11)
	if a != b {
		t.Errorf("trailing spaces should not change result: K1JT=%d, K1JT___=%d", a, b)
	}

	// Leading spaces ARE content for c58 (different from f71).
	// "K1JT" produces a different value than " K1JT".
	c := CallsignC58(" K1JT") // leading space + K1JT
	if c == a {
		t.Errorf("leading space should change c58 result; both produced %d", c)
	}
}

func BenchmarkCallsignC58(b *testing.B) {
	const call = "PJ4/K1ABC"
	b.ReportAllocs()
	for range b.N {
		_ = CallsignC58(call)
	}
}
