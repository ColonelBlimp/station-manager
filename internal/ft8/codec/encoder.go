// encoder.go — LDPC(174,91) systematic encoder for FT8/FT4.
//
// The encoder takes a 91-bit information payload (77 message + 14 CRC,
// as produced by message.Append91) and produces a 174-bit LDPC codeword
// using the generator matrix G.
//
// The output is systematic: bits 0–90 are the information bits, and bits
// 91–173 are the 83 parity bits.
//
// Reference: ft8_lib encode.c, WSJT-X LDPC_174_91_3_generator.f90.

package codec

import "math/bits"

// Encode performs systematic LDPC encoding of a 91-bit information payload.
//
// info contains the 91-bit payload packed MSB-first into 12 bytes (as returned
// by [message.Append91]). Any stale bits beyond bit 90 (the 5 low-order bits
// of info[11]) are cleared before encoding.
//
// Returns the 174-bit codeword packed MSB-first into 22 bytes. Bits 0–90 are
// the information bits (copied verbatim), bits 91–173 are the 83 parity bits.
// The 2 unused low-order bits of the 22nd byte are always zero.
func Encode(info [KBytes]byte) [NBytes]byte {
	// Mask off the 5 unused low-order bits of byte 11 so that stale bits
	// from the caller cannot leak into the codeword or parity computation.
	// Bit 90 is at byte 11, bit position 7−(90%8) = 5, so bits 4..0 are
	// outside the 91-bit payload.
	info[11] &= 0xE0

	var codeword [NBytes]byte

	// Step 1: Copy the 91 information bits into codeword positions 0–90.
	// This is a byte-level copy of the first 11 full bytes plus the
	// partial 12th byte (bits 88–90).
	copy(codeword[:KBytes], info[:])

	// Step 2: Compute each of the 83 parity bits via the generator matrix G
	// and write it into the codeword. For parity bit p, compute the GF(2)
	// dot product of generator row G[p] with the 91-bit information vector:
	//   parity[p] = popcount(G[p] AND info) mod 2
	for p := range M {
		var acc uint8
		for b := range KBytes {
			acc ^= G[p][b] & info[b]
		}
		// The parity bit is 1 if the total number of set bits is odd.
		parityBit := uint8(bits.OnesCount8(acc) % 2)

		// Write parity bit p into codeword position K+p (91+p).
		bitPos := K + p        // absolute bit position in the codeword
		byteIdx := bitPos / 8  // which byte
		bitIdx := 7 - bitPos%8 // MSB-first bit position within the byte
		codeword[byteIdx] |= parityBit << uint(bitIdx)
	}

	return codeword
}
