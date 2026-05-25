package unpack

import (
	"fmt"
	"strings"
)

// c28 encoding constants per QEX ref [14] std_call_to_c28.f90.
// The 28-bit space is partitioned into three regions:
//
//	[0, NTOKENS)              special tokens (CQ, DE, QRZ, "CQ XXX" variants)
//	[NTOKENS, NTOKENS+MAX22)  22-bit hash of a nonstandard callsign
//	[NTOKENS+MAX22, ...)      standard 6-character callsign, base-37 packed
const (
	c28NTokens uint32 = 2063592 // first standard-token boundary
	c28MAX22   uint32 = 4194304 // 22-bit hash range size = 2^22
)

// Per-position character sets for a right-aligned 6-char callsign.
// Position layout in the Fortran encoder:
//
//	i1: " 0-9 A-Z"    (37 chars, charset a1)
//	i2: "0-9 A-Z"     (36 chars, charset a2)
//	i3: "0-9"         (10 chars, charset a3)
//	i4, i5, i6: " A-Z" (27 chars, charset a4)
//
// e.g. "K1ABC" right-padded to "K1ABC " or right-aligned to
// " K1ABC"; the encoder uses adjustr (right-justify), so a 5-char
// call is " K1ABC" (leading space at i1).
const (
	c28Charset1 = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	c28Charset2 = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	c28Charset3 = "0123456789"
	c28Charset4 = " ABCDEFGHIJKLMNOPQRSTUVWXYZ"
)

// decodeC28 returns the callsign string represented by a 28-bit
// c28 value: a special token (CQ/DE/QRZ/"CQ XXX"), a hashed
// nonstandard callsign placeholder ("<...>"), or a standard 6-char
// callsign decoded from base-37 packing.
//
// Hashed nonstandard callsigns can't be resolved without the
// per-receiver running hashtable (a real receiver maintains it
// across slots). For this offline decoder we emit the placeholder
// "<...>" — matches jt9's display convention for unresolved hashes.
func decodeC28(c28 uint32) (string, error) {
	if c28 < c28NTokens {
		return decodeC28Token(c28), nil
	}
	if c28 < c28NTokens+c28MAX22 {
		return "<...>", nil
	}
	n := c28 - c28NTokens - c28MAX22

	// Reverse the encoder's base-(27,27,27,10,36,37) packing.
	i6 := n % 27
	n /= 27
	i5 := n % 27
	n /= 27
	i4 := n % 27
	n /= 27
	i3 := n % 10
	n /= 10
	i2 := n % 36
	n /= 36
	i1 := n

	if i1 >= 37 {
		return "", fmt.Errorf("c28: i1=%d exceeds charset size 37 (c28 value out of range?)", i1)
	}

	var sb strings.Builder
	sb.WriteByte(c28Charset1[i1])
	sb.WriteByte(c28Charset2[i2])
	sb.WriteByte(c28Charset3[i3])
	sb.WriteByte(c28Charset4[i4])
	sb.WriteByte(c28Charset4[i5])
	sb.WriteByte(c28Charset4[i6])
	// Strip leading/trailing spaces — the right-justified encoding
	// pads short calls with leading spaces in i1, and short calls
	// also leave i4/i5/i6 as spaces when they don't have suffixes.
	return strings.TrimSpace(sb.String()), nil
}

// decodeC28Token returns the special-token string for c28 values
// in [0, NTOKENS). Coverage:
//
//	0                "DE"
//	1                "QRZ"
//	2                "CQ"
//	3..1002          "CQ NNN" where NNN is a 3-digit number (CQ + frequency-bucket)
//	1003..532443     "CQ XXXX" — up-to-4-letter text, base-27 packed,
//	                 leading-space padded (charset " A-Z", 27 chars)
//	532444..NTOKENS  unhandled extended ranges (TOKEN_n placeholder)
//
// Empirical derivation from corpus: c28=1135 unpacks "  DX" =
// (0,0,4,24) in base-27, offset 1003 → "CQ DX". c28=71528 unpacks
// "COTA" = (3,15,20,1) → "CQ COTA". The single 27⁴-sized range
// (NOT two tiers as a first guess) is consistent with both samples
// and matches jt9's observed text.
//
// The 27-letter alphabet is charset4 = " ABCDEFGHIJKLMNOPQRSTUVWXYZ".
// Shorter texts ("DX", "POTA", "NA") have leading-space padding at
// the high digit positions; trimming the decoded string yields the
// canonical jt9 form ("CQ DX" not "CQ  DX" or "CQ DX ").
//
// Range size: 27⁴ = 531441. Total covered: 1003 + 531441 = 532444.
// NTOKENS = 2063592 leaves 1531148 further tokens whose encoding
// isn't covered here. Not yet seen in the test corpus.
func decodeC28Token(c28 uint32) string {
	switch {
	case c28 == 0:
		return "DE"
	case c28 == 1:
		return "QRZ"
	case c28 == 2:
		return "CQ"
	case c28 >= 3 && c28 <= 1002:
		return fmt.Sprintf("CQ %03d", c28-3)
	case c28 < 1003+531441:
		// "CQ XXXX" — up to 4 letters, base-27 packed, leading-space
		// padded for shorter texts.
		return "CQ " + unpackBase27Text(c28-1003, 4)
	default:
		return fmt.Sprintf("CQ_TOKEN_%d", c28)
	}
}

// unpackBase27Text expands an n-digit base-27 number into its
// corresponding text using the " A-Z" alphabet (charset4),
// returning the trimmed result. Used for extended "CQ XXXX" tokens
// where MSB-first encoding places leading spaces (= short text)
// at the high-digit positions.
func unpackBase27Text(n uint32, digits int) string {
	out := make([]byte, digits)
	for i := digits - 1; i >= 0; i-- {
		out[i] = c28Charset4[n%27]
		n /= 27
	}
	return strings.TrimSpace(string(out))
}
