# FT8 Decoder Integration Testing — Context Handoff

**Date:** 2026-04-10
**Updated:** 2026-04-10
**Status:** Baseband demodulation + sum-product BP + OSD-2(zsave) + sync8 + AP decoding + Type 4 message support + demodulator fixes (10/13 capture1, 8/15 capture2)

## What Was Built

### Phase 1: ft8test CLI (complete)
A CLI tool at `cmd/ft8test/` for stage-by-stage integration testing of the FT8 decode pipeline. Each stage is a separate Cobra subcommand.

### Phase 2: WSJT-X-style baseband demodulation (complete)
Ported WSJT-X's `ft8_downsample.f90`, `sync8d.f90`, and the core of `ft8b.f90` to Go, improving decode rate from **1/13 to 6/13** messages (with OSD fallback).

### Phase 3: A priori (AP) decoding (complete)
Ported WSJT-X's AP decode passes from `ft8b.f90` and `decode174_91.f90` to Go. When the operator's callsign is provided, additional AP decode passes inject known message bits as high-confidence LLRs, enabling decoding of signals at -21 to -23 dB that fail standard BP+OSD. Improves decode rate from **9/13 to 10/13** with appropriate AP context.

### Phase 4: Type 4 non-standard callsign messages (complete)
Implemented unpacking for i3=4 messages carrying non-standard callsigns (containing '/', up to 11 characters from a 38-symbol alphabet). The 58-bit base-38 encoded callsign is decoded; the 12-bit hashed companion callsign is shown as `<...>` (no hash lookup table). This enabled decoding messages like `VK/ZL4XZ <...> RR73` that previously failed with "Type 4 not yet supported".

### Phase 5: Sum-product BP + zsave OSD (complete)
Replaced the normalised min-sum BP decoder with sum-product BP (tanh/atanh) matching WSJT-X `decode174_91.f90`. OSD fallback now receives cumulative BP posterior LLR snapshots (zsave) instead of raw channel LLRs, plus a final raw-LLR OSD fallback. Key result: KB7THX WB9VGJ RR73 now decodes **without AP context** (previously required `--mycall`), demonstrating improved decoder sensitivity for weak signals.

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
- `internal/ft8/dsp/testdata/ft8test_capture_20260410.wav` — known-good 15s capture, 13 WSJT-X decodes (capture 1)
- `internal/ft8/dsp/testdata/ft8test_capture2_20260410.wav` — known-good 15s capture, 15 WSJT-X decodes (capture 2)

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
| Baseband (BP+OSD2+sync8, no Goertzel refine) | **9/13 + 1 false** | KB7THX decodes without AP; N3AQ OK4FX JO70 false |

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

## Test Results (capture2.wav)

### WSJT-X decoded 15 messages from the same WAV:
```
 -8  816 Hz  HA5LB 5B4AMX RR73
 -4 1776 Hz  CQ ZS4AW KG31
-13 2100 Hz  CQ SV0TPN KM28
-10  745 Hz  CQ Z62NS KN02
 -1  332 Hz  VK/ZL4XZ <...> RR73
-13  862 Hz  VK3ZSJ YO8RQP KN37
-14 1464 Hz  R1QD KB2ELA -12
-17  553 Hz  UY7VV KE6SU DM14
 -5  319 Hz  TL8GD UT2VX KN69
-18 1840 Hz  RU4LM 4X5JK R-14
-14 1768 Hz  JT1CO IZ7DIO 73
-18 1502 Hz  VK3ZSJ US7KC KO21
-15 1410 Hz  JR3UIC SP7IIT RR73
-22 1096 Hz  JT1CO YO3HST KN24
-12  451 Hz  CQ TN8GD JI75
```

### Our pipeline results (BP+OSD+sync8):

| Pipeline | Decoded | Correct | False |
|---|---|---|---|
| Goertzel (original) | **7** | **5/15** | **2** |
| Baseband (BP+OSD+sync8) | **10** | **7/15** | **3** |
| Baseband (BP+OSD2+sync8, no Goertzel refine) | **11** | **8/15** | **3** |

