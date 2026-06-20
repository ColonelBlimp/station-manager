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
- **`tx.caller_answer_mode`** — when WE call CQ, which answering station to work
  (ADR 0033): `"auto_first"` works the first valid answerer (WSJT-X "Auto Seq");
  `"operator_pick"` queues answerers for the operator to pop (the pile-up stack).
  Default `auto_first`; empty/invalid → the default. `operator_pick` is **not yet
  implemented** — Call CQ with it configured is **rejected** at start (501
  `ft8_caller_mode_unsupported`) rather than silently auto-picking, so the setting
  never misbehaves; use `auto_first` until the stack ships.
- **`tx.max_repeats`** — how many times the sequencer re-sends an unanswered rung
  before it auto-abandons the contact (ADR 0031 off-ramp; the caller side resumes
  CQ instead of abandoning). Default **6** (~90 s of calling); `0`/absent → default.
  **Hard-clamped to ≤ 10** (`Ft8MaxRepeatsCeiling`) — a safety bound like the
  tune-power / auto-off clamps, so no config value can leave the rig calling a dead
  station for minutes. Surfaced to the SPA as a per-rung "N calls left" countdown in
  the Working banner (see §4). Config-only today (no Settings-tab control yet).

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

- **Main-Freq band buttons** — one button per configured FT8 band; clicking
  tunes the operating VFO to that band's dial freq and, **when CAT is live, also
  asserts the rig's FT8 mode** (`setFreq` + `setMode(configState.bridge.ft8Mode)`),
  so picking a band guarantees data mode (e.g. `USB-D` on the IC-7300, `DATA-U`
  on the FTdx10) rather than leaving the rig in whatever mode it was. `ft8Mode`
  is the rigdef default (per-rig overridable) carried in `/v1/config`
  `bridge.ft8_mode`. CAT-off the button only tunes `manualState` (mode assert is
  live-only — `setMode` off would write the rig literal into `manualState`, which
  expects operator-friendly modes). Each call is independently capability-gated.
  A **dial-frequency label** sits below the buttons showing the live operating
  frequency (`displayedState` selected VFO) — **the exact value FT8 logs**, so the
  operator can verify the band before working anyone. It reads **"waiting for rig…"**
  (amber) until the rig has actually reported a dial frequency (`catState.freqKnown`),
  and in that state **FT8 TX is blocked** (CQ rows / pile-up rows un-clickable, Call CQ
  disabled) and **no band button highlights**. This guards a real data-integrity bug:
  `catState.vfoA`/`vfoB` initialise to a *valid-looking* placeholder (14.250 MHz), so a
  rig that is "responding" but whose frequency poll hasn't landed yet (seen on the
  IC-7300 — CI-V freq arrives via the bridge poll, not a broadcast) would otherwise let
  a QSO be logged on the wrong band with nothing to flag it. `freqKnown` flips true on
  the first `rig-state` carrying a `vfoA` and false on disconnect.
