// Package ldpc decodes the FT8 (174, 91) LDPC code into 91 info
// bits, gating acceptance on CRC14 verification per QEX paper §6
// (Franke, Somerville, Taylor — "The FT4 and FT8 Communication
// Protocols," QEX July/August 2020).
//
// The H matrix and CRC14 polynomial are reproduced from the
// public-domain QEX paper reference [14] tarball
// (http://physics.princeton.edu/pulsar/k1jt/, file
// ft4_ft8_protocols.tgz). parity.dat is embedded verbatim; the CRC14
// polynomial is read from gen_crc14.f90. These materials are
// licence-clean for an MIT codebase — they are the published
// reference the QEX paper points implementers at.
//
// Imports stdlib only — by rule the research tree must not depend
// on internal/ft8/*.
//
// Sign convention on input LLRs matches research/demod.LLRs:
// positive LLR ⇒ bit 0 more likely. Output hard-decided bits use
// the same orientation (1 = bit 1, 0 = bit 0).
package ldpc

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed parity.dat
var parityDat string

// FT8 LDPC code parameters per QEX paper §6:
//
//   - codewordBits = 174 (channel-coded length)
//   - infoBits     = 91  (information bits = 77 payload + 14 CRC)
//   - parityRows   = 83  (parity-check equations = 174 − 91)
//
// The code is regular: column weight 3 (every codeword bit appears
// in exactly 3 parity equations), row weight 6 or 7 (every parity
// equation involves 6 or 7 codeword bits).
const (
	codewordBits = 174
	infoBits     = 91
	payloadBits  = 77
	crcBits      = 14
	parityRows   = codewordBits - infoBits // 83
)

// H matrix in dual-orientation form. Populated once at init from the
// embedded parity.dat.
//
//   - hColumns[v] = the 3 parity rows that variable v participates in
//   - hRows[c]    = the list of variables in parity row c (length 6 or 7)
//
// Plus precomputed reverse-index lookups so the BP inner loops can
// translate between variable-side and check-side message indices in
// constant time:
//
//   - varEdgeRowPos[v][k] = position of v within hRows[hColumns[v][k]]
//   - chkEdgeColPos[c][i] = k such that hColumns[hRows[c][i]][k] == c
var (
	hColumns      [codewordBits][3]int
	hRows         [parityRows][]int
	varEdgeRowPos [codewordBits][3]int
	chkEdgeColPos [parityRows][]int
)

func init() {
	parseParity()
	buildEdgeLookups()
}

// parseParity reads the embedded parity.dat. Format: a few lines of
// human-readable header text, then 174 data lines each containing 3
// space-separated 1-indexed row numbers giving the rows where that
// column has a 1.
func parseParity() {
	lines := strings.Split(parityDat, "\n")

	// First data line is the first line whose three whitespace-
	// separated fields all parse as integers.
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
		panic("ldpc: no integer-triple data line found in parity.dat")
	}
	if start+codewordBits > len(lines) {
		panic("ldpc: parity.dat truncated — not enough data lines after header")
	}

	rowsBuf := make([][]int, parityRows)
	for col := 0; col < codewordBits; col++ {
		fields := strings.Fields(lines[start+col])
		if len(fields) != 3 {
			panic("ldpc: parity.dat data line malformed (want 3 integers): line " + strconv.Itoa(start+col+1))
		}
		for k := 0; k < 3; k++ {
			row, err := strconv.Atoi(fields[k])
			if err != nil {
				panic("ldpc: parity.dat parse error: " + err.Error())
			}
			row-- // 1-indexed → 0-indexed
			if row < 0 || row >= parityRows {
				panic("ldpc: parity.dat row out of range: " + strconv.Itoa(row+1))
			}
			hColumns[col][k] = row
			rowsBuf[row] = append(rowsBuf[row], col)
		}
	}
	for r := 0; r < parityRows; r++ {
		hRows[r] = rowsBuf[r]
	}
}

