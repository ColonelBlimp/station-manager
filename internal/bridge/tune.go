package bridge

import (
	"context"
	stderr "errors"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// Tune-carrier control (ADR 0027) — the first SM feature that transmits, so
// the safety model IS the feature. A tune keys a steady reduced-power RTTY
// carrier for the operator to tune an external linear amp against; the daemon
// (not the SPA) owns a guaranteed stop, because a closed tab / dropped network
// / forgotten toggle must never strand the rig keyed. Three independent
// guarantees converge on the same unkey-and-restore path:
//
//   - hard auto-off timer (tuneMaxDuration) — fires unconditionally
//   - release-on-disconnect — pipeline exit clears tune state
//   - single-flight — one tune at a time
//
// The TX-keying commands (tx_on/tx_off) are deliberately NOT Exposed in the
// rigdef, so they are unreachable via the generic command path (ADR 0026);
// only this controller may key the transmitter.

// Tune command names in the rigdef command table. set_mode + set_power are
// Exposed (also reachable via the generic command path — both are safe,
// neither transmits) and go through cat.EncodeCommand (value_map + pad).
// tx_on/tx_off are deliberately NOT Exposed and reached only here via the
// low-level cat.Encode, which bypasses the Exposed gate.
const (
	tuneModeCommand  = "set_mode"
	tunePowerCommand = "set_power"
	tuneTxOnCommand  = "tx_on"
	tuneTxOffCommand = "tx_off"
)

// tuneCarrierMode is the rig mode the tune carrier transmits in. RTTY keyed
// with no data is a steady single-tone carrier on every HF band at the set
// power (ADR 0027) — unlike FM (band-restricted on HF) or AM (carrier at
// ~¼ PEP, so the watts wouldn't match). Provisional per ADR 0027's "try RTTY
// and see"; promote to config if field use shows it needs to vary per rig.
const tuneCarrierMode = "RTTY-U"

// Tune knob defaults + hard ceilings (ADR 0027). The defaults are the code
// constants; config may lower them or raise them up to the ceiling, which the
// daemon clamps at construction (resolveTunePower / resolveTuneDuration) so
// config can never create an unsafe tune.
const (
	defaultTunePowerW   = 20
	maxTunePowerW       = 40
	defaultTuneDuration = 15 * time.Second
	maxTuneDuration     = 30 * time.Second

	// tuneRetryInterval is how soon the auto-off retries after a failed
	// unkey write. The carrier MUST come down, so a failed stop keeps
	// retrying (fail-safe) rather than giving up with the rig keyed.
	tuneRetryInterval = 1 * time.Second

	// defaultTuneRestoreSettle is the pause between unkeying (tx_off) and
	// restoring the pre-tune mode+power. The FTdx10 ignores a mode-change
	// command (MD0) during the TX→RX transition tail immediately after tx_off
	// — it accepts the power change but drops the mode change — so the rig is
	// left in the tune carrier's mode (task #270). The carrier is already down
	// before this pause, so it never affects the guaranteed stop. Config may
	// raise it up to maxTuneRestoreSettle.
	defaultTuneRestoreSettle = 150 * time.Millisecond
	maxTuneRestoreSettle     = 2 * time.Second
)

// ErrTuneStateUnknown is returned by StartTune when the bridge has not yet
// observed the rig's current mode + power, so it cannot guarantee the
// post-tune restore. Refusing is the safe choice: better no tune than a tune
// that can't put the rig back where it was. Resolves once the rig pushes
// state (AUTO mode) or the next READ snapshot lands.
var ErrTuneStateUnknown = stderr.New("bridge: current rig mode/power unknown; cannot tune safely")

// StartTune keys the tune carrier: set the carrier mode, drop to the tune
// power, key TX — as one atomic CAT line — then arm the hard auto-off
// backstop. Idempotent: a redundant call while already tuning is a no-op
// (single-flight). Refuses with ErrTuneStateUnknown if the current mode+power
// aren't known (can't guarantee the restore) and ErrRigNotConnected if no rig
// is up. On a failed keying write it rolls back so it never reports a tune the
// rig never entered.
func (s *Service) StartTune(ctx context.Context) error {
	const errOp errors.Op = "bridge.Service.StartTune"

	// keyMu: serialise against a concurrent release (and FT8 key). Held for the
	// whole key so a release that's still settling can't be raced by a new key.
	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		return errors.New(errOp).WithMsgf("no rig definition for driver %q", s.cfg.Cat.Driver)
	}
	// Encode before touching state — a missing tune command (rig can't tune)
	// fails loudly here rather than half-mutating tune state.
	on, err := s.encodeTuneOn(def)
	if err != nil {
		return errors.New(errOp).WithErr(err).WithMsg("encode tune-on")
	}
	// Never key a carrier we can't guarantee dropping (ADR 0027 guaranteed-stop).
	// encodeTuneOn proves set_mode/set_power/tx_on encode; prove tx_off encodes
	// too BEFORE keying, so a rigdef with tx_on but no usable tx_off is refused
	// here rather than keying TX and then stranding the carrier (review
	// 2026-06-04 H3).
	if _, err := encodeTuneUnkey(def); err != nil {
		return errors.New(errOp).WithErr(err).
			WithMsg("encode tune-off (refusing to key without a guaranteed unkey)")
	}

	s.mu.Lock()
	if s.tuneActive {
		s.mu.Unlock()
		return nil // single-flight: already tuning
	}
	// Shared single-flight with FT8 TX (ADR 0030): one keyed transmission at a
	// time on one rig/one PTT — refuse a tune while FT8 is keying.
	if s.ft8TxActive {
		s.mu.Unlock()
		// Typed so the API maps the mutual-exclusion conflict to 409
		// rig_tx_active, not a generic 500 (review 2026-06-19 L1).
		return errors.New(errOp).WithErr(ErrTxActive).WithMsg("ft8 tx active; refusing concurrent tune")
	}
	if s.lastMode == "" || s.lastPower <= 0 {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrTuneStateUnknown)
	}
	cl := s.activeClient
	if cl == nil {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigNotConnected)
	}
	// Never key TX on a rig whose identity isn't confirmed as the configured
	// driver — transmitting into the wrong / unverified rig is the most
	// dangerous case of H2.
	if !s.identityConfirmed {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigIdentityUnverified)
	}
	// Never key a rig the liveness strikes already read as non-responsive
	// (finding 3): keying a dead link strands the carrier with no working
	// unkey path.
	if !s.rigWritableLocked() {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigNotConnected).WithMsg("rig not responding (liveness strikes)")
	}
	// Never key while a PRIOR transmission is unconfirmed (ADR 0051).
	if s.txUncertain {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrTxUncertain)
	}
	// Snapshot the restore target NOW, before keying. captureTuneSnapshot
	// freezes lastMode/lastPower for the duration of the tune, so the rig's
	// own RTTY/tune-power pushes can't overwrite what we'll restore to.
	s.tuneRestoreMode = s.lastMode
	s.tuneRestorePower = s.lastPower
	s.tuneActive = true
	s.tuneStart = time.Now() // L4: duration baseline for the stop record
	// Arm the backstop together with tuneActive (atomic under mu) so the
	// guaranteed stop exists from the instant we commit to transmitting.
	// Bump the TX-transition generation and arm the backstop AGAINST it: the
	// callback may only release/re-arm its own generation (finding 6 — a stale
	// failed-release retry must not attach to a newer transmission).
	s.tuneGen++
	tuneGen := s.tuneGen
	s.tuneTimer = time.AfterFunc(s.tuneMaxDuration, func() { s.tuneAutoOff(tuneGen) })
	s.mu.Unlock()

	// CI-V waits for the rig's FB/FA so a rejected/dropped key fails here (and
	// rolls back) rather than reporting a carrier the rig never raised; Yaesu/
	// Kenwood is the unchanged fire-and-forget write.
	if err := s.writeKeyedLine(ctx, def, cl, on, "tune-on"); err != nil {
		// The write may have keyed the rig even though it returned an error: a
		// CI-V no-ACK (ErrCommandNoAck — "may or may not have applied") or a
		// watchdog-closed port that flushed the frame first. Rolling straight
		// back to idle would strand a possibly-live carrier with NO daemon
		// backstop — the auto-off timer is cancelled, and unkeyOnTeardown skips
		// a rig it believes is idle. So (ADR 0051) fire one best-effort tx_off
		// NOW and enter the uncertainty/confirmation cycle: the status query
		// answer clears it, silence escalates to the tx-alarm. A no-op if the
		// key never landed. The mode/power half of the atomic line may also
		// have applied; this path unkeys only (carrier-down is the safety
		// priority) and the post-INIT READ / operator reconciles mode+power.
		s.mu.Lock()
		s.tuneActive = false
		if s.tuneTimer != nil {
			s.tuneTimer.Stop()
			s.tuneTimer = nil
		}
		s.mu.Unlock()
		if off, oerr := encodeTuneUnkey(def); oerr == nil {
			// writeKeyedLine, NOT a raw WriteCommandBytes — the twin of
			// KeyFt8Tx's note: on CI-V only the awaited ACK proves the unkey
			// applied, and with no read_tx_status in the rigdef the confirm cycle
			// would otherwise let the next decoded frame "confirm" a lost stop
			// (2026-07-23 review P1).
			if werr := s.writeKeyedLine(context.Background(), def, cl, off, "post-failed-key defensive tx_off"); werr != nil {
				s.logger.ErrorWith().Err(werr).
					Msg("bridge: post-failed-key defensive tx_off failed — rig may be keyed; TX stays blocked")
				s.raiseTxAlarm(TxAlarmKeyWriteFailed)
				s.retryUnkeyStillKeyed()
				return errors.New(errOp).WithErr(err).WithMsg("write tune-on")
			}
			// Defensive tx_off ACCEPTED — twin of KeyFt8Tx: CI-V's awaited FB IS
			// positive RX confirmation, so confirm directly rather than entering
			// txUncertain and falling to the any-rig-data path (no read_tx_status
			// on the IC-7300), which would needlessly block writes and risk a
			// false alarm on a rig already known idle (50e35d review P2).
			if def.Protocol == cat.ProtocolIcomCIV {
				s.confirmTxIdle("civ ack (post-failed-key defensive tx_off)")
			} else {
				s.beginTxConfirm(def, cl)
			}
			return errors.New(errOp).WithErr(err).WithMsg("write tune-on")
		}
		// tx_off would not even encode (the pre-key gate proved it can): no
		// unkey was sent, so enter the uncertainty cycle and let the timeout
		// alarm rather than reporting idle.
		s.beginTxConfirm(def, cl)
		return errors.New(errOp).WithErr(err).WithMsg("write tune-on")
	}
	s.publishTuneState(true)
	// L4 durable start record: the normal operator path published SSE only, leaving no
	// log while the auto-off/disconnect teardowns did. reason/power/mode/auto-off match
	// the stop record (finishTune) so the whole tune lifecycle is uniform.
	s.logger.InfoWith().
		Str("reason", "operator").
		Int("power_w", s.tunePowerW).
		Str("mode", tuneCarrierMode).
		Int64("auto_off_seconds", int64(s.tuneMaxDuration.Seconds())).
		Msg("bridge: tune carrier started")
	return nil
}

