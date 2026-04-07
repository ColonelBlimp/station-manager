// window_test.go — tests for the Hann window function.

package dsp

import (
	"math"
	"testing"
)

const float32Eps = 1e-6 // tolerance for float32 comparisons

// --- Hann tests ---

// TestHannEndpoints verifies that the Hann window tapers to zero at both
// endpoints: w[0] = w[N−1] = 0.
func TestHannEndpoints(t *testing.T) {
	for _, n := range []int{2, 3, 8, 64, 1920, 2048} {
		samples := onesFloat32(n)
		Hann(samples)

		if samples[0] != 0 {
			t.Errorf("N=%d: w[0] = %g, want 0", n, samples[0])
		}
		if samples[n-1] != 0 {
			t.Errorf("N=%d: w[N-1] = %g, want 0", n, samples[n-1])
		}
	}
}

// TestHannMidpointOdd verifies that for odd N, the midpoint sample equals
// exactly 1.0 (the window peak).
func TestHannMidpointOdd(t *testing.T) {
	for _, n := range []int{3, 5, 9, 65, 1921} {
		samples := onesFloat32(n)
		Hann(samples)

		mid := (n - 1) / 2
		if !approxEq(samples[mid], 1.0, float32Eps) {
			t.Errorf("N=%d: w[mid=%d] = %g, want 1.0", n, mid, samples[mid])
		}
	}
}

// TestHannMidpointEven verifies that for even N, the two centre samples are
// close to 1.0 (since the peak falls between samples). For very small N
// (e.g., 4), the centre values are noticeably below 1.0; for larger N they
// converge towards 1.0.
func TestHannMidpointEven(t *testing.T) {
	for _, n := range []int{64, 1920, 2048} {
		samples := onesFloat32(n)
		Hann(samples)

		// For N ≥ 64 the centre samples are very close to 1.0.
		lo := n/2 - 1
		hi := n / 2
		if samples[lo] < 0.99 || samples[hi] < 0.99 {
			t.Errorf("N=%d: centre samples w[%d]=%g, w[%d]=%g — expected both > 0.99",
				n, lo, samples[lo], hi, samples[hi])
		}
	}
}

// TestHannSymmetry verifies that the Hann window is symmetric:
// w[k] == w[N−1−k] for all k.
func TestHannSymmetry(t *testing.T) {
	for _, n := range []int{3, 8, 64, 1920} {
		samples := onesFloat32(n)
		Hann(samples)

		for k := range n / 2 {
			mirror := n - 1 - k
			if !approxEq(samples[k], samples[mirror], float32Eps) {
				t.Errorf("N=%d: w[%d]=%g != w[%d]=%g (asymmetry)",
					n, k, samples[k], mirror, samples[mirror])
			}
		}
	}
}

// TestHannCoherentGain verifies that the mean of the Hann window
// coefficients is 0.5 (the coherent gain).
func TestHannCoherentGain(t *testing.T) {
	for _, n := range []int{64, 1920, 2048} {
		samples := onesFloat32(n)
		Hann(samples)

		var sum float64
		for _, v := range samples {
			sum += float64(v)
		}
		mean := sum / float64(n)

		// For large N, mean → 0.5. The two endpoint zeros pull the mean
		// slightly below 0.5; tolerance of 0.01 covers N ≥ 64.
		if !approxEq64(mean, 0.5, 0.01) {
			t.Errorf("N=%d: coherent gain (mean) = %g, want ~0.5", n, mean)
		}
	}
}

// TestHannEnergyNormalisation verifies that the sum of squared window
// coefficients (energy) matches the expected value.
//
// For a Hann window of length N, the normalised energy (1/N * Σ w²) is
// exactly 3/8 = 0.375 in the continuous limit. For finite N the discrete
// sum is close to this.
func TestHannEnergyNormalisation(t *testing.T) {
	for _, n := range []int{256, 1920, 2048} {
		samples := onesFloat32(n)
		Hann(samples)

		var sumSq float64
		for _, v := range samples {
			sumSq += float64(v) * float64(v)
		}
		normEnergy := sumSq / float64(n)

		// Continuous limit: 3/8 = 0.375. Discrete sum converges as N
		// grows; tolerance of 0.005 covers N ≥ 256.
		if !approxEq64(normEnergy, 0.375, 0.005) {
			t.Errorf("N=%d: normalised energy = %g, want ~0.375", n, normEnergy)
		}
	}
}

