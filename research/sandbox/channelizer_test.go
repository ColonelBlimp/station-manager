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
