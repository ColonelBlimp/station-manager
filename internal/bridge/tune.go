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
	// Snapshot the restore target NOW, before keying. captureTuneSnapshot
	// freezes lastMode/lastPower for the duration of the tune, so the rig's
	// own RTTY/tune-power pushes can't overwrite what we'll restore to.
	s.tuneRestoreMode = s.lastMode
	s.tuneRestorePower = s.lastPower
	s.tuneActive = true
	// Arm the backstop together with tuneActive (atomic under mu) so the
	// guaranteed stop exists from the instant we commit to transmitting.
	s.tuneTimer = time.AfterFunc(s.tuneMaxDuration, s.tuneAutoOff)
	s.mu.Unlock()

	if err := cl.WriteCommandBytes(ctx, on); err != nil {
		s.mu.Lock()
		s.tuneActive = false
		if s.tuneTimer != nil {
			s.tuneTimer.Stop()
			s.tuneTimer = nil
		}
		s.mu.Unlock()
		return errors.New(errOp).WithErr(err).WithMsg("write tune-on")
	}
	s.publishTuneState(true)
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
//  2. Settle, then restore power + mode — best-effort. The carrier is already
//     down after step 1, so the guaranteed stop is satisfied before the pause.
//     The settle exists because some rigs (FTdx10) ignore a mode change during
//     the TX→RX transition tail right after tx_off (task #270); the pause lets
//     the rig return to RX so the restore lands. A failed restore write is
//     logged but does NOT re-arm — the carrier is down, which is what matters.
//
// A no-op when no tune is active, and a clean state-sync (no write) when the
// rig is already gone.
func (s *Service) releaseTune(ctx context.Context, reason string) error {
	const errOp errors.Op = "bridge.Service.releaseTune"

	s.mu.Lock()
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
		s.finishTune()
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
	if err := cl.WriteCommandBytes(ctx, unkey); err != nil {
		s.logger.ErrorWith().Err(err).Str("reason", reason).
			Msg("bridge: tune unkey write failed; backstop will retry")
		return errors.New(errOp).WithErr(err).WithMsg("write tune-off")
	}

	// Carrier is down. Step 2 — settle, then best-effort restore. Respect ctx
	// cancellation during the pause (e.g. shutdown / client disconnect): the
	// carrier is already down, so skipping the restore is safe.
	if settle > 0 {
		select {
		case <-time.After(settle):
		case <-ctx.Done():
			s.finishTune()
			return nil
		}
	}
	if restore := encodeTuneRestore(def, restoreMode, restorePower); len(restore) > 0 {
		if err := cl.WriteCommandBytes(ctx, restore); err != nil {
			s.logger.WarnWith().Err(err).Str("reason", reason).
				Msg("bridge: tune mode/power restore write failed (carrier already down)")
		}
	}
	s.finishTune()
	return nil
}

// finishTune clears active tune state, cancels the backstop, and tells
// subscribers the carrier is down. Called on a confirmed-sent unkey, or when
// the rig is already gone.
func (s *Service) finishTune() {
	s.mu.Lock()
	s.tuneActive = false
	if s.tuneTimer != nil {
		s.tuneTimer.Stop()
		s.tuneTimer = nil
	}
	s.mu.Unlock()
	s.publishTuneState(false)
}

// tuneAutoOff is the hard-backstop timer callback. It runs on a background
// context — the safety action must fire regardless of any HTTP request's
// lifetime. NOTE: that background context does NOT bound the unkey write. The
// serial layer sets only a read timeout, and WriteCommandBytes checks ctx
// merely between writes, so a blocking port.Write here is not interruptible —
// a true write deadline is open work (review 2026-06-04 H4). On a failed unkey
// it re-arms a short retry so the carrier is guaranteed to come down
// eventually: the timer loops until the unkey succeeds or the rig disconnects
// (clearTuneOnDisconnect cancels it).
func (s *Service) tuneAutoOff() {
	if err := s.releaseTune(context.Background(), "auto-off"); err != nil {
		s.mu.Lock()
		if s.tuneActive {
			s.tuneTimer = time.AfterFunc(tuneRetryInterval, s.tuneAutoOff)
		}
		s.mu.Unlock()
		return
	}
	s.logger.InfoWith().Msg("bridge: tune auto-off fired; carrier dropped")
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
	if s.tuneTimer != nil {
		s.tuneTimer.Stop()
		s.tuneTimer = nil
	}
	s.lastMode = ""
	s.lastPower = 0
	s.mu.Unlock()
	if wasActive {
		s.logger.WarnWith().Msg("bridge: rig disconnected during tune; tune state cleared")
		s.publishTuneState(false)
	}
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

// TuneSupported reports whether the rigdef has every command the tune
// controller needs (set_mode, set_power, tx_on, tx_off). The config surface
// advertises this (BridgeInfo.Tune) so the SPA shows the Tune button only on a
// rig that can actually tune (ADR 0027).
func TuneSupported(def cat.RigDefinition) bool {
	return cat.HasCommand(def, tuneModeCommand) &&
		cat.HasCommand(def, tunePowerCommand) &&
		cat.HasCommand(def, tuneTxOnCommand) &&
		cat.HasCommand(def, tuneTxOffCommand)
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
