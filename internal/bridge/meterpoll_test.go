package bridge

/*
   ADR 0064 — continuous ALC/PO meter polling while an FT8 capture session is
   live. Cadence 250 ms / answer-bound 100 ms, both operator-ratified
   2026-08-06; the rigdef's METERPOLL command (`RM4;RM5;` on the FTdx10) is the
   data-driven capability flag, so a rig without it never meter-polls.

   OPERATOR-OBSERVABLE CRITERIA (ADR 0064 §Acceptance, drafted for
   ratification): a live ALC reading through a transmission, distinguishable
   three ways — deflecting (drive hot) vs zero (drive right) vs NO DATA (poll
   answers lost); the per-transmission PO summary and drive watch byte-for-byte
   unchanged with polling on; unkey timing unchanged.

   The rules below pin the invariants the ADR names:

   P1 — polling exists ONLY while an FT8 capture session is live (invariant 5:
        the session lifecycle is the whole state machine). Confusable pair: the
        same pipeline with the gate off writes no meter poll at all.
   P2 — polling continues THROUGH a keyed transmission. This is the deliberate
        divergence from the ADR 0035 Icom snapshot loop, whose five-frame burst
        is hard-skipped while keyed (2026-07-18 finding 5): the meter poll is
        one two-frame write bounded by the answer timeout, and mid-TX readings
        are the feature (measured 2026-08-06: RM4/RM5 answer during TX).
   P3 — RM4/RM5 ANSWERS publish rig-meters events named by the query prefix
        (matched by prefix, never arrival order — invariant 3); the pushed RM0
        stream does NOT reach rig-meters (it doesn't say which meter it is).
   P4 — THE DRIVE WATCH IS UNTOUCHED (invariant 4): poll answers must not feed
        the pushed-stream liveness/gap machinery. The silence of the pushed
        RM0 stream while keyed with PO selected is the drive-collapse signal;
        4 Hz poll answers refreshing `driveLastMeterAt` / the gap clock would
        mask exactly the fault the detector exists to catch — and a polled PO
        of 000 is a VALUE, not the pushed stream going quiet.
   P5 — a rigdef with no METERPOLL command (FT-710 today) never meter-polls,
        whatever the gate says: the capability is per-rigdef data, which is the
        ADR's "second rig with a during-TX read restriction" trigger built in.

   P6–P8 pin the answer-loss notice's window (codex 2026-08-09 P3: staleness
   was wall time since the last answer, so dead time the loop never polled
   through — a broadcast-storm skip, or a pipeline reconnect with FT8 capture
   still live — aged the window, and the FIRST poll written afterwards warned
   before its answer could physically arrive). The ratified meaning: the
   notice fires when meterAnswerStaleAfter consecutive WRITTEN polls go
   unanswered — a poll that was never written cannot have lost an answer.

   P6 — the window counts written polls, not wall time: one written poll after
        an arbitrarily long answerless gap must not warn. Confusable state:
        P8's genuine loss, where the same count of polls WAS written.
   P7 — pipeline teardown starts a fresh window: polls the old pipeline wrote
        cannot be answered by the new one, so after resetMeterObservation the
        next written polls get the full window — and the window still closes
        (the full count of new unanswered polls warns; a reconnect must not
        become a place a real loss can hide).
   P8 — genuine loss: the notice fires when the count of written unanswered
        polls reaches meterAnswerStaleAfter, NOT before, once per episode; any
        answer re-arms it for the next episode.
   P9 — an ANSWERED poll is never counted unanswered, whatever the scheduler
        does (codex 2026-08-09 P2 on 2653e859): the answer rides the readLoop
        goroutine and can be processed before the poll's write call even
        returns on the poll-loop goroutine. If the count ran after the write,
        the answer's reset would land first and the increment would survive
        it — seven further unanswered polls would then fire the notice one
        poll early. The count must therefore precede the write.
*/

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

var meterPollBurst = []byte("RM4;RM5;")

// meterPollTick mirrors runMeterPollLoop's healthy write sequence for the
// window rules below: count the poll before its write, check the window
// after it. P9 covers the ordering itself against the real loop.
func meterPollTick(s *Service) {
	s.countMeterPollWritten()
	s.checkMeterPollLoss()
}

func countMeterPolls(writes [][]byte) int {
	n := 0
	for _, w := range writes {
		if bytes.Equal(w, meterPollBurst) {
			n++
		}
	}
	return n
}

