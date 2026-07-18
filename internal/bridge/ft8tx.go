package bridge

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// FT8-TX PTT keying (ADR 0030) — the second caller of the ADR 0027
// guaranteed-stop machinery. Where the tune controller keys a steady RTTY
// carrier, this keys PTT for one FT8 slot: the ft8 subsystem (via the
// ft8.TxKeyer seam, with this Service injected as the implementation) sets the
// data mode + keys here, plays ~12.6 s of GFSK audio out the sound card, then
// unkeys here. The same three guarantees converge on the unkey path:
//
//   - hard auto-off timer (ft8TxMaxDuration) — fires unconditionally if the ft8
//     controller never calls UnkeyFt8Tx (closed tab / killed probe / hung audio)
//   - release-on-disconnect — pipeline exit drops PTT (clearFt8TxOnDisconnect)
//   - single-flight, SHARED with tune — one keyed transmission at a time on one
//     rig/one PTT, so FT8 can never key over a tune carrier or vice versa
//
// tx_on/tx_off stay unexposed (ADR 0026); only this controller (and the tune
// one) may key the transmitter. TX power is left untouched — FT8 runs at the
// operator's normal power, not a clamped carrier level.

// ft8TxMaxDuration is the hard auto-off backstop for one FT8 transmission. A
// slot is 15 s and the keyed window (pre-key lead + ~12.6 s waveform) is ~13 s,
// so 18 s leaves margin for the controller's own unkey while bounding how long
// a hung audio device can strand PTT. A const for now (ADR 0030 makes the TX
// timing knobs config if real hardware needs it).
const ft8TxMaxDuration = 18 * time.Second

// ft8TxModeCommand sets the FT8 data mode (Exposed, value_map + pad), same
// command the tune controller uses for the carrier mode. tx_on/tx_off reuse the
// tune controller's (unexposed) command names.
const ft8TxModeCommand = "set_mode"

