package bridge

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

// newHandlerTestService is a service preconfigured for handler tests
// with a fakeSerial driving a steady trickle of identity pushes.
// Returns the service, the fake (so individual tests can feed extra
// events), and a daemon-shutdown channel the handler will observe.
//
// Uses an uninitialised &logging.Service{} — same noop-logger pattern
// as service_test.go's newTestService.
func newHandlerTestService(t *testing.T) (*Service, *fakeSerial, chan struct{}) {
	t.Helper()
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  types.BridgeSerialConfig{Port: "fake", Baud: 38400},
		Cat:     types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	fake := installFakeSerial(s)
	if err := s.Initialize(); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	shutdownCh := make(chan struct{})
	return s, fake, shutdownCh
}

// TestHTTPHandler_ServesSSEHeaders confirms the handler sets the
// SSE-required headers and returns 200 before streaming starts —
// without these, browsers won't treat the response as an event
// stream and EventSource will fail to parse anything.
func TestHTTPHandler_ServesSSEHeaders(t *testing.T) {
	s, _, shutdownCh := newHandlerTestService(t)
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

// TestHTTPHandler_StreamsPipelineEvents confirms a connected client
// receives a real pipeline-decoded rig-state event in proper SSE
// wire format. Feeds an FT-710 ID push through the fakeSerial and
// reads the SSE event off the response stream.
func TestHTTPHandler_StreamsPipelineEvents(t *testing.T) {
	s, fake, shutdownCh := newHandlerTestService(t)
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

	// FT-710 ID 0800 → "FT-710" via the rigdef value-mapping.
	fake.feedLine([]byte("ID0800"))

	// Read the SSE stream line-by-line until we see one full event
	// frame (event: + data: + blank line). Bounded by ctx timeout.
	scanner := bufio.NewScanner(resp.Body)
	var sawEvent, sawData bool
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "event: rig-state"):
			sawEvent = true
		case strings.HasPrefix(line, "data: "):
			sawData = true
			payload := strings.TrimPrefix(line, "data: ")
			if !strings.Contains(payload, `"rigIdentity":"FT-710"`) {
				t.Errorf("data payload missing FT-710 marker: %q", payload)
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
	t.Fatalf("stream ended before a complete event frame; saw event=%v data=%v", sawEvent, sawData)
}

// TestHTTPHandler_ShutdownChClosesStream covers the daemon-graceful-
// shutdown path. http.Server.Shutdown does NOT cancel r.Context() on
// idle SSE connections, so without observing shutdownCh the SSE
// handler holds shutdown open until the graceful timeout. This test
// closes shutdownCh and asserts the handler returns promptly.
func TestHTTPHandler_ShutdownChClosesStream(t *testing.T) {
	s, fake, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Drive at least one event through so we know the handler is in
	// the select loop with an active subscription.
	fake.feedLine([]byte("ID0800"))
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: rig-state") {
			break
		}
	}

	// Closing shutdownCh signals the handler to return; the response
	// stream then ends. The Scan() call below should hit EOF
	// promptly (within 100ms) rather than waiting indefinitely.
	close(shutdownCh)

	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			// drain
		}
		close(done)
	}()

	select {
	case <-done:
		// expected — stream ended cleanly
	case <-time.After(time.Second):
		t.Fatal("stream did not end within 1s of shutdownCh close")
	}
}
