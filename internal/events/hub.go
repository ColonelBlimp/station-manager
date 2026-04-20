package events

import "sync"

// subscriberBufferSize is the per-subscriber channel capacity.
// Chosen to absorb a short burst of events (e.g. a rapid import
// run) without slowing the publisher, while being small enough
// that a truly-stuck subscriber is evicted promptly rather than
// sitting on a large in-memory backlog.
const subscriberBufferSize = 64

// Hub is an in-memory fan-out pub/sub for daemon lifecycle events.
// Safe for concurrent publishers and subscribers. The zero value is
// not usable — construct via NewHub.
type Hub struct {
	mu        sync.Mutex
	subs      map[int64]chan Event
	nextSubID int64
	nextEvtID int64
	closed    bool
}

// NewHub returns a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[int64]chan Event),
	}
}

// Publish fans an event out to every current subscriber. If a
// subscriber's buffer is full, that subscriber is disconnected —
// its channel is closed and it is removed from the hub. Publish
// on a closed hub is a no-op.
//
// Publish never blocks on a subscriber. Publishers are daemon-
// internal code paths (worker, qsoservice) that MUST NOT be slowed
// by a slow HTTP client.
func (h *Hub) Publish(name string, payload any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	h.nextEvtID++
	evt := Event{ID: h.nextEvtID, Name: name, Payload: payload}

	for id, ch := range h.subs {
		select {
		case ch <- evt:
		default:
			close(ch)
			delete(h.subs, id)
		}
	}
}

// Subscribe registers a new subscriber and returns its event
// channel plus an unsubscribe function. The channel is closed
// either when the caller invokes the unsubscribe function, when
// Publish evicts the subscriber for being too slow, or when
// Close is called on the hub; consumers should treat any of those
// the same ("stream ended, reconnect if desired").
//
// Calling the returned unsubscribe function more than once is
// safe; redundant calls are no-ops.
//
// Subscribing to a closed hub returns a channel that is already
// closed, so the consumer's range loop exits immediately.
func (h *Hub) Subscribe() (<-chan Event, func()) {
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

// Close disconnects every subscriber and marks the hub closed so
// later Publish calls are no-ops and later Subscribe calls return
// an already-closed channel. Safe to call more than once.
//
// Intended for use at daemon shutdown, after all publishers have
// stopped emitting.
func (h *Hub) Close() {
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
