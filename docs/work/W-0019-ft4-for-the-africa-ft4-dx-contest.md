# W-0019 — FT4 for the Africa FT4 DX Contest

**Status:** Proposed — design in ADR 0080; awaiting the operator's go/no-go and the go-ft8 FT4 decoder
**Selected:** not yet (planned 2026-09-10)
**Outcome:** The operator works the Africa FT4 DX Contest (Saturday 2026-09-12, 15:00–18:00 UTC, 80/40/20 m)
from Station Manager: FT4 decodes appear in Band Activity on the contest dial frequencies, an operator-initiated
answer or Call-CQ run completes the standard report-and-grid exchange on 7.5 s slots, each completed QSO is
logged once with `MODE=FT4`, the grid and SNR reports, and the QSOs reach the forwarders and SM Cloud through the
existing paths. The daemon returns to FT8 without a restart.

`W-0019` is an immutable identity. Its status may change, while priority and ranked position live only in
[`docs/backlog.md`](../backlog.md).

## Why this item exists

The contest is the first realistic opportunity to field-test the whole station — daemon, SPA, forwarders, SM
Cloud — on a second FT-family mode, and its rules make it a good fit: the exchange is the standard report-and-grid
ladder the subsystem already runs, contest message formats and Fox & Hound are forbidden, and the log is produced
from ADIF. The library dependency is in flight: go-ft8 `master` carries the FT4 encoder (`31d70fb`), and the
decoder is being implemented with a hoped-for completion on 2026-09-10. Everything in Station Manager that is not
the decoder can be built ahead of it. ADR 0080 records the design and the alternatives.

## Scope

- One `Profile` value in `internal/ft8` (FT8 and FT4) carrying slot length, sample counts, tone geometry, Gaussian
  pulse, Costas layout, nominal start, late window, occupancy signal width, encode/decode entry points and the mode
  name. Threaded through the scheduler, modulator, TX controller, sequencer timing, occupancy, slot references,
  QSO log and decode log.
- Sub-second slot references: boundaries, parity and `SlotRef.StartUTC` in integer milliseconds on both sides of
  the wire.
- `POST /v1/ft8/mode` and `mode` on the status event; the SPA's mode toggle, countdown, parity and band buttons
  follow the active profile.
- `ft8.ft4_frequencies` with cited defaults; `MODE=FT4` logging; the QSO service's SNR-report predicate.
- The go-ft8 bump to the tagged release carrying the FT4 decoder, as its own commit.
- Canonical references updated with the code: `docs/ft8.md`, `docs/v2-design/api-endpoints.md`,
  `docs/v2-design/config.md`, and the manual's FT8 chapter.

## Non-goals

- No contest message formats (RTTY Roundup, EU VHF), no serial numbers: the contest forbids them and the library
  does not encode them. The Field Day and type-4 ladders are not exercised on FT4 this weekend.
- No Cabrillo or SARL Excel writer: the operator exports ADIF from the logbook and runs SARL's converter.
- No change to operator initiation, the TX controller, the dial guard, capture gating or the rig data-mode
  literal.
- No rename of the package or the `ft8.*` configuration namespace.

## Acceptance criteria (operator-observable)

| # | Criterion | Nearest confusable outcome it must be distinguished from |
| --- | --- | --- |
| AC1 | With the rig on 14.080 MHz USB-D and FT4 selected, Band Activity shows FT4 decodes with sensible SNR and DT, on a slot clock that flips every 7.5 s and alternates parity. | FT8 decodes of a 15 s window on an FT4 band (nothing decodes); a countdown that still runs to 15. |
| AC2 | The mode switch is accepted only when idle: with an active session, armed TX or an in-flight transmission it is refused with a distinct code and nothing changes. | A switch that silently stops a session or leaves the scheduler on the old lattice. |
| AC3 | Answering a decoded FT4 CQ completes `<them> <us> <grid>` → `R-report` → `73` on consecutive opposite-parity 7.5 s slots, transmitting the synchronised remainder when the rung starts late. | A transmission that starts after the late window and spills into the partner's slot; an untruncated waveform shifted off the timebase. |
| AC4 | A Call-CQ run on FT4 runs the standard ladder with the operator's grid in the CQ and the confirm-hold from ADR 0067. | The FT8 ladder timing (CQ repeated every 15 s). |
| AC5 | Exactly one QSO row per completed exchange with `MODE=FT4`, the partner's grid, SNR reports, and the session-pinned frequency; one `ft8-logged` event; the PSK Reporter spot carries `FT4`. | `MODE=FT8` on an FT4 QSO; an RST default of `59` fabricated by the QSO service. |
| AC6 | Switching back to FT8 restores the 15 s lattice and FT8 decodes without a daemon restart. | Stale FT4 references in the SPA's parity store after the switch. |
| AC7 | The measured p95 decode time of one live FT4 slot on the station host is at or under 1.0 s (the operator may waive with a recorded reason). | A benchmark on a synthetic single-signal slot standing in for a busy contest band. |

## Slices

Each slice is RED-first with a reversion proof, one atomic commit, then the Codex review. Slices 1, 3 and 4 do
not need the FT4 decoder and can land now; slice 2 waits for the tagged go-ft8 release.

