// fft.go — pure-Go radix-2 Cooley-Tukey FFT for real-valued input.
//
// This implements a decimation-in-time (DIT) FFT that zero-pads the input
// to the next power of 2. For FT8's 1920-sample symbol frames, this means
// zero-padding to 2048 — the extra bins provide slightly interpolated
// frequency resolution but do not affect the 6.25 Hz tone spacing needed
// for symbol discrimination.
//
// Twiddle factors are precomputed per butterfly stage (not accumulated
// iteratively) to avoid numerical drift. The implementation uses complex128
// internally for twiddle-factor precision, converting to complex64 on
// output to match the float32 audio pipeline.
//
// Performance note: at 2048 points × ~93 FFTs per 15-second window, a
// pure-Go FFT is more than adequate. If profiling reveals a bottleneck,
// the implementation can be swapped for a mixed-radix or CGo FFTW wrapper
// behind the same [RealFFT] interface.
//
// Reference: Cooley, J.W. & Tukey, J.W. "An Algorithm for the Machine
// Calculation of Complex Fourier Series", Mathematics of Computation, 1965.

package dsp

import (
	"math"
	"math/bits"
)

// maxPow2 is the largest power of 2 representable as a positive int.
// On 64-bit systems: 1 << 62 = 4611686018427387904.
const maxPow2 = 1 << (bits.UintSize - 2)

// NextPow2 returns the smallest power of 2 that is ≥ n.
// Returns 1 for n ≤ 1. Panics if n > maxPow2 (the result would overflow int).
func NextPow2(n int) int {
	if n <= 1 {
		return 1
	}
	if n > maxPow2 {
		panic("dsp.NextPow2: n too large, result would overflow int")
	}
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// RealFFT computes the DFT of real-valued samples and returns the
// non-negative frequency bins. The input is zero-padded to the next power
// of 2 (N), and N/2+1 complex bins are returned:
//
//   - Bin 0: DC component
//   - Bin k: frequency k × sampleRate / N Hz
//   - Bin N/2: Nyquist component
//
// Returns nil for empty input.
func RealFFT(samples []float32) []complex64 {
	if len(samples) == 0 {
		return nil
	}

	n := NextPow2(len(samples))

	// Build complex input; zero-padding is implicit (Go zero-initialises).
	x := make([]complex128, n)
	for i, s := range samples {
		x[i] = complex(float64(s), 0)
	}

	fftDIT(x)

	// Return only the non-negative frequency bins.
	bins := n/2 + 1
	out := make([]complex64, bins)
	for i := range bins {
		out[i] = complex64(x[i])
	}
	return out
}

// fftDIT performs an in-place radix-2 decimation-in-time FFT.
// len(x) must be a power of 2.
func fftDIT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	// Step 1: Bit-reversal permutation.
	bitReverse(x)

	// Step 2: Butterfly stages, from 2-point DFTs up to n-point.
	for size := 2; size <= n; size <<= 1 {
		half := size / 2

		// Precompute twiddle factors for this stage. Computing each
		// factor directly from sin/cos avoids the accumulated error of
		// iterative w *= wn multiplication.
		step := -2 * math.Pi / float64(size)
		twiddles := make([]complex128, half)
		for j := range half {
			angle := step * float64(j)
			twiddles[j] = complex(math.Cos(angle), math.Sin(angle))
		}

		// Apply butterfly operations across all groups at this stage.
		for k := 0; k < n; k += size {
			for j := range half {
				t := twiddles[j] * x[k+j+half]
				u := x[k+j]
				x[k+j] = u + t
				x[k+j+half] = u - t
			}
		}
	}
}

// bitReverse performs an in-place bit-reversal permutation of x.
// len(x) must be a power of 2.
func bitReverse(x []complex128) {
	n := len(x)
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}
}
