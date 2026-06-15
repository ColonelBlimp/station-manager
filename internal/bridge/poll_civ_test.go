package bridge

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// Icom state-mirror poll loop (ADR 0035). The poll fires the rigdef's POLL
// read-list (VFO-B `25 01`, mode+data `26 00`, split `0F`) on a low-rate ticker,
// backing off while the rig is mid Transceive broadcast (a dial-turn storm).
// civFreqBroadcast / waitForIdentity live in command_civ_test.go (same package).

var pollVFOBFrame = []byte{0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x01} // 25 01 (FD stripped)

func countFrame(writes [][]byte, frame []byte) int {
	n := 0
	for _, w := range writes {
		if bytes.Equal(w, frame) {
			n++
		}
	}
	return n
}

// TestPollLoop_FiresStateMirrorReads: with the collision back-off disabled, the
// poll loop keeps firing the POLL read-list, so `25 01` is written more than the
// single time the connect READ snapshot sends it.
func TestPollLoop_FiresStateMirrorReads(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	s.civPollInterval = 20 * time.Millisecond
	s.civPollQuiet = 0 // never back off — poll every tick
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine(civFreqBroadcast)
	waitForIdentity(t, s)

	// The connect READ writes 25 01 once; the poll loop adds more.
	deadline := time.Now().Add(time.Second)
	for {
		if countFrame(fake.recordedWrites(), pollVFOBFrame) >= 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("poll loop did not fire repeated 25 01 reads; writes=% X", fake.recordedWrites())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestPollLoop_BacksOffDuringBroadcastStorm: a recent Transceive broadcast (the
// bus busy with a dial-turn storm) suppresses the poll — no new `25 01` reads
// while a broadcast is within the quiet window. Freq is pushed in real-time
// during a spin anyway, so deferring the gap-reads costs nothing.
func TestPollLoop_BacksOffDuringBroadcastStorm(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	s.civPollInterval = 20 * time.Millisecond
	s.civPollQuiet = time.Hour // any recent broadcast → always back off
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()

	fake.feedLine(civFreqBroadcast) // sets lastBroadcastAt; confirms identity
	waitForIdentity(t, s)           // ensures the broadcast was processed

	base := countFrame(fake.recordedWrites(), pollVFOBFrame) // == 1 (connect READ)
	time.Sleep(150 * time.Millisecond)                       // several poll ticks elapse
	if after := countFrame(fake.recordedWrites(), pollVFOBFrame); after != base {
		t.Errorf("poll wrote %d extra 25 01 reads during back-off; want 0", after-base)
	}
}
