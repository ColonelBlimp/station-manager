// dsp_test.go — tests for the top-level FT8 RX pipeline.

package dsp

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
)

// --- ProcessWindow edge cases ---

func TestProcessWindowNilSamples(t *testing.T) {
	if msgs := ProcessWindow(nil, 10, 25); msgs != nil {
		t.Errorf("nil samples: got %d messages, want nil", len(msgs))
	}
}

func TestProcessWindowEmptySamples(t *testing.T) {
	if msgs := ProcessWindow([]float32{}, 10, 25); msgs != nil {
		t.Errorf("empty samples: got %d messages, want nil", len(msgs))
	}
}

func TestProcessWindowTooShort(t *testing.T) {
	// Fewer than SamplesPerSymbol → cannot build even one spectrogram frame.
	samples := make([]float32, SamplesPerSymbol-1)
	if msgs := ProcessWindow(samples, 10, 25); msgs != nil {
		t.Errorf("too-short samples: got %d messages, want nil", len(msgs))
	}
}

func TestProcessWindowMaxCandidatesZero(t *testing.T) {
	samples := make([]float32, WindowSamples)
	if msgs := ProcessWindow(samples, 0, 25); msgs != nil {
		t.Errorf("maxCandidates=0: got %d messages, want nil", len(msgs))
	}
}

func TestProcessWindowMaxIterZero(t *testing.T) {
	samples := make([]float32, WindowSamples)
	if msgs := ProcessWindow(samples, 10, 0); msgs != nil {
		t.Errorf("maxIter=0: got %d messages, want nil", len(msgs))
	}
}

func TestProcessWindowSilence(t *testing.T) {
	// Silence → no candidates, no decodes.
	samples := make([]float32, WindowSamples)
	msgs := ProcessWindow(samples, 50, 25)
	if len(msgs) != 0 {
		t.Errorf("silence: got %d messages, want 0", len(msgs))
	}
}

func TestProcessWindowTooFewFrames(t *testing.T) {
	// Buffer that produces fewer than NumSymbols (79) frames.
	nSamples := (NumSymbols - 1) * SamplesPerSymbol
	samples := make([]float32, nSamples)
	msgs := ProcessWindow(samples, 10, 25)
	if msgs != nil {
		t.Errorf("too-few-frames: got %d messages, want nil", len(msgs))
	}
}

// --- Synthesised round-trip test ---

// TestProcessWindowRoundTrip encodes a known message, synthesises the full
// 79-symbol FT8 tone sequence as audio, embeds it in a capture buffer, and
// verifies that ProcessWindow decodes the original message.
func TestProcessWindowRoundTrip(t *testing.T) {
	// Known 77-bit message.
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}

	// TX chain: message → LDPC encode → symbols → channel sequence.
	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	// Base frequency: 1000 Hz (well within the 200–3000 Hz search range).
	const baseFreqHz = 1000.0
	const amplitude = 1.0

	// Synthesise the audio: 79 symbols × 1920 samples each = 151680 samples.
	// Embed starting at time offset 0 within a full-length window buffer.
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

	// Run the RX pipeline.
	msgs := ProcessWindow(samples, 50, 50)

	if len(msgs) == 0 {
		t.Fatal("ProcessWindow: no messages decoded from synthesised signal")
	}

	// Find the original message in the results.
	want := msg77
	want[9] &= 0xF8
	found := false
	for _, m := range msgs {
		got := m.Msg77
		got[9] &= 0xF8
		if got == want {
			found = true
			// Verify frequency is near 1000 Hz (within a few bin widths).
			if m.Freq < 900 || m.Freq > 1100 {
				t.Errorf("decoded message frequency %g Hz, want ~1000 Hz", m.Freq)
			}
			// Verify time offset is near 0 seconds.
			if m.TimeOff > 1.0 {
				t.Errorf("decoded message time offset %g s, want near 0", m.TimeOff)
			}
			break
		}
	}
	if !found {
		t.Errorf("original message %x not found in %d decoded messages", want, len(msgs))
		for i, m := range msgs {
			t.Logf("  message[%d]: %x freq=%.1f Hz time=%.3f s snr=%.1f dB",
				i, m.Msg77, m.Freq, m.TimeOff, m.SNR)
		}
	}
}

// --- Deduplication test ---