// StopTune drops the tune carrier and restores the pre-tune mode + power.
// Operator-initiated; the auto-off timer and disconnect-release reach the same
// release path independently. Idempotent no-op when no tune is active.
func (s *Service) StopTune(ctx context.Context) error {
	return s.releaseTune(ctx, "operator")
}

// releaseTune is the single unkey-and-restore path shared by the operator stop
// and the auto-off backstop. It is a two-step sequence, NOT one wire line:
//
//  1. Unkey (tx_off) — the safety-critical step. On a failed write it leaves
//     tune state armed (active + timer) and returns the error so the backstop
//     keeps retrying; the carrier must never be stranded by a transient glitch.
//  2. Wait for POSITIVE RX confirmation (waitTxConfirm — CI-V's ACK resolves
//     immediately; Yaesu resolves via the read_tx_status answer), then settle,
//     then restore power + mode — best-effort. The confirmation gate is
//     safety-relevant (2026-07-19 review P1): the restore raises power back to
//     the operator level, so writing it on a mere fixed settle could feed a
//     still-keyed carrier full power. Unconfirmed → the restore is SKIPPED
//     (the alarm is already standing; clamped-power RTTY is the safe state).
//     The settle exists because some rigs (FTdx10) ignore a mode change during
//     the TX→RX transition tail right after tx_off (task #270); the pause lets
//     the rig return to RX so the restore lands. A failed restore write is
//     logged but does NOT re-arm — the carrier is down, which is what matters.
//
// A no-op when no tune is active, and a clean state-sync (no write) when the
// rig is already gone.
func (s *Service) releaseTune(ctx context.Context, reason string) error {
	return s.releaseTuneChecked(ctx, reason, nil)
}

