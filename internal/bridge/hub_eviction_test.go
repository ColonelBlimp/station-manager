package bridge

import (
	"testing"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// SLOW-READER EVICTION MUST LEAVE A TRACE — finding B2, the bridge third of a
// three-hub class fix (internal/events, internal/bridge, internal/ft8).
//
// CANONICAL CRITERION AND THE OPERATOR'S REASONING LIVE IN
// internal/events/hub_eviction_test.go. Read that before changing anything here.
// The short form: every hub dropped a too-slow subscriber with a bare
// close(ch); delete(...) and said nothing, so an ended stream could not be told
// apart from a client that left normally. The ruling was LOGGING ONLY — keep the
// teardown, keep the buffers (64 here) — and DO NOT tune the buffer until these
// records show healthy clients being evicted.
//
// The bridge hub carries no TX consequence, unlike internal/ft8's: an evicted rig
// SSE subscriber loses its display until it reconnects, which is a display fault,
// not a safety one. The TX-behaviour rules are therefore in the ft8 file only.
//
// These rules exist SEPARATELY from the events ones rather than being trusted to
// generalise, because the three hubs are three types with three hand-written
// emits. Asserting the same field names in each package is what stops them
// drifting — see logEvictions' comment.

const bridgeEvictionMsg = "subscriber evicted"

// evictHubSubscriber fills a subscriber's buffer and publishes one more.
func evictHubSubscriber(h *hub) {
	for i := 0; i < subscriberBufferSize; i++ {
		h.publish(Event{Name: EventRigState})
	}
	h.publish(Event{Name: EventBridgeError})
}

// H1 — an eviction is reported once, at Warn, with the full field set. One rule
// rather than three because the bridge half of this class fix is the same code as
// the events half; what needs pinning here is that THIS package's copy carries
// the same names.
func TestBridgeHubEviction_ReportedOnceWithFields(t *testing.T) {
	buf := &syncBuf{}
	h := newHub(logging.NewForWriter(buf))
	// Burn subscriber id 0 so the evicted one cannot be confused with a hard-coded
	// zero — see the subscriber_id assertion below.
	_, gone := h.subscribe()
	gone()
	_, _ = h.subscribe()

	evictHubSubscriber(h)

	got := matching(t, buf, bridgeEvictionMsg)
	if len(got) != 1 {
		t.Fatalf("eviction records = %d, want exactly 1", len(got))
	}
	rec := got[0]
	if lvl, _ := rec["level"].(string); lvl != "warn" {
		t.Errorf("level = %q, want warn", lvl)
	}
	for _, field := range []string{
		"subscriber_id", "event", "queue_depth", "queue_capacity",
		"subs_before", "subs_after",
	} {
		if _, ok := rec[field]; !ok {
			t.Errorf("record is missing %q — the field set must match the other two hubs, "+
				"or one log has to be read three different ways", field)
		}
	}
	// VALUES, not just presence. Logging zeros would satisfy a presence check while
	// telling the operator nothing — the same "presence instead of value" fixture
	// failure caught in the drive-watch work earlier the same day. queue_depth must
	// equal the capacity because a FULL buffer is precisely what an eviction means;
	// a depth below capacity would mean something else dropped the subscriber.
	// THE ID MUST BE THE EVICTED SUBSCRIBER'S, not merely a number. A constant 0
	// satisfied "non-negative" — so the fixture above burns id 0 on a throwaway
	// subscriber, making the real one id 1. An emitter that hard-coded, or that
	// reported the wrong subscriber, now fails.
	if id, _ := rec["subscriber_id"].(float64); id != 1 {
		t.Errorf("subscriber_id = %v, want 1 (id 0 went to the discarded subscriber) — "+
			"a record that cannot name WHICH stream ended does not answer the question "+
			"it exists for", rec["subscriber_id"])
	}
	depth, _ := rec["queue_depth"].(float64)
	capv, _ := rec["queue_capacity"].(float64)
	if capv != float64(subscriberBufferSize) {
		t.Errorf("queue_capacity = %v, want %d", capv, subscriberBufferSize)
	}
	if depth != capv {
		t.Errorf("queue_depth = %v, queue_capacity = %v — an eviction happens because the "+
			"buffer is FULL, so a depth below capacity means this record describes "+
			"something else", depth, capv)
	}
	before, _ := rec["subs_before"].(float64)
	after, _ := rec["subs_after"].(float64)
	if before != 1 || after != 0 {
		t.Errorf("subs_before/after = %v/%v, want 1/0 — this WAS the last subscriber",
			before, after)
	}
	if ev, _ := rec["event"].(string); ev != string(EventBridgeError) {
		t.Errorf("event = %q, want %q — the record must name the event that was actually "+
			"rejected", ev, EventBridgeError)
	}
}

// H2 — the states that RESEMBLE an eviction stay silent. All three end a
// subscriber's stream identically from the client's side, and only the first is a
// fault; a warning on the others would bury the real one.
func TestBridgeHubEviction_LookalikeStatesAreSilent(t *testing.T) {
	t.Run("full buffer without overflow", func(t *testing.T) {
		buf := &syncBuf{}
		h := newHub(logging.NewForWriter(buf))
		ch, _ := h.subscribe()
		for i := 0; i < subscriberBufferSize; i++ {
			h.publish(Event{Name: EventRigState})
		}
		if len(ch) != subscriberBufferSize {
			t.Fatalf("fixture: buffer holds %d, want exactly %d", len(ch), subscriberBufferSize)
		}
		if got := matching(t, buf, bridgeEvictionMsg); len(got) != 0 {
			t.Errorf("eviction records = %d on a full-but-not-overflowed buffer, want 0", len(got))
		}
	})

	t.Run("normal unsubscribe", func(t *testing.T) {
		buf := &syncBuf{}
		h := newHub(logging.NewForWriter(buf))
		_, unsub := h.subscribe()
		unsub()
		h.publish(Event{Name: EventRigState})
		if got := matching(t, buf, bridgeEvictionMsg); len(got) != 0 {
			t.Errorf("eviction records = %d on a normal unsubscribe, want 0", len(got))
		}
	})

	t.Run("hub close", func(t *testing.T) {
		buf := &syncBuf{}
		h := newHub(logging.NewForWriter(buf))
		_, _ = h.subscribe()
		_, _ = h.subscribe()
		h.close()
		if got := matching(t, buf, bridgeEvictionMsg); len(got) != 0 {
			t.Errorf("eviction records = %d on hub close, want 0 — shutdown disconnects "+
				"everyone by design", len(got))
		}
	})
}

// H3 — an evicted subscriber is warned about once, not on every later publish. A
// hub that kept reporting a departed subscriber would turn one slow client into
// an unbounded warning stream, which is worse than the silence it replaced.
func TestBridgeHubEviction_NotRepeatedOnLaterPublishes(t *testing.T) {
	buf := &syncBuf{}
	h := newHub(logging.NewForWriter(buf))
	_, _ = h.subscribe()

	evictHubSubscriber(h)
	for i := 0; i < 5; i++ {
		h.publish(Event{Name: EventRigState})
	}

	if got := matching(t, buf, bridgeEvictionMsg); len(got) != 1 {
		t.Errorf("eviction records = %d after 5 further publishes, want exactly 1", len(got))
	}
}

// H4 — a hub with no logger still evicts and does not panic. Several tests and
// the split-host build construct hubs without one.
func TestBridgeHubEviction_NoLoggerIsSafe(t *testing.T) {
	h := newHub(nil)
	_, _ = h.subscribe()
	evictHubSubscriber(h) // must not panic
	if n := h.subscriberCount(); n != 0 {
		t.Errorf("subscriber count = %d after eviction, want 0", n)
	}
}
