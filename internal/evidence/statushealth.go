package evidence

import (
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// statusQueryHealth reports transitions for database-derived status groups.
// Status is an operator-polled honesty surface; logging every failed poll would
// turn a dashboard refresh into a warning flood, while silence would hide why
// fields are null. One degraded edge and one recovery make both states clear.
type statusQueryHealth struct {
	mu       sync.Mutex
	log      logging.Logger
	degraded bool
}

// statusQueryFaultForTest injects a single status-read failure without making
// the writer path depend on a test-only SQL wrapper. Nil in production.
var statusQueryFaultForTest func(group string) error

func statusQueryFault(group string) error {
	if statusQueryFaultForTest == nil {
		return nil
	}
	return statusQueryFaultForTest(group)
}

func newStatusQueryHealth(log logging.Logger) *statusQueryHealth {
	if log == nil {
		log = logging.Noop()
	}
	return &statusQueryHealth{log: log}
}

func (h *statusQueryHealth) observe(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		if !h.degraded {
			h.degraded = true
			h.log.WarnWith().Err(err).
				Msg("evidence: status database reads degraded; database-derived fields are unknown")
		}
		return
	}
	if h.degraded {
		h.degraded = false
		h.log.InfoWith().Msg("evidence: status database reads recovered")
	}
}