- **Band Activity** — live decode feed under a **sticky column header**
  (`dB · Hz · Beam · Message`) that stays pinned while the rows scroll; one row
  per decode, newest slot on top, frequency-ascending within a slot. Each CQ row
  that carries a grid shows a **beam heading** — the short-path bearing from your
  grid (`my_gridsquare`) to the CQ's grid (e.g. `045°`, indigo column) — so you can
  aim the antenna before answering. It reuses the same `pathInfo` bearing math as
  the Phone/CW Country panel, purely SPA-side (blank when the CQ has no grid or
  your grid is unset). In `accumulate` mode a **slot divider** (`HH:MM:SS · band`)
  heads each slot's block; the per-row timestamp was dropped (redundant with the
  divider + the footer's slot clock). **The feed clears on a band change** — when
  the operating band (derived from the selected VFO's dial frequency) crosses a band
  boundary, the accumulated rows are decodes from the previous band's watering hole,
  so they're dropped rather than mixed with the new band's traffic (intra-band dial
  nudges don't clear it). The footer slot label is `HH:MM:SS · even/odd`
  — a raw "N busy" count was removed as un-actionable (the occupancy strip carries
  congestion visually). Two display preferences, edited
  from the **Settings tab** (see below) and **daemon-backed** — they live in
  `config.json` under `ft8.display`, not browser localStorage, so they're durable
  per-operator (survive a browser change / data clear), read by the SPA from
  `configState.ft8Display`:
  - **feed mode** (`feed_mode`, default `accumulate`): `accumulate` rolls slots
    up into a rolling history; `single` shows only the current 15 s slot,
    replacing the list each slot (WSJT-X "clear each period" style).
  - **row cap** (`history_max`, default 100, clamped 10–2000 daemon-side): the
    accumulate-mode history limit (also a safety bound on a very busy single slot).
  - **float CQ to top** (`cq_to_top`, default off): pins CQ rows (the answerable
    stations) above the rest of the feed, stably partitioned (each group keeps its
    order). Per-slot separators are suppressed while on (the list is no longer
    slot-ordered). SPA-side reorder of `ft8State.decodes`; no daemon change.
  (Note `cq_to_top` is *ordering*, not filtering — it reorders, it never hides — so it
  lives in the Settings tab. The row-*hiding* filters live in the funnel popover below.)

  **Band Activity filters — the funnel popover.** A **funnel icon** sits to the right
  of the "Band Activity" header; it opens a small popover holding the two controls that
  **hide rows** (the funnel shows an active tint whenever either is narrowing the feed —
  the only cue that rows are hidden while the popover is closed). A station **calling you**
  (toMe) **always shows through** both filters, so you never miss a caller.
  - **typed filter** (`ft8State.bandFilter`): **token-prefix** — a decode shows when any
    whitespace token starts with the typed text, case-insensitive ("show calls starting
    with VK" matches the `VK3ABC` token, not `G4VKX`; `CQ`/`73` filter by message kind).
    **Session-scoped, in-memory** — a transient hunt, NOT a durable setting; cleared on
    tab close. No save.
  - **hide hashed calls** (`hide_hashed_calls`, default off): drops decodes carrying an
    unresolved hashed call (`<...>` — a non-standard/compound call the receiver can't
    expand → dross). **Durable** config (`ft8.display`); the popover toggle **auto-saves**
    on change (no Save button — reuses `configState.saveFt8Display()`).

  Both run SPA-side on `ft8State.decodes`, ahead of the CQ-to-top ordering; Band Activity
  only (the Rx Frequency pane is unaffected). No daemon change beyond the persisted flag.

    The **SNR** column (WSJT-X-style signed dB, e.g. `-13`/`+04`) comes
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
  **Working a caller — the pile-up (ADR 0033 "work a caller"):** a decode that is a
  station *calling you* — the grid-bearing opening `<yourCall> <theirCall> <grid>`
  (e.g. `7Q5MLV PA3KUS JO21`) — is tinted with the **calling colour** (daemon-backed
  `ft8.display.highlight_calling`, default amber `#b45309`; **no LSPA picker** — it's
  edited via config.json / the config SPA, the LSPA only reads + round-trips it) so it
  stands out from band chatter,
  and is **clickable** (under the same gate as answering: armed + offset + idle) to
  work that station via `POST /v1/ft8/qso/work`. The amber tint shows live — even mid-
  contact, so you can see who's waiting — but the row only becomes clickable once you
  go idle (finish or Abandon the current QSO), so you line the next one up and click it
  the moment you're free. The daemon then runs a caller-style exchange (we report first
  → RR73 → log) and returns to idle. Detection is SPA-side (`parseDirectedToMe`); only
  the unambiguous grid opening is matched, not the mid-exchange `R-12`/`RR73`/`73`
  replies.
  **Pile-up callsign stacking (ADR 0033, shipped 2026-06-17):** because a calling-you
  row is only *clickable* when armed + idle, callers you spot mid-QSO are gone before
  you can act. **Ctrl/Cmd+click** a calling-you decode instead to push it onto a
  **pile-up stack** — a FIFO (`ft8PileupStack`), worked **oldest-first**. Ctrl+click is
  available in **any** state (mid-QSO, disarmed; it's pure capture, no TX — a ✓ marks a
  row already stacked), so you grab callers the instant you see them in your RX slot and
  the SPA works them when it can. The Operate view **drains** the stack via the
  work-a-caller path whenever the rig is armed + idle, advancing as each contact
  completes, while you keep adding. SPA-only (daemon untouched); in-memory (erased on
  tab/browser close, like the Phone/CW `callsignStack`). This is the realised
  operator-pick experience and **supersedes** the daemon `operator_pick` Call-CQ mode.

  *The drawer* hangs off the **right edge of the logging card** (mounted alongside the
  Phone/CW Call Stack), always visible while non-empty regardless of which FT8 sub-tab
  is open; the Operate tab also carries a depth badge. It is a deliberate twin of the
  Phone/CW Call Stack so it operates the same on sight. It **auto-hides when empty** —
  it isn't shown until a caller is queued, and disappears once the queue drains or is
  cleared. Per-row **×** removes that one caller; the header **×** ("Clear all &
  abandon") clears the queue *and* abandons the run (aborts the active exchange if one
  is in flight). Clicking a row **does nothing** — unlike the Phone/CW stack (where a
  click loads the call into the QSO draft), the FT8 daemon auto-drains the queue for
  you, so there's nothing to "load." Grid/SNR are captured per entry but not displayed.

  **Re-clicking a station already on the stack is harmless.** The push de-dups by
  callsign: it **refreshes** that entry's grid/SNR/slot in place (a later decode is
  better data to work from) **without** adding a duplicate and **without** changing its
  FIFO position. (A ✓ on the Band Activity row tells you it's already queued.)

  **Auto-drain pause/resume.** The stack **starts draining** (enabled by default) and
  stays that way through QSO completions, decode gaps, and errors — nothing pauses it
  automatically. It is **suspended only by Abandon**: the ladder's **Abandon** button
  pauses the drain but *keeps* the queue (the drawer shows "· paused" + a **Resume**
  button), and the header **×** also pauses but then clears the queue so the drawer
  hides. It **resumes** when you press **Resume** — *or* when you Ctrl/Cmd+click a
  **new** caller (enqueuing a genuinely new station re-enables the drain). Re-clicking a
  station **already** on the stack only refreshes its data and does **not** un-pause —
  so once you've hit Abandon you stay paused until you explicitly Resume or stack a
  caller you haven't queued yet.
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
- **Worked-station enrichment box + short/long path radio** — while a QSO is active the
  Rx Frequency pane shows the worked station's flag/country/op-name and the **beam
  heading + distance**, with a **Short / Long path radio** beneath (mirrors the Phone/CW
  Country panel). The radio is **logging-only** — it picks which great-circle figures are
  recorded (`ANT_AZ`, `DISTANCE`) and stamps ADIF **`ANT_PATH`** (`S`/`L`); it never
  touches the on-air signal (FT8 messages carry no path info, and you aim the beam at the
  rig). Because FT8 QSOs are built **daemon-side**, the choice is sent via
  `POST /v1/ft8/qso/path` rather than on a SPA submit; the daemon stamps it in `BuildQso`
  (bearing/distance math mirrors `bearing.ts`, so FT8 and Phone/CW agree). Defaults to
  **short** and resets to short at the start of each contact (both SPA and daemon), so a
  prior QSO's "long" never carries over. *(Phone/CW logs `ANT_PATH` too now — its
  Country-panel radio stamps it on submit, mode-independent, matching FT8.)*
- **"Working [callsign]" channel readout** — occupies the
  always-visible info row above the lower tabs (so it shows on **every** lower tab), **replacing
  the idle offset readout** while a contact is in flight (`ft8State.qso.active`); when
  idle that cell reverts to the `Offset N Hz ±tol` / `No offset selected` text. It
  reads `Working <call> — channel clear/BUSY`
  and colours **green when the selected TX channel is clear, red when occupied** —
  the same overlap test as the Occupancy strip (`ft8State.channelOccupied`: the
  `[selectedOffset, selectedOffset + signalWidth)` span vs the latest occupied bands),
  re-evaluated each slot. Grey "channel unknown" when no offset is picked or no
  occupancy report has arrived yet. It closes the **pick-time → TX-time gap**: a
  channel chosen clear can have a station land on it a slot or two later, and this
  surfaces that **before** the next transmission keys rather than after. Purely
  SPA-side, RX-safe — no daemon change. The banner also carries a **per-rung
  attempts-remaining countdown** (`· N calls left`) while the current rung is subject
  to the auto-abandon cap (`ft8.tx.max_repeats`, default 6): it counts down each
  unanswered slot and reaches 0 on the slot before the sequencer abandons (or, on the
  caller side, resumes CQ). The daemon advertises the cap (`max_repeats`) on the
  `ft8-qso` payload **only on the rungs it governs**, so the countdown shows iff
  `max_repeats > 0` — it's absent on the uncapped calling-CQ rung and the one-shot
  73/RR73.
- **Lower section — tabs** (same tablist pattern + `.tab-item` class as InfoPanel,
  full WAI-ARIA keyboard nav; each tab carries a Heroicon to read alike with the
  Phone/CW InfoPanel tabs): **Occupancy** (chart-bar — the TX Offset strip below),
  **Operate** (signal — `Ft8MsgPanel`, the FT8 transmit surface, see next bullet),
  **Session** (list-bullet — the shared session log, see below), and **Settings**
  (cog — `Ft8SettingsPanel`, the FT8 display preferences: row cap, feed mode, float-
  CQ-to-top, CQ highlight colours). The Settings tab saves the **same way as the My Station tab**
  — controls bind to `configState.ft8Display` (live preview), a **Save** button PUTs
  `/v1/config` (bundling the current `logging_station`/`station` so the unconditional
  overwrite doesn't clobber them) and re-hydrates from the response.
  (The **slot countdown** sits in the **Band Activity footer** — one line reading
  `<UTC time> · <parity> · next in Ns`: the current displayed slot's time + parity
  (shown once here, not duplicated in the info row) plus the live countdown to the next
  slot boundary. It's in the always-visible top row, so it shows regardless of tab.)
- **Session tab** (`SessionPanel` + `SessionEmailControls`) — the **shared** session
  QSO log, identical to InfoPanel's Session tab: a "session" is everything worked
  this sitting **across both modes**, so FT8 QSOs and Phone/CW QSOs share one list
  (`sessionQsosState`) and one email-out, and FT8 QSOs also appear in the Phone/CW
  Session tab. FT8 QSOs reach the list via the **`ft8-logged` SSE event** (below):
  the daemon emits each completed exchange's session fields — carrying the canonical
  **UUID**, so email-out and edit work for FT8 rows exactly as for Phone/CW. A
  recipient input + paper-plane button (the extracted `SessionEmailControls`, used by
  both panels) emails the session ADIF when the daemon mailer is configured. A
  **`QSO logged — <call> (<band>)` toast** fires on each completed FT8 exchange (the
  only "it's in the log" signal, since there's no form to clear) — gated on the same
  **Toast on QSO stored** setting (`qsoDefaults.notifyQsoStored`, My Station →
  Notifications) and worded identically to the Phone/CW logged-toast. The **Country
  column is populated for FT8 rows** — the daemon enriches the contacted station
  before submit (see e4 below), so `country` rides the `ft8-logged` event. Distance
  is computed SPA-side from your grid.
- **Operate tab** (`Ft8MsgPanel`) — the FT8 transmit surface: an **Enable/Disable TX**
  button (the arm gate; red when enabled, gated on a live rig via
  `displayedState.isLive`); **Call CQ** and **Abandon** buttons, always visible but
  gated (Call CQ enabled when armed + idle + offset + callsign; Abandon only while a
  sequenced session — answer-a-CQ or call-CQ — is active); a **Next** button that
  appears below Abandon **only mid-contact with stations still queued** in the pile-up —
  it aborts the current exchange but, unlike Abandon, does **not** pause the drain, so it
  jumps straight to the next queued caller (the operator's "this one's a no-show, move
  on" shortcut — ditch a station after a rung or two instead of waiting out the full
  `max_repeats` backstop, which it leaves untouched); and a **message ladder**
  rendering the exchange one slot per row — our TX messages interleaved with the
  remote's expected responses (`rx`), unknowns as placeholders `<DX>` / `<GRID>` /
  `<RST>` — the **reports fill in live** from `qso.our_report`/`their_report` once the
  daemon knows them (the `<RST>` slots show the real `-12` etc.). The current slot's
  row is highlighted — our TX row while transmitting, the
  RX row below while listening. **The ladder is LIVE and role-aware**
  (`ft8State.qso.role`): answering a CQ (`answerer`, e3) shows the answer ladder
  (grid → R-report → 73); **Call CQ** (`caller`, ADR 0033) starts a *sequenced*
  session — the daemon calls CQ and auto-works the answerers (per
  `ft8.tx.caller_answer_mode`, default `auto_first`), the caller ladder highlights the
  real rung (`calling-cq → reporting → rogering`), and the button reads "Calling CQ…".
  When idle the caller ladder shows as a static preview. **Working a caller** (`worker`,
  ADR 0033 "work a caller") shows a third ladder: the caller-style exchange with **no CQ
  row** — the opening is the station's call to *us* (their actual grid, from
  `qso.their_grid`), then report → RR73. (`their_grid` is carried on the `ft8-qso`
  payload for all roles, so the opening row shows the real grid rather than a `<GRID>`
  placeholder.)
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
  picks signal-aligned); the strip is **best-effort guidance, not a hard gate** —
  SM is attended-only, so the offset is the operator's choice and the daemon does
  **not** refuse or snap an *overlapping* pick (it DOES reject a non-finite or
  out-of-passband offset — a safety bound, not overlap admission; see the review
  note below). The pick is
  **persisted** (localStorage `sm.ft8.tx.offset`, per device): it survives a slot
  change, a browser refresh, and a view-leave/return, so the chosen channel sticks
  until the operator picks another. A restored offset that has since become
  occupied simply re-colours busy on the strip — it's the operator's call to
  re-pick; SM does not block it.
  - **TX offset only — by design (decided 2026-06-09).** It sets where *you*
    transmit, never an RX focus. FT8 RX is wideband (the daemon decodes the whole
    passband every slot, so you already hear every station regardless of offset),
    and good FT8 practice is to call on a *clear* slot rather than on top of the
    station you're working — TX and RX offsets are normally different (this is the
    WSJT-X red Tx marker, not the green Rx marker). Choosing *which station* to
    work is a separate callsign-based action for the step-(e) sequencer, not a
    frequency this strip sets.

### Working a CQ — what to expect on the air

Clicking a CQ row sends a **directed** reply — `<their-call> <your-call> <your-grid>`
(e.g. `JA6CPQ 7Q5MLV KH78`), **never a CQ**. The sequencer repeats that call once per
your-parity slot until the station answers or you Abandon. (You can confirm exactly
what went out in the daemon log: `ft8 seq: transmitting rung` records the literal
message, rung, offset, and how late into the slot it fired. The `ft8 seq:` QSO
events log at **info**, so they are always captured; the full per-slot decode stream
— `ft8 decode`, one line per decoded signal — logs at **debug** to keep the normal
log quiet, so raise the daemon log level to `debug` for a one-off look, or — for a
durable record — enable the **decode log** below.)

**Don't expect the station you answered to always come back.** Two normal FT8
realities — neither a fault in SM, and both visible in the log as a string of
`calling`-rung transmits with no advance:

- **Propagation + power.** A weak-path or low-power signal may simply not decode at
  the far end. From a rare/remote location at low power, easy-path stations (e.g.
  same-or-neighbouring continent) hear you while harder paths (trans-equatorial,
  polar, the far side of the world) often don't — so the JA/US station you called
  stays silent even though your call went out correctly.
- **Pile-ups on a rare prefix.** The moment you transmit, your callsign is visible to
  the whole passband (FT8 RX is wideband — everyone decodes everyone). DXers chasing a
  rare prefix call *you* with their grid regardless of who you're working, so you
  routinely see a *different* station answer you (`<your-call> <their-call> <grid>`)
  while the one you picked never replies. That is the pile-up, not a sequencing bug.

**Operational takeaway:** from a sought-after location, work the callers in your
pile-up rather than chasing weak-path CQs — **press Call CQ** and SM works the
answerers for you (ADR 0033 caller-side sequencing, `auto_first`: it calls CQ,
auto-works the first answerer through RR73, logs it, and resumes — looping the pile-up
until you Abandon). To choose *which* callers to work and in what order, use **pile-up
callsign stacking** instead: **Ctrl/Cmd+click** each calling-you decode to queue it, and
SM works the stack oldest-first (see "Working a caller" above). `auto_first` Call CQ is
the hands-off option; the stack is the operator-curated one.

### Decode log (`ALL.TXT`-style) — `ft8.decode_log.*`

A durable, append-only record of **every RX decode and our own TX**, in the JTDX
`ALL.TXT` line format — so you can reconstruct an on-air exchange after the fact and
compare it line-for-line with another operator's log. It is written **independently of
the daemon log level** (the per-decode `ft8 decode` line is gated off at the default
`info`, a 12–16×/slot firehose; this is the way to keep the stream without running the
whole daemon at `debug`).

Off by default — like WSJT-X's `ALL.TXT`, the file grows unbounded and you clear it
yourself. Enable it in `config.json` (restart to apply; capture opens the file when the
FT8 view is first opened and closes it when the last subscriber leaves):

```jsonc
"ft8": {
  "decode_log": {
    "enabled": true,
    "path": ""   // optional; default $SM_WORKING_DIR/log/ft8-all.txt (next to smd.log)
  }
}
```

Line format (UTC throughout):

```
20260618_140830 -7 0.3 2752 ~ JM1ISX 7Q5MLV -15                       # RX: ts snr dt freqHz ~ message
20260618_140845.104 Transmitting 14.074 MHz + 2997Hz FT8: 7Q5MLV JM1ISX R-07   # TX: ts.ms dial + offset
```

RX lines carry no dial (only the audio offset within the passband); the TX line carries
the session dial (omitted for a manual transmit with no session dial). The TX timestamp
is stamped when the transmission commits — within ~1 s of the on-air key for a sequencer
rung. The default path resolves against the daemon's working dir (the same one `smd.log`
uses, honouring `--config` / `data_dir`).

