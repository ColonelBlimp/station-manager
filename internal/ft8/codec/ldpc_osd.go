package codec

import (
	"math"
	"sort"
)

// Ordered Statistics Decoding (OSD) — a fallback decoder applied
// when belief propagation fails to converge or its CRC doesn't
// validate. The algorithm is from the standard coding-theory
// literature (Fossorier & Lin, "Soft-Decision Decoding of Linear
// Block Codes Based on Ordered Statistics," IEEE Transactions on
// Information Theory, Sept 1995); per the QEX 2020 paper §6, OSD
// adds ~1 dB SNR sensitivity on AWGN over BP alone. This is a
// clean-room implementation from spec — no WSJT-X port.
//
// **High-level algorithm:**
//
//  1. Order the 174 codeword positions by absolute LLR descending —
//     most reliable first. The intuition is that bits with large
//     |LLR| are more likely correct; bits with small |LLR| are
//     candidates for flipping.
//  2. Find a Most-Reliable-Basis (MRB): partition the 174 positions
//     into 91 "information" bits (most reliable, linearly
//     independent in H) and 83 "parity" bits (the dependent
//     positions; preferably least reliable). Gauss-Jordan
//     elimination over GF(2) does this in one pass.
//  3. Read hard decisions on the 91 MRB positions, re-encode to
//     get the 83 parity bits, get a candidate codeword.
//  4. Extract the first 77 bits as the candidate message; validate
//     via CRC14. If it passes, return.
//  5. Order-1 search: for each MRB position (typically the
//     least-reliable subset), flip that one bit and re-encode.
//     First CRC-valid candidate wins. Order-2 flips pairs, etc.
//
// **Cost.** Order-0 is one re-encode + one CRC = ~1 μs. Order-1 is
// 91 trials = ~91 μs. Order-2 would be 91·90/2 = 4095 trials but
// SM's first cut stays at order-1 since that's where Taylor 2020's
// "~1 dB" gain comes from.

// OSDMaxOrder is the upper bound on the OSD search depth. Order-1
// flips a single MRB bit per trial; order-2 flips pairs;
// combinatoric blow-up beyond that is rarely worth the cost. Cap
// at 2 to keep the runtime bounded.
const OSDMaxOrder = 2

// osdSetup carries the per-LLR-vector decomposition: the
// permutation of codeword positions by reliability, the 91 info
// columns, the 83 parity columns, and the XOR patterns that re-
// encode each parity column from the info columns.
type osdSetup struct {
	// infoCols is the list of 91 codeword positions chosen as the
	// MRB (information bits). Ordered from most reliable to least
	// reliable.
	infoCols []int

	// parityCols is the list of 83 codeword positions that become
	// the dependent (parity) bits. Ordered roughly from least to
	// most reliable — these are the bits OSD treats as "computed
	// from the info bits via the parity equations."
	parityCols []int

	// parityDeps[i] is the list of indices INTO infoCols whose
	// bit values XOR together to produce parityCols[i]'s value.
	// Used by osdReencodeAndCheck during candidate re-encoding.
	parityDeps [][]uint8
}

