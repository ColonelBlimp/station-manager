// osd.go — Ordered Statistics Decoder (OSD) for the LDPC(174,91) code.
//
// OSD is a maximum-likelihood–style fallback decoder used when belief
// propagation fails to converge. It sorts bit positions by LLR reliability,
// Gaussian-eliminates the generator matrix to make the K most-reliable
// positions systematic, then searches over small error patterns in the
// information positions to find the minimum-distance codeword.
//
// The algorithm:
//  1. Hard-decide all 174 bits and rank them by |LLR| (reliability).
//  2. Reorder the generator matrix columns so the most-reliable bits come first.
//  3. Gaussian-eliminate to make the first K columns an identity block.
//  4. Order-0: encode the K most-reliable hard decisions — this is the baseline.
//  5. Order-1 (ndeep ≥ 1): flip each of the K information bits one at a time,
//     re-encode (incrementally, via column XOR), and keep the codeword with
//     the smallest weighted Hamming distance to the received signal.
//  6. Un-permute the best codeword and extract the 91 info bits.
//
// LLR sign convention: positive = bit more likely 0, negative = bit more
// likely 1 (same as [Decode]).
//
// Reference: WSJT-X lib/ft8/osd174_91.f90.

package codec

import (
	"math"
	"sort"
	"sync"
)

// osdMaxColSearch is the maximum number of columns beyond K to search for
// a pivot during Gaussian elimination. Matches the ad-hoc "+20" in
// WSJT-X's osd174_91.f90.
const osdMaxColSearch = 20

// fullGen is the K×N generator matrix for the (174,91) code, stored
// as individual bits (0 or 1). Row i is the full 174-bit codeword
// produced when only information bit i is set.
//
// Lazily initialised on first use by [initFullGenerator].
var (
	fullGenOnce sync.Once
	fullGen     [K][N]uint8
)

// initFullGenerator builds the K×N generator matrix by encoding each of
// the K unit vectors through [Encode]. The result is cached for the
// lifetime of the process.
func initFullGenerator() {
	fullGenOnce.Do(func() {
		for i := range K {
			var info [KBytes]byte
			info[i/8] |= 1 << uint(7-i%8)
			packed := Encode(info)
			for j := range N {
				fullGen[i][j] = (packed[j/8] >> uint(7-j%8)) & 1
			}
		}
	})
}

// DecodeOSD performs ordered-statistics decoding of a (174,91) LDPC codeword.
//
// llr contains 174 log-likelihood ratios (positive = bit more likely 0,
// negative = bit more likely 1). ndeep controls the search depth:
//   - 0: order-0 only (hard decisions from K most-reliable bits, encoded)
//   - 1: order-0 + order-1 (flip each of K bits one at a time)
//   - 2: order-0 + order-1 + order-2 (flip all pairs of K bits)
//
// ndeep=2 matches WSJT-X's norder=2 in ft8b.f90, testing K*(K-1)/2 = 4095
// additional two-bit flip patterns beyond the 91 single-bit flips.
//
// On success it returns the 91-bit information payload packed MSB-first
// into 12 bytes. ok=false is returned only when Gaussian elimination
// fails to find K independent columns (degenerate input).
//
// Unlike [Decode], OSD always produces a valid codeword (it encodes via
// the generator matrix). The caller must still verify CRC-14 to confirm
// correctness — [DecodeMessage] does this automatically.
func DecodeOSD(llr [N]float32, ndeep int) (info [KBytes]byte, ok bool) {
	var apmask [N]uint8
	return decodeOSDInternal(llr, apmask, ndeep)
}

// DecodeOSDAP performs ordered-statistics decoding with AP mask support.
//
// For bits where apmask[i]==1, the order-1 search skips flip patterns that
// touch those bits, matching WSJT-X osd174_91.f90 line 193:
//
//	if(any(iand(apmaskr(1:k),mi).eq.1)) cycle
//
// This ensures the OSD search does not flip known-correct AP bits.
func DecodeOSDAP(llr [N]float32, apmask [N]uint8, ndeep int) (info [KBytes]byte, ok bool) {
	return decodeOSDInternal(llr, apmask, ndeep)
}