**Never blocks FT8.** All disk I/O runs on a dedicated writer goroutine; the decode and
TX paths only format a line and hand it off without blocking. If the disk stalls (full,
slow, network-backed), lines are **dropped** (counted, and reported on close) rather than
stalling slot decoding or current-slot TX timing. Fail-soft on open too: a bad path / full
disk logs one warning and leaves the subsystem decoding normally without a log.

### SSE wire — `GET /v1/ft8/events`

Five event types over one stream. The first four are **replay-cached** (a tab
connecting mid-session gets the current state immediately); `ft8-logged` is a
one-shot notification and deliberately **not** cached (replaying it to a late
subscriber would duplicate a session-list row):

- **`ft8-decode`** → `DecodeReport{ slot, decodes:[{text, freq_hz, dt_s, snr}] }`
- **`ft8-occupancy`** → `OccupancyReport{ slot, passband, signal_width_hz,
  occupied:[{low_hz, high_hz, source, level}], suggested:[hz…] }`
- **`ft8-tx`** → `TxState{ armed, transmitting, message, offset_hz, error }` — the
  transmit arm/in-flight status (step e1).
- **`ft8-qso`** → `QsoStatus{ active, role, their_call, state, next_message, repeats,
  our_report, their_report }` — `our_report`/`their_report` are the exchanged signal
  reports (e.g. `-12`), empty until known, used to fill the ladder's `<RST>` slots —
  the manual sequencer's active contact (step e3).
