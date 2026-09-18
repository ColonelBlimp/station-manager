package ft8

import "time"

// SessionObserver is the FT8 subsystem's narrow seam for the Station Events
// store (W-0020, ADR 0076 §4): two typed facts about a session the daemon
// ended without the operator asking. FT8 stays isolated from the services
// around it (AGENTS.md), so it never sees a row — assembly (cmd/smd) turns
// these calls into stored events through a non-blocking recorder, and a call
// must return at once: nothing here may wait on a database.
//
// Ruling 2 (2026-09-14) decides which boundary reports what: a partner
// exchange in progress is terminated ONCE, at the sequencer's teardown
// boundary; an idle automatic disarm is reported at the service boundary only
// when no exchange is in progress; the routine unattended linger disarm —
// including an armed Call-CQ run waiting between contacts — is lifecycle and
// reports nothing. Operator causes (abandon, stop, band change, shutdown)
// report nothing.
type SessionObserver interface {
	// ExchangeTerminated — a partner exchange ended by the daemon: cause is one
	// of unattended, cat_lost, dial_moved, dial_unknown, tx_not_armed,
	// tx_bad_message; partnerCall is the raw decoded call (the recorder bounds
	// and normalises it); rung names the step the exchange was on.
	ExchangeTerminated(cause, partnerCall, rung string, at time.Time)
	// TxDisarmed — TX disarmed automatically while idle: cause is cat_lost or
	// dial_moved.
	TxDisarmed(cause string, at time.Time)
}

// SetSessionObserver injects the observer, exactly as TxKeyer and the idle
// inhibitor are. Called once during daemon wiring before Start; nil (every
// existing test, a deployment without the recorder) records nothing.
func (s *Service) SetSessionObserver(o SessionObserver) {
	s.txMu.Lock()
	s.sessionObserver = o
	s.txMu.Unlock()
}

// sessionObserverSnapshot returns the wired observer or nil. Read under txMu,
// called without it: an observer must never extend the sequencing gates.
func (s *Service) sessionObserverSnapshot() SessionObserver {
	s.txMu.Lock()
	defer s.txMu.Unlock()
	return s.sessionObserver
}

// daemonEndedCauses are the session-end causes that record a termination:
// the daemon ended a partner exchange without the operator asking. Operator
// causes (abandon, stop, band change, shutdown) and the repeat cap are absent
// on purpose — see the ruling in the SessionObserver doc.
var daemonEndedCauses = map[string]bool{
	disarmUnattended:      true,
	disarmCatLost:         true,
	EndReasonDialMoved:    true,
	EndReasonDialUnknown:  true,
	EndReasonTxNotArmed:   true,
	EndReasonTxBadMessage: true,
}

// disarmRowCauses are the automatic idle disarms that record tx.disarmed —
// only when the teardown found no partner exchange (ruling 2).
var disarmRowCauses = map[string]bool{
	disarmCatLost:   true,
	disarmDialMoved: true,
}

// noteExchangeTerminated is the sequencer's termination hook (Sequencer.onTerminated):
// called OUTSIDE the sequencer lock with a partner exchange already torn down.
func (s *Service) noteExchangeTerminated(cause, partnerCall, rung string, at time.Time) {
	if !daemonEndedCauses[cause] {
		return
	}
	if o := s.sessionObserverSnapshot(); o != nil {
		o.ExchangeTerminated(cause, partnerCall, rung, at)
	}
}

// noteTxDisarmed reports an automatic idle disarm. Called OUTSIDE txMu by
// disarmTxLocked once its abandon reported that no partner exchange ended —
// when one did, the sequencer boundary has already recorded the termination
// and this records nothing (one row per event).
func (s *Service) noteTxDisarmed(cause string, at time.Time) {
	if !disarmRowCauses[cause] {
		return
	}
	if o := s.sessionObserverSnapshot(); o != nil {
		o.TxDisarmed(cause, at)
	}
}