// TestHannNonNegative verifies that all Hann window coefficients are
// non-negative (the window never goes below zero).
func TestHannNonNegative(t *testing.T) {
	samples := onesFloat32(1920)
	Hann(samples)

	for i, v := range samples {
		if v < 0 {
			t.Errorf("w[%d] = %g < 0", i, v)
		}
	}
}

// TestHannInPlace verifies that Hann modifies the input slice in-place
// (multiplying the original values, not replacing them with window
// coefficients).
func TestHannInPlace(t *testing.T) {
	n := 64
	// Start with samples = 2.0 everywhere.
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = 2.0
	}

	// Compute expected: 2.0 * w[i].
	ref := onesFloat32(n)
	Hann(ref)

	Hann(samples)

	for i := range samples {
		want := 2.0 * ref[i]
		if !approxEq(samples[i], want, float32Eps) {
			t.Errorf("samples[%d] = %g, want %g (2.0 * w[%d])", i, samples[i], want, i)
		}
	}
}

// TestHannKnownValues verifies a few Hann coefficients against manually
// computed reference values for N=8.
//
//	w[n] = 0.5 * (1 − cos(2πn/7))
//	w[0] = 0.0
//	w[1] = 0.5 * (1 − cos(2π/7)) ≈ 0.18826
//	w[2] = 0.5 * (1 − cos(4π/7)) ≈ 0.61126
//	w[3] = 0.5 * (1 − cos(6π/7)) ≈ 0.95048
//	w[4] = 0.5 * (1 − cos(8π/7)) ≈ 0.95048
//	w[5] = 0.5 * (1 − cos(10π/7)) ≈ 0.61126
//	w[6] = 0.5 * (1 − cos(12π/7)) ≈ 0.18826
//	w[7] = 0.0
func TestHannKnownValues(t *testing.T) {
	samples := onesFloat32(8)
	Hann(samples)

	// Reference values computed with high-precision math.
	want := [8]float64{
		0.0,
		0.5 * (1 - math.Cos(2*math.Pi*1/7)),
		0.5 * (1 - math.Cos(2*math.Pi*2/7)),
		0.5 * (1 - math.Cos(2*math.Pi*3/7)),
		0.5 * (1 - math.Cos(2*math.Pi*4/7)),
		0.5 * (1 - math.Cos(2*math.Pi*5/7)),
		0.5 * (1 - math.Cos(2*math.Pi*6/7)),
		0.0,
	}

	for i, w := range want {
		if !approxEq(samples[i], float32(w), float32Eps) {
			t.Errorf("w[%d] = %g, want %g", i, samples[i], float32(w))
		}
	}
}

// TestHannEdgeCases verifies behaviour for degenerate inputs.
func TestHannEdgeCases(t *testing.T) {
	// Empty slice — should not panic.
	Hann(nil)
	Hann([]float32{})

	// Single element — should be unmodified (no window effect).
	single := []float32{42.0}
	Hann(single)
	if single[0] != 42.0 {
		t.Errorf("single-element: got %g, want 42.0", single[0])
	}

	// Two elements — both are endpoints, so both become zero.
	pair := []float32{1.0, 1.0}
	Hann(pair)
	if pair[0] != 0 || pair[1] != 0 {
		t.Errorf("two-element: got [%g, %g], want [0, 0]", pair[0], pair[1])
	}
}

// --- HannCoefficients tests ---

// TestHannCoefficientsMatchesHann verifies that HannCoefficients(n) produces
// the same values as applying Hann to an all-ones slice.
func TestHannCoefficientsMatchesHann(t *testing.T) {
	for _, n := range []int{1, 2, 8, 64, 1920} {
		coeffs := HannCoefficients(n)
		if len(coeffs) != n {
			t.Errorf("N=%d: len(HannCoefficients) = %d, want %d", n, len(coeffs), n)
			continue
		}

		ref := onesFloat32(n)
		Hann(ref)

		for i := range n {
			if !approxEq(coeffs[i], ref[i], float32Eps) {
				t.Errorf("N=%d: coeffs[%d] = %g, ref = %g", n, i, coeffs[i], ref[i])
			}
		}
	}
}

