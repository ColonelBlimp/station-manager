package api

import (
	"context"
	stderr "errors"
	"net"
	"net/http"
	"sync"
	"testing"
	"time"
)

/*
   W-0022 (2026-10-13; CI run 38356688161 attempt 1,
   TestIdentityWire_SW6_NoRetryWhileHeld: the http node failed its drain with
   "close unix …/smd.sock: use of closed network connection").

   StopAccepting closes the listener; http.Server.Serve tracks it inside its
   own close-once wrapper and untracks it only when Serve returns; Shutdown
   closes every listener still tracked, returns the first close error, then
   waits, with no deadline, for Serve to return (net/http go1.26
   server.go:3154–3159). Every test here runs the production sequence —
   ListenAndServe, StopAccepting, Shutdown — with the bound listener wrapped
   through the unexported listenerHook, which lets the test hold Serve.

     SO1  Serve untracks before Shutdown: Shutdown returns nil.
     SO2  Shutdown while Serve still tracks the listener: Shutdown returns
          nil (one close, never two).
     SO3  a genuine error from the listener's first close is reported by
          Shutdown, (a) when Serve untracked first and (b) when Shutdown ran
          while it still tracked the listener.
     SO4  a drain timeout stays a failure: context.DeadlineExceeded.
     SO5  an in-flight request completes and drains; Shutdown returns nil
          (characterization).
     SO7  Shutdown closes the listener first (no StopAccepting: the rollback
          path), the close fails, and the drain times out: Shutdown reports
          both, the close error and context.DeadlineExceeded.

   A genuine close error is injected BENEATH the close-once owner
   (innerListenerHook), as a real close would fail; the gate sits above it
   (listenerHook) to choose the order.
*/

var errGenuineClose = stderr.New("genuine listener close failure (test)")

// gatedListener holds Accept's error return until released (Serve keeps
// tracking the listener meanwhile). Its second close (Shutdown's, after
// StopAccepting's) releases the hold once that close has happened.
type gatedListener struct {
	net.Listener
	failed     chan struct{}
	failedOnce sync.Once
	hold       chan struct{}
	release    func()
	mu         sync.Mutex
	closes     int
}

// failingClose closes the real listener and reports a genuine failure.
type failingClose struct{ net.Listener }

func (l failingClose) Close() error {
	_ = l.Listener.Close()
	return errGenuineClose
}

func (l *gatedListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		l.failedOnce.Do(func() { close(l.failed) })
		<-l.hold
	}
	return c, err
}

func (l *gatedListener) Close() error {
	err := l.Listener.Close()
	l.mu.Lock()
	l.closes++
	n := l.closes
	l.mu.Unlock()
	if n == 2 {
		l.release()
	}
	return err
}

// serving runs the real ListenAndServe with the bound listener gated, and
// returns once the server has answered a request.
func serving(t *testing.T, s *Server, closeFails bool) (*gatedListener, <-chan error) {
	t.Helper()
	if closeFails {
		s.innerListenerHook = func(ln net.Listener) net.Listener { return failingClose{ln} }
	}
	ready := make(chan *gatedListener, 1)
	s.listenerHook = func(ln net.Listener) net.Listener {
		gl := &gatedListener{Listener: ln, failed: make(chan struct{}), hold: make(chan struct{})}
		gl.release = sync.OnceFunc(func() { close(gl.hold) })
		ready <- gl
		return gl
	}
	served := make(chan error, 1)
	go func() { served <- s.ListenAndServe("127.0.0.1:0") }()
	var gl *gatedListener
	select {
	case gl = <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("ListenAndServe never bound")
	}
	// Whatever happens, the listener closes and every hold is released, so a
	// failing test never leaves Serve or a held Accept behind.
	t.Cleanup(func() {
		s.StopAccepting()
		gl.release()
	})
	client := &http.Client{Timeout: 2 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get("http://" + gl.Addr().String() + "/v1/version")
	if err != nil {
		t.Fatalf("the server never answered: %v", err)
	}
	_ = resp.Body.Close()
	return gl, served
}

func shutdownWithin(t *testing.T, s *Server, d time.Duration) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return s.Shutdown(ctx)
}

