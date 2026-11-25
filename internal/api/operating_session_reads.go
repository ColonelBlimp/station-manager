package api

import (
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/adif"
	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/types"
	"github.com/ColonelBlimp/station-manager/internal/utils"
)

// What the Session panel reads (ADR 0100 §7, §8; W-0029 part 6): the open
// session's contacts, one contact of it, and the closed sessions Continue
// offers. The storage side is internal/database/sqlite/operating_session_read.go.
//
// Each answer names the served archive, and carries the session generation it
// was read at with the session's id, all of one snapshot of the file. A window
// draws an answer only for the archive it has proven and the session and
// generation it is showing.
//
// These three read the file and nothing else. They do NOT take the admission's
// order, unlike GET /v1/operating-session, which has to name the state for its
// caller: an app's start can hold that order for as long as earned logging
// takes, and a read that waited behind it would keep a request slot for
// nothing it needs.

const (
	// closedSessionsDefaultLimit is how many lines Continue's list has when the
	// caller names no limit (operator ruling 2026-11-25: the 20 most recent,
	// then "Show more").
	closedSessionsDefaultLimit = 20
	// closedSessionsMaxLimit is the most a caller may ask for at once: the cap
	// the daemon's other lists have (AW-6).
	closedSessionsMaxLimit = 1000
)

func (s *Server) registerOperatingSessionReads(apiMux *routeMux) {
	apiMux.HandleFunc("GET /v1/operating-session/members", s.handleOperatingSessionMembers)
	apiMux.HandleFunc("GET /v1/operating-session/members/{uuid}", s.handleOperatingSessionMember)
	apiMux.HandleFunc("GET /v1/operating-session/closed", s.handleOperatingSessionClosed)
}

// sessionMemberBody is one contact as the panel draws it.
type sessionMemberBody struct {
	UUID string `json:"uuid"`
	// Revision is the contact's edit counter (ADR 0050): of two answers about
	// one contact, the one with the higher counter is the later. The session
	// generation says nothing of this: an edit moves no generation.
	Revision int64  `json:"revision"`
	Call     string `json:"call"`
	QsoDate  string `json:"qso_date"`
	TimeOn   string `json:"time_on"`
	Band     string `json:"band"`
	Mode     string `json:"mode"`
	Submode  string `json:"submode,omitempty"`
	RstSent  string `json:"rst_sent"`
	RstRcvd  string `json:"rst_rcvd"`
	Name     string `json:"name"`
	Country  string `json:"country"`
	Comment  string `json:"comment"`
	// Emailed is whether the contact's own record says it was forwarded by
	// email (operator ruling 2026-11-25): every reader agrees, and it outlives
	// the window that sent it.
	Emailed bool `json:"emailed"`
}

func memberBody(q types.Qso) sessionMemberBody {
	return sessionMemberBody{
		UUID: q.UUID, Revision: q.Revision,
		Call: q.Call, QsoDate: q.QsoDate, TimeOn: q.TimeOn, Band: q.Band, Mode: q.Mode, Submode: q.Submode,
		RstSent: q.RstSent, RstRcvd: q.RstRcvd, Name: q.Name, Country: q.Country, Comment: q.Comment,
		Emailed: q.SmFwrdByEmailStatus == adif.YesString,
	}
}

// sessionIDOrNull is a session's id for the wire: null when no session is open.
func sessionIDOrNull(id string) *string {
	if id == "" {
		return nil
	}
	return &id
}

type sessionMembersResponse struct {
	ArchiveID  string              `json:"archive_id"`
	Generation int64               `json:"generation"`
	SessionID  *string             `json:"session_id"`
	Members    []sessionMemberBody `json:"members"`
}

type sessionMemberResponse struct {
	ArchiveID  string             `json:"archive_id"`
	Generation int64              `json:"generation"`
	SessionID  *string            `json:"session_id"`
	Member     *sessionMemberBody `json:"member"`
}

type closedSessionBody struct {
	ID                     string     `json:"id"`
	LogbookUUID            string     `json:"logbook_uuid"`
	OpenedAt               time.Time  `json:"opened_at"`
	LatestSittingStartedAt time.Time  `json:"latest_sitting_started_at"`
	LatestSittingEndedAt   *time.Time `json:"latest_sitting_ended_at"`
	AnEndUnconfirmed       bool       `json:"an_end_unconfirmed"`
	ContactCount           int        `json:"contact_count"`
}

type closedSessionsResponse struct {
	ArchiveID   string              `json:"archive_id"`
	Generation  int64               `json:"generation"`
	LogbookUUID string              `json:"logbook_uuid"`
	Sessions    []closedSessionBody `json:"sessions"`
	More        bool                `json:"more"`
}

// sessionReadFailed answers a read of the session's record that failed: an
// error, never an empty session or an empty list.
func (s *Server) sessionReadFailed(w http.ResponseWriter, op errors.Op, what string, err error) {
	s.logger.ErrorWith().Err(err).Msg("operating session: " + what + " could not be read")
	s.writeError(w, http.StatusServiceUnavailable, operatingSessionUnavailable, what+" could not be read", op)
}

