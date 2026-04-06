// Package message
// FT8/FT4 free-text encoding and decoding (71-bit, 13-character, base-42).
//
// Free text is one of the FT8 message types (i3=0, n3=0). It encodes up to
// 13 characters from a 42-symbol alphabet into 71 bits using base-42 packing.
//
// The 42-character alphabet (index 0–41):
//
//	0–9:   '0'–'9'
//	10–35: 'A'–'Z'
//	36: '+'   37: '-'   38: '.'   39: '/'   40: '?'   41: ' '
//
// This matches ft8_lib text.c FT8_CHAR_TABLE_FULL / nchar() and WSJT-X
// lib/ft8/pack77.f90 char_index/pack_text/unpack_text.
//
// Encoding: the 13-character string is treated as a base-42 number (most
// significant character first). The result fits in 71 bits since
// 42^13 = 1,265,437,718,438,866,624,512 < 2^71 = 2,361,183,241,434,822,606,848.
//
// Reference: ft8_lib src/ft8/text.c pack_text()/unpack_text().
package message

import (
	"math/big"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Free-text constants -----------------------------------------------------

// FreeTextLen is the fixed number of characters in a free-text message.
const FreeTextLen = 13

// FreeTextBits is the number of bits used to encode a free-text message.
const FreeTextBits = 71

// freeTextCharset is the 42-character FT8 free-text alphabet.
// Index 0–9 = '0'–'9', 10–35 = 'A'–'Z', 36–41 = '+', '-', '.', '/', '?', ' '.
//
// This matches ft8_lib FT8_CHAR_TABLE_FULL (text.c) and the nchar() mapping.
const freeTextCharset = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ+-./? "

// freeTextBase is the radix for free-text encoding (number of distinct symbols).
const freeTextBase = 42

// bigBase is the *big.Int form of freeTextBase, allocated once.
var bigBase = big.NewInt(freeTextBase)

// bigShift64 is 2^64 as a *big.Int, used to split the 71-bit value into hi:lo.
var bigShift64 = new(big.Int).Lsh(big.NewInt(1), 64)

// --- Encode ------------------------------------------------------------------

// EncodeFreeText packs a free-text string (up to 13 characters) into a 71-bit
// value returned as a uint64 pair: the high bits (bits 64–70) in hi and the
// low 64 bits in lo. Together they represent a 71-bit unsigned integer.
//
// The input is uppercased and right-padded with spaces to exactly 13 characters.
// Characters not in the 42-symbol alphabet are replaced with space (index 41),
// matching ft8_lib pack_text() behaviour.
//
// Returns an error only if the input exceeds 13 characters after trimming.
func EncodeFreeText(text string) (hi uint8, lo uint64, err error) {
	const op errors.Op = "message.EncodeFreeText"

	text = strings.ToUpper(strings.TrimRight(text, " "))
	if len(text) > FreeTextLen {
		return 0, 0, errors.New(op).Msgf(
			"free text %q exceeds maximum length %d", text, FreeTextLen)
	}

	// Right-pad with spaces to exactly 13 characters.
	for len(text) < FreeTextLen {
		text += " "
	}

	// Base-42 encode: most significant character first.
	// The result can be up to 71 bits (exceeds uint64), so we use math/big.
	val := new(big.Int)
	idx := new(big.Int)
	for i := 0; i < FreeTextLen; i++ {
		val.Mul(val, bigBase)
		idx.SetInt64(int64(freeTextCharIndex(text[i]))) // uint8 → int64: safe widening
		val.Add(val, idx)
	}

	// Split into hi (bits 70–64) and lo (bits 63–0).
	var mod big.Int
	val.DivMod(val, bigShift64, &mod)
	lo = mod.Uint64()
	hi = uint8(val.Uint64())

	return hi, lo, nil
}

// --- Decode ------------------------------------------------------------------

// DecodeFreeText unpacks a 71-bit value (hi:lo pair from EncodeFreeText) into
// a free-text string. The result is right-trimmed of trailing spaces.
//
// hi contains bits 64–70 (max 7 bits), lo contains bits 0–63.
func DecodeFreeText(hi uint8, lo uint64) string {
	// Reconstruct the 71-bit value: val = hi * 2^64 + lo.
	val := new(big.Int).SetUint64(uint64(hi))
	val.Lsh(val, 64)
	val.Add(val, new(big.Int).SetUint64(lo))

	// Base-42 decode: extract least significant digit first (right to left).
	var buf [FreeTextLen]byte
	var mod big.Int
	for i := FreeTextLen - 1; i >= 0; i-- {
		val.DivMod(val, bigBase, &mod)
		digit := mod.Int64()
		if digit >= 0 && digit < freeTextBase {
			buf[i] = freeTextCharset[digit]
		} else {
			buf[i] = ' ' // defensive
		}
	}

	return strings.TrimRight(string(buf[:]), " ")
}

// --- Internal helpers --------------------------------------------------------

// freeTextCharIndex returns the index (0–41) of byte c in the FT8 free-text
// charset. Characters not in the alphabet map to 41 (space), matching ft8_lib
// nchar() fallback behaviour.
func freeTextCharIndex(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'A' && c <= 'Z':
		return c - 'A' + 10
	case c == '+':
		return 36
	case c == '-':
		return 37
	case c == '.':
		return 38
	case c == '/':
		return 39
	case c == '?':
		return 40
	default:
		return 41 // space (and any invalid character)
	}
}
