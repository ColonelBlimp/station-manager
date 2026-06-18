package api

import (
	"net"
	"testing"
	"time"
)

// TestServer_StopAccepting_ReleasesPort verifies the shutdown fix: closing the
// listener up front frees the bound port immediately, and the Serve loop returns
// cleanly (net.ErrClosed tolerated) rather than reporting an error. Regression
// guard for the "address already in use" restart flap.
func TestServer_StopAccepting_ReleasesPort(t *testing.T) {
	srv := testServer(t)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe("127.0.0.1:0") }()

	// Wait for the listener to bind, then capture its actual address.
	var addr string
	for i := 0; i < 200; i++ {
		srv.listenerMu.Lock()
		ln := srv.listener
		srv.listenerMu.Unlock()
		if ln != nil {
			addr = ln.Addr().String()
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if addr == "" {
		t.Fatal("listener never bound")
	}

	srv.StopAccepting()

	// Serve must return cleanly — StopAccepting closing the listener is not an error.
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("ListenAndServe returned error after StopAccepting: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe did not return after StopAccepting (port still held)")
	}

	// The port must be free now — a fresh bind to the same address succeeds.
	ln2, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port %s not released after StopAccepting: %v", addr, err)
	}
	_ = ln2.Close()

	// StopAccepting is idempotent (second call must not panic).
	srv.StopAccepting()
}
