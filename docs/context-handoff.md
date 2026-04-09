# Context Handoff: FT8 Multi-Pass Pipeline — 0-Decode Regression

## Date
2026-04-09

## What Was Changed ("Close FT8 Decode-Rate Gap" Implementation)

Three new techniques were implemented to close the decode-rate gap with WSJT-X:

1. **Frequency-oversampled spectrogram** — `SpectrogramFT8HiRes` in `spectrogram.go` uses `analysisLen = SamplesPerSymbol × freqOSR` (3840 for freqOSR=2), producing 2049 bins at ~2.93 Hz spacing instead of the standard 1025 bins at ~5.86 Hz. This gives genuine sub-bin frequency resolution.

2. **Neighbor-comparison sync scoring** — `FindCandidatesHiRes` + `syncScoreNeighbor` in `hires.go` replaces the mean-based `syncScoreSteps` with ft8_lib's local-contrast method: each sync tone's power is compared against its immediate frequency and time neighbors rather than the global mean.

3. **Iterative signal subtraction** — `ProcessWindowMultiPass` in `multipass.go` runs up to 3 detect→decode→subtract passes. After each pass, decoded signals are removed from the audio buffer and detection re-runs on the residual.

Additionally, **coarse-fine refinement** (`RefineCandidateAudioFast` in `hires.go`) replaces the flat 33×49 grid of `RefineCandidateAudio` with a 96+25 point two-pass search, reducing Goertzel evaluations ~13× per candidate.

The production pipeline was switched: `service.go` line 574 now calls `dsp.ProcessWindowMultiPass` instead of `dsp.ProcessWindow`.

Default config values were updated: `DefaultMaxCandidates = 120` and `DefaultMaxIterations = 40` (in `multipass.go`), applied in `service.Initialize()` when config values are 0.

## Files Created

| File | Description |
|---|---|
| `internal/ft8/dsp/multipass.go` | `ProcessWindowMultiPass`, `subtractSignal`, `synthesizeSimple` — iterative pipeline with signal subtraction |
| `internal/ft8/dsp/hires.go` | `FindCandidatesHiRes`, `syncScoreNeighbor`, `RefineCandidateAudioFast` — hi-res candidate detection + coarse-fine refinement |

## Files Modified

| File | Changes |
|---|---|
| `internal/ft8/dsp/doc.go` | Updated package doc to describe both pipeline variants and future optimization opportunities |
| `internal/ft8/dsp/spectrogram.go` | Added `SpectrogramFT8HiRes(samples, freqOSR)` — frequency-oversampled spectrogram builder |
| `internal/ft8/dsp/candidates.go` | Added `FreqSub` field to `Candidate` struct |
| `internal/ft8/dsp/goertzel.go` | Added cached-coefficient variants: `GoertzelCoeff`, `GoertzelWithCoeff`, `GoertzelTonesCoeffs`, `GoertzelTonesWithCoeffs` |
| `internal/ft8/service/service.go` | Line 574: switched from `dsp.ProcessWindow` to `dsp.ProcessWindowMultiPass`; updated default config to use `dsp.DefaultMaxCandidates` (120) and `dsp.DefaultMaxIterations` (40) |
| `internal/types/ft8.go` | Updated `MaxCandidates` validate tag from `max=200` to `max=250` |
| `cmd/ft8/cmd/diag.go` | Updated hardcoded `maxCandidates` from 50→120 and `maxIter` from 25→40 |

## Files NOT Modified (retained for backward compatibility)

| File | Status |
|---|---|
| `internal/ft8/dsp/dsp.go` | `ProcessWindow` retained as-is (unit tests still use it) |
| `internal/ft8/dsp/dsp_test.go` | All existing tests still target `ProcessWindow` — **no tests for `ProcessWindowMultiPass`** |
| `internal/ft8/dsp/dsp_wav_test.go` | WAV regression tests still use `ProcessWindow` — **not testing the new pipeline** |

## The Bug: 0 Decodes from Live Audio

### Symptom
Running `./ft8 --device=1 --windows=4` (~60 seconds of live 10m FT8 audio) produces **0 decoded messages** across 4 windows. The log output shows `max_candidates=0 max_iterations=0`, which means the config values are zero in the config file. The service's `Initialize()` should apply defaults (MaxCandidates=120, MaxIterations=40), so this should work — but the pipeline decoded nothing.

