// spectrogram.go — time × frequency power matrix from an audio capture buffer.
//
// The spectrogram builder ties together windowing ([HannCoefficients],
// [ApplyWindow]), FFT ([RealFFT]), and power spectrum ([PowerSpectrum])
// into a single function that produces the matrix consumed by candidate
// detection ([FindCandidates]) and soft demodulation ([Demodulate]).
//
// For FT8, a typical call is:
//
//	sg := Spectrogram(samples, SamplesPerSymbol, SamplesPerSymbol)
//
// This slides a 1920-sample Hann-windowed FFT across the 180 000-sample
// capture buffer with no overlap (hop = symbol period), producing ~93 time
// steps × 1025 frequency bins (1920 → zero-padded to 2048 → 1025 bins).

package dsp

// Spectrogram computes a time × frequency power matrix from a capture buffer.
//
// Parameters:
//   - samples: the audio capture buffer (e.g., 180 000 samples at 12 kHz)
//   - fftSize: the number of audio samples read per frame (and the Hann window
//     length). [RealFFT] further pads to the next power of 2 if fftSize is not
//     already a power of 2.
//   - stepSamples: the hop size in samples between successive frames. When
//     stepSamples < fftSize, frames overlap; e.g., stepSamples = fftSize/2
//     gives 50% overlap.
//
// The returned matrix has dimensions [nFrames][nBins], where:
//   - nFrames = number of full fftSize-length frames fitting in the buffer
//     at the given step: (len(samples) − fftSize) / stepSamples + 1
//   - nBins = NextPow2(fftSize)/2 + 1
//
// Only full frames are included — trailing samples that do not fill a complete
// fftSize-length frame are discarded.
//
// Returns nil if the input is empty, either size parameter is ≤ 0, or the
// buffer is shorter than fftSize.
func Spectrogram(samples []float32, fftSize, stepSamples int) [][]float32 {
	if len(samples) == 0 || stepSamples <= 0 || fftSize <= 0 {
		return nil
	}
	if len(samples) < fftSize {
		return nil
	}

	// Number of full frames that fit.
	nFrames := (len(samples)-fftSize)/stepSamples + 1

	// Precompute the Hann window for the full FFT frame.
	window := HannCoefficients(fftSize)

	// Reusable frame buffer (length = fftSize).
	frame := make([]float32, fftSize)

	result := make([][]float32, nFrames)
	for i := range nFrames {
		start := i * stepSamples

		// Read fftSize samples and apply the window.
		copy(frame, samples[start:start+fftSize])
		ApplyWindow(frame, window)

		// FFT → power spectrum. RealFFT pads to the next power of 2 internally.
		bins := RealFFT(frame)
		result[i] = PowerSpectrum(bins)
	}

	return result
}

// SpectrogramFT8 computes an FT8-optimised spectrogram matching the ft8_lib
// reference implementation:
//
//   - Frame size: [SamplesPerSymbol] (1920) — one FT8 symbol period
//   - Step: [SamplesPerSymbol]/2 (960) — half-symbol overlap for 2× time
//     resolution, matching ft8_lib's nfft/2 step
//   - Periodic Hann window — the DFT-periodic variant used by ft8_lib
//   - Log2(power) output — log-domain representation for robust sync detection
//
// The returned matrix has dimensions [nFrames][nBins], where:
//   - nFrames ≈ (len(samples) − 1920) / 960 + 1
//   - nBins = NextPow2(1920)/2 + 1 = 1025
//
// Each row represents a half-symbol time step (80 ms). Symbol k of an FT8
// message starting at row t is at row t + 2*k.
//
// Returns nil if the buffer is shorter than one symbol period.
func SpectrogramFT8(samples []float32) [][]float32 {
	fftSize := SamplesPerSymbol  // 1920
	step := SamplesPerSymbol / 2 // 960 — half-symbol overlap

	if len(samples) < fftSize || step <= 0 {
		return nil
	}

	nFrames := (len(samples)-fftSize)/step + 1

	// Periodic Hann window matching ft8_lib.
	window := HannPeriodicCoefficients(fftSize)

	frame := make([]float32, fftSize)

	result := make([][]float32, nFrames)
	for i := range nFrames {
		start := i * step

		copy(frame, samples[start:start+fftSize])
		ApplyWindow(frame, window)

		bins := RealFFT(frame)
		result[i] = Log2PowerSpectrum(bins)
	}

	return result
}
