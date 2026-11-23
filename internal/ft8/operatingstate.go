package ft8

// OperatingState is what FT8 is doing at one moment, for a caller that needs to
// know whether a stop would stop something and nothing more (W-0027 A3, section
// I: the desktop app reads it when a close is asked for, so that its question
// says what the daemon knows and not what a page last heard).
//
// It is ONE OBSERVATION, not a hold. An arm already on its way can land after an
// idle answer, and a run can end after a busy one. A caller that must stop FT8
// sends the stop whatever this said.
type OperatingState struct {
	// TxArmed: TX is armed, whether or not anything is running.
	TxArmed bool
	// TxInFlight: a transmission is under way; a manual send waiting for its slot
	// is in flight.
	TxInFlight bool
	// RunActive: a session is under way (an exchange, a Call-CQ run).
	RunActive bool
	// AutoWorkArmed: an auto-work run is live (ADR 0059, ADR 0067). It means what
	// the stream's auto_work_armed means (QsoStatus), and no less: it is true
	// DURING a contact of the run as well as between contacts, so it does not by
	// itself say "waiting". With RunActive false it is a run waiting between
	// contacts, which is the one state none of the other three reports.
	AutoWorkArmed bool
}

// WouldStop reports whether a stop would stop something. Armed with nothing
// running counts, as it does for an archive switch (operator ruling 2026-09-22).
func (o OperatingState) WouldStop() bool {
	return o.TxArmed || o.TxInFlight || o.RunActive || o.AutoWorkArmed
}

// OperatingState reads what FT8 is doing now. It is passive: it subscribes to
// nothing, claims no profile, starts no capture, takes no device, publishes no
// status, retires no session and waits for no completion. It asks nothing about
// who is operating and so needs no lifetime: it authorises nothing.
//
// The parts are read together, under the order the package already uses for a
// check that spans them (seqGate → txMu → the sequencer's lock; SealTxAdmission),
// so the answer is never one half from before a start or a disarm and the other
// from after. They are the service's live state, never the stream's replay cache,
// which holds a frame only if one was ever published.
func (s *Service) OperatingState() OperatingState {
	s.seqGate.Lock()
	defer s.seqGate.Unlock()
	s.txMu.Lock()
	defer s.txMu.Unlock()
	st := OperatingState{TxArmed: s.txArmed, TxInFlight: s.txInFlight}
	if s.seq != nil {
		st.RunActive, st.AutoWorkArmed = s.seq.operatingState() // txMu → seq.mu is the established order
	}
	return st
}

// operatingState reads, together, whether a session is under way and whether an
// auto-work run is live (during a contact of it, or between contacts).
func (s *Sequencer) operatingState() (active, autoWorkArmed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mode != seqIdle, s.autoWork.armed
}
