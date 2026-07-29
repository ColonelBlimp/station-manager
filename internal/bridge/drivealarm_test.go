package bridge

import (
	"context"
	"sync"
	"testing"
	"time"
)

// Drive-collapse alarm (follow-up (1) of the 2026-07-29 meter arc).
//
// ACCEPTANCE CRITERION (operator-approved 2026-07-29):
//
//	When I transmit and no RF is leaving the rig, Station Manager raises an SSE
//	alarm banner DURING the slot rather than waiting for it to end — and stays
//	silent both when the meter instrumentation itself has failed and when drive
//	is merely reduced.
//
// Operator decisions folded in: alarm on TOTAL collapse only (not reduced
// drive), MID-SLOT (not at unkey), delivered as an SSE banner. Silence
// threshold: 3 s (operator's call — the healthy stream ran ~12 Hz during FT8
// TX so normal gaps are ~80 ms, and the observed collapse left a ~10 s gap;
// 3 s separates them and still leaves ~9 s of warning in a 12.6 s slot).
//
// WHY THE DISCRIMINATOR IS THE RX STREAM, NOT FRAMES WITHIN THE SLOT: total
// collapse is usually silent FROM KEY-DOWN — 12 of the 24 transmissions in the
// controlled sweep produced no frames at all — so "the stream stopped" would
// miss the common case. What proves the instrument is alive is that frames were
// flowing while receiving; the rig pushes the S-meter continuously in RX
// (measured: 654 RM0 frames in 25 s via cmd/catcli). Absence in RX *and* TX is
// a broken instrument, which is a different fault and must not raise this
// alarm. That distinction is D3, and it is the rule a naive "no meter data
// during TX ⇒ alarm" implementation fails.
//
// These rules were written before the implementation. They assert on what is
// OBSERVABLE — a published SSE event, the rig still keyed, whether a subsequent
// key is permitted — not on the detector's internal state, so the mechanism can
// change without rewriting them.
//
// OPEN QUESTION, deliberately not answered here: frames that FLOW but all read
// zero. The hardware does not produce this (the rig pushes on change, so a
// meter pinned at zero goes silent, which is why collapse manifests as silence)
// but it is reachable in principle. D5 pins only what was decided — frames
// flowing means no alarm, whatever their value. If that turns out to be wrong,
// it is a rule to add, not a threshold to invent.

// driveAlarmTestSilence shortens the silence threshold so a test does not sit
// out the real 3 s. Package-level, so these tests must not run in parallel —
// the same constraint txConfirmTimeout already carries.
func driveAlarmTestSilence(t *testing.T, d time.Duration) {
	t.Helper()
	prev := driveSilenceTimeout
	driveSilenceTimeout = d
	t.Cleanup(func() { driveSilenceTimeout = prev })
}

// eventWatch records everything published to one subscription, so a test can
// assert an event's ARRIVAL and its ABSENCE without racing the channel.
type eventWatch struct {
	mu   sync.Mutex
	seen []Event
}

func watchEvents(t *testing.T, s *Service) *eventWatch {
	t.Helper()
	ch, unsub := s.Subscribe()
	w := &eventWatch{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			w.mu.Lock()
			w.seen = append(w.seen, e)
			w.mu.Unlock()
		}
	}()
	t.Cleanup(func() { unsub(); <-done })
	return w
}

func (w *eventWatch) count(name EventName) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := 0
	for _, e := range w.seen {
		if e.Name == name {
			n++
		}
	}
	return n
}

// rxMeterFlowing feeds meter pushes with FT8 TX inactive — the receive-time
// stream that proves the instrument is alive.
func rxMeterFlowing(t *testing.T, s *Service) {
	t.Helper()
	for i := 0; i < 5; i++ {
		s.observeMeter(meterFrame(t, "RM0118000"))
	}
}

// feedMeterFor pushes a meter frame repeatedly for the given span, modelling a
// stream that keeps flowing rather than one sample.
func feedMeterFor(t *testing.T, s *Service, line string, span time.Duration) {
	t.Helper()
	deadline := time.Now().Add(span)
	for time.Now().Before(deadline) {
		s.observeMeter(meterFrame(t, line))
		time.Sleep(span / 10)
	}
}

// keyedTestSlot starts a real FT8 transmission on the test service.
func keyedTestSlot(t *testing.T, s *Service) {
	t.Helper()
	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
}

func wroteUnkey(fake *fakeSerial) bool {
	for _, w := range fake.recordedWrites() {
		if string(w) == "TX0;" {
			return true
		}
	}
	return false
}

// D1 — THE COMMON CASE. Drive is dead from key-down, so the rig pushes nothing
// at all during the slot. The instrument is known good because frames were
// flowing in receive. This must alarm, and must do so while the rig is STILL
// KEYED — an alarm at unkey is forensic and lets the next slot fail too.
func TestDriveAlarm_SilentFromKeyDown_AlarmsDuringSlot(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	// No meter frames at all for the whole slot — drive never came up.

	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"no drive alarm published while keyed with a dead drive and a known-good instrument")

	// Mid-slot, not at unkey: the transmission must still be running and no
	// tx_off may have been written when the alarm arrives. Without this the rule
	// "during the slot" is untested — an at-unkey implementation would pass a
	// test that only checks the alarm exists.
	s.mu.Lock()
	stillKeyed := s.ft8TxActive
	s.mu.Unlock()
	if !stillKeyed {
		t.Error("drive alarm arrived after the transmission ended; want it raised mid-slot")
	}
	if wroteUnkey(fake) {
		t.Error("drive alarm arrived after tx_off was written; want it raised mid-slot")
	}
}

