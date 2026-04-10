# FT8 Decoder Integration Testing — Context Handoff

**Date:** 2026-04-10
**Updated:** 2026-04-10
**Status:** Baseband demodulation implemented (4/13 decoded), further improvement work next

## What Was Built

### Phase 1: ft8test CLI (complete)
A CLI tool at `cmd/ft8test/` for stage-by-stage integration testing of the FT8 decode pipeline. Each stage is a separate Cobra subcommand.

### Phase 2: WSJT-X-style baseband demodulation (complete)
Ported WSJT-X's `ft8_downsample.f90`, `sync8d.f90`, and the core of `ft8b.f90` to Go, improving decode rate from **1/13 to 4/13** messages.

### Files Created/Modified

**New files (Phase 1):**
- `cmd/ft8test/main.go` — entry point
- `cmd/ft8test/go.mod` — module with `replace ../../internal`
- `cmd/ft8test/cmd/root.go` — Cobra root + DI wiring (config, logging)
- `cmd/ft8test/cmd/devices.go` — list audio capture/playback devices
- `cmd/ft8test/cmd/capture.go` — Stage 1: audio capture + decimation → WAV (also contains `readWAV`, `saveWAV`, `reportAudioStats` helpers)
- `cmd/ft8test/cmd/spectrogram.go` — Stage 2: compute spectrogram, report diagnostics
- `cmd/ft8test/cmd/candidates.go` — Stage 3: Costas sync candidate detection
- `cmd/ft8test/cmd/decode.go` — Stage 4: full pipeline with `--diagnose` and `--baseband` flags

**New files (Phase 2 — baseband demodulation):**
- `internal/ft8/dsp/baseband.go` — frequency-domain downsampling (`LongFFT`, `DownsampleBaseband`, `complexIFFT`, `complexIFFTUnnorm`); port of `ft8_downsample.f90`
- `internal/ft8/dsp/baseband_demod.go` — per-symbol 32-pt DFT, multi-pass LLR extraction (nsym=1,2,3 + bit-normalised), `Sync8d` fine sync, `NormalizeBmet`; port of `ft8b.f90` lines 104–239 + `sync8d.f90`
- `internal/ft8/dsp/baseband_pipeline.go` — `ProcessWindowBaseband` (multi-pass pipeline with signal subtraction), `ProcessWindowBasebandWithDiag` (diagnostic variant)
- `internal/ft8/dsp/baseband_test.go` — LLR comparison test across all 4 decode passes

**Modified files:**
- `go.work` — added `./cmd/ft8test`
- `Taskfile.yml` — added `ft8test` build task
- `cmd/ft8test/cmd/decode.go` — added `--baseband` flag that routes to the new baseband pipeline

**Test data:**
- `internal/ft8/dsp/testdata/ft8test_capture_20260410.wav` — known-good 15s capture, 13 WSJT-X decodes

### Build & Run

```bash
task ft8test                                          # build → build/bin/ft8test
./build/bin/ft8test decode --input capture.wav        # Goertzel decode (original)
./build/bin/ft8test decode --input capture.wav --baseband  # baseband decode (new)
./build/bin/ft8test decode --input capture.wav --baseband --diagnose  # with diagnostics
```

## Test Results (capture.wav)

### WSJT-X decoded 13 messages from the same WAV:
```
-3  1863 Hz  SV2SIH ES2AJ -16
-11 1310 Hz  VE1WT K4GBI 73
-6  1903 Hz  SV2SIH KI8JP -10
-12 1692 Hz  CQ PV8AJ FJ92
-15 2098 Hz  <...> RA1OHX KP91
-21 2328 Hz  KB7THX WB9VGJ RR73
-15  950 Hz  A61CK UA1CEI KP50
-15 1273 Hz  <...> LU3DXU GF05
-17 1814 Hz  <...> RA6ABC KN96
-23  835 Hz  ES2AJ UA3LAR KO75
-19  579 Hz  A61CK W3DQS -12
-21 2209 Hz  HZ1TT RU1AB R-10
-16  461 Hz  <...> RV6ASU KN94
```

### Our pipeline results:

| Pipeline | Decoded | Messages |
|---|---|---|
| Goertzel (original) | **1/13** | VE1WT K4GBI 73 |
| Baseband (new) | **4/13** | VE1WT K4GBI 73, <...> RA1OHX KP91, SV2SIH KI8JP -10, HZ1TT RU1AB R-10 |

### Baseband decode details:
```
  TIME (s)     SNR     FREQ  MESSAGE
    +0.140  +17.1   1309.9  VE1WT K4GBI 73
    +0.090  +11.2   2098.4  <...> RA1OHX KP91
    +0.030  +18.2   1902.9  SV2SIH KI8JP -10
    +1.080   +3.3   2208.4  HZ1TT RU1AB R-10
```

All 4 decoded messages use the nsym=1 (single-symbol) LLR pass. The multi-symbol passes (nsym=2,3) and bit-normalised pass produce LLRs with 96–100% sign agreement with nsym=1, but our BP-only LDPC decoder fails to converge on them.

## Architecture: Baseband Demodulation Pipeline

