package unpack

import (
	"strings"
)

// c58 alphabet per QEX ref [14] nonstd_to_c58.f90:
//
//	' 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/'
//
// 38 characters, index 0..37. Index 0 is space (used as left/right
// padding for callsigns shorter than 11 chars).
const c58Charset = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ/"

// decodeC58 expands a 58-bit nonstandard-callsign value into its
// up-to-11-character string form. The encoder packs 11 chars
// from a 38-char alphabet via repeated multiply-and-add:
//
//	n58 = Σ_{i=1..11} index(c, callsign[i]) · 38^(11-i)
//
// Decoder is the inverse — repeated mod/divide by 38, walking from
// the last character back to the first. Leading/trailing spaces
// from the padding are trimmed.
//
// Up to 11 characters: real FT8 nonstandard calls (e.g. "PJ4/K1ABC",
// "YW18FIFA") fit within the 11-char limit. Calls shorter than 11
// are space-padded by the encoder so the c58 fully populates the
// 58-bit field; trimming the leading/trailing spaces recovers the
// human form.
//
// Returns "<invalid>" for n58 values that decode to characters
// outside the alphabet (defensive; shouldn't happen given the
// encoder's range).
func decodeC58(n58 uint64) string {
	const length = 11
	var out [length]byte
	for i := length - 1; i >= 0; i-- {
		idx := uint(n58 % 38)
		if idx >= uint(len(c58Charset)) {
			return "<invalid>"
		}
		out[i] = c58Charset[idx]
		n58 /= 38
	}
	return strings.TrimSpace(string(out[:]))
}
