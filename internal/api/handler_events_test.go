package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/events"
)

// readOneSSEFrame reads lines until it finds a blank line, returning
// the accumulated frame's parsed id/event/data fields. Keep-alive
// comment lines (starting with `:`) are ignored; they don't terminate
// a frame. Returns ok=false if the stream ends before a full frame
// lands.
func readOneSSEFrame(t *testing.T, br *bufio.Reader) (id, name, data string, ok bool) {
	t.Helper()
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", "", "", false
		}
		line = strings.TrimRight(line, "\n")
		if line == "" {
			if id == "" && name == "" && data == "" {
				continue
			}
			return id, name, data, true
		}
		switch {
		case strings.HasPrefix(line, "id: "):
			id = strings.TrimPrefix(line, "id: ")
		case strings.HasPrefix(line, "event: "):
			name = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
	}
}

// startEventsServer spins up an httptest.Server whose only route is
// the events handler, backed by a fresh hub. shutdownCh is initialized
// so the handler's `<-s.shutdownCh` case is selectable; tests that
// want to drive shutdown call closeShutdownCh.
func startEventsServer(t *testing.T) (*httptest.Server, *events.Hub) {
	t.Helper()

	hub := events.NewHub()
	srv := &Server{hub: hub, shutdownCh: make(chan struct{})}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events", srv.handleEvents)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
		hub.Close()
	})
	return ts, hub
}

// startEventsServerWithSrv is the same as startEventsServer but also
// returns the Server so tests can close shutdownCh directly. Used to
// verify the H3 fix: shutdownCh closure unblocks idle SSE handlers
// promptly, without waiting on r.Context() (which only fires on
// connection close, not on http.Server.Shutdown).
func startEventsServerWithSrv(t *testing.T) (*httptest.Server, *events.Hub, *Server) {
	t.Helper()

	hub := events.NewHub()
	srv := &Server{hub: hub, shutdownCh: make(chan struct{})}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events", srv.handleEvents)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
		hub.Close()
	})
	return ts, hub, srv
}

// waitForSubscriberCount polls until hub.SubscriberCount() == want or
// the deadline elapses. Used to rendezvous with the handler goroutine
// before publishing (avoiding "publish before handler subscribed"
// flakes) and after disconnect (to prove unsub ran).
func waitForSubscriberCount(t *testing.T, hub *events.Hub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.SubscriberCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("subscriber count did not reach %d within 2s (final: %d)", want, hub.SubscriberCount())
}

func TestHandleEvents_DeliversPublishedEvent(t *testing.T) {
	ts, hub := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}

	waitForSubscriberCount(t, hub, 1)

	hub.Publish(events.NameQsoStored, events.QsoStoredPayload{QsoID: 42, LogbookID: 7})

	br := bufio.NewReader(resp.Body)
	id, name, data, ok := readOneSSEFrame(t, br)
	if !ok {
		t.Fatal("did not receive a frame")
	}
	if id != "1" {
		t.Errorf("id = %q, want 1", id)
	}
	if name != events.NameQsoStored {
		t.Errorf("event = %q, want %q", name, events.NameQsoStored)
	}

	var payload events.QsoStoredPayload
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("payload unmarshal: %v, raw=%q", err, data)
	}
	if payload.QsoID != 42 || payload.LogbookID != 7 {
		t.Errorf("payload = %+v, want {42, 7}", payload)
	}
}

func TestHandleEvents_ForwardSucceededShape(t *testing.T) {
	ts, hub := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)
	hub.Publish(events.NameForwardSucceeded, events.ForwardSucceededPayload{
		QsoID: 1, ForwarderName: "qrz", Action: "insert", UpstreamID: "abc123", Attempts: 2,
	})

	br := bufio.NewReader(resp.Body)
	_, name, data, ok := readOneSSEFrame(t, br)
	if !ok {
		t.Fatal("no frame")
	}
	if name != events.NameForwardSucceeded {
		t.Errorf("event = %q", name)
	}

	var p events.ForwardSucceededPayload
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		t.Fatalf("unmarshal: %v, raw=%q", err, data)
	}
	if p.ForwarderName != "qrz" || p.UpstreamID != "abc123" || p.Attempts != 2 {
		t.Errorf("payload = %+v", p)
	}
}

func TestHandleEvents_MultipleEventsInOrder(t *testing.T) {
	ts, hub := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)

	for i := range 3 {
		hub.Publish(events.NameQsoStored, events.QsoStoredPayload{QsoID: int64(100 + i)})
	}

	br := bufio.NewReader(resp.Body)
	for i := range 3 {
		id, name, _, ok := readOneSSEFrame(t, br)
		if !ok {
			t.Fatalf("frame %d missing", i)
		}
		if name != events.NameQsoStored {
			t.Errorf("frame %d name = %q", i, name)
		}
		want := []string{"1", "2", "3"}[i]
		if id != want {
			t.Errorf("frame %d id = %q, want %q", i, id, want)
		}
	}
}

func TestHandleEvents_ClientDisconnectUnsubscribes(t *testing.T) {
	ts, hub := startEventsServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/v1/events", nil)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)

	cancel()

	// After client disconnect, handler sees r.Context().Done() and
	// returns, which triggers the deferred unsubscribe.
	waitForSubscriberCount(t, hub, 0)
}

