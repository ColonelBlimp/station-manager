package worker

import (
	"time"

	"github.com/ColonelBlimp/station-manager/internal/database/sqlite"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// queueSummaryLog decides when a periodic queue-depth summary is worth logging (L11). The
// summary ticker fires on a fixed interval, but emitting one line every interval forever would
// itself be noise on an idle queue — so the tracker suppresses steady idle and speaks only
// when there is something to say:
//
//   - while backed up (pending > 0): every interval, so a growing/aging backlog is visible;
//   - when the durable failed total changes: a row gave up (or a failure was cleared);
//   - ONCE on the transition to empty: the "drained" summary that closes out a backlog;
//   - otherwise (steady idle — empty and failed unchanged): nothing.
//
// State is carried across ticks. Only the Run summary ticker touches it (one goroutine), so no
// mutex is needed; the fields are unexported and never shared.
type queueSummaryLog struct {
	log  *logging.Service
	name string
	now  func() time.Time

	started        bool
	lastPendingPos bool
	lastFailed     int64
}

// emit logs a summary for d if the decision rules say it is worth one, and records the state
// needed to make the next decision.
func (q *queueSummaryLog) emit(d sqlite.UploadQueueDepth) {
	backedUp := d.Pending > 0
	failedChanged := q.started && d.Failed != q.lastFailed
	drainedToEmpty := q.lastPendingPos && !backedUp
	firstWithContent := !q.started && (backedUp || d.Failed > 0)

	should := backedUp || failedChanged || drainedToEmpty || firstWithContent

	q.started = true
	q.lastPendingPos = backedUp
	q.lastFailed = d.Failed
	if !should {
		return
	}

	ev := q.log.InfoWith().
		Str("forwarder", q.name).
		Int64("pending", d.Pending).
		Int64("failed", d.Failed)
	if backedUp && !d.OldestQueued.IsZero() {
		// Only meaningful when something is pending; on the drained summary there is no
		// oldest row and the field is omitted rather than reported as a bogus age. The
		// IsZero guard is defence-in-depth: the store computes pending and oldest from one
		// atomic snapshot, so backed-up implies a non-zero oldest — but if that ever
		// regressed, an age computed from the zero time would log a ~60-billion-second value.
		ev = ev.Int64("oldest_age_seconds", queueAgeSeconds(q.now(), d.OldestQueued))
	}
	ev.Msg("forwarding: queue summary")
}
