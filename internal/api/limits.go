package api

import (
	"sync"
	"time"
)

// loadLimiter holds the runtime state for the three accidental-self-DoS
// defences documented in docs/v2-design/api.md §6:
//
//   - a concurrent-request semaphore covering all non-SSE endpoints,
//   - a subscriber counter covering the /v1/events SSE endpoint, and
//   - a per-endpoint token bucket covering POST /v1/qso.
//
// None of these defend against a malicious actor; they defend against
// a buggy local client (cron loop, retry storm, reconnect loop) that
// would otherwise starve sqlite connections, goroutines, or file
// descriptors in the single-user Unix-socket deployment.
type loadLimiter struct {
	// concurrent is a counting semaphore implemented as a buffered
	// channel of struct{}. Capacity == MaxConcurrentRequests. A
	// non-blocking send acquires a slot; a receive releases it.
	// Non-blocking semantics are deliberate: we refuse new work fast
	// (503) rather than piling up goroutines waiting for a slot.
	concurrent chan struct{}

	// subscribersMu guards subscribers; a plain counter + mutex is
	// clearer here than a semaphore because the SSE handler needs to
	// observe the count for logging / health as well as enforce it.
	subscribersMu  sync.Mutex
	subscribers    int
	maxSubscribers int

	// submitBucket is a token bucket for the POST /v1/qso endpoint.
	// Tokens refill at ratePerSec up to burst; each submit consumes
	// one. Refill is lazy (computed on each allow call from elapsed
	// time) so there is no ticker goroutine.
	submitMu       sync.Mutex
	submitTokens   float64
	submitLastFill time.Time
	submitRate     float64
	submitBurst    float64
}

func newLoadLimiter(maxConcurrent, maxSubs, submitRatePerSec, submitBurst int) *loadLimiter {
	return &loadLimiter{
		concurrent:     make(chan struct{}, maxConcurrent),
		maxSubscribers: maxSubs,
		submitTokens:   float64(submitBurst),
		submitLastFill: time.Now(),
		submitRate:     float64(submitRatePerSec),
		submitBurst:    float64(submitBurst),
	}
}

// acquireConcurrent tries to take a slot in the concurrent-request
// semaphore without blocking. Returns true on success and a release
// func that must be called when the request is done.
func (l *loadLimiter) acquireConcurrent() (bool, func()) {
	select {
	case l.concurrent <- struct{}{}:
		return true, func() { <-l.concurrent }
	default:
		return false, nil
	}
}

// acquireSubscriber tries to reserve a subscriber slot for /v1/events.
// Returns true on success and a release func that must be called when
// the subscriber disconnects.
func (l *loadLimiter) acquireSubscriber() (bool, func()) {
	l.subscribersMu.Lock()
	defer l.subscribersMu.Unlock()
	if l.subscribers >= l.maxSubscribers {
		return false, nil
	}
	l.subscribers++
	return true, l.releaseSubscriber
}

func (l *loadLimiter) releaseSubscriber() {
	l.subscribersMu.Lock()
	defer l.subscribersMu.Unlock()
	if l.subscribers > 0 {
		l.subscribers--
	}
}

// allowSubmit consumes one token from the submit bucket if available.
// Returns true when the submit may proceed. Refill is computed from
// elapsed wall time since the last check, so there is no background
// goroutine to manage.
func (l *loadLimiter) allowSubmit(now time.Time) bool {
	l.submitMu.Lock()
	defer l.submitMu.Unlock()
	elapsed := now.Sub(l.submitLastFill).Seconds()
	if elapsed > 0 {
		l.submitTokens += elapsed * l.submitRate
		if l.submitTokens > l.submitBurst {
			l.submitTokens = l.submitBurst
		}
	}
	// Always advance lastFill — even when elapsed <= 0 (e.g. a clock
	// step backwards, or an injected test time) — so the next call
	// measures from a known anchor rather than accumulating negative
	// elapsed forever.
	l.submitLastFill = now
	if l.submitTokens < 1 {
		return false
	}
	l.submitTokens--
	return true
}
