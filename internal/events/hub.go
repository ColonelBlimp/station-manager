package events

import (
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

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
	// logger reports slow-reader evictions. Set AFTER construction rather than
	// taken by NewHub because cmd/smd builds the hub before the DI container
	// builds the logging service — the hub must exist first so services with an
	// `eventhub` inject field all receive the same instance. Nil until then, which
	// is safe: subscribers only exist once the HTTP server is listening, long
	// after logging is up.
	logger logging.Logger
}

// NewHub returns a ready-to-use Hub.
func NewHub() *Hub {
	return &Hub{
		subs: make(map[int64]chan Event),
	}
}

// SetLogger installs the logger used to report slow-reader evictions. Safe to
// call once at startup; guarded by the hub's own mutex so it cannot race a
// concurrent Publish.
func (h *Hub) SetLogger(l logging.Logger) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logger = l
}

// Publish fans an event out to every current subscriber. If a
// subscriber's buffer is full, that subscriber is disconnected —
// its channel is closed and it is removed from the hub. Publish
// on a closed hub is a no-op.
//
// Publish never blocks on a subscriber. Publishers are daemon-
// internal code paths (worker, qsoservice) that MUST NOT be slowed
// by a slow HTTP client.
//
// No replay/backlog semantics. Once Close has been called, every
// subsequent Publish is silently dropped — there is no buffer that
// holds events for a future subscribe, and the hub never replays
// historical events on reconnect. The shutdown sequence relies on
// this: workers stop publishing before Close, so the only events
// that can reach a "closed-hub Publish" are stragglers from a
// race the daemon should never produce. A future "event replay
// on reconnect" feature would need to revisit this contract — it
// is not free to add on top of the current shape.
func (h *Hub) Publish(name string, payload any) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.nextEvtID++
	evt := Event{ID: h.nextEvtID, Name: name, Payload: payload}

	before := len(h.subs)
	var evicted []evictedSubscriber
	for id, ch := range h.subs {
		select {
		case ch <- evt:
		default:
			// Depth read BEFORE the close: a closed channel still reports len, but
			// reading it after would invite a later reordering to report zero for the
			// very buffer that overflowed.
			evicted = append(evicted, evictedSubscriber{id: id, depth: len(ch), capacity: cap(ch)})
			close(ch)
			delete(h.subs, id)
		}
	}
	logger, after := h.logger, len(h.subs)
	h.mu.Unlock()

	// Outside the lock. Publish is on daemon-internal paths (worker, qsoservice)
	// that must never be slowed by a slow consumer — the same reason it does not
	// block on a subscriber applies to the log write.
	logEvictions(logger, name, before, after, evicted)
}

// evictedSubscriber is one slow reader dropped by a single Publish.
type evictedSubscriber struct {
	id       int64
	depth    int
	capacity int
}

// logEvictions reports slow-reader evictions, one record each. Called WITHOUT
// h.mu.
//
// WARN, not Error: the hub behaved exactly as designed and the daemon is healthy —
// a client could not keep up. Error stays for faults the daemon itself has.
//
// SYNCHRONOUS, and that was challenged in review (codex P2 on 57619abe). The
// concern is real in mechanism — zerolog's Msg writes straight to lumberjack, so a
// stalled disk delays the publisher — but it is NOT a new class of risk here, and
// the proposed remedy (a bounded async path) would be new machinery for the rarest
// line in the package:
//
//   - The requirement these hubs actually document is that a publisher must not
//     block ON A SLOW SUBSCRIBER. That is untouched: the select is still
//     non-blocking, and the eviction is complete and the lock released before
//     anything is written.
//   - These goroutines ALREADY log synchronously, far more often. Measured on the
//     live log over 15 days: 1,502 `ft8 tx meters` records (one per transmission,
//     the same unkey path) and 8 drive alarms, against 0 evictions.
//   - An eviction is self-limiting — the subscriber is removed, so it cannot
//     repeat — and these hubs are documented for 1-3 concurrent subscribers, so the
//     worst case is a handful of writes, once.
//
// So: if a stalled log destination endangers these goroutines it endangers the
// whole package, and belongs at the logging layer, fixed once. Giving the least
// frequent line its own asynchronous path would leave the 1,502 more frequent ones
// exactly as exposed while adding a mechanism to maintain. Revisit if evictions
// ever become common — the same evidence the operator is waiting for before
// tuning the buffers.
//
// The field set is DUPLICATED in internal/bridge and internal/ft8, which have
// their own hubs. A shared helper would be a framework for three call sites; what
// stops the three drifting is that each package's rules assert the same names, so
// a rename in one turns that package red (hub_eviction_test.go E2 here).
func logEvictions(logger logging.Logger, evtName string, before, after int, evicted []evictedSubscriber) {
	if logger == nil {
		return
	}
	for _, e := range evicted {
		logger.WarnWith().
			Int64("subscriber_id", e.id).
			Str("event", evtName).
			Int("queue_depth", e.depth).
			Int("queue_capacity", e.capacity).
			Int("subs_before", before).
			Int("subs_after", after).
			Msg("events: subscriber evicted — too slow to keep up; its stream ended and it must reconnect")
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

// SubscriberCount returns the number of active subscribers. Primarily
// useful for test barriers ("wait until the handler has subscribed
// before publishing") and for debug introspection; production code
// should not branch on it.
func (h *Hub) SubscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
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
