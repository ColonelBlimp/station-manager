package bridge

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/serial"
	"github.com/ColonelBlimp/station-manager/internal/types"
)

/*
   W-0017 sub-item A (2026-10-13). The pipeline publishes activeClient only
   after its INIT and post-INIT READ writes, and TriggerBootstrap writes
   nothing while activeClient is nil. So:

     - a LATE subscriber (one that connects after the pipeline published) has
       its bootstrap READ written after it subscribes; the tests that prove that
       wait for the publication itself (waitForPipelineActive), never for a
       write count that the early ordering does not produce;
     - an EARLY subscriber (one that connects before the publication) gets no
       bootstrap, and still receives the rig's answer to the pipeline's own
       startup READ: EB1, before the port opens and between the startup writes
       and the publication.
*/

const ft710Read = "ID;FA;FB;ST;VS;MD0;MD1;PC;"

// waitForPipelineActive blocks until the pipeline has published its client:
// the condition TriggerBootstrap reads. Only then does a new subscriber's
// bootstrap write.
func waitForPipelineActive(t *testing.T, s *Service, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		active := s.activeClient != nil
		s.mu.Unlock()
		if active {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the pipeline did not publish its client within %s", timeout)
}

// passMarker proves the handler has finished its bootstrap attempt: it
// publishes a marker event and reads the stream until the marker's frame. The
// handler writes events only from its loop, which it enters after
// TriggerBootstrap returns.
func passMarker(t *testing.T, s *Service, sc *bufio.Scanner) {
	t.Helper()
	waitForSubscribers(t, s, 1, 2*time.Second)
	s.hub.publish(Event{Name: "test-marker", Payload: map[string]string{"m": "1"}})
	for sc.Scan() {
		if sc.Text() == "event: test-marker" {
			for sc.Scan() && sc.Text() != "" {
			}
			return
		}
	}
	t.Fatalf("the marker never passed through the handler: %v", sc.Err())
}

// releaseOnce returns an idempotent release for gate, also run at cleanup.
// Registered after the service's own cleanup, it runs BEFORE Stop, so a test
// that fails while the pipeline is held never leaves Stop waiting.
func releaseOnce(t *testing.T, gate chan struct{}) func() {
	release := sync.OnceFunc(func() { close(gate) })
	t.Cleanup(release)
	return release
}

// holdBeforePublication holds the pipeline after its second write (INIT, then
// the startup READ) is recorded, before it publishes its client. reached is
// closed once the pipeline is held there.
func holdBeforePublication(t *testing.T, f *fakeSerial) (release func(), reached <-chan struct{}) {
	t.Helper()
	gate := make(chan struct{})
	release = releaseOnce(t, gate)
	at := make(chan struct{})
	f.mu.Lock()
	f.afterWrite = func(n int) {
		if n == 2 {
			close(at)
			<-gate
		}
	}
	f.mu.Unlock()
	return release, at
}

// heldService is a handler test service held from the start, before any
// write: at the port's open (gateOpen) or after its second write, before the
// publication. The returned release lets it go. With answer, the fake answers
// the startup READ with the FT-710's identity, as the rig does.
func heldService(t *testing.T, gateOpen, answer bool) (*Service, *fakeSerial, func()) {
	t.Helper()
	s := New(types.BridgeConfig{
		Enabled: true,
		Serial:  &types.BridgeSerialConfig{Port: "fake"},
		Cat:     &types.BridgeCatConfig{Driver: "yaesu-ft710"},
	}, &logging.Service{})
	fake := newFakeSerial()
	if answer {
		fake.onWrite = func(written []byte) []byte {
			if string(written) == ft710Read {
				return []byte("ID0800")
			}
			return nil
		}
	}
	opened := make(chan struct{})
	s.openClient = func(_ serial.Config) (serial.Client, error) {
		<-opened
		return fake, nil
	}
	if err := s.Initialize(); err != nil {
		t.Fatal(err)
	}
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	openRelease := releaseOnce(t, opened)
	if gateOpen {
		return s, fake, openRelease
	}
	// Held after the second write: the hold is in place before the port opens,
	// and the caller connects only once the pipeline is held there.
	release, reached := holdBeforePublication(t, fake)
	openRelease()
	select {
	case <-reached:
	case <-time.After(3 * time.Second):
		t.Fatal("the pipeline never reached its hold after the second write")
	}
	return s, fake, release
}

func TestHTTPHandler_EB1_AnEarlySubscriberGetsTheStartupSnapshot(t *testing.T) {
	for name, gateOpen := range map[string]bool{
		"before the port opens":                          true,
		"between the startup writes and the publication": false,
	} {
		t.Run(name, func(t *testing.T) {
			s, fake, release := heldService(t, gateOpen, true)
			srv := httptest.NewServer(s.HTTPHandler(make(chan struct{})))
			t.Cleanup(srv.Close)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
			resp, err := srv.Client().Do(req)
			if err != nil {
				t.Fatalf("GET: %v", err)
			}
			defer resp.Body.Close()
			sc := bufio.NewScanner(resp.Body)
			passMarker(t, s, sc) // the bootstrap was attempted while the pipeline had not published
			// While the pipeline is still held, nothing but its own startup writes
			// exist: the bootstrap wrote nothing.
			wantHeld := []string{"AI1;", ft710Read}
			if gateOpen {
				wantHeld = nil
			}
			if got := writesOf(fake); !slices.Equal(got, wantHeld) {
				t.Fatalf("writes while held, after the bootstrap attempt = %q; want %q", got, wantHeld)
			}
			release()
			waitForPipelineActive(t, s, 3*time.Second)
			// The rig's answer to the startup READ reaches the early subscriber
			// as one complete frame.
			var sawEvent, sawData bool
			for sc.Scan() {
				line := sc.Text()
				switch {
				case line == "event: rig-state":
					sawEvent = true
				case strings.HasPrefix(line, "data: ") && sawEvent:
					sawData = strings.Contains(line, `"rigIdentity":"FT-710"`)
				case line == "" && sawEvent:
					if sawData {
						// Startup order: INIT, then the startup READ whose answer
						// this frame is. Later writes (identity checks, liveness
						// recovery) are the pipeline's own.
						if w := writesOf(fake); len(w) < 2 || w[0] != "AI1;" || w[1] != ft710Read {
							t.Fatalf("writes = %q; want INIT then the startup READ first", w)
						}
						return
					}
					sawEvent = false
				}
			}
			t.Fatalf("no complete FT-710 rig-state frame reached the early subscriber: %v", sc.Err())
		})
	}
}

// writesOf is the fake's recorded writes as strings.
func writesOf(f *fakeSerial) []string {
	var out []string
	for _, w := range f.recordedWrites() {
		out = append(out, string(w))
	}
	return out
}
