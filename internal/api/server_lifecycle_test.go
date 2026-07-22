package api

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestServer_RejectWhenDraining verifies that once shutdown has begun (drain gate
// raised by StopAccepting), NEW requests are turned away with 503 and never reach
// the inner handler — closing the keep-alive window where a request could hit a
// stopping subsystem (review 2026-07-22 #3).
func TestServer_RejectWhenDraining(t *testing.T) {
	srv := testServer(t)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})
	h := srv.rejectWhenDraining(next)

	// Before draining: request passes through untouched.
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest(http.MethodGet, "/v1/version", nil))
	if !nextCalled || w1.Code != http.StatusOK {
		t.Fatalf("pre-drain: nextCalled=%v code=%d, want true/200", nextCalled, w1.Code)
	}

	// StopAccepting raises the drain gate (httpServer is nil here — the
	// SetKeepAlivesEnabled call is guarded, so this is safe without ListenAndServe).
	srv.StopAccepting()
	nextCalled = false
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest(http.MethodPost, "/v1/qso", nil))
	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("post-drain status = %d, want 503; body = %s", w2.Code, w2.Body.String())
	}
	if nextCalled {
		t.Error("inner handler ran during drain; request was not short-circuited")
	}
	if !strings.Contains(w2.Body.String(), "shutting_down") {
		t.Errorf("body = %q, want shutting_down", w2.Body.String())
	}
}
