package ft8

import (
	"sync"
	"time"
)

// The operator's presence in a scope (W-0027 C4b; ADR 0102 ruling 6).
//
// A scope is a kind of operation in which the daemon can transmit by itself once the
// operator has started it. It names one stream as its presence: its own event stream,
// opened by its own view. Presence in the scope is an open subscription to that stream made
// under the app lifetime that is current.
//
// THIS HELPER KNOWS NOTHING OF FT8 (operator ruling 2026-11-17, questions 2 and 7): it
// lives here until a second real scope exists, and is moved out then. It answers two
// questions and makes one call.
//
//   - has(lifetime): does that lifetime have a subscription open right now? Judged when
//     it is asked, so a stream opened before its lifetime was admitted counts once it is,
//     and one of a replaced lifetime simply stops being asked about.
//   - hold(lifetime, arm) / release(): an arm is being held by that lifetime. While it is,
//     and the lifetime has no subscription open, the grace runs. Other lifetimes' streams,
//     and streams that named no lifetime, never stand in for it. A return inside the grace
//     ends the grace and changes nothing else.
//   - lost(lifetime, arm, grace): a grace ran out. It is called outside the helper's
//     lock, and names the arm and the grace the decision was made for. The scope acts on
//     it under its own gate, and asks lossStands first: the decision may have waited, and
//     it is for that one absence only. A timer that fires late, for an arm since released
//     or for a grace since ended, calls nothing.
//
// The helper's lock is a leaf: it is never held while anything else is called.

// presenceTimer is a pending grace; Stop is time.Timer's.
type presenceTimer interface {
	Stop() bool
}

type presenceHold struct {
	lifetime string
	arm      uint64
}

type operatorPresence struct {
	grace func() time.Duration
	after func(time.Duration, func()) presenceTimer
	lost  func(lifetime string, arm, grace uint64)

	mu      sync.Mutex
	streams map[string]int // app lifetime -> open operating subscriptions
	held    presenceHold   // the arm being held; the zero value when none is
	timer   presenceTimer  // the pending grace, nil when none
	graceN  uint64         // which grace is pending: a late timer names an older one
}

func newOperatorPresence(grace func() time.Duration, after func(time.Duration, func()) presenceTimer, lost func(lifetime string, arm, grace uint64)) *operatorPresence {
	return &operatorPresence{grace: grace, after: after, lost: lost, streams: map[string]int{}}
}

// realAfter is time.AfterFunc as the helper takes it.
func realAfter(d time.Duration, f func()) presenceTimer { return time.AfterFunc(d, f) }

// has reports whether lifetime has an operating subscription open now.
func (p *operatorPresence) has(lifetime string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return lifetime != "" && p.streams[lifetime] > 0
}

// open counts one subscription made under lifetime and returns its close, which counts
// once however often it is called. A subscription that named no lifetime is not counted.
func (p *operatorPresence) open(lifetime string) (closeStream func()) {
	if lifetime == "" {
		return func() {}
	}
	p.mu.Lock()
	p.streams[lifetime]++
	if p.held.lifetime == lifetime {
		p.stopGraceLocked() // the holder is back
	}
	p.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			p.mu.Lock()
			defer p.mu.Unlock()
			if p.streams[lifetime]--; p.streams[lifetime] <= 0 {
				delete(p.streams, lifetime)
				if p.held.lifetime == lifetime {
					p.startGraceLocked()
				}
			}
		})
	}
}

// hold begins holding an arm for lifetime. If it has no subscription open, the grace
// starts at once.
func (p *operatorPresence) hold(lifetime string, arm uint64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopGraceLocked()
	p.held = presenceHold{lifetime: lifetime, arm: arm}
	if p.streams[lifetime] == 0 {
		p.startGraceLocked()
	}
}

// release ends the hold: the arm is gone, whatever ended it.
func (p *operatorPresence) release() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopGraceLocked()
	p.held = presenceHold{}
}

func (p *operatorPresence) stopGraceLocked() {
	if p.timer != nil {
		p.timer.Stop()
		p.timer = nil
	}
	p.graceN++ // whatever was pending is no longer this grace
}

func (p *operatorPresence) startGraceLocked() {
	p.stopGraceLocked()
	h, n := p.held, p.graceN
	p.timer = p.after(p.grace(), func() { p.expire(h, n) })
}

// expire is the end of grace n for hold h. It reports the loss only if that grace is
// still the pending one, for the same hold, and the holder still has nothing open.
func (p *operatorPresence) expire(h presenceHold, n uint64) {
	p.mu.Lock()
	if p.graceN != n || p.held != h || p.streams[h.lifetime] > 0 {
		p.mu.Unlock()
		return
	}
	p.timer = nil
	p.mu.Unlock()
	p.lost(h.lifetime, h.arm, n)
}

// lossStands reports whether the loss decided at the end of grace `grace`, for the arm
// `arm` held by `lifetime`, still stands: that hold is still the one held, that grace is
// still the last one started, and the holder still has nothing open. The decision is made
// outside every lock and may wait before it is acted on; if the holder returned meanwhile
// the grace is over, and if it returned and left again a NEW grace has started for the
// same arm, which this decision must not cut short (codex-134).
func (p *operatorPresence) lossStands(lifetime string, arm, grace uint64) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.graceN == grace && p.held == (presenceHold{lifetime: lifetime, arm: arm}) && p.streams[lifetime] == 0
}
