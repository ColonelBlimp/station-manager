# OSD policy four-experiment sweep — strict corpus 2026-05-29

Session-107 OSD policy investigation. Per-candidate decoder trace
(`research/sandbox/trace.go`) surfaced that 14 of 21 strict-baseline
accepted extras came via OSD-2 rescue (BR.DecodeMethod prefix "OSD")
rather than BP convergence. Four experiments measured the controlled
levers on that finding.

Baseline for the comparison: strict mode with Stage2 filter
GeoContrast ≥ 0.70 (the 2026-05-29 Session-106 promotion). Pre-Stage2
baseline (113/23) and Session-105 baseline (111/23) remain reachable
via `-strict -stage2-mode off` and `-strict -stage2-mode off
-legacy-power-llr` respectively for cross-session comparison.

## Results

| experiment | matched | extras | Δ matched | Δ extras |
|---|---|---|---|---|
| **baseline (strict + Stage2, OSD-2 @ 0.05)** | **113** | **21** | — | — |
| Exp 1: `-osd-disable` (BP-only) | 107 | 10 | −6 | −11 |
| Exp 2: `-osd-disable-n1` | 113 | 20 | 0 | −1 |
| Exp 3a: `-osd-accept-ratio 0.040` | 112 | 18 | −1 | −3 |
| **Exp 3b: `-osd-accept-ratio 0.045`** | **113** | **18** | **0** | **−3** |
| Exp 3c: `-osd-accept-ratio 0.035` | 111 | 18 | −2 | −3 |
| Exp 3d: `-osd-accept-ratio 0.030` | 110 | 16 | −3 | −5 |
| Exp 3e: `-osd-accept-ratio 0.010 / 0.020` | 107 | 10 | −6 | −11 (≈BP-only) |
| Exp 4: `-osd-order 1` | 111 | 18 | −2 | −3 |
| **Combo: `-osd-disable-n1 -osd-accept-ratio 0.045`** | **113** | **17** | **0** | **−4** |
| Combo: `-osd-order 1 -osd-accept-ratio 0.045` | 111 | 18 | −2 | −3 |

## Headline

**Strict-mode default promoted 2026-05-29 (same day, post-OSD-sweep):
`OSDDisableForN1=true` + `OSD.AcceptDistanceRatio=0.045` (Order kept
at 2).** New strict baseline: **113/144 matched / 17 extras** (+0 / −4
vs the prior Stage2-only 113/21). Cumulative session shipping over a
single development day: 111/23 (power-domain) → 113/23 (magnitude-
domain) → 113/21 (+ Stage2 verifier) → **113/17 (+ OSD policy)** —
**+2 matched / −6 extras** total.

## Funnel-level attribution under the combo (113/17)

| metric | baseline 113/21 | combo 113/17 | Δ |
|---|---|---|---|
| BP-winning extras | 7 | 8 | +1 (displacement) |
| OSD-winning extras | 14 | 9 | −5 |
| N1-winning extras | 14 | 7 | −7 |
| N2-winning extras | 4 | 6 | +2 (displacement) |
| N3-winning extras | 0 | 2 | +2 (displacement) |
| N1Norm-winning extras | 3 | 2 | −1 |
| **total** | **21** | **17** | **−4** |

The two interventions are mostly complementary:
- Tightening `AcceptDistanceRatio` 0.05 → 0.045 catches CRC-lottery
  OSD codewords across all metrics.
- Disabling OSD on the N1 stage specifically blocks the N1+OSD path
  while letting deeper metrics catch the same candidate via their own
  BP or OSD; some of these displacements produce extras at N2/N3
  (+4 combined) but the net OSD-extras drop (-5) and N1-extras drop
  (-7) dominate.

## Interpretation per the decision rule

> If BP-only or "no N1 OSD" drops extras sharply with modest recall
> loss, promote a stricter OSD policy. If recall loss is too high,
> keep OSD but make it conditional on stronger grid/tone-agreement
> evidence.

- **Exp 1 (BP-only) loses 6 matched truths** — pure OSD-disable is
  not a viable promotion. OSD-2 is doing real recall work. The 11
  extras it kills represent the upper-bound on what OSD policy can
  cut.
- **Exp 2 (N1-only OSD-disable) is weaker solo than the trace
  suggested** — only −1 extra. Most of the 14 N1-winning extras
  came via N1+BP, not N1+OSD. But it's still useful in combination.
