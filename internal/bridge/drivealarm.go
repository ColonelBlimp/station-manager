package bridge

import "time"

// Drive-collapse detection — follow-up (1) of the 2026-07-29 meter arc.
//
// The fault this catches, measured on hardware that day: the rig keys, the CAT
// link is healthy, the daemon logs a clean transmission — and no RF leaves the
// radio because the audio feeding it has gone. Before this, the only evidence
// was retrospective, in the per-transmission meter summary, so the operator
// discovered it after the fact if at all.
//
// The detector is an idle-timeout on the rig's own meter stream. It needs no
// invented sampling interval: the rig pushes RM0 unprompted (the rigdef arms
// AI mode with `AI1;`), and drive that is absent goes mostly SILENT, because the
// rig pushes on change and a meter pinned at zero has little to report. That is
// why the controlled sweep saw 12 of 24 transmissions produce no frames whatever.
//
// "Mostly" is load-bearing and was learnt on the air, not designed. This comment
// previously said absent drive produces silence RATHER THAN zero-valued frames;
// the 2026-07-30 05:01:15 slot (muted from before key-down) disproves the
// absolute form — `n=35`, of which only ~5-9 frames can be attributed to the
// unmute at 05:01:27.67, leaving ~26-30 zero-valued frames pushed at roughly
// 2-3 Hz while no RF was leaving the rig. The alarm still fired at exactly +3 s
// because those frames began AFTER a complete gap from key-down.
//
// So this detector keys on GAPS, not on values, and its safety rests on absent
// drive being silent for LONG ENOUGH rather than silent at all. If those sparse
// zero-valued pushes ever start within the window, no gap opens and no alarm
// fires despite zero output — a latent false negative. Nothing measured says how
// reliably the gap appears; the cheap way to find out costs no transmission at
// all, being inter-frame-gap or value-histogram logging over one keyed window.
// A value-aware rule would need a definition of "zero", which is a judgement for
// the operator and must not be inferred.
//
// The whole difficulty is that silence is ALSO what a dead instrument looks
// like. The discriminator is the receive-time stream: the rig pushes the
// S-meter continuously while receiving (measured at ~26 Hz), so a meter frame
// arriving since the last transmission ended proves something is reading. With
// no such evidence the detector does not arm at all — telling an operator their
// transmitter is dead, when what is actually broken is the instrument watching
// it, is a worse failure than staying quiet.
//
// This is deliberately NOT the ADR 0051 tx-alarm. That one means the PTT may be
// stuck: it latches txUncertain to refuse all further keying, and may only be
// cleared by positive RX evidence. A drive fault shares none of that — the
// transmitter is behaving — so it gets its own event and touches no TX-safety
// state. Latching txUncertain here would turn "your drive died" into "you
// cannot transmit again", which is worse than the fault being reported.

// driveSilenceTimeout is how long the meter stream may be silent inside a keyed
// transmission before the drive is called dead. THE OPERATOR'S NUMBER, not a
// derived one: the healthy stream measured ~12 Hz during FT8 TX (normal gaps
// ~80 ms) and the observed collapse left a ~10 s gap, so anything from ~1 s to
// ~3 s separates them; 3 s was chosen because it still leaves ~9 s of warning
// inside a 12.6 s slot.
//
// A var rather than a const so tests can shorten it — the same reason
// txConfirmTimeout is one.
var driveSilenceTimeout = 3 * time.Second

// DriveAlarmNoOutput — the rig is keyed, the meter instrument is known good,
// and it has reported nothing for driveSilenceTimeout.
const DriveAlarmNoOutput = "drive_no_output"

// DriveAlarmPayload is the drive-alarm event payload. Code is an i18n key (ADR
// 0010) naming the fault; the SPA owns the wording.
//
// Active is always true today: the alarm is a per-transmission ONE-SHOT, not a
// latched state, so there is no clear event and nothing is hub-cached. That is
// deliberate. A drive fault is a property of the slot that just failed, and a
// cached "drive dead" replayed to a tab opening an hour later would be a lie;
// dismissal is the SPA's business, exactly as it already is for the tx-alarm
// banner. The field exists so a clear event can be added without a wire break
// if the alarm ever grows a latched form.
type DriveAlarmPayload struct {
	Active bool   `json:"active"`
	Code   string `json:"code,omitempty"`
}

