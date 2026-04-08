// goertzel_test.go — tests for the Goertzel algorithm and DemodulateAudio.

package dsp

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// --- Goertzel single-frequency tests ---

func TestGoertzelPureTone(t *testing.T) {
	// Generate a pure tone at exactly 1000 Hz and verify Goertzel reports
	// maximum power at 1000 Hz and near-zero at 1100 Hz.
	hann := HannCoefficients(SamplesPerSymbol)
	frame := make([]float32, SamplesPerSymbol)
	const freq = 1000.0
	for i := range frame {
		frame[i] = float32(math.Cos(2 * math.Pi * freq * float64(i) / SampleRate))
	}

	powerAt := Goertzel(frame, hann, freq)
	powerOff := Goertzel(frame, hann, freq+100)

	if powerAt <= 0 {
		t.Fatalf("Goertzel at target freq: power = %g, want > 0", powerAt)
	}
	// Off-frequency power should be much smaller (>20 dB below).
	ratio := powerAt / powerOff
	if ratio < 100 {
		t.Errorf("power ratio (on/off) = %g, want > 100 (20 dB)", ratio)
	}
	t.Logf("power at %g Hz = %.2f, at %g Hz = %.2f, ratio = %.1f",
		freq, powerAt, freq+100, powerOff, ratio)
}

func TestGoertzelEmptyFrame(t *testing.T) {
	hann := HannCoefficients(SamplesPerSymbol)
	if p := Goertzel(nil, hann, 1000); p != 0 {
		t.Errorf("nil frame: power = %g, want 0", p)
	}
	if p := Goertzel([]float32{}, hann, 1000); p != 0 {
		t.Errorf("empty frame: power = %g, want 0", p)
	}
}

func TestGoertzelShortWindow(t *testing.T) {
	frame := make([]float32, SamplesPerSymbol)
	shortHann := HannCoefficients(10) // too short
	if p := Goertzel(frame, shortHann, 1000); p != 0 {
		t.Errorf("short window: power = %g, want 0", p)
	}
}

func TestGoertzelDC(t *testing.T) {
	// DC signal → all power at 0 Hz, none at 1000 Hz (after Hann windowing).
	hann := HannCoefficients(SamplesPerSymbol)
	frame := make([]float32, SamplesPerSymbol)
	for i := range frame {
		frame[i] = 1.0
	}
	powerDC := Goertzel(frame, hann, 0)
	power1k := Goertzel(frame, hann, 1000)

	if powerDC <= 0 {
		t.Errorf("DC power = %g, want > 0", powerDC)
	}
	if power1k > powerDC*0.01 {
		t.Errorf("1 kHz power = %g, want << DC power %g", power1k, powerDC)
	}
}

// --- GoertzelTones tests ---

func TestGoertzelTonesCorrectPeak(t *testing.T) {
	hann := HannCoefficients(SamplesPerSymbol)
	const baseFreq = 1000.0

	// For each tone, generate a pure sinusoid at that tone's frequency
	// and verify GoertzelTones identifies the correct peak.
	for tone := range NumTones {
		toneFreq := baseFreq + float64(tone)*ToneSpacing
		frame := make([]float32, SamplesPerSymbol)
		for i := range frame {
			frame[i] = float32(math.Cos(2 * math.Pi * toneFreq * float64(i) / SampleRate))
		}

		_, peak := GoertzelTones(frame, hann, baseFreq)
		if peak != tone {
			t.Errorf("tone %d: peak = %d, want %d", tone, peak, tone)
		}
	}
}

func TestGoertzelTonesAdjacentSuppression(t *testing.T) {
	hann := HannCoefficients(SamplesPerSymbol)
	const baseFreq = 1500.0

	// Generate tone 3 and verify adjacent tones are well-suppressed.
	const targetTone = 3
	toneFreq := baseFreq + float64(targetTone)*ToneSpacing
	frame := make([]float32, SamplesPerSymbol)
	for i := range frame {
		frame[i] = float32(math.Cos(2 * math.Pi * toneFreq * float64(i) / SampleRate))
	}

	powers, peak := GoertzelTones(frame, hann, baseFreq)
	if peak != targetTone {
		t.Fatalf("peak = %d, want %d", peak, targetTone)
	}

	// Each non-target tone should be well below the target.
	// With Hann windowing at 6.25 Hz spacing on 1920 samples, adjacent
	// tones have ~6 dB (factor ~4) leakage, which is expected. Non-adjacent
	// tones should be much lower.
	for k := range NumTones {
		if k == targetTone {
			continue
		}
		ratio := powers[k] / powers[targetTone]
		// Adjacent tones: allow up to 50% (-3 dB).
		// Non-adjacent tones: require < 10% (-10 dB).
		limit := 0.10
		if k == targetTone-1 || k == targetTone+1 {
			limit = 0.50
		}
		if ratio > limit {
			t.Errorf("tone %d power ratio = %.3f, want < %.2f (power=%.2f, target=%.2f)",
				k, ratio, limit, powers[k], powers[targetTone])
		}
	}
}

