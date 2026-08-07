package ft8

/*
   Dial-disarm visibility (dogfood 2026-08-07): an arm-only safety disarm is
   invisible in the SPA.

   The incident: with no session active (the operator had abandoned it 6 s
   earlier), a 200 Hz dial nudge tore down the ARM — smd.log said
   `ft8 tx: disarmed, cause:dial_moved`, but the ft8-tx frame carries only
   `armed:false`, so the SPA showed TX drop with no explanation and the
   operator had to ask. The session-end path already solved this exact
   problem (invariant 5, end_reason on the terminal ft8-qso frame); the
   arm-only teardown is the case it does not cover.

   Record: TxState carries `disarm_cause` — the cause of the disarm the
   frame reports, cleared when a new arm commits. The daemon stays
   policy-free: ALL causes ride the frame (operator included); which ones
   deserve a toast is the SPA's call, exactly as SESSION_END_TEXT wording
   is (ADR 0010 — stable codes, client wording).

   CRITERION (confusable-state form): when TX disarms underneath the
   operator, the frame names why, and the operator can tell it apart from
   their own disarm (cause "operator"), from a replayed stale frame (the
   cause never outlives the disarm it explains — cleared on arm), and from
   an idempotent re-disarm of an already-idle path (which reports nothing
   and must not rewrite the recorded cause).
*/

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// X1 — A SAFETY DISARM NAMES ITS CAUSE ON THE WIRE. The dial guard's
// teardown publishes armed:false with disarm_cause "dial_moved" — the frame
// the SPA needs to say why TX dropped when no session end explains it.
func TestTxDisarmCause_SafetyDisarmCarriesCause(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))

	s.disarmTx(disarmDialMoved)

	ch, unsub := s.hub.subscribe()
	defer unsub()
	st := drainTxState(t, ch, func(st TxState) bool { return !st.Armed })
	require.Equal(t, "dial_moved", st.DisarmCause,
		"the frame must say why TX disarmed underneath the operator")
}

// X2 — THE CAUSE NEVER OUTLIVES THE DISARM IT EXPLAINS: a new arm clears it,
// so a reconnecting SPA replaying the cached armed frame cannot read a stale
// cause against a live arm.
func TestTxDisarmCause_ClearedWhenArmCommits(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	s.disarmTx(disarmDialMoved)
	require.NoError(t, s.ArmTx(true))

	ch, unsub := s.hub.subscribe()
	defer unsub()
	st := drainTxState(t, ch, func(st TxState) bool { return st.Armed })
	require.Empty(t, st.DisarmCause, "an armed frame must carry no disarm cause")
}

// X3 — AN IDEMPOTENT DISARM OF AN ALREADY-IDLE PATH DOES NOT REWRITE
// HISTORY: the cached frame keeps the cause of the disarm that actually
// happened. (The idle branch publishes nothing, so a late subscriber reads
// the original teardown's frame.)
func TestTxDisarmCause_IdleRedisarmKeepsOriginalCause(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.ArmTx(false)) // the operator disarmed
	s.disarmTx(disarmCatLost)          // subsystem already idle — a no-op teardown

	ch, unsub := s.hub.subscribe()
	defer unsub()
	st := drainTxState(t, ch, func(st TxState) bool { return !st.Armed })
	require.Equal(t, "operator", st.DisarmCause,
		"the frame reports the disarm that happened, not a later no-op's cause")
}

// X4 — THE OPERATOR'S OWN DISARM CARRIES "operator": the SPA's don't-toast
// decision needs the cause stated, not inferred from absence — absence is
// what an older daemon sends for every cause.
func TestTxDisarmCause_OperatorDisarmSaysSo(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.ArmTx(false))

	ch, unsub := s.hub.subscribe()
	defer unsub()
	st := drainTxState(t, ch, func(st TxState) bool { return !st.Armed })
	require.Equal(t, "operator", st.DisarmCause)
}
