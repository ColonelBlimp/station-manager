package sandbox

import (
	"fmt"
	"math"

	"github.com/ColonelBlimp/station-manager/research/sandbox/pfft"
)

// Channelizer turns a 15-second 12 kHz mono FT8 slot into per-candidate
// complex baseband signals via frequency-domain filtering.
//
// Two-stage shape per the 2026-05-27 sandbox design:
//
//   - Prepare runs once per slot: zero-pad the 180,000 samples to
//     192,000 and take a single 192k-point real FFT. The packed real-
//     FFT output stays cached on the Channelizer.
//   - Extract runs per candidate: slice ~bandwidthHz worth of bins
//     around the candidate's centre frequency, Tukey-taper the slice
//     edges, fftshift the slice into IFFT-input layout (DC at index 0,
//     positives at low indices, negatives at high indices), and IFFT
//     to a complex baseband signal at bandwidthHz complex sample rate.
//
// Typical use: bandwidthHz = 200 Hz, giving 3,200 complex samples at
// 200 Hz — exactly 32 samples per FT8 symbol (0.16 s × 200 Hz).
//
// The output is NOT amplitude-normalised. For input cos(2π f t) of
// unit amplitude with f exactly on a bin and equal to centerHz, the
// baseband magnitude is ~channelizerSlotLen / 2 = 90,000 (the
// half-spectrum capture from the real-input forward FFT, integrated
// over the 15-s of non-padded audio). Callers that need absolute
// magnitude can divide by channelizerFFTN.
type Channelizer struct {
	forwardPlan *pfft.RealPlan

	// spec is the FFT buffer in FFTPack packed real-FFT layout after
	// Prepare. Owned by the Channelizer; reused across slots by
	// overwriting in Prepare.
	spec []float64

	// plans caches a ComplexPlan per slice length so repeated
	// Extract calls at the same bandwidth reuse the precomputed
	// twiddle tables. Keyed by OUTPUT length (sliceN for symmetric
	// mode, outputBins for asymmetric mode), which is the actual
	// IFFT length — admittance width does not change the cache key.
	plans map[int]*pfft.ComplexPlan

	// mode selects the extraction geometry for Extract calls. The
	// zero value is sliceModeSymmetric (current behaviour). Toggle
	// via SetAsymmetricFT8Slice before the first decode pass; the
	// mode then applies to every Extract on this Channelizer.
	mode sliceMode
}

// sliceMode selects the channelizer's spectral admittance behaviour.
// Held on the Channelizer so RefineCandidate / ExtractSymbols and other
// downstream stages can ask for a "candidate baseband at N Hz" without
// caring whether the admitted spectrum was symmetric or FT8-shaped —
// the mode lives at the extraction boundary where the choice belongs.
type sliceMode int

const (
	// sliceModeSymmetric is the default mode: Extract admits a slice
	// of width bandwidthHz centred on centerHz, and emits at the same
	// bandwidthHz complex sample rate. Conflates admittance with
	// output rate but is the historical contract every downstream
	// stage was tuned against.
	sliceModeSymmetric sliceMode = iota

	// sliceModeAsymmetricFT8 dispatches Extract through
	// ExtractAsymmetric with the FT8 tone-band geometry baked in:
	// admit [-1.5·baud, +8.5·baud) = 62.5 Hz around centerHz, emit at
	// the caller's requested bandwidthHz rate. Decouples admittance
	// (fixed at the FT8 tone band) from output rate (still the
	// caller's choice — preserves ExtractSymbols' 32-samples-per-
	// symbol contract when bandwidthHz=200).
	sliceModeAsymmetricFT8
)

// FT8 asymmetric admittance geometry. Constants rather than parameters
// because the FT8 tone band is fixed: 8 tones at 6.25 Hz spacing, with
// 1.5·baud guard on each side. Sweeping the guard widths is a research
// task that uses ExtractAsymmetric directly with caller-supplied
// values, not the channelizer-mode dispatch.
const (
	ft8BaudHz            = 6.25
	asymmetricFT8LowerHz = 1.5 * ft8BaudHz // 9.375 Hz lower guard
	asymmetricFT8UpperHz = 8.5 * ft8BaudHz // 53.125 Hz: 1.5·baud above tone 7
)

