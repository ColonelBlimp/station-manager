---
number: 0036
title: Rig profile owns its audio devices — per-rig, per-direction, name-based
status: Accepted
date: 2026-06-15
---

# 0036 — Rig profile owns its audio devices — per-rig, per-direction, name-based

## Context

ADR 0028 (rig profiles) modelled a rig's audio as a single field,
`RigAudioConfig{ Device string }`, on the premise *"one physical codec → one
choice; the daemon resolves capture/playback direction internally"* (the KISS
goal: the operator never hand-types a hardware id, and switching
`default_rig_id` re-binds the whole rig). The actual FT8 device values, though,
stayed in the **global** `ft8.device` (capture) and `ft8.tx.device` (playback)
fields; `ActiveFt8()` only ever projected `rig.audio.device` onto the *capture*
side, and only when set.

A 2026-06-15 session bringing FT8 up on the borrowed IC-7300 disproved the
premise and exposed the model as broken on three counts:

1. **Capture and playback enumerate independently, with different indices for
   the same physical codec.** The IC-7300's USB codec (PCM2901) is capture index
   **4** but playback index **2**. A single `Device` field cannot hold both, and
   the daemon cannot derive one index from the other — so "resolve direction
   internally" is impossible.
2. **The device values aren't in the profile at all** — they live in global
   `ft8.*`. So changing the active rig does **not** re-bind audio: switch from
   the IC-7300 to another rig and FT8 still points at the IC-7300's devices,
   silently wrong. The operator must remember and hand-edit raw indices per rig
   — exactly the friction ADR 0028 set out to remove.
3. **Indices are fragile and ambiguous.** Two rigs' codecs (PCM2901 vs
   PCM2903C), and indices that shift across replug/reboot, made us pick the wrong
   device twice during the session. An index is not a stable identifier.

And it generalises beyond FT8: any future device-bound feature (the planned
full-CAT-control SPA's audio, recorded-audio playback, a panadapter IF stream, a
CW skimmer input) hits the same wall. The profile owns the *CAT port* per rig
but nothing else hardware-bound.

## Decision

The **rig profile owns its audio devices**, modelled as **named devices, per
direction** — `RigAudioConfig{ Capture string; Playback string }`, holding
device **names** (not indices), resolved to the live capture/playback index at
acquire time by matching the enumerated device lists. The active rig's `Capture`
feeds every capture feature (FT8 RX, future), its `Playback` every playback
feature (FT8 TX, future). Switching `default_rig_id` re-binds all of it. The
global `ft8.device` / `ft8.tx.device` (and the old single `RigAudioConfig.Device`)
become deprecated legacy fallbacks, used only when the active rig declares no
named devices. This amends ADR 0028's audio model; the rest of ADR 0028 stands.

## Alternatives considered

### Keep the single `Device` field (ADR 0028 status quo)

Rejected — disproven by the session. Capture and playback are independent device
lists with different indices for one codec, so a single field cannot address
both and the daemon cannot infer the missing direction. This is the cautionary
tale, not a hypothetical.

### Two **index** fields (`capture_index` + `playback_index`)

Rejected. It fixes the one-field problem but keeps the *index* fragility:
indices drift across replug/reboot and collide in meaning between rigs (the
PCM2901-vs-PCM2903C mixups). A name is a stable identifier the operator can
recognise; an index is a transient enumeration artifact.

### Per-feature device fields (`ft8_rx`, `ft8_tx`, `panadapter_in`, …)

Rejected as over-fit. A rig overwhelmingly has **one** codec serving all
features per direction, so `Capture` + `Playback` already covers FT8 RX/TX and
any future feature on that codec. Pre-modelling N feature fields is speculative
generality (cf. "build specific, not generic"); a genuine multi-codec rig is a
*trigger to revisit*, not a reason to inflate the schema now.

### Leave device config global (`ft8.*`)

