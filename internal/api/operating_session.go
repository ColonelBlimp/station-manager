package api

import (
	"context"
	stderr "errors"
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
	// sessionStateArchiveSwitching: an archive switch has closed the session and
	// the daemon is about to restart (the closing phase, ADR 0100 §4). No session
	// is open and none is opened until the switch has failed or the process ended.
	sessionStateArchiveSwitching = "archive_switching"
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
	outcome, err := ss.open(lifetime)
	if err == nil && outcome == sqlite.SessionRetired {
		return errLifetimeRetired
	}
	return err
}

// open is the session's start step, with what it did; an advance of the
// generation is published.
func (ss serverSessions) open(lifetime string) (sqlite.SessionOutcome, error) {
	st, outcome, err := ss.s.db.OpenOperatingSessionWithContext(context.Background(), lifetime, ss.s.sessionNow())
	if err != nil {
		return "", err
	}
	ss.s.publishSessionChange(st)
	return outcome, nil
}

// openSessionAtSetup opens the first session for the lifetime the admission
// holds, when setup's first save has made and recorded the default logbook
// (ADR 0100, Update 2026-11-19; W-0029 part 2, C). It runs with the admission's
// order held, and decides from the lifetime held there, not from what a read
// would say of it: a save that meets a replacement opens the session for the
// new lifetime, and one that meets an exit opens none for a lifetime that has
// gone. With nobody admitted it opens nothing; the next admission will.
//
// An error means the save must not succeed (operator ruling 2026-11-20): the
// record could not be read or written, or the default logbook just recorded is
// not there to bind a session to.
//
// In the closing phase of an archive switch it opens none, and the save
// succeeds (W-0029 part 3, G4): the next start's admission opens the session.
func (s *Server) openSessionAtSetup() error {
	var err error
	s.admission.observe(func(held string) {
		if held == "" || s.admission.closing {
			return
		}
		var outcome sqlite.SessionOutcome
		if outcome, err = (serverSessions{s}).open(held); err == nil && outcome == sqlite.SessionSetupPending {
			err = stderr.New("the default logbook is not recorded in the archive file")
		}
	})
	return err
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
	s.registerOperatingSessionControls(apiMux)
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
	// ContactCount leaves out contacts deleted since; HasContacts does not
	// (W-0029 part 4, D). It can be 0 while HasContacts is true.
	ContactCount int  `json:"contact_count"`
	HeldByYou    bool `json:"held_by_you"`
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
		resp = s.sessionResponse(st, caller, current, s.admission.closing)
	})
	if err != nil {
		s.logger.ErrorWith().Err(err).Msg("operating session: the state could not be read")
		s.writeError(w, http.StatusServiceUnavailable, operatingSessionUnavailable,
			"the operating session could not be read", op)
		return
	}
	s.writeJSON(w, http.StatusOK, resp)
}

// sessionResponse names the state FOR THE CALLER (W-0029 part 2, D1): a session
// held by the current lifetime, or by the caller, or an empty one, is open; one
// with contacts that another lifetime holds awaits the operator's Continue or
// New.
//
// "Or by the caller" is what a daemon restart needs. A new process has no
// current lifetime until the shell admits its own again, and the page's stream
// reconnects and reads before that. The app that holds the session must not be
// told it awaits an answer: admitting the same lifetime again changes nothing
// in the file, so no generation advances and no event follows to correct a
// question the page has drawn, and New pressed on that question would close the
// session the running app holds (the clean-room review of 2c362c3d).
//
// current is the admission's current lifetime
// as observed with the file, by the caller, and closing whether an archive
// switch is in its closing phase, observed in the same hold. That state comes
// first: whatever else is true of the file, no session will be opened in it by
// this process unless the switch fails.
func (s *Server) sessionResponse(st sqlite.OperatingSessionState, caller, current string, closing bool) operatingSessionResponse {
	resp := operatingSessionResponse{ArchiveID: s.servedArchiveID, Generation: st.Generation}
	o := st.Open
	switch {
	case closing:
		resp.State = sessionStateArchiveSwitching
	case o == nil && st.DefaultLogbookUUID == "":
		resp.State = sessionStateSetupPending
	case o == nil:
		resp.State = sessionStateNone
	case o.HasMembers && o.Holder != current && (caller == "" || o.Holder != caller):
		resp.State = sessionStateRecoveryPending
	default:
		resp.State = sessionStateOpen
	}
	if o != nil {
		resp.Session = &operatingSessionBody{
			ID: o.ID, LogbookUUID: o.LogbookUUID, OpenedAt: o.OpenedAt, SittingStartedAt: o.IntervalStartedAt,
			HasContacts: o.HasMembers, ContactCount: o.ContactCount, HeldByYou: caller != "" && o.Holder == caller,
		}
	}
	return resp
}

