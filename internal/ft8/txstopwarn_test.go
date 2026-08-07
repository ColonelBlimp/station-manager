package ft8

// Disarm teardown logging — refines ft8-logging-gaps finding 14 ("report device
// teardown errors; never assert a clean teardown that was not verified") with
// the dogfood 2026-08-07 evidence: 9 of that day's 24 warnings were
// "ft8 tx: audio device stop failed — audio playback not playing", one per idle
// disarm. Play/Stop are per-slot; an armed-but-idle device has nothing to stop,
// so ErrNotPlaying from Stop() at disarm is the EXPECTED result of a clean idle
// teardown, not a failed one — and warning on it teaches the log's reader to
// skip the line that matters on a real stuck teardown.
//
// Rules:
//   SW1: disarm with Stop() = playback.ErrNotPlaying logs NO "stop failed"
//        warn, and the teardown still proceeds to Close().
//   SW2: disarm with Stop() returning any OTHER error keeps the warn —
//        finding 14 preserved. This fixture is what differentiates the fix
//        from a blanket removal of the warn.
//
// The acceptance view: after an ordinary arm→disarm with no transmission, the
// operator's smd.log carries no warning — and a disarm that genuinely failed to
// stop the device still says so.

import (
	"bytes"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/audio/playback"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newTxLogTestService mirrors newTxTestService but captures log output, for
// rules about what the disarm path writes to smd.log.
func newTxLogTestService(player txPlayer) (*Service, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}},
		logging.NewForWriter(buf), nil)
	s.newPlayer = func(string, int) (txPlayer, error) { return player, nil }
	s.SetTxKeyer(&fakeKeyer{})
	return s, buf
}

func TestDisarm_IdleStopNotPlayingIsNotAWarning(t *testing.T) {
	p := newFakeTxPlayer()
	p.stopErr = playback.ErrNotPlaying
	s, buf := newTxLogTestService(p)

	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.ArmTx(false))

	require.NotContains(t, buf.String(), "audio device stop failed",
		"an idle device's ErrNotPlaying at disarm is the expected clean teardown, not a fault")
	require.Equal(t, 1, p.closes(), "teardown must still proceed to Close")
}

func TestDisarm_RealStopFailureStillWarns(t *testing.T) {
	p := newFakeTxPlayer()
	p.stopErr = stderrors.New("device wedged")
	s, buf := newTxLogTestService(p)

	require.NoError(t, s.ArmTx(true))
	require.NoError(t, s.ArmTx(false))

	require.Contains(t, buf.String(), "audio device stop failed",
		"a genuine stop failure must keep the finding-14 warn")
}