Rejected — that *is* the bug. Global devices don't follow the active rig, so a
rig switch silently points features at the previous rig's hardware.

## Consequences

- **Switching `default_rig_id` re-binds capture + playback atomically** —
  ADR 0028's "switch rig = one change" finally holds for device-bound features,
  not just the CAT port.
- **The operator picks each rig's audio once, by friendly name**, in the
  config-SPA rig-profile editor — no raw indices, and the choice survives
  replug/reboot (name resolved fresh each acquire). Restores the KISS goal
  (`feedback_kiss_frictionless_for_operator`).
- **`ActiveFt8()` completes its half-wiring:** it resolves `rig.audio.Capture` →
  `Ft8Config.Device` and `rig.audio.Playback` → `Ft8Config.TX.Device` (both
  directions, name→index), instead of capture-only.
- **Name resolution is runtime + fail-soft:** at device-acquire the daemon
  matches the stored name against the enumerated capture/playback lists (the same
  enumeration `cmd/ft8-capture-probe -list` / `GET /v1/hardware` expose). An
  unmatched name logs and leaves the subsystem idle (same posture as a missing
  capture device today), never crashes.
- **Legacy fields stay as fallback, no auto-migration.** `ft8.device` /
  `ft8.tx.device` / the old `RigAudioConfig.Device` are honoured only when the
  active rig has no named devices. Index→name auto-migration is *not* attempted
  (it would need an enumeration the loader can't safely assume); the config-SPA
  writes the named fields when the operator picks, and the legacy fields can be
  dropped from a profile once it has names.
- **Implementation is the config-SPA rig-profile-editor workstream** (ADR 0028
  pieces 2/3, deferred): the data model (this ADR) lands first; the picker UI
  consumes `GET /v1/hardware` device names. The model being wrong — not just the
  UI being absent — is what this ADR fixes.
- Until then, the loose `ft8.device` / `ft8.tx.device` remain the working knobs
  (per the 2026-06-15 fix that stopped an empty per-rig audio from clobbering
  them) — adequate for a single-rig dev/dogfood setup, inadequate the moment two
  rigs are swapped.

## Triggers to revisit

- **A rig that needs different capture devices per feature** (e.g. a separate
  IF/panadapter USB stream alongside the AF codec) → extend `RigAudioConfig` to a
  per-feature map *then*, not before.
- **Name collisions** (two identically-named codecs — e.g. two IC-7300s) → the
  name needs disambiguation (USB path / serial), a resolver refinement.
- **A non-audio per-rig device binding appears** (e.g. a dedicated PTT serial
  line separate from CAT) → the "profile owns all per-rig device bindings"
  principle extends to it; generalise the profile's device map then.

## References

- **ADR 0028** (rig profiles) — this amends its `RigAudioConfig` audio model; the
  catalogue / `default_rig_id` / `ActiveBridge`/`ActiveFt8` machinery stands.
- ADR 0029 / 0030 (FT8 TX — consumes the playback device), ADR 0035 (FT8
  state-mirror poll — sibling Icom work from the same session).
- `internal/types/rig.go` (`RigConfig.Audio` / `RigAudioConfig`),
  `internal/config/config.go` (`ActiveFt8` / `ActiveBridge` resolution),
  `internal/types/ft8.go` (legacy `ft8.device` / `ft8.tx.device`),
  `internal/hardware` + `GET /v1/hardware` (device enumeration the name resolver
  reuses).
- `docs/v2-design/config.md` §10 2e (name-based audio device — was deferred as a
  nicety; this promotes it to required and extends it to per-direction +
  in-profile).
- Cautionary tale: the 2026-06-15 IC-7300 FT8 bring-up — codec = PCM2901
  (capture 4 / playback 2), global `ft8.*` devices that didn't follow the active
  rig, and the wrong device picked twice off shifting indices.
- Memory `feedback_kiss_frictionless_for_operator` (the KISS goal restored).
