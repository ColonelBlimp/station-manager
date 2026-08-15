package api

import (
	"net/http"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// dbHealthLog logs DB-ping health as TRANSITIONS only (L10): one Warn when it starts
// failing (carrying the cause the endpoint would otherwise discard), one Info when it
// recovers (with the elapsed unhealthy duration), and repeated failures in between at
// Debug — so a frequently-polled health probe does not flood the log at the default
// level. Concurrency-safe: probes can overlap.
type dbHealthLog struct {
	logger *logging.Service
	now    func() time.Time

	mu      sync.Mutex
	failing bool
	since   time.Time
}

func newDBHealthLog(logger *logging.Service) *dbHealthLog {
	return &dbHealthLog{logger: logger, now: time.Now}
}

func (h *dbHealthLog) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failing {
		// Still failing — repeated at Debug so a frequent probe doesn't flood.
		h.logger.DebugWith().Err(err).Msg("health: database ping is still failing")
		return
	}
	h.failing = true
	h.since = h.now()
	h.logger.WarnWith().Err(err).Msg("health: database ping is failing")
}

func (h *dbHealthLog) ok() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.failing {
		return
	}
	h.failing = false
	h.logger.InfoWith().
		Int64("unhealthy_ms", h.now().Sub(h.since).Milliseconds()).
		Msg("health: database ping recovered")
}

// logHealthReporter is the slice of the logging service /v1/healthz needs: is the
// durable log writer currently failing? The real implementer is
// *logging.Service; a test injects a stub to drive the degraded branch.
type logHealthReporter interface {
	Degraded() bool
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	const op errors.Op = "api.handleHealthz"

	// 503 is reserved for a dependency that prevents correct request handling —
	// the database. A degraded log writer is an operational degradation, not a
	// loss of serving capacity, so it reports 200 with a degraded indicator
	// (operator decision 2026-08-12); the detailed cause + counters live in
	// journald via the logging fallback, not this body.
	if err := s.db.Ping(); err != nil {
		s.dbHealth.fail(err) // L10: log the cause on the unhealthy transition (was discarded)
		s.writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable", op)
		return
	}
	s.dbHealth.ok() // L10: one recovery Info with duration on the healthy transition

	logging := "ok"
	status := "ok"
	if s.logHealth != nil && s.logHealth.Degraded() {
		logging = "degraded"
		status = "degraded"
	}
	s.writeJSON(w, http.StatusOK, map[string]string{
		"status":   status,
		"database": "ok",
		"logging":  logging,
	})
}
