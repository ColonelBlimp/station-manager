// refimpl_test.go — shared reference implementation helpers for cross-validation.
//
// These helpers implement the ft8_lib / WSJT-X erf-difference overlap-add
// algorithm as a test-only reference. They are used by both gfsk_test.go
// and synth_test.go for cross-validation against our Gaussian kernel
// convolution approach.

package synth

import (
	"math"

	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// refDphi computes the per-sample phase increment array using the ft8_lib /
// WSJT-X erf-difference overlap-add algorithm.
//
// Returns a dphi array of length (NumSymbols+2) × SamplesPerSymbol. The
// first SamplesPerSymbol entries correspond to the leading dummy symbol and
// should be skipped when extracting the output signal.
//
// This is a direct port of ft8_lib gen_ft8.c synth_gfsk (lines 49–84)
// and WSJT-X gen_ft8wave.f90 (lines 22–44).
func refDphi(symbols [dsp.NumSymbols]uint8, baseFreqHz float64) []float64 {
	nsps := dsp.SamplesPerSymbol
	nsym := dsp.NumSymbols

	// Compute the GFSK pulse (erf-difference), length 3*nsps.
	c := math.Pi * math.Sqrt(2.0/math.Ln2) // GFSK_CONST_K ≈ 5.336446
	bt := GaussianBT
	pulse := make([]float64, 3*nsps)
	for i := range pulse {
		tt := float64(i)/float64(nsps) - 1.5
		arg1 := c * bt * (tt + 0.5)
		arg2 := c * bt * (tt - 0.5)
		pulse[i] = 0.5 * (math.Erf(arg1) - math.Erf(arg2))
	}

	// Build dphi array, length (nsym+2)*nsps.
	dphiLen := (nsym + 2) * nsps
	dphi := make([]float64, dphiLen)

	// Initialise with base frequency phase increment.
	baseDphi := 2 * math.Pi * baseFreqHz / dsp.SampleRate
	for i := range dphi {
		dphi[i] = baseDphi
	}

	// Overlap-add: for each symbol, add dphi_peak * tone * pulse.
	dphiPeak := 2 * math.Pi * 1.0 / float64(nsps) // hmod=1.0
	for j := range nsym {
		ib := j * nsps
		for k := range 3 * nsps {
			dphi[ib+k] += dphiPeak * float64(symbols[j]) * pulse[k]
		}
	}

	// Dummy symbols at start and end.
	for k := range 2 * nsps {
		dphi[k] += dphiPeak * float64(symbols[0]) * pulse[k+nsps]
		dphi[nsym*nsps+k] += dphiPeak * float64(symbols[nsym-1]) * pulse[k]
	}

	return dphi
}

// refFrequency converts the dphi array to an instantaneous frequency
// trajectory of length NumSymbols × SamplesPerSymbol, skipping the
// leading dummy symbol.
func refFrequency(symbols [dsp.NumSymbols]uint8, baseFreqHz float64) []float64 {
	dphi := refDphi(symbols, baseFreqHz)
	nsps := dsp.SamplesPerSymbol
	nWave := dsp.NumSymbols * nsps

	freq := make([]float64, nWave)
	for i := range nWave {
		freq[i] = dphi[i+nsps] * dsp.SampleRate / (2 * math.Pi)
	}
	return freq
}

// refSynthGFSK synthesises an FT8 waveform using the ft8_lib algorithm,
// including phase integration and raised-cosine envelope shaping.
func refSynthGFSK(symbols [dsp.NumSymbols]uint8, baseFreqHz, amplitude float64) []float32 {
	dphi := refDphi(symbols, baseFreqHz)
	nsps := dsp.SamplesPerSymbol
	nWave := dsp.NumSymbols * nsps

	// Phase integration — skip first dummy symbol (start at nsps).
	phi := 0.0
	signal := make([]float32, nWave)
	for k := range nWave {
		signal[k] = float32(amplitude * math.Sin(phi))
		phi = math.Mod(phi+dphi[k+nsps], 2*math.Pi)
	}

	// Envelope shaping.
	nRamp := nsps / 8
	for i := range nRamp {
		env := (1 - math.Cos(2*math.Pi*float64(i)/float64(2*nRamp))) / 2
		signal[i] *= float32(env)
		signal[nWave-1-i] *= float32(env)
	}

	return signal
}
