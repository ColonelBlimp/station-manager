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

// D19 — THE METER SELECTOR. Silence is only evidence about RF while the rig's
// meter is on PO.
//
// The rig pushes RM0 = "the value of whatever meter is CURRENTLY SELECTED"
// (meters.go, measured on hardware 2026-07-29), and the rigdef's READ burst
// (`ID;FA;FB;ST;VS;MD0;MD1;PC;MS;`) never queries RM4/RM5/RM6 — so that ONE
// selection is the entire meter stream. Select ALC and a correctly-driven FT8
// signal reads near zero; the rig pushes on CHANGE, so a meter sitting at zero
// has almost nothing to say and the stream goes quiet while RF leaves the radio
// perfectly normally.
//
// FOUND ON THE AIR 2026-07-31, two false alarms at 03:58:36 and 03:59:06 while
// the operator had moved the rig's meter to ALC to set audio drive — which is
// the meter you watch to do that, so this is the normal way to meet the bug.
// smd.log carries the selection in the summary line itself (meterFieldPrefix
// names the field after it), and the transition is unambiguous:
//
//	03:58:13  meters=meter_po   n=532  gap_max_ms=239
//	03:58:43  meters=meter_alc  n=8    gap_max_ms=9764
//
// RF was leaving the rig throughout — that is how the operator noticed.
//
// This is the SECOND instance of the rule armDriveWatch already implements for
// meterSeenSinceTx: DO NOT ARM WHERE SILENCE IS UNINFORMATIVE. The service
// already carried the fact needed to know it (Service.meterSel, exposed by
// meterSelection()); the detector simply never read it — the same shape as the
// meterGapAtUnkey finding of 2026-07-30, where the answer was already in hand
// and discarded.
//
// SCOPE, chosen deliberately and reversible: the gate blocks only a selection
// that is KNOWN AND NOT PO. An unknown selection (empty meterSel — no MS frame
// seen yet, or a rig whose rigdef does not report one) still arms, exactly as
// today. That keeps this fix to the case measured as wrong rather than silently
// disabling drive detection on any rig that never answers MS. The residual is
// stated rather than hidden: such a rig gets no protection from this class of
// false alarm.
//
// The table is the point — the ONLY difference between the two cases is the MS
// frame, so the PO row is the discriminator that stops this passing against an
// implementation that simply never arms.
//
// wantReported is the second half of the rule, and the reason it lives in THIS
// table rather than its own test: the operator must be told when monitoring is
// off (operator's instruction, 2026-07-31), and a banner that says "monitoring
// on" while armDriveWatch has silently declined to arm is worse than no banner
// at all. Asserting the reported code and the alarm behaviour against the SAME
// fixture is what pins them together; driveMonitorFor is the one rule both read.
func TestDriveAlarm_MeterSelectorGatesTheWatch(t *testing.T) {
	for _, tc := range []struct {
		name         string
		msFrame      string // MS0 = PO selected, MS2 = ALC selected (rigdef METERSEL map)
		wantAlarm    bool
		wantReported string
	}{
		{"PO selected — silence means no RF", "MS0", true, DriveMonitorOK},
		{"ALC selected — silence means nothing about RF", "MS2", false, DriveMonitorMeterNotPO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, fake := newCommandTestService(t)
			silence := shortDriveSilence(s)
			t.Cleanup(answerTxStatusQueries(s, fake))
			w := watchEvents(t, s)

			// What the operator is told, from the same frame the detector reads.
			p, populated := mapStatusToPayload(meterFrame(t, tc.msFrame))
			if !populated {
				t.Fatal("a meter-selection frame must publish rig state on its own — " +
					"turning the rig's meter knob sends no other tag, so a frame dropped " +
					"here leaves the banner stale until something unrelated changes")
			}
			if p.DriveMonitor != tc.wantReported {
				t.Errorf("rig-state reported driveMonitor=%q, want %q", p.DriveMonitor, tc.wantReported)
			}

			s.observeMeter(meterFrame(t, tc.msFrame))
			rxMeterFlowing(t, s) // instrument demonstrably alive in receive
			keyedTestSlot(t, s)  // ... then total silence for the whole slot

			if tc.wantAlarm {
				waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
					"no drive alarm with the meter on PO and a silent stream — "+
						"the selector gate must not disable the detector outright")
				return
			}
			time.Sleep(4 * silence)
			if n := w.count(EventDriveAlarm); n != 0 {
				t.Errorf("drive alarm published %d times with the rig's meter on ALC; want 0 — "+
					"a near-zero ALC reading is what correctly-set FT8 drive looks like, so a "+
					"quiet stream says nothing about whether RF is leaving the rig", n)
			}
		})
	}
}

