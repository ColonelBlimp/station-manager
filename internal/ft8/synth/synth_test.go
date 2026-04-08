package synth

import (
	"math"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/ft8/codec"
	"github.com/ColonelBlimp/station-manager/internal/ft8/dsp"
)

// --- SynthesizeWithAmplitude edge cases ---

func TestSynthesizeWithAmplitudeNil(t *testing.T) {
	var symbols [dsp.NumSymbols]uint8
	if s := SynthesizeWithAmplitude(symbols, 1000, 0); s != nil {
		t.Error("expected nil for amplitude=0")
	}
	if s := SynthesizeWithAmplitude(symbols, 1000, -1); s != nil {
		t.Error("expected nil for amplitude<0")
	}
}

func TestSynthesizeWithAmplitudeClamp(t *testing.T) {
	// amplitude > 1.0 should be clamped to 1.0, not rejected.
	var symbols [dsp.NumSymbols]uint8
	s := SynthesizeWithAmplitude(symbols, 1000, 2.0)
	if s == nil {
		t.Fatal("expected non-nil for amplitude>1.0 (should clamp)")
	}
	for i, v := range s {
		if v < -1.0 || v > 1.0 {
			t.Errorf("s[%d] = %f, out of [-1, 1] after clamping", i, v)
			break
		}
	}
}

// --- Synthesize output properties ---

func TestSynthesizeLength(t *testing.T) {
	var symbols [dsp.NumSymbols]uint8
	s := Synthesize(symbols, 1000)
	if len(s) != OutputSamples {
		t.Errorf("len = %d, want %d", len(s), OutputSamples)
	}
}

func TestSynthesizeAmplitudeBounds(t *testing.T) {
	// All samples must be within [-DefaultAmplitude, +DefaultAmplitude].
	// Use a varied symbol sequence to exercise all tones.
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = uint8(i % dsp.NumTones)
	}
	s := Synthesize(symbols, 1000)

	amp := float32(DefaultAmplitude)
	for i, v := range s {
		if v < -amp-1e-6 || v > amp+1e-6 {
			t.Errorf("s[%d] = %f, out of [-%f, %f]", i, v, amp, amp)
			break
		}
	}
}

func TestSynthesizePhaseStartsNearZero(t *testing.T) {
	// Phase starts at 0, so sin(0) = 0. The first sample should be ≈ 0.
	// With envelope shaping the first sample is additionally multiplied by
	// env(0) = 0, so it should be exactly 0.
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = 3
	}
	s := Synthesize(symbols, 1000)
	if s[0] != 0 {
		t.Errorf("s[0] = %f, want 0 (sin(0) × env(0))", s[0])
	}
}

func TestSynthesizeEnvelopeShaping(t *testing.T) {
	// The first rampSamples and last rampSamples should be envelope-shaped.
	// At the edges (i=0 and i=OutputSamples-1), the envelope is 0.
	// At mid-ramp (i=rampSamples/2), the envelope is ≈ 0.5.
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = 3 // constant tone so the underlying signal is uniform
	}
	s := Synthesize(symbols, 1000)

	// First and last samples must be zero (env=0).
	if s[0] != 0 {
		t.Errorf("s[0] = %f, want 0", s[0])
	}
	if s[OutputSamples-1] != 0 {
		t.Errorf("s[last] = %f, want 0", s[OutputSamples-1])
	}

	// After the ramp, samples should be at full amplitude. Check a sample
	// well past the ramp (at 2× rampSamples). For a constant-tone signal,
	// the absolute value should be close to the amplitude at some point.
	maxInBody := float32(0)
	bodyStart := 2 * rampSamples
	bodyEnd := OutputSamples - 2*rampSamples
	for i := bodyStart; i < bodyEnd; i++ {
		v := s[i]
		if v < 0 {
			v = -v
		}
		if v > maxInBody {
			maxInBody = v
		}
	}
	// The body should reach close to DefaultAmplitude.
	if maxInBody < float32(DefaultAmplitude)*0.95 {
		t.Errorf("max body amplitude = %f, expected ≈ %f", maxInBody, DefaultAmplitude)
	}
}

func TestSynthesizeConstantToneFrequency(t *testing.T) {
	// A constant-symbol input should produce a sinusoid at the expected
	// frequency. Verify by finding the FFT peak in a window well past
	// the envelope ramp.
	tone := uint8(3)
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = tone
	}

	baseFreq := 1000.0
	expectedFreq := baseFreq + float64(tone)*dsp.ToneSpacing // 1018.75 Hz

	s := Synthesize(symbols, baseFreq)

	// Extract a segment well past the ramp (symbols 5–15, 10 symbol periods).
	startSample := 5 * dsp.SamplesPerSymbol
	nSamples := 10 * dsp.SamplesPerSymbol
	segment := s[startSample : startSample+nSamples]

	// FFT the segment.
	bins := dsp.RealFFT(segment)
	ps := dsp.PowerSpectrum(bins)

	// Find the peak bin.
	peakBin := 0
	peakPower := ps[0]
	for i, p := range ps {
		if p > peakPower {
			peakPower = p
			peakBin = i
		}
	}

	// Convert bin index to frequency.
	fftSize := dsp.NextPow2(nSamples)
	binWidth := float64(dsp.SampleRate) / float64(fftSize)
	peakFreq := float64(peakBin) * binWidth

	// Allow ±1 bin tolerance.
	if math.Abs(peakFreq-expectedFreq) > binWidth {
		t.Errorf("peak frequency = %.2f Hz (bin %d), expected ≈ %.2f Hz (binWidth=%.2f)",
			peakFreq, peakBin, expectedFreq, binWidth)
	}
}

