package sandbox

import (
	_ "embed"
	"fmt"
	"strings"
)

//go:embed generator.dat
var sandboxGeneratorDat string

// generatorMatrix is the 83 × 91 LDPC generator matrix from QEX paper
// ref [14] generator.dat. Each row i specifies the info-bit pattern
// whose XOR produces parity bit i: parity[i] = ⊕_{j: g[i][j]==1} info[j].
//
// Sparse representation: generatorRowOnes[i] lists the info-bit
// indices where row i has a 1. Faster than scanning every bit for the
// per-row XOR.
var generatorRowOnes [LDPCParityRows][]int

// forwardGrayMap converts a 3-bit codeword triplet (MSB-first integer
// in [0, 8)) into the FT8 8-FSK tone index. The inverse of
// inverseGrayMap from metrics.go (which carries tone → bits).
//
//	bits 000 → tone 0    bits 100 → tone 5
//	bits 001 → tone 1    bits 101 → tone 6
//	bits 011 → tone 2    bits 110 → tone 4
//	bits 010 → tone 3    bits 111 → tone 7
//
// Adjacent tones differ by exactly one bit (Gray code), so a single
// tone error in demodulation flips at most one codeword bit.
var forwardGrayMap = [8]int{0, 1, 3, 2, 5, 6, 4, 7}

// costasTones is the 7-symbol Costas sync array placed at the start,
// middle, and end of an FT8 message. Same constant as costasArray in
// candidates.go — duplicated here as a typed [7]int for the encoder
// path's consumption convention.
var costasTones = [7]int{3, 1, 4, 0, 6, 5, 2}

func init() {
	parseSandboxGenerator()
}

// parseSandboxGenerator reads the embedded generator.dat, which has
// two header lines, a blank separator, and then 83 rows of 91 binary
// digits ('0' / '1') each. The parsed sparse form is stored in
// generatorRowOnes for fast per-row XOR during encode.
func parseSandboxGenerator() {
	lines := strings.Split(sandboxGeneratorDat, "\n")
	row := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) != LDPCInfoBits {
			continue
		}
		ok := true
		for _, c := range line {
			if c != '0' && c != '1' {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		if row >= LDPCParityRows {
			panic(fmt.Sprintf("sandbox: generator.dat — more than %d data rows", LDPCParityRows))
		}
		ones := []int{}
		for j, c := range line {
			if c == '1' {
				ones = append(ones, j)
			}
		}
		generatorRowOnes[row] = ones
		row++
	}
	if row != LDPCParityRows {
		panic(fmt.Sprintf("sandbox: generator.dat — found %d data rows, want %d", row, LDPCParityRows))
	}
}

// EncodeLDPC takes a 91-bit info word (77-bit payload + 14-bit CRC,
// MSB-first) and produces the systematic 174-bit codeword:
//
//	codeword[0:91]   = info (verbatim)
//	codeword[91:174] = parity, computed via the generator matrix
//
// This is the inverse direction of BPDecode: BP recovers info from a
// noisy codeword; EncodeLDPC produces a clean codeword from info.
// The two together provide the symbolic round-trip invariant
// EncodeLDPC(decoded[0:91]) == decoded for every BP-OK candidate —
// the load-bearing acceptance test of the encoder milestone.
func EncodeLDPC(info [LDPCInfoBits]uint8) [LDPCCodewordBits]uint8 {
	var cw [LDPCCodewordBits]uint8
	copy(cw[:LDPCInfoBits], info[:])
	for i := 0; i < LDPCParityRows; i++ {
		var p uint8
		for _, j := range generatorRowOnes[i] {
			p ^= info[j]
		}
		cw[LDPCInfoBits+i] = p
	}
	return cw
}

// CodewordToTones converts a 174-bit LDPC codeword into the
// 79-symbol FT8 tone sequence:
//
//   - Positions 0..6, 36..42, 72..78 carry the three Costas anchor
//     blocks (always [3, 1, 4, 0, 6, 5, 2]).
//   - Positions 7..35 and 43..71 carry the 58 data symbols. Each
//     data symbol consumes 3 consecutive codeword bits (MSB first)
//     and is mapped to a tone via forwardGrayMap.
//
// The output is invariant w.r.t. amplitude, phase, and timing —
// purely the integer tone sequence. Audio synthesis (M2) consumes
// this sequence plus a freq/dt origin.
func CodewordToTones(cw [LDPCCodewordBits]uint8) [ft8SymbolCount]int {
	var tones [ft8SymbolCount]int

	// Costas anchor blocks.
	for _, blockStart := range costasBlockStarts {
		for k := 0; k < 7; k++ {
			tones[blockStart+k] = costasTones[k]
		}
	}

	// Data symbols: walk dataSymbolIndices in order; each consumes
	// 3 codeword bits (MSB first) and maps via forwardGrayMap.
	for d := 0; d < 58; d++ {
		sym := dataSymbolIndices[d]
		bits := int(cw[3*d])<<2 | int(cw[3*d+1])<<1 | int(cw[3*d+2])
		tones[sym] = forwardGrayMap[bits]
	}
	return tones
}
