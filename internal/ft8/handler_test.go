package ft8

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

// newHandlerTestService builds an enabled FT8 Service for handler tests. It is
// not Started — tests publish reports directly onto the hub to simulate slots,
// which is all the SSE handler subscribes to.
func newHandlerTestService(t *testing.T) (*Service, chan struct{}) {
	t.Helper()
	s := newService(types.Ft8Config{Enabled: true}, logging.Noop(), newFakeSource())
	t.Cleanup(func() { _ = s.Stop() })
	return s, make(chan struct{})
}

// waitForOccSubscribers blocks until the hub reports at least n subscribers, so
// a test publishes only after the handler has registered (Do returns once
// headers flush, which is before Subscribe in the handler).
func waitForOccSubscribers(t *testing.T, s *Service, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.occHub.subscriberCount() >= n {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("hub did not reach %d subscribers within %s (have %d)", n, timeout, s.occHub.subscriberCount())
}

func TestHTTPHandler_ServesSSEHeaders(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", got)
	}
	if got := resp.Header.Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
}

// TestHTTPHandler_StreamsOccupancy confirms a connected client receives a
// published occupancy report in proper SSE wire format.
func TestHTTPHandler_StreamsOccupancy(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	waitForOccSubscribers(t, s, 1, time.Second)
	s.occHub.publish(OccupancyReport{
		Slot:          SlotRef{StartUTC: "2026-06-07T14:30:15Z", Period: "odd"},
		Passband:      Band{LowHz: 200, HighHz: 3000},
		SignalWidthHz: 50,
		Suggested:     []int{2200, 760},
	})

	scanner := bufio.NewScanner(resp.Body)
	var sawEvent, sawData bool
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: "+EventOccupancy):
			sawEvent = true
		case strings.HasPrefix(line, "data: "):
			sawData = true
			payload := strings.TrimPrefix(line, "data: ")
			if !strings.Contains(payload, `"signal_width_hz":50`) {
				t.Errorf("data payload missing expected field: %q", payload)
			}
			if !strings.Contains(payload, `"suggested":[2200,760]`) {
				t.Errorf("data payload missing suggested offsets: %q", payload)
			}
		case line == "":
			if sawEvent && sawData {
				return
			}
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scanner: %v", err)
	}
	t.Fatal("did not receive a complete ft8-occupancy SSE frame")
}

// TestHTTPHandler_ReplaysLatestToLateSubscriber confirms a tab connecting after
// a slot was processed gets the cached report immediately (the one-slot replay
// cache), not a 15 s wait.
func TestHTTPHandler_ReplaysLatestToLateSubscriber(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	s.occHub.publish(OccupancyReport{SignalWidthHz: 50, Suggested: []int{1234}})

	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), `"suggested":[1234]`) {
			return
		}
	}
	t.Fatal("late subscriber did not receive the replayed cached report")
}

// TestHTTPHandler_ShutdownChClosesStream confirms closing the daemon shutdown
// channel ends the stream promptly even with no client cancel.
func TestHTTPHandler_ShutdownChClosesStream(t *testing.T) {
	s, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	waitForOccSubscribers(t, s, 1, time.Second)
	close(shutdownCh)

	done := make(chan struct{})
	go func() {
		buf := make([]byte, 256)
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
		t.Fatal("stream did not end after shutdownCh closed")
	}
}