### Processing flow (per candidate):
```
audio (180k samples, 12 kHz)
  → LongFFT: 192000-point real FFT (Bluestein, computed once per window)
  → DownsampleBaseband: extract ±5-tone band around f0, cosine taper, cshift, 3200-pt IFFT
  → 3200 complex samples at 200 Hz, f0 at DC
  → Sync8d: fine time sync (±10 samples) + fine freq sync (±2.5 Hz, 0.5 Hz steps)
  → Re-downsample with corrected frequency
  → Final fine time sync (±4 samples)
  → 32-point FFT per symbol → cs[tone][symbol] complex values, s8[tone][symbol] magnitudes
  → Hard sync check: nsync > 6 required
  → Multi-pass LLR extraction:
      bmeta (nsym=1): max-metric, single symbol
      bmetb (nsym=2): joint 2-symbol coherent sum
      bmetc (nsym=3): joint 3-symbol coherent sum
      bmetd (nsym=1): bit-by-bit normalised
  → NormalizeBmet (unit σ) + scale by 2.83
  → 4 LDPC decode attempts (first success wins)
```

### Key implementation notes:
- The 192k-point FFT uses Bluestein's algorithm (192000 is not a power of 2). It's the most expensive single operation but is computed only once per window.
- The 3200-point IFFT uses an unnormalized inverse (`complexIFFTUnnorm`) to match WSJT-X's `four2a` convention, with scaling by `1/√(NFFT1 × NFFT2)`.
- The per-symbol 32-point FFT uses Go 0-indexed arrays: bins 0–7 correspond to tones 0–7 (DC through 43.75 Hz). The Fortran code uses 1-indexed `csymb(1:8)`.
- LLR sign convention: our decoder expects positive LLR → bit more likely 0. WSJT-X uses the opposite convention. The LLR extraction computes `max(bit=0 group) − max(bit=1 group)` to match our convention.
- Time offset convention: our `TimeOff` is absolute seconds from audio start. WSJT-X's `xdt` convention defines xdt=0 as 0.5 seconds into the capture. The baseband pipeline uses `i0 = round(timeOff × fs2)` without the WSJT-X +0.5 offset.

## Current Bottlenecks (in order of impact)

### 1. LDPC decoder strength
Our `codec.DecodeMessage` uses belief-propagation (BP) only. WSJT-X's `decode174_91` uses BP + ordered statistics decoding (OSD), which significantly improves decode capability for weak signals. The multi-symbol LLR passes (nsym=2,3) produce correct bit-sign patterns (96–100% agreement with nsym=1) but our BP decoder can't converge on them.

### 2. Missing AP (a priori) decoding
WSJT-X uses additional decode passes with a priori information (known callsigns from the QSO state). These passes substitute high-confidence LLR values for known message bits, effectively reducing the LDPC problem from 174→~100 unknown bits. This is particularly effective for signals in the -20 to -24 dB range.

### 3. 192k-point Bluestein FFT performance
The long FFT is recomputed for each subtraction pass. At ~192k complex points through Bluestein (padded to 262144), each computation takes ~50-100ms. For 3 subtraction passes, this adds ~150-300ms per window. Optimization paths:
- Cache the Bluestein tables (already done)
- Use a mixed-radix FFT for N=192000 = 2⁷ × 3 × 5³ instead of Bluestein

## Next Steps (Priority Order)

### 1. Implement OSD (ordered statistics decoding)
Add OSD-0 or OSD-2 to `codec.Decode` as a fallback when BP fails. This is the single biggest improvement path — it would enable the multi-symbol LLR passes to actually decode additional signals.

**Reference:** WSJT-X `lib/bpdecode174_91.f90`, Reed & Chase "On decoding of Reed-Solomon codes" (the OSD concept).

### 2. Optimize the 192k-point FFT
Replace Bluestein for NFFT1=192000 with a mixed-radix FFT that exploits the factorisation 192000 = 2⁷ × 3 × 5³. This would reduce the FFT cost by ~2-3×.

### 3. Consider AP decoding for the logging app
When the Wails logging app has QSO context (mycall, dxcall), AP passes could be added. This would require passing callsign info into the decode pipeline.

### 4. Profile and optimise baseband pipeline
The pipeline currently computes the full long FFT + downsample + sync8d + DFT for EVERY candidate. In WSJT-X, the long FFT is computed once and the downsample is cheap (band extraction + IFFT). Consider caching the long FFT across candidates within a single pass.

## Existing DSP Code Reference

Key files in `internal/ft8/dsp/`:
- `dsp.go` — `ProcessWindow()` top-level pipeline (Goertzel)
- `multipass.go` — `ProcessWindowMultiPass()` (Goertzel + subtraction)
- `baseband.go` — `LongFFT()`, `DownsampleBaseband()` (frequency-domain downsampling)
- `baseband_demod.go` — `DemodulateBaseband()`, `Sync8d()`, `NormalizeBmet()`
- `baseband_pipeline.go` — `ProcessWindowBaseband()`, `ProcessWindowBasebandWithDiag()`
- `spectrogram.go` — `SpectrogramFT8()` (3840-pt FFT, log2 power)
- `candidates.go` — `FindCandidates()`, `RefineCandidateAudio()`
- `hires.go` — `FindCandidatesHiRes()`, `RefineCandidateAudioFast()`
- `demod.go` — `DemodulateAudio()` (Goertzel-based), `NormalizeLLR()`
- `symbols.go` — FT8 constants (SampleRate=12000, SamplesPerSymbol=1920, etc.)
- `fft.go` — `RealFFT()`, `RealFFTN()`, `fftDIT()`, `bluesteinDFT()`
- `goertzel.go` — `Goertzel()` single-tone power
- `window.go` — `HannCoefficients()`, `HannPeriodicCoefficients()`

Key files in `internal/ft8/codec/`:
- `codec.go` — `DecodeMessage()` (LDPC BP + CRC-14)

Key files in `internal/ft8/message/`:
- `message.go` — `Unpack()`, `Message.String()`