// armDriveWatch starts drive-collapse detection for one transmission. Caller
// holds s.mu; called from the same critical section that commits ft8TxActive,
// so detection exists from the instant the bridge commits to transmitting.
//
// gen is the ft8TxGen of this transmission — the callback is gated on it like
// the auto-off backstop, so a timer outliving its transmission cannot alarm
// against the next one.
func (s *Service) armDriveWatch(gen uint64) {
	s.driveAlarmed = false
	// The gap measurement opens BEFORE the instrument-alive check, so it keeps
	// running on exactly the transmissions where the detector declines to act. Its
	// whole purpose is telling a dead instrument from dead drive, so switching it
	// off with the alarm would blind it where it is needed (metergap_test.go G6).
	s.openMeterGapWindow()
	// No evidence the instrument is alive: do not arm. Silence then means
	// nothing, and an alarm would misattribute a broken instrument (dead CAT
	// link, AI mode not armed, a rig that does not push RM) to a dead
	// transmitter.
	if !s.meterSeenSinceTx {
		return
	}
	// Silence is measured from the key, not from the last receive-time push, so
	// drive that never comes up at all is caught — the common shape.
	s.driveLastMeterAt = time.Now()
	s.driveTimer = time.AfterFunc(s.driveSilence, func() { s.checkDriveSilence(gen) })
}

// disarmDriveWatch stops detection and clears the per-transmission state.
// Caller holds s.mu; called from the same critical section that clears
// ft8TxActive.
func (s *Service) disarmDriveWatch() {
	if s.driveTimer != nil {
		s.driveTimer.Stop()
		s.driveTimer = nil
	}
	s.driveAlarmed = false
	s.closeMeterGapWindow()
	// The next transmission must earn its own instrument-alive evidence. A link
	// that dies mid-session would otherwise stay "known good" for the rest of the
	// session and alarm on every subsequent transmission.
	s.meterSeenSinceTx = false
}

// noteMeterPush records that the instrument spoke. Caller holds s.mu; called
// from observeMeter's unconditional observation layer, so receive-time pushes
// count — they are the evidence the instrument is alive.
func (s *Service) noteMeterPush() {
	s.meterSeenSinceTx = true
	now := time.Now()
	s.noteMeterGap(now)
	s.driveLastMeterAt = now
}

// openMeterGapWindow starts gap measurement for one transmission. Caller holds
// s.mu.
//
// The first interval is measured from the KEY, not from the first frame, so drive
// that never comes up at all is measurable rather than undefined — the same
// reason driveLastMeterAt is seeded here.
func (s *Service) openMeterGapWindow() {
	now := time.Now()
	s.meterGapWindowAt = now
	s.meterGapLastAt = now
	s.meterGapMax = 0
	s.meterGapKeyedFor = 0
	s.meterGapSealed = false
}

// sealMeterGapWindow freezes the measurement at the instant tx_off is written.
// Caller holds s.mu.
//
// PTT is down from here, but the transmission is not closed for some time yet:
// the release path waits for unkey confirmation (up to confirmTimeout + 1 s),
// pauses for the restore settle, and writes the mode restore. Measuring to the
// close would report that dead air as part of the keyed window — inflating
// keyed_ms, and inflating the widest silence past the 3 s it is judged against on
// a transmission that was fine. The number would then be worse than absent,
// because it would read as evidence.
//
// Idempotent, and a no-op with no window open: a second release (operator unkey
// racing the auto-off backstop) must not re-seal against a later clock.
func (s *Service) sealMeterGapWindow() {
	if s.meterGapWindowAt.IsZero() || s.meterGapSealed {
		return
	}
	now := time.Now()
	if tail := now.Sub(s.meterGapLastAt); tail > s.meterGapMax {
		s.meterGapMax = tail
	}
	s.meterGapKeyedFor = now.Sub(s.meterGapWindowAt)
	s.meterGapSealed = true
}

