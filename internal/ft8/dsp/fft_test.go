// fft_test.go — tests for the pure-Go radix-2 FFT.

package dsp

import (
	"math"
	"math/cmplx"
	"testing"
)

// --- NextPow2 tests ---

func TestNextPow2(t *testing.T) {
	tests := []struct {
		n, want int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{1920, 2048},
		{2048, 2048},
		{2049, 4096},
		{maxPow2, maxPow2},     // exact boundary — should not panic
		{maxPow2 - 1, maxPow2}, // just below — should not panic
	}
	for _, tc := range tests {
		got := NextPow2(tc.n)
		if got != tc.want {
			t.Errorf("NextPow2(%d) = %d, want %d", tc.n, got, tc.want)
		}
	}
}

// TestNextPow2Overflow verifies that NextPow2 panics for inputs whose
// result would overflow int, rather than looping infinitely.
func TestNextPow2Overflow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("NextPow2(maxPow2+1) did not panic")
		}
	}()
	NextPow2(maxPow2 + 1)
}

// --- RealFFT output length tests ---

// TestRealFFTOutputLength verifies that RealFFT returns N/2+1 bins where
// N is the zero-padded FFT size.
func TestRealFFTOutputLength(t *testing.T) {
	tests := []struct {
		inputLen int
		wantBins int
	}{
		{1, 1},       // N=1 → 1 bin
		{2, 2},       // N=2 → 2 bins
		{4, 3},       // N=4 → 3 bins
		{8, 5},       // N=8 → 5 bins
		{64, 33},     // N=64 → 33 bins
		{1920, 1025}, // N=2048 → 1025 bins
		{2048, 1025}, // N=2048 → 1025 bins
	}
	for _, tc := range tests {
		samples := make([]float32, tc.inputLen)
		bins := RealFFT(samples)
		if len(bins) != tc.wantBins {
			t.Errorf("inputLen=%d: got %d bins, want %d", tc.inputLen, len(bins), tc.wantBins)
		}
	}
}

// TestRealFFTNil verifies that nil/empty input returns nil.
func TestRealFFTNil(t *testing.T) {
	if bins := RealFFT(nil); bins != nil {
		t.Errorf("RealFFT(nil) = %v, want nil", bins)
	}
	if bins := RealFFT([]float32{}); bins != nil {
		t.Errorf("RealFFT([]) = %v, want nil", bins)
	}
}

// --- DC (all-ones) test ---

// TestRealFFTDC verifies that a constant signal produces energy only in
// the DC bin.
//
// For x[n] = 1 (N=8): X[0] = N = 8, X[k] = 0 for k > 0.
func TestRealFFTDC(t *testing.T) {
	const n = 8
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = 1.0
	}

	bins := RealFFT(samples)
	if len(bins) != n/2+1 {
		t.Fatalf("got %d bins, want %d", len(bins), n/2+1)
	}

	// Bin 0 (DC) should be N + 0i.
	if !approxEqC64(bins[0], complex(float32(n), 0), 1e-4) {
		t.Errorf("DC bin = %v, want %v", bins[0], complex(float32(n), 0))
	}

	// All other bins should be ~0.
	for k := 1; k < len(bins); k++ {
		if cmplx.Abs(complex128(bins[k])) > 1e-4 {
			t.Errorf("bin[%d] = %v, want ~0", k, bins[k])
		}
	}
}

// --- Impulse (delta) test ---

// TestRealFFTImpulse verifies that a unit impulse at t=0 produces a flat
// spectrum (all bins = 1 + 0i).
func TestRealFFTImpulse(t *testing.T) {
	const n = 8
	samples := make([]float32, n)
	samples[0] = 1.0

	bins := RealFFT(samples)

	for k, b := range bins {
		if !approxEqC64(b, 1+0i, 1e-4) {
			t.Errorf("bin[%d] = %v, want 1+0i", k, b)
		}
	}
}

// --- Single sample test ---

