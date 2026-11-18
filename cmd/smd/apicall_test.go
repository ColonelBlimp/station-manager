package main

import (
	"bufio"
	"crypto/tls"
	stderr "errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/desktopid"
)

/*
   The contract of the tests' request helpers (operator ruling 2026-10-20, after
   the CI race on 684fc6b4). apiCall (the app listener) and mainCall (the main
   listener) are one loop, callWhenListening, over two transports; AC1 to AC5
   hold the loop to the contract on the main listener's unix socket, and AC6 to
   AC8 hold the app listener's transport to it (W-0027 C2e). It waits for a
   listener that is still binding, and fails at once on the listener's own error. Only a dial that never connected is
   retried: once the request may have reached the daemon, a transport error
   fails without sending it again, and an HTTP error status is returned to the
   caller. A body that cannot be read in full fails.

     AC1 a socket that appears late is waited for; one request is received.
     AC2 a complete POST whose response is dropped fails after exactly one
         received request.
     AC3 a truncated response body fails after exactly one received request.
     AC4 a listener startup error fails promptly, naming the listener.
     AC5 an HTTP 500 reaches the caller with its body.
     AC6 an app listener the daemon has not published yet is waited for; one
         request is received, carrying the credential.
     AC7 over the app transport, a complete POST whose response is dropped
         fails promptly after exactly one received request.
     AC8 a listener on the app's port with another certificate is never sent a
         request, and is not retried.
     AC9 the aggregate POST the reconcile tests send off the test's goroutine
         (postAggregate) keeps the same contract: an unpublished listener is
         waited for; a dropped response and a truncated body each fail promptly
         after exactly one received request (codex-158).
*/

// fatalTB records the helper's failure instead of failing the test, and
// stops the helper's goroutine as t.Fatal would.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatal(args ...any) {
	f.msg = fmt.Sprint(args...)
	runtime.Goexit()
}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

type apiCallResult struct {
	code   int
	body   string
	failed string
}

// runAPICall runs mainCallBody against d, recording a failure as a result.
func runAPICall(t *testing.T, d *daemon, method, body string) apiCallResult {
	t.Helper()
	return runCall(t, func(tb testing.TB) (int, []byte) { return mainCallBody(tb, d, method, "/v1/probe", body) })
}

// runCall runs one helper call, recording a failure as a result.
func runCall(t *testing.T, call func(tb testing.TB) (int, []byte)) apiCallResult {
	t.Helper()
	tb := &fatalTB{TB: t}
	var res apiCallResult
	done := make(chan struct{})
	go func() {
		defer close(done)
		code, out := call(tb)
		res.code, res.body = code, string(out)
	}()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatal("the call did not return")
	}
	res.failed = tb.msg
	return res
}

// apiCallDaemon is a daemon with only what mainCall reads: the socket path and
// the listener's error channel.
func apiCallDaemon(t *testing.T) *daemon {
	t.Helper()
	return &daemon{
		cfgSvc: config.New(config.Config{SocketPath: filepath.Join(t.TempDir(), "smd.sock")}),
		errCh:  make(chan error, 1),
	}
}

// rawServer accepts on d's socket and hands each fully read request to
// answer, counting the requests it received.
type rawServer struct {
	received    atomic.Int32
	bodies      chan string
	credentials chan string // the operating credential each request presented
}

func serveRaw(t *testing.T, d *daemon, delay time.Duration, answer func(net.Conn)) *rawServer {
	t.Helper()
	s := &rawServer{bodies: make(chan string, 16), credentials: make(chan string, 16)}
	ready := make(chan net.Listener, 1)
	go func() {
		time.Sleep(delay)
		ln, err := net.Listen("unix", d.cfgSvc.Snapshot().SocketPath)
		if err != nil {
			ready <- nil
			return
		}
		ready <- ln
		s.serve(ln, answer)
	}()
	t.Cleanup(func() {
		select {
		case ln := <-ready:
			if ln != nil {
				_ = ln.Close()
			}
		case <-time.After(5 * time.Second):
		}
	})
	return s
}

// serve hands each fully read request on ln to answer, until ln closes.
func (s *rawServer) serve(ln net.Listener, answer func(net.Conn)) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		req, err := http.ReadRequest(bufio.NewReader(conn))
		if err != nil {
			_ = conn.Close()
			continue
		}
		b, err := io.ReadAll(req.Body)
		if err != nil {
			_ = conn.Close()
			continue
		}
		// Counted before the answer, so the count is final once the
		// client has its result; a replaying client must not block here.
		s.received.Add(1)
		select {
		case s.bodies <- string(b):
		default:
		}
		select {
		case s.credentials <- req.Header.Get("X-SM-Operating-Credential"):
		default:
		}
		answer(conn)
		_ = conn.Close()
	}
}

func reply(raw string) func(net.Conn) {
	return func(c net.Conn) { _, _ = io.WriteString(c, raw) }
}

