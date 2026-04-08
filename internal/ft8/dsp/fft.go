// fft.go — pure-Go radix-2 Cooley-Tukey FFT for real-valued input.
//
// This implements a decimation-in-time (DIT) FFT that zero-pads the input
// to the next power of 2. For FT8's 1920-sample symbol frames, this means
// zero-padding to 2048 — the extra bins provide slightly interpolated
// frequency resolution but do not affect the 6.25 Hz tone spacing needed
// for symbol discrimination.
//
// Twiddle factors are precomputed per butterfly stage once per FFT size and
// cached for reuse. Since the hot path (SpectrogramFT8) always uses 2048-
// point transforms, this eliminates ~2000 allocations per 15-second window.
// The cache is protected by a sync.Mutex; contention is negligible because
// the pipeline is single-threaded per window and the lock is only acquired
// once per FFT call (not per stage).
//
// The implementation uses complex128 internally for twiddle-factor precision,
// converting to complex64 on output to match the float32 audio pipeline.
//
// Performance note: at 2048 points × ~188 FFTs per 15-second window, a
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
	"sync"
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

// --- Twiddle factor cache ---

// twiddleTable holds precomputed twiddle factors for all butterfly stages of
// a specific FFT size N. Stage s (0-indexed) corresponds to butterfly size
// 2^(s+1) and contains 2^s twiddle factors.
//
// Each factor is computed directly from sin/cos (not accumulated iteratively)
// to avoid numerical drift.
type twiddleTable struct {
	stages [][]complex128
}

var (
	twiddleMu    sync.Mutex
	twiddleCache = make(map[int]*twiddleTable)
)

// getTwiddles returns cached twiddle factors for an FFT of length n (must be
// a power of 2). On the first call for a given n, the factors are computed
// and cached; subsequent calls return the cached table.
func getTwiddles(n int) *twiddleTable {
	twiddleMu.Lock()
	defer twiddleMu.Unlock()

	if t, ok := twiddleCache[n]; ok {
		return t
	}

	t := &twiddleTable{}
	for size := 2; size <= n; size <<= 1 {
		half := size / 2
		step := -2 * math.Pi / float64(size)
		tw := make([]complex128, half)
		for j := range half {
			angle := step * float64(j)
			tw[j] = complex(math.Cos(angle), math.Sin(angle))
		}
		t.stages = append(t.stages, tw)
	}

	twiddleCache[n] = t
	return t
}

// --- FFT implementation ---

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
	// Twiddle factors are retrieved from the cache (precomputed once per
	// FFT size, reused on every subsequent call).
	tw := getTwiddles(n)
	for s, size := 0, 2; size <= n; s, size = s+1, size<<1 {
		half := size / 2
		twiddles := tw.stages[s]

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