// TestRealFFTSingle verifies that a single-sample input returns just the
// DC bin equal to the sample value.
func TestRealFFTSingle(t *testing.T) {
	bins := RealFFT([]float32{42.0})
	if len(bins) != 1 {
		t.Fatalf("got %d bins, want 1", len(bins))
	}
	if !approxEqC64(bins[0], 42+0i, 1e-4) {
		t.Errorf("DC bin = %v, want 42+0i", bins[0])
	}
}

// --- Known sinusoid tests ---

// TestRealFFTCosine verifies that a cosine at an exact bin frequency
// produces a peak at the correct bin with the expected magnitude.
//
// For x[n] = cos(2π·k·n/N) with N samples:
//
//	X[k] = N/2,  X[N−k] = N/2,  all other bins = 0.
//
// Since RealFFT returns only bins 0..N/2, we see X[k] = N/2.
func TestRealFFTCosine(t *testing.T) {
	tests := []struct {
		name string
		n    int // input length (must be power of 2)
		k    int // bin number of the cosine frequency
	}{
		{"N=64_bin3", 64, 3},
		{"N=64_bin10", 64, 10},
		{"N=256_bin7", 256, 7},
		{"N=1024_bin50", 1024, 50},
		{"N=2048_bin100", 2048, 100},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			samples := make([]float32, tc.n)
			for i := range samples {
				samples[i] = float32(math.Cos(
					2 * math.Pi * float64(tc.k) * float64(i) / float64(tc.n)))
			}

			bins := RealFFT(samples)

			// Expected magnitude at the signal bin.
			wantMag := float64(tc.n) / 2.0

			// Check the peak bin.
			gotMag := cmplx.Abs(complex128(bins[tc.k]))
			if !approxEq64(gotMag, wantMag, 0.1) {
				t.Errorf("bin[%d] magnitude = %g, want %g", tc.k, gotMag, wantMag)
			}

			// Check that no other bin exceeds 1% of the peak magnitude.
			threshold := wantMag * 0.01
			for i, b := range bins {
				if i == tc.k {
					continue
				}
				m := cmplx.Abs(complex128(b))
				if m > threshold {
					t.Errorf("bin[%d] magnitude = %g, exceeds threshold %g (leakage)",
						i, m, threshold)
				}
			}
		})
	}
}

// TestRealFFTSine verifies that a sine wave produces a purely imaginary
// peak (no real component) at the signal's bin.
//
// For x[n] = sin(2π·k·n/N): X[k] = −jN/2.
func TestRealFFTSine(t *testing.T) {
	const n = 64
	const k = 5

	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(math.Sin(
			2 * math.Pi * float64(k) * float64(i) / float64(n)))
	}

	bins := RealFFT(samples)

	// X[k] should be approximately 0 − j*(N/2) = complex(0, -32).
	wantReal := float32(0)
	wantImag := float32(-n / 2)
	got := bins[k]

	if !approxEq(real(got), wantReal, 0.1) {
		t.Errorf("bin[%d] real = %g, want ~%g", k, real(got), wantReal)
	}
	if !approxEq(imag(got), wantImag, 0.1) {
		t.Errorf("bin[%d] imag = %g, want ~%g", k, imag(got), wantImag)
	}
}

// TestRealFFTCosinePhase verifies that a cosine produces a purely real
// (zero imaginary) peak at the signal's bin.
func TestRealFFTCosinePhase(t *testing.T) {
	const n = 64
	const k = 7

	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(math.Cos(
			2 * math.Pi * float64(k) * float64(i) / float64(n)))
	}

	bins := RealFFT(samples)
	got := bins[k]

	// Real part should be N/2 = 32.
	if !approxEq(real(got), float32(n/2), 0.1) {
		t.Errorf("bin[%d] real = %g, want ~%g", k, real(got), float32(n/2))
	}
	// Imaginary part should be ~0.
	if !approxEq(imag(got), 0, 0.1) {
		t.Errorf("bin[%d] imag = %g, want ~0", k, imag(got))
	}
}

// --- Parseval's theorem ---

