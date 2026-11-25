package sqlite

import (
	"context"
	"reflect"
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0029 part 6, slice 1: what the Session panel reads (the dossier's "What
   the daemon needs"). The open session's contacts, one contact of it, and the
   closed sessions Continue offers. Each answer is one snapshot of the file and
   carries the session generation it was read at, so that a window can tell an
   answer for the session it shows from one that is not.

   Nothing here changes a row: every case ends with the session's record as it
   was.
*/

func (b *sessBed) membersRead() SessionMembers {
	b.t.Helper()
	got, err := b.svc.OperatingSessionMembersWithContext(context.Background())
	if err != nil {
		b.t.Fatalf("read the session's contacts: %v", err)
	}
	return got
}

func (b *sessBed) memberRead(uuid string) SessionMember {
	b.t.Helper()
	got, err := b.svc.OperatingSessionMemberWithContext(context.Background(), uuid)
	if err != nil {
		b.t.Fatalf("read contact %s of the session: %v", uuid, err)
	}
	return got
}

func (b *sessBed) closedRead(limit int) ClosedSessions {
	b.t.Helper()
	got, err := b.svc.ClosedOperatingSessionsWithContext(context.Background(), limit)
	if err != nil {
		b.t.Fatalf("read the closed sessions: %v", err)
	}
	return got
}

func (b *sessBed) uuidOf(qso int64) string {
	b.t.Helper()
	return b.text(`SELECT uuid FROM qso WHERE id = ?`, qso)
}

func memberIDs(rows []types.Qso) []int64 {
	out := make([]int64, 0, len(rows))
	for _, q := range rows {
		out = append(out, q.ID)
	}
	return out
}

// The contacts read are the open session's members that are not deleted, the
// newest first, with the session's id and the generation. A contact of another
// logbook, one stored with no logging step (an import), one of an earlier
// session and a deleted member are not among them.
func TestSessionMembers_AreTheOpenSessionsContactsNotDeleted(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	earlier, _ := b.log(b.x, lifeA, 1)
	st = b.newSession(lifeA, st.Generation, 5, SessionApplied) // the earlier session keeps its contact
	first, _ := b.log(b.x, lifeA, 6)
	b.log(b.y, lifeA, 7) // another logbook: stored, in no session
	gone, _ := b.log(b.x, lifeA, 8)
	last, _ := b.log(b.x, lifeA, 9)
	imported, err := b.svc.InsertQsoWithContext(context.Background(), b.nextQso(b.x))
	if err != nil {
		t.Fatalf("a contact stored with no logging step: %v", err)
	}
	execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, gone)
	before := b.records()

	got := b.membersRead()

	if got.SessionID != st.Open.ID || got.Generation != st.Generation {
		t.Fatalf("read session %q at generation %d; want %s at %d", got.SessionID, got.Generation, st.Open.ID, st.Generation)
	}
	if ids := memberIDs(got.Members); !reflect.DeepEqual(ids, []int64{last, first}) {
		t.Fatalf("the contacts read are %v; want %v, newest first (not %d of the earlier session, %d deleted, %d imported)",
			ids, []int64{last, first}, earlier, gone, imported)
	}
	if got.Members[0].UUID != b.uuidOf(last) || got.Members[0].Call == "" || got.Members[0].LogbookID != b.x {
		t.Fatalf("a contact read is %+v; want the stored contact, with its uuid, its call and its logbook", got.Members[0])
	}
	if after := b.records(); !reflect.DeepEqual(after, before) {
		t.Fatal("reading the contacts changed the session's record")
	}
}

// With no session open the answer names none, and has the generation.
func TestSessionMembers_NoSessionOpen(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	b.log(b.x, lifeA, 1)
	st = b.exit(lifeA, st.Generation, true, 2, SessionApplied)

	got := b.membersRead()

	if got.SessionID != "" || len(got.Members) != 0 || got.Generation != st.Generation {
		t.Fatalf("with no session open the read is session %q, %d contacts, generation %d; want none, none and %d",
			got.SessionID, len(got.Members), got.Generation, st.Generation)
	}
}

// Each read is of ONE snapshot. While a read is between its first read of the
// file and its next (sessionReadGap), a contact joins the session it has just
// named, New closes that session and opens another, and the new one takes a
// contact. The answer is of the moment the read began: that session, at that
// generation, with the contacts it had then. A read made of two snapshots
// would name the old session with the newer rows, or the reverse. A read made
// afterwards is of afterwards.
func TestSessionReads_AreEachOneSnapshot(t *testing.T) {
	// between runs once, inside the next read.
	between := func(t *testing.T, fn func()) {
		t.Helper()
		sessionReadGap = func() {
			sessionReadGap = nil
			fn()
		}
		t.Cleanup(func() { sessionReadGap = nil })
	}
	t.Run("the session's contacts", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		first, _ := b.log(b.x, lifeA, 1)
		var next OperatingSessionState
		var later int64
		between(t, func() {
			b.log(b.x, lifeA, 2)
			next = b.newSession(lifeA, st.Generation, 3, SessionApplied)
			later, _ = b.log(b.x, lifeA, 4)
		})

		got := b.membersRead()

		if next.Open == nil || later == 0 {
			t.Fatal("the fixture: nothing was written inside the read")
		}
		if got.SessionID != st.Open.ID || got.Generation != st.Generation || !reflect.DeepEqual(memberIDs(got.Members), []int64{first}) {
			t.Fatalf("the read is session %q at generation %d with contacts %v; want %s at %d with %v, as when it began",
				got.SessionID, got.Generation, memberIDs(got.Members), st.Open.ID, st.Generation, []int64{first})
		}
		if now := b.membersRead(); now.SessionID != next.Open.ID || now.Generation != next.Generation || !reflect.DeepEqual(memberIDs(now.Members), []int64{later}) {
			t.Fatalf("a read made afterwards is session %q at generation %d with %v; want %s at %d with %v",
				now.SessionID, now.Generation, memberIDs(now.Members), next.Open.ID, next.Generation, []int64{later})
		}
	})
	t.Run("one contact", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		member, _ := b.log(b.x, lifeA, 1)
		uuid := b.uuidOf(member)
		between(t, func() {
			execT(t, b.svc, `UPDATE qso SET call = 'K9ZZZ' WHERE id = ?`, member)
			b.newSession(lifeA, st.Generation, 3, SessionApplied)
		})

		got := b.memberRead(uuid)

		if got.SessionID != st.Open.ID || got.Generation != st.Generation || got.Member == nil || got.Member.Call == "K9ZZZ" {
			t.Fatalf("the read is session %q at generation %d with %+v; want %s at %d with the contact as it was when the read began",
				got.SessionID, got.Generation, got.Member, st.Open.ID, st.Generation)
		}
		if now := b.memberRead(uuid); now.Member != nil || now.SessionID == st.Open.ID {
			t.Fatalf("a read made afterwards is %+v in session %q; want no row: the contact's session has been closed", now.Member, now.SessionID)
		}
	})
	t.Run("the closed sessions", func(t *testing.T) {
		b := newSessBed(t, true)
		st := b.open(lifeA, 0, SessionApplied)
		b.log(b.x, lifeA, 1)
		var next OperatingSessionState
		between(t, func() { next = b.newSession(lifeA, st.Generation, 3, SessionApplied) })

		got := b.closedRead(20)

		if got.Generation != st.Generation || len(got.Sessions) != 0 {
			t.Fatalf("the list is at generation %d with %d sessions; want %d and none, as when it began", got.Generation, len(got.Sessions), st.Generation)
		}
		if now := b.closedRead(20); now.Generation != next.Generation || len(now.Sessions) != 1 || now.Sessions[0].ID != st.Open.ID {
			t.Fatalf("a list read afterwards is at generation %d with %+v; want %d and the session New closed", now.Generation, now.Sessions, next.Generation)
		}
	})
}

