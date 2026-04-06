// Package message
// FT8/FT4 15-bit grid/report field encoding and decoding.
//
// The 15-bit igrid4 field in a standard FT8 message (type 1/2) is overloaded
// to encode one of several data types:
//
//	0..32,399          4-char Maidenhead grid (AA00..RR99), mixed-radix 18×18×10×10
//	32,401             empty (no grid/report)
//	32,402             RRR
//	32,403             RR73
//	32,404             73
//	32,405..32,465     signal report: dB = igrid4 − MaxGrid4 − 35 (range −30..+30)
//	32,400, 32,466+    unused/reserved
//
// A separate 1-bit ir (Roger) flag modifies grid and report values:
//   - ir=true with a grid:   "R " prefix before the grid (e.g. "R FN31")
//   - ir=true with a report: "R" prefix before the report (e.g. "R-08")
//   - ir is ignored for tokens (RRR, RR73, 73) and empty
//
// Reference: ft8_lib ft8/message.c packgrid()/unpackgrid(), WSJT-X lib/ft8/pack77.f90.
package message

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// --- Grid constants ----------------------------------------------------------

// MaxGrid4 is the number of distinct 4-char Maidenhead grid encodings:
// 18 × 18 × 10 × 10 = 32,400. Valid grid values are 0..MaxGrid4-1.
// Defined as MAXGRID4 = 32400 in ft8_lib (message.c line 11).
const MaxGrid4 uint16 = 32400

// Grid field tokens (above MaxGrid4).
const (
	gridEmpty uint16 = MaxGrid4 + 1 // no grid or report
	gridRRR   uint16 = MaxGrid4 + 2 // "RRR"
	gridRR73  uint16 = MaxGrid4 + 3 // "RR73"
	grid73    uint16 = MaxGrid4 + 4 // "73"
)

// reportBias is the offset added to a dB value to produce the report sub-field.
// igrid4 = MaxGrid4 + reportBias + dB, where dB ∈ [ReportMin, ReportMax].
//
// The bias value 35 ensures that the minimum report (−30 dB) maps to
// MaxGrid4 + 5, one above the last token (MaxGrid4 + 4 = "73"), avoiding
// overlap with the token region (MaxGrid4 + 1..4).
const reportBias = 35

// Signal report range (dB).
const (
	ReportMin = -30 // minimum encodable signal report
	ReportMax = 30  // maximum encodable signal report
)

// --- Encode functions --------------------------------------------------------

// EncodeGrid packs a 4-character Maidenhead grid locator into a 15-bit igrid4
// value. The grid must be in the form [A-R][A-R][0-9][0-9] (case-insensitive).
//
// The encoding uses mixed-radix 18 × 18 × 10 × 10, matching ft8_lib's
// packgrid() for valid 4-char grids.
func EncodeGrid(grid4 string) (uint16, error) {
	const op errors.Op = "message.EncodeGrid"

	grid4 = strings.ToUpper(strings.TrimSpace(grid4))
	if len(grid4) != 4 {
		return 0, errors.New(op).Msgf("grid must be exactly 4 characters, got %d", len(grid4))
	}

	f1, f2, d1, d2 := grid4[0], grid4[1], grid4[2], grid4[3]

	if f1 < 'A' || f1 > 'R' {
		return 0, errors.New(op).Msgf("field 1 %q not in range A–R", f1)
	}
	if f2 < 'A' || f2 > 'R' {
		return 0, errors.New(op).Msgf("field 2 %q not in range A–R", f2)
	}
	if d1 < '0' || d1 > '9' {
		return 0, errors.New(op).Msgf("digit 1 %q not in range 0–9", d1)
	}
	if d2 < '0' || d2 > '9' {
		return 0, errors.New(op).Msgf("digit 2 %q not in range 0–9", d2)
	}

	igrid4 := uint16(f1 - 'A')
	igrid4 = igrid4*18 + uint16(f2-'A')
	igrid4 = igrid4*10 + uint16(d1-'0')
	igrid4 = igrid4*10 + uint16(d2-'0')

	return igrid4, nil
}

// EncodeReport packs a signal report (dB) into a 15-bit igrid4 value.
// The report must be in range [ReportMin, ReportMax] (−30..+30).
//
// The ir (Roger) flag is not set by this function — the caller must track it
// separately for R-prefixed reports.
func EncodeReport(db int) (uint16, error) {
	const op errors.Op = "message.EncodeReport"

	if db < ReportMin || db > ReportMax {
		return 0, errors.New(op).Msgf("report %d dB out of range [%d, %d]",
			db, ReportMin, ReportMax)
	}

	return uint16(int(MaxGrid4) + reportBias + db), nil
}

// EncodeEmpty returns the igrid4 value for an empty grid/report field.
func EncodeEmpty() uint16 { return gridEmpty }

// EncodeRRR returns the igrid4 value for "RRR".
func EncodeRRR() uint16 { return gridRRR }

// EncodeRR73 returns the igrid4 value for "RR73".
func EncodeRR73() uint16 { return gridRR73 }

// Encode73 returns the igrid4 value for "73".
func Encode73() uint16 { return grid73 }