- **Exp 3b (`AcceptDistanceRatio = 0.045`) is the headline single-
  knob win** — `+0 / −3` at exactly one notch below default. Cliff
  at 0.030 (−3 matched) and near-BP-only behaviour below 0.020.
  Operating band is narrow: 0.040 - 0.050.
- **Exp 4 (Order 1) loses 2 matched for 3 extras** — order-2's extra
  4095 candidates buy 2 recall + admit 3 false positives. Order-2
  stays.
- **Combo (Exp 2 + Exp 3b) at zero recall loss / −4 extras** is the
  promotion target.

## Corpus-calibration caveat

Both the Stage2 threshold (0.70) and the OSD AcceptDistanceRatio
(0.045) are **empirical operating points** measured on the 6-fixture
strict corpus. The verifier algorithms (Stage2 = QEX § 4 Costas
verifier + Goertzel; OSD = Fossorier-Lin 1995, adapted) are spec-
clean and shippable into a clean-room MIT library. The operating
points are NOT.

Any holdout / expanded corpus must re-sweep before treating these as
production-grade defaults. The sweep methodology is shipped:
`cmd/sandbox-asym-ab -strict -stage2-mode <mode> -osd-accept-ratio
<x>` etc., reproduces every cell in the table above.

## What this leaves on the table

The Session-107 decoder trace surfaced 12 **unpack_fail** missed
truths (BP found a syndrome-clean codeword with valid CRC14, but the
77-bit message didn't parse as a legitimate FT8 message) + 3 wrong-
text accepts. The unpack_fail / wrong-codeword bucket survives all
four OSD experiments — OSD policy doesn't repair "BP converged to
nearby wrong codeword." This is the next attack surface, structurally
distinct from OSD tuning:

**Next experiment (per operator's plan): N1 reliability calibration.**
Per-symbol confidence scaling — reduce LLR magnitude on symbols with
weak tone separation or high competing-tone energy, so BP doesn't
over-commit on noisy symbols. Measured against whether unpack_fail
and accepted_wrong_text buckets shrink without recall loss.

## Reproducibility — relevant commands

```bash
# new strict default (113/17):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict

# A/B paths preserved:
# pre-OSD-promotion (113/21, Stage2 only):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -osd-disable-n1=false

# pre-Stage2 magnitude baseline (113/23):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -stage2-mode off -osd-disable-n1=false

# pre-magnitude-LLR-fix legacy baseline (111/23):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -stage2-mode off -legacy-power-llr -osd-disable-n1=false

# BP-only (107/10):
go run ./research/cmd/sandbox-asym-ab -dir captures -strict -osd-disable

# Funnel with same OSD/Stage2 config (per-truth + trace attribution):
go run ./research/cmd/sandbox-miss-funnel -dir captures \
    -stage2-mode filter -stage2-metric geo -stage2-threshold 0.70 \
    -osd-disable-n1 -osd-accept-ratio 0.045
```

## Files

- `research/sandbox/multipass.go` — new `OSDDisableForN1` field on
  `MultiPassOptions`. `runCascade` applies N1-stage OSD override
  before the N1 BP call; other stages keep the normally-configured
  `bpOpts.OSD`.
- `research/cmd/sandbox-asym-ab/main.go` — `-osd-disable` /
  `-osd-disable-n1` / `-osd-accept-ratio` / `-osd-order` CLI flags +
  `flag.Visit` override-detection so `-strict` applies the OSD
  policy defaults only when the operator didn't explicitly set any
  `-osd-*` flag. Strict banner updated to surface the OSD policy +
  corpus-calibrated caveat.
- `research/cmd/sandbox-miss-funnel/main.go` — same four `-osd-*`
  flags + identical strict-baseline-matching plumbing so the funnel
  reproduces decoder runs at any OSD configuration.
- `research/sandbox/reports/decoder-trace-stage2-filter-geo-0.70-2026-05-29.txt`
  — per-candidate BP/OSD trace that drove this investigation
  (Q1-Q5 attribution).
- `research/sandbox/reports/osd-policy-best-combo-2026-05-29.txt`
  — funnel under `-strict -osd-disable-n1 -osd-accept-ratio 0.045`
  with full Q4/Q5 attribution + per-truth funnel deltas.
