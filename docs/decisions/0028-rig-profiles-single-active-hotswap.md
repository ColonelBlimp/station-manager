---
number: 0028
title: Rig profiles with single-active hot-swap
status: Accepted
date: 2026-06-05
---

# 0028 — Rig profiles with single-active hot-swap

## Context

The daemon has never modelled "a rig" as a first-class thing. A rig is really a
`{driver, serial port, audio capture device}` triple, but those facts are smeared
across loose, single-valued config fields that live on the *bridge subsystem*
rather than on a rig identity: `BridgeSerialConfig.Port` (the device node) and
`BridgeCatConfig.Driver` (the rigdef id) in `internal/types/bridge.go`, plus
`Ft8Config.Device` (the audio device) in `internal/types/ft8.go`. There is
exactly one of each, so the daemon's model is "the bridge has a port and a
driver," when reality is "*this physical rig* is a (driver + port + audio) bundle,
and the operator may have several plugged in at once."

A half-built scaffolding for exactly this already exists (session 31): `types.RigConfig`
(`{id int64, model, port, overrides}` — "the operator's DTO for one rig installed at
their station"), `Config.DefaultRigID` (whose comment reads *"the rig's display fields
… live in `cfg.Rigs` when CAT lands"*), and the `default_rig` join in
`handler_config.go` (deferred *"until CAT lands and `cfg.Rigs` is populated"*). The
catalogue this ADR introduces is the thing that scaffolding was waiting for.

This bites a concrete workflow. The operator runs an FTdx10 for phone/CW and an
FT-710 for FT8, both attached to the same PC on different USB device nodes.
Switching between them today means editing `config.json` and restarting the
daemon — there is no hot-swap. The operating-mode toggle shipped in the SPA
(phone ⊕ ft8, `LoggingCard`) makes the gap sharper: flipping the mode that the
operator associates with a *different rig* can't actually move the CAT/audio
connection to that rig.

Two mechanisms already in the tree make a fix tractable. The pipeline supervisor
(ADR 0020) already tears a pipeline down and reopens the port on fault, including
the port-appears-later case — most of a hot-swap. The identity-verification
write-gate (ADR 0026 follow-up, "H2") already blocks writes when the bound driver
doesn't match the rig's `ID;` response — a natural safety net for "operator
swapped the cable to the wrong radio."

## Decision

> **Audio model amended by ADR 0036 (2026-06-15).** The single `audio.{device}`
> field below was disproven on the IC-7300: capture and playback enumerate
> independently with different indices for one codec, and the per-rig field was
> never wired for playback — so it can't carry a rig's audio. ADR 0036 replaces
> it with per-direction, **name-based** devices (`audio.{capture, playback}`)
> owned by the profile. The catalogue / `DefaultRigID` / `ActiveBridge`/`ActiveFt8`
> machinery in this ADR stands; only the audio sub-shape changes.

Introduce a **rig catalogue** — `Config.Rigs`, a list of the existing
`types.RigConfig` (reused, not a parallel struct), each extended with a per-rig
`audio` block (`{device}`). A rig is identified by its `model` (the rigdef id),
`port`, and `audio.device`. **`Config.DefaultRigID` is the active-rig selector**:
the rig it points at is the one the bridge binds *and* the one QSOs attribute to —
in the single-active model these are the same choice, so there is no separate
`active_rig` field. Exactly **one** rig is active at a time.

Switching is ultimately a **runtime hot-swap** — a select operation tears down the
current bridge pipeline and binds the newly-active rig, letting the supervisor bring
it up. The bridge and FT8 subsystems stop owning loose `port`/`driver`/`device`
fields and instead consume the active rig's values, resolved from the catalogue.

Concurrent multi-rig within one daemon is explicitly **out of scope** (see below).

This lands in phases (see `docs/v2-design/rig-profiles.md`): the **config model**
first (switch = edit `default_rig_id` + restart), then the hardware-discovery
endpoint, picker UI, and runtime hot-swap together with the config SPA.

## Alternatives considered

### Status quo — single loose port/driver on the bridge

Keep one `Serial.Port` + one `Cat.Driver`. Rejected: it is the problem. It can't
represent more than one attached rig and offers no path to switching without a
restart.

### Config-only multi-rig (catalogue, but switch by editing config + restart)

Add the profile catalogue but keep selection in `config.json`, requiring a daemon
restart to change the active rig. Rejected: it models the rigs correctly but
doesn't deliver the thing the operator actually asked for — *hot*-swap. The
supervisor's teardown/reopen machinery already makes a live re-bind cheap, so
paying a restart per switch is needless friction.

### A parallel `RigProfile` struct + a separate `active_rig` selector

The first sketch of this ADR invented a new `RigProfile{driver, serial, audio}`
struct in a `map[string]RigProfile` keyed by string ids, plus a new `active_rig`
string selector. Rejected on discovery of the session-31 scaffolding: it would
duplicate `types.RigConfig` (the "don't build parallel structs" anti-pattern) and
ignore the already-half-built `default_rig` join and `DefaultRigID` selector. Reusing
`RigConfig` (just adding `audio`) lights up the existing join, and folding the active
selector into `DefaultRigID` removes a redundant concept — in the single-operator,
single-active model the rig you're bound to *is* the rig you're logging on. Keeping
them separate would only matter to bind rig A while attributing QSOs to rig B, which
no single-operator workflow needs; split them if that ever appears.

### Concurrent multi-rig in one daemon

Two (or more) rigs bound and live simultaneously — e.g. the FTdx10 holding phone
state while the FT-710 decodes FT8 — within a single daemon. Rejected for v1, and
on principle for the single-operator case: it is a large change (multiple hubs,
per-rig SSE streams, every rig event tagged with its source, N tune controllers,
N identity gates) and it duplicates a capability the **topology already provides**.
The concurrent case is served by running a *second daemon* with its own config and
its own rig, forwarding its QSOs to a networked master daemon — the N-writers +
master-sink shape SM is already built around (see the field-master topology). One
operator running phone ⊕ ft8 does one mode at a time, which single-active fits
exactly; genuine simultaneity is a second-process problem, not a multiplex-one-
process problem. Keeping it out of the daemon preserves narrow single-daemon scope.

## Consequences

- A rig becomes a first-class entity, reusing `types.RigConfig` (+`audio`). `port`,
  `driver`, and the FT8 audio device move off the bridge/ft8 blocks into the catalogue
  entry; the bridge and FT8 subsystems consume the *active* rig's values rather than
  carrying loose fields. The `default_rig` join in `handler_config.go` (and
  `DefaultRig` in the config response) becomes live for the first time.
- `DefaultRigID` gains a second meaning — the **active/bound** rig, not just the
  logging-attribution default. Acceptable because they coincide in the single-active
  model; documented so a future reader doesn't treat them as separable without the
  split-then call-out above.
- A runtime select surface is needed (an endpoint + SPA control) that re-binds the
  bridge pipeline. This leans on the existing supervisor teardown/reopen path; the
  new work is "rebind to a different (port, driver) on command," not new
  port-lifecycle code.
- All per-rig live state — the tune mode+power restore snapshot (ADR 0027), the
  hub replay caches (ADR 0009/0010), identity confirmation — must be torn down and
  rebuilt cleanly across a swap. A half-swapped state that leaks the previous rig's
  snapshot into the new rig would be a safety bug, so the swap must be as clean as a
  disconnect.
- The identity write-gate composes for free: bind the wrong profile to a physical
  rig and writes stay blocked until the `ID;` response matches the driver.
- Config migration: an existing single-rig `bridge.serial.port` + `bridge.cat.driver`
  (+ `ft8.device`) config is migrated at load into a single `RigConfig` (id 1) with
  `default_rig_id = 1`. Operators must not have to hand-rewrite config.json. The
  catalogue is authoritative — a resolved active rig wins over any stale loose field
  left on disk.
- The operating-mode toggle (phone ⊕ ft8) and rig selection stay **orthogonal** for
  now — selecting a rig and selecting an operating mode are two independent choices.
  Auto-binding a mode to a rig is deliberately not part of this decision (it would
  conflate two concerns and not every operator maps modes to rigs the same way).
- ADRs written under the single-rig assumption (0013, 0019, 0026, 0027) are not
  invalidated — they describe how the *one bound* rig behaves, which is unchanged.
  This ADR sits above them: it decides *which* rig is bound and how that changes.

## Triggers to revisit

- If the operator genuinely needs two rigs *live at once on the same host* (e.g.
  decoding FT8 on one radio while logging phone on another, in one daemon),
  reconsider the concurrent-multi-rig alternative — but first weigh it against
  running a second daemon + forwarding, which is the intended answer.
- If runtime hot-swap proves to leak per-rig state across a switch in a way the
  disconnect path doesn't already cover, the "swap == clean disconnect + connect"
  assumption needs revisiting.
- If a second operator / multi-tenant shape ever lands (ADR 0016), profile
  ownership and per-operator rig catalogues become a new question.

## References

- `docs/v2-design/rig-profiles.md` — the long-form implementation plan (phased:
  config model now, discovery endpoint + picker UI + runtime hot-swap with the
  config SPA).
- Supersedes the single-rig config shape in ADR 0013 (daemon owns bridge as
  subsystem) and ADR 0019 (bridge subsystem v1 design) — extends, does not
  invalidate.
- Builds on ADR 0020 (pipeline supervisor) for the teardown/reopen machinery.
- Composes with the identity write-gate (ADR 0026 inbound command path) and the
  tune restore snapshot (ADR 0027) — both are per-rig state a swap must reset.
- Reused/extended type: `internal/types/rig.go` (`RigConfig` + new `RigAudioConfig`).
  Loose fields being superseded: `internal/types/bridge.go` (`BridgeSerialConfig`,
  `BridgeCatConfig`), `internal/types/ft8.go` (`Ft8Config.Device`). Selector:
  `Config.DefaultRigID`.
- Topology context for the rejected concurrent alternative: the N-writers +
  master-sink field-master shape.
