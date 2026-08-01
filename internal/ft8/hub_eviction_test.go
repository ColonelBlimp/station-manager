package ft8

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// SLOW-READER EVICTION MUST LEAVE A TRACE — finding #1, and the package where it
// matters most. Third of a three-hub class fix (internal/events,
// internal/bridge, internal/ft8).
//
// CANONICAL CRITERION AND THE OPERATOR'S REASONING LIVE IN
// internal/events/hub_eviction_test.go. Read that first.
//
// WHY THIS PACKAGE IS DIFFERENT. Eviction closes the subscriber's channel; the
// SSE handler treats that as "stream ended" and runs its unsubscribe; if it was
// the last subscriber, the linger expires and onLingerExpired calls disarmTx —
// dropping PTT and ABANDONING AN ACTIVE QSO. So a browser that merely stalls can
// end a contact, and before this change the log could not tell that from the
// operator closing the tab.
//
// THE TEARDOWN STAYS — operator's ruling, 2026-08-01, and it is the part most
// likely to be second-guessed later, so the reasoning is recorded here as well as
// in the events file:
//
//	The enforced proxy is a FUNCTIONING SSE subscription, not merely an open
//	browser tab. Once the channel overflows, operator-facing state is no longer
//	flowing. EventSource reconnect plus the existing linger IS the recovery
//	distinction — reconnect within the linger cancels the teardown; no reconnect
//	means the only presence signal remains absent, so disarming is correct.
//	Exempting eviction could leave TX running behind a dead display, or create a
//	phantom subscriber that can never later unsubscribe.
//
// F3, F4 and F5 below pin that, because a future reader who finds the eviction log
// and decides to "fix" the disarm needs a test that fails. F3 is the teardown
// (driven through the REAL HTTPHandler — an earlier version called unsub() by hand
// and would have stayed green if the handler's `defer unsub()` were deleted); F4
// is the ordinary reconnect; F5 is the reconnect that raced an already-fired
// timer, which F4 structurally cannot reach.

const ft8EvictionMsg = "subscriber evicted"

// ft8LogBuf is a mutex-guarded buffer: the decode loop and timers write while the
// test reads.
type ft8LogBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *ft8LogBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *ft8LogBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func ft8EvictionRecords(t *testing.T, buf *ft8LogBuf) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("log line is not JSON: %q (%v)", line, err)
		}
		if msg, _ := rec["message"].(string); strings.Contains(msg, ft8EvictionMsg) {
			out = append(out, rec)
		}
	}
	return out
}

// overflowFt8Hub fills a subscriber's buffer and publishes one more.
func overflowFt8Hub(h *hub) {
	for i := 0; i < subscriberBufferSize; i++ {
		h.publish(hubEvent{name: EventDecode})
	}
	h.publish(hubEvent{name: EventQso})
}

// F1 — an eviction is reported once, at Warn, with the same field set the other
// two hubs use. Asserted here rather than trusted to generalise: three hubs, three
// hand-written emits, and a rename in one would otherwise go unnoticed.
func TestFt8HubEviction_ReportedOnceWithFields(t *testing.T) {
	buf := &ft8LogBuf{}
	h := newHub(logging.NewForWriter(buf))
	// Burn subscriber id 0 so the evicted one cannot be confused with a hard-coded
	// zero — see the subscriber_id assertion below.
	_, gone := h.subscribe()
	gone()
	_, _ = h.subscribe()

	overflowFt8Hub(h)

	got := ft8EvictionRecords(t, buf)
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
			t.Errorf("record is missing %q — the field set must match the other two hubs", field)
		}
	}
	if ev, _ := rec["event"].(string); ev != EventQso {
		t.Errorf("event = %q, want %q — the record must name the rejected event", ev, EventQso)
	}
	// VALUES, not just presence. Logging zeros would satisfy a presence check while
	// telling the operator nothing — the same "presence instead of value" fixture
	// failure caught in the drive-watch work earlier the same day. queue_depth must
	// equal the capacity because a FULL buffer is precisely what an eviction means;
	// a depth below capacity would mean something else dropped the subscriber.
	// THE ID MUST BE THE EVICTED SUBSCRIBER'S, not merely a number. A constant 0
	// satisfied "non-negative" — so the fixture below burns id 0 on a throwaway
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
	// The last-subscriber case is the one with a TX consequence, so the counts that
	// reveal it must be right.
	before, _ := rec["subs_before"].(float64)
	after, _ := rec["subs_after"].(float64)
	if before != 1 || after != 0 {
		t.Errorf("subs_before/after = %v/%v, want 1/0", before, after)
	}
}

