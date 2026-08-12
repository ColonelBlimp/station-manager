package ft8

// Desktop idle inhibition while TX is armed.
//
// An armed FT8 run is exactly the state in which the machine looks IDLE to the
// desktop — no keyboard, no mouse, for however long the operator leaves it
// running — and exactly the state in which nobody is watching to notice the
// consequences. On 2026-07-28 an unattended 80m run went silent mid-session:
// the daemon kept keying and the decode log kept recording "Transmitting", but
// no audio reached the rig for 24 minutes and 48 CQ calls. The suspected trigger
// is a session/power event while the screen was blanked, of exactly the shape
// that destroyed the CAPTURE stream on 2026-07-18 ("KDE Plasma device fiddling
// destroyed+recreated the rig codec's PipeWire nodes mid-capture") — SM holds
// ONE playback device handle from arm to disarm, so a re-route while that handle
// is open leaves every subsequent slot writing into nothing.
//
// This is a MITIGATION for a cause that is not yet proven (see the 2026-07-28
// dogfood-inbox entry: the confirming experiment is an 80m run at 350 W with the
// operator actively at the PC). It is worth having either way — a station that is
// transmitting should not let its host idle, blank or suspend — but it is not a
// substitute for making the playback path recover on its own.
//
// The interface is injected exactly as TxKeyer and the capture source are, so
// internal/ft8 acquires no D-Bus dependency and stays testable without a session
// bus. cmd/smd wires the real implementation.
type IdleInhibitor interface {
	// Inhibit asks the desktop to stop idling, blanking and suspending, and
	// returns a release func. An implementation that cannot inhibit (no session
	// bus, headless, unsupported platform) returns an error; the caller treats
	// that as non-fatal and transmits anyway.
	Inhibit(why string) (release func(), err error)
}

// SetIdleInhibitor injects the desktop idle inhibitor. Called once during daemon
// wiring before Start; nil leaves the behaviour off entirely, which is the
// correct shape for a headless deployment and for every existing test.
func (s *Service) SetIdleInhibitor(in IdleInhibitor) {
	s.txMu.Lock()
	s.idleInhibitor = in
	s.txMu.Unlock()
}

// inhibitReason is the human-readable "why" the desktop shows in
// `systemd-inhibit --list` and in a session's power UI. It names the station and
// the condition, so an operator who finds their machine refusing to idle can see
// at a glance what is holding it and that disarming TX will free it.
const inhibitReason = "Station Manager: FT8 transmit is armed"

// acquireIdleInhibit takes an inhibition and stores its release, called with txMu
// FREE (Inhibit does D-Bus I/O). The re-check under the lock closes the window
// this opens: a disarm can complete while the bus call is in flight, and storing
// the release afterwards would leak it — the disarm has already run and will not
// run again. Releasing immediately in that case is the whole point.
//
// A failure is logged and swallowed. Inhibition is a courtesy to the desktop;
// the operator's ability to transmit is not negotiable against it (rule 5).
func (s *Service) acquireIdleInhibit(in IdleInhibitor) {
	release, err := in.Inhibit(inhibitReason)
	if err != nil {
		s.log.WarnWith().Err(err).
			Msg("ft8 tx: could not inhibit desktop idle; transmitting anyway (the host may blank or suspend mid-run)")
		return
	}
	s.txMu.Lock()
	if !s.txArmed || s.idleRelease != nil {
		// Disarmed while acquiring, or another acquisition already stored one.
		// Free this one NOW rather than storing it over the top.
		s.txMu.Unlock()
		release()
		return
	}
	s.idleRelease = release
	s.txMu.Unlock()
	// The held interval is the fact this file exists to record (the suspected
	// host-sleep event was mid-run): this line pairs with the release line in
	// takeIdleReleaseLocked so "inhibition held from T1 to T2" is reconstructable (#9).
	s.log.InfoWith().Msg("ft8 tx: desktop idle inhibited while TX is armed (host held awake)")
}

// takeIdleReleaseLocked hands back the pending release func and clears it, so the
// caller can invoke it OUTSIDE txMu. Caller holds txMu. Returns nil when nothing
// is held, which makes the disarm paths uniform: every one of them can call this
// and then release unconditionally.
func (s *Service) takeIdleReleaseLocked() func() {
	rel := s.idleRelease
	s.idleRelease = nil
	if rel != nil {
		// Pairs with the acquire line — closes the held interval (#9). Only when
		// something was actually held, so the uniform "call this on every disarm"
		// contract stays quiet on the paths that held nothing.
		s.log.InfoWith().Msg("ft8 tx: releasing desktop idle inhibition (TX disarmed)")
	}
	return rel
}
