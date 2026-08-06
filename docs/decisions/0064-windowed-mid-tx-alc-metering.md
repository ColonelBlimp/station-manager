---
number: 0064
title: Windowed mid-transmission ALC metering over CAT
status: Proposed
date: 2026-08-06
---

# 0064 — Windowed mid-transmission ALC metering over CAT

## Context

The operator's TX-drive feedback loop today runs through the linear amplifier's
front panel: when its indicated output looks wrong, the PC's mixer volume gets
adjusted by hand. That loop caused the 2026-08-01 step-shaped power dips, and
hand-mixer changes are the class that produced the 2026-07-30 muted-into-a-live-QSO
incident. The only in-chain instrument that can distinguish **overdrive**
(clipping in the rig's modulator) from every other drive fault is the rig's ALC
meter — PO cannot: it saturates at rated output whether the drive is right or
hot.

Three facts box the design in:

1. **The FTdx10 pushes only the currently-selected meter.** Under Auto
   Information the rig emits `RM0` — the value of whatever the meter switch
   shows (CAT reference 2308-F, RM page: the P1=0 form). ALC by push therefore
   requires selecting ALC on the rig, which both changes the operator's
   front-panel meter and takes PO away from the drive-collapse detector —
   `meterSelPO` is "the ONLY selection under which a silent push stream is
   evidence about RF" (`internal/bridge/meters.go`). The drive watch owns the
   meter switch.
2. **The spec places no restriction on explicit meter reads during
   transmission.** The RM page's Read form (`RM4;` → `RM4 nnn 000;`, ALC
   0–255) carries no TX/RX condition, and a sweep of the whole CAT reference
   finds no "during transmit" restriction on command processing at all
   (verified against the full manual 2026-08-06; local reference copy
   `docs/ftdx10-cat.md`, gitignored — manufacturer copyright). SM's own
   experience confirms CAT is fully alive while keyed: the rig pushes RM0
   meter frames throughout every transmission, and the drive watch consumes
   them mid-TX.
3. **`internal/bridge/meters.go` currently holds a self-imposed rule: no CAT
   writes on the key-down path.** Its two grounds are distinct and survive
   this ADR differently. (a) *The guaranteed-stop unkey must never queue
   behind anything* — absolute, kept, and the design constraint below. (b)
   *"ADR 0057 documents dropping commands in the TX→RX tail"* — re-examined
   2026-08-06: that is an **empirical observation from the July incident
   arc** (the tune-restore settle exists because of it), not a spec fact; the
   manual is silent on it. It remains a reason for a guard margin near the
   unkey, not a prohibition on mid-transmission traffic.

An earlier idea — adding a command to the rigdef `INIT` list — cannot work:
`INIT` (`AI1;`) and the `READ` burst are one-shot at connect and on the
quiet-rig liveness probe, which by construction never fires mid-transmission.
There is no CAT command that subscribes to a second meter.

**Measured 2026-08-06 (catcli, daemon stopped, RX only — no keying):** the
hypothesis that a meter query under AI subscribes continuing answers is
FALSE. Method: in one port session, query `RM1;` (S-meter — a value the
concurrent `RM0` push flood proved was changing every few frames, 034–044)
and keep listening 15 s. Result: exactly one `RM1` answer, ~30 pushed `RM0`
frames per second throughout, zero further `RM1`. The changing-value control
is what makes the negative conclusive — a static meter (ALC at rest) could
have hidden a change-driven subscription. Also observed live: a send's "next
frame" can be a pushed `RM0`, not the query's answer (the catcli Exec read
grabbed one) — the poller must match answers by `RM4` prefix, never by
arrival order. The rigdef already decodes `RM4nnn000` to the `ALC` tag
(`RM4000000 → ALC:000`, same session).

**Measured 2026-08-06 (second catcli experiment — operator-keyed, mic audio,
60 s window): `RM5;`/`RM4;` ANSWER DURING TRANSMISSION**, with live values
(`RM5029000` / `RM4026000` — PO 029, ALC 026 on the 0–255 scale; non-zero
readings exist only under drive). The mid-TX-query feasibility question is
settled by measurement, not spec inference. The same window also delivered
the strongest form of the no-subscription result — the pushed RM0 stream
swept 000–085 with the audio envelope (values changing constantly), and the
queried meters still answered exactly once each — plus one design gift: the
rig pushes `TX0` at unkey under AI, so the wire itself announces the TX→RX
transition. ALC 026 under the operator's normal voice drive is the first
live datum for the red-threshold calibration.

