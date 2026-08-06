package ft8

/*
   RX audio-level meter — the measurement half of the FT8 level indicator
   (dogfood 2026-08-06; operator: "we need to check the audio level coming
   into the PC from the rig - too high we may get clipping, too low can also
   be a problem decoding").

   ACCEPTANCE CRITERION (operator-agreed 2026-08-06, before mechanism):

       When capture is live, /v1/ft8/events carries a continuous
       ft8-audio-level event a few times a second: peak and RMS dBFS of the
       incoming audio over fixed windows — so the SPA can show clipping
       (peak at full scale) and too-quiet (RMS below the decode floor)
       apart from a healthy level. A SILENT capture still reports — a
       valid, very low level — which is what distinguishes it from NO
       capture, where nothing is published at all. Classification
       thresholds are NOT the daemon's: it publishes measurements, the SPA
       classifies against config-served, operator-calibratable bounds.

   The meter is pure arithmetic on the samples already flowing to the
   decoder, fed from a tee between the capture source and the scheduler —
   deliberately NOT inside the scheduler/slot path, which the TX +
   attribution invariants guard.

   WHY dBFS and not a percentage: clipping lives at a hard ceiling (0 dBFS
   = full-scale int16), and decode margin is a log quantity; both ends of
   the operator's question are natural in dB.

   The rules below pin the MEASUREMENT (values, cadence, accumulation);
   the wire event and its publish conditions are pinned at the service
   boundary in audiolevel_service_test.go.
*/

import (
	"math"
	"testing"
)

type emitted struct{ peak, rms float64 }

func collectEmits() (*[]emitted, func(float64, float64)) {
	var got []emitted
	return &got, func(p, r float64) { got = append(got, emitted{p, r}) }
}

// L1 — FULL SCALE READS 0 dBFS. Clipping is a hard ceiling, and the meter
// must place it exactly there — a peak that tops out below zero would make
// real clipping look like headroom.
func TestAudioLevel_FullScaleSquareReadsZeroDbfs(t *testing.T) {
	got, emit := collectEmits()
	m := newAudioLevelMeter(8, emit)

	m.feed([]int16{32767, -32768, 32767, -32768, 32767, -32768, 32767, -32768})

	if len(*got) != 1 {
		t.Fatalf("emits = %d, want 1", len(*got))
	}
	e := (*got)[0]
	if math.Abs(e.peak-0) > 0.01 {
		t.Errorf("peak = %.3f dBFS, want ~0", e.peak)
	}
	if math.Abs(e.rms-0) > 0.01 {
		t.Errorf("rms = %.3f dBFS, want ~0 for a square wave", e.rms)
	}
}

// L2 — A HALF-SCALE SINE READS ~-6 dBFS PEAK AND ~-9 dBFS RMS. Pins the
// scale is amplitude dB (20·log10) against full-scale int16, and that RMS
// really is RMS: a sine's crest factor puts RMS 3.01 dB under its peak —
// an implementation averaging |x| or reusing the peak cannot produce both
// numbers.
func TestAudioLevel_HalfScaleSineValues(t *testing.T) {
	got, emit := collectEmits()
	const n = 1200 // whole cycles so the RMS is exact
	m := newAudioLevelMeter(n, emit)

	buf := make([]int16, n)
	for i := range buf {
		buf[i] = int16(16384 * math.Sin(2*math.Pi*float64(i)/100))
	}
	m.feed(buf)

	if len(*got) != 1 {
		t.Fatalf("emits = %d, want 1", len(*got))
	}
	e := (*got)[0]
	if math.Abs(e.peak-(-6.02)) > 0.1 {
		t.Errorf("peak = %.2f dBFS, want ~-6.02", e.peak)
	}
	if math.Abs(e.rms-(-9.03)) > 0.1 {
		t.Errorf("rms = %.2f dBFS, want ~-9.03 (peak - 3.01 for a sine)", e.rms)
	}
}