// TestRealFFTParseval verifies energy conservation: the time-domain energy
// equals the frequency-domain energy.
//
// For real-valued input and the one-sided spectrum (bins 0..N/2):
//
//	N × Σ|x[n]|² = |X[0]|² + |X[N/2]|² + 2 × Σ_{k=1}^{N/2−1} |X[k]|²
func TestRealFFTParseval(t *testing.T) {
	tests := []struct {
		name    string
		samples []float32
	}{
		{"impulse_8", func() []float32 {
			s := make([]float32, 8)
			s[0] = 1.0
			return s
		}()},
		{"dc_16", func() []float32 {
			s := make([]float32, 16)
			for i := range s {
				s[i] = 3.0
			}
			return s
		}()},
		{"cosine_64", func() []float32 {
			s := make([]float32, 64)
			for i := range s {
				s[i] = float32(math.Cos(2 * math.Pi * 5 * float64(i) / 64))
			}
			return s
		}()},
		{"mixed_128", func() []float32 {
			s := make([]float32, 128)
			for i := range s {
				s[i] = float32(0.5*math.Cos(2*math.Pi*3*float64(i)/128) +
					0.3*math.Sin(2*math.Pi*17*float64(i)/128) +
					0.1)
			}
			return s
		}()},
		// Non-power-of-2 lengths exercise the zero-padding path.
		{"padded_5", func() []float32 {
			return []float32{1, -0.5, 0.3, -0.1, 0.7} // 5 → padded to 8
		}()},
		{"padded_1920", func() []float32 {
			s := make([]float32, SamplesPerSymbol) // 1920 → padded to 2048
			for i := range s {
				s[i] = float32(math.Sin(2 * math.Pi * 800 * float64(i) / float64(SampleRate)))
			}
			return s
		}()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			n := NextPow2(len(tc.samples))

			// Time-domain energy (over original samples; padded zeros
			// contribute nothing).
			var timeEnergy float64
			for _, s := range tc.samples {
				timeEnergy += float64(s) * float64(s)
			}

			bins := RealFFT(tc.samples)

			// Frequency-domain energy (one-sided → full-spectrum).
			nBins := len(bins)
			var freqEnergy float64
			freqEnergy += cmplxMagSq(bins[0])       // DC
			freqEnergy += cmplxMagSq(bins[nBins-1]) // Nyquist
			for k := 1; k < nBins-1; k++ {
				freqEnergy += 2 * cmplxMagSq(bins[k]) // positive + negative
			}
			freqEnergy /= float64(n) // normalise by FFT length

			if !approxEq64(timeEnergy, freqEnergy, timeEnergy*1e-6+1e-10) {
				t.Errorf("Parseval mismatch: time=%g, freq=%g", timeEnergy, freqEnergy)
			}
		})
	}
}

// --- Zero-padding tests ---

// TestRealFFTZeroPadding verifies that zero-padding a signal does not
// change the magnitude at exact bin frequencies — it only interpolates
// additional bins between them.
func TestRealFFTZeroPadding(t *testing.T) {
	// Create a 64-sample cosine at bin 4 (frequency = 4/64 of sample rate).
	const origN = 64
	const k = 4
	samples64 := make([]float32, origN)
	for i := range samples64 {
		samples64[i] = float32(math.Cos(
			2 * math.Pi * float64(k) * float64(i) / float64(origN)))
	}

	bins64 := RealFFT(samples64)

	// Now zero-pad to 128 samples.
	samples128 := make([]float32, 128)
	copy(samples128, samples64)
	bins128 := RealFFT(samples128)

	// Bin 4 in the 64-point FFT corresponds to bin 8 in the 128-point FFT
	// (same physical frequency: 4/64 = 8/128).
	mag64 := cmplx.Abs(complex128(bins64[k]))
	mag128 := cmplx.Abs(complex128(bins128[2*k]))

	// Both magnitudes should be ~32 (N/2 = 64/2). Use a relative
	// tolerance of 0.1% of the expected value.
	relTol := mag64 * 0.001
	if !approxEq64(mag64, mag128, relTol) {
		t.Errorf("magnitude mismatch after zero-padding: 64-pt bin[%d]=%g, 128-pt bin[%d]=%g",
			k, mag64, 2*k, mag128)
	}
}

// --- Non-power-of-two input ---

