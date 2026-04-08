// Package synth implements FT8 GFSK tone synthesis — the TX audio path
// from channel symbols to audio waveform samples.
//
// The synthesis pipeline converts a 79-symbol FT8 channel sequence (as
// produced by [dsp.InsertSync]) into a continuous-phase audio waveform
// using Gaussian Frequency-Shift Keying (GFSK). The Gaussian filter
// smooths the abrupt tone transitions, producing a constant-envelope
// signal with controlled spectral occupancy.
//
// The pipeline has three stages:
//
//  1. [GaussianFilter] — compute the Gaussian smoothing kernel (once).
//  2. [SmoothedFrequency] — convolve the step-wise frequency trajectory
//     with the kernel to produce a smooth per-sample frequency curve.
//  3. [Synthesize] / [SynthesizeWithAmplitude] — phase-integrate the
//     smoothed frequency and output sin(φ) samples with envelope shaping.
//
// The Gaussian filter parameters match the WSJT-X and ft8_lib reference
// implementations:
//
//   - BT product = 2.0 ([GaussianBT])
//   - Kernel span = 5 symbol periods ([KernelSpan], ±2 symbols)
//   - hmod = 1.0 (modulation index, meaning one tone spacing = one full
//     cycle per symbol period)
//
// The mathematical equivalence between this package's Gaussian kernel
// convolution and the erf-difference overlap-add used by WSJT-X
// ([gfsk_pulse.f90], [gen_ft8wave.f90]) and ft8_lib ([gen_ft8.c]) is
// exact: the GFSK pulse p(t) = 0.5·(erf(c·BT·(t+0.5)) − erf(c·BT·(t−0.5)))
// is the integral of the Gaussian impulse response h(t) over one symbol
// period, and overlap-adding p(t) weighted by tone values is identical to
// convolving the step-wise frequency signal with h(t).
//
// Reference: WSJT-X gfsk_pulse.f90, gen_ft8wave.f90;
// ft8_lib gen_ft8.c (GFSK_CONST_K = π·√(2/ln2) ≈ 5.336446).
package synth