func TestHandleEvents_HubCloseEndsStream(t *testing.T) {
	ts, hub := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)

	hub.Close()

	// Handler sees channel-closed, exits; subscriber count drops to 0.
	waitForSubscriberCount(t, hub, 0)

	// Response body should reach EOF shortly.
	buf := make([]byte, 1024)
	done := make(chan struct{})
	go func() {
		for {
			if _, err := resp.Body.Read(buf); err != nil {
				close(done)
				return
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end after hub.Close()")
	}
}

// TestHandleEvents_ShutdownChClosureEndsStream covers the H3 fix:
// closing srv.shutdownCh must unblock idle SSE handlers without
// waiting for the underlying connection to close. Without this case
// in the handler's select, http.Server.Shutdown would block on idle
// SSE subscribers until ctx expired (forcing every shutdown to pay
// the full graceful-shutdown timeout).
func TestHandleEvents_ShutdownChClosureEndsStream(t *testing.T) {
	ts, hub, srv := startEventsServerWithSrv(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)

	close(srv.shutdownCh)

	// Handler sees shutdownCh closed, returns, deferred unsub runs,
	// subscriber count drops to 0 promptly (no connection close required).
	waitForSubscriberCount(t, hub, 0)
}

// probeWriter is a minimal http.ResponseWriter + Flusher used to drive
// handleEvents directly (no loopback), so eviction/subscribe timing is
// deterministic. It records the hub subscriber count at the first Flush and can
// optionally block that first Flush on `release` (to park the handler before it
// consumes, forcing a buffer overflow → eviction). SetWriteDeadline is a no-op
// so http.NewResponseController succeeds and the handler never hits the
// nil-logger path. All fields are touched only by the handler goroutine until
// firstFlush closes, so no locking is needed.
type probeWriter struct {
	hub         *events.Hub
	hdr         http.Header
	flushed     bool
	subsAtFlush int
	firstFlush  chan struct{} // closed on the first Flush
	release     chan struct{} // if non-nil, the first Flush blocks until closed
}

func (w *probeWriter) Header() http.Header {
	if w.hdr == nil {
		w.hdr = http.Header{}
	}
	return w.hdr
}
func (w *probeWriter) Write(b []byte) (int, error)        { return len(b), nil }
func (w *probeWriter) WriteHeader(int)                    {}
func (w *probeWriter) SetWriteDeadline(_ time.Time) error { return nil }
func (w *probeWriter) Flush() {
	if w.flushed {
		return
	}
	w.flushed = true
	if w.hub != nil {
		w.subsAtFlush = w.hub.SubscriberCount()
	}
	if w.firstFlush != nil {
		close(w.firstFlush)
	}
	if w.release != nil {
		<-w.release
	}
}

// TestHandleEvents_SubscribesBeforeStreamObservable is the M1 regression: the
// hub subscription must exist BEFORE the client can observe the open stream
// (the first Flush), or an event published in that gap is lost (no replay).
func TestHandleEvents_SubscribesBeforeStreamObservable(t *testing.T) {
	hub := events.NewHub()
	defer hub.Close()
	srv := &Server{hub: hub, shutdownCh: make(chan struct{})}

	w := &probeWriter{hub: hub, firstFlush: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() { srv.handleEvents(w, req); close(done) }()

	select {
	case <-w.firstFlush:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never flushed the open response")
	}
	if w.subsAtFlush != 1 {
		t.Errorf("subscriber count at first flush = %d, want 1 (subscription must precede the observable open)", w.subsAtFlush)
	}
	cancel()
	<-done
}

func TestHandleEvents_SlowReaderIsEvictedAndStreamEnds(t *testing.T) {
	// Deterministic eviction: park the handler in its open-stream Flush (before
	// it consumes anything), overflow the per-subscriber buffer so the hub
	// evicts (closes the channel), then release the Flush — the handler loops,
	// sees the closed channel, returns, and the deferred unsub runs. Observable
	// end-state: SubscriberCount == 0. (The httptest-loopback version of this
	// could drain without evicting and asserted nothing — review 2026-06-19 L1.)
	hub := events.NewHub()
	defer hub.Close()
	srv := &Server{hub: hub, shutdownCh: make(chan struct{})}

	w := &probeWriter{firstFlush: make(chan struct{}), release: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)

	done := make(chan struct{})
	go func() { srv.handleEvents(w, req); close(done) }()

	<-w.firstFlush // handler has subscribed and is parked in the open Flush
	waitForSubscriberCount(t, hub, 1)

	// Overflow the buffer while the handler isn't consuming → hub evicts.
	for i := range 500 {
		hub.Publish(events.NameQsoStored, events.QsoStoredPayload{QsoID: int64(i)})
	}
	close(w.release) // let the open Flush return → handler sees the closed channel

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not exit after the subscriber was evicted")
	}
	waitForSubscriberCount(t, hub, 0) // deferred unsub ran
}

func TestHandleEvents_Keepalive(t *testing.T) {
	orig := sseKeepAliveInterval
	sseKeepAliveInterval = 50 * time.Millisecond
	t.Cleanup(func() { sseKeepAliveInterval = orig })

	ts, _ := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)

	deadline := time.Now().Add(1 * time.Second)
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if strings.HasPrefix(line, ":") {
			return
		}
	}
	t.Fatal("no keep-alive line within 1s")
}
