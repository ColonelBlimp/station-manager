package bridge

// Alarm recovery — how a standing tx-alarm gets back to idle.
//
// The alarm latches itself out of every clear path (2026-07-21 dogfood incident,
// mechanism confirmed 2026-07-23). confirmTxIdle is the only clear, and it needs
// an observed TXSTATUS; the only issuer of the status query was beginTxConfirm,
// reachable solely from an FT8/tune unkey — but KeyFt8Tx, StartTune AND the
// generic SendCommands path all refuse while txUncertain, which the alarm holds.
// read_tx_status is not in the rigdefs' periodic READ set, and observeRigData's
// liveness fallback is deliberately disabled for defs that HAVE a status query.
// So on an FTdx10/FT-710 an alarm could only ever be retired by an unsolicited
// AI push or a pipeline reconnect — and a rig front-panel power-cycle triggers
// neither, because the CP2105 stays USB-enumerated. The operator sat in front of
// an undismissable "CHECK YOUR RADIO" banner for thirteen minutes.
//
// The fix is MORE EVIDENCE, never a manual override. A rig with read_tx_status
// is asked for its actual state; the answer flows through readLoop →
// observeTxStatus. CI-V has no such query, but it does ACK commands, so its
// recovery operation safely re-asserts tx_off and clears only after the awaited
// FB ACK. A stop command cannot key or retune the rig, and the ACK proves that
// the command reached and was accepted by the addressed radio.
//
// What is deliberately NOT here: any way to clear txUncertain or to publish
// tx-alarm{active:false} by operator request. Clearing uncertainty would
// re-enable keying while the rig may still be transmitting — the ADR 0051
// guarantee — and publishing an inactive alarm without evidence would retire the
// only standing warning for every SPA tab and, via the hub cache, every future
// subscriber. Operator acknowledgement stays local to the UI (frontend/app's
// TxAlarmBanner hides the banner client-side without touching daemon state).

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// Alarm recovery cadence. The first attempt is near-immediate because the common
// case is an alarm raised by a confirm TIMEOUT on a rig that is actually idle
// (a busy bus swallowed the answer) — that clears in well under a second. The
// repeats then cover a rig that recovers shortly afterwards.
//
// Bounded on purpose: writing forever into a dead link is undesirable, and past
// a few minutes the situation needs the operator. On expiry the loop says so in
// the log, and manual RecheckTx remains available for as long as the alarm
// stands—including CI-V, where it performs another ACK-awaited safe tx_off.
var (
	txAlarmProbeDelay    = 250 * time.Millisecond
	txAlarmProbeInterval = 5 * time.Second
	txAlarmProbeAttempts = 60 // ≈5 minutes at the interval above
)

// probeTxRecovery obtains protocol-appropriate positive RX evidence while the
// ordinary command gates are closed by txUncertain: read_tx_status where the
// rigdef offers it, otherwise an ACK-awaited CI-V tx_off. Definitions with
// neither evidence mechanism remain unsupported.
func (s *Service) probeTxRecovery(reason string) error {
	const op errors.Op = "bridge.probeTxRecovery"

	s.mu.Lock()
	cl := s.activeClient
	s.mu.Unlock()

	driver := ""
	if s.cfg.Cat != nil {
		driver = s.cfg.Cat.Driver
	}
	def, ok := cat.Lookup(driver)
	if !ok {
		return errors.New(op).WithMsgf("no rig definition for driver %q", driver)
	}
	if cat.HasCommand(def, readTxStatusCommand) {
		return s.probeTxStatusOn(cl, reason)
	}
	if def.Protocol == cat.ProtocolIcomCIV {
		return s.reassertCIVTxOffOn(cl, def, reason)
	}
	return errors.New(op).WithErr(ErrTxRecheckUnsupported)
}

