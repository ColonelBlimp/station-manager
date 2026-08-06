package ft8

import (
	"sync"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

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
	logger logging.Logger
	nextID int64
	closed bool

	lastOccupancy *hubEvent
	lastDecode    *hubEvent
	lastTx        *hubEvent
	lastQso       *hubEvent

	// Audio-level meter: PULL delivery, never the subscriber buffers (review
	// d22eff6b). At ~4 Hz the meter filled the 8-slot buffer within a ~2 s
	// SSE write stall, after which the next event EVICTED the subscriber —
	// and through the capture linger that can disarm TX mid-QSO. Latest-wins
	// by construction: each SSE writer polls latestAudio on its own ticker
	// (audioLevelEmitInterval) and emits on generation change, so a stalled
	// reader just emits the newest value when it recovers. The 2026-08-01
	// eviction ruling (buffers stay 8) keeps its arithmetic: real events
	// have the whole buffer to themselves.
	lastAudio *hubEvent
	audioGen  uint64
}

func newHub(logger logging.Logger) *hub {
	return &hub{subs: make(map[int64]chan hubEvent), logger: logger}
}

// publish caches the event by type and fans it out. A subscriber whose buffer
// is full is evicted (channel closed, removed) — the decode loop must never
// block on a stuck SSE client. Publish on a closed hub is a no-op.
func (h *hub) publish(evt hubEvent) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
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
	before := len(h.subs)
	var evicted []evictedSubscriber
	for id, ch := range h.subs {
		select {
		case ch <- evt:
		default:
			// Depth read BEFORE the close, so a later reordering cannot report zero
			// for the very buffer that overflowed.
			evicted = append(evicted, evictedSubscriber{id: id, depth: len(ch), capacity: cap(ch)})
			close(ch)
			delete(h.subs, id)
		}
	}
	logger, after := h.logger, len(h.subs)
	h.mu.Unlock()
	logEvictions(logger, evt.name, before, after, evicted)
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
	//
	// OCCUPANCY IS DELIBERATELY NOT REPLAYED. The report carries no band, so a
	// subscriber cannot tell a cached pre-QSY snapshot from a live one and stamps
	// it with whatever band the rig is on NOW — and since the SPA's effectiveOffset
	// falls back to suggested[0], that mislabelled snapshot can become the transmit
	// offset on a band it was never measured on. The window is not exotic: capture
	// lingers 5 s past the last unsubscribe, and a report is published ~16 s after
	// its slot, so QSY-then-refresh lands squarely in it. Client-side age limits
	// cannot close this (the timestamps come from two different clocks, and a
	// threshold loose enough for decode latency is looser than the race).
	// The cost of dropping it is exactly what this file's own header already
	// accepts as the fallback: the next slot is ≤15 s away and refreshes the panel.
	// Decode/tx/qso REMAIN cached — a stale decode list is cosmetic, it does not
	// steer where we transmit.
	for _, cached := range []*hubEvent{h.lastDecode, h.lastTx, h.lastQso} {
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

// publishAudio stores the meter newest reading for pull delivery (see the
// lastAudio field note). No-op on a closed hub, like publish.
func (h *hub) publishAudio(evt hubEvent) {
	h.mu.Lock()
	if !h.closed {
		cp := evt
		h.lastAudio = &cp
		h.audioGen++
	}
	h.mu.Unlock()
}

// latestAudio returns the newest audio-level event and its generation
// (0 = none published yet).
func (h *hub) latestAudio() (*hubEvent, uint64) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.lastAudio, h.audioGen
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
	// The audio pull cache dies with its capture session too (review a1529400
	// P1): left in place, a tab connecting after release was served the DEAD
	// session's level as live — the no-capture state must publish nothing.
	// audioGen is NOT reset: an existing connection's seen-generation is from
	// the old numbering, and a reset that lands the new session on the same
	// small numbers would swallow live emits. Monotonic forever, nil value.
	h.lastAudio = nil
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

// evictedSubscriber is one slow reader dropped by a single publish.
type evictedSubscriber struct {
	id       int64
	depth    int
	capacity int
}

// logEvictions reports slow-reader evictions, one record each. Called WITHOUT
// h.mu — the publisher must never be slowed by a log write, the same reason publish does
// not block on a subscriber.
//
// WARN, not Error: the hub behaved exactly as designed and the daemon is
// healthy — a client could not keep up.
//
// IT MATTERS MOST HERE. Eviction closes the channel; the SSE handler treats that
// as "stream ended" and unsubscribes; if it was the last subscriber the linger
// expires and onLingerExpired calls disarmTx, dropping PTT and ABANDONING AN
// ACTIVE QSO. That teardown is deliberate and stays (operator, 2026-08-01) — the
// enforced proxy is a FUNCTIONING subscription, not an open tab, and a reconnect
// inside the linger cancels it. This record is what makes the difference visible.
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
// The field set is DUPLICATED across internal/events, internal/bridge and
// internal/ft8, each of which has its own hub type. A shared helper would be a
// framework for three call sites; what stops them drifting is that all three
// packages assert the SAME names. Canonical criterion + the operator's reasoning:
// internal/events/hub_eviction_test.go.
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
			Msg("ft8: subscriber evicted — too slow to keep up; its stream ended and it must reconnect")
	}
}
