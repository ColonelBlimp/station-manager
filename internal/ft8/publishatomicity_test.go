package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	PUBLISH ATOMICITY for the operator-command entry points (2026-07-27).

	Invariant 3 in internal/ft8/CLAUDE.md: the last published `ft8-qso` status always
	reflects the live session. A transition that snapshots its status, RELEASES the
	lock, and only then publishes can be overtaken — Abandon or a slot evaluation may
	acquire the lock in the gap, change or end the session, and publish first. The
	stale frame then lands last, and because the hub CACHES it, a reconnecting client
	is told about a contact that no longer exists.

	That invariant has now been violated and caught by review four separate times, and
	until this file there was nothing executable guarding it — which is why it kept
	coming back. `NextAnswerer` was the fourth (codex P2 on a9e51f96); it inherited the
	shape from `SetSkipIfSilent`, which had it too.

	The probe asserts the property directly rather than trying to lose a race: at
	publish time the sequencer lock must be HELD, because that is precisely what stops
	a replacement transition from interleaving. TryLock succeeding means the caller had
	already let go.

	SCOPE, stated so this file is not mistaken for a package-wide guarantee: it covers
	the operator-command entry points only. There are ~13 other publish-after-unlock
	sites in the slot paths (caller/work/type4 sequencers) which are the same SHAPE and
	were left alone deliberately — converting them is a sweep with its own lock-hold
	and re-entrancy questions, not a review fix. If they are converted, add them here.
*/

// publishLockProbe records frames published while s.mu was NOT held. It is armed
// explicitly, because session SETUP publishes after unlocking too and would otherwise
// drown the signal from the command under test.
type publishLockProbe struct {
	seq      *Sequencer
	armed    bool
	unlocked int
	total    int
}

func (p *publishLockProbe) publish(QsoStatus) {
	if !p.armed {
		return
	}
	p.total++
	// Held → TryLock fails. Succeeding means the transition and its publication are
	// not atomic, and something else can slot in between them.
	if p.seq.mu.TryLock() {
		p.seq.mu.Unlock()
		p.unlocked++
	}
}

func newProbedSeq(t *testing.T) (*Sequencer, *publishLockProbe) {
	t.Helper()
	r := &seqRecorder{}
	p := &publishLockProbe{}
	s := newSequencer(r.transmit, p.publish, 0, nil)
	p.seq = s
	return s, p
}

func TestOperatorCommands_PublishTheirTransitionUnderTheLock(t *testing.T) {
	slot := time.Unix(0, 0).UTC().Format(time.RFC3339)
	now := time.Unix(0, 0).UTC()

	t.Run("NextAnswerer", func(t *testing.T) {
		s, p := newProbedSeq(t)
		require.NoError(t, s.StartCallCq("7Q5MLV", "KH78", 2700, 28.074, "auto_first", "", now))
		driveTheir(s, 30, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
		require.NotNil(t, s.caller, "fixture: a contact to move on from")

		p.armed = true
		require.NoError(t, s.NextAnswerer())

		require.Positive(t, p.total, "fixture: the command must publish something to probe")
		require.Zero(t, p.unlocked,
			"the arm and its status frame must be atomic — an Abandon landing in the gap "+
				"would publish idle first and leave this stale frame cached as the truth")
	})

	t.Run("SetSkipIfSilent", func(t *testing.T) {
		s, p := newProbedSeq(t)
		require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", slot, 1500, 14.074, now))
		driveTheir(s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})

		p.armed = true
		require.NoError(t, s.SetSkipIfSilent(true))

		require.Positive(t, p.total, "fixture: the command must publish something to probe")
		require.Zero(t, p.unlocked, "same rule, same reason — this is where the shape came from")
	})
}
