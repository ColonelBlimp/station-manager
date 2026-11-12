package ft8

import (
	"context"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/stretchr/testify/require"
)

// W-0028: the daemon learns each contact's identity and partner as soon as they
// are known, through an injected sink fed by the sequencer's own status
// publication (not the lossy hub subscription), so it can start a lookup for
// that contact alongside the ladder. The completion carries the same identity.

// partnerLog records, in order, each partner the sink is told about (repeats
// collapsed: the status is republished on every rung) and each completed
// exchange handed to the QSO logger, with the contact identities of both.
type partnerLog struct {
	mu        sync.Mutex
	events    []string
	told      []uint64 // identity of each "partner" event, in order
	completed []uint64 // identity of each "completed" event, in order
}

func (l *partnerLog) partner(contact uint64, call string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := "partner " + call
	if n := len(l.told); n > 0 && l.told[n-1] == contact && l.events[len(l.events)-1] == e {
		return // the same contact, republished
	}
	l.events = append(l.events, e)
	l.told = append(l.told, contact)
}

func (l *partnerLog) complete(c CompletedQso) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.events = append(l.events, "completed "+c.TheirCall+" "+c.Mode)
	l.completed = append(l.completed, c.ContactID)
}

func (l *partnerLog) ids() (told, completed []uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]uint64(nil), l.told...), append([]uint64(nil), l.completed...)
}

func (l *partnerLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.events...)
}

// partnerService is a Service on profile p with the PRODUCTION publish closure
// and completion callback (the code under test), a recording transmit in place
// of the rig, and the partner sink and QSO logger feeding one ordered log.
func partnerService(p Profile) (*Service, *seqRecorder, *partnerLog) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	s.setProfileForTest(p)
	r := &seqRecorder{}
	s.seq.transmit = r.transmit
	l := &partnerLog{}
	s.SetPartnerSink(l.partner)
	s.SetQsoLogger(func(_ context.Context, c CompletedQso) { l.complete(c) })
	return s, r, l
}

// Answering a CQ, on both lattices: the partner is reported when the exchange
// starts, before the completed exchange reaches the logger.
func TestPartnerSink_AnswerLadder_PartnerIsKnownBeforeCompletion_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			s, _, l := partnerService(p)
			epoch := time.Unix(0, 0).UTC()
			their := p.SlotRefFromTime(epoch)
			require.NoError(t, s.seq.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, epoch))
			require.Equal(t, []string{"partner K1ABC"}, l.all(), "the partner is reported as the exchange starts")

			driveTheirOn(p, s.seq, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
			driveTheirOn(p, s.seq, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
			driveTheirOn(p, s.seq, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

			require.Equal(t, []string{"partner K1ABC", "completed K1ABC " + p.Name}, l.all())
			told, completed := l.ids()
			require.NotZero(t, told[0], "the sink is given a contact identity")
			require.Equal(t, told, completed, "the completion carries the identity the sink was given")
		})
	}
}

// The same station abandoned and answered again, through the production
// callback: the sink is told of a second contact with a new identity, and the
// completion is the second one's. Without a new identity a lookup made for
// the first contact would serve the second.
func TestPartnerSink_SameStationAnsweredAgain_IsReportedAsANewContact_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			s, _, l := partnerService(p)
			epoch := time.Unix(0, 0).UTC()
			their := p.SlotRefFromTime(epoch)
			require.NoError(t, s.seq.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, epoch))
			s.seq.Abandon()
			require.NoError(t, s.seq.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, epoch))
			driveTheirOn(p, s.seq, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
			driveTheirOn(p, s.seq, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
			driveTheirOn(p, s.seq, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})

			require.Equal(t, []string{"partner K1ABC", "partner K1ABC", "completed K1ABC " + p.Name}, l.all())
			told, completed := l.ids()
			require.Greater(t, told[1], told[0], "the second contact has a new identity")
			require.Equal(t, []uint64{told[1]}, completed, "the completion is the second contact's")
		})
	}
}

// Calling CQ, on both lattices: nothing is reported while only CQ is on the
// air; the answerer is reported when it is picked, before its completion.
func TestPartnerSink_CallCqLadder_AnswererIsKnownBeforeCompletion_BothProfiles(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			s, _, l := partnerService(p)
			require.NoError(t, s.seq.StartCallCq("7q5mlv", "kh78", 2700, 14.080, "auto_first", "", time.Unix(0, 0).UTC(), ""))
			driveTheirOn(p, s.seq, 30, nil)
			require.Empty(t, l.all(), "calling CQ has no partner yet")

			driveTheirOn(p, s.seq, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
			require.Equal(t, []string{"partner DL9UW"}, l.all(), "the picked answerer is reported at once")

			driveTheirOn(p, s.seq, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)})
			driveTheirOn(p, s.seq, 120, nil)

			require.Equal(t, []string{"partner DL9UW", "completed DL9UW " + p.Name}, l.all())
			told, completed := l.ids()
			require.NotZero(t, told[0], "the sink is given a contact identity")
			require.Equal(t, told, completed, "the completion carries the identity the sink was given")
			require.True(t, s.seq.Active(), "still calling CQ after the QSO")
		})
	}
}

func TestPartnerSink_IgnoresIdleAndPartnerlessStatus(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	var seen []string
	s.SetPartnerSink(func(_ uint64, call string) { seen = append(seen, call) })

	s.seq.publish(QsoStatus{})                                   // idle
	s.seq.publish(QsoStatus{Active: true, Role: "caller"})       // calling CQ, no answerer yet
	s.seq.publish(QsoStatus{Active: false, TheirCall: "JA1XYZ"}) // a terminal status
	if len(seen) != 0 {
		t.Fatalf("partner sink saw %v; want nothing without an active partner", seen)
	}
}

func TestPartnerSink_UnwiredIsHarmless(t *testing.T) {
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	s.seq.publish(QsoStatus{Active: true, TheirCall: "JA1XYZ"}) // must not panic
}
