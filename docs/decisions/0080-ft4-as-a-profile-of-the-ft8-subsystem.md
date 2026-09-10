---
number: 0080
title: Add FT4 as a second timing and modulation profile of the FT8 subsystem
status: Proposed
date: 2026-09-10
---

# 0080 — Add FT4 as a second timing and modulation profile of the FT8 subsystem

## Context

The Africa FT4 DX Contest runs 15:00–18:00 UTC on Saturday 2026-09-12 on 80, 40 and 20 m, with the
recommended dial frequencies 3576 kHz, 7047.5 kHz and 14080 kHz (USB), "the default FT4 frequencies
in WSJT-X". The exchange is a signal report and the four-character Maidenhead locator; the mode is
"Standard FT4 format … as implemented in WSJT-X V2.2.2 or later"; Fox & Hound and the dedicated
contest message formats "may not be used"; only one signal may be transmitted at a time; power is
limited to 100 W. Logs go in as the SARL Excel sheet, produced from the ADIF log with SARL's
converter, by 21:59 UTC on Thursday 2026-09-17 (2026 SARL Contest Manual v1.1, "The Africa FT4 DX
Contest", pp. 45–46). The operator wants to work it with Station Manager end to end — daemon, SPA,
forwarders and SM Cloud — as a field test.

That exchange is exactly the standard message ladder the subsystem already sequences: the answer
ladder opens with `<them> <us> <grid>` (`internal/ft8/sequence.go:306`), a Call-CQ run carries the
grid in the CQ, and the completion logs the partner's grid (`internal/ft8/qsolog.go:41`). The
contest's ban on contest-format messages removes the one thing the library cannot yet encode
(free text, telemetry and the RTTY-Roundup / EU-VHF encoders are "not yet accepted",
`go-ft8/ft4/encode.go`). FT4 and FT8 share the 77-bit source messages, the (174,91) LDPC code and
the message types; they differ in slot length, symbol rate, tone count, Gaussian pulse and Costas
arrays (Franke, Somerville, Taylor, *The FT4 and FT8 Communication Protocols*, QEX Jul/Aug 2020,
§3–5, Table 4):

| Parameter | FT8 | FT4 |
| --- | --- | --- |
| T/R period | 15 s | 7.5 s |
| Symbol duration | 0.160 s (1920 samples at 12 kHz) | 0.048 s (576 samples) |
| Tones / spacing | 8 / 6.25 Hz | 4 / 20.833 Hz |
| Gaussian pulse | BT = 2 | BT = 1 |
| Channel symbols | 79 (3 × 7 Costas + 58 data) | 105 (ramp, 4 × 4 Costas, 3 × 29 data, ramp) |
| Waveform | 12.96 s | 5.04 s |

The library side is in flight. Station Manager pins `go-ft8 v0.8.0`; the FT4 encoder exists on
go-ft8 `master` (`31d70fb feat(ft4): add protocol-level FT4 encoder`, unreleased, validated against
a pinned WSJT-X `jt9` by `task test:oracle-ft4`); FT4 receive-side decoding "is not yet implemented"
(go-ft8 `README.md`). The library's stated boundary stays: it stops at protocol artifacts, and audio
generation, transmit scheduling, PTT and rig control belong to the caller.

Station Manager is FT8-only by construction, not by design. The subsystem hard-codes the slot
geometry as package constants (`internal/ft8/scheduler.go:12–25`: `slotSeconds = 15`,
`SlotDuration`, `SlotSamples`), a whole-second boundary lattice (`nextSlotBoundary`,
`scheduler.go:483`), the FT8 GFSK parameters (`modulate.go:31–53`), the FT8 Costas layout for
late-start truncation (`modulate.go:190–199`), the 0.5 s nominal start and the audio budget
(`txcontroller.go:33–41`), the 4.5 s late window (`sequencer.go:41`), an FT8 signal width for
occupancy (`occupancy.go:42`), the slot parity lattice (`occupancy.go:183`), the ADIF mode stamped
on a logged QSO (`qsolog.go:42`, which PSK Reporter spots inherit at `qsolog.go:199`), the
transmit line in the decode log (`decodelog.go:261`), the FT8 dial table
(`internal/types/ft8.go:269`), the QSO service's FT8-only report rule (`internal/qsoservice/update.go:209`,
`submit.go:305`), and the SPA's 15-second countdown and parity (`Ft8Operate.svelte:61–68`,
`utils/ft8Parity.ts:16`). Everything above the slot — the six sequencer modes, the safety invariants
in `internal/ft8/AGENTS.md`, the hub and event wire, the occupancy ranking, the SPA anchors — is
protocol-independent, and the QEX condition on robotic operation (ADR 0021) names FT4 and FT8
together, so the operator-initiated session model carries over unchanged.