func TestSynthesizeDifferentAmplitudes(t *testing.T) {
	// Verify that amplitude scaling works: a signal at amplitude 0.5
	// should have half the peak of amplitude 1.0.
	var symbols [dsp.NumSymbols]uint8
	for i := range symbols {
		symbols[i] = 3
	}

	s1 := SynthesizeWithAmplitude(symbols, 1000, 1.0)
	s05 := SynthesizeWithAmplitude(symbols, 1000, 0.5)

	// Check a sample in the body (well past envelope ramp).
	// The ratio of corresponding samples should be ≈ 0.5.
	idx := 10 * dsp.SamplesPerSymbol // middle of the waveform
	if s1[idx] == 0 {
		// Find a non-zero sample in the body.
		for i := 5 * dsp.SamplesPerSymbol; i < 15*dsp.SamplesPerSymbol; i++ {
			if s1[i] != 0 {
				idx = i
				break
			}
		}
	}

	if s1[idx] == 0 {
		t.Fatal("no non-zero sample found in body")
	}

	ratio := float64(s05[idx]) / float64(s1[idx])
	if math.Abs(ratio-0.5) > 0.01 {
		t.Errorf("amplitude ratio = %.4f, want ≈ 0.5 (s1=%f, s05=%f)", ratio, s1[idx], s05[idx])
	}
}

// --- Full TX→RX round-trip ---

