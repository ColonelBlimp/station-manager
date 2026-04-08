package message

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

// --------------- CRC14 -------------------------------------------------------

func TestCRC14_AllZeros(t *testing.T) {
	// 77 zero bits → known CRC value.
	msg := make([]byte, 10)
	crc := CRC14(msg)
	// With all-zero input the shift register never gets feedback,
	// so the CRC must be 0.
	require.Equal(t, uint16(0), crc, "CRC of all-zero 77-bit message should be 0")
}

func TestCRC14_SingleBitSet(t *testing.T) {
	// Setting only bit 0 (MSB of byte 0) should produce a non-zero CRC.
	msg := make([]byte, 10)
	msg[0] = 0x80 // bit 0 set
	crc := CRC14(msg)
	require.NotEqual(t, uint16(0), crc, "CRC with bit 0 set must be non-zero")
}

func TestCRC14_DifferentBitsProduceDifferentCRCs(t *testing.T) {
	// Two messages differing in a single bit must produce different CRCs
	// (a fundamental property of CRC codes within their Hamming distance).
	msg1 := make([]byte, 10)
	msg1[0] = 0x80

	msg2 := make([]byte, 10)
	msg2[0] = 0x40

	crc1 := CRC14(msg1)
	crc2 := CRC14(msg2)
	require.NotEqual(t, crc1, crc2, "different single-bit messages must have different CRCs")
}

func TestCRC14_MaxValue(t *testing.T) {
	// CRC must fit in 14 bits regardless of input.
	msg := make([]byte, 10)
	for i := range msg {
		msg[i] = 0xFF
	}
	crc := CRC14(msg)
	require.LessOrEqual(t, crc, uint16(0x3FFF), "CRC must be at most 14 bits")
}

func TestCRC14_AllOnes77(t *testing.T) {
	// 77 one-bits. Byte 9 has bits 7..3 set (0xF8) and bits 2..0 clear.
	msg := make([]byte, 10)
	for i := 0; i < 9; i++ {
		msg[i] = 0xFF
	}
	msg[9] = 0xF8 // bits 72–76 set, 77–79 clear
	crc := CRC14(msg)
	require.NotEqual(t, uint16(0), crc, "CRC of all-ones should be non-zero")
}

func TestCRC14_IgnoresTrailingBits(t *testing.T) {
	// Bits 77–79 in byte 9 should be ignored by CRC14.
	msg1 := make([]byte, 10)
	msg1[5] = 0xAB // some data

	msg2 := make([]byte, 10)
	copy(msg2, msg1)
	msg2[9] = 0x07 // set the 3 trailing bits that are beyond bit 76

	require.Equal(t, CRC14(msg1), CRC14(msg2),
		"trailing bits beyond position 76 must not affect CRC")
}

func TestCRC14_Deterministic(t *testing.T) {
	msg := make([]byte, 10)
	msg[0] = 0xDE
	msg[1] = 0xAD
	msg[2] = 0xBE
	msg[3] = 0xEF

	crc1 := CRC14(msg)
	crc2 := CRC14(msg)
	require.Equal(t, crc1, crc2, "CRC must be deterministic")
}

// TestCRC14_KnownVector verifies against the ft8_lib reference implementation.
//
// Input: 77-bit message where only bit 76 (the last message bit) is set.
// Bit 76 is in byte 9 at position 7 - (76 % 8) = 7 - 4 = 3, i.e., byte 9 = 0x08.
//
// Reference value 0x3874 was computed using ft8_lib's ftx_compute_crc()
// with the same input and 82 processing bits (77 msg + 5 zero-pad).
func TestCRC14_KnownVector_LastBitSet(t *testing.T) {
	msg := make([]byte, 10)
	msg[9] = 0x08 // bit 76 set
	crc := CRC14(msg)
	require.Equal(t, uint16(0x3874), crc)
}

// TestCRC14_KnownVector_FirstBitSet verifies CRC with only bit 0 set.
//
// Bit 0 enters the shift register at step 0 and is then shifted through
// 81 more steps with XOR feedback whenever the top bit is set.
// This exercises the full polynomial feedback chain.
//
// Reference value 0x2BF8 was computed using ft8_lib's ftx_compute_crc().
func TestCRC14_KnownVector_FirstBitSet(t *testing.T) {
	msg := make([]byte, 10)
	msg[0] = 0x80 // bit 0 set

	crc := CRC14(msg)
	require.Equal(t, uint16(0x2BF8), crc)
}

