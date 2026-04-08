// gfsk.go — Gaussian filter and smoothed frequency trajectory for FT8 GFSK.
//
// The Gaussian impulse response kernel smooths the step-wise tone-to-frequency
// mapping, eliminating the abrupt transitions that would otherwise produce
// excessive spectral splatter. The kernel is parameterised by the bandwidth-
// time product (BT) and truncation span, following the WSJT-X and ft8_lib
// reference implementations.
//
// The Gaussian pulse shape used here:
//
//	h(t) = √(2π/ln2) · BT · exp(−2·(π·BT·t)² / ln2)
//
// is the derivative of the GFSK frequency pulse. When convolved with the
// step-wise frequency signal, it produces the same smoothed trajectory as
// the erf-difference overlap-add used by WSJT-X:
//
//	p(t) = 0.5 · (erf(c·BT·(t+0.5)) − erf(c·BT·(t−0.5)))
//
// where c = π·√(2/ln2) ≈ 5.336446 (ft8_lib's GFSK_CONST_K).
//
// Boundary handling: the raw frequency signal is extended by half the kernel
// length on each side, repeating the first/last symbol's frequency. This
// matches WSJT-X's dummy-symbol padding (gen_ft8wave.f90 lines 42–44) and
// ft8_lib's equivalent (gen_ft8.c lines 79–84), ensuring the edges of the
// waveform are smoothly ramped rather than truncated.

package synth

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// FT8 GFSK defaults.
const (
	// GaussianBT is the bandwidth-time product for FT8 GFSK modulation.
	// A higher BT produces a narrower Gaussian kernel (faster transitions,
	// wider occupied bandwidth). FT8 uses BT=2.0; FT4 uses BT=1.0.
	// TODO: FT4 uses BT=1.0 (ft8_lib FT4_SYMBOL_BT).
	GaussianBT = 2.0

	// KernelSpan is the truncation width of the Gaussian kernel in symbol
	// periods. The kernel is sampled over [−KernelSpan/2, +KernelSpan/2)
	// symbol periods. For BT=2.0, the Gaussian is essentially zero beyond
	// ±1.5 symbols, so a 5-symbol span is conservative.
	//
	// The reference implementations (WSJT-X, ft8_lib) use a 3-symbol pulse,
	// but that is the erf-difference pulse (one symbol wider than the
	// underlying Gaussian due to the integration). A 5-symbol Gaussian
	// kernel captures all significant energy.
	// TODO: FT4 may need a different span.
	KernelSpan = 5
)

// GaussianFilter returns a normalised Gaussian impulse response kernel for
// GFSK smoothing.
//
// Parameters:
//   - bt: bandwidth-time product (2.0 for FT8, 1.0 for FT4)
//   - span: truncation width in symbol periods (typically [KernelSpan] = 5)
//   - symbolSamples: audio samples per symbol period (typically [dsp.SamplesPerSymbol] = 1920)
//
// Returns a kernel of length span × symbolSamples, normalised so that its
// sum equals 1.0. This ensures that a constant-frequency input passes
// through unchanged.
//
// Returns nil if any parameter is ≤ 0.
func GaussianFilter(bt float64, span, symbolSamples int) []float64 {
	if bt <= 0 || span <= 0 || symbolSamples <= 0 {
		return nil
	}

	n := span * symbolSamples
	kernel := make([]float64, n)

	// The kernel is centred at index (n-1)/2.0. Time t is measured in
	// symbol periods relative to this centre.
	centre := float64(n-1) / 2.0
	scale := math.Sqrt(2*math.Pi/math.Ln2) * bt

	var sum float64
	for i := range n {
		// t in symbol periods: t=0 at centre, t=±span/2 at edges.
		t := (float64(i) - centre) / float64(symbolSamples)
		arg := math.Pi * bt * t
		kernel[i] = scale * math.Exp(-2*arg*arg/math.Ln2)
		sum += kernel[i]
	}

	// Normalise to unit sum.
	if sum > 0 {
		invSum := 1.0 / sum
		for i := range kernel {
			kernel[i] *= invSum
		}
	}

	return kernel
}

