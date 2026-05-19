package codec

import (
	"math/bits"
	"strconv"
	"strings"
)

// F71Bits is the width of the FT8 free-text message body per QEX
// paper Table 1 (message type 0.0 — Free Text). A 13-character
// message from the 42-symbol alphabet packs into 71 bits via
// base-42 polynomial conversion.
const F71Bits = 71

// Free-text encoding constants.
const (
	// f71Alphabet is the 42-char input alphabet per the reference
	// `free_text_to_f71.f90`: space + 0-9 + A-Z + + - . / ?
	// (Yes, those last five really are the trailing chars in order.)
	f71Alphabet = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./?"

	// f71AlphabetSize is the radix of the base conversion.
	f71AlphabetSize = 42

	// f71MessageLen is the fixed padded message length the algorithm
	// operates on. Shorter inputs are right-justified with leading
	// spaces (matches the reference's adjustr).
	f71MessageLen = 13

	// f71HiBits is the number of bits the (hi, lo) accumulator's
	// high word carries — 7 bits, since 71 - 64 = 7. Used both by
	// the post-loop range guard and the output bit-packing loop.
	f71HiBits = F71Bits - 64
)

// FreeTextToF71 packs up to 13 characters of arbitrary message text
// into a 71-bit code per QEX paper §A and the public-domain
// `free_text_to_f71.f90` reference in QEX ref [14]. The output is
// bit-per-byte form (each byte 0 or 1), 71 bytes long, MSB-first.
//
// Algorithm: right-justify the input to 13 chars (pad left with
// spaces — see CallsignC58's docs for the padding-asymmetry note
// across all the string primitives in this package), then walk the
// chars left-to-right computing the base-42 polynomial accumulator
// = accumulator*42 + alphabet_index(char). 42^13 ≈ 9.27 × 10^20
// fits in 71 bits with ~2.5× headroom.
//
// The accumulator overflows uint64 around iteration 12, so the
// implementation carries a two-word (hi:lo) accumulator and uses
// math/bits.Mul64 for the 64×64→128 multiplication step. The high
// word never exceeds 7 bits by the invariant `acc < 2^71`; a guard
// at the end of the loop catches any future regression.
//
// Used by FT8 message type 0.0 (Free Text) — the f71 slot is the
// entire 71-bit message body for that type (the remaining 6 of
// 77 bits are the i3.n3 type indicator).
//
// Panics on inputs longer than 13 chars or containing characters
// outside the 42-symbol alphabet. The Fortran reference is lenient
// (substitutes unknown chars with space); SM is strict because
// silently substituting would produce a message different from
// what the operator typed — a worse failure mode than rejecting it
// at the call site so the upstream message-pack layer can sanitize.
//
// Case-sensitive: callers must upper-case before invocation.
//
// Whitespace asymmetry is worth knowing: leading spaces in the input
// are equivalent to omitting them (the right-justify pad to 13
// chars absorbs them). Internal and trailing spaces ARE preserved
// as message content — FreeTextToF71("CQ") and FreeTextToF71("CQ ")
// produce different output. Callers passing raw operator-entered
// text should trim trailing whitespace if that's not the intended
// content.
func FreeTextToF71(text string) []byte {
	const op = "codec.FreeTextToF71"
	if len(text) > f71MessageLen {
		panic(op + ": text exceeds " + strconv.Itoa(f71MessageLen) + " chars, got len " + strconv.Itoa(len(text)))
	}

	// Right-justify with leading spaces (matches Fortran's adjustr).
	var padded [f71MessageLen]byte
	for i := range padded {
		padded[i] = ' '
	}
	copy(padded[f71MessageLen-len(text):], text)

	// Base-42 polynomial in (hi:lo) two-word accumulator. hi never
	// exceeds 7 bits by the algorithm's invariant.
	var hi, lo uint64
	for i := range f71MessageLen {
		j := strings.IndexByte(f71Alphabet, padded[i])
		if j < 0 {
			panic(op + ": character at index " + strconv.Itoa(i) + " (" + string(padded[i]) + ") not in free-text alphabet (space + 0-9 + A-Z + + - . / ?)")
		}
		// (hi:lo) = (hi:lo) * 42 + j
		productHiFromLo, productLo := bits.Mul64(lo, f71AlphabetSize)
		newLo, carry := bits.Add64(productLo, uint64(j), 0)
		newHi := hi*f71AlphabetSize + productHiFromLo + carry
		hi = newHi
		lo = newLo
	}

	// Output-range guard: hi must fit in f71HiBits = 7 bits.
	// 42^13 < 2^71 guarantees this by construction; the guard
	// catches any future regression in the multiplication carry
	// before garbled bits leak into downstream message packing.
	if hi >= (1 << f71HiBits) {
		panic(op + ": internal arithmetic regression, hi=" + strconv.FormatUint(hi, 10) + " exceeds " + strconv.Itoa(f71HiBits) + " bits")
	}

	// Pack to bit-per-byte, MSB-first: out[0] is bit 70 (top bit of
	// hi), out[70] is bit 0 (bottom bit of lo).
	out := make([]byte, F71Bits)
	for i := range f71HiBits {
		out[i] = byte((hi >> (f71HiBits - 1 - i)) & 1)
	}
	for i := range 64 {
		out[f71HiBits+i] = byte((lo >> (63 - i)) & 1)
	}
	return out
}

