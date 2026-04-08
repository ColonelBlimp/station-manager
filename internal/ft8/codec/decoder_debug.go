// decoder_debug.go — instrumented LDPC decoder for diagnosing BP convergence.
//
// [DecodeDebug] is identical to [Decode] but records per-iteration metrics
// that reveal why belief propagation is (or is not) converging:
//
//   - Syndrome weight: number of unsatisfied parity checks (83 → 0 is success)
//   - Hard-bit flips: bits that changed polarity since the last iteration
//   - Mean / max |APP|: a-posteriori LLR magnitudes (growing = good)
//   - Mean / max |tov|: check→variable message magnitudes
//   - Ones count: number of hard-decision 1-bits (should be ~87 for a
//     typical FT8 codeword; if stuck near 0 or 174, LLRs are biased)
//
// This function is intended for diagnostic tests only — it allocates
// per-iteration structs and should not be used on the hot path.

package codec

import (
	"fmt"
	"math"
	"strings"
)

// IterStats holds diagnostic metrics for a single BP iteration.
type IterStats struct {
	Iter           int     // 0-based iteration number
	SyndromeWeight int     // number of unsatisfied parity checks (0 = converged)
	BitFlips       int     // hard-decision bits that changed since previous iteration
	OnesCount      int     // number of hard-decision 1-bits
	MeanAbsAPP     float64 // mean |a-posteriori LLR| across all 174 bits
	MaxAbsAPP      float64 // max |a-posteriori LLR|
	MeanAbsTov     float64 // mean |check→variable message| across all edges
	MaxAbsTov      float64 // max |check→variable message|
}

// String returns a compact one-line summary of the iteration stats.
func (s IterStats) String() string {
	return fmt.Sprintf("iter=%2d  syndrome=%2d  flips=%3d  ones=%3d  "+
		"APP(mean=%.2f max=%.2f)  tov(mean=%.3f max=%.3f)",
		s.Iter, s.SyndromeWeight, s.BitFlips, s.OnesCount,
		s.MeanAbsAPP, s.MaxAbsAPP, s.MeanAbsTov, s.MaxAbsTov)
}

// DecodeResult contains the full diagnostic output of [DecodeDebug].
type DecodeResult struct {
	// Info is the decoded 91-bit payload (valid only when OK is true).
	Info [KBytes]byte

	// OK is true if decoding converged and all parity checks pass.
	OK bool

	// Iterations is the number of BP iterations actually performed.
	Iterations int

	// Stats contains per-iteration diagnostic metrics.
	Stats []IterStats

	// InputStats summarises the input LLR vector.
	InputMeanAbs   float64 // mean |LLR| of the 174 input values
	InputMaxAbs    float64 // max |LLR| of the 174 input values
	InputPosCount  int     // number of positive (bit=0) input LLRs
	InputNegCount  int     // number of negative (bit=1) input LLRs
	InputZeroCount int     // number of exactly-zero input LLRs
}

// Summary returns a multi-line human-readable diagnostic summary.
func (r DecodeResult) Summary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "=== LDPC Decode Debug (%d iterations, converged=%v) ===\n",
		r.Iterations, r.OK)
	fmt.Fprintf(&b, "Input LLR: mean|LLR|=%.3f max|LLR|=%.3f pos=%d neg=%d zero=%d\n",
		r.InputMeanAbs, r.InputMaxAbs, r.InputPosCount, r.InputNegCount, r.InputZeroCount)

	for _, s := range r.Stats {
		fmt.Fprintf(&b, "  %s\n", s.String())
	}

	if r.OK {
		fmt.Fprintf(&b, "RESULT: CONVERGED at iteration %d\n", r.Iterations)
	} else if len(r.Stats) > 0 {
		last := r.Stats[len(r.Stats)-1]
		fmt.Fprintf(&b, "RESULT: FAILED — final syndrome=%d\n", last.SyndromeWeight)
	}
	return b.String()
}

// DecodeDebug performs normalised min-sum BP decoding identical to [Decode]
// but records per-iteration diagnostic metrics.
//
// See [Decode] for parameter semantics.
func DecodeDebug(llr [N]float32, maxIter int) DecodeResult {
	var result DecodeResult

	// Compute input LLR statistics.
	for i := range N {
		a := math.Abs(float64(llr[i]))
		result.InputMeanAbs += a
		if a > result.InputMaxAbs {
			result.InputMaxAbs = a
		}
		if llr[i] > 0 {
			result.InputPosCount++
		} else if llr[i] < 0 {
			result.InputNegCount++
		} else {
			result.InputZeroCount++
		}
	}
	result.InputMeanAbs /= float64(N)

	var tov [N][3]float32
	var toc [M][7]float32
	var plain [N]uint8
	var prevPlain [N]uint8

	for iter := range maxIter {
		result.Iterations = iter + 1

		var stats IterStats
		stats.Iter = iter

		// --- Hard decision ---
		var appSum, appMax float64
		plainSum := 0
		for n := range N {
			app := llr[n] + tov[n][0] + tov[n][1] + tov[n][2]
			a := math.Abs(float64(app))
			appSum += a
			if a > appMax {
				appMax = a
			}
			if app < 0 {
				plain[n] = 1
			} else {
				plain[n] = 0
			}
			plainSum += int(plain[n])
		}
		stats.MeanAbsAPP = appSum / float64(N)
		stats.MaxAbsAPP = appMax
		stats.OnesCount = plainSum

		// Count bit flips from previous iteration.
		if iter > 0 {
			for n := range N {
				if plain[n] != prevPlain[n] {
					stats.BitFlips++
				}
			}
		}
		copy(prevPlain[:], plain[:])

		// All-zero codeword rejection.
		if plainSum == 0 {
			stats.SyndromeWeight = -1 // sentinel: all-zero rejected
			result.Stats = append(result.Stats, stats)
			return result
		}

		// --- Syndrome check ---
		sw := syndromeWeight(&plain)
		stats.SyndromeWeight = sw

		// Compute tov statistics.
		var tovSum, tovMax float64
		var tovCount int
		for n := range N {
			for j := range 3 {
				a := math.Abs(float64(tov[n][j]))
				tovSum += a
				if a > tovMax {
					tovMax = a
				}
				tovCount++
			}
		}
		if tovCount > 0 {
			stats.MeanAbsTov = tovSum / float64(tovCount)
		}
		stats.MaxAbsTov = tovMax

		result.Stats = append(result.Stats, stats)

		if sw == 0 {
			result.Info = packInfoBits(&plain)
			result.OK = true
			return result
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

	return result
}

// syndromeWeight returns the number of unsatisfied parity checks (0–83).
func syndromeWeight(plain *[N]uint8) int {
	w := 0
	for m := range M {
		deg := int(NmCount[m])
		var x uint8
		for i := range deg {
			x ^= plain[int(Nm[m][i])-1]
		}
		if x != 0 {
			w++
		}
	}
	return w
}
