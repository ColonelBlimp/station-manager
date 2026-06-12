# FT8 in Station Manager — operator & contributor guide

> **Status (2026-06-09):** Receive — decode + per-slot occupancy — is shipped.
> Transmit (ADR 0029) is building RX-safe-first: the GFSK modulator, audio-output
> device, and the **PTT + slot-timing controller** (ADR 0030) are done — so SM can
> now key the rig and transmit one FT8 slot from the gated `ft8-tx-probe -key`
> bench path. **First real RF live-validated on the bench 2026-06-09.** go-ft8
> **v0.3.0** added the per-decode **SNR** (dB) — now shown in Band Activity and the
> source for the report we'll send — clearing the step (e) blocker. Still ahead:
> the manual sequencer, QSO logging, and the SPA TX controls (step e). This guide
> is the single place the FT8 picture is captured; keep it current as the TX
> layers land.

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

- **`enabled`** — gates the whole subsystem (default false). Enabling it does
  *not* grab the audio device at boot: capture is **demand-driven** — the daemon
  opens the input device when the first `/v1/ft8/events` subscriber connects (you
  open the FT8 view) and releases it a few seconds after the last one leaves. So
  an idle daemon with `ft8.enabled=true` holds no microphone until you actually
  switch to FT8.
- **`device`** — integer capture-device index from `ft8-capture-probe -list`, as
  a string. Empty = system default. Under ADR 0028 the active rig's audio device
  in the rig catalogue wins over this loose field.
- **`enable_osd`** — go-ft8's OSD-2/MRB deeper decode (recovers weak signals BP
  misses, ~1.1–1.7× decode time). Default true; omit to keep it on.
- **`tx.device`** — the audio **output** device index (string) the TX waveform is
  played to, from `ft8-tx-probe -list`. Separate from the capture `device`: the
  playback and capture device enumerations are independent even when the rig's USB
  codec is physically one device. Empty = system default playback device.
- **`tx.mode`** — the rig data-mode literal the TX controller switches to before
  keying PTT (ADR 0030), e.g. `"DATA-U"` on the FTdx10 (the same vocabulary
  `set_mode` uses), restored after the transmission. Empty leaves the rig's
  current mode untouched, for operators who keep the rig in the data mode
  themselves.

FFT backend: the default is pure-Go **gonum**; the opt-in **PocketFFT** (CGO,
`SM_FFT=pocketfft`) is ~2× faster decode but dynamically linked. Decode time on
either is well inside the 15 s slot for **RX**. **For live FT8 *transmit*, PocketFFT
is preferred** — answering a CQ replies on a synchronised timebase and, if the
decode lands past the slot's nominal +0.5 s start, transmits the head-truncated
remainder (ADR 0032, §6), so a slower decode no longer slips a whole cycle.
PocketFFT still wins on decode speed (~0.72 s on a busy i3-10100F slot vs ~1.5 s
for gonum + OSD), which keeps the most symbols in a truncated reply and best
recall. `task deploy:local:dev` already defaults to PocketFFT; a plain static
release uses gonum.

**What the operator does to keep decode (and the answer-slot timing) fast:**

- **Run a PocketFFT build for live TX** — `task deploy:local:dev`, or
  `SM_FFT=pocketfft …`. This is the single biggest lever and usually the *only*
  thing needed.
- Keep `enable_osd` **on** (default): its ~1.3× cost sits well inside PocketFFT's
  headroom and it materially improves weak-signal recall. Only consider disabling
  it on a gonum build that's actually missing answer slots.
- Don't pin the CPU during a TX QSO — decode is CPU-bound and budgeted to a single
  slot; a loaded box drifts toward the miss threshold (more so on gonum).
- Nothing else: slot alignment is automatic, and a missed slot is never a failed
  QSO — the sequencer just retries the rung on the next cycle.

## 3. The SPA (FT8 view)

The header **Operating Mode** switch chooses Phone/CW vs FT8; the choice is
persisted to `localStorage` (survives reload). FT8 mode renders `Ft8Panel`,
which opens the `/v1/ft8/events` stream on mount and closes it on leave.