// One contact of the open session, by its uuid, with the session and the
// generation. A contact that is not a member that is shown answers no row: a
// deleted member, a contact of an earlier session, one in no session, and a
// uuid the file does not hold.
func TestSessionMember_OneContactOfTheOpenSession(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	earlier, _ := b.log(b.x, lifeA, 1)
	st = b.newSession(lifeA, st.Generation, 5, SessionApplied)
	member, _ := b.log(b.x, lifeA, 6)
	gone, _ := b.log(b.x, lifeA, 7)
	outside, _ := b.log(b.y, lifeA, 8)
	execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, gone)

	got := b.memberRead(b.uuidOf(member))
	if got.SessionID != st.Open.ID || got.Generation != st.Generation || got.Member == nil || got.Member.ID != member {
		t.Fatalf("the member read is session %q, generation %d, contact %+v; want %s, %d and contact %d",
			got.SessionID, got.Generation, got.Member, st.Open.ID, st.Generation, member)
	}
	first := got.Member.Revision
	execT(t, b.svc, `UPDATE qso SET call = 'K9ZZZ' WHERE id = ?`, member)
	if again := b.memberRead(b.uuidOf(member)); again.Member == nil || again.Member.Call != "K9ZZZ" || again.Member.Revision <= first {
		t.Fatalf("after an edit the member read is %+v; want the edit and a later edit counter than %d", again.Member, first)
	}

	for what, uuid := range map[string]string{
		"a deleted member":                b.uuidOf(gone),
		"a contact of an earlier session": b.uuidOf(earlier),
		"a contact in no session":         b.uuidOf(outside),
		"a uuid the file does not hold":   "01920000-0000-7000-8000-0000000000ff",
	} {
		if got := b.memberRead(uuid); got.Member != nil || got.SessionID != st.Open.ID || got.Generation != st.Generation {
			t.Fatalf("%s read as %+v in session %q at generation %d; want no row, in %s at %d",
				what, got.Member, got.SessionID, got.Generation, st.Open.ID, st.Generation)
		}
	}
}