// ---- The exit report (ADR 0100 §3; W-0029 part 2, E) ----

// The exit report's own codes.
const (
	// operatingSessionStale: the generation named is not the current one. The
	// answer carries the current generation; the exit, and only the exit, may
	// be repeated at it by the same lifetime without admitting again.
	operatingSessionStale = "operating_session_stale"
	// operatingSessionExitNotCurrent: the lifetime is not the current one and
	// still holds the open session (the daemon restarted before its exit was
	// stored). Terminal: the close is not confirmed, and the next start asks
	// (operator ruling 2026-11-19).
	operatingSessionExitNotCurrent = "operating_session_exit_not_current"
)

// SetCompletionDrain injects the wait for FT8 completion logging already
// earned: cmd/smd's tracked completion jobs. A confirmed exit calls it after
// it has stopped the scopes and sealed live entry, and before it commits, so a
// contact that completed on the air is stored before its session closes. It
// must not cancel anything. Nil when there is no FT8 subsystem.
func (s *Server) SetCompletionDrain(drain func()) { s.completionDrain = drain }

func (s *Server) registerOperatingSessionExit(apiMux *routeMux) {
	// Not behind the archive guard: a closing app whose archive was switched
	// under it names the archive it closed in, and must still be answered. The
	// archive is checked for the current lifetime only (exitOperatingSession).
	apiMux.HandleFunc("POST /v1/operating-session/exit", s.handleOperatingSessionExit)
}

type operatingSessionExitRequest struct {
	Generation *int64 `json:"generation"`
}

type operatingSessionExitResponse struct {
	Generation      int64 `json:"generation"`
	AlreadyRecorded bool  `json:"already_recorded"`
}

// operatingSessionStaleResponse is the error envelope with the current
// generation beside it.
type operatingSessionStaleResponse struct {
	Code       string    `json:"code"`
	Message    string    `json:"message"`
	Op         errors.Op `json:"op"`
	Generation int64     `json:"generation"`
}

// exitResult is what the exit decided, for the handler to answer.
type exitResult struct {
	status          int // 0 for success
	code, message   string
	generation      int64
	alreadyRecorded bool
	cause           error // logged, never sent
}

func (s *Server) handleOperatingSessionExit(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleOperatingSessionExit"
	var req operatingSessionExitRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	lifetime := appLifetime(r)
	if lifetime == "" {
		s.writeError(w, http.StatusBadRequest, "app_lifetime_required",
			"an exit names the app lifetime that is exiting", op)
		return
	}
	if req.Generation == nil {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "generation is required", op)
		return
	}
	res := s.exitOperatingSession(r, lifetime, *req.Generation)
	switch {
	case res.status == 0:
		s.writeJSON(w, http.StatusOK, operatingSessionExitResponse{Generation: res.generation, AlreadyRecorded: res.alreadyRecorded})
	case res.code == operatingSessionStale:
		s.writeJSON(w, res.status, operatingSessionStaleResponse{Code: res.code, Message: res.message, Op: op, Generation: res.generation})
	default:
		if res.cause != nil {
			s.logger.ErrorWith().Err(res.cause).Msg("operating session: the exit could not be stored; it is not confirmed")
		}
		s.writeError(w, res.status, res.code, res.message, op)
	}
}

func exitUnavailable(cause error) exitResult {
	return exitResult{status: http.StatusServiceUnavailable, code: operatingSessionUnavailable,
		message: "the exit could not be stored; the close is not confirmed", cause: cause}
}

func exitStale(generation int64) exitResult {
	return exitResult{status: http.StatusConflict, code: operatingSessionStale, generation: generation,
		message: "the session state has changed since this exit was sent; it may be sent again at the generation given"}
}