// TestRealFFTNonPowerOfTwo verifies correct behaviour when the input length
// is not a power of 2 (should be zero-padded).
func TestRealFFTNonPowerOfTwo(t *testing.T) {
	// 6-sample input → zero-padded to 8.
	samples := []float32{1, 2, 3, 4, 5, 6}
	bins := RealFFT(samples)

	// Output should have 8/2+1 = 5 bins.
	if len(bins) != 5 {
		t.Fatalf("got %d bins, want 5", len(bins))
	}

	// DC bin = sum of all samples = 21.
	if !approxEq(real(bins[0]), 21.0, 0.1) {
		t.Errorf("DC = %g, want 21", real(bins[0]))
	}
}

// --- FT8 frame size ---

// TestRealFFTFT8FrameSize verifies that a 1920-sample input (one FT8
// symbol period) produces the expected 1025 bins (zero-padded to 2048).
func TestRealFFTFT8FrameSize(t *testing.T) {
	samples := make([]float32, SamplesPerSymbol)
	// Fill with a cosine at ~1000 Hz. At sample rate 12000 and FFT size
	// 2048, bin frequency resolution is 12000/2048 ≈ 5.859 Hz.
	// 1000 Hz ≈ bin 170.67 — not an exact bin, so we expect some leakage.
	for i := range samples {
		samples[i] = float32(math.Cos(2 * math.Pi * 1000 * float64(i) / float64(SampleRate)))
	}

	bins := RealFFT(samples)

	// Expected output length: 2048/2 + 1 = 1025.
	if len(bins) != 1025 {
		t.Fatalf("got %d bins, want 1025", len(bins))
	}

	// The peak should be near bin 170–171 (1000 Hz / 5.859 Hz/bin).
	peakBin := 0
	peakMag := float64(0)
	for k, b := range bins {
		m := cmplx.Abs(complex128(b))
		if m > peakMag {
			peakMag = m
			peakBin = k
		}
	}

	expectedBin := int(math.Round(1000.0 / (float64(SampleRate) / 2048.0)))
	if absDiff(peakBin, expectedBin) > 2 {
		t.Errorf("peak at bin %d, expected near bin %d (1000 Hz)", peakBin, expectedBin)
	}
}

// --- DFT reference test ---

// TestRealFFTAgainstDFT verifies the FFT output against a brute-force DFT
// for a small input. This is the strongest correctness check — any FFT
// implementation bug will show up here.
func TestRealFFTAgainstDFT(t *testing.T) {
	samples := []float32{1, -0.5, 0.3, -0.1, 0.7, -0.4, 0.2, 0.6}
	n := len(samples) // 8, already power of 2

	bins := RealFFT(samples)

	// Brute-force DFT for bins 0..N/2.
	for k := range n/2 + 1 {
		var sum complex128
		for i, s := range samples {
			angle := -2 * math.Pi * float64(k) * float64(i) / float64(n)
			sum += complex(float64(s), 0) * complex(math.Cos(angle), math.Sin(angle))
		}
		want := complex64(sum)
		got := bins[k]

		if !approxEqC64(got, want, 1e-3) {
			t.Errorf("bin[%d]: FFT=%v, DFT=%v", k, got, want)
		}
	}
}

// --- Linearity ---

// TestRealFFTLinearity verifies that FFT(a·x + b·y) = a·FFT(x) + b·FFT(y).
func TestRealFFTLinearity(t *testing.T) {
	const n = 32
	const a, b = float32(2.5), float32(-1.3)

	x := make([]float32, n)
	y := make([]float32, n)
	sum := make([]float32, n)
	for i := range n {
		x[i] = float32(math.Cos(2 * math.Pi * 3 * float64(i) / float64(n)))
		y[i] = float32(math.Sin(2 * math.Pi * 7 * float64(i) / float64(n)))
		sum[i] = a*x[i] + b*y[i]
	}

	binsX := RealFFT(x)
	binsY := RealFFT(y)
	binsSum := RealFFT(sum)

	for k := range binsSum {
		want := complex64(complex(a, 0))*binsX[k] + complex64(complex(b, 0))*binsY[k]
		got := binsSum[k]
		if !approxEqC64(got, want, 0.1) {
			t.Errorf("bin[%d]: linearity violated: got %v, want %v", k, got, want)
		}
	}
}