// EncodeGridField parses the third field of a standard FT8 message and returns
// the 15-bit igrid4 value and the 1-bit ir (Roger) flag.
//
// Accepted input formats (case-insensitive):
//   - "" → empty
//   - "RRR", "RR73", "73" → special tokens (ir=false)
//   - "AA00".."RR99" → 4-char Maidenhead grid (ir=false)
//   - "R AA00".."R RR99" → Roger + grid (ir=true)
//   - "+dd" / "-dd" (e.g. "+05", "-12") → signal report (ir=false)
//   - "R+dd" / "R-dd" → Roger + signal report (ir=true)
//
// This combines the functionality of ft8_lib's packgrid() with the "R " grid
// prefix handling that packgrid() leaves as a TODO.
func EncodeGridField(extra string) (igrid4 uint16, ir bool, err error) {
	const op errors.Op = "message.EncodeGridField"

	extra = strings.ToUpper(strings.TrimSpace(extra))

	// Empty.
	if extra == "" {
		return gridEmpty, false, nil
	}

	// Exact token matches.
	switch extra {
	case "RRR":
		return gridRRR, false, nil
	case "RR73":
		return gridRR73, false, nil
	case "73":
		return grid73, false, nil
	}

	// Roger + grid: "R " followed by a 4-char grid.
	if len(extra) == 6 && extra[0] == 'R' && extra[1] == ' ' {
		ig, gridErr := EncodeGrid(extra[2:])
		if gridErr != nil {
			return 0, false, errors.New(op).Err(gridErr).Msg(gridErr.Error())
		}
		return ig, true, nil
	}

	// Plain 4-char grid.
	if len(extra) == 4 {
		ig, gridErr := EncodeGrid(extra)
		if gridErr == nil {
			return ig, false, nil
		}
	}

	// Roger + report: "R" followed by a signed number (e.g. "R+05", "R-12").
	if len(extra) > 1 && extra[0] == 'R' && (extra[1] == '+' || extra[1] == '-') {
		db, parseErr := strconv.Atoi(extra[1:])
		if parseErr == nil {
			ig, encErr := EncodeReport(db)
			if encErr == nil {
				return ig, true, nil
			}
			return 0, false, errors.New(op).Err(encErr).Msg(encErr.Error())
		}
	}

	// Plain report: signed number (e.g. "+05", "-12").
	if len(extra) > 0 && (extra[0] == '+' || extra[0] == '-') {
		db, parseErr := strconv.Atoi(extra)
		if parseErr == nil {
			ig, encErr := EncodeReport(db)
			if encErr == nil {
				return ig, false, nil
			}
			return 0, false, errors.New(op).Err(encErr).Msg(encErr.Error())
		}
	}

	return 0, false, errors.New(op).Msgf("unrecognized grid/report %q", extra)
}

// --- Decode function ---------------------------------------------------------

// DecodeGridField unpacks a 15-bit igrid4 value and 1-bit ir (Roger) flag into
// the third field string of a standard FT8 message.
//
// Return values by region:
//   - Grid (0..MaxGrid4-1): "FN31" or "R FN31" (if ir=true)
//   - Empty (MaxGrid4+1): ""
//   - Token (MaxGrid4+2..+4): "RRR", "RR73", "73"
//   - Report (MaxGrid4+5..+65): "+05", "-12", or "R+05", "R-12" (if ir=true)
func DecodeGridField(igrid4 uint16, ir bool) (string, error) {
	const op errors.Op = "message.DecodeGridField"

	// Grid region: 0..MaxGrid4-1.
	if igrid4 < MaxGrid4 {
		grid := decodeGrid(igrid4)
		if ir {
			return "R " + grid, nil
		}
		return grid, nil
	}

	irpt := igrid4 - MaxGrid4

	// Token region.
	switch irpt {
	case 1:
		return "", nil // empty
	case 2:
		return "RRR", nil
	case 3:
		return "RR73", nil
	case 4:
		return "73", nil
	}

	// Reserved/unused gap: irpt == 0 corresponds to igrid4 == MaxGrid4 (32400),
	// which the FT8 protocol defines as unused. Distinguish it from values
	// that overshoot the report region so callers can diagnose the cause.
	if irpt == 0 {
		return "", errors.New(op).Msgf("igrid4 %d is reserved/unused by the FT8 protocol", igrid4)
	}

	// Signal report region.
	db := int(irpt) - reportBias
	if db < ReportMin || db > ReportMax {
		return "", errors.New(op).Msgf("igrid4 %d out of valid range", igrid4)
	}

	// Format as sign + 2 zero-padded digits (matching ft8_lib int_to_dd with
	// width=2, full_sign=true): "+05", "-12", "+00", "+30", "-30".
	report := fmt.Sprintf("%+03d", db)
	if ir {
		return "R" + report, nil
	}
	return report, nil
}

// --- Internal helpers --------------------------------------------------------

// decodeGrid reverses the mixed-radix 18×18×10×10 encoding for a 4-char grid.
func decodeGrid(igrid4 uint16) string {
	n := igrid4
	d2 := n % 10
	n /= 10
	d1 := n % 10
	n /= 10
	f2 := n % 18
	n /= 18
	f1 := n

	return string([]byte{
		byte('A' + f1),
		byte('A' + f2),
		byte('0' + d1),
		byte('0' + d2),
	})
}
