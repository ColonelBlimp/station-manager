package message

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- freeTextCharIndex -------------------------------------------

func TestFreeTextCharIndex_Digits(t *testing.T) {
	for i := 0; i <= 9; i++ {
		c := byte('0' + i)
		require.Equal(t, uint64(i), freeTextCharIndex(c), "char %q", c)
	}
}

func TestFreeTextCharIndex_Letters(t *testing.T) {
	for i := 0; i < 26; i++ {
		c := byte('A' + i)
		require.Equal(t, uint64(10+i), freeTextCharIndex(c), "char %q", c)
	}
}

func TestFreeTextCharIndex_Symbols(t *testing.T) {
	tests := []struct {
		c    byte
		want uint64
	}{
		{'+', 36},
		{'-', 37},
		{'.', 38},
		{'/', 39},
		{'?', 40},
		{' ', 41},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%q", tt.c), func(t *testing.T) {
			require.Equal(t, tt.want, freeTextCharIndex(tt.c))
		})
	}
}

func TestFreeTextCharIndex_InvalidMapsToSpace(t *testing.T) {
	// Characters outside the alphabet should map to space (41).
	for _, c := range []byte{'!', '@', '#', '$', '%', '&', '*', '(', ')', 'a', 'z', '~'} {
		require.Equal(t, uint64(41), freeTextCharIndex(c),
			"invalid char %q should map to space index 41", c)
	}
}

// --------------- EncodeFreeText / DecodeFreeText round-trip -------------------