// D2 — drive comes up and then collapses partway through the slot. Same fault,
// the other shape (measured on hardware: max=34 with n=23, against ~155 healthy).
func TestDriveAlarm_CollapseMidSlot_Alarms(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0034000", 50*time.Millisecond) // drive present, then gone

	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"no drive alarm published after the meter stream stopped mid-slot")
}

// D3 — THE DISCRIMINATOR, and the load-bearing rule of this feature. The meter
// instrumentation is dead: nothing arrived in receive either. That is a
// different fault (CAT, AI mode, a rig that does not push RM) and it must NOT
// raise a drive alarm — the operator would be told their transmitter is dead
// when what is actually broken is the instrument reading it.
//
// This is the test a naive "no meter data during TX ⇒ alarm" fails.
func TestDriveAlarm_DeadInstrument_DoesNotAlarm(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	// Deliberately NO receive-time meter frames: the instrument never spoke.
	keyedTestSlot(t, s)
	time.Sleep(4 * driveSilenceTimeout)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times with a dead instrument; want 0 — "+
			"absence of meter data is only evidence of no drive when the instrument is known good", n)
	}
}

// D4 — healthy transmission. Frames flow throughout; nothing is wrong.
func TestDriveAlarm_HealthyDrive_DoesNotAlarm(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0034000", 3*driveSilenceTimeout)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times on a healthy transmission; want 0", n)
	}
}

// D5 — REDUCED drive, not absent. Measured on hardware at -3 dB: max=5, n=33,
// and it was producing real RF (5 W into a dummy load). The operator's decision
// is that this does NOT alarm. Pinned because it is the obvious wrong turn: an
// implementation that alarms on a low READING rather than on SILENCE passes
// every other test in this file.
func TestDriveAlarm_ReducedDrive_DoesNotAlarm(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0005000", 3*driveSilenceTimeout) // low but flowing

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times on reduced-but-present drive; want 0", n)
	}
}

// D6 — the drive alarm must NOT latch txUncertain. That flag is the ADR 0051
// stuck-carrier interlock: it refuses all further keying until positive RX
// evidence clears it, and nothing in a drive fault is evidence the PTT is
// stuck. Latching it would turn "your drive died" into "you cannot transmit
// again", which is a worse outcome than the fault.
func TestDriveAlarm_DoesNotBlockTheNextKey(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm to test against")

	s.mu.Lock()
	uncertain := s.txUncertain
	alarmed := s.txAlarmActive
	s.mu.Unlock()
	if uncertain {
		t.Error("drive alarm set txUncertain; want it clear — a drive fault is not a stuck carrier")
	}
	if alarmed {
		t.Error("drive alarm latched the ADR 0051 tx-alarm state; want it untouched")
	}

	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}
	if !s.TxReady() {
		t.Error("TxReady false after a drive alarm; want the next transmission permitted")
	}
}

// D7 — the drive alarm travels on its OWN event, never the ADR 0051 tx-alarm.
// The hub caches the latest tx-alarm for late subscribers and only positive RX
// evidence may publish Active=false on it; a drive alarm sharing that slot could
// retire a standing "CHECK YOUR RADIO" banner for every tab.
func TestDriveAlarm_DoesNotPublishOnTheTxAlarmEvent(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm to test against")

	if n := w.count(EventTxAlarm); n != 0 {
		t.Errorf("drive collapse published %d tx-alarm events; want 0 — the stuck-TX alarm is a "+
			"different fault with a different clear path", n)
	}
}

// D8 — silence while RECEIVING is normal (a rig with no signal pushes nothing
// once the S-meter settles). The detector is gated on an active transmission,
// so a quiet receive period must never alarm.
func TestDriveAlarm_SilenceOutsideATransmission_DoesNotAlarm(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, _ := newCommandTestService(t)
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	time.Sleep(4 * driveSilenceTimeout) // never keyed

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times without a transmission; want 0", n)
	}
}

// D9 — one alarm per transmission. A detector that re-checks on a ticker would
// publish every threshold for the rest of the slot, so a 12.6 s slot with a 3 s
// threshold would raise four banners for one fault.
func TestDriveAlarm_FiresOncePerTransmission(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm to test against")
	time.Sleep(4 * driveSilenceTimeout)

	if n := w.count(EventDriveAlarm); n != 1 {
		t.Errorf("drive alarm published %d times for one transmission; want exactly 1", n)
	}
}

// D10 — and it re-arms. Having alarmed once, the NEXT transmission must be able
// to alarm too; a one-shot latch would report the first collapse and go quiet
// for the rest of the session.
func TestDriveAlarm_RearmsForTheNextTransmission(t *testing.T) {
	driveAlarmTestSilence(t, 100*time.Millisecond)
	s, fake := newCommandTestService(t)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) == 1 }, "first transmission did not alarm")
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	rxMeterFlowing(t, s) // instrument still good between slots
	keyedTestSlot(t, s)

	waitFor(t, func() bool { return w.count(EventDriveAlarm) == 2 },
		"second transmission with a dead drive did not alarm; the detector did not re-arm")
}
