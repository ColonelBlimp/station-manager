---
number: 0035
title: Full rig-state mirror on Icom via hybrid push + targeted polling
status: Accepted
date: 2026-06-15
---

# 0035 — Full rig-state mirror on Icom via hybrid push + targeted polling

## Context

ADR 0034 chose **push-only, NO polling** for rig state — an operator directive,
rooted in past pain with poll loops. That was correct for Yaesu: its AUTO/AI mode
pushes the *complete* state (both VFOs, mode, submode, split, power), so the SPA
mirrors the rig with zero polling.

Bench work on the borrowed IC-7300 (2026-06-15) showed Icom's CI-V Transceive is
**not** the equivalent. Transceive broadcasts only the **operating VFO frequency**
(`00`) and the **base mode** (`01`) on front-panel changes. It never broadcasts
the **modifier flags** (the data flag → USB-vs-USB-D, and split) or the
**non-operating VFO**. Commanded changes don't broadcast at all (handled
separately by the wait-for-ACK path, ADR 0034 revision). The consequence: the SPA
cannot mirror an IC-7300 as completely as an FTdx10 — USB↔USB-D is invisible from
the front panel, split is invisible, VFO-B is untracked.

Two forces make that gap matter rather than being an acceptable Icom quirk: an
operator who runs **both** an Icom and a Yaesu hits non-parity ("my FTdx10 panel
is right, my IC-7300 lies about its mode") — friction that erodes trust in the
whole display; and a **future full-CAT-control SPA** (AGC/IPO/ATT/NB/NR) can't
work blind — it needs the rig's current state mirrored, making a complete mirror
load-bearing rather than a logging nicety.

The bench also established the building blocks exist and are safe (2026-06-15,
`cmd/civ-probe -mirror`): the IC-7300 answers `25 00`/`25 01` (selected /
**unselected** VFO freq — VFO-B with no select-dance), `26 00` (full operating
mode **including the data flag** in one frame), and `0F` (split) — 100% over 27
idle polls. Under a hard continuous dial-spin, polling produced only **benign,
self-healing misses**: ~27% of polls dropped exactly one *frequency* read as a
clean timeout (never a garbled frame; never the slow fields `26`/`0F`), recovered
on the next poll — and freq is pushed in real-time anyway, so nothing went stale.

## Decision

For `icom_civ` rigs only, add a **targeted, low-rate, collision-aware poll** of the
state Transceive does not push (non-operating VFO, full mode+data, split), while
**keeping Transceive push** for the fast-changing operating freq/mode. The polled
set is a **rigdef-declared, extensible read-list**; push + poll together produce a
full rig-state mirror with parity to Yaesu. Poll cadence and the collision
back-off threshold are config knobs. This **revises ADR 0034's "push-only, NO
polling" stance for Icom**; Yaesu stays push-only (it needs nothing more).

## Alternatives considered

### Keep push-only (status quo, ADR 0034)

Rejected. It bakes in structural Icom-vs-Yaesu non-parity — USB-D, split, and
VFO-B are permanently wrong/blank on Icom from front-panel changes — which is
real friction for a dual-rig operator and a hard blocker for the full-control SPA.
The "no polling" rule was right for its original target (Yaesu); it doesn't
generalise to a protocol whose push is structurally incomplete.

### Full periodic poll loop (poll everything every N seconds)

Rejected — this *is* the pattern that burned us. It floods the half-duplex bus,
causes UI jitter, and the fields that change fastest (operating freq/mode) are
already pushed, so re-polling them is pure waste **and** the dominant collision
source (the dial-spin bench showed freq reads are exactly what collides). The
hybrid keeps push for those and polls only what's missing.

### Event-triggered single reads only (the ADR 0034 escape hatch)

Rejected as insufficient for a *full* mirror. Firing one `1A 06` when a base-mode
broadcast arrives covers the data flag on that one trigger — but nothing triggers
on a front-panel split toggle, VFO-B edit, or general drift (bench-confirmed: no
`0F` broadcast, no VFO-B broadcast). There's no event that surfaces all the gaps,
so the event-only approach leaves the mirror incomplete. (It remains a valid
*supplement* and is subsumed by the poll.)

### Disruptive VFO-B read via the select-dance (`07 01` → read → `07 00`)

Rejected, and mooted. It would briefly move the operating VFO (visible, racy) just
to read VFO-B. The bench confirmed `25 01` reads the **unselected** VFO directly —
no select-dance needed — so this is unnecessary.

### A generic polling framework now

Rejected per the project's "build specific, not generic" lesson. We do **not**
build a configurable poll-scheduling engine for the not-yet-built full-control
SPA. We build the concrete Icom mirror (the four reads we need) but give it an
**extensible shape** — a rigdef read-list you append to — so a future field is a
rigdef entry, not an engine change. Shape extensible, scope minimal.

## Consequences

- **Full Icom mirror, parity with Yaesu.** VFO-A/B, mode + data flag (USB-D now
  visible), and split all track the rig. The single point where SM mirrors a rig
  is no longer protocol-dependent in what it can show.
- **Cleaner mode read.** `26 00` returns base mode + data flag + filter in one
  frame, replacing the `04` + `1A 06` pair; `25 01` gives VFO-B with no
  select-dance. The IC-7300 READ snapshot is rebuilt around `25`/`26`/`0F`.
- **The poll doubles as an active liveness probe.** Today an idle rig and a dead
  rig look identical until the ~5 s liveness timeout. A poll that expects a reply
  each interval turns a missing-reply pattern into the disconnect signal — better
  detection than passive silence, not worse.
- **New config knobs**, clamped, `bridge.timeouts.*` style: `civ_poll_interval_ms`
  (steady-state poll cadence) and `civ_poll_quiet_ms` (defer a poll tick if a
  Transceive frame arrived within this window — the collision back-off). Defaults
  chosen so a missed poll is invisible.
- **Added serial traffic on Icom only**, and small: a handful of tiny reads at a
  low rate, deferred while the bus is mid-burst. Collision is benign and
  self-healing (bench-proven), so even a dropped poll costs nothing.
- **No change to the daemon's stateless posture.** The poll fires the rigdef READ
  list, decodes, and publishes `rig-state` SSE exactly as a push does — the mirror
  still lives in the SPA's `catState` (ADR 0009), fed by push + poll. The daemon
  does **not** gain a persistent rig-state cache; the "no cache beyond the scoped
  exceptions" invariant holds.
- **Icom-scoped, behind the `icom_civ` seam.** Yaesu's push-only path is
  untouched. The poll list is rigdef data, so onboarding a later Icom — or adding
  AGC/IPO/ATT mirroring when the full-control SPA lands — is a rigdef edit, not
  engine code (the same "add a rig = rigdef only" property as ADR 0034).
- **Slow-field latency drops** from the ~10 s liveness probe to one poll interval;
  fast fields stay instant via push.

## Triggers to revisit

- **If an older/slower Icom doesn't support `25`/`26`**, that rigdef falls back to
  `03`/`04` + `1A 06` and the event-triggered reads — a per-rigdef read-list
  change, no redesign.
- **If poll traffic causes jitter or contention** on a specific rig, tighten the
  cadence / widen the quiet-bus back-off per rigdef, or drop fields from its poll
  list.
- **If the full-control SPA lands** and needs many more mirrored fields, revisit
  whether the flat rigdef read-list still scales or whether the generic
  poll-scheduling model we deferred here is finally warranted.
- **If an operator reports the poll masking a disconnect**, revisit the liveness
  interaction (the poll should *sharpen* disconnect detection, not hide it).

## References

- ADR 0034 (CI-V codec protocol seam + wait-for-ACK) — this revises its
  "push-only, NO polling" stance **for Icom**; the codec seam itself stands.
- ADR 0019 (read-only bridge / confirm-by-push), ADR 0009 (CAT state
  decomposition — the mirror lives in `catState`), ADR 0013 (narrow daemon scope).
- Bench: `cmd/civ-probe -mirror` / `-listen` (2026-06-15) — confirmed the
  `25`/`26`/`0F` reads and characterised the benign, self-healing collision.
- `internal/cat/rigs/icom-ic7300.json` (READ / poll read-list),
  `internal/bridge/pipeline.go` (readLoop + `writeSnapshotReads`),
  `internal/types/bridge.go` (`bridge.timeouts.*` knobs).
- `docs/v2-design/bridge.md` — the subsystem design doc (poll-loop mechanism to be
  detailed there on implementation).
