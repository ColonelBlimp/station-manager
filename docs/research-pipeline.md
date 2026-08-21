# Research-tree FT8 decode pipeline

Reference document describing the end-to-end pipeline implemented under `research/` for FT8 decoding, captured 2026-05-26 at the close of Session 94.

This document is the high-level orientation for future sessions that need to understand WHERE in the pipeline a decode failure happens or WHICH stage to attack for a parity improvement. Detailed per-stage memory lives in:

- [[project_research_candidate_finder]] — stage 1
- [[project_research_decoder]] — stages 2-4
- [[project_research_unpack]] — stage 5
- [[project_research_synth]] — signal synthesis (used by the Session 94 cancellation outer loop)

---

## Pipeline diagram

```
   WAV (180,000 float32 samples, 12 kHz mono, 15s slot)
                       │
                       ▼
  ┌────────────────────────────────────┐
  │ Stage 1: CANDIDATE DETECTION       │ research/candidates/
  │  - Spectrogram (FFT)               │ Find(samples) → []Candidate
  │  - Stage-1: matched filter sweep   │
  │  - Stage-2: Costas-anchor verifier │
  │  - NMS + refine → top 100/slot     │
  └─────────────┬──────────────────────┘
                │ For each candidate (freq, dt):
                ▼
  ┌────────────────────────────────────┐
  │ Stage 2: DEMODULATION              │ research/demod/
  │  - Per-symbol 8-tone Goertzel      │ Demod(samples, freq, dt)
  │  - 58 data symbols × 8 tones       │   → [58][8]energies
  │  - Energy matrix                   │
  └─────────────┬──────────────────────┘
                │
                ▼
  ┌────────────────────────────────────┐
  │ Stage 3: LLR COMPUTATION           │ research/demod/llr.go
  │  - Log-sum-exp 8-FSK demap         │ LLRs(energies)
  │  - Gray-code bit assignment        │   → [174]float64 LLRs
  │  - Winsorized noise (10% trim)     │ research/demod/calibration.go
  │  - Optional Costas-anchor calib.   │ LLRsCalibrated(energies, noise)
  │  - Optional surgical row erasure   │ LLRsSoftened(energies, T1, T2, c)
  └─────────────┬──────────────────────┘
                │
                ▼
  ┌────────────────────────────────────┐
  │ Stage 4: LDPC DECODE               │ research/ldpc/
  │  - BP (normalised min-sum, 25 it)  │ Decode(llrs)
  │  - If parity-clean + CRC: accept   │   → Result, Stats
  │  - Else: OSD-2 (4187 candidates)   │
  │  - CRC14 gate at every accept      │
  └─────────────┬──────────────────────┘
                │ If CRC passes:
                ▼
  ┌────────────────────────────────────┐
  │ Stage 5: MESSAGE UNPACK            │ research/unpack/
  │  - i3 routing (Type 1 / Type 4)    │ Unpack(infoBits)
  │  - c28/c58/g15 sub-decoders        │   → text string
  │  - Output: "CALL1 CALL2 GRID"      │
  └─────────────┬──────────────────────┘
                │
                ▼
            Text decoded
```

## Session 94 outer loop (interference cancellation)

Only invoked by `research/cmd/unmask-probe/`, NOT yet wired into `research/cmd/decode-eval/`. Lifts parity by re-running the pipeline on a residual buffer after subtracting decoded signals:

```
   ╭─→ Run stages 1-5 (the pipeline above)
   │   → set of decoded text records this pass
   │
   │   COHERENT-ADAPTIVE CANCELLATION:
   │     For each decode with clean phase fit:
   │       synthesize signal from codeword
   │       demod audio by reference (Hilbert-mixed)
   │       LPF the demodulated complex gain (Hann, ~1.4 Hz)
   │       overlap-area normalise
   │       timing refinement (±20 sample search)
   │       subtract reconstructed signal from working audio
   │
   ╰── if any new decodes this pass AND iter < 3, loop back
```

Configurable via `unmask-probe` flags: `-iterations` (1-3), `-rms` (RMS threshold for "clean enough to subtract"), `-top` (how many cleanest signals to subtract per pass), `-coherent-adaptive` / `-per-block` / `-per-symbol` (subtraction calibration mode).

## Per-stage characteristics

Real-capture corpus (6 WAVs, 144 jt9-oracle-confirmed signals):

| Stage | Inputs in | Survives out | Loss mechanism |
|---|---:|---:|---|
| Candidate detection | 180k samples × 6 WAVs | ~600 candidates total | 7 truth signals never detected (5 WinsTotal too low at truth-nearest bin; 2 with stage-1 matched-filter score below threshold) |
| Demod + LLRs | ~600 candidates | 174 LLRs each | Lossless (everything passes through) |
| BP | ~600 candidates | ~88 parity-clean codewords | BP fails on signals with noisy LLRs |
| BP + OSD | ~600 candidates | 96 CRC-clean codewords | OSD-2 rescues ~8 marginal cases beyond BP-alone |
| Unpack | 96 CRC-clean | 96 text records | Type 1+4 covered; Type 0/2/3/5 skipped (none in current corpus) |
| **+ Iterative cancellation (2-iter coherent-adaptive, Session 94)** | 96 + new candidates | **107 total decodes** | +11 unmasked from adjacent-signal subtraction |
| **+ Winsorized noise (Session 95, corrected)** | as above | **109 total decodes** | +2 net (+1 pass-1 baseline, +1 subtraction effectiveness) |
| **+ Surgical row-level LLR erasure (Session 96)** | as above | **110 total decodes** | +1 additional matched, -1 text-extra |

