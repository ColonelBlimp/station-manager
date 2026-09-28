package archive

import (
	"context"
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// Contents statuses: whether an archive's listed logbooks and counts can be
// trusted (ADR 0084). GET /v1/qso-archives serves them as contents_status.
const (
	// ContentsCurrent: the counts match the archive as it is now.
	ContentsCurrent = "current"
	// ContentsStale: the counts are the last known; the archive has changed since
	// (a write not yet recounted, or an inactive file that changed after close).
	ContentsStale = "stale"
	// ContentsUnknown: nothing is known — the archive has no summary yet.
	ContentsUnknown = "unknown"
)

// ActiveSummary keeps the ACTIVE archive's summary current off the QSO path
// (ADR 0084 slice 2b). Writers call Notify after they commit — a non-blocking,
// coalescing dirty signal, injected directly rather than through the event hub,
// whose subscribers can be evicted — and Run recounts. The summary reads stale
// from the first notification until a recount that no later notification
// overtook succeeds: a notification during a recount schedules another, because
// the running one may have read the counts before that write.
type ActiveSummary struct {
	id  string
	src SummarySource
	// Logger, when set, records a failed recount. Optional.
	Logger *logging.Service

	mu       sync.Mutex
	logbooks []LogbookSummary
	status   string
	dirty    chan struct{} // capacity 1: a pending recount, however many notifications
}

// NewActiveSummary starts from the summary built when the archive opened; a
// failed build (openErr) starts unknown, and Run recounts it straight away.
func NewActiveSummary(archiveID string, src SummarySource, opening []LogbookSummary, openErr error) *ActiveSummary {
	a := &ActiveSummary{id: archiveID, src: src, dirty: make(chan struct{}, 1), status: ContentsCurrent, logbooks: opening}
	if openErr != nil || opening == nil {
		a.status = ContentsUnknown
		a.logbooks = []LogbookSummary{}
		a.dirty <- struct{}{}
	}
	return a
}

// Notify marks the summary out of date and schedules a recount. It never blocks:
// the send is non-blocking, and a recount already pending covers this change.
// Held under the lock with the status change, so Run's "no notification since"
// check cannot miss it.
func (a *ActiveSummary) Notify() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.status == ContentsCurrent {
		a.status = ContentsStale
	}
	select {
	case a.dirty <- struct{}{}:
	default:
	}
}

// Run recounts on each notification until ctx ends.
func (a *ActiveSummary) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.dirty:
		}
		lbs, err := BuildLogbookSummaries(ctx, a.src)
		a.mu.Lock()
		if err == nil {
			a.logbooks = lbs
			if len(a.dirty) == 0 { // no notification overtook this recount
				a.status = ContentsCurrent
			} else {
				a.status = ContentsStale
			}
		}
		a.mu.Unlock()
		if err != nil && ctx.Err() == nil && a.Logger != nil {
			a.Logger.WarnWith().Err(err).Str("archive_id", a.id).Msg("archive: could not recount the active archive")
		}
	}
}

// Snapshot returns the archive's id, a copy of its logbooks (never nil) and the
// contents status.
func (a *ActiveSummary) Snapshot() (string, []LogbookSummary, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.id, append([]LogbookSummary{}, a.logbooks...), a.status
}
