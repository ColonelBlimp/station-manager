---
number: 0048
title: FT8 reduced type-4 ladder — work nonstandard/compound calls
status: Proposed
date: 2026-07-14
---

# 0048 — FT8 reduced type-4 ladder — work nonstandard/compound calls

## Context

A nonstandard callsign — compound (`PJ4/NA2AA`), odd suffix (`/D`, `/MM`),
special-event (`YW18FIFA`) — does not fit the regular shape that compresses to a
28-bit standard callsign, so it cannot ride a standard FT8 message (`i3=1`). It needs
a **type-4** message (`i3=4`), which spells the nonstandard call in ~58 bits and
reduces the *other* call to a 12-bit hash (`<...>`). After the type tag that leaves
~2 bits — enough only for `{blank, RRR, RR73, 73}`. **There is no room for a grid or
a signal report on the wire.** This is a protocol consequence of the fixed 77-bit
payload, not a design choice (QEX Jul/Aug 2020, §type-4).

go-ft8 v0.7.0 already **decodes** type-4 (a `CQ PJ4/NA2AA` reaches Band Activity —
`TestCompoundCQ_Decodes`), but SM cannot **complete a QSO** with one: the standard
sequencer opens with `<them> <us> <grid>` and exchanges a report, and those rungs are
**unencodable** for a type-4 partner (`TestPrefixCompound_EncoderBoundary`), so
`StartQso` fails soft (`ErrTxBadMessage`). The operator hit this on air — a `/D`
station was unworkable — and prioritised the fix NEXT (2026-07-14).

Empirical ground truth (probed against v0.7.0, both directions of a `7Q5MLV` ↔
`K1ABC/D` contact): the nonstandard partner is **always spelled** in what we receive;
our own standard call is **always hashed** to `<...>`; the **bare two-call opening**
(`K1ABC/D 7Q5MLV`) encodes; `RRR`/`RR73`/`73` encode; a report (`+05`) is rejected.
The existing FT8-TX machinery (ADR 0029/0030/0031/0032/0033) and the **Field Day
isolated-parallel pattern** (ADR 0037) are directly reusable.

## Decision

Build a **reduced type-4 QSO ladder** — `bare-calls → RR73 → 73`, no grid/report — as
an **isolated parallel path** cloning the Field Day template, for **answer-a-CQ and
work-a-caller only, attended-only**. Match inbound replies on the **spelled
nonstandard partner call** (no hash resolution). Log the degraded contact by
mirroring FD: measured SNR → `RST_SENT`, blank/default `RST_RCVD`, no grid.

## Alternatives considered

### Inbound matching — spelled-partner match (chosen) vs hash resolution

Every type-4 message we receive spells the partner's nonstandard call in full and
hashes *our* standard call to `<...>`, so matching on `from == TheirCall` is reliable;
the hashed `<...>` in the `to` slot is treated as presumed-us while a single exchange
is active. **Persistent `*ft8.Decoder`** (WSJT-X-style auto-resolution of `<...>`) was
rejected: it is a larger RX-path change (SM decodes stateless per slot via
`DecodeMessagesChecked`), touches decode determinism and its test surface, and — worse
— it learns calls only from their *spelled* side, but our standard call is *hashed* on
air in a type-4-only exchange, so it may never resolve `<...>` to us anyway, buying
nothing the spelled partner doesn't already give. **SM-side `hashCall` reimplementation**
was rejected because go-ft8 exposes no decoded-hash integer to compare against, making
it useless for matching (it would only help display, which is out of scope — hashed
partners of *other* stations are already hidden by `HideHashedCalls`).

### QSO logging — mirror Field Day (chosen)

No report is ever exchanged, so we log the measured SNR as `RST_SENT` and leave
`RST_RCVD` blank (or a config default, per `Ft8FieldDayConfig.DefaultRstRcvd`), with no
grid; `BuildQso` degrades cleanly. **Refusing to log** was rejected — a completed
exchange *is* a QSO (the invariant), and this is a real contact. **Fabricating a
`599`** was rejected — logging a report that was never exchanged is dishonest data.

### Path structure — isolated FD-clone (chosen) vs branch the standard path

The type-4 ladder is added as a parallel mode/exchange/handler/snapshot, leaving the
live standard path untouched — the pattern ADR 0037 proved. **Branching inside the
standard ladder** was rejected: it risks the daily-driver path for an edge case, the
exact reason FD was built parallel.

### Scope — answer + work only in v1 (chosen)

These cover the operator's real case (working a DXpedition / compound station).
**Type-4 Call-CQ** (us calling CQ and auto-working nonstandard answerers) was deferred:
it is a `caller_sequencer` clone plus pile-up interaction — more surface, no present
need.

## Consequences

- SM can **complete and log** a QSO with any nonstandard call, attended.
- **No RST on these rows** (`RST_RCVD` blank/default) — awards/analysis that assume a
  report see a gap here; this matches WSJT-X and is the protocol's doing, not a defect.
- A **bounded false-match risk**: if the partner acks a *different* station inside our
  exchange window we could misread it as ours. Bounded because FT8 stations ack one QSO
  at a time, the operator is present, and the silence off-ramps fire. Documented caveat.
- **Completion depends on the far station's client resolving our hashed call**, which is
  inherently flaky in type-4 — some contacts won't complete. That is the protocol, not
  an SM bug; do not chase it as one.
- A **new `parseType4` path** that accepts `<...>` tokens — additive; the standard
  `parseMessage`/`looksLikeCall` (which deliberately drop hashed tokens) are untouched.
- ~1 week of sequencer/protocol work, **RF-safe until on-air (Phase 5)** behind the
  offline round-trip gate (`type4_roundtrip_test.go`, mirroring `fd_roundtrip_test.go`).
  Reuses the guaranteed-stop keyer, slot timing, and encode-guard — **no new RF-safety
  surface**.

## Triggers to revisit

- **go-ft8 adds type-4 grid/report forms** (`TestPrefixCompound_EncoderBoundary` flips)
  → a report could then be exchanged and this reduced ladder could merge back into the
  standard path.
- **The false-match risk shows up on air** (a logged QSO that never completed) → add
  hash confirmation via a persistent decoder for the type-4 RX path.
- **Operators want to Call-CQ and auto-work nonstandard answerers** → build the type-4
  caller path.
- **Display of other stations' real (unhashed) partners is wanted** in Band Activity →
  reconsider the persistent `*ft8.Decoder` for the whole RX path.
- **Flip status Proposed → Accepted** once the offline round-trip gate passes and a real
  nonstandard station is worked end-to-end on air.

## References

- ADR 0029 (FT8 TX manual sequencing), 0030 (PTT/slot controller), 0031 (manual send
  policy / auto-advance), 0032 (synchronised-truncate timing), 0033 (caller-side
  sequencing), **0037 (Field Day — the isolated-parallel template this clones)**.
- go-ft8 v0.7.0 (`EncodeStandardMessage`, type-4 encode/decode; no exported hash API).
- `internal/ft8/`: `sequence.go`, `caller.go`, `field_day.go`, `sequencer.go`,
  `work_sequencer.go`, `servicetx.go`, `modulate.go`, `qsolog.go`;
  `compound_roundtrip_test.go` (encoder boundary), `fd_roundtrip_test.go` (RF-safety
  gate template).
- `docs/backlog.md` → "FT8 — work type-4 compound calls"; `docs/ft8.md`.
- QEX Jul/Aug 2020, Franke/Somerville/Taylor — the 77-bit type-4 message format.
