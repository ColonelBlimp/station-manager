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
//   - fftSize: the FFT input length; if larger than stepSamples, frames are
//     zero-padded to this length before the FFT. [RealFFT] further pads to the
//     next power of 2 if fftSize is not already a power of 2.
//   - stepSamples: the hop size in samples between successive frames — typically
//     one symbol period (1920 for FT8). This also determines the Hann window
//     length.
//
// The returned matrix has dimensions [nFrames][nBins], where:
//   - nFrames = number of full stepSamples-length frames fitting in the buffer
//   - nBins = NextPow2(max(fftSize, stepSamples))/2 + 1
//
// Only full frames are included — trailing samples that do not fill a complete
// frame are discarded.
//
// Returns nil if the input is empty, either size parameter is ≤ 0, or the
// buffer is shorter than one frame.
func Spectrogram(samples []float32, fftSize, stepSamples int) [][]float32 {
	if len(samples) == 0 || stepSamples <= 0 || fftSize <= 0 {
		return nil
	}
	if len(samples) < stepSamples {
		return nil
	}

	// Number of full frames.
	nFrames := len(samples) / stepSamples

	// Precompute the Hann window (reused for every frame).
	window := HannCoefficients(stepSamples)

	// FFT input length: at least stepSamples, zero-padded to fftSize if larger.
	inputLen := stepSamples
	if fftSize > inputLen {
		inputLen = fftSize
	}

	// Reusable frame buffer. The portion beyond stepSamples stays zero
	// (initialised by make, never modified since copy+ApplyWindow only
	// touch frame[:stepSamples]).
	frame := make([]float32, inputLen)

	result := make([][]float32, nFrames)
	for i := range nFrames {
		start := i * stepSamples

		// Overwrite the audio portion with fresh samples and apply the window.
		copy(frame[:stepSamples], samples[start:start+stepSamples])
		ApplyWindow(frame[:stepSamples], window)

		// FFT → power spectrum. RealFFT pads to the next power of 2 internally.
		bins := RealFFT(frame)
		result[i] = PowerSpectrum(bins)
	}

	return result
}