// handleOperatingSessionMembers answers the open session's contacts that are
// not deleted, the newest stored first. With no session open, session_id is
// null and the list is empty.
func (s *Server) handleOperatingSessionMembers(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionMembers"
	got, err := s.db.OperatingSessionMembersWithContext(r.Context())
	if err != nil {
		s.sessionReadFailed(w, op, "the operating session's contacts", err)
		return
	}
	resp := sessionMembersResponse{ArchiveID: s.servedArchiveID, Generation: got.Generation,
		SessionID: sessionIDOrNull(got.SessionID), Members: make([]sessionMemberBody, 0, len(got.Members))}
	for _, q := range got.Members {
		resp.Members = append(resp.Members, memberBody(q))
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// handleOperatingSessionMember answers one contact of the open session. A
// contact that is not a member that is shown (deleted, of another session or
// of none, or not in the file) is 200 with a member of null: "it is not in the
// session" is an answer a window acts on, not an error.
func (s *Server) handleOperatingSessionMember(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionMember"
	uuid := r.PathValue("uuid")
	if !utils.IsValidUUIDv7(uuid) {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", "uuid must be a QSO's UUID", op)
		return
	}
	got, err := s.db.OperatingSessionMemberWithContext(r.Context(), uuid)
	if err != nil {
		s.sessionReadFailed(w, op, "the operating session's contact", err)
		return
	}
	resp := sessionMemberResponse{ArchiveID: s.servedArchiveID, Generation: got.Generation, SessionID: sessionIDOrNull(got.SessionID)}
	if got.Member != nil {
		row := memberBody(*got.Member)
		resp.Member = &row
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// closedSessionsPage reads which page of Continue's list a request asks for.
// refusal is "" for a request that can be answered.
//
// A page is bounded (limit), so the rest of a long history is reached by a
// further page: after names the last session of the page before it and
// generation that page's generation, both or neither.
func closedSessionsPage(q url.Values) (limit int, after *sqlite.ClosedSessionsAfter, refusal string) {
	limit = closedSessionsDefaultLimit
	if values, asked := q["limit"]; asked {
		n, err := strconv.Atoi(values[0])
		if len(values) != 1 || err != nil || n < 1 || n > closedSessionsMaxLimit {
			return 0, nil, "limit must be one whole number from 1 to " + strconv.Itoa(closedSessionsMaxLimit)
		}
		limit = n
	}
	sessions, generations := q["after"], q["generation"]
	if len(sessions) == 0 && len(generations) == 0 {
		return limit, nil, ""
	}
	const both = "a further page names the session it follows (after) and that page's generation, each once"
	if len(sessions) != 1 || len(generations) != 1 || sessions[0] == "" {
		return 0, nil, both
	}
	generation, err := strconv.ParseInt(generations[0], 10, 64)
	if err != nil || generation < 0 {
		return 0, nil, both
	}
	return limit, &sqlite.ClosedSessionsAfter{Generation: generation, SessionID: sessions[0]}, ""
}

// handleOperatingSessionClosed answers a page of Continue's list: the closed
// sessions of the default logbook, by the start of each one's latest sitting,
// the most recent first. Whether one of them can still be continued is decided
// again, in the transaction, when it is chosen (POST
// /v1/operating-session/continue).
//
// A further page of a list that has changed since the page it follows is
// refused as stale: the caller reads the list again from its start, and never
// shows pages of two different lists as one.
func (s *Server) handleOperatingSessionClosed(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionClosed"
	// The query is parsed strictly: URL.Query() drops a pair it cannot parse
	// (an unescaped ';', a bad %-escape), and a further page that lost its
	// pairs would be answered, with 200, as the first page again.
	query, qerr := url.ParseQuery(r.URL.RawQuery)
	if qerr != nil {
		s.writeError(w, http.StatusBadRequest, "invalid_query_param", "the query string could not be parsed", op)
		return
	}
	limit, after, refusal := closedSessionsPage(query)
	if refusal != "" {
		s.writeError(w, http.StatusBadRequest, "invalid_field_value", refusal, op)
		return
	}
	got, err := s.db.ClosedOperatingSessionsWithContext(r.Context(), limit, after)
	if err != nil {
		s.sessionReadFailed(w, op, "the closed operating sessions", err)
		return
	}
	if got.Stale {
		s.writeError(w, http.StatusConflict, operatingSessionStale,
			"the list of closed sessions has changed since the page this one follows; read it again from its start", op)
		return
	}
	resp := closedSessionsResponse{ArchiveID: s.servedArchiveID, Generation: got.Generation,
		LogbookUUID: got.DefaultLogbookUUID, Sessions: make([]closedSessionBody, 0, len(got.Sessions)), More: got.More}
	for _, c := range got.Sessions {
		resp.Sessions = append(resp.Sessions, closedSessionBody(c))
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// closedSessionBody is sqlite.ClosedSession on the wire, field for field.
var _ = closedSessionBody(sqlite.ClosedSession{})
