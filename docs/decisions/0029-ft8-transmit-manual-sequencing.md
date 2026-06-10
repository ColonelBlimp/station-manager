---
number: 0029
title: FT8 transmit — manual-sequenced first, daemon-owned TX
status: Accepted
date: 2026-06-06
---

# 0029 — FT8 transmit — manual-sequenced first, daemon-owned TX

## Context

FT8 in SM is **receive-only** today: the live pipeline captures audio, go-ft8
decodes it, and each decode is logged but is explicitly **not** a QSO (ADR 0024).
That is a dead end for actually *operating* FT8 — you cannot complete a contact,
and a logging app that cannot log the QSOs the operator makes on his most-used
digital mode is incomplete. FT8 transmit was parked as "not in v1"; this ADR
reverses that and commits to building it.

Two facts shape the design. First, the linked library draws a clean line:
`go-ft8`'s `EncodeStandardMessage` returns the 79-symbol tone sequence and its
doc comment states it "deliberately stops at FT8 symbols — audio generation,
transmit scheduling, PTT, and device I/O belong outside this package." So the
hard, silently-wrong-if-botched protocol layer (packing, CRC, LDPC) is
library-owned and round-trip-verified; everything from tones onward is SM's.
Second, SM already has the safety-critical half of TX: the ADR 0027 tune
controller is a daemon-owned TX state machine with a guaranteed stop
(hard auto-off, release-on-disconnect, single-flight) and the invariant that
`tx_on`/`tx_off` are never `exposed`.

A standard FT8 contact is a fixed six-message ladder (CQ → call → report →
R-report → RR73 → 73), one message per UTC-aligned 15 s slot, and **each
outgoing message is mechanically determined by the decode just received** —
there is no judgement in advancing it, only a lookup. That property is what
makes the manual-vs-auto question a real fork rather than a cosmetic toggle:
the *plumbing* (encode → modulate → audio-out → PTT → slot timing) is identical
either way; the only difference is whether a human or a daemon state machine
decides to send each rung.

## Decision

Build FT8 transmit as a **daemon-owned capability** that reuses the ADR 0027
TX-safety discipline, layered tones → GFSK audio → audio-output device → PTT →
slot timing. Sequencing is **manual first**: the operator advances each rung of
the exchange (double-click a decode → the correct next message is pre-filled →
operator enqueues it for the next slot); the daemon transmits exactly the one
message it was handed, on the chosen slot, with a guaranteed unkey. The QSO is
logged through the normal `qsoservice` submit path when the exchange completes.

**Auto-sequencing** (the daemon parsing decodes and walking the ladder
unattended to completion) is explicitly **deferred to a later ADR** that builds
on this machinery.

### Clear-offset picker — data contract

Build step (a) (the per-slot occupancy detector) emits, once per completed RX
slot, an `OccupancyReport`: **decision data for TX-offset selection, not a
spectrogram for display.**

```go
type OccupancyReport struct {
    Slot          SlotRef // which UTC slot this covers (start_utc + even/odd)
    Passband      Band    // {200, 3000} Hz — the span the picker shows
    SignalWidthHz int     // 50 — clear room a TX needs
    Occupied      []Band  // merged busy ranges, ascending; Source decode|energy|both, optional Level 0..1
    Suggested     []int   // ranked clear base offsets (Hz), best first
}
```

The two occupancy tiers collapse into the single `Occupied` list:
`source:"decode"` is `[FreqHz, FreqHz+SignalWidthHz]` — go-ft8's `FreqHz` is the
base-tone (sync) frequency per WSJT-X convention, and an FT8 signal's 8-FSK tones
extend ~50 Hz *upward* from it, so the occupied span runs up from the reported
frequency, not symmetrically around it; `source:"energy"` is a contiguous run of
per-slot FFT bins over the daemon's floor threshold; `"both"` where they overlap. The daemon does all thresholding and merging — the SPA
receives intent (~3–15 bands/slot), not raw spectrum. The SPA inverts `Occupied`
against `Passband` to paint the clear/busy strip and to find selectable gaps
(any gap ≥ `SignalWidthHz`); the operator clicks anywhere in a gap and that
integer becomes the TX base offset, with no daemon round-trip to validate the
pick.

