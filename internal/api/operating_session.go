package api

import (
	"context"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// The operating session's lifecycle (ADR 0100 §3; W-0029 part 2). The record
// and its transitions are internal/database/sqlite/operating_session.go; this
// is where the daemon uses them:
//
//   - the session's start step runs inside the app's admission
//     (app_admission.go), so a lifetime is current only with its session
//     decided from the archive file;
//   - GET /v1/operating-session reads the state, and changes nothing;
//   - every advance of the session generation is published as one event,
//     operating_session.changed, after its commit.
//
// A session is opened for the lifetime the admission is about to make current,
// never for one named by a request alone.

// operatingSessionUnavailable is the code for a session record that could not
// be read or written. The app treats it as the daemon not being ready.
const operatingSessionUnavailable = "operating_session_unavailable"

// The states the read route names.
const (
	sessionStateOpen            = "open"
	sessionStateNone            = "none"
	sessionStateSetupPending    = "setup_pending"
	sessionStateRecoveryPending = "recovery_pending"
)

// serverSessions is the admission's view of the session record.
type serverSessions struct{ s *Server }

func (ss serverSessions) exitRecorded(lifetime string) (bool, error) {
	st, err := ss.s.db.OperatingSessionStateWithContext(context.Background(), lifetime)
	return st.ExitRecorded, err
}

// start runs the start step for a lifetime the admission is about to make
// current. It does not follow the request's context: an admission the client
// gave up on must not be left half done.
func (ss serverSessions) start(lifetime string) error {
	st, outcome, err := ss.s.db.OpenOperatingSessionWithContext(context.Background(), lifetime, ss.s.sessionNow())
	if err != nil {
		return err
	}
	ss.s.publishSessionChange(st)
	if outcome == sqlite.SessionRetired {
		return errLifetimeRetired
	}
	return nil
}

func (s *Server) sessionNow() time.Time {
	if s.sessionClock != nil {
		return s.sessionClock()
	}
	return time.Now()
}

// publishSessionChange publishes the one event for a transition that advanced
// the generation, after its commit. A transition that changed nothing, or was
// rolled back, publishes nothing.
func (s *Server) publishSessionChange(st sqlite.OperatingSessionState) {
	if !st.Advanced || s.hub == nil {
		return
	}
	s.hub.Publish(events.NameOperatingSessionChanged, events.OperatingSessionChangedPayload{
		ArchiveID: s.servedArchiveID, Generation: st.Generation,
	})
}

func (s *Server) registerOperatingSession(apiMux *routeMux) {
	apiMux.HandleFunc("GET /v1/operating-session", s.handleOperatingSessionRead)
}

type operatingSessionResponse struct {
	ArchiveID  string                `json:"archive_id"`
	Generation int64                 `json:"generation"`
	State      string                `json:"state"`
	Session    *operatingSessionBody `json:"session"`
}

type operatingSessionBody struct {
	ID               string    `json:"id"`
	LogbookUUID      string    `json:"logbook_uuid"`
	OpenedAt         time.Time `json:"opened_at"`
	SittingStartedAt time.Time `json:"sitting_started_at"`
	HasContacts      bool      `json:"has_contacts"`
	HeldByYou        bool      `json:"held_by_you"`
}

// handleOperatingSessionRead answers the served archive's session state in one
// snapshot of the file and the admission together: the file is read, and the
// state named from the current lifetime, with the admission's order held
// across both, so no close and retirement can come between them. It changes
// nothing, whatever lifetime asks, or none. A record that cannot be read is an
// error, never an empty session.
func (s *Server) handleOperatingSessionRead(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionRead"
	caller := appLifetime(r)
	var (
		st   sqlite.OperatingSessionState
		resp operatingSessionResponse
		err  error
	)
	s.admission.observe(func(current string) {
		if st, err = s.db.OperatingSessionStateWithContext(r.Context(), caller); err != nil {
			return
		}
		if s.sessionReadGap != nil {
			s.sessionReadGap()
		}
		resp = s.sessionResponse(st, caller, current)
	})
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("operating session: the state could not be read")
		s.writeError(w, http.StatusServiceUnavailable, operatingSessionUnavailable,
			"the operating session could not be read", op)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// sessionResponse names the state: a session held by the current lifetime, or
// an empty one, is open; one with contacts that another lifetime holds awaits
// the operator's Continue or New. current is the admission's current lifetime
// as observed with the file, by the caller.
func (s *Server) sessionResponse(st sqlite.OperatingSessionState, caller, current string) operatingSessionResponse {
	resp := operatingSessionResponse{ArchiveID: s.servedArchiveID, Generation: st.Generation}
	o := st.Open
	switch {
	case o == nil && st.DefaultLogbookUUID == "":
		resp.State = sessionStateSetupPending
	case o == nil:
		resp.State = sessionStateNone
	case o.HasMembers && o.Holder != current:
		resp.State = sessionStateRecoveryPending
	default:
		resp.State = sessionStateOpen
	}
	if o != nil {
		resp.Session = &operatingSessionBody{
			ID: o.ID, LogbookUUID: o.LogbookUUID, OpenedAt: o.OpenedAt, SittingStartedAt: o.IntervalStartedAt,
			HasContacts: o.HasMembers, HeldByYou: caller != "" && o.Holder == caller,
		}
	}
	return resp
}
