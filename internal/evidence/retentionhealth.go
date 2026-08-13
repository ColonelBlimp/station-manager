package evidence

import (
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// retentionMeasurementHeartbeat mirrors L1's cadence: while retention measurement
// stays degraded, re-warn at most once per interval, driven by write attempts
// (not a wall clock) — quiet while capture is inactive. Operator decision
// (2026-08-12): 5 minutes.
const retentionMeasurementHeartbeat = 5 * time.Minute

// retentionHealth tracks whether the evidence-retention MEASUREMENT layer (the
// freelist / metadata / physical-usage probes and compaction) is currently
// failing, so a swallowed SQL/filesystem error becomes an observable degraded
// transition instead of being misread as capacity pressure (L2,
// internal-codebase-logging-gaps.md). The confusable state it breaks: "archive
// genuinely full" vs "archive could not be measured".
//
// Cadence (operator decision 2026-08-12): edge-triggered + a 5-minute
// write-driven heartbeat. One Warn on entering degraded; while still degraded a
// re-warn only once ≥5 min have elapsed, carrying outage duration + the affected
// measurement + dropped-slot counts; the stored last error is refreshed between
// heartbeats; one Info on recovery after the first complete successful
// measurement. Never one line per failed retry.
type retentionHealth struct {
	log    logging.Logger
	dbPath string
	beat   time.Duration
	now    func() time.Time

	mu           sync.Mutex
	degraded     bool
	outageStart  time.Time
	lastBeatAt   time.Time
	op           string // affected measurement, e.g. "physical_usage"
	errMsg       string
	droppedSince int64 // dropped since the last emitted notice
	droppedTotal int64 // dropped over the whole incident
}

func newRetentionHealth(log logging.Logger, dbPath string, beat time.Duration, now func() time.Time) *retentionHealth {
	if now == nil {
		now = time.Now
	}
	return &retentionHealth{log: log, dbPath: dbPath, beat: beat, now: now}
}

// dropped records that a slot was dropped for a measurement failure. Call BEFORE
// fail so the triggering slot is counted in the notice that fail may emit.
func (h *retentionHealth) dropped() {
	h.mu.Lock()
	h.droppedSince++
	h.droppedTotal++
	h.mu.Unlock()
}

// fail records a measurement failure and emits the bounded transition/heartbeat.
// op names the measurement (e.g. "freelist_count"); err is its cause. The stored
// last error is always refreshed so a heartbeat reports the most recent cause.
func (h *retentionHealth) fail(op string, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	h.op = op
	if err != nil {
		h.errMsg = err.Error()
	}
	if !h.degraded {
		h.degraded = true
		h.outageStart = now
		h.lastBeatAt = now
		// The edge IS a notice, so "dropped since last notice" restarts here: the
		// triggering drop is already in droppedTotal (the incident total) and must
		// NOT also land in the first heartbeat's interval count (codex 468a9ad1 P2).
		h.droppedSince = 0
		h.log.WarnWith().
			Str("operation", op).
			Str("db_path", h.dbPath).
			Str("error", h.errMsg).
			Msg("evidence: retention measurement degraded (fail-closed)")
		return
	}
	if h.beat > 0 && now.Sub(h.lastBeatAt) >= h.beat {
		h.log.WarnWith().
			Str("operation", op).
			Str("db_path", h.dbPath).
			Str("error", h.errMsg).
			Int64("outage_seconds", int64(now.Sub(h.outageStart).Seconds())).
			Int64("dropped_since_notice", h.droppedSince).
			Int64("dropped_total", h.droppedTotal).
			Msg("evidence: retention measurement still degraded")
		h.lastBeatAt = now
		h.droppedSince = 0
	}
}

// ok records a complete successful measurement cycle. It announces recovery once
// (with the incident's outage duration + total dropped) and clears the incident.
func (h *retentionHealth) ok() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.degraded {
		return
	}
	now := h.now()
	h.log.InfoWith().
		Str("operation", h.op).
		Int64("outage_seconds", int64(now.Sub(h.outageStart).Seconds())).
		Int64("dropped_total", h.droppedTotal).
		Msg("evidence: retention measurement recovered")
	h.degraded = false
	h.droppedSince = 0
	h.droppedTotal = 0
	h.op = ""
	h.errMsg = ""
}

func (h *retentionHealth) isDegraded() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.degraded
}