**Most of the mechanism already exists (established 2026-08-06, after the
catcli experiment):** ADR 0035's poll loop (`runPollLoop`, rigdef `POLL`
command as the data-driven switch, 1 s default cadence, config
`bridge.timeouts.civ_poll_interval_ms`) is generic plumbing the FTdx10 simply
never declares — and the rigdef's decode table already maps `RM4→ALC` and
`RM5→PO` into the accumulator slots `meterTags` reserved in July. Two
findings bound the delta:

- The CI-V collision back-off is inert on Yaesu (`lastBroadcastAt` is set
  only by `cat.IsCIVBroadcast`), so a declared POLL fires on schedule.
- The existing loop **hard-skips keyed intervals** — `tuneActive ||
  ft8TxActive → continue`, re-checked under `cmdMu` — citing the 2026-07-18
  TX-safety finding (a five-frame snapshot holding `cmdMu` directly ahead of
  an emergency `tx_off`). So declaring `"POLL": "RM4;RM5;"` today yields
  1 Hz meter reads exactly when they are meaningless (receive) and nothing
  when they matter (keyed). The build is therefore NOT a new poller: it is a
  keyed-interval variant of the existing one — a two-frame meter read (a
  fraction of the five-frame snapshot the skip was written against) allowed
  inside the window rules below, with the unkey still waiting on at most one
  short in-flight exchange, quantified in the guard margin.

## Decision

**Proposed:** during FT8 keyed transmissions (and the ADR 0027 tune carrier),
the bridge polls `RM4;` (ALC) on a fixed cadence inside a bounded window —
opening a start margin after key-down and closing a guard margin before the
*scheduled* unkey — single-flight, lowest priority, and structurally incapable
of delaying the unkey. Answers feed the existing explicit-answer accumulator
slot (`meterTags` already carries `ALC`) and a live value published to the SPA
via pull/latest-wins delivery (the ADR 0064-adjacent pattern shipped for the
RX audio meter after review d22eff6b). The rig's meter switch stays on PO; the
drive watch is untouched.

This narrows, and does not repeal, the `meters.go` rule: the key-down path
stays clear of *unscheduled* traffic, and the unkey's right-of-way is an
invariant the implementation must demonstrate, not assert.

### Invariants the implementation must hold

1. **The unkey never waits.** No RM4 exchange may be in flight at, or queued
   ahead of, the unkey write. The poller stops at the window's close and the
   guard margin is sized so a worst-case in-flight answer drains before the
   scheduled unkey.
2. **Poll failure is meter degradation only.** A timeout, garbled answer, or
   missed window ends polling for that transmission and at most surfaces a
   monitoring notice — it never touches TX state, the session, or the alarm
   machinery.
3. **The drive watch is unaffected.** METERSEL stays PO; the pushed RM0
   stream and the per-transmission PO summary are byte-identical with polling
   on or off.
4. **Session-scoped.** Polling starts only after the bridge commits a
   transmission and stops on ANY end of that transmission — completion,
   abandon, disarm, dial-guard teardown — not just the happy path.

### Open questions — the operator's calls, deliberately unfilled

- **Guard margin** before the scheduled unkey (bounds our observed TX→RX tail
  behaviour; every threshold invented without asking has been wrong).
- **Cadence** (2 Hz? 4 Hz? — an FT8 transmission is ~13 s; even 2 Hz gives ~24
  readings).
- **Start margin** after key-down (let the keying settle before the first
  query).
- **Whether Tune polls too** (recommended: it is the calibration moment).
- The SPA-side **red threshold** for ALC display (0–255 scale; config-served
  like `ft8.audio`, hardware-calibrated, not invented).

## Alternatives considered

### Select ALC on the rig and retrain the drive watch

Loses the no-RF detector: with ALC selected, a silent or zero push stream is
*normal* (good FT8 drive reads zero ALC), so drive-collapse detection — which
caught real incidents — has no signal. Also flips the operator's front-panel
meter. Rejected outright.

