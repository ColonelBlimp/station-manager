package decoder

// MessageBits is the number of information bits in an FT8 message.
// Per QEX paper §2: "FT4 and FT8 transmissions always convey
// exactly 77 bits of user information."
const MessageBits = 77

// CRCBits is the FT8 cyclic redundancy check size. Per QEX paper §3:
// "A 14-bit cyclic redundancy check (CRC) is appended to each 77-bit
// information packet to create a 91-bit message-plus-CRC word."
const CRCBits = 14

// crc14Poly is the FT8 CRC generator polynomial 0x6757, expressed
// MSB-first with the leading x^14 coefficient included. Per QEX
// paper §3: "The CRC algorithm uses the polynomial 0x6757
// (hexadecimal)."
//
//	0x6757 = 0b110_0111_0101_0111
//	      = x^14 + x^13 + x^10 + x^9 + x^8 + x^6 + x^4 + x^2 + x + 1
//
// The 15-element representation matches the reference algorithm in
// `gen_crc14.f90` from the public-domain QEX reference [14] tarball.
var crc14Poly = [15]byte{1, 1, 0, 0, 1, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1}

// CRC14 computes the FT8 14-bit cyclic redundancy check over a
// 77-bit message. The input is one bit per byte (each byte must be
// 0 or 1, MSB-first); the message is zero-padded internally from
// 77 to 96 bits before division. The return value is the 14-bit
// remainder in the low 14 bits of the result (bits 13..0).
//
// Per QEX paper §3: the CRC uses polynomial 0x6757 with an initial
// value of zero, computed over the 77-bit information packet
// zero-extended to 82 bits. (gen_crc14 in QEX ref [14] pads to 96
// bits internally — the extra 14 zeros at the high-end function as
// the standard CRC division augmentation.)
//
// Algorithm: bit-by-bit MSB-first polynomial division using a
// 15-element shift register. Mirrors the public-domain
// `gen_crc14.f90` reference algorithm. The expected throughput at
// this scale (one CRC per FT8 message) makes a table-driven or
// SIMD-accelerated variant unnecessary; the clarity of the direct
// algorithm beats the speed of an optimised one.
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
			panic("decoder.CRC14: message bit at index " + itoa(i) + " is not 0 or 1")
		}
	}

	// Zero-pad to 96 bits. The extra 19 bits past the 77-bit message
	// (14 standard CRC division augmentation + 5 carry-bits the
	// reference algorithm uses for its 15-element shift register
	// alignment) are zero.
	var mc [96]byte
	copy(mc[:MessageBits], msg)

	// 15-element shift register initialised with the first 15 message
	// bits. r[0] is the MSB; r[14] is the LSB.
	var r [15]byte
	copy(r[:], mc[:15])

	// Process bits 15..95 (82 iterations). Per the gen_crc14.f90
	// algorithm: load the next bit into r[14], conditionally XOR
	// with the polynomial when r[0]=1, then cyclic-shift left.
	//
	// Iteration 0 redundantly reloads r[14]=mc[14] (already there
	// from initialisation) before the first XOR test — preserved
	// here so the loop indices match the Fortran reference exactly.
	for i := 0; i <= 81; i++ {
		r[14] = mc[i+14]
		if r[0] == 1 {
			for j := 0; j < 15; j++ {
				r[j] ^= crc14Poly[j]
			}
		}
		// Cyclic shift left by 1: new r[0] = old r[1], ...,
		// new r[13] = old r[14], new r[14] = old r[0].
		first := r[0]
		for j := 0; j < 14; j++ {
			r[j] = r[j+1]
		}
		r[14] = first
	}

	// Pack r[0..13] into a uint16, MSB-first.
	var crc uint16
	for j := 0; j < CRCBits; j++ {
		crc = (crc << 1) | uint16(r[j])
	}
	return crc
}

// itoa is a tiny stdlib-free int-to-decimal helper for panic
// messages, avoiding an import of strconv in this leaf file.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := i < 0
	if neg {
		i = -i
	}
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