// F2 — the lookalike states stay silent. Same three as the other hubs; all end a
// stream identically from the client's side and only one is a fault.
func TestFt8HubEviction_LookalikeStatesAreSilent(t *testing.T) {
	t.Run("full buffer without overflow", func(t *testing.T) {
		buf := &ft8LogBuf{}
		h := newHub(logging.NewForWriter(buf))
		ch, _ := h.subscribe()
		for i := 0; i < subscriberBufferSize; i++ {
			h.publish(hubEvent{name: EventDecode})
		}
		if len(ch) != subscriberBufferSize {
			t.Fatalf("fixture: buffer holds %d, want exactly %d", len(ch), subscriberBufferSize)
		}
		if got := ft8EvictionRecords(t, buf); len(got) != 0 {
			t.Errorf("eviction records = %d on a full-but-not-overflowed buffer, want 0", len(got))
		}
	})

	t.Run("normal unsubscribe", func(t *testing.T) {
		buf := &ft8LogBuf{}
		h := newHub(logging.NewForWriter(buf))
		_, unsub := h.subscribe()
		unsub()
		h.publish(hubEvent{name: EventDecode})
		if got := ft8EvictionRecords(t, buf); len(got) != 0 {
			t.Errorf("eviction records = %d on a normal unsubscribe, want 0", len(got))
		}
	})

	t.Run("hub close", func(t *testing.T) {
		buf := &ft8LogBuf{}
		h := newHub(logging.NewForWriter(buf))
		_, _ = h.subscribe()
		h.close()
		if got := ft8EvictionRecords(t, buf); len(got) != 0 {
			t.Errorf("eviction records = %d on hub close, want 0", len(got))
		}
	})
}

// stalledSSEClient is a ResponseWriter that can be made to block mid-write, which
// is what a slow SSE client actually is: the handler stalls inside writeEvent, so
// it stops draining its channel and the hub's buffer fills behind it.
//
// Headers and the opening flush must NOT block or the handler never reaches
// Subscribe, so blocking is armed by the test after the subscription exists.
type stalledSSEClient struct {
	hdr  http.Header
	mu   sync.Mutex
	gate chan struct{} // non-nil once armed; Write blocks on it
	// blocked is closed the FIRST time a Write actually parks on the gate. A
	// cumulative write COUNTER cannot serve: the hub replays cached events to a new
	// subscriber, so the handler has usually written before the test arms the stall,
	// and "writes >= 1" is then already true while nothing is blocked (review,
	// 2026-08-01). This signals the state the test needs, not a proxy for it.
	blocked  chan struct{}
	didBlock bool
}

func newStalledSSEClient() *stalledSSEClient {
	return &stalledSSEClient{hdr: http.Header{}}
}
func (c *stalledSSEClient) Header() http.Header { return c.hdr }
func (c *stalledSSEClient) WriteHeader(int)     {}
func (c *stalledSSEClient) Flush()              {}
func (c *stalledSSEClient) Write(p []byte) (int, error) {
	c.mu.Lock()
	g := c.gate
	if g != nil && !c.didBlock {
		c.didBlock = true
		close(c.blocked) // announce it BEFORE parking, or nobody hears it
	}
	c.mu.Unlock()
	if g != nil {
		<-g // stall here until the test releases
	}
	return len(p), nil
}

// stall arms the gate and returns a channel closed once a Write is parked on it.
func (c *stalledSSEClient) stall() <-chan struct{} {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gate = make(chan struct{})
	c.blocked = make(chan struct{})
	c.didBlock = false
	return c.blocked
}
func (c *stalledSSEClient) release() {
	c.mu.Lock()
	g := c.gate
	c.gate = nil
	c.mu.Unlock()
	if g != nil {
		close(g)
	}
}