// D21 — THE SELECTOR CAN MOVE MID-TRANSMISSION, and D19 does not cover it: D19
// fixes the selection before key-down, so it only ever exercises the arm-time
// gate. A transmission that ARMS on PO and then has the meter switched to ALC
// while keyed keeps a live silence timer over a stream that has just stopped
// meaning anything — and raises exactly the false NO RF OUTPUT alarm this whole
// change exists to prevent (codex a0b0ac45 P1).
//
// REACHABLE ON THIS HARDWARE, not a thought experiment. smd.log 2026-07-31
// 04:04:13 reports `meters=meter_alc,meter_po` — BOTH accumulator buckets inside
// one transmission, which is the rig's own record of the selection changing while
// keyed. (Honest scope: that is proof the STATE occurs, not proof it caused
// tonight's alarms. The 03:58:36 alarm reported only meter_alc with n=8, so that
// transmission had ALC selected from the start and is D19's case, not this one.)
//
// The fixture must switch the meter AFTER arming, or it degenerates into D19 and
// passes against the already-shipped arm-time gate while proving nothing.
func TestDriveAlarm_SelectorSwitchedMidTransmission_DoesNotAlarm(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	s.observeMeter(meterFrame(t, "MS0")) // PO at key-down: the watch arms
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)

	// ... and now the operator turns the meter to ALC to look at drive, which is
	// precisely what they were doing when this was found.
	s.observeMeter(meterFrame(t, "MS2"))
	time.Sleep(4 * silence) // the ALC stream says nothing, as it should

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times after the meter moved to ALC mid-transmission; "+
			"want 0 — the armed timer must stop treating silence as evidence the moment "+
			"the stream stops being about RF", n)
	}
}

// D22 — the same transmission is not evidence of HEALTH either. Recovery is a
// positive claim ("output was confirmed normal on a later transmission"), and a
// transmission whose meter stream stopped being about RF part-way through
// confirms nothing. Without this rule the fix for D21 buys silence on the alarm
// and pays for it by retiring a standing alarm on no evidence — the more
// dangerous direction, because the operator is told the fault is gone.
func TestDriveAlarm_SelectorSwitchedMidTransmission_IsNotRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	// A genuine alarm first, so there is something a false recovery could retire.
	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"no drive alarm raised; the fixture proves nothing")
	s.finishFt8Tx()

	// A transmission that arms on PO, is switched to ALC, and then runs quietly to
	// the end — no alarm, but nothing verified either.
	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	s.observeMeter(meterFrame(t, "MS2"))
	feedMeterFor(t, s, "RM0034000", 2*silence)
	s.finishFt8Tx()
	time.Sleep(2 * silence)

	for _, p := range w.drivePayloads() {
		if !p.Active {
			t.Errorf("recovery published from a transmission whose meter was switched away "+
				"from PO while keyed: %+v — that transmission confirmed nothing", w.drivePayloads())
		}
	}
}

