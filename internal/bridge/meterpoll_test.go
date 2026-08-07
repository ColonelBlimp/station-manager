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
*/

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

var meterPollBurst = []byte("RM4;RM5;")

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