// untrackFirst closes the listener and lets Serve return before Shutdown.
func untrackFirst(t *testing.T, s *Server, gl *gatedListener, served <-chan error) {
	t.Helper()
	s.StopAccepting()
	gl.release()
	select {
	case err := <-served:
		if err != nil {
			t.Fatalf("ListenAndServe after StopAccepting = %v; want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after the listener closed")
	}
}

// stillTracked closes the listener and holds Serve, so Shutdown finds the
// listener still tracked.
func stillTracked(t *testing.T, s *Server, gl *gatedListener) {
	t.Helper()
	s.StopAccepting()
	select {
	case <-gl.failed:
	case <-time.After(2 * time.Second):
		t.Fatal("Accept never saw the listener close")
	}
}

func TestServerShutdown_SO1_ServeUntracksFirst(t *testing.T) {
	s := testServer(t)
	gl, served := serving(t, s, false)
	untrackFirst(t, s, gl, served)
	if err := shutdownWithin(t, s, 2*time.Second); err != nil {
		t.Fatalf("Shutdown after Serve returned = %v; want nil", err)
	}
}

func TestServerShutdown_SO2_ShutdownWhileStillTracked(t *testing.T) {
	s := testServer(t)
	gl, _ := serving(t, s, false)
	stillTracked(t, s, gl)
	if err := shutdownWithin(t, s, 2*time.Second); err != nil {
		t.Fatalf("Shutdown while Serve still tracks the listener = %v; want nil", err)
	}
}

func TestServerShutdown_SO3_AGenuineCloseErrorIsReported(t *testing.T) {
	t.Run("a: Serve untracked first", func(t *testing.T) {
		s := testServer(t)
		gl, served := serving(t, s, true)
		untrackFirst(t, s, gl, served)
		if err := shutdownWithin(t, s, 2*time.Second); !stderr.Is(err, errGenuineClose) {
			t.Fatalf("Shutdown = %v; want the first close's error", err)
		}
	})
	t.Run("b: Shutdown while still tracked", func(t *testing.T) {
		s := testServer(t)
		gl, _ := serving(t, s, true)
		stillTracked(t, s, gl)
		if err := shutdownWithin(t, s, 2*time.Second); !stderr.Is(err, errGenuineClose) {
			t.Fatalf("Shutdown = %v; want the first close's error", err)
		}
	})
}

// blockingRoute serves /block until released, ahead of the server's own
// handler; inside reports that a request is in its handler.
func blockingRoute(s *Server) (inside <-chan struct{}, release func()) {
	in := make(chan struct{}, 1)
	gate := make(chan struct{})
	next := s.httpServer.Handler
	s.httpServer.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/block" {
			next.ServeHTTP(w, r)
			return
		}
		in <- struct{}{}
		<-gate
		w.WriteHeader(http.StatusOK)
	})
	return in, sync.OnceFunc(func() { close(gate) })
}

// enterHandler waits for the /block request to be in its handler, failing
// fast — never hanging — if the request failed or never arrived.
func enterHandler(t *testing.T, inside <-chan struct{}, code <-chan int) {
	t.Helper()
	select {
	case <-inside:
	case c := <-code:
		t.Fatalf("the /block request ended (%d) before reaching its handler", c)
	case <-time.After(5 * time.Second):
		t.Fatal("the /block request never reached its handler")
	}
}

func requestBlocked(t *testing.T, gl *gatedListener) <-chan int {
	t.Helper()
	code := make(chan int, 1)
	go func() {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get("http://" + gl.Addr().String() + "/block")
		if err != nil {
			code <- 0
			return
		}
		_ = resp.Body.Close()
		code <- resp.StatusCode
	}()
	return code
}

func TestServerShutdown_SO4_ADrainTimeoutStaysAFailure(t *testing.T) {
	s := testServer(t)
	inside, releaseHandler := blockingRoute(s)
	t.Cleanup(releaseHandler)
	gl, served := serving(t, s, false)
	code := requestBlocked(t, gl)
	enterHandler(t, inside, code)
	untrackFirst(t, s, gl, served)
	err := shutdownWithin(t, s, 200*time.Millisecond)
	releaseHandler()
	<-code
	if !stderr.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown with a request still in its handler = %v; want context.DeadlineExceeded", err)
	}
}

func TestServerShutdown_SO5_AnInFlightRequestDrains(t *testing.T) {
	s := testServer(t)
	inside, releaseHandler := blockingRoute(s)
	t.Cleanup(releaseHandler)
	gl, served := serving(t, s, false)
	code := requestBlocked(t, gl)
	enterHandler(t, inside, code)
	untrackFirst(t, s, gl, served)
	done := make(chan error, 1)
	go func() { done <- shutdownWithin(t, s, 5*time.Second) }()
	select {
	case err := <-done:
		t.Fatalf("Shutdown returned (%v) while a request was still in its handler", err)
	case <-time.After(100 * time.Millisecond):
	}
	releaseHandler()
	if c := <-code; c != http.StatusOK {
		t.Fatalf("the in-flight request ended with %d; want 200", c)
	}
	if err := <-done; err != nil {
		t.Fatalf("Shutdown after the in-flight request = %v; want nil", err)
	}
}

func TestServerShutdown_SO7_ACloseErrorAndADrainTimeoutAreBothReported(t *testing.T) {
	s := testServer(t)
	inside, releaseHandler := blockingRoute(s)
	t.Cleanup(releaseHandler)
	gl, _ := serving(t, s, true)
	code := requestBlocked(t, gl)
	enterHandler(t, inside, code)
	// No StopAccepting: Shutdown's own close is the listener's first. Serve
	// need not be held here, and Shutdown waits for it with no deadline, so the
	// gate is released first.
	gl.release()
	err := shutdownWithin(t, s, 200*time.Millisecond)
	releaseHandler()
	<-code
	if !stderr.Is(err, errGenuineClose) || !stderr.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown = %v; want both the close error and context.DeadlineExceeded", err)
	}
}
