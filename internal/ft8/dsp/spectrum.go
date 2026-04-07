// spectrum.go — power spectrum computation from FFT output.
//
// The power spectrum is the magnitude-squared of each complex frequency bin:
//
//	P[k] = |X[k]|² = re(X[k])² + im(X[k])²
//
// It is used by the candidate detector (to correlate against Costas sync
// patterns) and by the spectrogram builder (to produce the time × frequency
// matrix). A separate log-power variant is provided for the soft demodulator,
// which operates in the log domain for numerical stability.

package dsp

import "math"

// PowerSpectrum returns the magnitude-squared (power) of each complex
// frequency bin: P[k] = re²+im² for k = 0..len(bins)-1.
//
// Returns nil for nil or empty input.
func PowerSpectrum(bins []complex64) []float32 {
	if len(bins) == 0 {
		return nil
	}
	ps := make([]float32, len(bins))
	for i, b := range bins {
		re := real(b)
		im := imag(b)
		ps[i] = re*re + im*im
	}
	return ps
}

// LogPowerSpectrum returns 10·log10(|X[k]|²) for each bin, i.e. the power
// in decibels. Bins with zero power are clamped to floorDB to avoid −Inf.
//
// This is useful for display (waterfall) and SNR estimation. The soft
// demodulator uses raw log(power) instead (via natural log), so this
// function targets human-readable output.
//
// Returns nil for nil or empty input.
func LogPowerSpectrum(bins []complex64, floorDB float32) []float32 {
	if len(bins) == 0 {
		return nil
	}
	ps := make([]float32, len(bins))
	for i, b := range bins {
		re := float64(real(b))
		im := float64(imag(b))
		power := re*re + im*im
		if power > 0 {
			ps[i] = float32(10 * math.Log10(power))
		} else {
			ps[i] = floorDB
		}
	}
	return ps
}