// releaseTuneChecked is releaseTune with an optional generation guard for the
// auto-off backstop (review of a1d031cf): the callback's cheap pre-check runs
// BEFORE keyMu is acquired, so a stop + new key completing in that gap would
// let the stale callback unkey the NEWER transmission. wantGen re-checks
// UNDER keyMu — the lock every key/release transition holds — closing the
// window; nil (operator stop) releases unconditionally.
func (s *Service) releaseTuneChecked(ctx context.Context, reason string, wantGen *uint64) error {
	const errOp errors.Op = "bridge.Service.releaseTune"

	// keyMu: one release at a time, and no key may start mid-release. A second
	// release (operator stop racing the auto-off backstop) blocks here, then
	// observes tuneActive=false below and no-ops. Held across the settle +
	// restore so a stale restore can't land over a new transmission.
	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	s.mu.Lock()
	if wantGen != nil && s.tuneGen != *wantGen {
		// A newer transition owns the carrier — this stale backstop must not
		// touch it (its own generation's transmission is long gone).
		s.mu.Unlock()
		return nil
	}
	if !s.tuneActive {
		s.mu.Unlock()
		return nil
	}
	restoreMode := s.tuneRestoreMode
	restorePower := s.tuneRestorePower
	cl := s.activeClient
	settle := s.tuneRestoreSettle
	s.mu.Unlock()

	if cl == nil {
		// Rig gone — the carrier dropped with it (power-off / cable yank
		// physically unkeys). Nothing to write; just sync state.
		s.finishTune(reason)
		return nil
	}

	// Driver was validated at StartTune and can't change during a tune.
	def, _ := cat.Lookup(s.cfg.Cat.Driver)

	// Step 1 — unkey. Must encode (StartTune's pre-key gate guarantees it; if
	// somehow not, stay armed + loud rather than reporting a carrier down that
	// never got a tx_off). Must write (else stay armed for the backstop).
	unkey, err := encodeTuneUnkey(def)
	if err != nil {
		s.logger.ErrorWith().Err(err).Str("reason", reason).
			Msg("bridge: tune unkey encode failed; carrier may be keyed — refusing to report down")
		return errors.New(errOp).WithErr(err).WithMsg("encode tune-off")
	}
	// CI-V waits for the FB/FA: a rig that NAKs or never ACKs tx_off must not be
	// reported as unkeyed (that would cancel the auto-off backstop and strand the
	// carrier) — the error keeps tune armed so the backstop retries. Yaesu/Kenwood
	// is the unchanged fire-and-forget write.
	if err := s.writeKeyedLine(ctx, def, cl, unkey, "tune-off"); err != nil {
		s.logger.ErrorWith().Err(err).Str("reason", reason).
			Msg("bridge: tune unkey write failed; backstop will retry")
		return errors.New(errOp).WithErr(err).WithMsg("write tune-off")
	}
	// ADR 0051 confirm-or-alarm — see releaseFt8Tx's twin note.
	if def.Protocol == cat.ProtocolIcomCIV {
		s.confirmTxIdle("civ ack")
	} else {
		s.beginTxConfirm(def, cl)
	}

	// Step 2 gate (2026-07-19 review P1): the restore raises power from the
	// clamped tune level back to full operator power, so it may ONLY follow
	// POSITIVE RX confirmation. A rig that missed TX0 but still receives later
	// frames would otherwise get the PC restore while keyed — a full-power
	// carrier into the amp. Unconfirmed/alarmed: skip the restore entirely;
	// clamped-power RTTY plus the standing "check your radio" banner is the
	// safe state, and the operator restores by hand once the rig is verified.
	// CI-V confirmed synchronously above, so this returns immediately there.
	if !s.waitTxConfirm() {
		s.logger.ErrorWith().Str("reason", reason).
			Msg("bridge: skipping tune mode/power restore — unkey unconfirmed (rig may still be keyed)")
		s.invalidateTuneSnapshot() // rig is still at tune mode/power; the snapshot now lies
		s.finishTune(reason)
		return nil
	}

	// Carrier is confirmed down. Step 2 — settle, then best-effort restore.
	// DETACHED from the caller's ctx (F3): a cancelled request — e.g. the browser
	// disconnecting during the settle — must NOT skip the restore and strand the
	// rig in RTTY / tune-power. Like the auto-off backstop (which already passes
	// context.Background), the restore of the operator's pre-tune mode + power is
	// a correctness action that must complete regardless of the HTTP request's
	// lifetime. Step 1's unkey stays on the caller's ctx (a cancel there just
	// re-arms the backstop, which is safe); only this best-effort restore
	// detaches. The restore write is still bounded by the serial write watchdog,
	// so detaching cannot hang teardown.
	if settle > 0 {
		time.Sleep(settle)
	}
	if restore := encodeTuneRestore(def, restoreMode, restorePower); len(restore) > 0 {
		// Best-effort: the carrier is already down, so a failed/un-ACked restore is
		// logged, never surfaced (CI-V waits for the ACK; Yaesu fire-and-forget).
		if err := s.writeKeyedLine(context.Background(), def, cl, restore, "tune restore"); err != nil {
			s.logger.WarnWith().Err(err).Str("reason", reason).
				Msg("bridge: tune mode/power restore write failed (carrier already down)")
			s.invalidateTuneSnapshot() // same lie as the skip path above
		}
	}
	s.finishTune(reason)
	return nil
}