## Decision

FT4 becomes a second **profile** of the existing `internal/ft8` subsystem: one value that carries
the slot length, sample counts, tone geometry, Gaussian pulse, Costas layout, nominal start, late
window, occupancy signal width, encode and decode entry points, and the mode name stamped on QSOs,
spots and the decode log. The operator selects the profile per capture session from the FT8 view
through a new `POST /v1/ft8/mode` endpoint; the switch is refused while a session is active, TX is
armed, or a transmission is in flight, and it rebuilds the scheduler and decoder pair the way the
existing capture acquire/release already does. The sequencers, ladders, hub, invariants and the
SPA's operating anchors are shared unchanged; the SPA reads the active profile from the status event
and derives its countdown, parity and band frequencies from it. The FT4 decoder is consumed only
from a tagged go-ft8 release, bumped in its own commit.

## Alternatives considered

### A sibling `internal/ft4` package

Copy the subsystem and swap the constants. Rejected: it duplicates six sequencer modes, the seven
transmit-and-attribution invariants, the dial guard and the structural test guards, and every
future fix lands twice. The protocols share their message layer by design (QEX §3), so the
duplication buys nothing the profile does not.

### A static `ft8.mode` config key applied at start

Simplest daemon change: read the mode at boot, no runtime switch. Rejected as the primary mechanism
because the contest is band-agile and the operator also runs FT8 the same evening; a restart per
mode change fights the demand-driven capture the subsystem already has. It may return later as the
boot default for the runtime switch once a second operator asks for it.

### Work the contest in WSJT-X and log into Station Manager

No FT4 code before Saturday. Rejected as the plan because Station Manager has no WSJT-X UDP listener
(W-0013 lists UDP compatibility as trigger-bound) and it would field-test nothing on the FT path.
It remains the **fallback** if the go-ft8 FT4 decoder does not ship in time: the operator works the
contest in WSJT-X and enters QSOs through the Operate view's manual entry, which already accepts
FT4 as a mode, so SM Cloud and the forwarders still get exercised.

### Transmit-only FT4 against a WSJT-X decoder

Use go-ft8's shipped encoder now and let WSJT-X decode. Rejected: the sequencers act on the daemon's
own decodes; a split pipeline cannot satisfy invariant 4 (every acted-on decode attributable to one
known frequency) and invents an integration that has no other consumer.

## Consequences

- The FT8 subsystem gains a `Profile` value threaded through the scheduler, modulator, TX
  controller, sequencer timing, occupancy, slot references, QSO logging and the decode log; the
  package name and the `ft8.*` configuration namespace stay, because the subsystem is the FT-family
  subsystem, and renaming it now is a sweep without a functional payoff.
- Slot references gain sub-second resolution. FT4 boundaries fall on multiples of 7.5 s from the
  Unix epoch (`:00.0`, `:07.5`, `:15.0`, …), so `nextSlotBoundary`, `SlotRefFromTime`, the parity
  rule (`floor(unixMillis / 7500) mod 2`) and the SPA's helpers move to integer milliseconds, and
  `SlotRef.StartUTC` is formatted with fractional seconds. FT8 references are unchanged by this
  (their fractional part is zero), and the exact-string `wasTxSlot` match must use one formatter on
  both sides.
