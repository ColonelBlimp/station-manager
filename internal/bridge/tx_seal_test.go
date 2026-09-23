package bridge

import (
	"context"
	stderr "errors"
	"testing"
)

// W-0021 slice 3B (ADR 0071): an archive activation seals the rig's keyed
// paths under the bridge's own single-flight snapshot. Idle → sealed; tune and
// FT8 keying then refuse with the named reason and write nothing to the rig.

func TestSealTx_IdleSealsAndKeyedPathsRefuse(t *testing.T) {
	s, fake := newCommandTestService(t)
	s.lastMode = "USB"
	s.lastPower = 50
	if err := s.SealTx(); err != nil {
		t.Fatalf("seal idle: %v", err)
	}
	if err := s.SealTx(); err != nil {
		t.Fatalf("sealing twice must be idempotent: %v", err)
	}
	if err := s.StartTune(context.Background()); !stderr.Is(err, ErrTxSealed) {
		t.Fatalf("StartTune under the seal = %v, want ErrTxSealed", err)
	}
	if err := s.KeyFt8Tx(context.Background(), "DATA-U"); !stderr.Is(err, ErrTxSealed) {
		t.Fatalf("KeyFt8Tx under the seal = %v, want ErrTxSealed", err)
	}
	if w := fake.recordedWrites(); len(w) != 0 {
		t.Fatalf("a sealed key wrote to the rig: %q", w)
	}
	s.ReleaseTxSeal()
	s.ReleaseTxSeal() // releasing an open seal is a no-op
	if err := s.KeyFt8Tx(context.Background(), ""); err != nil {
		t.Fatalf("KeyFt8Tx after release: %v", err)
	}
	s.finishFt8Tx()
}

func TestSealTx_RefusedWhileTuneIsKeyed(t *testing.T) {
	s, _ := newCommandTestService(t)
	s.mu.Lock()
	s.tuneActive = true
	s.mu.Unlock()
	if err := s.SealTx(); !stderr.Is(err, ErrTxActive) {
		t.Fatalf("SealTx during tune = %v, want ErrTxActive", err)
	}
	if s.txSealedNow() {
		t.Fatal("a refused seal was left behind")
	}
}

func TestSealTx_RefusedWhileFt8IsKeyed(t *testing.T) {
	s, _ := newCommandTestService(t)
	s.mu.Lock()
	s.ft8TxActive = true
	s.mu.Unlock()
	if err := s.SealTx(); !stderr.Is(err, ErrTxActive) {
		t.Fatalf("SealTx during FT8 TX = %v, want ErrTxActive", err)
	}
	if s.txSealedNow() {
		t.Fatal("a refused seal was left behind")
	}
}

func (s *Service) txSealedNow() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txSealed
}
