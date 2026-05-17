package decoder

import "strconv"

// MessageBits is the number of information bits in an FT8 message.
// Per QEX paper §2: "FT4 and FT8 transmissions always convey
// exactly 77 bits of user information."
const MessageBits = 77

// CRCBits is the FT8 cyclic redundancy check size. Per QEX paper §3:
// "A 14-bit cyclic redundancy check (CRC) is appended to each 77-bit
// information packet to create a 91-bit message-plus-CRC word."
const CRCBits = 14

// CRC14 algorithm dimensions. Named so the relationships between the
// magic numbers in the implementation are self-evident:
//
//   - crc14RegBits = polynomial degree (14) + 1, also = len(crc14Poly),
//     also = the count of initial message bits loaded straight into
//     the shift register during setup.
//   - crc14PaddedBits = message length after zero-padding for
//     polynomial division. Matches `gen_crc14.f90`'s shift-register-
//     alignment convention; bits past MessageBits are zero.
//   - The loop count is then (crc14PaddedBits - crc14RegBits + 1) = 82,
//     and the LSB index in the register is (crc14RegBits - 1) = 14.
const (
	crc14RegBits    = 15
	crc14PaddedBits = 96
)

// crc14Poly is the FT8 CRC generator polynomial 0x6757, expressed
// MSB-first with the leading x^14 coefficient included. Per QEX
// paper §3: "The CRC algorithm uses the polynomial 0x6757
// (hexadecimal)."
//
//	0x6757 = 0b110_0111_0101_0111
//	      = x^14 + x^13 + x^10 + x^9 + x^8 + x^6 + x^4 + x^2 + x + 1
//
// The crc14RegBits-element representation matches the reference
// algorithm in `gen_crc14.f90` from the public-domain QEX reference
// [14] tarball.
var crc14Poly = [crc14RegBits]byte{1, 1, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1}

// CRC14 computes the FT8 14-bit cyclic redundancy check over a
// 77-bit message. The input is one bit per byte (each byte must be
// 0 or 1, MSB-first); the function returns the 14-bit remainder in
// the low 14 bits of the result (bits 13..0).
//
// Algorithm: the 77-bit message is zero-padded to 96 bits internally
// and divided by polynomial 0x6757 using a bit-by-bit MSB-first
// 15-element shift register. This mirrors the public-domain
// `gen_crc14.f90` reference algorithm in QEX paper reference [14].
// QEX paper §3 describes the conceptual computation as the CRC over
// the 77-bit message "zero-extended from 77 to 82 bits"; gen_crc14's
// internal 96-bit padding is a shift-register alignment detail, not
// a different algorithm — both formulations yield the same 14-bit
// remainder. The throughput at FT8 scale (one CRC per decode
// candidate) makes a table-driven or SIMD-accelerated variant
// unnecessary; clarity of the direct algorithm wins.
//
// Panics if msg is not exactly MessageBits (77) elements long, or
// if any byte is not 0 or 1. CRC14 is called from inside the
// decoder and encoder where the 77-bit invariant is already
// guaranteed by upstream packing; a panic here signals a bug at the
// call site, not bad input data.
func CRC14(msg []byte) uint16 {
	if len(msg) != MessageBits {
		panic("decoder.CRC14: message must be exactly 77 bits")
	}
	for i, b := range msg {
		if b > 1 {
			panic("decoder.CRC14: message bit at index " + strconv.Itoa(i) + " is not 0 or 1")
		}
	}

	// Zero-pad — gen_crc14's shift-register-alignment convention.
	// Bits past MessageBits are zero.
	var mc [crc14PaddedBits]byte
	copy(mc[:MessageBits], msg)

	// Shift register initialised with the first crc14RegBits message
	// bits. r[0] is the MSB; r[crc14RegBits-1] is the LSB.
	var r [crc14RegBits]byte
	copy(r[:], mc[:crc14RegBits])

	// Process the padded message. Per the gen_crc14.f90 algorithm:
	// load the next bit into r[crc14RegBits-1], conditionally XOR
	// with the polynomial when r[0]=1, then cyclic-shift left.
	//
	// Iteration 0 redundantly reloads r[crc14RegBits-1] (already
	// there from initialisation) before the first XOR test —
	// preserved here so loop indices match the Fortran reference
	// exactly.
	for i := range crc14PaddedBits - crc14RegBits + 1 {
		r[crc14RegBits-1] = mc[i+crc14RegBits-1]
		if r[0] == 1 {
			for j := range crc14Poly {
				r[j] ^= crc14Poly[j]
			}
		}
		// Cyclic shift left by 1: new r[0] = old r[1], ...,
		// new r[crc14RegBits-2] = old r[crc14RegBits-1],
		// new r[crc14RegBits-1] = old r[0].
		first := r[0]
		for j := range crc14RegBits - 1 {
			r[j] = r[j+1]
		}
		r[crc14RegBits-1] = first
	}

	// Pack r[0..13] into a uint16, MSB-first.
	var crc uint16
	for j := range CRCBits {
		crc = (crc << 1) | uint16(r[j])
	}
	return crc
}
