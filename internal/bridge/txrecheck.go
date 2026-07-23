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
// The fix is MORE EVIDENCE, never a manual override. Everything here writes the
// rigdef's read_tx_status query and nothing else: a READ cannot key the rig, so
// it is the one operation that is safe to perform outside the key/command gates
// while the transmitter's state is unknown. The answer flows through the normal
// readLoop → observeTxStatus path, so an idle rig clears the alarm through
// confirmTxIdle exactly as it always did, on genuine positive evidence.
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

// Alarm re-probe cadence. The first probe is near-immediate because the common
// case is an alarm raised by a confirm TIMEOUT on a rig that is actually idle
// (a busy bus swallowed the answer) — that clears in well under a second. The
// repeats then cover a rig that recovers shortly afterwards.
//
// Bounded on purpose: probing forever would write into a dead link indefinitely,
// and past a few minutes the situation needs the operator, not another query. On
// expiry the loop says so in the log, and the manual re-check (RecheckTx) stays
// available for as long as the alarm stands.
var (
	txAlarmProbeDelay    = 250 * time.Millisecond
	txAlarmProbeInterval = 5 * time.Second
	txAlarmProbeAttempts = 60 // ≈5 minutes at the interval above
)

// probeTxStatus writes the rigdef's read_tx_status query, bypassing every
// key/command gate. Safe precisely because it is a read: it carries no TX
// intent, so unlike SendCommands it cannot retune or re-key a rig whose state
// is in doubt, and it is the only way to obtain the positive evidence
// confirmTxIdle requires while the alarm blocks everything else.
//
// Fire-and-forget, exactly like beginTxConfirm's query: the ANSWER is the
// confirmation, delivered by the readLoop to observeTxStatus. Returns an error
// only when the query could not be put on the wire at all — which is itself
// meaningful (a write path that cannot carry a query is a write path that could
// not have carried the unkey either), so callers report it rather than retrying
// harder.
func (s *Service) probeTxStatus(reason string) error {
	s.mu.Lock()
	cl := s.activeClient
	s.mu.Unlock()
	return s.probeTxStatusOn(cl, reason)
}

// probeTxStatusOn is probeTxStatus against a CALLER-PINNED client. The re-unkey
// sequence uses it so its whole stop-and-ask attempt stays on ONE connection: it
// validates the client, unkeys, then asks — and if a reconnect lands between the
// unkey and the question, re-resolving would send the question down the NEW
// connection while the old sequence still holds the single-flight latch, so the
// replacement's own "still keyed" answer could not start a fresh sequence
// (2026-07-23 review). Pinning makes the attempt connection-specific end to end.
func (s *Service) probeTxStatusOn(cl serial.Client, reason string) error {
	const op errors.Op = "bridge.probeTxStatus"

	s.mu.Lock()
	idOK := s.identityConfirmed
	s.mu.Unlock()

	if cl == nil {
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

			if err := s.probeTxStatus("alarm re-probe"); err != nil {
				// Log the first failure and then stay quiet: an alarm raised
				// BECAUSE the link died would otherwise emit one warning every
				// interval for the whole bounded run.
				if attempt == 1 {
					s.logger.WarnWith().Err(err).
						Msg("bridge: tx-status re-probe failed; the rig cannot be asked, alarm stands")
				}
			}
			timer.Reset(txAlarmProbeInterval)
		}

		s.mu.Lock()
		stillAlarmed := s.txAlarmActive && s.txAlarmProbeGen == gen
		s.mu.Unlock()
		if stillAlarmed {
			s.logger.WarnWith().
				Msg("bridge: automatic tx-status re-probes exhausted; alarm stands — " +
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

// retryUnkeyStillKeyed re-asserts the STOP after the rig has answered the status
// query with "CAT TX on" — positive evidence that the unkey did not take effect
// (2026-07-23 dogfood: a 2-second tune on 20m left the carrier up for two
// minutes, ending only when the operator switched the radio off).
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
			// Re-check under the lock: the rig may have confirmed idle while we
			// waited for keyMu. The window cannot be closed completely — a
			// confirmation landing between this check and the write still lets
			// one unkey through — and it does not need to be: an extra tx_off to
			// a rig already in RX is a no-op, which is exactly why re-asserting
			// the stop is safe to do at all.
			if !s.TxUncertain() {
				s.keyMu.Unlock()
				return
			}
			werr := s.writeKeyedLine(context.Background(), def, cl, off, "stuck-tx re-unkey")
			s.keyMu.Unlock()
			if werr != nil {
				s.logger.ErrorWith().Err(werr).Int("attempt", attempt).
					Msg("bridge: re-unkey write failed on a rig reporting itself still keyed")
				continue
			}
			s.logger.WarnWith().Int("attempt", attempt).
				Msg("bridge: re-sent tx_off — rig reported it was still transmitting")
			// Ask again so the answer, not the write, decides whether it worked.
			if perr := s.probeTxStatusOn(cl, "post re-unkey"); perr != nil {
				s.logger.DebugWith().Err(perr).Msg("bridge: post-re-unkey status probe failed")
			}
		}
	}()
}

// RecheckTx is the operator-initiated re-probe behind POST /v1/rig/tx/recheck.
// It asks the rig the same question the automatic loop asks, on demand, for the
// case the bounded loop has expired or the operator has just fixed something
// (power-cycled the rig, re-seated the cable) and wants an answer NOW.
//
// It CANNOT clear the alarm by itself, and that is the whole design: it only
// puts a query on the wire, and only the rig's own "I am in RX" answer —
// arriving through observeTxStatus → confirmTxIdle — retires the alarm. A
// successful return therefore means "asked", not "safe". Restarting the probe
// loop on a manual re-check is deliberate: the operator asking usually means
// the situation just changed, so it is worth watching again for a while.
func (s *Service) RecheckTx() error {
	const op errors.Op = "bridge.RecheckTx"
	if s == nil {
		return errors.New(op).WithErr(ErrRigNotConnected)
	}
	if err := s.probeTxStatus("operator re-check"); err != nil {
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