- **`ft8-logged`** → `LoggedQso{ uuid, callsign, freq_hz, band, rst_sent, rst_rcvd,
  mode, time_on, qso_date, gridsquare, country }` — a completed exchange the daemon
  just stored (step e4), so the SPA adds it to its session list. Emitted by the e4 sink
  (`cmd/smd`) after a successful `qsoservice.Submit`, via `Service.PublishQsoLogged`;
  `internal/ft8` builds the payload (`NewLoggedQso`) but never touches storage.
  `country` is the enriched country the sink resolved before submit (so the Session-tab
  Country column matches Phone/CW).

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

A **guard margin** additionally forbids *candidates* (the ranked suggestions)
that don't keep clearance from adjacent occupied bands, so a recommendation never
sits flush ("brushed edge"). Unlike WSJT-X — which gives the operator no occupancy
cue at all — SM ranks and highlights the clean spots and shades the busy ones, so
a clear offset is obvious at a glance. But the pick is the **operator's** (SM is
attended-only): the strip is best-effort **guidance**, not a hard gate — SM does
**not** refuse or snap an *overlapping* selection. A daemon-side overlap-admission
gate was considered (review 2026-06-16 H1) and deliberately left out; enforcement
would fight the attended-operation model. The daemon DOES, however, reject an
offset that is **non-finite or outside the usable passband** at send time (review
2026-06-19 M1) — a hardware-safety sanity bound, distinct from overlap admission:
a station may transmit where it overlaps another, but never where the tone can't
be a valid FT8 placement at all.

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

