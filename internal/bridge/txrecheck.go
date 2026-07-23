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
	const op errors.Op = "bridge.probeTxStatus"

	s.mu.Lock()
	cl := s.activeClient
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
	stopped := s.stopped
	s.mu.Unlock()

	// No run context (alarm raised before Start, or after Stop) means nothing
	// could cancel the loop — don't start one.
	if ctx == nil || stopped {
		return
	}

	s.wg.Add(1)
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
