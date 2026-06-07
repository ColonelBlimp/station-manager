# FT8 in Station Manager — operator & contributor guide

> **Status (2026-06-07):** Receive — decode + per-slot occupancy — is shipped.
> Transmit is decided (ADR 0029) and partly built: the GFSK modulator and the
> audio-output device are done and bench-verifiable (zero RF); PTT, slot timing,
> and the interactive picker are still ahead. This guide is the single place the
> FT8 picture is captured; keep it current as the TX layers land.

## 1. What it is

SM links **go-ft8** (`github.com/ColonelBlimp/go-ft8`, GPL-3.0-only, a WSJT-X/jt9
derivative) for the FT8 **protocol layer in both directions** — decode (audio →
message) and encode (message → the 79-symbol tone sequence: pack, CRC, LDPC). go-ft8
deliberately stops at tones; **SM owns everything around the protocol**: audio
capture *and* output, the UTC slot scheduler, occupancy analysis, GFSK modulation
(tones → audio), PTT/timing, and sequencing. The architecture decisions are
**ADR 0024** (external library + live RX pipeline) and **ADR 0029** (transmit).

Key invariant: **a decode is not (yet) a QSO.** RX only logs/streams "heard
this" — nothing touches the QSO store or upload queue, so the narrow-daemon-scope
rule holds by import graph. When TX lands, a *completed exchange* becomes a QSO
(`internal/ft8` will import `qsoservice`, never the reverse).

## 2. Enabling FT8

Live FT8 needs a **CGO build** — audio capture is CGO (miniaudio/malgo). The
static CGO-free build logs `capture unavailable; subsystem idle` and does
nothing.

| Goal | Command | CGO? |
| --- | --- | --- |
| Fast dev loop (no deploy) | `task run:smd` (+ `task frontend:dev` for SPA) | yes (default) |
| Build the daemon for dev | `task build` / `task build:smd` | yes (default) |
| Faster decode | `task build:smd:pocketfft` / `SM_FFT=pocketfft …` | yes + PocketFFT |
| Dogfood RPM install | `task deploy:local:dev` | yes (PocketFFT) |
| Headless / static release shape | `task build:smd:static` / `release-rpm.sh` | no (FT8 idle) |

**Fast iteration:** stop the systemd daemon first so it isn't holding the
audio/serial device — `systemctl --user stop smd` — then `task run:smd`.

Config (`config.json`; **stop the daemon before editing** — it rewrites the file
on any PUT):

```json
"ft8": {
  "enabled": true,
  "device": "1",
  "enable_osd": true
}
```

- **`enabled`** — gates the whole subsystem (default false).
- **`device`** — integer capture-device index from `ft8-capture-probe -list`, as
  a string. Empty = system default. Under ADR 0028 the active rig's audio device
  in the rig catalogue wins over this loose field.
- **`enable_osd`** — go-ft8's OSD-2/MRB deeper decode (recovers weak signals BP
  misses, ~1.1–1.7× decode time). Default true; omit to keep it on.
- **`tx.device`** — the audio **output** device index (string) the TX waveform is
  played to, from `ft8-tx-probe -list`. Separate from the capture `device`: the
  playback and capture device enumerations are independent even when the rig's USB
  codec is physically one device. Empty = system default playback device. (Not
  used until the step-(d) controller streams TX audio; `ft8-tx-probe` exercises it
  today.)

FFT backend: the default is pure-Go **gonum**; the opt-in **PocketFFT** (CGO,
`SM_FFT=pocketfft`) is ~2× faster decode but dynamically linked. Decode time on
either is well inside the 15 s slot.

## 3. The SPA (FT8 view)

The header **Operating Mode** switch chooses Phone/CW vs FT8; the choice is
persisted to `localStorage` (survives reload). FT8 mode renders `Ft8Panel`,
which opens the `/v1/ft8/events` stream on mount and closes it on leave.

- **Band Activity** — live decode feed: a rolling list (newest slot on top,
  frequency-ascending within a slot, ~100-row cap) of `time · freq · message`.
  No dB column — go-ft8 doesn't report an SNR.