// probeTxStatusOn sends a fire-and-forget TX-state query against a
// caller-pinned client. The ANSWER is delivered independently by readLoop to
// observeTxStatus; merely putting the query on the wire clears nothing.
func (s *Service) probeTxStatusOn(cl serial.Client, reason string) error {
	const op errors.Op = "bridge.probeTxStatus"

	s.mu.Lock()
	idOK := s.identityConfirmed
	current := cl != nil && s.activeClient == cl
	s.mu.Unlock()

	if !current {
		return errors.New(op).WithErr(ErrRigNotConnected)
	}
	// Same H2 gate the write paths use: never drive a rig whose identity is
	// unconfirmed. A query is harmless in itself, but its ANSWER would be
	// interpreted as this rig's TX state and could clear the alarm on evidence
	// from the wrong radio.
	if !idOK {
		return errors.New(op).WithErr(ErrRigIdentityUnverified)
	}

	def, ok := cat.Lookup(s.cfg.Cat.Driver)
	if !ok {
		return errors.New(op).WithMsgf("no rig definition for driver %q", s.cfg.Cat.Driver)
	}
	if !cat.HasCommand(def, readTxStatusCommand) {
		// Nothing to ask with. These defs confirm through observeRigData's
		// any-data fallback instead, so there is no query to re-send.
		return errors.New(op).WithErr(ErrTxRecheckUnsupported)
	}
	q, err := cat.Encode(def, readTxStatusCommand)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("encode tx-status query")
	}
	if err := cl.WriteCommandBytes(context.Background(), q); err != nil {
		return errors.New(op).WithErr(err).WithMsg("write tx-status query")
	}
	s.logger.DebugWith().Str("reason", reason).Msg("bridge: tx-status re-probe sent")
	return nil
}

// reassertCIVTxOffOn is the CI-V evidence path for a standing alarm. CI-V has
// no read_tx_status command, so the only positive safe evidence available is an
// FB ACK to tx_off itself. keyMu binds the complete write+ACK+state transition
// to cl: pipeline teardown takes the same lock before replacing activeClient,
// preventing an old connection's late ACK from confirming its replacement.
func (s *Service) reassertCIVTxOffOn(cl serial.Client, def cat.RigDefinition, reason string) error {
	const op errors.Op = "bridge.reassertCIVTxOff"

	off, err := encodeTuneUnkey(def)
	if err != nil {
		return errors.New(op).WithErr(err).WithMsg("encode CI-V tx_off")
	}

	s.keyMu.Lock()
	defer s.keyMu.Unlock()

	s.mu.Lock()
	current := cl != nil && s.activeClient == cl
	idOK := s.identityConfirmed
	s.mu.Unlock()
	if !current {
		return errors.New(op).WithErr(ErrRigNotConnected)
	}
	if !idOK {
		return errors.New(op).WithErr(ErrRigIdentityUnverified)
	}
	if err := s.writeKeyedLine(context.Background(), def, cl, off, "tx-alarm recovery unkey"); err != nil {
		return errors.New(op).WithErr(err).WithMsg("write CI-V tx_off")
	}

	// Still under keyMu: the active pipeline cannot tear down or be replaced
	// between the ACK and the service-wide confirmation.
	s.confirmTxIdle("civ ack (" + reason + ")")
	return nil
}

// startAlarmProbes launches the bounded re-probe loop for a NEWLY raised alarm.
// Called from the two raise sites while they hold no lock, and only on a
// false→true transition, so a re-raise of an already-standing alarm does not
// stack loops. The generation snapshot is what actually enforces that: any
// clear (confirmTxIdle) or later raise bumps txAlarmProbeGen, and a loop whose
// generation is stale exits at its next tick without probing.
func (s *Service) startAlarmProbes() {
	s.mu.Lock()
	s.txAlarmProbeGen++
	gen := s.txAlarmProbeGen
	ctx := s.runCtx
	// No run context (alarm raised before Start, or after Stop) means nothing
	// could cancel the loop — don't start one.
	if ctx == nil || s.stopped {
		s.mu.Unlock()
		return
	}
	// wg.Add MUST happen under the same lock that observed s.stopped. Stop sets
	// that flag under mu and only then calls wg.Wait(); adding after releasing
	// the lock leaves a window where Stop has already begun waiting on a zero
	// counter, which is the documented WaitGroup misuse (panic, or a Stop that
	// returns while this goroutine still runs). Start() adds under mu for the
	// same reason.
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()

		timer := time.NewTimer(txAlarmProbeDelay)
		defer timer.Stop()

		for attempt := 1; attempt <= txAlarmProbeAttempts; attempt++ {
			select {
			case <-ctx.Done():
				return // Stop is waiting on the WaitGroup — exit now, not at the next tick
			case <-timer.C:
			}

			s.mu.Lock()
			live := s.txAlarmActive && s.txAlarmProbeGen == gen
			s.mu.Unlock()
			if !live {
				return // alarm cleared, or superseded by a newer one
			}

			if err := s.probeTxRecovery("alarm recovery"); err != nil {
				// Log the first failure and then stay quiet: an alarm raised
				// BECAUSE the link died would otherwise emit one warning every
				// interval for the whole bounded run.
				if attempt == 1 {
					s.logger.WarnWith().Err(err).
						Msg("bridge: automatic TX-alarm recovery failed; alarm stands")
				}
			}
			timer.Reset(txAlarmProbeInterval)
		}

		s.mu.Lock()
		stillAlarmed := s.txAlarmActive && s.txAlarmProbeGen == gen
		s.mu.Unlock()
		if stillAlarmed {
			s.logger.WarnWith().
				Msg("bridge: automatic TX-alarm recovery attempts exhausted; alarm stands — " +
					"use the re-check action once the rig is responding")
		}
	}()
}

