package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// The operating-client check (ADR 0102 ruling 1 and its 2026-11-13 update; W-0027 C2a).
//
// With server.require_desktop_app on, a request is served only if it arrived on the desktop
// app's listener and carries exactly one operating-credential header whose value is this
// install's credential. Everything else is refused, on every route, page and stream alike,
// because the check stands in front of the router. There is one exception, GET /v1/healthz,
// and no view-only class. The main listener serves nothing else, a valid credential included
// (operator rulings 2026-11-13; the Unix-socket form is refused as TCP is).
//
// What it keeps out is other users and other machines. A process of the operator's own user
// that reads the credential file is inside the accepted limit, and which connection counts as
// the operator's presence is not decided here (C4).
//
// With the switch off (the default, and temporary) nothing changes: every request passes.

// operatingCredentialHeader carries the credential. The desktop shell's main process adds it;
// page script never sees it (ADR 0103 point 7).
const operatingCredentialHeader = "X-Sm-Operating-Credential"

// healthPath is the one route open without the credential (operator ruling 2026-11-13). It
// reports three words and no station data.
const healthPath = "/v1/healthz"

// SetOperatingCredential installs the test for a presented credential. The server never
// holds the value itself. Until it is set, a server that requires the desktop app refuses
// every request but the health check.
func (s *Server) SetOperatingCredential(matches func(presented string) bool) {
	s.operatingCredential.Store(&matches)
}

// RequiresDesktopApp reports whether this server was built with the check on.
func (s *Server) RequiresDesktopApp() bool { return s.requireDesktopApp }

func (s *Server) requireOperatingClient(next http.Handler) http.Handler {
	const op errors.Op = "api.requireOperatingClient"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Whatever was presented stops here, in either state: no handler, proxy or
		// diagnostic downstream is ever handed the credential.
		presented := r.Header.Values(operatingCredentialHeader)
		r.Header.Del(operatingCredentialHeader)

		if !s.requireDesktopApp || s.operatingClientAdmitted(r, presented) {
			next.ServeHTTP(w, r)
			return
		}
		listener := "main"
		if isAppRequest(r) {
			listener = "app"
		}
		// The method and path only; never a header value, presented or expected.
		s.logger.WarnWith().
			Str("listener", listener).Str("method", r.Method).Str("path", r.URL.Path).
			Bool("credential_present", len(presented) > 0).
			Msg("operating-client check: request refused")
		s.writeError(w, http.StatusForbidden, "desktop_app_required",
			"Station Manager is operated from its desktop app", op)
	})
}

// operatingClientAdmitted is the rule with the check on: the health check, or the app
// listener with exactly one header carrying the credential. Which listener a request arrived
// on is the server's own mark on the connection, never anything the client sent.
func (s *Server) operatingClientAdmitted(r *http.Request, presented []string) bool {
	if r.Method == http.MethodGet && r.URL.Path == healthPath {
		return true
	}
	if !isAppRequest(r) || len(presented) != 1 {
		return false
	}
	matches := s.operatingCredential.Load()
	return matches != nil && *matches != nil && (*matches)(presented[0])
}
