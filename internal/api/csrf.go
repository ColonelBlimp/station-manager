package api

import (
	"net"
	"net/http"
	"net/url"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// requireSameOrigin is a lightweight CSRF guard for STATE-CHANGING requests
// (POST/PUT/PATCH/DELETE) — it protects the whole mutating API surface (/v1/qso,
// /v1/config, /v1/rig/tune, /v1/restart, …) in one place (codex 088bdb84 P1).
//
// It validates the destination against a FIXED allowlist (loopback names + the
// configured tcp bind host), NOT equality with the request's own — attacker-
// controllable — Host. That closes DNS rebinding (codex 85997b79 P1): a page from
// evil.example that rebinds its name to our address arrives with both Host and
// Origin = evil.example, which an Origin==Host check would wave through but the
// allowlist rejects. For unsafe methods:
//   - the request Host must be an allowed host (else 403) — the rebinding defense;
//   - a present Origin must also be an allowed host (else 403) — catches a plain
//     cross-origin POST (real Host, attacker Origin);
//   - absent Origin (curl, the CLI, server-to-server) is allowed — not a browser
//     CSRF vector; browsers always send Origin on a cross-origin POST.
//
// Loopback (localhost / 127.0.0.1 / ::1, any port) is always allowed, so the
// same-origin SPA and the Vite dev proxy (:5173 → :8080) pass. Safe methods
// (GET/HEAD/OPTIONS) are untouched (no state change; SSE GETs must stay open).
func (s *Server) requireSameOrigin(next http.Handler) http.Handler {
	const op errors.Op = "api.requireSameOrigin"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if !s.hostAllowed(hostOnly(r.Host)) {
			s.writeError(w, http.StatusForbidden, "cross_origin", "host not allowed", op)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			u, err := url.Parse(origin)
			if err != nil || u.Host == "" || !s.hostAllowed(u.Hostname()) {
				s.writeError(w, http.StatusForbidden, "cross_origin",
					"cross-origin request rejected", op)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether h is a host the daemon is legitimately served under:
// a loopback name, or the configured tcp bind host. The allowlist is fixed by our
// own config, never by the request — that is what makes it rebinding-proof.
func (s *Server) hostAllowed(h string) bool {
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	if s.protocol == "tcp" {
		if bindHost, _, err := net.SplitHostPort(s.cfg.Snapshot().SocketPath); err == nil && bindHost == h {
			return true
		}
	}
	return false
}

// hostOnly strips the port from a Host header ("127.0.0.1:8080" → "127.0.0.1"; a
// bare host with no port is returned unchanged).
func hostOnly(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}
