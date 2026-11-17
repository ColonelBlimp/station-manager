package api

import (
	"context"
	"encoding/base64"
	stderr "errors"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/ft8"
)

// App admission (W-0027 C4a; ADR 0102 ruling 6; ADR 0100 §3, App lifetimes).
//
// With the operating-client check on, the desktop app's main process makes one random
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
// Nothing here is stored, and no session is opened or closed: this is not ADR 0100's
// session attachment. A new daemon process has no current lifetime and no retired ones,
// and never takes one as current without an admission.
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

// appLifetime is the request's validated app lifetime, or "" when it carried none, or
// when the check is off and the header is ignored.
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
)

// appAdmission is the one current lifetime and the order of its replacements. mu is held
// across a replacement, the scopes' stop included, and is taken before any scope's own
// lock; no scope calls back into it.
type appAdmission struct {
	mu         sync.Mutex
	current    string
	generation uint64
	retired    map[string]struct{}
	scopes     []operatingScope

	// published is the generation as last published, for the response header. It is read
	// without mu, so a request is never held up behind a replacement that is stopping a
	// scope.
	published atomic.Uint64
}

func (a *appAdmission) read(lifetime string) (generation uint64, current bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.generation, lifetime != "" && lifetime == a.current
}

// admit applies the rule at the head of this file and returns the generation after it.
func (a *appAdmission) admit(lifetime, instance string, generation uint64) (uint64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if lifetime == a.current {
		return a.generation, nil
	}
	if _, gone := a.retired[lifetime]; gone {
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
	if a.current != "" {
		if a.retired == nil {
			a.retired = map[string]struct{}{}
		}
		a.retired[a.current] = struct{}{}
	}
	a.current = lifetime
	a.generation++
	a.published.Store(a.generation)
	for _, sc := range a.scopes {
		sc.PublishOperating(lifetime)
	}
	return a.generation, nil
}

type appLifetimeState struct {
	// Required is false with the check off: the app has nothing to be admitted to.
	Required   bool   `json:"required"`
	Instance   string `json:"daemon_instance"`
	Generation uint64 `json:"generation"`
	Current    bool   `json:"current"`
}

type appLifetimeAdmitRequest struct {
	Instance   string  `json:"daemon_instance"`
	Generation *uint64 `json:"generation"`
}

// registerAppAdmission declares the two admission routes. The desktop app's main process
// reads, then admits its lifetime, before it loads the workspace. With the check off both
// do nothing.
func (s *Server) registerAppAdmission(apiMux *routeMux) {
	apiMux.HandleFunc("GET /v1/app/lifetime", s.handleAppLifetimeRead)
	apiMux.HandleFunc("POST /v1/app/lifetime", s.handleAppLifetimeAdmit)
}

// handleAppLifetimeRead answers the read: this process's marker, the admission
// generation, and whether the caller is the current lifetime.
func (s *Server) handleAppLifetimeRead(w http.ResponseWriter, r *http.Request) {
	st := appLifetimeState{Required: s.requireDesktopApp, Instance: processInstance}
	if s.requireDesktopApp {
		st.Generation, st.Current = s.admission.read(appLifetime(r))
	}
	s.writeJSON(w, http.StatusOK, st)
}

// handleAppLifetimeAdmit admits the caller's lifetime against what it read.
func (s *Server) handleAppLifetimeAdmit(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleAppLifetimeAdmit"
	var req appLifetimeAdmitRequest
	if !s.readCommandJSON(w, r, op, &req) {
		return
	}
	if !s.requireDesktopApp {
		s.writeJSON(w, http.StatusOK, appLifetimeState{Instance: processInstance})
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
	default:
		s.writeJSON(w, http.StatusOK, appLifetimeState{
			Required: true, Instance: processInstance, Generation: generation, Current: true})
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