func TestAPICall_AC1_ALateSocketIsWaitedFor(t *testing.T) {
	d := apiCallDaemon(t)
	s := serveRaw(t, d, 300*time.Millisecond, reply("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	res := runAPICall(t, d, http.MethodPost, `{"n":1}`)
	if res.failed != "" || res.code != http.StatusOK || res.body != "ok" {
		t.Fatalf("result %+v; want 200 ok once the socket is up", res)
	}
	if n := s.received.Load(); n != 1 {
		t.Fatalf("%d request(s) received; want 1", n)
	}
	if b := <-s.bodies; b != `{"n":1}` {
		t.Fatalf("body %q; want the whole request body", b)
	}
}

// AC2 covers the bodyless POST every existing caller sends and the POST with
// a body, and requires the failure promptly rather than at the deadline.
func TestAPICall_AC2_ADroppedResponseIsNotReplayed(t *testing.T) {
	for _, body := range []string{"", `{"n":2}`} {
		t.Run(fmt.Sprintf("body %q", body), func(t *testing.T) {
			d := apiCallDaemon(t)
			s := serveRaw(t, d, 0, func(net.Conn) {})
			waitForSocket(t, d)
			start := time.Now()
			res := runAPICall(t, d, http.MethodPost, body)
			if res.failed == "" {
				t.Fatalf("result %+v; want a failure for the dropped response", res)
			}
			if n := s.received.Load(); n != 1 {
				t.Fatalf("%d request(s) received; want exactly 1, never replayed (failure %q)", n, res.failed)
			}
			if b := <-s.bodies; b != body {
				t.Fatalf("body %q; want the complete POST %q", b, body)
			}
			if took := time.Since(start); took > 5*time.Second {
				t.Fatalf("failed after %v (%q); want at once, not at the readiness deadline", took, res.failed)
			}
		})
	}
}

func TestAPICall_AC3_ATruncatedBodyFails(t *testing.T) {
	d := apiCallDaemon(t)
	s := serveRaw(t, d, 0, reply("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nConnection: close\r\n\r\nshort"))
	waitForSocket(t, d)
	res := runAPICall(t, d, http.MethodPost, `{"n":3}`)
	if res.failed == "" {
		t.Fatalf("result %+v; want a failure for the truncated body", res)
	}
	if n := s.received.Load(); n != 1 {
		t.Fatalf("%d request(s) received; want exactly 1 (failure %q)", n, res.failed)
	}
}

func TestAPICall_AC4_AListenerErrorFailsPromptly(t *testing.T) {
	d := apiCallDaemon(t)
	d.errCh <- stderr.New("bind refused")
	start := time.Now()
	res := runAPICall(t, d, http.MethodGet, "")
	if !strings.Contains(res.failed, "the API listener on") || !strings.Contains(res.failed, "bind refused") {
		t.Fatalf("failure %q; want the listener's own error", res.failed)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("failed after %v; want promptly, not at the readiness deadline", took)
	}
}

func TestAPICall_AC5_AnHTTPErrorReachesTheCaller(t *testing.T) {
	d := apiCallDaemon(t)
	s := serveRaw(t, d, 0, reply("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 4\r\nConnection: close\r\n\r\nboom"))
	waitForSocket(t, d)
	res := runAPICall(t, d, http.MethodPost, `{"n":5}`)
	if res.failed != "" || res.code != http.StatusInternalServerError || res.body != "boom" {
		t.Fatalf("result %+v; want 500 boom returned to the caller", res)
	}
	if n := s.received.Load(); n != 1 {
		t.Fatalf("%d request(s) received; want 1", n)
	}
}

// waitForSocket waits for the raw server's bind, so a proof that is not about
// the readiness wait does not depend on it.
func waitForSocket(t *testing.T, d *daemon) {
	t.Helper()
	sock := d.cfgSvc.Snapshot().SocketPath
	waitUntil(t, "the raw server's socket", func() bool {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	})
}

// rawAppListener is a raw TLS server on a loopback port presenting id, as the app
// listener presents the install's desktop identity.
func rawAppListener(t *testing.T, id desktopid.Identity, answer func(net.Conn)) (*rawServer, int) {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{id.TLS}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	s := &rawServer{bodies: make(chan string, 16), credentials: make(chan string, 16)}
	go s.serve(ln, answer)
	return s, ln.Addr().(*net.TCPAddr).Port
}

func testIdentity(t *testing.T) desktopid.Identity {
	t.Helper()
	id, _, err := desktopid.Ensure(privateRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestAPICall_AC6_AnAppListenerNotYetPublishedIsWaitedFor(t *testing.T) {
	d := apiCallDaemon(t)
	id := testIdentity(t)
	s, port := rawAppListener(t, id, reply("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	published := time.Now().Add(300 * time.Millisecond)
	var early atomic.Int32
	res := runCall(t, func(tb testing.TB) (int, []byte) {
		return callWhenListening(tb, d, "the app listener", func() (callTarget, bool) {
			if time.Now().Before(published) {
				early.Add(1)
				return callTarget{}, false
			}
			return appTarget(port, id, "the-install-credential"), true
		}, http.MethodPost, "/v1/probe", `{"n":6}`, nil)
	})
	if res.failed != "" || res.code != http.StatusOK || res.body != "ok" {
		t.Fatalf("result %+v; want 200 ok once the app listener is published", res)
	}
	if early.Load() == 0 {
		t.Fatal("the fixture: the listener was published before the first attempt, so nothing was waited for")
	}
	if n := s.received.Load(); n != 1 {
		t.Fatalf("%d request(s) received; want 1", n)
	}
	if b := <-s.bodies; b != `{"n":6}` {
		t.Fatalf("body %q; want the whole request body", b)
	}
	if c := <-s.credentials; c != "the-install-credential" {
		t.Fatalf("credential presented %q; want the install's", c)
	}
}

func TestAPICall_AC7_OverTheAppTransportADroppedResponseIsNotReplayed(t *testing.T) {
	d := apiCallDaemon(t)
	id := testIdentity(t)
	s, port := rawAppListener(t, id, func(net.Conn) {})
	start := time.Now()
	res := runCall(t, func(tb testing.TB) (int, []byte) {
		return callWhenListening(tb, d, "the app listener", func() (callTarget, bool) {
			return appTarget(port, id, "c"), true
		}, http.MethodPost, "/v1/probe", `{"n":7}`, nil)
	})
	if res.failed == "" {
		t.Fatalf("result %+v; want a failure for the dropped response", res)
	}
	if n := s.received.Load(); n != 1 {
		t.Fatalf("%d request(s) received; want exactly 1, never replayed (failure %q)", n, res.failed)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("failed after %v (%q); want at once, not at the readiness deadline", took, res.failed)
	}
}

func TestAPICall_AC8_AnotherCertificateOnTheAppPortIsSentNothing(t *testing.T) {
	d := apiCallDaemon(t)
	theirs, ours := testIdentity(t), testIdentity(t)
	s, port := rawAppListener(t, theirs, reply("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	start := time.Now()
	res := runCall(t, func(tb testing.TB) (int, []byte) {
		return callWhenListening(tb, d, "the app listener", func() (callTarget, bool) {
			return appTarget(port, ours, "c"), true
		}, http.MethodPost, "/v1/probe", `{"n":8}`, nil)
	})
	if !strings.Contains(res.failed, "not smd's desktop identity") {
		t.Fatalf("result %+v; want the pin's refusal", res)
	}
	if n := s.received.Load(); n != 0 {
		t.Fatalf("%d request(s) reached a listener that is not this daemon; want none", n)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("failed after %v; want at once, not at the readiness deadline", took)
	}
}

func TestAPICall_AC9_TheAggregatePostKeepsTheContract(t *testing.T) {
	const ok = "HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"
	post := func(t *testing.T, answer func(net.Conn), publishAfter time.Duration) (postResult, *rawServer, time.Duration) {
		t.Helper()
		id := testIdentity(t)
		s, port := rawAppListener(t, id, answer)
		start := time.Now()
		res := postAggregate(func() (callTarget, bool) {
			if time.Since(start) < publishAfter {
				return callTarget{}, false
			}
			return appTarget(port, id, "c"), true
		})
		return res, s, time.Since(start)
	}
	t.Run("an unpublished listener is waited for", func(t *testing.T) {
		res, s, took := post(t, reply(ok), 300*time.Millisecond)
		if res.err != nil || res.code != http.StatusOK || res.body != "ok" {
			t.Fatalf("result %+v; want 200 ok once the app listener is published", res)
		}
		if took < 300*time.Millisecond {
			t.Fatalf("the fixture: answered after %v, before the listener was published", took)
		}
		if n := s.received.Load(); n != 1 {
			t.Fatalf("%d request(s) received; want 1", n)
		}
	})
	for name, answer := range map[string]func(net.Conn){
		"a dropped response": func(net.Conn) {},
		"a truncated body":   reply("HTTP/1.1 200 OK\r\nContent-Length: 100\r\nConnection: close\r\n\r\nshort"),
	} {
		t.Run(name+" is not replayed", func(t *testing.T) {
			res, s, took := post(t, answer, 0)
			if res.err == nil {
				t.Fatalf("result %+v; want an error, never a success", res)
			}
			if n := s.received.Load(); n != 1 {
				t.Fatalf("%d request(s) received; want exactly 1, never replayed (error %v)", n, res.err)
			}
			if took > 5*time.Second {
				t.Fatalf("failed after %v (%v); want at once, not at the deadline", took, res.err)
			}
		})
	}
}
