---
number: 0064
title: Continuous ALC/PO meter polling while an FT8 capture session is live
status: Accepted (2026-08-08 — §4 on-hardware acceptance complete; colour
  grammar fully ratified: green/amber only, red folded into amber)
date: 2026-08-06
---

# 0064 — Continuous ALC/PO meter polling while an FT8 capture session is live

## Context

The operator's TX-drive feedback loop today runs through the linear
amplifier's front panel: when its indicated output looks wrong, the PC's
mixer volume gets adjusted by hand. That loop caused the 2026-08-01
step-shaped power dips, and hand-mixer changes are the class that produced
the 2026-07-30 muted-into-a-live-QSO incident. The only in-chain instrument
that can distinguish **overdrive** (clipping in the rig's modulator) from
every other drive fault is the rig's ALC meter — PO cannot: it saturates at
rated output whether the drive is right or hot.

Three facts box the design in:

1. **The FTdx10 pushes only the currently-selected meter.** Under Auto
   Information the rig emits `RM0` — the value of whatever the meter switch
   shows (CAT reference 2308-F, RM page: the P1=0 form). ALC by push
   therefore requires selecting ALC on the rig, which both changes the
   operator's front-panel meter and takes PO away from the drive-collapse
   detector — `meterSelPO` is "the ONLY selection under which a silent push
   stream is evidence about RF" (`internal/bridge/meters.go`). The drive
   watch owns the meter switch.
2. **The spec places no restriction on explicit meter reads during
   transmission.** The RM page's Read form (`RM4;` → `RM4 nnn 000;`, ALC
   0–255) carries no TX/RX condition, and a sweep of the whole CAT reference
   finds no "during transmit" restriction on command processing at all
   (verified against the full manual 2026-08-06; local reference copy
   `docs/ftdx10-cat.md`, gitignored — manufacturer copyright).
3. **`internal/bridge/meters.go` holds a self-imposed rule: no CAT writes on
   the key-down path.** Its two grounds survive this ADR differently. (a)
   *The guaranteed-stop unkey must never queue behind anything* — absolute,
   kept, and quantified below. (b) *"ADR 0057 documents dropping commands in
   the TX→RX tail"* — re-examined 2026-08-06: an **empirical observation
   from the July incident arc**, not a spec fact. It survives here as the
   reason a poll's answer can be LOST near the transition — which sets the
   timeout-skip rule, not a prohibition.

An earlier idea — adding `RM4;RM5;` to the rigdef `INIT` or `READ` lists —
cannot work: both are one-shot at connect / SSE-open / liveness probe, which
by construction never fire mid-transmission. There is no CAT command that
subscribes to a second meter.

**Measured 2026-08-06, first catcli experiment (daemon stopped, RX only):**
the hypothesis that a meter query under AI subscribes continuing answers is
FALSE. In one port session, `RM1;` (S-meter — a value the concurrent `RM0`
push flood proved was changing every few frames) answered exactly once,
with ~22 pushed `RM0` frames per second throughout and zero further `RM1`.
The changing-value control is what makes the negative conclusive. Also
observed: a send's "next frame" can be a pushed `RM0`, not the query's
answer — the poller must match answers by prefix, never by arrival order.
The rigdef already decodes `RM4nnn000 → ALC` and `RM5nnn000 → PO`.

**Measured 2026-08-06, second catcli experiment (operator-keyed, mic audio,
60 s window): `RM5;`/`RM4;` ANSWER DURING TRANSMISSION**, with live values
(`RM5029000` / `RM4026000` — PO 029, ALC 026 on the 0–255 scale; non-zero
readings exist only under drive). Mid-TX-query feasibility is settled by
measurement. The same window delivered the strongest no-subscription result
— the pushed stream swept 000–085 with the audio envelope while the queried
meters answered exactly once each — plus one design gift: the rig pushes
`TX0` at unkey under AI, so the wire itself announces the TX→RX transition.
ALC 026 under the operator's normal voice drive is the first live datum for
the red-threshold calibration.

