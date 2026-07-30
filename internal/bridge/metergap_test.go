package bridge

import (
	"context"
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
// "UNKEY" MEANS THE tx_off WRITE, NOT THE END OF THE TRANSMISSION, and the first
// implementation got that wrong — it measured to finishFt8Tx, which the release
// path reaches only after the confirmation wait, the settle and the mode restore,
// all with PTT already down. Those two instants read as the same thing and are
// seconds apart, and the error inflated the very quantity being measured. G8 pins
// the distinction; the rules that call finishFt8Tx directly cannot see it.
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

// G8 — THE WINDOW ENDS WHEN tx_off IS WRITTEN, not when the release path
// finishes. Production unkeys, then waits for confirmation (up to confirmTimeout
// + 1 s), then settles, then writes the mode restore, and only then closes the
// transmission. PTT is down for all of that, so counting it inflates keyed_ms and
// — the part that actually matters — can push gap_max past the operator's 3 s on a
// perfectly healthy transmission, purely because confirmation was slow. A
// measurement that corrupts the number it exists to produce is worse than none.
//
// The other rules call finishFt8Tx directly and cannot see this: they never
// exercise the release sequence.
func TestMeterGap_WindowEndsAtUnkeyNotAtRestore(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)

	// Production's post-unkey tail: a mode to restore and a settle before it.
	// Nothing feeds meter frames during it, so it is 200ms of pure silence that
	// must NOT be attributed to the transmission.
	s.mu.Lock()
	s.ft8TxRestoreMode = "USB"
	s.tuneRestoreSettle = 200 * time.Millisecond
	s.mu.Unlock()

	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	sum := s.lastFt8TxMeters()
	if !sum.GapMeasured {
		t.Fatal("no gap measured across a real unkey")
	}
	if sum.KeyedFor > 150*time.Millisecond {
		t.Errorf("keyed_ms = %v for a ~50ms transmission with a 200ms post-unkey tail; the window is running past the unkey write",
			sum.KeyedFor)
	}
	if sum.GapMax > 100*time.Millisecond {
		t.Errorf("gap_max = %v; the post-unkey silence is being reported as a drive gap", sum.GapMax)
	}
}

// G9 — SEALED MEANS FROZEN. Meter frames keep arriving after the unkey — the rig
// resumes pushing the S-meter in receive at ~26 Hz — and they must not enlarge
// the sealed maximum. The first implementation stored the window length at the
// seal but went on reading the running maximum live, so every transmission in
// production would have had the TX→RX lull folded into its widest silence, and a
// rig that fell quiet during the confirmation wait would have contributed the
// whole of it. G8 could not see this: nothing feeds frames after its unkey, so
// the sealed and unsealed paths agree there.
func TestMeterGap_SealedResultIgnoresLaterPushes(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)

	// A long post-unkey tail, and one meter frame landing in the middle of it —
	// the receive stream coming back after a lull far wider than anything inside
	// the transmission.
	s.mu.Lock()
	s.ft8TxRestoreMode = "USB"
	s.tuneRestoreSettle = 400 * time.Millisecond
	s.mu.Unlock()
	go func() {
		time.Sleep(200 * time.Millisecond)
		s.observeMeter(meterFrame(t, "RM0118000"))
	}()

	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	sum := s.lastFt8TxMeters()
	if !sum.GapMeasured {
		t.Fatal("no gap measured across a real unkey")
	}
	if sum.GapMax > 150*time.Millisecond {
		t.Errorf("gap_max = %v; a push arriving after the seal enlarged the frozen maximum", sum.GapMax)
	}
}

// G10 — the window ends when tx_off is ISSUED, not when the write call returns.
// On CI-V that call waits for the rig's FB/FA acknowledgement before returning,
// so PTT is already down for the whole of the ACK latency; the same holds for any
// slow write on any protocol. Measuring from the call's return folds that latency
// into the transmission. Stated protocol-neutrally, and tested by making the write
// itself slow, because the defect is about WHEN the instant is taken and not about
// CI-V.
func TestMeterGap_SlowUnkeyWriteDoesNotInflateWindow(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)

	// The unkey write blocks, modelling an awaited acknowledgement. Only tx_off is
	// slowed: the key write must stay fast or the fixture would measure that too.
	fake.onWrite = func(w []byte) []byte {
		if string(w) == "TX0;" {
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	}

	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	sum := s.lastFt8TxMeters()
	if !sum.GapMeasured {
		t.Fatal("no gap measured across a real unkey")
	}
	if sum.KeyedFor > 150*time.Millisecond {
		t.Errorf("keyed_ms = %v for a ~50ms transmission whose unkey write took 200ms; the instant is taken after the write returns",
			sum.KeyedFor)
	}
	if sum.GapMax > 150*time.Millisecond {
		t.Errorf("gap_max = %v; the unkey write's own duration is being counted as a drive gap", sum.GapMax)
	}
}

// G11 — a meter push arriving DURING the unkey write must not enlarge the gap
// either. This is the seam between G9 and G10 and it survived both: G9's push
// lands after the write returns, G10 slows the write but sends no push, and the
// interval in between — PTT down, ACK not yet received, window not yet sealed —
// was still accepting frames. A push at t2 recorded t2 minus the last in-window
// push permanently, and sealing afterwards with the earlier instant could not
// remove it, because sealing only computes a TRAILING gap (negative by then) and
// never revisits the running maximum.
//
// Stopping at the cutoff instant is what the rule needs, not stopping once the
// seal has been recorded.
func TestMeterGap_PushDuringUnkeyWriteDoesNotEnlargeGap(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)

	// The unkey write blocks, and the receive stream comes back in the middle of
	// it — exactly what a CI-V ACK wait looks like from the meter's side.
	fake.onWrite = func(w []byte) []byte {
		if string(w) == "TX0;" {
			time.Sleep(200 * time.Millisecond)
			s.observeMeter(meterFrame(t, "RM0118000"))
			time.Sleep(200 * time.Millisecond)
		}
		return nil
	}

	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	sum := s.lastFt8TxMeters()
	if !sum.GapMeasured {
		t.Fatal("no gap measured across a real unkey")
	}
	if sum.GapMax > 150*time.Millisecond {
		t.Errorf("gap_max = %v; a push arriving while the unkey write was in flight was counted as a drive gap", sum.GapMax)
	}
}

// G12 — an unkey whose write FAILS must not end the measurement. Sealing before
// the write creates this state: TX stays armed, the auto-off backstop retries, and
// the rig may still be transmitting — which is precisely when the operator needs
// the drive measurement to keep running. A window frozen at the failed attempt
// would report a transmission that was still in progress as finished.
func TestMeterGap_FailedUnkeyWriteResumesMeasurement(t *testing.T) {
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	rxMeterFlowing(t, s)

	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 50*time.Millisecond)

	_ = fake.Close() // the tx-off write now returns ErrClosed
	if err := s.UnkeyFt8Tx(context.Background()); err == nil {
		t.Fatal("UnkeyFt8Tx reported success on a dead port; the fixture proves nothing")
	}

	// Still keyed as far as the bridge knows, so the window is still open.
	time.Sleep(150 * time.Millisecond)
	s.finishFt8Tx()

	sum := s.lastFt8TxMeters()
	if !sum.GapMeasured {
		t.Fatal("no gap measured after a failed unkey")
	}
	if sum.KeyedFor < 150*time.Millisecond {
		t.Errorf("keyed_ms = %v; the window froze at the FAILED unkey attempt, but the transmission ran on for ~200ms after it",
			sum.KeyedFor)
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