// D23 — THE KEYED WINDOW ENDS AT THE SEAL, NOT WHEN ft8TxActive CLEARS. The two
// are seconds apart and the difference is load-bearing (codex 71bbf123 P1).
//
// releaseFt8TxChecked is a SEQUENCE, not an instant: seal -> issue tx_off (CI-V
// waits for the ACK) -> confirm idle -> settle -> restore mode -> finishFt8Tx.
// ft8TxActive stays true for that whole tail, with PTT already down. D21's taint
// gated on ft8TxActive alone, so an MS frame arriving anywhere in the tail marked
// a transmission tainted whose meter never left PO while it was actually keyed —
// and a tainted transmission publishes no recovery, so a standing alarm would go
// unretired on evidence that was in fact good.
//
// The repo has already paid for this lesson on this exact function: "the window
// ends at unkey" took five review rounds and four real defects, every one from
// treating that sequence as a single moment. The right boundary is the one the
// gap measurement already uses — meterGapSealed, frozen at the instant tx_off is
// ISSUED and unsealed again if the write fails, because the transmission is then
// still running.
//
// THE FIXTURE MUST USE THE REAL RELEASE PATH. D21/D22 call finishFt8Tx directly,
// which skips the tail entirely, so neither can see this — the onWrite hook below
// injects the selection change between the seal and finishFt8Tx, which is the
// only place the bug lives.
func TestDriveAlarm_SelectorChangedAfterUnkey_StillReportsRecovery(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	// A genuine alarm, so there is a standing alarm a recovery must retire.
	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"no drive alarm raised; the fixture proves nothing")
	s.finishFt8Tx()

	// A transmission that is healthy on PO for its whole KEYED length.
	s.observeMeter(meterFrame(t, "MS0"))
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM0118000", 2*silence)

	// The operator turns the meter to ALC during the post-unkey tail — after
	// tx_off is issued (so the keyed window is sealed) but before finishFt8Tx.
	fake.onWrite = func(b []byte) []byte {
		if string(b) == "TX0;" {
			s.observeMeter(meterFrame(t, "MS2"))
		}
		return nil
	}
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}

	waitFor(t, func() bool { return len(w.drivePayloads()) >= 2 },
		"no recovery after a transmission that was on PO for its whole keyed length — "+
			"a meter change made AFTER the unkey must not retire its verdict")
	got := w.drivePayloads()
	if got[len(got)-1].Active != false {
		t.Errorf("last payload = %+v, want recovery (Active false)", got)
	}
}

// D24 — A FAILED tx_off PUTS THE TAINT QUESTION BACK (codex 287825b6 P1).
//
// D23 stopped meter changes in the post-unkey tail from tainting a transmission.
// But the seal happens BEFORE the tx_off write is issued, and that write can
// FAIL — the watchdog bounds it at ~2 s, and releaseFt8TxChecked then UNSEALS,
// because the rig may still be keyed and the backstop will retry. A selection
// change discarded during that pending write was simply lost: the rig can sit on
// ALC without sending another MS frame (MS is pushed on change, not polled), so
// the still-live drive timer would read the quiet ALC stream as no RF and raise
// exactly the false alarm this arc exists to remove.
//
// This is a rollback state, which the previous round's rule created and did not
// answer: an instant chosen inside a sequence has to say what happens when a
// LATER step fails.
//
// RE-DERIVED, NOT REPLAYED. The fix asks "what is the meter on NOW?" at unseal
// rather than remembering the discarded frame, and the difference is real: a
// switch to ALC and back to PO inside the sealed window would leave a remembered
// taint wrong, while the current selection is right by construction. observeMeter
// records meterSel unconditionally — only the taint decision was ever gated — so
// the answer is always available there.
//
// THE WINDOW IS CONSTRUCTED, NOT RACED, following the precedent set by
// TestKeyFt8Tx_StartsWithEmptyMeterAccumulator: the fake's onWrite hook fires
// only on writes that SUCCEED, so a failing write cannot be hit deterministically
// from outside. seal/unseal are the real functions the real path calls, in the
// real order; the assertion stays on the observable (no alarm published).
func TestDriveAlarm_MeterMovedWhileUnkeyWritePending_TaintsOnRollback(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	s.observeMeter(meterFrame(t, "MS0")) // PO at key-down: the watch arms clean
	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)

	// The unkey attempt: the keyed window seals at the instant tx_off is issued.
	s.mu.Lock()
	s.sealMeterGapWindow(time.Now())
	s.mu.Unlock()

	// The operator turns the meter to ALC while that write is still pending.
	s.observeMeter(meterFrame(t, "MS2"))

	// The write FAILS, so the transmission is not over and the window reopens.
	s.mu.Lock()
	s.unsealMeterGapWindow()
	s.mu.Unlock()

	// The rig sends no further MS frame — it is simply sitting on ALC — and the
	// stream is quiet, as a near-zero ALC reading always is.
	time.Sleep(4 * silence)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Errorf("drive alarm published %d times after a FAILED unkey left the rig keyed "+
			"with its meter on ALC; want 0 — the selection change arrived while the "+
			"write was pending and must not be lost when the window reopens", n)
	}
}