**Most of the mechanism already exists:** ADR 0035's poll loop
(`runPollLoop`, rigdef `POLL` command as the data-driven switch, config
`bridge.timeouts.civ_poll_interval_ms`) is generic plumbing the FTdx10
simply never declares, and the decode chain into the `meterTags`
accumulator slots has been waiting since July. Two findings bound the
delta: the CI-V collision back-off is inert on Yaesu (`lastBroadcastAt` is
set only by `cat.IsCIVBroadcast`), and the existing loop **hard-skips
keyed intervals** (`tuneActive || ft8TxActive`, re-checked under `cmdMu`),
citing the 2026-07-18 finding that a five-frame snapshot held `cmdMu`
directly ahead of an emergency `tx_off`. That skip stays correct for the
Icom snapshot it was written for; the FT8 meter poll is a different animal
— two short frames, not five, with its own bound below.

## Decision

**Proposed:** while an FT8 capture session is live, the bridge polls
`RM4;RM5;` (ALC + PO) **continuously — receive and transmit alike — every
250 ms**, single-flight, with a **100 ms answer timeout**; a timeout skips
the cycle silently, never retries. Answers feed the existing `ALC`/`PO`
accumulator slots and a live value published to the SPA via pull/latest-wins
delivery (the pattern shipped for the RX audio meter). The rig's meter
switch stays on PO; the drive watch is untouched.

Cadence and timeout were **operator-ratified 2026-08-06**. Both live in
config beside the existing poll knob so field calibration needs no rebuild.

The continuous model replaced an earlier windowed draft (poll only inside a
guarded keyed window) on the operator's simplification: with single-flight
and a short answer timeout, the whole windowing state machine — start
margin, guard margin, session-end enumeration, schedule coupling — buys
nothing. **The load-bearing number is the answer timeout, not the cadence**:
the unkey waits behind at most one in-flight two-frame exchange, and the
timeout bounds that wait even when the TX→RX tail eats the answer.

### Invariants the implementation must hold

