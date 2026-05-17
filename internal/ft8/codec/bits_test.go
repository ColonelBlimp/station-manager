package codec

import (
	"bytes"
	"testing"
)

func TestPack_EmptyInputProducesEmptyOutput(t *testing.T) {
	got := Pack([]byte{})
	if len(got) != 0 {
		t.Errorf("Pack([]): got %v, want empty", got)
	}
}

func TestPack_KnownVectors(t *testing.T) {
	cases := []struct {
		name string
		bits []byte
		want []byte
	}{
		{"all_zero_byte", bitsFrom("00000000"), []byte{0x00}},
		{"all_one_byte", bitsFrom("11111111"), []byte{0xFF}},
		{"msb_only", bitsFrom("10000000"), []byte{0x80}},
		{"lsb_only", bitsFrom("00000001"), []byte{0x01}},
		{"alternating_starting_high", bitsFrom("10101010"), []byte{0xAA}},
		{"alternating_starting_low", bitsFrom("01010101"), []byte{0x55}},
		{
			// 9 bits → 2 bytes; trailing 7 low bits of byte 1 are zero
			name: "nine_bits_msb_set",
			bits: bitsFrom("100000000"),
			want: []byte{0x80, 0x00},
		},
		{
			// 9 bits where only the 9th is set: byte 0 = 0x00,
			// byte 1's MSB carries the 9th bit → 0x80
			name: "ninth_bit_set",
			bits: bitsFrom("000000001"),
			want: []byte{0x00, 0x80},
		},
		{
			// 77 bits — the FT8 message size; verifies the realistic
			// case CRC14's callers will be packing
			name: "ft8_message_size",
			bits: append(append(append([]byte{1}, zeros(7)...), zeros(68)...), 1),
			// bit 0 set (0x80 in byte 0), bit 76 set (0x08 in byte 9 — bit 76 mod 8 = 4, so position 7-4=3 from MSB)
			want: []byte{0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0x08},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Pack(tc.bits)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Pack(%v):\n  got  % 02x\n  want % 02x", tc.bits, got, tc.want)
			}
		})
	}
}

func TestPack_RejectsInvalidBit(t *testing.T) {
	// Cover both the smallest invalid byte (2) and the maximum
	// (255). byte is unsigned, so the b > 1 check is the only line
	// between valid bits and arbitrary garbage; pin both ends.
	cases := []struct {
		name  string
		input []byte
	}{
		{"byte_value_two", []byte{0, 0, 1, 2, 0}},
		{"byte_value_max", []byte{0, 0, 1, 255, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Error("Pack should panic on non-binary input; did not")
				}
			}()
			Pack(tc.input)
		})
	}
}

func TestUnpack_EmptyInputZeroBits(t *testing.T) {
	got := Unpack([]byte{}, 0)
	if len(got) != 0 {
		t.Errorf("Unpack([], 0): got %v, want empty", got)
	}
}

func TestUnpack_KnownVectors(t *testing.T) {
	cases := []struct {
		name   string
		packed []byte
		n      int
		want   []byte
	}{
		{"all_zero_byte", []byte{0x00}, 8, bitsFrom("00000000")},
		{"all_one_byte", []byte{0xFF}, 8, bitsFrom("11111111")},
		{"msb_only", []byte{0x80}, 8, bitsFrom("10000000")},
		{"lsb_only", []byte{0x01}, 8, bitsFrom("00000001")},
		{"alternating_high", []byte{0xAA}, 8, bitsFrom("10101010")},
		{"alternating_low", []byte{0x55}, 8, bitsFrom("01010101")},
		{
			// n=9 across 2 bytes; second byte's MSB is the 9th bit
			name:   "nine_bits",
			packed: []byte{0xFF, 0x80},
			n:      9,
			want:   bitsFrom("111111111"),
		},
		{
			// n=7 from a single byte truncates the last bit
			name:   "seven_bits_truncate_trailing",
			packed: []byte{0xFF},
			n:      7,
			want:   bitsFrom("1111111"),
		},
		{
			// n=77 — the FT8 message size, recovered from 10 packed bytes
			name:   "ft8_message_size",
			packed: []byte{0x80, 0, 0, 0, 0, 0, 0, 0, 0, 0x08},
			n:      77,
			want:   append(append(append([]byte{1}, zeros(7)...), zeros(68)...), 1),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Unpack(tc.packed, tc.n)
			if !bytes.Equal(got, tc.want) {
				t.Errorf("Unpack(% 02x, %d):\n  got  %v\n  want %v", tc.packed, tc.n, got, tc.want)
			}
		})
	}
}

