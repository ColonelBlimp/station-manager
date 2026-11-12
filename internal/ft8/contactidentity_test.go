package ft8

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	goft8 "github.com/ColonelBlimp/go-ft8/ft8"
	"github.com/stretchr/testify/require"
)

/*
	W-0028: every contact has an identity (contactFlags.id) that rides each of its
	status frames and its completion. The daemon ties a lookup it starts at the
	first frame to the one completion that may use it, so the rules below are what
	make "a result for one contact never decorates another" true.

	Two layers:

	  - a RUNTIME probe in the shared harness (newTestSeqLogged), so every
	    recorder-based test in the package — every mode, both profiles, the
	    automatic transitions — checks the rules on the frames and completions it
	    happens to produce. Violations are attributed to the test that drove them
	    and fail the run in TestMain (collected, like the publish probe, so one run
	    names every offender);

	  - explicit tests below for the cases the probe cannot see by itself: a NEW
	    identity for a later contact with the SAME station, which looks identical
	    frame by frame.

	The rules:

	  S1  a frame of a live contact (Active, with a partner) carries a nonzero
	      identity;
	  S2  one identity names one station for its whole life;
	  S3  identities only move forward: a frame never carries one older than a
	      frame already published;
	  C1  a completion carries a nonzero identity;
	  C2  that identity was published, with that station, in a live frame first.
*/

var contactViolations = struct {
	sync.Mutex
	seen map[string]int
}{seen: map[string]int{}}

// drivingTest names the Test function on the current call stack: the probe runs
// inside the sequencer, driven synchronously by the test goroutine.
func drivingTest() string {
	var pcs [48]uintptr
	n := runtime.Callers(2, pcs[:])
	frames := runtime.CallersFrames(pcs[:n])
	for {
		f, more := frames.Next()
		if i := strings.Index(f.Function, ".Test"); i >= 0 {
			name := f.Function[i+1:]
			if j := strings.Index(name, "."); j >= 0 {
				name = name[:j] // drop closure suffixes (.func1)
			}
			return name
		}
		if !more {
			return "unknown test (the frame was not driven from a test goroutine)"
		}
	}
}

func recordContactViolation(format string, args ...any) {
	msg := drivingTest() + ": " + fmt.Sprintf(format, args...)
	contactViolations.Lock()
	contactViolations.seen[msg]++
	contactViolations.Unlock()
}

// contactProbe checks the rules for one sequencer.
type contactProbe struct {
	mu      sync.Mutex
	station map[uint64]string // identity → the station it named in a live frame
	highest uint64
}

func newContactProbe() *contactProbe { return &contactProbe{station: map[uint64]string{}} }

func (p *contactProbe) status(st QsoStatus) {
	if !st.Active || st.TheirCall == "" {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if st.contactID == 0 {
		recordContactViolation("S1: a live frame for %s carries no contact identity", st.TheirCall)
		return
	}
	if was, ok := p.station[st.contactID]; ok && was != st.TheirCall {
		recordContactViolation("S2: contact %d named %s and now names %s", st.contactID, was, st.TheirCall)
	}
	if st.contactID < p.highest {
		recordContactViolation("S3: a frame carries contact %d after contact %d was published", st.contactID, p.highest)
	}
	p.station[st.contactID] = st.TheirCall
	if st.contactID > p.highest {
		p.highest = st.contactID
	}
}

func (p *contactProbe) completed(c CompletedQso) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c.ContactID == 0 {
		recordContactViolation("C1: the completion with %s carries no contact identity", c.TheirCall)
		return
	}
	if was, ok := p.station[c.ContactID]; !ok {
		recordContactViolation("C2: the completion with %s carries contact %d, which no live frame published", c.TheirCall, c.ContactID)
	} else if was != c.TheirCall {
		recordContactViolation("C2: the completion with %s carries contact %d, which was published for %s", c.TheirCall, c.ContactID, was)
	}
}

