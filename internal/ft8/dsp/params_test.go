package dsp

import "testing"

// TestGrayMapInversesGrayUnmap pins the Gray-mapping table integrity:
// GrayMap and GrayUnmap must be inverses, per QEX paper Table 3
// (bi-directional). A drift in either table is the kind of bug that
// produces subtle wrong-bit demodulation in only certain symbols —
// the worst kind to debug. This test catches that drift cheaply.
//
// History: an earlier version of GrayUnmap had positions 5 and 7
// swapped (a copy from a prior research codebase that didn't
// exercise those paths). Caught during demodulator development by
// failing tests; pinned here so it can't come back.
func TestGrayMapInversesGrayUnmap(t *testing.T) {
	for bits := uint8(0); bits < TonesPerSymbol; bits++ {
		tone := GrayMap[bits]
		if got := GrayUnmap[tone]; got != bits {
			t.Errorf("GrayUnmap[GrayMap[%d]] = %d, want %d (GrayMap and GrayUnmap must invert)", bits, got, bits)
		}
	}
	for tone := uint8(0); tone < TonesPerSymbol; tone++ {
		bits := GrayUnmap[tone]
		if got := GrayMap[bits]; got != tone {
			t.Errorf("GrayMap[GrayUnmap[%d]] = %d, want %d (GrayMap and GrayUnmap must invert)", tone, got, tone)
		}
	}
}

// TestGrayMapMatchesQEXTable3 pins the canonical mapping from QEX
// paper Table 3 column 1 (Channel Symbol) ↔ column 2 (FT8 Bits).
// If either table drifts away from the paper's definition,
// interoperability with other FT8 implementations breaks.
func TestGrayMapMatchesQEXTable3(t *testing.T) {
	// Channel symbol → FT8 bits per QEX Table 3.
	want := [8]uint8{
		0, // symbol 0 → 000
		1, // symbol 1 → 001
		3, // symbol 2 → 011
		2, // symbol 3 → 010
		6, // symbol 4 → 110
		4, // symbol 5 → 100
		5, // symbol 6 → 101
		7, // symbol 7 → 111
	}
	for tone := range want {
		if GrayUnmap[tone] != want[tone] {
			t.Errorf("GrayUnmap[%d] = %d, want %d (QEX paper Table 3)", tone, GrayUnmap[tone], want[tone])
		}
	}
}
