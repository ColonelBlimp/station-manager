package bridge

// TX-uncertainty tracking + the stuck-TX alarm (ADR 0051). The 2026-07-18
// incident proved a successful serial write is NOT a confirmed unkey: a
// stalled USB endpoint accepted every TX0 while the rig stayed keyed, and the
// bridge — treating write-success as carrier-down — cancelled its backstops
// and logged a clean session. The guarantee is now confirm-or-alarm:
//
//   - an unkey that was only WRITTEN (fire-and-forget protocols) moves the
//     service into txUncertain, not idle;
//   - positive confirmation clears it — a CI-V unkey confirms via its awaited
//     ACK (never enters uncertain); the Yaesu family confirms via the rigdef's
//     read_tx_status query ("TX;" → "TXn;"), whose ANSWER both proves the
//     write path is alive and reports RX directly;
//   - a def without a TX-status query falls back to any-rig-data-received
//     (an alive, pushing rig whose unkey was written is overwhelmingly
//     unkeyed — the documented ADR 0051 fallback, weaker by design);
//   - unconfirmed past txConfirmTimeout, a liveness strike-out while
//     keyed/uncertain, an unkey write error at teardown, or a key write that
//     MAY have keyed all raise the persistent tx-alarm the SPA renders as a
//     "check your radio" banner.
//
// Uncertainty is ONE service-level flag, not per-path: tune and FT8 TX are
// single-flight on one PTT, so at most one transmission can be the cause.
// The uncertainty/alarm track is deliberately parallel to the UI state
// (tune-state still reports "down" after the unkey write — the carrier is
// PROBABLY down and the button should reflect that); it changes what the
// daemon does (refuse new keys, alarm) rather than what the button shows.

import (
	"context"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/cat"
	"github.com/ColonelBlimp/station-manager/internal/serial"
)

// readTxStatusCommand is the optional rigdef query for the transmitter's
// actual state. Not Exposed — only this confirmation machinery sends it.
const readTxStatusCommand = "read_tx_status"

// txConfirmTimeout bounds how long an unconfirmed unkey may stay silent
// before the alarm fires. A healthy link answers the status query in tens of
// milliseconds; three seconds absorbs a busy half-duplex bus without leaving
// the operator watching a stuck carrier for long.
var txConfirmTimeout = 3 * time.Second

// TX-alarm i18n codes (ADR 0010: the SPA maps code → wording).
const (
	// TxAlarmUnconfirmed — an unkey was written but never confirmed.
	TxAlarmUnconfirmed = "tx_unconfirmed"
	// TxAlarmStillKeyed — the rig ANSWERED the status query with "CAT TX on":
	// the unkey definitively did not take effect.
	TxAlarmStillKeyed = "tx_still_keyed"
	// TxAlarmLivenessLost — CAT liveness struck out while a transmission was
	// keyed or unconfirmed: the control path died with the PTT possibly up.
	TxAlarmLivenessLost = "tx_liveness_lost"
	// TxAlarmTeardownUnconfirmed — the pipeline tore down while keyed and its
	// final unkey write failed.
	TxAlarmTeardownUnconfirmed = "tx_teardown_unconfirmed"
	// TxAlarmKeyWriteFailed — a key write errored in a way that may still
	// have keyed the rig (CI-V no-ACK, watchdog-closed port).
	TxAlarmKeyWriteFailed = "tx_key_write_failed"
)

// beginTxConfirm enters the uncertain state after an unkey (or possibly-keyed
// write failure) on a non-ACK protocol, sends the rigdef's TX-status query
// when it has one, and arms the confirm-timeout that raises the alarm. The
// query is fire-and-forget: its ANSWER (decoded by the readLoop →
// observeTxStatus) is the confirmation, and a failed query write simply
// leaves the state unconfirmed — escalating to the alarm at the timeout,
// which is the correct reading of a write path that can't even carry a query.
func (s *Service) beginTxConfirm(def cat.RigDefinition, cl serial.Client) {
	s.mu.Lock()
	s.txUncertain = true
	s.txConfirmGen++
	gen := s.txConfirmGen
	// Watermark: the liveness fallback may only confirm on frames decoded
	// after this point (8bd88c1b review - a pre-unkey frame proves nothing).
	s.txConfirmAfterFrame = s.rxFrameCount.Load()
	if s.txConfirmTimer != nil {
		s.txConfirmTimer.Stop()
	}
	s.txConfirmTimer = time.AfterFunc(txConfirmTimeout, func() { s.txConfirmTimeout(gen) })
	// A superseded cycle's waiters wake now and read the (still-uncertain)
	// state — a stale release must not sleep out its full grace timeout.
	s.closeTxConfirmDoneLocked()
	s.txConfirmDone = make(chan struct{})
	s.mu.Unlock()

	if !cat.HasCommand(def, readTxStatusCommand) || cl == nil {
		return // liveness fallback: observeRigData confirms
	}
	q, err := cat.Encode(def, readTxStatusCommand)
	if err != nil {
		s.logger.WarnWith().Err(err).Msg("bridge: tx-status query encode failed; relying on confirm timeout")
		return
	}
	if err := cl.WriteCommandBytes(context.Background(), q); err != nil {
		s.logger.WarnWith().Err(err).Msg("bridge: tx-status query write failed; relying on confirm timeout")
	}
}