- **Clear Slots** — the daemon's ranked clear base offsets, shown
  frequency-sorted with **★** marking the daemon's top pick. Read-only today;
  becomes click-to-select TX at step (e).
- **TX Frequency** — *temporary validation view*: the per-slot **Occupied (Hz)**
  list with each band's source/level (`decode` / `both 0.42` / `energy 0.06`),
  added to cross-check the detector against WSJT-X. Step (e) reclaims this panel
  for the interactive TX picker.

### SSE wire — `GET /v1/ft8/events`

Two event types over one stream, each with a one-slot replay cache (a tab
connecting mid-slot gets the current state immediately):

- **`ft8-decode`** → `DecodeReport{ slot, decodes:[{text, freq_hz, dt_s}] }`
- **`ft8-occupancy`** → `OccupancyReport{ slot, passband, signal_width_hz,
  occupied:[{low_hz, high_hz, source, level}], suggested:[hz…] }`

## 4. How occupancy works

Per completed slot, the detector turns audio + that slot's decodes into the
`OccupancyReport` the picker consumes. It is **data, not a spectrogram.**

1. **Spectrum** — Hann-windowed, 50 %-overlap Welch average over the slot, FFT
   size 3840 (3.125 Hz bins, half an FT8 tone).
2. **Two occupancy tiers**, merged into one `occupied` list:
   - **energy** — contiguous bins above `median × threshold_factor`. Gated: a run
     narrower than ~12 Hz (`minEnergyBandHz`, ¼ of a signal width) is dropped as a
     noise/leakage spike, not an occupant.
   - **decode** — each decode's `[FreqHz, FreqHz + 50]` (go-ft8 reports the
     base/sync tone, WSJT-X convention; the signal extends *upward* ~50 Hz).
     CRC+LDPC-verified, so a decode is a real signal — **never gated**, at any
     energy level. This is how weak stations the waterfall barely shows still get
     marked.
   - Overlapping/touching bands merge; mixed sources become **`both`**.
3. **Suggested clear offsets** — invert `occupied` within the passband, keep gaps
   wide enough for a signal plus a guard band each side, step candidates one
   signal-width apart, score each, return the best handful (cap 8).

### Ranking (why a clear offset is "good")

Daemon-side and config-tunable; the SPA treats `suggested` as opaque and never
re-ranks. Each candidate scores 0..1 on three weighted terms:

- **margin** — wider clear room in its gap;
- **edge** — distance from the passband edges (filter roll-off / splatter);
- **centered** — sitting in the middle of its gap rather than flush against a
  neighbour.

