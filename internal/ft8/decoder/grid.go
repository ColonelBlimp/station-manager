package decoder

import "strconv"

// G15Bits is the width of the FT8 grid+report code per QEX paper §A:
//
//	"Similarly there are 180×180 = 32,400 four-digit Maidenhead grid
//	locators. This number is less than 2^15 = 32,768, so a grid code
//	can be uniquely represented in 15 bits. Some of the 368 fifteen-
//	bit values not needed for grid locators are used to convey
//	numerical signal reports of the form ±nn in the range -30 to
//	+99 dB, or a blank, or one of the words RRR, RR73, or 73."
const G15Bits = 15

// Reserved g15 values per QEX paper Table 2 and the
// `grid4_to_g15.f90` reference. The g15 slot multiplexes a 4-char
// Maidenhead grid (0..maxGrid4-1) with five reserved tokens and a
// signed-report band above maxGrid4.
const (
	maxGrid4      = 18 * 18 * 10 * 10 // 32400 — first non-grid g15 value
	g15Empty      = maxGrid4 + 1      // 32401: blank message word
	g15RRR        = maxGrid4 + 2      // 32402: "RRR"
	g15RR73       = maxGrid4 + 3      // 32403: "RR73"
	g15_73        = maxGrid4 + 4      // 32404: "73"
	g15ReportBias = 35                // signed-report offset: stored = maxGrid4 + (n + bias)
)

// Grid4ToG15 packs a g15 message fragment per QEX paper Table 2 and
// the public-domain `grid4_to_g15.f90` reference in ref [14]. The
// g15 slot carries one of:
//
//   - 4-character Maidenhead grid locator (e.g. "FN20"): encoded as
//     a base-(18,18,10,10) integer in [0, maxGrid4).
//   - Reserved tokens "" / "RRR" / "RR73" / "73": fixed values
//     maxGrid4+1..maxGrid4+4.
//   - Signed signal report "±NN" (e.g. "-11", "+02"): encoded as
//     maxGrid4 + (N + g15ReportBias). The FT8 protocol uses reports
//     in [-30, +99] dB; values outside that range are NOT rejected
//     by this function but may collide with reserved-token slots
//     (n=-34..-31 maps to the same stored value as ""/RRR/RR73/73)
//     — bounds-checking belongs at the message-pack layer above.
//
// Special case: the string "RR73" matches the 4-char-grid letter/
// letter/digit/digit pattern, but the reference algorithm
// short-circuits "RR73" to its reserved code instead of treating it
// as a Maidenhead locator. Without the short-circuit, a station
// signing off with "RR73" would be miscoded.
//
// Inputs are case-sensitive: callers must upper-case before
// invocation. Unrecognised inputs panic with a clear diagnostic.
// Output range invariant (g15 < 2^15) is enforced as a final guard.
//
// Returns uint16 even though the result fits in 15 bits — uint8 is
// too narrow, and the upper bit will be zero for any in-range input.
func Grid4ToG15(w string) uint16 {
	const op = "decoder.Grid4ToG15"

	// 4-char Maidenhead path. RR73 is short-circuited to its
	// reserved token rather than encoded as a grid even though it
	// passes the shape check.
	if isGrid4(w) && w != "RR73" {
		v := int(w[0]-'A')*18*10*10 +
			int(w[1]-'A')*10*10 +
			int(w[2]-'0')*10 +
			int(w[3]-'0')
		return uint16(v)
	}

	// Reserved tokens.
	switch w {
	case "":
		return g15Empty
	case "RRR":
		return g15RRR
	case "RR73":
		return g15RR73
	case "73":
		return g15_73
	}

	// Signed report path: must start with '+' or '-' and parse as an
	// integer. Anything else falls through to the panic.
	if len(w) >= 2 && (w[0] == '+' || w[0] == '-') {
		n, err := strconv.Atoi(w)
		if err == nil {
			g15 := maxGrid4 + n + g15ReportBias
			if g15 < 0 || g15 >= 1<<G15Bits {
				panic(op + ": report " + strconv.Quote(w) + " produces out-of-range g15 " + strconv.Itoa(g15))
			}
			return uint16(g15)
		}
	}

	panic(op + ": input " + strconv.Quote(w) + " is not a 4-char grid (A-R/A-R/0-9/0-9), reserved token (\"\", \"RRR\", \"RR73\", \"73\"), or signed report (\"±NN\")")
}

// isGrid4 reports whether s matches the 4-char Maidenhead grid
// pattern: [A-R][A-R][0-9][0-9].
func isGrid4(s string) bool {
	if len(s) != 4 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'R' &&
		s[1] >= 'A' && s[1] <= 'R' &&
		s[2] >= '0' && s[2] <= '9' &&
		s[3] >= '0' && s[3] <= '9'
}
