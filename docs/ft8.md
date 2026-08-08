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

> **TX + attribution invariants** (what must never break on the transmit and
> slot-attribution paths, and why) live in **`internal/ft8/CLAUDE.md`**, so they
> auto-load when working in that package — which is where they were being missed.
> Read them before changing the TX or slot path; they are not repeated here.

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
  switch to FT8. **And when CAT is configured (the bridge is enabled), capture is
  additionally gated on the rig being live:** opening the FT8 view with the rig
  *off* does not grab the mic — the daemon waits until the rig is connected +
  identity-confirmed, acquires then, and releases the mic again if the rig drops
  mid-session. (This stops the daemon seizing the audio device when the SPA
  reopens to FT8 on PC boot before the rig is powered.) With no CAT configured
  there's no rig to gate on, so capture stays purely demand-driven. The liveness
  check is `bridge.Service.RigConnected()`, injected via `ft8.Service.SetCatGate`
  in `cmd/smd`; a 2 s reconcile loop (`catReconcileInterval`) tracks the rig
  powering on/off. A rig that goes silent but keeps its serial port open (a
  passive no-data disconnect) is caught too — `RigConnected` falls to false after
  a couple of consecutive no-data liveness timeouts, distinguishing a genuinely
  dead rig from a merely-quiet one the bridge's probe recovers each cycle.
  **A live capture stream that goes DEAD is self-healed too** (dogfood
  2026-07-18, "Plasma upgrade day"): a desktop audio reshuffle (KDE/PipeWire
  device fiddling) can destroy+recreate the device's nodes under a running
  session, leaving the daemon's stream dangling — no error anywhere, Band
  Activity just never decodes again. A scheduler-side watchdog
  (`internal/ft8/deadsource.go`) checks each 15 s boundary for a starved
  (< quarter-slot of samples) or silent (all literal zeros — an analog input
  always carries ADC noise) window; two consecutive dead windows release +
  reacquire the session (fresh OS stream → links to the current nodes),
  mirroring the CAT no-data strike pattern. Worst-case ~45 s outage instead
  of silent-forever; if the reacquire fails outright, the CAT-reconcile loop
  keeps retrying as before.
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
- **`tx.caller_answer_mode`** — when WE call CQ, which answering station the daemon
  works next (ADR 0033): `"auto_first"` works the first valid answerer by decode
  order (WSJT-X "Auto Seq"); `"auto_strongest"` works the highest-SNR valid answerer
  in the slot (clear the loud signals first). **SESSION STATE since ADR 0066
  (operator: "all the config knobs should be available session based"):** the
  live control is the **Answer selector** in the TX control bar (`Answer | CQ
  slot`, one centred row; locked while a run is active — changes apply to the
  next run), carried on `POST /v1/ft8/cq/start` as `answer_mode` exactly as
  `tx_parity` is, and on `qso/start`/`qso/work` alongside the auto-work intent
  (it is what an armed run selects with). `ft8.tx.caller_answer_mode` is only
  the DEFAULT that seeds the selector at page load; the `/v1/config` PUT
  accepts all three literals as defaults (fork 4 retired the ADR 0065
  operator_pick fence with the config-only world it guarded). **Default
  `operator_pick` (operator-ratified 2026-08-08, superseding ADR 0033's
  `auto_first`):** automatic operation is licence-restricted in many
  jurisdictions, so a station whose operator never CHOSE an auto mode must not
  auto-work anyone — a clean install starts every session listing answerers
  for the operator, and with `auto_work_callers` also defaulting off, fully
  manual until both automations are opted into (now visible per-session
  gestures, which serves the licensing intent better than a config edit).
  **The auto-work arming gate reads the SESSION, not config** (ADR 0066 fork
  5): intent + an auto session mode arms; under "I pick" the SPA disables the
  toggle with the reason and drops the intent at the source (a pick run cannot
  auto-work — invariant 7); `ft8.tx.auto_work_callers` survives only as the
  toggle's boot seed, served on GET as `ft8_auto_work_callers`. Spec:
  `internal/ft8/adr0066_test.go` (daemon R-rules) +
  `ft8AutoWork.svelte.test.ts` SP-rules (SPA). The third literal `"operator_pick"` is **implemented
  since 2026-08-07 (ADR 0065 decision 3)** but stays a **config.json-only** value
  (operator-ratified): during a CQ run the daemon LISTS answerers on the `ft8-qso`
  frames (`answerers`, expiring 3 min after last heard) instead of auto-committing,
  the SPA's pile-up drawer renders them, and clicking one commits it via
  `POST /v1/ft8/cq/pick`; the CQ keeps calling until then and the run resumes CQ
  after the contact. A park (Next / the repeat cap) under this mode never
  auto-picks a replacement. The SPA dropdown still never offers it and a
  `/v1/config` PUT carrying it is still a 400 — enforcement of the config.json-only
  decision, no longer a missing feature. (The ctrl-click pile-up stack always
  drains **FIFO** regardless of this knob — the mode governs only how a Call-CQ
  run selects its answerers.)
- **`tx.max_repeats`** — how many times the sequencer re-sends an unanswered rung
  before it auto-abandons the contact (ADR 0031 off-ramp; the caller side drops the
  silent contact and, since 2026-07-17, first re-scans that slot's decodes for
  another live answerer — the pile-up kept calling while we worked the silent one —
  working them immediately and only resuming CQ when nobody else is calling).
  Default **6** (~90 s of calling); `0`/absent → default.
  **Hard-clamped to ≤ 10** (`Ft8MaxRepeatsCeiling`) — a safety bound like the
  tune-power / auto-off clamps, so no config value can leave the rig calling a dead
  station for minutes. Surfaced to the SPA as a per-rung "N calls left" countdown in
  the Working banner (see §4). Config-only today (no Settings-tab control yet).
