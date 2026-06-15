package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"testing"
	"time"
)

// CI-V wait-for-ACK command path (ADR 0034). The IC-7300 confirms a set-command
// with a bare FB (OK) / FA (NG) ACK and never broadcasts the commanded change,
// so SendCommands writes each frame, waits for its ACK, and on FB synthesizes
// the state push from the commanded value. These tests drive that through the
// real readLoop: the fakeSerial's onWrite hook returns the ACK frame for each
// write, exactly as the rig would.

const (
	civAckOKFrame = "\xFE\xFE\xE0\x94\xFB" // FE FE E0 94 FB (FD stripped by framing)
	civAckNGFrame = "\xFE\xFE\xE0\x94\xFA"
)

// civFreqBroadcast is a Transceive freq push (cmd 00) for 14074000 Hz from the
// rig at 0x94 — used to confirm identity (an FB never confirms identity, since
// CIVAck intercepts it before the decode/identity path).
var civFreqBroadcast = []byte{0xFE, 0xFE, 0x00, 0x94, 0x00, 0x00, 0x40, 0x07, 0x14, 0x00}

// startedCIVService brings up an IC-7300 service over the fake serial with the
// pipeline running, optionally auto-ACKing every write with ackFrame (nil =
// silent rig), and identity confirmed via a freq broadcast. Returns a fresh
// subscriber channel for asserting synthesized state, plus cleanup.
func startedCIVService(t *testing.T, ackFrame []byte) (*Service, *fakeSerial, <-chan Event, func()) {
	t.Helper()
	s, fake := newCIVPipelineTestService(t)
	if ackFrame != nil {
		fake.onWrite = func(_ []byte) []byte { return append([]byte(nil), ackFrame...) }
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, unsub := s.Subscribe()
	fake.feedLine(civFreqBroadcast)
	waitForIdentity(t, s)
	cleanup := func() { unsub(); _ = s.Stop() }
	return s, fake, ch, cleanup
}

func waitForIdentity(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !s.identityOK() {
		if time.Now().After(deadline) {
			t.Fatal("CI-V identity not confirmed within 1s")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// waitForRigState reads events until one is a rig-state matching pred, or fails
// on timeout. Skips the identity broadcast's own rig-state and any others.
func waitForRigState(t *testing.T, ch <-chan Event, pred func(RigStatePayload) bool, within time.Duration) RigStatePayload {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case evt := <-ch:
			if evt.Name != EventRigState {
				continue
			}
			if p, ok := evt.Payload.(RigStatePayload); ok && pred(p) {
				return p
			}
		case <-deadline:
			t.Fatal("no matching rig-state event within timeout")
		}
	}
}

// assertNoRigState fails if a rig-state matching pred arrives within the window.
func assertNoRigState(t *testing.T, ch <-chan Event, pred func(RigStatePayload) bool, within time.Duration) {
	t.Helper()
	deadline := time.After(within)
	for {
		select {
		case evt := <-ch:
			if evt.Name != EventRigState {
				continue
			}
			if p, ok := evt.Payload.(RigStatePayload); ok && pred(p) {
				t.Fatalf("unexpected rig-state matched predicate: %+v", p)
			}
		case <-deadline:
			return
		}
	}
}

// TestSendCommandsCIV_AdoptsFreqOnAck: a set_freq that the rig ACKs (FB) returns
// nil and synthesizes a rig-state push carrying the commanded freq — the rig
// never broadcasts it, so the SPA only learns via this synthesized push.
func TestSendCommandsCIV_AdoptsFreqOnAck(t *testing.T) {
	s, _, ch, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	if err := s.SendCommand(context.Background(), "set_freq", "18100000"); err != nil {
		t.Fatalf("SendCommand(set_freq): %v", err)
	}
	got := waitForRigState(t, ch, func(p RigStatePayload) bool { return p.VfoA == 18100000 }, time.Second)
	if got.VfoA != 18100000 {
		t.Fatalf("synthesized VfoA = %d, want 18100000", got.VfoA)
	}
}

// TestSendCommandsCIV_AdoptsModeOnMultiFrameAck: set_mode "USB-D" encodes to TWO
// CI-V frames (06 then 1A06); each must be ACKed, then the commanded mode
// literal is synthesized — proving the per-frame wait loop and that adopt-on-ACK
// resolves USB-D precisely (a read-back could not).
func TestSendCommandsCIV_AdoptsModeOnMultiFrameAck(t *testing.T) {
	s, fake, ch, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	before := len(fake.recordedWrites())
	if err := s.SendCommand(context.Background(), "set_mode", "USB-D"); err != nil {
		t.Fatalf("SendCommand(set_mode): %v", err)
	}
	got := waitForRigState(t, ch, func(p RigStatePayload) bool { return p.Mode == "USB-D" }, time.Second)
	if got.Mode != "USB-D" {
		t.Fatalf("synthesized Mode = %q, want USB-D", got.Mode)
	}
	// Two frames written for the one op (base mode + data flag).
	if n := len(fake.recordedWrites()) - before; n != 2 {
		t.Fatalf("set_mode USB-D wrote %d frames, want 2", n)
	}
}

// TestSendCommandsCIV_ReadsBackAfterSwap: swap_vfo has no sets_state (the new
// operating freq+mode is "whatever was in the other VFO"), so once the rig ACKs
// the swap the bridge fires a READ-back snapshot — the IC-7300 never broadcasts
// a commanded change, so without this the display would lag to the liveness
// probe. Asserts the swap frame is immediately followed by the two READ frames.
func TestSendCommandsCIV_ReadsBackAfterSwap(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	before := len(fake.recordedWrites())
	if err := s.SendCommand(context.Background(), "swap_vfo", ""); err != nil {
		t.Fatalf("SendCommand(swap_vfo): %v", err)
	}
	got := fake.recordedWrites()[before:]
	want := [][]byte{
		{0xFE, 0xFE, 0x94, 0xE0, 0x07, 0xB0}, // the swap (07 B0)
		{0xFE, 0xFE, 0x94, 0xE0, 0x03},       // read-back: freq query
		{0xFE, 0xFE, 0x94, 0xE0, 0x04},       // read-back: mode query
	}
	if len(got) != len(want) {
		t.Fatalf("swap_vfo wrote %d frames (% X), want swap + 2 read-back", len(got), got)
	}
	for i := range want {
		if !bytes.Equal(got[i], want[i]) {
			t.Errorf("write[%d] = % X, want % X", i, got[i], want[i])
		}
	}
}

// TestSendCommandsCIV_RejectsOnNak: an FA (NG) ACK returns ErrCommandRejected
// and synthesizes NO state — the rig refused the command, so the SPA must not be
// told the value changed.
func TestSendCommandsCIV_RejectsOnNak(t *testing.T) {
	s, _, ch, cleanup := startedCIVService(t, []byte(civAckNGFrame))
	defer cleanup()

	err := s.SendCommand(context.Background(), "set_freq", "18100000")
	if !stderr.Is(err, ErrCommandRejected) {
		t.Fatalf("err = %v, want ErrCommandRejected", err)
	}
	assertNoRigState(t, ch, func(p RigStatePayload) bool { return p.VfoA == 18100000 }, 100*time.Millisecond)
}

// TestSendCommandsCIV_NoAckTimesOut: a silent rig (no ACK) returns ErrCommandNoAck
// after the ack window — the bridge surfaces the uncertainty rather than
// synthesizing a state it never saw acknowledged.
func TestSendCommandsCIV_NoAckTimesOut(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	s.civAckTimeout = 80 * time.Millisecond // before Start — only SendCommands reads it
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()
	ch, unsub := s.Subscribe()
	defer unsub()
	fake.feedLine(civFreqBroadcast) // no onWrite → no ACK ever
	waitForIdentity(t, s)

	start := time.Now()
	err := s.SendCommand(context.Background(), "set_freq", "18100000")
	if !stderr.Is(err, ErrCommandNoAck) {
		t.Fatalf("err = %v, want ErrCommandNoAck", err)
	}
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("returned after %s, want ≥ the 80ms ack window", elapsed)
	}
	assertNoRigState(t, ch, func(p RigStatePayload) bool { return p.VfoA == 18100000 }, 50*time.Millisecond)
}

// TestSendCommandsCIV_ContextCancel: a cancelled request context aborts the wait
// promptly with the context error (not a no-ack timeout).
func TestSendCommandsCIV_ContextCancel(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer func() { _ = s.Stop() }()
	_, unsub := s.Subscribe()
	defer unsub()
	fake.feedLine(civFreqBroadcast)
	waitForIdentity(t, s)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — no ACK will be waited for
	err := s.SendCommand(ctx, "set_freq", "18100000")
	if !stderr.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