Operator display settings, served resolved on `/v1/config` (`ft8_display`) and PUT back
to persist. Edited across a few surfaces: `history_max` / `feed_mode` / `cq_to_top` from
the **FT8 Settings tab**; `hide_hashed_calls` from the **Band Activity filter funnel**
(auto-saves on toggle); the three `highlight_*` colours from the **config SPA**. The
daemon does not consume these — they're pure SPA presentation — it stores + resolves them
(`types.ResolveFt8Display`), so a fresh config still yields sensible values.

| Key | Default | Meaning |
| --- | --- | --- |
| `history_max` | 100 | Band Activity row cap (clamped 10–2000) |
| `feed_mode` | `accumulate` | `accumulate` (roll slots up) or `single` (current slot only) |
| `highlight_unworked` | `#15803d` | CQ tint — not worked on this band (attention) |
| `highlight_worked` | `#9ca3af` | CQ tint — worked-before (muted) |
| `highlight_calling` | `#b45309` | text colour for a station calling you (toMe/pile-up rows) — no LSPA picker, config-SPA/hand-edited |
| `cq_to_top` | `false` | float CQ rows to the top of Band Activity (separators suppressed) |
| `hide_hashed_calls` | `false` | hide decodes with an unresolved hashed call (`<...>`); stations calling you still show |