// KeyFt8Tx keys PTT for an FT8 slot: optionally switch to the FT8 data mode,
// then key TX — as one atomic CAT line — and arm the hard auto-off backstop.
// `mode` is the rig's data-mode literal (e.g. "DATA-U"); empty leaves the rig's
// current mode untouched. Idempotent: a redundant call while already keyed is a
// no-op (single-flight). Refuses if a tune is active (shared single-flight), if
// no rig is connected, or if the rig identity isn't confirmed. On a failed
// keying write it rolls back so it never reports TX the rig never entered.
func (s *Service) KeyFt8Tx(ctx context.Context, mode string) error {
	const errOp errors.Op = "bridge.Service.KeyFt8Tx"

	// keyMu: serialise against a concurrent release (and tune key). Held for the
	// whole key so a release that's still settling can't be raced by a new key.
	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		return errors.New(errOp).WithMsgf("no rig definition for driver %q", s.cfg.Cat.Driver)
	}
	// Encode before touching state — a missing tx_on (rig can't key) or, when a
	// mode switch is requested, a missing/broken set_mode fails loudly here
	// rather than half-mutating TX state.
	on, err := s.encodeFt8TxOn(def, mode)
	if err != nil {
		return errors.New(errOp).WithErr(err).WithMsg("encode ft8 tx-on")
	}
	// Never key without a guaranteed unkey (ADR 0027/0030 guaranteed-stop):
	// prove tx_off encodes BEFORE keying, so a rigdef with tx_on but no usable
	// tx_off is refused here rather than stranding PTT.
	if _, err := encodeTuneUnkey(def); err != nil {
		return errors.New(errOp).WithErr(err).
			WithMsg("encode ft8 tx-off (refusing to key without a guaranteed unkey)")
	}

	s.mu.Lock()
	if s.ft8TxActive {
		s.mu.Unlock()
		return nil // single-flight: already keyed
	}
	if s.tuneActive {
		s.mu.Unlock()
		// Typed (ErrTxActive) so callers can classify this mutual-exclusion
		// conflict rather than see a generic transmit failure (review L1).
		return errors.New(errOp).WithErr(ErrTxActive).WithMsg("tune carrier active; refusing concurrent FT8 TX")
	}
	cl := s.activeClient
	if cl == nil {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigNotConnected)
	}
	// Never key TX on a rig whose identity isn't confirmed as the configured
	// driver — transmitting into the wrong/unverified rig is the most dangerous
	// case (H2).
	if !s.identityConfirmed {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigIdentityUnverified)
	}
	// Never key a rig the liveness strikes already read as non-responsive
	// (finding 3) — see StartTune's twin gate.
	if !s.rigWritableLocked() {
		s.mu.Unlock()
		return errors.New(errOp).WithErr(ErrRigNotConnected).WithMsg("rig not responding (liveness strikes)")
	}
	// Snapshot the mode to restore on unkey (only meaningful when we switch it).
	// Unlike tune, an unknown prior mode is NOT a refusal: leaving the rig in the
	// FT8 data mode after a transmission is harmless (it is the operating mode),
	// so we proceed and simply skip the restore.
	if mode != "" {
		s.ft8TxRestoreMode = s.lastMode
	} else {
		s.ft8TxRestoreMode = ""
	}
	s.ft8TxActive = true
	// Arm the backstop together with ft8TxActive (atomic under mu) so the
	// guaranteed stop exists from the instant we commit to transmitting.
	// Generation-armed backstop — see StartTune's twin (finding 6).
	s.ft8TxGen++
	ft8Gen := s.ft8TxGen
	s.ft8TxTimer = time.AfterFunc(ft8TxMaxDuration, func() { s.ft8TxAutoOff(ft8Gen) })
	s.mu.Unlock()

	// CI-V waits for the rig's FB/FA so a rejected/dropped key fails here (and
	// rolls back) rather than reporting a key the rig never entered; Yaesu/Kenwood
	// is the unchanged fire-and-forget write. The mode frame (if any) ACKs before
	// tx_on is written, so tx_on never follows an unconfirmed mode switch.
	if err := s.writeKeyedLine(ctx, def, cl, on, "ft8 tx-on"); err != nil {
		// The write may have keyed the rig even though it returned an error: a
		// CI-V no-ACK (ErrCommandNoAck — "may or may not have applied") or a
		// watchdog-closed port that flushed the frame first. Rolling straight
		// back to idle would strand a possibly-live PTT with NO daemon backstop
		// — the auto-off timer is cancelled, and unkeyOnTeardown skips a rig it
		// believes is idle. So arm the ADR 0042 stranded-keyed flag:
		// defensiveUnkeyIfStranded then fires a guaranteed tx_off on the next
		// confirmed frame (this instance if the rig is still pushing, else the
		// next instance on reconnect). A no-op if the key never landed.
		s.mu.Lock()
		s.ft8TxActive = false
		if s.ft8TxTimer != nil {
			s.ft8TxTimer.Stop()
			s.ft8TxTimer = nil
		}
		s.strandedKeyed = true
		s.mu.Unlock()
		return errors.New(errOp).WithErr(err).WithMsg("write ft8 tx-on")
	}
	return nil
}

// UnkeyFt8Tx drops PTT and restores the pre-TX mode. Operator/controller-
// initiated; the auto-off backstop and disconnect-release reach the same path
// independently. Idempotent no-op when no FT8 TX is active.
func (s *Service) UnkeyFt8Tx(ctx context.Context) error {
	return s.releaseFt8Tx(ctx, "controller")
}

