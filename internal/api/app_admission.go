package api

import (
	"context"
	"encoding/base64"
	stderr "errors"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// App admission (W-0027 C4a; ADR 0102 ruling 6; ADR 0100 §3, App lifetimes).
//
// The desktop app's main process makes one random
// identity at each start, its app lifetime, and sends it on every request beside the
// credential. Before it loads the workspace it is admitted: it reads this process's
// marker and the admission generation, and then admits against what it read.
//
//   - The caller is already current: success, nothing changes. A repeat, or a retry
//     after a lost answer, is harmless.
//   - The caller has been replaced in this process: refused for good. It never comes
//     back, by a late duplicate, a retry or a fresh read (codex-126, finding 1).
//   - The marker or the generation it read is not this process's now: refused as stale,
//     and the current app is not touched. A lifetime that has never been replaced may
//     read again and admit again; that is how the running app gets past an admission a
//     dead app had already sent.
//   - Otherwise it replaces the current lifetime: every scope is sealed and stopped, the
//     replaced lifetime is retired, and only then is the new one published.
//
// WHAT THE ORDER PROMISES. Admissions are handled one at a time and each succeeds only
// against the generation it read, so successful admissions are in one order and nothing
// an earlier app had already sent can succeed after a later admission. It does not
// establish which of two shells was started later: a random label and a read cannot. The
// app is single-instance, so two shells of one operator do not run at once.
//
// The admission itself stores nothing. A new daemon process has no current lifetime and
// none retired in memory, and never takes one as current without an admission. What IS
// decided here from the archive file is the operating session (ADR 0100 §3; W-0029 part
// 2; operating_session.go): a lifetime whose exit the file records is refused as retired,
// and a lifetime becomes current only after the session's start step has run.
//
// Only operating a scope asks for the current lifetime (internal/ft8/operating.go).
// Reading, logging, settings and every stream are served whatever lifetime a request
// carries, or none.

const (
	// appLifetimeHeader carries the app lifetime. The shell's main process adds it, as it
	// adds the credential; it is a label, not a secret.
	appLifetimeHeader = "X-Sm-App-Lifetime"

	// daemonInstanceHeader carries this process's marker on what is served to the app, so
	// its main process can see a restart when response headers arrive, without polling.
	daemonInstanceHeader = "X-Sm-Daemon-Instance"

	// admissionGenerationHeader rides beside the marker: the admission generation as it is
	// now. An app that has been replaced in this same process sees it change on its next
	// response, asks, and is told it is retired (codex-129, finding 2).
	admissionGenerationHeader = "X-Sm-Admission-Generation"

	// appLifetimeBytes is the size of the identity: 16 random bytes, base64url unpadded.
	appLifetimeBytes = 16
)

type appLifetimeKey struct{}

// appLifetime is the request's validated app lifetime, or "" when it carried none.
func appLifetime(r *http.Request) string {
	v, _ := r.Context().Value(appLifetimeKey{}).(string)
	return v
}

// validAppLifetime reports whether v has the identity's exact form.
func validAppLifetime(v string) bool {
	if len(v) != base64.RawURLEncoding.EncodedLen(appLifetimeBytes) {
		return false
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(v)
	return err == nil && len(b) == appLifetimeBytes
}

// withAppLifetime attaches the lifetime a served request presented. More than one
// header, or a malformed one, is refused, as more than one credential is.
func (s *Server) withAppLifetime(w http.ResponseWriter, r *http.Request, presented []string) (*http.Request, bool) {
	const op errors.Op = "api.withAppLifetime"
	switch {
	case len(presented) == 0:
		return r, true
	case len(presented) > 1 || !validAppLifetime(presented[0]):
		s.writeError(w, http.StatusBadRequest, "invalid_app_lifetime",
			"the app lifetime must be one well-formed identity", op)
		return nil, false
	}
	return r.WithContext(context.WithValue(r.Context(), appLifetimeKey{}, presented[0])), true
}

// operatingScope is a scope's boundary at a replacement: seal and stop, then publish.
//
// A scope is a kind of operation in which the daemon can transmit by itself once the
// operator has started it. FT8 and FT4 are the one scope built. Every such scope is held
// by the operator's presence in it (operator ruling 2026-11-17: "if we add RTTY and it is
// in unattended operation, that scope (or any other scope) should be covered"). A NEW
// SCOPE MUST:
//
//   - have an event stream that its own view holds open, and name it as its presence: a
//     subscription made under the current app lifetime (internal/ft8/presence.go is the
//     helper, with nothing of FT8 in it; move it out when the second scope is real);
//   - accept whatever lets it transmit by itself only from the current lifetime and only
//     while that lifetime is present, checked under the gate its own stop takes and held
//     until the command has taken effect (internal/ft8/operating.go);
//   - disarm, by its guaranteed-stop path and with the cause the daemon uses when nobody
//     is attending, when the lifetime holding it has had no stream for the grace;
//   - implement this interface and be added to the admission's scopes, so that a
//     replacement stops it before the new lifetime is current;
//   - say so in docs/v2-design/api-endpoints.md.
type operatingScope interface {
	RetireOperating()
	PublishOperating(lifetime string)
}

var (
	errAdmissionStale  = stderr.New("the admission was read against an earlier state")
	errLifetimeRetired = stderr.New("the app lifetime has been replaced")
	// errSessionUnavailable: the operating session's record could not be read or
	// written, so the lifetime was not admitted (operator ruling 2026-11-20).
	errSessionUnavailable = stderr.New("the operating session is unavailable")
)

// admissionSessions is what an admission needs of the operating session's
// record (ADR 0100 §3; operating_session.go). Nil in a test of the admission
// alone: then there is no session work.
type admissionSessions interface {
	// exitRecorded is whether the served archive records the lifetime's exit.
	exitRecorded(lifetime string) (bool, error)
	// start is the session's start step for a lifetime about to be current.
	start(lifetime string) error
}

// appAdmission is the one current lifetime and the order of its replacements. mu is held
// across a replacement, the scopes' stop included, and is taken before any scope's own
// lock; no scope calls back into it.
type appAdmission struct {
	mu         sync.Mutex
	current    string
	generation uint64
	retired    map[string]struct{}
	scopes     []operatingScope
	// sessions is the operating session's record, consulted under mu.
	sessions admissionSessions
	// stopped is true while the scopes are stopped and published to nobody
	// because a session step failed after they were stopped. The next admission
	// that succeeds, the current lifetime's own included, publishes them.
	stopped bool
	// closing is true in the closing phase of an archive switch (ADR 0100 §4):
	// from the switch's close of the session until the switch fails or the
	// process ends. No session is opened in it.
	closing bool
	// awaitEarned waits for FT8 completion logging already earned: the wait a
	// confirmed exit and an archive switch use (Server.SetCompletionDrain). It
	// is called under mu by the admissions that start a session (awaitBeforeStart).
	// Nil until the session membership is connected (operating_session.go, the
	// one function that connects it): then no admission waits.
	awaitEarned func()

	// published is the generation as last published, for the response header. It is read
	// without mu, so a request is never held up behind a replacement that is stopping a
	// scope.
	published atomic.Uint64

	// holder is current, published for the logging step (W-0029 part 5, D): the
	// lifetime that holds a session a contact has to open because none is open.
	// It is read without mu, since logging never takes the admission's order
	// (the one order: this, a scope's gate, the live-entry gate, the file), and
	// only setCurrent writes it, so the two never differ once mu is released.
	holder atomic.Pointer[string]
}

// setCurrent makes a lifetime, or with "" nobody, the one the admission holds,
// and publishes it as holding. The caller holds mu. It is the only place
// current is written: a failed replacement does not come here, so the lifetime
// it leaves in place, with its scopes stopped, is still the one published.
func (a *appAdmission) setCurrent(lifetime string) {
	a.current = lifetime
	a.holder.Store(&lifetime)
}

// holding is the lifetime published as holding, "" when no app is admitted or
// the one that was has confirmed its exit. It is one observation: the caller's
// transaction is not ordered with the admission by it, which is why the
// logging step also checks the file for that lifetime's recorded exit.
func (a *appAdmission) holding() string {
	if h := a.holder.Load(); h != nil {
		return *h
	}
	return ""
}

// read answers the generation and whether the lifetime is current. While the
// scopes are stopped after a failed session step, nobody reads as current, the
// lifetime the admission still holds included: the app stops at a read that
// says "current" and sends no admit (desktop/src/admission.mjs), and it is the
// admit that completes what the failure left undone.
//
// answered is false when the caller went away while the read waited for the
// admission's order (lockUnlessGone): then nothing was read.
func (a *appAdmission) read(ctx context.Context, lifetime string) (generation uint64, current, answered bool) {
	if !a.lockUnlessGone(ctx) {
		return 0, false, false
	}
	defer a.mu.Unlock()
	return a.generation, lifetime != "" && lifetime == a.current && !a.stopped, true
}

// orderPoll is the interval at which a read that waits for the admission's
// order tries for it again. It is when the read looks, not how soon it is
// answered: see lockUnlessGone.
const orderPoll = 2 * time.Millisecond

// lockUnlessGone takes the admission's order for a READ, and gives up when the
// caller has gone. It reports whether mu is now held.
//
// An admission can hold the order for as long as earned logging takes to
// finish (awaitBeforeStart), and so can an exit. The shell gives up on a
// request after five seconds and asks again: the read of the admission when a
// start has not been answered, and the read of the session when the operator
// closes a window that is still waiting (desktop/src/admission.mjs,
// closing.mjs). A read that waited with Lock would keep its handler, and with
// it one of the daemon's request slots, until the wait ended, whether or not
// anyone was listening. A long wait would fill the slots with reads nobody
// wants, and a request with no part in the start, a contact being logged
// included, would be refused as server_busy (the reviews of W-0029 part 5,
// slice 2: codex-261, codex-262).
//
// A mutex's Lock cannot be given up, so the read tries for the order and, while
// its caller is there, tries again at each orderPoll. A read that gets the
// order has exactly what Lock would have given it: one snapshot, ordered with
// every admission and exit. The cost is that a waiting read is not queued with
// the writers that use Lock. It has no promised time: a writer can take the
// order between two of its tries and hold it past the next, and it is then
// passed again.
//
// It is for the two reads only. Everything that changes the admission or the
// session takes the order with Lock, and is not abandoned with its caller.
func (a *appAdmission) lockUnlessGone(ctx context.Context) bool {
	if a.tryForRead(ctx) {
		return true
	}
	again := time.NewTicker(orderPoll)
	defer again.Stop()
	for {
		select {
		case <-ctx.Done():
			return false
		case <-again.C:
			if a.tryForRead(ctx) {
				return true
			}
		}
	}
}

// tryForRead takes the order if it is free and the caller is still there, and
// reports whether mu is held. The caller is looked at after the order is
// taken, every time: a free order must not be taken for a request that is
// already over, at the first try or at a tick that came due together with the
// cancellation. The read would then run with a cancelled context, and the
// session's would fail in the database, be logged as the file's fault, and
// answer 503 to nobody (the review of W-0029 part 5, slice 2: codex-263).
func (a *appAdmission) tryForRead(ctx context.Context) bool {
	if !a.mu.TryLock() {
		return false
	}
	if ctx.Err() != nil {
		a.mu.Unlock()
		return false
	}
	return true
}

// observeUnlessGone is observe for a read whose caller may give up: it reports
// false, having run nothing, when the caller went away while it waited for the
// order (lockUnlessGone).
func (a *appAdmission) observeUnlessGone(ctx context.Context, fn func(current string)) bool {
	if !a.lockUnlessGone(ctx) {
		return false
	}
	defer a.mu.Unlock()
	fn(a.current)
	return true
}

// admit applies the rule at the head of this file and returns the generation after it.
func (a *appAdmission) admit(lifetime, instance string, generation uint64) (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lifetime == a.current {
		return a.generation, a.complete(lifetime)
	}
	if _, gone := a.retired[lifetime]; gone {
		return a.generation, errLifetimeRetired
	}
	// A recorded exit comes before anything is stopped, as the in-memory
	// retirement does: a lifetime that has exited touches neither the current
	// app nor its scopes, in this process or after a restart of the daemon.
	exited, readErr := a.exitRecorded(lifetime)
	if exited {
		return a.generation, errLifetimeRetired
	}
	if instance != processInstance || generation != a.generation {
		return a.generation, errAdmissionStale
	}
	// Seal and stop every scope before anything is published: between the two no
	// lifetime is current, so the old one cannot arm again and the new one not early.
	for _, sc := range a.scopes {
		sc.RetireOperating()
	}
	// The session's start step, before the lifetime is current. If the record
	// cannot be read or written the lifetime is not admitted: nothing below
	// happens, and the scopes stay stopped until an admission succeeds.
	a.stopped = true
	if readErr == nil {
		a.awaitBeforeStart()
		readErr = a.startSession(lifetime)
	}
	if readErr != nil {
		return a.generation, readErr
	}
	a.stopped = false
	if a.current != "" {
		a.retire(a.current)
	}
	a.setCurrent(lifetime)
	a.generation++
	a.published.Store(a.generation)
	for _, sc := range a.scopes {
		sc.PublishOperating(lifetime)
	}
	return a.generation, nil
}

// retire records in memory that a lifetime never comes back to this process.
// The caller holds mu.
func (a *appAdmission) retire(lifetime string) {
	if a.retired == nil {
		a.retired = map[string]struct{}{}
	}
	a.retired[lifetime] = struct{}{}
}

// complete is the admission of the lifetime that is already current. It changes
// nothing, unless a failed step left the scopes stopped: then it runs the
// session's step and publishes them to this lifetime, so a success never
// leaves the scopes authorizing nobody. Publishing is not arming.
func (a *appAdmission) complete(lifetime string) error {
	if !a.stopped {
		return nil
	}
	a.awaitBeforeStart()
	if err := a.startSession(lifetime); err != nil {
		return err
	}
	a.stopped = false
	for _, sc := range a.scopes {
		sc.PublishOperating(lifetime)
	}
	return nil
}

func (a *appAdmission) exitRecorded(lifetime string) (bool, error) {
	if a.sessions == nil {
		return false, nil
	}
	exited, err := a.sessions.exitRecorded(lifetime)
	if err != nil {
		return false, stderr.Join(errSessionUnavailable, err)
	}
	return exited, nil
}

// awaitBeforeStart is the wait before a leftover session is judged empty
// (W-0029 part 5, E). The start step discards an open session that has no
// contact and belongs to another lifetime. A contact an earlier app earned on
// the air can still be on its way to the file when the next app starts: its
// scopes are stopped by now, so nothing new is earned, but its logging job may
// not have written. That job is waited for here, so the contact is in the
// leftover session before the session is judged, and the session is kept and
// asked about instead of thrown away with the contact left to open another.
//
// It is called by the admissions that start a session, each with the scopes
// already stopped: a first admission or a replacement (admit), and the held
// lifetime admitted again after a failed step (complete). It is NOT in
// startSession: a failed archive switch calls that too (SwitchSession.Failed),
// after its seals are released and with the scopes running. In the closing
// phase of a switch no session is started, so nothing is waited for.
//
// The wait cancels nothing and has no limit of its own (operator ruling
// 2026-11-24): the app stays on its waiting page until the logging has
// finished. It runs with mu held, as the exit's does. That is safe because
// logging takes no lock of the admission's, before its commit or after
// (W-0029 part 5, C5).
func (a *appAdmission) awaitBeforeStart() {
	if a.awaitEarned == nil || a.closing {
		return
	}
	a.awaitEarned()
}

// startSession is the session's start step. In the closing phase of an archive
// switch it does nothing: the lifetime is admitted, by the order ruled on
// 2026-11-20, and given no session (W-0029 part 3, G1). The start after the
// restart, or the switch's own failure (SwitchSession.Failed), opens one.
func (a *appAdmission) startSession(lifetime string) error {
	if a.sessions == nil || a.closing {
		return nil
	}
	if err := a.sessions.start(lifetime); err != nil {
		if stderr.Is(err, errLifetimeRetired) {
			return err
		}
		return stderr.Join(errSessionUnavailable, err)
	}
	return nil
}

// observe runs fn with the admission's order held and the lifetime that is
// current: what fn reads from the archive file and this lifetime are then one
// snapshot, since every change of the session's record that the admission
// orders takes the same lock. fn must not call back into the admission.
func (a *appAdmission) observe(fn func(current string)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	fn(a.current)
}

type appLifetimeState struct {
	Instance   string `json:"daemon_instance"`
	Generation uint64 `json:"generation"`
	Current    bool   `json:"current"`
}

type appLifetimeAdmitRequest struct {
	Instance   string  `json:"daemon_instance"`
	Generation *uint64 `json:"generation"`
}

// registerAppAdmission declares the two admission routes. The desktop app's main process
// reads, then admits its lifetime, before it loads the workspace.
func (s *Server) registerAppAdmission(apiMux *routeMux) {
	apiMux.HandleFunc("GET /v1/app/lifetime", s.handleAppLifetimeRead)
	apiMux.HandleFunc("POST /v1/app/lifetime", s.handleAppLifetimeAdmit)
}

// handleAppLifetimeRead answers the read: this process's marker, the admission
// generation, and whether the caller is the current lifetime. A caller that
// has gone while the read waited for the admission's order is answered
// nothing (appAdmission.lockUnlessGone).
func (s *Server) handleAppLifetimeRead(w http.ResponseWriter, r *http.Request) {
	st := appLifetimeState{Instance: processInstance}
	var answered bool
	if st.Generation, st.Current, answered = s.admission.read(r.Context(), appLifetime(r)); answered {
		s.writeJSON(w, http.StatusOK, st)
	}
}

// handleAppLifetimeAdmit admits the caller's lifetime against what it read.
func (s *Server) handleAppLifetimeAdmit(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleAppLifetimeAdmit"
	var req appLifetimeAdmitRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	lifetime := appLifetime(r)
	if lifetime == "" {
		s.writeError(w, http.StatusBadRequest, "app_lifetime_required",
			"an admission names the app lifetime it admits", op)
		return
	}
	if req.Generation == nil {
		s.writeError(w, http.StatusBadRequest, "missing_required_field", "generation is required", op)
		return
	}
	generation, err := s.admission.admit(lifetime, req.Instance, *req.Generation)
	switch {
	case stderr.Is(err, errLifetimeRetired):
		s.writeError(w, http.StatusConflict, "app_lifetime_retired",
			"another start of Station Manager has taken over; close this one", op)
	case stderr.Is(err, errAdmissionStale):
		s.writeError(w, http.StatusConflict, "app_admission_stale",
			"the admission state has changed since it was read; read it again", op)
	case stderr.Is(err, errSessionUnavailable):
		s.logger.ErrorWith().Err(err).Msg("app admission: the operating session's record could not be read or written; the app is not admitted")
		s.writeError(w, http.StatusServiceUnavailable, operatingSessionUnavailable,
			"the operating session could not be opened; Station Manager is not ready", op)
	default:
		s.writeJSON(w, http.StatusOK, appLifetimeState{
			Instance: processInstance, Generation: generation, Current: true})
	}
}

// ft8As is the FT8 command surface as this request's caller. Every FT8 command goes
// through it (TestSource_Ft8CommandsAreCalledAsTheRequestsCaller).
func (s *Server) ft8As(r *http.Request) ft8.Operating { return s.ft8.OperatingAs(appLifetime(r)) }

// writeLifetimeRefusal answers an FT8 command refused because its caller is not the
// current app lifetime, and reports whether err was that refusal. A caller that named no
// lifetime is a tool, or an app left running from before C4, and is told to restart
// (operator ruling 2026-11-17, question 10).
func (s *Server) writeLifetimeRefusal(w http.ResponseWriter, op errors.Op, err error) bool {
	switch {
	case stderr.Is(err, ft8.ErrNoAppLifetime):
		s.writeError(w, http.StatusConflict, "app_lifetime_not_current",
			"this Station Manager was started before an update and cannot operate FT8. Restart Station Manager.", op)
	case stderr.Is(err, ft8.ErrLifetimeNotCurrent):
		s.writeError(w, http.StatusConflict, "app_lifetime_not_current",
			"this is not the Station Manager that is operating the station", op)
	default:
		return false
	}
	return true
}