// F71ToFreeText inverts FreeTextToF71. Reads the 71-bit code in
// bit-per-byte form (MSB-first per the package convention), recovers
// the 13-char base-42 representation, and returns the message text
// with leading spaces trimmed.
//
// Why "leading" not "trailing": FreeTextToF71 right-justifies via the
// Fortran-style adjustr pad — short inputs sit at the right of the
// 13-char buffer with spaces on the LEFT. The inverse recovers the
// 13-char buffer as `<padding-spaces><user-content>`; trimming the
// leading run of spaces recovers the user-typed content. Internal
// and trailing spaces (genuine message content per the forward
// function's doc) are preserved.
//
// One inherent asymmetry of the encoding: leading spaces in the
// original input are indistinguishable from padding and are NOT
// recovered. Round-trip is text-identity for any input without
// leading spaces, which covers normal Free Text usage.
//
// Panics on inputs not exactly 71 bits, on bit values outside {0, 1},
// or on a (hi:lo) word whose high part exceeds f71HiBits (= 7) — the
// last would indicate corruption upstream of this function (a Layer 2
// decoder reading from a malformed 77-bit body should already have
// guarded the 71-bit slot to 71 bits before calling here).
func F71ToFreeText(f71 []byte) string {
	const op = "codec.F71ToFreeText"
	if len(f71) != F71Bits {
		panic(op + ": input must be " + strconv.Itoa(F71Bits) + " bits, got " + strconv.Itoa(len(f71)))
	}

	// Reconstruct the two-word (hi:lo) accumulator from the bit-per-
	// byte buffer. hi takes the top f71HiBits = 7 bits; lo takes the
	// next 64 bits. Each byte masked with &1 defends any future raw-
	// bit caller from smuggling non-bit-valued bytes through (matches
	// readBitsUint64's defensive masking in decode.go).
	var hi, lo uint64
	for i := range f71HiBits {
		hi = (hi << 1) | uint64(f71[i]&1)
	}
	for i := range 64 {
		lo = (lo << 1) | uint64(f71[f71HiBits+i]&1)
	}

	// Range guard: hi must fit in f71HiBits. The 71-bit slot's
	// shape guarantees this by construction; a violation would
	// indicate the caller wrote bits past the slot's width upstream.
	if hi >= (1 << f71HiBits) {
		panic(op + ": hi=" + strconv.FormatUint(hi, 10) + " exceeds " + strconv.Itoa(f71HiBits) + " bits — caller wrote past the f71 slot")
	}

	// Recover 13 base-42 digits via repeated divmod from right to
	// left. The forward computed acc = acc*42 + digit[i] processing
	// digits left-to-right; the inverse strips the rightmost digit
	// each iteration (= acc mod 42) and shrinks (acc /= 42) until
	// all 13 are recovered. The recovered digits feed back into the
	// 13-char buffer right-to-left.
	//
	// 128-bit / 64-bit divide: math/bits.Div64(remHi, lo, divisor)
	// requires remHi < divisor to avoid overflow. We split hi first
	// (hi may be up to 7 bits = 127 initially, larger than divisor
	// 42) into qHi + rHi, then use rHi as the high-word input to
	// Div64. After a few iterations hi shrinks to 0 and the split
	// becomes a no-op.
	var padded [f71MessageLen]byte
	for k := f71MessageLen - 1; k >= 0; k-- {
		qHi := hi / f71AlphabetSize
		rHi := hi % f71AlphabetSize
		qLo, r := bits.Div64(rHi, lo, f71AlphabetSize)
		hi = qHi
		lo = qLo
		padded[k] = f71Alphabet[r]
	}

	return strings.TrimLeft(string(padded[:]), " ")
}