- FT4 transmit timing distinguishes the **sync reference** from the **waveform origin** (operator
  ruling 2026-09-10). The DT reference is the start of the first Costas array at slot + 0.500 s.
  The waveform carries a one-symbol (576-sample) leading ramp before that array, so PCM sample zero
  belongs at slot + 0.452 s, not + 0.500 s. WSJT-X's `ft4sim` encodes exactly that relationship
  (operator's citation: `lib/ft4/ft4sim.f90:85`), and go-ft8's oracle mirrors it by placing the
  waveform at `SampleRate/2 − SamplesPerSymbol` (`go-ft8/ft4/oracle_test.go:98`). The profile
  therefore carries both values, and slice 1 asserts offline that a waveform placed at + 0.452 s
  decodes with DT near zero. (FT8 keeps its existing 0.5 s convention; whether its own ramp symbol
  is placed the same way is checked, not assumed, in the same slice.)
- The **late window** is an admission policy, not a decodability guarantee (operator ruling
  2026-09-10). The proposal keeps + 2.000 s into the 7.5 s slot as the latest a rung may be
  admitted, with explicit refusals at the edges. It must not be described as preserving three of
  the four Costas arrays: the second array begins at 0.500 + 33 × 0.048 = 2.084 s, and the
  controller adds at least the 200 ms pre-key lead between the sequencer's decision and audio
  (`txcontroller.go:143–149`) plus CAT latency, so an admission at + 2.000 s can key after the
  second array has begun. The post-admission `maxDecodableSkip` check, re-derived from the FT4
  Costas positions instead of the FT8 middle-Costas rule, stays authoritative: it refuses the
  transmission when the remaining waveform is not worth calling sent. The audio budget follows from
  the slot: 7.5 s minus the waveform origin against a 5.04 s waveform plus the 0.25 s tail leaves
  roughly 1.76 s of slack, comparable to FT8's 1.3 s.
- Decode latency becomes a gate rather than a given. FT8 decodes one 15 s slot in about 124 ms on
  the station host (`BenchmarkDecodeSlot`, 2026-09-10); the FT4 decoder's cost is unknown until it
  exists. The answer-a-CQ path needs the partner's decode inside our reply slot early enough to start
  by the late window, so the acceptance criterion is a measured p95 decode time of one FT4 slot at
  or under 1.0 s on the station host, as an initial gate. Keyed acceptance additionally records the
  slot-close-to-first-audio latency, which is what the admission policy above actually depends on.
  If the budget is missed, answering degrades to the following period while Call-CQ runs stay
  viable, and the operator decides whether that is acceptable for the contest.
- Logged FT4 QSOs carry `MODE=FT4` (an ADIF main mode the SPA already resolves), SNR reports in
  `RST_SENT`/`RST_RCVD`, and the partner's grid; PSK Reporter spots inherit the mode. The QSO
  service's FT8-only empty-report rule becomes a shared "SNR-report mode" predicate covering FT8 and
  FT4, mirroring the SPA's `usesSignalReport`.
- A second dial table, `ft8.ft4_frequencies`, with the same override semantics as `ft8.frequencies`.
  The three contest bands are cited (SARL rule 5.4b); every other band's default needs a citation
  from the WSJT-X frequency table before it ships, or it ships absent.
- The event wire gains `mode` on the status event so the SPA never infers the profile from timing.
  `docs/v2-design/api-endpoints.md`, `docs/v2-design/config.md` and `docs/ft8.md` change with the
  code.
- What does not change: operator-initiated sessions, the single guaranteed-stop TX controller, the
  rig data-mode literal (`ft8.tx.mode`, the same DATA-U for both), capture gating on the rig, the
  dial guard, and the type-4 and Field Day ladders (both encode identically for FT4, though only the
  standard ladder is in scope for the contest).

## Triggers to revisit

- If go-ft8's FT4 decoder cannot meet the 1.0 s p95 budget on the station host, reconsider running
  the decode off the slot boundary earlier (a partial-slot decode) or accept answer-a-CQ slipping a
  period, and record which.
- If a third FT-family mode (FST4, or an FT4 contest format) is wanted, the profile grows a message
  layer; if that layer diverges materially, the sibling-package alternative deserves a second look.
- If the operator wants FT4 as the boot default, add the static key as the runtime switch's default.
- If a WSJT-X UDP listener lands (W-0013), the fallback becomes a first-class integration and the
  transmit-only alternative may be worth revisiting for stations that keep WSJT-X.

## References

- ADR 0021 (FT8 as an SM subsystem; licensing and the QEX no-robotic-operation condition), ADR 0024
  (external library and live pipeline), ADR 0029/0030/0032 (transmit roadmap, PTT controller,
  synchronised truncate), ADR 0033 (caller-side sequencing), ADR 0067 (one-rule run model).
- `docs/ft8.md` (subsystem reference), `internal/ft8/AGENTS.md` (invariants and the
  add-a-mode checklist), `docs/work/W-0019-ft4-for-the-africa-ft4-dx-contest.md` (plan, gates and
  evidence).
- Franke, Somerville, Taylor, "The FT4 and FT8 Communication Protocols", QEX July/August 2020,
  <https://wsjt.sourceforge.io/FT4_FT8_QEX.pdf>.
- 2026 SARL Contest Manual v1.1 (2025-12-15), "The Africa FT4 DX Contest", pp. 45–46,
  <https://mysarl.org.za/wp-content/uploads/2025/12/2026-SARL-Contest-Manual-Version-1.1-2025-12-15.pdf>.
- go-ft8 `master` at `31d70fb` (`ft4/encode.go`, `ft4/ft4_params.go`, `README.md`).
