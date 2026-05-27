package sandbox

import (
	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// FT8 protocol parameters per the QEX 2020 paper §4. Re-declared per
// the research-tree firewall rule (no imports from research/candidates/
// or internal/ft8/*).
const (
	fs    = 12000.0  // sample rate
	nsps  = 1920     // samples per symbol (160 ms at fs)
	nfft  = 2 * nsps // 3840-point FFT → 3.125 Hz output bin spacing
	nstep = nsps / 4 // 480-sample column step → 40 ms (4× time oversample)
)

// Spectrogram returns spec[t][f] = |X[t][f]|² for time frames at
// nstep-sample intervals and frequency bins [0, nfft/2).
//
// Geometry differs from research/candidates/'s spectrogram: that one
// reads a full nfft-sample (2 symbol periods) window of audio per
// frame. This one reads a 1-symbol (nsps = 1920) window and zero-pads
// the back half of the FFT buffer. Same output bin count and spacing
// (1920 bins × 3.125 Hz), but the underlying frequency resolution is
// 6.25 Hz — the extra bins are sinc-interpolated, not independent.
// Trade: doubles each frame's mainlobe width but halves its time
// extent. Whether this is a win depends on what the downstream
// detector wants — finer freq vs better time localisation.
//
// No input scaling: samples enter the FFT verbatim as ReadWAV
// returns them (normalised float32 in [-1.0, 1.0]). The FFT is
// linear and all candidate-detection metrics downstream of a power
// spectrum are ratios; any uniform scale factors out.
//
// The Nyquist bin (X[nfft/2]) is dropped — for FT8 at 12 kHz the
// candidate search tops out well below the Nyquist at 6000 Hz.
func Spectrogram(samples []float32) [][]float64 {
	if len(samples) < nsps {
		return nil
	}

	plan := audio.NewRealPlan(nfft)
	halfFFT := nfft / 2

	nFrames := 0
	for s := 0; s+nsps <= len(samples); s += nstep {
		nFrames++
	}
	if nFrames == 0 {
		return nil
	}

	backing := make([]float64, nFrames*halfFFT)
	spec := make([][]float64, nFrames)

	// Reused buffer: front half receives nsps audio samples per frame;
	// back half stays at the zero value the make() established at
	// allocation, providing the zero-padding for the 2× oversampled FFT.
	chunk := make([]float32, nfft)

	for t := 0; t < nFrames; t++ {
		copy(chunk[:nsps], samples[t*nstep:t*nstep+nsps])
		X := plan.FFT(chunk)
		row := backing[t*halfFFT : (t+1)*halfFFT]
		for f := 0; f < halfFFT; f++ {
			re, im := real(X[f]), imag(X[f])
			row[f] = re*re + im*im
		}
		spec[t] = row
	}
	return spec
}
