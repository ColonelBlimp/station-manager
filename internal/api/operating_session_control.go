package api

import (
	"context"
	stderr "errors"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// The session's two controls (ADR 0100 §3, §5, §6, §8; W-0029 part 4): New and
// Continue. They are the two answers to the recovery question, and the Session
// panel's controls: the routes are built whole (operator ruling 2026-11-23),
// as the transitions in internal/database/sqlite/operating_session.go are.
//
//   - New closes the open session and opens one for the caller. Closing the
//     caller's own is new_session; closing another lifetime's is the recovery
//     answer New, unconfirmed_exit.
//   - Continue names a session. The open one held by another lifetime: the
//     recovery answer Continue. A closed one of the default logbook: it is
//     opened again and the current one closed with continued_other.
//
// A control changes the session's record and nothing else. It does not arm TX,
// start, stop or resume an FT8 run, stop or publish an operating scope, take or
// release a seal, or move a contact.

// The controls' own codes.
const (
	// operatingSessionIneligible: with no such question waiting, Continue named
	// a session that cannot be continued (not a closed session of the default
	// logbook in this archive, or the one the caller already holds).
	operatingSessionIneligible = "operating_session_ineligible"
	// operatingSessionRecoveryPending: a session left open by an earlier start
	// awaits its answer, and Continue named any session but that one, a closed
	// one or one that does not exist. That question is answered first: this
	// comes before operatingSessionIneligible, and the named session is not
	// looked up.
	operatingSessionRecoveryPending = "operating_session_recovery_pending"
	// operatingSessionSetupPending: New with no default logbook to bind to.
	operatingSessionSetupPending = "operating_session_setup_pending"
	// operatingSessionSwitching: an archive switch has closed the session and
	// no session is opened until the switch has failed or the process ended
	// (the closing phase; W-0029 part 3, G7).
	operatingSessionSwitching = "operating_session_switching"
)

func (s *Server) registerOperatingSessionControls(apiMux *routeMux) {
	apiMux.HandleFunc("POST /v1/operating-session/new", s.handleOperatingSessionNew)
	apiMux.HandleFunc("POST /v1/operating-session/continue", s.handleOperatingSessionContinue)
}

type operatingSessionNewRequest struct {
	Generation *int64 `json:"generation"`
}

type operatingSessionContinueRequest struct {
	Generation *int64 `json:"generation"`
	SessionID  string `json:"session_id"`
}

// operatingSessionStaleStateResponse is the error envelope of a control
// refused as stale, with the state as it now is beside it (ADR 0100 §5: the
// window redraws to it, and sends nothing again by itself).
type operatingSessionStaleStateResponse struct {
	Code             string                   `json:"code"`
	Message          string                   `json:"message"`
	Op               errors.Op                `json:"op"`
	OperatingSession operatingSessionResponse `json:"operating_session"`
}

// sessionTransition is a control's transaction for a lifetime at an instant.
type sessionTransition func(ctx context.Context, lifetime string, now time.Time) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error)

// controlResult is what a control decided, for the handler to answer.
type controlResult struct {
	status        int // 0 for success
	code, message string
	// state is the state the control left (success) or found (stale): the
	// control's own transaction's, named under the admission's order.
	state operatingSessionResponse
	cause error // logged, never sent
}

func (s *Server) handleOperatingSessionNew(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionNew"
	if !s.checkArchive(w, r, op, true) {
		return
	}
	var req operatingSessionNewRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	lifetime, ok := s.controlCaller(w, r, op, req.Generation)
	if !ok {
		return
	}
	generation := *req.Generation
	s.writeControl(w, op, s.answerOperatingSession(lifetime, func(ctx context.Context, lifetime string, now time.Time) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
		return s.db.NewOperatingSessionWithContext(ctx, lifetime, generation, now)
	}))
}

func (s *Server) handleOperatingSessionContinue(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionContinue"
	if !s.checkArchive(w, r, op, true) {
		return
	}
	var req operatingSessionContinueRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	lifetime, ok := s.controlCaller(w, r, op, req.Generation)
	if !ok {
		return
	}
	if req.SessionID == "" {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "session_id is required", op)
		return
	}
	generation, session := *req.Generation, req.SessionID
	s.writeControl(w, op, s.answerOperatingSession(lifetime, func(ctx context.Context, lifetime string, now time.Time) (sqlite.OperatingSessionState, sqlite.SessionOutcome, error) {
		return s.db.ContinueOperatingSessionWithContext(ctx, lifetime, generation, session, now)
	}))
}

