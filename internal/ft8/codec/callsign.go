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

// c28 range boundaries per QEX paper Appendix A Table 7. The 28-bit
// callsign code space is partitioned into three disjoint regions:
//
//	[0, nTokens)                  → CQ / DE / QRZ + "CQ <suffix>"
//	                                message-word tokens (Table 7
//	                                rows 1..7)
//	[nTokens, nTokens+max22)      → 22-bit hashes for non-standard
//	                                callsigns (Table 7 "22-bit hash
//	                                codes" row)
//	[nTokens+max22, 2^28)         → standard amateur callsigns
//	                                (Table 7 "Standard call signs"
//	                                row)
//
// Per QEX Appendix A: "28 bits are enough to encode any standard
// call sign uniquely." Every std-shape callsign (3..6 chars,
// 1..2-char prefix + 1 digit + 1..3-char suffix) packs into the
// third range via CallsignC28 with digit-position-3 alignment.
// The 22-bit hash range is reserved for non-standard calls that
// don't fit the std-call form (compound calls like PJ4/K1ABC,
// special-event calls like YW18FIFA); those land here via
// HashedCallC28 when they need to be referenced from a Type 1
// c28 slot after a prior Type 4 has carried the full c58 spelling.
const (
	nTokens        = 2063592 // QEX Table 7: pre-callsign-space tokens
	max22          = 1 << 22 // QEX Table 7: 22-bit hash range size (4194304)
	stdCallOffset  = nTokens + max22
	stdCallAlphaSz = 27 // a4 size; appears in the weights for positions 3..6
)

