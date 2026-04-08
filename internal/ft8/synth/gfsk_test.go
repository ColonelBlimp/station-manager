package synth

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// --- GaussianFilter tests ---

func TestGaussianFilterNil(t *testing.T) {
	// All invalid parameter combinations should return nil.
	cases := []struct {
		name          string
		bt            float64
		span, samples int
	}{
		{"bt=0", 0, 5, 1920},
		{"bt<0", -1, 5, 1920},
		{"span=0", 2.0, 0, 1920},
		{"span<0", 2.0, -1, 1920},
		{"samples=0", 2.0, 5, 0},
		{"samples<0", 2.0, 5, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if k := GaussianFilter(tc.bt, tc.span, tc.samples); k != nil {
				t.Errorf("expected nil, got %d-element slice", len(k))
			}
		})
	}
}

func TestGaussianFilterLength(t *testing.T) {
	// Kernel length must equal span × symbolSamples.
	cases := []struct {
		span, samples int
	}{
		{5, 1920},
		{3, 1920},
		{5, 960},
		{1, 100},
	}
	for _, tc := range cases {
		k := GaussianFilter(GaussianBT, tc.span, tc.samples)
		want := tc.span * tc.samples
		if len(k) != want {
			t.Errorf("span=%d samples=%d: len=%d, want %d", tc.span, tc.samples, len(k), want)
		}
	}
}

func TestGaussianFilterNormalisation(t *testing.T) {
	// The kernel must sum to 1.0 (unit DC gain).
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	if k == nil {
		t.Fatal("kernel is nil")
	}

	var sum float64
	for _, v := range k {
		sum += v
	}
	if diff := math.Abs(sum - 1.0); diff > 1e-12 {
		t.Errorf("sum = %.15f, want 1.0 (diff = %e)", sum, diff)
	}
}

func TestGaussianFilterSymmetry(t *testing.T) {
	// The Gaussian kernel is symmetric: kernel[i] == kernel[N-1-i].
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	if k == nil {
		t.Fatal("kernel is nil")
	}

	n := len(k)
	for i := 0; i < n/2; i++ {
		if diff := math.Abs(k[i] - k[n-1-i]); diff > 1e-15 {
			t.Errorf("k[%d]=%.15e != k[%d]=%.15e (diff=%e)", i, k[i], n-1-i, k[n-1-i], diff)
			break // one failure is enough
		}
	}
}

func TestGaussianFilterPeakAtCentre(t *testing.T) {
	// The maximum value must be at the centre index.
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	if k == nil {
		t.Fatal("kernel is nil")
	}

	n := len(k)
	centre := n / 2
	maxVal := k[0]
	maxIdx := 0
	for i, v := range k {
		if v > maxVal {
			maxVal = v
			maxIdx = i
		}
	}

	// For even-length kernels the centre may be at n/2-1 or n/2.
	// Both are acceptable since the kernel is symmetric and the true
	// peak is between them.
	if maxIdx != centre && maxIdx != centre-1 {
		t.Errorf("peak at index %d, expected near centre %d (n=%d)", maxIdx, centre, n)
	}
}

func TestGaussianFilterNonNegative(t *testing.T) {
	// A Gaussian kernel must have all non-negative values.
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	for i, v := range k {
		if v < 0 {
			t.Errorf("k[%d] = %e < 0", i, v)
			break
		}
	}
}

func TestGaussianFilterCentreValueFT8(t *testing.T) {
	// For BT=2.0, span=5, symbolSamples=1920, verify the un-normalised
	// centre value against the formula:
	//   h(0) = √(2π/ln2) · BT = √(9.0647...) · 2 ≈ 6.02153
	// After normalisation by the sum, the centre value should be
	// approximately h(0) / sum. Since ∫h(t)dt ≈ 1.0, the discrete sum
	// ≈ symbolSamples, so normalised centre ≈ h(0) / symbolSamples.
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	if k == nil {
		t.Fatal("kernel is nil")
	}

	centre := len(k) / 2
	// The centre value is at index centre or centre-1 (even-length kernel).
	// Use the larger of the two.
	v := k[centre]
	if centre > 0 && k[centre-1] > v {
		v = k[centre-1]
	}

	// Expected: √(2π/ln2) * 2.0 / symbolSamples ≈ 6.02153 / 1920 ≈ 0.003136
	// But the discrete sum slightly differs from symbolSamples, so allow
	// 1% tolerance.
	expected := math.Sqrt(2*math.Pi/math.Ln2) * GaussianBT / float64(dsp.SamplesPerSymbol)
	relErr := math.Abs(v-expected) / expected
	if relErr > 0.01 {
		t.Errorf("centre value = %.8e, expected ≈ %.8e (relErr = %.4f)", v, expected, relErr)
	}
}