// controlCaller is the request's lifetime, once the request is whole enough to
// be decided: it names a lifetime and a generation.
func (s *Server) controlCaller(w http.ResponseWriter, r *http.Request, op errors.Op, generation *int64) (string, bool) {
	lifetime := appLifetime(r)
	switch {
	case lifetime == "":
		s.writeError(w, http.StatusBadRequest, "app_lifetime_required",
			"a session control names the app lifetime that asks for it", op)
	case generation == nil:
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "generation is required", op)
	default:
		return lifetime, true
	}
	return "", false
}

// answerOperatingSession decides a control with the admission's order held
// from start to finish, so it is ordered against every app start, exit report,
// setup save, default change and archive switch, and against every other
// control (W-0029 part 4, F).
//
// Who may ask is decided from the lifetime the admission HOLDS, as an exit is,
// not from what a read would say of it: a held lifetime whose scopes a failed
// step left stopped may ask, and the control repairs nothing of that (its own
// admission does). This is the gate for a session control; FT8's authorization
// of a scope is another rule and is not consulted.
//
// The state returned is the transaction's own, named here under the same hold:
// nothing is read again after the commit, so no database step can fail between
// a control that changed something and its answer. It does not follow the
// request's context: a control the client gave up on is still decided whole.
func (s *Server) answerOperatingSession(lifetime string, transition sessionTransition) controlResult {
	a := &s.admission
	a.mu.Lock()
	defer a.mu.Unlock()

	if _, gone := a.retired[lifetime]; gone {
		return controlRetired()
	}
	exited, err := a.exitRecorded(lifetime)
	switch {
	case err != nil:
		return controlUnavailable(err)
	case exited:
		return controlRetired()
	case lifetime != a.current:
		return controlResult{status: http.StatusConflict, code: "app_lifetime_not_current",
			message: "this is not the Station Manager that is operating the station"}
	case a.closing:
		return controlResult{status: http.StatusConflict, code: operatingSessionSwitching,
			message: "The archive is being switched. Nothing was changed."}
	}

	st, outcome, err := transition(context.Background(), lifetime, s.sessionNow())
	if err != nil {
		return controlUnavailable(err)
	}
	// A refusal is not a rollback (ADR 0100 §5): a session the default-epoch
	// test closed is committed, and published, even when the control is then
	// refused as stale.
	s.publishSessionChange(st)
	state := s.sessionResponse(st, lifetime, a.current, a.closing)
	switch outcome {
	case sqlite.SessionApplied:
		return controlResult{state: state}
	case sqlite.SessionStale:
		return controlResult{status: http.StatusConflict, code: operatingSessionStale, state: state,
			message: "the session state has changed since this was sent; nothing was applied, and the current state is given"}
	case sqlite.SessionRetired:
		return controlRetired()
	case sqlite.SessionSetupPending:
		return controlResult{status: http.StatusConflict, code: operatingSessionSetupPending,
			message: "there is no default logbook yet; finish setup first"}
	case sqlite.SessionIneligible:
		return controlResult{status: http.StatusConflict, code: operatingSessionIneligible,
			message: "that session cannot be continued: it is not a closed session of the default logbook in this archive"}
	case sqlite.SessionRecoveryPending:
		return controlResult{status: http.StatusConflict, code: operatingSessionRecoveryPending,
			message: "a session left open by an earlier start is waiting for its answer; answer that first"}
	}
	// No other outcome says what the file now holds, so this one does not say
	// that nothing changed.
	return controlResult{status: http.StatusServiceUnavailable, code: operatingSessionUnavailable,
		message: "the session's state is not confirmed; read it again",
		cause:   stderr.New("the control answered " + string(outcome))}
}

func controlRetired() controlResult {
	return controlResult{status: http.StatusConflict, code: "app_lifetime_retired",
		message: "another start of Station Manager has taken over; close this one"}
}

func controlUnavailable(cause error) controlResult {
	return controlResult{status: http.StatusServiceUnavailable, code: operatingSessionUnavailable,
		message: "the session could not be changed; nothing was changed", cause: cause}
}

func (s *Server) writeControl(w http.ResponseWriter, op errors.Op, res controlResult) {
	switch {
	case res.status == 0:
		s.writeJSON(w, http.StatusOK, res.state)
	case res.code == operatingSessionStale:
		s.writeJSON(w, res.status, operatingSessionStaleStateResponse{Code: res.code, Message: res.message, Op: op, OperatingSession: res.state})
	default:
		if res.cause != nil {
			s.logger.ErrorWith().Err(res.cause).Msg("operating session: a control could not be applied")
		}
		s.writeError(w, res.status, res.code, res.message, op)
	}
}
