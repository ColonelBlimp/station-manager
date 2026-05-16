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
		Serial:  types.BridgeSerialConfig{Port: "fake"},
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

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Wait for the handler to subscribe + bootstrap before feeding.
	// Do() returns as soon as response headers are flushed (which
	// happens BEFORE Subscribe in the handler), so feedLine could
	// otherwise race the registration and publish to zero
	// subscribers. The bootstrap READ write landing in fake.writes
	// (writes[0]=INIT from pipeline startup, writes[1]=READ from
	// bootstrap) proves Subscribe completed.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(fake.recordedWrites()) < 2 {
		t.Fatal("handler did not subscribe + bootstrap within 1s")
	}

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

	// Wait for the handler to subscribe + bootstrap before feeding —
	// Do() returns as soon as headers flush, which precedes Subscribe
	// in the handler. Without this gate, feedLine can race the
	// subscription and publish to zero subscribers (then the scanner
	// below hangs forever waiting for a rig-state that already came
	// and went).
	//
	// Write log shape post-2026-05-16:
	//   writes[0] = INIT          (runPipeline startup)
	//   writes[1] = post-INIT READ (runPipeline — fresh snapshot every cycle)
	//   writes[2] = bootstrap READ (TriggerBootstrap, fires AFTER Subscribe)
	//
	// Waiting for writes >= 3 proves both Subscribe + bootstrap have
	// completed. (Pre-2026-05-16 this was writes >= 2 because
	// runPipeline only wrote INIT at startup and the bootstrap READ
	// was the second write; the post-INIT READ added in the
	// supervisor work shifted the count.)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 3 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

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

// TestHTTPHandler_BootstrapFiresOnSubscribe covers the M3a.3
// bootstrap-on-SSE-open contract from the SSE-handler side: a fresh
// HTTP connection writes the rigdef's READ command to the rig
// promptly after subscribing. The fake's recorded writes show
// INIT first (pipeline startup), then the READ from the bootstrap
// trigger.
func TestHTTPHandler_BootstrapFiresOnSubscribe(t *testing.T) {
	s, fake, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	// Pipeline spawns eagerly at Start, so INIT is sent before any
	// HTTP GET. Wait for it so we can distinguish INIT (writes[0])
	// from the bootstrap READ that fires on SSE-open (writes[1]).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(fake.recordedWrites()) < 1 {
		t.Fatal("pipeline did not send INIT within 1s")
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Now the bootstrap READ should land. Wait for the second write.
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(fake.recordedWrites()) >= 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	writes := fake.recordedWrites()
	if len(writes) < 2 {
		t.Fatalf("expected at least 2 writes (INIT + bootstrap READ), got %d", len(writes))
	}
	if string(writes[0]) != "AI1;" {
		t.Errorf("first write = %q, want %q (INIT)", writes[0], "AI1;")
	}
	if string(writes[1]) != "ID;FA;FB;ST;VS;MD0;MD1;PC;" {
		t.Errorf("bootstrap write = %q, want %q", writes[1], "ID;FA;FB;ST;VS;MD0;MD1;PC;")
	}
}

// TestHTTPHandler_FanOutToMultipleSubscribers covers the M3a.3
// multi-subscriber acceptance: 5 concurrent SSE clients all see the
// same rig-state event when one rig push fires. Validates the
// existing hub.publish fan-out under realistic concurrency rather
// than as an isolated unit.
func TestHTTPHandler_FanOutToMultipleSubscribers(t *testing.T) {
	s, fake, shutdownCh := newHandlerTestService(t)
	srv := httptest.NewServer(s.HTTPHandler(shutdownCh))
	t.Cleanup(srv.Close)

	const numClients = 5
	type result struct {
		idx    int
		sawEvt bool
		err    error
	}
	results := make(chan result, numClients)

	// Spin up N concurrent SSE clients. Each opens a connection,
	// scans for the FA frequency push we'll feed below, and reports.
	for i := 0; i < numClients; i++ {
		idx := i
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			resp, err := srv.Client().Do(req)
			if err != nil {
				results <- result{idx: idx, err: err}
				return
			}
			defer resp.Body.Close()

			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				line := scanner.Text()
				// Looking for the data line carrying our test
				// frequency. Each subscriber should see it because
				// the hub fans out to all.
				if strings.Contains(line, `"vfoA":21074000`) {
					results <- result{idx: idx, sawEvt: true}
					return
				}
			}
			results <- result{idx: idx, err: scanner.Err()}
		}()
	}

	// Give all clients time to connect + subscribe before we feed
	// the test event. Bootstrap READ writes from each subscribe
	// will happen but produce no responses (fake doesn't auto-reply).
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		// At least N+1 writes means N bootstrap READs landed (plus
		// the pipeline's startup INIT).
		if len(fake.recordedWrites()) >= numClients+1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Feed one frequency push; every subscriber should see it.
	fake.feedLine([]byte("FA021074000"))

	// Collect results.
	saw := 0
	for i := 0; i < numClients; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Errorf("client %d error: %v", r.idx, r.err)
			}
			if r.sawEvt {
				saw++
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("client %d did not finish within 3s", i)
		}
	}
	if saw != numClients {
		t.Errorf("only %d of %d clients saw the broadcast event", saw, numClients)
	}
}