// TestProcessWindowDeduplication verifies that the same message at two
// slightly different frequencies is decoded only once.
func TestProcessWindowDeduplication(t *testing.T) {
	msg77 := [10]byte{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30}

	cw := codec.EncodeMessage(msg77)
	var cwDSP [CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := BitsToSymbols(cwDSP)
	chanSyms := InsertSync(dataSyms)

	// Place the same signal at two frequencies: 1000 Hz and ~1006.25 Hz (one
	// tone apart — close enough that the candidate detector may pick up both).
	const freq1 = 1000.0
	const freq2 = 1500.0 // far enough for separate candidate detection
	const amplitude = 1.0

	samples := make([]float32, WindowSamples)
	for _, baseFreq := range [2]float64{freq1, freq2} {
		for sym := range NumSymbols {
			toneIdx := chanSyms[sym]
			toneFreq := baseFreq + float64(toneIdx)*ToneSpacing
			sampleStart := sym * SamplesPerSymbol
			for n := range SamplesPerSymbol {
				globalN := sampleStart + n
				samples[globalN] += amplitude * float32(
					math.Cos(2*math.Pi*toneFreq*float64(globalN)/float64(SampleRate)))
			}
		}
	}

	msgs := ProcessWindow(samples, 100, 50)

	// Count how many times the message appears.
	want := msg77
	want[9] &= 0xF8
	count := 0
	for _, m := range msgs {
		got := m.Msg77
		got[9] &= 0xF8
		if got == want {
			count++
		}
	}

	if count == 0 {
		t.Fatal("message not decoded at all")
	}
	if count > 1 {
		t.Errorf("message decoded %d times, want 1 (deduplication failed)", count)
	}
}

// --- Multiple distinct messages ---

// TestProcessWindowMultipleMessages verifies that two distinct messages
// at different frequencies are both decoded.
func TestProcessWindowMultipleMessages(t *testing.T) {
	msg1 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	msg2 := [10]byte{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30}

	const freq1 = 800.0
	const freq2 = 1800.0 // well separated
	const amplitude = 1.0

	samples := make([]float32, WindowSamples)

	for _, tc := range []struct {
		msg  [10]byte
		freq float64
	}{
		{msg1, freq1},
		{msg2, freq2},
	} {
		cw := codec.EncodeMessage(tc.msg)
		var cwDSP [CodewordBytes]byte
		copy(cwDSP[:], cw[:])
		dataSyms := BitsToSymbols(cwDSP)
		chanSyms := InsertSync(dataSyms)

		for sym := range NumSymbols {
			toneIdx := chanSyms[sym]
			toneFreq := tc.freq + float64(toneIdx)*ToneSpacing
			sampleStart := sym * SamplesPerSymbol
			for n := range SamplesPerSymbol {
				globalN := sampleStart + n
				samples[globalN] += amplitude * float32(
					math.Cos(2*math.Pi*toneFreq*float64(globalN)/float64(SampleRate)))
			}
		}
	}

	msgs := ProcessWindow(samples, 100, 50)

	want1 := msg1
	want1[9] &= 0xF8
	want2 := msg2
	want2[9] &= 0xF8

	found1, found2 := false, false
	for _, m := range msgs {
		got := m.Msg77
		got[9] &= 0xF8
		if got == want1 {
			found1 = true
		}
		if got == want2 {
			found2 = true
		}
	}

	if !found1 {
		t.Errorf("message 1 (%x) not decoded", want1)
	}
	if !found2 {
		t.Errorf("message 2 (%x) not decoded", want2)
	}
}

// --- SNR estimate tests ---

func TestEstimateSNRPositiveScore(t *testing.T) {
	// score > noiseFloor → positive SNR.
	snr := estimateSNR(100.0, 10.0)
	want := float32(10.0 * math.Log10(100.0/10.0)) // 10 dB
	if !approxEq(snr, want, 0.01) {
		t.Errorf("estimateSNR(100,10) = %g dB, want %g dB", snr, want)
	}
}

func TestEstimateSNRScoreLessThanNoise(t *testing.T) {
	// score < noiseFloor → negative SNR.
	snr := estimateSNR(1.0, 100.0)
	if snr >= 0 {
		t.Errorf("estimateSNR(1,100) = %g dB, want < 0", snr)
	}
}

func TestEstimateSNRZeroScore(t *testing.T) {
	// score ≤ 0 → floor.
	snr := estimateSNR(0, 10.0)
	if snr != -30.0 {
		t.Errorf("estimateSNR(0,10) = %g, want -30", snr)
	}
	snr2 := estimateSNR(-5.0, 10.0)
	if snr2 != -30.0 {
		t.Errorf("estimateSNR(-5,10) = %g, want -30", snr2)
	}
}

func TestEstimateSNRZeroNoiseFloor(t *testing.T) {
	// noiseFloor = 0 → falls back to 10*log10(score).
	snr := estimateSNR(100.0, 0)
	want := float32(10.0 * math.Log10(100.0))
	if !approxEq(snr, want, 0.01) {
		t.Errorf("estimateSNR(100,0) = %g dB, want %g dB", snr, want)
	}
}

// --- Noise floor estimator ---

func TestEstimateNoiseFloorEmpty(t *testing.T) {
	if nf := estimateNoiseFloor(nil); nf != 0 {
		t.Errorf("nil spectrogram: noise floor = %g, want 0", nf)
	}
	if nf := estimateNoiseFloor([][]float32{}); nf != 0 {
		t.Errorf("empty spectrogram: noise floor = %g, want 0", nf)
	}
}

func TestEstimateNoiseFloorUniform(t *testing.T) {
	// Uniform spectrogram → noise floor equals the fill value.
	sg := makeSpectrogram(10, 100, 5.0)
	nf := estimateNoiseFloor(sg)
	if !approxEq64(nf, 5.0, 1e-6) {
		t.Errorf("uniform spectrogram: noise floor = %g, want 5.0", nf)
	}
}

func TestEstimateNoiseFloorSilence(t *testing.T) {
	sg := makeSpectrogram(10, 100, 0)
	nf := estimateNoiseFloor(sg)
	if nf != 0 {
		t.Errorf("silence spectrogram: noise floor = %g, want 0", nf)
	}
}

// --- DecodedMessage struct ---

func TestDecodedMessageZeroValue(t *testing.T) {
	var dm DecodedMessage
	if dm.Freq != 0 || dm.TimeOff != 0 || dm.SNR != 0 {
		t.Error("zero-value DecodedMessage has non-zero fields")
	}
	for i, b := range dm.Msg77 {
		if b != 0 {
			t.Errorf("zero-value Msg77[%d] = %d, want 0", i, b)
			break
		}
	}
}

// --- ProcessWindow with spectrogram-only input (minimal frame count) ---

func TestProcessWindowExactly79Frames(t *testing.T) {
	// A buffer that produces exactly 79 frames — the minimum for one
	// FT8 message. With silence, nothing should decode, but it must not panic.
	nSamples := NumSymbols * SamplesPerSymbol
	samples := make([]float32, nSamples)
	msgs := ProcessWindow(samples, 10, 25)
	if len(msgs) != 0 {
		t.Errorf("silence with 79 frames: got %d messages, want 0", len(msgs))
	}
}

// --- SNR ordering ---

func TestProcessWindowSNROrdering(t *testing.T) {
	// Two messages with different amplitudes. The stronger one should have
	// a higher (or equal) SNR in the output. This test synthesises two
	// signals and checks the relative SNR ordering.
	msg1 := [10]byte{0xAA, 0xBB, 0xCC, 0xDD, 0xEE, 0xFF, 0x11, 0x22, 0x33, 0x40}
	msg2 := [10]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xA0}

	const freq1 = 800.0
	const freq2 = 2000.0
	const ampStrong = 2.0
	const ampWeak = 0.5

	samples := make([]float32, WindowSamples)

	for _, tc := range []struct {
		msg  [10]byte
		freq float64
		amp  float32
	}{
		{msg1, freq1, ampStrong},
		{msg2, freq2, ampWeak},
	} {
		cw := codec.EncodeMessage(tc.msg)
		var cwDSP [CodewordBytes]byte
		copy(cwDSP[:], cw[:])
		dataSyms := BitsToSymbols(cwDSP)
		chanSyms := InsertSync(dataSyms)

		for sym := range NumSymbols {
			toneIdx := chanSyms[sym]
			toneFreq := tc.freq + float64(toneIdx)*ToneSpacing
			sampleStart := sym * SamplesPerSymbol
			for n := range SamplesPerSymbol {
				globalN := sampleStart + n
				samples[globalN] += tc.amp * float32(
					math.Cos(2*math.Pi*toneFreq*float64(globalN)/float64(SampleRate)))
			}
		}
	}

	msgs := ProcessWindow(samples, 100, 50)
	if len(msgs) < 2 {
		t.Skipf("only %d messages decoded; need 2 for SNR comparison", len(msgs))
	}

	want1 := msg1
	want1[9] &= 0xF8
	want2 := msg2
	want2[9] &= 0xF8

	var snr1, snr2 float32
	found1, found2 := false, false
	for _, m := range msgs {
		got := m.Msg77
		got[9] &= 0xF8
		if got == want1 {
			snr1 = m.SNR
			found1 = true
		}
		if got == want2 {
			snr2 = m.SNR
			found2 = true
		}
	}

	if !found1 || !found2 {
		t.Skipf("not both messages decoded (found1=%v, found2=%v)", found1, found2)
	}

	// The stronger signal (msg1, amp=2.0) should have higher SNR.
	if snr1 < snr2 {
		t.Errorf("stronger signal SNR %g dB < weaker signal SNR %g dB", snr1, snr2)
	}
}
