package codec

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
// Right-justifies with leading spaces (matches Fortran's adjustr).
// See CallsignC58's padding-asymmetry doc block for the comparative
// layout across all four codec string primitives.
//
// Validation that DOES happen:
//
//   - Length must be 3..6 (the FT8 standard-callsign length range).
//     Real std calls are prefix(1-2 chars) + digit + suffix(1-3 chars)
//     = 3-6 chars total. Inputs shorter than 3 produce arithmetic
//     values that don't correspond to any real callsign and are
//     almost certainly call-site bugs; rejecting them noisily is
//     better than silently producing meaningless c28 values.
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
//     the prefix). Calls that don't match the format go through
//     CallsignC58 instead.
//   - Convert to uppercase. This function is case-sensitive; "g4abc"
//     panics, "G4ABC" works.
//   - Strip trailing whitespace. Leading spaces are absorbed by the
//     right-justify pad (they're indistinguishable from omission),
//     but a trailing space is content — `CallsignC28("K1JT ")`
//     produces a different c28 than `CallsignC28("K1JT")` because
//     the trailing space shifts the call one position left in the
//     padded layout.
//
// Returns uint32 even though the result fits in 28 bits — uint16
// is too small (28 bits don't fit), and the 4 high bits will be
// zero for any in-range input.
func CallsignC28(call string) uint32 {
	const op = "codec.CallsignC28"
	if len(call) < 3 || len(call) > 6 {
		panic(op + ": callsign length must be 3..6 (real FT8 std calls are prefix+digit+suffix), got len " + strconv.Itoa(len(call)))
	}
	for i := range len(call) {
		if strings.IndexByte(callsignAlphaPos1, call[i]) < 0 {
			panic(op + ": character at index " + strconv.Itoa(i) + " (" + string(call[i]) + ") not in std-call alphabet (space + 0-9 + A-Z)")
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
	// off-by-one mental gymnastics. Using len(alphabet) rather than
	// literal sizes couples the weights to the alphabets at compile
	// time — an alphabet edit that wasn't reflected here would still
	// compile but produce wrong c28 values; this way it can't drift.
	const (
		m1 = len(callsignAlphaPos2) * len(callsignAlphaPos3) * stdCallAlphaSz * stdCallAlphaSz * stdCallAlphaSz
		m2 = len(callsignAlphaPos3) * stdCallAlphaSz * stdCallAlphaSz * stdCallAlphaSz
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
		panic(op + ": internal arithmetic regression, n28=" + strconv.Itoa(n28) + " out of [0, 2^28)")
	}
	return uint32(n28)
}

// C28Kind discriminates which partition of the c28 value space a
// decoded c28 falls into per QEX paper Table 7. C28ToCallsign
// returns the Kind alongside the recovered string so callers can
// dispatch on it.
type C28Kind int

const (
	// C28KindUnknown is the zero value. C28ToCallsign never returns
	// it on real input; useful as a default for code holding a Kind
	// variable before assignment.
	C28KindUnknown C28Kind = iota

	// C28KindToken indicates c28 ∈ [0, nTokens) — a special message
	// token (CQ, DE, QRZ, or "CQ <suffix>"). C28ToCallsign currently
	// returns "" for this kind; token-name decoding lands alongside
	// ParseMessage in Phase 2D.
	C28KindToken

	// C28KindHash22 indicates c28 ∈ [nTokens, stdCallOffset) — a
	// 22-bit callsign hash. C28ToCallsign returns ""; original-call
	// recovery requires a running hash table populated from prior
	// decodes (the FT8 service layer's responsibility, not the
	// codec's). Per QEX §A, short std calls (3-4 char and 5-char-2-
	// prefix calls) also produce values in this range via
	// HashedCallC28 — their recovery uses the same hash table.
	C28KindHash22

	// C28KindStdCall indicates c28 ∈ [stdCallOffset, 2^28) — a
	// standard amateur callsign produced via CallsignC28 with all
	// non-negative per-position indices. Only "long-format" std
	// calls (5-char-1-prefix and 6-char-2-prefix) land here cleanly;
	// other shapes produce hash-range values and must go through
	// HashedCallC28. C28ToCallsign returns the recovered callsign.
	C28KindStdCall
)

// C28ToCallsign decodes a c28 value back to its string form by
// inverting CallsignC28. The C28Kind discriminator tells the caller
// which partition of the c28 value space the input occupied per
// QEX paper Table 7. Only C28KindStdCall yields a non-empty
// recovered string from this function alone — C28KindToken and
// C28KindHash22 callers need additional state (token table or
// 22-bit hash table) to recover the original message fragment.
//
// For C28KindStdCall, the inverse is a straightforward mixed-base
// divmod against the per-position alphabet sizes. The std-call
// range [stdCallOffset, 2^28) guarantees all forward indices were
// non-negative (the negative-index cases that arise from short-call
// right-padding produce values below stdCallOffset, in the hash
// range), so no shift is needed and each extracted index lands
// directly in [0, posSize) for the corresponding alphabet lookup.
//
// The returned callsign is the 6-char right-padded form with
// leading spaces stripped. For tuples that don't correspond to a
// real callsign shape (e.g. wire bits decoded from a corrupted
// message), the string is the arithmetic inverse of the bits and
// may look like a non-callsign — the codec doesn't classify shapes.
//
// Trailing spaces in the recovered string are preserved (the pos4..6
// alphabet includes space, so trailing spaces are valid characters
// at those positions, not padding). Such strings will NOT round-trip
// through CallsignC28 because that function rejects space characters
// in the input — they originate only from corrupted wire input or
// hand-constructed bit patterns, not from real-callsign encodings.
func C28ToCallsign(c28 uint32) (string, C28Kind) {
	if c28 < nTokens {
		return "", C28KindToken
	}
	if c28 < stdCallOffset {
		return "", C28KindHash22
	}
	return c28ToStdCallsign(c28), C28KindStdCall
}

// c28ToStdCallsign inverts CallsignC28 for c28 ∈ [stdCallOffset, 2^28).
// Precondition: caller has verified c28 is in the std-call partition;
// otherwise the divmod produces bytes that may index out of the
// per-position alphabets and the array indexing will panic.
func c28ToStdCallsign(c28 uint32) string {
	const op = "codec.C28ToCallsign"

	// The forward computes
	//   n28 = stdCallOffset + m1*i1 + m2*i2 + m3*i3 + m4*i4 + m5*i5 + i6
	// where m_k is the product of the per-position alphabet sizes
	// for positions k+1..6. The mixed-base divmod recovers each i_k
	// in turn by dividing by the corresponding alphabet size from
	// position 6 backward. Constants here mirror the forward's m_k
	// chain by referring to the alphabet sizes directly so any
	// alphabet edit propagates to both directions at compile time.
	const (
		a2Sz = len(callsignAlphaPos2) // 36
		a3Sz = len(callsignAlphaPos3) // 10
		a4Sz = stdCallAlphaSz         // 27
	)

	x := int(c28) - stdCallOffset

	i6 := x % a4Sz
	x /= a4Sz
	i5 := x % a4Sz
	x /= a4Sz
	i4 := x % a4Sz
	x /= a4Sz
	i3 := x % a3Sz
	x /= a3Sz
	i2 := x % a2Sz
	i1 := x / a2Sz

	// Belt-and-braces: c28 in std-call range guarantees i1 < 37.
	// (i1 < 0 is unreachable since x >= 0 by partition.) An
	// out-of-range value here would indicate the precondition was
	// violated by the caller.
	if i1 >= len(callsignAlphaPos1) {
		panic(op + ": i1=" + strconv.Itoa(i1) + " ≥ 37 for c28=" + strconv.FormatUint(uint64(c28), 10))
	}

	var padded [6]byte
	padded[0] = callsignAlphaPos1[i1]
	padded[1] = callsignAlphaPos2[i2]
	padded[2] = callsignAlphaPos3[i3]
	padded[3] = callsignAlphaPos4[i4]
	padded[4] = callsignAlphaPos4[i5]
	padded[5] = callsignAlphaPos4[i6]

	return strings.TrimLeft(string(padded[:]), " ")
}
