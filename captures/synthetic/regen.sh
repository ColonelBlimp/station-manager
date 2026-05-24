#!/bin/bash
# regen.sh — rebuild the canonical synthetic FT8 fixtures.
# All audio is deterministic from its parameters; re-running yields
# byte-identical WAVs (modulo any future changes to the synth code).
#
# Usage:
#   go build -o /tmp/sm-stage-probe ./cmd/ft8-stage-probe/
#   bash captures/synthetic/regen.sh
#
# Override the probe path or the output dir via env vars:
#   PROBE=./bin/ft8-stage-probe OUTDIR=/tmp/wavs bash captures/synthetic/regen.sh
set -e

PROBE="${PROBE:-/tmp/sm-stage-probe}"
OUTDIR="${OUTDIR:-captures/synthetic}"
mkdir -p "$OUTDIR"

if [ ! -x "$PROBE" ]; then
  echo "build the probe first:  go build -o $PROBE ./cmd/ft8-stage-probe/" >&2
  exit 1
fi

save() {
  freq2="$1"; gain1="$2"; gain2="$3"; snr="$4"; seed="$5"; out="$6"
  "$PROBE" \
    -msg "CQ K1JT FN20" -freq 1500 -gain1 "$gain1" \
    -msg2 "CQ G0ABC IO91" -freq2 "$freq2" -gain2 "$gain2" \
    -snr="$snr" -seed="$seed" \
    -out="$OUTDIR/$out" \
    > /dev/null
  echo "  $OUTDIR/$out"
}

echo "Equal gain (A=1.0, B=1.0) — Gap 1"
save 1510 1.0 1.0 -6 1 eq_dF10_SNR-6_seed1.wav
save 1520 1.0 1.0 -6 1 eq_dF20_SNR-6_seed1.wav
save 1530 1.0 1.0 -6 1 eq_dF30_SNR-6_seed1.wav
save 1540 1.0 1.0 -6 1 eq_dF40_SNR-6_seed1.wav
save 1550 1.0 1.0 -6 1 eq_dF50_SNR-6_seed1.wav

echo "A 2x stronger than B — original Session 84 finding"
save 1510 2.0 1.0 -6 1 a2x_dF10_SNR-6_seed1.wav
save 1520 2.0 1.0 -6 1 a2x_dF20_SNR-6_seed1.wav
save 1540 2.0 1.0 -6 1 a2x_dF40_SNR-6_seed1.wav
save 1560 2.0 1.0 -6 1 a2x_dF60_SNR-6_seed1.wav

echo "A 4x stronger than B — Gap 2"
save 1510 4.0 1.0 -6 1 a4x_dF10_SNR-6_seed1.wav
save 1520 4.0 1.0 -6 1 a4x_dF20_SNR-6_seed1.wav
save 1540 4.0 1.0 -6 1 a4x_dF40_SNR-6_seed1.wav

echo "A 2x B, louder SNR (-2 dB) — Gap 3"
save 1510 2.0 1.0 -2 1 a2x_dF10_SNR-2_seed1.wav
save 1520 2.0 1.0 -2 1 a2x_dF20_SNR-2_seed1.wav
save 1540 2.0 1.0 -2 1 a2x_dF40_SNR-2_seed1.wav

echo
echo "wrote $(ls -1 "$OUTDIR"/*.wav 2>/dev/null | wc -l) WAVs to $OUTDIR/"
