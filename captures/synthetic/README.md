# Synthetic FT8 test fixtures

Reproducible two-signal FT8 WAVs built by `cmd/ft8-stage-probe` for
decoder refinement work. Every file in this directory is fully
deterministic from its parameters — the audio = synth(msg₁, f₁, gain₁)
+ synth(msg₂, f₂, gain₂) + Gaussian noise(SNR, seed). Regenerating from
the parameters yields a byte-identical WAV.

Audio format: 12 kHz mono, 16-bit signed PCM, 15-second slot.

## File naming

`<config>_dF<delta>_SNR<n>_seed<n>.wav`

- `config` — `eq` (equal gain 1.0/1.0), `a2x` (A=2.0, B=1.0), `a4x`
  (A=4.0, B=1.0). Larger gain numbers = louder signal A relative to B.
- `dF` — frequency offset of signal B from signal A, in Hz (positive
  = B above A).
- `SNR` — AWGN SNR in dB (WSJT-X 2500 Hz reference convention), measured
  against signal A at gain=1.0. With gain1>1.0 the *effective* SNR of A
  is shifted by +20·log10(gain1) dB.
- `seed` — PCG seed for the noise generator. Different seeds → different
  noise realisations of the same signal configuration.

## Shared parameters across this set

- `msg1 = "CQ K1JT FN20"` at `freq1 = 1500 Hz`
- `msg2 = "CQ G0ABC IO91"` at `freq2 = 1500 + dF Hz`
- `dt = 0` (both signals on-time at the slot start)
- `seed = 1` (single noise realisation per config)

## Regeneration

Build the probe once, then run the recipe:

```
go build -o /tmp/sm-stage-probe ./cmd/ft8-stage-probe/
bash captures/synthetic/regen.sh
```

`regen.sh` is the canonical script. Edit it (or use it as a template)
to add new fixtures.

## Why these fixtures exist

Session 84 (2026-05-23) used these to characterise the
adjacent-signal interference failure mode that dominates SM's
real-capture decode loss. Headline finding: matched-filter signal
subtraction (`internal/ft8/dsp/SubtractSignal` + `Decode`'s
`SubtractionPasses` loop) cannot separate two FT8 signals when their
tone bins overlap (Δf < 50 Hz) — the projection's amplitude/phase
estimator absorbs energy from both signals, so subtracting "A" also
rips out part of B's signal. Outside the channel (Δf ≥ 50 Hz) both
signals decode on their own; subtraction is unnecessary.

These WAVs let future refinement work (e.g. coherent demodulation, AP
decoding, per-symbol cancellation) be re-evaluated against the same
ground truth. The decoded message content is known by construction:
expected decodes are exactly `"CQ K1JT FN20"` and `"CQ G0ABC IO91"`,
nothing else.

Full diagnostic context lives in
`docs/v2-design/milestones.md` § Milestone 4.1 refinement,
`docs/session-handoff.md` (Session 84), and memory
`project_ft8_refinement`.