// CallsignC28 packs a standard amateur callsign into its 28-bit
// code per QEX paper Appendix A and Table 7. Returns a value in
// the std-call range [stdCallOffset, 2^28) for any valid std-shape
// callsign of length 3..6.
//
// **Digit-position-3 alignment.** The std c28 packer's per-position
// alphabets are:
//
//	pos 1: space + digit + letter   (37 chars)
//	pos 2: digit + letter           (36 chars)
//	pos 3: digit                    (10 chars)
//	pos 4..6: space + letter        (27 chars)
//
// Only pos 3 holds a digit, so the call's single digit must land
// at field index 2 (0-indexed) regardless of whether the prefix is
// 1 or 2 chars. The function detects the digit position in the
// input (must be at input index 1 for a 1-char prefix or 2 for a
// 2-char prefix per QEX §A) and left-pads with (2 - digitIdx)
// spaces so the digit aligns to field pos 3, then right-pads to
// 6 chars. Examples:
//
//	"K1JT"   → " K1JT "    (1-char prefix, 1 leading + 1 trailing)
//	"G3X"    → " G3X  "    (1-char prefix, 1 leading + 2 trailing)
//	"K1ABC"  → " K1ABC"    (1-char prefix, 1 leading + 0 trailing)
//	"AB1CDE" → "AB1CDE"    (2-char prefix, 0 padding)
//	"AB1CD"  → "AB1CD "    (2-char prefix, 0 leading + 1 trailing)
//	"VK7MO"  → "VK7MO "    (2-char prefix, 5-char call, 1 trailing)
//
// With this alignment every position holds a character from its
// alphabet — no negative-index arithmetic, no hash-range overlap.
// Every std-shape input produces a value ≥ stdCallOffset, decoded
// cleanly by C28ToCallsign.
//
// **Why this matters.** The public-domain `std_call_to_c28.f90`
// reference in QEX ref [14] uses Fortran's `adjustr` (right-justify
// only) and is incomplete: it only handles 5-char-1-prefix and
// 6-char-2-prefix shapes. Shorter calls and 5-char-2-prefix calls
// produce negative per-position indices under adjustr, yielding
// arithmetic byproducts that land inside the 22-bit hash range.
// That contradicts QEX Appendix A's invariant that "28 bits are
// enough to encode any standard call sign uniquely". The
// digit-aligned algorithm here pins to the QEX paper's spec, not
// the reference program's gap.
//
// Validation:
//
//   - Length must be 3..6 (FT8 std-call range).
//   - Every character must be [0-9A-Z] (uppercase).
//   - Exactly one digit, at input index 1 or 2 (prefix len 1 or 2).
//
// All three checks panic on violation. Callers are expected to have
// passed validateStdCallsign upstream; the panics catch contract
// breaches rather than turning into silent wire corruption.
//
// Returns uint32 even though the result fits in 28 bits — uint16
// is too small, and the 4 high bits are zero for any in-range
// input.
func CallsignC28(call string) uint32 {
	const op = "codec.CallsignC28"
	if len(call) < 3 || len(call) > 6 {
		panic(op + ": callsign length must be 3..6 (real FT8 std calls are prefix+digit+suffix), got len " + strconv.Itoa(len(call)))
	}

	// Per-character alphabet check: every char must be uppercase
	// ASCII letter or digit. Non-[0-9A-Z] inputs (lowercase, space,
	// punctuation) are caller-side bugs — std callsigns don't
	// contain such chars by definition. Panic so the caller learns
	// of the contract violation rather than getting silent corruption.
	for i := range len(call) {
		c := call[i]
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			panic(op + ": character at index " + strconv.Itoa(i) + " (" + string(c) + ") not in std-call alphabet (0-9, A-Z); call=" + call)
		}
	}

	// Determine the std-shape prefix length (1 or 2). The prefix
	// length identifies which char is the "decimal digit between
	// prefix and suffix" per QEX §A — distinct from any digits that
	// may appear inside a 2-char prefix (e.g. "9V1ABC" or "7Q5MLV"
	// where the prefix's first char is itself a digit). Without
	// this distinction, multi-digit inputs would be ambiguous.
	prefixLen := stdCallPrefixLen(call)
	if prefixLen == 0 {
		panic(op + ": " + call + " is not a valid std-callsign shape (need 1-2 char prefix with ≥1 letter, then digit, then 1-3 letter suffix); pre-validate via validateStdCallsign")
	}
	digitIdx := prefixLen

	// Left-pad so the digit lands at field index 2, then right-pad
	// to length 6 with spaces. leadingSpaces is either 1 (1-char
	// prefix) or 0 (2-char prefix).
	var padded [6]byte
	for i := range padded {
		padded[i] = ' '
	}
	leadingSpaces := 2 - digitIdx
	copy(padded[leadingSpaces:], call)

	// Per-position lookups. With digit-aligned padding every position
	// holds a char from its alphabet, so all indices are non-negative.
	i1 := strings.IndexByte(callsignAlphaPos1, padded[0])
	i2 := strings.IndexByte(callsignAlphaPos2, padded[1])
	i3 := strings.IndexByte(callsignAlphaPos3, padded[2])
	i4 := strings.IndexByte(callsignAlphaPos4, padded[3])
	i5 := strings.IndexByte(callsignAlphaPos4, padded[4])
	i6 := strings.IndexByte(callsignAlphaPos4, padded[5])

	if i1 < 0 || i2 < 0 || i3 < 0 || i4 < 0 || i5 < 0 || i6 < 0 {
		// Unreachable given the alignment + alphabet checks above;
		// defensive panic catches any future regression in the
		// alignment math before it corrupts wire output.
		panic(op + ": alignment regression, negative alphabet index for call=" + call + " padded=" + string(padded[:]))
	}

	// mN is the weight applied to position N's index — i.e. the
	// product of the alphabet sizes of all positions *after* N.
	// Using len(alphabet) rather than literal sizes couples the
	// weights to the alphabets at compile time.
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

	// Belt-and-braces output-range guard. With digit alignment and
	// all-non-negative indices, n28 is provably in [stdCallOffset,
	// 2^28). A regression here would corrupt wire packing silently —
	// one int compare per call is negligible vs catching that early.
	if n28 < stdCallOffset || n28 >= 1<<CallsignBits {
		panic(op + ": internal arithmetic regression, n28=" + strconv.Itoa(n28) + " out of [stdCallOffset, 2^28)")
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
	// codec's). Per QEX Appendix A Table 7 this range is reserved
	// for hashes of NON-STANDARD callsigns (compound calls like
	// PJ4/K1ABC, special-event calls like YW18FIFA) — standard calls
	// of any length 3..6 pack into the C28KindStdCall range, not
	// here.
	C28KindHash22

	// C28KindStdCall indicates c28 ∈ [stdCallOffset, 2^28) — a
	// standard amateur callsign. Per QEX Appendix A "28 bits are
	// enough to encode any standard call sign uniquely"; every
	// std-shape input (1..2 char prefix + 1 digit + 1..3 char suffix,
	// length 3..6) lands here via CallsignC28's digit-position-3
	// alignment. C28ToCallsign returns the recovered callsign.
	C28KindStdCall
)

// C28ToCallsign decodes a c28 value back to its string form by
// inverting CallsignC28. The C28Kind discriminator tells the caller
// which partition of the c28 value space the input occupied per
// QEX paper Appendix A Table 7. Only C28KindStdCall yields a non-
// empty recovered string from this function alone — C28KindToken
// and C28KindHash22 callers need additional state (token table or
// 22-bit hash table) to recover the original message fragment.
//
// For C28KindStdCall, the inverse is a mixed-base divmod against
// the per-position alphabet sizes, recovering the 6-char padded
// form. CallsignC28 uses digit-position-3 alignment (left-pad to
// align the digit, then right-pad to length 6), so the padded form
// may have leading and/or trailing space padding; the returned
// string strips both via TrimSpace.
//
// For c28 values not produced by a real CallsignC28 encode (hand-
// built bit patterns, corrupted wire), the divmod still yields a
// 6-char string but the digit may not land at position 3 and the
// shape may not be a valid std-callsign. The codec is bit-faithful:
// it returns whatever the arithmetic produces. Format-layer
// validation rejects out-of-shape recovered calls.
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

	// Both leading and trailing spaces are alignment padding (digit-
	// position-3 alignment produces padding on either side or both);
	// trim them to recover the operator-visible callsign.
	return strings.TrimSpace(string(padded[:]))
}