- **Band Activity** — live decode feed: `time · SNR · freq · message`, newest
  slot on top, frequency-ascending within a slot. Two display preferences, edited
  from the **Settings tab** (see below) and **daemon-backed** — they live in
  `config.json` under `ft8.display`, not browser localStorage, so they're durable
  per-operator (survive a browser change / data clear), read by the SPA from
  `configState.ft8Display`:
  - **feed mode** (`feed_mode`, default `accumulate`): `accumulate` rolls slots
    up into a rolling history; `single` shows only the current 15 s slot,
    replacing the list each slot (WSJT-X "clear each period" style).
  - **row cap** (`history_max`, default 100, clamped 10–2000 daemon-side): the
    accumulate-mode history limit (also a safety bound on a very busy single
    slot). The **SNR** column (WSJT-X-style signed dB, e.g. `-13`/`+04`) comes
  from go-ft8's `DecodedMessage.SNR` (dB, 2500 Hz reference), added in go-ft8
  v0.3.0 and threaded through `DecodeLine.SNR` → the `ft8-decode` SSE → the row.
  **CQ lines are enriched**: each
  carries the calling station's **country flag** and a **worked-before tint** —
  un-worked-on-this-band stations show in the attention colour, worked-before
  (dupe) stations are muted, WSJT-X-style. Hovering the flag reveals the country
  name (a `title` tooltip from the same enrichment lookup; the flag uses a
  default cursor, not a text caret). Enrichment is purely SPA-side and
  reuses existing endpoints (`/v1/enrich/callsign` → flag, `/v1/contest-dupe` →
  worked on the current band+mode); it's progressive and fail-soft — the row
  renders immediately and the decorations appear when the lookups resolve, so a
  slow/absent hamnut or DB answer never stalls the feed. Results are cached per
  `call|band` for the session (CQ stations recur, so steady-state lookups ≈ 0).
  Only CQ messages are decorated today (one unambiguous callsign); reply/report
  lines stay plain. The two highlight colours are operator-configurable from the
  **Settings tab** (daemon-backed `ft8.display.highlight_unworked` /
  `highlight_worked`; defaults green = new, grey = worked). **Answering (e3):** a
  CQ row is clickable to start a sequenced QSO when TX is armed + a clear offset is
  picked + no QSO is already running (the daemon then auto-advances the ladder).
- **Clear Offsets** — the daemon's ranked clear base offsets, shown
  frequency-sorted with **★** marking the daemon's top pick. **Click a chip to
  select it as the TX base offset**; the selected chip is marked with a **darker
  green border** (`border-green-700` + light-green fill). It only appears while the
  selected offset is among the current slot's suggestions (the selection itself
  persists in `ft8State.selectedOffset` regardless).
- **Rx Frequency** — a WSJT-X-style filtered decode pane (reuses the Band Activity
  row rendering) showing just the conversation being watched. While a QSO is active
  it filters to messages **involving the worked station's callsign** (callsign-exact,
  so a few-Hz offset drift never drops them — more precise than WSJT-X's
  pure-frequency window); when idle it shows decodes within ±tolerance (≈ the signal
  width) of the **selected offset** — "what's on the channel I'm parked on". A caption
  keys it (`Following <call>` / `Offset N Hz ±tol` / `No offset selected`) and empty
  states prompt accordingly. Purely SPA-side — filters the existing
  `ft8State.decodes`, no daemon change. (Replaced the temporary "TX Frequency"
  occupied-Hz validation view, which the Occupancy-tab strip superseded.)
