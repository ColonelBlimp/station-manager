package bridge

import "sync"

// subscriberBufferSize is the per-subscriber channel capacity.
// Same default as internal/events.Hub — small enough to evict
// truly-stuck subscribers promptly, large enough to absorb a short
// burst of rig pushes (rapid dial-spin) without slowing the
// publisher.
const subscriberBufferSize = 64

// hub is a bridge-internal pub/sub fan-out. One publisher (the
// serial+CAT decode loop, future M3a.2; the stub-event ticker in
// M3a.1), N subscribers (one per open SSE connection). Mirrors the
// shape of internal/events.Hub but typed for bridge.Event so callers
// don't have to type-assert payloads.
//
// Build-specific-not-generic: the project already had events.Hub but
// it's typed for the QSO-stored event vocabulary; threading a
// bridge.Event through a generic interface would re-introduce the
// type-assertion ceremony events.Hub was built to avoid. A second
// hub typed for bridge events is cheaper than the abstraction.
type hub struct {
	mu        sync.Mutex
	subs      map[int64]chan Event
	nextSubID int64
	closed    bool
}

func newHub() *hub {
	return &hub{
		subs: make(map[int64]chan Event),
	}
}

// publish fans an event out to every current subscriber. Slow
// subscribers (channel buffer full) are evicted (channel closed,
// removed from map) — same policy as events.Hub. Publish on a closed
// hub is a no-op; the bridge subsystem MUST NOT block its decode
// loop on a stuck SSE client.
func (h *hub) publish(evt Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	for id, ch := range h.subs {
		select {
		case ch <- evt:
		default:
			close(ch)
			delete(h.subs, id)
		}
	}
}

// subscribe registers a new subscriber. Returns the receive-only
// channel + an idempotent unsubscribe function. The channel is closed
// when (a) the unsubscribe fires, (b) the subscriber is evicted for
// being too slow, or (c) the hub is closed. SSE handlers should treat
// any of those the same — stream ended, return.
//
// Subscribing to a closed hub returns an already-closed channel so
// the consumer's range-loop exits immediately.
func (h *hub) subscribe() (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan Event)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan Event, subscriberBufferSize)
	id := h.nextSubID
	h.nextSubID++
	h.subs[id] = ch

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

// close disconnects all subscribers and marks the hub closed.
// Subsequent publish calls are no-ops; subsequent subscribe calls
// return already-closed channels. Safe to call more than once.
func (h *hub) close() {
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

// subscriberCount returns the number of active subscribers. Test-only
// helper for "wait until the handler has subscribed before publishing"
// barriers. Production code should not branch on it.
func (h *hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
