package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"sync/atomic"
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
	// Replies stay DISARMED through Start: the INIT/READ startup writes must
	// not queue stale ACK frames that could later be misdelivered to a real
	// waiter (they have no waiter of their own). Armed just before identity.
	var replyArmed atomic.Bool
	// The ADR 0051 defensive recovery sends tx_off (1C 00 00) on identity
	// confirmation; a real rig ACKs that benign frame regardless of the
	// behaviour a test scripts for its OWN writes — so answer it OK always,
	// and apply the scripted ackFrame to everything else.
	fake.onWrite = func(w []byte) []byte {
		if !replyArmed.Load() {
			return nil
		}
		if bytes.HasSuffix(w, []byte{0x1C, 0x00, 0x00}) {
			return append([]byte(nil), civAckOKFrame...)
		}
		if ackFrame != nil {
			return append([]byte(nil), ackFrame...)
		}
		return nil
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ch, unsub := s.Subscribe()
	replyArmed.Store(true)
	fake.feedLine(civFreqBroadcast)
	waitForIdentity(t, s)
	// Let the defensive recovery reach EITHER outcome, then normalise to a
	// settled TX-ready state. (Confirmation is the usual path; the fake's
	// zero-latency reply can occasionally be consumed before the recovery's
	// ACK waiter registers, which times the recovery out into the alarm — a
	// fixture-speed artifact real serial latency doesn't produce.)
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		uncertain, alarmed := s.txUncertain, s.txAlarmActive
		s.mu.Unlock()
		if !uncertain {
			break
		}
		if alarmed {
			s.confirmTxIdle("test fixture normalisation")
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("defensive recovery did not settle")
		}
		time.Sleep(5 * time.Millisecond)
	}
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

// TestSendCommandsCIV_UpdatesDialSnapshotOnAck guards review 2026-06-19 M1: an
// ACKed set_freq must update the internal dial snapshot (CurrentDialMHz) — the
// authoritative dial cmd/smd logs FT8 QSOs / PSK Reporter spots from — not just
// the SSE state, and without waiting for a later poll. startedCIVService seeds
// identity from a 14.074 MHz broadcast, so the dial starts there.
func TestSendCommandsCIV_UpdatesDialSnapshotOnAck(t *testing.T) {
	s, _, ch, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	if mhz, ok := s.CurrentDialMHz(); !ok || mhz != 14.074 {
		t.Fatalf("seed dial = %v (ok=%v), want 14.074", mhz, ok)
	}

	if err := s.SendCommand(context.Background(), "set_freq", "18100000"); err != nil {
		t.Fatalf("SendCommand(set_freq): %v", err)
	}
	// The synthesized push and the dial-snapshot write happen in the same call;
	// waiting for the push guarantees captureDialFreq has run.
	waitForRigState(t, ch, func(p RigStatePayload) bool { return p.VfoA == 18100000 }, time.Second)

	if mhz, ok := s.CurrentDialMHz(); !ok || mhz != 18.1 {
		t.Errorf("dial after ACKed set_freq = %v (ok=%v), want 18.1 (no poll)", mhz, ok)
	}
}

// TestTriggerBootstrapCIV_SerialisesBehindCmdMu guards review 2026-06-19 M2: a
// CI-V bootstrap READ burst must not write to the half-duplex bus while a
// command/key ACK sequence holds cmdMu — it has to wait its turn. Holding cmdMu
// here stands in for that in-flight sequence; TriggerBootstrap must neither
// return nor write until the lock is released. The liveness-recovery READ uses
// the same underCmdMuCIV helper, so this also covers that path's serialization.
func TestTriggerBootstrapCIV_SerialisesBehindCmdMu(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	s.cmdMu.Lock() // simulate an in-flight command/key sequence owning the bus

	before := len(fake.recordedWrites())
	done := make(chan error, 1)
	go func() { done <- s.TriggerBootstrap(context.Background()) }()

	// While cmdMu is held, the bootstrap must be parked — no return, no writes.
	select {
	case err := <-done:
		s.cmdMu.Unlock()
		t.Fatalf("TriggerBootstrap returned while cmdMu held (err=%v) — not serialised", err)
	case <-time.After(50 * time.Millisecond):
	}
	if got := len(fake.recordedWrites()) - before; got != 0 {
		s.cmdMu.Unlock()
		t.Fatalf("bootstrap wrote %d frame(s) while cmdMu held; want 0", got)
	}

	s.cmdMu.Unlock()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("TriggerBootstrap after release: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("TriggerBootstrap did not complete after cmdMu released")
	}
	if len(fake.recordedWrites()) <= before {
		t.Error("bootstrap wrote no READ frames after cmdMu released")
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
		{0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x00}, // read-back: VFO-A freq
		{0xFE, 0xFE, 0x94, 0xE0, 0x25, 0x01}, // read-back: VFO-B freq
		{0xFE, 0xFE, 0x94, 0xE0, 0x26, 0x00}, // read-back: mode + data
		{0xFE, 0xFE, 0x94, 0xE0, 0x0F},       // read-back: split
		{0xFE, 0xFE, 0x94, 0xE0, 0x14, 0x0A}, // read-back: power level
	}
	if len(got) != len(want) {
		t.Fatalf("swap_vfo wrote %d frames (% X), want swap + 5 read-back", len(got), got)
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
	// The silent rig never ACKs the ADR 0051 defensive tx_off either, so the
	// recovery correctly ends in the alarm; reset so the scenario under test
	// starts from a settled state.
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.txAlarmActive
	}, "defensive recovery did not alarm on the silent rig")
	s.confirmTxIdle("test setup reset")

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
	waitFor(t, func() bool {
		s.mu.Lock()
		defer s.mu.Unlock()
		return s.txAlarmActive
	}, "defensive recovery did not alarm on the silent rig")
	s.confirmTxIdle("test setup reset")

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled — no ACK will be waited for
	err := s.SendCommand(ctx, "set_freq", "18100000")
	if !stderr.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