// releaseFt8Tx is the single unkey-and-restore path shared by the controller
// unkey and the auto-off backstop. Like releaseTune it is two steps, not one
// wire line:
//
//  1. Unkey (tx_off) — safety-critical. On a failed write it leaves TX state
//     armed and returns the error so the backstop keeps retrying; PTT must
//     never be stranded by a transient glitch.
//  2. Settle, then restore mode — best-effort. PTT is already down after step 1,
//     so the guaranteed stop is satisfied before the pause. The settle exists
//     because some rigs ignore a mode change during the TX→RX transition tail
//     right after tx_off (task #270, reusing tuneRestoreSettle).
func (s *Service) releaseFt8Tx(ctx context.Context, reason string) error {
	const errOp errors.Op = "bridge.Service.releaseFt8Tx"

	// keyMu: one release at a time, and no key may start mid-release. A second
	// release (operator unkey racing the auto-off backstop) blocks here, then
	// observes ft8TxActive=false below and no-ops. Held across the settle +
	// restore so a stale restore can't land over a new transmission.
	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	s.mu.Lock()
	if !s.ft8TxActive {
		s.mu.Unlock()
		return nil
	}
	restoreMode := s.ft8TxRestoreMode
	cl := s.activeClient
	settle := s.tuneRestoreSettle
	s.mu.Unlock()

	if cl == nil {
		// Rig gone — PTT dropped with it (power-off / cable yank physically
		// unkeys). Nothing to write; just sync state.
		s.finishFt8Tx()
		return nil
	}

	def, _ := cat.Lookup(s.cfg.Cat.Driver)

	// Step 1 — unkey. Must encode (KeyFt8Tx's pre-key gate guarantees it) and
	// must write (else stay armed for the backstop).
	unkey, err := encodeTuneUnkey(def)
	if err != nil {
		s.logger.ErrorWith().Err(err).Str("reason", reason).
			Msg("bridge: ft8 tx-off encode failed; PTT may be keyed — refusing to report down")
		return errors.New(errOp).WithErr(err).WithMsg("encode ft8 tx-off")
	}
	// CI-V waits for the FB/FA: a rig that NAKs or never ACKs tx_off must not be
	// reported as unkeyed (that would cancel the auto-off backstop and strand
	// PTT) — the error keeps TX armed so the backstop retries. Yaesu/Kenwood is
	// the unchanged fire-and-forget write.
	if err := s.writeKeyedLine(ctx, def, cl, unkey, "ft8 tx-off"); err != nil {
		s.logger.ErrorWith().Err(err).Str("reason", reason).
			Msg("bridge: ft8 tx-off write failed; backstop will retry")
		return errors.New(errOp).WithErr(err).WithMsg("write ft8 tx-off")
	}

	// PTT is down. Step 2 — settle, then best-effort mode restore. Respect ctx
	// cancellation during the pause (shutdown / disconnect): PTT is already down,
	// so skipping the restore is safe.
	if restoreMode != "" {
		if settle > 0 {
			select {
			case <-time.After(settle):
			case <-ctx.Done():
				s.finishFt8Tx()
				return nil
			}
		}
		if m, err := cat.EncodeCommand(def, ft8TxModeCommand, restoreMode); err == nil {
			// Best-effort: PTT is already down, so a failed/un-ACKed restore is
			// logged, never surfaced (CI-V waits for the ACK; Yaesu fire-and-forget).
			if err := s.writeKeyedLine(ctx, def, cl, m, "ft8 mode restore"); err != nil {
				s.logger.WarnWith().Err(err).Str("reason", reason).
					Msg("bridge: ft8 mode restore write failed (PTT already down)")
			}
		}
	}
	s.finishFt8Tx()
	return nil
}

// finishFt8Tx clears active TX state and cancels the backstop. Called on a
// confirmed-sent unkey, or when the rig is already gone.
func (s *Service) finishFt8Tx() {
	s.mu.Lock()
	s.ft8TxActive = false
	s.ft8TxGen++ // invalidate any in-flight auto-off callback (finding 6)
	if s.ft8TxTimer != nil {
		s.ft8TxTimer.Stop()
		s.ft8TxTimer = nil
	}
	s.mu.Unlock()
}

