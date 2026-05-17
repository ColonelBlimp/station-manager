package decoder

import (
	"strconv"
	"strings"
)

// CallsignBits is the width of the standard-callsign code per
// QEX paper §A: "Standard amateur call signs can be conveyed in
// 28 bits".
const CallsignBits = 28

// Per-position alphabets used by the standard-callsign encoding.
// The 6-character right-justified callsign uses a different alphabet
// at each position:
//
//	pos 1: space + digit + letter  (37 chars)  → first prefix char or space
//	pos 2: digit + letter          (36 chars)  → second prefix char (or first if 1-char prefix)
//	pos 3: digit                   (10 chars)  → the required digit
//	pos 4..6: space + letter       (27 chars)  → suffix chars or padding
//
// Per QEX paper §A: "a standard amateur call sign consists of a one-
// or two-character prefix, at least one of which must be a letter,
// followed by a decimal digit and a suffix of up to three letters."
// The alphabets here are the algorithmic embodiment of that format.
const (
	callsignAlphaPos1 = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ" // 37 chars
	callsignAlphaPos2 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"  // 36 chars
	callsignAlphaPos3 = "0123456789"                            // 10 chars
	callsignAlphaPos4 = " ABCDEFGHIJKLMNOPQRSTUVWXYZ"           // 27 chars (used at positions 4, 5, 6)
)

// c28 nominal-range boundaries per QEX paper Table 7. The 28-bit
// callsign code space is conceptually partitioned:
//
//	[0, nTokens)                  → reserved for CQ / DE / QRZ +
//	                                "CQ <suffix>" message words
//	[nTokens, nTokens+max22)      → 22-bit hashes of non-standard calls
//	[nTokens+max22, 2^28)         → standard amateur callsigns
//
// CallsignC28 emits values nominally from the third range, but
// for short callsigns (3..4 chars) the algorithm's negative-index
// handling can produce values that land INSIDE the 22-bit hash
// range. That's not a collision: the FT8 protocol disambiguates
// std-calls from hash codes via the message-type tag (i3/n3 bits)
// in the surrounding 77-bit message, not by c28 range. The hash
// encoder and the special-token encoder land in subsequent commits.
const (
	nTokens        = 2063592 // QEX Table 7: pre-callsign-space tokens
	max22          = 1 << 22 // QEX Table 7: 22-bit hash range size (4194304)
	stdCallOffset  = nTokens + max22
	stdCallAlphaSz = 27 // a4 size; appears in the weights for positions 3..6
)

// CallsignC28 packs a standard amateur callsign into its 28-bit
// code per QEX paper §A and the public-domain `std_call_to_c28.f90`
// reference in QEX ref [14]. Returns the c28 value in the low 28
// bits of the result.
//
// The input is right-justified to 6 characters internally (so 3..6
// char callsigns are all handled). Per-position alphabet validation
// is NOT performed: the reference algorithm intentionally allows
// out-of-position-alphabet characters to produce negative indices,
// and the c28 arithmetic absorbs them — that's how short callsigns
// encode their length implicitly via the indexing scheme.
//
// Validation that DOES happen:
//
//   - Length must be 1..6 (zero-length panics, longer would silently
//     truncate). Empty input is a bug at the call site.
//   - Every character must appear in the broadest alphabet
//     (callsignAlphaPos1 = space + 0-9 + A-Z). Characters outside
//     it (lowercase letters, punctuation, non-ASCII) signal a bug
//     at the call site — the caller should have validated callsign
//     format upstream.
//
// What the caller must do upstream:
//
//   - Verify the input matches the standard-callsign format
//     ([A-Z0-9]{1,2} + [0-9] + [A-Z]{1,3}, at least one letter in
//     the prefix). Calls that don't match the format go through the
//     nonstandard c58 encoder (`nonstd_to_c58.f90` analogue, to be
//     implemented), not this function.
//   - Convert to uppercase. This function is case-sensitive; "g4abc"
//     panics, "G4ABC" works.
//
// Returns uint32 even though the result fits in 28 bits — uint16
// is too small (28 bits don't fit), and the 4 high bits will be
// zero for any in-range input.
func CallsignC28(call string) uint32 {
	if len(call) == 0 || len(call) > 6 {
		panic("decoder.CallsignC28: callsign length must be 1..6, got len " + strconv.Itoa(len(call)))
	}
	for i := range len(call) {
		if strings.IndexByte(callsignAlphaPos1, call[i]) < 0 {
			panic("decoder.CallsignC28: character at index " + strconv.Itoa(i) + " (" + string(call[i]) + ") not in std-call alphabet (space + 0-9 + A-Z)")
		}
	}

	// Right-justify to 6 chars with leading spaces. The reference
	// uses Fortran's adjustr; this is the same shape.
	var padded [6]byte
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded[6-len(call):], call)

	// Per-position lookups. Out-of-position-alphabet characters
	// return -1; that's intentional per the reference (see doc above).
	i1 := strings.IndexByte(callsignAlphaPos1, padded[0])
	i2 := strings.IndexByte(callsignAlphaPos2, padded[1])
	i3 := strings.IndexByte(callsignAlphaPos3, padded[2])
	i4 := strings.IndexByte(callsignAlphaPos4, padded[3])
	i5 := strings.IndexByte(callsignAlphaPos4, padded[4])
	i6 := strings.IndexByte(callsignAlphaPos4, padded[5])

	// Compute c28 in signed int — i2..i4 may be -1 by design.
	// The arithmetic absorbs the negatives; the final value always
	// lands in [0, 2^28) for any input that passed the alpha-pos1
	// validation above.
	//
	// mN is the weight applied to position N's index — i.e. the
	// product of the alphabet sizes of all positions *after* N.
	// Named with matching indices so the expression below reads
	// "weight for position N times index for position N" without
	// off-by-one mental gymnastics.
	const (
		m1 = 36 * 10 * stdCallAlphaSz * stdCallAlphaSz * stdCallAlphaSz
		m2 = 10 * stdCallAlphaSz * stdCallAlphaSz * stdCallAlphaSz
		m3 = stdCallAlphaSz * stdCallAlphaSz * stdCallAlphaSz
		m4 = stdCallAlphaSz * stdCallAlphaSz
		m5 = stdCallAlphaSz
		m6 = 1
	)
	n28 := stdCallOffset +
		m1*i1 +
		m2*i2 +
		m3*i3 +
		m4*i4 +
		m5*i5 +
		m6*i6

	// Belt-and-braces output-range guard. The documented invariant
	// is n28 in [0, 2^28); a regression that produced a negative or
	// out-of-range value would silently wrap through uint32(n28) and
	// corrupt downstream message packing, which is exactly the class
	// of bug that's a nightmare to chase in FT8 decode output. One
	// int compare per call is negligible vs. catching a corruption
	// at the boundary.
	if n28 < 0 || n28 >= 1<<CallsignBits {
		panic("decoder.CallsignC28: internal arithmetic regression, n28=" + strconv.Itoa(n28) + " out of [0, 2^28)")
	}
	return uint32(n28)
}
