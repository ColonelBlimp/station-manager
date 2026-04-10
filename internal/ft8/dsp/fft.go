// fft.go — pure-Go FFT for real-valued input.
//
// Two DFT entry points are provided:
//
//   - [RealFFT]: zero-pads to the next power of 2, uses radix-2 Cooley-Tukey.
//     Retained for backward compatibility.
//   - [RealFFTN]: computes an N-point DFT for arbitrary N. Uses radix-2 when
//     N is a power of 2, mixed-radix Cooley-Tukey for 5-smooth N (factors of
//     2, 3, 5 only), and Bluestein's algorithm (chirp-Z) otherwise.
//
// The FT8-critical use cases are all 5-smooth:
//   - N = 3840  (= 2⁸×3×5):  spectrogram FFT, 2 bins per FT8 tone
//   - N = 192000 (= 2⁹×3×5³): long FFT in baseband pipeline
//   - N = 3200  (= 2⁷×5²):   short IFFT in baseband pipeline
//
// Twiddle factors are precomputed and cached per FFT size. Since the hot
// paths always reuse the same sizes, tables are computed once and reused.
//
// See fft_mixedradix.go for the mixed-radix implementation.
//
// Reference: Bluestein, L.I. "A linear filtering approach to the computation
// of discrete Fourier transform", IEEE Trans. Audio Electroacoustics, 1970.

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
//
// For FT8 use, prefer [RealFFTN] with an explicit FFT size to control bin
// alignment with FT8 tone spacing.
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

// RealFFTN computes an N-point DFT of real-valued samples and returns the
// non-negative frequency bins (N/2 + 1 bins).
//
// If len(samples) < n, the input is zero-padded. If len(samples) > n, the
// input is truncated. Algorithm selection:
//   - Power of 2: radix-2 Cooley-Tukey
//   - 5-smooth (factors of 2, 3, 5 only): mixed-radix Cooley-Tukey
//   - Other: Bluestein's algorithm (chirp-Z)
//
// Returns nil for n ≤ 0.
func RealFFTN(samples []float32, n int) []complex64 {
	if n <= 0 {
		return nil
	}

	// Power-of-2 fast path: use the existing radix-2 FFT.
	if n > 0 && (n&(n-1)) == 0 {
		x := make([]complex128, n)
		for i := 0; i < len(samples) && i < n; i++ {
			x[i] = complex(float64(samples[i]), 0)
		}
		fftDIT(x)
		bins := n/2 + 1
		out := make([]complex64, bins)
		for i := range bins {
			out[i] = complex64(x[i])
		}
		return out
	}

	// Non-power-of-2: mixed-radix (5-smooth) or Bluestein.
	x := make([]complex128, n)
	for i := 0; i < len(samples) && i < n; i++ {
		x[i] = complex(float64(samples[i]), 0)
	}

	generalDFT(x)

	bins := n/2 + 1
	out := make([]complex64, bins)
	for i := range bins {
		out[i] = complex64(x[i])
	}
	return out
}

// --- Bluestein's algorithm ---

// bluesteinTab holds precomputed chirp sequences and the FFT of the
// convolution kernel for a specific DFT size N. Cached to avoid
// recomputation across spectrogram frames.
type bluesteinTab struct {
	chirp []complex128 // chirp[k] = exp(-jπk²/N) for k = 0..N-1
	hFFT  []complex128 // FFT of the zero-padded chirp kernel, length M
	m     int          // convolution length (power of 2, ≥ 2N-1)
}

var (
	bluesteinMu    sync.Mutex
	bluesteinCache = make(map[int]*bluesteinTab)
)

// getBluestein returns the cached Bluestein table for DFT size n. On the
// first call for a given n, the chirp sequence and kernel FFT are computed.
func getBluestein(n int) *bluesteinTab {
	bluesteinMu.Lock()
	defer bluesteinMu.Unlock()

	if tab, ok := bluesteinCache[n]; ok {
		return tab
	}

	m := NextPow2(2*n - 1)

	// Chirp: b[k] = exp(-jπk²/N).
	chirp := make([]complex128, n)
	for k := range n {
		angle := -math.Pi * float64(k) * float64(k) / float64(n)
		chirp[k] = complex(math.Cos(angle), math.Sin(angle))
	}

	// Convolution kernel h: conj(chirp) with circular wrap-around.
	// h[0..N-1] = conj(chirp[0..N-1])
	// h[M-k]    = conj(chirp[k]) for k = 1..N-1  (wrap-around)
	h := make([]complex128, m)
	for k := range n {
		c := complex(real(chirp[k]), -imag(chirp[k])) // conj
		h[k] = c
		if k > 0 {
			h[m-k] = c
		}
	}

	// Precompute FFT of h.
	fftDIT(h)

	tab := &bluesteinTab{chirp: chirp, hFFT: h, m: m}
	bluesteinCache[n] = tab
	return tab
}

// bluesteinDFT computes the in-place N-point DFT of x using Bluestein's
// chirp-Z algorithm. len(x) = N, which can be any positive integer.
//
// The algorithm converts the DFT into a circular convolution:
//
//	X[k] = chirp[k] · (a ∗ h)[k]
//
// where a[n] = x[n]·chirp[n] and h is the conjugate chirp kernel.
// The convolution is computed via power-of-2 FFTs of size M ≥ 2N-1.
func bluesteinDFT(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	tab := getBluestein(n)
	m := tab.m

	// Modulated input: a[k] = x[k] · chirp[k], zero-padded to M.
	a := make([]complex128, m)
	for k := range n {
		a[k] = x[k] * tab.chirp[k]
	}

	// Circular convolution via FFT: Y = IFFT(FFT(a) · H).
	fftDIT(a)

	for k := range m {
		a[k] *= tab.hFFT[k]
	}

	// Inverse FFT: IFFT(z) = conj(FFT(conj(z))) / M.
	for k := range m {
		a[k] = complex(real(a[k]), -imag(a[k]))
	}
	fftDIT(a)
	invM := 1.0 / float64(m)
	for k := range m {
		a[k] = complex(real(a[k])*invM, -imag(a[k])*invM)
	}

	// Extract result: X[k] = chirp[k] · conv[k].
	for k := range n {
		x[k] = tab.chirp[k] * a[k]
	}
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