// exitOperatingSession decides an exit report, with the admission's order held
// from start to finish, so it is ordered against every admission and every
// other exit. Who may report an exit is decided from the lifetime the
// admission HOLDS, not from what a read would say of it:
//
//  1. the exit is already recorded in the served file: acknowledged, nothing
//     changes and nothing is stopped, whoever is current, whatever generation
//     and archive are named;
//  2. it is the held lifetime: exitCurrent;
//  3. it is not, and holds the open session: refused for good;
//  4. it is not, and holds none: recorded and acknowledged, nothing else.
//
// It does not follow the request's context: an exit the client gave up on must
// not be left sealed, or half decided.
func (s *Server) exitOperatingSession(r *http.Request, lifetime string, generation int64) exitResult {
	a := &s.admission
	a.mu.Lock()
	defer a.mu.Unlock()
	ctx := context.Background()

	st, err := s.db.OperatingSessionStateWithContext(ctx, lifetime)
	if err != nil {
		return exitUnavailable(err)
	}
	if st.ExitRecorded {
		return exitResult{generation: st.Generation, alreadyRecorded: true}
	}
	if lifetime != a.current {
		return s.exitNotCurrent(ctx, a, lifetime, generation)
	}
	// The held lifetime. A refusal here has sealed nothing.
	if status, code, message := s.archiveRefusal(r, true); status != 0 {
		return exitResult{status: status, code: code, message: message}
	}
	// In the closing phase of an archive switch the exit is acknowledged without
	// change (W-0029 part 3, G3): the switch has closed the session, so there is
	// none whose close a stale generation could confirm by mistake, and an app
	// that has not yet heard of the close must still be able to close cleanly.
	// The exit is recorded at the file's generation; it takes and releases only
	// its own seal, never the switch's.
	if a.closing {
		generation = st.Generation
	}
	if generation != st.Generation {
		return exitStale(st.Generation)
	}
	return s.exitCurrent(ctx, a, lifetime, generation)
}

// exitNotCurrent is the exit of a lifetime this process does not hold: the
// archive was switched under a closing app, the daemon restarted, or a later
// start replaced it. It changes no session and stops nothing.
func (s *Server) exitNotCurrent(ctx context.Context, a *appAdmission, lifetime string, generation int64) exitResult {
	st, outcome, err := s.db.RecordOperatingAppExitWithContext(ctx, lifetime, generation, false, s.sessionNow())
	switch {
	case err != nil:
		return exitUnavailable(err)
	case outcome == sqlite.SessionNotCurrent:
		return exitResult{status: http.StatusConflict, code: operatingSessionExitNotCurrent,
			message: "this start of Station Manager is not the current one and its session is still open; the close is not confirmed"}
	}
	a.retire(lifetime)
	return exitResult{generation: st.Generation, alreadyRecorded: outcome == sqlite.SessionExitAlreadyRecorded}
}

// exitCurrent is the exit of the held lifetime, in the order an archive switch
// uses (ADR 0100 §4): new work is excluded, what was earned is finished, and
// only then is the exit stored.
//
//   - every scope is stopped, and live Phone/CW entry sealed with the exit's
//     own seal, which waits for submits already admitted;
//   - FT8 completion logging already earned finishes; none is cancelled;
//   - one commit records the exit and, if the lifetime holds the open session,
//     closes it;
//   - the lifetime is retired, the exit's seal released, the change published.
//
// If the commit fails, or refuses the exit as stale, the seal is released and
// the lifetime published to its scopes again: the exit is not confirmed, and
// operation goes on. (Scopes a failed replacement had already stopped are left
// stopped: the lifetime's own admission repairs that.) After the commit
// nothing restores operation under it.
func (s *Server) exitCurrent(ctx context.Context, a *appAdmission, lifetime string, generation int64) exitResult {
	// The scopes may be stopped already: a replacement that failed leaves the
	// held lifetime so until its own admission repairs it. A failed exit then
	// gives back what it took and no more.
	wasStopped := a.stopped
	for _, sc := range a.scopes {
		sc.RetireOperating()
	}
	a.stopped = true
	if s.exitSeal != nil {
		_ = s.exitSeal.Seal() // never refuses: it waits for what is already running
	}
	restore := func() {
		if s.exitSeal != nil {
			s.exitSeal.Release()
		}
		if wasStopped {
			return
		}
		for _, sc := range a.scopes {
			sc.PublishOperating(lifetime)
		}
		a.stopped = false
	}
	if s.completionDrain != nil {
		s.completionDrain()
	}
	st, outcome, err := s.db.RecordOperatingAppExitWithContext(ctx, lifetime, generation, true, s.sessionNow())
	switch {
	case err != nil:
		restore()
		return exitUnavailable(err)
	case outcome == sqlite.SessionStale:
		restore()
		s.publishSessionChange(st) // the default-epoch test may have closed a session
		return exitStale(st.Generation)
	case outcome != sqlite.SessionApplied && outcome != sqlite.SessionExitRecorded && outcome != sqlite.SessionExitAlreadyRecorded:
		// No other outcome says the exit is stored. Nothing is retired on a
		// word this code does not know.
		restore()
		return exitUnavailable(stderr.New("the exit's commit answered " + string(outcome)))
	}
	a.retire(lifetime)
	a.current, a.stopped = "", false
	if s.exitSeal != nil {
		s.exitSeal.Release()
	}
	s.publishSessionChange(st)
	return exitResult{generation: st.Generation, alreadyRecorded: outcome == sqlite.SessionExitAlreadyRecorded}
}