- **`field_day.{class, section}`** — the operator's **ARRL Field Day exchange**, sent
  when **answering** a `CQ FD` over FT8 (search & pounce; SM does **not** call CQ FD).
  `class` is `<transmitters><category>` (e.g. `2A`, `1D`); `section` is the ARRL/RAC
  section, or `DX` for a station outside US/Canada. Both empty = FD identity not set
  (the off-season default). Surfaced over `/v1/config` as `ft8_field_day` (presence-aware),
  stored upper-cased. **Validation:** class strict (`^[1-9][0-9]?[A-F]$`, in `types`);
  **section checked against go-ft8's canonical ARRL/RAC list** (`ValidARRLFieldDaySection`,
  go-ft8 v0.4.0) — done in `internal/config/validate.go` rather than `types`, which
  stays stdlib-only and so can't import go-ft8. The same list (`ARRLFieldDaySections()`,
  a `[]ARRLFieldDaySection`) will drive the future SPA section dropdown (config is
  config.json-only for now). This block is the SM-owned identity both FD exchanges
  carry — see **ARRL Field Day operating** below for the answer + work paths it feeds.
- **`field_day.default_rst_rcvd`** — the value logged as **RST_RCVD** for an FD QSO. FD
  exchanges class/section, not a report, so we never receive an RST — but some OQRS
  systems require RST_RCVD non-empty. The operator sets this (e.g. `59`, `599`, `-15`);
  empty → RST_RCVD left blank. (RST_SENT is the **measured SNR**, recorded from the
  decode like a standard FT8 QSO — the SPA passes our SNR of the clicked decode as
  `their_snr`, threaded to `FdExchange`/`FdWorkExchange.SendSnr` → `CompletedQso.OurReport`.
  The RST_RCVD default is applied at log time in the `cmd/smd` e4 sink, which has config —
  `BuildQso` stays config-free.)

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

**Operating state is remembered across a mode switch — BOTH ways** (`LoggingCard` +
`rigControl.snapshotOperatingState`/`restoreOperatingState`): on every Phone/CW ↔ FT8
switch SM snapshots the **outgoing** mode's operating state (VFO A/B, mode, selection)
and restores the **incoming** mode's last snapshot — so phone→ft8 returns to your last
FT8 band/freq and ft8→phone to your last Phone/CW, instead of staying parked on the
other mode's frequencies. First switch into a mode (no snapshot yet) restores nothing.
CAT-off rewrites `manualState`; **CAT-live auto re-tunes the rig**, sending
`set_freq`/`set_freq_b`/`set_mode` for only the values that changed, each
capability-gated. No VFO-swap is issued (it would exchange VFO contents; selection is
preserved across an excursion since each mode tunes the selected VFO). Snapshots are
in-memory only, so a reload mid-mode won't trigger a surprise re-tune on return. **The
CAT-live re-tune is opt-out** via the daemon config `restore_rig_on_mode_switch`
(default ON; an explicit `false` disables the live re-tune — the harmless CAT-off
`manualState` restore is unaffected). Editable on the **config SPA's General tab**
(2026-06-26); the daemon only stores/serves the flag, the behaviour is SPA-side.

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
  lines stay plain. Worked-vs-new is shown with the app's own **theme-aware
  palette** — amber tint = not worked on this band, muted text = worked before
  (`Ft8BandActivity.svelte` `rowClass`) — and NOT from the
  `ft8.display.highlight_*` config keys, which nothing reads (see the
  `ft8.display.*` table below). **Answering (e3):** a
  CQ row is clickable to start a sequenced QSO when TX is armed + a clear offset is
  picked + no QSO is already running (the daemon then auto-advances the ladder).
  **Working a caller — the pile-up (ADR 0033 "work a caller"):** a decode that is a
  station *calling you* — the grid-bearing opening `<yourCall> <theirCall> <grid>`
  (e.g. `7Q5MLV PA3KUS JO21`) — is given a **blue tint** by the same theme-aware
  palette (`ft8.display.highlight_calling` is stored but not read — see the
  `ft8.display.*` table) so it stands out from band chatter,
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
  tab/browser close, like the Phone/CW `callsignStack`). This is the operator-pick
  experience for callers that did NOT come from a CQ run; since ADR 0065 the daemon
  `operator_pick` Call-CQ mode complements it — during such a run the same drawer
  renders the DAEMON's answerer list first ("Answering your CQ", from the `ft8-qso`
  frames) and clicking one commits it into the run via `POST /v1/ft8/cq/pick`.

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

  **Single-parity rule (2026-06-27).** FT8 is half-duplex on a two-parity 15 s grid, so
  every station you can work in one run must sit on **your RX parity** — the slot parity
  you receive on (= opposite your TX = the parity of the CQ you answered). The pile-up is
  therefore **single-parity**, enforced against a **stable run-parity lock**
  (`ft8PileupStack.lockedParity`): the FIRST station added fixes the run's parity, and the
  lock is **held across the drain emptying the queue** between contacts — so Ctrl/Cmd+click
  rejects any later add whose slot parity differs (info toast + the wrong-parity calling-you
  row is **muted/greyed** with a reason). The lock **releases** when a fresh run starts —
  detected lazily at enqueue time: queue empty **and** no contact active means the previous
  run is over, so the next add relocks to its parity — and on Abandon/Clear. (Earlier this
  anchored on the live queue *head*, which the drain kept clearing, so the rule almost never
  bit — the lock fix is what makes it stick.) Band Activity rows carry an **even/odd badge**
  (E sky / O purple, derived SPA-side from the slot time via `utils/ft8Parity.ts`; even =
  :00/:30, odd = :15/:45 UTC), and the pile-up drawer header shows the run's parity
  (`Pile-up (N) · odd`). **Expected, not a bug:** while *working* a pile-up you transmit on
  one parity and are therefore **deaf to it**, so Band Activity only ever shows your RX
  parity for the whole run (all-E if you're replying on odd) — the other parity only appears
  when you're not transmitting (idle gaps), which is the only time a wrong-parity add is even
  possible. The daemon's `their_period` (on the `ft8-qso` SSE) seeds the workable parity
  before anything is queued; once a station is queued the SPA lock is authoritative.

  **Queueing is disabled while *calling* CQ (2026-06-27).** During a Call-CQ session the
  daemon's auto-pick is the single "who's next" engine, so Ctrl/Cmd+click does **not**
  enqueue — it shows a "Calling CQ — pile-up queue disabled" toast. (Letting it enqueue
  built a second, competing controller: a non-empty stack lit the **Next** button, whose
  click abandoned the CQ run and silently handed the rig to the pile-up drain, which never
  resumed CQ.) **Abandon** is the one way to stop a Call-CQ run. Queueing stays available
  in answer-a-CQ / work-a-caller (role ≠ caller), where it's the whole point.

  **Currently-working station + same-session dupes (2026-06-27).** The station you're
  working *this exchange* (`qso.their_call` while active) is **highlighted** in Band
  Activity (indigo row + ring — any decode mentioning their call) so it reads as the live
  contact, not an available caller, and **can't be re-queued** (its grid-opening re-calls
  still decode, but Ctrl+click shows "Already working <call>" — it was just dequeued when
  the contact started). Separately, a **same-session dupe** — a call already logged on the
  **current band this session** (`sessionQsosState`, contest-dupe style) — is **greyed
  out** and **blocked** from being worked or queued (answer/work/enqueue all short-circuit
  with "Already worked <call> this session"). This is **session-scoped on purpose**:
  re-working a station you hold in the **durable logbook from a *prior* session is fine**
  (left freely workable, only the existing worked-before *tint* applies); only a repeat
  **within this session** is the dupe. Cross-band is not a dupe (you'd want them on the new
  band). NB the guard is on the **SPA Band Activity clicks** only — the daemon auto-workers
  (Call-CQ `auto_first`, the pile-up drain) don't yet honour the session-dupe rule (backlog).
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
  caller side, works another live answerer / resumes CQ). The daemon advertises the cap (`max_repeats`) on the
  `ft8-qso` payload **only on the rungs it governs**, so the countdown shows iff
  `max_repeats > 0` — it's absent on the uncapped calling-CQ rung and the one-shot
  73/RR73.

  **ON AIR indicator (2026-06-27).** The same always-visible banner row shows a red,
  pulsing **`● ON AIR`** pill whenever the rig is keyed for an FT8 transmission, in any
  role (calling CQ, answering, working a caller) — it tracks `ft8State.tx.transmitting`,
  so it lights for the ~12.6 s of each TX slot and clears during the RX half. Being in
  the upper section it shows on **every** lower tab (Occupancy / Operate / Session /
  Settings), so "am I transmitting?" is answerable from anywhere in the FT8 view.