**Ranking is daemon-side.** `Suggested` is a daemon-ranked, best-first list of
clear offsets for one-click slot selection; the SPA treats it as opaque and
never re-ranks, so "what counts as a *good* clear slot" lives in one tunable,
unit-testable place. The heuristic (config-tunable under `ft8.tx.occupancy.*`,
code constants only as fallback defaults) weighs: **clear-margin width** (more
isolation ranks higher), **distance from band edges** (deprioritize near 200/3000 Hz
where filter roll-off and splatter bite), and **centered-in-gap** (prefer the
middle of a wide clear region so a late neighbour doesn't clip the signal).
Conventional FT8 sub-band taste is out of scope for v1 — that's operator
preference, not a clear/busy fact.

**Refinement (2026-06-10): offset hysteresis.** The per-slot scoring chooses the
*initial* recommendation, but the top pick (the ★) then **sticks across slots
while it stays clear** instead of re-optimising every 15 s — which made the ★ hop
to whatever gap was widest that slot even when the operator's spot was still fine.
Each slot, if the previous top pick still passes the guard-margin admission bar
(`offsetClear`), it is floated back to the front of `Suggested`; it only loses the
★ when a signal actually moves into its space. The scoring weights are unchanged
and the rest of `Suggested` still follows in score order — stickiness only governs
which clear offset leads. State is the previous pick carried in the decode loop
for the life of a capture session (no persistence, no new config).

The report carries **no human strings** (consistent with the ADR 0010
`{code, details}` discipline), **no currently-selected offset** (the report is
pure RX observation; the operator's chosen TX offset is separate SPA/operator
state that later feeds the modulator), and **no raw bin strip** in the first cut
(an optional downsampled `Bins []uint8` is a non-breaking later add if an
at-a-glance visual texture is wanted — still not a scrolling waterfall). It rides
the FT8 SSE stream as `ft8-occupancy` with a one-slot hub cache (the ADR 0009
replay pattern) so a SPA connecting mid-slot gets the last report immediately
rather than waiting up to 15 s.

## Alternatives considered

### Auto-sequence from the start

Match WSJT-X's default: arm TX, then let the daemon answer decodes and run the
exchange hands-free. Rejected as the *first* target because it makes the daemon
transmit on its own decisions, unattended — a far larger trust-and-safety
surface than the tune carrier (one click → one carrier → hard stop). It needs
watchdogs the manual path does not (stop after N unanswered repeats, never
auto-start a fresh CQ cycle without re-arming, abort on operator input) layered
on top of a TX chain that has *not yet been proven on real RF*. Manual is a
strict subset: it exercises the entire encode→modulate→audio→PTT→timing path
with a human in the loop on every transmission, so it de-risks the plumbing
before any autonomy is added. Auto becomes a clean follow-on once the plumbing
is trustworthy — not a thing reopened, a thing layered.

### Full real-time waterfall for TX-frequency selection

Render a WSJT-X-style scrolling spectrogram so the operator eyeballs a clear
audio offset. Rejected as the mechanism: occupancy is *data*, not pixels — the
daemon already knows every decoded signal's `FreqHz`, and a single averaged
per-slot FFT (via the retained CGO-free `internal/audio` FFT) catches the
sub-decode energy that decode-occupancy alone misses, at one small array per
15 s slot rather than a 10 fps streamed image plus Canvas animation. A
clear-offset **picker** built on per-slot occupancy gives ~95% of the
waterfall's TX-selection value for a fraction of the cost. A visual waterfall
remains a possible *later* operator-facing nicety, not a prerequisite for TX.

**Refinement (2026-06-07, dogfooding step a):** the step-(e) picker UI is a
**clickable occupancy *strip*** — a *static* per-slot view (busy bands shaded,
clear gaps selectable) rendered from the same `OccupancyReport`, shown alongside
the ranked **Clear Slots** list (click a chip or a clear point to set the TX base
offset). This is still "data, not pixels" — a single per-slot array, not a 10 fps
scrolling spectrogram — so the *scrolling waterfall* stays deferred; only its
time-history dimension is given up. Crucially, SM **enforces good practice** where
WSJT-X does not: WSJT-X lets the operator double-click *anywhere*, including onto
an occupied signal; SM only offers clean spots and the daemon TX gate **refuses or
snaps** an offset that overlaps current occupancy. Enforcement is best-effort *at
pick time* — occupancy re-evaluates each slot, so a station can still land on the
operator mid-exchange; SM guards the choice, not the whole QSO. A configurable
**guard margin** (`ft8.tx.occupancy.guard_margin_hz`, default 10 Hz, 0 = off)
keeps even the suggestions off a neighbour's edge.

### Client-side modulation (browser generates the audio)

Have the SPA turn tones into PCM and stream it down for playback. Rejected:
audio I/O to the rig is daemon-side (the capture seam already is), TX keying
must be daemon-owned for the guaranteed stop, and slot timing is daemon-owned;
splitting the waveform generation into the browser fragments a path that has to
be atomic and safety-bounded. The daemon owns the whole TX chain.

### Stay receive-only

Do nothing; keep FT8 as a decode-and-log curiosity. Rejected — it does not let
the operator complete a contact, which is the entire point of running the mode.

## Consequences

- **Invariant evolution.** "A decode is not a QSO" (ADR 0024) becomes "a
  *completed exchange* is a QSO." The FT8 subsystem gains a path that produces a
  `types.Qso` and submits it via `qsoservice`. Direction is one-way —
  `internal/ft8` imports `qsoservice`, never the reverse — so narrow-daemon-scope
  (ADR 0013) holds by import graph: the log/forward packages still do not import
  FT8.
- **New audio-output path.** SM only captures today; TX adds a malgo *output*
  device (mirror of the capture seam — `//go:build cgo`, fail-soft, probe-listed
  device index, operator never hand-types an id per the KISS rule). Live TX, like
  live decode, requires a CGO build; the static default leaves it idle.
- **New DSP, offline-verifiable.** A CGO-free GFSK modulator turns the 79 tones
  into ~12.6 s of 12 kHz int16 PCM at a chosen offset. The whole encode→modulate
  chain is validated with **zero RF** by feeding generated audio back through the
  shipped decoder and asserting round-trip, before any audio device or PTT exists.
- **TX keying reuses ADR 0027.** PTT for an FT8 slot extends the daemon-owned
  controller (key at slot start, unkey at slot end + on disconnect + single-flight);
  `tx_on`/`tx_off` stay controller-only and never `exposed` (the load-time schema
  invariant from the 2026-06-06 cat review).
- **New SPA surface** in the operator's FT8 stream: the live decode feed (Band
  Activity), the clickable occupancy **strip** + ranked **Clear Slots** list for
  TX-offset selection (see the picker refinement above), the next-message row, an
  Enable-TX / hold control, and the slot timer. This is the larger half of the
  work.