// D20 — an UNKNOWN selection reports nothing rather than claiming either state.
// Empty is how every other RigStatePayload field says "this frame carried no
// value"; the SPA keeps its last. Reporting DriveMonitorOK here would be a claim
// the daemon cannot support, and reporting meter_not_po would put a banner up on
// every rig whose rigdef has no MS tag — while armDriveWatch still arms for them.
func TestDriveMonitor_UnknownSelectionReportsNothing(t *testing.T) {
	if got := driveMonitorFor(""); got != "" {
		t.Errorf("driveMonitorFor(unknown) = %q, want empty", got)
	}
	// A frame with no meter selection must not carry a stale verdict either.
	p, _ := mapStatusToPayload(meterFrame(t, "FA014074000"))
	if p.DriveMonitor != "" {
		t.Errorf("driveMonitor=%q on a frame with no meter selection; want empty", p.DriveMonitor)
	}
}

// ---------------------------------------------------------------------------
// The poll witness (dogfood 2026-08-08 12:29, EVERY transmission alarming).
// The pushed RM0 stream collapsed to 6-8 frames/13 s while the ADR 0064 poll
// answered 53x/slot with PO steady 104-108 — full output, measured, the whole
// slot — and the alarm cried "no RF output" anyway, because its only witness
// was the pushed stream. The rig pushes on CHANGE, and a dead-flat PO
// envelope (po_min = po_max = 105) pushes almost nothing, so pushed silence
// stopped being sufficient evidence the moment the poll existed.
//
// DP1 — pushes silent + polls measuring POSITIVE output: NO alarm. The
//       operator's exact session, and the confusable it guards is the alarm
//       state itself (a false "no RF" on every slot trains the operator to
//       ignore the banner that exists for a real collapse).
// DP2 — pushes silent + polls answering ZERO: the alarm FIRES. A genuinely
//       dead drive still polls — at zero. "Positive" is the alarm's own
//       claim tested against a measurement, not an invented threshold.
// DP3 — a positive poll OLDER than the silence window does not suppress:
//       freshness is bounded by the same driveSilence window, no new number.
// ---------------------------------------------------------------------------

func TestDriveAlarm_PollsMeasurePositiveOutput_DoesNotAlarm(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	// The pushed stream is SILENT for the whole slot; the poll answers keep
	// measuring full output (RM5 -> tag PO, value 105).
	feedMeterFor(t, s, "RM5105", 4*silence)

	if n := w.count(EventDriveAlarm); n != 0 {
		t.Fatalf("drive alarm fired %d time(s) while the poll measured positive output", n)
	}
}

func TestDriveAlarm_PollsAnswerZero_StillAlarms(t *testing.T) {
	s, fake := newCommandTestService(t)
	silence := shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	keyedTestSlot(t, s)
	feedMeterFor(t, s, "RM5000", 4*silence)

	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"pushes silent and polls at ZERO is the real collapse; the alarm must fire")
}

func TestDriveAlarm_StalePositivePoll_StillAlarms(t *testing.T) {
	s, fake := newCommandTestService(t)
	shortDriveSilence(s)
	t.Cleanup(answerTxStatusQueries(s, fake))
	w := watchEvents(t, s)

	rxMeterFlowing(t, s)
	// One positive poll answer BEFORE the key — then nothing at all.
	s.observeMeter(meterFrame(t, "RM5105"))
	keyedTestSlot(t, s)

	waitFor(t, func() bool { return w.count(EventDriveAlarm) > 0 },
		"a positive poll older than the silence window is not evidence about THIS silence")
}
