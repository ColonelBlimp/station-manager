package codec

// Type 3 (RTTY Roundup) Layer 1 primitive for the s13 exchange slot.
// The 13-bit field is multi-modal per QEX Appendix A: serial number
// in [0, 7999], state/province abbreviation in [8001, 8065], or
// unassigned otherwise. The state/province lookup table is verbatim
// from the QEX ref [14] tarball's states_provinces.txt (public
// domain per QEX paper §9), 65 entries: 50 US states + 5 territories
// (DC, etc.) + 13 Canadian provinces/territories + LB.
//
// The s13 partition exists only in Type 3 — Type 0.3 / 0.4 Field
// Day uses different exchange slots (k3 + n4 + S7). Layer 2's
// encodeRTTYRoundup / decodeRTTYRoundup are the only callers.

// rttyRoundupStates is the state/province lookup table from QEX
// ref [14] states_provinces.txt. Order is significant: the index
// IS the encoding offset within the [s13StateBase, ...) wire range.
// First 50 entries: US states in standard 2-letter abbreviation
// order. Next 5: territorial / non-state codes (NB through NF here
// is the Fortran data file's verbatim order). Last 10: Canadian
// territories + DC. NWT and PEI are 3-char codes; the rest are 2.
//
// Editing this table changes wire-format semantics — only update
// to match QEX ref [14] revisions, never for operator preference.
var rttyRoundupStates = [...]string{
	"AL", "AK", "AZ", "AR", "CA", "CO", "CT", "DE", "FL", "GA",
	"HI", "ID", "IL", "IN", "IA", "KS", "KY", "LA", "ME", "MD",
	"MA", "MI", "MN", "MS", "MO", "MT", "NE", "NV", "NH", "NJ",
	"NM", "NY", "NC", "ND", "OH", "OK", "OR", "PA", "RI", "SC",
	"SD", "TN", "TX", "UT", "VT", "VA", "WA", "WV", "WI", "WY",
	"NB", "NS", "QC", "ON", "MB", "SK", "AB", "BC", "NWT", "NF",
	"LB", "NU", "YT", "PEI", "DC",
}

// S13Kind discriminates the three regions the s13 slot's value
// can land in. Returned by S13ToExchange so the decoder knows
// which Message field to populate (Serial vs StateProvince).
type S13Kind int

const (
	// S13KindUnknown is the zero value; never returned by
	// S13ToExchange on real input.
	S13KindUnknown S13Kind = iota

	// S13KindSerial indicates s13 ∈ [0, s13SerialMax] — a contest-
	// style serial number 0..7999.
	S13KindSerial

	// S13KindState indicates s13 ∈ [s13StateBase, s13StateBase +
	// len(rttyRoundupStates)) — a US state or Canadian province
	// abbreviation from rttyRoundupStates.
	S13KindState

	// S13KindUnassigned indicates s13 lands in one of the gap
	// codepoints (s13=8000 between serial and state ranges, or
	// s13 > 8065 above the state range). The encoder never emits
	// these; receiver-side gets ErrInvalidS13 from Layer 2.
	S13KindUnassigned
)

// SerialToS13 maps a serial number 0..7999 to its s13 wire value.
// Returns ok=false for serial > s13SerialMax (the Type 3 RTTY
// Roundup serial slot is bounded at 7999, narrower than the wire
// field's 0..8191).
func SerialToS13(serial uint16) (uint16, bool) {
	if serial > s13SerialMax {
		return 0, false
	}
	return serial, true
}

// StateToS13 looks up a US state / Canadian province abbreviation
// in the rttyRoundupStates table and returns its s13 wire value.
// Match is case-sensitive against the table — callers normalise
// upstream. ok=false when the input isn't in the table.
func StateToS13(state string) (uint16, bool) {
	for i, s := range rttyRoundupStates {
		if s == state {
			return uint16(s13StateBase + i), true
		}
	}
	return 0, false
}

// S13ToExchange inverts SerialToS13 and StateToS13. Returns one
// of three shapes per the kind discriminator:
//
//   - S13KindSerial: serial holds the 0..7999 value; state == "".
//   - S13KindState:  state holds the abbreviation; serial == 0.
//   - S13KindUnassigned: both zero/empty.
//
// Wire-faithful: returns Unassigned for the gap codepoints
// (s13=8000 and s13 in [s13StateBase + len(rttyRoundupStates),
// 1<<s13Bits)). Layer 2 surfaces those as ErrInvalidS13.
func S13ToExchange(s13 uint16) (serial uint16, state string, kind S13Kind) {
	if s13 <= s13SerialMax {
		return s13, "", S13KindSerial
	}
	idx := int(s13) - s13StateBase
	if idx >= 0 && idx < len(rttyRoundupStates) {
		return 0, rttyRoundupStates[idx], S13KindState
	}
	return 0, "", S13KindUnassigned
}
