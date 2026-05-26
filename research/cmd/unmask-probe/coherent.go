// coherent.go — adaptive coherent cancellation for unmask-probe.
//
// Implements the subtraction algorithm settled with operator 2026-05-26:
//   1. For each ±sample timing offset within a small search window:
//      - Synthesize the decoded reference at that offset
//      - Demodulate audio by the reference (audio × conj(reference))
//      - LPF-smooth the demodulated complex gain (Hann window, N=12000,
//        zero-phase via centered convolution)
//      - Apply overlap-area normalisation at TX-window edges
//      - Reconstruct the channel-distorted reference
//      - Score residual energy (audio - reconstructed)²
//   2. Pick the timing offset with minimum residual energy
//   3. RE-ESTIMATE c(t) at the chosen offset (don't reuse the search-time
//      estimate — operator directive 2026-05-26: avoid mixing hypotheses)
//   4. Subtract the re-estimated reconstruction from audio, bounded to TX window
//
// The LPF is linear-phase (Hann window applied centered). The cancellation
// AS A WHOLE is matched coherent — it uses the known decoded codeword to
// build the reference, so it's not a generic LPF on audio. See operator's
// 2026-05-26 framing for the linear-phase-on-estimate vs matched-coherent-on-
// cancellation distinction.
//
// Cost per signal: 41 LPF evaluations during timing search + 1 final estimate
// + 1 final subtract. With FFT-based convolution at length 196608, ~10M ops
// per LPF → ~430M ops per signal. ~16 signals × 6 WAVs × 3 iterations ≈ 60s
// corpus runtime.

package main

import (
	"math"
	"math/cmplx"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/research/synth"
)

const (
	// hannN is the LPF window length. 12000 samples = 1.0 s at fs=12 kHz.
	// Operator-chosen default (2026-05-26) as the middle ground between
	// fast tracking (6000) and over-smoothing (24000). Hann window has
	// main-lobe ≈ 1.4 cycles per window, so ≈ 1.4 Hz tracking bandwidth.
	hannN = 12000

	// fftN is the FFT length for linear convolution: must be ≥ nsamples +
	// hannN - 1 = 180000 + 11999 = 191999. 196608 = 2^16 × 3 is the
	// smallest 5-smooth value above that — the audio package's FFT
	// supports it natively.
	fftN = 196608

	// nSamples is the FT8 slot length we expect; consistent with the rest
	// of the probe.
	nSamples = 180000

	// txSymbolCount is the FT8 channel-symbol count (79) and nsps the
	// samples per symbol (1920). Replicated here to avoid coupling.
	txSymbolCount = 79
	nspsLocal     = 1920

	// timingSearchHalf bounds the timing-refinement delta search at ±20
	// samples (~1.7 ms ≈ 1% of a symbol). At larger shifts the GFSK pulse
	// boundaries move relative to symbol boundaries and the reference
	// becomes structurally wrong — the LPF can't absorb that.
	timingSearchHalf = 20
	timingSearchStep = 1
)

var (
	hannKernelFFT     []complex128
	hannKernelFFTOnce sync.Once

	fftPlan     *audio.Plan
	fftPlanOnce sync.Once

	// inputBufPool reuses zero-padded complex input buffers across LPF
	// calls. fftN is large (~3 MB) and the per-signal cost of allocating
	// fresh ones (42 calls × ~3 MB = ~125 MB) is GC-visible.
	inputBufPool = sync.Pool{
		New: func() any {
			b := make([]complex128, fftN)
			return &b
		},
	}
)

// getFFTPlan returns the shared complex FFT plan.
func getFFTPlan() *audio.Plan {
	fftPlanOnce.Do(func() {
		fftPlan = audio.NewPlan(fftN)
	})
	return fftPlan
}

// getHannKernelFFT returns the FFT of the centered Hann window of length
// hannN, zero-padded to fftN. Computed once and reused across all LPF calls.
func getHannKernelFFT() []complex128 {
	hannKernelFFTOnce.Do(func() {
		plan := getFFTPlan()
		// Centered Hann window: 0.5 × (1 - cos(2π·n/(N-1))) for n in [0, N).
		// Place the window starting at index 0 in the FFT buffer (linear
		// convolution semantics; we shift the output afterwards to recover
		// zero-phase alignment).
		buf := make([]complex128, fftN)
		for i := 0; i < hannN; i++ {
			buf[i] = complex(0.5*(1-math.Cos(2*math.Pi*float64(i)/float64(hannN-1))), 0)
		}
		hannKernelFFT = plan.FFT(buf)
	})
	return hannKernelFFT
}

