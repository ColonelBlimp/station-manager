// goertzel.go — single-frequency power measurement via the Goertzel algorithm.
//
// The Goertzel algorithm efficiently computes the DFT magnitude-squared at a
// single frequency, without computing the full FFT. For FT8 demodulation
// this is critical: the FFT bin width (5.859 Hz at 2048-point / 12 kHz) does
// not match the FT8 tone spacing (6.25 Hz), causing spectral leakage when
// tone powers are read from FFT bins. The Goertzel algorithm targets the
// exact FT8 tone frequency, eliminating this mismatch entirely.
//
// Complexity: O(N) per frequency, where N = len(frame). For 8 tones × 58
// data symbols per FT8 message, this is 464 Goertzel evaluations of 1920
// samples each — about 0.9M multiply-adds, well within budget for a 15 s
// RX window processed in under 100 ms.
//
// Reference: Goertzel, G. "An Algorithm for the Evaluation of Finite
// Trigonometric Series", The American Mathematical Monthly, 1958.

package dsp

import "math"

// Goertzel computes the DFT magnitude-squared (power) at a single frequency
// using the Goertzel algorithm. The frame is multiplied by the Hann window
// coefficients before processing.
//
// Parameters:
//   - frame: audio samples for one symbol period (len = SamplesPerSymbol)
//   - hann:  pre-computed Hann window coefficients (same length as frame)
//   - freqHz: the target frequency in Hz
//
// Returns the power |X(f)|² at the target frequency. Returns 0 for empty
// input or if hann is shorter than frame.
func Goertzel(frame, hann []float32, freqHz float64) float64 {
	n := len(frame)
	if n == 0 || len(hann) < n {
		return 0
	}

	// Compute the normalised angular frequency.
	// k = f * N / fs  (fractional bin index)
	// ω = 2π k / N = 2π f / fs
	w := 2.0 * math.Pi * freqHz / float64(SampleRate)
	coeff := 2.0 * math.Cos(w)

	var s0, s1, s2 float64
	for i := range n {
		s0 = float64(frame[i])*float64(hann[i]) + coeff*s1 - s2
		s2 = s1
		s1 = s0
	}

	// |X(f)|² = s1² + s2² − coeff·s1·s2
	return s1*s1 + s2*s2 - coeff*s1*s2
}

// GoertzelTones computes the DFT power at each of the 8 FT8 tones relative
// to a base frequency. The tones are spaced at [ToneSpacing] Hz intervals:
//
//	tone k → baseFreqHz + k × ToneSpacing,  k = 0..7
//
// Also returns the index of the tone with the highest power (peak).
func GoertzelTones(frame, hann []float32, baseFreqHz float64) (powers [NumTones]float64, peak int) {
	for k := range NumTones {
		freq := baseFreqHz + float64(k)*ToneSpacing
		powers[k] = Goertzel(frame, hann, freq)
		if powers[k] > powers[peak] {
			peak = k
		}
	}
	return powers, peak
}
