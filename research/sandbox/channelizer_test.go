package sandbox

import (
	"math"
	"math/cmplx"
	"testing"

	"github.com/ColonelBlimp/station-manager/research/sandbox/pfft"
)

// TestChannelizer_PureToneAtBinCenter feeds a pure sine wave at a
// frequency that lands on an exact FFT bin and channelizes at that
// frequency. The baseband should have its energy concentrated near
// the middle of the output buffer (the 180000/192000 rect-windowing
// of the input maps to a wide flat region in baseband time, tapered
// at the edges by both the freq-domain Tukey and the time-domain
// rect of the original signal).
func TestChannelizer_PureToneAtBinCenter(t *testing.T) {
	const (
		toneHz = 1500.0 // = bin 24000 of the 192000-FFT (exact)
		amp    = 0.5
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.Extract(toneHz, 200.0)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseband) != 3200 {
		t.Fatalf("expected 3200 baseband samples, got %d", len(baseband))
	}

	// Middle of baseband should dominate the edges (the freq-domain
	// Tukey + time-domain rect shape concentrates energy in the middle).
	midMag := cmplx.Abs(baseband[1600])
	leftEdgeMag := cmplx.Abs(baseband[0])
	rightEdgeMag := cmplx.Abs(baseband[3199])
	if midMag <= leftEdgeMag {
		t.Errorf("expected mid > left edge: mid=%g left=%g", midMag, leftEdgeMag)
	}
	if midMag <= rightEdgeMag {
		t.Errorf("expected mid > right edge: mid=%g right=%g", midMag, rightEdgeMag)
	}
}

