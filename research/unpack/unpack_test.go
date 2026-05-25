package unpack

import (
	"testing"
)

// TestDecodeGrid4_KnownPoints pins a few hand-picked Maidenhead grid
// values from the standard encoding. The four-letter conventions
// (j1, j2 ∈ A..R; j3, j4 ∈ 0..9) are well established in amateur
// radio; encoding "AA00" should yield 0, "RR99" the max, etc.
func TestDecodeGrid4_KnownPoints(t *testing.T) {
	cases := []struct {
		g    uint16
		want string
	}{
		{0, "AA00"},
		{1, "AA01"},
		{10, "AA10"},
		{100, "AB00"},
		{1800, "BA00"},
		// FN20 = F=5, N=13, 2, 0 → 5*1800 + 13*100 + 2*10 + 0 = 9000+1300+20 = 10320.
		{10320, "FN20"},
		// JN58 = 9, 13, 5, 8 → 9*1800 + 13*100 + 5*10 + 8 = 16200+1300+50+8 = 17558.
		{17558, "JN58"},
		// Maximum grid: RR99 = 17, 17, 9, 9 → 17*1800 + 17*100 + 17*10 + 9 ... wait
		//   17*1800 = 30600, +17*100 = 1700, +9*10 = 90, +9 = 9. Total = 32399.
		{32399, "RR99"},
	}
	for _, c := range cases {
		if got := decodeGrid4(c.g); got != c.want {
			t.Errorf("decodeGrid4(%d) = %q, want %q", c.g, got, c.want)
		}
	}
}

// TestDecodeG15_SpecialTokens pins the report / blank / RRR / RR73 / 73
// region directly above MAXGRID4.
func TestDecodeG15_SpecialTokens(t *testing.T) {
	cases := []struct {
		g    uint16
		want string
	}{
		{g15MaxGrid4 + 1, ""},     // blank
		{g15MaxGrid4 + 2, "RRR"},  //
		{g15MaxGrid4 + 3, "RR73"}, //
		{g15MaxGrid4 + 4, "73"},   //
		{g15MaxGrid4 + 5, "-30"},  // most negative report
		{g15MaxGrid4 + 35, "+00"}, // zero report
		{g15MaxGrid4 + 37, "+02"}, // +02 dB report
		{g15MaxGrid4 + 26, "-09"}, // -09 dB report
		{g15MaxGrid4 + 65, "+30"}, // most positive standard report
	}
	for _, c := range cases {
		got, err := decodeG15(c.g)
		if err != nil {
			t.Errorf("decodeG15(%d) error: %v", c.g, err)
			continue
		}
		if got != c.want {
			t.Errorf("decodeG15(%d) = %q, want %q", c.g, got, c.want)
		}
	}
}

// TestDecodeC28_SpecialTokens pins the three reserved tokens at the
// bottom of the c28 space — these are the only ones whose mapping
// is fully nailed down in the QEX paper text.
func TestDecodeC28_SpecialTokens(t *testing.T) {
	cases := []struct {
		c28  uint32
		want string
	}{
		{0, "DE"},
		{1, "QRZ"},
		{2, "CQ"},
		{3, "CQ 000"},
		{1002, "CQ 999"},
	}
	for _, c := range cases {
		got := decodeC28Token(c.c28)
		if got != c.want {
			t.Errorf("decodeC28Token(%d) = %q, want %q", c.c28, got, c.want)
		}
	}
}

// TestDecodeC28_HashRange confirms any value inside the 22-bit hash
// region returns the "<...>" placeholder rather than attempting to
// resolve a callsign we have no hashtable for.
func TestDecodeC28_HashRange(t *testing.T) {
	for _, c := range []uint32{
		c28NTokens,                // first hash value
		c28NTokens + 1,            //
		c28NTokens + c28MAX22/2,   // middle of hash range
		c28NTokens + c28MAX22 - 1, // last hash value
	} {
		got, err := decodeC28(c)
		if err != nil {
			t.Errorf("decodeC28(%d) error: %v", c, err)
			continue
		}
		if got != "<...>" {
			t.Errorf("decodeC28(%d) = %q, want %q", c, got, "<...>")
		}
	}
}

// TestUnpack_BitOrderIsBigEndian pins the convention. Feed a payload
// where every bit is 0 except a single 1 at the high bit of c28:
// c28 should read as a large value, not 1.
func TestUnpack_BitOrderIsBigEndian(t *testing.T) {
	var info [91]uint8
	info[0] = 1 // MSB of c28 field
	c1 := bitsToUint(info[0:28])
	want := uint64(1) << 27
	if c1 != want {
		t.Errorf("bitsToUint MSB convention: got %d, want %d", c1, want)
	}
}

// TestUnpack_Type1RoundTrip builds a 77-bit Type 1 payload from a
// known c28/g15 set, runs Unpack, verifies the output text. The
// c28 values used are reserved tokens (CQ) plus standard-callsign
// values computed from the encoder formula in std_call_to_c28.f90.
//
// This is the load-bearing structural test: if bit alignment or
// field ordering is wrong, this test fails before any real decode
// is attempted.
func TestUnpack_Type1RoundTrip(t *testing.T) {
	// Encode K1JT as c28: i1=' ' (idx 0), i2='K' (idx 20 in a2 = "0-9A-Z"),
	// i3='1' (idx 1), i4='J' (idx 10 in a4 = " A-Z"), i5='T' (idx 20),
	// i6=' ' (idx 0).
	// n28 = NTOKENS + MAX22 + 36*10*27^3*0 + 10*27^3*20 + 27^3*1 + 27^2*10
	//                       + 27*20 + 0
	c28K1JT := c28NTokens + c28MAX22 +
		36*10*27*27*27*0 +
		10*27*27*27*20 +
		27*27*27*1 +
		27*27*10 +
		27*20 +
		0

	// g15 for FN20: F=5, N=13, 2, 0 → 5*1800 + 13*100 + 2*10 + 0 = 10320.
	g15FN20 := uint16(10320)

	// Build payload: c28(CQ=2) p1(0) c28(K1JT) p1(0) R1(0) g15(FN20) i3(1).
	var info [91]uint8
	writeBits(info[0:28], uint64(2))
	info[28] = 0
	writeBits(info[29:57], uint64(c28K1JT))
	info[57] = 0
	info[58] = 0
	writeBits(info[59:74], uint64(g15FN20))
	writeBits(info[74:77], uint64(1)) // i3 = 1

	got, err := Unpack(info)
	if err != nil {
		t.Fatalf("Unpack error: %v", err)
	}
	want := "CQ K1JT FN20"
	if got.Text != want {
		t.Errorf("Unpack text = %q, want %q", got.Text, want)
	}
	if got.MsgType != 1 {
		t.Errorf("Unpack msgType = %d, want 1", got.MsgType)
	}
}

// writeBits stores a uint64 into a bit slice MSB-first. Reverse of
// bitsToUint — used by tests to construct synthetic payloads.
func writeBits(dst []uint8, v uint64) {
	for i := len(dst) - 1; i >= 0; i-- {
		dst[i] = uint8(v & 1)
		v >>= 1
	}
}
