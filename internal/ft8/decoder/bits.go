package decoder

import "strconv"

// Bit-vector representation conventions used throughout this
// package:
//
//   - **Bit-per-byte** form is a []byte where each element is 0 or 1.
//     Used by algorithm code (CRC14, LDPC, sync) because it matches
//     the spec descriptions (and the QEX reference [14] Fortran
//     programs) one-to-one and stays readable in tests.
//
//   - **Packed** form is a []byte holding bits MSB-first, eight per
//     byte. Used for storage, wire format, and any context where
//     8× memory overhead matters. The bit count is not stored
//     alongside the bytes — callers carry it separately (n=77 for a
//     message, n=91 for message+CRC, n=174 for an LDPC codeword,
//     etc.) and pass it explicitly to Unpack.
//
// Pack and Unpack are the two conversion primitives. Both are
// MSB-first; bit index 0 is the high bit of byte 0, bit index 7 is
// the low bit of byte 0, bit index 8 is the high bit of byte 1, etc.
// This matches the order the QEX paper §3-5 uses when describing
// the 77-bit message and the 174-bit codeword as sequences of bits.

// Pack converts a sequence of one-bit-per-byte values into MSB-first
// packed bytes. The output length is ceil(len(bits)/8); when the bit
// count is not a multiple of 8 the trailing low bits of the last
// output byte are zero.
//
// Panics if any input byte is not 0 or 1 — the bit-per-byte form
// is an internal invariant and a violation indicates a bug at the
// call site, not bad user data.
func Pack(bits []byte) []byte {
	out := make([]byte, (len(bits)+7)/8)
	for i, b := range bits {
		if b > 1 {
			panic("decoder.Pack: bit at index " + strconv.Itoa(i) + " is not 0 or 1")
		}
		if b == 1 {
			out[i/8] |= 1 << (7 - i%8)
		}
	}
	return out
}

// Unpack expands MSB-first packed bytes into one-bit-per-byte form.
// n is the number of bits to extract; the returned slice has length
// exactly n.
//
// Panics if n is negative or exceeds 8*len(packed). The bit-count
// invariant lives at the caller (which knows the protocol-defined
// bit width); passing a wrong n is a call-site bug, not bad data.
func Unpack(packed []byte, n int) []byte {
	if n < 0 {
		panic("decoder.Unpack: negative bit count " + strconv.Itoa(n))
	}
	if n > 8*len(packed) {
		panic("decoder.Unpack: requested " + strconv.Itoa(n) + " bits but only " + strconv.Itoa(8*len(packed)) + " available")
	}
	out := make([]byte, n)
	for i := range n {
		if packed[i/8]&(1<<(7-i%8)) != 0 {
			out[i] = 1
		}
	}
	return out
}