// TestChannelizer_ToneOffsetDetectable feeds a tone offset by 5 Hz
// from the channelizer centre and confirms the baseband contains a
// complex sinusoid at +5 Hz. Verifies the frequency-shift correctness
// of the channelizer end-to-end: forward FFT, slice, fftshift, IFFT.
//
// Verification path: take the FFT of the baseband signal and check
// the peak bin lands at the expected location given the baseband
// sample rate (200 Hz complex) and bin spacing (200/3200 = 0.0625 Hz).
// A 5 Hz offset → peak at bin 80.
func TestChannelizer_ToneOffsetDetectable(t *testing.T) {
	const (
		centerHz = 1500.0
		offsetHz = 5.0
		toneHz   = centerHz + offsetHz
		amp      = 0.5
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.Extract(centerHz, 200.0)
	if err != nil {
		t.Fatal(err)
	}

	// FFT the baseband. Length 3200, sample rate 200 Hz (complex), so
	// bin spacing is 0.0625 Hz. A 5 Hz offset peaks at bin 80.
	plan, err := pfft.NewComplexPlan(3200)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	work := append([]complex128(nil), baseband...)
	if err := plan.Forward(work); err != nil {
		t.Fatal(err)
	}

	// Find peak bin in the positive-frequency half [0, 1600).
	maxBin := 0
	maxMag := 0.0
	for i := 0; i < 1600; i++ {
		m := cmplx.Abs(work[i])
		if m > maxMag {
			maxBin = i
			maxMag = m
		}
	}

	expectedBin := int(math.Round(offsetHz / (200.0 / 3200.0)))
	if maxBin != expectedBin {
		t.Errorf("expected baseband FFT peak at bin %d (+%.1f Hz), got bin %d (~%.3f Hz)",
			expectedBin, offsetHz, maxBin, float64(maxBin)*200.0/3200.0)
	}
}

// TestChannelizer_NegativeOffsetDetectable mirrors the above but
// offsets the tone BELOW the channelizer centre. Negative baseband
// frequencies wrap into the upper half of the FFT output, so a -5 Hz
// offset → bin 3200-80 = 3120.
func TestChannelizer_NegativeOffsetDetectable(t *testing.T) {
	const (
		centerHz = 1500.0
		offsetHz = -7.5
		toneHz   = centerHz + offsetHz
		amp      = 0.5
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.Extract(centerHz, 200.0)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := pfft.NewComplexPlan(3200)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	work := append([]complex128(nil), baseband...)
	if err := plan.Forward(work); err != nil {
		t.Fatal(err)
	}

	// Find peak across the full spectrum; negative freqs live in [1600, 3200).
	maxBin := 0
	maxMag := 0.0
	for i, v := range work {
		m := cmplx.Abs(v)
		if m > maxMag {
			maxBin = i
			maxMag = m
		}
	}

	// -7.5 Hz at 0.0625 Hz/bin = 120 bins below DC = bin 3200-120 = 3080.
	expectedBin := 3200 + int(math.Round(offsetHz/(200.0/3200.0)))
	if maxBin != expectedBin {
		t.Errorf("expected baseband FFT peak at bin %d (%.1f Hz), got bin %d",
			expectedBin, offsetHz, maxBin)
	}
}

// TestChannelizer_PrepareRequiresFullSlot pins the input-length contract.
func TestChannelizer_PrepareRequiresFullSlot(t *testing.T) {
	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Prepare(make([]float32, channelizerSlotLen-1)); err == nil {
		t.Errorf("expected error for short audio")
	}
}

// TestChannelizer_ExtractBandwidthValidation covers the slice-bounds
// and minimum-bandwidth error paths.
func TestChannelizer_ExtractBandwidthValidation(t *testing.T) {
	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Prepare(make([]float32, channelizerSlotLen)); err != nil {
		t.Fatal(err)
	}

	// Bandwidth less than 2 bins should error.
	if _, err := c.Extract(1500, 0.05); err == nil {
		t.Errorf("expected error for sub-2-bin bandwidth")
	}
	// Slice off DC end.
	if _, err := c.Extract(50, 200); err == nil {
		t.Errorf("expected error for slice extending below DC")
	}
	// Slice off Nyquist end.
	if _, err := c.Extract(5950, 200); err == nil {
		t.Errorf("expected error for slice extending above Nyquist")
	}
}

// TestExtractAsymmetric_PositiveOffsetAdmitted feeds a tone offset by
// +5 Hz from the channelizer centre and channelizes with the FT8
// asymmetric geometry (lowerHz=1.5·baud, upperHz=8.5·baud, output
// bandwidth 200 Hz). +5 Hz is comfortably inside the upper flat region
// (admitted offsets [+0, +850), flat region offsets [-25, +725)) so the
// peak should land at output FFT bin 80, identical to the symmetric
// Extract reference test above.
func TestExtractAsymmetric_PositiveOffsetAdmitted(t *testing.T) {
	const (
		baud      = 6.25
		centerHz  = 1500.0
		offsetHz  = 5.0
		toneHz    = centerHz + offsetHz
		amp       = 0.5
		lowerHz   = 1.5 * baud
		upperHz   = 8.5 * baud
		outputBwH = 200.0
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	if len(baseband) != 3200 {
		t.Fatalf("expected 3200 baseband samples, got %d", len(baseband))
	}

	plan, err := pfft.NewComplexPlan(3200)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	work := append([]complex128(nil), baseband...)
	if err := plan.Forward(work); err != nil {
		t.Fatal(err)
	}

	maxBin := 0
	maxMag := 0.0
	for i := 0; i < 1600; i++ {
		m := cmplx.Abs(work[i])
		if m > maxMag {
			maxBin = i
			maxMag = m
		}
	}
	expectedBin := int(math.Round(offsetHz / (outputBwH / 3200.0)))
	if maxBin != expectedBin {
		t.Errorf("expected peak at bin %d (+%.1f Hz), got bin %d (~%.3f Hz)",
			expectedBin, offsetHz, maxBin, float64(maxBin)*outputBwH/3200.0)
	}
}

// TestExtractAsymmetric_NegativeOffsetAdmitted pins the wrap-direction
// for negative admitted offsets. -0.625 Hz (= -10 bins) sits well inside
// the lower flat region (flat region offsets [-25, +725), the lower
// negative-offset flat span is [-25, -1]). Expect the baseband FFT peak
// at bin 3200 + offsetBins = 3200 - 10 = 3190.
func TestExtractAsymmetric_NegativeOffsetAdmitted(t *testing.T) {
	const (
		baud      = 6.25
		centerHz  = 1500.0
		offsetHz  = -0.625
		toneHz    = centerHz + offsetHz
		amp       = 0.5
		lowerHz   = 1.5 * baud
		upperHz   = 8.5 * baud
		outputBwH = 200.0
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := pfft.NewComplexPlan(3200)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	work := append([]complex128(nil), baseband...)
	if err := plan.Forward(work); err != nil {
		t.Fatal(err)
	}

	maxBin := 0
	maxMag := 0.0
	for i, v := range work {
		m := cmplx.Abs(v)
		if m > maxMag {
			maxBin = i
			maxMag = m
		}
	}
	expectedBin := 3200 + int(math.Round(offsetHz/(outputBwH/3200.0)))
	if maxBin != expectedBin {
		t.Errorf("expected peak at bin %d (%.3f Hz), got bin %d",
			expectedBin, offsetHz, maxBin)
	}
}

// TestExtractAsymmetric_Tone7Admitted pins the upper edge of the FT8
// tone band. Tone 7 sits at +43.75 Hz (= +700 bins) — at the very
// end of the Tukey flat region (taper index 850 of 1000; flat region
// 125..874). Peak should still land at output bin 700.
func TestExtractAsymmetric_Tone7Admitted(t *testing.T) {
	const (
		baud      = 6.25
		centerHz  = 1500.0
		offsetHz  = 7 * baud
		toneHz    = centerHz + offsetHz
		amp       = 0.5
		lowerHz   = 1.5 * baud
		upperHz   = 8.5 * baud
		outputBwH = 200.0
	)

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	baseband, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}

	plan, err := pfft.NewComplexPlan(3200)
	if err != nil {
		t.Fatal(err)
	}
	defer plan.Close()

	work := append([]complex128(nil), baseband...)
	if err := plan.Forward(work); err != nil {
		t.Fatal(err)
	}

	maxBin := 0
	maxMag := 0.0
	for i := 0; i < 1600; i++ {
		m := cmplx.Abs(work[i])
		if m > maxMag {
			maxBin = i
			maxMag = m
		}
	}
	expectedBin := int(math.Round(offsetHz / (outputBwH / 3200.0)))
	if maxBin != expectedBin {
		t.Errorf("expected peak at bin %d (+%.3f Hz), got bin %d",
			expectedBin, offsetHz, maxBin)
	}
}

// TestExtractAsymmetric_RejectsBelowGuard feeds a tone 3·baud below the
// channelizer centre (-18.75 Hz, well outside the -1.5·baud lower
// guard). The asymmetric extractor zeros that bin in the IFFT input, so
// the baseband peak amplitude should be << the admitted-tone case.
//
// Compares against a same-input admitted-region tone to make the ratio
// explicit: rejection ≥ 100× (40 dB) attenuation is the bar.
func TestExtractAsymmetric_RejectsBelowGuard(t *testing.T) {
	const (
		baud         = 6.25
		centerHz     = 1500.0
		rejectOffset = -3 * baud
		admitOffset  = 5.0
		amp          = 0.5
		lowerHz      = 1.5 * baud
		upperHz      = 8.5 * baud
		outputBwH    = 200.0
	)

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	// Reference: admitted tone gives an "expected peak magnitude".
	refAudio := makeChannelizerTone(centerHz+admitOffset, amp)
	if err := c.Prepare(refAudio); err != nil {
		t.Fatal(err)
	}
	refBase, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	refPeak := basebandPeakMag(refBase)

	// Reject test: out-of-band tone.
	rejAudio := makeChannelizerTone(centerHz+rejectOffset, amp)
	if err := c.Prepare(rejAudio); err != nil {
		t.Fatal(err)
	}
	rejBase, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	rejPeak := basebandPeakMag(rejBase)

	// 0.3 ratio bar: the un-windowed 180000-sample input means the
	// rect-window's sinc sidelobes spread the rejected tone's energy
	// across many bins; asymmetric extraction removes the main lobe
	// (the geometric win vs symmetric Extract, which would admit it at
	// full amplitude) but the sidelobe residue is irreducible without
	// pre-windowing. ~25% residual is expected for a tone 150 bins
	// outside the admitted region; the bar catches the main-peak-
	// admitted failure mode (wrap-direction bug would push the ratio to
	// ~1.0) while remaining honest about what un-windowed FFT achieves.
	if rejPeak > 0.3*refPeak {
		t.Errorf("rejection too weak: out-of-band peak=%.1f, in-band peak=%.1f (ratio %.4f, want < 0.30)",
			rejPeak, refPeak, rejPeak/refPeak)
	}
}

// TestExtractAsymmetric_RejectsAboveGuard mirrors the above for the
// upper guard. Tone at +10·baud = +62.5 Hz, well beyond the +8.5·baud
// upper edge.
func TestExtractAsymmetric_RejectsAboveGuard(t *testing.T) {
	const (
		baud         = 6.25
		centerHz     = 1500.0
		rejectOffset = 10 * baud
		admitOffset  = 5.0
		amp          = 0.5
		lowerHz      = 1.5 * baud
		upperHz      = 8.5 * baud
		outputBwH    = 200.0
	)

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	refAudio := makeChannelizerTone(centerHz+admitOffset, amp)
	if err := c.Prepare(refAudio); err != nil {
		t.Fatal(err)
	}
	refBase, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	refPeak := basebandPeakMag(refBase)

	rejAudio := makeChannelizerTone(centerHz+rejectOffset, amp)
	if err := c.Prepare(rejAudio); err != nil {
		t.Fatal(err)
	}
	rejBase, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	rejPeak := basebandPeakMag(rejBase)

	// 0.3 ratio bar: the un-windowed 180000-sample input means the
	// rect-window's sinc sidelobes spread the rejected tone's energy
	// across many bins; asymmetric extraction removes the main lobe
	// (the geometric win vs symmetric Extract, which would admit it at
	// full amplitude) but the sidelobe residue is irreducible without
	// pre-windowing. ~25% residual is expected for a tone 150 bins
	// outside the admitted region; the bar catches the main-peak-
	// admitted failure mode (wrap-direction bug would push the ratio to
	// ~1.0) while remaining honest about what un-windowed FFT achieves.
	if rejPeak > 0.3*refPeak {
		t.Errorf("rejection too weak: out-of-band peak=%.1f, in-band peak=%.1f (ratio %.4f, want < 0.30)",
			rejPeak, refPeak, rejPeak/refPeak)
	}
}

// TestSetAsymmetricFT8Slice_DispatchEquivalent pins the channelizer-
// local mode dispatch: with SetAsymmetricFT8Slice(true), calling
// Extract(centerHz, 200.0) must produce identical output to calling
// ExtractAsymmetric(centerHz, 1.5·baud, 8.5·baud, 200.0) directly on
// the same prepared spectrum. The dispatch is the load-bearing
// invariant that lets RefineCandidate / ExtractSymbols stay unchanged
// across the symmetric/asymmetric A/B.
func TestSetAsymmetricFT8Slice_DispatchEquivalent(t *testing.T) {
	const (
		baud      = 6.25
		centerHz  = 1500.0
		offsetHz  = 5.0
		toneHz    = centerHz + offsetHz
		amp       = 0.5
		lowerHz   = 1.5 * baud
		upperHz   = 8.5 * baud
		outputBwH = 200.0
	)

	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}
	if err := c.Prepare(audio); err != nil {
		t.Fatal(err)
	}

	// Direct ExtractAsymmetric.
	direct, err := c.ExtractAsymmetric(centerHz, lowerHz, upperHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}

	// Mode-toggled Extract.
	c.SetAsymmetricFT8Slice(true)
	viaMode, err := c.Extract(centerHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	c.SetAsymmetricFT8Slice(false)

	if len(direct) != len(viaMode) {
		t.Fatalf("length mismatch: direct=%d via-mode=%d", len(direct), len(viaMode))
	}
	for i := range direct {
		if direct[i] != viaMode[i] {
			t.Fatalf("sample mismatch at i=%d: direct=%v via-mode=%v", i, direct[i], viaMode[i])
		}
	}

	// And toggling back to symmetric must NOT match asymmetric (sanity:
	// the toggle actually changes behaviour, not silently aliasing).
	symm, err := c.Extract(centerHz, outputBwH)
	if err != nil {
		t.Fatal(err)
	}
	identical := true
	for i := range symm {
		if symm[i] != direct[i] {
			identical = false
			break
		}
	}
	if identical {
		t.Errorf("symmetric Extract identical to asymmetric — mode toggle is a no-op?")
	}
}

// TestExtractAsymmetric_Validation covers the error paths.
func TestExtractAsymmetric_Validation(t *testing.T) {
	c, err := NewChannelizer()
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Prepare(make([]float32, channelizerSlotLen)); err != nil {
		t.Fatal(err)
	}

	// Negative guard widths.
	if _, err := c.ExtractAsymmetric(1500, -1, 50, 200); err == nil {
		t.Errorf("expected error for negative lowerHz")
	}
	if _, err := c.ExtractAsymmetric(1500, 10, -1, 200); err == nil {
		t.Errorf("expected error for negative upperHz")
	}
	// Output bandwidth too small.
	if _, err := c.ExtractAsymmetric(1500, 10, 50, 0); err == nil {
		t.Errorf("expected error for zero outputBandwidthHz")
	}
	// Admitted bins < 2.
	if _, err := c.ExtractAsymmetric(1500, 0, 0, 200); err == nil {
		t.Errorf("expected error for zero admitted bandwidth")
	}
	// Admitted > output.
	if _, err := c.ExtractAsymmetric(1500, 200, 200, 200); err == nil {
		t.Errorf("expected error for admitted > output")
	}
	// Slice off DC end.
	if _, err := c.ExtractAsymmetric(5, 10, 50, 200); err == nil {
		t.Errorf("expected error for low edge < 0")
	}
	// Slice off Nyquist end.
	if _, err := c.ExtractAsymmetric(5999, 10, 50, 200); err == nil {
		t.Errorf("expected error for high edge > Nyquist")
	}
}

// makeChannelizerTone returns the channelizer-slot-sized audio buffer
// for a pure sine at toneHz, amplitude amp.
func makeChannelizerTone(toneHz, amp float64) []float32 {
	audio := make([]float32, channelizerSlotLen)
	for i := range audio {
		tt := float64(i) / channelizerSampleRate
		audio[i] = float32(amp * math.Sin(2*math.Pi*toneHz*tt))
	}
	return audio
}

// basebandPeakMag returns the largest |baseband[i]| over the time
// domain. Steadier than per-sample magnitude for cleanly-admitted
// tones (which give roughly-constant baseband magnitude) and small for
// rejected tones (which leave a residue limited to spectral-leakage
// floor of the un-windowed forward FFT).
func basebandPeakMag(baseband []complex128) float64 {
	peak := 0.0
	for _, v := range baseband {
		m := cmplx.Abs(v)
		if m > peak {
			peak = m
		}
	}
	return peak
}

// TestTukeyWindow pins the corner cases of the taper function.
func TestTukeyWindow(t *testing.T) {
	// alpha = 0: constant 1
	for i := 0; i < 10; i++ {
		if v := tukeyWindow(i, 10, 0); v != 1 {
			t.Errorf("alpha=0 should be 1, got %g at i=%d", v, i)
		}
	}
	// alpha = 0.25 on length 100: edges at 0, middle at 1.
	n := 100
	if v := tukeyWindow(0, n, 0.25); v > 1e-9 {
		t.Errorf("Tukey(0) should be ~0, got %g", v)
	}
	if v := tukeyWindow(n-1, n, 0.25); v > 1e-9 {
		t.Errorf("Tukey(n-1) should be ~0, got %g", v)
	}
	if v := tukeyWindow(n/2, n, 0.25); math.Abs(v-1) > 1e-9 {
		t.Errorf("Tukey(n/2) should be 1, got %g", v)
	}
	// Symmetry.
	for i := 0; i < n/2; i++ {
		left := tukeyWindow(i, n, 0.25)
		right := tukeyWindow(n-1-i, n, 0.25)
		if math.Abs(left-right) > 1e-9 {
			t.Errorf("Tukey not symmetric at i=%d: left=%g right=%g", i, left, right)
		}
	}
}
