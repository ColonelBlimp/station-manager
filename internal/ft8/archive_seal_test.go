package ft8

import (
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/testbarrier"
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
	t.Cleanup(func() { abandonQso(t, s) })

	require.ErrorIs(t, s.SealTxAdmission(), ErrQsoInProgress)
	require.False(t, s.sealedNow(), "a refused seal is not left behind")
}

// An archive switch seals TX admission and then waits for completion logging
// already earned (ADR 0100 §4; W-0029 part 3, D3). A contact earned before the
// seal, which the decode loop is still handing to its logging job when the seal
// is taken, does not escape that wait: the seal does not refuse on it, since
// the exchange has ended, and AwaitCompletionHandover after the seal returns
// only when the contact has been handed over. The seal is held throughout, so
// nothing new can be earned behind it.
func TestSealTxAdmission_AContactBeingHandedOverIsWaitedForAfterTheSeal(t *testing.T) {
	f := newReplacedContact(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce sync.Once
	letThrough := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(letThrough)
	f.mu.Lock()
	f.holdLog = func() {
		enterOnce.Do(func() { close(entered) })
		<-release
	}
	f.mu.Unlock()

	f.toTheirReport(t)
	// The closing 73 is refused before a transmission starts: K1ABC's roger
	// earns the contact, and the decode loop hands it over.
	real := f.s.seq.transmit
	f.s.seq.transmit = func(msg string, offsetHz, dialMHz float64, gen uint64, onDone func(bool)) error {
		if msg == closingRung {
			return ErrTxNotReady
		}
		return real(msg, offsetHz, dialMHz, gen, onDone)
	}
	slot := make(chan struct{})
	go func() { // the decode loop's slot
		defer close(slot)
		f.s.driveSequencer(ProfileFT8.SlotRefFromTime(time.Unix(f.base.Unix()+90, 0).UTC()),
			[]goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)},
			time.Unix(f.base.Unix()+90, 0).UTC().Add(ProfileFT8.Slot+time.Second))
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the fixture: the refused closing 73 never handed the contact over")
	}
	require.Empty(t, f.loggedNow(), "the fixture: the hand-over is in progress, not finished")

	// The operator has disarmed: a switch does not disarm FT8, and refuses while armed.
	require.ErrorIs(t, f.s.SealTxAdmission(), ErrTxArmed, "the fixture: still armed, the switch is refused as busy")
	require.NoError(t, f.s.OperatingAs(lifeA).ArmTx(false))
	require.NoError(t, f.s.SealTxAdmission(), "the exchange has ended: the seal is taken with the hand-over still to finish")

	done := make(chan int, 1)
	waitsForTheSlot := testbarrier.Expect(t, testbarrier.AtMutex, "(*Service).AwaitCompletionHandover")
	go func() {
		f.s.AwaitCompletionHandover()
		done <- len(f.loggedNow())
	}()
	testbarrier.Waiting(waitsForTheSlot, "the switch's wait, with the contact still being handed over", done)
	require.ErrorIs(t, f.s.OperatingAs(lifeA).ArmTx(true), ErrArchiveSwitchPending, "the seal is held while the switch waits")
	letThrough()
	select {
	case logged := <-done:
		require.Equal(t, 1, logged, "when the wait returned the contact had not been handed over")
	case <-time.After(2 * time.Second):
		t.Fatal("the wait never returned after the hand-over finished")
	}
	<-slot
}
