package codec

import (
	"strconv"
	"strings"
)

// Hash code widths per QEX paper Table 2 (h10, h12, h22 tags). All
// three are derived from the same base-38 polynomial-times-magic-
// prime product; different message types reference different widths
// to fit their bit budgets:
//
//   - h10 (10 bits) goes into compact message types
//   - h12 (12 bits) is the workhorse for "hashed second callsign"
//     references in EU VHF / hashed-call message slots
//   - h22 (22 bits) packs the full hash slot in c28 — biased by
//     +nTokens to land in c28's reserved [nTokens, nTokens+max22)
//     hash-code range (see callsign.go's c28 range layout)
const (
	HashBits10 = 10
	HashBits12 = 12
	HashBits22 = 22

	// hashAlphabet is the 38-char input alphabet for the hash:
	// space + 0-9 + A-Z + '/' (the '/' is what makes hashing useful
	// for compound calls like "PJ4/K1ABC" that don't fit the
	// standard-call alphabet).
	hashAlphabet = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"

	// hashAlphabetSize is len(hashAlphabet). Pulled out as a named
	// constant, so the radix-38 multiplication in HashCodes reads as
	// "multiply by alphabet size."
	hashAlphabetSize = 38

	// hashCallsignLen is the padded callsign length the algorithm
	// operates on. Inputs shorter than this are right-padded with
	// spaces; longer inputs panic.
	hashCallsignLen = 11

	// hashPrime is the multiplicative constant from the public-
	// domain `hashcodes.f90` reference. The hash is computed as
	// the top N bits of (hashPrime * baseN) mod 2^64.
	hashPrime uint64 = 47055833459
)

// HashCodes computes the three FT8 callsign-hash widths (10, 12, 22
// bits) per QEX paper §A and the public-domain `hashcodes.f90`
// reference in QEX ref [14]. All three values derive from a single
// base-38 polynomial-times-prime product, so they're returned
// together — callers extract whichever width their message slot
// uses and ignore the rest.
//
// Algorithm: pad callsign to 11 chars (right-pad with spaces — see
// CallsignC58's docs for the padding-asymmetry note across all the
// string primitives in this package), build a base-38 integer over
// the alphabet (" 0-9A-Z/"), multiply by hashPrime modulo 2^64,
// then right-shift by (64-N) to extract the top N bits for each
// width.
//
// Used by message types that reference a non-standard callsign by
// hash (rather than carrying its full 28- or 58-bit code), e.g.
// "<PJ4/K1ABC> M0XYZ -10" where the angle-bracketed call is sent
// as a hash code and the receiver looks it up in a recently-decoded
// callsign table.
//
// Panics on inputs longer than 11 chars or containing characters
// outside the hash alphabet (lowercase, punctuation other than '/',
// non-ASCII). The hash alphabet IS case-sensitive: callers must
// upper-case before invocation.
//
// Empty input is valid and produces (0, 0, 0) — the all-spaces
// padded vector multiplies to zero.
//
// Caller-responsibility note: trailing whitespace is indistinguishable
// from a shorter callsign, because short inputs pad with spaces and
// space is alphabet index 0. HashCodes("K1JT  ") == HashCodes("K1JT").
// Callers should trim before invoking. (Same shape as the reference's
// adjustl-then-pad semantics.)
func HashCodes(callsign string) (h10, h12, h22 uint32) {
	const op = "codec.HashCodes"
	if len(callsign) > hashCallsignLen {
		panic(op + ": callsign exceeds " + strconv.Itoa(hashCallsignLen) + " chars, got len " + strconv.Itoa(len(callsign)))
	}

	// Pad to 11 chars with trailing spaces (matches the reference's
	// adjustl + character*11 storage).
	var padded [hashCallsignLen]byte
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded[:], callsign)

	// Build the base-38 polynomial value.
	var n8 uint64
	for i := range hashCallsignLen {
		j := strings.IndexByte(hashAlphabet, padded[i])
		if j < 0 {
			panic(op + ": character at index " + strconv.Itoa(i) + " (" + string(padded[i]) + ") not in hash alphabet (space + 0-9 + A-Z + /)")
		}
		n8 = hashAlphabetSize*n8 + uint64(j)
	}

	// Multiply by the magic prime mod 2^64 (uint64 wraps naturally)
	// and extract the top N bits per width.
	prod := hashPrime * n8
	h10 = uint32(prod >> (64 - HashBits10))
	h12 = uint32(prod >> (64 - HashBits12))
	h22 = uint32(prod >> (64 - HashBits22))
	return
}

// HashedCallC28 returns the c28 value used to reference a callsign
// by its 22-bit hash code: h22 + nTokens. This lands the value in
// c28's reserved [nTokens, nTokens+max22) hash range per QEX
// paper Table 7 — the receiver looks at this range to know the
// referenced call is identified by hash, not packed in full.
func HashedCallC28(callsign string) uint32 {
	_, _, h22 := HashCodes(callsign)
	return h22 + nTokens
}
