---
number: 0037
title: FT8 ARRL Field Day — answer a CQ FD and work a caller in FD
status: Accepted
date: 2026-06-28
---

# 0037 — FT8 ARRL Field Day — answer a CQ FD and work a caller in FD

## Context

ARRL Field Day is a major annual operating event. Its FT8 exchange is **not** the
standard grid/report contact: it carries the station's **class** (number of
transmitters + category letter, e.g. `2A`) and its **ARRL/RAC section** (e.g. `EMA`,
or `DX` for stations outside US/Canada). This is a distinct FT8 message type
(`i3=0` / `n3=3,4`) — RX-incompatible with the standard `i3=1` decoder, so it needs
the codec library's support, not a config flag.

The trigger was concrete: 7Q5MLV (Malawi, rare DX) wanted to make Field Day FT8
contacts during the live 2026 event. go-ft8 **v0.4.0** (landed 2026-06-28) added the
FD packer (`EncodeStandardMessage` encodes/decodes FD exchange messages) and the
section contract (`ARRLFieldDaySections()` / `ValidARRLFieldDaySection()`). The
existing FT8 TX machinery (ADR 0029/0030/0031/0033) — slot timing, the offline
round-trip RF gate, the guaranteed-stop keyer, the manual attended sequencer — was
already on air and could be reused.

## Decision

Support Field Day over FT8 in **both directions, attended-only**, building on the
existing sequencer:

1. **Answer a `CQ FD`** (search & pounce). Click a `CQ FD <call> <grid>` decode → send
   our exchange `<them> <us> <class> <section>` → receive their `R <class> <section>`
   → send `RR73` → log. (`FdExchange`, `seqAnsweringFd`, `StartQsoFd`.)
2. **Work a caller in FD** (we are the sought-after DX). A station calls us
   `<us> <them> <class> <section>`; click it → send `<them> <us> R <ourClass>
   <ourSection>` → receive their `RR73` → send `RR73` → log. (`FdWorkExchange`,
   `seqWorkingFd`, `StartWorkCallerFd`.) Added reactively mid-contest when callers
   piled on 7Q5MLV and there was no way to work them — it is the **dominant** path for
   a rare DX station.
3. **We do NOT call `CQ FD`** (no FD run / caller-CQ side). Out of scope, consistent
   with the attended-only stance and the original scoping.

Mechanics:

- **Encode/decode is go-ft8's** (v0.4.0 packer handles FD messages), so the
  encode→modulate→PTT seam is **unchanged**. Proven offline before any RF by
  `TestFieldDay_RoundTrip` (encode → modulate → decode every ladder message).
- **Operator identity** (our class/section) is daemon config `ft8.field_day.{class,
  section}`, read server-side — never client-supplied. Class validated by a syntactic
  rule in `internal/types`; **section validated against go-ft8's canonical list in
  `internal/config`** (`types` stays stdlib-only, so it cannot import go-ft8).
- **Wire:** `POST /v1/ft8/qso/{start,work}` take an optional `mode:"fd"` (work also
  carries the caller's `their_class`/`their_section`, parsed SPA-side). `QsoStatus`
  carries `fd:true`.
- **Logging:** `CompletedQso.Class/Section` → `BuildQso` → ADIF `CLASS` / `ARRL_SECT`
  (+ `CONTEST_ID=ARRL-FD`). `types.QsoDetails` gained `Class`/`ArrlSect` (persist via
  the additional_data blob — a one-line ADIF-field change, no migration).
- **The two FD slot handlers are ISOLATED parallel copies** of the standard
  answer/work handlers (`onSlotAnsweringFd`, `onSlotWorkingFd`), not branches inside
  the standard ones — so building FD live during the contest could not destabilise the
  working standard FT8 path.

## Alternatives considered

- **Embed the ARRL/RAC section list in SM** (instead of validating via go-ft8). Rejected:
  go-ft8 owns it for encode, so a second copy drifts. SM defers to
  `ValidARRLFieldDaySection`.
- **Branch the standard slot handlers for FD** (one handler, `if fd {…}`). Rejected for
  the contest build: the standard handler is intricate, concurrency-sensitive, and
  on-air; isolated copies kept the proven path untouched. Worth revisiting as a refactor
  if the duplication becomes a maintenance burden (see Triggers).
- **Call CQ FD** (FD run side). Out of scope — attended-only, and the answer + work
  paths cover the operator's need.

## Consequences

- Both FD directions are live and **on-air validated during ARRL FD 2026**: K7T, W6A
  (answer); K7IOC `1D WWA` (work).
- Four near-parallel slot handlers now exist (`onSlotAnswering[Fd]`,
  `onSlotWorking[Fd]`) — accepted duplication, per "build specific, not generic."
- **Pending polish:** the Operate-tab message ladder still renders standard-shaped for
  FD sessions (works + logs, but the rungs aren't FD-aware); the Ctrl/Cmd+click pile-up
  queue does not yet support FD callers (plain-click works them directly); the config
  SPA has no Field Day section editor (config.json-only).
- Operational gotcha recorded: after a deploy, an open browser tab runs the old SPA, so
  FD-caller rows look un-clickable until a hard-refresh — the daemon was serving the new
  SPA correctly.

## Triggers to revisit

- The four-handler duplication grows hard to keep in sync → factor the slot machinery
  behind a small exchange interface.
- A non-ARRL contest exchange is wanted → generalise beyond `CONTEST_ID=ARRL-FD`.
- The config SPA Field Day editor lands → wire the section dropdown from
  `ARRLFieldDaySections()`.

## References

- ADR 0029 (FT8 transmit), 0030 (PTT/first RF), 0031 (manual send policy), 0033
  (caller-side + work-a-caller), 0024 (go-ft8 integration).
- Code: `internal/ft8/field_day.go`, `sequence.go`, `sequencer.go`, `work_sequencer.go`,
  `servicetx.go`; `internal/api/handler_ft8_qso.go`; `internal/types/{ft8,qso_details}.go`;
  `frontend/logging/src/lib/{utils/ft8Message.ts,api/ft8qso.ts,ui/panels/Ft8Panel.svelte}`.
- `docs/ft8.md` "ARRL Field Day operating"; memory `project_sm_ft8_field_day`.
