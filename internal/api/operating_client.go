package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// The operating-client check (ADR 0102 ruling 1 and its 2026-11-13 update; W-0027 C2a).
//
// A request is served only if it arrived on the desktop app's listener and carries exactly one operating-credential header whose value is this
// install's credential. Everything else is refused, on every route, page and stream alike,
// because the check stands in front of the router. There is one exception, GET /v1/healthz,
// and no view-only class. The main listener serves nothing else, a valid credential included
// (operator rulings 2026-11-13; the Unix-socket form is refused as TCP is). A page asked of
// the main listener is refused with a short fixed notice instead of the JSON refusal (C2c).
//
// What it keeps out is other users and other machines. A process of the operator's own user
// that reads the credential file is inside the accepted limit, and which connection counts as
// the operator's presence is not decided here (C4).
//
// There is no setting that turns the check off (W-0027 C2e; config version 7 removed the
// temporary switch, server.require_desktop_app).

// operatingCredentialHeader carries the credential. The desktop shell's main process adds it;
// page script never sees it (ADR 0103 point 7).
const operatingCredentialHeader = "X-Sm-Operating-Credential"

// healthPath is the one route open without the credential (operator ruling 2026-11-13). It
// reports three words and no station data.
const healthPath = "/v1/healthz"

// SetOperatingCredential installs the test for a presented credential. The server never
// holds the value itself. Until it is set, the server refuses every request but the health
// check.
func (s *Server) SetOperatingCredential(matches func(presented string) bool) {
	s.operatingCredential.Store(&matches)
}

func (s *Server) requireOperatingClient(next http.Handler) http.Handler {
	const op errors.Op = "api.requireOperatingClient"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Whatever was presented stops here, served or refused: no handler, proxy or
		// diagnostic downstream is ever handed the credential.
		presented := r.Header.Values(operatingCredentialHeader)
		r.Header.Del(operatingCredentialHeader)
		// The app lifetime (app_admission.go) is read here too and goes no further as a
		// header: a handler is given the validated value, or nothing.
		lifetimes := r.Header.Values(appLifetimeHeader)
		r.Header.Del(appLifetimeHeader)

		if s.operatingClientAdmitted(r, presented) {
			// Served for its credential, not as the open health check: it is the app, or a
			// tool of the operator's. It gets this process's marker and admission
			// generation, and its lifetime if it named one.
			if isAppRequest(r) && len(presented) == 1 {
				var ok bool
				if r, ok = s.withAppLifetime(w, r, lifetimes); !ok {
					return
				}
				w.Header().Set(daemonInstanceHeader, processInstance)
				w.Header().Set(admissionGenerationHeader, strconv.FormatUint(s.admission.published.Load(), 10))
			}
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
		if listener == "main" && isPageRequest(r) {
			writeOperatingClientNotice(w, r)
			return
		}
		s.writeError(w, http.StatusForbidden, "desktop_app_required",
			"Station Manager is operated from its desktop app", op)
	})
}

// isPageRequest reports whether a request asks for a page rather than the API: a GET or HEAD
// outside the /v1 namespace, by the router's own division (isAPIPath).
func isPageRequest(r *http.Request) bool {
	return (r.Method == http.MethodGet || r.Method == http.MethodHead) && !isAPIPath(r.URL.Path)
}

// writeOperatingClientNotice refuses a page with the notice. It is never cached.
func writeOperatingClientNotice(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	if r.Method != http.MethodHead {
		_, _ = io.WriteString(w, operatingClientNotice)
	}
}

// operatingClientAdmitted is the rule: the health check, or the app
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

// operatingClientNotice is the whole of what smd does for a browser (W-0027
// C2c; operator ruling 2026-11-13: "a notice can be good"): one short, fixed page for a page
// asked of the main listener. It carries no data and no script, loads nothing and links
// nowhere, and has no route of its own. It is sent as a refusal.
const operatingClientNotice = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<title>Station Manager</title>
<h1>Station Manager is operated from its desktop app</h1>
<p>This address no longer serves Station Manager. Open the Station Manager desktop app instead.</p>
</html>
`
