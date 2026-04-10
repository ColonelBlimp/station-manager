// fft_mixedradix_test.go — tests for the mixed-radix Cooley-Tukey FFT.

package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

// --- is5Smooth tests ---

func TestIs5Smooth(t *testing.T) {
	tests := []struct {
		n    int
		want bool
	}{
		{0, false},
		{-1, false},
		{1, true},
		{2, true},
		{3, true},
		{4, true},
		{5, true},
		{6, true},      // 2×3
		{7, false},     // prime > 5
		{8, true},      // 2³
		{9, true},      // 3²
		{10, true},     // 2×5
		{11, false},    // prime
		{12, true},     // 2²×3
		{15, true},     // 3×5
		{16, true},     // 2⁴
		{25, true},     // 5²
		{30, true},     // 2×3×5
		{100, true},    // 2²×5²
		{128, true},    // 2⁷
		{3840, true},   // 2⁸×3×5 — spectrogram FFT
		{3200, true},   // 2⁷×5² — NFFT2
		{192000, true}, // 2⁹×3×5³ — NFFT1
		{7, false},
		{11, false},
		{13, false},
		{17, false},
		{14, false}, // 2×7
		{21, false}, // 3×7
		{35, false}, // 5×7
	}
	for _, tc := range tests {
		got := is5Smooth(tc.n)
		if got != tc.want {
			t.Errorf("is5Smooth(%d) = %v, want %v", tc.n, got, tc.want)
		}
	}
}

// --- factorize235 tests ---

func TestFactorize235(t *testing.T) {
	tests := []struct {
		n       int
		wantLen int // number of factors
	}{
		{1, 0},
		{2, 1},       // [2]
		{3, 1},       // [3]
		{5, 1},       // [5]
		{6, 2},       // [3, 2]
		{8, 3},       // [2, 2, 2]
		{10, 2},      // [5, 2]
		{15, 2},      // [5, 3]
		{30, 3},      // [5, 3, 2]
		{3840, 10},   // 5, 3, 2, 2, 2, 2, 2, 2, 2, 2 — 2⁸×3×5
		{3200, 9},    // 5, 5, 2, 2, 2, 2, 2, 2, 2 — 2⁷×5²
		{192000, 13}, // 5, 5, 5, 3, 2, 2, 2, 2, 2, 2, 2, 2, 2 — 2⁹×3×5³
	}
	for _, tc := range tests {
		factors := factorize235(tc.n)
		if len(factors) != tc.wantLen {
			t.Errorf("factorize235(%d): got %d factors %v, want %d factors",
				tc.n, len(factors), factors, tc.wantLen)
			continue
		}
		// Verify product equals n
		product := 1
		for _, f := range factors {
			product *= f
		}
		if product != tc.n {
			t.Errorf("factorize235(%d): product of %v = %d, want %d",
				tc.n, factors, product, tc.n)
		}
		// Verify largest-first ordering
		for i := 1; i < len(factors); i++ {
			if factors[i] > factors[i-1] {
				t.Errorf("factorize235(%d): not largest-first: %v", tc.n, factors)
				break
			}
		}
	}
}

// --- Mixed-radix DFT vs brute-force DFT ---

// TestMixedRadixDFT_AgainstDFT tests the mixed-radix FFT against brute-force
// DFT for small 5-smooth sizes.
func TestMixedRadixDFT_AgainstDFT(t *testing.T) {
	sizes := []int{2, 3, 4, 5, 6, 8, 9, 10, 12, 15, 16, 18, 20, 24, 25,
		27, 30, 32, 36, 40, 45, 48, 50, 60, 64, 72, 75, 80, 90, 96,
		100, 120, 125, 128, 144, 150, 160, 180, 192, 200, 240, 250,
		256, 270, 288, 300, 320, 360, 375, 384, 400, 432, 450, 480,
		500, 512, 540, 576, 600, 625, 640, 720, 750, 768, 800, 900,
		960, 1000}

	// Pseudo-random test signal.
	maxN := 1000
	signal := make([]complex128, maxN)
	for i := range signal {
		signal[i] = complex(
			math.Sin(float64(i)*0.7+0.3)+0.5*math.Cos(float64(i)*2.1),
			math.Cos(float64(i)*1.3-0.7)+0.3*math.Sin(float64(i)*0.9),
		)
	}

	for _, n := range sizes {
		if !is5Smooth(n) {
			continue
		}
		t.Run(intToName(n), func(t *testing.T) {
			// Prepare input
			x := make([]complex128, n)
			copy(x, signal[:n])

			// Compute via mixed-radix
			mixed := make([]complex128, n)
			copy(mixed, x)
			mixedRadixDFT(mixed)

			// Brute-force DFT
			for k := range n {
				var sum complex128
				for i := range n {
					angle := -2 * math.Pi * float64(k) * float64(i) / float64(n)
					sum += x[i] * complex(math.Cos(angle), math.Sin(angle))
				}
				diff := cmplx.Abs(mixed[k] - sum)
				mag := cmplx.Abs(sum)
				tol := 1e-8 * float64(n) // scale tolerance with N
				if mag > 1 {
					tol = 1e-8 * float64(n) * mag
				}
				if diff > tol {
					t.Errorf("bin[%d]: mixed=%v, DFT=%v, diff=%g",
						k, mixed[k], sum, diff)
				}
			}
		})
	}
}