// closedBed leaves, in order of their latest sitting's start:
//
//	S3  two sittings: the first ended unconfirmed, the second confirmed;
//	S2  one sitting, ended unconfirmed (its app was replaced);
//	S1  one sitting, confirmed, whose only contact is deleted;
//
// with S4 open and empty, all of logbook X.
type closedBed struct {
	*sessBed
	s1, s2, s3, s4 string
	st             OperatingSessionState
}

func newClosedBed(t *testing.T) *closedBed {
	t.Helper()
	b := &closedBed{sessBed: newSessBed(t, true)}
	st := b.open(lifeA, 0, SessionApplied)
	b.s1 = st.Open.ID
	only, _ := b.log(b.x, lifeA, 1)
	st = b.newSession(lifeA, st.Generation, 10, SessionApplied) // S1 closed, confirmed at 10
	execT(t, b.svc, `UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, only)

	b.s2 = st.Open.ID // opened at 10
	b.log(b.x, lifeA, 11)
	b.log(b.x, lifeA, 12)
	g := b.open(lifeB, 20, SessionRecoveryPending).Generation
	st = b.newSession(lifeB, g, 21, SessionApplied) // S2 closed, its end unconfirmed

	b.s3 = st.Open.ID // opened at 21, held by B
	b.log(b.x, lifeB, 22)
	g = b.open(lifeC, 30, SessionRecoveryPending).Generation
	st = b.newSession(lifeC, g, 31, SessionApplied)                        // S3 closed, its first end unconfirmed; an empty session open
	st = b.continueSession(lifeC, st.Generation, b.s3, 40, SessionApplied) // S3 again: a second sitting from 40
	b.log(b.x, lifeC, 41)
	st = b.newSession(lifeC, st.Generation, 50, SessionApplied) // S3 closed, confirmed at 50
	b.s4, b.st = st.Open.ID, st
	return b
}

const lifeC = "lifetime-C"

// Continue's list: the closed sessions of the default logbook, by the start of
// each one's latest sitting, the most recent first; the open session is not in
// it. Each gives its first start, its latest sitting's start and end, whether
// any sitting's end was not confirmed, and its count of contacts not deleted.
func TestClosedSessions_AreOrderedByTheirLatestSitting(t *testing.T) {
	b := newClosedBed(t)
	before := b.records()

	got := b.closedRead(20)

	if got.Generation != b.st.Generation || got.DefaultLogbookUUID != b.xUUID || got.More {
		t.Fatalf("the list is at generation %d for logbook %s, more %v; want %d, %s and no more", got.Generation, got.DefaultLogbookUUID, got.More, b.st.Generation, b.xUUID)
	}
	var ids []string
	for _, s := range got.Sessions {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{b.s3, b.s2, b.s1}) {
		t.Fatalf("the list is %v; want %v (and not the open %s)", ids, []string{b.s3, b.s2, b.s1}, b.s4)
	}
	s3, s2, s1 := got.Sessions[0], got.Sessions[1], got.Sessions[2]
	// S3: continued once. Its first start stands; its latest sitting is the
	// second; that one ended confirmed, and an earlier end was not.
	if !s3.OpenedAt.Equal(at(21)) || !s3.LatestSittingStartedAt.Equal(at(40)) || s3.LatestSittingEndedAt == nil ||
		!s3.LatestSittingEndedAt.Equal(at(50)) || !s3.AnEndUnconfirmed || s3.ContactCount != 2 {
		t.Fatalf("S3 = %+v; want opened at 21, latest sitting 40 to 50, an earlier end unconfirmed, 2 contacts", s3)
	}
	// S2: one sitting, its end not confirmed: no end time is made up.
	if !s2.LatestSittingStartedAt.Equal(at(10)) || s2.LatestSittingEndedAt != nil || !s2.AnEndUnconfirmed || s2.ContactCount != 2 {
		t.Fatalf("S2 = %+v; want a sitting from 10 with no end time, an end unconfirmed, 2 contacts", s2)
	}
	// S1: its only contact is deleted. It is still a session, with 0 contacts.
	if s1.LatestSittingEndedAt == nil || !s1.LatestSittingEndedAt.Equal(at(10)) || s1.AnEndUnconfirmed || s1.ContactCount != 0 {
		t.Fatalf("S1 = %+v; want a sitting ended at 10, confirmed, with 0 contacts", s1)
	}
	if after := b.records(); !reflect.DeepEqual(after, before) {
		t.Fatal("reading the list changed the session's record")
	}
}

// A session is placed by its LATEST sitting, not by when it first started: the
// oldest session, continued again, comes first.
func TestClosedSessions_AContinuedSessionIsPlacedByItsLatestSitting(t *testing.T) {
	b := newClosedBed(t)
	st := b.continueSession(lifeC, b.st.Generation, b.s1, 60, SessionApplied) // S1, first started at 0, sits again from 60
	b.newSession(lifeC, st.Generation, 70, SessionApplied)

	got := b.closedRead(20)

	var ids []string
	for _, s := range got.Sessions {
		ids = append(ids, s.ID)
	}
	if !reflect.DeepEqual(ids, []string{b.s1, b.s3, b.s2}) {
		t.Fatalf("the list is %v; want %v: the session continued last comes first, though it started first", ids, []string{b.s1, b.s3, b.s2})
	}
	if first := got.Sessions[0]; !first.OpenedAt.Equal(at(0)) || !first.LatestSittingStartedAt.Equal(at(60)) || first.LatestSittingEndedAt == nil || !first.LatestSittingEndedAt.Equal(at(70)) {
		t.Fatalf("the continued session = %+v; want first started at 0, its latest sitting 60 to 70", first)
	}
}

// The list is cut at the limit asked for, and says when there are more.
func TestClosedSessions_AreCutAtTheLimit(t *testing.T) {
	b := newClosedBed(t)
	got := b.closedRead(2)
	if len(got.Sessions) != 2 || got.Sessions[0].ID != b.s3 || got.Sessions[1].ID != b.s2 || !got.More {
		t.Fatalf("with a limit of 2 the list is %d sessions, more %v; want the two most recent and more", len(got.Sessions), got.More)
	}
	if got := b.closedRead(3); len(got.Sessions) != 3 || got.More {
		t.Fatalf("with a limit of 3 the list is %d sessions, more %v; want all three and no more", len(got.Sessions), got.More)
	}
}

// Sessions whose latest sittings began at the same instant are ordered by
// their ids, the later id first, the same way at every read.
func TestClosedSessions_EqualStartsAreOrderedByID(t *testing.T) {
	b := newSessBed(t, true)
	st := b.open(lifeA, 0, SessionApplied)
	var want []string
	for i := 0; i < 4; i++ {
		want = append(want, st.Open.ID)
		b.log(b.x, lifeA, 5)
		st = b.newSession(lifeA, st.Generation, 5, SessionApplied) // every one opened and closed at minute 5 but the first
	}
	want = want[1:] // the first was opened at 0: it sorts last, and is not part of the tie
	for i, j := 0, len(want)-1; i < j; i, j = i+1, j-1 {
		want[i], want[j] = want[j], want[i]
	}
	sortedDesc := append([]string(nil), want...)
	for i := 1; i < len(sortedDesc); i++ {
		for j := i; j > 0 && sortedDesc[j] > sortedDesc[j-1]; j-- {
			sortedDesc[j], sortedDesc[j-1] = sortedDesc[j-1], sortedDesc[j]
		}
	}
	for round := 0; round < 3; round++ {
		got := b.closedRead(20)
		var tied []string
		for _, s := range got.Sessions {
			if s.LatestSittingStartedAt.Equal(at(5)) {
				tied = append(tied, s.ID)
			}
		}
		if !reflect.DeepEqual(tied, sortedDesc) {
			t.Fatalf("sessions with equal starts are listed as %v; want them by id, the later first: %v", tied, sortedDesc)
		}
	}
}

// Only sessions of the logbook that is the default now are offered: after the
// default moves to Y the list is Y's, and it is X's again when the default
// returns. With no default logbook the list is empty.
func TestClosedSessions_AreTheDefaultLogbooksOnly(t *testing.T) {
	b := newClosedBed(t)
	b.setDefault(b.y)
	st := b.open(lifeC, 60, SessionApplied) // the empty session of X goes; one opens for Y
	inY := st.Open.ID
	b.log(b.y, lifeC, 61)
	st = b.newSession(lifeC, st.Generation, 62, SessionApplied)

	got := b.closedRead(20)
	if len(got.Sessions) != 1 || got.Sessions[0].ID != inY || got.DefaultLogbookUUID != b.yUUID {
		t.Fatalf("with Y the default the list is %+v for %s; want the one session of Y", got.Sessions, got.DefaultLogbookUUID)
	}
	b.setDefault(b.x)
	if got := b.closedRead(20); len(got.Sessions) != 3 || got.Sessions[0].ID != b.s3 || got.DefaultLogbookUUID != b.xUUID {
		t.Fatalf("with X the default again the list is %d sessions for %s; want X's three", len(got.Sessions), got.DefaultLogbookUUID)
	}

	none := newSessBed(t, false)
	if got := none.closedRead(20); len(got.Sessions) != 0 || got.DefaultLogbookUUID != "" || got.More {
		t.Fatalf("with no default logbook the list is %+v", got)
	}
}