- **Lower section — tabs** (same tablist pattern + `.tab-item` class as InfoPanel,
  full WAI-ARIA keyboard nav; each tab carries a Heroicon to read alike with the
  Phone/CW InfoPanel tabs): **Occupancy** (chart-bar — the TX Offset strip below),
  **Operate** (signal — `Ft8MsgPanel`, the FT8 transmit surface, see next bullet),
  **Session** (list-bullet — the shared session log, see below), and **Settings**
  (cog — `Ft8SettingsPanel`, the FT8 display preferences: row cap, feed mode, float-
  CQ-to-top, CQ highlight colours — **plus a Call CQ → Answer dropdown** for the
  auto-answerer selection mode, `ft8_caller_answer_mode`: First answerer / Strongest
  signal). The Settings tab saves the **same way as the My Station tab** — controls
  bind to `configState.ft8Display` / `configState.ft8CallerAnswerMode` (live preview),
  a **Save** button PUTs `/v1/config` (bundling the current `logging_station`/`station`
  so the unconditional overwrite doesn't clobber them) and re-hydrates from the response.
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
  `displayedState.isLive`); a **CQ slot** parity selector (WSJT-X "Tx even/1st":
  **Next** = call CQ on the next slot regardless of parity, the fast default; **Even**
  = :00/:30; **Odd** = :15/:45) — operating state in localStorage (`sm.ft8.tx.parity`,
  like the selected offset), sent as `tx_parity` on `cq/start`, locked while a session
  is active; caller-side only (answering a CQ forces the opposite parity); **Call CQ**
  and **Abandon** buttons, always visible but gated (Call CQ enabled when armed + idle +
  offset + callsign; Abandon only while a sequenced session — answer-a-CQ or call-CQ —
  is active); a **Next** button that
  appears below Abandon **only mid-contact with stations still queued** in the pile-up —
  it aborts the current exchange but, unlike Abandon, does **not** pause the drain, so it
  jumps straight to the next queued caller (the operator's "this one's a no-show, move
  on" shortcut — ditch a station after a rung or two instead of waiting out the full
  `max_repeats` backstop, which it leaves untouched). **Next is disabled while
  transmitting** (2026-06-27) — it acts only in the RX half of the cycle, so the drop
  lands cleanly and the next caller opens on the following TX slot; clicking it mid-slot
  used to abandon the contact while the current slot played out *and* then key the next
  caller, running one station's message tail straight into a transmission for a different
  callsign. **Deferred skip → DAEMON-SIDE (frontend/app, 2026-07-13):** the app SPA first
  made Next a deferred "skip if silent" (arm, resolve on the RX outcome), then moved
  the whole mechanism into the sequencer: `POST /v1/ft8/qso/skip {armed}` sets
  `skip_if_silent`, and a silent cycle on an already-transmitted rung ends the session
  **instead of keying the repeat**. Why: the SPA-side resolve could only observe the
  silent cycle via the `ft8-qso` status published *after* the daemon had already keyed
  the repeat — so every skip cut a just-started transmission (an audible PTT
  "tick-tick" and a fraction of a second of RF at a station already being dropped).
  The arm clears when the partner replies, on session start, and on Abandon; it rides
  the `ft8-qso` SSE as `skip_armed` (confirm-by-push renders the amber button);
  applies to answering + working sessions (standard and FD) — a Call-CQ run's Next
  stays an immediate takeover. Guard: the skip never fires before the rung has
  transmitted at least once (`repeats > 0`). And a **message ladder**
  rendering the exchange one slot per row — our TX messages interleaved with the
  remote's expected responses (`rx`), unknowns as placeholders `<DX>` / `<GRID>` /
  `<RST>` — the **reports fill in live** from `qso.our_report`/`their_report` once the
  daemon knows them (the `<RST>` slots show the real `-12` etc.). The current slot's
  row is highlighted — our TX row while transmitting, the
  RX row below while listening. **The ladder is LIVE and role-aware**
  (`ft8State.qso.role`): answering a CQ (`answerer`, e3) shows the answer ladder
  (grid → R-report → 73); **Call CQ** (`caller`, ADR 0033) starts a *sequenced*
  session — the daemon calls CQ and works the answerers per
  `ft8.tx.caller_answer_mode` (default `operator_pick` since 2026-08-08 — an
  unconfigured station lists answerers for the operator instead of
  auto-working), the caller ladder highlights the
  real rung (`calling-cq → reporting → rogering`), and the button reads "Calling CQ…"
  **and turns red** while the run is live (2026-06-27 — an unmistakable "I'm running CQ"
  cue, distinct from the per-slot ON AIR pulse below).
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
  the offset is the operator's choice and the daemon does
  **not** refuse or snap an *overlapping* pick (it DOES reject a non-finite or
  out-of-passband offset — a safety bound, not overlap admission; see the review
  note below). The pick is
  **persisted** (localStorage `sm.ft8.tx.offset`, per device): it survives a slot
  change, a browser refresh, and a view-leave/return, so the chosen channel sticks
  until the operator picks another. A restored offset that has since become
  occupied simply re-colours busy on the strip — it's the operator's call to
  re-pick; SM does not block it.
  - **Spectrum view (switchable, shipped 2026-06-26).** A second presentation of the
    SAME per-slot occupancy snapshot, toggled by a **Channels | Spectrum** control
    (operating state, `ft8State.occupancyView`, localStorage `sm.ft8.occupancy.view`;
    default Channels). Where the channelised strip discretises into ~50 Hz cells —
    which makes a band *look* fuller than it is and turns a pick **binary-red** when a
    neighbour merely touches its span — the Spectrum view (`Ft8OccupancySpectrum.svelte`)
    shows the **continuous** truth: signals as soft neutral shading at their true
    `low_hz`→`high_hz` positions, the daemon's clear offsets as ▾ ticks (★ = top pick)
    at their real positions (aligned with the Clear Offsets list, which never
    channelised), **click-anywhere or drag** for a continuous offset (clamped so the
    ±signal-width footprint fits; drag is Pointer Events + `setPointerCapture` so it
    tracks off-bar, mouse/touch/pen, with **live preview** — `previewOffset`, no
    localStorage write per move — and **persist-on-release**; arrow keys nudge, Home/End
    to the edges), and a **graded** pick
    status — **clear / near / sharing** (neutral status words, no advice — the operator
    judges) instead of binary red. Rationale: FT8 is
    continuous + overlap-tolerant (strong FEC; close/overlapping signals routinely both
    decode), so straddling a gap or sharing an offset is normal — the channelisation
    over-reported "full" and manufactured TX guilt. Grading is **position-only**
    (`Ft8Band` carries no strength — distinguishing a loud vs weak signal needs the
    waterfall's FFT magnitudes; backlog). Pure logic in `lib/utils/ft8Spectrum.ts`
    (`signalProximity` / `offsetFromFraction`, unit-tested). Both views write the one
    `selectedOffset`; a rendered scrolling **waterfall** remains a separate backlog item.
  - **TX offset only — by design (decided 2026-06-09).** It sets where *you*
    transmit, never an RX focus. FT8 RX is wideband (the daemon decodes the whole
    passband every slot, so you already hear every station regardless of offset),
    and good FT8 practice is to call on a *clear* slot rather than on top of the
    station you're working — TX and RX offsets are normally different (this is the
    WSJT-X red Tx marker, not the green Rx marker). Choosing *which station* to
    work is a separate callsign-based action for the step-(e) sequencer, not a
    frequency this strip sets.