1. **The unkey's worst-case wait is one exchange, bounded by the answer
   timeout.** Single-flight is structural, and the poll's `cmdMu` hold may
   never exceed the timeout — no retry inside the hold, no multi-frame
   bursts.

   > **AMENDED 2026-08-07 (build-time finding, codex P1 on d7c4dcdc,
   > operator-ratified).** The answer timeout cannot bound a BLOCKED write:
   > `internal/serial` honours ctx only between `write(2)` calls, and the
   > poll burst is a single call. The deliverable bound is therefore
   > two-tier — **healthy path ~3 ms** (one 12-byte burst at 38400 baud;
   > single-flight holds), **fault path = the serial write watchdog**
   > (`bridge.timeouts.write_watchdog_ms`), which frees the write path by
   > closing the port. To keep the fault-path bound tight now that a write
   > sits in every keyed interval, the watchdog default was tightened
   > **2 s → 500 ms** with this amendment (~170× a healthy write; the
   > wedged-port-mid-TX consequence itself predates this ADR — any CAT
   > write could always wedge — this ADR changes the exposure, and the
   > amendment cuts the whole system's bound fourfold). The answer timeout
   > keeps its other jobs: the CI-V between-frames bound and the
   > answer-freshness discipline (skip, never retry).
2. **Timeout is skip, not failure.** A lost answer (transition tail, busy
   rig) ends that cycle silently; sustained loss at most surfaces a
   monitoring notice. Polling never touches TX state, the session, or the
   alarm machinery.
3. **Answers are matched by prefix (`RM4`/`RM5`), never by arrival order** —
   the push stream interleaves freely (observed live).
4. **The drive watch is unaffected.** METERSEL stays PO; the pushed RM0
   stream and the per-transmission PO summary are byte-identical with
   polling on or off.
5. **Polling lives and dies with the FT8 capture session** — the existing
   session lifecycle, no additional state machine.

### Open questions — the operator's calls, deliberately unfilled

- **Whether the ADR 0027 tune carrier polls too** (recommended: it is the
  calibration moment, and the loop is already running if the FT8 view is
  open — the question is only the no-FT8-view case).
- The SPA-side **red threshold** for ALC display (0–255 scale; config-served
  like `ft8.audio`; first live datum: ALC 026 at normal voice drive).
  **PARTLY RATIFIED 2026-08-07, first on-air FT8 session:** healthy FT8 drive
  measured ALC 15–18 (min 15, every slot; low-power slots 7–12) with PO flat
  at target — and never zero while keyed, which broke the chip's original
  zero-only green: healthy transmissions always rendered amber, a colour that
  reads "act to make this green" and pointed at reducing audio that was
  already right (operator's own observation, on air). Ratified: **green is
  the healthy band**, ceiling `ft8.meter.alc_amber` = **30** (clears every
  healthy datum with headroom); amber = 30..red−1, genuinely elevated.
  ~~`alc_red` = 50 stays **PROVISIONAL** — no overdrive datum exists yet; the
  §4 (ii) deliberate-overdrive calibration is what produces it.~~
  **CLOSED 2026-08-08 — RED FOLDED INTO AMBER (operator-ratified) and
  `alc_red` REMOVED.** The §4 (ii) run produced the datum, and it was about
  the instrument, not the threshold: with the PipeWire sink at 1.0 (the
  digital ceiling; calibrated level ~0.4) the RM ALC answer SATURATED at
  29–30 of 255 across three slots while the operator watched the front-panel
  needle deflect far past the zone into the +20 dB over-region, and in-band
  PO collapsed from the healthy 109–121 to ~35 on both PO witnesses. So §4
  (iii)'s meter-face agreement FAILS in the over-region — the RM answer
  cannot distinguish zone-edge drive from gross overdrive — and no ALC-only
  threshold above ~30 can ever fire. Amber (≥ alc_amber) is therefore the
  TERMINAL state and its message carries the action ("reduce the audio
  level"); the unreachable red band was removed rather than documented dead
  (`internal/bridge/meters.go` holds the measurement; the SPA/daemon spec is
  `txDrive.svelte.test.ts` + `TestResolveFt8Meter`). A DISTINCT overdrive
  state remains buildable from facts SM already carries — ALC-at-ceiling
  paired with collapsed PO (121→35 in the run) — captured in
  `docs/dogfood-inbox.md` as a follow-up option, not built.

## Alternatives considered

### 50 ms cadence (operator-floated, jointly rejected 2026-08-06)

Bus-feasible (~600 B/s of 3,840), but an FT8 transmission is a
constant-envelope carrier — ALC/PO are flat from key to unkey, so 20 Hz
reads the same number twenty times while multiplying the odds that an unkey
meets an in-flight poll. Worth revisiting only if voice-mode envelope
metering ever becomes a goal.

### Windowed keyed-interval polling (this ADR's first draft)

Poll only inside [key+start-margin, scheduled-unkey−guard-margin]. Rejected
as needless complexity once the timeout bound was identified: the window
edges, margins, and every-way-a-session-ends enumeration were the bulk of
the build cost, and all of it defends against a wait the answer timeout
already bounds.

### Select ALC on the rig and retrain the drive watch

Loses the no-RF detector: with ALC selected, a silent or zero push stream
is *normal*, so drive-collapse detection — which caught real incidents —
has no signal. Also flips the operator's front-panel meter. Rejected.

### ALC at Tune time only

Safe and nearly free, but cannot warn when drive drifts mid-session — the
operator's actual observed failure mode. Subsumed: under the continuous
model, tune-time coverage falls out for free whenever the FT8 view is open.

### Display whatever the rig's meter shows (pushed stream, labelled)

Zero new CAT traffic, but ALC visibility would depend on flipping the meter
knob and standing the drive watch down. Complementary as a display detail
(the SPA can label what the pushed meter is), not a competing mechanism.

### Adding `RM4;RM5;` to the rigdef INIT/READ burst

One answer per connect / SSE-open / liveness probe — by construction never
mid-transmission. Mechanically unable to deliver the feature.

## Consequences

- The operator's drive-setting loop moves from the amp's panel + PC mixer
  to an on-screen ALC readout with the meter switch untouched and the drive
  watch armed.
- The bridge gains deliberate keyed-interval CAT traffic for the first
  time. The `meters.go` "nothing here writes to the rig" comment must be
  rewritten to name this ADR and the narrower invariant (one bounded
  exchange), or it becomes a trap for the next reader.
- The FT8 meter poll and the Icom snapshot poll are two variants of one
  loop with different keyed-interval policies — the difference (two frames
  bounded by timeout vs five frames hard-skipped) must be recorded where
  the policies diverge, not left as an apparent inconsistency.
- Polling through receive costs ~8 zero-value exchanges per second of idle
  FT8 — accepted for the design simplicity, and it doubles as a live
  "meter path alive" signal.
- Live TX path: implementation and on-air validation happen only under the
  standing per-occasion agreement, with the acceptance procedure written
  before the build.

## Acceptance criteria (drafted for operator ratification)

> **RESULTS, 2026-08-08 — all four criteria met; ADR flipped Accepted.**
> Criterion 1: live ALC updating with distinct states — observed on air
> 2026-08-07 and through the overdrive run (amber during hot slots).
> Criteria 2 + 3: per-TX meter summaries, drive-watch arming, `keyed_ms`
> and alarm behaviour all unchanged across every polled session (passive,
> from `smd.log`). §4 (i) normal slots ✓ (ALC 7–18, PO flat) · (ii)
> operator-driven overdrive ✓ — the run produced the saturation finding
> recorded under Open questions (RM ALC clips at ~30; PO collapsed 121→35)
> · (iii) meter-face comparison ✓ run, with the qualified outcome that
> agreement HOLDS in the healthy region and FAILS in the over-region (panel
> +20 dB over vs RM 30) — which is the §4 (ii) finding restated, not a
> polling defect. Meter-frame gaps of 2.0–2.5 s appeared ONLY on slots where
> the operator was hands-on (meter-face flip, mixer slide); every hands-off
> slot stayed ≤ 400 ms. Re-check trigger: a 2 s+ gap on a hands-off slot.

1. When transmitting FT8 with the rig's meter on PO, I see a live ALC
   reading updating through the transmission — and I can tell *ALC
   deflecting* (drive hot) from *ALC at zero* (drive right) from *no ALC
   data* (poll answers lost), each rendered distinctly.
2. The per-transmission meter summary in `smd.log` is unchanged with
   polling enabled — same PO sample counts and gaps — and the drive watch
   arms exactly as before. (Passive check.)
3. Unkey behaviour is unchanged: `keyed_ms` stays at the slot length across
   a session of transmissions, and no tx-alarm or drive-alarm regressions
   appear. (Passive check over existing log fields.)
4. On-hardware procedure, passive-first, written before the build: (i)
   normal FT8 transmissions with polling on — criteria 2 and 3 from the log
   alone; (ii) operator-driven overdrive (the operator raises their own
   audio gain momentarily — SM never touches drive per the standing
   boundary) — the on-screen ALC deflects and the red state shows; (iii)
   flip the rig meter to ALC and compare the on-screen value against the
   front panel — expect agreement. The catcli `-cmd`+`-listen` combo
   (built for this ADR's experiments) is the capture tool.

## Triggers to revisit

- If on-hardware acceptance shows ANY unkey-timing or PO-stream
  disturbance, fall back to "ALC at Tune time only" and record the
  measurement here.
- If a second rig lands whose CAT reference restricts reads during TX, the
  polling gains a per-rigdef capability flag rather than being assumed.
- If voice-mode envelope metering becomes a goal, revisit the 50 ms
  cadence.

## References

- FTdx10 CAT reference 2308-F, RM / AI / TX pages (local gitignored copy:
  `docs/ftdx10-cat.md`).
- The two 2026-08-06 catcli experiments (methods and results in Context).
- `internal/bridge/meters.go` — the pushed-meter architecture and the rule
  this ADR narrows; `internal/bridge/pipeline.go` `runPollLoop` — the ADR
  0035 loop this extends.
- ADR 0057 — the empirical TX→RX tail observation and unkey right-of-way.
- ADR 0027 / 0030 — the guaranteed-stop discipline the bounded-wait
  invariant belongs to.
- `docs/backlog.md` "FT8 audio levels" entry — stage 2; the RX meter
  (stage 1) shipped 2026-08-06 with the pull/latest-wins delivery this
  reuses.