// TestMixedRadixDFT_MatchesBluestein verifies that mixedRadixDFT produces
// identical results to bluesteinDFT for the three hot-path FT8 sizes.
func TestMixedRadixDFT_MatchesBluestein(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large FFT comparison in short mode")
	}

	sizes := []struct {
		name string
		n    int
	}{
		{"3840_spectrogram", 3840},
		{"3200_NFFT2", 3200},
	}

	// Pseudo-random signal
	maxN := 4000
	signal := make([]complex128, maxN)
	for i := range signal {
		signal[i] = complex(
			math.Sin(float64(i)*0.7+0.3)+0.5*math.Cos(float64(i)*2.1),
			math.Cos(float64(i)*1.3-0.7)+0.3*math.Sin(float64(i)*0.9),
		)
	}

	for _, tc := range sizes {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.n

			// Mixed-radix
			xMR := make([]complex128, n)
			copy(xMR, signal[:n])
			mixedRadixDFT(xMR)

			// Bluestein
			xBS := make([]complex128, n)
			copy(xBS, signal[:n])
			bluesteinDFT(xBS)

			// Compare
			maxDiff := 0.0
			for k := range n {
				diff := cmplx.Abs(xMR[k] - xBS[k])
				if diff > maxDiff {
					maxDiff = diff
				}
			}

			// Tolerance: relative to N and signal magnitude
			tol := 1e-6 * float64(n)
			if maxDiff > tol {
				t.Errorf("max diff = %g, tolerance = %g", maxDiff, tol)
			}
			t.Logf("N=%d: max diff between mixed-radix and Bluestein = %g", n, maxDiff)
		})
	}
}

// TestMixedRadixDFT_192000_MatchesBluestein compares the 192k-point FFT.
func TestMixedRadixDFT_192000_MatchesBluestein(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping 192k-point FFT comparison in short mode")
	}

	n := NFFT1 // 192000

	// Pseudo-random signal (shorter generation for speed)
	x := make([]complex128, n)
	for i := range n {
		x[i] = complex(
			math.Sin(float64(i)*0.0007+0.3),
			math.Cos(float64(i)*0.0013-0.7),
		)
	}

	// Mixed-radix
	xMR := make([]complex128, n)
	copy(xMR, x)
	mixedRadixDFT(xMR)

	// Bluestein
	xBS := make([]complex128, n)
	copy(xBS, x)
	bluesteinDFT(xBS)

	// Sample comparison (checking every bin of 192k would be slow)
	maxDiff := 0.0
	checkBins := []int{0, 1, 2, n / 4, n / 3, n/2 - 1, n / 2, n - 1}
	for _, k := range checkBins {
		diff := cmplx.Abs(xMR[k] - xBS[k])
		if diff > maxDiff {
			maxDiff = diff
		}
	}

	// Also spot-check 1000 evenly-spaced bins
	step := n / 1000
	for k := 0; k < n; k += step {
		diff := cmplx.Abs(xMR[k] - xBS[k])
		if diff > maxDiff {
			maxDiff = diff
		}
	}

	tol := 1e-3 * float64(n) // generous for 192k
	if maxDiff > tol {
		t.Errorf("max diff = %g, tolerance = %g", maxDiff, tol)
	}
	t.Logf("N=%d: max sampled diff between mixed-radix and Bluestein = %g", n, maxDiff)
}

// --- Parseval's theorem for mixed-radix sizes ---

// TestMixedRadixParseval verifies energy conservation for 5-smooth sizes.
func TestMixedRadixParseval(t *testing.T) {
	sizes := []int{3, 5, 6, 9, 10, 12, 15, 18, 20, 24, 25, 27, 30,
		36, 45, 48, 50, 60, 72, 75, 80, 90, 100, 120, 150, 200,
		240, 300, 360, 480, 500, 600, 750, 960, 1000, 3840, 3200}

	signal := make([]complex128, 4000)
	for i := range signal {
		signal[i] = complex(
			math.Sin(float64(i)*0.7+0.3),
			math.Cos(float64(i)*1.3-0.7),
		)
	}

	for _, n := range sizes {
		t.Run(intToName(n), func(t *testing.T) {
			x := make([]complex128, n)
			copy(x, signal[:n])

			// Time-domain energy
			var timeE float64
			for _, v := range x {
				timeE += real(v)*real(v) + imag(v)*imag(v)
			}

			// DFT
			xf := make([]complex128, n)
			copy(xf, x)
			mixedRadixDFT(xf)

			// Frequency-domain energy: Σ|X[k]|² / N
			var freqE float64
			for _, v := range xf {
				freqE += real(v)*real(v) + imag(v)*imag(v)
			}
			freqE /= float64(n)

			relErr := math.Abs(timeE-freqE) / (timeE + 1e-30)
			if relErr > 1e-10 {
				t.Errorf("Parseval: time=%g, freq=%g, relErr=%g", timeE, freqE, relErr)
			}
		})
	}
}

