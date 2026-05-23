// ft8-stage-probe is a clean-room stage-level diagnostic for the FT8
// decode pipeline. It synthesises a KNOWN message (via dsp.Synthesize),
// adds calibrated AWGN at a target SNR, and runs it through each decode
// stage — sync, downsample, demod, LDPC/OSD — reporting where the signal
// is lost. Because the transmitted message (and therefore the exact
// 174-bit codeword) is known, the demod stage can be scored directly:
// how many of the 174 LLR hard-decisions disagree with the true
// codeword. That bit-error count is the precise measure of demod
// quality — the clean-room-safe equivalent of "where does our pipeline
// diverge", with NO reference to WSJT-X internals.
//
// All ground truth comes from SM's own encoder + the QEX-paper-derived
// pipeline. jt9 is not involved. Stage localisation:
//
//   - sync finds no candidate near f0  → sync sensitivity.
//   - demod bit-errors high at the TRUE f0/dt → demod quality (the
//     LLR formula / per-symbol processing is the bottleneck).
//   - demod errors low at true f0 but high at the sync candidate's
//     (quantised) f0 → frequency/timing precision is the bottleneck.
//   - demod errors low but BP/OSD still fail → decoder weakness.
//
// SNR uses the WSJT-X convention (signal power in a 2500 Hz reference
// bandwidth), so thresholds are comparable to FT8's published ~-21 dB
// sensitivity.
//
// Usage:
//
//	# Single condition, verbose per-stage report
//	ft8-stage-probe -msg "CQ K1JT FN20" -snr=-15
//
//	# Sweep SNR to find SM's per-stage thresholds (averaged over trials)
//	ft8-stage-probe -sweep=-26:-6:2 -trials=20
//
//	# Add a strong same-msg masker 50 Hz away (dense-slot demod stress)
//	ft8-stage-probe -snr=-12 -interferer=50
//
// **Two-signal subtraction mode** — synthesise two distinct messages
// at nearby frequencies and ask whether iterative subtraction can
// recover both. Score is per-message presence (found / missed) plus
// the count of any extra unexpected decodes ("false positives" from
// the subtraction residue or CRC14 lottery):
//
//	# A and B at +10 Hz spacing, single SNR
//	ft8-stage-probe \
//	    -msg "CQ K1JT FN20" -freq 1500 \
//	    -msg2 "CQ G0ABC IO91" -freq2 1510 \
//	    -snr=-12 -subtract
//
//	# Sweep SNR with subtraction on/off; A0%/B0% = no-subtraction,
//	# A1%/B1% = SubtractionPasses=1.
//	ft8-stage-probe \
//	    -msg "CQ K1JT FN20" -freq 1500 \
//	    -msg2 "CQ G0ABC IO91" -freq2 1510 \
//	    -sweep=-22:-4:2 -trials=10 -subtract
//
// Developer tool, not part of smd; peer of ft8-eval (corpus-level) and
// ft8-capture-probe (live audio).
package main
