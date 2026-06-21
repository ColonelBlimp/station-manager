package bridge

import (
	"bytes"
	"context"
	stderr "errors"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// TestKeyFt8Tx_HappyPath: with mode set, KeyFt8Tx writes the data-mode switch
// then tx_on as one line; the FT8-TX single-flight goes active. FTdx10:
// set_mode DATA-U → "MD0C;", tx_on → "TX1;".
func TestKeyFt8Tx_HappyPath(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.lastMode = "USB" // restore target snapshot

	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
	writes := fake.recordedWrites()
	if len(writes) != 1 || string(writes[0]) != "MD0C;TX1;" {
		t.Fatalf("key writes = %q, want one %q", writes, "MD0C;TX1;")
	}
	s.mu.Lock()
	active := s.ft8TxActive
	s.mu.Unlock()
	if !active {
		t.Fatal("ft8TxActive should be true after KeyFt8Tx")
	}

	// Skip the post-unkey settle so the test doesn't wait.
	s.tuneRestoreSettle = 0
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}
	writes = fake.recordedWrites()
	// key line + tx_off + mode restore.
	if len(writes) < 2 || string(writes[1]) != "TX0;" {
		t.Fatalf("unkey writes = %q, want second write %q", writes, "TX0;")
	}
	s.mu.Lock()
	active = s.ft8TxActive
	s.mu.Unlock()
	if active {
		t.Fatal("ft8TxActive should be false after UnkeyFt8Tx")
	}
}

// TestKeyFt8Tx_NoMode: with mode empty, KeyFt8Tx keys tx_on only and UnkeyFt8Tx
// drops tx_off only — no mode switch/restore (the operator manages mode).
func TestKeyFt8Tx_NoMode(t *testing.T) {
	s, fake := newCommandTestService(t)

	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
	if w := fake.recordedWrites(); len(w) != 1 || string(w[0]) != "TX1;" {
		t.Fatalf("key writes = %q, want one %q", w, "TX1;")
	}
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx: %v", err)
	}
	w := fake.recordedWrites()
	if len(w) != 2 || string(w[1]) != "TX0;" {
		t.Fatalf("writes = %q, want [TX1; TX0;]", w)
	}
}

// TestKeyFt8Tx_RefusesUnverifiedIdentity: keying TX on an unverified rig is the
// most dangerous H2 case — refused, nothing reaches the wire.
func TestKeyFt8Tx_RefusesUnverifiedIdentity(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.mu.Lock()
	s.identityConfirmed = false
	s.mu.Unlock()

	err := s.KeyFt8Tx(context.Background(), "DATA-U")
	if !stderr.Is(err, ErrRigIdentityUnverified) {
		t.Fatalf("KeyFt8Tx error = %v, want ErrRigIdentityUnverified", err)
	}
	if w := fake.recordedWrites(); len(w) != 0 {
		t.Errorf("expected no writes on refused key, got %q", w)
	}
}

// TestKeyFt8Tx_NoActiveClient: no rig connected → ErrRigNotConnected, no writes.
func TestKeyFt8Tx_NoActiveClient(t *testing.T) {
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ftdx10"},
	}, &logging.Service{})
	err := s.KeyFt8Tx(context.Background(), "DATA-U")
	if !stderr.Is(err, ErrRigNotConnected) {
		t.Fatalf("KeyFt8Tx with no active client = %v, want ErrRigNotConnected", err)
	}
}

// TestKeyFt8Tx_RefusesDuringTune and the reverse prove the shared single-flight:
// tune and FT8 TX are mutually exclusive (one rig, one PTT).
func TestKeyFt8Tx_RefusesDuringTune(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.mu.Lock()
	s.tuneActive = true
	s.mu.Unlock()

	// Typed so the API can classify it as a 409 conflict, not a generic 500.
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); !stderr.Is(err, ErrTxActive) {
		t.Fatalf("KeyFt8Tx during tune = %v, want ErrTxActive", err)
	}
	if w := fake.recordedWrites(); len(w) != 0 {
		t.Errorf("expected no writes when refused, got %q", w)
	}
}

func TestStartTune_RefusesDuringFt8Tx(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.lastMode = "USB"
	s.lastPower = 50
	s.mu.Lock()
	s.ft8TxActive = true
	s.mu.Unlock()

	if err := s.StartTune(context.Background()); !stderr.Is(err, ErrTxActive) {
		t.Fatalf("StartTune during FT8 TX = %v, want ErrTxActive", err)
	}
	if w := fake.recordedWrites(); len(w) != 0 {
		t.Errorf("expected no tune writes when refused, got %q", w)
	}
}

// TestKeyFt8Tx_SingleFlight: a redundant key while already keyed is a no-op.
func TestKeyFt8Tx_SingleFlight(t *testing.T) {
	s, fake := newCommandTestService(t)

	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("first KeyFt8Tx: %v", err)
	}
	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("second KeyFt8Tx (should no-op): %v", err)
	}
	if w := fake.recordedWrites(); len(w) != 1 {
		t.Fatalf("expected one key write (single-flight), got %q", w)
	}
	s.finishFt8Tx() // cancel the auto-off timer
}