// closeMeterGapWindow ends measurement. Caller holds s.mu. Called from
// disarmDriveWatch, which runs after the summary has been flushed, so the flush
// still sees the window it is reporting on.
func (s *Service) closeMeterGapWindow() {
	s.meterGapWindowAt = time.Time{}
	s.meterGapLastAt = time.Time{}
	s.meterGapMax = 0
	s.meterGapKeyedFor = 0
	s.meterGapSealed = false
}

// noteMeterGap folds one push into the widest-silence measurement. Caller holds
// s.mu. A no-op outside a keyed window: receive-time pushes are what proves the
// instrument alive, but they are not part of any transmission's window.
func (s *Service) noteMeterGap(now time.Time) {
	if s.meterGapWindowAt.IsZero() {
		return
	}
	if gap := now.Sub(s.meterGapLastAt); gap > s.meterGapMax {
		s.meterGapMax = gap
	}
	s.meterGapLastAt = now
}

// meterGapAtUnkey reports the transmission's window length and widest silence,
// including the trailing silence up to unkey. Caller holds s.mu.
//
// The tail counts because the detector would have tripped on it: a stream that
// stops with 6 s of the slot left is the collapse this exists to measure, and
// ending the measurement at the last frame would report it as healthy.
//
// A sealed window returns the frozen result — see sealMeterGapWindow. Falling
// through to the clock is for the paths that reach the close WITHOUT writing
// tx_off: a rig that vanished mid-transmission took PTT with it, and "now" is
// then the honest end of the window.
//
// Both spans come from ONE reading of the clock, so a totally silent transmission
// reports a silence exactly equal to its window rather than two values that
// disagree by a scheduling delay.
func (s *Service) meterGapAtUnkey() (measured bool, gapMax, keyedFor time.Duration) {
	if s.meterGapWindowAt.IsZero() {
		return false, 0, 0
	}
	if s.meterGapSealed {
		return true, s.meterGapMax, s.meterGapKeyedFor
	}
	now := time.Now()
	gapMax = s.meterGapMax
	if tail := now.Sub(s.meterGapLastAt); tail > gapMax {
		gapMax = tail
	}
	return true, gapMax, now.Sub(s.meterGapWindowAt)
}

// checkDriveSilence is the idle-timeout callback: alarm if the stream has been
// silent for the whole window, otherwise re-arm for the remainder.
//
// One timer per transmission that re-arms, rather than a timer reset on every
// pushed frame: the rig pushes at up to ~26 Hz, and rescheduling that often to
// express "still alive" is work proportional to health rather than to fault.
func (s *Service) checkDriveSilence(gen uint64) {
	s.mu.Lock()
	// Stale: the transmission ended, a newer one owns the PTT, or this
	// transmission has already alarmed.
	if !s.ft8TxActive || s.ft8TxGen != gen || s.driveAlarmed {
		s.mu.Unlock()
		return
	}
	if since := time.Since(s.driveLastMeterAt); since < s.driveSilence {
		s.driveTimer = time.AfterFunc(s.driveSilence-since, func() { s.checkDriveSilence(gen) })
		s.mu.Unlock()
		return
	}
	// Once per transmission. A ticker would raise four banners for one fault in a
	// 12.6 s slot, and the second one tells the operator nothing the first did
	// not.
	s.driveAlarmed = true
	s.driveTimer = nil
	s.mu.Unlock()

	// Outside the lock — the log write and the fan-out to every subscriber must
	// not block the read loop that feeds this detector.
	s.logger.ErrorWith().Str("code", DriveAlarmNoOutput).
		Msg("bridge: rig is keyed but the meter reports no output — check drive to the radio")
	s.hub.publish(Event{
		Name:    EventDriveAlarm,
		Payload: DriveAlarmPayload{Active: true, Code: DriveAlarmNoOutput},
	})
}
