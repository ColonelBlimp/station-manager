package api

import (
	"net/http"

	"github.com/ColonelBlimp/station-manager/internal/errors"
)

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
		s.writeError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable", op)
		return
	}

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
