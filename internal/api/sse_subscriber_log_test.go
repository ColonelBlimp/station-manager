package api

// H3 (docs/reviews/internal-codebase-logging-gaps.md) — a currently-connected SSE client was
// invisible at Info: the access log records a stream only at its END (disconnect). The shared
// admission layer (limitEventSubscribers, covering /v1/events, /v1/rig/events, /v1/ft8/events)
// now emits a lightweight Info transition on every successful acquire and release. Operator
// rulings 2026-08-16: a record per successful acquire + release; fields event=connected|
// disconnected, path (request path), client (normalized IP), subscribers (new global count);
// Info level; rejected clients get no connected transition; the end-of-stream access record is
// unchanged.

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
)

const (
	msgSSEConnected    = "sse: subscriber connected"
	msgSSEDisconnected = "sse: subscriber disconnected"
)

// Unit (H3 + review fix): the transition callback receives the running global count and is
// invoked WHILE subscribersMu is held, so the count mutation and its log are atomic — under
// concurrent connects the records cannot be emitted out of count order. Over the cap, acquire
// fails with a nil release and no callback.
func TestAcquireSubscriber_CallbackCountsUnderLock(t *testing.T) {
	l := newLoadLimiter(1, 2, 1, 1) // maxSubscribers = 2

	var connects, disconnects []int
	onEvt := func(connected bool, n int) {
		// The lock MUST be held here — TryLock succeeding would mean the count/log pair is
		// not atomic (the review's out-of-order hazard).
		if l.subscribersMu.TryLock() {
			l.subscribersMu.Unlock()
			t.Error("transition callback ran without subscribersMu held — count/log not atomic")
		}
		if connected {
			connects = append(connects, n)
		} else {
			disconnects = append(disconnects, n)
		}
	}

	ok1, rel1 := l.acquireSubscriber(onEvt)
	if !ok1 {
		t.Fatal("acquire 1 not ok")
	}
	ok2, rel2 := l.acquireSubscriber(onEvt)
	if !ok2 {
		t.Fatal("acquire 2 not ok")
	}
	ok3, rel3 := l.acquireSubscriber(onEvt)
	if ok3 || rel3 != nil {
		t.Fatalf("acquire over cap = (%v, rel!=nil %v), want (false, nil)", ok3, rel3 != nil)
	}
	rel2()
	rel1()

	if !reflect.DeepEqual(connects, []int{1, 2}) {
		t.Errorf("connect counts = %v, want [1 2]", connects)
	}
	if !reflect.DeepEqual(disconnects, []int{1, 0}) {
		t.Errorf("disconnect counts = %v, want [1 0]", disconnects)
	}
}

// Review fix: the log callback runs under subscribersMu, so its unlock MUST be panic-safe
// (deferred). If a panicking log write left the mutex locked, every later SSE connect/disconnect
// across all three endpoints would deadlock. After a panicking callback, a fresh acquire must
// still complete promptly.
func TestAcquireSubscriber_CallbackPanic_ReleasesLock(t *testing.T) {
	l := newLoadLimiter(1, 2, 1, 1)

	func() {
		defer func() { _ = recover() }() // swallow the callback panic
		l.acquireSubscriber(func(bool, int) { panic("log write blew up") })
	}()

	done := make(chan struct{})
	go func() {
		l.acquireSubscriber(func(bool, int) {}) // must not block on a leaked lock
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("subscribersMu left locked after a panicking callback — SSE admission deadlocked")
	}
}

// C1 + C2: a successful SSE admission logs a connected transition IMMEDIATELY (visible while
// the client is still connected, not only at disconnect), and a disconnected transition when
// the slot releases — each carrying event/path/client/subscribers at Info.
func TestSSESubscriberTransitions_LoggedOnConnectAndDisconnect(t *testing.T) {
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	srv.limits = newLoadLimiter(128, 4, 1000, 1000)

	var connectedWhileConnected bool
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// C1's whole point: the connected record already exists mid-stream.
		connectedWhileConnected = len(allMessages(t, buf, msgSSEConnected)) == 1
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitEventSubscribers(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/rig/events", nil)
	req.RemoteAddr = "10.1.2.3:5555"
	wrapped.ServeHTTP(httptest.NewRecorder(), req)

	if !connectedWhileConnected {
		t.Errorf("connected transition was not logged while the client was connected (only at end)\n%s", buf.String())
	}

	conn := allMessages(t, buf, msgSSEConnected)
	if len(conn) != 1 {
		t.Fatalf("connected records = %d, want 1\n%s", len(conn), buf.String())
	}
	c := conn[0]
	if c["level"] != "info" {
		t.Errorf("connected level = %v, want info", c["level"])
	}
	if c["event"] != "connected" {
		t.Errorf("event = %v, want connected", c["event"])
	}
	if c["path"] != "/v1/rig/events" {
		t.Errorf("path = %v, want /v1/rig/events", c["path"])
	}
	if c["client"] != "10.1.2.3" {
		t.Errorf("client = %v, want 10.1.2.3 (normalized IP)", c["client"])
	}
	if s, _ := c["subscribers"].(float64); int(s) != 1 {
		t.Errorf("subscribers = %v, want 1", c["subscribers"])
	}

	disc := allMessages(t, buf, msgSSEDisconnected)
	if len(disc) != 1 {
		t.Fatalf("disconnected records = %d, want 1\n%s", len(disc), buf.String())
	}
	d := disc[0]
	if d["event"] != "disconnected" {
		t.Errorf("event = %v, want disconnected", d["event"])
	}
	if s, _ := d["subscribers"].(float64); int(s) != 0 {
		t.Errorf("disconnected subscribers = %v, want 0 (last one left)", d["subscribers"])
	}
}

// Ruling: a rejected client (admission cap reached) gets NO connected transition.
func TestSSESubscriberTransitions_RejectedClientNotLogged(t *testing.T) {
	buf := &bytes.Buffer{}
	srv := testServerWithLogger(t, nil, nil, logging.NewForWriter(buf))
	srv.limits = newLoadLimiter(128, 1, 1000, 1000) // one slot only

	block := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		<-block // hold the single slot
		w.WriteHeader(http.StatusOK)
	})
	wrapped := srv.limitEventSubscribers(handler)

	done := make(chan struct{})
	go func() {
		defer close(done)
		req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
		wrapped.ServeHTTP(httptest.NewRecorder(), req)
	}()
	// wait for the slot to be taken
	waitForSubscribers(t, srv, 1)

	// second client is rejected (cap = 1)
	rw := httptest.NewRecorder()
	wrapped.ServeHTTP(rw, httptest.NewRequest(http.MethodGet, "/v1/events", nil))
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("rejected client status = %d, want 503", rw.Code)
	}

	close(block)
	<-done

	// exactly one connected (the admitted one), never two.
	if n := len(allMessages(t, buf, msgSSEConnected)); n != 1 {
		t.Errorf("connected records = %d, want 1 — a rejected client must not log a connected transition\n%s", n, buf.String())
	}
}

func waitForSubscribers(t *testing.T, srv *Server, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		srv.limits.subscribersMu.Lock()
		got := srv.limits.subscribers
		srv.limits.subscribersMu.Unlock()
		if got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("subscribers never reached %d", want)
}
