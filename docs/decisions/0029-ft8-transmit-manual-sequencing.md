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
- **New SPA surface** in the operator's FT8 stream: decode list with clickable
  callsigns, the occupancy / clear-offset picker, the next-message row, an
  Enable-TX / hold control, and the slot timer. This is the larger half of the
  work.
- **Build order (RX-safe first):** (a) per-slot occupancy detector (RX-only,
  useful immediately) → (b) modulator + offline round-trip → (c) audio-output
  device → (d) PTT/slot controller → (e) manual sequencer + logging, with the SPA
  growing alongside. Each step is independently testable; RF only enters at (c).
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
