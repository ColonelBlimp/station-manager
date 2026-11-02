# W-0025 — RTTY in Station Manager

**Status:** Open — design only; no code. Depends on the `go-rtty` library, which is itself at
proposed-design stage (see *Dependency*).
**Opened:** 2026-11-02 (operator: "I am going to add support for RTTY to SMD"; "open a W-dossier").
**Outcome:** The operator can receive and transmit RTTY from Station Manager — decode shown live,
transmit through the same guaranteed-stop keying path as tune and FT8, and completed contacts logged
as RTTY — without weakening any existing TX-safety or logging invariant.

`W-0025` is an immutable identity. Its status may change, while priority and ranked position live only
in [`docs/backlog.md`](../backlog.md).

## Dependency — the `go-rtty` library

The modem is a separate project, `github.com/ColonelBlimp/go-rtty` (module path from its `go.mod`;
local tree `~/Development/go-rtty`). Its `docs/RTTY_DESIGN.md` at commit `132e007` ("Tighten RTTY
design contracts and spectral acceptance criteria", 2026-11-02; a proposed design, not an
implementation) specifies a stateful receiver (`Decoder.Process` over mono `[]int16` at 12 or 48 kHz), a text
and framing codec, and a pull-based AFSK transmitter (`Transmitter.QueueText` / `ReadPCM` / `End`).
It leaves audio devices, radio control, PTT, presentation and logging to the application and expects
Station Manager to consume it "through an adapter in that project". Version 0.1 scope: one caller-
selected signal per decoder, 45.45 baud, 170 Hz shift, US figures table, fixed tuning, explicit
polarity. Its delivery plan puts a usable receiver and transmitter at its stages 3–5.

A review of the earlier draft (2026-11-02) raised two P2 gaps: no measurable spectral criterion before
on-air transmission, and an unspecified rule for detecting the next start between back-to-back
characters. At `132e007` both have explicit design contracts — a numeric spectral acceptance gate
(`RTTY_DESIGN.md` *Spectral acceptance*, with `docs/RTTY_TX_SPECTRUM.md`) and stated acquisition and
inter-character rules with a dense-traffic search window — but their implementation and validation
remain outstanding. The same commit states that version 0.1 has **no idle or total-transmission
timeout**: an active transmitter emits mark indefinitely until `End`. Bounding a transmission is
therefore Station Manager's job (open choice 4 below).

**Nothing in this dossier may be built before the library offers the contract it depends on.** Design
work here can proceed in parallel.

## What Station Manager already has that RTTY must fit

Verified against the tree on 2026-11-02; read the cited owner before relying on any of it.

- **One keying path.** Tune and FT8 share the single guaranteed-stop / single-flight TX controller;
  only it keys the rig, and `tx_on` / `tx_off` are never exposed through the generic command surface
  (root `AGENTS.md`; tune: [ADR 0027](../decisions/0027-tune-carrier-control.md); FT8 slot PTT:
  [ADR 0030](../decisions/0030-ft8-tx-ptt-slot-controller.md); detection is not a guarantee:
  [ADR 0057](../decisions/0057-tx-safety-scope-cat-confirmation-is-detection-not-guarantee.md);
  stuck-TX alarm: [ADR 0051](../decisions/0051-tx-state-uncertainty-and-stuck-tx-alarm.md)).
- **Operator initiation.** FT8 sessions are operator-initiated; the daemon may continue a run the
  operator started but never begins one (root `AGENTS.md`). The same rule is assumed for RTTY.
- **Audio.** `internal/audio` has `capture` and `playback` packages used by the FT8 pipeline
  ([ADR 0024](../decisions/0024-ft8-external-lib-and-live-pipeline.md)); FT8 decodes 12 kHz mono, the
  rate `go-rtty` 0.1 also accepts.