// buildEdgeLookups fills the reverse-index tables. Each edge (v, c)
// has two index views — the position of c within v's 3-check list,
// and the position of v within c's row. The BP message arrays are
// stored in those node-local index spaces, so the message updates
// need both lookups available in O(1).
func buildEdgeLookups() {
	for v := 0; v < codewordBits; v++ {
		for k := 0; k < 3; k++ {
			c := hColumns[v][k]
			pos := -1
			for i, w := range hRows[c] {
				if w == v {
					pos = i
					break
				}
			}
			if pos < 0 {
				panic("ldpc: edge not found — H inconsistency at v=" + strconv.Itoa(v))
			}
			varEdgeRowPos[v][k] = pos
		}
	}

	for c := 0; c < parityRows; c++ {
		row := hRows[c]
		chkEdgeColPos[c] = make([]int, len(row))
		for i, v := range row {
			pos := -1
			for k := 0; k < 3; k++ {
				if hColumns[v][k] == c {
					pos = k
					break
				}
			}
			if pos < 0 {
				panic("ldpc: edge not found — H inconsistency at c=" + strconv.Itoa(c))
			}
			chkEdgeColPos[c][i] = pos
		}
	}
}

// Stats holds decode diagnostics. Returned alongside Result so the
// caller can distinguish "decoded successfully" (ConvergedCRC=true)
// from "found a parity-clean codeword but CRC failed" (ConvergedParity
// but not ConvergedCRC) from "BP never converged" (neither).
type Stats struct {
	// Iterations is the number of BP iterations actually executed
	// (1..maxIter). Even on success this is useful — easy decodes
	// finish in 2-5 iterations; marginal decodes use most of the budget.
	Iterations int

	// ConvergedParity is true iff H·ĉ = 0 at termination — i.e.
	// the hard-decided codeword is a valid LDPC codeword. Note that
	// "valid LDPC codeword" is necessary but NOT sufficient for an
	// FT8 decode — CRC14 also has to pass.
	ConvergedParity bool

	// ConvergedCRC is true iff syndrome=0 AND CRC14 over the 77
	// payload bits matches the 14 CRC bits in the recovered info word.
	// This is the load-bearing "decode succeeded" flag.
	ConvergedCRC bool

	// BestSyndromeWeight is the minimum Hamming weight of H·ĉ seen
	// across all iterations. A run that never converged but reached
	// e.g. 2 unsatisfied checks is closer to a real codeword than one
	// that stayed at 40 unsatisfied — useful for OSD-fallback heuristics.
	BestSyndromeWeight int

	// FinalSyndromeWeight is the syndrome Hamming weight at the
	// terminating iteration (zero on parity-clean decodes).
	FinalSyndromeWeight int

	// UsedOSD reports whether the OSD-2 fallback ran. True iff BP
	// alone failed to reach syndrome=0 + CRC pass within maxIter,
	// regardless of whether OSD itself then succeeded.
	UsedOSD bool

	// OSDCandidatesExplored is the count of test-pattern codewords
	// OSD-2 scored before returning. For order 2 over a (174, 91)
	// code this is 1 + 91 + 91*90/2 = 4187 if OSD ran to completion.
	// Zero if OSD did not run (BP succeeded).
	OSDCandidatesExplored int
}

// Result carries the hard-decided bits from a decode attempt. Info
// is populated regardless of CRC outcome; the caller checks
// Stats.ConvergedCRC to know whether Info actually decodes to a valid
// FT8 message.
type Result struct {
	// Info is the 91-bit info word (77 payload + 14 CRC). Index 0
	// is the first transmitted info bit; layout matches the FT8
	// systematic encoding (info bits occupy the first 91 codeword
	// positions).
	Info [infoBits]uint8

	// Codeword is the full 174-bit hard-decided codeword at
	// termination. Useful for diagnostics on non-converged decodes.
	Codeword [codewordBits]uint8
}

// Decode-time parameters. maxIter and msScaleFactor are the
// design-discussion starting points (Session 87, 2026-05-25): 25
// iterations with a 0.75 normalised min-sum scale. Both are tunable;
// changes should rerun the regression tests in ldpc_test.go.
const (
	maxIter       = 25
	msScaleFactor = 0.75
)
