package sandbox

import (
	"fmt"

	"github.com/ColonelBlimp/station-manager/research/sandbox/pfft"
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
//
// FFT backend: PocketFFT via research/sandbox/pfft (BSD 3-Clause).
// Per Session 97 bench results, ~6× faster than internal/audio on
// the per-slot forward FFT path. The plan is built once per call
// and reused across all frames; plan-create failure on nfft=3840
// is essentially impossible (no large prime factors), so the function
// panics rather than complicate the signature with an error return.
func Spectrogram(samples []float32) [][]float64 {
	if len(samples) < nsps {
		return nil
	}

	plan, err := pfft.NewRealPlan(nfft)
	if err != nil {
		panic(fmt.Sprintf("sandbox.Spectrogram: pfft.NewRealPlan(%d): %v", nfft, err))
	}
	defer plan.Close()
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

	// Reused buffer for in-place FFT. Front nsps positions receive the
	// audio (widened to float64); back nfft-nsps positions are re-zeroed
	// each frame because the previous frame's FFT output overwrote them.
	chunk := make([]float64, nfft)

	for t := 0; t < nFrames; t++ {
		base := t * nstep
		for i := 0; i < nsps; i++ {
			chunk[i] = float64(samples[base+i])
		}
		for i := nsps; i < nfft; i++ {
			chunk[i] = 0
		}
		if err := plan.Forward(chunk); err != nil {
			panic(fmt.Sprintf("sandbox.Spectrogram: plan.Forward: %v", err))
		}

		// FFTPack packed real-FFT layout: chunk[0]=Re(X[0]) (DC); for
		// k in [1, nfft/2-1]: chunk[2k-1]=Re(X[k]), chunk[2k]=Im(X[k]);
		// chunk[nfft-1]=Re(X[nfft/2]) (Nyquist). We drop the Nyquist bin.
		row := backing[t*halfFFT : (t+1)*halfFFT]
		row[0] = chunk[0] * chunk[0]
		for f := 1; f < halfFFT; f++ {
			re := chunk[2*f-1]
			im := chunk[2*f]
			row[f] = re*re + im*im
		}
		spec[t] = row
	}
	return spec
}
