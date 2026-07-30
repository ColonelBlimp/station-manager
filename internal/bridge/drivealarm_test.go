package bridge

import (
	"bytes"
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
// ON-HARDWARE ACCEPTANCE (layer 3) PASSED 2026-07-30, on the air at operating
// power during a live CQ run — both cases inside the slot, no healthy slot
// firing. Citations are smd.log timestamps:
//
//	collapse mid-slot     keyed 04:57:15, alarm 04:57:24 (+9 s = 3 s after the
//	                      last frame), n=129 against a clean 327-482
//	silent from key-down  keyed 05:01:15, alarm 05:01:18 (+3 s exactly), n=35
//
// OPEN QUESTION, NARROWED by that run and still not answered: frames that flow
// but all read zero. This header used to say the hardware does not produce them.
// It does — the 05:01:15 slot pushed ~26-30 zero-valued frames at roughly 2-3 Hz
// while no RF was leaving the rig, and the alarm fired only because a complete
// gap preceded them (see drivealarm.go, which now carries the arithmetic). What
// remains unobserved is frames arriving CONTINUOUSLY at zero, with no gap wide
// enough to trip the timeout; that case would not alarm today.
//
// D5 therefore pins something weaker than it reads: not "frames flowing means no
// alarm, whatever their value" but "a stream with no 3 s gap means no alarm".
// Closing the gap between those two needs a definition of "zero" from the
// operator — a rule to add, never a threshold to invent.
//
// KNOWN GAP IN THIS CRITERION, found by the operator at ~05:02 the same day: the
// banner carries no time anchor, so an alarm three minutes and four healthy
// transmissions old is indistinguishable from one firing right now. The
// nearest-confusable-state clause was written about no-RF versus dead instrument
// and never asked about fresh versus stale. Not a code defect — the alarm's
// refusal to auto-clear is deliberate — but a rule this file does not yet state.
// Draft criterion and the open judgement calls: docs/dogfood-inbox.md 2026-07-30.

// shortDriveSilence shortens THIS service's silence threshold so a test does not
// sit out the real 3 s, and returns it for the test's own waits.
//
// Per-service, not a package var: these tests deliberately leave a transmission
// running at test end, so a still-pending re-arming timer would read a global
// while the cleanup restored it — a real data race, caught by -race on the first
// run. Must be called before the transmission is keyed; armDriveWatch reads it.
func shortDriveSilence(s *Service) time.Duration {
	s.driveSilence = 100 * time.Millisecond
	return s.driveSilence
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
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
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
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
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
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	// Deliberately NO receive-time meter frames: the instrument never spoke.
	keyedTestSlot(t, s)
	time.Sleep(4 * silence)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times with a dead instrument; want 0 — "+
			"absence of meter data is only evidence of no drive when the instrument is known good", n)
	}
}

// D4 — healthy transmission. Frames flow throughout; nothing is wrong.
func TestDriveAlarm_HealthyDrive_DoesNotAlarm(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0034000", 3*silence)

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
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0005000", 3*silence) // low but flowing

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
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
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
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
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
	s, _ := newCommandTestService(t)
	silence := shortDriveSilence(s)
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	time.Sleep(4 * silence) // never keyed

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times without a transmission; want 0", n)
	}
}

// D9 — one alarm per transmission. A detector that re-checks on a ticker would
// publish every threshold for the rest of the slot, so a 12.6 s slot with a 3 s
// threshold would raise four banners for one fault.
func TestDriveAlarm_FiresOncePerTransmission(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm to test against")
	time.Sleep(4 * silence)

	if n := w.count(EventDriveAlarm); n != 1 {
		t.Errorf("drive alarm published %d times for one transmission; want exactly 1", n)
	}
}

// D10 — and it re-arms. Having alarmed once, the NEXT transmission must be able
// to alarm too; a one-shot latch would report the first collapse and go quiet
// for the rest of the session.
func TestDriveAlarm_RearmsForTheNextTransmission(t *testing.T) {
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
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

// D11 — a key write that BLOCKS must not burn the silence window. The rig is
// not transmitting until the key command has actually left the host, and the
// write can legally take longer than the whole window: write_watchdog_ms is
// resolved by resolveTimeout with no ceiling (service.go), and CI-V additionally
// waits on cmdMu and an ACK. Arming at the moment ft8TxActive is set would then
// alarm "no RF" about a transmission that had not begun — and it would do so
// precisely when a write is already in trouble, pointing the operator at the
// audio path when the fault is the serial one.
func TestDriveAlarm_BlockedKeyWrite_DoesNotAlarmBeforeTheRigIsKeyed(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	// The key write stalls for longer than the entire silence window.
	fake.onWrite = func(b []byte) []byte {
		if bytes.Contains(b, []byte("TX1;")) {
			time.Sleep(4 * silence)
		}
		return nil
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = s.KeyFt8Tx(context.Background(), "")
	}()

	// Sample while the write is still stalled: the rig is not keyed yet.
	time.Sleep(2 * silence)
	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times while the key write was still blocked; want 0 — "+
			"nothing is transmitting until the key command has gone out", n)
	}
	<-done
}