// finishTune clears active tune state, cancels the backstop, and tells
// subscribers the carrier is down. Called on a confirmed-sent unkey, or when
// the rig is already gone.
func (s *Service) finishTune(reason string) {
	s.mu.Lock()
	s.tuneActive = false
	s.tuneGen++ // invalidate any in-flight auto-off callback (finding 6)
	if s.tuneTimer != nil {
		s.tuneTimer.Stop()
		s.tuneTimer = nil
	}
	dur := time.Since(s.tuneStart)
	s.mu.Unlock()
	s.logTuneStopped(reason, dur)
	s.publishTuneState(false)
}

// logTuneStopped is the L4 durable stop record, shared by every teardown path
// (operator, auto_off, disconnect) so they read uniformly. reason names the path;
// power/mode mirror the start record; duration is the actual keyed time.
func (s *Service) logTuneStopped(reason string, dur time.Duration) {
	s.logger.InfoWith().
		Str("reason", reason).
		Int("power_w", s.tunePowerW).
		Str("mode", tuneCarrierMode).
		Int64("duration_ms", dur.Milliseconds()).
		Msg("bridge: tune carrier stopped")
}

// tuneAutoOff is the hard-backstop timer callback. It runs on a background
// context — the safety action must fire regardless of any HTTP request's
// lifetime. The unkey write itself is bounded by the serial write watchdog
// (bridge.timeouts.write_watchdog_ms → serial.Config.WriteTimeoutMS, review
// 2026-06-04 H4): a hung port.Write closes the port and returns ErrWriteTimeout
// rather than blocking forever, so this callback can't wedge. On a failed unkey
// it re-arms a short retry so the carrier is guaranteed to come down
// eventually: the timer loops until the unkey succeeds or the rig disconnects
// (clearTuneOnDisconnect cancels it; a watchdog-closed port surfaces as a
// terminal serial error → pipeline teardown → clearTuneOnDisconnect).
// gen is the TX-transition generation this callback was armed for: the
// callback releases and re-arms ONLY its own generation, so a stale retry
// (armed for a transmission that has since been stopped and replaced) can
// never unkey the newer one (finding 6). Timer.Stop can't cancel an
// already-executing callback — the generation check is what makes that safe.
func (s *Service) tuneAutoOff(gen uint64) {
	s.mu.Lock()
	stale := !s.tuneActive || s.tuneGen != gen
	s.mu.Unlock()
	if stale {
		return
	}
	// Pass the generation through: this cheap pre-check runs outside keyMu, so
	// the release re-verifies it under the lock (review of a1d031cf).
	if err := s.releaseTuneChecked(context.Background(), "auto-off", &gen); err != nil {
		s.mu.Lock()
		if s.tuneActive && s.tuneGen == gen {
			s.tuneTimer = time.AfterFunc(tuneRetryInterval, func() { s.tuneAutoOff(gen) })
		}
		s.mu.Unlock()
		return
	}
	// The stop record (finishTune, reason=auto-off) is the auto-off's log — no separate
	// line, so every teardown path reads uniformly (L4).
}

