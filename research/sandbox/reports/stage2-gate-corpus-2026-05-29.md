# Stage2 verifier gate — strict-corpus sweep 2026-05-29

Lighter-touch wiring: post-NMS Costas verifier (`research/sandbox/costas_verify.go`,
clean-room from QEX § 4 + Goertzel 1958) inserted between `FindCandidates` and
`RefineCandidate` in `MultiPassDecodeFull`. Sandbox finder, NMS, refine, BP/OSD,
gate, and multi-pass dedup were left unchanged. Strict-mode magnitude-domain
LLR baseline (113/23) is the comparison point.

Sandbox `VerifyCostasAt` agrees bit-exactly (1e-9 relative) with
`research/candidates.VerifyCostas` across 200 candidates per the
parity test (`costas_verify_parity_test.go`), so the discriminator
behaviour matches the 2026-05-29 audit oracle.

## Results

| Mode | matched | extras | post-Stage2 cands | Δ vs baseline |
|---|---|---|---|---|
| off (baseline) | 113 | 23 | 1200 | — |
| observe | 113 | 23 | 1200 | exact no-op (sanity ✓) |
| rerank minblock | 112 | 23 | 1200 | **−1 matched, 0 extras** |
| filter geo @ 0.70 | 113 | 21 | 204 | **+0 / −2** |
| filter wins @ 8 | 113 | 21 | 204 | **+0 / −2** (same 2 extras as geo) |

## Headline

**First positive ROC trade on this corpus.** GeoContrast filter @ 0.70 and
WinsTotal filter @ 8 both eliminate 2 false-positive decodes (one from
`20m_slot3.wav`, one from `live_slot2.wav`) with zero matched-truth loss.
Post-Stage2 candidate volume drops from 1200 to 204 (83% reduction), so 996
alias-shaped candidates are removed from refinement and downstream BP/OSD.

The two metrics flag the **same two false-positive decodes** — the per-fixture
extras diff is identical between `geo @ 0.70` and `wins @ 8`. Their
discrimination machinery is closely related (both come from the per-anchor
8-tone vote / log-contrast) and they agree on which post-NMS candidates are
alias-shaped enough to drop.

## Threshold sweeps (matched / extras)

### MinBlockContrast filter

| threshold | matched | extras |
|---|---|---|
| ≤0.15 | 113 | 23 (no effect) |
| 0.20 | 112 | 21 (−1m, −2e) |
| 0.30 | 110 | 18 (−3m, −5e) |
| 0.40 | 107 | 13 (−6m, −10e) |
| 0.50 | 101 | 10 (−12m, −13e) |

MinBlock as a filter metric trades ~1:1 matched-for-extras above 0.20 — not
a free lunch on this corpus, despite its strong p50 discrimination in the
audit. The marginal-decode tail is wider than the alias tail at low MinBlock.

### GeoContrast filter

| threshold | matched | extras |
|---|---|---|
| ≤0.50 | 113 | 23 (no effect) |
| 0.60 | 113 | 22 (+0, −1) |
| 0.65–0.75 | 113 | 21 (+0, −2) ✓ |
| 0.80 | 111 | 21 (−2m, −2e) |
| 0.85 | 111 | 21 (−2m, −2e) |
| 0.90 | 108 | 21 (−5m, −2e) |

GeoContrast has a clean plateau across 0.65–0.75 — the sweet spot operating
band. 0.70 is the natural pick (centre of plateau).

### WinsTotal filter

| threshold | matched | extras |
|---|---|---|
| ≤6 | 113 | 23 (no effect) |
| 7–10 | 113 | 21 (+0, −2) ✓ |
| 11 | 109 | 20 (−4m, −3e) |

WinsTotal also has a plateau at 7–10. Cliff at 11 because some marginal
decodes have only 11 correct Costas wins out of 21.

## Per-truth funnel deltas (off → filter geo @ 0.70)

| stage | off | filter geo 0.70 | Δ |
|---|---|---|---|
| matched | 113 | 113 | 0 |
| gate-bound | 1 | 1 | 0 |
| decoder-bound | 21 | 18 | −3 |
| refine-bound | 0 | 1 | +1 |
| stage2-bound | 0 | 2 | +2 |
| cap-bound | 0 | 0 | 0 |
| nms-bound | 7 | 7 | 0 |
| finder-miss | 2 | 2 | 0 |

Three near-truth candidates (2 → stage2-bound, 1 → refine-bound) were
dropped earlier in the pipeline than baseline. These were already
decoder-bound at baseline (BP/OSD wasn't going to succeed on them); Stage2
catches them upstream without touching the matched truths.

## Rerank caveat

Rerank mode lost 1 matched truth at default options. The candidate set is
unchanged (rerank doesn't drop anything); the loss comes from order-dependent
side effects in `MultiPassDecodeFull`:

- **Pass-2 subtraction order.** Pass-1 decoded set determines what gets
  subtracted from the audio before pass 2. Reranking changes which
  candidates pass-1 visits first, which can change which decodes succeed,
  which changes the pass-2 residual.
- **`CallsignHashTable` accumulation order.** `registerCallsigns` populates
  the hash table per accepted decode. Type-4 messages and hashed Type-1
  references read from this table — visiting candidates in a different
  order can change what's in the table when a later Type-4 is decoded.

Rerank is not a clean "set-membership-only" operation in this pipeline.
Filter mode is the clean intervention.

## What this rules in / out

- **Rules in**: Stage2 verification IS the structural front-end fix the
  Session-105 audit predicted. Adding a Costas-anchor 8-tone gate after NMS
  cuts confirmed false-positive decodes with zero true-positive cost.
- **Rules out**: this alone is not enough to crack the 21 decoder-bound
  misses or the 7 NMS-bound misses or the 2 finder-miss misses. The matched
  count holds at 113. The +2 extras-reduction is a quality-of-output win,
  not a recall win.

## Next-step candidates

The per-truth count of "decoder-bound" misses still dominates (18 of the
remaining 31 misses). Stage2 trims the alias false positives that confuse
the audit picture; with that cleaner picture, the next attack surfaces are
the same ones flagged in Sessions 84/95/96:

- 7 NMS-bound truths could be recovered by widening K2 — Session 105 showed
  this trades alias-growth, but Stage2 now sits between the wider K and
  refinement and would absorb the extra aliases. Worth a re-measurement.
- 18 decoder-bound truths — these reach BP with insufficient symbol
  evidence. Channelizer / refinement quality is the candidate next lever.
- 2 finder-miss truths — far-from-grid; the matched filter doesn't peak.

## Files

- `research/sandbox/costas_verify.go` — clean-room verifier + Stage2Mode /
  Stage2Metric enums + applyStage2 (exported as `ApplyStage2` for the
  funnel).
- `research/sandbox/costas_verify_parity_test.go` — sandbox-vs-candidates
  parity test (200 candidates, 1e-9 relative tolerance, agreement on
  WinsTotal / GeoContrast / MinBlockContrast).
- `research/sandbox/multipass.go` — `MultiPassOptions.Stage2Mode/Metric/
  Threshold` fields + `applyStage2` hook between `FindCandidates` and
  refine loop.
- `research/cmd/sandbox-asym-ab/main.go` — `-stage2-mode`, `-stage2-metric`,
  `-stage2-threshold` flags + parsers.
- `research/cmd/sandbox-miss-funnel/main.go` — same flags + new
  `stage2-bound` funnel stage + corpus-totals candidate-survival report.
- `research/sandbox/reports/funnel-stage2-{off,observe,rerank-minblock,
  filter-geo-0.70,filter-wins-8}-2026-05-29.txt` — per-mode funnel +
  survival output.
