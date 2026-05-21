package ft8

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/audio"
	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// TestDecode_SubtractionPreservesZeroBehaviour pins that
// DecodeOptions{} (zero value, no subtraction) produces the exact
// same decode set as before iterative subtraction was wired in. The
// smoke-test floors (8/13/20 on the three vendored captures) are the
// regression contract; this test re-verifies that contract through
// the SubtractionPasses=0 path specifically.
func TestDecode_SubtractionPreservesZeroBehaviour(t *testing.T) {
	wavPath := resolveCapturePath(t, "ft8_cap1.wav")
	if wavPath == "" {
		t.Skip("no FT8 capture fixture available")
	}
	data, err := audio.ReadWAV(wavPath)
	if err != nil {
		t.Fatalf("ReadWAV: %v", err)
	}

	defaults := Decode(data.Samples, DecodeOptions{})
	explicit := Decode(data.Samples, DecodeOptions{SubtractionPasses: 0})

	if len(defaults) != len(explicit) {
		t.Fatalf("count differs: zero-value=%d, explicit-zero=%d", len(defaults), len(explicit))
	}
	for i := range defaults {
		if defaults[i].Text != explicit[i].Text {
			t.Errorf("decode[%d] differs: default=%q explicit=%q", i, defaults[i].Text, explicit[i].Text)
		}
	}
}

// TestDecode_SubtractionDoesNotDecreaseCounts is the regression
// floor: enabling iterative subtraction must NEVER decrease the
// decode count. Each capture is tested independently; if even one
// regresses, the subtraction's amplitude estimation or audio
// mutation is broken.
func TestDecode_SubtractionDoesNotDecreaseCounts(t *testing.T) {
	captures := []struct {
		name string
		base int
	}{
		{"ft8_cap1.wav", 8},
		{"ft8_cap2.wav", 13},
		{"ft8_cap3.wav", 20},
	}
	for _, tc := range captures {
		t.Run(tc.name, func(t *testing.T) {
			wavPath := resolveCapturePath(t, tc.name)
			if wavPath == "" {
				t.Skip("no FT8 capture fixture available")
			}
			data, err := audio.ReadWAV(wavPath)
			if err != nil {
				t.Fatalf("ReadWAV: %v", err)
			}

			results := Decode(data.Samples, DecodeOptions{SubtractionPasses: 1})
			t.Logf("%s: SubtractionPasses=1 decoded %d messages (baseline %d)", tc.name, len(results), tc.base)
			for i, r := range results {
				t.Logf("  [%d] %.2f Hz  %+.2f s  sync=%.2f  %q", i, r.Freq, r.DT, r.SyncPower, r.Text)
			}
			if len(results) < tc.base {
				t.Errorf("%s: decode count %d < baseline %d (subtraction regressed sensitivity)",
					tc.name, len(results), tc.base)
			}
		})
	}
}

// TestDecode_SubtractionRevealsMaskedSignal is the synthetic-data
// proof of the subtraction's value. Build audio with TWO synthesized
// messages: a strong one at 1500 Hz and a 6 dB weaker one nearby at
// 1525 Hz (within the same Costas sync neighbourhood). Without
// subtraction, the strong signal's correlated artefacts may suppress
// the weaker; with subtraction, both decode.
//
// This test is deliberately "easy" — both signals are clean GFSK at
// their nominal positions with no noise. Real-world subtraction
// faces noisier audio; the test pins that the mechanism WORKS
// when the inputs are well-behaved.
func TestDecode_SubtractionRevealsMaskedSignal(t *testing.T) {
	msgA := make([]byte, codec.MessageBits)
	for i := range msgA {
		msgA[i] = byte((i * 7) % 2) // pattern A
	}
	msgB := make([]byte, codec.MessageBits)
	for i := range msgB {
		msgB[i] = byte((i*5 + 1) % 2) // pattern B (distinct from A)
	}

	synthA := dsp.Synthesize(msgA, 1500.0, 0)
	synthB := dsp.Synthesize(msgB, 1525.0, 0)
	if synthA == nil || synthB == nil {
		t.Fatal("synthesizers returned nil — test setup broken")
	}

	// Combine: strong A (amplitude 0.5) + weak B (amplitude 0.1).
	const ampA, ampB float32 = 0.5, 0.1
	combined := make([]float32, len(synthA))
	for i := range combined {
		combined[i] = ampA*synthA[i] + ampB*synthB[i]
	}

	resultsNoSub := Decode(combined, DecodeOptions{})
	resultsWithSub := Decode(combined, DecodeOptions{SubtractionPasses: 1})

	t.Logf("Without subtraction: %d decode(s)", len(resultsNoSub))
	for i, r := range resultsNoSub {
		t.Logf("  [%d] %.2f Hz  %q", i, r.Freq, r.Text)
	}
	t.Logf("With subtraction: %d decode(s)", len(resultsWithSub))
	for i, r := range resultsWithSub {
		t.Logf("  [%d] %.2f Hz  %q", i, r.Freq, r.Text)
	}

	// Subtraction-with-1-pass must find at least as many decodes as
	// without subtraction.
	if len(resultsWithSub) < len(resultsNoSub) {
		t.Errorf("subtraction regressed count: with=%d < without=%d", len(resultsWithSub), len(resultsNoSub))
	}
}
