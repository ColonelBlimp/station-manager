package codec

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
//   - Signed signal report "±N" or "±NN" (sign + 1 or 2 digits,
//     e.g. "-11", "+02", "+2"): encoded as maxGrid4 + (N + g15ReportBias).
//     The FT8 protocol uses reports in [-30, +99] dB; values outside
//     that range are NOT rejected by this function but may collide
//     with reserved-token slots (n=-34..-31 maps to the same stored
//     value as ""/RRR/RR73/73; +0 and -0 map to the same value) —
//     bounds-checking belongs at the message-pack layer above.
//     Forms like "+002" (zero-padding past 2 digits) and "+9999"
//     (out-of-range magnitude) are rejected by the shape check —
//     accept ONLY the canonical sign-plus-1-or-2-digits form so a
//     buggy caller doesn't silently squeak through with a non-
//     canonical encoding.
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
	const op = "codec.Grid4ToG15"

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

	// Signed report path: canonical shape is sign + 1 or 2 digits
	// ('+', '-' followed by [0-9]{1,2}). Looser forms — leading
	// zeros past 2 digits (+002), out-of-range magnitudes (+9999) —
	// are rejected at the shape check rather than slipping through
	// to Atoi and producing a value that may or may not survive
	// the output-range guard. The output-range guard remains as a
	// belt-and-braces secondary check for any future change that
	// loosens the shape rule.
	if isSignedReport(w) {
		n, _ := strconv.Atoi(w) // safe: shape pre-validated
		g15 := maxGrid4 + n + g15ReportBias
		if g15 < 0 || g15 >= 1<<G15Bits {
			panic(op + ": report " + strconv.Quote(w) + " produces out-of-range g15 " + strconv.Itoa(g15))
		}
		return uint16(g15)
	}

	panic(op + ": input " + strconv.Quote(w) + " is not a 4-char grid (A-R/A-R/0-9/0-9), reserved token (\"\", \"RRR\", \"RR73\", \"73\"), or signed report (sign + 1-or-2 digits)")
}

// isSignedReport reports whether s matches the canonical signal-
// report shape: '+' or '-' followed by 1 or 2 ASCII digits.
func isSignedReport(s string) bool {
	if len(s) != 2 && len(s) != 3 {
		return false
	}
	if s[0] != '+' && s[0] != '-' {
		return false
	}
	for i := 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
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

// G25Bits is the width of the FT8 6-character Maidenhead grid code
// per QEX paper Table 2.
const G25Bits = 25

// Grid6ToG25 packs a 6-character Maidenhead grid locator into a
// 25-bit code per QEX paper Table 2 and the public-domain
// `grid6_to_g25.f90` reference in ref [14]. The 6-char grid extends
// the 4-char grid (encoded by Grid4ToG15) with two subfield letters
// for finer geographic resolution:
//
//	chars 1,2: [A-R] (18 letters) — field
//	chars 3,4: [0-9] (10 digits)  — square
//	chars 5,6: [A-X] (24 letters) — subsquare
//
// Output range: [0, 18*18*10*10*24*24) = [0, 18,662,400), which
// fits comfortably in 25 bits (2^25 = 33,554,432).
//
// Unlike g15, the g25 slot has NO reserved-token or signed-report
// multiplexing — it's strictly a 6-char grid encoder. Reserved
// words / reports / shorter grids are not valid inputs.
//
// Case-sensitive: callers must upper-case before invocation.
// Panics on length other than 6 or any character outside its
// position-specific alphabet — these are call-site bugs (the
// message-packer should have validated the grid format upstream).
//
// Returns uint32 even though the result fits in 25 bits — uint16
// is too narrow, and the upper 7 bits are always zero for any
// in-range input.
func Grid6ToG25(w string) uint32 {
	const op = "codec.Grid6ToG25"
	if !isGrid6(w) {
		panic(op + ": input " + strconv.Quote(w) + " is not a 6-char Maidenhead grid ([A-R][A-R][0-9][0-9][A-X][A-X])")
	}
	v := int(w[0]-'A')*18*10*10*24*24 +
		int(w[1]-'A')*10*10*24*24 +
		int(w[2]-'0')*10*24*24 +
		int(w[3]-'0')*24*24 +
		int(w[4]-'A')*24 +
		int(w[5]-'A')
	return uint32(v)
}

// isGrid6 reports whether s matches the 6-char Maidenhead grid
// pattern: [A-R][A-R][0-9][0-9][A-X][A-X].
func isGrid6(s string) bool {
	if len(s) != 6 {
		return false
	}
	return s[0] >= 'A' && s[0] <= 'R' &&
		s[1] >= 'A' && s[1] <= 'R' &&
		s[2] >= '0' && s[2] <= '9' &&
		s[3] >= '0' && s[3] <= '9' &&
		s[4] >= 'A' && s[4] <= 'X' &&
		s[5] >= 'A' && s[5] <= 'X'
}
