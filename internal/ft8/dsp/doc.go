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
// # RX Pipeline Variants
//
// Two top-level entry points are provided:
//
//   - [ProcessWindow]: single-pass pipeline for backward compatibility and
//     unit testing. Uses standard-resolution spectrogram (1025 bins) with
//     mean-based sync scoring and flat refinement grid.
//   - [ProcessWindowMultiPass]: production pipeline with three enhancements:
//     (1) frequency-oversampled spectrogram (2049 bins via [SpectrogramFT8HiRes]),
//     (2) neighbor-comparison sync scoring ([FindCandidatesHiRes]),
//     (3) iterative signal subtraction — after each decode pass, the decoded
//     signal is subtracted from the audio and detection re-runs on the
//     residual to uncover weaker signals.
//
// The multi-pass pipeline also uses coarse-fine refinement
// ([RefineCandidateAudioFast]) for ~13× fewer Goertzel evaluations per
// candidate compared to [RefineCandidateAudio].
//
// # Future Optimisation Opportunities
//
// ## 1. Struct-based pipeline with pre-allocated buffers (all files)
//
// [ProcessWindow] and [ProcessWindowMultiPass] are stateless: every call
// allocates a fresh spectrogram, FFT buffers, Hann coefficients, and LLR
// arrays. A struct-based API (e.g. a Processor struct with Reset/Process
// methods) could pre-allocate all working memory once and reuse it across
// successive 15-second windows, eliminating the majority of per-window
// allocations.
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
// ## 4. Parallelism for candidate evaluation
//
// Each candidate's refine→demod→decode is independent. A worker-pool of
// runtime.NumCPU() goroutines processing candidates concurrently would
// nearly eliminate the per-candidate speed bottleneck, especially after the
// refinement grid is already shrunk by the coarse-fine approach. This is
// orthogonal to the pipeline structure and can be added independently.
//
// ## 5. Full GFSK signal subtraction
//
// The current signal subtraction uses simple per-symbol cosine tones
// (synthesizeSimple in multipass.go). Full GFSK synthesis (via the synth
// package) would provide more accurate cancellation, especially at symbol
// transitions. This requires either a higher-level package that imports
// both dsp and synth, or factoring the GFSK kernel into a shared package.
//
// Reference: ft8_lib (https://github.com/kgoba/ft8_lib),
// WSJT-X (https://sourceforge.net/projects/wsjt/).
package dsp