func TestGaussianFilterDifferentBT(t *testing.T) {
	// A higher BT produces a narrower (more peaked) kernel.
	k1 := GaussianFilter(1.0, KernelSpan, dsp.SamplesPerSymbol) // FT4-like
	k2 := GaussianFilter(2.0, KernelSpan, dsp.SamplesPerSymbol) // FT8

	centre := len(k1) / 2
	if k2[centre] <= k1[centre] {
		t.Error("BT=2.0 centre should be larger (narrower peak) than BT=1.0")
	}
}

// --- SmoothedFrequency tests ---

func TestSmoothedFrequencyNil(t *testing.T) {
	var symbols [dsp.NumSymbols]uint8
	if f := SmoothedFrequency(symbols, 1000, nil, 1920); f != nil {
		t.Error("expected nil for nil kernel")
	}
	if f := SmoothedFrequency(symbols, 1000, []float64{}, 1920); f != nil {
		t.Error("expected nil for empty kernel")
	}
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	if f := SmoothedFrequency(symbols, 1000, k, 0); f != nil {
		t.Error("expected nil for symbolSamples=0")
	}
}

func TestSmoothedFrequencyLength(t *testing.T) {
	var symbols [dsp.NumSymbols]uint8
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	f := SmoothedFrequency(symbols, 1000, k, dsp.SamplesPerSymbol)
	wantLen := dsp.NumSymbols * dsp.SamplesPerSymbol // 151 680
	if len(f) != wantLen {
		t.Errorf("len = %d, want %d", len(f), wantLen)
	}
}

func TestSmoothedFrequencyConstantSymbol(t *testing.T) {
	// When all symbols are the same tone, the smoothed frequency should
	// be exactly baseFreq + tone × ToneSpacing everywhere, because the
	// normalised kernel sums to 1.0 and the input is constant.
	for _, tone := range []uint8{0, 3, 7} {
		t.Run("tone="+string(rune('0'+tone)), func(t *testing.T) {
			var symbols [dsp.NumSymbols]uint8
			for i := range symbols {
				symbols[i] = tone
			}

			baseFreq := 1000.0
			expected := baseFreq + float64(tone)*dsp.ToneSpacing

			k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
			f := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)
			if f == nil {
				t.Fatal("got nil")
			}

			for i, v := range f {
				if diff := math.Abs(v - expected); diff > 1e-9 {
					t.Errorf("f[%d] = %.10f, want %.10f (diff=%e)", i, v, expected, diff)
					break
				}
			}
		})
	}
}

func TestSmoothedFrequencyMidSymbol(t *testing.T) {
	// For a single tone surrounded by same-tone symbols, the mid-symbol
	// frequency should be exactly baseFreq + tone × ToneSpacing.
	// Use a sequence where tone 3 is surrounded by tone 3 neighbours
	// (ensuring no edge effects reach the measurement point).
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = 3
	}
	// Change the middle symbol to a different tone.
	midSym := dsp.NumSymbols / 2 // symbol 39
	symbols[midSym] = 5

	baseFreq := 1500.0
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	f := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)

	// At the centre of symbol 39, the frequency should be dominated by
	// tone 5 but partially pulled towards tone 3 by the Gaussian tails.
	// Verify it's between the two tone frequencies.
	midSample := midSym*dsp.SamplesPerSymbol + dsp.SamplesPerSymbol/2
	freqLow := baseFreq + 3*dsp.ToneSpacing
	freqHigh := baseFreq + 5*dsp.ToneSpacing
	v := f[midSample]
	if v < freqLow || v > freqHigh {
		t.Errorf("f[%d] = %.4f, expected between %.4f and %.4f", midSample, v, freqLow, freqHigh)
	}

	// For BT=2.0, the kernel is quite narrow, so the mid-symbol value
	// should be much closer to tone 5 than to tone 3.
	distTo5 := math.Abs(v - freqHigh)
	distTo3 := math.Abs(v - freqLow)
	if distTo5 > distTo3 {
		t.Errorf("mid-symbol frequency %.4f is closer to tone 3 than tone 5", v)
	}
}

