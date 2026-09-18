package bridge

import "time"

// AlarmObserver is the bridge's narrow seam for the Station Events store
// (W-0020, ADR 0076 §4): each method is one typed fact about the alarm family,
// reported from the single publish site that knows the outcome. The bridge
// must not import storage, so it never sees a row — assembly (cmd/smd) turns
// these calls into stored events through a non-blocking recorder, and a call
// must return at once: nothing here may wait on a database.
//
// Codes are the ADR 0051 / drive-alarm i18n codes already on the hub payloads
// (tx_unconfirmed, tx_still_keyed, …, drive_no_output). Times are the
// occurrence times captured at the publish site, not at the recorder.
type AlarmObserver interface {
	// TxAlarmRaised — the TX alarm latched with code at `at`.
	TxAlarmRaised(code string, at time.Time)
	// TxAlarmCleared — the standing alarm (code, raised at raisedAt) cleared on
	// positive RX evidence at `at`. The bridge retains the alarm's identity from
	// raise to clear so the row can say how long it stood.
	TxAlarmCleared(code string, raisedAt, at time.Time)
	// DriveAlarmRaised — the TX-drive alarm raised with code at `at`.
	DriveAlarmRaised(code string, at time.Time)
	// DriveAlarmRecovered — output confirmed normal after the drive alarm.
	DriveAlarmRecovered(code string, at time.Time)
}

// SetAlarmObserver injects the observer. Called once during daemon wiring
// before Start; nil (every existing test, a deployment without the recorder)
// records nothing.
func (s *Service) SetAlarmObserver(o AlarmObserver) {
	s.mu.Lock()
	s.alarmObserver = o
	s.mu.Unlock()
}

// alarmObserverSnapshot returns the wired observer or nil. Callers read it
// OUTSIDE the publish site's critical section and call the observer without
// holding s.mu, so an observer can never extend a lock the read loop needs.
func (s *Service) alarmObserverSnapshot() AlarmObserver {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.alarmObserver
}
