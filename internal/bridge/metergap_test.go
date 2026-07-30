package bridge

import (
	"testing"
	"time"
)

// Meter-stream gap measurement — the follow-up measurement for the drive alarm's
// FINDING 1 (2026-07-30).
//
// ACCEPTANCE CRITERION:
//
//	When a transmission's drive has failed, the per-transmission log line tells
//	me the largest silence inside the keyed window, so I can see whether it
//	exceeded the alarm's 3 s — and I can tell "silent the whole slot" apart from
//	"silent for a moment".
//
// WHY THIS EXISTS. The drive alarm keys on GAPS, not on values, and the on-air
// run of 2026-07-30 showed absent drive is not silent as designed but sparsely
// noisy: the 05:01:15 slot pushed ~26-30 zero-valued frames at roughly 2-3 Hz
// while no RF was leaving the rig, and alarmed only because a complete gap
// happened to precede them. If those pushes ever start inside the window no gap
// opens and no alarm fires. Whether that can happen is a QUESTION ABOUT THE RIG,
// and this is the cheapest instrument that answers it: it costs no transmission
// of its own, riding along on whatever the operator was going to transmit anyway.
//
// THE WINDOW IS KEY-DOWN THROUGH UNKEY, deliberately mirroring the detector's own
// window rather than measuring frame-to-frame. Frame-to-frame gaps are undefined
// when no frames arrive at all — which is the exact case being investigated — so
// that definition would report nothing precisely where the answer lives. Both the
// LEADING silence (G4) and the TRAILING silence (G3) therefore count, because
// both are silences the detector would have tripped on.
//
// NO THRESHOLD IS INVENTED HERE. A gap is a measurement; the 3 s it will be
// compared against is already the operator's number. What the data may later
// justify — a value-aware rule needing a definition of "zero" — stays out of this
// file until the operator sets that definition.
//
// LIMIT OF THESE RULES, stated rather than left implied: they assert on the
// summary struct, which the log line renders field-for-field, because this
// package has no log capture (the test service takes a real &logging.Service{}).
// An implementation could therefore carry the numbers in the summary and fail to
// print them, and these rules would not catch it. Both log branches share one
// field-emitting path so there is a single visible place for that to be wrong.

// gapSlot keys a real FT8 transmission, runs body while it is keyed, then ends it
// and returns the summary the operator would have seen logged.
//
// The REAL key path, not setFt8Keyed: the measurement window opens where the
// drive watch arms, which is after the key write, and a test that set the TX flag
// directly would measure a window production never has (G7 pins that difference).
func gapSlot(t *testing.T, s *Service, body func()) ft8MeterSummary {
	t.Helper()
	keyedTestSlot(t, s)
	if body != nil {
		body()
	}
	s.finishFt8Tx()
	return s.lastFt8TxMeters()
}

// G1 — a stream that keeps flowing reports a gap far SMALLER than the window.
// Without this the whole measurement could be a constant: a broken
// implementation that always reported the window length would satisfy every
// silence rule below and still tell the operator nothing.
func TestMeterGap_SteadyStreamReportsSmallGap(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	sum := gapSlot(t, s, func() {
		feedMeterFor(t, s, "RM0118000", 200*time.Millisecond)
	})

	if !sum.GapMeasured {
		t.Fatal("no gap measured for a keyed transmission; the window never opened")
	}
	if sum.GapMax > sum.KeyedFor/2 {
		t.Errorf("gap_max = %v of a %v window; a continuously-fed stream must report a gap well under the window",
			sum.GapMax, sum.KeyedFor)
	}
}

// G2 — THE CASE FINDING 1 IS ABOUT. No frames at all for the whole slot: the gap
// is the entire window, and the operator gets NUMBERS rather than today's bare
// "rig pushed no meter data" line. Total silence is the shape 12 of the sweep's
// 24 transmissions took, so a measurement that goes blank here is useless.
func TestMeterGap_TotalSilenceReportsWholeWindow(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	sum := gapSlot(t, s, func() { time.Sleep(120 * time.Millisecond) })

	if !sum.GapMeasured {
		t.Fatal("a silent transmission reported no gap; silence is the finding, not a non-event")
	}
	if len(sum.Samples) != 0 {
		t.Fatalf("no frames were fed, yet samples = %+v", sum.Samples)
	}
	// Both are taken from one instant at flush, so the silence IS the window.
	if sum.GapMax != sum.KeyedFor {
		t.Errorf("gap_max = %v but window = %v; with no frames at all they must be the same span",
			sum.GapMax, sum.KeyedFor)
	}
	if sum.KeyedFor < 100*time.Millisecond {
		t.Errorf("window = %v, want at least the ~120ms the slot was held; it is not measuring from key-down",
			sum.KeyedFor)
	}
}

