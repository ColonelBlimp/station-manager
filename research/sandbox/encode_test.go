package sandbox

import (
	"testing"
)

// TestEncodeLDPC_AllZeroInfoYieldsAllZeroCodeword pins the property
// that a zero-input gives a zero-output: every parity bit is the XOR
// of some subset of zeros, so all parity bits are zero. This is the
// minimum sanity check on the generator matrix parser and the encode
// loop.
func TestEncodeLDPC_AllZeroInfoYieldsAllZeroCodeword(t *testing.T) {
	var info [LDPCInfoBits]uint8
	cw := EncodeLDPC(info)
	for i, b := range cw {
		if b != 0 {
			t.Errorf("cw[%d] = %d, want 0", i, b)
			break
		}
	}
}

// TestEncodeLDPC_SystematicProperty pins that EncodeLDPC produces a
// systematic codeword: codeword[0:91] == info verbatim, regardless
// of the info bit pattern. The first 91 bits of the codeword carry
// the original info, the last 83 bits carry the computed parity.
func TestEncodeLDPC_SystematicProperty(t *testing.T) {
	var info [LDPCInfoBits]uint8
	// Mix some 0s and 1s so the test would catch a bit-reversal or
	// alignment bug.
	for i := range info {
		if i%3 == 0 || i%7 == 0 {
			info[i] = 1
		}
	}
	cw := EncodeLDPC(info)
	for i := 0; i < LDPCInfoBits; i++ {
		if cw[i] != info[i] {
			t.Errorf("cw[%d] = %d, want info[%d] = %d (systematic violation)",
				i, cw[i], i, info[i])
		}
	}
}

// TestEncodeLDPC_SatisfiesParityChecks pins that any codeword produced
// by EncodeLDPC satisfies all 83 parity-check equations of the
// LDPC graph. This validates that the generator and parity-check
// matrices are mutually consistent — H · cw = 0 for every cw in the
// image of EncodeLDPC.
func TestEncodeLDPC_SatisfiesParityChecks(t *testing.T) {
	// Two non-trivial info patterns; the LDPC code's algebraic
	// consistency must hold on each.
	patterns := [][LDPCInfoBits]uint8{}
	{
		var p1 [LDPCInfoBits]uint8
		for i := range p1 {
			p1[i] = uint8(i % 2)
		}
		patterns = append(patterns, p1)
	}
	{
		var p2 [LDPCInfoBits]uint8
		for i := range p2 {
			if i%5 == 0 {
				p2[i] = 1
			}
		}
		patterns = append(patterns, p2)
	}
	for idx, info := range patterns {
		cw := EncodeLDPC(info)
		for c := 0; c < LDPCParityRows; c++ {
			var x uint8
			for _, v := range checkVars[c] {
				x ^= cw[v]
			}
			if x != 0 {
				t.Errorf("pattern %d: parity check %d failed (H · cw ≠ 0)", idx, c)
				break
			}
		}
	}
}

// TestCodewordToTones_CostasPositions pins that the 21 Costas anchor
// positions always hold the literal Costas tones [3,1,4,0,6,5,2],
// regardless of the codeword's data bits.
func TestCodewordToTones_CostasPositions(t *testing.T) {
	// Use an arbitrary non-zero codeword.
	var cw [LDPCCodewordBits]uint8
	for i := range cw {
		cw[i] = uint8(i % 2)
	}
	tones := CodewordToTones(cw)
	expected := [7]int{3, 1, 4, 0, 6, 5, 2}
	for _, blockStart := range costasBlockStarts {
		for k := 0; k < 7; k++ {
			if tones[blockStart+k] != expected[k] {
				t.Errorf("Costas position %d: tone = %d, want %d",
					blockStart+k, tones[blockStart+k], expected[k])
			}
		}
	}
}

// TestCodewordToTones_DataTonesUseGrayMap pins that data symbol tones
// come from the forward Gray map applied to consecutive 3-bit codeword
// groups. Builds a codeword with known bit patterns at the first data
// symbol and checks the resulting tone.
func TestCodewordToTones_DataTonesUseGrayMap(t *testing.T) {
	// Data symbol 0 = symbol index 7 (first non-Costas).
	// Codeword bits 0..2 carry symbol 0's 3-bit value.
	cases := []struct {
		bits     [3]uint8
		wantTone int
	}{
		{[3]uint8{0, 0, 0}, 0},
		{[3]uint8{0, 0, 1}, 1},
		{[3]uint8{0, 1, 1}, 2},
		{[3]uint8{0, 1, 0}, 3},
		{[3]uint8{1, 0, 0}, 5},
		{[3]uint8{1, 0, 1}, 6},
		{[3]uint8{1, 1, 0}, 4},
		{[3]uint8{1, 1, 1}, 7},
	}
	for _, tc := range cases {
		var cw [LDPCCodewordBits]uint8
		cw[0] = tc.bits[0]
		cw[1] = tc.bits[1]
		cw[2] = tc.bits[2]
		tones := CodewordToTones(cw)
		got := tones[7] // first data-symbol slot
		if got != tc.wantTone {
			t.Errorf("bits %v → tone %d, want %d", tc.bits, got, tc.wantTone)
		}
	}
}

// TestForwardGrayMap_InvertsInverseGrayMap pins the algebraic
// identity that ties the encoder's forward map to the decoder's
// inverse map: forwardGrayMap[inverseGrayMap[t]] = t for every tone
// t in [0, 8). A single XOR test would suffice for Gray-code shape,
// but the direct round-trip is the actual property the encoder needs.
func TestForwardGrayMap_InvertsInverseGrayMap(t *testing.T) {
	for tone := 0; tone < 8; tone++ {
		bits := inverseGrayMap[tone]
		recovered := forwardGrayMap[bits]
		if recovered != tone {
			t.Errorf("tone %d → bits %d → tone %d (round-trip broken)",
				tone, bits, recovered)
		}
	}
}
