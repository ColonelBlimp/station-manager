// decoder.go — LDPC(174,91) sum-product belief-propagation decoder.
//
// The decoder takes 174 log-likelihood ratios and iteratively refines hard
// bit decisions until all 83 parity checks pass or the iteration limit is
// reached.
//
// LLR sign convention: positive means bit more likely 0, negative means bit
// more likely 1.
//
// Reference algorithm: sum-product BP using tanh/atanh, matching WSJT-X
// decode174_91.f90 lines 120–133. Collects cumulative zsum snapshots for
// OSD fallback (lines 51–64).

package codec

import "math"

// platanh is a piecewise-linear approximation of atanh(x), clamped at ±7.0.
// Matches WSJT-X lib/platanh.f90 exactly.
func platanh(x float32) float32 {
	sign := float32(1.0)
	z := x
	if x < 0 {
		sign = -1.0
		z = -x
	}
	switch {
	case z <= 0.664:
		return x / 0.83
	case z <= 0.9217:
		return sign * (z - 0.4064) / 0.322
	case z <= 0.9951:
		return sign * (z - 0.8378) / 0.0524
	case z <= 0.9998:
		return sign * (z - 0.9914) / 0.0012
	default:
		return sign * 7.0
	}
}

// Decode performs sum-product belief-propagation decoding of a
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
	var apmask [N]uint8
	info, _, ok = bpDecode(llr, apmask, maxIter)
	return
}

// DecodeWithZsave performs sum-product BP decoding and also returns
// cumulative zsum snapshots from iterations 1–3 for OSD fallback.
// This is the entry point used by [DecodeMessage] to get both the BP
// result and zsave in a single pass.
func DecodeWithZsave(llr [N]float32, maxIter int) (info [KBytes]byte, zsave [3][N]float32, ok bool) {
	var apmask [N]uint8
	return bpDecode(llr, apmask, maxIter)
}

// DecodeAP performs sum-product belief-propagation decoding with
// a priori (AP) mask support.
//
// For bits where apmask[i]==1, the a-posteriori LLR is held at the channel
// value (extrinsic messages from check nodes are ignored), matching WSJT-X
// decode174_91.f90 lines 54–59. This prevents the BP iterations from
// "washing out" the injected AP information.
//
// The function also accumulates zsum snapshots for up to 3 OSD fallback
// calls, matching WSJT-X's maxosd=2 behaviour.
func DecodeAP(llr [N]float32, apmask [N]uint8, maxIter int) (info [KBytes]byte, zsave [3][N]float32, ok bool) {
	return bpDecode(llr, apmask, maxIter)
}

// bpDecode is the unified sum-product BP implementation. It performs BP
// iterations with convergence checking AND collects cumulative zsum
// snapshots at iterations 1, 2, and 3 for OSD fallback.
//
// This matches WSJT-X decode174_91.f90:
//   - Sum-product check→variable update (tanh/atanh, lines 120–133)
//   - zsum accumulation and zsave snapshots (lines 51–64)
//   - Convergence check each iteration (lines 66–89)
//   - Early stopping when syndrome isn't improving (lines 91–104)
func bpDecode(llr [N]float32, apmask [N]uint8, maxIter int) (info [KBytes]byte, zsave [3][N]float32, ok bool) {
	var tov [N][3]float32
	var toc [M][7]float32
	var tanhtoc [M][7]float32
	var zn [N]float32
	var zsum [N]float32
	var plain [N]uint8

	iterations := maxIter
	if iterations > 30 {
		iterations = 30
	}

	nclast := 0
	ncnt := 0

	for iter := range iterations {
		// --- A-posteriori LLR ---
		// zn = channel LLR + all incoming check→variable messages.
		// For AP-masked bits, ignore extrinsic messages (hold at channel value).
		for n := range N {
			if apmask[n] == 1 {
				zn[n] = llr[n]
			} else {
				zn[n] = llr[n] + tov[n][0] + tov[n][1] + tov[n][2]
			}
		}

		// --- Accumulate zsum and save zsave snapshots ---
		// Matches WSJT-X decode174_91.f90 lines 61–64.
		for n := range N {
			zsum[n] += zn[n]
		}
		if iter >= 1 && iter <= 3 {
			copy(zsave[iter-1][:], zsum[:])
		}

		// --- Hard decision ---
		plainSum := 0
		for n := range N {
			if zn[n] < 0 {
				plain[n] = 1
			} else {
				plain[n] = 0
			}
			plainSum += int(plain[n])
		}

		// All-zero codeword is a trivial fixed point of BP — reject it.
		if plainSum == 0 {
			return info, zsave, false
		}

		// --- Syndrome check ---
		if syndromeOK(&plain) {
			return packInfoBits(&plain), zsave, true
		}

		// --- Early stopping criterion (WSJT-X lines 91–104) ---
		if iter > 0 {
			ncheck := 0
			for m := range M {
				deg := int(NmCount[m])
				var x uint8
				for i := range deg {
					x ^= plain[int(Nm[m][i])-1]
				}
				if x != 0 {
					ncheck++
				}
			}
			nd := ncheck - nclast
			if nd < 0 {
				ncnt = 0
			} else {
				ncnt++
			}
			if ncnt >= 5 && iter >= 10 && ncheck > 15 {
				break // BP is stuck, exit early
			}
			nclast = ncheck
		}

		// --- Variable→Check update ---
		// toc[m][nIdx] = zn[n] - tov from check m (extrinsic message)
		// Matches WSJT-X decode174_91.f90 lines 108–118.
		for m := range M {
			deg := int(NmCount[m])
			for nIdx := range deg {
				n := int(Nm[m][nIdx]) - 1
				q := zn[n]
				for kk := range 3 {
					if int(Mn[n][kk])-1 == m {
						q -= tov[n][kk]
					}
				}
				toc[m][nIdx] = q
			}
		}

		// --- Check→Variable update (sum-product) ---
		// tov[n][mIdx] = 2 × atanh(∏ tanh(toc_k / 2)) for k ≠ n
		//
		// Note: WSJT-X uses tanh(-toc/2) and atanh(-Tmn) because its LLR
		// convention is positive=likely 1. Our convention is positive=likely 0,
		// so we use tanh(toc/2) and atanh(prod) — no negations.

		// Pre-compute tanh(toc/2) for all check→variable messages.
		for m := range M {
			deg := int(NmCount[m])
			for nIdx := range deg {
				tanhtoc[m][nIdx] = float32(math.Tanh(float64(toc[m][nIdx] / 2)))
			}
		}

		// Compute tov from product of tanhtoc, excluding the current variable.
		for n := range N {
			for mIdx := range 3 {
				m := int(Mn[n][mIdx]) - 1
				deg := int(NmCount[m])

				prod := float32(1.0)
				for nIdx := range deg {
					if int(Nm[m][nIdx])-1 != n {
						prod *= tanhtoc[m][nIdx]
					}
				}

				tov[n][mIdx] = 2 * platanh(prod)
			}
		}
	}

	return info, zsave, false
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
