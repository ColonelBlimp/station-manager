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

// The drive-watch states, as they appear in the log. One vocabulary for the
// per-transmission `drive_watch` field AND the transition lines, so a reader
// never has to map one set of words onto another.
//
// driveWatchUnknown is the zero value and means "no transmission has arrived at
// the arm point yet" — reported as a transition's `from`, never as an outcome.
const (
	driveWatchUnknown    = "unknown"
	driveWatchArmed      = "armed"
	driveWatchNoMeter    = "no_meter"
	driveWatchMeterNotPO = "meter_not_po"
	// driveWatchMovedOffPO is reached MID-transmission only: the watch armed on PO
	// and the operator turned the meter knob while keyed, so the remaining silence
	// stopped being evidence about RF. Distinct from meter_not_po, which is the
	// arm-time refusal — the two have different causes and different fixes.
	driveWatchMovedOffPO = "meter_moved_off_po"
)

// DriveAlarmPayload is the drive-alarm event payload. Code is an i18n key (ADR
// 0010) naming the fault; the SPA owns the wording.
//
// Active=true raises the alarm; Active=false REPORTS RECOVERY — output was
// confirmed normal on a later transmission. Recovery does NOT mean "clear the
// banner": the banner stays until the operator dismisses it, because a rig whose
// output came back has still not been looked at. That is why this is one event
// with a flag rather than a raise and a clear.
//
// Nothing is hub-cached, deliberately. A drive fault is a property of the slot
// that failed, and a cached "drive dead" replayed to a tab opening an hour later
// would be a lie.
type DriveAlarmPayload struct {
	Active bool   `json:"active"`
	Code   string `json:"code,omitempty"`
}

// driveMonitorFor reports whether drive-collapse detection can run under a given
// rig meter selection, as a RigStatePayload.DriveMonitor code. Empty for an
// unknown selection — never guessed, the same discipline meterSelection() keeps.
//
// ONE rule with TWO readers, deliberately: armDriveWatch ACTS on it and
// mapStatusToPayload TELLS THE OPERATOR about it. Split into two comparisons they
// could drift, and the failure would be the worst shape available — a banner
// saying monitoring is on while the detector has quietly declined to arm.
func driveMonitorFor(meterSel string) string {
	switch meterSel {
	case "":
		return ""
	case meterSelPO:
		return DriveMonitorOK
	default:
		return DriveMonitorMeterNotPO
	}
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
		s.enterDriveWatchStateLocked(driveWatchNoMeter, gen)
		return
	}
	// The rig pushes RM0 = the value of whatever meter is SELECTED, and only PO
	// says anything about RF. A correctly-driven FT8 signal reads near zero on
	// ALC, and the rig pushes on CHANGE, so a meter parked at zero goes quiet
	// while output is perfectly normal — silence stops being evidence. Measured
	// on the air 2026-07-31: two false alarms with the meter moved to ALC to set
	// audio drive, sample count 532 -> 8 and the gap 239 ms -> 9.7 s across the
	// selection change, with RF leaving the rig throughout.
	//
	// Deliberately blocks only a KNOWN non-PO selection: unknown (no MS frame
	// seen, or a rigdef that reports none) still arms, so this fixes the measured
	// fault without silently disabling detection on rigs that never answer MS.
	if driveMonitorFor(s.meterSel) == DriveMonitorMeterNotPO {
		s.enterDriveWatchStateLocked(driveWatchMeterNotPO, gen)
		return
	}
	// From here the transmission IS being watched, which is what makes a silent
	// outcome positive evidence of normal output rather than an absence of data.
	s.driveWatchArmed = true
	// Armed on PO; observeMeter taints this if the selection moves while keyed.
	s.driveSelTainted = false
	// Silence is measured from the key, not from the last receive-time push, so
	// drive that never comes up at all is caught — the common shape.
	s.driveLastMeterAt = time.Now()
	s.driveTimer = time.AfterFunc(s.driveSilence, func() { s.checkDriveSilence(gen) })
	s.enterDriveWatchStateLocked(driveWatchArmed, gen)
}