// ft8TxAutoOff is the hard-backstop timer callback (mirrors tuneAutoOff). Runs
// on a background context — the safety action must fire regardless of any
// request's lifetime. On a failed unkey it re-arms a short retry so PTT is
// guaranteed to come down eventually (the timer loops until the unkey succeeds
// or the rig disconnects).
// gen gates the callback to its own TX transition — see tuneAutoOff
// (finding 6).
func (s *Service) ft8TxAutoOff(gen uint64) {
	s.mu.Lock()
	stale := !s.ft8TxActive || s.ft8TxGen != gen
	s.mu.Unlock()
	if stale {
		return
	}
	if err := s.releaseFt8Tx(context.Background(), "auto-off"); err != nil {
		s.mu.Lock()
		if s.ft8TxActive && s.ft8TxGen == gen {
			s.ft8TxTimer = time.AfterFunc(tuneRetryInterval, func() { s.ft8TxAutoOff(gen) })
		}
		s.mu.Unlock()
		return
	}
	s.logger.InfoWith().Msg("bridge: ft8 tx auto-off fired; PTT dropped")
}

// clearFt8TxOnDisconnect releases FT8-TX state when the pipeline exits (rig
// powered off, cable yank, terminal serial error). PTT physically drops with
// the rig, so there's nothing to unkey — this just cancels the backstop and
// clears state. Called from runPipeline's teardown alongside
// clearTuneOnDisconnect.
func (s *Service) clearFt8TxOnDisconnect() {
	s.mu.Lock()
	wasActive := s.ft8TxActive
	s.ft8TxActive = false
	s.ft8TxGen++ // invalidate any in-flight auto-off callback (finding 6)
	if s.ft8TxTimer != nil {
		s.ft8TxTimer.Stop()
		s.ft8TxTimer = nil
	}
	s.mu.Unlock()
	if wasActive {
		s.logger.WarnWith().Msg("bridge: rig disconnected during ft8 tx; tx state cleared")
	}
}

// encodeFt8TxOn builds the atomic FT8 key line: optionally set the data mode,
// then key TX — in that order, so PTT comes up already in the data mode. When
// mode is empty only tx_on is sent (the operator manages mode). set_mode goes
// through EncodeCommand (value_map + pad + Exposed gate); tx_on through the
// low-level Encode (it is intentionally not Exposed).
func (s *Service) encodeFt8TxOn(def cat.RigDefinition, mode string) ([]byte, error) {
	var line []byte
	if mode != "" {
		m, err := cat.EncodeCommand(def, ft8TxModeCommand, mode)
		if err != nil {
			return nil, err
		}
		line = append(line, m...)
	}
	tx, err := cat.Encode(def, tuneTxOnCommand)
	if err != nil {
		return nil, err
	}
	line = append(line, tx...)
	return line, nil
}

// TxReady reports whether the bridge can key TX right now: a rig is connected,
// its identity is confirmed, and no keyed transmission already owns the PTT —
// the gates KeyFt8Tx (and StartTune) enforce. Including the tune/FT8 active
// flags means an active tune no longer reports "ready" to the FT8 arm/transmit
// gate (review 2026-06-16), so a caller waits rather than arming and then
// hitting a refused key. Exposed so a caller can poll readiness rather than
// discovering it via a refused key (the ft8-tx-probe polls it; the SPA TX gate
// reads it too).
func (s *Service) TxReady() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	// rigWritableLocked folds in the liveness strikes (finding 3): a rig the
	// bridge reads as non-responsive must not report TX-ready.
	return s.rigWritableLocked() && !s.tuneActive && !s.ft8TxActive
}

// Ft8TxSupported reports whether the rigdef can key PTT for FT8 — it dry-runs
// the exact encodes KeyFt8Tx/UnkeyFt8Tx perform (tx_on + tx_off through the
// low-level Encode, since they are deliberately not Exposed). The data-mode
// switch is optional (config-driven), so it is not part of the capability gate.
func Ft8TxSupported(def cat.RigDefinition) bool {
	if _, err := cat.Encode(def, tuneTxOnCommand); err != nil {
		return false
	}
	if _, err := cat.Encode(def, tuneTxOffCommand); err != nil {
		return false
	}
	return true
}