// Re-unkey cadence for a rig that reports itself STILL TRANSMITTING. Spaced far
// enough apart to let the rig act on each stop and answer the follow-up query,
// bounded because past a few attempts the wire is not the problem and the
// operator's own intervention (and the rig's TOT) is what will end it.
var (
	txStopRetryAttempts = 4
	txStopRetryInterval = 400 * time.Millisecond
)

// retryUnkeyStillKeyed runs a short burst of STOP attempts after the rig has
// answered the status query with "CAT TX on", or after CI-V failed to ACK a
// defensive tx_off. Both mean the stop cannot yet be trusted (2026-07-23
// dogfood: a 2-second tune on 20m left the carrier up for two minutes, ending
// only when the operator switched the radio off).
//
// Raising the alarm was previously the WHOLE response to that answer, which is
// half a reaction: the rig has just told us it is transmitting when it should
// not be, and the guaranteed-stop discipline says the daemon's job is to keep
// trying to stop it, not merely to report it. Re-sending tx_off is the safest
// possible write — it carries no TX intent and cannot key anything — so unlike
// the generic command path there is no reason to withhold it while the
// transmitter's state is in doubt.
//
// Deliberately NOT a substitute for the alarm: the banner stays up, TX stays
// blocked, and only the rig's own "in RX" answer (via observeTxStatus →
// confirmTxIdle) retires either. This just gives the rig more chances to obey
// before the operator has to reach for the radio.
//
// Runs on its own goroutine because observeTxStatus is called FROM the readLoop
// and writeKeyedLine awaits a CI-V ACK that only the readLoop can deliver —
// writing inline would self-deadlock until the ACK timeout (the same reasoning
// as beginDefensiveRecovery). Single-flighted, since the re-probe loop means the
// "still keyed" answer can arrive repeatedly.
func (s *Service) retryUnkeyStillKeyed() {
	s.mu.Lock()
	if s.txStopRetrying || s.stopped || s.runCtx == nil {
		s.mu.Unlock()
		return
	}
	// The connection this sequence belongs to. Each attempt compares against it,
	// so a reconnect ends the sequence rather than letting it write to whatever
	// client happens to be current.
	startClient := s.activeClient
	if startClient == nil {
		s.mu.Unlock()
		return
	}
	s.txStopRetrying = true
	ctx := s.runCtx
	// Registered under the same lock that observed s.stopped — see startAlarmProbes.
	s.wg.Add(1)
	s.mu.Unlock()

	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			s.txStopRetrying = false
			s.mu.Unlock()
		}()

		def, ok := cat.Lookup(s.cfg.Cat.Driver)
		if !ok {
			return
		}
		off, err := encodeTuneUnkey(def)
		if err != nil {
			return
		}

		for attempt := 1; attempt <= txStopRetryAttempts; attempt++ {
			select {
			case <-ctx.Done():
				return
			case <-time.After(txStopRetryInterval):
			}
			// The rig may have obeyed an earlier attempt (or the operator may
			// have unkeyed at the radio) — confirmTxIdle clears uncertainty, and
			// there is nothing left to stop.
			if !s.TxUncertain() {
				return
			}
			// End the sequence if the client is gone OR has been REPLACED
			// (2026-07-23 review round 6). Checking only for nil was not enough:
			// the supervisor's reconnect backoff can be as short as 50 ms —
			// well inside this 400 ms retry interval — so a reconnect can
			// complete between attempts and leave a NON-nil, different client.
			// The old sequence would then spend its remaining budget writing to
			// the replacement while txStopRetrying suppressed the new
			// pipeline's own "still keyed" answers from starting a clean
			// sequence; if the budget ran out first, no further automatic unkey
			// happened at all and the carrier stood until the rig TOT.
			// Identity comparison — not just nil — is what makes the sequence
			// strictly per-connection.
			s.mu.Lock()
			cl := s.activeClient
			s.mu.Unlock()
			if cl == nil || cl != startClient {
				return
			}

			// keyMu: the release paths hold it across their own unkey+confirm,
			// so taking it here keeps this write from interleaving with theirs.
			s.keyMu.Lock()
			// Re-check BOTH connection identity and uncertainty under keyMu.
			// Pipeline teardown takes keyMu before replacing activeClient, so
			// this binds the write and its result to one pipeline instance.
			s.mu.Lock()
			current := s.activeClient == startClient
			uncertain := s.txUncertain
			s.mu.Unlock()
			if !current || !uncertain {
				s.keyMu.Unlock()
				return
			}
			werr := s.writeKeyedLine(context.Background(), def, cl, off, "stuck-tx re-unkey")
			if werr != nil {
				s.keyMu.Unlock()
				s.logger.ErrorWith().Err(werr).Int("attempt", attempt).
					Msg("bridge: safety re-unkey write failed; rig may still be keyed")
				continue
			}
			s.logger.WarnWith().Int("attempt", attempt).
				Msg("bridge: re-sent tx_off while TX state remained uncertain")
			if def.Protocol == cat.ProtocolIcomCIV {
				// writeKeyedLine awaited FB: unlike an unrelated state frame,
				// this ACK is positive evidence that the stop applied. Confirm
				// before releasing keyMu so an old pipeline cannot clear a
				// replacement pipeline's defensive-recovery uncertainty.
				s.confirmTxIdle("civ ack (stuck-tx re-unkey)")
				s.keyMu.Unlock()
				return
			}
			// Fire-and-forget protocols need a fresh confirmation cycle. It
			// asks read_tx_status when available, or explicitly arms the weak
			// post-write rig-data fallback for a no-query rigdef. Arm it before
			// releasing keyMu for the same connection-lifetime guarantee. If an
			// RX answer landed during the write, do not overwrite that all-clear
			// with a fresh uncertain cycle.
			s.beginTxConfirmIfUncertain(def, cl)
			s.keyMu.Unlock()
		}
	}()
}