// driveWatchTransition is a CHANGE in whether drive detection is running. The
// zero value means "no change", which is the overwhelmingly common case — 691
// transmissions on 2026-08-01 against a whole-log warn population of 460, so a
// per-transmission line would invert what Warn means in this log.
// meterSel is captured when the transition is RECORDED, not when it is emitted.
// Reading the live selection at emit time reported whatever the knob was on by
// then, which on the very transition that says the meter moved is the wrong
// answer by construction.
type driveWatchTransition struct {
	from, to string
	gen      uint64
	meterSel string
}

// enterDriveWatchStateLocked records this transmission's outcome and reports any
// change from the last one. Caller holds s.mu.
//
// TWO fields, and the split is load-bearing. driveWatchOutcome is
// per-transmission and is cleared by disarmDriveWatch, so a transmission that
// never reaches the arm point (a failed key write, a teardown racing it) carries
// no outcome and its meters record omits the field rather than inheriting the
// previous transmission's. driveWatchState is the cross-transmission memory the
// transition machine compares against, and must NOT be cleared — clearing it
// would re-report every state as new after each transmission, which is exactly
// the per-transmission spray this design exists to avoid.
// It QUEUES any change rather than returning it for the caller to log, because
// the state changes under s.mu while the emit happens after the unlock. Two
// goroutines — the pipeline read loop in observeMeter and whoever is keying —
// could therefore record A then B and emit B then A, leaving "drive detection
// restored" as the last line while the state was actually dark. That inverts the
// one fact this feature exists to report (codex P1 on 1273752d).
func (s *Service) enterDriveWatchStateLocked(next string, gen uint64) {
	s.driveWatchOutcome = next
	prev := s.driveWatchState
	if prev == "" {
		prev = driveWatchUnknown
	}
	if prev == next {
		return
	}
	s.driveWatchState = next
	s.drivePendingLog = append(s.drivePendingLog, driveWatchTransition{
		from: prev, to: next, gen: gen, meterSel: s.meterSel,
	})
}

// flushDriveWatchLog emits queued transitions in the order the state actually
// changed. Called WITHOUT s.mu — a stalled log write must not block the read loop
// that feeds the detector, the same rule the alarm and the meter summary follow.
//
// The ordering guarantee comes from two locks doing different jobs. Records are
// APPENDED under s.mu, which is what serialises the state changes themselves; they
// are DRAINED under driveLogMu, which admits one emitter at a time. A drainer
// takes whatever is queued in FIFO order, so a caller that records first is
// reported first even if a later caller reaches the emitter first.
//
// Every path that records must call this after unlocking. It is safe to call with
// nothing queued, and safe for two callers to race — one drains both records and
// the other finds an empty queue.
func (s *Service) flushDriveWatchLog() {
	s.driveLogMu.Lock()
	defer s.driveLogMu.Unlock()
	for {
		s.mu.Lock()
		if len(s.drivePendingLog) == 0 {
			s.mu.Unlock()
			return
		}
		tr := s.drivePendingLog[0]
		s.drivePendingLog = s.drivePendingLog[1:]
		s.mu.Unlock()
		s.emitDriveWatchTransition(tr)
	}
}