// decodeOSDInternal is the shared OSD implementation supporting both regular
// and AP-masked decoding.
func decodeOSDInternal(llr [N]float32, apmask [N]uint8, ndeep int) (info [KBytes]byte, ok bool) {
	initFullGenerator()

	// --- Hard decisions and reliability --------------------------------
	var hdec [N]uint8
	var absLLR [N]float32
	for i := range N {
		if llr[i] < 0 {
			hdec[i] = 1
		}
		absLLR[i] = float32(math.Abs(float64(llr[i])))
	}

	// --- Sort indices by decreasing reliability -----------------------
	var indices [N]int
	for i := range N {
		indices[i] = i
	}
	sort.Slice(indices[:], func(a, b int) bool {
		return absLLR[indices[a]] > absLLR[indices[b]]
	})

	// --- Reorder generator matrix columns by reliability --------------
	var genmrb [K][N]uint8
	for col := range N {
		origCol := indices[col]
		for row := range K {
			genmrb[row][col] = fullGen[row][origCol]
		}
	}

	// --- Gaussian elimination with partial column pivoting ------------
	// After elimination, columns 0..K-1 of genmrb form an identity block,
	// and indices[0..K-1] track which original bit positions they map to.
	for id := range K {
		maxCol := K + osdMaxColSearch
		if maxCol > N {
			maxCol = N
		}
		pivotFound := false
		for icol := id; icol < maxCol; icol++ {
			if genmrb[id][icol] != 1 {
				continue
			}
			pivotFound = true
			if icol != id {
				// Swap columns in genmrb and the permutation tracker.
				for row := range K {
					genmrb[row][id], genmrb[row][icol] = genmrb[row][icol], genmrb[row][id]
				}
				indices[id], indices[icol] = indices[icol], indices[id]
			}
			// Eliminate all other rows with a 1 in column id.
			for ii := range K {
				if ii != id && genmrb[ii][id] == 1 {
					for j := range N {
						genmrb[ii][j] ^= genmrb[id][j]
					}
				}
			}
			break
		}
		if !pivotFound {
			return info, false // singular — should not happen in practice
		}
	}

	// --- Transpose to g2[N][K] for fast column-wise encoding ----------
	var g2 [N][K]uint8
	for i := range K {
		for j := range N {
			g2[j][i] = genmrb[i][j]
		}
	}

	// --- Reorder received signal into the permuted order --------------
	var hdecPerm [N]uint8
	var absRPerm [N]float32
	var apmaskPerm [N]uint8
	for i := range N {
		hdecPerm[i] = hdec[indices[i]]
		absRPerm[i] = absLLR[indices[i]]
		apmaskPerm[i] = apmask[indices[i]]
	}

	// --- Order-0: encode MRB hard decisions ---------------------------
	var m0 [K]uint8
	copy(m0[:], hdecPerm[:K])

	c0 := osdEncode(&m0, &g2)

	dmin := osdDistance(&c0, &hdecPerm, &absRPerm)
	bestCW := c0

	// --- Order-1: try flipping each of K bits one at a time -----------
	if ndeep >= 1 {
		// Flipping bit j in the message is equivalent to XORing the
		// codeword with column j of g2 (since codeword = g2 × msg).
		for j := range K {
			// Skip flipping AP-masked bits — these are known-correct.
			// Matches WSJT-X osd174_91.f90 line 193.
			if apmaskPerm[j] == 1 {
				continue
			}

			var ce [N]uint8
			for i := range N {
				ce[i] = c0[i] ^ g2[i][j]
			}

			dd := osdDistance(&ce, &hdecPerm, &absRPerm)
			if dd < dmin {
				dmin = dd
				bestCW = ce
			}
		}
	}

	// --- Order-2: try flipping all pairs of K bits -------------------
	// Matches WSJT-X osd174_91.f90 norder=2: tries K*(K-1)/2 = 4095
	// two-bit flip patterns. This is significantly more powerful than
	// order-1 for marginal signals where single-bit flips are insufficient.
	if ndeep >= 2 {
		for j1 := 0; j1 < K; j1++ {
			if apmaskPerm[j1] == 1 {
				continue
			}
			for j2 := j1 + 1; j2 < K; j2++ {
				if apmaskPerm[j2] == 1 {
					continue
				}

				var ce [N]uint8
				for i := range N {
					ce[i] = c0[i] ^ g2[i][j1] ^ g2[i][j2]
				}

				dd := osdDistance(&ce, &hdecPerm, &absRPerm)
				if dd < dmin {
					dmin = dd
					bestCW = ce
				}
			}
		}
	}

	// --- Un-permute the best codeword to original bit order -----------
	var cw [N]uint8
	for i := range N {
		cw[indices[i]] = bestCW[i]
	}

	return packInfoBits(&cw), true
}

// osdEncode encodes a K-bit message using the transposed generator matrix g2.
// For each set bit in msg, it XORs the corresponding column of g2 into the
// codeword. The result is always a valid LDPC codeword.
func osdEncode(msg *[K]uint8, g2 *[N][K]uint8) [N]uint8 {
	var cw [N]uint8
	for i := range K {
		if msg[i] == 1 {
			for j := range N {
				cw[j] ^= g2[j][i]
			}
		}
	}
	return cw
}

// osdDistance computes the weighted Hamming distance between a candidate
// codeword and the hard-decision vector, weighted by per-bit reliability.
//
//	d = Σ (candidate[i] ⊕ hdec[i]) × |LLR[i]|
func osdDistance(candidate, hdec *[N]uint8, absLLR *[N]float32) float32 {
	var d float32
	for i := range N {
		if candidate[i] != hdec[i] {
			d += absLLR[i]
		}
	}
	return d
}