const (
	// channelizerSampleRate is the input sample rate the Channelizer
	// expects: 12 kHz mono float32, matching SM's FT8 slot convention.
	channelizerSampleRate = 12000.0

	// channelizerSlotLen is the documented FT8 slot length in samples
	// (15 seconds at 12 kHz).
	channelizerSlotLen = 180000

	// channelizerFFTN is the FFT length used per slot. 192,000 =
	// 2^9 × 3 × 5^3, the smallest 5-smooth integer >= channelizerSlotLen.
	// The 12,000-sample zero-pad keeps the FFT size friendly to all
	// PocketFFT codelets without inflating it beyond necessary.
	channelizerFFTN = 192000

	// channelizerBinHz is the frequency spacing between adjacent FFT
	// bins: 12000 / 192000 = 0.0625 Hz.
	channelizerBinHz = channelizerSampleRate / channelizerFFTN

	// defaultTukeyAlpha sets the cosine-edge fraction of the slice
	// taper. 0.25 puts the taper region in the outer 25% of each
	// slice edge, leaving 75% of the bandwidth flat-pass. For
	// bandwidth = 200 Hz the flat region covers 150 Hz, which fully
	// contains the 50 Hz FT8 8-FSK band (8 tones × 6.25 Hz).
	defaultTukeyAlpha = 0.25
)

// NewChannelizer constructs a Channelizer with its forward plan
// pre-built. Plans hold C-allocated memory; call Close to release.
func NewChannelizer() (*Channelizer, error) {
	plan, err := pfft.NewRealPlan(channelizerFFTN)
	if err != nil {
		return nil, fmt.Errorf("channelizer: forward plan: %w", err)
	}
	return &Channelizer{
		forwardPlan: plan,
		spec:        make([]float64, channelizerFFTN),
		plans:       map[int]*pfft.ComplexPlan{},
	}, nil
}

// Close releases the forward plan and any per-bandwidth IFFT plans.
// Safe to call multiple times. After Close, Prepare and Extract will
// panic.
func (c *Channelizer) Close() {
	if c.forwardPlan != nil {
		c.forwardPlan.Close()
		c.forwardPlan = nil
	}
	for _, p := range c.plans {
		p.Close()
	}
	c.plans = nil
}

// Prepare loads a slot's audio and runs the forward FFT. Input must
// be at least channelizerSlotLen samples (180,000) of 12 kHz mono
// float32; any excess is ignored. The first channelizerSlotLen
// samples are copied (with widening to float64) into the FFT buffer;
// the tail is zero-padded.
//
// Prepare is the only per-slot cost. After Prepare returns, an
// arbitrary number of Extract calls operate on the same cached
// spectrum without re-doing the forward FFT.
func (c *Channelizer) Prepare(audio []float32) error {
	if len(audio) < channelizerSlotLen {
		return fmt.Errorf("channelizer: audio length %d < required %d",
			len(audio), channelizerSlotLen)
	}
	for i := 0; i < channelizerSlotLen; i++ {
		c.spec[i] = float64(audio[i])
	}
	for i := channelizerSlotLen; i < channelizerFFTN; i++ {
		c.spec[i] = 0
	}
	return c.forwardPlan.Forward(c.spec)
}

