package logging

import (
	"encoding/json"
	"io"
	"os"
	"sync"
	"time"
)

// defaultDegradedHeartbeat is how often, while the durable log writer stays
// failing, the non-recursive fallback re-warns. The heartbeat is WRITE-DRIVEN:
// it re-emits on the next record after the interval elapses, not on a wall clock
// — while the daemon is idle no records are being lost, so a missed heartbeat
// means nothing is at risk. Operator decision (2026-08-12): 5 minutes.
const defaultDegradedHeartbeat = 5 * time.Minute

// Fallback event names carried on the non-recursive stderr/journald line.
const (
	fallbackDegraded      = "degraded"       // healthy -> failing (edge)
	fallbackStillDegraded = "still_degraded" // heartbeat while failing
	fallbackRecovered     = "recovered"      // failing -> healthy (edge)
)

// healthTarget is one log sink plus whether a write failure there means durable
// records are being lost. The rolling file is durable; the console is
// best-effort (a closed stderr must not mark the daemon's logging degraded).
type healthTarget struct {
	name    string
	w       io.Writer
	durable bool
}

// healthWriter fans each log record out to every target and, UNLIKE
// io.MultiWriter, does not stop at the first target that errors — a failing file
// must not prevent delivery to the console, and vice versa (L1 isolation). It
// owns the degraded-state signalling for the durable target(s):
//
//   - it ALWAYS returns success to zerolog, so a failing durable writer never
//     triggers zerolog's generic per-write stderr line and can never recurse
//     into the very logger that is failing;
//   - on the healthy->failing and failing->healthy transitions it writes ONE
//     line to a non-recursive fallback sink (os.Stderr, which systemd routes to
//     journald), carrying the failure count, cause and a timestamp;
//   - while still failing it re-warns at most once per `heartbeat` (write-driven).
//
// The degraded state and counters are exposed for /v1/healthz surfacing (AC5/AC6).
type healthWriter struct {
	targets   []healthTarget
	fallback  io.Writer
	heartbeat time.Duration
	now       func() time.Time

	mu             sync.Mutex
	isDegraded     bool
	failureCount   int64
	lastErrMsg     string
	lastFallbackAt time.Time
}

func newHealthWriter(targets []healthTarget, fallback io.Writer, heartbeat time.Duration, now func() time.Time) *healthWriter {
	if fallback == nil {
		fallback = os.Stderr
	}
	if now == nil {
		now = time.Now
	}
	return &healthWriter{targets: targets, fallback: fallback, heartbeat: heartbeat, now: now}
}

// Write delivers p to every target regardless of any single target's error, then
// updates durable-writer health. It never reports an error to the caller
// (zerolog): the record has been offered to all sinks and the failure is
// accounted for out-of-band, so returning an error would only produce recursive
// zerolog stderr spam.
func (h *healthWriter) Write(p []byte) (int, error) {
	var durableFailed bool
	var durableErr error
	for _, t := range h.targets {
		if _, err := t.w.Write(p); err != nil && t.durable {
			durableFailed = true
			if durableErr == nil {
				durableErr = err
			}
		}
	}
	h.record(durableFailed, durableErr)
	return len(p), nil
}

// record advances the durable-writer health state and emits the bounded fallback
// line on the transitions and heartbeat. Held under mu so the healthy<->failing
// edges each fire exactly once even under concurrent Writes.
func (h *healthWriter) record(failed bool, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if failed {
		h.failureCount++
		if err != nil {
			h.lastErrMsg = err.Error()
		}
		switch {
		case !h.isDegraded:
			h.isDegraded = true
			h.lastFallbackAt = now
			h.emitFallback(fallbackDegraded)
		case h.heartbeat > 0 && now.Sub(h.lastFallbackAt) >= h.heartbeat:
			h.lastFallbackAt = now
			h.emitFallback(fallbackStillDegraded)
		}
		return
	}
	// The durable target(s) accepted this record. Announce recovery once.
	if h.isDegraded {
		h.isDegraded = false
		h.emitFallback(fallbackRecovered)
	}
}

// emitFallback writes one JSON line to the non-recursive sink. Caller holds mu.
// A marshal or write error here is unrecoverable (the whole point is that the
// normal log path is broken), so it is deliberately dropped.
func (h *healthWriter) emitFallback(event string) {
	rec := map[string]any{
		"component": "logging",
		"event":     event,
		"failures":  h.failureCount,
		"time":      h.now().UTC().Format(time.RFC3339),
	}
	if event != fallbackRecovered {
		rec["error"] = h.lastErrMsg
	}
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = h.fallback.Write(append(b, '\n'))
}

func (h *healthWriter) degraded() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.isDegraded
}

func (h *healthWriter) failures() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.failureCount
}

func (h *healthWriter) lastError() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastErrMsg
}
