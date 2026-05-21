# FT8 test corpus

15-second FT8 audio captures used by `internal/ft8/decode_test.go` and
`internal/ft8/dsp/sync_test.go` for the L4 (real-signal) layer of the
milestone-4 test architecture: synthetic and bit-level tests run in CI
with zero external deps; these real-signal tests verify the full
audio→messages pipeline against captures from a real FT8 station.

## Provenance

Operator-recorded captures from 7Q5MLV's station (Marc Veary's
WSJT-X-driven FT8 setup). Originally lived in the operator's go-ft8
research repo at `testdata/`; copied verbatim into SM when the FT8
subsystem in this repo became the primary line of work.

These are **not** WSJT-X-distributed sample files. WSJT-X ships GPL
v3 sample recordings (in its `samples/` directory) which SM does not
import per the licensing rules in CLAUDE.md and ADR 0021. The
captures here are the operator's own recordings of public radio
traffic — FT8 transmissions on the amateur bands are by definition
broadcast to anyone listening, and callsigns + grids + reports in the
embedded QSOs are public information by protocol design.

## File format

Each capture is one FT8 15-second slot:

- **Sample rate:** 12 000 Hz (WSJT-X canonical)
- **Channels:** 1 (mono)
- **Format:** PCM 16-bit signed
- **Length:** 15 s = 180 000 samples = 360 044 bytes incl. WAV header

## The three fixtures

| File | WSJT-X 2.7.0 decodes |
|------|----------------------|
| `ft8_cap1.wav` | 11 signals |
| `ft8_cap2.wav` | 14 signals |
| `ft8_cap3.wav` | 23 signals |

These three captures were the regression suite the operator's go-ft8
research pipeline tuned against — its main-loop achieves parity with
WSJT-X 2.7.0's main-loop decode count on all three. SM's clean-room
spec implementation (per QEX 2020 paper + ADR 0021) is expected to
decode fewer signals initially — the sync detector and soft
demodulator are deliberately simpler than the WSJT-X versions, and
gain back sensitivity over future sessions by adding the QEX-paper-
described block detection (§6) and OSD (Ordered Statistics Decoding
per Taylor 2020 §6) on top of the current BP-only LDPC chain.

## How tests find these files

Test resolution order:

1. `$FT8_TEST_CORPUS/ft8_cap1.wav` if the env var is set — useful for
   pointing tests at a larger corpus that's not vendored.
2. Vendored fixture at the test's `testdata/` path (this directory).

If neither resolves, the test skips gracefully via `t.Skip`. CI never
hard-fails on missing real-signal fixtures; the synthetic-signal
tests carry the structural correctness check.