// HoldSessionOrder takes the order an app start, an exit report and setup's
// first session each run in, and returns its release. It is for the archive
// manager, which orders a change of the default logbook and an archive
// activation with them (archive.Manager.SetSessionOrder; W-0029 part 2, G6):
// while it is held none of those begins, and it is not given while one is
// under way. The holder must take it before any seal and any write (the one
// order: this, then a scope's gate, then the live-entry gate, then the file).
func (s *Server) HoldSessionOrder() (release func()) {
	s.admission.mu.Lock()
	return s.admission.mu.Unlock
}

// ---- An archive switch (ADR 0100 §4; W-0029 part 3) ----

// SwitchSession is the operating session's part in an archive switch, for the
// archive manager (archive.Manager.SetSwitch). The manager calls it with the
// session order held (HoldSessionOrder), so each method is wholly before or
// wholly after any app start, exit report, read and setup save, and none of
// them takes the order itself.
type SwitchSession struct{ s *Server }

// SwitchSession returns the port.
func (s *Server) SwitchSession() *SwitchSession { return &SwitchSession{s: s} }

// Drain waits for FT8 completion logging already earned: the wait a confirmed
// exit uses (SetCompletionDrain). The caller has sealed FT8, so nothing new is
// earned while it waits. It cancels nothing and has no limit of its own
// (operator ruling 2026-11-21).
func (p *SwitchSession) Drain() {
	if p.s.completionDrain != nil {
		p.s.completionDrain()
	}
}

// Close closes the open session in the archive being left, whoever holds it,
// with its interval's end confirmed, and begins the closing phase: from here
// until Failed, or the end of the process, no session is opened in this
// archive. With no session open it changes nothing in the file and the phase
// begins all the same. The change is published after its commit.
//
// An error means nothing was closed and the phase has not begun.
func (p *SwitchSession) Close() error {
	s := p.s
	// Not a request's context: a close the client gave up on must not be left
	// half decided, as an exit is not.
	st, _, err := s.db.CloseOperatingSessionForSwitchWithContext(context.Background(), false, s.sessionNow())
	if err != nil {
		return err
	}
	s.admission.closing = true
	s.publishSessionChange(st)
	return nil
}

// Failed ends the closing phase of a switch that failed after its close. The
// close stands. One new session is opened, by the ordinary rule, for the app
// that is current, before the switch answers; with no app current none is, and
// the next app start opens it.
//
// If the session cannot be opened the admission is left as a failed start step
// leaves it (app_admission.go): the scopes stopped, authorizing nobody, and
// the app's read saying it is not current, so that the app's own next
// admission opens the session and publishes the scopes. Scopes an earlier
// failed step had already stopped are in that state already, and are left to
// the same repair.
func (p *SwitchSession) Failed() {
	s := p.s
	a := &s.admission
	a.closing = false
	if a.current == "" || a.stopped {
		return
	}
	if err := a.startSession(a.current); err != nil {
		for _, sc := range a.scopes {
			sc.RetireOperating()
		}
		a.stopped = true
		s.logger.ErrorWith().Err(err).Msg("operating session: no session could be opened after a failed archive switch; the app's next admission opens it")
	}
}