// contactViolationReport is TestMain's summary; empty when the rules held.
func contactViolationReport() []string {
	contactViolations.Lock()
	defer contactViolations.Unlock()
	out := make([]string, 0, len(contactViolations.seen))
	for k, n := range contactViolations.seen {
		out = append(out, fmt.Sprintf("%s  x%d", k, n))
	}
	return out
}

// liveContacts returns, in order, each distinct (identity, station) a recorder
// saw in a live frame.
func liveContacts(r *seqRecorder) []QsoStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []QsoStatus
	for _, st := range r.statuses {
		if !st.Active || st.TheirCall == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].contactID == st.contactID && out[n-1].TheirCall == st.TheirCall {
			continue
		}
		out = append(out, QsoStatus{TheirCall: st.TheirCall, contactID: st.contactID})
	}
	return out
}

// The same station, abandoned and answered again: the second contact has its
// own identity, so nothing looked up for the first can serve it. Frame by
// frame the two look alike, which is why this is a test and not a probe rule.
func TestContactIdentity_SameStationAbandonedAndAnsweredAgain_IsANewContact(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			r := &seqRecorder{}
			s := newTestSeq(r)
			s.profile = p
			epoch := time.Unix(0, 0).UTC()
			their := p.SlotRefFromTime(epoch)

			require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, epoch))
			s.Abandon()
			require.False(t, s.Active())
			// The same instant on purpose: an identity built from the start time
			// would repeat here.
			require.NoError(t, s.StartQso("G0XYZ", "IO91", "K1ABC", "FN42", their.StartUTC, 1500, 14.080, epoch))

			got := liveContacts(r)
			require.Len(t, got, 2, "two contacts with K1ABC")
			require.NotZero(t, got[0].contactID)
			require.Greater(t, got[1].contactID, got[0].contactID, "the second contact with the same station has a new identity")

			driveTheirOn(p, s, 30, []goft8.DecodedMessage{dm("CQ K1ABC FN42", -1)})
			driveTheirOn(p, s, 60, []goft8.DecodedMessage{dm("G0XYZ K1ABC -10", -12)})
			driveTheirOn(p, s, 90, []goft8.DecodedMessage{dm("G0XYZ K1ABC RR73", -11)})
			require.Len(t, r.completed, 1)
			require.Equal(t, got[1].contactID, r.completed[0].ContactID, "the completion is the second contact's")
		})
	}
}

// Calling CQ: each answerer worked is its own contact, its identity repeated
// on every frame of that contact and carried by its completion.
func TestContactIdentity_CallCq_EachAnswererIsItsOwnContact(t *testing.T) {
	for _, p := range []Profile{ProfileFT8, ProfileFT4} {
		t.Run(p.Name, func(t *testing.T) {
			r := &seqRecorder{}
			s := newTestSeq(r)
			s.profile = p
			require.NoError(t, s.StartCallCq("7q5mlv", "kh78", 2700, 14.080, "auto_first", "", time.Unix(0, 0).UTC(), ""))

			driveTheirOn(p, s, 30, nil)
			driveTheirOn(p, s, 60, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
			driveTheirOn(p, s, 90, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)})
			driveTheirOn(p, s, 120, nil)
			// The same station answers the resumed CQ again: a second contact.
			driveTheirOn(p, s, 150, []goft8.DecodedMessage{dm("7Q5MLV DL9UW JO41", -8)})
			driveTheirOn(p, s, 180, []goft8.DecodedMessage{dm("7Q5MLV DL9UW R-15", -10)})

			got := liveContacts(r)
			require.Len(t, got, 2, "two contacts with DL9UW: %+v", got)
			require.Greater(t, got[1].contactID, got[0].contactID)
			require.Len(t, r.completed, 2)
			require.Equal(t, got[0].contactID, r.completed[0].ContactID)
			require.Equal(t, got[1].contactID, r.completed[1].ContactID)
		})
	}
}