**Previously** (before the multi-pass changes), `ProcessWindow` was decoding 7+ messages per window from the same hardware setup on 28.074 MHz.

### Root Cause Analysis — Top Suspects

#### 1. `syncScoreNeighbor` threshold mismatch (MOST LIKELY)
The `FindCandidatesHiRes` function uses `threshold = float32(minSyncScoreLog2)` = **1.5**. But `syncScoreNeighbor` computes scores **differently** than `syncScoreSteps`:

- `syncScoreSteps` returns `meanSyncPower − meanTotalPower` (absolute excess, typically 1.5–5.0 for real signals in log2 domain)
- `syncScoreNeighbor` returns `sum(syncVal − neighborVal) / numAvg` — a **local contrast** metric. This is a different scale; the threshold of 1.5 may be too high or too low for this scoring method.

**If the neighbor-scoring produces much smaller values than 1.5 for real signals, all candidates will be filtered out → 0 decodes.**

#### 2. Hi-res spectrogram frame count
`SpectrogramFT8HiRes` with `freqOSR=2` uses `analysisLen = 3840` vs `1920` for standard:
- Standard: `(180000 − 1920) / 960 + 1 = 186 frames`
- Hi-res: `(180000 − 3840) / 960 + 1 = 184 frames`

This is only 2 fewer frames — unlikely to cause 0 decodes. Both are above the minimum 157 frames.

#### 3. `binsPerTone` rounding in `syncScoreNeighbor`
With a 4096-point FFT at 12 kHz: `binsPerTone = 6.25 / 2.929688 ≈ 2.133`. The `int(math.Round(...))` for tone-to-bin mapping should be correct, but verify that `CostasSync[k]` values map to the right bins.

#### 4. Log output is misleading
The `service.Initialize()` log on lines 288-295 logs the raw `cfg` values (before defaults are applied), not `s.ft8Config` (after defaults). This makes it appear that `MaxCandidates=0` and `MaxIterations=0`, which would cause `ProcessWindowMultiPass` to return `nil` immediately (line 61). **Verify that the actual values passed to `ProcessWindowMultiPass` have defaults applied — add a debug log if needed.**

## Debugging Steps for Next Context

### Step 1: Verify `ProcessWindowMultiPass` works on WAV files
Add a WAV regression test for `ProcessWindowMultiPass` in `dsp_wav_test.go`:
```go
msgs := ProcessWindowMultiPass(wav.Samples, 120, 40)
```
Compare decode count against `ProcessWindow`. If `ProcessWindowMultiPass` returns 0 on WAV files that `ProcessWindow` decodes 14+, the bug is in the multi-pass pipeline itself, not in the live audio path.

### Step 2: Instrument `syncScoreNeighbor` threshold
Temporarily log the max score returned by `FindCandidatesHiRes` before the threshold filter. If all scores are < 1.5 (the current threshold), the threshold needs adjustment.

### Step 3: Compare candidate counts
Run both pipelines on the same WAV data:
```go
sgStd := SpectrogramFT8(samples)
candsStd := FindCandidates(sgStd, 120, 2)

sgHiRes := SpectrogramFT8HiRes(samples, FreqOSR)
candsHiRes := FindCandidatesHiRes(sgHiRes, 120, 2)
```
Compare: How many candidates does each produce? If standard finds 50+ and hi-res finds 0, the scoring/threshold is the issue.

### Step 4: Test `ProcessWindowMultiPass` with `freqOSR=1` fallback
Temporarily change `SpectrogramFT8HiRes(audio, FreqOSR)` to `SpectrogramFT8HiRes(audio, 1)` in `ProcessWindowMultiPass`. When `freqOSR=1`, `SpectrogramFT8HiRes` delegates to `SpectrogramFT8` (standard resolution). If this produces decodes, the bug is in the hi-res spectrogram or hi-res candidate detection.

### Step 5: Validate `syncScoreNeighbor` scoring correctness
Write a unit test with a synthesized FT8 signal:
- Encode a known message → synthesize audio → build hi-res spectrogram → call `syncScoreNeighbor` at the known (time, freq) → verify the score is positive and above threshold.