// --- Symmetry ---

// TestRealFFTHermitianSymmetry verifies that for real input, the FFT
// output satisfies X[N−k] = conj(X[k]). Since RealFFT only returns bins
// 0..N/2, we verify this by running a full complex DFT and checking that
// the positive-frequency bins from RealFFT match.
func TestRealFFTHermitianSymmetry(t *testing.T) {
	samples := []float32{1, -0.5, 0.3, -0.1, 0.7, -0.4, 0.2, 0.6}
	n := len(samples)

	bins := RealFFT(samples)

	// Full DFT for comparison.
	full := make([]complex128, n)
	for k := range n {
		var sum complex128
		for i, s := range samples {
			angle := -2 * math.Pi * float64(k) * float64(i) / float64(n)
			sum += complex(float64(s), 0) * complex(math.Cos(angle), math.Sin(angle))
		}
		full[k] = sum
	}

	// Verify Hermitian symmetry: X[N-k] = conj(X[k]) for k=1..N/2-1.
	for k := 1; k < n/2; k++ {
		xk := full[k]
		xnk := full[n-k]
		conjXk := cmplx.Conj(xk)
		if cmplx.Abs(xnk-conjXk) > 1e-8 {
			t.Errorf("Hermitian symmetry: X[%d]=%v, X[%d]=%v, conj(X[%d])=%v",
				n-k, xnk, k, xk, k, conjXk)
		}
	}

	// Verify RealFFT bins match the full DFT positive-frequency bins.
	for k := range n/2 + 1 {
		want := complex64(full[k])
		if !approxEqC64(bins[k], want, 1e-3) {
			t.Errorf("bin[%d]: RealFFT=%v, fullDFT=%v", k, bins[k], want)
		}
	}
}

// --- RealFFTN tests ---

// TestRealFFTN_PowerOf2_MatchesRealFFT verifies that RealFFTN with a
// power-of-2 size produces identical output to RealFFT.
func TestRealFFTN_PowerOf2_MatchesRealFFT(t *testing.T) {
	samples := []float32{1, -0.5, 0.3, -0.1, 0.7, -0.4, 0.2, 0.6}
	binsFFT := RealFFT(samples)
	binsFFTN := RealFFTN(samples, 8)

	if len(binsFFT) != len(binsFFTN) {
		t.Fatalf("length mismatch: RealFFT=%d, RealFFTN=%d", len(binsFFT), len(binsFFTN))
	}
	for k := range binsFFT {
		if !approxEqC64(binsFFT[k], binsFFTN[k], 1e-3) {
			t.Errorf("bin[%d]: RealFFT=%v, RealFFTN=%v", k, binsFFT[k], binsFFTN[k])
		}
	}
}

// TestRealFFTN_Bluestein_AgainstDFT verifies Bluestein's algorithm for
// non-power-of-2 sizes against a brute-force DFT.
func TestRealFFTN_Bluestein_AgainstDFT(t *testing.T) {
	tests := []struct {
		name string
		n    int
	}{
		{"N=3", 3},
		{"N=5", 5},
		{"N=6", 6},
		{"N=7", 7},
		{"N=10", 10},
		{"N=15", 15},
		{"N=100", 100},
		{"N=3840", 3840}, // WSJT-X NFFT1
	}

	// Use a pseudo-random input.
	samples := make([]float32, 4000)
	for i := range samples {
		samples[i] = float32(math.Sin(float64(i)*0.7+0.3) + 0.5*math.Cos(float64(i)*2.1))
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			input := samples[:tc.n]
			bins := RealFFTN(input, tc.n)

			if len(bins) != tc.n/2+1 {
				t.Fatalf("got %d bins, want %d", len(bins), tc.n/2+1)
			}

			// Brute-force DFT for comparison.
			for k := range tc.n/2 + 1 {
				var sum complex128
				for i := range tc.n {
					var val float64
					if i < len(input) {
						val = float64(input[i])
					}
					angle := -2 * math.Pi * float64(k) * float64(i) / float64(tc.n)
					sum += complex(val, 0) * complex(math.Cos(angle), math.Sin(angle))
				}
				want := complex64(sum)

				if !approxEqC64(bins[k], want, 0.05) {
					t.Errorf("bin[%d]: Bluestein=%v, DFT=%v", k, bins[k], want)
				}
			}
		})
	}
}