// drivePayloads returns every drive-alarm payload published, in order, so a rule
// can assert on the ALARM/RECOVERY sequence rather than a bare count.
func (w *eventWatch) drivePayloads() []DriveAlarmPayload {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]DriveAlarmPayload, 0, 2)
	for _, e := range w.seen {
		if e.Name != EventDriveAlarm {
			continue
		}
		if p, ok := e.Payload.(DriveAlarmPayload); ok {
			out = append(out, p)
		}
	}
	return out
}

// healthySlot runs one transmission with the meter stream flowing throughout —
// armed, and silent for no part of it, so it is positive evidence that output was
// normal.
func healthySlot(t *testing.T, s *Service, silence time.Duration) {
	t.Helper()
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 2*silence)
	s.finishFt8Tx()
}

// RECOVERY REPORTING (operator's calls, 2026-07-30): absolute time in the banner,
// and "output has been normal since" after ONE healthy transmission.
//
// The daemon owns the recovery signal because only it can tell a healthy FT8
// transmission from a tune carrier or a dropped frame; the SPA seeing tx-status go
// 1→0 cannot. It rides the EXISTING drive-alarm event as Active=false, which is
// what that field was reserved for.
//
// "HEALTHY" MEANS ARMED AND SILENT, not merely un-alarmed. A transmission where the
// watch never armed — no instrument-alive evidence — says NOTHING about output, and
// reporting recovery from it would claim a measurement that was never made. That is
// the fault DriveAlarmBanner's S8 exists to prevent, applied to the recovery half.

// E1 — a healthy transmission after an alarm reports recovery.
func TestDriveAlarm_HealthySlotAfterAlarmPublishesRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	// A transmission that alarms: instrument known good, then no frames at all.
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised; the fixture proves nothing")
	s.finishFt8Tx()

	healthySlot(t, s, silence)

	waitFor(t, func() bool { return len(w.drivePayloads()) >= 2 },
		"no recovery published after a healthy transmission following an alarm")
	got := w.drivePayloads()
	if got[0].Active != true {
		t.Errorf("first payload = %+v, want the alarm (Active true)", got[0])
	}
	if got[len(got)-1].Active != false {
		t.Errorf("last payload = %+v, want recovery (Active false)", got[len(got)-1])
	}
}

// E2 — no alarm has fired, so a healthy transmission reports NOTHING. Recovery is
// only meaningful against a standing alarm; publishing it every slot would put an
// event on every subscriber's stream every 15 s to say nothing happened.
func TestDriveAlarm_HealthySlotWithNoAlarmPublishesNothing(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	healthySlot(t, s, silence)
	healthySlot(t, s, silence)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("published %d drive-alarm events with no alarm ever raised; want 0", n)
	}
}

// E3 — ONE recovery per alarm. The second healthy transmission adds nothing: the
// operator has already been told, and a per-slot repeat is the same noise E2 avoids.
func TestDriveAlarm_RecoveryPublishedOncePerAlarm(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()

	healthySlot(t, s, silence)
	waitFor(t, func() bool { return len(w.drivePayloads()) >= 2 }, "no recovery published")
	healthySlot(t, s, silence)
	healthySlot(t, s, silence)

	recoveries := 0
	for _, p := range w.drivePayloads() {
		if !p.Active {
			recoveries++
		}
	}
	if recoveries != 1 {
		t.Errorf("got %d recovery events across three healthy transmissions; want exactly 1", recoveries)
	}
}

// E4 — a transmission that ALARMS is not a recovery, however it ends. Without this
// an implementation keying on "the transmission finished" would report output
// normal for the very slot that just failed.
func TestDriveAlarm_AlarmingSlotIsNotRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	for i := 0; i < 2; i++ {
		rxMeterFlowing(t, s)
		keyedTestSlot(t, s)
		waitFor(t, func() bool { return w.count(EventDriveAlarm) > i }, "no drive alarm raised")
		s.finishFt8Tx()
	}

	// Settle before asserting ABSENCE (the D8 pattern): publishing happens inside
	// finishFt8Tx but the watcher records off a channel, so an immediate read can
	// pass while the event it should have caught is still in flight.
	time.Sleep(4 * silence)

	for _, p := range w.drivePayloads() {
		if !p.Active {
			t.Fatalf("recovery published for a transmission that alarmed: %+v", w.drivePayloads())
		}
	}
}

// E5 — THE NEAREST CONFUSABLE STATE for the recovery half. A transmission where the
// watch never armed (nothing proved the instrument alive) is silent about output,
// not evidence of health, so it must not clear a standing alarm.
func TestDriveAlarm_UnarmedSlotIsNotRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()

	// No rxMeterFlowing: the watch cannot arm, so this slot measures nothing.
	keyedTestSlot(t, s)
	time.Sleep(2 * silence)
	s.finishFt8Tx()

	// Settle before asserting ABSENCE (the D8 pattern). Without this the rule
	// passed against an implementation that ignored driveWatchArmed entirely — the
	// event was published, just not yet recorded when the assertion ran.
	time.Sleep(4 * silence)

	for _, p := range w.drivePayloads() {
		if !p.Active {
			t.Errorf("recovery published from a transmission whose watch never armed: %+v", w.drivePayloads())
		}
	}
}

