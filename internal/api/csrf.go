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
// It validates the destination against a FIXED allowlist decided by our own config,
// NOT equality with the request's own (attacker-controllable) Host — which closes
// DNS rebinding (codex 85997b79 P1). For unsafe methods:
//   - the request Host must be an allowed host (else 403) — the rebinding defense;
//   - a present Origin must be loopback (any port) or an EXACT same host:PORT (else
//     403) — catches a plain cross-origin POST, incl. a different-port page on the
//     same hostname (codex e77a573f P2);
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
		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, r.Host) {
			s.writeError(w, http.StatusForbidden, "cross_origin",
				"cross-origin request rejected", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether h (the request Host, port stripped) is a host the
// daemon is legitimately served under: a loopback name, or the configured tcp bind
// host. A WILDCARD bind (0.0.0.0 / :: / empty) or a non-tcp listener has no
// derivable external host, so it admits LOOPBACK ONLY — fail-closed and
// rebinding-proof (Option A, codex 5664434c P1). A LAN deployment must bind a
// SPECIFIC IP (itself rebinding-proof, since a rebound attacker name won't equal
// it), not 0.0.0.0, to accept browser writes from the network.
func (s *Server) hostAllowed(h string) bool {
	if isLoopbackName(h) {
		return true
	}
	bind := s.tcpBindHost()
	return bind != "" && h == bind
}

// tcpBindHost returns the configured tcp bind host, or "" for a wildcard bind
// (0.0.0.0 / :: / empty host) or a non-tcp listener.
func (s *Server) tcpBindHost() string {
	if s.protocol != "tcp" {
		return ""
	}
	h, _, err := net.SplitHostPort(s.cfg.Snapshot().SocketPath)
	if err != nil {
		return ""
	}
	switch h {
	case "", "0.0.0.0", "::":
		return ""
	}
	return h
}

// originAllowed permits a loopback Origin (any port — the same-origin loopback SPA
// and the dev proxy) or an EXACT same host:port match (port preserved, so a
// different-port page on the same hostname is rejected — codex e77a573f P2).
func originAllowed(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if isLoopbackName(u.Hostname()) {
		return true
	}
	return u.Host == host
}

func isLoopbackName(h string) bool {
	switch h {
	case "localhost", "127.0.0.1", "::1":
		return true
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