// newFtdx10PipelineTestService is newPipelineTestService with the FTdx10
// rigdef (Kenwood-style ASCII CAT; declares METERPOLL).
func newFtdx10PipelineTestService(t *testing.T) (*Service, *fakeSerial) {
	t.Helper()
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fake := installFakeSerial(s)
	return s, fake
}

func waitForMeterPolls(t *testing.T, fake *fakeSerial, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if countMeterPolls(fake.recordedWrites()) >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("meter poll fired %d times, want >= %d; writes=%q",
		countMeterPolls(fake.recordedWrites()), want, fake.recordedWrites())
}

// P1 — POLLING LIVES AND DIES WITH THE FT8 CAPTURE SESSION. Gate off: zero
// RM4;RM5; writes however long the pipeline runs. Gate on: the burst repeats
// at the cadence.
func TestMeterPoll_RunsOnlyWhileFt8CaptureLive(t *testing.T) {
	s, fake := newFtdx10PipelineTestService(t)
	s.ft8MeterPollInterval = 20 * time.Millisecond
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	time.Sleep(150 * time.Millisecond) // several would-be ticks with the gate off
	if n := countMeterPolls(fake.recordedWrites()); n != 0 {
		t.Fatalf("meter poll fired %d times with no FT8 capture session; want 0", n)
	}

	s.SetFt8CaptureLive(true)
	waitForMeterPolls(t, fake, 2)

	s.SetFt8CaptureLive(false)
	base := countMeterPolls(fake.recordedWrites())
	time.Sleep(150 * time.Millisecond)
	// One in-flight tick may land after the gate drops; the loop must not keep
	// going. >1 extra means it ignored the gate.
	if n := countMeterPolls(fake.recordedWrites()); n > base+1 {
		t.Fatalf("meter poll kept firing after the capture session ended: %d extra", n-base)
	}
}

// P2 — POLLING CONTINUES WHILE KEYED: the divergence from the Icom snapshot
// loop's hard-skip, and the point of the feature (mid-TX ALC). The confusable
// implementation — reusing the existing loop's keyed-skip — freezes exactly
// when the reading matters.
func TestMeterPoll_ContinuesThroughKeyedTransmission(t *testing.T) {
	s, fake := newFtdx10PipelineTestService(t)
	s.ft8MeterPollInterval = 20 * time.Millisecond
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	s.SetFt8CaptureLive(true)
	setFt8Keyed(s, true)
	defer setFt8Keyed(s, false)

	waitForMeterPolls(t, fake, 3)
}

// P3 — ANSWERS PUBLISH BY QUERY PREFIX; THE PUSHED METER DOES NOT. RM4/RM5
// name their meter in the frame; RM0 is whatever the meter switch shows and
// must not masquerade as either.
func TestMeterPoll_AnswersPublishRigMetersPushedStreamDoesNot(t *testing.T) {
	s, _ := newCommandTestService(t)
	ch, unsub := s.hub.subscribe()
	defer unsub()

	s.publishMeterAnswers(meterFrame(t, "RM4026000"))
	s.publishMeterAnswers(meterFrame(t, "RM5029000"))
	s.publishMeterAnswers(meterFrame(t, "RM0033000"))

	var got []RigMetersPayload
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case evt := <-ch:
			if evt.Name != EventRigMeters {
				continue
			}
			p, ok := evt.Payload.(RigMetersPayload)
			if !ok {
				t.Fatalf("rig-meters payload type %T", evt.Payload)
			}
			got = append(got, p)
		case <-deadline:
			t.Fatalf("rig-meters events = %+v, want ALC 26 + PO 29", got)
		}
	}
	if got[0].Meter != "ALC" || got[0].Value != 26 {
		t.Errorf("first event = %+v, want ALC 26", got[0])
	}
	if got[1].Meter != "PO" || got[1].Value != 29 {
		t.Errorf("second event = %+v, want PO 29", got[1])
	}
	// The pushed RM0 frame must publish nothing: drain briefly and require
	// silence.
	select {
	case evt := <-ch:
		if evt.Name == EventRigMeters {
			t.Fatalf("pushed RM0 published a rig-meters event: %+v", evt.Payload)
		}
	case <-time.After(50 * time.Millisecond):
	}
}

