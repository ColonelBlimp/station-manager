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
//   - a present Origin must be loopback (any port) or — for tcp — an EXACT same
//     host:PORT (else 403) — catches a plain cross-origin POST, incl. a different-
//     port page on the same hostname (codex e77a573f P2);
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
			s.logHostRefused(r)
			s.writeError(w, http.StatusForbidden, "cross_origin", "host not allowed", op)
			return
		}
		// The Origin is compared against a TRUSTED host. For tcp, hostAllowed already
		// validated r.Host to be loopback or the exact bind host, so r.Host is a safe
		// basis. For a non-tcp listener r.Host is arbitrary — a reverse proxy fronting
		// the socket could forward a rebound Host — so no non-loopback host is trusted
		// and only a loopback Origin passes (codex d068ff9c P1).
		trusted := ""
		if s.protocol == "tcp" {
			trusted = r.Host
		}
		if origin := r.Header.Get("Origin"); origin != "" && !originAllowed(origin, trusted) {
			s.logOriginRefused(r, origin)
			s.writeError(w, http.StatusForbidden, "cross_origin",
				"cross-origin request rejected", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether h (the request Host, port stripped) is a host the
// daemon is legitimately served under.
//
// A non-tcp listener (Unix socket) has no network host, can't be reached by a
// browser, and so has no DNS-rebinding vector — its (arbitrary) Host header isn't a
// trust signal, so it's accepted (codex 6525f509 P2). The allowlist only bites for
// tcp: a loopback name or the configured bind host passes; a WILDCARD tcp bind
// (0.0.0.0 / :: / empty, tcpBindHost == "") admits LOOPBACK ONLY — fail-closed and
// rebinding-proof (Option A, codex 5664434c P1). A LAN deployment must bind a
// SPECIFIC IP (itself rebinding-proof, since a rebound attacker name won't equal
// it), not 0.0.0.0, to accept browser writes from the network.
func (s *Server) hostAllowed(h string) bool {
	if s.protocol != "tcp" {
		return true
	}
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
// and the dev proxy) or an EXACT match to trustedHost (host:port preserved, so a
// different-port page on the same hostname is rejected — codex e77a573f P2).
// trustedHost is the (validated) request Host for a tcp listener, or "" when no
// non-loopback host is trusted — a non-tcp listener, where r.Host is arbitrary, so
// only a loopback Origin passes (codex d068ff9c P1).
func originAllowed(origin, trustedHost string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if isLoopbackName(u.Hostname()) {
		return true
	}
	return trustedHost != "" && u.Host == trustedHost
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

// ---- refusal logging (api-logging-gaps A3) ----------------------------------
//
// A refusal here is the API's only security control firing, and the static 403
// discards its entire diagnostic content — WHICH destination was refused. That
// value separates states demanding opposite actions: a rebinding attempt
// (investigate) vs a LAN deployment refusing legitimate traffic under a
// wildcard bind (fix the bind), a foreign page vs a stale bookmark on the
// wrong port. A dedicated Warn line rather than access-log fields: a security
// refusal at Info interleaved with routine traffic reads as routine. Volume is
// bounded by the access log's own — at most one line per refused request.
//
// Only PARSED fields are logged, never a raw header (the A3 amendment): Origin
// and Host are client-controlled and can carry user:pass@host — the
// credential-into-a-0644-file shape of the 2026-07-25 P1s. url.URL keeps
// userinfo in u.User, never in u.Host, so logging u.Host sheds it; a value
// that doesn't parse to a host logs the fact of unparseability only.

// maxLoggedHost bounds a logged destination: RFC 1035 §2.3.4 caps a DNS name
// at 253 octets, plus ":65535" — anything longer is not a real destination and
// is reported unparseable rather than copied into the log.
const maxLoggedHost = 260

// sanitizedHostForLog reduces a client-controlled host[:port] (a Host header,
// or a parsed Origin's u.Host) to a loggable value, per the rules above.
func sanitizedHostForLog(raw string) (string, bool) {
	u, err := url.Parse("//" + raw)
	if err != nil || u.Host == "" || len(u.Host) > maxLoggedHost {
		return "", false
	}
	return u.Host, true
}

func (s *Server) logHostRefused(r *http.Request) {
	ev := s.logger.WarnWith().
		Str("remote", r.RemoteAddr).Str("method", r.Method).Str("path", r.URL.Path)
	if h, ok := sanitizedHostForLog(r.Host); ok {
		ev = ev.Str("host", h)
	} else {
		ev = ev.Bool("host_unparseable", true)
	}
	ev.Msg("cross-origin refused: request host not allowed")
}

func (s *Server) logOriginRefused(r *http.Request, origin string) {
	ev := s.logger.WarnWith().
		Str("remote", r.RemoteAddr).Str("method", r.Method).Str("path", r.URL.Path)
	// The request Host passed hostAllowed to reach this refusal — it is what
	// the Origin failed to match, so it rides along for the diagnosis.
	if h, ok := sanitizedHostForLog(r.Host); ok {
		ev = ev.Str("host", h)
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && len(u.Host) <= maxLoggedHost {
		ev = ev.Str("origin_scheme", u.Scheme).Str("origin_host", u.Host)
	} else {
		ev = ev.Bool("origin_unparseable", true)
	}
	ev.Msg("cross-origin refused: origin not allowed")
}