## Where the 34 remaining unmatched signals live

After all current pipeline stages including 2-iter coherent-adaptive cancellation + Winsorized noise + surgical row-level LLR erasure (Sessions 94-96), 144 − 110 = 34 signals are not recovered:

- **7 fail at Stage 1** (candidate detection)
  - Classification per Session 92's on-lattice probe:
    - 5 signals: WINS_LOW — wins=5-7 at truth-nearest spectrogram bin, gate floor is 8.
    - 2 signals: STAGE1_LOW — stage-1 matched-filter score below threshold of 1.0 (measured 0.68 and 0.74).
  - Session 92 gate-relaxation sweep + Session 93 cap-sweep both confirmed: lowering gates admits these signals but they STILL don't produce CRC-clean decodes downstream. So even fixing the finder gap wouldn't lift parity for these 7 — they're decoder-limited via the LLRs they'd produce.

- **27 fail at Stage 4** (LDPC decode)
  - Candidate detection works, demod produces LLRs, BP fails, OSD-2's 4187-codeword enumeration contains zero CRC-passing codewords.
  - Per Session 93's OSD instrumentation: these are *fundamentally information-limited* given current LLR quality + OSD-2 search depth.
  - Session 95's Winsorized noise estimator already rescued 1 from this bucket; Session 96's surgical row-level LLR erasure rescued 1 more; the remaining 27 are deeper.

## Per-stage CPU budget

Default build (`go build`), single-pass coherent-adaptive enabled, i3-10100F:

| Stage | % of total runtime |
|---|---:|
| Candidate detection (verifyCostas + SIMD Goertzel dominate) | ~25% |
| Demod + LLRs | ~3% |
| LDPC decode (BP+OSD-2) | ~30% |
| Iterative cancellation (Session 94 coherent-adaptive) | ~40% |
| Unpack | <1% |
| Misc (WAV I/O, scoring, candidates.Find at re-detect) | ~2% |

Total: ~70s for 6-slot corpus = ~12s/slot (under the 15s real-time slot budget).

## Structural interpretation — what attack surfaces remain

The 37-signal gap decomposes into a small finder loss (7 signals; structurally hard, gate-relaxation doesn't help) and a substantial decoder loss (30 signals; the dominant remaining bucket).

Within the 30-signal decoder loss, signals are **information-limited** given current LLR quality + BP+OSD-2's reach. The audio has been made as clean as our subtraction tools allow (Session 94's coherent-adaptive at 2 iterations converges). What remains is the decoder side of the equation.

Two structurally distinct attack surfaces:

1. **Improve LLR quality** (so existing BP+OSD-2 extracts more from each candidate)
   - Per-symbol noise floor estimation
   - Joint estimation of (freq, dt, amplitude) refinement before LLR computation
   - More-precise demodulation (e.g., longer Goertzel windows, coherent integration)

2. **Deepen the decoder** (so existing LLRs feed a more capable codeword search)
   - OSD-3 (operator ruled out in Session 94)
   - AP (a-priori) decoding — operator's status: tentatively ruled out pending verification that jt9 doesn't use AP by default
   - List BP (keep top-K codeword candidates from BP's posterior)
   - More BP iterations / different scaling

Compute optimisations don't change which signals decode — only how fast we get the same answer. Algorithm/pipeline-level changes are required for further parity gain.

## Closed attack surfaces (do not revisit without new information)

- Linear coherent demod (Session 89): do-no-harm only on real captures.
- Piecewise coherent demod (Session 91): regressed.
- PhaseRefineFreq for candidate-stage timing (Session 92): no lift, +2 extras.
- Candidate-finder gate floors (Session 92): full 30-cell sweep, every cell identical to baseline.
- `stage2MaxResults` candidate cap (Session 93): {100, 200, 500} all flat.
- OSD CRC-pass rescue (Finding 2, Session 93): raw + normalised metric distributions overlap; no threshold separates rescuable from CRC-lottery.
- Matched-filter subtraction at lattice-snapped frequency (Session 84, corrected by Session 89-94): the original ruling was on a misdiagnosed root cause; coherent-adaptive cancellation with sub-bin freq + per-sample channel-gain estimation does work (+11 matched).
- Costas-anchor LLR calibration (Session 95): measured-neutral on this corpus. The Winsorized data-symbol estimator already handles the QRM/leakage vulnerability it was meant to attack, and the smaller Costas anchor sample pool (147 vs 406) adds variance. Retained as research scaffolding (`LLRsCalibrated` + `EstimateCostasCalibration` + `-calibrate-costas` flag, default off).

## References

- `docs/session-handoff.md` — route to the retired session-by-session Git snapshot
- Memory files for per-stage detail (see top of this doc)
- QEX paper 2020 (Franke/Somerville/Taylor) — spec source
- QEX ref [14] tarball at `references/ft4_ft8_public/` — public-domain encoding tables
