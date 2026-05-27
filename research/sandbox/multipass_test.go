package sandbox

import (
	"math"
	"testing"
)

// TestSubtractFromAudio_SelfNullsOut covers M2-style self-subtraction
// at audio rate: synthesize a known signal at carrierHz/dtSec,
// subtract it from itself via per-symbol fit, residual within the
// signal window should drop to near-zero.
//
// Uses a clean synthetic audio buffer (just the signal, no other
// content) so the residual energy ratio quantifies subtraction
// effectiveness directly.
func TestSubtractFromAudio_SelfNullsOut(t *testing.T) {
	const (
		carrierHz = 1500.0
		dtSec     = 0.0
		audioRate = 12000.0
	)
	var tones [ft8SymbolCount]int
	for i := range tones {
		tones[i] = (i * 5) % 8
	}
	// Build audio by synthesising cos as the "received signal".
	bufLen := int(15 * audioRate)
	cosSynth, sinSynth, sigStart, sigLen := SynthesizeAudio(tones, carrierHz, dtSec, audioRate, bufLen)
	audio := make([]float32, bufLen)
	copy(audio, cosSynth) // unit-amplitude pure-cos as the "channel"

	residual := FitAndSubtractAudio(audio, cosSynth, sinSynth, sigStart, sigLen, int(audioRate*ft8SymbolPeriod))

	// Energy ratio: residual/signal inside the signal window.
	var signalE, residualE float64
	for n := sigStart; n < sigStart+sigLen; n++ {
		signalE += float64(audio[n]) * float64(audio[n])
		residualE += float64(residual[n]) * float64(residual[n])
	}
	ratio := residualE / signalE
	// 60+ dB suppression is the bar for a clean subtraction in float32.
	if ratio > 1e-6 {
		t.Errorf("residual/signal energy ratio = %g (%.1f dB), want < 1e-6 (60+ dB suppression)",
			ratio, 10*math.Log10(ratio))
	}
}

// TestSubtractFromAudio_OutsideWindowUnchanged pins that
// FitAndSubtractAudio doesn't touch audio samples outside the
// signal's time range. A signal placed at dtSec=0 occupies
// audio[6000 : 6000+151680). Samples before 6000 and after must
// equal the input verbatim.
func TestSubtractFromAudio_OutsideWindowUnchanged(t *testing.T) {
	const audioRate = 12000.0
	var tones [ft8SymbolCount]int
	bufLen := int(15 * audioRate)
	cosSynth, sinSynth, sigStart, sigLen := SynthesizeAudio(tones, 1500.0, 0.0, audioRate, bufLen)
	audio := make([]float32, bufLen)
	for i := range audio {
		audio[i] = float32(0.1 * math.Sin(float64(i)*0.001)) // arbitrary pattern
	}
	residual := FitAndSubtractAudio(audio, cosSynth, sinSynth, sigStart, sigLen, int(audioRate*ft8SymbolPeriod))
	// Samples before signal: must be unchanged.
	for n := 0; n < sigStart; n++ {
		if residual[n] != audio[n] {
			t.Errorf("residual[%d] = %g, want %g (pre-signal sample modified)",
				n, residual[n], audio[n])
			break
		}
	}
	// Samples after signal: must be unchanged.
	for n := sigStart + sigLen; n < bufLen; n++ {
		if residual[n] != audio[n] {
			t.Errorf("residual[%d] = %g, want %g (post-signal sample modified)",
				n, residual[n], audio[n])
			break
		}
	}
}

// TestSynthesizeAudio_OutputLengthMatchesBuffer pins the contract
// that both returned buffers are exactly audioLen samples.
func TestSynthesizeAudio_OutputLengthMatchesBuffer(t *testing.T) {
	var tones [ft8SymbolCount]int
	bufLen := 12000 * 15
	cosOut, sinOut, _, _ := SynthesizeAudio(tones, 1500.0, 0.0, 12000.0, bufLen)
	if len(cosOut) != bufLen || len(sinOut) != bufLen {
		t.Fatalf("output lengths = %d/%d, want %d/%d", len(cosOut), len(sinOut), bufLen, bufLen)
	}
}