- **Build order (RX-safe first):** (a) per-slot occupancy detector (RX-only,
  useful immediately) → (b) modulator + offline round-trip → (c) audio-output
  device → (d) PTT/slot controller → (e) manual sequencer + logging + the
  interactive picker (clickable strip + list, daemon-enforced no-overlap), with
  the SPA growing alongside. Each step is independently testable; actual RF only
  enters at (d), when PTT keys the rig — (c) drives a sound card, so it is still
  RF-safe to bench. **(d) shipped 2026-06-09 — see ADR 0030** (PTT + slot-timing
  controller; first real RF, reachable only from the gated `ft8-tx-probe -key`).
  **(a), (b), and (c) shipped 2026-06-07.** (a) occupancy + SSE
  + SPA readout; (b) GFSK modulator round-trip-verified; (c) `internal/audio/playback`
  — a malgo S16/12 kHz/mono output device mirroring `internal/audio/capture`
  (`//go:build cgo`, fail-soft, probe-listed via `cmd/ft8-tx-probe`). `Play` is
  non-blocking and returns a done channel; **the caller owns the stop** — the
  guaranteed-stop discipline the (d) controller inherits. `ft8.tx.device` config
  field selects the output device.
- **Standard messages only, initially.** `EncodeStandardMessage` covers standard
  structured messages (callsign + grid/report exchanges) — enough for a normal
  CQ→73 contact. Free text, compound/portable calls, and telemetry are not yet
  encodable and are out of scope for this first cut.
- **Multi-ADR, multi-session feature.** This is the largest capability in the
  project; each layer above may spawn its own ADR (audio-out device model, the
  sequencer/auto-seq design, the QSO-completion → log mapping).

## Triggers to revisit

- **If manual sequencing proves too slow to keep up with FT8's 15 s cadence in
  practice**, promote auto-sequence sooner (it is already the planned follow-on,
  not a reversal).
- **If the operator needs free-text, compound/portable, or contest-exchange
  messages**, the `EncodeStandardMessage`-only limit forces either an upstream
  go-ft8 encoder extension or an SM-side packer — reopen the message-scope cut.
- **If a second operating position / contest topology lands** (the N-writers
  model), unattended auto-sequencing and TX arbitration across stations become
  pressing and this single-position manual model needs revisiting.
- **If the clear-offset picker proves insufficient** for reading band congestion
  in the field, the deferred visual waterfall comes back onto the table.

## References

- ADR 0024 — FT8 external library + live pipeline (decode≠QSO; the seam this evolves).
- ADR 0027 — Tune-carrier control via daemon-owned TX state machine (the TX-safety pattern reused).
- ADR 0021 — FT8 as an SM subsystem (sibling-isolation rule, parked).
- ADR 0026 — Rig command path (exposed-ops model; `tx_on`/`tx_off` never exposed).
- ADR 0013 — Daemon owns bridge as subsystem (narrow-daemon-scope, import-graph enforcement).
- `docs/v1-analysis/invariants.md` — "a decode is not a QSO" / enrichment-never-blocks-logging.
- `internal/audio/realfft.go` — retained CGO-free FFT (occupancy detector + future waterfall).
- `internal/ft8/{ring,scheduler}.go` — sample ring + UTC slot timing reused by the TX half.
