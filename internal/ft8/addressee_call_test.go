package ft8

import (
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

// Calling the station a heard line is ADDRESSED TO (operator, 2026-11-17: with only
// `VK9XY ZL4KX R-07` decoded, "it will not call the VK9XY"). The app names the slot the
// CALLER's line was heard in, and that the station to call is its addressee.
//
//   - The sequencer listens in the addressee's slots, the ones opposite the caller's, and
//     so transmits in the slot its callers use.
//   - Nothing of the addressee's own is needed to open: the first message carries our
//     grid. Its grid is never taken from the line, which carries the caller's.
//   - The exchange then runs as any answer does once the addressee is heard, and the
//     contact is logged with the grid left empty.
func TestSequencer_CallingTheAddresseeOfAHeardLine_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			r := &seqRecorder{}
			s := newTestSeq(r)
			s.profile = p

			// The caller's line, `VK9XY ZL4KX R-07`, was heard in this slot.
			heard := p.SlotRefFromTime(time.Unix(0, 0).UTC().Add(p.Slot))
			now := time.Unix(0, 0).UTC().Add(2 * p.Slot) // it has just been decoded
			require.NoError(t, s.StartQsoToAddressee("G0XYZ", "IO91", "VK9XY", heard.StartUTC, 1500, 14.080, now))
			require.True(t, s.Active())
			require.Equal(t, oppositePeriod(heard.Period), s.statusForTest().TheirPeriod,
				"the sequencer must listen in the addressee's slots and so transmit in its callers'")

			// The addressee's own slots: first it finishes with someone else, then it
			// answers us.
			driveTheirOn(p, s, 30, []goft8.DecodedMessage{dm("ZL4KX VK9XY RR73", -9)})
			driveTheirOn(p, s, 60, []goft8.DecodedMessage{dm("G0XYZ VK9XY -10", -12)})
			driveTheirOn(p, s, 90, []goft8.DecodedMessage{dm("G0XYZ VK9XY RR73", -11)})

			require.Equal(t, []string{
				"VK9XY G0XYZ IO91", // the call: our grid, nothing of theirs
				"VK9XY G0XYZ R-12", // the report is measured when it answers
				"VK9XY G0XYZ 73",
			}, r.sentMsgs())
			require.False(t, s.Active())
			require.Len(t, r.completed, 1)
			require.Equal(t, "VK9XY", r.completed[0].TheirCall)
			require.Empty(t, r.completed[0].TheirGrid, "a grid that was never heard is not invented")
			require.Equal(t, -10, r.completed[0].TheirReport)
			require.Equal(t, -12, r.completed[0].OurReport)
		})
	}
}

// codex-132: choosing the other parity must not change what "fresh" means. The line that
// was heard is the one whose age counts. A line 2 m 55 s old is callable as either target;
// one 3 m 05 s old is refused as either. (An earlier form sent the slot before the heard
// one, which made the daemon read the line as a slot older and refuse it inside the limit.)
func TestSequencer_TheAddresseeIsAsFreshAsTheLineItWasHeardOn_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		for _, c := range []struct {
			name  string
			age   time.Duration
			stale bool
		}{
			{"inside the limit", staleDecodeLimit - 5*time.Second, false},
			{"at the limit", staleDecodeLimit, false},
			{"beyond the limit", staleDecodeLimit + 5*time.Second, true},
		} {
			t.Run(p.Name+"/"+c.name, func(t *testing.T) {
				// A real slot start, and a `now` exactly `age` after it.
				heardAt := time.Unix(600, 0).UTC().Truncate(2 * p.Slot)
				heard := p.SlotRefFromTime(heardAt)
				now := heardAt.Add(c.age)

				asSender := newTestSeq(&seqRecorder{})
				asSender.profile = p
				errSender := asSender.StartQso("G0XYZ", "IO91", "ZL4KX", "", heard.StartUTC, 1500, 14.080, now)

				asAddressee := newTestSeq(&seqRecorder{})
				asAddressee.profile = p
				errAddressee := asAddressee.StartQsoToAddressee("G0XYZ", "IO91", "VK9XY", heard.StartUTC, 1500, 14.080, now)

				if c.stale {
					require.ErrorIs(t, errSender, ErrStaleDecode)
					require.ErrorIs(t, errAddressee, ErrStaleDecode)
					return
				}
				require.NoError(t, errSender, "the sender of a line this old is callable")
				require.NoError(t, errAddressee, "the addressee of the same line must be callable too")
			})
		}
	}
}