// F3 — THE TEARDOWN IS UNCHANGED: an evicted LAST subscriber still disarms TX
// after the linger. The operator's explicit ruling, and the rule that must fail if
// someone later decides eviction should be exempt. TX running behind a dead
// display is the outcome being refused.
//
// DRIVEN THROUGH THE REAL HTTPHandler, not by calling unsub() by hand. An earlier
// version did the latter and it was a hollow rule: the handler's `defer unsub()`
// (handler.go:113) is the ONLY thing that decrements subCount on an eviction — the
// hub knows nothing about the Service's refcount — so deleting that one line would
// have left the hand-driven test green while TX stayed armed forever behind a dead
// stream. Here the handler's own exit does the work.
//
// The client stalls mid-write, which is what a slow reader IS. That is also why
// the buffer can fill at all: a handler that is draining its channel cannot be
// overflowed.
func TestFt8HubEviction_EvictedLastSubscriberStillDisarmsTx(t *testing.T) {
	withShortLinger(t, 10*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	s.newPlayer = func(string, int) (txPlayer, error) { return newFakeTxPlayer(), nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	client := newStalledSSEClient()
	done := make(chan struct{})
	req := httptest.NewRequest(http.MethodGet, "/v1/ft8/events", nil)
	go func() {
		defer close(done)
		s.HTTPHandler(make(chan struct{})).ServeHTTP(client, req)
	}()

	// The real handler has subscribed and capture is up.
	require.Eventually(t, func() bool { return src.startCount() == 1 }, 2*time.Second, 5*time.Millisecond,
		"fixture: the SSE handler must have subscribed, or nothing below is exercised")
	require.NoError(t, s.ArmTx(true))
	armed := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txArmed }
	require.True(t, armed(), "fixture: TX must be armed, or this rule proves nothing")

	// BARRIER, and it is load-bearing. Publishing straight after stall() races the
	// handler: if it had already taken one event off the channel, the overflow burst
	// leaves the buffer exactly FULL rather than overflowing, no eviction happens,
	// and the rule fails on scheduler timing rather than on the behaviour it names.
	// So: stall, feed ONE event, and wait until the handler is provably PARKED inside
	// Write before filling the buffer behind it.
	//
	// The wait is on a signal raised by Write itself when it reaches the gate — not
	// on a write count. The hub replays cached events to a new subscriber, so a
	// counter is usually already non-zero when the stall is armed, and waiting on it
	// would return immediately with the handler still draining.
	blocked := client.stall()
	s.hub.publish(hubEvent{name: EventDecode})
	select {
	case <-blocked:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture: no Write ever parked on the gate, so the handler is still " +
			"draining and the overflow below would race it")
	}

	overflowFt8Hub(s.hub) // the buffer fills behind the stalled handler, and overflows
	// The eviction must ACTUALLY have happened, or the handler exits for some other
	// reason and this rule passes without exercising the path it names.
	require.Zero(t, s.hub.subscriberCount(),
		"fixture: the hub must have evicted the stalled subscriber")

	client.release() // the stalled write completes; the handler loops
	select {         // ...and returns on the closed channel, running defer unsub()
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the SSE handler did not return after its subscriber was evicted — " +
			"an unbounded wait here would hang the suite instead of reporting this")
	}

	require.Eventually(t, func() bool { return !armed() }, 2*time.Second, 5*time.Millisecond,
		"an evicted last subscriber must still disarm TX past the linger — the enforced "+
			"proxy is a FUNCTIONING subscription, not an open tab")
}

