package sandbox

import (
	"testing"
)

// TestUnpackCallsign28_Tokens covers the special-token range of c28.
func TestUnpackCallsign28_Tokens(t *testing.T) {
	cases := []struct {
		n    uint32
		want string
		ok   bool
	}{
		{0, "DE", true},
		{1, "QRZ", true},
		{2, "CQ", true},
		{3, "CQ 000", true},
		{12, "CQ 009", true},
		{1002, "CQ 999", true},
		{1003, "CQ AAAA", true},
		{1003 + 26*26*26*26 - 1, "CQ ZZZZ", true},
	}
	for _, tc := range cases {
		got, ok := unpackCallsign28(tc.n)
		if got != tc.want || ok != tc.ok {
			t.Errorf("unpackCallsign28(%d) = %q,%v; want %q,%v",
				tc.n, got, ok, tc.want, tc.ok)
		}
	}
}

// TestUnpackCallsign28_HashPlaceholder pins that the 22-bit hash
// placeholder range is surfaced as a placeholder string with OK=false.
func TestUnpackCallsign28_HashPlaceholder(t *testing.T) {
	n := uint32(ntokens) + 1234
	got, ok := unpackCallsign28(n)
	if ok {
		t.Errorf("expected ok=false for hash-placeholder %d", n)
	}
	if got == "" {
		t.Errorf("expected non-empty placeholder text, got %q", got)
	}
}

// TestDecodeStandardCall_KnownEncodings pins the mixed-radix
// callsign decoder against hand-computed reference values.
//
// Encoding formula (per std_call_to_c28.f90, with the 6-char string
// right-justified so the digit lands at slot 3):
//
//	n = i1·(36·10·27³) + i2·(10·27³) + i3·(27³) + i4·(27²) + i5·27 + i6
//
// Hand-computed n for "K1JT" → " K1JT ":
//
//	i1=0 (' '), i2=20 ('K'), i3=1 ('1'), i4=10 ('J'), i5=20 ('T'), i6=0 (' ')
//	n = 20·(10·19683) + 1·19683 + 10·729 + 20·27 + 0
//	  = 3 936 600 + 19 683 + 7 290 + 540
//	  = 3 964 113
//
// And for "OH8X" → " OH8X ":
//
//	i1=0 (' '), i2=24 ('O'), i3=8 ('8') - wait, '8' is callA3[8]
//	Actually 'O' is callA2[24]; let's just check the expected output.
func TestDecodeStandardCall_KnownEncodings(t *testing.T) {
	cases := []struct {
		n    uint32
		want string
	}{
		{3964113, "K1JT"},
	}
	for _, tc := range cases {
		got := decodeStandardCall(tc.n)
		if got != tc.want {
			t.Errorf("decodeStandardCall(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

// TestUnpackGrid15_KnownGrids hand-computes a few Maidenhead grid
// encodings and checks they round-trip through unpackGrid15.
//
//	"FN20" → j1=5·1800 + 13·100 + 2·10 + 0 = 9000 + 1300 + 20 = 10320
//	"IO91" → j1=8·1800 + 14·100 + 9·10 + 1 = 14400 + 1400 + 90 + 1 = 15891
//	"PM95" → j1=15·1800 + 12·100 + 9·10 + 5 = 27000 + 1200 + 90 + 5 = 28295
func TestUnpackGrid15_KnownGrids(t *testing.T) {
	cases := []struct {
		n    uint32
		want string
	}{
		{10320, "FN20"},
		{15891, "IO91"},
		{28295, "PM95"},
	}
	for _, tc := range cases {
		got, ok := unpackGrid15(tc.n)
		if !ok {
			t.Errorf("unpackGrid15(%d) ok=false; want true", tc.n)
		}
		if got != tc.want {
			t.Errorf("unpackGrid15(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

// TestUnpackGrid15_ReportsAndTokens covers the maxGrid4+ range:
// reports, RRR, RR73, 73, and the empty-grid marker.
//
//	maxGrid4 + 1: "" (empty)
//	maxGrid4 + 2: "RRR"
//	maxGrid4 + 3: "RR73"
//	maxGrid4 + 4: "73"
//	maxGrid4 + 35: "+00" (dB = irpt - 35 = 0)
//	maxGrid4 + 23: "-12" (dB = 23 - 35 = -12)
//	maxGrid4 + 40: "+05" (dB = 5)
func TestUnpackGrid15_ReportsAndTokens(t *testing.T) {
	cases := []struct {
		n    uint32
		want string
	}{
		{maxGrid4 + 1, ""},
		{maxGrid4 + 2, "RRR"},
		{maxGrid4 + 3, "RR73"},
		{maxGrid4 + 4, "73"},
		{maxGrid4 + 35, "+00"},
		{maxGrid4 + 23, "-12"},
		{maxGrid4 + 40, "+05"},
	}
	for _, tc := range cases {
		got, ok := unpackGrid15(tc.n)
		if !ok {
			t.Errorf("unpackGrid15(%d) ok=false; want true", tc.n)
		}
		if got != tc.want {
			t.Errorf("unpackGrid15(%d) = %q; want %q", tc.n, got, tc.want)
		}
	}
}

// TestUnpack77_SynthesisedType1 builds a Type 1 payload by packing
// known field values and runs Unpack77 on it. This validates the
// trailing-i3 dispatch and the field-layout offsets end-to-end,
// independent of the LDPC/BP layers.
//
// Synthesises "CQ K1JT FN20":
//
//	c28_1 = 2 (CQ token)
//	p1    = 0
//	c28_2 = NTOKENS + MAX22 + 3_964_113 = 10_222_009 (K1JT)
//	p2    = 0
//	r1    = 0
//	g15   = 10320 (FN20)
//	i3    = 1
func TestUnpack77_SynthesisedType1(t *testing.T) {
	var payload [LDPCPayloadBits]uint8
	writeBits(payload[:], 0, 28, 2)                 // c28_1 = CQ
	writeBits(payload[:], 29, 28, callBase+3964113) // c28_2 = K1JT
	writeBits(payload[:], 59, 15, 10320)            // g15 = FN20
	writeBits(payload[:], 74, 3, 1)                 // i3 = 1 (Type 1)

	res := Unpack77(payload)
	if !res.OK {
		t.Fatalf("Unpack77 returned ok=false (detail=%q)", res.Detail)
	}
	if res.Text != "CQ K1JT FN20" {
		t.Errorf("Unpack77.Text = %q; want %q", res.Text, "CQ K1JT FN20")
	}
	if res.I3 != 1 {
		t.Errorf("Unpack77.I3 = %d; want 1", res.I3)
	}
}

// writeBits packs an integer value into the bit slice starting at
// offset, n bits wide, MSB-first.
func writeBits(bits []uint8, offset, n int, value uint64) {
	for i := 0; i < n; i++ {
		bits[offset+i] = uint8((value >> uint(n-1-i)) & 1)
	}
}
