// synth.go — FT8 GFSK audio waveform synthesis.
//
// This file implements the final stage of the TX audio pipeline: converting
// a smoothed frequency trajectory (from [SmoothedFrequency]) into audio
// samples via continuous phase integration.
//
// The synthesis pipeline mirrors the reference implementations:
//
//   - WSJT-X gen_ft8wave.f90: phi accumulation + sin(phi) output
//   - ft8_lib gen_ft8.c: identical phi accumulation with sinf(phi)
//
// Phase integration is performed in float64 to maintain precision over the
// full 151 680-sample waveform (max accumulated phase ≈ 238k radians, well
// within float64's ~15 significant digits). Phase is wrapped modulo 2π
// every symbol period as cheap insurance against drift.
//
// Envelope shaping: the first and last [dsp.SamplesPerSymbol]/8 samples are
// multiplied by a raised-cosine ramp, matching WSJT-X (gen_ft8wave.f90
// lines 67–80) and ft8_lib (gen_ft8.c lines 94–101). This eliminates key
// clicks at TX on/off transitions.

package synth

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// Default synthesis parameters.
const (
	// DefaultAmplitude is the peak sample amplitude used by [Synthesize].
	// Kept below 1.0 to avoid clipping in DACs and audio codecs.
	DefaultAmplitude = 0.95

	// OutputSamples is the number of audio samples in one FT8 TX waveform:
	// NumSymbols × SamplesPerSymbol = 79 × 1920 = 151 680.
	OutputSamples = dsp.NumSymbols * dsp.SamplesPerSymbol

	// rampSamples is the length of the raised-cosine envelope ramp applied
	// at the start and end of the waveform. Matches WSJT-X/ft8_lib:
	// nramp = nsps / 8 = 1920 / 8 = 240.
	rampSamples = dsp.SamplesPerSymbol / 8
)

// Synthesize generates FT8 audio samples from the 79-symbol channel sequence
// using GFSK modulation with default parameters (BT=2.0, amplitude=0.95).
//
// This is a convenience wrapper around [SynthesizeWithAmplitude].
//
// Parameters:
//   - symbols: the full channel sequence from [dsp.InsertSync] (sync + data)
//   - baseFreqHz: audio offset frequency for tone 0 (typically 1000–2000 Hz)
//
// Returns [OutputSamples] (151 680) float32 samples at 12 kHz.
func Synthesize(symbols [dsp.NumSymbols]uint8, baseFreqHz float64) []float32 {
	return SynthesizeWithAmplitude(symbols, baseFreqHz, DefaultAmplitude)
}

// SynthesizeWithAmplitude generates FT8 audio samples with configurable
// peak amplitude.
//
// The synthesis pipeline:
//  1. Compute the Gaussian smoothing kernel ([GaussianFilter]).
//  2. Convolve the step-wise frequency trajectory ([SmoothedFrequency]).
//  3. Phase-integrate the smoothed frequency to produce sin(φ) samples.
//  4. Apply raised-cosine envelope shaping to the first and last
//     [dsp.SamplesPerSymbol]/8 samples (matching WSJT-X and ft8_lib).
//
// Phase starts at 0, so the first sample (before envelope shaping) is
// approximately sin(0) = 0. Phase is wrapped modulo 2π every symbol period.
//
// Parameters:
//   - symbols: the full channel sequence from [dsp.InsertSync]
//   - baseFreqHz: audio offset frequency for tone 0 (Hz)
//   - amplitude: peak sample amplitude (e.g., 0.95). Clamped to [0, 1].
//
// Returns [OutputSamples] (151 680) float32 samples at 12 kHz.
// Returns nil if amplitude ≤ 0.
func SynthesizeWithAmplitude(symbols [dsp.NumSymbols]uint8,
	baseFreqHz, amplitude float64) []float32 {

	if amplitude <= 0 {
		return nil
	}
	if amplitude > 1.0 {
		amplitude = 1.0
	}

	// Step 1–2: compute the smoothed frequency trajectory.
	kernel := GaussianFilter(GaussianBT, KernelSpan, dsp.SamplesPerSymbol)
	freq := SmoothedFrequency(symbols, baseFreqHz, kernel, dsp.SamplesPerSymbol)
	if freq == nil {
		return nil
	}

	// Step 3: phase integration.
	//
	// φ[n+1] = φ[n] + 2π × freq[n] / SampleRate
	// sample[n] = amplitude × sin(φ[n])
	//
	// Uses float64 for phase accumulation; output is float32.
	out := make([]float32, OutputSamples)
	phi := 0.0
	twoPiOverSR := 2.0 * math.Pi / dsp.SampleRate

	for n := range OutputSamples {
		out[n] = float32(amplitude * math.Sin(phi))
		phi += freq[n] * twoPiOverSR

		// Wrap phase modulo 2π every symbol period for insurance.
		// This prevents unbounded growth, though float64 precision is
		// more than sufficient for 151k samples.
		if (n+1)%dsp.SamplesPerSymbol == 0 {
			phi = math.Mod(phi, 2*math.Pi)
		}
	}

	// Step 4: envelope shaping — raised-cosine ramp on the first and last
	// rampSamples samples, matching WSJT-X gen_ft8wave.f90 lines 67–80
	// and ft8_lib gen_ft8.c lines 94–101.
	//
	// Start ramp: env = (1 − cos(2π·i / (2·rampSamples))) / 2,  i = 0..rampSamples-1
	//   → ramps from 0 to 1 (raised-cosine).
	// End ramp: same envelope applied in reverse to the last rampSamples samples.
	twoPiOverRamp := 2.0 * math.Pi / float64(2*rampSamples)
	for i := range rampSamples {
		env := float32((1.0 - math.Cos(float64(i)*twoPiOverRamp)) / 2.0)
		out[i] *= env
		out[OutputSamples-1-i] *= env
	}

	return out
}