// emitDriveWatchTransition writes one transition. Caller holds driveLogMu and no
// other lock.
//
// WARN, not Error, and the distinction is the operator's (2026-08-01): this is a
// safety-monitoring DEGRADATION, not a confirmed transmitter failure. Error stays
// reserved for DriveAlarmNoOutput, so an Error in this log continues to mean "the
// rig is keyed and nothing is coming out".
func (s *Service) emitDriveWatchTransition(tr driveWatchTransition) {
	if tr.to == driveWatchArmed {
		// unknown -> armed is the ordinary first transmission of a healthy session.
		// Nothing was ever reported lost, so there is nothing to restore, and a line
		// here would fire once per daemon start to say the station is working.
		if tr.from == driveWatchUnknown {
			return
		}
		s.logger.InfoWith().Str("from", tr.from).Str("to", tr.to).Uint64("tx_gen", tr.gen).
			Msg("bridge: drive detection restored")
		return
	}
	s.logger.WarnWith().Str("from", tr.from).Str("to", tr.to).Uint64("tx_gen", tr.gen).
		Str("meter_sel", tr.meterSel).
		Msg("bridge: drive detection went dark — a transmitter failure would not be reported")
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
	// Per-transmission, unlike driveAlarmStanding: the next transmission must earn
	// its own arming before its silence counts as evidence of anything.
	s.driveWatchArmed = false
	// Per-transmission for the same reason, and NOT driveWatchState — see
	// enterDriveWatchStateLocked. Cleared so a transmission that never reaches the
	// arm point reports no outcome instead of inheriting the previous one.
	s.driveWatchOutcome = ""
	s.driveSelTainted = false
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

// inKeyedMeterWindowLocked reports whether a meter frame arriving NOW is
// evidence about the transmission in progress. Caller holds s.mu.
//
// ONE name for a moment that three review rounds each answered separately, and
// wrongly in a different way each time. ft8TxActive is NOT that moment: it means
// "the TX controller holds the single-flight claim", which is deliberately wider
// — it stays true through the whole of releaseFt8TxChecked's tail (the tx_off
// ACK, the confirm cycle, the restore settle, the mode restore) so that nothing
// else can key the rig meanwhile. PTT drops at the FIRST step of that tail, so
// every frame after it is a RECEIVE reading.
//
// Consumers that ask "may I touch the rig" must keep using ft8TxActive — the
// answer there is no for the whole tail. This predicate answers the narrower
// question "is the rig actually radiating", and only meter evidence may use it.
//
// A failed tx_off unseals, and correctly: the transmission is then still running.
func (s *Service) inKeyedMeterWindowLocked() bool {
	return s.ft8TxActive && !s.meterGapSealed
}

// sealMeterGapWindow freezes the measurement at `at`, the instant tx_off was
// ISSUED. Caller holds s.mu.
//
// `at` is passed in rather than read here because the write call does not return
// at the moment PTT drops: on CI-V it waits for the rig's acknowledgement, so
// sealing on return would fold the whole ACK latency into the transmission.
//
// Sealing happens BEFORE the write is issued, not after it succeeds, and the
// difference is a defect that survived two rounds of review. The seal is what
// makes noteMeterGap stop accepting frames; while it was applied afterwards, a
// receive-time frame landing mid-write recorded a gap spanning the unkey and no
// later sealing could remove it, because this function only computes the TRAILING
// gap and never revisits the running maximum. An unkey that fails to write is
// undone by unsealMeterGapWindow.
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
func (s *Service) sealMeterGapWindow(at time.Time) {
	if s.meterGapWindowAt.IsZero() || s.meterGapSealed {
		return
	}
	if tail := at.Sub(s.meterGapLastAt); tail > s.meterGapMax {
		s.meterGapMax = tail
	}
	s.meterGapKeyedFor = at.Sub(s.meterGapWindowAt)
	s.meterGapSealed = true
}

// unsealMeterGapWindow resumes measurement after an unkey that failed to write.
// Caller holds s.mu.
//
// The transmission is NOT over in that case — TX stays armed and the auto-off
// backstop retries — so the window must keep measuring, and the retry seals it
// again with its own instant.
//
// meterGapMax deliberately keeps whatever the seal folded in: the silence from the
// last frame up to the attempted unkey genuinely occurred while the rig was keyed.
// Frames pushed during the failed write are lost to the measurement, which errs
// towards reporting MORE silence than there was — the safe direction for an
// instrument hunting for silence, and the rig may well still have been keyed.
func (s *Service) unsealMeterGapWindow() {
	s.meterGapKeyedFor = 0
	s.meterGapSealed = false
	// The transmission is resuming, so the selector question re-opens — and any
	// selection change that arrived while the write was pending was DISCARDED by
	// observeMeter's seal check. Answer it from the selection in force NOW rather
	// than replaying that frame: a switch to ALC and back to PO inside the sealed
	// window would leave a remembered taint wrong, while the current selection is
	// right by construction. meterSel is recorded unconditionally — only the taint
	// decision is gated — so it is always known here (codex 287825b6 P1).
	if driveMonitorFor(s.meterSel) == DriveMonitorMeterNotPO {
		s.driveSelTainted = true
		// Same reconciliation as observeMeter's taint site, and for the same reason:
		// this transmission is RESUMING after a failed unkey, so its remaining
		// silence stops being evidence from here — and there may be no further timer
		// tick to notice. Gated on an armed watch, since with none there is no
		// protection to report lost.
		if s.driveWatchArmed {
			s.enterDriveWatchStateLocked(driveWatchMovedOffPO, s.ft8MeterGen)
		}
	}
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
//
// Also a no-op once SEALED, which is not a detail. The receive stream comes back
// at ~26 Hz within moments of the unkey, and the window stays open until the
// release path finishes, so without this every transmission would have the TX→RX
// lull folded into its widest silence — the frozen result mutating after it was
// frozen.
func (s *Service) noteMeterGap(now time.Time) {
	if s.meterGapWindowAt.IsZero() || s.meterGapSealed {
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
	// The meter was moved off PO while keyed: this stream stopped being about RF,
	// so its silence is no longer evidence. Drop the watch for the rest of the
	// transmission rather than re-arming the timer — nothing later in it can
	// restore the meaning of the interval already elapsed.
	if s.driveSelTainted {
		s.driveTimer = nil
		// Protection disappeared MID-transmission, which no arm-time transition can
		// see: this watch armed and succeeded. Reported here so the slot that lost
		// detection is the slot that says so — the next arm would report it one
		// transmission late, against a different tx_gen (operator's call, 2026-08-01).
		s.enterDriveWatchStateLocked(driveWatchMovedOffPO, gen)
		s.mu.Unlock()
		s.flushDriveWatchLog()
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
	// Outlives this transmission: the operator has been told output failed, and the
	// report that it came back is owed until a watched transmission proves it.
	s.driveAlarmStanding = true
	s.driveTimer = nil
	// The evidence for this alarm, taken under the SAME lock that decided to raise
	// it. Every value here was already in hand and was being discarded — the same
	// shape as the meterGapAtUnkey finding of 2026-07-30. Without them, judging an
	// alarm means finding the separate meters record emitted at unkey and joining
	// the two by timestamp, which is what cost the time on 2026-08-01: two alarms
	// that could only be shown almost certainly false that way.
	//
	// driveSelTainted is deliberately ABSENT. The taint branch above returns before
	// reaching here, so at this point it is always false — a constant, and a
	// constant field reads as evidence while carrying none. It is reported on its
	// own transition line instead, at the moment it becomes true.
	meterN, meterPoMax := s.driveMeterEvidenceLocked()
	sinceLast := time.Since(s.driveLastMeterAt)
	// The gap that just TRIPPED the alarm is still running, so it is not yet in
	// meterGapMax — noteMeterGap only folds an interval in when the next frame
	// arrives, and on a totally silent transmission no next frame ever comes.
	// Reporting the stored maximum alone gave gap_ms≈3000 beside gap_max_ms=0 on
	// exactly the worst case this alarm exists for, which reads as "no silence at
	// all" next to the silence that raised the alarm.
	gapMax := s.meterGapMax
	if sinceLast > gapMax {
		gapMax = sinceLast
	}
	meterSel := s.meterSel
	s.mu.Unlock()

	// Outside the lock — the log write and the fan-out to every subscriber must
	// not block the read loop that feeds this detector.
	s.logger.ErrorWith().Str("code", DriveAlarmNoOutput).
		Str("meter_sel", meterSel).
		Int("meter_n", meterN).
		Int("meter_po_max", meterPoMax).
		Int("gap_ms", int(sinceLast.Milliseconds())).
		Int("gap_max_ms", int(gapMax.Milliseconds())).
		Uint64("tx_gen", gen).
		Msg("bridge: rig is keyed but the meter reports no output — check drive to the radio")
	s.hub.publish(Event{
		Name:    EventDriveAlarm,
		Payload: DriveAlarmPayload{Active: true, Code: DriveAlarmNoOutput},
	})
}

// driveMeterEvidenceLocked summarises what the meter stream has said about the
// transmission SO FAR, without consuming the accumulator — flushFt8TxMetersLocked
// clears it, and the transmission is still running here. Caller holds s.mu.
//
// Two numbers, answering the two questions actually asked of a drive alarm:
// meter_n says whether anything arrived at all, which separates a dead instrument
// from dead drive; meter_po_max says whether output was EVER seen, which
// separates drive that collapsed part-way from drive that never came up. Both
// were the numbers reached for by hand on 2026-08-01.
//
// A pushed reading is keyed {METER, <selection>} while an explicit query answer is
// keyed {PO, ""} — see meterKey. Both are PO readings and both count, because the
// question is what the rig said about output, not which frame carried it.
func (s *Service) driveMeterEvidenceLocked() (n, poMax int) {
	for k, acc := range s.ft8Meters {
		if acc == nil {
			continue
		}
		n += acc.Count
		isPO := (k.Tag == meterPushedTag && k.Sel == meterSelPO) || k.Tag == meterSelPO
		if isPO && acc.Max > poMax {
			poMax = acc.Max
		}
	}
	return n, poMax
}

// takeDriveRecoveryLocked reports whether the transmission now ending is positive
// evidence that output came back, consuming the standing alarm if it is. Caller
// holds s.mu, and must call this BEFORE disarmDriveWatch clears the
// per-transmission flags it reads.
//
// All three conditions are load-bearing. Without a standing alarm there is nothing
// to report and publishing would put an event on every subscriber's stream every
// slot to say nothing happened. Without driveWatchArmed the transmission measured
// NOTHING — no instrument-alive evidence — and reporting recovery from it would
// claim a measurement never made, which is the fault the banner's wording rules
// exist to prevent. And a transmission that alarmed is plainly not evidence of
// health, however it ended.
func (s *Service) takeDriveRecoveryLocked() bool {
	if !s.driveAlarmStanding || !s.driveWatchArmed || s.driveAlarmed {
		return false
	}
	// The meter left PO while keyed, so this transmission measured RF for only
	// part of its length and cannot support the positive claim recovery makes.
	if s.driveSelTainted {
		return false
	}
	// THE MEASUREMENT DECIDES, not whether the alarm timer got to run. Those come
	// apart: checkDriveSilence takes s.mu on entry, so when finishFt8Tx wins that
	// lock the callback finds the transmission already over and returns without
	// alarming — leaving driveAlarmed false for a slot that contained exactly the
	// silence being hunted. The frozen gap is immune to that race and is the direct
	// evidence for what the banner will claim.
	//
	// Two conditions, each doing work the other cannot:
	//
	//   - the window must be at least as long as the threshold, because in a shorter
	//     one "no silence reached the threshold" is trivially true and establishes
	//     nothing; an operator abandoning a slot early would otherwise clear a
	//     standing alarm by accident.
	//   - the widest silence in it must be under the threshold, which is the actual
	//     claim. It also subsumes a keyed-time-observation check: with no frames at
	//     all the widest gap IS the whole window, so it cannot be under a threshold
	//     the window is at least as long as.
	measured, gapMax, keyedFor := s.meterGapAtUnkey()
	if !measured || keyedFor < s.driveSilence || gapMax >= s.driveSilence {
		return false
	}
	s.driveAlarmStanding = false
	return true
}

// publishDriveRecovery reports that output was confirmed normal after an alarm.
// Called WITHOUT s.mu — the log write and the fan-out must not block the read loop
// that feeds the detector.
//
// Deliberately not a clear: the SPA keeps the banner up and adds the recovery to
// it, because the operator asked to be told the rig is fine now without losing the
// record that it was not.
func (s *Service) publishDriveRecovery() {
	s.logger.InfoWith().Str("code", DriveAlarmNoOutput).
		Msg("bridge: rig output confirmed normal on a later transmission; the drive alarm is no longer current")
	s.hub.publish(Event{
		Name:    EventDriveAlarm,
		Payload: DriveAlarmPayload{Active: false, Code: DriveAlarmNoOutput},
	})
}