// P4 — POLL ANSWERS DO NOT FEED THE PUSHED-STREAM LIVENESS: the drive-collapse
// detector's signal is the pushed RM0 stream going quiet while keyed with PO
// selected, and a 4 Hz answer stream refreshing the gap clock would mask it.
// The same frames land in the accumulator (that part is the ADR's intent);
// only the liveness/gap machinery must not see them.
func TestMeterPoll_AnswersDoNotFeedPushedStreamLiveness(t *testing.T) {
	s, _ := newCommandTestService(t)

	s.observeMeter(meterFrame(t, "RM4026000")) // ALC answer
	s.observeMeter(meterFrame(t, "RM5029000")) // PO answer
	s.mu.Lock()
	afterAnswers := s.driveLastMeterAt
	seenAfterAnswers := s.meterSeenSinceTx
	s.mu.Unlock()
	if !afterAnswers.IsZero() {
		t.Fatalf("a poll answer refreshed driveLastMeterAt — the silence detector is masked")
	}
	if seenAfterAnswers {
		t.Fatalf("a poll answer set meterSeenSinceTx — instrument-alive evidence must be pushed-stream only")
	}

	s.observeMeter(meterFrame(t, "RM0033000")) // the pushed meter
	s.mu.Lock()
	afterPush := s.driveLastMeterAt
	s.mu.Unlock()
	if afterPush.IsZero() {
		t.Fatalf("a pushed RM0 frame did not refresh driveLastMeterAt — liveness broke the other way")
	}
}

// P6 — THE LOSS WINDOW COUNTS WRITTEN POLLS, NOT WALL TIME. The interval is
// dialled to 1ns so ANY elapsed wall time dwarfs the old 8-interval clock:
// an implementation measuring wall-since-last-answer fires here on the first
// written poll; one measuring written polls cannot.
func TestMeterPoll_LossNoticeCountsWrittenPollsNotWallTime(t *testing.T) {
	s, _, buf := newDriveWatchService(t)
	s.ft8MeterPollInterval = time.Nanosecond

	s.publishMeterAnswers(meterFrame(t, "RM5029000")) // healthy answer, then quiet
	time.Sleep(2 * time.Millisecond)                  // ≫ 8 "intervals" of wall time, zero polls written
	meterPollTick(s)                                  // the first poll actually written since

	if n := len(matching(t, buf, "answers missing")); n != 0 {
		t.Fatalf("loss notice fired on the first written poll after an unpolled gap (%d records) — staleness must count written polls, not wall time", n)
	}
}

// P7 — PIPELINE TEARDOWN STARTS A FRESH WINDOW, AND THE FRESH WINDOW STILL
// CLOSES. Seven polls go unanswered, the pipeline tears down (reconnect with
// FT8 capture still live), and the new pipeline's first seven unanswered
// polls must stay quiet — then the eighth warns, so a reconnect is not a
// place a genuine loss can hide.
func TestMeterPoll_ReconnectStartsFreshLossWindow(t *testing.T) {
	s, _, buf := newDriveWatchService(t)
	s.ft8MeterPollInterval = time.Nanosecond

	s.publishMeterAnswers(meterFrame(t, "RM5029000"))
	for i := 0; i < meterAnswerStaleAfter-1; i++ {
		meterPollTick(s)
	}
	s.resetMeterObservation() // pipeline teardown
	time.Sleep(2 * time.Millisecond)

	for i := 0; i < meterAnswerStaleAfter-1; i++ {
		meterPollTick(s)
	}
	if n := len(matching(t, buf, "answers missing")); n != 0 {
		t.Fatalf("loss notice exists before the post-reconnect window closed (%d records)", n)
	}
	meterPollTick(s)
	if n := len(matching(t, buf, "answers missing")); n != 1 {
		t.Fatalf("full window of unanswered polls after reconnect produced %d notices; want exactly 1", n)
	}
}

