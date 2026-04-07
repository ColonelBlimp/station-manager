// window.go — window functions for spectral analysis.
//
// Before computing an FFT on a finite audio segment, a window function is
// applied to taper the samples smoothly to zero at the edges. This reduces
// spectral leakage — the smearing of energy across frequency bins caused by
// the abrupt truncation of the signal at the buffer boundaries.
//
// The Hann window (also called "Hanning" or raised-cosine window) is the
// standard choice for FT8 DSP work. It offers a good trade-off between
// main-lobe width and side-lobe suppression.
//
// Reference: Harris, F.J. "On the Use of Windows for Harmonic Analysis with
// the Discrete Fourier Transform", Proceedings of the IEEE, 1978.

package dsp

import "math"

// Hann applies an in-place Hann (raised-cosine) window to the samples.
//
// The Hann window is defined as:
//
//	w[n] = 0.5 * (1 − cos(2πn / (N−1)))
//
// where N = len(samples) and n = 0, 1, ..., N−1.
//
// Properties:
//   - Endpoints: w[0] = w[N−1] = 0
//   - Midpoint:  w[(N−1)/2] = 1.0 (for odd N), or close to 1.0 (even N)
//   - Coherent gain: mean(w) = 0.5
//   - Side-lobe suppression: ~31 dB below the main lobe
//
// For a single-sample or empty slice, Hann is a no-op.
func Hann(samples []float32) {
	n := len(samples)
	if n <= 1 {
		return
	}
	// Precompute 2π / (N−1). Using float64 for the trig computation avoids
	// accumulating float32 rounding error across many samples.
	inv := 2 * math.Pi / float64(n-1)
	for i := range samples {
		w := 0.5 * (1.0 - math.Cos(inv*float64(i)))
		samples[i] *= float32(w)
	}
}

// HannCoefficients returns a pre-computed Hann window of length n.
//
// This is useful when the same window size is applied repeatedly (e.g.,
// in the spectrogram builder) and the cost of recomputing the cosines
// per FFT frame should be avoided.
//
// Returns nil if n ≤ 0.
func HannCoefficients(n int) []float32 {
	if n <= 0 {
		return nil
	}
	if n == 1 {
		return []float32{1.0}
	}
	w := make([]float32, n)
	inv := 2 * math.Pi / float64(n-1)
	for i := range w {
		w[i] = float32(0.5 * (1.0 - math.Cos(inv*float64(i))))
	}
	return w
}

// ApplyWindow multiplies samples by the corresponding window coefficients
// in-place. If the slices differ in length, ApplyWindow applies up to the
// shorter length (no panic).
func ApplyWindow(samples, window []float32) {
	n := len(samples)
	if len(window) < n {
		n = len(window)
	}
	for i := range n {
		samples[i] *= window[i]
	}
}
