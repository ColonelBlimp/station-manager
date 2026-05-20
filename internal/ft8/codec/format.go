package codec

import (
	"fmt"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// FormatMessage renders a Message struct as its on-air human-readable
// FT8 text form. Inverse of ParseMessage; together they form the
// text-layer above the bit-level codec (EncodeMessage / DecodeMessage).
//
// Phase 2D scope: MessageTypeStd only. Other types return
// ErrUnsupportedMessageType; their formatters land alongside their
// respective bit-level packers in Phase 3 / Phase 4.
//
// The output is a single line (no trailing newline) with single-space
// field separators. Conventional FT8 text patterns produced:
//
//	"<Call1> <Call2> <field>"               - canonical 3-field form
//	"<Call1> <Call2>"                       - empty Grid slot, no AckBit
//	"CQ <Call2> <field>"                    - Call1 = CQ token
//	"CQ <suffix> <Call2> <field>"           - Call1 = "CQ <suffix>" token
//	"<Call1>/R <Call2>"                     - Suffix1 set (Type 1 renders as /R)
//	"<Call1> <Call2> R<report>"             - AckBit set with signed report
//	"<Call1> <Call2> R <Grid>"              - AckBit set with locator
//
// Format validates the same fields the encoder would — so a Message
// that round-trips through EncodeMessage can also round-trip through
// FormatMessage with no behavioural divergence.
func FormatMessage(m Message) (string, error) {
	switch m.Type {
	case MessageTypeStd:
		return formatStd(m)
	case MessageTypeEUVHFP:
		return formatEUVHFP(m)
	case MessageTypeEUVHFHash:
		return formatEUVHFHash(m)
	case MessageTypeNonStdCall:
		return formatNonStdCall(m)
	case MessageTypeFreeText:
		return formatFreeText(m)
	default:
		return "", fmt.Errorf("%w: %d", ErrUnsupportedMessageType, m.Type)
	}
}

// formatFreeText renders a Type 0.0 message. The text payload IS the
// rendered output — no field separators, no embellishments. Validation
// mirrors encodeFreeText's gate so any Message that round-trips
// through one round-trips through the other.
func formatFreeText(m Message) (string, error) {
	if err := validateFreeText(m.FreeText); err != nil {
		return "", err
	}
	return m.FreeText, nil
}

// formatStd renders a Type 1 message. See FormatMessage's doc for
// the output shapes. Validation mirrors encodeStd's gate so any
// Message that round-trips through one round-trips through the
// other; see validateType1Suffix's doc for the wire-vs-text-layer
// asymmetry on token+suffix combinations.
func formatStd(m Message) (string, error) {
	if err := validateType1Call(m.Call1, "Call1"); err != nil {
		return "", err
	}
	if err := validateType1Suffix(m.Call1, m.Suffix1, "Call1"); err != nil {
		return "", err
	}
	if err := validateType1Call(m.Call2, "Call2"); err != nil {
		return "", err
	}
	if err := validateType1Suffix(m.Call2, m.Suffix2, "Call2"); err != nil {
		return "", err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return "", err
	}

	call1 := m.Call1
	if m.Suffix1 {
		call1 += "/R"
	}
	call2 := m.Call2
	if m.Suffix2 {
		call2 += "/R"
	}

	field := formatGridField(m.Grid, m.AckBit)
	if field == "" {
		return call1 + " " + call2, nil
	}
	return call1 + " " + call2 + " " + field, nil
}

// formatEUVHFP renders a Type 2 (EU VHF /P) message. Mirrors formatStd
// but emits /P (portable) instead of /R (rover) for the Suffix bit —
// the wire slot is shared between the two types and the rendered
// character is chosen by the Type discriminator. Validation runs
// through validateType2Call, which rejects tokens (Type 2's c28
// partition is std-callsign-only per QEX Table 7).
func formatEUVHFP(m Message) (string, error) {
	if err := validateType2Call(m.Call1, "Call1"); err != nil {
		return "", err
	}
	if err := validateType2Call(m.Call2, "Call2"); err != nil {
		return "", err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return "", err
	}

	call1 := m.Call1
	if m.Suffix1 {
		call1 += "/P"
	}
	call2 := m.Call2
	if m.Suffix2 {
		call2 += "/P"
	}

	field := formatGridField(m.Grid, m.AckBit)
	if field == "" {
		return call1 + " " + call2, nil
	}
	return call1 + " " + call2 + " " + field, nil
}

// formatNonStdCall renders a Type 4 (NonStd Call) message. The
// hashed side renders bracketed (WSJT-X convention); the nonstd side
// renders verbatim; the optional trailing token (Grid field) is one
// of "", "RRR", "RR73", "73".
//
// Three call-slot shapes are accepted, covering the round-trip
// lifecycle:
//
//   - Std-shaped string ("W9XYZ"): operator-typed-encode case;
//     formatter wraps in angle brackets.
//   - "<...>" sentinel: decoder-output case for an unresolved hash;
//     formatter emits verbatim.
//   - Pre-bracketed std-shaped string ("<W9XYZ>"): decoder-output
//     case for a Phase-4-resolved hash; formatter emits verbatim.
//
// Plus "CQ" in Call1 for the c1=1 (CQ-from-nonstd) case, and any
// nonstd c58-alphabet callsign in the non-hashed slot.
//
// The format-side validator is laxer than encode's — encode requires
// a real callsign string in the hashed slot (to compute h12 from
// HashCodes); format just renders whatever shape the Message holds.
// A decoded Message with `Call1="<...>"` round-trips through
// formatNonStdCall but would fail at encodeNonStdCall (Phase 4's
// hash-table lookup resolves it before re-encoding).
func formatNonStdCall(m Message) (string, error) {
	const op errors.Op = "codec.formatNonStdCall"

	if _, ok := gridToR2(m.Grid); !ok {
		return "", errors.New(op).WithMsgf("Grid = %q is not a valid Type 4 token; allowed values are \"\", \"RRR\", \"RR73\", \"73\"", m.Grid)
	}

	call1, err := renderType4Call(m.Call1, "Call1")
	if err != nil {
		return "", err
	}
	call2, err := renderType4Call(m.Call2, "Call2")
	if err != nil {
		return "", err
	}

	if m.Grid == "" {
		return call1 + " " + call2, nil
	}
	return call1 + " " + call2 + " " + m.Grid, nil
}

// renderType4Call maps a Message call slot to its display form for
// Type 4 output. See formatNonStdCall's doc for the accepted shapes.
func renderType4Call(call, field string) (string, error) {
	const op errors.Op = "codec.formatNonStdCall"
	if call == "" {
		return "", errors.New(op).WithMsgf("%s is empty", field)
	}
	if call == hashedCallSentinel || call == "CQ" {
		return call, nil
	}
	if len(call) >= 2 && call[0] == '<' && call[len(call)-1] == '>' {
		inner := call[1 : len(call)-1]
		if !isStdCallsignShape(inner) {
			return "", errors.New(op).WithMsgf("%s = %q is bracketed but inner content %q is not a standard callsign", field, call, inner)
		}
		return call, nil
	}
	if isStdCallsignShape(call) {
		return "<" + call + ">", nil
	}
	if isType4ValidNonStdCall(call) {
		return call, nil
	}
	return "", errors.New(op).WithMsgf("%s = %q is not a valid Type 4 call form (expected CQ, <hashed>, std callsign, or nonstd callsign)", field, call)
}

// formatEUVHFHash renders a Type 5 (EU VHF hashes+g25) message. The
// canonical form per QEX paper Table 1 example is:
//
//	<call1> <call2> [R ]<report><serial> <grid6>
//
// e.g. "<G4ABC> <PA9XYZ> R 570007 JO22DB" — both calls bracketed
// (the wire carries hashes for both sides, so WSJT-X convention
// brackets the display form whether or not the call is resolved),
// optional R ack prefix, report+serial concatenated as one token
// (report = "52".."59" two chars, serial zero-padded to 4 chars),
// then the 6-char grid.
//
// Call rendering parallels Type 4's renderType4Call: a "<...>"
// sentinel passes through verbatim (unresolved hash), a pre-bracketed
// std call passes through (resolved hash from Phase 4), and a bare
// std-callsign string gets bracketed.
func formatEUVHFHash(m Message) (string, error) {
	if err := validateType5Report(m.Report3); err != nil {
		return "", err
	}
	if err := validateType5Serial(m.Serial); err != nil {
		return "", err
	}
	if err := validateType5Grid(m.Grid6); err != nil {
		return "", err
	}
	call1, err := renderType5Call(m.Call1, "Call1")
	if err != nil {
		return "", err
	}
	call2, err := renderType5Call(m.Call2, "Call2")
	if err != nil {
		return "", err
	}

	// Report + serial fuse into one token. Report displays as
	// "52".."59" (two chars); serial zero-pads to 4 chars (s11 max
	// 2047 fits in 4 decimal digits).
	reportStr := strconv.Itoa(int(m.Report3) + r3Bias)
	serialStr := strconv.Itoa(int(m.Serial))
	for len(serialStr) < 4 {
		serialStr = "0" + serialStr
	}
	body := reportStr + serialStr

	if m.AckBit {
		return call1 + " " + call2 + " R " + body + " " + m.Grid6, nil
	}
	return call1 + " " + call2 + " " + body + " " + m.Grid6, nil
}

// renderType5Call maps a Message call slot to its Type 5 display form.
// Mirrors renderType4Call's three-shape acceptance (sentinel,
// pre-bracketed, bare std) but always emits the bracketed form (Type
// 5 hashes both sides on the wire, so WSJT-X display brackets both).
func renderType5Call(call, field string) (string, error) {
	const op errors.Op = "codec.formatEUVHFHash"
	if call == "" {
		return "", errors.New(op).WithMsgf("%s is empty", field)
	}
	if call == hashedCallSentinel {
		return call, nil
	}
	if len(call) >= 2 && call[0] == '<' && call[len(call)-1] == '>' {
		inner := call[1 : len(call)-1]
		if !isStdCallsignShape(inner) {
			return "", errors.New(op).WithMsgf("%s = %q is bracketed but inner content %q is not a standard callsign", field, call, inner)
		}
		return call, nil
	}
	if isStdCallsignShape(call) {
		return "<" + call + ">", nil
	}
	return "", errors.New(op).WithMsgf("%s = %q is not a valid Type 5 call form (expected <hashed sentinel>, std callsign, or pre-bracketed std callsign)", field, call)
}

// formatGridField renders the g15 slot's text form with the AckBit
// prefix applied per the conventional FT8 patterns.
//
//	AckBit=0, Grid=""        →  ""              (no field)
//	AckBit=0, Grid="IO91"    →  "IO91"
//	AckBit=0, Grid="-11"     →  "-11"
//	AckBit=0, Grid="RR73"    →  "RR73"
//	AckBit=1, Grid=""        →  "R"             (bare ack, no g15 token)
//	AckBit=1, Grid="-11"     →  "R-11"          (R fused with report)
//	AckBit=1, Grid="IO91"    →  "R IO91"        (R separated from grid)
//	AckBit=1, Grid="RR73"    →  "R RR73"        (R separated from reserved)
//
// The fused-vs-separated split matches WSJT-X's display convention:
// reports get R as an inline prefix ("R-09"), grids and reserved
// tokens get R as a separate field. The parser inverts this on the
// way back.
func formatGridField(grid string, ack bool) string {
	if !ack {
		return grid
	}
	if grid == "" {
		return "R"
	}
	if isSignedReport(grid) {
		return "R" + grid
	}
	return "R " + grid
}
