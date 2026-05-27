package sandbox

import (
	_ "embed"
	"fmt"
	"strconv"
	"strings"
)

// LDPC code parameters for FT8 per QEX paper §3:
//
//	LDPCCodewordBits  = 174 (channel-coded length, also = ft8 data bits/3 × 1 — wait same thing)
//	LDPCInfoBits      = 91  (information bits = 77 payload + 14 CRC)
//	LDPCPayloadBits   = 77  (the actual message bits before CRC)
//	LDPCCRCBits       = 14  (CRC over the 77 payload bits)
//	LDPCParityRows    = 83  (codeword − info)
//
// The code is regular: column weight 3 (every codeword bit appears in
// exactly 3 parity equations) and row weight 6 or 7 (every parity
// equation involves 6 or 7 codeword bits). Total ones in the matrix
// = 174 × 3 = 522.
const (
	LDPCCodewordBits = FT8CodewordBits // 174
	LDPCInfoBits     = 91
	LDPCPayloadBits  = 77
	LDPCCRCBits      = 14
	LDPCParityRows   = LDPCCodewordBits - LDPCInfoBits // 83
)

//go:embed parity.dat
var sandboxParityDat string

// Parity-check graph in dual-orientation form. Populated at init from
// the embedded parity.dat (copied verbatim from QEX paper ref [14] —
// public-domain, the source the QEX paper points implementers at).
//
//	varChecks[v] = the 3 parity rows that codeword bit v appears in.
//	checkVars[c] = the codeword bits in parity row c (length 6 or 7).
//
// Edge-position lookups for O(1) translation between var-side and
// check-side message indices:
//
//	varEdgePos[v][k]   = position of v within checkVars[varChecks[v][k]].
//	checkEdgePos[c][i] = k such that varChecks[checkVars[c][i]][k] == c.
//
// Together these let the BP inner loops walk each edge with a single
// integer indirection.
var (
	varChecks    [LDPCCodewordBits][3]int
	checkVars    [LDPCParityRows][]int
	varEdgePos   [LDPCCodewordBits][3]int
	checkEdgePos [LDPCParityRows][]int
)

func init() {
	parseSandboxParity()
	buildSandboxEdgePositions()
}

// parseSandboxParity reads the embedded parity.dat. Format: a few
// lines of human-readable preamble, then 174 data lines, each
// containing 3 whitespace-separated 1-indexed row numbers giving the
// rows where that column has a 1.
func parseSandboxParity() {
	lines := strings.Split(sandboxParityDat, "\n")

	// First data line: the first line whose three whitespace-separated
	// fields all parse as integers.
	start := -1
	for i, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		ok := true
		for _, f := range fields {
			if _, err := strconv.Atoi(f); err != nil {
				ok = false
				break
			}
		}
		if ok {
			start = i
			break
		}
	}
	if start < 0 {
		panic("sandbox: parity.dat — no integer-triple data line found")
	}
	if start+LDPCCodewordBits > len(lines) {
		panic("sandbox: parity.dat — truncated, not enough data lines after header")
	}

	rowsBuf := make([][]int, LDPCParityRows)
	for col := 0; col < LDPCCodewordBits; col++ {
		fields := strings.Fields(lines[start+col])
		if len(fields) != 3 {
			panic(fmt.Sprintf("sandbox: parity.dat — col %d malformed (want 3 ints)", col))
		}
		for k := 0; k < 3; k++ {
			row, err := strconv.Atoi(fields[k])
			if err != nil {
				panic(fmt.Sprintf("sandbox: parity.dat — col %d parse: %v", col, err))
			}
			row-- // 1-indexed → 0-indexed
			if row < 0 || row >= LDPCParityRows {
				panic(fmt.Sprintf("sandbox: parity.dat — col %d row %d out of range", col, row+1))
			}
			varChecks[col][k] = row
			rowsBuf[row] = append(rowsBuf[row], col)
		}
	}
	for r := 0; r < LDPCParityRows; r++ {
		checkVars[r] = rowsBuf[r]
	}
}

// buildSandboxEdgePositions fills the reverse-index tables. Each edge
// (v, c) is addressable two ways — by v's slot k (0..2) within
// varChecks[v], and by v's slot i within checkVars[c]. The BP messages
// are stored check-side (one slot per checkVars[c] entry), so the
// var-side update needs to look up "which checkVars[c] slot am I"
// in O(1).
func buildSandboxEdgePositions() {
	for v := 0; v < LDPCCodewordBits; v++ {
		for k := 0; k < 3; k++ {
			c := varChecks[v][k]
			pos := -1
			for i, w := range checkVars[c] {
				if w == v {
					pos = i
					break
				}
			}
			if pos < 0 {
				panic(fmt.Sprintf("sandbox: parity graph inconsistent — var %d not found in check %d", v, c))
			}
			varEdgePos[v][k] = pos
		}
	}
	for c := 0; c < LDPCParityRows; c++ {
		checkEdgePos[c] = make([]int, len(checkVars[c]))
		for i, v := range checkVars[c] {
			pos := -1
			for k := 0; k < 3; k++ {
				if varChecks[v][k] == c {
					pos = k
					break
				}
			}
			if pos < 0 {
				panic(fmt.Sprintf("sandbox: parity graph inconsistent — check %d not in var %d's row", c, v))
			}
			checkEdgePos[c][i] = pos
		}
	}
}