// osdMRBSetup builds an OSD decomposition from a set of LLR
// values. The most-reliable 91 codeword positions become the
// information set (MRB); the remaining 83 (least reliable subject
// to linear-independence constraints) become parity. Gauss-Jordan
// elimination over GF(2) finds the parity-column dependence
// equations used to re-encode.
//
// Returns nil if H is rank-deficient (impossible for a valid LDPC
// code; serves as a structural guard).
func osdMRBSetup(llrs []float64) *osdSetup {
	if len(llrs) != CodewordBits {
		return nil
	}

	// 1. Reliability ranking — ranking[i] is the original codeword
	//    position of the i-th most reliable bit.
	ranking := make([]int, CodewordBits)
	for i := range ranking {
		ranking[i] = i
	}
	sort.SliceStable(ranking, func(i, j int) bool {
		return math.Abs(llrs[ranking[i]]) > math.Abs(llrs[ranking[j]])
	})

	// 2. Build the permuted H matrix as bitsets (one [3]uint64 per
	//    row covers 174 bits with 18 unused). H[r][col] = 1 means
	//    row r has a 1 at the col-th reliability-permuted position
	//    (i.e., position ranking[col] in original codeword indexing).
	const wordsPerRow = 3 // ceil(174 / 64)
	var H [ParityBits][wordsPerRow]uint64
	for permCol := 0; permCol < CodewordBits; permCol++ {
		origCol := ranking[permCol]
		for k := 0; k < LDPCParityColumnDensity; k++ {
			r := int(ldpcParity[origCol][k])
			H[r][permCol/64] |= uint64(1) << (permCol % 64)
		}
	}

	// 3. Gauss-Jordan elimination over GF(2). Pivots are chosen
	//    right-to-left (least-reliable column first) so the parity
	//    columns end up at the least-reliable positions where
	//    possible. Each row gets used as a pivot at most once.
	colIsParity := make([]bool, CodewordBits)
	pivotRowForCol := make([]int, CodewordBits)
	rowPivoted := make([]bool, ParityBits)

	for iter := 0; iter < ParityBits; iter++ {
		pivotCol := -1
		pivotRow := -1

		// Scan columns right-to-left (least reliable first).
		for permCol := CodewordBits - 1; permCol >= 0; permCol-- {
			if colIsParity[permCol] {
				continue
			}
			// Find an unused row with a 1 in this column.
			for r := 0; r < ParityBits; r++ {
				if rowPivoted[r] {
					continue
				}
				if H[r][permCol/64]&(uint64(1)<<(permCol%64)) != 0 {
					pivotCol = permCol
					pivotRow = r
					break
				}
			}
			if pivotCol >= 0 {
				break
			}
		}
		if pivotCol < 0 {
			// No pivot found — H rank-deficient. Should never happen
			// for a valid LDPC parity matrix.
			return nil
		}

		// Eliminate pivotCol from all OTHER rows by XOR-ing the
		// pivot row into them when they have a 1 at pivotCol.
		mask := uint64(1) << (pivotCol % 64)
		word := pivotCol / 64
		for r := 0; r < ParityBits; r++ {
			if r == pivotRow {
				continue
			}
			if H[r][word]&mask != 0 {
				H[r][0] ^= H[pivotRow][0]
				H[r][1] ^= H[pivotRow][1]
				H[r][2] ^= H[pivotRow][2]
			}
		}

		colIsParity[pivotCol] = true
		rowPivoted[pivotRow] = true
		pivotRowForCol[pivotCol] = pivotRow
	}

	// 4. Build the info/parity column lists in original codeword
	//    indices and the parity-dependence XOR patterns.
	infoCols := make([]int, 0, InfoBits)
	parityCols := make([]int, 0, ParityBits)
	infoIndexByPermCol := make([]uint8, CodewordBits) // permCol → index into infoCols, when applicable
	for permCol := 0; permCol < CodewordBits; permCol++ {
		origCol := ranking[permCol]
		if colIsParity[permCol] {
			parityCols = append(parityCols, origCol)
		} else {
			infoIndexByPermCol[permCol] = uint8(len(infoCols))
			infoCols = append(infoCols, origCol)
		}
	}

	// 5. For each parity column, list the info columns it depends
	//    on. After elimination, the pivot row for a parity column
	//    has 1s at exactly: this parity column itself (the pivot)
	//    plus the info columns whose XOR equals this parity bit.
	parityDeps := make([][]uint8, len(parityCols))
	for parityIdx, origParityCol := range parityCols {
		// Find permuted column index for this parity column.
		var permParityCol int
		for permCol := 0; permCol < CodewordBits; permCol++ {
			if ranking[permCol] == origParityCol {
				permParityCol = permCol
				break
			}
		}
		r := pivotRowForCol[permParityCol]

		// Walk the eliminated pivot row for non-pivot (= info)
		// columns with a 1.
		var deps []uint8
		for permCol := 0; permCol < CodewordBits; permCol++ {
			if colIsParity[permCol] {
				continue // skip parity columns (only the pivot itself is 1; others are 0 post-elimination)
			}
			if H[r][permCol/64]&(uint64(1)<<(permCol%64)) != 0 {
				deps = append(deps, infoIndexByPermCol[permCol])
			}
		}
		parityDeps[parityIdx] = deps
	}

	return &osdSetup{
		infoCols:   infoCols,
		parityCols: parityCols,
		parityDeps: parityDeps,
	}
}

