package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

// RestartFunc triggers a graceful daemon restart. cmd/smd wires it to signal the
// shutdown path; the process then exits ExitRestart and systemd
// (RestartForceExitStatus) respawns it. nil when there is no service-manager
// respawn (split-host / tests).
type RestartFunc func()

// SetRestart injects the restart trigger (cmd/smd). When nil, POST /v1/restart
// answers 503 (nothing would bring the daemon back up).
func (s *Server) SetRestart(fn RestartFunc) { s.restart = fn }

// handleRestart drives POST /v1/restart — an ATTENDED, operator-triggered daemon
// restart for the "Requires a restart" config-apply flows (rig connection, active
// rig, mode mappings, serial overrides). It reuses the normal graceful shutdown,
// so the tune/FT8 carrier is released cleanly (TX-safe), then the process exits
// ExitRestart for systemd to respawn (~RestartSec later); SSE clients auto-
// reconnect.
//
// Refuses with 409 while a tune carrier or FT8 transmission is CURRENTLY keyed —
// the operator must stop transmitting first. A stuck/unconfirmed TX is NOT refused
// (bridge.TxActive excludes it), so a recovery restart stays possible.
//
// CSRF: like every mutating endpoint, this is covered by the API-wide
// requireSameOrigin middleware (csrf.go) — a cross-origin drive-by POST from a page
// in the operator's browser is rejected 403; the same-origin SPA and loopback (dev
// proxy) pass. Still unauthenticated: SM is a single-operator loopback service, so
// there's no per-request auth beyond the origin check.
func (s *Server) handleRestart(w http.ResponseWriter, _ *http.Request) {
	const op errors.Op = "api.handleRestart"
	if s.restart == nil {
		s.writeError(w, http.StatusServiceUnavailable, "restart_unavailable",
			"this daemon has no service-manager restart configured", op)
		return
	}
	if s.bridge.TxActive() {
		s.writeError(w, http.StatusConflict, "tx_active",
			"stop transmitting before restarting the daemon", op)
		return
	}
	// 202 first so the response flushes before the graceful shutdown (which
	// http.Server.Shutdown drains this now-complete request out of) begins. The
	// trigger is non-blocking (a guarded channel close), so the handler returns
	// immediately after.
	w.WriteHeader(http.StatusAccepted)
	s.restart()
}
