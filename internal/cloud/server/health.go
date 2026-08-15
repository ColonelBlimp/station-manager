package server

import (
	"log/slog"
	"sync"
	"time"
)

// dbHealthLog logs DB-ping health as TRANSITIONS only (L10): one Warn when it starts
// failing (with the cause), one Info when it recovers (with the elapsed unhealthy
// duration), and repeated failures in between at Debug — so a frequently-polled health
// probe does not flood the log at the default level. Concurrency-safe: probes overlap.
type dbHealthLog struct {
	log *slog.Logger
	now func() time.Time

	mu      sync.Mutex
	failing bool
	since   time.Time
}

func newDBHealthLog(log *slog.Logger) *dbHealthLog {
	return &dbHealthLog{log: log, now: time.Now}
}

func (h *dbHealthLog) fail(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.failing {
		// Still failing — repeated at Debug so a frequent probe doesn't flood.
		h.log.Debug("health: db ping still failing", "err", err)
		return
	}
	h.failing = true
	h.since = h.now()
	h.log.Warn("health: db ping failing", "err", err)
}

func (h *dbHealthLog) ok() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.failing {
		return
	}
	h.failing = false
	h.log.Info("health: db ping recovered", "unhealthy_ms", h.now().Sub(h.since).Milliseconds())
}