1. **Profile and timing.** Introduce `Profile`; move `slotSeconds`, `SlotDuration`, `SlotSamples`,
   `txSamplesPerSymbol`, `txGfskBT`, tone count, `txNominalDtSec`, `txAudioBudget`, `txLateWindowSec`,
   `signalWidthHz` and the Costas skip rule onto it; rewrite `nextSlotBoundary` and `SlotRefFromTime` in integer
   milliseconds; format `StartUTC` with fractional seconds. The profile distinguishes the **sync reference**
   (first Costas array at slot + 0.500 s) from the **waveform origin** (PCM sample zero at slot + 0.452 s, one
   576-sample ramp earlier) — ratified 2026-09-10, see ADR 0080. Tests: the FT4 lattice (`:00.0/:07.5/:15.0`),
   parity under both profiles, the `wasTxSlot` string match, the FT4 waveform length of 60 480 samples with a
   measured 20.833 Hz tone spacing, a waveform placed at + 0.452 s decoding with DT near zero (offline, once the
   decoder lands; until then the `jt9` oracle), the + 2.000 s admission edge refusals, the re-derived
   `maxDecodableSkip` refusing a start past the FT4 Costas rule, and the whole existing `internal/ft8` suite run
   table-driven over both profiles to prove the ladders are mode-independent.
   Offline oracle: `WSJTX_JT9=… ` decodes SM's FT4 waveform (local evidence only; CI has no `jt9`).
2. **Decoder adapter.** Bump go-ft8 to the tagged release with the FT4 decoder (own commit). Make `slotDecoder` an
   interface with FT8 and FT4 implementations; keep the stateful-decoder skip/reset semantics. Tests: the FT4
   encode → modulate → decode round trip, the truncated-start round trip, and a decode benchmark on the
   station host recorded in this dossier (AC7).
3. **Mode switch and wire.** `Service.SetMode` rebuilding the scheduler/decoder pair under the existing
   acquire/release; `POST /v1/ft8/mode` with the refusal states enumerated (idle, active session, armed, in
   flight, no capture); `mode` on the status event. Tests: each refusal state; a switch during a live capture
   yields FT4 references on the next slot and no slot from the mixed window is acted on (invariant 4).
4. **Logging, frequencies, SPA.** `MODE` from the profile; the QSO service's SNR-report predicate for FT8 and FT4;
   `ft8.ft4_frequencies` (cited defaults 3 576 000 / 7 047 500 / 14 080 000 Hz; other bands only with a citation);
   the SPA's mode toggle (disabled while active, like the band selector), countdown and parity from the profile,
   band buttons using the FT4 table, mode-aware labels. Tests: Vitest for the toggle states, the 7.5 s countdown
   and parity, and the frequency source; Go tests for the empty-report acceptance on FT4.
5. **Deploy and validate.** `task deploy:local:dev`, then the gates below; a record entry per gate.

## Gates (operator-controlled)

- **G1 — library.** The FT4 decoder is in a tagged go-ft8 release and pinned; no `replace` directive ships.
- **G2 — passive RX.** At least 30 minutes of FT4 decodes on 14.080 MHz with DT clustered near zero (the
  nominal-start check from ADR 0080) and AC7 measured. RX only; no rig command beyond the operator's own tuning.
- **G3 — keyed test.** One operator-agreed transmission into a dummy load, audio-only first, then RF; per-occasion
  agreement as always. Records the slot-close-to-first-audio latency (sequencer decision → PTT → first PCM sample),
  which the admission policy depends on, alongside the decode time from AC7.
- **G4 — first QSO.** Operator-initiated; the record entry names the partner, band and slot parity.

If G1 is not met by the evening of Friday 2026-09-11, the contest is worked in WSJT-X with manual entry in the
Operate view (the fallback in ADR 0080), and W-0019 continues afterwards without the date pressure.

## Contest runbook (Saturday 2026-09-12)

1. Rig on the contest band, USB-D, on the SARL-recommended dial; confirm the dial in the Rig panel.
2. FT8 view → mode FT4; watch a few slots (G2 evidence if not already recorded).
3. Search and pounce first (answer ladder, AC3); Call CQ once the timing is trusted (AC4).
4. After the contest: export ADIF from the logbook, run SARL's converter, submit by Thursday 2026-09-17 21:59 UTC.
5. Record the session in the dogfood record: QSO count, any stalls, decode times, forwarder and SM Cloud outcomes.

## Open rulings for the operator

- Ratified 2026-09-10: the DT reference is the first Costas array at slot + 0.500 s with PCM sample zero at
  + 0.452 s; + 2.000 s is an admission policy with edge refusals, and `maxDecodableSkip` remains authoritative.
- AC7's 1.0 s decode budget is accepted as the initial gate; the keyed latency recorded at G3 may tighten or relax
  the admission edge.
- Whether FT4 is a boot default later (`ft8.mode`), or stays runtime-only.
- Which non-contest bands get an FT4 default dial, and from which citation.

## Evidence

- 2026-09-10 — `BenchmarkDecodeSlot` (FT8, station host, Ryzen 9 9900X): 3 runs, ~124 ms per 15 s slot.
- 2026-09-10 — go-ft8 `master` at `31d70fb`: `ft4/encode.go` and `ft4/ft4_params.go` present; README states FT4
  receive-side decoding is not yet implemented.
- Contest rules: 2026 SARL Contest Manual v1.1, "The Africa FT4 DX Contest", pp. 45–46.