### Baseband decode details:
```
  TIME (s)     SNR     FREQ  MESSAGE
    +2.960  -15.9    815.6  HA5LB 5B4AMX RR73         ✓ WSJT-X match
    +2.350   -3.8   1776.0  CQ ZS4AW KG31             ✓ WSJT-X match
    +2.310   -6.1    745.6  CQ Z62NS KN02             ✓ WSJT-X match
    +2.360   -7.7    862.5  VK3ZSJ YO8RQP KN37        ✓ WSJT-X match
    +2.360  -20.3   2100.0  CQ SV0TPN KM28            ✓ WSJT-X match
    +2.310  -12.8   1463.8  R1QD KB2ELA -12            ✓ WSJT-X match
    +2.320   -7.2    331.8  VK/ZL4XZ <...> RR73        ✓ WSJT-X match (Type 4)
    +2.270  -20.3   2694.5  VK2USH UA6EED LN14         ✗ not in WSJT-X (likely false)
    +2.290  -25.5   2751.0  CQ 5W1SA AH46              ✗ not in WSJT-X (likely false)
    +2.360  -32.6   2594.2  UA4CCH VK2VT RR73          ✗ not in WSJT-X (likely false)
```

### Analysis — capture 2:
- **8 correct matches** out of 15 WSJT-X decodes (53%), including VK/ZL4XZ (Type 4 non-standard callsign) and JR3UIC SP7IIT RR73 (newly decoded by OSD order-2).
- **3 likely false decodes** at high frequencies (2594–2751 Hz) with extreme SNRs (-20 to -33 dB). These pass CRC-14 by chance — expected false alarm rate with 240 candidates × 4 LLR passes + AP CQ pass + OSD order-2 inflate false alarm probability.
- **7 WSJT-X signals missed** — analysis by failure mode:
  - UY7VV KE6SU DM14 (553 Hz, -17 dB) — nsync=6 (≤ 6 threshold, skipped)
  - TL8GD UT2VX KN69 (319 Hz, -5 dB) — nsync=6 (≤ 6, skipped; NP2 bound zeroes 3rd Costas block at this time offset)
  - RU4LM 4X5JK R-14 (1840 Hz, -18 dB) — nsync=12(2+7+3), rawσ=0.000447, all passes fail (marginal LLR quality)
  - JT1CO IZ7DIO 73 (1768 Hz, -14 dB) — nsync=4 (skipped)
  - VK3ZSJ US7KC KO21 (1502 Hz, -18 dB) — not detected as candidate near this frequency
  - JT1CO YO3HST KN24 (1096 Hz, -22 dB) — no candidate near this frequency
  - CQ TN8GD JI75 (451 Hz, -12 dB) — nsync=8(0+6+2), rawσ=0.000552, all passes fail (marginal LLR quality)

## Architecture: Baseband Demodulation Pipeline