func TestSynthesizeTXRXRoundTrip(t *testing.T) {
	// This is the capstone test: encode a message, synthesise GFSK audio,
	// then decode it with dsp.ProcessWindow and verify the original message
	// is recovered.
	//
	// Pipeline:
	//   codec.EncodeMessage → dsp.BitsToSymbols → dsp.InsertSync →
	//   synth.Synthesize → embed in WindowSamples buffer →
	//   dsp.ProcessWindow → verify msg77 matches
	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	msg77[9] &= 0xF8 // mask trailing bits for clean comparison

	// TX: encode → symbols → GFSK synthesis.
	cw := codec.EncodeMessage(msg77)
	var cwDSP [dsp.CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := dsp.BitsToSymbols(cwDSP)
	chanSyms := dsp.InsertSync(dataSyms)

	baseFreq := 1000.0
	signal := Synthesize(chanSyms, baseFreq)
	if signal == nil {
		t.Fatal("Synthesize returned nil")
	}

	// Embed the signal in a full 15-second window buffer.
	// Signal is placed at the start (time offset = 0).
	window := make([]float32, dsp.WindowSamples)
	copy(window, signal)

	// RX: decode.
	decoded := dsp.ProcessWindow(window, 100, 50)
	if len(decoded) == 0 {
		t.Fatal("ProcessWindow returned no decoded messages")
	}

	// Verify the original message was recovered.
	found := false
	for _, dm := range decoded {
		if dm.Msg77 == msg77 {
			found = true
			t.Logf("decoded: freq=%.1f Hz, timeOff=%.3f s, SNR=%.1f dB",
				dm.Freq, dm.TimeOff, dm.SNR)
			break
		}
	}
	if !found {
		t.Error("original message not recovered in decoded output")
		for i, dm := range decoded {
			t.Logf("  decoded[%d]: msg77=%x", i, dm.Msg77)
		}
	}
}

func TestSynthesizeMultipleMessageRoundTrip(t *testing.T) {
	// Synthesise two distinct messages at different frequencies and verify
	// both are decoded.
	msgs := [2][10]byte{
		{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8},
		{0xCA, 0xFE, 0xBA, 0xBE, 0xDE, 0xAD, 0x00, 0x11, 0x22, 0x30},
	}
	freqs := [2]float64{800.0, 1500.0}

	// Mask trailing bits.
	msgs[0][9] &= 0xF8
	msgs[1][9] &= 0xF8

	window := make([]float32, dsp.WindowSamples)

	for m := range 2 {
		cw := codec.EncodeMessage(msgs[m])
		var cwDSP [dsp.CodewordBytes]byte
		copy(cwDSP[:], cw[:])
		dataSyms := dsp.BitsToSymbols(cwDSP)
		chanSyms := dsp.InsertSync(dataSyms)
		signal := Synthesize(chanSyms, freqs[m])
		// Add (not overwrite) so both signals coexist.
		for i, v := range signal {
			window[i] += v
		}
	}

	decoded := dsp.ProcessWindow(window, 100, 50)

	found := [2]bool{}
	for _, dm := range decoded {
		for m := range 2 {
			if dm.Msg77 == msgs[m] {
				found[m] = true
				t.Logf("decoded msg[%d]: freq=%.1f Hz, SNR=%.1f dB", m, dm.Freq, dm.SNR)
			}
		}
	}

	for m := range 2 {
		if !found[m] {
			t.Errorf("message %d (%x) not decoded", m, msgs[m])
		}
	}
}

// --- Cross-validation against ft8_lib's synth_gfsk ---

func TestSynthesizeCrossValidationFt8Lib(t *testing.T) {
	// Validate our synthesis output against a reference implementation
	// of ft8_lib's synth_gfsk algorithm. Both should produce waveforms
	// that decode to the same message, and their phase trajectories
	// should be similar.

	msg77 := [10]byte{0xDE, 0xAD, 0xBE, 0xEF, 0x12, 0x34, 0x56, 0x78, 0x9A, 0xB8}
	cw := codec.EncodeMessage(msg77)
	var cwDSP [dsp.CodewordBytes]byte
	copy(cwDSP[:], cw[:])
	dataSyms := dsp.BitsToSymbols(cwDSP)
	chanSyms := dsp.InsertSync(dataSyms)

	baseFreq := 1000.0
	ours := Synthesize(chanSyms, baseFreq)
	ref := referenceSynthGFSK(chanSyms, baseFreq, DefaultAmplitude)

	if len(ours) != len(ref) {
		t.Fatalf("length mismatch: ours=%d, ref=%d", len(ours), len(ref))
	}

	// Compare waveforms in the body (past envelope ramp on both ends).
	// The two implementations use slightly different smoothing approaches
	// (Gaussian kernel conv vs erf-difference overlap-add) so we expect
	// close but not bit-identical results. Check that the maximum sample
	// difference is small.
	bodyStart := 2 * rampSamples
	bodyEnd := OutputSamples - 2*rampSamples
	maxDiff := float32(0)
	for i := bodyStart; i < bodyEnd; i++ {
		d := ours[i] - ref[i]
		if d < 0 {
			d = -d
		}
		if d > maxDiff {
			maxDiff = d
		}
	}

	// Allow up to 0.02 sample difference (~2% of amplitude). The main
	// source of difference is the smoothing kernel truncation.
	if maxDiff > 0.02 {
		t.Errorf("max body sample difference = %f, want < 0.02", maxDiff)
	}
}

// referenceSynthGFSK implements the ft8_lib synth_gfsk algorithm for
// cross-validation. This is a direct port of gen_ft8.c's synth_gfsk.
func referenceSynthGFSK(symbols [dsp.NumSymbols]uint8, baseFreqHz, amplitude float64) []float32 {
	nsps := dsp.SamplesPerSymbol
	nsym := dsp.NumSymbols
	nWave := nsym * nsps

	// Compute the GFSK pulse (erf-difference), length 3*nsps.
	c := math.Pi * math.Sqrt(2.0/math.Ln2) // GFSK_CONST_K
	bt := GaussianBT
	pulse := make([]float64, 3*nsps)
	for i := range pulse {
		tt := float64(i)/float64(nsps) - 1.5
		arg1 := c * bt * (tt + 0.5)
		arg2 := c * bt * (tt - 0.5)
		pulse[i] = 0.5 * (math.Erf(arg1) - math.Erf(arg2))
	}

	// Build dphi array (phase increment per sample), length (nsym+2)*nsps.
	dphiLen := (nsym + 2) * nsps
	dphi := make([]float64, dphiLen)

	// Initialise with base frequency phase increment.
	baseDphi := 2 * math.Pi * baseFreqHz / dsp.SampleRate
	for i := range dphi {
		dphi[i] = baseDphi
	}

	// Overlap-add: for each symbol, add dphi_peak * tone * pulse.
	dphiPeak := 2 * math.Pi * 1.0 / float64(nsps) // hmod=1.0
	for j := range nsym {
		ib := j * nsps
		for k := range 3 * nsps {
			dphi[ib+k] += dphiPeak * float64(symbols[j]) * pulse[k]
		}
	}

	// Dummy symbols at start and end.
	for k := range 2 * nsps {
		dphi[k] += dphiPeak * float64(symbols[0]) * pulse[k+nsps]
		dphi[nsym*nsps+k] += dphiPeak * float64(symbols[nsym-1]) * pulse[k]
	}

	// Phase integration — skip first dummy symbol (start at nsps).
	phi := 0.0
	signal := make([]float32, nWave)
	for k := range nWave {
		signal[k] = float32(amplitude * math.Sin(phi))
		phi = math.Mod(phi+dphi[k+nsps], 2*math.Pi)
	}

	// Envelope shaping.
	nRamp := nsps / 8
	for i := range nRamp {
		env := (1 - math.Cos(2*math.Pi*float64(i)/float64(2*nRamp))) / 2
		signal[i] *= float32(env)
		signal[nWave-1-i] *= float32(env)
	}

	return signal
}
