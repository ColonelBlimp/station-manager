// callsign.go — FT8/FT4 28-bit callsign field encoding and decoding.
//
// The 28-bit field (0..268,435,455) is partitioned as follows:
//
//	0 .. NTokens-1                     Special tokens (DE, QRZ, CQ, CQ nnn, CQ suffix)
//	NTokens .. NTokens+Max22-1         22-bit hashed callsign (non-standard calls)
//	NTokens+Max22 .. 2^28-1            Standard callsign (mixed-radix encoding)
//
// Token sub-ranges (0 .. NTokens-1):
//
//	0          DE
//	1          QRZ
//	2          CQ (plain)
//	3..1002    CQ nnn (3-digit frequency offset, 0–999)
//	1003..532443  CQ XXXX (1–4 letter directed suffix, base-27)
//	532444..NTokens-1  reserved/unused
//
// Reference: ft8_lib src/ft8/message.c pack28()/unpack28(), WSJT-X lib/ft8/pack77.f90.

package message

import (
	"fmt"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Constants ---------------------------------------------------------------

// NBase is the number of distinct standard callsign encodings:
// 37 × 36 × 10 × 27 × 27 × 27 = 262,177,560.
const NBase uint32 = 262177560

// Max22 is the size of the 22-bit hash space (2^22 = 4,194,304).
const Max22 uint32 = 4194304

// NTokens is the number of reserved token values at the start of the 28-bit
// field. Defined as 2,063,592 in ft8_lib (message.c line 10).
const NTokens uint32 = 2063592

// Sentinel token values (low end of the 28-bit field).
const (
	TokenDE  uint32 = 0 // "DE"
	TokenQRZ uint32 = 1 // "QRZ"
	TokenCQ  uint32 = 2 // "CQ" (plain, no suffix)
)

// CQ sub-ranges within the token region.
const (
	tokenCQNumBase uint32 = 3      // start of CQ nnn
	tokenCQNumMax  uint32 = 1002   // end of CQ nnn (inclusive)
	tokenCQSufBase uint32 = 1003   // start of CQ with letter suffix
	tokenCQSufMax  uint32 = 532443 // end of CQ suffix (inclusive); 1003 + 26*(27^3 + 27^2 + 27 + 1) = 1003 + 531440
)

// hashBase is the start of the 22-bit hash region in the 28-bit field.
const hashBase uint32 = NTokens

// callBase is the start of the standard callsign region in the 28-bit field.
// callBase = NTokens + Max22 = 6,257,896.
const callBase uint32 = NTokens + Max22

// callLen is the fixed normalized callsign length (6 characters).
const callLen = 6

// charset is the FT8 37-character alphabet for callsign fields.
// Index 0 = space, 1–10 = '0'–'9', 11–36 = 'A'–'Z'.
//
// This matches ft8_lib's FT8_CHAR_TABLE_ALPHANUM_SPACE (text.c nchar()):
// space→0, '0'–'9'→1–10, 'A'–'Z'→11–36.
const charset = " 0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// --- Encode functions --------------------------------------------------------

// EncodeCallsign packs a standard amateur radio callsign into a 28-bit value.
//
// The callsign is uppercased, validated (2–6 alphanumeric characters, no '/'),
// and normalized to a 6-character form with the separating digit at position 2.
// It is then encoded using the mixed-radix system 37 × 36 × 10 × 27 × 27 × 27
// and offset by callBase (= NTokens + Max22) to place it in the standard
// callsign region of the 28-bit field.
//
// Special prefix workarounds (matching ft8_lib pack_basecall):
//   - Swaziland (3DA0): "3DA0XY" → packed as "3D0XY" (drops the 'A')
//   - Guinea (3X): "3XAYY" → packed as "QAYY" (replaces "3X" with "Q")
//
// These are reversed by DecodeCallsign on unpack.
//
// This function handles standard callsigns only. For special tokens, use
// EncodeCQ, EncodeCQNum, EncodeCQSuffix, EncodeDE, or EncodeQRZ.
func EncodeCallsign(call string) (uint32, error) {
	const op errors.Op = "message.EncodeCallsign"

	// Apply special prefix workarounds before normalization.
	call = packCallWorkaround(call)

	c6, err := normalizeCallsign(call)
	if err != nil {
		return 0, errors.New(op).Err(err).Msgf("callsign %q: %s", call, err)
	}

	// Charset indices for each position.
	i0 := charsetIndex(c6[0])
	i1 := charsetIndex(c6[1])
	i2 := charsetIndex(c6[2])
	i3 := charsetIndex(c6[3])
	i4 := charsetIndex(c6[4])
	i5 := charsetIndex(c6[5])

	if i0 < 0 || i1 < 0 || i2 < 0 || i3 < 0 || i4 < 0 || i5 < 0 {
		return 0, errors.New(op).Msgf("invalid character in normalized callsign %q", c6)
	}

	// Mixed-radix encoding (matching ft8_lib pack_basecall):
	//   c[0]: FT8_CHAR_TABLE_ALPHANUM_SPACE → 0–36  (37 values)
	//   c[1]: FT8_CHAR_TABLE_ALPHANUM       → 0–35  (36 values)
	//   c[2]: FT8_CHAR_TABLE_NUMERIC        → 0–9   (10 values)
	//   c[3]: FT8_CHAR_TABLE_LETTERS_SPACE  → 0–26  (27 values)
	//   c[4]: FT8_CHAR_TABLE_LETTERS_SPACE  → 0–26  (27 values)
	//   c[5]: FT8_CHAR_TABLE_LETTERS_SPACE  → 0–26  (27 values)
	n := uint32(i0)
	n = n*36 + uint32(i1-1)
	n = n*10 + uint32(i2-1)
	n = n*27 + letterSpaceMR(i3)
	n = n*27 + letterSpaceMR(i4)
	n = n*27 + letterSpaceMR(i5)

	// Offset into standard callsign region (ft8_lib: NTOKENS + MAX22 + n).
	return callBase + n, nil
}

// EncodeCQ returns the 28-bit token for plain "CQ".
func EncodeCQ() uint32 { return TokenCQ }

// EncodeDE returns the 28-bit token for "DE".
func EncodeDE() uint32 { return TokenDE }

// EncodeQRZ returns the 28-bit token for "QRZ".
func EncodeQRZ() uint32 { return TokenQRZ }

// EncodeCQNum encodes a CQ with a 3-digit frequency offset (0–999).
// For example, EncodeCQNum(350) represents "CQ 350".
func EncodeCQNum(freq int) (uint32, error) {
	const op errors.Op = "message.EncodeCQNum"

	if freq < 0 || freq > 999 {
		return 0, errors.New(op).Msgf("frequency %d out of range [0, 999]", freq)
	}
	return tokenCQNumBase + uint32(freq), nil
}

// EncodeCQSuffix encodes a directed CQ with a 1–4 uppercase letter suffix.
// For example, EncodeCQSuffix("DX") represents "CQ DX".
//
// The suffix is encoded in base-27 (A=1..Z=26) following ft8_lib
// parse_cq_modifier() (message.c line 792).
func EncodeCQSuffix(suffix string) (uint32, error) {
	const op errors.Op = "message.EncodeCQSuffix"

	suffix = strings.ToUpper(strings.TrimSpace(suffix))
	if len(suffix) < 1 || len(suffix) > 4 {
		return 0, errors.New(op).Msgf("suffix length %d out of range [1, 4]", len(suffix))
	}

	var k uint32
	for i := 0; i < len(suffix); i++ {
		c := suffix[i]
		if c < 'A' || c > 'Z' {
			return 0, errors.New(op).Msgf("invalid character %q at position %d (letters only)", c, i)
		}
		k = k*27 + uint32(c-'A') + 1
	}

	return tokenCQSufBase + k, nil
}

// --- Decode function ---------------------------------------------------------

// DecodeCallsign unpacks a 28-bit value into a callsign or token string.
//
// Return values by region:
//   - Token (0..NTokens-1): "DE", "QRZ", "CQ", "CQ 350", "CQ DX", etc.
//   - Hash22 (NTOKENS..NTOKENS+Max22-1): "<...>" (hash cannot be reversed)
//   - Standard callsign (NTOKENS+Max22..): decoded and trimmed, e.g. "W1AW"
func DecodeCallsign(n28 uint32) (string, error) {
	const op errors.Op = "message.DecodeCallsign"

	switch {
	case n28 < NTokens:
		return decodeToken(n28)

	case n28 < hashBase+Max22:
		// 22-bit hashed callsign — cannot be reversed without a lookup table.
		return "<...>", nil

	default:
		n := n28 - callBase
		if n >= NBase {
			return "", errors.New(op).Msgf("value %d exceeds maximum standard callsign", n28)
		}
		return decodeStandard(n)
	}
}

// --- Internal helpers --------------------------------------------------------

// charsetIndex returns the index (0–36) of byte c in the FT8 callsign charset.
// Returns -1 if c is not a valid character.
func charsetIndex(c byte) int {
	switch {
	case c == ' ':
		return 0
	case c >= '0' && c <= '9':
		return int(c-'0') + 1
	case c >= 'A' && c <= 'Z':
		return int(c-'A') + 11
	default:
		return -1
	}
}

// letterSpaceMR maps a charset index for a letter-or-space position to a
// mixed-radix digit 0–26. Space (charset 0) → 0, A–Z (charset 11–36) → 1–26.
// Matches ft8_lib's FT8_CHAR_TABLE_LETTERS_SPACE: space=0, A=1..Z=26.
//
// Precondition: idx must be 0 (space) or 11–36 (A–Z). Digit indices (1–10)
// and negative values are invalid; normalizeCallsign ensures positions 3–5
// contain only letters or spaces before this function is called.
func letterSpaceMR(idx int) uint32 {
	if idx == 0 {
		return 0
	}
	if idx < 11 || idx > 36 {
		panic(fmt.Sprintf("letterSpaceMR: charset index %d is not space or letter (expected 0 or 11–36)", idx))
	}
	return uint32(idx - 10)
}

// mrToLetterSpace is the inverse of letterSpaceMR.
// Mixed-radix digit 0 → charset index 0 (space), 1–26 → charset index 11–36 (A–Z).
func mrToLetterSpace(d int) int {
	if d == 0 {
		return 0
	}
	return d + 10
}

// normalizeCallsign uppercases, validates, and pads a callsign to a 6-byte
// array with the separating digit at position 2.
//
// Padding rules (per ft8_lib pack_basecall, message.c line 719):
//   - If call[2] is a digit and length ≤ 6 → copy as-is (two-char prefix).
//   - Else if call[1] is a digit and length ≤ 5 → left-pad with space
//     (single-char prefix, e.g. W1AW → " W1AW ").
//   - Right-pad with spaces to exactly 6 characters.
//   - Validate position 2 is a digit, position 1 is alphanumeric, and
//     positions 3–5 are letter or space.
func normalizeCallsign(call string) ([callLen]byte, error) {
	const op errors.Op = "message.normalizeCallsign"

	call = strings.ToUpper(strings.TrimSpace(call))

	if len(call) < 2 || len(call) > callLen {
		return [callLen]byte{}, errors.New(op).Msgf(
			"length %d out of range [2, %d]", len(call), callLen)
	}

	// Validate: only alphanumeric in the raw callsign (no space, no '/').
	for i := 0; i < len(call); i++ {
		if !isAlphanumeric(call[i]) {
			return [callLen]byte{}, errors.New(op).Msgf(
				"invalid character %q at position %d", call[i], i)
		}
	}

	// Initialize with spaces.
	var c6 [callLen]byte
	for i := range c6 {
		c6[i] = ' '
	}

	if call[1] >= '0' && call[1] <= '9' {
		// Second character is a digit → left-pad with space.
		// Max original length is 5 (single-char prefix + digit + 3 suffix).
		if len(call) > callLen-1 {
			return [callLen]byte{}, errors.New(op).Msgf(
				"callsign %q too long after left-pad (max %d chars with single-char prefix)",
				call, callLen-1)
		}
		copy(c6[1:1+len(call)], call)
	} else {
		copy(c6[:len(call)], call)
	}

	// Validate normalized position constraints.
	if c6[2] < '0' || c6[2] > '9' {
		return [callLen]byte{}, errors.New(op).Msgf(
			"position 2 must be a digit, got %q (normalized: %q)",
			c6[2], string(c6[:]))
	}
	if c6[1] == ' ' {
		return [callLen]byte{}, errors.New(op).Msgf(
			"position 1 must be alphanumeric (normalized: %q)",
			string(c6[:]))
	}
	for i := 3; i < callLen; i++ {
		if c6[i] >= '0' && c6[i] <= '9' {
			return [callLen]byte{}, errors.New(op).Msgf(
				"position %d must be letter or space, got %q (normalized: %q)",
				i, c6[i], string(c6[:]))
		}
	}

	return c6, nil
}

// decodeToken handles the token region (n28 < NTokens).
func decodeToken(n28 uint32) (string, error) {
	const op errors.Op = "message.decodeToken"

	switch {
	case n28 == TokenDE:
		return "DE", nil
	case n28 == TokenQRZ:
		return "QRZ", nil
	case n28 == TokenCQ:
		return "CQ", nil
	case n28 <= tokenCQNumMax:
		return fmt.Sprintf("CQ %03d", n28-tokenCQNumBase), nil
	case n28 <= tokenCQSufMax:
		return decodeCQSuffix(n28 - tokenCQSufBase)
	default:
		// Reserved/unused token space (532444..NTokens-1).
		return "", errors.New(op).Msgf("reserved token value %d", n28)
	}
}

// decodeStandard reverses the mixed-radix encoding for a standard callsign.
// n is the basecall value (0..NBase-1), already offset-adjusted.
//
// After decoding, special prefix workarounds are applied to reverse the
// 3DA0→3D0 and 3X→Q remapping done at encode time.
func decodeStandard(n uint32) (string, error) {
	const op errors.Op = "message.decodeStandard"

	d5 := int(n % 27)
	n /= 27
	d4 := int(n % 27)
	n /= 27
	d3 := int(n % 27)
	n /= 27
	d2 := int(n % 10)
	n /= 10
	d1 := int(n % 36)
	n /= 36
	d0 := int(n)

	if d0 > 36 {
		return "", errors.New(op).Msgf("position 0 value %d exceeds charset", d0)
	}

	var c6 [callLen]byte
	c6[0] = charset[d0]
	c6[1] = charset[d1+1]                // 0–35 → charset 1–36
	c6[2] = charset[d2+1]                // 0–9  → charset 1–10
	c6[3] = charset[mrToLetterSpace(d3)] // 0–26 → space or A–Z
	c6[4] = charset[mrToLetterSpace(d4)]
	c6[5] = charset[mrToLetterSpace(d5)]

	call := strings.TrimSpace(string(c6[:]))

	// Reverse special prefix workarounds (3D0→3DA0, Q→3X).
	return unpackCallWorkaround(call), nil
}

// decodeCQSuffix decodes a CQ directed suffix from its base-27 encoding.
// Each position: 0 = unused (space), 1–26 = A–Z. Up to 4 characters.
// Matches ft8_lib unpack28() (message.c line 903).
func decodeCQSuffix(k uint32) (string, error) {
	var buf [4]byte
	for i := 3; i >= 0; i-- {
		d := k % 27
		k /= 27
		if d == 0 {
			buf[i] = ' '
		} else {
			buf[i] = byte('A' + d - 1)
		}
	}

	suffix := strings.TrimSpace(string(buf[:]))
	if suffix == "" {
		// k=0 produces all-space digits → empty suffix. This is unreachable via
		// EncodeCQSuffix (minimum suffix is one letter → k ≥ 1), but could arise
		// from a raw n28 = tokenCQSufBase (1003). We return "CQ" to match ft8_lib's
		// trim_front() behaviour, noting that this is the same decoded string as the
		// plain-CQ token (TokenCQ = 2) — the two 28-bit values are distinct but
		// decode identically.
		return "CQ", nil
	}
	return "CQ " + suffix, nil
}

func isAlphanumeric(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')
}

// --- Special prefix workarounds (3DA0 / 3X) ----------------------------------

// packCallWorkaround applies encode-time prefix remapping for callsigns that
// don't fit the standard 6-character normalization scheme.
//
// Matches ft8_lib pack_basecall() (message.c):
//   - Swaziland (3DA0): "3DA0XY" → "3D0XY" (drops the 'A')
//   - Guinea (3X): "3XAYY" → "QAYY" (replaces "3X" with "Q")
func packCallWorkaround(call string) string {
	call = strings.ToUpper(strings.TrimSpace(call))

	// 3DA0 prefix: "3DA0..." → "3D0..." (remove the 'A').
	if len(call) >= 4 && call[0] == '3' && call[1] == 'D' &&
		call[2] == 'A' && call[3] == '0' {
		return "3D0" + call[4:]
	}

	// 3X prefix: "3X..." → "Q..." (replace "3X" with "Q").
	if len(call) >= 2 && call[0] == '3' && call[1] == 'X' {
		return "Q" + call[2:]
	}

	return call
}

// unpackCallWorkaround reverses the encode-time prefix remapping applied by
// packCallWorkaround.
//
// Matches ft8_lib unpack28() (message.c):
//   - "3D0..." → "3DA0..." (Swaziland)
//   - "Q[A-Z]..." → "3X[A-Z]..." (Guinea)
func unpackCallWorkaround(call string) string {
	// 3D0 → 3DA0: if decoded call starts with "3D0", insert 'A'.
	if len(call) >= 3 && call[0] == '3' && call[1] == 'D' && call[2] == '0' {
		return "3DA0" + call[3:]
	}

	// Q → 3X: if decoded call starts with 'Q' followed by a letter.
	if len(call) >= 2 && call[0] == 'Q' && call[1] >= 'A' && call[1] <= 'Z' {
		return "3X" + call[1:]
	}

	return call
}
