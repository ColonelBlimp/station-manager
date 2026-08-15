package worker

import (
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// reachabilityLog logs a destination's reachability as TRANSITIONS only (L11): one Warn
// when the destination first becomes unreachable (with the cause), one Info when it is
// reachable again (with the outage duration), and NOTHING at the default level in between —
// so an outage that retries indefinitely (OutcomeUnreachable never gives up, ADR 0038)
// does not produce one Info per retry. The per-attempt records for those retries are
// demoted to Debug at their source (logAttempt), so this tracker owns all default-level
// outage signal.
//
// "Reachable again" is the first non-unreachable outcome, INCLUDING a terminal rejection:
// reaching the host to be rejected still proves the host is up. Concurrency-safe because a
// worker processes one row at a time today, but the guard is cheap and future-proofs the
// tracker against a batched processor.
type reachabilityLog struct {
	log  *logging.Service
	name string
	now  func() time.Time

	mu    sync.Mutex
	down  bool
	since time.Time
}

func newReachabilityLog(log *logging.Service, name string, now func() time.Time) *reachabilityLog {
	return &reachabilityLog{log: log, name: name, now: now}
}

// unreachable records an OutcomeUnreachable. Logs a Warn only on the down edge (up→down);
// subsequent unreachable results while already down emit nothing here.
func (r *reachabilityLog) unreachable(cause error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.down {
		return
	}
	r.down = true
	r.since = r.now()
	r.log.WarnWith().
		Str("forwarder", r.name).
		Err(cause).
		Msg("forwarding: destination unreachable")
}

// reachable records any non-unreachable outcome. Logs an Info only on the recovery edge
// (down→up), carrying how long the destination was unreachable; a no-op when already up.
func (r *reachabilityLog) reachable() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.down {
		return
	}
	r.down = false
	r.log.InfoWith().
		Str("forwarder", r.name).
		Int64("unreachable_seconds", int64(r.now().Sub(r.since)/time.Second)).
		Msg("forwarding: destination recovered")
}