### ARRL Field Day operating (answer + work) — SHIPPED + on-air validated 2026-06-28

SM operates **ARRL Field Day** over FT8 in **both directions** — search & pounce (answer
a `CQ FD`) and as the sought-after station (work a caller who calls you in FD). It does
**NOT** call `CQ FD` (no FD run/caller-CQ side), and every FD contact starts from an
**operator click** — including under the ADR 0059 auto-work run, which cannot pick up an
FD caller: `pickAnswererLocked` accepts only `msgGrid`, and an FD exchange parses as
`msgFdExchange`/`msgFdRExchange` (`caller_sequencer.go:390`, `sequence.go:120–126`). FD is a distinct FT8 message type (`i3=0`/`n3=3,4`) carrying **class +
ARRL/RAC section** instead of grid/report — go-ft8 v0.4.0's packer handles the encode and
decode, so the encode/modulate seam is unchanged (offline round-trip proof:
`TestFieldDay_RoundTrip`). Your own class/section come from config (`ft8.field_day`);
clicking is the only way a contact starts.

**Answer a CQ FD** (you S&P) — click a `CQ FD <call> <grid>` row (the SPA's `isCqFd`
routes the click to `mode:"fd"`; `Service.StartQsoFd` → `seqAnsweringFd` / `FdExchange`):

```
RX  CQ FD K1ABC FN42
TX  K1ABC 7Q5MLV 1D DX        (your exchange — class + section from config)
RX  7Q5MLV K1ABC R 2A EMA     (their R + exchange — parsed: msgFdExchange)
TX  K1ABC 7Q5MLV RR73         → QSO logs (CLASS=2A, ARRL_SECT=EMA, CONTEST_ID=ARRL-FD)
```

