package ft8

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// W-0021 slice 3A (ADR 0071): an archive activation seals TX admission as one
// check-and-set under the FT8 lock order. Idle → sealed, and every admission
// path then refuses with the named reason until the seal is released.

func TestSealTxAdmission_IdleSealsAndEveryAdmissionRefuses(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.SealTxAdmission())
	require.NoError(t, s.SealTxAdmission(), "sealing twice is idempotent")

	now := time.Now().UTC().Format(time.RFC3339)
	require.ErrorIs(t, s.ArmTx(true), ErrArchiveSwitchPending)
	require.False(t, s.txArmed, "a refused arm leaves TX disarmed")
	require.ErrorIs(t, s.TransmitNext("CQ G0XYZ IO91", 1500), ErrArchiveSwitchPending)
	require.ErrorIs(t, s.StartQso("7Q5MLV", "IO91", "K1ABC", "FN42", now, 1600, 14.074, 1, false, ""), ErrArchiveSwitchPending)
	require.ErrorIs(t, s.StartCallCq("7Q5MLV", "IO91", 1600, 14.074, "", "", 1, ""), ErrArchiveSwitchPending)
	require.ErrorIs(t, s.StartWorkCaller("7Q5MLV", "K1ABC", "FN42", -12, now, 1600, 14.074, 1, false, ""), ErrArchiveSwitchPending)
	_, err := s.ClaimProfile("ft4")
	require.ErrorIs(t, err, ErrArchiveSwitchPending)
	require.Equal(t, "FT8", s.Profile().Name, "a refused claim changes nothing")

	s.ReleaseTxAdmissionSeal()
	s.ReleaseTxAdmissionSeal() // releasing an open seal is a no-op
	require.NoError(t, s.ArmTx(true), "admission is open again once the seal is released")
	_ = s.ArmTx(false)
}

// Ruled 2026-09-22: an armed but idle session is busy for activation.
func TestSealTxAdmission_RefusedWhileArmed(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })

	require.ErrorIs(t, s.SealTxAdmission(), ErrTxArmed)
	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500), "a refused seal leaves admission open")
}

// A manual send waiting for its slot is in flight (txInFlight is set under txMu
// before the slot wait) and counts as busy — the interleaving review finding 3b named.
func TestSealTxAdmission_RefusedWhileAManualSendWaitsForItsSlot(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })
	require.NoError(t, s.TransmitNext("CQ G0XYZ IO91", 1500))
	require.True(t, s.txInFlightNow())

	require.ErrorIs(t, s.SealTxAdmission(), ErrTxInFlight)
}

func TestSealTxAdmission_RefusedWhileASessionIsActive(t *testing.T) {
	s := newTxTestService(&fakeKeyer{}, newFakeTxPlayer(), nil)
	require.NoError(t, s.ArmTx(true))
	t.Cleanup(func() { _ = s.ArmTx(false) })
	require.NoError(t, s.StartCallCq("7Q5MLV", "IO91", 1500, 14.074, "", "", 1, ""))
	require.True(t, s.seq.Active())
	t.Cleanup(s.AbandonQso)

	require.ErrorIs(t, s.SealTxAdmission(), ErrQsoInProgress)
	require.False(t, s.sealedNow(), "a refused seal is not left behind")
}
