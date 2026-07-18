package bridge

import "sync"

// subscriberBufferSize is the per-subscriber channel capacity.
// Same default as internal/events.Hub — small enough to evict
// truly-stuck subscribers promptly, large enough to absorb a short
// burst of rig pushes (rapid dial-spin) without slowing the
// publisher.
const subscriberBufferSize = 64

// hub is a bridge-internal pub/sub fan-out. One publisher (the
// serial+CAT decode loop), N subscribers (one per open SSE connection).
// Mirrors the shape of internal/events.Hub but typed for bridge.Event so
// callers don't have to type-assert payloads.
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

	// lastBridgeError caches the most recent EventBridgeError so a
	// subscriber that connects AFTER the pipeline emitted the error
	// (typical case: daemon starts with a typo'd driver, fails
	// immediately, SPA tab opens 30s later) still sees the toast.
	// Without the cache, startup-time bridge-errors fire to zero
	// subscribers and the operator gets blank UI with no signal.
	//
	// Scope is per-Service-instance (hub is owned by Service, fresh
	// hub on daemon restart). New cache entry overwrites previous.
	// Permanent / operator-actionable errors stay cached for the
	// Service's lifetime — once observed they stay observable, on the
	// principle that surfacing them is more valuable than forgetting.
	// Per the 2026-05-10 internal-bridge-pipeline review (#2): chosen
	// over lazy-start because operators expect the daemon to hold the
	// rig from start.
	//
	// The one exception (review 2026-06-04 M1): a TRANSIENT cached error
	// (serial_open_failed / init_write_failed — the rig was off at boot)
	// is cleared on the first EventRigState, because a successful rig
	// push proves the supervisor recovered and the cached toast is now
	// stale. See publish + BridgeErrorCode.isTransient.
	lastBridgeError *Event

	// lastRigDisconnected caches the most recent EventRigDisconnected
	// for the same late-subscriber reason as lastBridgeError, with
	// one critical difference: rig-disconnected auto-recovers. When
	// the rig starts pushing data again the pipeline emits
	// EventRigState (implicit reconnect per ADR 0009 — the SPA's
	// rigResponding flips back to true on any rig-state); a cached
	// disconnect at that point would be stale and misleading. So
	// publish clears the cache on EventRigState arrival.
	//
	// Without this cache, the operator's second / refreshed SPA tab
	// never sees the "Rig disconnected" toast for an off-rig that
	// never came up — the pipeline's announcedDisconnect dedup flag
	// fires the disconnect exactly once per silent-window, the first
	// subscriber gets it (or misses it if not yet subscribed), and
	// every later subscriber gets nothing because no new event ever
	// fires while the rig stays silent. The SPA's bridgeState shows
	// `rigResponding=false` but the toast notification is absent.
	lastRigDisconnected *Event

	// lastTuneState caches the most recent EventTuneState so a SPA tab
	// opening mid-tune learns the carrier is up (ADR 0027). Unlike
	// lastRigDisconnected there's no clear-on-other-event rule: tune-state
	// is a clean boolean the daemon owns, so the latest value is always the
	// truth — an inactive cache replayed to a late subscriber matches the
	// SPA's default and is harmless.
	lastTuneState *Event

	// lastTxAlarm caches the most recent EventTxAlarm (ADR 0051) — same
	// latest-value-is-truth rule as tune-state, but with real safety weight:
	// a tab opened AFTER the alarm raised must still show the banner.
	lastTxAlarm *Event
}

// clearCachedBridgeError drops the cached bridge-error replay when its code
// matches — used when the condition it warned about has demonstrably resolved
// (e.g. identity later confirmed after an unrecognised first ID). No-op on a
// different cached code or an empty cache.
func (h *hub) clearCachedBridgeError(code BridgeErrorCode) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.lastBridgeError == nil {
		return
	}
	if p, ok := h.lastBridgeError.Payload.(BridgeErrorPayload); ok && p.Code == code {
		h.lastBridgeError = nil
	}
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
	// Cache bridge-error events so late subscribers (SPA tab opening
	// after a startup failure) still see the toast.
	if evt.Name == EventBridgeError {
		cp := evt
		h.lastBridgeError = &cp
	}
	// Cache rig-disconnected so late subscribers see the toast even
	// after the pipeline's once-per-silent-window dedup has fired.
	// Clear it on rig-state arrival: a successful rig push is the
	// implicit-reconnect signal (per ADR 0009), so a cached
	// disconnect would be stale and would misleadingly toast new
	// subscribers when the rig is actually fine.
	switch evt.Name {
	case EventRigDisconnected:
		cp := evt
		h.lastRigDisconnected = &cp
	case EventRigState:
		h.lastRigDisconnected = nil
		// A successful rig push means any TRANSIENT bridge-error we cached
		// (serial_open_failed / init_write_failed at first boot, since
		// recovered by the supervisor) is stale — drop it so a tab opening
		// after recovery doesn't get a phantom toast. Permanent faults exit
		// the pipeline and never produce a rig-state, so they never reach
		// here; identity warnings are non-transient and stay cached
		// (review 2026-06-04 M1).
		if h.lastBridgeError != nil {
			if p, ok := h.lastBridgeError.Payload.(BridgeErrorPayload); ok && p.Code.isTransient() {
				h.lastBridgeError = nil
			}
		}
	case EventTuneState:
		cp := evt
		h.lastTuneState = &cp
	case EventTxAlarm:
		cp := evt
		h.lastTxAlarm = &cp
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

	// Replay the cached bridge-error (if any) to the new subscriber
	// as their first event. Non-blocking send is safe: the channel
	// was just allocated with cap=64 so the buffer is empty.
	if h.lastBridgeError != nil {
		select {
		case ch <- *h.lastBridgeError:
		default:
		}
	}
	// Replay the cached rig-disconnected too. Order doesn't matter
	// for the SPA's merge semantics: both events touch different
	// bridgeState facets (bridge-error → toast only; rig-disconnected
	// → toast + rigResponding=false), so a second slot in the
	// cap-64 buffer is also safe.
	if h.lastRigDisconnected != nil {
		select {
		case ch <- *h.lastRigDisconnected:
		default:
		}
	}
	// Replay the cached tune-state so a tab opening mid-tune (ADR 0027)
	// learns the carrier is up. Third optional replay into the cap-64
	// buffer — still safe.
	if h.lastTuneState != nil {
		select {
		case ch <- *h.lastTuneState:
		default:
		}
	}
	// Replay a standing TX alarm (ADR 0051) — safety-critical: the operator's
	// fresh tab must show the banner for a rig that may still be keyed.
	if h.lastTxAlarm != nil {
		select {
		case ch <- *h.lastTxAlarm:
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

// subscriberCount returns the number of active subscribers. Used by tests as a
// "wait until the handler has subscribed before publishing" barrier, and by
// Service.publishClientCount to report the advisory multi-tab count on the SSE
// stream. Report it (EventRigClients) but do NOT gate rig writes on it — that is
// the future operating-lock's job, not an anonymous count's.
func (h *hub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}
