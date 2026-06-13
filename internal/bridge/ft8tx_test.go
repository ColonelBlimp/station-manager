package bridge

import (
	"context"
	stderr "errors"
	"testing"

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

	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); err == nil {
		t.Fatal("KeyFt8Tx should refuse while a tune is active")
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

	if err := s.StartTune(context.Background()); err == nil {
		t.Fatal("StartTune should refuse while FT8 TX is active")
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
