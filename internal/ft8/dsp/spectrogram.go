// spectrogram.go — time × frequency power matrix from an audio capture buffer.
//
// The spectrogram builder ties together windowing ([HannCoefficients],
// [ApplyWindow]), FFT ([RealFFT]), and power spectrum ([PowerSpectrum])
// into a single function that produces the matrix consumed by candidate
// detection ([FindCandidates]) and soft demodulation.
//
// Two spectrogram variants are provided:
//
//   - [Spectrogram] produces linear power values. These can be passed to
//     [Demodulate] (which applies math.Log internally).
//   - [SpectrogramFT8] produces log2(power) values for robust sync detection.
//     It is used by [ProcessWindow] for candidate detection; demodulation
//     is handled by [DemodulateAudio] (Goertzel on raw audio), NOT by
//     [Demodulate].
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

// SpectrogramFT8 computes an FT8-optimised spectrogram matching WSJT-X's
// sync8.f90 parameters:
//
//   - Analysis window: [SamplesPerSymbol] (1920) — one FT8 symbol period
//   - FFT size: 2 × [SamplesPerSymbol] (3840) — zero-padded for exact
//     2-bins-per-tone alignment (NFFT1 = 2 × NSPS in WSJT-X)
//   - Step: [SamplesPerSymbol]/4 (480) — quarter-symbol overlap (NSTEP in
//     WSJT-X), giving 4× time resolution
//   - Periodic Hann window — the DFT-periodic variant
//   - Log2(power) output — log-domain representation for sync detection
//
// The returned matrix has dimensions [nFrames][nBins], where:
//   - nFrames ≈ (len(samples) − 1920) / 480 + 1 ≈ 372 (matching WSJT-X NHSYM)
//   - nBins = 3840/2 + 1 = 1921 (matching WSJT-X NH1)
//
// Each row represents a quarter-symbol time step (40 ms). Symbol k of an FT8
// message starting at row t is at row t + 4*k.
//
// Bin alignment: with binWidth = 12000/3840 = 3.125 Hz, each FT8 tone
// (6.25 Hz spacing) maps to exactly 2 bins — eliminating the fractional-bin
// alignment issue that degraded sync scoring with the old 2048-point FFT.
//
// Returns nil if the buffer is shorter than one symbol period.
func SpectrogramFT8(samples []float32) [][]float32 {
	analysisLen := SamplesPerSymbol // 1920 — audio samples per frame
	nfft := 2 * SamplesPerSymbol    // 3840 — zero-padded FFT size (WSJT-X NFFT1)
	step := SamplesPerSymbol / 4    // 480 — quarter-symbol step (WSJT-X NSTEP)

	if len(samples) < analysisLen || step <= 0 {
		return nil
	}

	nFrames := (len(samples)-analysisLen)/step + 1

	// Periodic Hann window of the analysis length (not the FFT size).
	window := HannPeriodicCoefficients(analysisLen)

	frame := make([]float32, analysisLen)

	result := make([][]float32, nFrames)
	for i := range nFrames {
		start := i * step

		copy(frame, samples[start:start+analysisLen])
		ApplyWindow(frame, window)

		// 3840-point FFT via Bluestein — gives exact 2 bins/tone alignment.
		bins := RealFFTN(frame, nfft)
		result[i] = Log2PowerSpectrum(bins)
	}

	return result
}

// SpectrogramFT8HiRes computes a frequency-oversampled FT8 spectrogram
// with sub-bin frequency resolution.
//
// It uses a longer analysis window of [SamplesPerSymbol] × freqOSR samples
// (e.g., 3840 for freqOSR=2) and an FFT size of 2 × analysisLen (e.g., 7680),
// giving freqOSR × 2 bins per tone. Combined with the quarter-symbol time
// step matching WSJT-X's NSTEP, this provides high time–frequency resolution
// for multi-pass candidate detection and signal subtraction.
//
// Parameters:
//   - samples: audio capture buffer (one FT8 window)
//   - freqOSR: frequency oversampling rate (typically 2). Values < 1 are
//     treated as 1 (standard resolution).
//
// Returns nil if the buffer is shorter than the analysis window.
func SpectrogramFT8HiRes(samples []float32, freqOSR int) [][]float32 {
	if freqOSR < 1 {
		freqOSR = 1
	}
	if freqOSR == 1 {
		return SpectrogramFT8(samples) // no oversampling — use standard path
	}

	// Analysis window: SamplesPerSymbol × freqOSR (e.g., 3840 for freqOSR=2).
	analysisLen := SamplesPerSymbol * freqOSR
	nfft := 2 * analysisLen      // e.g., 7680 — exact integer bins/tone
	step := SamplesPerSymbol / 4 // 480 — quarter-symbol step

	if len(samples) < analysisLen || step <= 0 {
		return nil
	}

	nFrames := (len(samples)-analysisLen)/step + 1

	// Periodic Hann window of length analysisLen.
	window := HannPeriodicCoefficients(analysisLen)

	frame := make([]float32, analysisLen)

	result := make([][]float32, nFrames)
	for i := range nFrames {
		start := i * step

		copy(frame, samples[start:start+analysisLen])
		ApplyWindow(frame, window)

		bins := RealFFTN(frame, nfft)
		result[i] = Log2PowerSpectrum(bins)
	}

	return result
}
