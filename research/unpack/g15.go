package unpack

import "fmt"

// g15 encoding constants per QEX ref [14] grid4_to_g15.f90.
// The 15-bit space is partitioned:
//
//	[0, MAXGRID4)           4-character Maidenhead grid square
//	MAXGRID4 + 1            blank report (g15 == 32401)
//	MAXGRID4 + 2            "RRR"
//	MAXGRID4 + 3            "RR73"
//	MAXGRID4 + 4            "73"
//	MAXGRID4 + 5..32767     numeric report; report = (irpt - 35) dB,
//	                        with irpt = g15 - MAXGRID4 in [5, 367].
//	                        Standard reports are -30..+30, so irpt in [5, 65].
const g15MaxGrid4 uint16 = 32400

// decodeG15 returns the grid/report/special string encoded by a
// 15-bit g15 value. The output is what appears as the third slot
// of a Type 1 message ("EXTRA" in "CALL1 CALL2 EXTRA"). Returns
// "" for the blank-report case so the caller can omit the trailing
// space entirely.
func decodeG15(g uint16) (string, error) {
	if g < g15MaxGrid4 {
		return decodeGrid4(g), nil
	}
	irpt := g - g15MaxGrid4
	switch irpt {
	case 0:
		return "", fmt.Errorf("g15: reserved value MAXGRID4+0")
	case 1:
		return "", nil
	case 2:
		return "RRR", nil
	case 3:
		return "RR73", nil
	case 4:
		return "73", nil
	default:
		// Numeric report: irpt = report + 35.
		report := int(irpt) - 35
		if report >= 0 {
			return fmt.Sprintf("+%02d", report), nil
		}
		return fmt.Sprintf("-%02d", -report), nil
	}
}

// decodeGrid4 expands a 4-character Maidenhead grid value back into
// its "AANN" string form. The encoder uses
//
//	g = (j1)*1800 + (j2)*100 + (j3)*10 + j4
//
// where j1, j2 are letters A..R (18 options each) and j3, j4 are
// digits 0..9. Inverting:
//
//	j1 = g / 1800
//	j2 = (g % 1800) / 100
//	j3 = (g % 100) / 10
//	j4 = g % 10
//
// caller guarantees g < g15MaxGrid4, so j1 ∈ [0, 17] and the output
// is always 4 valid characters.
func decodeGrid4(g uint16) string {
	j1 := g / 1800
	r := g % 1800
	j2 := r / 100
	r %= 100
	j3 := r / 10
	j4 := r % 10
	return string([]byte{
		byte('A' + j1),
		byte('A' + j2),
		byte('0' + j3),
		byte('0' + j4),
	})
}
