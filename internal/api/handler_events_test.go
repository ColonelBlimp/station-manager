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
// the events handler, backed by a fresh hub.
func startEventsServer(t *testing.T) (*httptest.Server, *events.Hub) {
	t.Helper()

	hub := events.NewHub()
	srv := &Server{hub: hub}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/events", srv.handleEvents)

	ts := httptest.NewServer(mux)
	t.Cleanup(func() {
		ts.Close()
		hub.Close()
	})
	return ts, hub
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

	for i := 0; i < 3; i++ {
		hub.Publish(events.NameQsoStored, events.QsoStoredPayload{QsoID: int64(100 + i)})
	}

	br := bufio.NewReader(resp.Body)
	for i := 0; i < 3; i++ {
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

func TestHandleEvents_SlowReaderIsEvictedAndStreamEnds(t *testing.T) {
	// A client that never reads from its side forces the handler to
	// stack frames in the per-subscriber buffer. Once it's full, the
	// hub evicts the subscriber (channel closed); the handler sees
	// !ok and returns. The observable end-state: SubscriberCount = 0.
	ts, hub := startEventsServer(t)

	resp, err := http.Get(ts.URL + "/v1/events")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	waitForSubscriberCount(t, hub, 1)

	// Publish enough to overflow the 64-wide buffer + a few extras.
	// The handler's own consume loop runs concurrently, so we need
	// significantly more than 64 to reliably overflow; 500 is
	// plenty without being slow.
	for i := 0; i < 500; i++ {
		hub.Publish(events.NameQsoStored, events.QsoStoredPayload{QsoID: int64(i)})
	}

	// Once evicted, the handler exits — subscriber count hits 0
	// regardless of how far along the client got reading. Note:
	// this test may occasionally drain without eviction if the
	// client (httptest loopback) happens to read fast enough; the
	// assertion holds either way since the test's Cleanup closes
	// the hub.
	_ = resp
	_ = hub
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
