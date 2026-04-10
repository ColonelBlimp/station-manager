# FT8 Decoder Integration Testing — Context Handoff

**Date:** 2026-04-10
**Updated:** 2026-04-10
**Status:** Baseband demodulation + OSD + sync8 + AP decoding (9/13 baseline, 10/13 with AP context)

## What Was Built

### Phase 1: ft8test CLI (complete)
A CLI tool at `cmd/ft8test/` for stage-by-stage integration testing of the FT8 decode pipeline. Each stage is a separate Cobra subcommand.

### Phase 2: WSJT-X-style baseband demodulation (complete)
Ported WSJT-X's `ft8_downsample.f90`, `sync8d.f90`, and the core of `ft8b.f90` to Go, improving decode rate from **1/13 to 6/13** messages (with OSD fallback).

### Phase 3: A priori (AP) decoding (complete)
Ported WSJT-X's AP decode passes from `ft8b.f90` and `decode174_91.f90` to Go. When the operator's callsign is provided, additional AP decode passes inject known message bits as high-confidence LLRs, enabling decoding of signals at -21 to -23 dB that fail standard BP+OSD. Improves decode rate from **9/13 to 10/13** with appropriate AP context.

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
./build/bin/ft8test decode --input capture.wav --baseband --mycall KB7THX  # AP decode (CQ + MyCall)
./build/bin/ft8test decode --input capture.wav --baseband --mycall KB7THX --dxcall WB9VGJ  # AP with both calls
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
| Baseband (BP only) | **4/13** | VE1WT K4GBI 73, <...> RA1OHX KP91, SV2SIH KI8JP -10, HZ1TT RU1AB R-10 |
| Baseband (BP+OSD) | **6/13** | + <...> RV6ASU KN94, A61CK W3DQS -12 |
| Baseband (BP+OSD+sync8) | **9/13** | + SV2SIH ES2AJ -16, CQ PV8AJ FJ92, A61CK UA1CEI KP50 |
| Baseband (BP+OSD+sync8+AP) | **10/13** | + KB7THX WB9VGJ RR73 (with mycall=KB7THX, dxcall=WB9VGJ) |

### Baseband decode details (BP+OSD+sync8):
```
  TIME (s)     SNR     FREQ  MESSAGE
    +0.000   -9.1   1860.5  SV2SIH ES2AJ -16
    +0.140   -9.9   1309.9  VE1WT K4GBI 73
    +0.030   -8.8   1902.9  SV2SIH KI8JP -10
    +0.090  -15.8   2098.6  <...> RA1OHX KP91
    +0.000  -10.2   1691.8  CQ PV8AJ FJ92
    +0.000  -16.0    948.2  A61CK UA1CEI KP50
    +0.180  -16.9    460.9  <...> RV6ASU KN94
    +0.050  -14.6    579.1  A61CK W3DQS -12
    +1.080  -23.7   2208.4  HZ1TT RU1AB R-10
```

The 3 new decodes (SV2SIH ES2AJ, CQ PV8AJ, A61CK UA1CEI) were previously **invisible** to the candidate detector — they didn't even appear as candidates. The sync8 algorithm finds them because:
- Ratio-metric sync scoring (syncPower / meanNonSyncPower) is more robust than the previous neighbor-difference metric
- Linear-power spectrogram (not log2) preserves the power contrast needed for weak signals
- The sync_bc mode (blocks 2+3 only) catches late-arriving signals that the old detector missed
- 40th-percentile baseline normalization adapts to the local noise floor
- Near-dupe suppression frees candidate budget for weaker signals

## Architecture: Baseband Demodulation Pipeline

### Processing flow (per candidate):
```
audio (180k samples, 12 kHz)
  → Sync8: linear-power spectrogram (no window, 1920 samples, 3840-pt FFT)
    → ratio-metric sync scoring (sync_abc + sync_bc)
    → 40th-percentile normalization, near-dupe suppression
    → candidate list with freq + time offset
  → RefineCandidateAudioFast: Goertzel coarse-fine grid search
  → LongFFT: 192000-point real FFT (mixed-radix, computed once per pass)
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
  → Signal subtraction → repeat for 3 passes
```

