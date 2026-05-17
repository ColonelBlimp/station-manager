package codec

import (
	"strings"
	"testing"
)

// FreeTextToF71 test vectors generated 2026-05-17 using the public-
// domain `free_text_to_f71` reference program from QEX ref [14]
// (built from `free_text_to_f71.f90`). Per QEX paper §9 the
// reference is in the public domain; the 71-bit strings below are
// facts produced by running it against chosen inputs.
//
// Coverage spans length range, alphabet boundaries, and structural
// variety:
//
//   - "TNX BOB 73 GL" — the reference's own example, mid-length
//   - "" — empty input; algorithm pads with spaces; result = all zeros
//   - "CQ" — 2 chars, exercises short-input padding
//   - "HELLO WORLD" — 11 chars with embedded space, common pattern
//   - "ABCDEFGHIJKLM" — full 13 chars, all letters
//   - "1234567890123" — full 13 chars, all digits (different base-42 region)
//   - "TEST/123" — exercises the '/' char (alphabet position 40)
//   - "73 K1JT" — short callsign-style message
//
// Each `want` is the 71-character '0'/'1' string the reference
// produces, captured verbatim from its output.
var freeTextVectors = []struct {
	name string
	in   string
	want string
}{
	{"reference_example", "TNX BOB 73 GL", "01100011111011011100111011100010101001001010111000000111111101010000000"},
	{"empty_all_zeros", "", "00000000000000000000000000000000000000000000000000000000000000000000000"},
	{"two_char_CQ", "CQ", "00000000000000000000000000000000000000000000000000000000000001000111101"},
	{"hello_world", "HELLO WORLD", "00000000000010001011010101101001100000011011100110110001010100000010010"},
	{"all_letters_13char", "ABCDEFGHIJKLM", "00100100111001000010000000001011111011100010010110011110110001100110111"},
	{"all_digits_13char", "1234567890123", "00000110110001100011010101110101101110110000010110101111010101101110010"},
	{"slash_TEST_123", "TEST/123", "00000000000000000000000000001100101111001011111101011001001000101001010"},
	{"callsign_style", "73 K1JT", "00000000000000000000000000000000000101001011000101000000110001100110110"},
}

func TestFreeTextToF71_SpecVectors(t *testing.T) {
	for _, tc := range freeTextVectors {
		t.Run(tc.name, func(t *testing.T) {
			got := FreeTextToF71(tc.in)
			gotStr := bitsToString(got)
			if gotStr != tc.want {
				t.Errorf("FreeTextToF71(%q):\n  got  %s\n  want %s", tc.in, gotStr, tc.want)
			}
		})
	}
}

func TestFreeTextToF71_OutputLength(t *testing.T) {
	for _, tc := range freeTextVectors {
		got := FreeTextToF71(tc.in)
		if len(got) != F71Bits {
			t.Errorf("%s: len = %d, want %d", tc.name, len(got), F71Bits)
		}
	}
}

func TestFreeTextToF71_OutputBitsAreBinary(t *testing.T) {
	for _, tc := range freeTextVectors {
		got := FreeTextToF71(tc.in)
		for i, b := range got {
			if b > 1 {
				t.Errorf("%s: out[%d] = %d, must be 0 or 1", tc.name, i, b)
			}
		}
	}
}

func TestFreeTextToF71_EmptyInputAllZero(t *testing.T) {
	got := FreeTextToF71("")
	for i, b := range got {
		if b != 0 {
			t.Errorf("FreeTextToF71(\"\"): bit %d = %d, want 0", i, b)
		}
	}
}

func TestFreeTextToF71_RejectsOversizedInput(t *testing.T) {
	// Each input is in-alphabet and only fails the length check,
	// so the rejection signal is unambiguous (no dual-rejection
	// confusion with the alphabet check).
	cases := []string{
		"ABCDEFGHIJKLMN",        // 14 chars, all in alphabet
		strings.Repeat("X", 50), // 50 chars, all in alphabet
	}
	for _, in := range cases {
		t.Run(in[:min(len(in), 20)], func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("FreeTextToF71(len=%d) should panic on length; did not", len(in))
				}
			}()
			FreeTextToF71(in)
		})
	}
}

func TestFreeTextToF71_RejectsNonAlphabetChars(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"lowercase", "hello"},
		{"exclamation_not_in_alphabet", "HELLO!"},
		{"comma_not_in_alphabet", "A,B"},
		{"underscore_not_in_alphabet", "A_B"},
		{"non_ascii", "HÉLLO"},
		{"control_char", "A\x01B"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("FreeTextToF71(%q) should panic; did not", tc.in)
				}
			}()
			FreeTextToF71(tc.in)
		})
	}
}

// TestFreeTextToF71_AcceptsAllAlphabetChars exercises every position
// in the 42-char alphabet through the encoder. Pinned as a positive
// invariant against accidental alphabet narrowing.
func TestFreeTextToF71_AcceptsAllAlphabetChars(t *testing.T) {
	for i, c := range f71Alphabet {
		input := string(c)
		// FreeTextToF71 must not panic for any single-char input
		// drawn from the alphabet.
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("alphabet char %d (%q) caused panic: %v", i, c, r)
				}
			}()
			_ = FreeTextToF71(input)
		}()
	}
}

func BenchmarkFreeTextToF71(b *testing.B) {
	const text = "TNX BOB 73 GL"
	b.ReportAllocs()
	for range b.N {
		_ = FreeTextToF71(text)
	}
}

// bitsToString turns a bit-per-byte slice into a string of '0'/'1'
// chars. Inverse of grid_test.go's `bitsFrom()` helper.
func bitsToString(b []byte) string {
	var sb strings.Builder
	sb.Grow(len(b))
	for _, v := range b {
		if v == 0 {
			sb.WriteByte('0')
		} else {
			sb.WriteByte('1')
		}
	}
	return sb.String()
}