// SetAsymmetricFT8Slice toggles the FT8-tuned asymmetric extraction
// mode for subsequent Extract calls on this Channelizer. When on,
// Extract dispatches through ExtractAsymmetric with admittance fixed
// at [centerHz-1.5·baud, centerHz+8.5·baud) = 62.5 Hz; the output
// remains at the caller's requested bandwidthHz rate (so callers like
// ExtractSymbols still see len(baseband) = bandwidthHz/binHz at the
// same complex sample rate). When off (the zero-value default), Extract
// uses the original symmetric path.
//
// Call once after NewChannelizer (or after a Reset); the mode then
// applies to every Extract for the lifetime of the channelizer or
// until toggled back.
//
// Intended for A/B experiments at the decode pipeline level — a
// single binary can run the full pipeline twice (mode off vs on) on
// the same prepared spectrum and compare LLR quality / decode parity
// without any other code paths diverging.
func (c *Channelizer) SetAsymmetricFT8Slice(on bool) {
	if on {
		c.mode = sliceModeAsymmetricFT8
	} else {
		c.mode = sliceModeSymmetric
	}
}

// Extract returns the complex baseband signal at bandwidthHz complex
// sample rate centred on centerHz. Output length is round(bandwidthHz /
// channelizerBinHz), rounded up to even. centerHz is rounded to the
// nearest bin; the residual sub-bin offset (up to ±channelizerBinHz/2 =
// ±0.03125 Hz) shows up as a slow phase rotation in the baseband.
//
// Spectral admittance depends on the channelizer's current mode (see
// SetAsymmetricFT8Slice):
//
//   - sliceModeSymmetric (default): admits a slice of width bandwidthHz
//     centred on centerHz — admittance == output rate.
//   - sliceModeAsymmetricFT8: admits the FT8 tone band [-1.5·baud,
//     +8.5·baud) = 62.5 Hz; output rate stays at bandwidthHz, so the
//     guard region is zero-filled before IFFT.
//
// Errors when the requested geometry doesn't fit inside the positive-
// frequency half of the spectrum, or when bandwidthHz is too small.
func (c *Channelizer) Extract(centerHz, bandwidthHz float64) ([]complex128, error) {
	if c.mode == sliceModeAsymmetricFT8 {
		return c.ExtractAsymmetric(centerHz, asymmetricFT8LowerHz, asymmetricFT8UpperHz, bandwidthHz)
	}
	return c.extractSymmetric(centerHz, bandwidthHz)
}

// extractSymmetric is the original Extract body: admit a slice of width
// bandwidthHz centred on centerHz, taper, fftshift, IFFT. Preserved
// verbatim from before the slice-mode introduction.
func (c *Channelizer) extractSymmetric(centerHz, bandwidthHz float64) ([]complex128, error) {
	sliceN := int(math.Round(bandwidthHz / channelizerBinHz))
	if sliceN < 2 {
		return nil, fmt.Errorf("channelizer: bandwidth %.3g Hz < 2 bins (%.3g Hz)",
			bandwidthHz, 2*channelizerBinHz)
	}
	if sliceN%2 != 0 {
		sliceN++
	}
	halfSlice := sliceN / 2

	centerBin := int(math.Round(centerHz / channelizerBinHz))
	startBin := centerBin - halfSlice
	endBin := startBin + sliceN
	if startBin < 0 {
		return nil, fmt.Errorf("channelizer: slice startBin %d < 0 (centerHz=%.3g, bandwidthHz=%.3g)",
			startBin, centerHz, bandwidthHz)
	}
	if endBin > channelizerFFTN/2 {
		return nil, fmt.Errorf("channelizer: slice endBin %d > Nyquist %d (centerHz=%.3g, bandwidthHz=%.3g)",
			endBin, channelizerFFTN/2, centerHz, bandwidthHz)
	}

	plan, ok := c.plans[sliceN]
	if !ok {
		var err error
		plan, err = pfft.NewComplexPlan(sliceN)
		if err != nil {
			return nil, fmt.Errorf("channelizer: complex plan(%d): %w", sliceN, err)
		}
		c.plans[sliceN] = plan
	}

	// Extract sliceN consecutive bins centred on centerBin into
	// rawSlice, applying the Tukey taper in natural (un-shifted) order
	// where the slice middle (rawSlice[halfSlice]) corresponds to the
	// candidate's DC.
	rawSlice := make([]complex128, sliceN)
	for i := 0; i < sliceN; i++ {
		tap := tukeyWindow(i, sliceN, defaultTukeyAlpha)
		rawSlice[i] = c.forwardPlan.Bin(c.spec, startBin+i) * complex(tap, 0)
	}

	// fftshift: rotate the slice so DC lands at index 0 of the IFFT
	// input. Positive baseband frequencies occupy the lower half;
	// negative baseband frequencies wrap into the upper half. This is
	// the standard layout PocketFFT (and FFTW, NumPy) expect for the
	// inverse complex transform.
	sliceWork := make([]complex128, sliceN)
	copy(sliceWork[0:halfSlice], rawSlice[halfSlice:])
	copy(sliceWork[halfSlice:], rawSlice[0:halfSlice])

	if err := plan.Backward(sliceWork); err != nil {
		return nil, fmt.Errorf("channelizer: IFFT(%d): %w", sliceN, err)
	}
	return sliceWork, nil
}

