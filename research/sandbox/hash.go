package sandbox

import (
	"strings"
)

// c58Alphabet is the 38-character base used to encode FT8 nonstandard
// callsigns (and to compute callsign hashes). Per QEX ref [14]
// nonstd_to_c58.f90 and hashcodes.f90:
//
//	" 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"
//
// Index 0 = space (used for right-padding short callsigns to the
// 11-character base-38 polynomial). The trailing "/" is for compound
// calls like "PJ4/K1ABC".
const c58Alphabet = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"

// hashNPrime is the multiplier used by the FT8 callsign hash, per
// QEX ref [14] hashcodes.f90:
//
//	nprime = 47_055_833_459  (≈ 2³⁵.45)
//
// The hash is (nprime × n8) shifted right by (64 − nbits), where
// n8 is the base-38 encoding of the 11-character callsign and nbits
// is the desired hash width (10, 12, or 22). Top bits of the 64-bit
// product become the hash; relies on multiplicative-hash dispersion.
const hashNPrime uint64 = 47_055_833_459

// HashCallsign computes the three FT8 callsign hashes for a given
// callsign string. Sign convention matches ref [14] hashcodes.f90.
//
//   - h10: 10-bit hash (legacy, reserved for future message types)
//   - h12: 12-bit hash (Type 4 messages' addressee field)
//   - h22: 22-bit hash (Type 1 messages' c28 hash-placeholder field)
//
// Returns the three hashes via a single base-38 encoding pass.
//
// Algorithm:
//
//  1. Left-justify (strip leading whitespace, keep content at the
//     start) and right-pad with spaces to 11 characters.
//  2. Compute n8 = Σ char_index[i] × 38^(10−i) — the base-38
//     polynomial of the 11-character string.
//  3. For each hash width w: h_w = (nprime × n8) shifted right by
//     (64 − w). uint64 multiplication wraps on overflow, matching
//     Fortran integer*8 modular arithmetic.
func HashCallsign(callsign string) (h10, h12, h22 uint32) {
	// Left-justify: trim leading whitespace.
	callsign = strings.TrimLeft(callsign, " ")
	if len(callsign) > 11 {
		callsign = callsign[:11]
	}
	for len(callsign) < 11 {
		callsign += " "
	}

	var n8 uint64
	for i := 0; i < 11; i++ {
		idx := strings.IndexByte(c58Alphabet, callsign[i])
		if idx < 0 {
			idx = 0 // unknown character maps to space; defensive
		}
		n8 = n8*38 + uint64(idx)
	}
	product := hashNPrime * n8 // uint64 wraps on overflow
	h10 = uint32(product >> (64 - 10))
	h12 = uint32(product >> (64 - 12))
	h22 = uint32(product >> (64 - 22))
	return
}