// --------------- Append91 ----------------------------------------------------

func TestAppend91_PreservesMessage(t *testing.T) {
	msg := make([]byte, 10)
	msg[0] = 0xAB
	msg[1] = 0xCD
	msg[4] = 0x42

	out := Append91(msg)

	// Bytes 0–8 must be identical.
	for i := 0; i < 9; i++ {
		require.Equal(t, msg[i], out[i], "byte %d mismatch", i)
	}
	// Byte 9: upper 5 bits (message bits 72–76) preserved, lower 3 bits are CRC.
	require.Equal(t, msg[9]&0xF8, out[9]&0xF8, "byte 9 upper 5 bits mismatch")
}

func TestAppend91_CRCEmbedded(t *testing.T) {
	msg := make([]byte, 10)
	msg[0] = 0xDE
	msg[1] = 0xAD

	crc := CRC14(msg)
	out := Append91(msg)

	// Extract the 14 CRC bits from positions 77–90.
	// out[9] bits 2..0 → CRC bits 13..11
	// out[10] bits 7..0 → CRC bits 10..3
	// out[11] bits 7..5 → CRC bits 2..0
	extracted := uint16(out[9]&0x07) << 11
	extracted |= uint16(out[10]) << 3
	extracted |= uint16(out[11]>>5) & 0x07

	require.Equal(t, crc, extracted, "embedded CRC must match CRC14()")
}

func TestAppend91_AllZeros(t *testing.T) {
	msg := make([]byte, 10)
	out := Append91(msg)

	// CRC of all-zeros is 0, so the entire output should be zero.
	for i, b := range out {
		require.Equal(t, byte(0), b, "byte %d should be 0 for all-zero input", i)
	}
}

func TestAppend91_ClearsTrailingBits(t *testing.T) {
	// If the caller has stale bits in byte 9 positions 2..0 (beyond bit 76),
	// Append91 must clear them before inserting CRC bits.
	msg := make([]byte, 10)
	msg[9] = 0x07 // stale trailing bits

	out := Append91(msg)
	crc := CRC14(msg) // CRC ignores trailing bits → same as all-zeros → 0

	require.Equal(t, uint16(0), crc)
	// With CRC=0, byte 9 lower 3 bits should be 0 (stale bits cleared).
	require.Equal(t, byte(0), out[9]&0x07, "stale trailing bits must be cleared")
}

func TestAppend91_RoundTrip(t *testing.T) {
	// Verify we can extract the CRC from an Append91 result and it matches.
	msg := make([]byte, 10)
	msg[0] = 0xFF
	msg[3] = 0x55
	msg[7] = 0xAA
	msg[9] = 0xF8 // bits 72–76 set

	out := Append91(msg)

	// Re-extract the message (first 77 bits) and recompute CRC.
	var reMsg [10]byte
	copy(reMsg[:], out[:10])
	reMsg[9] &= 0xF8 // mask off CRC bits in byte 9

	reCRC := CRC14(reMsg[:])

	// Extract CRC from out.
	embedded := uint16(out[9]&0x07) << 11
	embedded |= uint16(out[10]) << 3
	embedded |= uint16(out[11]>>5) & 0x07

	require.Equal(t, reCRC, embedded, "round-trip CRC must match")
}

// --------------- Input length guards -----------------------------------------

func TestCRC14_PanicsOnShortInput(t *testing.T) {
	for _, n := range []int{0, 1, 5, 9} {
		t.Run(fmt.Sprintf("len=%d", n), func(t *testing.T) {
			require.PanicsWithValue(t,
				fmt.Sprintf("message: CRC14 requires at least %d bytes, got %d", MsgBytes, n),
				func() { CRC14(make([]byte, n)) },
			)
		})
	}
}

func TestAppend91_PanicsOnShortInput(t *testing.T) {
	for _, n := range []int{0, 1, 5, 9} {
		t.Run(fmt.Sprintf("len=%d", n), func(t *testing.T) {
			require.PanicsWithValue(t,
				fmt.Sprintf("message: Append91 requires at least %d bytes, got %d", MsgBytes, n),
				func() { Append91(make([]byte, n)) },
			)
		})
	}
}