// TestRigConnected is the CAT-live signal used to gate FT8 capture: connected +
// identity confirmed → true; identity unconfirmed → false. Unlike TxReady it is
// indifferent to the tune/FT8-TX single-flight flags — a keyed transmission does
// not mean the rig stopped being connected.
func TestRigConnected(t *testing.T) {
	s, _ := newCommandTestService(t) // connected + identity confirmed
	if !s.RigConnected() {
		t.Fatal("RigConnected should be true when connected + identity confirmed")
	}

	// A tune/FT8-TX in flight must NOT flip RigConnected (it is not TxReady).
	s.mu.Lock()
	s.tuneActive = true
	s.mu.Unlock()
	if !s.RigConnected() {
		t.Error("RigConnected should stay true during a tune — CAT is still live")
	}

	s.mu.Lock()
	s.tuneActive = false
	s.identityConfirmed = false
	s.mu.Unlock()
	if s.RigConnected() {
		t.Error("RigConnected should be false when identity is not confirmed")
	}

	// Nil receiver is safe (absent/disabled bridge → not connected).
	var nilSvc *Service
	if nilSvc.RigConnected() {
		t.Error("nil-receiver RigConnected should be false")
	}
}

// TestTxReady_FalseWhileTransmitting (review 2026-06-16 #4): TxReady must report
// not-ready while a tune carrier or FT8 TX already owns the PTT, so the FT8 arm/
// transmit gate waits instead of arming and then hitting a refused key.
func TestTxReady_FalseWhileTransmitting(t *testing.T) {
	s, _ := newCommandTestService(t) // connected + identity confirmed
	if !s.TxReady() {
		t.Fatal("TxReady should be true when connected + identity confirmed")
	}
	s.mu.Lock()
	s.tuneActive = true
	s.mu.Unlock()
	if s.TxReady() {
		t.Error("TxReady should be false while a tune is active")
	}
	s.mu.Lock()
	s.tuneActive = false
	s.ft8TxActive = true
	s.mu.Unlock()
	if s.TxReady() {
		t.Error("TxReady should be false while FT8 TX is active")
	}
}

// TestKeyUnkeyFt8TxCIV_ConfirmedByAck (review 2026-06-16 H2): on a CI-V rig the
// FT8 key + unkey go through the wait-for-ACK path — tx_on and tx_off are CI-V
// commands the IC-7300 confirms with FB, so the bridge waits for that ACK rather
// than reporting success on bytes-written alone. With the rig ACKing, key/unkey
// complete and the single-flight clears.
func TestKeyUnkeyFt8TxCIV_ConfirmedByAck(t *testing.T) {
	s, fake, _, cleanup := startedCIVService(t, []byte(civAckOKFrame))
	defer cleanup()

	before := len(fake.recordedWrites())
	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx (CI-V): %v", err)
	}
	s.mu.Lock()
	active := s.ft8TxActive
	s.mu.Unlock()
	if !active {
		t.Fatal("ft8TxActive should be true after a CI-V key")
	}
	if err := s.UnkeyFt8Tx(context.Background()); err != nil {
		t.Fatalf("UnkeyFt8Tx (CI-V): %v", err)
	}
	s.mu.Lock()
	active = s.ft8TxActive
	s.mu.Unlock()
	if active {
		t.Fatal("ft8TxActive should be false after a CI-V unkey")
	}
	// tx_on (1C 00 01) then tx_off (1C 00 00) reached the wire.
	var sawOn, sawOff bool
	for _, w := range fake.recordedWrites()[before:] {
		if bytes.Contains(w, []byte{0x1C, 0x00, 0x01}) {
			sawOn = true
		}
		if bytes.Contains(w, []byte{0x1C, 0x00, 0x00}) {
			sawOff = true
		}
	}
	if !sawOn || !sawOff {
		t.Fatalf("expected tx_on and tx_off frames, sawOn=%v sawOff=%v", sawOn, sawOff)
	}
}

// TestUnkeyFt8TxCIV_NoAckKeepsArmed (review 2026-06-16 H2, the safety case): if a
// CI-V rig never ACKs tx_off, the unkey must NOT report success — that would
// cancel the auto-off backstop and strand PTT. Instead it returns ErrCommandNoAck
// and leaves TX armed so the backstop keeps retrying. The rig here ACKs tx_on but
// goes silent on tx_off.
func TestUnkeyFt8TxCIV_NoAckKeepsArmed(t *testing.T) {
	s, fake := newCIVPipelineTestService(t)
	s.civAckTimeout = 60 * time.Millisecond // before Start — only the cmd path reads it
	// ACK everything except tx_off (1C 00 00), which the rig leaves unacknowledged.
	fake.onWrite = func(w []byte) []byte {
		if bytes.Contains(w, []byte{0x1C, 0x00, 0x00}) {
			return nil
		}
		return append([]byte(nil), civAckOKFrame...)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_, unsub := s.Subscribe()
	defer func() { unsub(); _ = s.Stop() }()
	fake.feedLine(civFreqBroadcast)
	waitForIdentity(t, s)

	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx (tx_on is ACKed): %v", err)
	}
	err := s.UnkeyFt8Tx(context.Background())
	if !stderr.Is(err, ErrCommandNoAck) {
		t.Fatalf("UnkeyFt8Tx with un-ACKed tx_off = %v, want ErrCommandNoAck", err)
	}
	s.mu.Lock()
	active := s.ft8TxActive
	s.mu.Unlock()
	if !active {
		t.Fatal("ft8TxActive must stay true after an un-ACKed tx_off (backstop must keep retrying)")
	}
	// Cancel the armed auto-off backstop so it can't fire after the test.
	s.clearFt8TxOnDisconnect()
}

// TestClearFt8TxOnDisconnect clears active TX state + cancels the backstop when
// the pipeline tears down (PTT physically dropped with the rig).
func TestClearFt8TxOnDisconnect(t *testing.T) {
	s, _ := newCommandTestService(t)
	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx: %v", err)
	}
	s.clearFt8TxOnDisconnect()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ft8TxActive {
		t.Error("ft8TxActive should be false after disconnect")
	}
	if s.ft8TxTimer != nil {
		t.Error("ft8TxTimer should be nil after disconnect")
	}
}