// L3 — SILENCE IS A NUMBER, NOT NaN/-Inf. A silent-but-alive capture is the
// state the SPA must tell apart from a dead one, so the meter reports the
// floor value rather than nothing (or a value JSON cannot carry).
func TestAudioLevel_SilenceReadsTheFloor(t *testing.T) {
	got, emit := collectEmits()
	m := newAudioLevelMeter(8, emit)

	m.feed(make([]int16, 8))

	if len(*got) != 1 {
		t.Fatalf("emits = %d, want 1", len(*got))
	}
	e := (*got)[0]
	if e.peak != audioLevelFloorDbfs || e.rms != audioLevelFloorDbfs {
		t.Errorf("silence = (%.1f, %.1f), want the %.0f floor", e.peak, e.rms, audioLevelFloorDbfs)
	}
	if math.IsNaN(e.peak) || math.IsInf(e.peak, 0) || math.IsNaN(e.rms) || math.IsInf(e.rms, 0) {
		t.Errorf("silence produced a non-finite level")
	}
}

// L4 — ONE EMIT PER FULL WINDOW, REMAINDER CARRIED. 2.5 windows in one feed
// is exactly 2 emits; the half window is not dropped — the next feed
// completes it. Pins the cadence the SSE stream inherits.
func TestAudioLevel_WindowCadenceAndCarry(t *testing.T) {
	got, emit := collectEmits()
	m := newAudioLevelMeter(8, emit)

	m.feed(make([]int16, 20)) // 2.5 windows
	if len(*got) != 2 {
		t.Fatalf("emits after 2.5 windows = %d, want 2", len(*got))
	}

	m.feed(make([]int16, 4)) // completes the third
	if len(*got) != 3 {
		t.Fatalf("emits after the carry completes = %d, want 3", len(*got))
	}
}

// L5 — ACCUMULATION IS BATCH-SHAPE-INDEPENDENT. The same window split
// across three feeds reports the same values as one feed: the fixture puts
// the sine's PEAK in the last fragment, so an implementation that resets
// state per batch reads a different (quieter) window and fails.
func TestAudioLevel_SplitFeedsMatchOneFeed(t *testing.T) {
	const n = 1200
	buf := make([]int16, n)
	for i := range buf {
		// Quarter-cycle start: the early fragments are the LOW part of the
		// swing; the true peak arrives only near the end.
		buf[i] = int16(16384 * math.Sin(2*math.Pi*float64(i)/2400))
	}

	whole, emitW := collectEmits()
	mw := newAudioLevelMeter(n, emitW)
	mw.feed(buf)

	split, emitS := collectEmits()
	ms := newAudioLevelMeter(n, emitS)
	ms.feed(buf[:100])
	ms.feed(buf[100:700])
	ms.feed(buf[700:])

	if len(*whole) != 1 || len(*split) != 1 {
		t.Fatalf("emits = (%d, %d), want (1, 1)", len(*whole), len(*split))
	}
	if (*whole)[0] != (*split)[0] {
		t.Errorf("split feeds = %+v, one feed = %+v — accumulation depends on batch shape",
			(*split)[0], (*whole)[0])
	}
}

// L6 — AN EMPTY BATCH IS INERT: no emit, no state disturbance (the window
// in progress keeps its fill).
func TestAudioLevel_EmptyBatchIsInert(t *testing.T) {
	got, emit := collectEmits()
	m := newAudioLevelMeter(8, emit)

	m.feed(make([]int16, 4))
	m.feed(nil)
	m.feed([]int16{})
	if len(*got) != 0 {
		t.Fatalf("emits = %d, want 0", len(*got))
	}

	m.feed(make([]int16, 4)) // completes the window started before the empties
	if len(*got) != 1 {
		t.Fatalf("emits = %d, want 1 — the in-progress window was disturbed", len(*got))
	}
}