### Step 6: Check if diag tool confirms audio is OK
Run `./ft8 --device=1 --diag` — this uses the **standard** pipeline (`ProcessWindow` via `runDSPPipeline`). If diag decodes messages but the live pipeline doesn't, the audio is fine and the bug is confirmed to be in `ProcessWindowMultiPass`.

## Implementation Concerns

### 1. Score scale mismatch (CRITICAL)
`syncScoreNeighbor` returns a **different metric** than `syncScoreSteps`. The threshold `minSyncScoreLog2 = 1.5` was calibrated for the mean-subtraction method. The neighbor method likely needs its own threshold, possibly much lower (e.g., 0.3–0.5) or possibly higher. This is the most likely cause of 0 decodes.

### 2. No tests for the new code
Neither `ProcessWindowMultiPass`, `FindCandidatesHiRes`, `syncScoreNeighbor`, `RefineCandidateAudioFast`, `subtractSignal`, nor `synthesizeSimple` have any unit tests. The WAV regression tests still exercise only `ProcessWindow`.

### 3. Hi-res spectrogram analysis window overlap
The hi-res spectrogram uses `analysisLen = 3840` (2 symbol periods) with `step = 960` (half-symbol). This means each frame overlaps with 3 preceding frames (75% overlap), vs 50% for standard. The `Log2PowerSpectrum` values will have different magnitudes and dynamics because the longer window captures energy from adjacent symbols. This affects how `syncScoreNeighbor` interprets the data.

### 4. `subtractSignal` phase mismatch
`synthesizeSimple` uses abrupt tone transitions (no GFSK smoothing), which will produce phase discontinuities at symbol boundaries that don't match the actual signal. The least-squares amplitude scaling mitigates this, but cancellation may only achieve 3–6 dB rather than 6–12 dB. This is acceptable for passes 2–3 but doesn't explain the pass-1 failure.

## Quick Revert Path

If debugging stalls, the fastest revert is to change line 574 of `service.go` from:
```go
decoded := dsp.ProcessWindowMultiPass(samples, s.ft8Config.MaxCandidates, s.ft8Config.MaxIterations)
```
back to:
```go
decoded := dsp.ProcessWindow(samples, s.ft8Config.MaxCandidates, s.ft8Config.MaxIterations)
```
This immediately restores live decoding while the multi-pass pipeline is debugged offline with WAV files.

## Key Files Reference

| File | Purpose |
|---|---|
| `internal/ft8/dsp/multipass.go` | `ProcessWindowMultiPass` — the new production pipeline |
| `internal/ft8/dsp/hires.go` | `FindCandidatesHiRes`, `syncScoreNeighbor`, `RefineCandidateAudioFast` |
| `internal/ft8/dsp/dsp.go` | `ProcessWindow` — the original working pipeline |
| `internal/ft8/dsp/spectrogram.go` | `SpectrogramFT8` and `SpectrogramFT8HiRes` |
| `internal/ft8/dsp/candidates.go` | `FindCandidates`, `syncScoreSteps`, `RefineCandidateAudio`, `refineSyncScore`, `syncScoreAudio` |
| `internal/ft8/dsp/goertzel.go` | Goertzel functions (original + cached-coeff variants) |
| `internal/ft8/dsp/demod.go` | `DemodulateAudio`, `NormalizeLLR` |
| `internal/ft8/dsp/symbols.go` | All FT8 constants (`SamplesPerSymbol=1920`, `SampleRate=12000`, etc.) |
| `internal/ft8/dsp/dsp_test.go` | Pipeline unit tests (all for `ProcessWindow`, none for `ProcessWindowMultiPass`) |
| `internal/ft8/dsp/dsp_wav_test.go` | WAV regression tests (all for `ProcessWindow`) |
| `internal/ft8/service/service.go` | FT8 service wiring — `processWindow` calls `dsp.ProcessWindowMultiPass` (line 574) |
| `internal/types/ft8.go` | `FT8Config` struct |
| `cmd/ft8/cmd/root.go` | CLI entry point |
| `cmd/ft8/cmd/diag.go` | Diagnostic tool (still uses `ProcessWindow`) |
| `AGENTS.md` | Project conventions |