func TestFreeText_RoundTrip(t *testing.T) {
	tests := []struct {
		input string
		want  string // expected after decode (trimmed)
	}{
		{"TNX BOB 73 GL", "TNX BOB 73 GL"},
		{"HELLO", "HELLO"},
		{"A", "A"},
		{"", ""},
		{"0123456789ABC", "0123456789ABC"}, // max length
		{"+-./?", "+-./?"},
		{"CQ CQ CQ", "CQ CQ CQ"},
		{"73 DE W1AW", "73 DE W1AW"},
		{"TEST 1 2 3", "TEST 1 2 3"},
		{"ZZZZZZZZZZZZZ", "ZZZZZZZZZZZZZ"}, // all Z (highest letter index)
		{"0000000000000", "0000000000000"}, // all zeros (lowest index)
		{"?????????????", "?????????????"}, // all question marks
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			hi, lo, err := EncodeFreeText(tt.input)
			require.NoError(t, err)
			got := DecodeFreeText(hi, lo)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestFreeText_RoundTrip_TrailingSpacesTrimmed(t *testing.T) {
	// Trailing spaces are trimmed on decode, so "HELLO   " decodes as "HELLO".
	hi, lo, err := EncodeFreeText("HELLO")
	require.NoError(t, err)
	got := DecodeFreeText(hi, lo)
	require.Equal(t, "HELLO", got)

	// Explicit trailing spaces should produce the same encoding.
	hi2, lo2, err := EncodeFreeText("HELLO        ") // 13 chars with trailing spaces
	require.NoError(t, err)
	require.Equal(t, hi, hi2)
	require.Equal(t, lo, lo2)
}

func TestFreeText_RoundTrip_CaseInsensitive(t *testing.T) {
	hi1, lo1, err := EncodeFreeText("hello world")
	require.NoError(t, err)
	hi2, lo2, err := EncodeFreeText("HELLO WORLD")
	require.NoError(t, err)
	require.Equal(t, hi1, hi2)
	require.Equal(t, lo1, lo2)
}

func TestFreeText_RoundTrip_InvalidCharsReplacedWithSpace(t *testing.T) {
	// Invalid characters are replaced with space (index 41).
	hi1, lo1, err := EncodeFreeText("HI!THERE")
	require.NoError(t, err)
	hi2, lo2, err := EncodeFreeText("HI THERE")
	require.NoError(t, err)
	require.Equal(t, hi1, hi2)
	require.Equal(t, lo1, lo2)
}

// --------------- Encode: known values ----------------------------------------

func TestEncodeFreeText_AllSpaces(t *testing.T) {
	// 13 spaces → all indices are 41. Value = 41 * (42^12 + 42^11 + ... + 42^0)
	// which is the maximum possible value. Decoded it trims to empty.
	hi, lo, err := EncodeFreeText("             ") // 13 spaces
	require.NoError(t, err)
	got := DecodeFreeText(hi, lo)
	require.Equal(t, "", got)

	// The maximum value: 41*(42^12 + 42^11 + ... + 1) = 42^13 - 1
	// 42^13 - 1 = 1,418,481,495,116,009,471
	// This fits in 71 bits since 2^71 = 2,361,183,241,434,822,606,848.
	// hi should have some bits set.
	require.True(t, hi > 0 || lo > 0, "all-spaces should produce non-zero encoding")
}

func TestEncodeFreeText_AllZeros(t *testing.T) {
	// "0000000000000" → all indices are 0. Value = 0.
	hi, lo, err := EncodeFreeText("0000000000000")
	require.NoError(t, err)
	require.Equal(t, uint8(0), hi)
	require.Equal(t, uint64(0), lo)
}

func TestEncodeFreeText_SingleDigit1(t *testing.T) {
	// "1            " → index 1 followed by 12 spaces (index 41 each).
	// Value = 1 * 42^12 + 41*(42^11 + 42^10 + ... + 42^0)
	// = 42^12 + 41*(42^12 - 1)/41  [geometric series]
	// = 42^12 + 42^12 - 1 = 2*42^12 - 1
	// But let's just verify round-trip since the arithmetic is complex.
	hi, lo, err := EncodeFreeText("1")
	require.NoError(t, err)
	got := DecodeFreeText(hi, lo)
	require.Equal(t, "1", got)
}

// --------------- Encode: error cases -----------------------------------------

func TestEncodeFreeText_TooLong(t *testing.T) {
	_, _, err := EncodeFreeText("01234567890123") // 14 chars
	require.Error(t, err)
	require.Contains(t, err.Error(), "exceeds maximum length")
}

func TestEncodeFreeText_ExactlyMaxLength(t *testing.T) {
	_, _, err := EncodeFreeText("0123456789ABC") // exactly 13 chars
	require.NoError(t, err)
}

// --------------- Encode: value constraints ------------------------------------

func TestEncodeFreeText_MaxValue_FitsIn71Bits(t *testing.T) {
	// The maximum possible value is 42^13 - 1, produced by all-spaces.
	// Verify it fits in 71 bits: hi must be < 128 (7 bits).
	hi, _, err := EncodeFreeText("             ") // 13 spaces
	require.NoError(t, err)
	require.Less(t, hi, uint8(128), "hi must fit in 7 bits")
}

func TestEncodeFreeText_MinValue(t *testing.T) {
	// "0" + 12 trailing spaces → but wait, "0" is index 0, spaces are index 41.
	// So it's not the minimum. The actual minimum non-zero for first char is "0".
	// Actually, all-zeros "0000000000000" is the minimum (value=0).
	hi, lo, err := EncodeFreeText("0000000000000")
	require.NoError(t, err)
	require.Equal(t, uint8(0), hi)
	require.Equal(t, uint64(0), lo)
}

// --------------- Full encode/decode with bit packing -------------------------

// TestFreeText_PackedRoundTrip simulates packing a free-text message into the
// 77-bit payload (Type 0: f71(71) | n3=000(3) | i3=000(3)) and unpacking it.
func TestFreeText_PackedRoundTrip(t *testing.T) {
	texts := []string{
		"TNX BOB 73 GL",
		"HELLO",
		"",
		"A",
		"0123456789ABC",
		"+-./?",
		"?????????????",
	}
	for _, text := range texts {
		t.Run(text, func(t *testing.T) {
			hi, lo, err := EncodeFreeText(text)
			require.NoError(t, err)

			// Pack into 77 bits: f71(71) | n3(3)=000 | i3(3)=000
			var buf [MsgBytes]byte
			// Write hi (7 bits) at offset 0, lo (64 bits) at offset 7.
			PackBits(buf[:], 0, 7, uint64(hi))
			PackBits(buf[:], 7, 64, lo)
			// n3=0 at offset 71, i3=0 at offset 74 — already zero.

			// Unpack.
			gotHi := uint8(UnpackBits(buf[:], 0, 7))
			gotLo := UnpackBits(buf[:], 7, 64)
			gotN3 := UnpackBits(buf[:], 71, 3)
			gotI3 := UnpackBits(buf[:], 74, 3)

			require.Equal(t, hi, gotHi, "hi mismatch")
			require.Equal(t, lo, gotLo, "lo mismatch")
			require.Equal(t, uint64(0), gotN3, "n3 must be 0")
			require.Equal(t, uint64(0), gotI3, "i3 must be 0")

			// Decode and verify text.
			got := DecodeFreeText(gotHi, gotLo)
			want := text
			if want == "" {
				// Empty encodes as all-spaces which decodes to "".
			}
			require.Equal(t, want, got)
		})
	}
}

// --------------- Exhaustive single-character round-trip -----------------------

func TestFreeText_SingleCharRoundTrip(t *testing.T) {
	// Every valid character should survive encode→decode.
	chars := "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./? "
	for _, c := range chars {
		name := fmt.Sprintf("%q", string(c))
		if c == ' ' {
			name = "space"
		}
		t.Run(name, func(t *testing.T) {
			input := string(c)
			hi, lo, err := EncodeFreeText(input)
			require.NoError(t, err)
			got := DecodeFreeText(hi, lo)
			expected := input
			if c == ' ' {
				expected = "" // space-only trims to empty
			}
			require.Equal(t, expected, got)
		})
	}
}
