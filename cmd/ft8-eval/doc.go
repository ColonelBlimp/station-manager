// ft8-eval is the FT8 decoder evaluation workbench — the tool used to
// measure SM's decode quality and performance while refining the
// internal/ft8 pipeline (sensitivity tuning, OSD/subtraction knobs,
// speed/allocation work).
//
// Given one or more WAV files (or directories of them) it runs
// ft8.Decode on each with a configurable DecodeOptions, and prints a
// per-file table of decode count + wall time (+ optional heap bytes),
// followed by a totals row. The decode knobs that matter for tuning are
// exposed as flags so a refinement loop is a single command:
//
//	# Baseline counts + timing over the whole corpus
//	ft8-eval internal/ft8/testdata captures
//
//	# Same, but compare against the WSJT-X jt9 oracle and show parity
//	ft8-eval -oracle internal/ft8/testdata captures
//
//	# Sweep a knob: sensitivity vs cost of one subtraction pass
//	ft8-eval -subtraction-passes=1 -runs=3 captures
//
//	# Inspect what changed (which messages SM recovered)
//	ft8-eval -msgs -hashtable captures/live_slot2.wav
//
// Per ADR 0021's licensing constraint, the optional -oracle mode uses
// the WSJT-X `jt9 -8` binary purely as a BLACK BOX: it shells out, runs
// the decoder in a temp working directory, and counts decoded-message
// lines. jt9's GPL source is never consulted as an implementation
// reference. -oracle is a no-op (with a warning) when jt9 isn't on PATH.
//
// This is a developer tool, not part of smd. It's the file-input peer of
// cmd/ft8-capture-probe (live audio) and complements cmd/ft8-corpus-prep
// (which runs the oracle alone and prints its messages).
package main
