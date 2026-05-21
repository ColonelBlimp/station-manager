package dsp

import (
	"github.com/ColonelBlimp/station-manager/internal/audio"
)

// Spectrogram computes the FT8 power spectrogram of audio samples
// at Fs=12000 Hz. Output is power[t][f] where:
//
//   - t indexes spectrogram time columns in [0, NHSYM) — each
//     column covers NSPS=1920 samples (160 ms) of audio, stepping
//     by NSTEP=480 samples (40 ms) between columns. The 4× temporal
//     oversampling vs. the natural symbol boundary gives sync
//     detection sub-symbol precision.
//   - f indexes positive-frequency bins in [0, NH1) — each bin
//     spans Fs/NFFT1 = 3.125 Hz. The 2× frequency oversampling
//     vs. the natural symbol rate gives sync detection sub-bin
//     precision.
//
// **Algorithm.** For each time column t:
//
//  1. Extract NSPS samples starting at audio[t*NSTEP].
//  2. Multiply by SpectrogramScale (= 1/300 — WSJT-X's empirical
//     scaling for float-range management; preserved for parity).
//  3. Zero-pad to NFFT1 (= 2 × NSPS) and FFT.
//  4. Take the positive-frequency power: |X[f]|² for f ∈ [0, NH1).
//
// Audio shorter than NMAX is zero-padded at the end (so the last
// few time columns may include silence); audio longer than NMAX is
// truncated.
//
// The output is a fresh slice of slices, dimensions [NHSYM][NH1].
// Allocation is one contiguous backing array (NHSYM × NH1 float64s
// = ~5.4 MB at the canonical sizes) sliced into row views to keep
// adjacent bins contiguous in memory — cache-friendly for the
// Costas correlator's per-bin scanning pattern.
//
// Panics if audio is nil. Empty audio (len = 0) returns an empty
// spectrogram so callers don't need a special-case guard.
func Spectrogram(samples []float32) [][]float64 {
	if samples == nil {
		panic("dsp.Spectrogram: nil samples")
	}

	// One contiguous backing array for the [NHSYM][NH1] grid.
	backing := make([]float64, NHSYM*NH1)
	out := make([][]float64, NHSYM)
	for t := range out {
		out[t] = backing[t*NH1 : (t+1)*NH1]
	}

	// Reusable real-input FFT scratch sized to NFFT1; the head
	// NSPS samples are filled with scaled audio, the tail
	// NFFT1-NSPS stays zero (the RealPlan zero-pads internally
	// when len(input) < N, but we keep a single backing buffer
	// re-filled per iteration for clarity).
	in := make([]float32, NFFT1)

	// One real-FFT Plan reused across all NHSYM (= 372) time
	// columns. RealPlan halves both compute time and allocation
	// vs the complex-Plan path (operator-authored algorithm
	// adapted into internal/audio/realfft.go from the standard
	// pack-and-unpack textbook technique).
	plan := audio.NewRealPlan(NFFT1)

	for t := 0; t < NHSYM; t++ {
		ia := t * NSTEP
		// Scaled real input, zero-padded tail.
		for k := 0; k < NSPS; k++ {
			idx := ia + k
			if idx < len(samples) {
				in[k] = samples[idx] * SpectrogramScale
			} else {
				in[k] = 0
			}
		}
		for k := NSPS; k < NFFT1; k++ {
			in[k] = 0
		}

		X := plan.FFT(in)

		// Power spectrum at positive-frequency bins. RealPlan
		// returns NH1+1 bins; we read NH1 (= NFFT1/2 = 1920), the
		// same range Spectrogram has always exposed to callers.
		for f := 0; f < NH1; f++ {
			re := real(X[f])
			im := imag(X[f])
			out[t][f] = re*re + im*im
		}
	}

	return out
}