**Work a caller in FD** (you're the DX) — a station calls you with their exchange,
`<yourCall> <theirCall> <class> <section>`. The SPA's `parseDirectedToMeFd` recognises
that shape (distinct from a grid opening) and makes the row clickable; a plain click
routes to `mode:"fd"` with their class/section (`Service.StartWorkCallerFd` →
`seqWorkingFd` / `FdWorkExchange`):

```
RX  7Q5MLV K7IOC 1D WWA       (their call — their class/section captured from it)
TX  K7IOC 7Q5MLV R 1D DX      (your R + exchange)
RX  7Q5MLV K7IOC RR73
TX  K7IOC 7Q5MLV RR73         → QSO logs (CLASS=1D, ARRL_SECT=WWA, CONTEST_ID=ARRL-FD)
```

As a rare DX station the **work-a-caller** path is the dominant one (people call *you*
far more than you S&P). Both ladders are pure value-returning state machines (`field_day.go`)
driven one slot at a time by isolated handlers (`onSlotAnsweringFd` / `onSlotWorkingFd`,
kept separate from the standard path so it's untouched), with the same off-ramp
(`max_repeats`), final-rung `onDone` logging, and `sessionGen` guard as the standard
sequencer. The QSO maps to ADIF via `BuildQso` (their class/section → `CLASS` /
`ARRL_SECT`, plus `CONTEST_ID=ARRL-FD`); `CompletedQso` carries `Class`/`Section`.
**RST on FD:** the on-air exchange carries no report, but the QSO still logs both —
`RST_SENT` = our **measured SNR** of the station (the SPA passes our SNR of the clicked
decode as `their_snr`; `BuildQso` writes it like a standard QSO), and `RST_RCVD` = the
operator's `ft8.field_day.default_rst_rcvd` (applied in the e4 sink), because some OQRS
systems require both fields non-empty.

**Wire:** both `POST /v1/ft8/qso/start` and `.../work` take an optional `mode:"fd"`
(work also takes `their_class`/`their_section`); your class/section are read from config
by the daemon, never sent by the client (`ft8_field_day_unset` 400 if unset). The
`ft8-qso` SSE `QsoStatus` carries `fd:true` for an FD session.

**Known limits (2026-06-28):** the Operate-tab message **ladder still renders
standard-shaped** (the QSO works + logs, but the rung visuals aren't FD-aware yet); the
**Ctrl/Cmd+click pile-up queue does not support FD callers** (plain-click works them
directly); the config **SPA section dropdown** is pending (config.json-only for now).
On-air validated during ARRL FD 2026: **K7T, W6A** (answer) and **K7IOC** (work).

### Nonstandard / compound calls — the reduced type-4 ladder (ADR 0048) — SHIPPED (offline-gated; on-air pending)

A **nonstandard callsign** — prefix-compound (`PJ4/NA2AA`), odd suffix (`/D`, `/M`,
`/MM`), special-event — does not compress to a 28-bit standard call, so it cannot ride a
standard FT8 message. It needs a **type-4** message (`i3=4`), which **spells** the
nonstandard call and reduces the *other* call to a 12-bit **hash** rendered `<...>`. After
the type tag there is room only for `{blank, RRR, RR73, 73}` — **no grid and no signal
report on the wire** (a fixed-payload consequence, QEX Jul/Aug 2020 §type-4). So the
standard grid→report→73 ladder is unencodable for a type-4 partner; SM runs a **reduced
ladder** instead: **bare-calls → RR73 → 73**, no grid/report. Operator-click only — a
type-4 partner is never picked up by an auto-work run either, since `pickAnswererLocked`
accepts only `msgGrid` and type-4 carries no grid at all; the exception
is `/P`, which packs *standard* and already walks the normal ladder (`/R` packs nonstandard
but go-ft8 cannot yet decode it, so SM does not offer it — it would key a frame the far end
can't read back).

**Matching is on the SPELLED partner** (`from == TheirCall`). Our own standard call is
always **hashed** to `<...>` in a type-4 exchange, so the addressed call can't be verified
exactly — a hashed `<...>` in the `to` slot is treated as **presumed-us** while a single
exchange is active (a bounded, documented false-match risk; ADR 0048). SM does **not**
reimplement the 22-bit hash table: go-ft8 exposes no decoded-hash integer to compare
against, and the partner always spells itself, so a hash table buys nothing for matching.
A new additive `parseType4` accepts the `<...>` token that the standard
`parseMessage`/`looksLikeCall` (which drop hashed tokens) deliberately reject.

**Answer a CQ** (you S&P a nonstandard station) — click a `CQ PJ4/NA2AA` row (the SPA's
`isCqType4` routes to `mode:"type4"`; `Service.StartQsoT4` → `seqAnsweringT4` / `T4Exchange`).
Also reached by **double-clicking** any plain decode whose sender is nonstandard (the
directed-call gesture — `isNonstandardCall`):

```
RX  CQ PJ4/NA2AA
TX  PJ4/NA2AA 7Q5MLV          (bare opening — on air "PJ4/NA2AA <...>", our call hashed)
RX  <...> PJ4/NA2AA RR73      (their roger — matched on the spelled PJ4/NA2AA)
TX  PJ4/NA2AA 7Q5MLV 73       → QSO logs (RST_SENT = our SNR; RST_RCVD blank; no grid)
```

**Work a caller** (a nonstandard station calls you) — `Service.StartWorkCallerT4` →
`seqWorkingT4` / `T4WorkExchange` runs a single **RR73** rung (no report), logged after it
transmits. The daemon path is built + tested, but the **SPA trigger is deferred**: with our
call hashed on the wire (`<...> PJ4/NA2AA`) the browser can't distinguish "called me" from
"called someone else", so a nonstandard caller is worked via the **answer** path above (a
bare opening completes the QSO regardless of who initiated).

Both ladders are pure value-returning state machines (`type4.go`) driven one slot at a time
by isolated handlers (`onSlotAnsweringT4` / `onSlotWorkingT4` in `type4_sequencer.go`, kept
separate from the standard path — the ADR 0037 Field Day pattern), reusing the guaranteed-stop
keyer, slot timing, `sessionGen` guard, and final-rung `onDone` logging. **Logging is
degraded** (a completed exchange is still a QSO): `RST_SENT` = our measured SNR, `RST_RCVD`
**blank** (no report is exchanged — and unlike FD there is no config default), no grid, no
contest fields; `BuildQso` degrades cleanly with no branch. The enrich+submit path uses the
**spelled** call, so the country/DXCC row is written normally.

**Wire:** `POST /v1/ft8/qso/start` and `.../work` take `mode:"type4"` (no new routes, no
config identity — our own call is standard); the `ft8-qso` SSE `QsoStatus` carries
`type4:true` so the SPA renders the reduced ladder. **RF-safety gate:** `TestType4_RoundTrip`
proves every ladder message (both directions) encodes → modulates → decodes with the shipped
decoder, offline, zero RF. **Completion depends on the far station's client resolving our
hashed call**, which is inherently flaky in type-4 — some contacts won't complete; that is
the protocol, not an SM bug. **On-air validation pending** (needs a real nonstandard station
on the band); the ADR flips Proposed→Accepted then.

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
answerers for you (ADR 0033 caller-side sequencing: it calls CQ, auto-works one
answerer through RR73, logs it, and resumes — looping the pile-up until you Abandon).
Which answerer it picks each round is the **FT8 Settings tab → Call CQ → Answer**
knob: **First answerer** (`auto_first`, by decode order) or **Strongest signal**
(`auto_strongest`, highest SNR in the slot — clears the loud ones first). To choose
*which* callers to work and in what order yourself, use **pile-up callsign stacking**
instead: **Ctrl/Cmd+click** each calling-you decode to queue it, and SM works the stack
oldest-first (FIFO, see "Working a caller" above). Auto-answer Call CQ is the hands-off
option; the stack is the operator-curated one.

### Call CQ — the confirm-hold (why you no longer work the same station twice)

**Shipped 2026-07-26, from a dogfood diagnosis.** A Call-CQ contact logs the moment
its closing `RR73` transmits, and the contact is then cleared so the loop can work
the next station. If the partner did **not** copy that `RR73` they keep repeating
their `R-report` — but `pickAnswererLocked` only accepts a **grid** answer, so once
the contact is cleared those repeats are invisible. They eventually give up, restart
from the top with a grid answer, and get worked and logged a **second** time.

That is not theory. On 2026-07-26 the decode log caught **XE1GM** repeating
`7Q5MLV XE1GM R-07` **eleven times** at −9..−13 while the sequencer, having logged
and moved on, ignored every one. The same mechanism produced duplicate rows for
AC8MR, KI2Y and KE4IHI the night before — duplicates that were also uploaded to QRZ,
ClubLog and SM Cloud (QRZ accepted both copies; it does not dedupe).

So a completed Call-CQ contact now stays **listenable for one of the answerers'
slots** (`confirmHold`, `internal/ft8/caller_sequencer.go`):

- they send a bare `73` → they copied it; release the hold **in that same slot**, so
  its decodes still feed the normal answerer pick (no throughput lost)
- they send `RR73` → releases ONLY if their roger carried an **R-report**. Both
  `RRR` and `RR73` are what the caller ladder accepts as a roger, so a partner who
  MISSED our `RR73` repeats exactly that token — reading it as confirmation would
  release the hold at the one moment the re-send is needed. But a partner who rogered
  with an R-report and now sends `RR73` has moved PAST that rung, and advancing
  requires having received us (a partner who missed it repeats the R-report instead).
  Observed live: EW8DU closed with `RR73` rather than a bare `73`
- they send bare `RRR` → **never** a release. `RR73` carries the 73; `RRR` is an
  acknowledgement that still expects a closing `73`, so the sender may still be
  waiting on us. Every doubtful case resolves the same way: releasing early costs a
  duplicate QSO in three logbooks, holding on costs one slot
- silence → they copied it or have gone; release
- anything else addressed to us → they are still asking; **re-send the `RR73`**,
  bounded so a deaf partner can't hold the loop hostage. TWO bounds, because one
  counter cannot do both jobs: `confirmResendLimit` (2) is the RF budget, spent only
  when a re-send is ACCEPTED for transmission — a slot deferred by the late window or
  refused by the transmitter's single-flight/readiness gate never reached the air and
  costs nothing — and `confirmHoldSlotLimit` (5) is the lifetime, spent on every
  consulted slot whatever the outcome, so a rig that refuses everything still retires
  the hold instead of stalling the CQ loop on one contact

The QSO is already logged, so a re-send carries **no completion callback** and can
never log the contact twice. Both parameters came from the decode log rather than
guesswork: the repeat arrives in the **very next slot** (+15 s), and a partner who
copied it sends `73` (four for four in that session).

Deliberately **Call-CQ only**. The other Group B ladders go idle after their `RR73`,
so re-working a station there needs an operator click; the CQ loop is the one path
that re-works automatically. Log lines: `partner still asking after our RR73;
re-sending it` and `partner confirmed the contact; releasing hold`.

**It narrows the window, it does not close it.** Once the budget is spent the call is
forgotten, so a partner who heard none of our `RR73`s and later restarts with a grid
answer is worked as a fresh contact and logged again. That is deliberate: by then
they genuinely never received the roger, so working them again is correct on air and
is the only way they get their contact. The residual defect is the second ROW, and
the fix for that is duplicate detection/merge at log level — suppressing the re-work
would deny a station its contact to keep our log tidy.

*Known cosmetic gap:* during a re-send slot the ladder still reads `calling-cq`
(the contact is cleared), so the SPA shows CQ while an `RR73` is keyed. The daemon
log is authoritative.

### Decode log (`ALL.TXT`-style) — `ft8.decode_log.*`

**Rotation + archiving (2026-07-26).** The file used to grow without bound — WSJT-X
ALL.TXT behaviour, left for the operator to clear by hand. It now rotates through
lumberjack (the same library as `smd.log`): **10 MB** per file, **5** gzipped
backups, which holds roughly a month of continuous operating in a few MB. These are
NOT operator-configurable — a diagnostic stream has no tuning value and the defaults
already outlast any session. It is also created **0600** now (it was 0644), and an
existing log from an older build is tightened on open, because lumberjack copies the
EXISTING file's mode onto every rotated file and would otherwise propagate the wider
mode forever.

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
- **`ft8-qso`** → `QsoStatus{ active, role, their_call, their_grid, state, next_message,
  repeats, max_repeats, our_report, their_report, their_period }` —
  `our_report`/`their_report` are the exchanged signal reports (e.g. `-12`), empty until
  known, used to fill the ladder's `<RST>` slots; **`their_period`** ("even"|"odd", empty
  when idle) is the slots we process = the operator's RX parity, used by the SPA as the
  pile-up's single workable parity — the manual sequencer's active contact (step e3).
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

A slot the daemon **transmitted in is skipped entirely** — no decode, no
`ft8-occupancy` event — because its captured audio is our own signal (rig TX-audio
bleed). The SPA holds the prior RX-slot occupancy report and keeps its slot clock
ticking from the (empty) `ft8-decode` event that still fires; this is why the
busy/clear readout no longer flickers in lockstep with TX/RX.

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
a clear offset is obvious at a glance. But the pick is the **operator's**: the
strip is best-effort **guidance**, not a hard gate — SM does
**not** refuse or snap an *overlapping* selection. A daemon-side overlap-admission
gate was considered (review 2026-06-16 H1) and deliberately left out; enforcement
would fight the model in which the offset is the operator's to choose. The daemon DOES, however, reject an
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
to persist. Edited from **Settings → FT8** in the app shell
(`frontend/app/src/lib/config/Ft8Section.svelte`) — the only writer. The Band Activity
**filter funnel** merely reflects `hide_hashed_calls` in its "active" cue; it is
read-only there (the auto-save-on-toggle funnel belonged to the retired logging SPA). The
daemon does not consume these — they're pure SPA presentation — it stores + resolves them
(`types.ResolveFt8Display`), so a fresh config still yields sensible values. A save is
applied to the RUNNING view (`setFt8PrefsSaved` → `setFt8DisplayPrefs`), so no restart
and no reload.

| Key | Default | Meaning |
| --- | --- | --- |
| `history_max` | 100 | Band Activity row cap (clamped 10–2000) |
| `feed_mode` | `accumulate` | `accumulate` (roll slots up) or `single` (current slot only) |
| `highlight_unworked` | `#15803d` | **VESTIGIAL** — see below |
| `highlight_worked` | `#9ca3af` | **VESTIGIAL** — see below |
| `highlight_calling` | `#b45309` | **VESTIGIAL** — see below |
| `cq_to_top` | `false` | float CQ rows to the top of Band Activity (separators suppressed) |
| `hide_hashed_calls` | `false` | hide decodes with an unresolved hashed call (`<...>`); stations calling you still show |

**The three `highlight_*` keys are stored but not read by anything** (operator's
ruling, 2026-08-05). Their only consumer was the `frontend/logging` SPA, retired
2026-07-21; the app shell's Band Activity uses a **theme-aware palette** instead
(`Ft8BandActivity.svelte` `rowClass`), because a single operator-picked hex cannot
serve both light and dark — a row tint chosen for one is unreadable on the other. So
Settings → FT8 deliberately offers **no colour pickers**: a control the app cannot
honour is indistinguishable from a broken one. They are still **round-tripped
verbatim** on every save (`ft8_display` is a whole-block replace daemon-side, so
omitting them would erase a hand-set config.json value), and `ResolveFt8Display` still
defaults them — so nothing breaks if the decision is revisited.

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

Daemon-owned TX, **operator-initiated** — a human starts every RUN (the click plus
arming TX), and the rungs then auto-advance. Note "run", not "QSO": under `auto_first`
Call-CQ and the ADR 0059 auto-work run, subsequent contacts within a started run are
picked up without a further click. **Daemon-initiated sequencing is out of scope and
unsupported — the QEX FT8 specification forbids automatic operation.** SM does not
enforce attendance — the precise claim is stated under §"Step (e) sequencer — design". Reusing the ADR 0027 guaranteed-stop
discipline — `tx_on`/`tx_off` are never `exposed`, only the TX controller keys
the rig. Build order is **RX-safe first**; RF first enters at (d) — (a)–(c) are
audio-only / offline.

| Step | What | State |
| --- | --- | --- |
| (a) | Per-slot occupancy detector + SSE + SPA readout | **done** |
| (b) | GFSK modulator + offline round-trip vs the shipped decoder (zero RF) | **done** |
| (c) | Audio-output device (malgo, `//go:build cgo`, fail-soft, probe-listed) | **done** |
| (d) | PTT + slot-timing controller (daemon-owned guaranteed stop) | **done — bench path; ADR 0030** |
| (e) | Manual sequencer + QSO logging; **interactive picker** | e1–e4 shipped 2026-06-10 (TX path, resolver, sequencer ADR 0031, logging) — **answer-a-CQ complete + logged**. **Call-CQ `auto_first` shipped 2026-06-12 (ADR 0033)** — Call CQ → daemon works the pile-up (first answerer) → logged, looping until Abandon. The **SPA pile-up stack shipped 2026-06-17** (Ctrl/Cmd+click a caller → FIFO → work-a-caller drain — the curated path for callers outside a CQ run). **`caller_answer_mode=operator_pick` shipped 2026-08-07 (ADR 0065)** — a CQ run lists answerers in the drawer and the operator commits one via `POST /v1/ft8/cq/pick` (config.json-only knob). Automatic/unattended sequencing is out of scope — QEX-forbidden. |

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
  targets. **Controller-side decodability guards (2026-07-25, two review rounds).**
  The sequencer's window is checked *before* the encode, the CAT key and the pre-key
  settle, so `transmit()` adds two independent checks — the distinction that matters
  is that a transmission which cannot be decoded must be reported as **failed**, not
  merely short: success is what logs the QSO, so a badly delayed final 73/RR73 could
  otherwise book and forward a contact the other station never decoded. A failure
  instead leaves the exchange in `txConfirming` to retry next cycle, and PTT still
  drops on both reject paths.
  1. **Head-loss floor**, before `Play`: fail if truncation has reached FT8's middle
     Costas array (`maxDecodableSkip`, ~5.92 s of the 12.96 s waveform; the arrays
     sit at tone indices 0-6/36-42/72-78 and the receiver needs two of the three).
     Deliberately looser than the sequencer's implied 4.0 s, so it never tightens
     the late window that already works on air.
  2. **Slot-overrun check**, after `Play` returns: `Play` returns once the device is
     *running*, and the production player does device enumeration + `malgo.InitDevice`
     + `device.Start` inline, none of it context-bounded (seconds, on a waking USB
     codec or contended PipeWire). That delay is **uncompensated shift** — unlike CAT
     keying latency, which the truncation absorbs — so RF must still stop inside its
     own slot: `elapsed + audio + txPlayTail ≤ txAudioBudget` (14.5 s from nominal,
     leaving ~1.29 s for device-start latency). The `txPlayTail` reserve is
     load-bearing — the player's `done` only means samples *reached* the device, and
     the drain wait exists because the buffered tail is still emitting, so counting
     PCM duration alone would let this guard permit the overrun it prevents. Overrun
     ⇒ halt output and fail (preserving `ctx.Err()` so a disarm during a slow start
     stays a normal stop, not `ft8_tx_failed`). This is the only guard that covers an
     **untruncated** rung: a manual next-slot CQ drops no head at all, so no
     head-loss test can see a slow device start shift the whole waveform off DT. **First-rung immediate-fire (2026-06-12):** `StartQso` takes `now` and a
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
  - **Directed call — double-click ANY plain decode row (frontend/app, 2026-07-13).**
    The WSJT-X double-click semantic: the operator calls the **sender** of a plain
    (non-CQ, not-calling-you) line without waiting for their CQ — a DX running a
    pile-up can go many minutes between CQs, and waiting costs the contact (the
    operator's T22TT case). **No daemon change**: the opening message of a directed
    call is identical to answering a CQ, and `Sequencer.StartQso` never required the
    heard message to BE a CQ — it takes their call + the slot they transmitted in
    (fixing their parity; we TX opposite, i.e. into their RX slot) + the offset. SPA:
    `parseSender` (`utils/ft8Message.ts`) extracts the sender (+ their grid when the
    payload is one; RR73 excluded; **hashed senders rejected** — `<...>` has no
    known callsign to encode); `Ft8BandActivity` plain rows with a callable sender
    render as buttons (dotted-underline hover, "Double-click to call X" title) wired
    to the same `answerCq` action with `fd:false`. **Double-click, not single** —
    plain rows are dense non-actionable text; starting a transmission from them must
    be a deliberate gesture (single-click stays inert). Same guard chain as
    answer-a-CQ (TX armed, CAT live, no active QSO, offset picked, freq known,
    not-worked-this-session), extracted as `txPreflight`. The ● "working now" row
    marker also lights on the worked station's plain transmissions (mid-exchange
    lines aren't addressed to us, so they'd otherwise lose the marker).
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
- **Daemon-initiated sequencing is OUT OF SCOPE and NOT SUPPORTED.** The FT8
  protocol forbids automatic operation (per the QEX FT8 protocol specification),
  and unattended operation is illegal without a special licence in many
  jurisdictions. SM therefore supports **only operator-initiated** FT8: every RUN
  is started by a human — answer-a-CQ (click a CQ), work-a-caller (click a caller),
  and call-CQ (press Call CQ). Nothing starts a run that the operator did not.
  **Per CONTACT is a weaker claim, and was overstated here until 2026-07-30:**
  `auto_first`/`auto_strongest` Call-CQ work each answerer, and an ADR 0059
  auto-work run works each caller, with no further click. What is guaranteed is
  that the RUN was deliberate, not that every contact in it was.
  **The one enforced presence check is the OPEN VIEW, not the operator** — see the
  linger disarm below.

**Scope order: answering a CQ first, then calling CQ** (calling CQ adds
multi-answerer management).

**Manual/auto seam (the key idea):** a **pure next-message resolver** shared by
both; the only difference is the **send policy**. **RATIFIED 2026-06-10 (ADR 0031):**
the operator's judgement is *whom to work* (the click) + arming TX, and rung
advance is mechanical — so within a QSO the rungs **auto-advance** (the daemon
walks the ladder via the e2 resolver; the operator intervenes only to
retry/abandon). i.e. **manual = operator-initiated-per-QSO with automatic rung
advance** — on the manual paths (answer-a-CQ, work-a-caller) the operator initiates
every QSO with the click; on a RUN (Call-CQ, ADR 0059 auto-work) the click starts the
run and subsequent contacts follow from it. Either way the daemon initiates nothing on
its own. A daemon-*initiated* mode would be automatic/unattended operation,
which is **out of scope and unsupported** — the QEX FT8 specification forbids
automatic operation (see above).

Stated precisely, because the looser phrasing overclaimed: SM does not ENFORCE
attendance. A Call-CQ run keeps working answerers until Abandon, and an ADR 0059
auto-work run keeps working callers until Abandon, so an operator can start either and
walk away from the desk and nothing stops it. What SM guarantees is that the run had
to be started deliberately; remaining at the rig is the operator's obligation under
their licence, not a property the software checks (operator, 2026-07-27).

**But one presence check IS enforced, and the phrase "nothing stops it" is wrong
without it (found 2026-07-30, grepping the tree rather than trusting this
paragraph): the FT8 VIEW must stay open.** When the last `/v1/ft8/events` subscriber
goes away past `captureLinger`, `Service.onLingerExpired` calls `disarmTx(false)` —
dropping PTT and abandoning any active QSO — and only then releases the capture
device; the disarm is deliberately NOT gated on `capturing`, so it still runs when
the capture loop has already died (`internal/ft8/service.go:398–429`, 2026-07-25
review). The code calls this the "attended-only guarantee", which is where the
over-broad word entered the docs: what it enforces is an open SSE subscription, not
a human. **So the accurate statement has two halves** — walking away with the
browser open does not stop a run; closing the browser stops it after the linger. A
reconnect inside the linger window keeps the session and re-arms TX. Per-rung confirm
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
`auto_first` for callers outside a CQ run. **`operator_pick` shipped 2026-08-07 (ADR
0065 decision 3)**: under `caller_answer_mode=operator_pick` a CQ run lists its
answerers (`ft8-qso` `answerers`, 3-min staleness) in the same drawer and the operator
commits one via `POST /v1/ft8/cq/pick`; CQ continues until then, parks never
auto-pick, and the run resumes CQ after each contact. Spec:
`internal/ft8/operatorpick_test.go`.
**Daemon-initiated sequencing is out of scope and unsupported — the QEX FT8
specification forbids automatic operation.**

`go-ft8`'s `EncodeStandardMessage` covers standard structured messages, **including
the standard `/P` variant** (go-ft8 ≥ **v0.3.5**). SM works `/P` stations end to end
with **no SM code change** — every TX guard decides by trying `EncodeStandardMessage`
and skipping on error, so an upstream encoder gain flows straight through (proven
offline in `internal/ft8/modulate_test.go`: `TestEncodeStandardMessage_Portable` +
`TestModulate_RoundTrip_Portable`). **Type-4 compound/nonstandard calls (`PJ4/NA2AA`,
`/D`, `/MM`, …) now have their own reduced ladder** (bare-calls→RR73→73, ADR 0048 — see
"Nonstandard / compound calls" above); **free text** is still unbuilt. SM owns tones →
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