func TestSmoothedFrequencyNoDisco(t *testing.T) {
	// The maximum adjacent-sample frequency delta should be bounded —
	// no discontinuities. For BT=2.0 and the worst case (tone 0 → tone 7,
	// a 43.75 Hz step), the Gaussian smoothing should limit the per-sample
	// delta to well below ToneSpacing.
	var symbols [dsp.NumSymbols]uint8
	// Alternating between tone 0 and tone 7 — worst-case transitions.
	for i := range symbols {
		if i%2 == 0 {
			symbols[i] = 0
		} else {
			symbols[i] = 7
		}
	}

	baseFreq := 1000.0
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	f := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)

	maxDelta := 0.0
	for i := 1; i < len(f); i++ {
		d := math.Abs(f[i] - f[i-1])
		if d > maxDelta {
			maxDelta = d
		}
	}

	// For BT=2.0, the max Gaussian derivative is ≈ h(0) ≈ 6.02, so
	// max Δf/sample ≈ 7*6.25 * 6.02 / 1920 ≈ 0.137 Hz/sample.
	// Use a generous bound of 1.0 Hz/sample.
	if maxDelta > 1.0 {
		t.Errorf("max adjacent-sample delta = %.4f Hz, expected < 1.0 Hz", maxDelta)
	}

	// Verify it's positive — there should be some transitions.
	if maxDelta < 1e-10 {
		t.Error("max delta is effectively zero — no transitions detected")
	}
}

func TestSmoothedFrequencyBounds(t *testing.T) {
	// The smoothed frequency should always be within the range of
	// [baseFreq + minTone*ToneSpacing, baseFreq + maxTone*ToneSpacing].
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = uint8(i % dsp.NumTones)
	}

	baseFreq := 1000.0
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	f := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)

	lo := baseFreq // tone 0
	hi := baseFreq + float64(dsp.NumTones-1)*dsp.ToneSpacing

	for i, v := range f {
		if v < lo-1e-6 || v > hi+1e-6 {
			t.Errorf("f[%d] = %.6f, out of range [%.2f, %.2f]", i, v, lo, hi)
			break
		}
	}
}

func TestSmoothedFrequencyMonotoneTransition(t *testing.T) {
	// A single step transition (all tone 0, then all tone 7) should
	// produce a monotonically increasing frequency in the transition
	// region, with no overshoot.
	var symbols [dsp.NumSymbols]uint8
	half := dsp.NumSymbols / 2
	for i := half; i < dsp.NumSymbols; i++ {
		symbols[i] = 7
	}

	baseFreq := 1000.0
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	f := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)

	// Check monotonicity around the transition point.
	startSample := (half - 3) * dsp.SamplesPerSymbol
	endSample := (half + 3) * dsp.SamplesPerSymbol
	if endSample > len(f) {
		endSample = len(f)
	}

	for i := startSample + 1; i < endSample; i++ {
		if f[i] < f[i-1]-1e-10 {
			t.Errorf("non-monotonic at sample %d: f[%d]=%.8f > f[%d]=%.8f",
				i, i-1, f[i-1], i, f[i])
			break
		}
	}
}

// --- Cross-validation against the erf-difference pulse (WSJT-X/ft8_lib) ---

func TestSmoothedFrequencyCrossValidation(t *testing.T) {
	// Verify that the Gaussian kernel convolution approach produces
	// the same result as the erf-difference overlap-add used by WSJT-X
	// and ft8_lib.
	//
	// The erf-difference approach builds a dphi (phase increment) array
	// via overlap-add of the GFSK pulse p(t) = 0.5*(erf(c*b*(t+0.5)) −
	// erf(c*b*(t-0.5))), then converts to instantaneous frequency.
	// Our approach directly convolves frequency with the Gaussian kernel.
	// The results should match to within floating-point tolerance.

	// Use a known FT8 symbol sequence with varied tones.
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = uint8(i % dsp.NumTones)
	}

	baseFreq := 1000.0

	// Our approach.
	k := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	ours := SmoothedFrequency(symbols, baseFreq, k, dsp.SamplesPerSymbol)

	// Reference: erf-difference overlap-add (ft8_lib algorithm).
	ref := refFrequency(symbols, baseFreq)

	if len(ours) != len(ref) {
		t.Fatalf("length mismatch: ours=%d, ref=%d", len(ours), len(ref))
	}

	// The two approaches should agree to within a small tolerance.
	// The Gaussian kernel is truncated at ±2.5 symbols while the erf
	// pulse spans ±1.5 symbols, so there will be minor differences at
	// symbol boundaries. Allow 0.01 Hz tolerance.
	maxDiff := 0.0
	for i := range ours {
		d := math.Abs(ours[i] - ref[i])
		if d > maxDiff {
			maxDiff = d
		}
	}

	if maxDiff > 0.01 {
		t.Errorf("max difference between Gaussian conv and erf-diff = %.6f Hz, want < 0.01", maxDiff)
	}
}
