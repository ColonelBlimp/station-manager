package codec

import (
	"strconv"
	"strings"
)

// C58Bits is the width of the FT8 nonstandard-callsign code per
// QEX paper §A: "Nonstandard call signs can be conveyed using 58
// bits." Used for compound calls with '/' (e.g. "PJ4/K1ABC") and
// special-event calls (e.g. "YW18FIFA") that don't fit the standard-
// call 28-bit slot.
const C58Bits = 58

// nonstdCallLen is the fixed padded callsign length the algorithm
// operates on. Inputs shorter than this are left-justified with
// trailing spaces (unlike FreeTextToF71 which right-justifies — see
// the padding-asymmetry note in CallsignC58's docs).
const nonstdCallLen = 11

// CallsignC58 packs a callsign-shaped string into a 58-bit code per
// QEX paper §A and the public-domain `nonstd_to_c58.f90` reference
// in QEX ref [14]. Typical use is for nonstandard callsigns —
// compound calls with '/' (e.g. "PJ4/K1ABC") and special-event
// calls (e.g. "YW18FIFA") — that don't fit the standard-call 28-bit
// slot. The encoder will accept any callsign-shaped string in the
// alphabet, including standard calls like "K1JT"; routing standard
// calls to CallsignC28 and nonstandard ones here is the upstream
// message-pack layer's job, not this function's.
//
// Algorithm: pad to 11 chars with TRAILING spaces (left-justify),
// then walk chars left-to-right computing the base-38 polynomial:
// n58 = n58*38 + alphabet_index. 38^11 ≈ 2.39 × 10^17 fits in 58
// bits with ~17% headroom (2^58 ≈ 2.88 × 10^17).
//
// Shares hashAlphabet (38 chars: space + 0-9 + A-Z + /) with
// HashCodes — both are spec-pinned to the same FT8 callsign-
// character set per QEX paper Table 2. The constant has lived in
// hashcodes.go since that file came first; this file references it
// rather than redeclaring to enforce the "same alphabet" invariant
// at compile time.
//
// # Padding asymmetry across the codec's string primitives
//
// Each primitive's character-array storage in the Fortran reference
// produces a different padding default:
//
//	CallsignC28:    right-justifies (adjustr; leading spaces)
//	CallsignC58:    left-justifies  (char*11, no adjust; trailing spaces)
//	HashCodes:      left-justifies  (adjustl + char*11; trailing spaces)
//	FreeTextToF71:  right-justifies (adjustr; leading spaces)
//
// The asymmetry is preserved exactly so spec vectors round-trip
// against the reference programs. A future reader verifying any
// primitive against its `.f90` counterpart needs to know which way
// it justifies.
//
// Panics on inputs longer than 11 chars or containing characters
// outside the alphabet. Case-sensitive: callers must upper-case
// before invocation.
//
// Returns uint64 because 58 bits don't fit in uint32 but fit
// comfortably in uint64 (the upper 6 bits are always zero for any
// in-range input).
func CallsignC58(call string) uint64 {
	const op = "codec.CallsignC58"
	if len(call) > nonstdCallLen {
		panic(op + ": callsign exceeds " + strconv.Itoa(nonstdCallLen) + " chars, got len " + strconv.Itoa(len(call)))
	}

	// Left-justify with trailing spaces (matches the Fortran's
	// character*11 + no-adjust storage).
	var padded [nonstdCallLen]byte
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded[:], call)

	var n58 uint64
	for i := range nonstdCallLen {
		j := strings.IndexByte(hashAlphabet, padded[i])
		if j < 0 {
			panic(op + ": character at index " + strconv.Itoa(i) + " (" + string(padded[i]) + ") not in callsign alphabet (space + 0-9 + A-Z + /)")
		}
		n58 = n58*hashAlphabetSize + uint64(j)
	}

	// Output-range guard: result must fit in 58 bits (38^11 < 2^58
	// by construction). The guard catches any future arithmetic
	// regression before garbled bits leak into downstream message
	// packing.
	if n58 >= (1 << C58Bits) {
		panic(op + ": internal arithmetic regression, n58=" + strconv.FormatUint(n58, 10) + " exceeds " + strconv.Itoa(C58Bits) + " bits")
	}
	return n58
}

// C58ToCallsign is the Layer 1 inverse of CallsignC58. Given a 58-bit
// code, recover the original callsign string. Used by the Type 4
// (NonStd Call) decoder to surface the c58 side of the wire as text.
//
// Algorithm: walk right-to-left over the 11 character positions,
// extracting each alphabet index via divmod by 38. Trim trailing
// spaces to undo CallsignC58's left-justified-with-trailing-spaces
// padding (see the padding-asymmetry note in CallsignC58's doc).
//
// Panics on inputs ≥ 2^58 — the wire layer guards the bit width
// upstream so this only fires on internal misuse.
//
// Returns the empty string for c58=0 (the all-spaces encoding). The
// QEX paper doesn't reserve any c58 codepoints, so every input in
// [0, 2^58) maps to a recoverable string, but some of those strings
// will not be syntactically valid callsigns (e.g. "/////" or pure
// digits) — the Layer 1 primitive is bit-faithful; syntactic
// validation is the message-layer's concern.
func C58ToCallsign(c58 uint64) string {
	const op = "codec.C58ToCallsign"
	if c58 >= (1 << C58Bits) {
		panic(op + ": c58=" + strconv.FormatUint(c58, 10) + " exceeds " + strconv.Itoa(C58Bits) + " bits")
	}
	var chars [nonstdCallLen]byte
	n := c58
	for i := nonstdCallLen - 1; i >= 0; i-- {
		chars[i] = hashAlphabet[n%hashAlphabetSize]
		n /= hashAlphabetSize
	}
	return strings.TrimRight(string(chars[:]), " ")
}
