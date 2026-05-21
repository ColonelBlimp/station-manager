package dsp

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// Baseband downsampling via FFT-based band extraction. For a given
// candidate centre frequency f0, the audio is transformed to the
// frequency domain, a passband around f0 is extracted, edge-tapered,
// shifted to centre on DC of a smaller FFT, and inverse-transformed
// to produce complex baseband at 200 Hz.
//
// **Why FFT-based?** Two equivalent textbook approaches exist:
//
//   - Time-domain: mix audio with exp(-j·2π·f0·t), apply a low-pass
//     filter, decimate. Requires designing/implementing the
//     low-pass filter (FIR coefficients, taps, etc.).
//   - Frequency-domain (this implementation): FFT the audio once,
//     extract the bins around f0, IFFT a smaller buffer. The
//     "filter" is the bin extraction window + edge taper. Reuses
//     the FFT primitive we already shipped.
//
// Both yield the same result modulo numerical precision. The
// frequency-domain approach is simpler given the FFT in hand —
// no FIR design needed.
//
// **Output sample rate.** Input is Fs = 12000 Hz at NMAX = 180000
// samples (15 s). Forward FFT zero-pads to NFFT1DS = 192000 (next
// 5-smooth size). Extract NFFT2 = 3200 bins around f0; IFFT
// produces 3200 complex samples at Fs / (NFFT1DS/NFFT2) = 12000 /
// 60 = 200 Hz. The 3200-sample output covers 3200 / 200 = 16 s
// (the original 15 s + 1 s zero-pad headroom).
//
// **Anti-aliasing.** The extracted 3200-bin window covers
// 3200 × (Fs/NFFT1DS) = 3200 × 0.0625 = 200 Hz of bandwidth around
// f0 (±100 Hz). At the output sample rate of 200 Hz, that's the
// full Nyquist range; the bin-extraction itself is the anti-alias
// filter. An additional Hann taper near the band edges smooths
// the cutoff so the inverse FFT doesn't produce ringing artifacts.

// downsamplerTaperBins is the half-width of the edge taper applied
// to the extracted band. 100 bins (out of 3200) = ~3% of the
// passband on each side. Hann-shaped taper smooths the sharp
// rectangular cutoff that would otherwise cause IFFT ringing in
// the time-domain output.
const downsamplerTaperBins = 100

// Downsample mixes the audio at the given centre frequency to
// baseband and decimates to a 200 Hz complex sample rate. Returns
// 3200 complex samples representing the audio's content within
// ±100 Hz of f0.
//
// audio: 15-second slot at Fs = 12000 Hz. Slices shorter than
// NMAX are zero-padded at the end; slices longer are truncated.
//
// f0: candidate centre frequency in Hz. Typically 200..2900 (the
// FT8 working passband).
//
// Returns nil if f0 is outside the audio band [0, Fs/2].
//
// **Edge-of-band behaviour:** if the requested passband extends
// past the spectrum's edge (e.g. f0 very close to DC or Nyquist),
// the missing bins are filled with zeros and the function returns
// a baseband slice rather than refusing. This is friendlier than
// nil — callers that probe near the edges still get a usable
// result with the out-of-band content as silence.
func Downsample(audio []float32, f0 float64) []complex128 {
	if f0 <= 0 || f0 >= Fs/2 {
		return nil
	}
	spectrum := ForwardSpectrum(audio)
	return DownsampleFromSpectrum(spectrum, f0)
}

// ForwardSpectrum computes the 192000-point forward FFT used by
// Downsample's frequency-domain extraction. Callers that downsample
// many candidate frequencies from the same audio slot (the FT8
// receive path) should call this once and reuse the result across
// all candidates via DownsampleFromSpectrum — the per-candidate
// forward FFT is identical otherwise.
//
// Returns the complex spectrum of the audio zero-padded to NFFT1DS.
// audio shorter than NMAX is zero-padded at the end; longer is
// truncated to NMAX (matching Downsample's contract).
//
// Returned slice is safe to read concurrently — Downsample's
// per-candidate work only reads from it.
//
// Builds an ad-hoc audio.Plan internally; callers that decode many
// slots in succession should use ForwardSpectrumWithPlan with a
// shared Plan (size NFFT1DS) to amortise the ~9 MB twiddle/workspace
// allocation across slots.
func ForwardSpectrum(audio []float32) []complex128 {
	return ForwardSpectrumWithPlan(audio, nil)
}

// ForwardSpectrumWithPlan is the Plan-reuse variant of
// ForwardSpectrum. If plan is non-nil it must have been constructed
// for size NFFT1DS (Plan.FFT panics on a size mismatch). If plan is
// nil, an ad-hoc plan is built per call (same cost as
// ForwardSpectrum itself).
func ForwardSpectrumWithPlan(samples []float32, plan *audio.Plan) []complex128 {
	timeIn := make([]complex128, NFFT1DS)
	n := len(samples)
	if n > NMAX {
		n = NMAX
	}
	for i := 0; i < n; i++ {
		timeIn[i] = complex(float64(samples[i]), 0)
	}
	// timeIn[n..NFFT1DS] stays zero.
	if plan != nil {
		return plan.FFT(timeIn)
	}
	return audioFFT(timeIn)
}