// G3 — frames that STOP part-way in: the trailing silence counts, measured to
// unkey. This is the collapse the alarm caught on air at 04:57:24, and an
// implementation that only measured between frames would report a tiny gap for
// it — the loudest possible false reassurance.
func TestMeterGap_StreamStoppingMidSlotReportsTrailingSilence(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	sum := gapSlot(t, s, func() {
		feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)
		time.Sleep(150 * time.Millisecond) // drive dies; nothing more is pushed
	})

	if !sum.GapMeasured {
		t.Fatal("no gap measured")
	}
	if sum.GapMax < 120*time.Millisecond {
		t.Errorf("gap_max = %v after ~150ms of trailing silence; the tail to unkey is not being counted",
			sum.GapMax)
	}
}

// G4 — frames that START LATE: the leading silence counts, measured from
// key-down. This is the 05:01:15 shape, where drive was dead from the key and
// sparse zero-valued frames arrived only later; it is also the only rule that
// distinguishes "measured from key-down" from "measured from the first frame".
func TestMeterGap_StreamStartingLateReportsLeadingSilence(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	sum := gapSlot(t, s, func() {
		time.Sleep(150 * time.Millisecond) // drive dead from the key
		feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)
	})

	if !sum.GapMeasured {
		t.Fatal("no gap measured")
	}
	if sum.GapMax < 120*time.Millisecond {
		t.Errorf("gap_max = %v after ~150ms of silence before the first frame; the window is starting at the first frame, not at key-down",
			sum.GapMax)
	}
}

// G5 — the measurement is PER TRANSMISSION. Without this a single bad slot would
// stain every slot after it, which is exactly the defect the accumulator's own
// reset rule (R5) exists to prevent, and the reason the drive alarm re-arms.
//
// THE FIXTURE MATTERS HERE and the first version of it was wrong. A transmission
// that is silent throughout never writes the running maximum at all — its silence
// is computed at flush as a local — so a leak of that field could not show up,
// and the rule passed against an implementation that never reset it. The silence
// must therefore be BETWEEN frames, which is what actually persists.
func TestMeterGap_DoesNotCarryAcrossTransmissions(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	collapsed := gapSlot(t, s, func() {
		s.observeMeter(meterFrame(t, "RM0118000"))
		time.Sleep(150 * time.Millisecond)         // drive dies...
		s.observeMeter(meterFrame(t, "RM0118000")) // ...and comes back, banking the gap
	})
	if collapsed.GapMax < 120*time.Millisecond {
		t.Fatalf("first transmission gap_max = %v; the fixture is not producing the silence the rule needs", collapsed.GapMax)
	}

	rxMeterFlowing(t, s)
	healthy := gapSlot(t, s, func() {
		feedMeterFor(t, s, "RM0118000", 200*time.Millisecond)
	})

	if healthy.GapMax > healthy.KeyedFor/2 {
		t.Errorf("second transmission gap_max = %v of a %v window; the previous transmission's silence carried over",
			healthy.GapMax, healthy.KeyedFor)
	}
}

// G6 — measured even when the DETECTOR DECLINED TO ARM. With no receive-time
// evidence the alarm deliberately stays silent (D3), because a dead instrument
// must not be reported as a dead transmitter — and that is precisely when the
// operator needs the numbers, since telling those two apart is the open question.
// A measurement that switched off with the alarm would be blind exactly where it
// is needed.
func TestMeterGap_MeasuredEvenWhenDetectorDidNotArm(t *testing.T) {
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)
	// No rxMeterFlowing: nothing proves the instrument is alive.

	sum := gapSlot(t, s, func() { time.Sleep(120 * time.Millisecond) })

	if !sum.GapMeasured {
		t.Error("no gap measured when the instrument was never proven alive; that is the case the measurement exists for")
	}
	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm fired %d time(s) with no instrument-alive evidence; the measurement must not have armed the detector", n)
	}
}

// G7 — a transmission whose window never opened reports NO gap rather than a
// fabricated one. The window opens after the key write, so a failed or bypassed
// key leaves it closed; a zero timestamp subtracted from now would print a gap of
// decades and read as the most catastrophic drive fault ever recorded.
func TestMeterGap_UnopenedWindowReportsNothing(t *testing.T) {
	s, _ := newCommandTestService(t)

	setFt8Keyed(s, true) // TX flag only — never went through the key path
	s.finishFt8Tx()

	sum := s.lastFt8TxMeters()
	if sum.GapMeasured {
		t.Errorf("gap reported as measured for a transmission that never keyed: gap_max=%v window=%v",
			sum.GapMax, sum.KeyedFor)
	}
	if sum.GapMax != 0 || sum.KeyedFor != 0 {
		t.Errorf("gap_max=%v window=%v, want both zero when nothing was measured", sum.GapMax, sum.KeyedFor)
	}
}