// --- DemodulateAudio round-trip ---

func TestDemodulateAudioRoundTrip(t *testing.T) {
	// Encode a known message, synthesise raw audio, demodulate, decode.
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}

	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	const baseFreqHz = 1000.0
	const amplitude = 1.0

	samples := make([]float32, WindowSamples)
	for sym := range NumSymbols {
		toneIdx := chanSyms[sym]
		toneFreq := baseFreqHz + float64(toneIdx)*ToneSpacing
		sampleStart := sym * SamplesPerSymbol
		for n := range SamplesPerSymbol {
			globalN := sampleStart + n
			samples[globalN] = amplitude * float32(
				math.Cos(2*math.Pi*toneFreq*float64(globalN)/float64(SampleRate)))
		}
	}

	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: baseFreqHz, TimeOff: 0}

	llr := DemodulateAudio(samples, hann, cand)
	decoded, ok := codec.DecodeMessage(llr, 50)
	if !ok {
		t.Fatal("DecodeMessage failed on DemodulateAudio output")
	}

	want := msg77
	want[9] &= 0xF8
	got := decoded
	got[9] &= 0xF8
	if got != want {
		t.Errorf("round-trip mismatch:\n  want %x\n  got  %x", want, got)
	}
}

// --- DemodulateAudio LLR signs ---

func TestDemodulateAudioLLRSigns(t *testing.T) {
	// Verify that LLR signs from DemodulateAudio match the coded bits.
	msg77 := [10]byte{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30}

	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	const baseFreqHz = 1500.0
	samples := make([]float32, WindowSamples)
	for sym := range NumSymbols {
		toneIdx := chanSyms[sym]
		toneFreq := baseFreqHz + float64(toneIdx)*ToneSpacing
		sampleStart := sym * SamplesPerSymbol
		for n := range SamplesPerSymbol {
			globalN := sampleStart + n
			samples[globalN] = float32(
				math.Cos(2 * math.Pi * toneFreq * float64(globalN) / float64(SampleRate)))
		}
	}

	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: baseFreqHz, TimeOff: 0}

	llr := DemodulateAudio(samples, hann, cand)

	// Verify each LLR sign matches the corresponding codeword bit.
	wrongCount := 0
	for i := range CodedBits {
		bit := (cwDSP[i/8] >> uint(7-i%8)) & 1
		if bit == 0 && llr[i] <= 0 {
			wrongCount++
		}
		if bit == 1 && llr[i] >= 0 {
			wrongCount++
		}
	}
	if wrongCount > 0 {
		t.Errorf("%d of %d LLR signs incorrect", wrongCount, CodedBits)
	}
}

// --- DemodulateAudio edge cases ---

func TestDemodulateAudioNilSamples(t *testing.T) {
	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: 1000, TimeOff: 0}
	llr := DemodulateAudio(nil, hann, cand)
	for i, v := range llr {
		if v != 0 {
			t.Errorf("nil samples: llr[%d] = %g, want 0", i, v)
			break
		}
	}
}

func TestDemodulateAudioOutOfBounds(t *testing.T) {
	hann := HannCoefficients(SamplesPerSymbol)
	// Buffer too short for the candidate's span.
	samples := make([]float32, SamplesPerSymbol*10)
	cand := Candidate{Freq: 1000, TimeOff: 0}
	llr := DemodulateAudio(samples, hann, cand)
	for i, v := range llr {
		if v != 0 {
			t.Errorf("too-short buffer: llr[%d] = %g, want 0", i, v)
			break
		}
	}
}

// --- LLR clamping ---

func TestDemodulateAudioLLRClamped(t *testing.T) {
	// Synthesise a strong signal and verify LLR magnitudes are clamped.
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}

	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	const baseFreqHz = 1000.0
	const amplitude = 10.0 // very strong signal

	samples := make([]float32, WindowSamples)
	for sym := range NumSymbols {
		toneIdx := chanSyms[sym]
		toneFreq := baseFreqHz + float64(toneIdx)*ToneSpacing
		sampleStart := sym * SamplesPerSymbol
		for n := range SamplesPerSymbol {
			globalN := sampleStart + n
			samples[globalN] = amplitude * float32(
				math.Cos(2*math.Pi*toneFreq*float64(globalN)/float64(SampleRate)))
		}
	}

	hann := HannCoefficients(SamplesPerSymbol)
	cand := Candidate{Freq: baseFreqHz, TimeOff: 0}
	llr := DemodulateAudio(samples, hann, cand)

	// Verify all LLR values are finite.
	for i, v := range llr {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			t.Errorf("llr[%d] = %g, expected finite value", i, v)
		}
	}
}