Daemon-backed rather than browser localStorage so they survive a browser change /
data clear and follow the operator (per the "settings live in config.json, not
localStorage" rule). The **selected TX offset** is the one exception — it stays in
localStorage (`sm.ft8.tx.offset`) as live operating state, not a setting.

### PSK Reporter upload — `psk_reporter.*` (`internal/pskreporter`)

Uploads FT8 **reception reports** ("I heard this station") to PSK Reporter
(<https://pskreporter.info/pskdev.html>) — the propagation-map / "who's hearing me"
feed. The **report/upload** side only; the retrieve/query feed is future work.

- **Opt-in, default OFF** (`psk_reporter.enabled`) — it publishes your RX to a public
  service. Also gated on a configured receiver callsign (`logging_station`).
- **Fed by the FT8 decode stream** via `ft8.Service.SetDecodeSink` (one-way DI like
  `SetQsoLogger`, so `internal/ft8` stays decode-only — narrow-daemon-scope holds).
  `cmd/smd` extracts a spot per decode with `ft8.SpotFrom` (sender call + grid; hashed
  `<...>` / free text skipped), and reports **freq = dial + audio offset** (the real RF,
  not the dial-only QSO convention), SNR, mode `FT8`, slot time.
- **Transport:** IPFIX over one long-lived UDP socket (constant source port). Dedup per
  call (best SNR) within a window; flush ~5 min (program-relative timer + jitter, never
  system-clock-synced); descriptors in the first 3 datagrams + hourly. **Best-effort —
  a send failure logs and drops; never blocks decoding.**
- **Config keys** (`config.json` → `psk_reporter`, not on `/v1/config`):

| Key | Default | Meaning |
| --- | --- | --- |
| `enabled` | `false` | upload FT8 spots (opt-in) |
| `host` | `report.pskreporter.info` | UDP collector host (NOT `pskreporter.info` — that's the website and drops UDP) |
| `port` | `4739` | `4739` = production; `14739` = test port on the same host (parses without writing the live DB) |

The reported antenna comes from the station's **`MY_ANTENNA`** (My Station config), not a
`psk_reporter` key — single source of truth with the antenna stamped on logged QSOs.

The encoder is verified byte-for-byte against the spec's worked example
(`internal/pskreporter/ipfix_test.go`), and validated end-to-end against the live
collector: a probe datagram was received + fully parsed (receiver + sender records)
per `/cgi-bin/psk-analysis.pl`. For manual validation without the daemon,
**`cmd/ft8-psk-probe`** (dev/test only, not a production path) builds spots from
sample decode lines via `ft8.SpotFrom` and sends one datagram —
defaulting to **`report.pskreporter.info:14739`** (the test port: parsed but not
written to the live DB); `-port=4739` for production. `-dry` parses + prints without
sending.

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
| (e) | Manual sequencer + QSO logging; **interactive picker** | e1–e4 shipped 2026-06-10 (TX path, resolver, sequencer ADR 0031, logging) — **answer-a-CQ complete + logged**. **Call-CQ `auto_first` shipped 2026-06-12 (ADR 0033)** — Call CQ → daemon works the pile-up (first answerer) → logged, looping until Abandon. The **operator-pick experience shipped as the SPA pile-up stack** (Ctrl/Cmd+click a caller → FIFO → work-a-caller drain) — the daemon `caller_answer_mode=operator_pick` mode is **superseded by it** and rejected 501 (`ft8_caller_mode_unsupported`), not a pending roadmap item. Automatic/unattended sequencing is out of scope — QEX-forbidden. |

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
    click-a-CQ-row sequencer are deliberately deferred to e3. (The tab — since renamed
    **Operate** — has since gained the answer-a-CQ sequencer (e3/e4) and, per ADR 0033,
    a live caller-side Call-CQ session; its message ladder is now daemon-driven, not
    presentational — see the panel-layout section above.)
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
  targets. **First-rung immediate-fire (2026-06-12):** `StartQso` takes `now` and a
  `fireOpening(now)` helper sends the opening call in the click's *current* TX slot
  when it's the opposite parity within `txLateWindowSec` — otherwise the opening rung
  waits for the next qualifying `OnSlot`, which lands at a boundary, so a click just
  after one stalled a full ~30 s cycle. (Caller-side Call CQ is unchanged — it picks
  its CQ parity as the *next* slot, so its first CQ is already ≤ ~15 s.) **PocketFFT
  remains the preferred live-TX build** (§2): a faster decode
  (~0.72 s busy-slot vs ~1.5 s gonum) keeps more symbols in a truncated reply and
  best recall, but a slower decode now truncates rather than slipping a cycle. On
  the 73 it captures a `CompletedQso`
  (e4 logs). Endpoints `POST /v1/ft8/qso/{start,abandon}` (start gated on TX armed;
  our identity resolved daemon-side from config, not client-sent) + the `ft8-qso`
  SSE. **SPA:** initiation = **click a CQ row in Band Activity** (clickable when TX
  armed + an offset picked + no QSO running → `startFt8Qso`); the Operate tab shows
  the live rung / next message / Abandon (`ft8State.qso`).
- **e4 — QSO completion → log: SHIPPED 2026-06-10.** On the 73, the sequencer
  captures a `CompletedQso` and hands it to an **injected sink** (`SetQsoLogger`):
  `internal/ft8/qsolog.go`'s pure `BuildQso(c, station, logbookID, now)` assembles
  a `types.Qso` (their call/grid; mode FT8; **freq = the DIAL frequency** — the FT8
  logging convention (WSJT-X/JTDX log the dial, not dial+audio-offset); both stations
  share the dial but sit at different audio offsets, so the TX offset is deliberately
  NOT added to FREQ (fixed 2026-06-13 — it was previously dial+offset, which disagreed
  with the worked station's log + QRZ/LoTW); band derived from the dial; RST_SENT = our
  report, RST_RCVD = theirs; the whole `LoggingStation`
  identity copied in, STATION_CALLSIGN falling back to OPERATOR), and the daemon's
  sink (`cmd/smd`) does `adif.QsoToRecord` → `qsoservice.Submit` (force=false, so
  dupe detection applies). **The "decode ≠ QSO" rule (ADR 0024) becomes "a
  completed *exchange* is a QSO."** Narrow-daemon-scope still holds: `internal/ft8`
  does **not** import `qsoservice` — the assembly + submit live in the composition
  root (`cmd/smd`), wired in via the `SetQsoLogger` callback (dependency injection;
  the one-way direction ADR 0029 wanted, achieved without the import). Best-effort:
  a submit failure is logged, never fatal (the QSO already happened on the air).
  - **Country enrichment at log time (2026-06-13).** Before `Submit`, the sink calls
    `enrichOrchestrator.Enrich(theirCall)` and copies the merged contacted-station
    fields (country, DXCC, CQ/ITU zone, …) onto the QSO — the daemon-side equivalent
    of the SPA calling `/v1/enrich/callsign` before a Phone/CW submit. This both fills
    the stored QSO's country (otherwise `Submit` defaults it to "Unknown") **and**
    triggers the cold-miss `country`-table cache write inside `Enrich` (so a worked
    DXCC entity gets its country record). The on-air grid stays authoritative over any
    cached locator. The whole sink — enrich + submit + `PublishQsoLogged` — runs in a
    one-shot `safego` goroutine **off the FT8 decode loop**, because the sink fires on
    that loop (after the 73) and a cold-miss country lookup is network I/O that would
    otherwise stall slot decoding and drop slots. `Enrich` never errors (failures fold
    to empty fields), so logging is never blocked — the "enrichment never blocks
    logging" invariant holds.
  - **Report validation is mode-aware.** FT8 RST_SENT/RST_RCVD are signed dB SNRs
    (`-12`, `+04`), not phone/CW RST digits. The shared `Rst` SPA component takes a
    `mode` prop and switches the validator + input cleaning when the mode is a
    WSJT-X-family weak-signal mode (`utils/mode.ts` `usesSignalReport`,
    `validators/rst.ts` `isValidSignalReport`) so editing an FT8 QSO in the edit
    overlay doesn't flag the report red or strip its sign.
  The logged dial frequency is the **rig's live dial read from the bridge at QSO
  completion** (`bridge.CurrentDialMHz()`, injected into the `cmd/smd` e4 sink exactly
  like `CurrentPowerW()` — `internal/ft8` stays import-clean). The SPA's `qso/start`
  `operating_freq_mhz` is now only a **fallback** for when the bridge has no dial yet.
  This was a deliberate fix (2026-06-17): the SPA value is a *start-time snapshot*
  captured once and reused for an entire Call-CQ pile-up, so it logged a stale/wrong
  band when the QSO started before the rig's frequency poll had landed (seen on the
  IC-7300). The bridge is always on frequency, so reading it at completion is correct.
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
daemon TX + Operate-tab Arm/Call-CQ. e2 = pure resolver. e3 = daemon manual sequencer
(ADR 0031) + click-a-CQ initiation + Operate view/Abandon. e4 = completed exchange
→ `types.Qso` (`BuildQso`) → `qsoservice` via the injected `SetQsoLogger` sink
(`internal/ft8` stays narrow — no `qsoservice` import). **Call-CQ caller-side
sequencing shipped 2026-06-12 (ADR 0033, `auto_first`):** Call CQ starts a sequenced
session — the daemon calls CQ, auto-works the first answerer through RR73, logs it,
and loops the pile-up until Abandon (`CallerExchange` + `onSlotCalling` +
`POST /v1/ft8/cq/start`; needs on-air validation). **Pile-up callsign stacking shipped
2026-06-17** (ADR 0033 amendment): Ctrl+click calling-you decodes onto an SPA-owned FIFO
that drains via the work-a-caller path — the operator-curated alternative to
`auto_first`, superseding the daemon `operator_pick` Call-CQ mode.
**Automatic/unattended sequencing is out of scope and unsupported — the QEX FT8
specification forbids automatic operation.**

`go-ft8`'s `EncodeStandardMessage` covers standard structured messages, **including
the standard `/P` variant** (go-ft8 ≥ **v0.3.5**). SM works `/P` stations end to end
with **no SM code change** — every TX guard decides by trying `EncodeStandardMessage`
and skipping on error, so an upstream encoder gain flows straight through (proven
offline in `internal/ft8/modulate_test.go`: `TestEncodeStandardMessage_Portable` +
`TestModulate_RoundTrip_Portable`). **Still unencodable → still skipped:** type-4
compound/nonstandard calls (`PJ4/K1ABC`, `/MM`, …) and free text. SM owns tones →
GFSK audio → output → PTT → timing.

## 6. Where the code lives

- **Daemon:** `internal/ft8/` — `service.go` (lifecycle, decode loop, hub
  publish; capture is **subscriber-driven** — acquired on the first
  `/v1/ft8/events` subscriber, released after a short linger when the last
  leaves, so the device is only held while an FT8 view is open),
  `scheduler.go` + `ring.go` (UTC slots), `decode.go` (go-ft8 wrapper +
  `DecodeReport`), `occupancy.go` (detector + ranking + guard), `modulate.go`
  (GFSK + offline round-trip), `qsolog.go` (`BuildQso` + the `LoggedQso` payload /
  `NewLoggedQso` mapper for the `ft8-logged` event), `hub.go` + `handler.go` (SSE). Capture seam:
  `source_cgo.go` / `source_nocgo.go`, `internal/audio/capture`. Output device:
  `internal/audio/playback` (S16 mono playback, `//go:build cgo`). TX (ADR 0030):
  `txkeyer.go` (`TxKeyer`/`slotPlayer` seams) + `txcontroller.go` (slot-aligned
  key→play→unkey); PTT keying in `internal/bridge/ft8tx.go` (`KeyFt8Tx`/
  `UnkeyFt8Tx`/`TxReady`, reusing the tune guaranteed-stop, single-flight shared
  with tune).
- **Dev tools:** `cmd/ft8-capture-probe` (list/validate capture + decode smoke),
  `cmd/ft8-tx-probe` (list playback devices + encode-and-play; `-key` keys the rig
  for one slot — REAL RF, gated), `cmd/ft8-decode-file` (offline WAV decode). All CGO.
- **SPA:** `frontend/logging/src/lib/states/ft8.svelte.ts` (EventSource consumer;
  the `ft8-logged` listener builds a session row — distance via `pathInfo`, dedup by
  uuid — and calls `sessionQsosState.add`), `lib/ui/panels/Ft8Panel.svelte`,
  `lib/ui/cards/LoggingCard.svelte` (mode switch). Session tab reuses
  `lib/ui/panels/SessionPanel.svelte` + the extracted
  `lib/ui/panels/SessionEmailControls.svelte` (recipient + send, shared with InfoPanel).
  Per-CQ beam heading: `lib/utils/bearing.ts` (`pathInfo`).
  Band Activity CQ enrichment: `lib/states/ft8Enrich.svelte.ts` (per-`call|band`
  cache + fail-soft flag/worked lookups + configurable highlight colours),
  `lib/utils/ft8Message.ts` (`parseCqCall`), `lib/utils/flag.ts` (`ccodeToFlag`),
  `lib/api/contest-dupe.ts` (worked-before client). TX-offset picker:
  `lib/ui/panels/Ft8OccupancyStrip.svelte` + `ft8State.selectedOffset` /
  `selectOffset()` (inert selection, RX-safe).
- **Decisions:** ADR 0024 (RX pipeline), ADR 0027 (guaranteed-stop TX pattern),
  ADR 0029 (transmit), ADR 0030 (step (d): PTT + slot-timing controller).
  Licensing: ADR 0023 + `docs/licensing.md`.
