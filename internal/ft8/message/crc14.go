// crc14.go — FT8/FT4 CRC-14 checksum computation.
//
// Reference: WSJT-X source lib/ft8/pack77.f90, ft8_lib src/ft8/crc.c.

package message

import "fmt"

// crc14Poly is the FT8/FT4 CRC-14 generator polynomial (degree 14).
// The implicit leading 1-bit is included, making it a 15-bit value.
// Polynomial: x^14 + x^13 + x^10 + x^9 + x^8 + x^6 + x^4 + x^2 + x + 1
// = 0x6757 (with leading 1) / 0x2757 (without).
const crc14Poly = 0x6757

// CRC14Bits is the number of CRC bits produced.
const CRC14Bits = 14

// MsgBits is the number of payload bits in a packed FT8/FT4 message.
const MsgBits = 77

// MsgBytes is the minimum byte-slice length required to hold MsgBits
// (⌈77/8⌉ = 10). Both CRC14 and Append91 require at least this many bytes.
const MsgBytes = (MsgBits + 7) / 8

// CRC14 computes the 14-bit CRC over a packed 77-bit FT8/FT4 message.
//
// The algorithm follows the WSJT-X reference implementation:
//  1. The 77 message bits are processed MSB-first.
//  2. Five zero bits are appended (3 padding + 2 flush) to push the
//     remainder through the shift register, for a total of 82 input bits.
//  3. The generator polynomial is 0x2757 (degree 14).
//
// The message is represented as a byte slice in big-endian bit order:
// bit 0 (MSB of msg[0]) is the first transmitted bit. Only the first
// 77 bits are used; any trailing bits in msg[9] beyond bit 76 are ignored.
//
// Returns the 14-bit CRC as a uint16.
func CRC14(msg []byte) uint16 {
	if len(msg) < MsgBytes {
		panic(fmt.Sprintf("message: CRC14 requires at least %d bytes, got %d", MsgBytes, len(msg)))
	}

	// Total bits to process: 77 message + 5 zero-pad = 82.
	const totalBits = MsgBits + 5

	// Work in a 16-bit shift register. Only the top 14 bits matter, but
	// using 16 bits simplifies the shift-and-XOR logic.
	var sr uint16

	for i := range totalBits {
		// Extract the next input bit (MSB-first from the byte slice).
		// Bits 77–81 are implicitly zero (the 5 appended zero bits).
		var bit uint16
		if i < MsgBits {
			byteIdx := i / 8
			bitIdx := uint(7 - i%8)
			bit = uint16((msg[byteIdx] >> bitIdx) & 1)
		}
		// else: bit remains 0 for the appended zero bits

		// Shift the register left by 1 and insert the new bit at position 0.
		feedback := (sr >> 13) & 1 // the bit about to be shifted out
		sr = (sr << 1) | bit

		if feedback != 0 {
			sr ^= crc14Poly
		}
	}

	// The CRC is the lower 14 bits of the shift register.
	return sr & 0x3FFF
}

// Append91 takes a 77-bit packed message (10 bytes, only first 77 bits used)
// and returns a new 12-byte array containing the 91-bit payload:
// 77 message bits + 14 CRC bits, suitable for LDPC encoding.
//
// The returned array has bits packed MSB-first. Bit 0 of out[0] is the
// first message bit; bits 77–90 are the CRC (MSB of CRC in bit position 77).
func Append91(msg []byte) [12]byte {
	if len(msg) < MsgBytes {
		panic(fmt.Sprintf("message: Append91 requires at least %d bytes, got %d", MsgBytes, len(msg)))
	}

	crc := CRC14(msg)

	var out [12]byte

	// Copy the 77 message bits (bytes 0–8 fully, byte 9 partially).
	copy(out[:10], msg[:10])

	// Clear any stale bits in byte 9 beyond bit 76.
	// Bit 76 is bit index 76%8 = 4, i.e., position 3 (7-4=3) in byte 9.
	// We keep bits 7..3 (positions 76..72 within the stream) and zero bits 2..0.
	out[9] &= 0xF8

	// Write the 14 CRC bits starting at bit position 77.
	// Bit 77 is byte 9, bit index 77%8 = 5, position 7-5 = 2.
	// So CRC bit 13 (MSB) goes to out[9] bit 2, CRC bit 12 to out[9] bit 1, etc.
	//
	// Layout:
	//   out[9] bits 2..0 → CRC bits 13..11
	//   out[10] bits 7..0 → CRC bits 10..3
	//   out[11] bits 7..5 → CRC bits 2..0
	out[9] |= byte((crc >> 11) & 0x07)
	out[10] = byte((crc >> 3) & 0xFF)
	out[11] = byte((crc & 0x07) << 5)

	return out
}
