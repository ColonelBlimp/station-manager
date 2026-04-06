package message

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- EncodeCallsign ----------------------------------------------

// TestEncodeCallsign_KnownValues verifies encoding against values cross-checked
// with compiled ft8_lib pack_basecall() output (commit 9fec6ca).
// See docs/ft8-callsign-constants-verification.md §7.
//
// Each basecall value is computed via the mixed-radix formula
//
//	n = i0*36*10*27*27*27 + i1*10*27*27*27 + i2*27*27*27 + i3*27*27 + i4*27 + i5
//
// over the normalized 6-char form, then offset by callBase (= NTokens + Max22 = 6,257,896).
//
// Derivations:
//
//	W1AW   → " W1AW " → (0,32,1,1,23,0)  → basecall 6,319,593
//	K1ABC  → " K1ABC" → (0,20,1,1,2,3)   → basecall 3,957,069
//	VK2XYZ → "VK2XYZ" → (32,20,2,24,25,26) → basecall 230,742,323
//	9A1A   → "9A1A  " → (10,10,1,1,0,0)  → basecall 72,847,512
func TestEncodeCallsign_KnownValues(t *testing.T) {
	tests := []struct {
		call string
		want uint32
	}{
		{"W1AW", callBase + 6319593},
		{"K1ABC", callBase + 3957069},
		{"VK2XYZ", callBase + 230742323},
		{"9A1A", callBase + 72847512},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			got, err := EncodeCallsign(tt.call)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeCallsign_CaseInsensitive(t *testing.T) {
	n1, err := EncodeCallsign("w1aw")
	require.NoError(t, err)
	n2, err := EncodeCallsign("W1AW")
	require.NoError(t, err)
	require.Equal(t, n1, n2)
}

func TestEncodeCallsign_LeftPad(t *testing.T) {
	// Single-char prefix: call[1] is a digit → left-padded with space.
	tests := []struct {
		call       string
		normalized string
	}{
		{"W1AW", " W1AW "},
		{"K1ABC", " K1ABC"},
		{"A1A", " A1A  "},
		{"A1ABC", " A1ABC"}, // 5-char: longest single-prefix call
		{"A1", " A1   "},    // 2-char: minimum length call
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			c6, err := normalizeCallsign(tt.call)
			require.NoError(t, err)
			require.Equal(t, tt.normalized, string(c6[:]))
		})
	}
}

func TestEncodeCallsign_NoPad(t *testing.T) {
	// Two-char prefix: call[1] is NOT a digit → no left-pad.
	tests := []struct {
		call       string
		normalized string
	}{
		{"VK2XYZ", "VK2XYZ"},
		{"9A1A", "9A1A  "},
		{"AA1A", "AA1A  "},
	}
	for _, tt := range tests {
		t.Run(tt.call, func(t *testing.T) {
			c6, err := normalizeCallsign(tt.call)
			require.NoError(t, err)
			require.Equal(t, tt.normalized, string(c6[:]))
		})
	}
}

func TestEncodeCallsign_Invalid(t *testing.T) {
	tests := []struct {
		name string
		call string
	}{
		{"too short", "A"},
		{"too long 7 chars", "VK2XYZZ"},
		{"slash", "W1AW/P"},
		{"punctuation", "W1AW!"},
		{"lowercase slash", "w1aw/p"},
		{"digit at suffix pos", "VK21XY"},
		{"no digit at pos 2", "VKAXYZ"},
		{"single-char prefix too long", "K1ABCD"},
		{"empty", ""},
		{"spaces only", "   "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeCallsign(tt.call)
			require.Error(t, err, "expected error for %q", tt.call)
		})
	}
}

// --------------- DecodeCallsign (standard) -----------------------------------