// F5 — THE RECONNECT-RACED-THE-TIMER GUARD. service.go:394 documents it: "the
// subCount re-check makes it robust against a reconnect that raced the timer
// (onSubscriberAdded stops the timer, but if it HAD ALREADY FIRED we still see the
// new subscriber here and keep the session)."
//
// F4 cannot reach that interleaving — its reconnect is immediate, so Timer.Stop
// always wins and the guard is never consulted. This rule enters onLingerExpired
// DIRECTLY with a subscriber already present, which is exactly the state the
// callback finds when it fired first and the reconnect landed before it took the
// lock. Constructed rather than raced, following the precedent set for the
// sealed-window case in internal/bridge.
//
// Without the guard, an operator whose browser reconnected in that window would
// have TX disarmed underneath them for no reason.
func TestFt8HubEviction_ReconnectRacingTheFiredTimerKeepsTx(t *testing.T) {
	withShortLinger(t, time.Hour) // the timer must not fire on its own here
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	s.newPlayer = func(string, int) (txPlayer, error) { return newFakeTxPlayer(), nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	t.Cleanup(unsub)
	require.Eventually(t, func() bool { return src.startCount() == 1 }, time.Second, 5*time.Millisecond)
	require.NoError(t, s.ArmTx(true))
	armed := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txArmed }
	require.True(t, armed(), "fixture: TX must be armed")

	// The subscriber above IS the reconnect that landed while the callback was in
	// flight. Entering the callback now models the timer having already fired.
	s.onLingerExpired()

	require.True(t, armed(),
		"onLingerExpired must not disarm when a subscriber is present — the timer fired "+
			"but the operator came back before it looked, and Timer.Stop cannot help once "+
			"the callback is already running")
}

// F4 — ...AND A REAL RECONNECT INSIDE THE LINGER STILL PREVENTS IT. The other half,
// and the reason F3 is not simply "eviction kills TX": EventSource reconnects, and
// a client that comes back within the window is attending again.
//
// Without this rule, an implementation that disarmed immediately on eviction —
// skipping the linger entirely — would satisfy F3 while breaking every ordinary
// browser reload.
//
// ITS PROOF NEEDS A DOUBLE REVERT, and that is a property of the code, not a
// weakness in the rule — but read the correction below before drawing conclusions
// from it. TWO mechanisms keep a reconnect from being torn down: onSubscriberAdded
// stops the pending timer, and onLingerExpired re-checks subCount before
// disarming. Removing either ALONE leaves THIS test green (measured — both single
// reverts failed to challenge it); only removing both turns it red.
//
// CORRECTION, after review: that does NOT mean the two are interchangeable, and an
// earlier version of this comment implied they were. They cover DIFFERENT
// interleavings, and F4 can only ever reach the first. Its reconnect is immediate,
// so Timer.Stop always wins and the subCount guard is never consulted. The guard
// exists for the case where the timer had ALREADY FIRED before the reconnect
// landed — service.go:394 says so — which Stop cannot help with, because the
// callback is already running. **F5 covers that case and proves the guard on its
// own.** So: do not read a green F4 after touching the guard as evidence the guard
// is dead code; run F5.
func TestFt8HubEviction_ReconnectInsideLingerPreventsDisarm(t *testing.T) {
	withShortLinger(t, 250*time.Millisecond)
	src := newFakeSource()
	s := newService(types.Ft8Config{Enabled: true, TX: &types.Ft8TXConfig{}}, logging.Noop(), src)
	s.newPlayer = func(string, int) (txPlayer, error) { return newFakeTxPlayer(), nil }
	s.SetTxKeyer(&fakeKeyer{})
	require.NoError(t, s.Initialize())
	require.NoError(t, s.Start(context.Background()))
	t.Cleanup(func() { _ = s.Stop() })

	_, unsub := s.Subscribe()
	require.Eventually(t, func() bool { return src.startCount() == 1 }, time.Second, 5*time.Millisecond)
	require.NoError(t, s.ArmTx(true))
	armed := func() bool { s.txMu.Lock(); defer s.txMu.Unlock(); return s.txArmed }
	require.True(t, armed(), "fixture: TX must be armed")

	overflowFt8Hub(s.hub)
	unsub()

	// The browser reconnects well inside the window.
	_, unsub2 := s.Subscribe()
	t.Cleanup(unsub2)

	// Past the point the teardown would have fired had it not been cancelled.
	time.Sleep(3 * 250 * time.Millisecond / 2)
	require.True(t, armed(),
		"a reconnect inside the linger must cancel the teardown — this is the ordinary "+
			"browser reload, and disarming through it would make FT8 unusable")
}