// clearTuneOnDisconnect releases tune state when the pipeline exits (rig
// powered off, cable yank, terminal serial error). The carrier physically
// drops with the rig, so there's nothing to unkey — this cancels the backstop,
// forgets the now-stale restore snapshot (a value from a previous rig session
// must not seed a later restore; the post-INIT READ repopulates on reconnect),
// and tells the SPA the carrier is down. Called from runPipeline's teardown.
func (s *Service) clearTuneOnDisconnect() {
	s.mu.Lock()
	wasActive := s.tuneActive
	s.tuneActive = false
	s.tuneGen++ // invalidate any in-flight auto-off callback (finding 6)
	if s.tuneTimer != nil {
		s.tuneTimer.Stop()
		s.tuneTimer = nil
	}
	dur := time.Since(s.tuneStart)
	s.clearRigSnapshotLocked()
	s.mu.Unlock()
	if wasActive {
		// L4 uniform stop record (reason=disconnect) — the carrier physically dropped
		// with the rig, so there is nothing to unkey; this is the durable teardown log.
		s.logTuneStopped("disconnect", dur)
		s.publishTuneState(false)
	}
}

// clearRigSnapshotLocked invalidates every rolling value that callers treat as
// authoritative current-rig state. It is used both on pipeline teardown and
// when passive no-data liveness reaches the disconnect threshold: the latter
// does not exit readLoop, so waiting for teardown would leave stale dial/power
// values available to FT8 logging and PSK Reporter. Caller holds s.mu.
func (s *Service) clearRigSnapshotLocked() {
	s.lastMode = ""
	s.lastPower = 0
	s.lastVfoA = 0
	s.lastVfoB = 0
	if s.selectedVfoExplicit {
		// A fresh Yaesu snapshot returns FA/FB/VS as separate replies. Keep the
		// operating VFO unknown until VS arrives; defaulting an empty selection
		// to A would briefly expose the wrong dial when the rig is on VFO-B.
		s.lastSelectedVfo = ""
		s.lastSelectedVfoKnown = false
	} else {
		// Definitions without SELECT use the operating-VFO-A model (currently
		// the IC-7300), so its first VFO-A frequency is authoritative.
		s.lastSelectedVfo = "A"
		s.lastSelectedVfoKnown = true
	}
}

