# KissFFT vendored sources

This directory contains vendored sources from KissFFT
(https://github.com/mborgerding/kissfft) compiled into Station Manager
via CGO. License is BSD-3-Clause — see `LICENSE`. Compatible with SM's
MIT licence; permissive-redistribution requirements satisfied by
including the LICENSE file and these source-level SPDX headers.

## Files

| File | Purpose |
|---|---|
| `kiss_fft.h` / `kiss_fft.c` | Complex-input forward + inverse FFT. The general-purpose entry point used by SM. |
| `_kiss_fft_guts.h` | Internal helpers included by `kiss_fft.c`. |
| `kiss_fftr.h` / `kiss_fftr.c` | Real-input FFT primitives. Vendored for potential future use — SM's forward FFT paths (Spectrogram, ForwardSpectrum) operate on real audio and could exploit the ~2× real-FFT speedup. Not used by the current CGO wrapper. |

## Build configuration

KissFFT's `kiss_fft_scalar` typedef defaults to `float`. SM requires
**double precision** to match the existing pure-Go path's numerical
behaviour (LDPC magnitudes and BP convergence are tuned against
double-precision spectra). Override via the CGO CFLAGS in
`internal/audio/fft_cgo.go`:

```go
// #cgo CFLAGS: -Dkiss_fft_scalar=double
```

This forces `kiss_fft_scalar = double` throughout, giving us
double-precision complex (`kiss_fft_cpx { double r; double i; }`).

## Why CGO + KissFFT (vs alternatives)

Profiling showed the pure-Go mixed-radix FFT was ~33% of decode-pipeline
CPU on real-WAV captures. CGO-linked KissFFT is ~4-5× faster on the
FT8 sizes (N = 32, 3200, 3840, 192000) — a ~25-28% slot-time saving.

Alternatives considered:

- **FFTW3** — fastest available, but GPL v2; conflicts with SM's MIT
  per ADR 0021 § Licensing constraint. Excluded.
- **PocketFFT** — Apache 2.0, similar performance to KissFFT. Either
  would work; KissFFT chosen because the operator's go-ft8 research
  repo already had vetted KissFFT sources locally, simplifying the
  vendoring step.
- **Sleef DFT** — newer, similar performance. Sleef is already in
  the SM dependency tree for tanh/atanh (via `sleef-devel` system
  package), so using Sleef DFT would reduce the CGO surface to one
  library. Deferred — KissFFT's stability + the existing vetted
  vendor copy made it the lower-risk first cut.

## Build-tag gating

The CGO path is gated by `//go:build ft8cgo` in `internal/audio/fft_cgo.go`.
Default builds (no tags) use the pure-Go `audio.Plan` in `fft.go`. CI
runs the pure-Go path; operator's local fast-path build uses
`-tags ft8cgo` (wired into `task deploy:local:dev`).
