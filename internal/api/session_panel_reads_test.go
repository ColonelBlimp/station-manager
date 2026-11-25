package api

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
   W-0029 part 6, slice 1: what the Session panel will read. Three routes of
   the app's listener: the open session's contacts, one contact of it, and the
   closed sessions Continue offers. Nothing in the page calls them yet.

   Each answer names the served archive and the session generation it was read
   at, with the session's id, so that a window can tell an answer for the
   session it shows from one that is not.
*/

type memberRowView struct {
	UUID     string `json:"uuid"`
	Revision *int64 `json:"revision"` // a pointer: an edit counter left out is not a counter of 0
	Call     string `json:"call"`
	QsoDate  string `json:"qso_date"`
	TimeOn   string `json:"time_on"`
	Band     string `json:"band"`
	Mode     string `json:"mode"`
	Submode  string `json:"submode"`
	RstSent  string `json:"rst_sent"`
	RstRcvd  string `json:"rst_rcvd"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	Comment  string `json:"comment"`
	Emailed  *bool  `json:"emailed"`
}

type membersView struct {
	ArchiveID  string           `json:"archive_id"`
	Generation int64            `json:"generation"`
	SessionID  *string          `json:"session_id"`
	Members    *[]memberRowView `json:"members"`
}

type memberView struct {
	ArchiveID  string         `json:"archive_id"`
	Generation int64          `json:"generation"`
	SessionID  *string        `json:"session_id"`
	Member     *memberRowView `json:"member"`
}

type closedSessionView struct {
	ID                     string  `json:"id"`
	LogbookUUID            string  `json:"logbook_uuid"`
	OpenedAt               string  `json:"opened_at"`
	LatestSittingStartedAt string  `json:"latest_sitting_started_at"`
	LatestSittingEndedAt   *string `json:"latest_sitting_ended_at"`
	AnEndUnconfirmed       *bool   `json:"an_end_unconfirmed"`
	ContactCount           *int    `json:"contact_count"`
}

type closedView struct {
	ArchiveID   string               `json:"archive_id"`
	Generation  int64                `json:"generation"`
	LogbookUUID string               `json:"logbook_uuid"`
	Sessions    *[]closedSessionView `json:"sessions"`
	More        *bool                `json:"more"`
}

// read answers a GET of the app's listener into v and returns the status.
func (b *memberBed) read(lifetime, path string, v any) int {
	b.t.Helper()
	status, body, _ := b.appDo(b.t, b.c, lifetime, http.MethodGet, path, "")
	if status == http.StatusOK {
		if err := json.Unmarshal(body, v); err != nil {
			b.t.Fatalf("GET %s: %v in %s", path, err, body)
		}
	}
	return status
}

func (b *memberBed) uuidOf(qso int64) string {
	b.t.Helper()
	var uuid string
	if err := b.raw.QueryRow(`SELECT uuid FROM qso WHERE id = ?`, qso).Scan(&uuid); err != nil {
		b.t.Fatalf("the uuid of contact %d: %v", qso, err)
	}
	return uuid
}

// The contacts of the open session, newest first, in the panel's columns, with
// the archive, the session and the generation. A contact stored by a
// deliberate submit is not among them.
func TestSessionPanel_TheMembersRead(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	state := b.view(lifeA)
	first := b.live(lifeA)
	if a := b.log(lifeA, b.def, b.otherUUID); a.status != http.StatusCreated { // deliberate: stored, in no session
		t.Fatalf("the deliberate submit: %+v", a)
	}
	second := b.live(lifeA)

	var got membersView
	if status := b.read(lifeA, "/v1/operating-session/members", &got); status != http.StatusOK {
		t.Fatalf("the members read answered %d", status)
	}
	if got.ArchiveID != testServedArchive || got.Generation != state.Generation || got.SessionID == nil || *got.SessionID != state.Session.ID {
		t.Fatalf("the read names archive %q, generation %d, session %v; want %s, %d, %s", got.ArchiveID, got.Generation, got.SessionID, testServedArchive, state.Generation, state.Session.ID)
	}
	if got.Members == nil || len(*got.Members) != 2 || (*got.Members)[0].UUID != b.uuidOf(second) || (*got.Members)[1].UUID != b.uuidOf(first) {
		t.Fatalf("the contacts read are %+v; want the two live contacts, the newest first", got.Members)
	}
	row := (*got.Members)[0]
	if row.Call == "" || row.QsoDate != "20261120" || row.TimeOn == "" || row.Band != "20m" || row.Mode != "SSB" ||
		row.RstSent != "59" || row.RstRcvd != "59" || row.Revision == nil || row.Emailed == nil || *row.Emailed {
		t.Fatalf("a row is %+v; want the panel's columns, an edit counter, and not emailed", row)
	}
}

// With no session open the answer is a session of null and no rows: an empty
// list, not a missing one, and the generation.
func TestSessionPanel_TheMembersReadWithNoSessionOpen(t *testing.T) {
	b := newMemberBed(t, true)
	var got membersView
	if status := b.read("", "/v1/operating-session/members", &got); status != http.StatusOK {
		t.Fatalf("the members read answered %d", status)
	}
	if got.SessionID != nil || got.Members == nil || len(*got.Members) != 0 || got.ArchiveID != testServedArchive {
		t.Fatalf("with no session open the read is %+v; want a null session and an empty list", got)
	}
}

// One contact of the open session by its uuid; a contact that is not a member
// that is shown answers 200 with no row, so that "it is not in the session" is
// an answer and not an error.
func TestSessionPanel_TheMemberRead(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	state := b.view(lifeA)
	member := b.live(lifeA)
	outside := b.log(lifeA, b.def, b.otherUUID)

	var got memberView
	if status := b.read(lifeA, "/v1/operating-session/members/"+b.uuidOf(member), &got); status != http.StatusOK {
		t.Fatalf("the member read answered %d", status)
	}
	if got.Member == nil || got.Member.UUID != b.uuidOf(member) || got.SessionID == nil || *got.SessionID != state.Session.ID || got.Generation != state.Generation || got.ArchiveID != testServedArchive {
		t.Fatalf("the member read is %+v with %+v; want the contact, in session %s at generation %d", got, got.Member, state.Session.ID, state.Generation)
	}

	b.exec(`UPDATE qso SET deleted_at = datetime('now') WHERE id = ?`, member)
	for what, uuid := range map[string]string{
		"a deleted member":              b.uuidOf(member),
		"a contact in no session":       b.uuidOf(outside.id),
		"a uuid the file does not hold": "01920000-0000-7000-8000-0000000000ff",
	} {
		var none memberView
		if status := b.read(lifeA, "/v1/operating-session/members/"+uuid, &none); status != http.StatusOK {
			t.Fatalf("%s answered %d; want 200 with no row", what, status)
		}
		if none.Member != nil || none.SessionID == nil || *none.SessionID != state.Session.ID {
			t.Fatalf("%s read as %+v; want no row, in session %s", what, none.Member, state.Session.ID)
		}
	}
	if status, body, _ := b.appDo(t, b.c, lifeA, http.MethodGet, "/v1/operating-session/members/not-a-uuid", ""); status != http.StatusBadRequest {
		t.Fatalf("a malformed uuid answered %d %s; want 400", status, body)
	}
}

// Continue's list: the closed sessions of the default logbook, with the
// archive and the generation the list was read at.
func TestSessionPanel_TheClosedSessionsRead(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	earlier := b.view(lifeA)
	b.live(lifeA)
	b.live(lifeA)
	if a := b.newAs(lifeA, earlier.Generation); a.status != http.StatusOK {
		t.Fatalf("New: %+v", a)
	}
	now := b.view(lifeA)

	var got closedView
	if status := b.read(lifeA, "/v1/operating-session/closed", &got); status != http.StatusOK {
		t.Fatalf("the closed sessions read answered %d", status)
	}
	if got.ArchiveID != testServedArchive || got.Generation != now.Generation || got.LogbookUUID != b.logbook || got.More == nil || *got.More {
		t.Fatalf("the list names archive %q, generation %d, logbook %q, more %v; want %s, %d, %s and no more", got.ArchiveID, got.Generation, got.LogbookUUID, got.More, testServedArchive, now.Generation, b.logbook)
	}
	if got.Sessions == nil || len(*got.Sessions) != 1 {
		t.Fatalf("the list is %+v; want the one session New closed", got.Sessions)
	}
	s := (*got.Sessions)[0]
	if s.ID != earlier.Session.ID || s.LogbookUUID != b.logbook || s.OpenedAt == "" || s.LatestSittingStartedAt == "" || s.LatestSittingEndedAt == nil ||
		s.AnEndUnconfirmed == nil || *s.AnEndUnconfirmed || s.ContactCount == nil || *s.ContactCount != 2 {
		t.Fatalf("the line is %+v; want %s, with its start, its sitting's start and end, every end confirmed, 2 contacts", s, earlier.Session.ID)
	}

	// The limit: one line and "more" when there are two; a limit that is not a
	// count from 1 to 1000 is refused.
	b.live(lifeA)
	if a := b.newAs(lifeA, now.Generation); a.status != http.StatusOK {
		t.Fatalf("the second New: %+v", a)
	}
	var one closedView
	if status := b.read(lifeA, "/v1/operating-session/closed?limit=1", &one); status != http.StatusOK || one.Sessions == nil || len(*one.Sessions) != 1 || one.More == nil || !*one.More {
		t.Fatalf("with a limit of 1 the list is %+v (status %d); want one line and more", one, status)
	}
	for _, bad := range []string{"0", "-1", "1001", "many", "1&limit=2"} {
		if status, body, _ := b.appDo(t, b.c, lifeA, http.MethodGet, "/v1/operating-session/closed?limit="+bad, ""); status != http.StatusBadRequest {
			t.Fatalf("limit=%s answered %d %s; want 400", bad, status, body)
		}
	}
}

// The three reads take no part in the admission's order: with it held, as an
// app's start can hold it for as long as earned logging takes, they answer. So
// none of them can keep a request slot behind a start that is waiting.
func TestSessionPanel_TheReadsDoNotWaitForTheAdmissionsOrder(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	qso := b.live(lifeA)
	uuid := b.uuidOf(qso)
	release := b.srv.HoldSessionOrder()
	defer release()
	for _, path := range []string{"/v1/operating-session/members", "/v1/operating-session/members/" + uuid, "/v1/operating-session/closed"} {
		done := make(chan int, 1)
		go func() {
			status, _, _ := b.appDo(t, b.c, lifeA, http.MethodGet, path, "")
			done <- status
		}()
		if status := finished(t, "GET "+path+" with the admission's order held", done); status != http.StatusOK {
			t.Fatalf("GET %s with the order held answered %d", path, status)
		}
	}
}

// A file that cannot be read is an error, never an empty session or an empty
// list.
func TestSessionPanel_AReadThatFailsIsAnError(t *testing.T) {
	b := newMemberBed(t, true)
	b.mustBeAdmitted(lifeA)
	uuid := b.uuidOf(b.live(lifeA))
	b.exec(`ALTER TABLE operating_session_member RENAME TO operating_session_member_gone`)
	for _, path := range []string{"/v1/operating-session/members", "/v1/operating-session/members/" + uuid, "/v1/operating-session/closed"} {
		status, body, _ := b.appDo(t, b.c, lifeA, http.MethodGet, path, "")
		if status != http.StatusServiceUnavailable || errorCode(t, body) != operatingSessionUnavailable {
			t.Fatalf("GET %s on a file that cannot be read answered %d %s; want 503 %s", path, status, body, operatingSessionUnavailable)
		}
	}
}
