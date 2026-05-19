package codec

import "fmt"

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
//	"<Call1>/R <Call2>"                     - Rover1 suffix
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
	default:
		return "", fmt.Errorf("%w: %d", ErrUnsupportedMessageType, m.Type)
	}
}

// formatStd renders a Type 1 message. See FormatMessage's doc for
// the output shapes. Validation mirrors encodeStd's gate so any
// Message that round-trips through one round-trips through the
// other; see validateType1Rover's doc for the wire-vs-text-layer
// asymmetry on token+rover combinations.
func formatStd(m Message) (string, error) {
	if err := validateType1Call(m.Call1, "Call1"); err != nil {
		return "", err
	}
	if err := validateType1Rover(m.Call1, m.Rover1, "Call1"); err != nil {
		return "", err
	}
	if err := validateType1Call(m.Call2, "Call2"); err != nil {
		return "", err
	}
	if err := validateType1Rover(m.Call2, m.Rover2, "Call2"); err != nil {
		return "", err
	}
	if err := validateG15Slot(m.Grid); err != nil {
		return "", err
	}

	call1 := m.Call1
	if m.Rover1 {
		call1 += "/R"
	}
	call2 := m.Call2
	if m.Rover2 {
		call2 += "/R"
	}

	field := formatGridField(m.Grid, m.AckBit)
	if field == "" {
		return call1 + " " + call2, nil
	}
	return call1 + " " + call2 + " " + field, nil
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
