// decoder.go — LDPC(174,91) normalised min-sum belief-propagation decoder.
//
// The decoder takes 174 log-likelihood ratios and iteratively refines hard
// bit decisions until all 83 parity checks pass or the iteration limit is
// reached.
//
// LLR sign convention: positive means bit more likely 0, negative means bit
// more likely 1.
//
// Reference algorithm: normalised min-sum BP, matching the compact array
// layout of ft8_lib bp_decode (tov[N][3] / toc[M][7]) but replacing
// sum-product (tanh/atanh) with sign × β × min-magnitude.

package codec

import "math"

// beta is the normalised min-sum scaling factor, matching ft8_lib.
// It attenuates the check-to-variable messages to compensate for the
// min-sum approximation of the exact BP update.
const beta = 0.8

// Decode performs normalised min-sum belief-propagation decoding of a
// (174,91) LDPC codeword.
//
// llr contains 174 log-likelihood ratios (positive = bit more likely 0,
// negative = bit more likely 1). maxIter is the maximum number of BP
// iterations (typically 25–50).
//
// On success it returns the 91-bit information payload packed MSB-first
// into 12 bytes and ok=true. If decoding fails to converge within
// maxIter iterations, it returns a zero-valued array and ok=false.
func Decode(llr [N]float32, maxIter int) (info [KBytes]byte, ok bool) {
	// tov[n][j] holds the check→variable message from check node Mn[n][j]-1
	// to variable node n. Initialised to zero (no extrinsic info yet).
	var tov [N][3]float32

	// toc[m][i] holds the variable→check message from variable node
	// Nm[m][i]-1 to check node m.
	var toc [M][7]float32

	// plain holds the current hard-decision bits for the full codeword.
	var plain [N]uint8

	for range maxIter {
		// --- Hard decision ---
		// A-posteriori LLR = channel LLR + all incoming check→variable messages.
		plainSum := 0
		for n := range N {
			app := llr[n] + tov[n][0] + tov[n][1] + tov[n][2]
			if app < 0 {
				plain[n] = 1
			} else {
				plain[n] = 0
			}
			plainSum += int(plain[n])
		}

		// All-zero codeword is a trivial fixed point of BP — reject it.
		if plainSum == 0 {
			return info, false
		}

		// --- Syndrome check ---
		if syndromeOK(&plain) {
			return packInfoBits(&plain), true
		}

		// --- Variable→Check update ---
		// For each edge (variable n, check m): compute the extrinsic LLR
		// from variable n to check m, which is the channel LLR plus all
		// check→variable messages EXCEPT the one from check m.
		for m := range M {
			deg := int(NmCount[m])
			for nIdx := range deg {
				n := int(Nm[m][nIdx]) - 1 // 0-indexed variable node
				q := llr[n]
				// Sum check→variable messages from all checks EXCEPT m.
				// Mn[n] always has exactly 3 entries (variable-node degree is 3),
				// and exactly one of them equals m+1, so this adds exactly 2
				// of the 3 tov values. Graph consistency is validated by
				// TestBipartiteConsistency.
				for mIdx := range 3 {
					if int(Mn[n][mIdx])-1 != m {
						q += tov[n][mIdx]
					}
				}
				toc[m][nIdx] = q
			}
		}

		// --- Check→Variable update (normalised min-sum) ---
		// For each edge (variable n, check m): compute the extrinsic
		// message from check m to variable n as:
		//   sign(∏ q_other) × β × min(|q_other|)
		// where q_other are the variable→check messages from all OTHER
		// variable nodes connected to check m.
		for n := range N {
			for mIdx := range 3 {
				m := int(Mn[n][mIdx]) - 1 // 0-indexed check node
				deg := int(NmCount[m])

				sign := float32(1.0)
				// Every check node has degree 6 or 7 (NmCount), and we exclude
				// the current variable node, so at least 5 terms contribute.
				// minAbs is therefore always overwritten before use.
				minAbs := float32(math.MaxFloat32)

				for nIdx := range deg {
					if int(Nm[m][nIdx])-1 != n {
						val := toc[m][nIdx]
						if val < 0 {
							sign = -sign
							val = -val
						}
						if val < minAbs {
							minAbs = val
						}
					}
				}

				tov[n][mIdx] = sign * beta * minAbs
			}
		}
	}

	return info, false
}

// syndromeOK returns true if all 83 parity checks are satisfied.
func syndromeOK(plain *[N]uint8) bool {
	for m := range M {
		deg := int(NmCount[m])
		var x uint8
		for i := range deg {
			x ^= plain[int(Nm[m][i])-1]
		}
		if x != 0 {
			return false
		}
	}
	return true
}

// packInfoBits extracts the first K=91 bits from the hard-decision array
// and packs them MSB-first into KBytes=12 bytes.
func packInfoBits(plain *[N]uint8) [KBytes]byte {
	var info [KBytes]byte
	for i := range K {
		if plain[i] != 0 {
			info[i/8] |= 1 << uint(7-i%8)
		}
	}
	return info
}