- **Lower section — tabs** (same tablist pattern + `.tab-item` class as InfoPanel,
  full WAI-ARIA keyboard nav): **Occupancy** (the TX Offset strip below), **Ladder**
  (`Ft8MsgPanel` — the FT8 transmit surface, see next bullet), and
  **Settings** (`Ft8SettingsPanel` — the FT8 display preferences: row cap, feed mode,
  CQ highlight colours). The Settings tab saves the **same way as the My Station tab**
  — controls bind to `configState.ft8Display` (live preview), a **Save** button PUTs
  `/v1/config` (bundling the current `logging_station`/`station` so the unconditional
  overwrite doesn't clobber them) and re-hydrates from the response.
- **Ladder tab** (`Ft8MsgPanel`) — the FT8 transmit surface: an **Enable/Disable TX**
  button (the arm gate; red when enabled, gated on a live rig via
  `displayedState.isLive`) + a slot countdown; **Call CQ** and **Abandon** buttons
  always visible but gated (Call CQ enabled only when enabled + idle + offset +
  callsign; Abandon only while a sequenced answer-a-CQ QSO is active); and a
  **message ladder** rendering the full call-CQ exchange one slot per row — our TX
  messages interleaved with the remote's expected responses (`rx`). Unknowns are
  placeholders: `<DX>` (their call), `<GRID>` (their locator), `<RST>` (a report).
  The current slot's row is highlighted — our TX row while transmitting, the RX row
  below while listening for the reply. **NB the ladder is presentational**:
  calling-CQ *sequencing* is the deferred call-CQ scope (today Call CQ is a
  single-shot send on the next slot boundary), so the highlight is currently
  borrowed from the answer-a-CQ `qso.state` machine; the real call-CQ driver
  replaces it when that backend lands.
- **TX Offset strip** (in the Occupancy tab, shipped 2026-06-09) — a horizontal, per-slot
  *spatial* view of the passband, **channelised** into uniform ~50 Hz slots
  (≈56 across 200–3000). FT8 has no standard offset grid — a signal is ~50 Hz
  wide and sits at any continuous offset — so this grid is an SM picker
  convention: one slot = one signal width, so a pick can't half-overlap. **Each
  cell is coloured from the daemon's occupancy: green = clear, red = busy** (any
  occupied band overlapping the cell's span); the **selected slot keeps its
  occupancy colour and is bracketed by a ▼ above / ▲ below** (so the pick reads
  without hiding busy/clear); **grey underline = the daemon's #1 recommendation**
  (its continuous top-ranked offset snapped to the nearest cell). Every cell's
  offset is in its hover title
  (`TX offset 1500 Hz — clear, recommended`). Clicking a cell — or a Clear Slots
  chip — sets the one `ft8State.selectedOffset`. **Selection is inert (RX-safe):
  it only marks "this is where I'll transmit"; nothing keys the rig until the TX
  controller (step d/e) consumes it.** Any cell is clickable (the grid keeps
  picks signal-aligned); daemon-side no-overlap enforcement at pick time lands
  with step (e). The pick is **persisted** (localStorage `sm.ft8.tx.offset`, per
  device): it survives a slot change, a browser refresh, and a view-leave/return,
  so the chosen channel sticks until the operator picks another. A restored offset
  that has since become occupied is harmless — the daemon TX gate refuses/snaps an
  overlapping offset at send time (step e).
  - **TX offset only — by design (decided 2026-06-09).** It sets where *you*
    transmit, never an RX focus. FT8 RX is wideband (the daemon decodes the whole
    passband every slot, so you already hear every station regardless of offset),
    and good FT8 practice is to call on a *clear* slot rather than on top of the
    station you're working — TX and RX offsets are normally different (this is the
    WSJT-X red Tx marker, not the green Rx marker). Choosing *which station* to
    work is a separate callsign-based action for the step-(e) sequencer, not a
    frequency this strip sets.

### SSE wire — `GET /v1/ft8/events`

Four event types over one stream, each with a replay cache (a tab connecting
mid-session gets the current state immediately):

- **`ft8-decode`** → `DecodeReport{ slot, decodes:[{text, freq_hz, dt_s, snr}] }`
- **`ft8-occupancy`** → `OccupancyReport{ slot, passband, signal_width_hz,
  occupied:[{low_hz, high_hz, source, level}], suggested:[hz…] }`
- **`ft8-tx`** → `TxState{ armed, transmitting, message, offset_hz, error }` — the
  transmit arm/in-flight status (step e1).
- **`ft8-qso`** → `QsoStatus{ active, their_call, state, next_message, repeats }` —
  the manual sequencer's active contact (step e3).

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

**Offset hysteresis (stickiness).** The per-slot scoring above picks the *first*
recommendation, but the ★ then **stays put across slots while it remains clear**
rather than re-optimising (and hopping) every 15 s. Each slot, if the previous
top pick still fits a clear gap with the guard margin (`offsetClear` — the same
admission bar candidates use), it is floated back to the front of `suggested`;
only when a signal moves into its space does the ★ fall back to the freshly
ranked best. This is daemon-side and stateless across restarts — the previous
pick is carried in the decode loop for the life of a capture session. The rest of
`suggested` still follows in score order, so the operator always sees the other
options; stickiness only governs which clear offset leads.

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