func TestUnpack_RejectsNegativeN(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Unpack(_, -1) should panic; did not")
		}
	}()
	Unpack([]byte{0xFF}, -1)
}

func TestUnpack_RejectsOversizedN(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("Unpack with n > 8*len(packed) should panic; did not")
		}
	}()
	Unpack([]byte{0xFF}, 9)
}

// TestPackUnpack_RoundTrip is the property test that catches most
// bugs in either direction: for any bit-per-byte input, packing then
// unpacking yields the original.
func TestPackUnpack_RoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		{0},
		{1},
		bitsFrom("10101010"),
		bitsFrom("11111111"),
		bitsFrom("00000000"),
		bitsFrom("100000000"), // 9 bits (1 high + 8 zeros)
		bitsFrom("11111111111111111111111111111111111111111111111111111111111111111111111111111"), // 77 ones
		bitsFrom("00000000000000000000000000100000010011011111110011011100100010100001010000001"), // QEX example
	}
	for i, in := range cases {
		got := Unpack(Pack(in), len(in))
		if !bytes.Equal(got, in) {
			t.Errorf("case %d (len=%d) round-trip mismatch:\n  in   %v\n  got  %v", i, len(in), in, got)
		}
	}
}

// TestUnpackPack_RoundTrip is the other direction: unpacking and
// re-packing recovers the original bytes, provided n is a multiple
// of 8 (otherwise the trailing low bits of the last byte are lost
// to zero-padding, which is expected behaviour, not a bug).
func TestUnpackPack_RoundTrip(t *testing.T) {
	cases := [][]byte{
		{},
		{0x00},
		{0xFF},
		{0x80, 0x40, 0x20, 0x10, 0x08, 0x04, 0x02, 0x01},
		{0xDE, 0xAD, 0xBE, 0xEF},
	}
	for i, in := range cases {
		got := Pack(Unpack(in, 8*len(in)))
		if !bytes.Equal(got, in) {
			t.Errorf("case %d (len=%d) round-trip mismatch:\n  in   % 02x\n  got  % 02x", i, len(in), in, got)
		}
	}
}

// bits is a test-only helper that turns a string of '0'/'1' chars
// into bit-per-byte form. Mirrors parseBitString in crc14_test.go
// but lives here too so this file is self-contained (tests in the
// same package share helpers but having the helper next to its
// callers reads better).
func bitsFrom(s string) []byte {
	out := make([]byte, len(s))
	for i, c := range s {
		switch c {
		case '0':
			out[i] = 0
		case '1':
			out[i] = 1
		default:
			panic("bits: non-binary character " + string(c))
		}
	}
	return out
}

func zeros(n int) []byte {
	return make([]byte, n)
}

// FuzzPackUnpackRoundTrip cheaply exercises the round-trip property
// (Unpack(Pack(b), len(b)) == b) across arbitrary bit-vector shapes
// including partial-byte boundaries — exactly the cases hand-written
// vectors are most likely to miss.
//
// Run locally with: go test -fuzz=FuzzPackUnpackRoundTrip -fuzztime=10s ./internal/ft8/codec/
func FuzzPackUnpackRoundTrip(f *testing.F) {
	seeds := [][]byte{
		{},
		{0},
		{1},
		{1, 0, 1, 0, 1, 0, 1, 0},    // exactly 8 bits
		{1, 0, 1, 0, 1, 0, 1, 0, 1}, // 9 bits — crosses byte boundary
		make([]byte, 77),            // FT8 message size
		bitsFrom("00000000000000000000000000100000010011011111110011011100100010100001010000001"),
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Coerce arbitrary bytes into bit-per-byte form (each byte
		// 0 or 1). This is a property test of Pack/Unpack's
		// invariant, not of input validation, so don't waste fuzz
		// cycles tripping the panic check.
		in := make([]byte, len(data))
		for i, b := range data {
			in[i] = b & 1
		}
		out := Unpack(Pack(in), len(in))
		if !bytes.Equal(out, in) {
			t.Errorf("round-trip mismatch:\n  in  %v\n  out %v", in, out)
		}
	})
}