// TestRealFFTN_3840_BinAlignment verifies that a 3840-point FFT gives
// exactly 2 bins per FT8 tone, eliminating the fractional alignment issue.
func TestRealFFTN_3840_BinAlignment(t *testing.T) {
	const nfft = 2 * SamplesPerSymbol // 3840

	bins := RealFFTN(make([]float32, SamplesPerSymbol), nfft)

	// Expected: 3840/2 + 1 = 1921 bins.
	if len(bins) != nfft/2+1 {
		t.Fatalf("got %d bins, want %d", len(bins), nfft/2+1)
	}

	binWidth := float64(SampleRate) / float64(nfft)
	binsPerTone := ToneSpacing / binWidth

	t.Logf("FFT size:     %d", nfft)
	t.Logf("Freq bins:    %d", len(bins))
	t.Logf("Bin width:    %.6f Hz", binWidth)
	t.Logf("Bins/tone:    %.6f", binsPerTone)

	// The key assertion: bins per tone must be EXACTLY 2.
	if math.Abs(binsPerTone-2.0) > 1e-10 {
		t.Errorf("bins per tone = %g, want exactly 2.0", binsPerTone)
	}
}

// TestRealFFTN_3840_Cosine verifies that a cosine at an FT8 tone frequency
// produces a clean peak at the exact expected bin with no spectral leakage.
func TestRealFFTN_3840_Cosine(t *testing.T) {
	const nfft = 2 * SamplesPerSymbol // 3840
	const testFreq = 1000.0           // Hz — choose a frequency on the bin grid
	binWidth := float64(SampleRate) / float64(nfft)
	expectedBin := int(math.Round(testFreq / binWidth))
	actualFreq := float64(expectedBin) * binWidth

	// Generate a cosine at the exact bin frequency.
	samples := make([]float32, SamplesPerSymbol) // 1920 samples
	for i := range samples {
		samples[i] = float32(math.Cos(2 * math.Pi * actualFreq * float64(i) / float64(SampleRate)))
	}

	bins := RealFFTN(samples, nfft)

	// Find the peak.
	peakBin := 0
	peakMag := float64(0)
	for k, b := range bins {
		m := math.Sqrt(float64(real(b))*float64(real(b)) + float64(imag(b))*float64(imag(b)))
		if m > peakMag {
			peakMag = m
			peakBin = k
		}
	}

	if peakBin != expectedBin {
		t.Errorf("peak at bin %d, expected %d (freq %.1f Hz)", peakBin, expectedBin, actualFreq)
	}

	t.Logf("Cosine at %.1f Hz → peak at bin %d (expected %d), magnitude %.1f",
		actualFreq, peakBin, expectedBin, peakMag)
}

// TestRealFFTN_Nil verifies edge cases.
func TestRealFFTN_Nil(t *testing.T) {
	if bins := RealFFTN(nil, 0); bins != nil {
		t.Errorf("RealFFTN(nil, 0) = %v, want nil", bins)
	}
	if bins := RealFFTN(nil, -1); bins != nil {
		t.Errorf("RealFFTN(nil, -1) = %v, want nil", bins)
	}
}

// --- Test helpers ---

// approxEqC64 returns true if both real and imaginary parts of a and b
// are within tolerance.
func approxEqC64(a, b complex64, tolerance float32) bool {
	return approxEq(real(a), real(b), tolerance) &&
		approxEq(imag(a), imag(b), tolerance)
}

// cmplxMagSq returns |c|² = re² + im² for a complex64 value.
func cmplxMagSq(c complex64) float64 {
	r := float64(real(c))
	i := float64(imag(c))
	return r*r + i*i
}

// absDiff returns |a − b|.
func absDiff(a, b int) int {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
