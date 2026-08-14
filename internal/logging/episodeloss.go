package logging

import (
	"sync"
	"time"
)

// EpisodeLoss logs a bounded record of a non-blocking producer's drop "episode" — a
// run of items dropped while a downstream queue is full. It warns when the episode
// total reaches 1, 10, 100, 1000, … (count-based exponential spacing, operator
// decision 2026-08-14), each warning carrying the running episode total and the
// queue depth/capacity; it emits one Info recovery summary (total lost + episode
// duration) when the caller signals the episode has ended, and resets the schedule
// for the next episode. This gives operators a distinguishable, bounded record of
// backpressure loss (L3) without one line per dropped item.
//
// It never blocks a producer beyond a brief mutex + an occasional (spaced) log; the
// caller decides WHEN to call Add (per drop, or per polled batch) and Recover
// (evidence: the next successful persist; audio: a quiet-window monitor). All
// methods are safe on a nil receiver and for concurrent use.
type EpisodeLoss struct {
	log     Logger
	warnMsg string
	recMsg  string
	reason  string
	now     func() time.Time

	mu     sync.Mutex
	active bool
	total  int64
	start  time.Time
	next   int64 // next episode-total threshold to warn at (1, 10, 100, …)
}

// NewEpisodeLoss builds a tracker. warnMsg/recMsg are the per-threshold and recovery
// log messages; reason is the structured "reason" field distinguishing the loss
// class (e.g. "evidence_queue_full", "audio_queue_full"). A nil logger becomes a
// no-op; now defaults to time.Now.
func NewEpisodeLoss(log Logger, warnMsg, recMsg, reason string, now func() time.Time) *EpisodeLoss {
	if log == nil {
		log = Noop()
	}
	if now == nil {
		now = time.Now
	}
	return &EpisodeLoss{log: log, warnMsg: warnMsg, recMsg: recMsg, reason: reason, now: now}
}

// Add records n newly dropped items (n >= 1) against the current episode — starting
// one if none is active — and warns if the running total crosses the next
// power-of-ten threshold. depth/capacity are the producer queue's current
// length/capacity. Logs at most once per call, only at the spaced thresholds.
func (e *EpisodeLoss) Add(n int64, depth, capacity int) {
	if e == nil || n <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.active {
		e.active = true
		e.start = e.now()
		e.total = 0
		e.next = 1
	}
	e.total += n
	if e.total >= e.next {
		e.log.WarnWith().
			Str("reason", e.reason).
			Int64("dropped", e.total).
			Int("queue_depth", depth).
			Int("queue_capacity", capacity).
			Msg(e.warnMsg)
		for e.next <= e.total { // advance past every threshold this call crossed
			e.next *= 10
		}
	}
}

// Recover ends the current episode (if any) with one Info summary carrying the total
// dropped and the episode's duration, then resets. No-op when no episode is active,
// so it is safe to call unconditionally on the recovery path.
func (e *EpisodeLoss) Recover() {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.active {
		return
	}
	e.log.InfoWith().
		Str("reason", e.reason).
		Int64("total_dropped", e.total).
		Int64("episode_seconds", int64(e.now().Sub(e.start).Seconds())).
		Msg(e.recMsg)
	e.active = false
}

// Active reports whether an episode is currently in progress.
func (e *EpisodeLoss) Active() bool {
	if e == nil {
		return false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.active
}
