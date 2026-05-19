package codec

import (
	"strconv"
	"strings"
)

// Token alphabet a4 per QEX paper §A and the std_call_to_c28.f90
// reference's pos-4..6 alphabet (' ABCDEFGHIJKLMNOPQRSTUVWXYZ', 27
// chars). The CQ-letters partition uses a 4-position base-27 encoding
// over this alphabet: space = 0, A = 1, ..., Z = 26. Letters land in
// the trailing positions; leading positions hold spaces.
const tokenLettersBase = 27

// Token-table boundaries per QEX paper Table 7. The full c28 token
// partition [0, nTokens) breaks down as:
//
//	c28 = 0:                 DE
//	c28 = 1:                 QRZ
//	c28 = 2:                 CQ
//	c28 ∈ [3, 1002]:          "CQ 000" .. "CQ 999"
//	c28 = 1003:              (gap — would be bare "CQ"/zero suffix
//	                          but bare CQ is the singleton at c28=2)
//	c28 ∈ [1004, 532443]:    "CQ <letters>" widths 1..4 with
//	                          intra-row gaps for embedded/trailing
//	                          spaces in the 4-char base-27 form.
//	c28 ∈ [532444, nTokens-1]: large reserved gap.
//
// The CQ-letters formula is c28 = cqLettersBase + base27(suffix)
// where suffix is left-space-padded to 4 chars (space=0, A=1..Z=26).
// Derived analytically from Table 7's row boundaries — ref [14] ships
// no token encoder reference program.
const (
	tokenDE  = uint32(0)
	tokenQRZ = uint32(1)
	tokenCQ  = uint32(2)

	cqDigitsLo = uint32(3)    // "CQ 000"
	cqDigitsHi = uint32(1002) // "CQ 999"

	cqLettersBase = uint32(1003)   // c28 = cqLettersBase + base27(suffix)
	cqLettersLo   = uint32(1004)   // "CQ A"    (base27 1)
	cqLettersHi   = uint32(532443) // "CQ ZZZZ" (base27 531440)
)

// TokenToC28 maps a special-token string to its c28 value per QEX
// paper Table 7. Returns (c28, true) for any recognised token form;
// (0, false) for anything that doesn't match a token shape — the
// caller routes such inputs through CallsignC28 / HashedCallC28 or
// reports a parse error at the message-level layer.
//
// Recognised forms (case-sensitive, already-uppercase):
//
//   - "DE", "QRZ", "CQ"               (bare singletons)
//   - "CQ NNN"                        (3 decimal digits, 000..999)
//   - "CQ X" .. "CQ XXXX"             (1-4 uppercase letters)
//
// The "CQ " prefix is exactly three characters (C, Q, single space);
// extra whitespace, lowercase, or unicode are not accepted. The
// caller is expected to have normalised input upstream.
func TokenToC28(s string) (uint32, bool) {
	switch s {
	case "DE":
		return tokenDE, true
	case "QRZ":
		return tokenQRZ, true
	case "CQ":
		return tokenCQ, true
	}
	if !strings.HasPrefix(s, "CQ ") {
		return 0, false
	}
	suffix := s[3:]
	switch {
	case len(suffix) == 3 && allDigits(suffix):
		// Safe: allDigits true means strconv.Atoi cannot fail.
		n, _ := strconv.Atoi(suffix)
		return cqDigitsLo + uint32(n), true
	case len(suffix) >= 1 && len(suffix) <= 4 && allLetters(suffix):
		// Left-space-pad to 4 chars, base-27 encode. Leading pad
		// digits are 0 (space); letter digits are letterIndex(A..Z) =
		// 1..26.
		var v uint32
		pad := 4 - len(suffix)
		for i := 0; i < pad; i++ {
			v *= tokenLettersBase // multiply by 27; current digit = 0
		}
		for i := 0; i < len(suffix); i++ {
			v = v*tokenLettersBase + uint32(suffix[i]-'A'+1)
		}
		return cqLettersBase + v, true
	}
	return 0, false
}