func TestDecodeCallsign_KnownValues(t *testing.T) {
	tests := []struct {
		n28  uint32
		want string
	}{
		{callBase + 6319593, "W1AW"},
		{callBase + 3957069, "K1ABC"},
		{callBase + 230742323, "VK2XYZ"},
		{callBase + 72847512, "9A1A"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := DecodeCallsign(tt.n28)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCallsign_RoundTrip(t *testing.T) {
	calls := []string{
		"W1AW", "K1ABC", "VK2XYZ", "9A1A", "AA1A", "ZZ9ZZZ",
		"A1A", "N0CAL", "3A2X",
		"A1ABC", // 5-char single-prefix call (longest before left-pad overflow)
		"A1",    // 2-char minimum-length call
	}
	for _, call := range calls {
		t.Run(call, func(t *testing.T) {
			n, err := EncodeCallsign(call)
			require.NoError(t, err)
			dec, err := DecodeCallsign(n)
			require.NoError(t, err)
			require.Equal(t, call, dec)
		})
	}
}

// --------------- Boundary values ---------------------------------------------

func TestDecodeCallsign_MaxStandard(t *testing.T) {
	// callBase + NBase - 1 is the maximum standard callsign value → "ZZ9ZZZ".
	dec, err := DecodeCallsign(callBase + NBase - 1)
	require.NoError(t, err)
	require.Equal(t, "ZZ9ZZZ", dec)
}

func TestEncodeCallsign_MaxStandard(t *testing.T) {
	n, err := EncodeCallsign("ZZ9ZZZ")
	require.NoError(t, err)
	require.Equal(t, callBase+NBase-1, n)
}

func TestDecodeCallsign_MinStandard(t *testing.T) {
	// callBase + 0 decodes basecall 0 → " 00   " → trimmed "00".
	// Not a real callsign, but verifies the encoding boundary.
	dec, err := DecodeCallsign(callBase)
	require.NoError(t, err)
	require.Equal(t, "00", dec)
}

func TestDecodeCallsign_FieldLayout(t *testing.T) {
	// Verify the 28-bit field partitioning matches ft8_lib.
	// NTOKENS + MAX22 + NBASE - 1 must equal 2^28 - 1.
	require.Equal(t, uint32(1<<28-1), NTokens+Max22+NBase-1,
		"field layout must exactly fill 28 bits")
}

// --------------- Sentinels ---------------------------------------------------

func TestDecodeCallsign_Sentinels(t *testing.T) {
	tests := []struct {
		n28  uint32
		want string
	}{
		{TokenDE, "DE"},
		{TokenQRZ, "QRZ"},
		{TokenCQ, "CQ"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got, err := DecodeCallsign(tt.n28)
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestEncodeSentinels(t *testing.T) {
	require.Equal(t, uint32(0), EncodeDE())
	require.Equal(t, uint32(1), EncodeQRZ())
	require.Equal(t, uint32(2), EncodeCQ())
}

// --------------- Hash22 ------------------------------------------------------

func TestDecodeCallsign_Hash22(t *testing.T) {
	// Hashed callsign values return "<...>" since the hash cannot be reversed.
	// Hash region: NTokens .. NTokens+Max22-1 (2063592..6257895).
	tests := []struct {
		name string
		n28  uint32
	}{
		{"first hash", NTokens},
		{"mid hash", NTokens + Max22/2},
		{"last hash", NTokens + Max22 - 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dec, err := DecodeCallsign(tt.n28)
			require.NoError(t, err)
			require.Equal(t, "<...>", dec)
		})
	}
}

// --------------- CQ with number ----------------------------------------------

func TestCQNum_RoundTrip(t *testing.T) {
	for _, freq := range []int{0, 350, 999} {
		t.Run(fmt.Sprintf("CQ_%03d", freq), func(t *testing.T) {
			n, err := EncodeCQNum(freq)
			require.NoError(t, err)
			dec, err := DecodeCallsign(n)
			require.NoError(t, err)
			require.Equal(t, fmt.Sprintf("CQ %03d", freq), dec)
		})
	}
}

func TestCQNum_OutOfRange(t *testing.T) {
	_, err := EncodeCQNum(-1)
	require.Error(t, err)
	_, err = EncodeCQNum(1000)
	require.Error(t, err)
}

// --------------- CQ with suffix ----------------------------------------------

func TestCQSuffix_RoundTrip(t *testing.T) {
	suffixes := []string{"A", "DX", "EU", "NA", "POTA", "ZZZZ"}
	for _, suf := range suffixes {
		t.Run("CQ_"+suf, func(t *testing.T) {
			n, err := EncodeCQSuffix(suf)
			require.NoError(t, err)
			dec, err := DecodeCallsign(n)
			require.NoError(t, err)
			require.Equal(t, "CQ "+suf, dec)
		})
	}
}

func TestCQSuffix_ZeroK_DecodesToPlainCQ(t *testing.T) {
	// n28 = tokenCQSufBase (1003) corresponds to k=0 in decodeCQSuffix.
	// k=0 produces an all-space suffix which trims to "", falling back to "CQ".
	// This is the same decoded string as TokenCQ (n28=2), so the two distinct
	// 28-bit values are ambiguous on decode. EncodeCQSuffix never produces k=0
	// (minimum suffix is one letter → k ≥ 1), so no round-trip hazard exists.
	dec, err := DecodeCallsign(tokenCQSufBase)
	require.NoError(t, err)
	require.Equal(t, "CQ", dec, "tokenCQSufBase with k=0 should decode as plain CQ")

	// Confirm it matches the plain-CQ token decode.
	plainCQ, err := DecodeCallsign(TokenCQ)
	require.NoError(t, err)
	require.Equal(t, plainCQ, dec, "k=0 suffix decode must match plain-CQ token decode")
}

func TestCQSuffix_CaseInsensitive(t *testing.T) {
	n1, err := EncodeCQSuffix("dx")
	require.NoError(t, err)
	n2, err := EncodeCQSuffix("DX")
	require.NoError(t, err)
	require.Equal(t, n1, n2)
}

func TestCQSuffix_Invalid(t *testing.T) {
	tests := []struct {
		name   string
		suffix string
	}{
		{"empty", ""},
		{"too long", "ABCDE"},
		{"digit", "D1"},
		{"space", "D X"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := EncodeCQSuffix(tt.suffix)
			require.Error(t, err)
		})
	}
}

// --------------- Out of range ------------------------------------------------

func TestDecodeCallsign_OutOfRange(t *testing.T) {
	// Values above callBase + NBase - 1 are out of range.
	_, err := DecodeCallsign(callBase + NBase)
	require.Error(t, err)

	// Max 28-bit value is exactly callBase + NBase - 1, so 2^28 should fail.
	// (2^28 doesn't fit in the 28-bit field, but if passed, it's out of range.)
}

func TestDecodeCallsign_ReservedToken(t *testing.T) {
	// Values between tokenCQSufMax+1 and NTokens-1 are reserved.
	_, err := DecodeCallsign(tokenCQSufMax + 1)
	require.Error(t, err)

	_, err = DecodeCallsign(NTokens - 1)
	require.Error(t, err)
}

// --------------- charsetIndex ------------------------------------------------

func TestCharsetIndex(t *testing.T) {
	require.Equal(t, 0, charsetIndex(' '))
	require.Equal(t, 1, charsetIndex('0'))
	require.Equal(t, 10, charsetIndex('9'))
	require.Equal(t, 11, charsetIndex('A'))
	require.Equal(t, 36, charsetIndex('Z'))
	require.Equal(t, -1, charsetIndex('!'))
	require.Equal(t, -1, charsetIndex('a'))
}

// --------------- letterSpaceMR -----------------------------------------------

func TestLetterSpaceMR_ValidIndices(t *testing.T) {
	// Space (charset index 0) → mixed-radix 0.
	require.Equal(t, uint32(0), letterSpaceMR(0))
	// A (charset index 11) → mixed-radix 1.
	require.Equal(t, uint32(1), letterSpaceMR(11))
	// Z (charset index 36) → mixed-radix 26.
	require.Equal(t, uint32(26), letterSpaceMR(36))
}

func TestLetterSpaceMR_PanicsOnDigitIndex(t *testing.T) {
	// Digit indices (1–10) are invalid for letter-or-space positions.
	for idx := 1; idx <= 10; idx++ {
		idx := idx
		t.Run(fmt.Sprintf("idx=%d", idx), func(t *testing.T) {
			require.Panics(t, func() { letterSpaceMR(idx) })
		})
	}
}

func TestLetterSpaceMR_PanicsOnNegativeIndex(t *testing.T) {
	require.Panics(t, func() { letterSpaceMR(-1) })
}

func TestLetterSpaceMR_PanicsOnOutOfRange(t *testing.T) {
	require.Panics(t, func() { letterSpaceMR(37) })
}
