package ft8

import "sync"

// subscriberBufferSize is the per-subscriber channel capacity. FT8 events fire
// at most a couple per slot (one decode + one occupancy every 15 s), so a small
// buffer absorbs the publish-vs-handler-read window without ever needing real
// headroom; a wedged subscriber is evicted rather than slowing the decode loop.
const subscriberBufferSize = 8

// hubEvent is one FT8 → SSE-subscriber message: an SSE event name and a payload
// the handler JSON-marshals into the data line. Payload is untyped at this
// layer (each name has its own shape — OccupancyReport, DecodeReport); the
// handler marshals whatever the publisher provides.
type hubEvent struct {
	name    string
	payload any
}

// hub is the FT8 subsystem's pub/sub fan-out: one publisher (the decode loop,
// per slot) to N subscribers (one per open /v1/ft8/events SSE connection).
// Mirrors internal/bridge's hub — multiple event types over one stream, each
// with a one-slot replay cache so a SPA tab connecting mid-slot sees the
// current state immediately (the ADR 0009 late-subscriber pattern) rather than
// waiting up to 15 s for the next slot.
//
// The two cached slots are the latest of each event type. There is no
// clear-on-other-event rule: within a live capture session each latest report is
// the truth, and replaying a recent slot to a new tab is correct — the next slot
// overwrites it. The decode + occupancy caches ARE dropped (clearActivity) when a
// capture session is released, so a tab connecting after the session ended (rig
// off, capture can't reacquire) isn't shown a stale slot from the prior session.
type hub struct {
	mu     sync.Mutex
	subs   map[int64]chan hubEvent
	nextID int64
	closed bool

	lastOccupancy *hubEvent
	lastDecode    *hubEvent
	lastTx        *hubEvent
	lastQso       *hubEvent
}

func newHub() *hub {
	return &hub{subs: make(map[int64]chan hubEvent)}
}

// publish caches the event by type and fans it out. A subscriber whose buffer
// is full is evicted (channel closed, removed) — the decode loop must never
// block on a stuck SSE client. Publish on a closed hub is a no-op.
func (h *hub) publish(evt hubEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return
	}
	cp := evt
	switch evt.name {
	case EventOccupancy:
		h.lastOccupancy = &cp
	case EventDecode:
		h.lastDecode = &cp
	case EventTx:
		h.lastTx = &cp
	case EventQso:
		h.lastQso = &cp
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

// subscribe registers a new subscriber, returning a receive-only channel and an
// idempotent unsubscribe. Both cached events (if present) are replayed as the
// subscriber's first messages. The channel closes on unsubscribe, slow-reader
// eviction, or hub close — the SSE handler treats all three as "stream ended".
// Subscribing to a closed hub returns an already-closed channel.
func (h *hub) subscribe() (<-chan hubEvent, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.closed {
		ch := make(chan hubEvent)
		close(ch)
		return ch, func() {}
	}

	ch := make(chan hubEvent, subscriberBufferSize)
	id := h.nextID
	h.nextID++
	h.subs[id] = ch

	// Replay cached events into the just-allocated buffer (non-blocking; cap>0).
	for _, cached := range []*hubEvent{h.lastDecode, h.lastOccupancy, h.lastTx, h.lastQso} {
		if cached != nil {
			select {
			case ch <- *cached:
			default:
			}
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

// latestOccupancy returns the most recent occupancy report, or nil if none has
// been published.
func (h *hub) latestOccupancy() *OccupancyReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastOccupancy == nil {
		return nil
	}
	if rep, ok := h.lastOccupancy.payload.(OccupancyReport); ok {
		return &rep
	}
	return nil
}

// clearActivity drops the cached decode + occupancy slots so a subscriber that
// connects later is not replayed a stale Band Activity / occupancy snapshot from
// an ended capture session (e.g. the operator reopens the SPA with the rig off:
// capture can't reacquire, so nothing would overwrite the replay). Called when a
// capture session is released. The TX/QSO caches are left intact — they reflect
// daemon-owned state independent of capture, and the SPA keeps the selected TX
// offset in localStorage, so neither is affected.
func (h *hub) clearActivity() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lastDecode = nil
	h.lastOccupancy = nil
}

// close disconnects all subscribers and marks the hub closed. Idempotent.
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

// subscriberCount returns the active subscriber count. Test-only barrier helper.
func (h *hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
