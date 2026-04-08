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
//
// Precision note: [PowerSpectrum] computes power in float32 arithmetic,
// while [LogPowerSpectrum] computes power in float64 (needed for the log10
// step). The two functions do NOT share a common power computation, so for
// bins with large dynamic range or many significant digits, callers should
// not assume LogPowerSpectrum(bins)[k] == 10·log10(PowerSpectrum(bins)[k])
// to full float32 precision. In practice the difference is negligible
// (< 0.01 dB).

package dsp

import "math"

// PowerSpectrum returns the magnitude-squared (power) of each complex
// frequency bin: P[k] = re²+im² for k = 0..len(bins)-1.
//
// Computed in float32 arithmetic. See the package-level precision note.
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
// in decibels. The output is clamped so that no value falls below floorDB,
// which prevents −Inf for zero-power bins and caps the output range for
// subnormal values.
//
// Computed in float64 arithmetic for the log10 step. See the package-level
// precision note regarding differences from [PowerSpectrum].
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
	floor64 := float64(floorDB)
	ps := make([]float32, len(bins))
	for i, b := range bins {
		re := float64(real(b))
		im := float64(imag(b))
		power := re*re + im*im
		db := floor64
		if power > 0 {
			db = 10 * math.Log10(power)
		}
		if db < floor64 {
			db = floor64
		}
		ps[i] = float32(db)
	}
	return ps
}

// Log2PowerSpectrum returns log2(|X[k]|²) for each bin, matching the
// power representation used by ft8_lib. This log-domain representation
// compresses dynamic range and makes the Costas sync correlation more
// robust against strong narrowband interferers.
//
// Zero-power bins are floored at log2Floor (approximately −240 dB) to
// prevent −Inf from propagating into downstream computations.
//
// Returns nil for nil or empty input.
func Log2PowerSpectrum(bins []complex64) []float32 {
	if len(bins) == 0 {
		return nil
	}
	// log2(x) = ln(x) / ln(2)
	const ln2inv = 1.0 / 0.6931471805599453 // 1/ln(2)
	const log2Floor = -240.0                // floor for zero-power bins

	ps := make([]float32, len(bins))
	for i, b := range bins {
		re := float64(real(b))
		im := float64(imag(b))
		power := re*re + im*im
		if power > 0 {
			ps[i] = float32(math.Log(power) * ln2inv)
		} else {
			ps[i] = log2Floor
		}
	}
	return ps
}