// TestHannCoefficientsEdge verifies degenerate inputs.
func TestHannCoefficientsEdge(t *testing.T) {
	if c := HannCoefficients(0); c != nil {
		t.Errorf("HannCoefficients(0) = %v, want nil", c)
	}
	if c := HannCoefficients(-1); c != nil {
		t.Errorf("HannCoefficients(-1) = %v, want nil", c)
	}
	c := HannCoefficients(1)
	if len(c) != 1 || c[0] != 1.0 {
		t.Errorf("HannCoefficients(1) = %v, want [1.0]", c)
	}
}

// --- ApplyWindow tests ---

// TestApplyWindowEquivalence verifies that pre-computed coefficients applied
// via ApplyWindow produce the same result as calling Hann directly.
func TestApplyWindowEquivalence(t *testing.T) {
	n := 1920
	coeffs := HannCoefficients(n)

	samplesA := make([]float32, n)
	samplesB := make([]float32, n)
	for i := range n {
		v := float32(i) * 0.01
		samplesA[i] = v
		samplesB[i] = v
	}

	Hann(samplesA)
	ApplyWindow(samplesB, coeffs)

	for i := range n {
		if !approxEq(samplesA[i], samplesB[i], float32Eps) {
			t.Errorf("index %d: Hann=%g, ApplyWindow=%g", i, samplesA[i], samplesB[i])
		}
	}
}

// TestApplyWindowMismatchedLengths verifies that ApplyWindow handles
// mismatched slice lengths without panic, applying up to the shorter.
func TestApplyWindowMismatchedLengths(t *testing.T) {
	samples := []float32{1, 2, 3, 4, 5}
	window := []float32{0.5, 0.5, 0.5} // shorter than samples

	ApplyWindow(samples, window)

	// First 3 elements should be halved; last 2 should be unchanged.
	want := []float32{0.5, 1.0, 1.5, 4, 5}
	for i, w := range want {
		if !approxEq(samples[i], w, float32Eps) {
			t.Errorf("samples[%d] = %g, want %g", i, samples[i], w)
		}
	}
}

// --- FT8-specific tests ---

// TestHannFT8SymbolSize verifies Hann behaviour at the FT8 FFT frame size
// (1920 samples = one symbol period at 12 kHz).
func TestHannFT8SymbolSize(t *testing.T) {
	n := SamplesPerSymbol // 1920
	samples := onesFloat32(n)
	Hann(samples)

	// Endpoints must be zero.
	if samples[0] != 0 {
		t.Errorf("FT8 frame: w[0] = %g, want 0", samples[0])
	}
	if samples[n-1] != 0 {
		t.Errorf("FT8 frame: w[N-1] = %g, want 0", samples[n-1])
	}

	// Peak should be at or near the centre.
	peak := float32(0)
	peakIdx := 0
	for i, v := range samples {
		if v > peak {
			peak = v
			peakIdx = i
		}
	}

	centre := n / 2
	if abs(peakIdx-centre) > 1 {
		t.Errorf("FT8 frame: peak at index %d, expected near centre %d", peakIdx, centre)
	}
	if !approxEq(peak, 1.0, 0.001) {
		t.Errorf("FT8 frame: peak value = %g, want ~1.0", peak)
	}
}

// --- Test helpers ---

// onesFloat32 returns a []float32 of length n with all elements set to 1.0.
func onesFloat32(n int) []float32 {
	s := make([]float32, n)
	for i := range s {
		s[i] = 1.0
	}
	return s
}

// approxEq returns true if |a − b| ≤ tolerance.
func approxEq(a, b float32, tolerance float32) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tolerance
}

// approxEq64 returns true if |a − b| ≤ tolerance (float64 version).
func approxEq64(a, b, tolerance float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d <= tolerance
}

// abs returns the absolute value of an int.
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