// C28ToToken inverts TokenToC28. Returns (string, true) for any c28
// that lands on a defined token codepoint per QEX paper Table 7;
// (_, false) for c28 ≥ nTokens (not a token at all) and for c28
// values inside the token partition that land on gaps — both the
// inter-row gaps at 1003, 1030, 1732..1759, 20686..21442, and the
// large 532444..nTokens-1 reserved region, AND the intra-row gaps
// inside widths 2/3/4 of the CQ-letters range where the 4-char
// base-27 decode produces an embedded or trailing space.
//
// A (_, false) return for a c28 in [0, nTokens) is the "wire error"
// signal — the c28 came from a corrupted/spec-violating message;
// callers handle this as a decode-side error, not as "not a token".
func C28ToToken(c28 uint32) (string, bool) {
	switch c28 {
	case tokenDE:
		return "DE", true
	case tokenQRZ:
		return "QRZ", true
	case tokenCQ:
		return "CQ", true
	}
	if c28 >= cqDigitsLo && c28 <= cqDigitsHi {
		return cqDigitsFormat(c28 - cqDigitsLo), true
	}
	if c28 < cqLettersLo || c28 > cqLettersHi {
		// Inter-row gaps (c28=1003) and the large reserved gap
		// (532444..nTokens-1) land here. So does c28 ≥ nTokens
		// (caller's bug if reaching here, but handled gracefully).
		return "", false
	}
	return cqLettersDecode(c28 - cqLettersBase)
}

// cqDigitsFormat renders n in [0, 999] as "CQ NNN" with zero-pad.
func cqDigitsFormat(n uint32) string {
	return "CQ " + string([]byte{
		'0' + byte(n/100),
		'0' + byte((n/10)%10),
		'0' + byte(n%10),
	})
}

// cqLettersDecode decodes a 4-char base-27 value into "CQ <suffix>".
// Returns ("", false) if the value's base-27 representation has
// embedded or trailing spaces (intra-row gap — the 4-tuple must read
// as "k leading spaces followed by 4-k letters" with at least one
// letter and no zero digit after a non-zero one).
//
// Precondition: v ∈ [1, 531440] (caller has verified c28 is in the
// CQ-letters row). v == 0 corresponds to the bare "CQ" singleton
// which is handled upstream as c28 = 2.
func cqLettersDecode(v uint32) (string, bool) {
	// Extract 4 base-27 digits. d[0] is the most-significant
	// (leftmost in the 4-char string), d[3] is the least-significant.
	var d [4]byte
	d[3] = byte(v % tokenLettersBase)
	v /= tokenLettersBase
	d[2] = byte(v % tokenLettersBase)
	v /= tokenLettersBase
	d[1] = byte(v % tokenLettersBase)
	v /= tokenLettersBase
	// v ∈ [0, 26] now by partition (max input 531440 = 26*27^3 +
	// 26*27^2 + 26*27 + 26 → after three /27 yields 26).
	d[0] = byte(v)

	// Validity: find the leftmost non-zero digit; everything from
	// there to the right must be non-zero (the "spaces-then-letters,
	// no embedded/trailing space" rule).
	firstLetter := -1
	for i, dig := range d {
		if dig != 0 {
			firstLetter = i
			break
		}
	}
	if firstLetter < 0 {
		// All zeros: bare "CQ" — shouldn't reach here (caller routes
		// c28=2 to the singleton).
		return "", false
	}
	for i := firstLetter; i < 4; i++ {
		if d[i] == 0 {
			return "", false
		}
	}

	// Build the suffix: digits firstLetter..3 are letters A..Z.
	width := 4 - firstLetter
	suffix := make([]byte, width)
	for i := 0; i < width; i++ {
		suffix[i] = 'A' + d[firstLetter+i] - 1
	}
	return "CQ " + string(suffix), true
}

// allDigits reports whether s is non-empty and every byte is in
// '0'..'9'. Used by TokenToC28 to recognise the 3-digit CQ-suffix
// shape. Mirrors allLetters in encode.go.
func allDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