- **FT8 as a subsystem.** FT8 lives in `internal/ft8`, isolated from `qsoservice`, `config` and
  `adif`, with QSO assembly and submission at injected boundaries
  ([ADR 0021](../decisions/0021-ft8-as-sm-subsystem.md); `internal/ft8/AGENTS.md` invariants,
  including "never key without positive frequency confirmation" and per-frequency attribution of a
  capture window). FT4 became a profile of that subsystem
  ([ADR 0080](../decisions/0080-ft4-as-a-profile-of-the-ft8-subsystem.md)); RTTY is not a profile of
  it — a continuous character stream, not timed slots.
- **The rig's own RTTY mode is already used — for tune.** The tune carrier keys the FTdx10 in RTTY mode
  with no data (ADR 0027), and the rig reports `RTTY-U` while tuning. AFSK RTTY through the audio path
  would instead run in a data mode (as FT8 uses `DATA-U`, config `bridge.ft8_mode`).
- **Operating views.** The router has Phone/CW, FT8 and FT4 views; `modeRestore.svelte.ts` keeps each
  view's operating point across a switch (W-0012).
- **Contesting.** [W-0023](W-0023-contest-logging.md) and
  [ADR 0096](../decisions/0096-contest-occurrences-reserved-serials-and-submission-export.md) own
  contest occurrences, reserved serials and export; RTTY contests (e.g. a UK/EI DX RTTY-style event or
  the RTTY Roundup) would reuse them rather than add a parallel model.

## Open choices (for the operator; record rulings here)

1. **AFSK or FSK.** AFSK through the audio path in a data mode (what `go-rtty` 0.1 generates), or the
   rig's own FSK RTTY mode keyed by a serial/CAT line (outside `go-rtty` 0.1's scope; its `FrameCodes`
   output is the stated basis for a future FSK adapter). Polarity mapping (USB/LSB, normal/reverse) is
   the application's job in either case.
2. **Where the subsystem lives.** A new `internal/rtty` beside `internal/ft8`, with the same isolation
   and injected-boundary rules, versus extending an existing package.
3. **Audio sharing.** Whether RTTY and FT8 can listen at the same time (one capture feeding both) or
   are mutually exclusive views, and how the playback path is claimed for transmit.
4. **Transmit model.** How an open-ended RTTY transmission (type, send, idle on mark, end) maps onto
   the guaranteed-stop controller built for bounded FT8 slots and a timed tune carrier: maximum
   transmission time, idle limit, what "stop" does mid-character, and the stuck-TX alarm's role.
5. **Receive tuning.** Click-to-tune on a waterfall/spectrum versus a fixed audio centre, given
   `go-rtty` 0.1 has fixed tuning and no AFC; one decoder or several caller-selected channels.
6. **The operating view.** A new RTTY view (receive text, transmit buffer, macros) and how much it
   shares with the Phone/CW logging card (callsign capture from received text, contest exchange).
7. **Logging.** ADIF mode `RTTY` (submode none), QSO assembly from the view, frequency attribution
   (dial plus audio offset, as FT8 files its contacts), and what counts as a completed contact.
8. **Order of slices.** Receive-only first (no RF risk, decode evidence on air) before any transmit.

## Constraints that are not open

- Never initiate a live transmission, keyed test or hardware experiment without the operator's
  agreement for that occasion; prefer offline and passive evidence (root `AGENTS.md`).
- Only the guaranteed-stop controller keys TX; no second keying path.
- A decode is not a QSO; ordinary RTTY has no CRC or FEC, so a framed character is an observation,
  not a verified message (`go-rtty` design).
- Enrichment never blocks logging; QSO and upload-queue writes stay atomic.
- Claims about the FTdx10's RTTY/FSK behaviour, PipeWire routing or CAT need a citation or a
  measurement.

## Next

Rule on the open choices above (receive-first is the recommended first slice), then a design record
— an ADR for the subsystem boundary and the transmit model — before any code, and only once the
`go-rtty` contract it depends on exists.
