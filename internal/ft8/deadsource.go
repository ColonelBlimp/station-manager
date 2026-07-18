package ft8

// Dead-capture-stream watchdog (dogfood 2026-07-18, "Plasma upgrade day").
//
// A desktop audio-stack reshuffle (KDE/PipeWire device fiddling) can destroy
// and recreate the capture device's nodes UNDER a live session, leaving the
// daemon's stream dangling: miniaudio keeps "running", no error surfaces
// anywhere, and the scheduler sees either no sample batches at all (a starved
// callback) or batches of pure digital silence — so the operator watches an
// apparently-live Band Activity that never decodes again. The watchdog turns
// that silent failure into an automatic release + reacquire (a fresh stream
// links to the current device nodes — exactly the manual close/reopen fix,
// without the operator).
//
// Detection runs at the slot boundary (the scheduler's timer fires there even
// when NO samples arrive, which is what the incident looked like — the ring
// never filled, so no Slot was ever emitted and a decode-side check would
// never have run). Two signals, either marks the window dead:
//
//   - starved: fewer than minLiveWindowSamples arrived since the previous
//     boundary. A healthy window delivers SlotSamples (180 000); the generous
//     quarter-slot floor tolerates transient PipeWire under-delivery.
//   - silent: samples arrived but every one was exactly zero. An analog
//     input always carries ADC noise in the LSBs — a whole window of literal
//     digital zeros does not happen on a live analog device. (A digital
//     input with nothing wired COULD be true zero; the cost of a false
//     positive is one ~30 s release/reacquire cycle plus a warn log, not an
//     outage, and capture is CAT-gated so the rig is on whenever this runs.)
//
// Strike policy mirrors the bridge's CAT no-data counter (noDataStrikeLimit):
// a dead window is a strike, a live window resets to zero, and the limit
// fires the callback ONCE per scheduler run (the session is being replaced;
// the fresh session gets a fresh monitor). The first boundary after start
// only baselines — its window is legitimately partial (capture starts
// mid-slot). Worst-case detection latency is therefore ~3 boundaries (~45 s).
//
// If the reacquired stream is also dead the fresh monitor re-fires and the
// cycle retries at that cadence; if the reacquire itself fails (device truly
// gone) the CAT-reconcile loop keeps retrying every catReconcileInterval —
// converging with the already-proven "device not found" recovery path the
// moment the device returns.

// deadSourceStrikeLimit is how many consecutive dead windows fire the
// callback. Two, like the CAT gate's noDataStrikeLimit: one window absorbs a
// transient hiccup, two in a row is a dead stream.
const deadSourceStrikeLimit = 2

// minLiveWindowSamples is the per-window delivery floor below which the
// source counts as starved. A quarter slot — far below the healthy 180 000,
// far above trickle-from-a-dead-stream.
const minLiveWindowSamples = SlotSamples / 4

// deadSourceMonitor accumulates per-window liveness between slot boundaries.
// Owned and driven by the scheduler goroutine (no locking, like sampleRing);
// inert when onDead is nil. Pure bookkeeping — timers stay in the scheduler —
// so the strike policy is unit-testable without wall-clock slots.
type deadSourceMonitor struct {
	onDead     func(reason string)
	primed     bool  // first boundary seen (baseline only — partial window)
	lastFilled int64 // ring fill count at the previous boundary
	windowLive bool  // any non-zero sample seen this window
	strikes    int
	fired      bool
}

// observeBatch notes whether the window has carried any live (non-zero)
// audio yet. Early-exits once it has, so the healthy path scans each batch
// at most until its first non-zero sample.
func (m *deadSourceMonitor) observeBatch(batch []int16) {
	if m.onDead == nil || m.fired || m.windowLive {
		return
	}
	for _, v := range batch {
		if v != 0 {
			m.windowLive = true
			return
		}
	}
}

// onBoundary closes the window that just ended (filled = the ring's total
// fill count at this boundary), applies the strike policy, and opens the
// next window. Fires onDead at the strike limit, once.
func (m *deadSourceMonitor) onBoundary(filled int64) {
	if m.onDead == nil || m.fired {
		return
	}
	if !m.primed {
		m.primed = true
		m.lastFilled = filled
		m.windowLive = false
		return
	}
	delta := filled - m.lastFilled
	starved := delta < minLiveWindowSamples
	dead := starved || !m.windowLive
	m.lastFilled = filled
	m.windowLive = false
	if !dead {
		m.strikes = 0
		return
	}
	m.strikes++
	if m.strikes < deadSourceStrikeLimit {
		return
	}
	m.fired = true
	reason := "silent"
	if starved {
		reason = "starved"
	}
	m.onDead(reason)
}