// P8 — GENUINE LOSS: the notice fires when written unanswered polls reach
// meterAnswerStaleAfter, NOT before, once per episode; an answer re-arms it.
// This is P6's confusable state — same silence, but here the polls WERE
// written, so the notice is owed.
func TestMeterPoll_GenuineLossWarnsAtWindowOncePerEpisode(t *testing.T) {
	s, _, buf := newDriveWatchService(t)
	s.ft8MeterPollInterval = time.Nanosecond

	// A rig that never answers at all still trips the notice (the seed case).
	for i := 0; i < meterAnswerStaleAfter-1; i++ {
		meterPollTick(s)
	}
	if n := len(matching(t, buf, "answers missing")); n != 0 {
		t.Fatalf("notice fired after %d written polls; the window is %d", meterAnswerStaleAfter-1, meterAnswerStaleAfter)
	}
	meterPollTick(s)
	if n := len(matching(t, buf, "answers missing")); n != 1 {
		t.Fatalf("notice count after the window closed = %d; want 1", n)
	}
	meterPollTick(s) // still silent: one line per episode, not per cycle
	if n := len(matching(t, buf, "answers missing")); n != 1 {
		t.Fatalf("notice repeated inside one loss episode (%d records)", n)
	}

	s.publishMeterAnswers(meterFrame(t, "RM4026000")) // recovery re-arms
	for i := 0; i < meterAnswerStaleAfter; i++ {
		meterPollTick(s)
	}
	if n := len(matching(t, buf, "answers missing")); n != 2 {
		t.Fatalf("second loss episode produced %d total notices; want 2", n)
	}
}

// P9 — AN ANSWERED POLL IS NEVER COUNTED UNANSWERED. The fixture forces the
// extreme of the race: the FIRST poll's answer is processed synchronously
// INSIDE the fake's write hook — i.e. before the write call has even
// returned to the poll loop. Seven genuinely unanswered polls follow; no
// notice may exist at that point (defective accounting reaches the
// threshold here, one poll early). The NINTH write is held on a channel as
// the deterministic observation window — the loop is sequential, so poll
// 8's accounting is provably complete and poll 9's post-write check
// provably has not run. Released, the ninth poll is the eighth consecutive
// unanswered one and the notice is owed: the same test pins the confusable
// state (P8's genuine loss) on the other side of the window.
func TestMeterPoll_AnswerRacingItsOwnWriteIsNotCountedUnanswered(t *testing.T) {
	buf := &syncBuf{}
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, logging.NewForWriter(buf))
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	fake := installFakeSerial(s)
	s.ft8MeterPollInterval = 20 * time.Millisecond

	var polls atomic.Int32
	ninthStarted := make(chan struct{})
	release := make(chan struct{})
	fake.onWrite = func(w []byte) []byte {
		if !bytes.Equal(w, meterPollBurst) {
			return nil // INIT etc.
		}
		switch n := polls.Add(1); {
		case n == 1:
			// The answer beats the write's return: processed on this stack,
			// inside WriteCommandBytes. (Called directly rather than fed to
			// the read stream so no LATER decode can reset the count again.)
			s.publishMeterAnswers(meterFrame(t, "RM5029000"))
		case n == 9:
			close(ninthStarted)
			<-release
		}
		return nil
	}

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()
	defer func() { // release before Stop so the blocked write can finish
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	s.SetFt8CaptureLive(true)

	select {
	case <-ninthStarted:
	case <-time.After(5 * time.Second):
		t.Fatalf("ninth meter poll not reached; polls=%d", polls.Load())
	}
	// Polls so far: 1 answered + 7 unanswered, all fully accounted. The
	// blocked ninth's own check cannot have run.
	if n := len(matching(t, buf, "answers missing")); n != 0 {
		t.Fatalf("loss notice exists after only 7 unanswered polls — the answered poll was counted unanswered (%d records)", n)
	}

	close(release)
	// The released ninth poll is the 8th consecutive unanswered: notice owed.
	deadline := time.Now().Add(2 * time.Second)
	for len(matching(t, buf, "answers missing")) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("expected exactly 1 loss notice after the 8th unanswered poll; got %d", len(matching(t, buf, "answers missing")))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// P5 — NO METERPOLL COMMAND, NO METER POLL: the FT-710 rigdef declares none,
// so even with the gate on nothing is written. The capability is rigdef data.
func TestMeterPoll_RigdefWithoutCommandNeverPolls(t *testing.T) {
	s, fake := newPipelineTestService(t) // yaesu-ft710
	s.ft8MeterPollInterval = 20 * time.Millisecond
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	s.SetFt8CaptureLive(true)
	time.Sleep(150 * time.Millisecond)
	if n := countMeterPolls(fake.recordedWrites()); n != 0 {
		t.Fatalf("FT-710 wrote %d meter polls; the capability must be rigdef-gated", n)
	}
}