// E6 — recovery must not touch TX-safety state, exactly as the alarm must not
// (ADR 0051). It is a report about drive, not about the carrier.
func TestDriveAlarm_RecoveryLeavesTxSafetyStateAlone(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()
	healthySlot(t, s, silence)
	waitFor(t, func() bool { return len(w.drivePayloads()) >= 2 }, "no recovery published")

	s.mu.Lock()
	uncertain, alarmed := s.txUncertain, s.txAlarmActive
	s.mu.Unlock()
	if uncertain || alarmed {
		t.Errorf("txUncertain=%v txAlarmActive=%v after a drive recovery; want both clear", uncertain, alarmed)
	}
	if n := w.count(EventTxAlarm); n != 0 {
		t.Errorf("recovery published %d tx-alarm events; want 0", n)
	}
}

// E7 — a transmission the detector could not JUDGE is not evidence of health.
// Arming proves only that a meter frame arrived while RECEIVING; it says nothing
// about the keyed window. A transmission ended before the silence threshold could
// elapse, with no keyed-time frames at all, therefore never alarms — and reporting
// recovery from it would tell the operator "the meter reported output on a later
// transmission" when the meter reported nothing whatever. That is the same claim
// the banner's S8 rule forbids, in the recovery half.
func TestDriveAlarm_TransmissionTooShortToJudgeIsNotRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()

	// Armed (receive-time frames flowed), but unkeyed at once: no keyed-time frame
	// arrived and the window is far shorter than the silence threshold.
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	time.Sleep(4 * silence) // settle before asserting absence (the D8 pattern)

	for _, p := range w.drivePayloads() {
		if !p.Active {
			t.Errorf("recovery published from a transmission too short to judge, with no keyed-time meter frame: %+v",
				w.drivePayloads())
		}
	}
}

// E8 — an owed recovery SURVIVES a pipeline teardown. The banner does: the SPA's
// onRigDisconnected only moves rig.cat to 'lost', and nothing clears the drive
// alarm (resetCatLink is a test seam with no production caller — a comment here
// once claimed otherwise). So a transient rig disconnect between the alarm and the
// next healthy transmission must not leave the operator with a banner that can
// never be answered.
func TestDriveAlarm_OwedRecoverySurvivesPipelineTeardown(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()

	s.resetMeterObservation() // the rig dropped and the supervisor reconnected

	healthySlot(t, s, silence)

	waitFor(t, func() bool { return len(w.drivePayloads()) >= 2 },
		"no recovery after a healthy transmission following a reconnect; the owed report was forgotten at teardown")
}

// setDriveSilence changes the threshold on a live service under the lock, so a
// test can neutralise ONE conjunct of the recovery decision and isolate another.
// Unlike shortDriveSilence this is safe mid-transmission: armDriveWatch,
// checkDriveSilence and takeDriveRecoveryLocked all read the field under s.mu.
func setDriveSilence(s *Service, d time.Duration) {
	s.mu.Lock()
	s.driveSilence = d
	s.mu.Unlock()
}

// E9 — a transmission whose MEASURED widest silence reached the threshold is not
// recovery, even when the alarm timer never got to fire. The two are different
// things: checkDriveSilence takes s.mu on entry, so if finishFt8Tx wins that lock
// the callback finds the transmission already over and returns without alarming.
// Resting recovery on "the timer did not fire" therefore reports output normal for
// a slot that contained exactly the silence being hunted. The frozen gap
// measurement is the evidence, and it is not subject to that race.
//
// The threshold is moved rather than the race being run, so the rule is pinned
// deterministically: a large silence while keyed means no timer fires, and lowering
// it before the unkey makes the already-measured gap exceed it.
func TestDriveAlarm_MeasuredSilenceAtThresholdIsNotRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 }, "no drive alarm raised")
	s.finishFt8Tx()

	// A threshold too long to fire, so this slot cannot alarm however silent it is.
	setDriveSilence(s, 30*time.Second)
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	s.observeMeter(meterFrame(t, "RM0118000")) // one keyed-time frame...
	time.Sleep(4 * silence)                    // ...then silence for the rest of it

	// Now the measured gap exceeds the threshold, while the window is still long
	// enough and no alarm ever fired: only the gap measurement can refuse this.
	setDriveSilence(s, 2*silence)
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}
	time.Sleep(4 * silence) // settle before asserting absence

	for _, p := range w.drivePayloads() {
		if !p.Active {
			t.Errorf("recovery published for a transmission whose measured silence reached the threshold: %+v",
				w.drivePayloads())
		}
	}
}
