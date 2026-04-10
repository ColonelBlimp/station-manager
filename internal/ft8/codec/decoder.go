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
	var apmask [N]uint8
	return decodeInternal(llr, apmask, maxIter)
}

// DecodeAP performs normalised min-sum belief-propagation decoding with
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
	info, ok = decodeInternal(llr, apmask, maxIter)
	if ok {
		return
	}

	// If BP failed, produce zsave snapshots for OSD.
	// We re-run BP to collect the cumulative zsum at iterations 1, 2, 3.
	// This matches WSJT-X decode174_91.f90 lines 61–64.
	zsave = bpCollectZsave(llr, apmask, maxIter)
	return
}

// decodeInternal is the shared BP implementation supporting both regular
// and AP-masked decoding.
func decodeInternal(llr [N]float32, apmask [N]uint8, maxIter int) (info [KBytes]byte, ok bool) {
	var tov [N][3]float32
	var toc [M][7]float32
	var plain [N]uint8

	for range maxIter {
		// --- Hard decision ---
		// A-posteriori LLR = channel LLR + all incoming check→variable messages.
		// For AP-masked bits, ignore extrinsic messages (hold at channel value).
		plainSum := 0
		for n := range N {
			var app float32
			if apmask[n] == 1 {
				app = llr[n]
			} else {
				app = llr[n] + tov[n][0] + tov[n][1] + tov[n][2]
			}
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
		for m := range M {
			deg := int(NmCount[m])
			for nIdx := range deg {
				n := int(Nm[m][nIdx]) - 1
				q := llr[n]
				for mIdx := range 3 {
					if int(Mn[n][mIdx])-1 != m {
						q += tov[n][mIdx]
					}
				}
				toc[m][nIdx] = q
			}
		}

		// --- Check→Variable update (normalised min-sum) ---
		for n := range N {
			for mIdx := range 3 {
				m := int(Mn[n][mIdx]) - 1
				deg := int(NmCount[m])

				sign := float32(1.0)
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

// bpCollectZsave runs BP iterations and collects cumulative zsum snapshots
// at iterations 1, 2, and 3 for use by the OSD fallback decoder.
// This matches WSJT-X decode174_91.f90 lines 51–64.
func bpCollectZsave(llr [N]float32, apmask [N]uint8, maxIter int) [3][N]float32 {
	var zsave [3][N]float32
	var tov [N][3]float32
	var toc [M][7]float32
	var zsum [N]float32

	iterations := maxIter
	if iterations > 30 {
		iterations = 30
	}

	for iter := range iterations {
		// Compute zn (a-posteriori LLR).
		var zn [N]float32
		for n := range N {
			if apmask[n] == 1 {
				zn[n] = llr[n]
			} else {
				zn[n] = llr[n] + tov[n][0] + tov[n][1] + tov[n][2]
			}
		}

		// Accumulate into zsum.
		for n := range N {
			zsum[n] += zn[n]
		}

		// Save snapshots at iterations 1, 2, 3 (0-indexed).
		if iter >= 1 && iter <= 3 {
			copy(zsave[iter-1][:], zsum[:])
		}

		// Variable→Check update.
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

		// Check→Variable update (normalised min-sum).
		for n := range N {
			for mIdx := range 3 {
				m := int(Mn[n][mIdx]) - 1
				deg := int(NmCount[m])

				sign := float32(1.0)
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

	return zsave
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
