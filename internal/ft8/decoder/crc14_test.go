package decoder

import (
	"testing"
)

// CRC14 test vectors generated 2026-05-17 using the public-domain
// `gen_crc14` reference program from the QEX paper's reference [14]
// tarball (built locally from `gen_crc14.f90`). These are the M4.1
// Layer 1 spec-vector tests: every input below is a 77-bit message
// represented as a string of '0' and '1' characters (matching
// gen_crc14's CLI input format); every expected_crc was produced by
// running `gen_crc14 "<bits>"` and reading the "crc14 as an integer"
// line.
//
// Per QEX paper §9, reference [14] is in the public domain. The
// vectors below are facts derived from running a public-domain
// program against chosen inputs — facts aren't copyrightable, and
// no expression is copied from the reference into this test file.
var crc14Vectors = []struct {
	name     string
	bitsStr  string
	expected uint16
}{
	{
		name:     "all_zeros",
		bitsStr:  "00000000000000000000000000000000000000000000000000000000000000000000000000000",
		expected: 0,
	},
	{
		name:     "all_ones",
		bitsStr:  "11111111111111111111111111111111111111111111111111111111111111111111111111111",
		expected: 1969,
	},
	{
		name:     "single_msb",
		bitsStr:  "10000000000000000000000000000000000000000000000000000000000000000000000000000",
		expected: 11256,
	},
	{
		name:     "single_lsb",
		bitsStr:  "00000000000000000000000000000000000000000000000000000000000000000000000000001",
		expected: 14452,
	},
	{
		name:     "alternating_bits",
		bitsStr:  "10101010101010101010101010101010101010101010101010101010101010101010101010101",
		expected: 4850,
	},
	{
		name:     "qex_help_example",
		bitsStr:  "00000000000000000000000000100000010011011111110011011100100010100001010000001",
		expected: 5497,
	},
	{
		name:     "first14_set",
		bitsStr:  "11111111111111000000000000000000000000000000000000000000000000000000000000000",
		expected: 6401,
	},
}

func TestCRC14_SpecVectors(t *testing.T) {
	for _, tc := range crc14Vectors {
		t.Run(tc.name, func(t *testing.T) {
			bits := parseBitString(t, tc.bitsStr)
			got := CRC14(bits)
			if got != tc.expected {
				t.Errorf("CRC14(%s): got %d (0x%04x), want %d (0x%04x)",
					tc.name, got, got, tc.expected, tc.expected)
			}
		})
	}
}

func TestCRC14_OutputIs14Bits(t *testing.T) {
	// Every CRC14 result must fit in 14 bits regardless of input.
	// Belt-and-braces invariant — catches an off-by-one in the
	// final pack-into-uint16 that no specific vector might trigger.
	for _, tc := range crc14Vectors {
		bits := parseBitString(t, tc.bitsStr)
		got := CRC14(bits)
		if got >= (1 << 14) {
			t.Errorf("%s: CRC14 result %d (0x%04x) exceeds 14 bits", tc.name, got, got)
		}
	}
}

func TestCRC14_RejectsWrongLength(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CRC14(short input) should panic; did not")
		}
	}()
	CRC14(make([]byte, 76))
}

func TestCRC14_RejectsInvalidBits(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("CRC14(byte > 1) should panic; did not")
		}
	}()
	bits := make([]byte, MessageBits)
	bits[42] = 2
	CRC14(bits)
}

// parseBitString turns a 77-character string of '0'/'1' into the
// bit-per-byte form CRC14 expects. Fails the test on bad input.
func parseBitString(t *testing.T, s string) []byte {
	t.Helper()
	if len(s) != MessageBits {
		t.Fatalf("test bit-string must be %d chars; got %d", MessageBits, len(s))
	}
	out := make([]byte, MessageBits)
	for i, c := range s {
		switch c {
		case '0':
			out[i] = 0
		case '1':
			out[i] = 1
		default:
			t.Fatalf("test bit-string char %d (%q) is not '0' or '1'", i, c)
		}
	}
	return out
}
