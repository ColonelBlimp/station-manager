// Package dsp implements the FT8 DSP pipeline: symbol mapping, FFT-based
// spectrogram computation, candidate detection, and soft demodulation.
//
// This package bridges the bit-level LDPC codec ([codec]) with the audio
// domain. On the TX side it maps coded bits to 8-FSK channel symbols; on
// the RX side it processes captured audio to produce soft LLR inputs for
// the LDPC decoder.
//
// The symbol mapping utilities ([BitsToSymbols], [InsertSync], and their
// inverses) are the foundation: they are needed by both the TX synthesis
// path and the RX demodulator.
//
// FT8 protocol parameters (8-FSK, 6.25 baud, 6.25 Hz tone spacing,
// 79 symbols per message) are defined as package-level constants.
//
// # Future Optimisation Opportunities
//
// The current implementation prioritises correctness and clarity over raw
// throughput. The following optimisations are identified for when profiling
// shows they are needed. None changes the public API.
//
// ## 1. Struct-based pipeline with pre-allocated buffers (all files)
//
// [ProcessWindow] is stateless: every call allocates a fresh spectrogram,
// FFT buffers, Hann coefficients, and LLR arrays. A struct-based API (e.g.
// a Processor struct with Reset/Process methods) could pre-allocate all
// working memory once and reuse it across successive 15-second windows,
// eliminating the majority of per-window allocations.
//
// ## 2. RealFFT per-call allocations (fft.go)
//
// [RealFFT] allocates a []complex128 input buffer and a []complex64 output
// buffer on every call. For [SpectrogramFT8], this means ~188 allocations
// of 2048×16 bytes (input) + 1025×8 bytes (output) per 15-second window.
// Moving to a struct-based FFT that owns its buffers would eliminate these
// entirely. The twiddle-factor cache (already implemented) would remain
// shared across instances.
//
// ## 3. Spectrogram per-row allocations (spectrum.go, spectrogram.go)
//
// [PowerSpectrum], [Log2PowerSpectrum], and [LogPowerSpectrum] each allocate
// a new []float32 for every spectrogram row. A flat pre-allocated matrix
// (single []float32 of nFrames×nBins) with row slicing would reduce GC
// pressure and improve cache locality.
//
// ## 4. Refinement grid search cost (candidates.go)
//
// [RefineCandidateAudio] evaluates a (time × frequency) grid around each
// coarse candidate: ±1 symbol period in refineTimeSteps steps × ±refineFreqRange
// in refineFreqStep increments ≈ 33 × 49 ≈ 1,617 grid points per candidate.
// Each point requires 21 [Goertzel] evaluations ([refineSyncScore]), so with
// 50 candidates the refinement phase performs ~1.7 million Goertzel calls of
// 1,920 samples each (~3.3 billion multiply-adds). This is the dominant cost
// in the RX pipeline, confirmed by profiling and the TestProcessWindow stack
// traces. Possible mitigations:
//
//   - Coarse-to-fine grid: start with a wider step, then refine around the
//     best point (halves total evaluations).
//   - Early termination: skip remaining grid points once the score exceeds
//     a high-confidence threshold.
//   - Parallelise across candidates: each candidate's refinement is
//     independent and can run in its own goroutine.
//   - Pre-compute Goertzel coefficients: the 2·cos(ω) coefficient for each
//     tone frequency is recomputed on every [Goertzel] call via math.Cos.
//     Since the refinement loop sweeps a small frequency range around 8
//     fixed tone offsets, caching these coefficients per frequency would
//     save redundant trig evaluations.
//
// ## 5. Goertzel coefficient recomputation (goertzel.go)
//
// [Goertzel] computes 2·cos(2πf/fs) on every call. In the demodulation
// path ([DemodulateAudio]), the same 8 tone frequencies are evaluated for
// all 58 data symbols — 464 calls with only 8 distinct coefficients. A
// variant that accepts pre-computed coefficients (or a [GoertzelTones] that
// caches them across symbols) would eliminate ~456 redundant math.Cos calls
// per candidate.
//
// Reference: ft8_lib (https://github.com/kgoba/ft8_lib),
// WSJT-X (https://sourceforge.net/projects/wsjt/).
package dsp
