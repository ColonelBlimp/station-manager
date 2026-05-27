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
	// twiddle tables.
	plans map[int]*pfft.ComplexPlan
}

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

// Extract returns the complex baseband signal of bandwidthHz Hz
// centred on centerHz. Output length is round(bandwidthHz /
// channelizerBinHz), rounded up to even. Output complex sample rate
// equals bandwidthHz. centerHz is rounded to the nearest bin; the
// residual sub-bin offset (up to ±channelizerBinHz/2 = ±0.03125 Hz)
// shows up as a slow phase rotation in the baseband.
//
// Errors when the requested slice doesn't fit inside the positive-
// frequency half of the spectrum, or when bandwidthHz is smaller than
// the minimum two bins.
func (c *Channelizer) Extract(centerHz, bandwidthHz float64) ([]complex128, error) {
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