// osdReencodeAndCheck takes hard-decision bit values for the 91
// MRB positions, re-encodes the parity bits, assembles the full
// 174-bit codeword, and validates it via CRC14. Returns the
// recovered 77-bit message if the CRC passes.
//
// codeword[]byte and msgBuf[]byte are caller-supplied scratch so
// the hot order-1 loop doesn't allocate per trial.
func osdReencodeAndCheck(infoBits []byte, s *osdSetup, codeword []byte) ([]byte, bool) {
	// Place info bits at their codeword positions.
	for i, col := range s.infoCols {
		codeword[col] = infoBits[i]
	}
	// Compute parity bits from info-column XORs.
	for i, pCol := range s.parityCols {
		var val byte
		for _, infoIdx := range s.parityDeps[i] {
			val ^= infoBits[infoIdx]
		}
		codeword[pCol] = val
	}

	// Codeword[0..76] = message, [77..90] = CRC14, [91..173] = parity.
	msg := codeword[:MessageBits]
	rxCRC := codeword[MessageBits : MessageBits+CRCBits]
	expectedCRC := CRC14(msg)
	receivedCRC := packBitsMSBFirst(rxCRC)
	if expectedCRC != receivedCRC {
		return nil, false
	}
	// Caller must copy if they want to retain msg — codeword is
	// scratch and may be overwritten on subsequent trials.
	out := make([]byte, MessageBits)
	copy(out, msg)
	return out, true
}

// OSDDecode attempts to recover a 77-bit FT8 message from soft
// LLRs using Ordered Statistics Decoding at the given search order.
//
//   - order=0: re-encode the MRB once, check CRC. Cheapest.
//   - order=1: also flip each MRB bit individually (91 trials).
//   - order=2: also flip pairs (4095 trials).
//
// Higher orders rarely pay for themselves; OSDMaxOrder caps at 2.
// llrs: 174 LLRs in LDPC-literature convention (positive ⟹ bit=0).
//
// Returns the recovered message + true iff a CRC14-valid candidate
// is found; nil, false otherwise.
func OSDDecode(llrs []float64, order int) ([]byte, bool) {
	if len(llrs) != CodewordBits {
		return nil, false
	}
	if order < 0 {
		order = 0
	}
	if order > OSDMaxOrder {
		order = OSDMaxOrder
	}

	setup := osdMRBSetup(llrs)
	if setup == nil {
		return nil, false
	}

	// Hard decisions on the MRB positions per LDPC convention:
	// positive LLR ⟹ bit=0, negative ⟹ bit=1.
	infoBits := make([]byte, InfoBits)
	for i, col := range setup.infoCols {
		if llrs[col] < 0 {
			infoBits[i] = 1
		}
	}

	codeword := make([]byte, CodewordBits)

	// Order-0: try the unmodified MRB hard decision.
	if msg, ok := osdReencodeAndCheck(infoBits, setup, codeword); ok {
		return msg, true
	}
	if order == 0 {
		return nil, false
	}

	// Order-1: flip each MRB bit, one at a time. The least-reliable
	// bits are at the END of infoCols (it's sorted MRB-most-reliable
	// first), so iterate from the tail backward for cache-friendly
	// reliability ordering of the trials.
	for flipIdx := len(infoBits) - 1; flipIdx >= 0; flipIdx-- {
		infoBits[flipIdx] ^= 1
		if msg, ok := osdReencodeAndCheck(infoBits, setup, codeword); ok {
			return msg, true
		}
		infoBits[flipIdx] ^= 1 // restore
	}
	if order == 1 {
		return nil, false
	}

	// Order-2: flip pairs. ~4095 trials.
	for i := len(infoBits) - 1; i >= 0; i-- {
		infoBits[i] ^= 1
		for j := i - 1; j >= 0; j-- {
			infoBits[j] ^= 1
			if msg, ok := osdReencodeAndCheck(infoBits, setup, codeword); ok {
				return msg, true
			}
			infoBits[j] ^= 1
		}
		infoBits[i] ^= 1
	}

	return nil, false
}