// txConfirmTimeout fires when an unkey stayed unconfirmed. Generation-gated
// like the auto-off backstops: a stale timer (confirmation arrived and a NEW
// uncertainty window opened) must not alarm the wrong transition.
func (s *Service) txConfirmTimeout(gen uint64) {
	s.mu.Lock()
	fire := s.txUncertain && s.txConfirmGen == gen && !s.txAlarmActive
	if fire {
		s.txAlarmActive = true
		s.closeTxConfirmDoneLocked() // cycle resolved (alarmed): wake waiters
	}
	s.mu.Unlock()
	if fire {
		s.publishTxAlarm(true, TxAlarmUnconfirmed)
	}
}

// closeTxConfirmDoneLocked wakes any waitTxConfirm callers for the current
// cycle. Caller holds s.mu.
func (s *Service) closeTxConfirmDoneLocked() {
	if s.txConfirmDone != nil {
		close(s.txConfirmDone)
		s.txConfirmDone = nil
	}
}

// waitTxConfirm blocks until the pending confirmation cycle resolves and
// reports whether the transmitter was POSITIVELY confirmed idle. The release
// paths call it before their best-effort restore (2026-07-19 review P1): a
// full-power restore written on a fixed settle while the rig is still keyed —
// missed TX0 but a live enough link to deliver the PC frame — would raise a
// keyed carrier from clamped tune power to operator power. false means
// unconfirmed or alarmed: the caller must SKIP the restore (clamped-power RTTY
// plus the standing alarm banner is the safe state).
//
// The cycle self-resolves within txConfirmTimeout; the extra grace guards a
// beginTxConfirm/wait interleaving so a missed channel can't block forever.
// Returns immediately when no cycle is pending (CI-V's ACK already confirmed).
func (s *Service) waitTxConfirm() bool {
	s.mu.Lock()
	ch := s.txConfirmDone
	s.mu.Unlock()
	if ch != nil {
		select {
		case <-ch:
		case <-time.After(txConfirmTimeout + time.Second):
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.txUncertain && !s.txAlarmActive
}

// raiseTxAlarm latches the alarm with the given code (idempotent while
// already alarmed) and marks the state uncertain — every alarm means "the
// rig may be transmitting."
func (s *Service) raiseTxAlarm(code string) {
	s.mu.Lock()
	already := s.txAlarmActive
	s.txAlarmActive = true
	s.txUncertain = true
	s.closeTxConfirmDoneLocked() // cycle resolved (alarmed): wake waiters
	s.mu.Unlock()
	if !already {
		s.publishTxAlarm(true, code)
	}
}

// confirmTxIdle clears uncertainty (and any standing alarm) on positive
// evidence the transmitter is in RX.
func (s *Service) confirmTxIdle(how string) {
	s.mu.Lock()
	wasUncertain := s.txUncertain
	wasAlarmed := s.txAlarmActive
	s.txUncertain = false
	s.txAlarmActive = false
	s.txConfirmGen++ // invalidate any in-flight confirm-timeout
	if s.txConfirmTimer != nil {
		s.txConfirmTimer.Stop()
		s.txConfirmTimer = nil
	}
	s.closeTxConfirmDoneLocked() // cycle resolved (idle): wake waiters
	s.mu.Unlock()
	if wasAlarmed {
		s.publishTxAlarm(false, "")
	}
	if wasUncertain {
		s.logger.InfoWith().Str("how", how).Msg("bridge: tx state confirmed idle")
	}
}

// observeTxStatus consumes a decoded TXSTATUS push — the answer to the
// read_tx_status query (or an unsolicited status push). Yaesu semantics:
// "0" = RX, "1" = CAT-commanded TX, "2" = TX by other means (mic PTT,
// footswitch — not ours to manage; our CAT TX is off, which is what the
// unkey promised).
func (s *Service) observeTxStatus(v string) {
	s.mu.Lock()
	uncertain := s.txUncertain
	s.mu.Unlock()
	if !uncertain {
		return
	}
	switch v {
	case "0", "2":
		s.confirmTxIdle("tx-status " + v)
	case "1":
		s.logger.ErrorWith().Msg("bridge: rig reports CAT TX still keyed after unkey — CHECK YOUR RADIO")
		s.raiseTxAlarm(TxAlarmStillKeyed)
	}
}

// observeRigData is the liveness-fallback confirmation for rigdefs WITHOUT a
// TX-status query: any successfully decoded rig data after the unkey write.
// Deliberately NOT used when the def can answer properly — a half-dead link
// (reads alive, writes stalled) would look "confirmed" on mere pushes, and
// the status query exists precisely to prove the write path.
func (s *Service) observeRigData() {
	s.mu.Lock()
	confirm := s.txUncertain && !s.hasTxStatusQuery &&
		s.rxFrameCount.Load() > s.txConfirmAfterFrame
	s.mu.Unlock()
	if confirm {
		s.confirmTxIdle("rig data (no tx-status query in rigdef)")
	}
}

// publishTxAlarm emits the tx-alarm event (hub-cached for late subscribers)
// and mirrors it to the log at a severity matching its weight.
func (s *Service) publishTxAlarm(active bool, code string) {
	if active {
		s.logger.ErrorWith().Str("code", code).
			Msg("bridge: TX ALARM — the rig may still be transmitting; check the radio")
	} else {
		s.logger.InfoWith().Msg("bridge: tx alarm cleared (transmitter confirmed idle)")
	}
	s.hub.publish(Event{Name: EventTxAlarm, Payload: TxAlarmPayload{Active: active, Code: code}})
}

// TxUncertain reports whether an unconfirmed transmission may own the PTT —
// the key paths refuse while this is true (keying over a possibly-keyed rig
// would defeat the single-flight guarantee the flags can't see).
func (s *Service) TxUncertain() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.txUncertain
}

// beginDefensiveRecovery is the identity-confirmation hook (ADR 0051 as
// hardened by the 8bd88c1b review): EVERY newly confirmed connection runs one
// defensive unkey through the FULL confirm-or-alarm cycle — unconditionally,
// because a fresh process has no memory of how the previous one died, and the
// silently-discarded-write failure mode applies to the defensive TX0 exactly
// as it does to any other write. Ordering is the safety argument:
//
//  1. txUncertain is set BEFORE identityConfirmed — the key/command gates
//     refuse while uncertain, so nothing can race into the recovery window
//     (the old busy-skip + one-shot latch could silently drop the recovery).
//  2. identity unlocks (write paths legal, H2 satisfied).
//  3. the defensive tx_off goes out under keyMu — on a GOROUTINE, because
//     this hook is called FROM the readLoop and the CI-V write awaits an ACK
//     only the readLoop can deliver (synchronous = self-deadlock until the
//     ACK timeout). The state transitions above are synchronous, so the
//     ordering guarantee holds regardless; the write itself is bounded by
//     the serial write watchdog + the CI-V ACK timeout, so the untracked
//     goroutine cannot linger (a Stop mid-write just fails the write, and
//     alarm publishes on a closed hub are no-ops).
//  4. CI-V's awaited ACK confirms directly; otherwise the status-query cycle
//     runs — and its write failure or silence ALARMS, with TX still blocked.
func (s *Service) beginDefensiveRecovery(def cat.RigDefinition, client serial.Client) {
	txOff, encErr := cat.Encode(def, tuneTxOffCommand)
	if encErr != nil {
		// Rigdef has no TX capability — nothing to defend against, nothing
		// to block. Identity unlocks normally.
		s.setIdentityConfirmed(true)
		return
	}

	s.mu.Lock()
	s.txUncertain = true // pre-block: keys/commands refuse from this instant
	s.mu.Unlock()
	s.setIdentityConfirmed(true)

	go func() {
		s.keyMu.Lock()
		werr := s.writeKeyedLine(context.Background(), def, client, txOff, "defensive unkey")
		s.keyMu.Unlock()
		if werr != nil {
			s.logger.ErrorWith().Err(werr).
				Msg("bridge: defensive unkey write failed — rig may be keyed from a prior life; TX stays blocked")
			s.raiseTxAlarm(TxAlarmUnconfirmed)
			return
		}
		if def.Protocol == cat.ProtocolIcomCIV {
			// writeKeyedLine awaited the ACK — positive confirmation.
			s.confirmTxIdle("civ ack (defensive unkey)")
			return
		}
		s.logger.InfoWith().Msg("bridge: sent defensive tx_off on confirmed connection (ADR 0051)")
		s.beginTxConfirm(def, client)
	}()
}