// DownsampleFromSpectrum extracts a baseband signal around f0 from
// a precomputed forward FFT (from ForwardSpectrum). This is the
// per-candidate hot path for the FT8 receive loop — callers
// typically run it once per Sync candidate, reusing the same
// spectrum slice across all candidates from one 15-s slot.
//
// Returns 3200 complex samples at 200 Hz, or nil if f0 is outside
// the audio band [0, Fs/2]. See Downsample's doc for the
// edge-of-band zero-fill behaviour.
//
// Profiling note (Session 80): factoring this out of Downsample
// cuts the receive-pipeline's CPU time by ~78% and allocations by
// ~100× on a typical 100-candidate slot, because the 192000-point
// forward FFT no longer runs once per candidate.
//
// Builds an ad-hoc audio.Plan internally for the inverse FFT;
// callers running this many times per slot should use
// DownsampleFromSpectrumWithPlan with a shared Plan (size NFFT2).
func DownsampleFromSpectrum(spectrum []complex128, f0 float64) []complex128 {
	return DownsampleFromSpectrumWithPlan(spectrum, f0, nil)
}

// DownsampleFromSpectrumWithPlan is the Plan-reuse variant of
// DownsampleFromSpectrum. If inversePlan is non-nil it must have
// been constructed for size NFFT2 (Plan.IFFT panics on a size
// mismatch). If nil, an ad-hoc plan is built per call.
func DownsampleFromSpectrumWithPlan(spectrum []complex128, f0 float64, inversePlan *audio.Plan) []complex128 {
	if f0 <= 0 || f0 >= Fs/2 {
		return nil
	}
	if len(spectrum) != NFFT1DS {
		return nil
	}

	// Locate the bin centre for f0.
	const df = Fs / float64(NFFT1DS) // 0.0625 Hz per bin
	centreBin := int(math.Round(f0 / df))

	// Build the output frequency window centred on the new DC.
	// We extract NFFT2 bins from spectrum around centreBin and place
	// them into a NFFT2-sized complex buffer with positive frequencies
	// at low indices and negative at high indices (standard FFT
	// layout for the inverse transform).
	out := make([]complex128, NFFT2)
	half := NFFT2 / 2

	// Positive frequencies: out[0..half) ← spectrum[centreBin..centreBin+half)
	for k := 0; k < half; k++ {
		src := centreBin + k
		if src < 0 || src >= NFFT1DS {
			continue
		}
		out[k] = spectrum[src]
	}
	// Negative frequencies: out[half..NFFT2) ← spectrum[centreBin-half..centreBin)
	for k := 0; k < half; k++ {
		src := centreBin - half + k
		if src < 0 || src >= NFFT1DS {
			continue
		}
		out[half+k] = spectrum[src]
	}

	// Apply Hann-shaped taper at the edges so the inverse FFT
	// doesn't ring from a sharp rectangular cutoff.
	applyHannTaper(out, downsamplerTaperBins)

	// Inverse FFT → time-domain complex baseband at 200 Hz.
	if inversePlan != nil {
		return inversePlan.IFFT(out)
	}
	return audioIFFT(out)
}

// audioFFT and audioIFFT are tiny wrappers so the import alias
// stays local. Pure passthroughs to internal/audio.
func audioFFT(x []complex128) []complex128  { return audio.FFT(x) }
func audioIFFT(x []complex128) []complex128 { return audio.IFFT(x) }

// applyHannTaper multiplies the first and last `width` elements of
// out by a Hann window edge profile. The taper rises from 0 at the
// extreme edge to 1 at width elements in; everything between
// width and NFFT2-width is left at full amplitude.
//
// The "edges" here are the high-frequency boundaries of the
// extracted passband — out[width..NFFT2-width] is the central
// band that passes through untouched.
//
// The Hann profile is `0.5 · (1 - cos(π·i/width))` for i in
// [0, width). At i=0 the multiplier is 0; at i=width-1 it's
// approximately 1.
func applyHannTaper(out []complex128, width int) {
	if width <= 0 || 2*width >= len(out) {
		return
	}
	// Bottom edge (lowest positive frequencies, near DC).
	// Wait: out[0] is DC after extraction, the strongest signal.
	// Tapering DC would be wrong. The "edges" of the passband
	// after our layout are out[half-width..half] (top of positive)
	// and out[half..half+width] (bottom of negative). Those are
	// the band edges — the frequencies furthest from DC.
	half := len(out) / 2
	for i := 0; i < width; i++ {
		// w rises from 0 at i=0 to ~1 at i=width-1.
		w := 0.5 * (1.0 - math.Cos(math.Pi*float64(i)/float64(width)))
		// Top positive edge: highest positive freq is at index
		// half-1; taper inwards from there.
		idxTop := half - 1 - i
		out[idxTop] = complex(real(out[idxTop])*w, imag(out[idxTop])*w)
		// Bottom negative edge: lowest negative freq is at index
		// half; taper inwards from there.
		idxBot := half + i
		out[idxBot] = complex(real(out[idxBot])*w, imag(out[idxBot])*w)
	}
}