// RecheckTx is the operator-initiated evidence attempt behind
// POST /v1/rig/tx/recheck. On a rig with TX status it asks the question again;
// on CI-V it re-asserts the safe tx_off and awaits its ACK. This remains
// available after the bounded automatic loop expires, so an IC-7300 that
// recovers later on the same USB connection is not stuck behind a permanent
// in-process alarm.
//
// It never clears by operator assertion. Only protocol evidence does that:
// TXSTATUS=RX or an accepted CI-V tx_off ACK. Restarting the automatic loop when
// the alarm remains is deliberate: the operator asking usually means the
// situation just changed, so it is worth watching again for a while.
func (s *Service) RecheckTx() error {
	const op errors.Op = "bridge.RecheckTx"
	if s == nil {
		return errors.New(op).WithErr(ErrRigNotConnected)
	}
	if err := s.probeTxRecovery("operator re-check"); err != nil {
		return errors.New(op).WithErr(err)
	}
	s.mu.Lock()
	alarmed := s.txAlarmActive
	s.mu.Unlock()
	if alarmed {
		s.startAlarmProbes()
	}
	return nil
}

// TxAlarmActive reports whether the stuck-TX alarm is standing. Read-only
// accessor for the API layer's re-check response, so the SPA can tell "asked
// and the rig answered idle" from "asked, still alarmed".
func (s *Service) TxAlarmActive() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txAlarmActive
}
