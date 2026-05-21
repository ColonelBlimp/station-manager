package dsp

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// TestSynthesize_OutputShape pins the audio shape contract: NMAX
// float32 samples, regardless of input.
func TestSynthesize_OutputShape(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	out := Synthesize(msg, 1500.0, 0)
	if len(out) != NMAX {
		t.Errorf("output length = %d, want %d", len(out), NMAX)
	}
}

// TestSynthesize_RejectsBadMessageLength pins that wrong-length
// input → nil.
func TestSynthesize_RejectsBadMessageLength(t *testing.T) {
	for _, n := range []int{0, codec.MessageBits - 1, codec.MessageBits + 1, 174} {
		if out := Synthesize(make([]byte, n), 1500.0, 0); out != nil {
			t.Errorf("Synthesize(len=%d) = non-nil, want nil", n)
		}
	}
}

// TestSynthesize_TXWindow pins the time-domain location of the
// active TX portion. The synth should produce silence before the
// nominal slot start + dt, then active samples for the 12.64s
// transmission window, then silence again.
func TestSynthesize_TXWindow(t *testing.T) {
	msg := make([]byte, codec.MessageBits)
	out := Synthesize(msg, 1500.0, 0)

	// Sample 0 (= time 0s, before TX which starts at 0.5s).
	if out[0] != 0 {
		t.Errorf("sample 0: expected silence, got %v", out[0])
	}
	// Sample at the very start of TX (0.5s × 12000 = sample 6000).
	// This should be inside the TX window — non-zero amplitude
	// (the synth's GFSK ramp-in is gradual but the first sample is
	// not exactly zero either since the Gaussian shaping smooths
	// the frequency, not the envelope).
	const txStart = int(SynthSlotStartSeconds * Fs)
	// Look around txStart+100 to be safely inside the window after
	// any initial transient. Check that we have non-trivial
	// amplitude somewhere in the active window.
	const txMid = txStart + NN*NSPS/2 // middle of TX
	if math.Abs(float64(out[txMid])) < 0.01 {
		t.Errorf("sample at TX-mid (%d): amplitude %g, expected non-trivial activity", txMid, out[txMid])
	}
}

// TestSynthesize_RoundTrip is the headline correctness check:
// synthesize a known message, run it through the full Decode
// pipeline, verify the recovered message text matches. If the
// synth's GFSK shaping is correct, our demodulator should recover
// the original message.
//
// This is the same "demod-compatibility" sanity check used by
// (separately) decode_test.go's TestDecode_SyntheticRoundTrip,
// which uses tone-bursts (no GFSK) — we re-do it here with the
// proper GFSK synth to confirm GFSK output also round-trips.
//
// Note: the actual Decode call lives in the parent `ft8` package,
// not in `dsp`. So this test verifies the synth is "demodulator-
// compatible at the symbol level" by running through the lower-
// level Demodulate primitive. A full end-to-end test through ft8.Decode
// lives in the ft8 package's own test files (will be added with
// Step 3 — the subtraction wiring).
func TestSynthesize_RoundTrip(t *testing.T) {
	// All-zero message — exercises the synth + demod chain on a
	// well-defined input.
	msg := make([]byte, codec.MessageBits)

	audio := Synthesize(msg, 1500.0, 0)
	if audio == nil {
		t.Fatal("Synthesize returned nil")
	}

	// Build the baseband signal at 1500 Hz via the standard pipeline:
	// ForwardSpectrum → DownsampleFromSpectrum at f0=1500.
	spec := ForwardSpectrum(audio)
	baseband := DownsampleFromSpectrum(spec, 1500.0)
	if baseband == nil {
		t.Fatal("DownsampleFromSpectrum returned nil")
	}

	// Demodulate at dt=0 (synth was generated at the nominal start).
	llrs := Demodulate(baseband, 0, DefaultLLRScale)
	if llrs == nil {
		t.Fatal("Demodulate returned nil")
	}

	// LDPC decode.
	msgBits, ok := codec.LDPCDecode(llrs, codec.LDPCMaxIterationsDefault)
	if !ok {
		t.Fatal("LDPCDecode failed — synth not demod-compatible (GFSK shaping likely wrong)")
	}

	// Verify the recovered bits match the input.
	for i := 0; i < codec.MessageBits; i++ {
		if msgBits[i] != msg[i] {
			t.Errorf("recovered msg bit %d = %d, want %d", i, msgBits[i], msg[i])
			break
		}
	}
}

// BenchmarkSynthesize measures the cost of one full waveform
// synthesis. For Step 3 (iterative subtraction) we'd call this
// once per decoded message per subtraction pass — typically a few
// times per slot, so the per-call cost matters but isn't the
// bottleneck.
func BenchmarkSynthesize(b *testing.B) {
	msg := make([]byte, codec.MessageBits)
	for i := range msg {
		msg[i] = byte(i % 2) // mix of 0s and 1s
	}
	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		_ = Synthesize(msg, 1500.0, 0)
	}
}