// invalidateTuneSnapshot forgets the mode+power snapshot because the rig is
// KNOWN to no longer match it: the tune restore was skipped (unkey unconfirmed)
// or its write failed, so the rig is still sitting at RTTY / clamped tune power
// while lastMode+lastPower describe the pre-tune state.
//
// Leaving them was a quiet data bug (2026-07-23 review P2): captureTuneSnapshot
// is frozen for the whole tune, so nothing corrected them afterwards, and
// CurrentPowerW feeds TX_PWR on every logged FT8 QSO — contacts would have been
// stamped with the operator's normal power while the rig ran at 20 W. Clearing
// is the honest state and self-heals: the next decoded rig-state refills both
// (tuneActive is false by now), and until then StartTune refuses to key a rig
// whose restore target is unknown, which is the behaviour we want anyway.
func (s *Service) invalidateTuneSnapshot() {
	s.mu.Lock()
	s.lastMode = ""
	s.lastPower = 0
	s.mu.Unlock()
}

// captureTuneSnapshot records the rig's current main mode + power so a later
// tune can restore them. Fed by every rig-state the readLoop decodes. Frozen
// while a tune is active — the rig's own RTTY/tune-power pushes must not
// overwrite the values StartTune will restore to.
func (s *Service) captureTuneSnapshot(p RigStatePayload) {
	if p.Mode == "" && p.Power == 0 {
		return
	}
	s.mu.Lock()
	if !s.tuneActive {
		if p.Mode != "" {
			s.lastMode = p.Mode
		}
		if p.Power != 0 {
			s.lastPower = p.Power
		}
	}
	s.mu.Unlock()
}

// CurrentPowerW returns the rig's last-known TX power in watts, or 0 if unknown
// (no power decoded yet, or CAT down). Reads the rolling current-rig snapshot
// the read loop maintains. The FT8 QSO sink (cmd/smd) calls this to stamp
// TX_PWR on a completed contact, so internal/ft8 gets the rig power by
// dependency injection without importing internal/bridge (ADR 0013 scope).
func (s *Service) CurrentPowerW() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rigWritableLocked() {
		return 0
	}
	return s.lastPower
}