### Config — `ft8.display.*` (Band Activity preferences)

Operator display settings edited from the **FT8 Settings tab** (not hand-edited);
served resolved on `/v1/config` (`ft8_display`) and PUT back to persist. The daemon
does not consume these — they're pure SPA presentation — it stores + resolves them
(`types.ResolveFt8Display`), so a fresh config still yields sensible values.

| Key | Default | Meaning |
| --- | --- | --- |
| `history_max` | 100 | Band Activity row cap (clamped 10–2000) |
| `feed_mode` | `accumulate` | `accumulate` (roll slots up) or `single` (current slot only) |
| `highlight_unworked` | `#15803d` | CQ tint — not worked on this band (attention) |
| `highlight_worked` | `#9ca3af` | CQ tint — worked-before (muted) |

Daemon-backed rather than browser localStorage so they survive a browser change /
data clear and follow the operator (per the "settings live in config.json, not
localStorage" rule). The **selected TX offset** is the one exception — it stays in
localStorage (`sm.ft8.tx.offset`) as live operating state, not a setting.

## 5. Transmit roadmap (ADR 0029)

Daemon-owned TX, **operator-initiated and attended** (a human starts each QSO; the
CQ→73 rungs then auto-advance within that QSO). **Automatic/unattended sequencing
is out of scope and unsupported — the QEX FT8 specification forbids automatic
operation.** Reusing the ADR 0027 guaranteed-stop
discipline — `tx_on`/`tx_off` are never `exposed`, only the TX controller keys
the rig. Build order is **RX-safe first**; RF first enters at (d) — (a)–(c) are
audio-only / offline.

| Step | What | State |
| --- | --- | --- |
| (a) | Per-slot occupancy detector + SSE + SPA readout | **done** |
| (b) | GFSK modulator + offline round-trip vs the shipped decoder (zero RF) | **done** |
| (c) | Audio-output device (malgo, `//go:build cgo`, fail-soft, probe-listed) | **done** |
| (d) | PTT + slot-timing controller (daemon-owned guaranteed stop) | **done — bench path; ADR 0030** |
| (e) | Manual sequencer + QSO logging; **interactive picker** | e1–e4 shipped 2026-06-10 (TX path, resolver, sequencer ADR 0031, logging) — **answer-a-CQ complete + logged**; call-CQ (operator-initiated) pending. Automatic/unattended sequencing is out of scope — QEX-forbidden. |

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

**Step (d) — PTT + slot-timing controller (shipped 2026-06-09, ADR 0030; first
real RF).** The PTT seam is `ft8.TxKeyer` (`KeyTx`/`UnkeyTx`) — the same
injection pattern as the capture source, so `internal/ft8` keys the rig without
importing `internal/bridge`. The bridge implements it (`KeyFt8Tx`/`UnkeyFt8Tx`,
`internal/bridge/ft8tx.go`) by **reusing the tune controller's guaranteed-stop
machinery**: hard auto-off backstop (`ft8TxMaxDuration`, 18 s), release-on-
disconnect, the rig-identity gate, and a **single-flight shared with tune** so an
FT8 transmission and a tune carrier can never key at once. `tx_on`/`tx_off` stay
unexposed; TX power is left at the operator's setting (no tune-style clamp).
`ft8.TxController` (`internal/ft8/txcontroller.go`) orchestrates one slot:
`EncodeToSlot` → wait for the next UTC boundary (keying a touch early so PTT
settles) → `KeyTx` (optionally switching to `ft8.tx.mode`) → `Player.Play` → on
the done channel `UnkeyTx`. PTT is dropped on **every** return path (a deferred
unconditional unkey), with the bridge auto-off as the backstop. First RF is
reached **only** from the gated **`cmd/ft8-tx-probe -key`** — it stands up its
own bridge connection (so **stop the daemon first** to free the serial port),
waits for connect + identity (`bridge.TxReady()`), then transmits one slot:

```
# AUDIO-ONLY (default, RF-safe): encode + play to a sound card
ft8-tx-probe -msg="CQ G0ABC IO91" -offset=1500

# REAL RF (gated): key the rig + transmit one slot on the next UTC boundary.
# Stop the daemon first; use a dummy load. Reads bridge + ft8.tx.{device,mode}
# from config.json.
smctl stop
ft8-tx-probe -key -msg="CQ G0ABC IO91" -offset=1500
```

No SPA can transmit yet — the sequencer + TX controls are step (e).

**Step (e) picker (decided 2026-06-07; strip shipped 2026-06-09):** a **clickable
occupancy strip** — a *static* per-slot view, **not** a scrolling waterfall —
alongside the existing ranked **Clear Slots** list. **The strip + selection are now
built** (`Ft8OccupancyStrip.svelte`, `ft8State.selectedOffset`). It is
**channelised**: the passband is split into uniform ~50 Hz slots (≈56), each one
signal wide, and **any** slot is clickable — the grid keeps every pick
signal-aligned (no half-overlap), so there's no need for "vetted markers only." Per
cell: green = clear, red = busy (derived from the daemon's `occupied` ranges), the
selected slot bracketed by ▼/▲, and the daemon's #1 recommendation underlined
grey. Clicking a slot or a Clear Slots chip drives the one selection, which is
**inert — RX-safe — until a transmitter exists** (it marks intent, keys nothing).
Still pending for step (e) proper: the TX controller *consuming* the selected
offset, and daemon-side no-overlap enforcement (refusing/snapping the pick). That
enforcement is best-effort *at pick time* — occupancy re-evaluates each slot, so a
station can still land on you mid-exchange; SM guards the *choice*, not the whole
QSO. Finer-than-50 Hz placement (the 6.25 Hz tone grid) and a full scrolling
waterfall (time history) stay deferred niceties.

### Step (e) sequencer — design (2026-06-09; **UNBLOCKED — go-ft8 SNR landed in v0.3.0**)

Design captured; **e2 shipped 2026-06-10, e1/e3/e4 pending.** The former blocker
(go-ft8 had no SNR) cleared when **go-ft8 v0.3.0** added `DecodedMessage.SNR`
(dB, 2500 Hz reference); SM is on v0.3.0 and the field is threaded through the RX
display (`DecodeLine.SNR` → `ft8-decode` SSE → Band Activity dB column) and now
read by the e2 resolver. Step (e) breaks into increments:

- **e1 — daemon TX wiring (no sequencing): daemon side SHIPPED 2026-06-10; SPA UX
  pending design.** The `TxController` (probe-only at step d) is now wired into the
  daemon `ft8.Service` (`internal/ft8/servicetx.go`):
  - **Arm gate.** `Service.ArmTx(bool)` — the explicit operator gate before any FT8
    RF; **disarmed at construction**, nothing transmits until armed. Arming requires
    a wired, ready keyer (`TxKeyer.TxReady()` — new on the seam; the bridge already
    has it) and an available output device, which it acquires (`Init`) and the
    controller is built against; disarming aborts any in-flight TX (PTT drops) and
    releases the device. `Stop` disarms + latches (no re-arm after shutdown).
  - **Send.** `Service.TransmitNext(message, offsetHz)` — refused unless armed, idle,
    and the message encodes (validated synchronously → a bad message is an immediate
    error, not an async failure after the slot wait). Runs `TxController.TransmitSlot`
    in a `safego`-tracked goroutine so the HTTP call returns at once; the guaranteed
    stop is unchanged (controller deferred unkey + bridge auto-off + single-flight).
  - **Output-device seam.** Build-tagged `newTxPlayer` (`txplayer_cgo.go` real malgo
    `playback.Player` + `txplayer_nocgo.go` stub) — exactly like the capture seam, so
    the static CGO-free build reports `ErrTxUnavailable` and never keys.
  - **Keyer injection.** `cmd/smd` wires the bridge as `ft8.TxKeyer` via the `ft8Keyer`
    adapter (`SetTxKeyer`), so `internal/ft8` keys PTT without importing `internal/bridge`.
  - **Endpoints (SPA-reachable, gated by `ft8.enabled`):** `POST /v1/ft8/tx/arm`
    `{armed}` and `POST /v1/ft8/tx/send` `{message, offset_hz}` (202 = applied/queued;
    error codes `ft8_tx_unavailable` 503 / `rig_not_ready` 503 / `ft8_tx_not_armed` 409 /
    `ft8_tx_in_flight` 409 / `ft8_tx_bad_message` 400, per the ADR 0010 `{code,details}`
    discipline).
  - **SSE:** a new `ft8-tx` event `{armed, transmitting, message, offset_hz, error}` on
    `/v1/ft8/events`, hub-cached for late-subscriber replay (current arm state on connect).
  - **Slot timer is SPA-derived** (not a daemon event): FT8 slots are wall-clock-aligned
    (00/15/30/45 s) and every decode/occupancy event is slot-stamped, so the countdown is
    computed client-side (KISS).
  - **SPA UX SHIPPED 2026-06-10** in the **Ladder tab** (`Ft8MsgPanel`): an **Enable/Disable
    TX** toggle (the arm gate — originally labelled "Arm/Disarm"; red when enabled; disabled
    when the rig isn't live — `displayedState.isLive`),
    a **slot countdown** (SPA-derived), and — since the sequencer (e3) doesn't exist yet —
    a single **Call CQ** action that builds `CQ <mycall> <mygrid>` from the My Station
    identity and sends it on the picked offset (`ft8State.selectedOffset`). A TX-state line
    reflects `ft8State.tx` (the `ft8-tx` SSE): disarmed / armed-ready / "Transmitting … @ N Hz"
    / last-error. Arm + send go through `lib/api/ft8tx.ts` (`armFt8Tx`/`sendFt8Tx` → the two
    POSTs); the daemon confirms by push (no optimistic local state). Free-text send and the
    click-a-CQ-row sequencer are deliberately deferred to e3. (The Ladder tab has since
    gained always-visible Call CQ/Abandon buttons and a presentational call-CQ message
    ladder — see the panel-layout section above.)
- **e2 — message model + next-message resolver (pure): SHIPPED 2026-06-10**
  (`internal/ft8/sequence.go`). A `parseMessage` model reduces a decoded line to
  `{kind, to, from, grid, report}` (CQ / grid / report / R-report / RRR·RR73 / 73),
  and an `Exchange` value type walks the answer-a-CQ ladder via pure methods:
  `NewExchange(ourCall, ourGrid, theirCall)` → `TxMessage()` (the message to send
  this rung) + `Advance(decodeText, snr)` (applies a received decode, advances only
  on `<ourCall> <theirCall> <token>` from the worked station, records the report
  they sent + our SNR of their signal) + `Sent()`/`Done()` (final-73 → log). The
  report we send (rung 3) is formatted from the recorded SNR, clamped to the
  [-50, 49] range `EncodeStandardMessage` accepts; a round-trip test asserts every
  message the resolver emits is encodable (RF-safe, no rig). No I/O, no timing,
  no rig — **shared by manual and auto** (only the send policy differs). The
  daemon-side call/grid recognisers mirror the SPA's `parseCqCall` helpers.
- **e3 — manual sequencer: SHIPPED 2026-06-10 (ADR 0031).** Daemon-side
  `internal/ft8/sequencer.go` (`Sequencer`) owns one active answer-a-CQ exchange,
  driven per slot from `decodeLoop` via `OnSlot`: it feeds the worked station's
  decode to the e2 `Exchange.Advance`, then transmits the next rung in the
  **current slot** on a **synchronised timebase** (`seqTransmit` →
  `TransmitCurrentSlot`) in the parity **opposite** theirs — the only timing that
  answers a CQ correctly (the next boundary would be their parity → collision).
  Because the decode lands ~0.7 s into our slot, past the nominal +0.5 s start, the
  controller drops the elapsed head and transmits the **synchronised remainder**
  (truncate-don't-shift, ADR 0032); the receiver re-syncs on the Costas arrays
  (QEX §8 — a reply up to ~5 s late, ~8 s with AP-mycall, still decodes). Off-ramps
  (ADR 0031): late-window guard — `txLateWindowSec` (~4.5 s into the slot) skips a
  rung only when too few symbols would survive truncation; plus
  N-unanswered-repeats → abandon, abort on Disarm/Abandon, never auto-switch
  targets. **PocketFFT remains the preferred live-TX build** (§2): a faster decode
  (~0.72 s busy-slot vs ~1.5 s gonum) keeps more symbols in a truncated reply and
  best recall, but a slower decode now truncates rather than slipping a cycle. On
  the 73 it captures a `CompletedQso`
  (e4 logs). Endpoints `POST /v1/ft8/qso/{start,abandon}` (start gated on TX armed;
  our identity resolved daemon-side from config, not client-sent) + the `ft8-qso`
  SSE. **SPA:** initiation = **click a CQ row in Band Activity** (clickable when TX
  armed + an offset picked + no QSO running → `startFt8Qso`); the Ladder tab shows
  the live rung / next message / Abandon (`ft8State.qso`).
- **e4 — QSO completion → log: SHIPPED 2026-06-10.** On the 73, the sequencer
  captures a `CompletedQso` and hands it to an **injected sink** (`SetQsoLogger`):
  `internal/ft8/qsolog.go`'s pure `BuildQso(c, station, logbookID, now)` assembles
  a `types.Qso` (their call/grid; mode FT8; **freq = dial + audio offset**, band
  derived; RST_SENT = our report, RST_RCVD = theirs; the whole `LoggingStation`
  identity copied in, STATION_CALLSIGN falling back to OPERATOR), and the daemon's
  sink (`cmd/smd`) does `adif.QsoToRecord` → `qsoservice.Submit` (force=false, so
  dupe detection applies). **The "decode ≠ QSO" rule (ADR 0024) becomes "a
  completed *exchange* is a QSO."** Narrow-daemon-scope still holds: `internal/ft8`
  does **not** import `qsoservice` — the assembly + submit live in the composition
  root (`cmd/smd`), wired in via the `SetQsoLogger` callback (dependency injection;
  the one-way direction ADR 0029 wanted, achieved without the import). Best-effort:
  a submit failure is logged, never fatal (the QSO already happened on the air).
  The dial freq comes from the SPA at `qso/start` (`operating_freq_mhz`) — the
  bridge is a pass-through and can't surface it.
- **Automatic / unattended sequencing is OUT OF SCOPE and NOT SUPPORTED.** The FT8
  protocol forbids automatic operation (per the QEX FT8 protocol specification),
  and unattended operation is illegal without a special licence in many
  jurisdictions. SM therefore supports **only operator-initiated (attended)** FT8:
  every contact is started by a human — answer-a-CQ (click a CQ) and call-CQ
  (press Call CQ). There is no daemon-initiated auto-answer mode.

**Scope order: answering a CQ first, then calling CQ** (calling CQ adds
multi-answerer management).

**Manual/auto seam (the key idea):** a **pure next-message resolver** shared by
both; the only difference is the **send policy**. **RATIFIED 2026-06-10 (ADR 0031):**
the operator's judgement is *whom to work* (the click) + arming TX, and rung
advance is mechanical — so within a QSO the rungs **auto-advance** (the daemon
walks the ladder via the e2 resolver; the operator intervenes only to
retry/abandon). i.e. **manual = operator-initiated-per-QSO with automatic rung
advance** — but the operator still initiates every QSO (the click + arming TX), so
SM stays **attended-only**. A daemon-*initiated* mode would be automatic/unattended
operation, which is **out of scope and unsupported** — the QEX FT8 specification
forbids automatic operation (see above). Per-rung confirm
was rejected (the 15 s cadence makes it frantic; the Arm-TX gate already provides
the deliberate-consent safety). Off-ramps: stop after N unanswered repeats; never
auto-start a fresh CQ cycle; abort on operator action; never auto-switch targets.

**Resolver + live QSO/sequencer state live daemon-side** (working assumption):
auto needs it there, it is shared orchestration state (ADR 0004), and
QSO-completion is daemon-side. The SPA is a thin sequencer view (show next
message, arm/confirm/abandon).

**Answer-a-CQ state machine** (our side, answering `CQ K1ABC FN42` as
`G0XYZ IO91`, transmitting in the slot parity **opposite** K1ABC):

| State | We heard | We send | advance on |
| --- | --- | --- | --- |
| Calling | (op clicked the CQ) | `K1ABC G0XYZ IO91` | repeat until answered |
| Reporting | `G0XYZ K1ABC -10` | `K1ABC G0XYZ R-<snr>` | a report to us |
| Confirming | `G0XYZ K1ABC RR73`/RRR | `K1ABC G0XYZ 73` → **log** | RR73/RRR to us |

Off-ramps: K1ABC answers someone else (`SP9ABC K1ABC …`) → stay Calling
(repeat/abandon); operator abandon; timeout after N unanswered repeats. **Advance
rule:** a decode advances the QSO only if it parses `<ourCall> <theirCall>
<token>` from the worked station.

**Report source (DECIDED, now available):** the report we send (rung 3, `R-<snr>`)
is the **real SNR from go-ft8** — `DecodedMessage.SNR` (dB), added in **v0.3.0**
(SM now links it). The sequencer records the partner's latest SNR; rung 3 sends it.
SNR belongs in the decoder (jt9 already computes it; go-ft8 is a jt9 derivative),
so a configured default and SM-side SNR computation were both rejected. The SNR is
already threaded through `decode.go` → `DecodeReport` → SPA (the Band Activity dB
column); e2's resolver reads the same field to form rung 3.

**STATUS:** **e1–e4 shipped 2026-06-10 — the answer-a-CQ flow is complete end to
end** (arm → pick offset → click a CQ → auto-advance CQ→73 → **logged**). e1 =
daemon TX + Ladder Arm/Call-CQ. e2 = pure resolver. e3 = daemon manual sequencer
(ADR 0031) + click-a-CQ initiation + Ladder view/Abandon. e4 = completed exchange
→ `types.Qso` (`BuildQso`) → `qsoservice` via the injected `SetQsoLogger` sink
(`internal/ft8` stays narrow — no `qsoservice` import). **Next: the deferred
**call-CQ** scope** (operator-initiated caller-side state machine + multi-answerer
handling — calling CQ today is only the single-shot button). The send-policy seam
is ratified (ADR 0031 Accepted). **Automatic/unattended sequencing is out of scope
and unsupported — the QEX FT8 specification forbids automatic operation.**

`go-ft8`'s `EncodeStandardMessage` covers standard structured messages only (no
free text / compound calls yet); SM owns tones → GFSK audio → output → PTT →
timing.

## 6. Where the code lives

- **Daemon:** `internal/ft8/` — `service.go` (lifecycle, decode loop, hub
  publish; capture is **subscriber-driven** — acquired on the first
  `/v1/ft8/events` subscriber, released after a short linger when the last
  leaves, so the device is only held while an FT8 view is open),
  `scheduler.go` + `ring.go` (UTC slots), `decode.go` (go-ft8 wrapper +
  `DecodeReport`), `occupancy.go` (detector + ranking + guard), `modulate.go`
  (GFSK + offline round-trip), `hub.go` + `handler.go` (SSE). Capture seam:
  `source_cgo.go` / `source_nocgo.go`, `internal/audio/capture`. Output device:
  `internal/audio/playback` (S16 mono playback, `//go:build cgo`). TX (ADR 0030):
  `txkeyer.go` (`TxKeyer`/`slotPlayer` seams) + `txcontroller.go` (slot-aligned
  key→play→unkey); PTT keying in `internal/bridge/ft8tx.go` (`KeyFt8Tx`/
  `UnkeyFt8Tx`/`TxReady`, reusing the tune guaranteed-stop, single-flight shared
  with tune).
- **Dev tools:** `cmd/ft8-capture-probe` (list/validate capture + decode smoke),
  `cmd/ft8-tx-probe` (list playback devices + encode-and-play; `-key` keys the rig
  for one slot — REAL RF, gated), `cmd/ft8-decode-file` (offline WAV decode). All CGO.
- **SPA:** `frontend/logging/src/lib/states/ft8.svelte.ts` (EventSource consumer),
  `lib/ui/panels/Ft8Panel.svelte`, `lib/ui/cards/LoggingCard.svelte` (mode switch).
  Band Activity CQ enrichment: `lib/states/ft8Enrich.svelte.ts` (per-`call|band`
  cache + fail-soft flag/worked lookups + configurable highlight colours),
  `lib/utils/ft8Message.ts` (`parseCqCall`), `lib/utils/flag.ts` (`ccodeToFlag`),
  `lib/api/contest-dupe.ts` (worked-before client). TX-offset picker:
  `lib/ui/panels/Ft8OccupancyStrip.svelte` + `ft8State.selectedOffset` /
  `selectOffset()` (inert selection, RX-safe).
- **Decisions:** ADR 0024 (RX pipeline), ADR 0027 (guaranteed-stop TX pattern),
  ADR 0029 (transmit), ADR 0030 (step (d): PTT + slot-timing controller).
  Licensing: ADR 0023 + `docs/licensing.md`.
