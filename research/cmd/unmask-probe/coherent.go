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
	"runtime"
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

	// timingSearchStep is the delta granularity. Step=2 cuts the search
	// from 41 deltas to 21 with negligible accuracy loss — the residual-
	// energy surface is smooth at this scale because the LPF absorbs
	// sub-sample shifts via the constant-phase component of c(t).
	timingSearchStep = 2

	// timingTightHalf / timingTightStep bound the re-refinement search
	// in iter 2+: ±2 samples in steps of 1 (5 deltas total) around the
	// best delta found in iter 1. Iter 1 has done the broad search; iter 2+
	// only needs a small correction in case residual audio shifts the
	// optimum by a sample or two.
	timingTightHalf = 2
	timingTightStep = 1
)

var (
	hannKernelFFT     []complex128
	hannKernelFFTOnce sync.Once

	fftPlan     *audio.Plan
	fftPlanOnce sync.Once

	// planPool holds per-goroutine FFT plans. audio.Plan is documented as
	// NOT safe for concurrent use, so parallel timing search uses one
	// plan per active goroutine. sync.Pool's lazy construction means
	// plans only exist when needed; idle goroutines release back to the
	// pool.
	planPool = sync.Pool{
		New: func() any { return audio.NewPlan(fftN) },
	}

	// inputBufPool reuses zero-padded complex input buffers across LPF
	// calls. fftN is large (~3 MB) and the per-signal cost of allocating
	// fresh ones (42 calls × ~3 MB = ~125 MB) is GC-visible.
	inputBufPool = sync.Pool{
		New: func() any {
			b := make([]complex128, fftN)
			return &b
		},
	}

	// cRawPool reuses the demodulated-product buffer (180k complex128 =
	// 2.88 MB). Each timing-search delta needs its own; pooling across
	// the parallel fan-out avoids allocating one per worker call.
	cRawPool = sync.Pool{
		New: func() any {
			b := make([]complex128, nSamples)
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
// hannN, zero-padded to fftN. Computed once and reused across all LPF
// calls. The result is purely a function of the input (Hann window),
// not of the Plan instance used to compute it, so it's safe to share
// across all per-goroutine plans.
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
//
// The plan is caller-supplied so the parallel timing-search can hold
// one Plan per goroutine (audio.Plan is not concurrent-safe).
func convolveLPF(plan *audio.Plan, signal []complex128) []complex128 {
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
func convolveLPFReal(plan *audio.Plan, mask []float64) []float64 {
	cmask := make([]complex128, len(mask))
	for i := range mask {
		cmask[i] = complex(mask[i], 0)
	}
	result := convolveLPF(plan, cmask)
	out := make([]float64, len(mask))
	for i := range out {
		out[i] = real(result[i])
	}
	return out
}

// cancelCoherentAdaptiveInPlace is the broad-search variant — full ±20
// delta search at step=2. Used in iter 1 of the outer iterative loop;
// returns the best delta found so iter 2+ can do a tight search around
// it via cancelCoherentAdaptiveAroundDelta.
//
// The audio buffer is modified in place over the TX-window range only.
// Samples outside the TX window are untouched.
func cancelCoherentAdaptiveInPlace(audio []float32, d decoded) (bestDelta int, ok bool) {
	return cancelCoherentAdaptiveSearched(audio, d, -timingSearchHalf, timingSearchHalf, timingSearchStep)
}

// cancelCoherentAdaptiveAroundDelta is the tight-search variant — ±2
// delta search at step=1, centered on a previously-found best delta.
// Used in iter 2+ of the outer iterative loop: the broad search from
// iter 1 found the right neighborhood; subsequent iterations only need
// small corrections in case residual audio drift shifts the optimum.
func cancelCoherentAdaptiveAroundDelta(audio []float32, d decoded, centerDelta int) (bestDelta int, ok bool) {
	return cancelCoherentAdaptiveSearched(audio, d,
		centerDelta-timingTightHalf, centerDelta+timingTightHalf, timingTightStep)
}

// cancelCoherentAdaptiveSearched does the cancellation with a configurable
// delta search range. The variants above are thin wrappers.
//
// Parameter is `audioBuf` not `audio` to avoid shadowing the imported
// audio package, which we need inside this function to look up
// `audio.Plan` from the goroutine-local plan pool.
func cancelCoherentAdaptiveSearched(audioBuf []float32, d decoded, deltaMin, deltaMax, deltaStep int) (bestDelta int, ok bool) {
	if len(audioBuf) != nSamples {
		return 0, false
	}

	txStartSample := int(math.Round((0.5 + d.dt) * float64(expectedSampleRate)))
	txEndSample := txStartSample + txSymbolCount*nspsLocal
	if txStartSample < 0 || txEndSample > len(audioBuf) {
		// Edge-DT signals don't fit cleanly. Skip rather than under-handle.
		return 0, false
	}

	// Overlap-normalisation weight. Constant across delta search; small
	// edge regions (≤ ±timingSearchHalf samples = ±20) shift the TX window
	// but the weight there is still within the LPF's edge transient zone
	// at delta=0, so reusing the delta=0 weight is a safe approximation.
	insideMask := make([]float64, nSamples)
	for i := txStartSample; i < txEndSample; i++ {
		insideMask[i] = 1.0
	}
	mainPlan := planPool.Get().(*audio.Plan)
	defer planPool.Put(mainPlan)
	weight := convolveLPFReal(mainPlan, insideMask)

	// --- Pass 1: timing search, parallel across deltas ---
	// Build the delta list up front so we can fan out across goroutines.
	var deltas []int
	for delta := deltaMin; delta <= deltaMax; delta += deltaStep {
		deltas = append(deltas, delta)
	}

	type result struct {
		residEnergy float64
	}
	results := make([]result, len(deltas))

	// Limit concurrency to NumCPU so the Plan pool's working set stays
	// bounded. Each goroutine acquires its own Plan from the pool.
	maxParallel := runtime.NumCPU()
	if maxParallel > len(deltas) {
		maxParallel = len(deltas)
	}
	sem := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	for idx, delta := range deltas {
		wg.Add(1)
		sem <- struct{}{}
		go func(idx, delta int) {
			defer wg.Done()
			defer func() { <-sem }()

			plan := planPool.Get().(*audio.Plan)
			defer planPool.Put(plan)

			dt := d.dt + float64(delta)/float64(expectedSampleRate)
			zR := synth.SynthesizeComplex(d.codeword, d.preciseFreq, dt, len(audioBuf), 1.0, 0.0)

			cRawPtr := cRawPool.Get().(*[]complex128)
			defer cRawPool.Put(cRawPtr)
			cRaw := *cRawPtr
			for i := range cRaw {
				cRaw[i] = 0
			}
			for i := txStartSample; i < txEndSample; i++ {
				cRaw[i] = complex(float64(audioBuf[i]), 0) * cmplx.Conj(zR[i])
			}

			cFiltered := convolveLPF(plan, cRaw)

			var residEnergy float64
			for i := txStartSample; i < txEndSample; i++ {
				if weight[i] < 1e-6 {
					continue
				}
				cN := cFiltered[i] / complex(weight[i], 0)
				recon := 2 * real(cN*zR[i])
				diff := float64(audioBuf[i]) - recon
				residEnergy += diff * diff
			}
			results[idx] = result{residEnergy: residEnergy}
		}(idx, delta)
	}
	wg.Wait()

	bestDelta = deltas[0]
	bestResidual := math.Inf(1)
	for i, r := range results {
		if r.residEnergy < bestResidual {
			bestResidual = r.residEnergy
			bestDelta = deltas[i]
		}
	}
	_ = bestResidual

	// --- Pass 2: re-estimate c(t) at chosen delta + subtract ---
	bestDt := d.dt + float64(bestDelta)/float64(expectedSampleRate)
	zR := synth.SynthesizeComplex(d.codeword, d.preciseFreq, bestDt, len(audioBuf), 1.0, 0.0)

	cRawPtr := cRawPool.Get().(*[]complex128)
	defer cRawPool.Put(cRawPtr)
	cRaw := *cRawPtr
	for i := range cRaw {
		cRaw[i] = 0
	}
	for i := txStartSample; i < txEndSample; i++ {
		cRaw[i] = complex(float64(audioBuf[i]), 0) * cmplx.Conj(zR[i])
	}
	cFiltered := convolveLPF(mainPlan, cRaw)

	for i := txStartSample; i < txEndSample; i++ {
		if weight[i] < 1e-6 {
			continue
		}
		cN := cFiltered[i] / complex(weight[i], 0)
		recon := 2 * real(cN*zR[i])
		audioBuf[i] -= float32(recon)
	}

	return bestDelta, true
}