// captureDialFreq records the rig's current per-VFO dial frequency + selected VFO
// from each decoded rig-state. Unlike captureTuneSnapshot it is NOT frozen during a
// tune/TX — the operating frequency does not move while keyed, and we always want the
// latest. Partial payloads merge (a field absent from this rig-state keeps its prior
// value), mirroring the SPA's rig-state merge.
func (s *Service) captureDialFreq(p RigStatePayload) {
	if p.VfoA == 0 && p.VfoB == 0 && p.SelectedVfo == "" {
		return
	}
	s.mu.Lock()
	if p.VfoA != 0 {
		s.lastVfoA = p.VfoA
	}
	if p.VfoB != 0 {
		s.lastVfoB = p.VfoB
	}
	if p.SelectedVfo == "A" || p.SelectedVfo == "B" {
		s.lastSelectedVfo = p.SelectedVfo
		s.lastSelectedVfoKnown = true
	}
	s.mu.Unlock()
}

// CurrentDialMHz returns the rig's last-known operating (selected-VFO) dial frequency
// in MHz, or ok=false if no frequency has been decoded yet (or CAT is down). The FT8
// QSO sink (cmd/smd) uses this as the authoritative logged frequency — the rig is on
// frequency, whereas the SPA's start-time snapshot is captured once and reused across
// a whole Call-CQ pile-up, so it can be stale (e.g. logged before the IC-7300's freq
// poll landed). Injected the same way as CurrentPowerW, so internal/ft8 needs no
// internal/bridge import (ADR 0013 scope).
func (s *Service) CurrentDialMHz() (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.rigWritableLocked() {
		return 0, false
	}
	if !s.lastSelectedVfoKnown {
		return 0, false
	}
	// Knownness is per-VFO (2026-07-19 review P2): 0 Hz means that VFO has
	// never been decoded this session. The SELECTED VFO must itself be known —
	// falling back to the other one would log a QSO on the wrong frequency,
	// which is worse than reporting unknown (the caller then omits/uses its
	// own fallback).
	hz := s.lastVfoA
	if s.lastSelectedVfo == "B" {
		hz = s.lastVfoB
	}
	if hz == 0 {
		return 0, false
	}
	return float64(hz) / 1_000_000, true
}

// publishTuneState fans a tune-state event to subscribers (and the hub's
// one-slot cache, so a SPA tab opening mid-tune still learns the carrier is
// up). Daemon-authoritative: an auto-off the operator didn't trigger reaches
// the SPA the same way an operator stop does.
func (s *Service) publishTuneState(active bool) {
	s.hub.publish(Event{Name: EventTuneState, Payload: TuneStatePayload{Active: active}})
}

// encodeTuneOn builds the atomic tune-on CAT line: set the carrier mode, set
// the tune power, key TX — in that order, so the carrier comes up already in
// RTTY at the tune power rather than briefly in the old mode/power. set_mode +
// set_power go through EncodeCommand (value_map + pad + Exposed gate); tx_on
// through the low-level Encode (it is intentionally not Exposed).
func (s *Service) encodeTuneOn(def cat.RigDefinition) ([]byte, error) {
	mode, err := cat.EncodeCommand(def, tuneModeCommand, tuneCarrierMode)
	if err != nil {
		return nil, err
	}
	power, err := cat.EncodeCommand(def, tunePowerCommand, strconv.Itoa(s.tunePowerW))
	if err != nil {
		return nil, err
	}
	tx, err := cat.Encode(def, tuneTxOnCommand)
	if err != nil {
		return nil, err
	}
	line := make([]byte, 0, len(mode)+len(power)+len(tx))
	line = append(line, mode...)
	line = append(line, power...)
	line = append(line, tx...)
	return line, nil
}

// encodeTuneUnkey builds the safety-critical unkey command (tx_off). It MUST
// encode — a rigdef with tx_on but no usable tx_off is refused at StartTune
// (review 2026-06-04 H3), and the release path stays armed + loud if it ever
// can't unkey. Sent on its own (not concatenated with the restore) so the
// carrier drops before the TX→RX settle that precedes the mode restore.
func encodeTuneUnkey(def cat.RigDefinition) ([]byte, error) {
	return cat.Encode(def, tuneTxOffCommand)
}