### Key implementation notes:
- Sync8 candidate detection uses a linear-power spectrogram (no Hann window, scale by 1/300, zero-padded to 3840) matching WSJT-X sync8.f90 exactly.
- The sync metric is a RATIO (syncPower / meanNonSyncPower), not a difference. This is more robust than the previous neighbor-comparison approach.
- Two sync modes: sync_abc (all 3 Costas blocks) and sync_bc (blocks 2+3 only, for late-arriving signals).
- Near-dupe suppression removes candidates within 4 Hz and 40 ms, preventing the budget waste seen with the old detector (where one strong signal consumed ~13 of 95 candidate slots).
- The 192k-point FFT uses mixed-radix Cooley-Tukey (192000 = 2⁹ × 3 × 5³ is 5-smooth). The `generalDFT` dispatcher auto-routes to the optimal algorithm. It's 1.29× faster than Bluestein with 63% less memory.
- The 3200-point IFFT uses an unnormalized inverse (`complexIFFTUnnorm`) to match WSJT-X's `four2a` convention, with scaling by `1/√(NFFT1 × NFFT2)`.
- The per-symbol 32-point FFT uses Go 0-indexed arrays: bins 0–7 correspond to tones 0–7 (DC through 43.75 Hz). The Fortran code uses 1-indexed `csymb(1:8)`.
- LLR sign convention: our decoder expects positive LLR → bit more likely 0. WSJT-X uses the opposite convention. The LLR extraction computes `max(bit=0 group) − max(bit=1 group)` to match our convention.
- Time offset convention: our `TimeOff` is absolute seconds from audio start. WSJT-X's `xdt` convention defines xdt=0 as 0.5 seconds into the capture. Sync8 converts via `TimeOff = (jpeak − 0.5) × tstep + 0.5`.
- The diagnostic variant (`ProcessWindowBasebandWithDiag`) now performs multi-pass signal subtraction, matching the production path.

## Current Bottlenecks (in order of impact)

### 1. ~~LDPC decoder strength~~ ✅ RESOLVED
OSD (Ordered Statistics Decoding) order-1 has been implemented as a fallback when BP fails to converge. `codec.DecodeMessage` now automatically chains BP → OSD.

### 2. ~~192k-point FFT performance~~ ✅ RESOLVED
Mixed-radix Cooley-Tukey FFT implemented for 5-smooth sizes. 1.29× faster, 63% less memory for 192k-point FFT.

### 3. ~~Candidate detection missing signals~~ ✅ RESOLVED
WSJT-X-faithful sync8 algorithm ported to Go, replacing the neighbor-comparison scoring. Improved decode rate from 6/13 to **9/13**. The 3 newly decoded signals (SV2SIH ES2AJ, CQ PV8AJ, A61CK UA1CEI) were previously invisible to the candidate detector.

**Files added:**
- `internal/ft8/dsp/sync8.go` — `Sync8FindCandidates()` with linear-power spectrogram, ratio-metric sync scoring, 40th-percentile normalization, dual sync modes (abc + bc), and near-dupe suppression.

**Files modified:**
- `internal/ft8/dsp/baseband_pipeline.go` — `ProcessWindowBaseband()` and `ProcessWindowBasebandWithDiag()` now use `Sync8FindCandidates` instead of `SpectrogramFT8HiRes` + `FindCandidatesHiRes`. Diagnostic variant now includes multi-pass signal subtraction.
- `internal/ft8/dsp/dsp.go` — added `estimateSNRFromScore()` for SNR estimation without a spectrogram noise floor.

### 4. Remaining 4/13 signals not decoded (3/13 with AP context)

Of the 4 remaining undecoded signals (without AP), 1 can now be decoded with appropriate AP context:
- **Decoded with AP**: KB7THX WB9VGJ RR73 (2328 Hz, -21 dB) — decoded at -23.6 dB with mycall=KB7THX, dxcall=WB9VGJ via AP type 2 (MyCall + i3).
- **2 signals found as candidates but LDPC fails**: <...> LU3DXU GF05 (1273 Hz, -15 dB), <...> RA6ABC KN96 (1814 Hz, -17 dB). These have hashed first callsigns; AP types 1/2 don't match because the first field is a 22-bit hash, not CQ or a known callsign. Type 3+ AP would require knowing the hash source callsign.
- **1 signal not found as candidate**: ES2AJ UA3LAR KO75 (835 Hz, -23 dB) — too weak for sync8 detection.


## Next Steps (Priority Order)

### 1. ~~AP (a priori) decoding for weak signals~~ ✅ IMPLEMENTED
AP decoding has been implemented matching WSJT-X's ft8b.f90 approach. The system supports 6 AP types (CQ, MyCall, MyCall+DxCall, MyCall+DxCall+RRR/73/RR73) across all 6 QSO progress states. When the operator's callsign matches a signal, AP passes inject high-confidence LLR values for known message bits, reducing the LDPC problem dimension. This decoded KB7THX WB9VGJ RR73 at -23.6 dB (previously undecodable at -21 dB SNR).