// --- generalDFT dispatcher tests ---

// TestGeneralDFT_RoutesToCorrectAlgorithm verifies that generalDFT produces
// correct results for power-of-2, 5-smooth, and other sizes.
func TestGeneralDFT_RoutesToCorrectAlgorithm(t *testing.T) {
	sizes := []struct {
		name string
		n    int
	}{
		{"pow2_16", 16},
		{"pow2_64", 64},
		{"smooth_15", 15},
		{"smooth_30", 30},
		{"smooth_3840", 3840},
		{"prime_7", 7},
		{"prime_11", 11},
		{"composite_14", 14}, // 2×7 — not 5-smooth, needs Bluestein
	}

	signal := make([]complex128, 4000)
	for i := range signal {
		signal[i] = complex(
			math.Sin(float64(i)*0.7+0.3),
			math.Cos(float64(i)*1.3-0.7),
		)
	}

	for _, tc := range sizes {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.n
			x := make([]complex128, n)
			copy(x, signal[:n])

			// generalDFT
			xg := make([]complex128, n)
			copy(xg, x)
			generalDFT(xg)

			// Brute-force DFT
			for k := range n {
				var sum complex128
				for i := range n {
					angle := -2 * math.Pi * float64(k) * float64(i) / float64(n)
					sum += x[i] * complex(math.Cos(angle), math.Sin(angle))
				}
				diff := cmplx.Abs(xg[k] - sum)
				tol := 1e-6 * float64(n) * (cmplx.Abs(sum) + 1)
				if diff > tol {
					t.Errorf("bin[%d]: generalDFT=%v, DFT=%v, diff=%g",
						k, xg[k], sum, diff)
				}
			}
		})
	}
}

// --- RealFFTN integration tests for 5-smooth sizes ---

// TestRealFFTN_MixedRadix_AgainstDFT tests RealFFTN with 5-smooth sizes
// against brute-force DFT, verifying the full public API path.
func TestRealFFTN_MixedRadix_AgainstDFT(t *testing.T) {
	sizes := []int{3, 5, 6, 9, 10, 12, 15, 18, 20, 24, 25, 30,
		36, 40, 45, 48, 50, 60, 72, 75, 80, 90, 96, 100, 120, 150, 200,
		240, 250, 300, 360, 480, 500, 600, 750, 960, 1000, 3840}

	// Pseudo-random real input
	maxN := 4000
	samples := make([]float32, maxN)
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i)*0.7+0.3) + 0.5*math.Cos(float64(i)*2.1))
	}

	for _, n := range sizes {
		t.Run(intToName(n), func(t *testing.T) {
			input := samples
			if n < maxN {
				input = samples[:n]
			}
			bins := RealFFTN(input, n)

			if len(bins) != n/2+1 {
				t.Fatalf("got %d bins, want %d", len(bins), n/2+1)
			}

			// Brute-force DFT
			for k := range n/2 + 1 {
				var sum complex128
				for i := range n {
					var val float64
					if i < len(input) {
						val = float64(input[i])
					}
					angle := -2 * math.Pi * float64(k) * float64(i) / float64(n)
					sum += complex(val, 0) * complex(math.Cos(angle), math.Sin(angle))
				}
				want := complex64(sum)

				if !approxEqC64(bins[k], want, 0.05) {
					t.Errorf("bin[%d]: FFT=%v, DFT=%v", k, bins[k], want)
				}
			}
		})
	}
}

// --- Digit reversal tests ---

func TestDigitReversalPermute_IsPermutation(t *testing.T) {
	// Verify that digit reversal is a valid permutation (every index appears
	// exactly once in the output).
	sizes := []int{6, 10, 12, 15, 18, 20, 24, 25, 30, 36, 45, 48, 50, 60}

	for _, n := range sizes {
		if !is5Smooth(n) {
			continue
		}
		t.Run(intToName(n), func(t *testing.T) {
			factors := factorize235(n)

			// Create identity-like data: x[i] = complex(i, 0)
			x := make([]complex128, n)
			for i := range x {
				x[i] = complex(float64(i), 0)
			}

			digitReversalPermute(x, factors)

			// Verify all values 0..n-1 appear exactly once.
			seen := make([]bool, n)
			for i, v := range x {
				idx := int(real(v))
				if idx < 0 || idx >= n {
					t.Fatalf("index %d: value %d out of range", i, idx)
				}
				if seen[idx] {
					t.Fatalf("index %d: value %d appears more than once", i, idx)
				}
				seen[idx] = true
			}
		})
	}
}

// --- Helper ---

func intToName(n int) string {
	return "N=" + itoa(n)
}

// itoa is a simple int-to-string without importing strconv.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := false
	if n < 0 {
		neg = true
		n = -n
	}
	buf := [20]byte{}
	i := len(buf) - 1
	for n > 0 {
		buf[i] = byte('0' + n%10)
		n /= 10
		i--
	}
	if neg {
		buf[i] = '-'
		i--
	}
	return string(buf[i+1:])
}