### Meter-switch juggling (MS to ALC during TX, back to PO after)

Writes on the key-down path *and* blinds the drive watch during exactly the
keyed intervals it exists for, *and* flickers the front panel. Strictly worse
than both polling and doing nothing.

### ALC at Tune time only

Safe and nearly free (the tune carrier is an operator-initiated diagnostic
keying), but it cannot warn when drive drifts mid-session — the operator's
actual observed failure mode. Retained as the fallback if on-hardware
acceptance shows mid-TX polling disturbing anything.

### Display whatever the rig's meter shows (pushed stream, labelled by METERSEL)

Zero new CAT traffic; a live ALC face whenever the operator flips the meter
for a drive-setting session. Complementary rather than competing — it may
still be worth building as the PO face of the TX meter — but as the only
mechanism it makes ALC visibility depend on giving up drive-watch coverage
for those transmissions.

### Adding `RM4;` to the rigdef INIT/READ burst

One answer per connect / liveness probe, which by construction happens when
the rig is quiet — never mid-transmission, where ALC means something.
Mechanically unable to deliver the feature.

## Consequences

- The operator's drive-setting loop moves from the amp's panel + PC mixer to
  an on-screen ALC readout with the meter switch untouched and the drive
  watch armed — removing the recurring cause of hand-mixer drift.
- The bridge gains its first deliberate mid-transmission CAT traffic. The
  no-writes-during-key-down rule in `meters.go` must be rewritten to name
  this ADR and the narrower invariant (unkey right-of-way), or the comment
  becomes a trap for the next reader.
- A new scheduled component (the poll window) rides the FT8 TX timetable —
  one more thing bound to the slot clock, with the usual enumerate-the-steps
  discipline at its edges (what happens when a transmission ends early is a
  named invariant, not an afterthought).
- Live TX path: implementation and on-air validation happen only under the
  standing per-occasion agreement, with the acceptance procedure written
  before the build.

## Acceptance criteria (drafted for operator ratification)

1. When transmitting FT8 with the rig's meter on PO, I see a live ALC reading
   updating through the transmission — and I can tell *ALC deflecting* (drive
   hot) from *ALC at zero* (drive right) from *no ALC data* (polling
   declined/failed), each rendered distinctly.
2. The per-transmission meter summary in `smd.log` is unchanged with polling
   enabled — same PO sample counts and gaps — and the drive watch arms
   exactly as before. (Passive check, costs nothing.)
3. Unkey behaviour is unchanged: `keyed_ms` stays at the slot length across a
   session of transmissions, and no tx-alarm or drive-alarm regressions
   appear. (Passive check over existing log fields.)
4. On-hardware procedure, passive-first, written before the build: (i) normal
   FT8 transmissions with polling on — expect criterion 2 and 3 observations
   from the log alone; (ii) operator-driven overdrive (the operator raises
   their own audio gain momentarily — SM never touches drive per the standing
   boundary) — expect the on-screen ALC to deflect and the red state to show;
   (iii) flip the rig meter to ALC and compare the on-screen value against
   the front panel — expect agreement.

## Triggers to revisit

- If on-hardware acceptance shows ANY unkey-timing or PO-stream disturbance,
  fall back to "ALC at Tune time only" and record the measurement here.
- If a second rig lands whose CAT reference *does* restrict reads during TX,
  the polling gains a per-rigdef capability flag rather than being assumed.
- If the drive watch ever moves to explicit polling itself (ADR 0034
  territory), fold both pollers into one scheduler rather than running two.

## References

- FTdx10 CAT reference 2308-F, RM / AI / TX pages (local gitignored copy:
  `docs/ftdx10-cat.md`).
- `internal/bridge/meters.go` — the pushed-meter architecture and the rule
  this ADR narrows.
- ADR 0057 — the empirical TX→RX tail observation and unkey right-of-way.
- ADR 0027 / 0030 — the guaranteed-stop discipline the unkey invariant
  belongs to.
- `docs/backlog.md` "FT8 audio levels" entry — stage 2 (TX indicator); the RX
  meter (stage 1) shipped 2026-08-06 with the pull/latest-wins delivery this
  ADR reuses.