### Processing flow (per candidate):
```
audio (180k samples, 12 kHz)
  → Sync8: linear-power spectrogram (no window, 1920 samples, 3840-pt FFT)
    → ratio-metric sync scoring (sync_abc + sync_bc)
    → 40th-percentile normalization, near-dupe suppression
    → candidate list with freq + time offset
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

### 1b. ~~Sum-product BP + zsave OSD~~ ✅ RESOLVED
Replaced normalised min-sum BP (sign × β × min) with sum-product BP (tanh/atanh) matching WSJT-X `decode174_91.f90`. OSD fallback now receives cumulative BP posterior LLR snapshots (zsave) from iterations 1–3, plus raw-LLR OSD as a final fallback. This enabled KB7THX WB9VGJ RR73 to decode **without AP context** (the AP CQ type 1 pass succeeds with the stronger BP+zsave chain).

**Files modified:**
- `internal/ft8/codec/decoder.go` — Replaced `decodeInternal` + `bpCollectZsave` with unified `bpDecode` using sum-product check→variable update. Added `platanh()` (piecewise-linear atanh matching WSJT-X). Added `DecodeWithZsave()`. Added early stopping criterion (WSJT-X lines 91–104).
- `internal/ft8/codec/decoder_debug.go` — Updated `DecodeDebug` to use sum-product c→v update.
- `internal/ft8/codec/codec.go` — `DecodeMessage()` now calls `DecodeWithZsave()`, tries OSD with zsave[0] and zsave[1], then raw-LLR OSD as final fallback.
- `internal/ft8/codec/osd_test.go` — Updated `TestDecodeMessageOSDFallback` for zsave-aware path.

**Key implementation notes:**
- LLR sign convention required adapting the WSJT-X formula: WSJT-X uses `tanh(-toc/2)` and `atanh(-Tmn)` for positive=likely 1 convention. Our positive=likely 0 convention uses `tanh(toc/2)` and `atanh(prod)` — no negations.
- `platanh` is a piecewise-linear approximation of atanh, clamped at ±7.0, avoiding infinity for inputs near ±1. Matches WSJT-X `lib/platanh.f90` exactly.
- The unified `bpDecode` function collects zsave during its single BP pass (no double BP run), then does convergence checking and early stopping in the same loop.

### 1c. ~~Demodulator fixes + OSD order-2~~ ✅ RESOLVED
Three discrepancies between the Go baseband demodulator and WSJT-X `ft8b.f90` were identified and fixed, plus OSD was upgraded from order-1 to order-2. Combined result: capture 1 went from 9/13 to **10/13** (9 correct + 1 false), capture 2 from 7/15 to **8/15** (8 correct + 3 false).

**Bug fixes:**
1. **DC bin inclusion** (`baseband.go`): `DownsampleBaseband` clamped `ib` to 0 (includes DC), but WSJT-X `ft8_downsample.f90` line 36 clamps to 1 (skips DC). DC energy contaminated the baseband signal. Fixed: `if ib < 1 { ib = 1 }`.
2. **NP2 bounds** (`baseband_demod.go`): `Sync8d` and the per-symbol 32-point DFT used `len(cd0)=3200` as upper bound, but WSJT-X uses `NP2=2812` (ft8b.f90 line 10, sync8d.f90 line 5). Samples 2812–3199 are zero-pad circular wrap-around artifacts. Added `const BasebandNP2 = 2812` and applied to all bounds checks.
3. **Goertzel refinement removed** (`baseband_pipeline.go`): WSJT-X passes sync8 candidates **directly** to `ft8b` with no intermediate Goertzel refinement. The Go pipeline had inserted `RefineCandidateAudioFast` between sync8 and `DemodulateBaseband`, which could push frequency/time estimates outside Sync8d's ±2.5 Hz / ±10-sample recovery window. Removed: sync8 candidates now go directly to `DemodulateBaseband`, matching WSJT-X.

**OSD order-2:**
- `internal/ft8/codec/osd.go` — Added order-2 pair-flip search: tries all K×(K-1)/2 = 4,095 two-bit flip patterns in the information positions, matching WSJT-X `osd174_91.f90` `norder=2`. This is 45× more candidate codewords than order-1.
- `internal/ft8/codec/codec.go` — `DecodeMessage()` and `DecodeMessageAP()` now call OSD with `ndeep=2` (was 1).

**Diagnostic improvements:**
- `BasebandDemodResult` now includes per-Costas-block sync counts (`Is1`, `Is2`, `Is3`), valid symbol count (`ValidSyms`), and raw LLR sigma before normalization (`RawSigma`).
- `BasebandDiag` in the diagnostic pipeline exposes these fields.
- ft8test CLI diagnostic output shows `nsync=N(a+b+c)`, `ibest`, `vsym`, and `rawσ` for each candidate.

**Key findings from diagnostics:**
- Successfully decoded signals have rawσ > 0.0008; failing signals have rawσ < 0.0008. The raw LLR quality (not the normalization or decoder) is the limiting factor.
- JR3UIC SP7IIT RR73 (1410 Hz, rawσ=0.001236) was on the boundary — OSD order-2 pushed it over the decode threshold.
- RU4LM 4X5JK R-14 (1840 Hz, rawσ=0.000447) remains undecoded — too marginal even for OSD order-2.

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

### 2. ~~Type 4 non-standard callsign messages~~ ✅ IMPLEMENTED
Implemented unpacking for i3=4 messages carrying non-standard callsigns (containing '/', up to 11 characters from a 38-symbol alphabet: space, 0-9, A-Z, /). The 58-bit base-38 encoded callsign is decoded; the 12-bit hashed companion callsign is shown as `<...>` (no hash lookup table).

**Files added:**
- `internal/ft8/message/type4.go` — `unpackType4()`, `decodeCallsign58()`, charset38 constant, Type 4 bit offsets.
- `internal/ft8/message/type4_test.go` — roundtrip and unpack tests for Type 4 messages.

**Files modified:**
- `internal/ft8/message/message.go` — `Unpack()` now routes i3=4 to `unpackType4()`; `String()` handles `TypeNonStandard` the same as `TypeStandard`.
- `internal/ft8/message/doc.go` — updated supported types documentation.
- `internal/ft8/message/type1_test.go` — updated old `TestUnpack_Type4Unsupported` → `TestUnpack_Type4Supported`; updated `TestMessageString_Unsupported` → new `TestMessageString_NonStandard`/`TestMessageString_NonStandardNoGrid`.

### 3. Wire AP context into Wails logging facade
The `APContext` is currently only accessible via the ft8test CLI (`--mycall`/`--dxcall`). To benefit real-time operation, the Wails logging app facade should construct an `APContext` from the operator's configured callsign and pass it to `ProcessWindowBaseband()`. The QSO progress state should advance as the logging app tracks QSO exchanges.

### 4. Reduce false decode rate
Capture 2 produced 3 likely false decodes (CRC-14 collisions). Potential mitigations:
- Post-decode plausibility checks: reject decoded callsigns that fail basic format validation.
- SNR-based filtering: reject decodes with implausible SNR (e.g. < -28 dB).
- Reduce decode attempts per candidate: the current 4 LLR passes × 3 subtraction passes × (zsave + raw LLR OSD + AP CQ) inflate false alarm probability.
- Consider OSD order-0 only for AP CQ pass (lower false rate, similar sensitivity).

### 5. Investigate LLR extraction quality (highest-impact remaining bottleneck)
Both captures have 5+ signals detected as candidates with good nsync (11–16) but failing ALL 4 LLR extraction passes AND all OSD fallback attempts (zsave + raw). This includes TL8GD UT2VX KN69 at -5 dB with nsync=11 — a strong signal that should decode trivially. The LDPC decoder is no longer the bottleneck; the LLR quality from the demodulator is the limiting factor.

Investigation approach:
- Export raw LLR arrays for the failing signals and compare against WSJT-X's ft8b.f90 output for the same candidates
- Check if the fine-sync Δf correction from Sync8d is accurate — a sub-bin frequency error would contaminate all LLR passes
- Verify that the 32-point per-symbol DFT bin assignment (Go 0-indexed vs Fortran 1-indexed) is correct for all tone mappings
- Check the NormalizeBmet scaling: the 2.83 scale factor was originally tuned for sum-product BP; verify it's still appropriate
- Compare the s8 (magnitude-squared) arrays against WSJT-X to identify where demodulation diverges
- Verify that the 32-point per-symbol DFT bin assignment (Go 0-indexed vs Fortran 1-indexed) is correct for all tone mappings
- Check the NormalizeBmet scaling: the 2.83 scale factor was originally tuned for sum-product BP; verify it's still appropriate
- Compare the s8 (magnitude-squared) arrays against WSJT-X to identify where demodulation diverges

### 6. SNR calibration
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