// SmoothedFrequency convolves the step-wise symbol-to-frequency mapping with
// the Gaussian kernel to produce a smooth per-sample frequency trajectory.
//
// Parameters:
//   - symbols: the 79-symbol FT8 channel sequence (from [dsp.InsertSync])
//   - baseFreqHz: audio offset frequency for tone 0 (Hz)
//   - kernel: Gaussian smoothing kernel (from [GaussianFilter])
//   - symbolSamples: audio samples per symbol period (typically [dsp.SamplesPerSymbol])
//
// Returns a frequency trajectory of length [dsp.NumSymbols] × symbolSamples
// (151 680 for FT8). Each element is the instantaneous frequency in Hz at
// that sample position.
//
// Boundary handling: the raw frequency signal is extended by len(kernel)/2
// samples on each side, with the first symbol's frequency repeated at the
// start and the last symbol's frequency repeated at the end. This matches
// the dummy-symbol padding used by WSJT-X and ft8_lib.
//
// Performance: the raw frequency signal is piecewise constant (one value per
// symbol block), so the convolution is decomposed into per-segment
// contributions using prefix sums of the kernel. This reduces complexity
// from O(outLen × kernelLen) to O(outLen × numSegments), giving ~100×
// speedup for FT8 parameters.
//
// Returns nil if the kernel is empty or symbolSamples ≤ 0.
func SmoothedFrequency(symbols [dsp.NumSymbols]uint8, baseFreqHz float64,
	kernel []float64, symbolSamples int) []float64 {

	kLen := len(kernel)
	if kLen == 0 || symbolSamples <= 0 {
		return nil
	}

	outLen := dsp.NumSymbols * symbolSamples
	halfK := kLen / 2
	extLen := outLen + 2*halfK

	// Build the segment table. The extended raw signal is piecewise
	// constant with these segments:
	//   [0, halfK)                             → first symbol's frequency
	//   [halfK + s*symS, halfK + (s+1)*symS)   → symbol s (for s = 0..78)
	//   [halfK + outLen, extLen)                → last symbol's frequency
	firstFreq := baseFreqHz + float64(symbols[0])*dsp.ToneSpacing
	lastFreq := baseFreqHz + float64(symbols[dsp.NumSymbols-1])*dsp.ToneSpacing

	type segment struct {
		start int
		end   int
		freq  float64
	}

	segs := make([]segment, 0, dsp.NumSymbols+2)
	segs = append(segs, segment{0, halfK, firstFreq})
	for s := range dsp.NumSymbols {
		start := halfK + s*symbolSamples
		segs = append(segs, segment{start, start + symbolSamples,
			baseFreqHz + float64(symbols[s])*dsp.ToneSpacing})
	}
	segs = append(segs, segment{halfK + outLen, extLen, lastFreq})

	// Precompute prefix sums of the kernel for O(1) range-sum queries:
	//   prefK[n] = Σ_{j=0}^{n-1} kernel[j]
	// so Σ_{j=a}^{b-1} kernel[j] = prefK[b] − prefK[a].
	prefK := make([]float64, kLen+1)
	for i := range kLen {
		prefK[i+1] = prefK[i] + kernel[i]
	}

	// Compute convolution via segment decomposition.
	//
	// For each output sample i, the kernel window covers raw indices
	// [i, i+kLen). For each segment overlapping this window, the
	// contribution is freq(seg) × Σ kernel[j] over the overlap range,
	// computed in O(1) via the prefix sums.
	//
	// Because the kernel is symmetric (Gaussian), convolution and
	// cross-correlation produce identical results.
	out := make([]float64, outLen)
	for i := range outLen {
		winEnd := i + kLen // exclusive
		var acc float64
		for _, seg := range segs {
			// Skip segments entirely outside the kernel window.
			if seg.end <= i || seg.start >= winEnd {
				continue
			}
			// Intersection of segment with kernel window, in raw indices.
			lo := seg.start
			if lo < i {
				lo = i
			}
			hi := seg.end
			if hi > winEnd {
				hi = winEnd
			}
			// Convert to kernel indices and look up prefix sums.
			acc += seg.freq * (prefK[hi-i] - prefK[lo-i])
		}
		out[i] = acc
	}

	return out
}