// ExtractAsymmetric returns the complex baseband signal admitting only
// spectral content in [centerHz - lowerHz, centerHz + upperHz), zero-
// filling the guard region inside an outputBandwidthHz-wide output
// buffer before IFFT.
//
// This decouples admitted spectral width from output sample rate, which
// the symmetric Extract conflates. The intended FT8 use is to admit
// the 8-FSK tone band tightly (1.5·baud guard on each side of the 8
// tones = 62.5 Hz admitted total) while emitting at the existing 200 Hz
// baseband sample rate (3200 bins / 32 samples per FT8 symbol), so
// ExtractSymbols' contract is preserved and admitted-tone amplitudes
// stay comparable to the symmetric variant (Parseval: time-domain
// energy equals admitted-bin energy regardless of the zero-filled
// guard).
//
// Geometry per call:
//
//   - centerHz: FFT-bin-quantised; sub-bin offset shows up as slow
//     baseband phase rotation (same as Extract).
//   - lowerHz, upperHz: admitted bin offsets are computed as
//     round(lowerHz / channelizerBinHz) and round(upperHz / channelizerBinHz);
//     the half-open admitted set is [-lowerBins, +upperBins).
//   - outputBandwidthHz: output buffer length is round(outputBandwidthHz /
//     channelizerBinHz), rounded up to even.
//
// IFFT layout (PocketFFT/FFTW/NumPy convention):
//
//   - admitted bin 0 (DC) → output index 0
//   - admitted bin +k (1 ≤ k < upperBins) → output index k
//   - admitted bin -k (1 ≤ k ≤ lowerBins) → output index outputBins-k
//   - all other output indices: zero (guard region)
//
// A Tukey taper of width = lowerBins + upperBins (default alpha = 0.25)
// is applied across the admitted region only, centred so taper index 0
// sits at the most-negative admitted offset and taper index
// admittedBins-1 sits at the most-positive admitted offset. With the
// FT8 geometry above this gives a 750-bin flat pass region that fully
// covers the 8 tones plus ~25 bins of margin each side.
//
// Errors when admitted bins fall outside the cached spectrum, when the
// admitted set is wider than the output buffer, when admittedBins < 2,
// or when outputBandwidthHz is too small.
func (c *Channelizer) ExtractAsymmetric(
	centerHz, lowerHz, upperHz, outputBandwidthHz float64,
) ([]complex128, error) {
	if lowerHz < 0 || upperHz < 0 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: lowerHz=%.3g upperHz=%.3g must be >= 0",
			lowerHz, upperHz)
	}
	if outputBandwidthHz <= 0 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: outputBandwidthHz=%.3g must be > 0",
			outputBandwidthHz)
	}

	outputBins := int(math.Round(outputBandwidthHz / channelizerBinHz))
	if outputBins < 2 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: outputBandwidthHz %.3g Hz < 2 bins (%.3g Hz)",
			outputBandwidthHz, 2*channelizerBinHz)
	}
	if outputBins%2 != 0 {
		outputBins++
	}

	lowerBins := int(math.Round(lowerHz / channelizerBinHz))
	upperBins := int(math.Round(upperHz / channelizerBinHz))
	admittedBins := lowerBins + upperBins
	if admittedBins < 2 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: admitted band %d bins < 2 (lowerHz=%.3g, upperHz=%.3g)",
			admittedBins, lowerHz, upperHz)
	}
	if admittedBins > outputBins {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: admitted bins %d > output bins %d (admittedHz=%.3g > outputBandwidthHz=%.3g)",
			admittedBins, outputBins, lowerHz+upperHz, outputBandwidthHz)
	}

	centerBin := int(math.Round(centerHz / channelizerBinHz))
	if centerBin-lowerBins < 0 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: low edge bin %d < 0 (centerHz=%.3g, lowerHz=%.3g)",
			centerBin-lowerBins, centerHz, lowerHz)
	}
	if centerBin+upperBins > channelizerFFTN/2 {
		return nil, fmt.Errorf("channelizer: ExtractAsymmetric: high edge bin %d > Nyquist %d (centerHz=%.3g, upperHz=%.3g)",
			centerBin+upperBins, channelizerFFTN/2, centerHz, upperHz)
	}

	plan, ok := c.plans[outputBins]
	if !ok {
		var err error
		plan, err = pfft.NewComplexPlan(outputBins)
		if err != nil {
			return nil, fmt.Errorf("channelizer: complex plan(%d): %w", outputBins, err)
		}
		c.plans[outputBins] = plan
	}

	// work is zero-filled; only admitted bins are written, leaving the
	// guard region between upperBins and outputBins-lowerBins as zeros.
	work := make([]complex128, outputBins)
	// Positive offsets [0, upperBins): src bin centerBin+off → output index off.
	// Taper index for off >= 0 is lowerBins + off.
	for off := 0; off < upperBins; off++ {
		tap := tukeyWindow(lowerBins+off, admittedBins, defaultTukeyAlpha)
		work[off] = c.forwardPlan.Bin(c.spec, centerBin+off) * complex(tap, 0)
	}
	// Negative offsets [-lowerBins, 0): src bin centerBin-k for k in [1, lowerBins].
	// Wrapped output index is outputBins-k. Taper index for off=-k is lowerBins-k.
	for k := 1; k <= lowerBins; k++ {
		tap := tukeyWindow(lowerBins-k, admittedBins, defaultTukeyAlpha)
		work[outputBins-k] = c.forwardPlan.Bin(c.spec, centerBin-k) * complex(tap, 0)
	}

	if err := plan.Backward(work); err != nil {
		return nil, fmt.Errorf("channelizer: IFFT(%d): %w", outputBins, err)
	}
	return work, nil
}

// tukeyWindow returns the i-th value of a Tukey (tapered cosine)
// window of length n with cosine-edge fraction alpha in [0, 1].
//   - alpha = 0: rectangular (constant 1)
//   - alpha = 1: Hann (full cosine)
//   - 0 < alpha < 1: flat middle of length (1-alpha)*(n-1), cosine
//     tapers of length alpha*(n-1)/2 at each end.
//
// Standard formulation (matches numpy.signal.windows.tukey).
func tukeyWindow(i, n int, alpha float64) float64 {
	if n < 2 {
		return 1
	}
	if alpha <= 0 {
		return 1
	}
	if alpha >= 1 {
		return 0.5 * (1 - math.Cos(2*math.Pi*float64(i)/float64(n-1)))
	}
	flatStart := alpha * float64(n-1) / 2
	flatEnd := float64(n-1) - flatStart
	x := float64(i)
	switch {
	case x < flatStart:
		return 0.5 * (1 + math.Cos(math.Pi*(x/flatStart-1)))
	case x > flatEnd:
		return 0.5 * (1 + math.Cos(math.Pi*(x-flatEnd)/flatStart))
	default:
		return 1
	}
}
