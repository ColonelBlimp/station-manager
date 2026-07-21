package api

import (
	"net/http"
	"net/url"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// requireSameOrigin is a lightweight CSRF guard for STATE-CHANGING requests
// (POST/PUT/PATCH/DELETE) — it protects the whole mutating API surface (/v1/qso,
// /v1/config, /v1/rig/tune, /v1/restart, …) in one place (codex 088bdb84 P1).
//
// A browser attaches an Origin header to every cross-origin request (and to
// same-origin POSTs): a same-origin request's Origin matches the request's own
// Host, a cross-site drive-by page's (evil.com) does not. So for unsafe methods we
// reject (403) an Origin that is present AND neither same-host nor loopback:
//   - absent Origin → allowed (curl, the CLI, server-to-server on a LAN — not
//     browser CSRF vectors; browsers always send Origin on a cross-origin POST);
//   - loopback Origin (localhost / 127.0.0.1 / ::1, any port) → allowed, so the
//     Vite dev proxy (:5173 → :8080) works while a real remote page is still
//     blocked;
//   - safe methods (GET/HEAD/OPTIONS) → always allowed (no state change; SSE GETs
//     must stay open, and a cross-origin GET can't read the response anyway).
func (s *Server) requireSameOrigin(next http.Handler) http.Handler {
	const op errors.Op = "api.requireSameOrigin"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host) {
			s.writeError(w, http.StatusForbidden, "cross_origin",
				"cross-origin request rejected", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// originAllowed reports whether an Origin header belongs to the request's own host
// or a loopback host. Same-host is compared with the port (the SPA is served on the
// same host:port as the API); loopback is host-only so any local dev port passes.
func originAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false // malformed / opaque Origin → treat as cross-origin
	}
	if u.Host == host {
		return true
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}