A **guard margin** additionally forbids candidates that don't keep clearance
from adjacent occupied bands, so a recommendation never sits flush ("brushed
edge"). Unlike WSJT-X — which lets the operator click *anywhere*, including onto
a signal — SM only offers clean spots, and at step (e) the daemon TX gate refuses
(or snaps) an overlapping offset. Good practice is enforced, not optional.

### Config — `ft8.tx.occupancy.*`

| Key | Default | Meaning |
| --- | --- | --- |
| `passband_low_hz` / `passband_high_hz` | 200 / 3000 | audio range the picker spans |
| `threshold_factor` | 4.0 | energy cutoff = `median × this`; higher = fewer/stronger marks |
| `weight_margin` / `weight_edge` / `weight_centered` | 0.5 / 0.2 / 0.3 | ranking weights (relative only) |
| `guard_margin_hz` | 10 | clearance kept from neighbours; **0 = off** (flush allowed) |

All omittable (zero/absent → default); `guard_margin_hz` is pointer-typed so an
explicit `0` (off) is distinct from "unset". Structural constants (FFT size,
50 Hz signal width, ~12 Hz energy gate, cap of 8 suggestions) live in code, not
config.

## 5. Transmit roadmap (ADR 0029)

Daemon-owned TX, **manual-sequenced first** (operator advances each rung of the
CQ→73 ladder; auto-sequence is a later ADR), reusing the ADR 0027 guaranteed-stop
discipline — `tx_on`/`tx_off` are never `exposed`, only the TX controller keys
the rig. Build order is **RX-safe first**; RF only enters at (c).

| Step | What | State |
| --- | --- | --- |
| (a) | Per-slot occupancy detector + SSE + SPA readout | **done** |
| (b) | GFSK modulator + offline round-trip vs the shipped decoder (zero RF) | **done** |
| (c) | Audio-output device (malgo, `//go:build cgo`, fail-soft, probe-listed) | **done** |
| (d) | PTT + slot-timing controller (daemon-owned guaranteed stop) | next |
| (e) | Manual sequencer + QSO logging; **interactive picker** | — |

**Step (c) — audio output (shipped 2026-06-07).** `internal/audio/playback` is the
output mirror of `internal/audio/capture`: a malgo/miniaudio **S16, 12 kHz, mono**
playback device behind `//go:build cgo` (the static build excludes it; only the
pure `fillFrame`/`bytesAsInt16` helpers compile CGO-free, and they carry the
package's CGO-free unit tests). The int16 waveform from `ft8.EncodeToSlot` streams
straight to the device with no float conversion. Lifecycle `New → Init →
Play(samples) → <done> → Stop / Close`: `Play` is non-blocking and returns a channel
closed when the whole waveform has been handed to the device; **the caller owns the
stop** (`Stop` halts immediately) — the discipline the step-(d) controller inherits
for its guaranteed stop. This layer drives a **sound card, not a transmitter** — no
PTT yet, so it is RF-safe to build and bench. Validate it with
`cmd/ft8-tx-probe` (`-list` enumerates playback devices for `ft8.tx.device`;
`-msg=… -offset=… [-wav=…]` encodes and plays a message, optionally writing the
slot WAV for an A/B decode back through `ft8-decode-file` / `jt9`).

**Step (e) picker (decided 2026-06-07):** a **clickable occupancy strip** — a
*static* per-slot view (busy bands shaded, clear gaps selectable), **not** a
scrolling waterfall — alongside the existing ranked **Clear Slots** list. Click a
clear spot (or a chip) to set the TX base offset; the daemon enforces no-overlap
by refusing/snapping the pick. Enforcement is best-effort *at pick time* —
occupancy re-evaluates each slot, so a station can still land on you mid-exchange;
SM guards the *choice*, not the whole QSO. A full scrolling waterfall (time
history) stays a deferred nicety.

`go-ft8`'s `EncodeStandardMessage` covers standard structured messages only (no
free text / compound calls yet); SM owns tones → GFSK audio → output → PTT →
timing.

## 6. Where the code lives

- **Daemon:** `internal/ft8/` — `service.go` (lifecycle, decode loop, hub
  publish), `scheduler.go` + `ring.go` (UTC slots), `decode.go` (go-ft8 wrapper +
  `DecodeReport`), `occupancy.go` (detector + ranking + guard), `modulate.go`
  (GFSK + offline round-trip), `hub.go` + `handler.go` (SSE). Capture seam:
  `source_cgo.go` / `source_nocgo.go`, `internal/audio/capture`. Output device:
  `internal/audio/playback` (S16 mono playback, `//go:build cgo`).
- **Dev tools:** `cmd/ft8-capture-probe` (list/validate capture + decode smoke),
  `cmd/ft8-tx-probe` (list playback devices + encode-and-play a message, zero RF),
  `cmd/ft8-decode-file` (offline WAV decode). All CGO.
- **SPA:** `frontend/logging/src/lib/states/ft8.svelte.ts` (EventSource consumer),
  `lib/ui/panels/Ft8Panel.svelte`, `lib/ui/cards/LoggingCard.svelte` (mode switch).
- **Decisions:** ADR 0024 (RX pipeline), ADR 0027 (guaranteed-stop TX pattern),
  ADR 0029 (transmit). Licensing: ADR 0023 + `docs/licensing.md`.
