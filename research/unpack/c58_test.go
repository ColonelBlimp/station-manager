package unpack

import "testing"

// TestDecodeC58_KnownEncoderInputs pins the c58 alphabet-and-base
// mapping against hand-derived encoder values. The Fortran encoder
// (nonstd_to_c58.f90) does:
//
//	n58 = Σ_{i=1..11} index(c, callsign[i]) · 38^(11-i)
//
// with c = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/" (38 chars,
// index 0..37, ' ' = 0). Callsigns are right-padded? Left-padded?
// The encoder reads exactly 11 characters from getarg, which Fortran
// pads with TRAILING spaces by default. So a callsign shorter than
// 11 chars is left-justified with trailing spaces (= alphabet idx 0).
//
// Test vectors below were computed by hand from the encoder formula:
//
//	"PJ4/K1ABC  " (with 2 trailing spaces) →
//	  n58 = idx('P')·38^10 + idx('J')·38^9 + idx('4')·38^8 + idx('/')·38^7
//	      + idx('K')·38^6 + idx('1')·38^5 + idx('A')·38^4 + idx('B')·38^3
//	      + idx('C')·38^2 + 0·38^1 + 0·38^0
//	(idx values: P=25 J=19 4=4 /=37 K=20 1=1 A=10 B=11 C=12)
//
// Decode is the inverse — repeated mod-38 / divide.
func TestDecodeC58_RoundTrip(t *testing.T) {
	// Round-trip via the encoder formula and decoder.
	cases := []struct {
		in        string // padded to 11 chars in the test
		expectDec string // result after TrimSpace
	}{
		{"PJ4/K1ABC  ", "PJ4/K1ABC"},
		{"YW18FIFA   ", "YW18FIFA"},
		{"K1ABC      ", "K1ABC"},
		{"A          ", "A"},
		{"           ", ""}, // all spaces → empty after trim
	}
	for _, c := range cases {
		t.Run(c.expectDec, func(t *testing.T) {
			if len(c.in) != 11 {
				t.Fatalf("test fixture %q is not 11 chars", c.in)
			}
			// Encode via the same formula the Fortran reference uses.
			var n58 uint64
			for i := 0; i < 11; i++ {
				idx := indexInCharset(c.in[i])
				if idx < 0 {
					t.Fatalf("char %q not in c58 charset", c.in[i:i+1])
				}
				n58 = n58*38 + uint64(idx)
			}
			got := decodeC58(n58)
			if got != c.expectDec {
				t.Errorf("decodeC58(%d) = %q, want %q (input padded %q)", n58, got, c.expectDec, c.in)
			}
		})
	}
}

// indexInCharset returns the alphabet index of c in the c58 charset,
// or -1 if c isn't in the charset.
func indexInCharset(c byte) int {
	for i := 0; i < len(c58Charset); i++ {
		if c58Charset[i] == c {
			return i
		}
	}
	return -1
}

// TestUnpackType4_RoundTrip builds a synthetic Type-4 payload and
// verifies Unpack produces the expected text. Covers the three
// display variants: c1=1 (CQ + nonstd), c1=0 h1=0 (hashed first),
// c1=0 h1=1 (hashed second).
func TestUnpackType4_RoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		nonstd string // up to 11 chars
		h12    uint64 // arbitrary
		h1     uint8
		r2     uint8
		c1     uint8
		want   string
	}{
		{"CQ + nonstd", "PJ4/K1ABC", 0xABC, 0, 0, 1, "CQ PJ4/K1ABC"},
		{"hashed first, no report", "PJ4/K1ABC", 0xABC, 0, 0, 0, "<...> PJ4/K1ABC"},
		{"hashed first, RRR", "PJ4/K1ABC", 0xABC, 0, 1, 0, "<...> PJ4/K1ABC RRR"},
		{"hashed second, 73", "PJ4/K1ABC", 0xABC, 1, 3, 0, "PJ4/K1ABC <...> 73"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Encode nonstd → c58.
			padded := c.nonstd + "           "
			padded = padded[:11]
			var n58 uint64
			for i := 0; i < 11; i++ {
				idx := indexInCharset(padded[i])
				if idx < 0 {
					t.Fatalf("char %q not in c58 charset", padded[i:i+1])
				}
				n58 = n58*38 + uint64(idx)
			}

			// Build 77-bit payload: h12(12) c58(58) h1(1) r2(2) c1(1) i3(3).
			var info [91]uint8
			writeBits(info[0:12], c.h12)
			writeBits(info[12:70], n58)
			info[70] = c.h1
			writeBits(info[71:73], uint64(c.r2))
			info[73] = c.c1
			writeBits(info[74:77], 4) // i3 = 4

			got, err := Unpack(info)
			if err != nil {
				t.Fatalf("Unpack returned error: %v", err)
			}
			if got.MsgType != 4 {
				t.Errorf("MsgType = %d, want 4", got.MsgType)
			}
			if got.Text != c.want {
				t.Errorf("Text = %q, want %q", got.Text, c.want)
			}
		})
	}
}
