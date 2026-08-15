package evidence

import (
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// queueLossMonitor turns the producer's queueDropped counter into a bounded
// EpisodeLoss record OUT of the producer path: CaptureSlot only bumps the atomic
// (non-blocking, no log), and this monitor — driven by a ticker — reports newly-
// observed drops (warned at episode totals 1/10/100/…, each carrying the writer
// queue's depth/capacity) and declares recovery after `idle` with no new drops. step
// is pure and deterministic; runQueueLossMonitor just calls it on a ticker.
//
// This is the SAME model as internal/audio/capture's lossMonitor, so both queue-full
// paths recover identically. The small duplication is deliberate (the two counters
// and queue shapes differ, and "build specific" is the house default); the piece that
// must NOT drift — the log format — already lives once in logging.EpisodeLoss.
type queueLossMonitor struct {
	dropped  *atomic.Int64
	depthCap func() (depth, capacity int)
	loss     *logging.EpisodeLoss
	idle     time.Duration

	lastSeen int64
	lastAt   time.Time
}

// step processes one poll at time now: feed any newly-observed drops to the
// EpisodeLoss (recording the writer queue depth/capacity) and, when an episode is
// active and the idle window has elapsed since the last new drop, declare recovery.
func (m *queueLossMonitor) step(now time.Time) {
	cur := m.dropped.Load()
	if cur > m.lastSeen {
		depth, capacity := m.depthCap()
		m.loss.Add(cur-m.lastSeen, depth, capacity)
		m.lastSeen = cur
		m.lastAt = now
		return
	}
	if m.loss.Active() && now.Sub(m.lastAt) >= m.idle {
		m.loss.Recover()
	}
}

// runQueueLossMonitor samples queueDropped on a ticker until Stop closes s.quit, then
// flushes any open episode. It OWNS the L3 backpressure warn and recovery logging, so
// the producer and write paths carry none of it. Started in Start under
// safego.GoTracked; Stop waits on s.wg.
//
// baseline is queueDropped as of Start (captured synchronously by the caller, NOT
// Load()ed here): Start holds s.mu for its whole body so no CaptureSlot drop can have
// run yet, but a burst of drops immediately after Start returns could race the
// goroutine's first Load and be baselined away as pre-existing — so the count is
// pinned before the goroutine is launched.
func (s *Service) runQueueLossMonitor(baseline int64) {
	ticker := time.NewTicker(evidenceLossPollInterval)
	defer ticker.Stop()
	m := &queueLossMonitor{
		dropped:  &s.queueDropped,
		depthCap: func() (int, int) { return len(s.ch), cap(s.ch) },
		loss:     s.queueLoss,
		idle:     evidenceLossIdle,
		lastSeen: baseline,
	}
	for {
		select {
		case <-s.quit:
			// Sample once more before flushing: drops since the last poll have not
			// reached EpisodeLoss yet (the producer only bumps the atomic), so without
			// this a short overload right before shutdown would log neither the
			// first-loss warn nor a recovery, and an open episode's total would be
			// understated. step Adds any new drops; Recover then flushes the episode.
			m.step(time.Now())
			s.queueLoss.Recover()
			return
		case t := <-ticker.C:
			m.step(t)
		}
	}
}
