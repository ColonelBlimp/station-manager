package ft8

import "sync"

// occSubscriberBufferSize is the per-subscriber channel capacity. Occupancy
// fires once per 15-second slot — a tiny buffer absorbs the rare case where a
// new slot publishes in the window between a subscriber being registered and
// its SSE handler reading, without ever needing real headroom.
const occSubscriberBufferSize = 4

// occHub is the FT8 occupancy pub/sub fan-out: one publisher (the decode loop,
// once per slot) to N subscribers (one per open ft8-occupancy SSE connection).
// It mirrors the shape of internal/bridge's hub but is much simpler — a single
// event type and a single one-slot replay cache.
//
// The replay cache (last) holds the most recent report so a SPA tab opening
// mid-slot gets current occupancy immediately rather than waiting up to 15 s
// for the next slot (the ADR 0009 late-subscriber-replay pattern). Unlike the
// bridge's caches there is no clear-on-other-event rule: the latest report is
// always the truth, and replaying a stale-but-recent slot to a new tab is
// harmless — the next slot overwrites it.
type occHub struct {
	mu     sync.Mutex
	subs   map[int64]chan OccupancyReport
	nextID int64
	closed bool
	last   *OccupancyReport
}

func newOccHub() *occHub {
	return &occHub{subs: make(map[int64]chan OccupancyReport)}
}

// publish caches the report and fans it out to every current subscriber. A
// subscriber whose buffer is full is evicted (channel closed, removed) — the
// decode loop must never block on a stuck SSE client. Publish on a closed hub
// is a no-op.
func (h *occHub) publish(rep OccupancyReport) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	cp := rep
	h.last = &cp
	for id, ch := range h.subs {
		select {
		case ch <- rep:
		default:
			close(ch)
			delete(h.subs, id)
		}
	}
}

// subscribe registers a new subscriber, returning a receive-only channel and an
// idempotent unsubscribe. The cached latest report (if any) is replayed as the
// subscriber's first event. The channel closes on unsubscribe, slow-reader
// eviction, or hub close — the SSE handler treats all three as "stream ended".
// Subscribing to a closed hub returns an already-closed channel.
func (h *occHub) subscribe() (<-chan OccupancyReport, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan OccupancyReport)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan OccupancyReport, occSubscriberBufferSize)
	id := h.nextID
	h.nextID++
	h.subs[id] = ch

	if h.last != nil {
		// Non-blocking send into the just-allocated buffer (empty, cap>0).
		select {
		case ch <- *h.last:
		default:
		}
	}

	unsub := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if existing, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(existing)
		}
	}
	return ch, unsub
}

// latest returns the most recent report, or nil if none has been published.
func (h *occHub) latest() *OccupancyReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.last
}

// close disconnects all subscribers and marks the hub closed. Idempotent.
func (h *occHub) close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.closed = true
	for id, ch := range h.subs {
		close(ch)
		delete(h.subs, id)
	}
}

// subscriberCount returns the active subscriber count. Test-only barrier helper.
func (h *occHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