// encodeTuneRestore builds the best-effort post-unkey restore line: restore
// power, then restore mode. Sent AFTER the carrier is down and the rig has
// settled back to RX (task #270 — the FTdx10 ignores a mode change during the
// TX→RX transition tail). Best-effort: the values came from decoded rig pushes
// so they encode, but a defensive miss is dropped, never surfaced — by the time
// this runs the carrier is already down, so nothing here is safety-critical. A
// package func with no Service state so it's trivially unit-testable. Returns
// an empty slice when there's nothing to restore (unknown mode + power).
func encodeTuneRestore(def cat.RigDefinition, restoreMode string, restorePower int) []byte {
	var line []byte
	if restorePower > 0 {
		if p, err := cat.EncodeCommand(def, tunePowerCommand, strconv.Itoa(restorePower)); err == nil {
			line = append(line, p...)
		}
	}
	if restoreMode != "" {
		if m, err := cat.EncodeCommand(def, tuneModeCommand, restoreMode); err == nil {
			line = append(line, m...)
		}
	}
	return line
}

// TuneSupported reports whether the rigdef can actually run the tune carrier.
// The config surface advertises this (BridgeInfo.Tune) so the SPA shows the
// Tune button only on a rig that can tune (ADR 0027).
//
// It advertises by ENCODEABILITY, not command-name presence: it dry-runs the
// exact encodes StartTune performs, so GET /v1/config cannot report tune:true
// for a rigdef whose set_mode/set_power is unexposed, broken, or missing its
// value-map, or whose tx_on/tx_off won't encode (review 2026-06-05 L1). The
// probe values mirror encodeTuneOn / encodeTuneUnkey exactly — the RTTY carrier
// mode + the default tune power through the Exposed-gated EncodeCommand, and
// tx_on/tx_off through the low-level Encode (they are deliberately not Exposed).
func TuneSupported(def cat.RigDefinition) bool {
	if _, err := cat.EncodeCommand(def, tuneModeCommand, tuneCarrierMode); err != nil {
		return false
	}
	if _, err := cat.EncodeCommand(def, tunePowerCommand, strconv.Itoa(defaultTunePowerW)); err != nil {
		return false
	}
	if _, err := cat.Encode(def, tuneTxOnCommand); err != nil {
		return false
	}
	if _, err := cat.Encode(def, tuneTxOffCommand); err != nil {
		return false
	}
	return true
}

// validateTunePowerForDef reports whether the rigdef can actually encode w as
// its set_power value. resolveTunePower's range check knows only the package
// ceilings, not the rig's own floor (2026-07-19 review P3: power_w:1 passed
// the clamp, but both tune-capable Yaesu defs carry min:5 — so config served
// tune:true while every StartTune failed at encode). The caller falls back to
// defaultTunePowerW on false — which TuneSupported already dry-runs, so an
// advertised tune capability implies the default encodes.
func validateTunePowerForDef(def cat.RigDefinition, w int) bool {
	_, err := cat.EncodeCommand(def, tunePowerCommand, strconv.Itoa(w))
	return err == nil
}

// resolveTunePower clamps the operator's configured tune power to the safe
// range (ADR 0027): zero/omitted → default 20 W; positive → honoured up to the
// 40 W ceiling. Returns the effective watts and whether the config value had
// to be clamped (so New can warn the operator their value was capped).
func resolveTunePower(cfgW int) (int, bool) {
	if cfgW <= 0 {
		return defaultTunePowerW, false
	}
	if cfgW > maxTunePowerW {
		return maxTunePowerW, true
	}
	return cfgW, false
}

// resolveTuneDuration clamps the configured auto-off backstop (ADR 0027):
// zero/omitted → default 15 s; positive → honoured up to the 30 s ceiling.
func resolveTuneDuration(cfgMs int) (time.Duration, bool) {
	if cfgMs <= 0 {
		return defaultTuneDuration, false
	}
	d := time.Duration(cfgMs) * time.Millisecond
	if d > maxTuneDuration {
		return maxTuneDuration, true
	}
	return d, false
}

// resolveTuneRestoreSettle clamps the configured unkey→restore settle (task
// #270): zero/omitted → default 150 ms; positive → honoured up to the 2 s
// ceiling. The ceiling guards a config typo from making every tune-off appear
// to hang (the carrier is already down during the settle, so it's not a safety
// bound — just a responsiveness one).
func resolveTuneRestoreSettle(cfgMs int) (time.Duration, bool) {
	if cfgMs <= 0 {
		return defaultTuneRestoreSettle, false
	}
	d := time.Duration(cfgMs) * time.Millisecond
	if d > maxTuneRestoreSettle {
		return maxTuneRestoreSettle, true
	}
	return d, false
}
