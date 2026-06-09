---
number: 0030
title: FT8 transmit step (d) — PTT + slot-timing controller
status: Accepted
date: 2026-06-09
---

# 0030 — FT8 transmit step (d) — PTT + slot-timing controller

## Context

ADR 0029 committed to daemon-owned FT8 transmit, built RX-safe first in five
steps. Steps (a)–(c) shipped with **zero RF**: the occupancy detector + picker,
the GFSK modulator (`ft8.EncodeToSlot` → a full 15 s int16 slot waveform), and
the `internal/audio/playback` output device (`Player.Play` is non-blocking,
returns a done channel, caller owns the stop). Step (d) is the first increment
that **keys a transmitter**: align to a UTC slot boundary, key PTT, play the
waveform, unkey — with a guaranteed stop.

The work straddles two subsystems. Audio output, the modulator, and slot timing
live in `internal/ft8`. PTT keying lives in `internal/bridge`: it owns the
serial client, the `tx_on`/`tx_off` commands (deliberately **not** `exposed` per
ADR 0026), the rig-identity gate, and disconnect-release. The two packages must
not import each other — narrow-daemon-scope (ADR 0013) and the parked-but-live
FT8↔bridge sibling-isolation rule (ADR 0021). SM already has the safety-critical
half built: the ADR 0027 tune controller is a daemon-owned, guaranteed-stop TX
state machine (hard auto-off, release-on-disconnect, single-flight). Step (d)
should reuse that discipline rather than grow a second, unproven one.

## Decision

Add an FT8 TX controller as a thin orchestrator in `internal/ft8` that drives
PTT through a small **`TxKeyer` interface** (the same injection seam as
`captureSource`); `internal/bridge` implements it by **generalising the tune
controller's guaranteed-stop machinery**, and `cmd/smd` injects the bridge keyer
into the FT8 service. The bridge owns keying + the hard auto-off backstop +
release-on-disconnect + identity gate + a **single-flight shared with tune** (one
keyed transmission of any kind at a time); `internal/ft8` owns slot-boundary
timing and audio playback, calling key/unkey around one slot's waveform.

Keying sets and restores a configured FT8 data mode (mirroring how tune sets and
restores RTTY); TX **power is left at the operator's setting** (no tune-style
clamp — FT8 is a normal operating mode, not a reduced-power carrier). Step (d) is
exercised only from a **gated `cmd/ft8-tx-probe -key`** bench path; no SPA can
trigger TX until step (e). `tx_on`/`tx_off` stay unexposed.

## Alternatives considered

### Standalone guaranteed-stop controller built inside `internal/ft8`

Give the FT8 controller its own auto-off timer, single-flight, and
disconnect-release, with the bridge exposing only raw `KeyTx`/`UnkeyTx`. Rejected
as the primary home of the safety logic: it duplicates the exact machinery ADR
0027 already proved on real RF, and — worse — a single-flight *local* to FT8
wouldn't know a tune carrier is up, so FT8 could key on top of a tune (or vice
versa). The guaranteed stop and the single-flight must live where the rig client
and disconnect signal already are: the bridge. The interface seam keeps the
import graph clean without moving the safety core.

### `internal/ft8` imports `internal/bridge` directly

Simplest wiring — call `bridge.Service` methods straight from the FT8 controller.
Rejected: it couples the two sibling subsystems at the import-graph level, the
exact thing ADR 0013/0021 forbid; boundary tests would fail. The `TxKeyer`
interface costs almost nothing and preserves the invariant (cmd/smd does the
wiring, as it already does for every service).

### Don't touch rig mode — require the operator to pre-set DATA-U

Key `tx_on` and play audio assuming the rig is already in the right data mode.
Rejected for the first RF cut: keying with the rig in a voice/CW mode transmits a
wrong or empty signal, and "did you remember to switch modes" is exactly the
silent-footgun the tune controller's snapshot/restore avoids. Setting + restoring
a configured data mode mirrors the proven tune path and makes a mis-set rig
impossible. (The configured mode may be left empty to opt out per rig, for
operators who manage mode themselves.)

### Clamp TX power like tune

Reuse the 20 W/40 W tune clamp. Rejected: a tune carrier is a deliberately
reduced-power nuisance-minimising signal; FT8 is the operator working stations at
their normal power. Clamping would cripple the mode. Power stays whatever the
operator has set.

### Trigger the first TX from the SPA

Wire the SPA Enable-TX control straight to step (d). Rejected as premature: the
SPA TX surface + sequencing is step (e), and first RF wants a human at a CLI with
a dummy load, behind an explicit flag — not a button a stray click can hit. The
probe path validates the controller before any UI can reach it.

## Consequences

- **Import graph holds.** `internal/ft8` gains a `TxKeyer` interface and a
  controller; it still does not import `internal/bridge`. `cmd/smd` wires the
  bridge keyer in. Boundary tests stay green.
- **The bridge's TX safety core is generalised, not duplicated.** Tune and FT8
  become two callers of one guaranteed-stop, single-flight keyed-TX path —
  mutually exclusive at the hardware level, both releasing on disconnect, both
  backed by a hard auto-off. `tx_on`/`tx_off` remain controller-only and
  unexposed (the load-time schema invariant).
- **Two independent stop guarantees per FT8 slot.** `internal/ft8` unkeys when
  playback finishes (the primary stop); the bridge's hard auto-off (slot length +
  a small margin) is the backstop if the FT8 controller ever goes silent. A
  closed tab / killed probe / hung audio device still cannot strand the rig keyed.
- **New config.** `ft8.tx.mode` (the rig's data-mode literal to set+restore;
  empty = leave mode untouched). No new power knob.
- **RF reachable from the bench.** `cmd/ft8-tx-probe -key` keys the rig; the
  default (no `-key`) stays audio-only/RF-safe as today. Live TX still requires a
  CGO build (audio output is CGO).
- **Step (e) unblocked.** The sequencer, QSO logging, and SPA TX controls build
  on this controller; they decide *which* message and *when*, then hand it to the
  same key→play→unkey path.

## Triggers to revisit

- **If a rig needs PTT keyed by a method other than CAT `tx_on`/`tx_off`** (a
  hardware PTT line, RTS/DTR, or VOX), the `TxKeyer` implementation grows a
  second strategy — the interface seam already isolates that from `internal/ft8`.
- **If tune and FT8 TX ever need to be genuinely concurrent** (they cannot today —
  one rig, one PTT), the shared single-flight assumption breaks and the bridge TX
  session model needs rethinking.
- **If audio-vs-PTT timing on real hardware needs a pre-key lead or post-audio
  tail** beyond a fixed margin, the controller's slot-alignment constants become
  config (mirroring `bridge.tune.restore_settle_ms`).
- **If `EncodeStandardMessage`'s standard-message-only limit blocks a needed
  exchange**, that reopens ADR 0029's message-scope cut, not this one.

## References

- ADR 0029 — FT8 transmit (manual-sequenced first); this is its step (d).
- ADR 0027 — Tune-carrier control; the guaranteed-stop machinery generalised here.
- ADR 0026 — Rig command path; `tx_on`/`tx_off` never exposed.
- ADR 0024 — FT8 external library + live pipeline (the `captureSource` seam mirrored).
- ADR 0021 / ADR 0013 — FT8↔bridge sibling isolation / narrow-daemon-scope (import graph).
- `internal/bridge/tune.go` — the controller being generalised.
- `internal/ft8/{modulate,scheduler}.go`, `internal/audio/playback` — the audio + timing pieces.
