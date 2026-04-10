# FT8 Decoder Integration Testing — Context Handoff

**Date:** 2026-04-10
**Status:** ft8test CLI complete (4 stages), decoder improvement work next

## What Was Built

A new CLI tool at `cmd/ft8test/` for stage-by-stage integration testing of the FT8 decode pipeline. Each stage is a separate Cobra subcommand that can be tested in isolation with WAV files.

### Files Created/Modified

**New files:**
- `cmd/ft8test/main.go` — entry point
- `cmd/ft8test/go.mod` — module with `replace ../../internal`
- `cmd/ft8test/cmd/root.go` — Cobra root + DI wiring (config, logging)
- `cmd/ft8test/cmd/devices.go` — list audio capture/playback devices
- `cmd/ft8test/cmd/capture.go` — Stage 1: audio capture + decimation → WAV (also contains `readWAV`, `saveWAV`, `reportAudioStats` helpers)
- `cmd/ft8test/cmd/spectrogram.go` — Stage 2: compute spectrogram, report diagnostics
- `cmd/ft8test/cmd/candidates.go` — Stage 3: Costas sync candidate detection
- `cmd/ft8test/cmd/decode.go` — Stage 4: full pipeline with `--diagnose` flag

**Modified files:**
- `go.work` — added `./cmd/ft8test`
- `Taskfile.yml` — added `ft8test` build task

**Test data:**
- `internal/ft8/dsp/testdata/ft8test_capture_20260410.wav` — known-good 15s capture, 13 WSJT-X decodes

### Build & Run

```bash
task ft8test                                          # build → build/bin/ft8test
./build/bin/ft8test devices                           # list audio devices
./build/bin/ft8test capture --device 1                # capture 15s → capture.wav
./build/bin/ft8test spectrogram --input capture.wav   # spectrogram diagnostics
./build/bin/ft8test candidates --input capture.wav    # sync candidate detection
./build/bin/ft8test decode --input capture.wav        # full decode
./build/bin/ft8test decode --input capture.wav --diagnose  # per-candidate diagnostics
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
- **Stage 2 (spectrogram):** 372 frames × 1921 bins — **exact match** with WSJT-X NHSYM=372
- **Stage 3 (candidates):** 102 candidates found, all 13 WSJT-X signals represented
- **Stage 4 (decode):** **1/13 decoded** (VE1WT K4GBI 73 at 1309.9 Hz)

### Diagnostic Findings (--diagnose output)

Key observations from per-candidate diagnostics:

| Signal (WSJT-X freq) | Refined Score | LLR mean | LDPC Result |
|---|---|---|---|
| ~1310 Hz (VE1WT) | 51.83 | +0.55 | ✓ **DECODED** |
| ~1692 Hz (PV8AJ) | 48.19 | +1.08 | ❌ LDPC fail |
| ~1903 Hz (SV2SIH) | 66.23 | +0.19 | ❌ LDPC fail |
| ~2099 Hz (RA1OHX) | 13.32 | -0.03 | ❌ LDPC fail |
| ~1861 Hz (SV2SIH) | 61.85 | -0.44 | ❌ LDPC fail |
| ~1814 Hz (RA6ABC) | 13.45 | -1.61 | ❌ LDPC fail |
| ~2328 Hz (KB7THX) | 2.20 | +0.45 | ❌ LDPC fail |

**All LLR arrays have var=24.00** (post-normalization target) and zero count=0.

**The bottleneck is the demodulation stage, not sync detection or refinement.** Candidates with strong sync scores (66+) still fail LDPC because the soft symbols (LLRs) aren't good enough.

## Root Cause: Demodulation Method

### Our approach (single pass, Goertzel):
```
audio → Goertzel per symbol (1920 samples, 1 symbol) → 8 tone powers → max-log LLR
```
- Equivalent to WSJT-X's simplest pass: `nsym=1, bmeta`
- Single decode attempt per candidate

### WSJT-X approach (ft8b.f90, multi-pass):
```
audio → ft8_downsample (mix to baseband, decimate 60×, 200 Hz complex) →
  sync8d (fine time/freq on complex baseband) →
  32-point DFT per symbol → 8 complex tone values →
  4 different LLR extraction methods:
    bmeta: nsym=1 (single symbol)
    bmetb: nsym=2 (joint 2-symbol)
    bmetc: nsym=3 (joint 3-symbol)
    bmetd: nsym=1 bit-by-bit normalized
  Each scaled by 2.83, then 4 separate LDPC decode passes
```

### Key differences:
1. **Complex baseband processing** — WSJT-X downconverts to baseband and works with complex samples at 200 Hz, not raw audio at 12 kHz
2. **32-point DFT** — at 200 Hz sample rate, 32 samples = 1 symbol period (1920/60=32). The DFT gives 8 complex tone bins directly
3. **Multi-symbol joint LLR** — nsym=2 and nsym=3 consider 2–3 adjacent symbols jointly, improving SNR for weak signals by ~3–5 dB
4. **4 decode passes** — each with different LLR computation; any pass succeeding counts
5. **AP (a priori) decoding** — additional passes using known callsigns (not relevant for our basic decoder)

## Next Steps (Priority Order)

### 1. Implement WSJT-X-style baseband demodulation
The biggest gain. Port `ft8_downsample` + 32-point DFT approach:
- Mix candidate freq to baseband (complex multiply)
- Decimate 60× (12000 → 200 Hz) using existing FIR or simple averaging
- 32-point FFT per symbol → 8 complex tone values
- Extract LLRs from tone magnitudes

**Reference files:**
- `wsjt-wsjtx/lib/ft8/ft8b.f90` lines 104–161 (downsample + DFT)
- `wsjt-wsjtx/lib/ft8/ft8_downsample.f90` (baseband mixing + decimation)

### 2. Add multi-pass LLR extraction
Implement nsym=1,2,3 joint symbol LLR computation and try each:
- `ft8b.f90` lines 182–239 (LLR extraction with 1/2/3 symbol joint decoding)
- `ft8b.f90` lines 265–269 (4 decode passes with different LLR sets)

### 3. Add normalizebmet
WSJT-X normalizes LLRs per `normalizebmet` then scales by 2.83:
- `ft8b.f90` lines 230–236

### 4. Improve refinement
WSJT-X refines using `sync8d` on complex baseband (lines 108–151), not Goertzel grid search.

## Existing DSP Code Reference

Key files in `internal/ft8/dsp/`:
- `dsp.go` — `ProcessWindow()` top-level pipeline
- `spectrogram.go` — `SpectrogramFT8()` (3840-pt FFT, log2 power)
- `candidates.go` — `FindCandidates()`, `RefineCandidateAudio()`
- `demod.go` — `DemodulateAudio()` (Goertzel-based), `NormalizeLLR()`
- `symbols.go` — FT8 constants (SampleRate=12000, SamplesPerSymbol=1920, etc.)
- `decimate.go` — 48kHz→12kHz decimator
- `goertzel.go` — `Goertzel()` single-tone power
- `fft.go` — `RealFFT()`, `RealFFTN()`
- `window.go` — `HannCoefficients()`, `HannPeriodicCoefficients()`

Key files in `internal/ft8/codec/`:
- `codec.go` or similar — `DecodeMessage()` (LDPC BP + CRC-14)

Key files in `internal/ft8/message/`:
- `message.go` — `Unpack()`, `Message.String()`

