package bridge

import "testing"

// TxActive reflects the tune/FT8 single-flight and, deliberately, NOT txUncertain
// — a stuck/unconfirmed TX must still allow a recovery restart (POST /v1/restart).
func TestTxActive(t *testing.T) {
	var nilSvc *Service
	if nilSvc.TxActive() {
		t.Fatal("nil bridge should read TxActive=false")
	}

	s := &Service{}
	if s.TxActive() {
		t.Fatal("idle bridge should read TxActive=false")
	}

	s.tuneActive = true
	if !s.TxActive() {
		t.Fatal("a keyed tune carrier should read TxActive=true")
	}
	s.tuneActive = false

	s.ft8TxActive = true
	if !s.TxActive() {
		t.Fatal("a keyed FT8 transmission should read TxActive=true")
	}
	s.ft8TxActive = false

	// A stuck / unconfirmed prior TX must NOT read active — a recovery restart has
	// to stay possible (2026-07-21 stuck-TX incident).
	s.txUncertain = true
	if s.TxActive() {
		t.Fatal("txUncertain must not read as TxActive (recovery restart must be allowed)")
	}
}