// convolveLPF applies the prebuilt Hann LPF to a complex signal via FFT.
// Output length equals input length; result is centered (zero-phase) by
// shifting the convolution output by hannN/2 to compensate for the kernel
// being placed at index 0 in the FFT buffer.
func convolveLPF(signal []complex128) []complex128 {
	plan := getFFTPlan()
	kernelFFT := getHannKernelFFT()

	padPtr := inputBufPool.Get().(*[]complex128)
	defer inputBufPool.Put(padPtr)
	pad := *padPtr
	copy(pad, signal)
	// Zero the tail (Pool returns dirty buffers).
	for i := len(signal); i < fftN; i++ {
		pad[i] = 0
	}

	sigFFT := plan.FFT(pad)
	for i := range sigFFT {
		sigFFT[i] *= kernelFFT[i]
	}
	result := plan.IFFT(sigFFT)

	// Shift output by hannN/2 to align convolution output with input
	// indexing (zero-phase semantics).
	out := make([]complex128, len(signal))
	shift := hannN / 2
	for i := range out {
		src := i + shift
		if src < len(result) {
			out[i] = result[src]
		}
	}
	return out
}

// convolveLPFReal is convolveLPF specialised to real-valued input. Used
// for computing the overlap-weight mask (binary 1/0 indicator convolved
// with the Hann window).
func convolveLPFReal(mask []float64) []float64 {
	cmask := make([]complex128, len(mask))
	for i := range mask {
		cmask[i] = complex(mask[i], 0)
	}
	result := convolveLPF(cmask)
	out := make([]float64, len(mask))
	for i := range out {
		out[i] = real(result[i])
	}
	return out
}

// cancelCoherentAdaptiveInPlace removes signal d from audio via adaptive
// coherent cancellation. Searches ±timingSearchHalf samples for the dt
// that minimises post-subtract residual energy, re-estimates c(t) at
// that dt, then subtracts. Returns true on success.
//
// The audio buffer is modified in place over the TX-window range only.
// Samples outside the TX window are untouched.
func cancelCoherentAdaptiveInPlace(audio []float32, d decoded) bool {
	if len(audio) != nSamples {
		return false
	}

	txStartSample := int(math.Round((0.5 + d.dt) * float64(expectedSampleRate)))
	txEndSample := txStartSample + txSymbolCount*nspsLocal
	if txStartSample < 0 || txEndSample > len(audio) {
		// Edge-DT signals don't fit cleanly. Skip rather than under-handle.
		return false
	}

	// Overlap-normalisation weight. Constant across delta search; small
	// edge regions (≤ ±timingSearchHalf samples = ±20) shift the TX window
	// but the weight there is still within the LPF's edge transient zone
	// at delta=0, so reusing the delta=0 weight is a safe approximation.
	insideMask := make([]float64, nSamples)
	for i := txStartSample; i < txEndSample; i++ {
		insideMask[i] = 1.0
	}
	weight := convolveLPFReal(insideMask)

	// Pre-allocate cRaw buffer reused across delta search.
	cRaw := make([]complex128, nSamples)

	// --- Pass 1: timing search ---
	bestDelta := 0
	bestResidual := math.Inf(1)

	for delta := -timingSearchHalf; delta <= timingSearchHalf; delta += timingSearchStep {
		dt := d.dt + float64(delta)/float64(expectedSampleRate)
		zR := synth.SynthesizeComplex(d.codeword, d.preciseFreq, dt, len(audio), 1.0, 0.0)

		// Demod: audio × conj(zR), masked to TX window.
		for i := range cRaw {
			cRaw[i] = 0
		}
		for i := txStartSample; i < txEndSample; i++ {
			cRaw[i] = complex(float64(audio[i]), 0) * cmplx.Conj(zR[i])
		}

		cFiltered := convolveLPF(cRaw)

		// Score residual energy across the TX window.
		var residEnergy float64
		for i := txStartSample; i < txEndSample; i++ {
			if weight[i] < 1e-6 {
				continue
			}
			cN := cFiltered[i] / complex(weight[i], 0)
			recon := 2 * real(cN*zR[i])
			diff := float64(audio[i]) - recon
			residEnergy += diff * diff
		}
		if residEnergy < bestResidual {
			bestResidual = residEnergy
			bestDelta = delta
		}
	}

	// --- Pass 2: re-estimate c(t) at chosen delta + subtract ---
	bestDt := d.dt + float64(bestDelta)/float64(expectedSampleRate)
	zR := synth.SynthesizeComplex(d.codeword, d.preciseFreq, bestDt, len(audio), 1.0, 0.0)

	for i := range cRaw {
		cRaw[i] = 0
	}
	for i := txStartSample; i < txEndSample; i++ {
		cRaw[i] = complex(float64(audio[i]), 0) * cmplx.Conj(zR[i])
	}
	cFiltered := convolveLPF(cRaw)

	for i := txStartSample; i < txEndSample; i++ {
		if weight[i] < 1e-6 {
			continue
		}
		cN := cFiltered[i] / complex(weight[i], 0)
		recon := 2 * real(cN*zR[i])
		audio[i] -= float32(recon)
	}

	return true
}
