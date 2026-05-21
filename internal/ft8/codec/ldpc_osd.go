package codec

import (
	"math"
	"sort"
	"sync"
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

// osdScratch holds the fixed-size working buffers that osdMRBSetup
// needs. Pooling these per-call eliminates ~50 KB of slice
// allocations on every OSD invocation; in the FT8 receive loop OSD
// runs up to 25× per failed candidate, so this matters a lot for
// GC pressure.
//
// All slices below are reused across calls; osdMRBSetup resets
// their headers (length) but the backing arrays persist via
// osdScratchPool.
type osdScratch struct {
	// Permutation: ranking[i] = original codeword position of the
	// i-th most reliable bit. Length CodewordBits.
	ranking []int

	// Bitset-packed H matrix permuted by reliability: H[r] is one
	// row of ParityBits×CodewordBits bits packed into 3 uint64s
	// (covers 174 bits with 18 unused).
	H [ParityBits][3]uint64

	// Per-column / per-row state for the Gauss-Jordan pass.
	colIsParity        []bool  // CodewordBits
	pivotRowForCol     []int   // CodewordBits
	rowPivoted         []bool  // ParityBits
	infoIndexByPermCol []uint8 // CodewordBits

	// Setup outputs after a successful osdMRBSetup call.

	// infoCols is the 91 codeword positions used as info bits.
	// Most-reliable-first ordering.
	infoCols []int

	// parityCols is the 83 codeword positions used as parity bits.
	// Least-reliable-first where possible.
	parityCols []int

	// parityDepsBuf is a flat backing array holding each parity
	// column's info-bit XOR pattern, contiguously. parityDeps(i)
	// returns the slice view for parity column i.
	//
	// Worst case: each parity column depends on all 91 info
	// columns; total ParityBits × InfoBits = 7553 uint8s.
	parityDepsBuf []uint8
	parityDepsLen []int // ParityBits — actual dep count per parity

	// Hard-decision buffer for OSD's MRB infoBits. Length InfoBits.
	infoBits []byte

	// Re-encoding scratch codeword. Length CodewordBits.
	codeword []byte
}

// parityDeps returns the info-bit XOR pattern for parity column i
// as a slice view into the flat parityDepsBuf.
func (s *osdScratch) parityDeps(i int) []uint8 {
	start := i * InfoBits
	return s.parityDepsBuf[start : start+s.parityDepsLen[i]]
}

// osdScratchPool is the lifetime-anonymous pool of osdScratch
// instances. GC reclaims pooled entries between heavy bursts; the
// hot loop reuses entries without allocation.
var osdScratchPool = sync.Pool{
	New: func() any {
		return &osdScratch{
			ranking:            make([]int, CodewordBits),
			colIsParity:        make([]bool, CodewordBits),
			pivotRowForCol:     make([]int, CodewordBits),
			rowPivoted:         make([]bool, ParityBits),
			infoIndexByPermCol: make([]uint8, CodewordBits),
			infoCols:           make([]int, 0, InfoBits),
			parityCols:         make([]int, 0, ParityBits),
			parityDepsBuf:      make([]uint8, ParityBits*InfoBits),
			parityDepsLen:      make([]int, ParityBits),
			infoBits:           make([]byte, InfoBits),
			codeword:           make([]byte, CodewordBits),
		}
	},
}

// reset prepares the scratch for a fresh osdMRBSetup call. Zeroes
// the bool/int state and resets the dynamic slices' lengths to 0
// without releasing their backing arrays.
func (s *osdScratch) reset() {
	for i := range s.colIsParity {
		s.colIsParity[i] = false
	}
	for i := range s.rowPivoted {
		s.rowPivoted[i] = false
	}
	for r := range s.H {
		s.H[r][0] = 0
		s.H[r][1] = 0
		s.H[r][2] = 0
	}
	s.infoCols = s.infoCols[:0]
	s.parityCols = s.parityCols[:0]
}

// osdMRBSetup builds an OSD decomposition from a set of LLR
// values into the provided scratch buffer (modifies it in place).
// The most-reliable 91 codeword positions become the information
// set (MRB); the remaining 83 (least reliable subject to linear-
// independence constraints) become parity. Gauss-Jordan elimination
// over GF(2) finds the parity-column dependence equations used to
// re-encode.
//
// Returns false if H is rank-deficient (impossible for a valid
// LDPC code; serves as a structural guard). On true, s.infoCols /
// s.parityCols / s.parityDepsBuf+parityDepsLen are populated.
func osdMRBSetup(llrs []float64, s *osdScratch) bool {
	if len(llrs) != CodewordBits {
		return false
	}
	s.reset()

	// 1. Reliability ranking — s.ranking[i] is the original
	//    codeword position of the i-th most reliable bit.
	for i := range s.ranking {
		s.ranking[i] = i
	}
	ranking := s.ranking
	sort.SliceStable(ranking, func(i, j int) bool {
		return math.Abs(llrs[ranking[i]]) > math.Abs(llrs[ranking[j]])
	})

	// 2. Build the permuted H matrix as bitsets in s.H. H[r][col]
	//    = 1 means row r has a 1 at the col-th reliability-permuted
	//    position (i.e., position ranking[col] in original codeword
	//    indexing). s.reset() already zeroed H.
	for permCol := 0; permCol < CodewordBits; permCol++ {
		origCol := ranking[permCol]
		for k := 0; k < LDPCParityColumnDensity; k++ {
			r := int(ldpcParity[origCol][k])
			s.H[r][permCol/64] |= uint64(1) << (permCol % 64)
		}
	}

	// 3. Gauss-Jordan elimination over GF(2). Pivots chosen
	//    right-to-left (least-reliable column first) so parity
	//    columns end up at the least-reliable positions where
	//    possible. Each row gets used as a pivot at most once.
	for iter := 0; iter < ParityBits; iter++ {
		pivotCol := -1
		pivotRow := -1

		// Scan columns right-to-left (least reliable first).
		for permCol := CodewordBits - 1; permCol >= 0; permCol-- {
			if s.colIsParity[permCol] {
				continue
			}
			// Find an unused row with a 1 in this column.
			for r := 0; r < ParityBits; r++ {
				if s.rowPivoted[r] {
					continue
				}
				if s.H[r][permCol/64]&(uint64(1)<<(permCol%64)) != 0 {
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
			// No pivot found — H rank-deficient. Should never
			// happen for a valid LDPC parity matrix.
			return false
		}

		// Eliminate pivotCol from all OTHER rows by XOR-ing the
		// pivot row into them when they have a 1 at pivotCol.
		mask := uint64(1) << (pivotCol % 64)
		word := pivotCol / 64
		for r := 0; r < ParityBits; r++ {
			if r == pivotRow {
				continue
			}
			if s.H[r][word]&mask != 0 {
				s.H[r][0] ^= s.H[pivotRow][0]
				s.H[r][1] ^= s.H[pivotRow][1]
				s.H[r][2] ^= s.H[pivotRow][2]
			}
		}

		s.colIsParity[pivotCol] = true
		s.rowPivoted[pivotRow] = true
		s.pivotRowForCol[pivotCol] = pivotRow
	}

	// 4. Build the info/parity column lists in original codeword
	//    indices and the per-column position map.
	for permCol := 0; permCol < CodewordBits; permCol++ {
		origCol := ranking[permCol]
		if s.colIsParity[permCol] {
			s.parityCols = append(s.parityCols, origCol)
		} else {
			s.infoIndexByPermCol[permCol] = uint8(len(s.infoCols))
			s.infoCols = append(s.infoCols, origCol)
		}
	}

	// 5. For each parity column, list the info columns it depends
	//    on. After elimination, the pivot row for a parity column
	//    has 1s at exactly: this parity column itself (the pivot)
	//    plus the info columns whose XOR equals this parity bit.
	//
	//    We walk parity columns in the order they appear in
	//    s.parityCols, and store the dependency uint8 indices in a
	//    flat backing buffer to avoid per-column slice allocations.
	for parityIdx, origParityCol := range s.parityCols {
		// Find permuted column index for this parity column.
		var permParityCol int
		for permCol := 0; permCol < CodewordBits; permCol++ {
			if ranking[permCol] == origParityCol {
				permParityCol = permCol
				break
			}
		}
		r := s.pivotRowForCol[permParityCol]

		// Walk the eliminated pivot row for non-pivot (= info)
		// columns with a 1. Write into the flat parityDepsBuf;
		// remember the count in parityDepsLen.
		base := parityIdx * InfoBits
		count := 0
		for permCol := 0; permCol < CodewordBits; permCol++ {
			if s.colIsParity[permCol] {
				continue
			}
			if s.H[r][permCol/64]&(uint64(1)<<(permCol%64)) != 0 {
				s.parityDepsBuf[base+count] = s.infoIndexByPermCol[permCol]
				count++
			}
		}
		s.parityDepsLen[parityIdx] = count
	}

	return true
}

// osdReencodeAndCheck takes hard-decision bit values for the 91
// MRB positions, re-encodes the parity bits using the scratch's
// decomposition, assembles the full 174-bit codeword into
// s.codeword, and validates it via CRC14. Returns the recovered
// 77-bit message if the CRC passes — allocated fresh so callers
// can retain it independently of the scratch's lifetime.
func osdReencodeAndCheck(s *osdScratch) ([]byte, bool) {
	infoBits := s.infoBits
	codeword := s.codeword

	// Place info bits at their codeword positions.
	for i, col := range s.infoCols {
		codeword[col] = infoBits[i]
	}
	// Compute parity bits from info-column XORs.
	for i, pCol := range s.parityCols {
		var val byte
		deps := s.parityDeps(i)
		for _, infoIdx := range deps {
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

	s := osdScratchPool.Get().(*osdScratch)
	defer osdScratchPool.Put(s)

	if !osdMRBSetup(llrs, s) {
		return nil, false
	}

	// Hard decisions on the MRB positions per LDPC convention:
	// positive LLR ⟹ bit=0, negative ⟹ bit=1.
	for i, col := range s.infoCols {
		if llrs[col] < 0 {
			s.infoBits[i] = 1
		} else {
			s.infoBits[i] = 0
		}
	}

	// Order-0: try the unmodified MRB hard decision.
	if msg, ok := osdReencodeAndCheck(s); ok {
		return msg, true
	}
	if order == 0 {
		return nil, false
	}

	// Order-1: flip each MRB bit, one at a time. The least-reliable
	// bits are at the END of infoCols (it's sorted MRB-most-reliable
	// first), so iterate from the tail backward for cache-friendly
	// reliability ordering of the trials.
	for flipIdx := len(s.infoBits) - 1; flipIdx >= 0; flipIdx-- {
		s.infoBits[flipIdx] ^= 1
		if msg, ok := osdReencodeAndCheck(s); ok {
			return msg, true
		}
		s.infoBits[flipIdx] ^= 1 // restore
	}
	if order == 1 {
		return nil, false
	}

	// Order-2: flip pairs. ~4095 trials.
	for i := len(s.infoBits) - 1; i >= 0; i-- {
		s.infoBits[i] ^= 1
		for j := i - 1; j >= 0; j-- {
			s.infoBits[j] ^= 1
			if msg, ok := osdReencodeAndCheck(s); ok {
				return msg, true
			}
			s.infoBits[j] ^= 1
		}
		s.infoBits[i] ^= 1
	}

	return nil, false
}
