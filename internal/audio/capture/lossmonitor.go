package capture

import (
	"sync/atomic"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// audioLossPollInterval and audioLossIdle drive the drop monitor. Package vars so
// tests can dial them (the captureLinger pattern). idle is the quiet window after
// which recovery is declared — operator decision (2026-08-14): 5 s, deliberately
// shorter than L1/L2's 5-minute heartbeat so a brief overload is not reported as
// permanently active and the recovery summary is not delayed.
var (
	audioLossPollInterval = 500 * time.Millisecond
	audioLossIdle         = 5 * time.Second
)

// lossMonitor watches an audio capture's dropped-chunk counter OUT of the real-time
// callback — the callback only does an atomic increment (never blocks, never logs,
// L3's non-blocking-producer requirement) — and drives a bounded EpisodeLoss record:
// it reports newly-observed drops (warned at episode totals 1/10/100/…, each with
// the channel depth/capacity) and declares recovery after `idle` with no new drops.
// step is pure and deterministic; the capture goroutine just calls it on a ticker.
type lossMonitor struct {
	dropped  *atomic.Int64
	depthCap func() (depth, capacity int)
	loss     *logging.EpisodeLoss
	idle     time.Duration

	lastSeen int64
	lastAt   time.Time
}

// step processes one poll at time now: feed any newly-observed drops to the
// EpisodeLoss (recording the queue depth/capacity) and, when an episode is active
// and the idle window has elapsed since the last new drop, declare recovery.
func (m *lossMonitor) step(now time.Time) {
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
