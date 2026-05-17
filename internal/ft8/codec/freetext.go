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
// spaces), then walk the chars left-to-right computing the base-42
// polynomial accumulator = accumulator*42 + alphabet_index(char).
// 42^13 ≈ 9.27 × 10^20 fits in 71 bits with ~2.5× headroom.
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