**Files added:**
- `internal/ft8/dsp/ap.go` — `APContext`, `NewAPContext()`, AP type constants, pass tables, known-fragment bipolar arrays (CQ, RRR, 73, RR73), and `applyAPPass()` for LLR injection.

**Files modified:**
- `internal/ft8/codec/decoder.go` — Added `DecodeAP()` with apmask support (holds AP bits at channel value during BP iterations) and `bpCollectZsave()` for OSD fallback with cumulative zsum snapshots. Refactored `Decode()` to share `decodeInternal()`.
- `internal/ft8/codec/osd.go` — Added `DecodeOSDAP()` that skips flipping AP-masked bits during order-1 search. Refactored `DecodeOSD()` to share `decodeOSDInternal()`.
- `internal/ft8/codec/codec.go` — Added `DecodeMessageAP()` chaining BP→OSD with apmask and zsave, plus shared `verifyAndExtract()`.
- `internal/ft8/dsp/baseband_pipeline.go` — `ProcessWindowBaseband()` and `ProcessWindowBasebandWithDiag()` now accept `*APContext`. Added `tryAPPasses()` helper that iterates AP types per QSO progress state.
- `cmd/ft8test/cmd/decode.go` — Added `--mycall` and `--dxcall` flags for AP testing.

**Key implementation notes:**
- LLR sign convention: Our decoder uses positive=likely 0 (opposite of WSJT-X). AP bit injection negates the bipolar values: `llrz[i] = -apmag * bipolar[i]`.
- The `apmask` parameter flows through BP → OSD: during BP, masked bits ignore extrinsic messages; during OSD, masked bits are excluded from the flip search.
- `bpCollectZsave` accumulates `zsum` across iterations and saves snapshots at iterations 1–3 for OSD fallback, matching WSJT-X's `maxosd=2` approach.
- AP type 1 (CQ) doesn't require mycall — it injects the fixed CQ bit pattern for any candidate.
- QSO progress state 0 (default) enables AP types 1 (CQ) and 2 (MyCall) only. Deeper AP types (3–6) activate at higher QSO progress states when both callsigns are known.

### 2. Wire AP context into Wails logging facade
The `APContext` is currently only accessible via the ft8test CLI (`--mycall`/`--dxcall`). To benefit real-time operation, the Wails logging app facade should construct an `APContext` from the operator's configured callsign and pass it to `ProcessWindowBaseband()`. The QSO progress state should advance as the logging app tracks QSO exchanges.

### 3. Explore multi-symbol LLR passes with OSD
Currently only the nsym=1 LLR pass successfully decodes in most cases. The nsym=2,3 and bit-normalised passes produce LLRs with 96–100% sign agreement but fail both BP and OSD. Investigate whether the LLR magnitude scaling or normalisation differs from WSJT-X.

### 3. SNR calibration
The current SNR estimation uses a placeholder calibration (`estimateSNRFromScore`). WSJT-X computes SNR from the per-symbol s8 array after decoding. A proper port would improve SNR accuracy for logging and display.

## Existing DSP Code Reference

Key files in `internal/ft8/dsp/`:
- `dsp.go` — `ProcessWindow()` top-level pipeline (Goertzel)
- `multipass.go` — `ProcessWindowMultiPass()` (Goertzel + subtraction)
- `sync8.go` — `Sync8FindCandidates()` (WSJT-X-faithful candidate detection)
- `baseband.go` — `LongFFT()`, `DownsampleBaseband()` (frequency-domain downsampling)
- `baseband_demod.go` — `DemodulateBaseband()`, `Sync8d()`, `NormalizeBmet()`
- `baseband_pipeline.go` — `ProcessWindowBaseband()`, `ProcessWindowBasebandWithDiag()`
- `ap.go` — `APContext`, `NewAPContext()`, `applyAPPass()`, AP type constants/tables
- `spectrogram.go` — `SpectrogramFT8()` (3840-pt FFT, log2 power)
- `candidates.go` — `FindCandidates()`, `RefineCandidateAudio()`
- `hires.go` — `FindCandidatesHiRes()`, `RefineCandidateAudioFast()`
- `demod.go` — `DemodulateAudio()` (Goertzel-based), `NormalizeLLR()`
- `symbols.go` — FT8 constants (SampleRate=12000, SamplesPerSymbol=1920, etc.)
- `fft.go` — `RealFFT()`, `RealFFTN()`, `fftDIT()`, `bluesteinDFT()`
- `fft_mixedradix.go` — `mixedRadixDFT()`, `generalDFT()`, radix-2/3/5 DIT butterflies
- `goertzel.go` — `Goertzel()` single-tone power
- `window.go` — `HannCoefficients()`, `HannPeriodicCoefficients()`
