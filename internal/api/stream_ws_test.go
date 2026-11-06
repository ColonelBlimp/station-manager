package api

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/ColonelBlimp/station-manager/internal/config"
	"github.com/ColonelBlimp/station-manager/internal/logging"
)

// ADR 0099 W1: the shell and rig event streams over WebSocket. These tests pin
// the contract in the ADR's "WebSocket event delivery" section: exact-origin
// admission, subscribe-before-101, the {event,data} envelope, the SMD-owned
// reader, the close coordinator's transitions, the per-write deadline, and the
// stream registry's shutdown promise.

const wsEventsPath = "/v1/events/ws"

// wsTestServer is a full api.Server (real middleware chain) behind httptest.
func wsTestServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	srv := testServer(t)
	ts := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(ts.Close)
	return srv, ts
}

func hostOf(ts *httptest.Server) string { return strings.TrimPrefix(ts.URL, "http://") }

func waitFor(t *testing.T, what string, within time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("%s: not reached within %v", what, within)
}

// --- a minimal raw RFC 6455 client, for peers the library client cannot fake
// (slow or silent close replies, never reading). Client frames are masked. ---

type rawWS struct {
	conn net.Conn
	br   *bufio.Reader
}

func rawDial(t *testing.T, addr, path, host, origin string) (*rawWS, *http.Response) {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	req := "GET " + path + " HTTP/1.1\r\nHost: " + host + "\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n"
	if origin != "" {
		req += "Origin: " + origin + "\r\n"
	}
	req += "\r\n"
	if _, err := io.WriteString(c, req); err != nil {
		t.Fatalf("write handshake: %v", err)
	}
	br := bufio.NewReader(c)
	resp, err := http.ReadResponse(br, nil)
	if err != nil {
		t.Fatalf("read handshake response: %v", err)
	}
	return &rawWS{conn: c, br: br}, resp
}

// readFrame reads one unmasked server frame (no fragmentation in these tests).
func (w *rawWS) readFrame() (op byte, payload []byte, err error) {
	h := make([]byte, 2)
	if _, err = io.ReadFull(w.br, h); err != nil {
		return 0, nil, err
	}
	n := uint64(h[1] & 0x7f)
	switch n {
	case 126:
		var v uint16
		if err = binary.Read(w.br, binary.BigEndian, &v); err != nil {
			return 0, nil, err
		}
		n = uint64(v)
	case 127:
		if err = binary.Read(w.br, binary.BigEndian, &n); err != nil {
			return 0, nil, err
		}
	}
	payload = make([]byte, n)
	_, err = io.ReadFull(w.br, payload)
	return h[0] & 0x0f, payload, err
}

func (w *rawWS) writeFrame(op byte, payload []byte) error {
	mask := [4]byte{1, 2, 3, 4}
	b := []byte{0x80 | op, 0x80 | byte(len(payload))}
	b = append(b, mask[:]...)
	for i, p := range payload {
		b = append(b, p^mask[i%4])
	}
	_, err := w.conn.Write(b)
	return err
}

// readUntilClose reads frames until a close frame (returns its code) or an error.
func (w *rawWS) readUntilClose() (code int, err error) {
	for {
		op, p, err := w.readFrame()
		if err != nil {
			return 0, err
		}
		if op == 0x8 {
			if len(p) >= 2 {
				return int(binary.BigEndian.Uint16(p)), nil
			}
			return 1005, nil
		}
	}
}

func closePayload(code int) []byte {
	return binary.BigEndian.AppendUint16(nil, uint16(code))
}

func libDial(t *testing.T, ts *httptest.Server, path string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, resp, err := websocket.Dial(ctx, "ws://"+hostOf(ts)+path, &websocket.DialOptions{
		HTTPHeader: http.Header{"Origin": []string{ts.URL}},
	})
	if err != nil {
		t.Fatalf("dial %s: %v (resp %v)", path, err, resp)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

type wsFrameJSON struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

func libRead(t *testing.T, c *websocket.Conn) wsFrameJSON {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	typ, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if typ != websocket.MessageText {
		t.Fatalf("frame type %v, want text", typ)
	}
	var f wsFrameJSON
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("frame %q is not the {event,data} envelope: %v", b, err)
	}
	return f
}

// =====================================================================
// RED-first (ADR 0099, reviewer 2026-11-05): the two coordinator cases.
// =====================================================================

// A policy close where the SMD reader has ALREADY finished (it reported the
// data message and exited) and the cooperative peer has not yet replied: the
// 1008 close frame must be sent, and the transport must stay open for the
// peer's reply within the grace. A coordinator that treats the finished reader
// as "done" closes the transport before (or instead of) the close frame.
func TestWSStream_PolicyCloseWaitsForCooperativePeer(t *testing.T) {
	srv, ts := wsTestServer(t)
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status %d, want 101", resp.StatusCode)
	}
	if f, err := readOneText(ws); err != nil || !strings.Contains(f, `"connected"`) {
		t.Fatalf("first frame %q (%v), want connected", f, err)
	}
	waitFor(t, "subscribed", time.Second, func() bool { return srv.hub.SubscriberCount() == 1 })

	if err := ws.writeFrame(0x1, []byte("unexpected")); err != nil {
		t.Fatalf("write data: %v", err)
	}
	code, err := ws.readUntilClose()
	if err != nil {
		t.Fatalf("no close frame before the transport closed: %v", err)
	}
	if code != int(websocket.StatusPolicyViolation) {
		t.Fatalf("close code %d, want 1008", code)
	}
	// Withhold the reply for 300 ms, well inside the 1 s grace. Throughout, the
	// server must keep the transport open (a read times out rather than seeing
	// EOF) and must not have cleaned up (still subscribed and registered).
	_ = ws.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if _, err := ws.br.ReadByte(); !isTimeout(err) {
		t.Fatalf("while the reply was withheld the server closed or wrote (%v); it must wait for the peer", err)
	}
	if srv.hub.SubscriberCount() != 1 || srv.streams.active() != 1 {
		t.Fatalf("cleanup ran while the reply was withheld (subscribers %d, active %d)",
			srv.hub.SubscriberCount(), srv.streams.active())
	}
	if err := ws.writeFrame(0x8, closePayload(int(websocket.StatusPolicyViolation))); err != nil {
		t.Fatalf("write close reply: %v", err)
	}
	replied := time.Now()
	_ = ws.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := ws.br.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("after the reply, want the server to close the transport (EOF); got %v", err)
	}
	if d := time.Since(replied); d > 900*time.Millisecond {
		t.Fatalf("server closed %v after the reply; the completed handshake should end it promptly", d)
	}
	waitFor(t, "handler cleaned up", 2*time.Second, func() bool { return srv.hub.SubscriberCount() == 0 })
}

// An ordinary disconnect, with no shutdown and no hub event: the handler must
// clean up at once (reader exit -> direct cleanup), not at the next 30 s
// keepalive, an eviction or a shutdown.
func TestWSStream_DisconnectCleansUpWithoutOtherEvents(t *testing.T) {
	srv, ts := wsTestServer(t)
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("handshake status %d, want 101", resp.StatusCode)
	}
	// Gate on connected: the handler is in its loop, so the disconnect must be
	// noticed by the reader-exit branch, not the failed-initial-write path.
	if f, err := readOneText(ws); err != nil || !strings.Contains(f, `"connected"`) {
		t.Fatalf("connected: %q (%v)", f, err)
	}
	_ = ws.conn.Close()
	waitFor(t, "unsubscribed after a plain disconnect", time.Second, func() bool { return srv.hub.SubscriberCount() == 0 })
	waitFor(t, "registry drained", time.Second, func() bool { return srv.streams.active() == 0 })
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func readOneText(ws *rawWS) (string, error) {
	_ = ws.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer func() { _ = ws.conn.SetReadDeadline(time.Time{}) }()
	op, p, err := ws.readFrame()
	if err != nil {
		return "", err
	}
	if op != 0x1 {
		return "", errors.New("not a text frame")
	}
	return string(p), nil
}

// =====================================================================
// Admission: exact same origin (responsibility 1).
// =====================================================================

func TestWSStream_OriginRule(t *testing.T) {
	_, ts := wsTestServer(t)
	addr := hostOf(ts)
	port := addr[strings.LastIndex(addr, ":")+1:]
	cases := []struct {
		name, host, origin string
		want               int
	}{
		{"same origin 127.0.0.1", addr, "http://" + addr, 101},
		{"same origin localhost", "localhost:" + port, "http://localhost:" + port, 101},
		{"dev proxy: Host and Origin both the dev port", "localhost:5176", "http://localhost:5176", 101},
		{"missing Origin", addr, "", 403},
		{"null Origin", addr, "null", 403},
		{"malformed Origin", addr, "http://%zz", 403},
		{"different scheme", addr, "https://" + addr, 403},
		{"different port, same host", addr, "http://127.0.0.1:1", 403},
		{"loopback alias is a different origin", addr, "http://localhost:" + port, 403},
		{"foreign host", addr, "http://evil.example", 403},
		{"Origin with a path", addr, "http://" + addr + "/x", 403},
		{"Origin with userinfo", addr, "http://u@" + addr, 403},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ws, resp := rawDial(t, addr, wsEventsPath, tc.host, tc.origin)
			defer ws.conn.Close()
			if resp.StatusCode != tc.want {
				t.Fatalf("status %d, want %d", resp.StatusCode, tc.want)
			}
		})
	}
}

// =====================================================================
// Delivery: subscribe before 101, connected first, the envelope.
// =====================================================================

func TestWSStream_SubscribesBeforeUpgradeAndDeliversEnvelope(t *testing.T) {
	srv, ts := wsTestServer(t)
	c := libDial(t, ts, wsEventsPath)
	// Dial has returned, so the 101 was observed: the subscription must exist.
	if n := srv.hub.SubscriberCount(); n != 1 {
		t.Fatalf("subscribers at 101: %d, want 1 (subscribe precedes the upgrade)", n)
	}
	if f := libRead(t, c); f.Event != "connected" {
		t.Fatalf("first frame event %q, want connected", f.Event)
	}
	srv.hub.Publish("qso.stored", map[string]any{"qso_uuid": "u-1", "logbook_id": 7})
	f := libRead(t, c)
	if f.Event != "qso.stored" {
		t.Fatalf("event %q, want qso.stored", f.Event)
	}
	var data map[string]any
	if err := json.Unmarshal(f.Data, &data); err != nil || data["qso_uuid"] != "u-1" || data["logbook_id"] != float64(7) {
		t.Fatalf("data %s: want the payload object itself (%v)", f.Data, err)
	}
}

// A cooperative peer that sends a data message gets 1008 promptly — well
// inside the library's 15 s self-wait that CloseRead would have caused.
func TestWSStream_DataMessageClosesWith1008Promptly(t *testing.T) {
	srv, ts := wsTestServer(t)
	c := libDial(t, ts, wsEventsPath)
	_ = libRead(t, c) // connected
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Write(ctx, websocket.MessageText, []byte("hi")); err != nil {
		t.Fatalf("write: %v", err)
	}
	start := time.Now()
	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusPolicyViolation {
		t.Fatalf("close status %v (err %v), want 1008", got, err)
	}
	waitFor(t, "cleaned up", 2*time.Second, func() bool { return srv.streams.active() == 0 })
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("1008 handshake took %v", d)
	}
}

// Hub eviction (or hub close) ends the stream with 1001.
func TestWSStream_HubCloseEndsWith1001(t *testing.T) {
	srv, ts := wsTestServer(t)
	c := libDial(t, ts, wsEventsPath)
	_ = libRead(t, c)
	srv.hub.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := c.Read(ctx)
	if got := websocket.CloseStatus(err); got != websocket.StatusGoingAway {
		t.Fatalf("close status %v (err %v), want 1001", got, err)
	}
	waitFor(t, "cleaned up", 2*time.Second, func() bool { return srv.streams.active() == 0 })
}

// A wedged peer (socket open, never reading) is ended by the per-write deadline.
func TestWSStream_WedgedPeerEndedByWriteDeadline(t *testing.T) {
	orig := sseWriteTimeout
	sseWriteTimeout = 200 * time.Millisecond
	t.Cleanup(func() { sseWriteTimeout = orig })
	srv, ts := wsTestServer(t)
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	_ = ws // never read from again
	waitFor(t, "subscribed", time.Second, func() bool { return srv.hub.SubscriberCount() == 1 })
	big := strings.Repeat("x", 256*1024)
	stop := time.Now().Add(5 * time.Second)
	for srv.hub.SubscriberCount() == 1 && time.Now().Before(stop) {
		srv.hub.Publish("qso.stored", map[string]any{"pad": big, "logbook_id": 1, "qso_uuid": "x"})
		time.Sleep(5 * time.Millisecond)
	}
	waitFor(t, "wedged handler ended", 3*time.Second, func() bool { return srv.streams.active() == 0 })
}

// =====================================================================
// Shutdown (responsibility 7).
// =====================================================================

// writeDone is one wsWriteDone report: the frame name and Conn.Write's error.
type writeDone struct {
	name string
	err  error
}

// wsTestServerObserved is wsTestServer with the write-completion seam set
// before serving (it stays unchanged until every handler has finished) and,
// optionally, the accepted transports wrapped.
func wsTestServerObserved(t *testing.T, wrap func(net.Conn) net.Conn) (*Server, *httptest.Server, <-chan writeDone) {
	t.Helper()
	srv := testServer(t)
	done := make(chan writeDone, 16)
	srv.wsWriteDone = func(name string, err error) { done <- writeDone{name, err} }
	ts := httptest.NewUnstartedServer(srv.httpServer.Handler)
	if wrap != nil {
		ts.Listener = &wrapListener{Listener: ts.Listener, wrap: wrap}
	}
	ts.Start()
	t.Cleanup(ts.Close)
	return srv, ts, done
}

type wrapListener struct {
	net.Listener
	wrap func(net.Conn) net.Conn
}

func (l *wrapListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return l.wrap(c), nil
}

// holdConn holds the server's "connected" write AFTER its bytes reach the
// peer, until the transport is closed — an interruptible in-flight write.
// Closing the transport releases the held write with an error. It also
// reports when a read blocked on the transport returns after the close (the
// SMD-owned reader is the only reader of a hijacked stream).
type holdConn struct {
	net.Conn
	held          chan struct{} // the "connected" bytes are out; the write is now held
	writeReleased chan struct{} // the held Write has returned
	closed        chan struct{} // Close was called
	readEnded     chan struct{} // a Read returned after Close
	heldOnce      sync.Once
	closeOnce     sync.Once
	readOnce      sync.Once
}

func newHoldConn(c net.Conn) *holdConn {
	return &holdConn{Conn: c, held: make(chan struct{}), writeReleased: make(chan struct{}),
		closed: make(chan struct{}), readEnded: make(chan struct{})}
}

func (h *holdConn) Write(b []byte) (int, error) {
	n, err := h.Conn.Write(b)
	if err != nil || !strings.Contains(string(b), `"connected"`) {
		return n, err
	}
	first := false
	h.heldOnce.Do(func() { first = true; close(h.held) })
	if !first {
		return n, err
	}
	<-h.closed
	close(h.writeReleased)
	return n, net.ErrClosed
}

func (h *holdConn) Read(b []byte) (int, error) {
	n, err := h.Conn.Read(b)
	if err != nil {
		select {
		case <-h.closed:
			h.readOnce.Do(func() { close(h.readEnded) })
		default:
		}
	}
	return n, err
}

func (h *holdConn) Close() error {
	h.closeOnce.Do(func() { close(h.closed) })
	return h.Conn.Close()
}

// clientCloseWait starts the library client's read (it answers the close
// handshake only while reading) and returns where its terminal error lands.
func clientCloseWait(c *websocket.Conn) <-chan error {
	got := make(chan error, 1)
	go func() {
		_, _, err := c.Read(context.Background())
		got <- err
	}()
	return got
}

func awaitWriteDone(t *testing.T, done <-chan writeDone, name string) writeDone {
	t.Helper()
	select {
	case d := <-done:
		if d.name != name {
			t.Fatalf("first completed write %q, want %q", d.name, name)
		}
		return d
	case <-time.After(5 * time.Second):
		t.Fatalf("no completion reported for the %q write", name)
	}
	return writeDone{}
}

// Write completed: the "connected" Conn.Write has RETURNED (the server-side
// barrier) before Shutdown, so no write is in flight and the close coordinator
// owns the close: the peer must see 1001, and every handler must have ended.
// Reading the frame client-side is not that barrier: the bytes arrive before
// the library's write returns, and a shutdown in that tail interrupts the
// write (the next test; ADR 0099 "Timeouts after the upgrade").
func TestWSStream_ShutdownSends1001AndWaits(t *testing.T) {
	srv, ts, done := wsTestServerObserved(t, nil)
	c := libDial(t, ts, wsEventsPath)
	_ = libRead(t, c)
	if d := awaitWriteDone(t, done, "connected"); d.err != nil {
		t.Fatalf("connected write failed: %v", d.err)
	}
	got := clientCloseWait(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if n := srv.streams.active(); n != 0 {
		t.Fatalf("Shutdown returned success with %d stream handlers still active", n)
	}
	select {
	case err := <-got:
		if code := websocket.CloseStatus(err); code != websocket.StatusGoingAway {
			t.Fatalf("peer saw close %v, want 1001 (client error: %v)", code, err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("peer saw no close")
	}
}

// Write in progress: the "connected" bytes have reached the peer but the
// server's write is held when Shutdown starts. Shutdown cancels the write
// (agreed contract: an in-flight write is interrupted; 1001 is best effort), so
// an abnormal close is permitted. Required: the write is interrupted (it
// returns with an error), the transport is closed, the reader's blocked read
// returns, the handler ends, and Shutdown succeeds.
func TestWSStream_ShutdownDuringWriteInterrupts(t *testing.T) {
	conns := make(chan *holdConn, 1)
	srv, ts, done := wsTestServerObserved(t, func(c net.Conn) net.Conn {
		h := newHoldConn(c)
		conns <- h
		return h
	})
	c := libDial(t, ts, wsEventsPath)
	h := <-conns
	_ = libRead(t, c) // the bytes are out ...
	<-h.held          // ... and the server's write is still held
	select {
	case d := <-done:
		t.Fatalf("the %q write completed before Shutdown (err %v): the fixture did not hold it", d.name, d.err)
	default:
	}
	got := clientCloseWait(c)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if d := awaitWriteDone(t, done, "connected"); d.err == nil {
		t.Fatal("the held write reported success: Shutdown did not interrupt it")
	}
	for what, ch := range map[string]chan struct{}{
		"held write released": h.writeReleased,
		"transport closed":    h.closed,
		"reader's read ended": h.readEnded,
	} {
		select {
		case <-ch:
		default:
			t.Fatalf("after Shutdown returned: %s not observed", what)
		}
	}
	if n := srv.streams.active(); n != 0 {
		t.Fatalf("Shutdown returned success with %d stream handlers still active", n)
	}
	select {
	case err := <-got:
		t.Logf("peer saw close %v (permitted: the write was interrupted); client error: %v",
			websocket.CloseStatus(err), err)
	case <-time.After(2 * time.Second):
		t.Fatal("peer saw no close")
	}
}

// A peer that never answers the close handshake: its transport is closed at
// grace expiry and Shutdown still returns success, promptly.
func TestWSStream_ShutdownForceClosesSilentPeerAtGrace(t *testing.T) {
	srv, ts, done := wsTestServerObserved(t, nil)
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, err := readOneText(ws); err != nil {
		t.Fatalf("connected: %v", err)
	}
	// The server-side barrier: the "connected" Conn.Write has RETURNED, so no
	// write is in flight when Shutdown starts. Reading the frame is not that
	// barrier — a shutdown in the write's tail interrupts it and takes the
	// direct cleanup path, ending at once instead of at the grace (CI run
	// 42161998519).
	if d := awaitWriteDone(t, done, "connected"); d.err != nil {
		t.Fatalf("connected write failed: %v", d.err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	start := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if d := time.Since(start); d < 900*time.Millisecond || d > 2500*time.Millisecond {
		t.Fatalf("Shutdown took %v; want about the 1 s grace (silent peer, forced at grace expiry)", d)
	}
	// The peer was sent 1001 (best effort) and the transport then closed.
	code, err := ws.readUntilClose()
	if err != nil || code != int(websocket.StatusGoingAway) {
		t.Fatalf("peer close %d (%v), want 1001", code, err)
	}
	_ = ws.conn.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := ws.br.ReadByte(); err == nil {
		t.Fatal("transport still open after the grace")
	}
}

// Shutdown arriving while a policy close is underway starts no second close
// and honours the earlier deadline.
func TestWSStream_ShutdownDuringPolicyClose(t *testing.T) {
	orig := wsCloseGrace
	wsCloseGrace = 3 * time.Second // so the shutdown deadline is the earlier one
	t.Cleanup(func() { wsCloseGrace = orig })
	srv, ts := wsTestServer(t)
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, err := readOneText(ws); err != nil {
		t.Fatalf("connected: %v", err)
	}
	if err := ws.writeFrame(0x1, []byte("unexpected")); err != nil {
		t.Fatal(err)
	}
	code, err := ws.readUntilClose()
	if err != nil || code != int(websocket.StatusPolicyViolation) {
		t.Fatalf("close %d (%v), want 1008", code, err)
	}
	// The policy close is now waiting for a reply that never comes.
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = srv.Shutdown(ctx) // success or deadline both acceptable at the boundary
	// The transport closed by the SHUTDOWN deadline (~700 ms), not the 3 s grace,
	// and no second close frame (1001) was sent.
	_ = ws.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	op, _, err := ws.readFrame()
	if err == nil {
		t.Fatalf("got a further frame (op %#x) after the 1008 close; want only transport closure", op)
	}
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Fatalf("transport closed %v after shutdown began; want the earlier (shutdown) deadline", d)
	}
	waitFor(t, "cleaned up", 3*time.Second, func() bool { return srv.streams.active() == 0 })
}

// No registration after shutdown has begun: 503, never an upgrade.
func TestWSStream_RegistrationAfterShutdownRefused(t *testing.T) {
	srv, ts := wsTestServer(t)
	srv.streams.beginShutdown(time.Time{})
	ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	defer ws.conn.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status %d, want 503 (no upgrade after shutdown began)", resp.StatusCode)
	}
}

// Shutdown's promise: success only when every tracked handler has ended; an
// expired context reports the deadline instead.
func TestStreamRegistry_ExpiredContextIsNotSuccess(t *testing.T) {
	var r streamRegistry
	if !r.register() {
		t.Fatal("register refused before shutdown")
	}
	r.beginShutdown(time.Time{})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := r.wait(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("wait with a handler still active: %v, want DeadlineExceeded", err)
	}
	r.done()
	if err := r.wait(context.Background()); err != nil {
		t.Fatalf("wait after done: %v", err)
	}
	if r.register() {
		t.Fatal("register accepted after shutdown began")
	}
}

// =====================================================================
// The bootstrap hook under a hijacked connection (responsibility 8): a hook
// blocked on a lock that observes no context (as cmdMu does) sees workCtx
// cancelled by a disconnect, writes nothing after release, and the handler
// then ends.
// =====================================================================

func TestWSStream_BootstrapHeldLockThenDisconnect(t *testing.T) {
	srv := testServer(t)
	var held sync.Mutex
	held.Lock()
	entered := make(chan struct{})
	ctxErrAfterRelease := make(chan error, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWSStream(srv, w, r, wsStreamSpec[int]{
			subscribe: func() (<-chan int, func()) { return make(chan int), func() {} },
			encode:    func(int) (string, any) { return "n", 0 },
			afterConnected: func(ctx context.Context) {
				close(entered)
				held.Lock() // observes no context, like cmdMu
				held.Unlock()
				ctxErrAfterRelease <- ctx.Err()
			},
		})
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()
	ws, resp := rawDial(t, hostOf(ts), "/test/ws", hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	<-entered
	_ = ws.conn.Close() // the client goes away while the hook waits on the lock
	time.Sleep(100 * time.Millisecond)
	held.Unlock()
	select {
	case err := <-ctxErrAfterRelease:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("hook's context after disconnect: %v, want canceled (workCtx)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook never resumed")
	}
	waitFor(t, "handler ended", 2*time.Second, func() bool { return srv.streams.active() == 0 })
}

// =====================================================================
// Admission classification: the stream paths stay out of limitConcurrent.
// =====================================================================

func TestIsStreamPath_IncludesWebSocketRoutes(t *testing.T) {
	for _, p := range []string{"/v1/events", "/v1/rig/events", "/v1/ft8/events", "/v1/events/ws", "/v1/rig/events/ws"} {
		if !isStreamPath(p) {
			t.Errorf("isStreamPath(%q) = false, want true", p)
		}
	}
	if isStreamPath("/v1/config") {
		t.Error("isStreamPath(/v1/config) = true")
	}
}

// The rig route: the bridge's own Subscribe and (no-op, pipeline not running)
// bootstrap run through the shared stream core; a second subscriber makes the
// bridge announce rig-clients, which arrives in the envelope on the first.
func TestWSStream_RigRouteDeliversBridgeEvents(t *testing.T) {
	srv := rigTestServer(t)
	mux := http.NewServeMux()
	mux.Handle("GET "+wsRigEventsRoute, srv.rigEventsWSHandler(srv.bridge))
	ts := httptest.NewServer(mux)
	defer ts.Close()
	a := libDial(t, ts, wsRigEventsRoute)
	if f := libRead(t, a); f.Event != "connected" {
		t.Fatalf("first frame %q, want connected", f.Event)
	}
	b := libDial(t, ts, wsRigEventsRoute)
	_ = libRead(t, b)
	f := libRead(t, a)
	if f.Event != "rig-clients" {
		t.Fatalf("event %q, want rig-clients", f.Event)
	}
	var p map[string]any
	if err := json.Unmarshal(f.Data, &p); err != nil || len(p) == 0 {
		t.Fatalf("rig-clients data %s: want the payload object (%v)", f.Data, err)
	}
}

// A panic after the upgrade (here in an encoder; the rig bootstrap is the other
// candidate) must not leave a live transport and reader behind: through the
// recovery middleware, the socket is closed, the handler unregisters, and a
// later Shutdown has nothing untracked to miss.
func TestWSStream_PanicAfterUpgradeClosesTransport(t *testing.T) {
	srv := testServer(t)
	events := make(chan int, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /test/ws", func(w http.ResponseWriter, r *http.Request) {
		serveWSStream(srv, w, r, wsStreamSpec[int]{
			subscribe: func() (<-chan int, func()) { return events, func() {} },
			encode:    func(int) (string, any) { panic("encoder bug") },
		})
	})
	ts := httptest.NewServer(srv.recoverPanic(mux))
	defer ts.Close()
	ws, resp := rawDial(t, hostOf(ts), "/test/ws", hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("status %d", resp.StatusCode)
	}
	if _, err := readOneText(ws); err != nil {
		t.Fatalf("connected: %v", err)
	}
	events <- 1 // the encoder panics
	_ = ws.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		if _, err := ws.br.ReadByte(); err != nil {
			if isTimeout(err) {
				t.Fatal("transport still open 2 s after a panic in the handler")
			}
			break
		}
	}
	waitFor(t, "handler unregistered", time.Second, func() bool { return srv.streams.active() == 0 })
}

// =====================================================================
// Origin before admission (reviewer, 2026-11-05): a refused upgrade must not
// take a subscriber slot or log a subscriber transition; valid upgrades still
// obey the shared cap.
// =====================================================================

// lockedBuffer is a goroutine-safe log sink (handlers log concurrently).
type lockedBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func wsLogServer(t *testing.T, maxSubs int) (*Server, *httptest.Server, *lockedBuffer) {
	t.Helper()
	logs := &lockedBuffer{}
	srv := testServerWithLogger(t, func(cfg *config.Config) {
		if maxSubs > 0 {
			cfg.Server.MaxEventSubscribers = maxSubs
		}
	}, nil, logging.NewForWriter(logs))
	ts := httptest.NewServer(srv.httpServer.Handler)
	t.Cleanup(ts.Close)
	return srv, ts, logs
}

func TestWSStream_RefusedOriginTakesNoSubscriberSlot(t *testing.T) {
	_, ts, logs := wsLogServer(t, 0)
	for _, origin := range []string{"", "null", "http://evil.example", "https://" + hostOf(ts)} {
		ws, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), origin)
		_ = ws.conn.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: status %d, want 403", origin, resp.StatusCode)
		}
	}
	out := logs.String()
	if !strings.Contains(out, "cross-origin refused: origin not allowed") {
		t.Fatalf("refusals not logged:\n%s", out)
	}
	if strings.Contains(out, "sse: subscriber connected") || strings.Contains(out, "sse: subscriber disconnected") {
		t.Fatalf("a refused upgrade produced a subscriber transition:\n%s", out)
	}
}

func TestWSStream_ValidUpgradesObeyTheSubscriberCap(t *testing.T) {
	_, ts, logs := wsLogServer(t, 1)
	first, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	if resp.StatusCode != http.StatusSwitchingProtocols {
		t.Fatalf("first upgrade: status %d, want 101", resp.StatusCode)
	}
	defer first.conn.Close()
	second, resp := rawDial(t, hostOf(ts), wsEventsPath, hostOf(ts), ts.URL)
	defer second.conn.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second upgrade over the cap of 1: status %d, want 503", resp.StatusCode)
	}
	if n := strings.Count(logs.String(), "sse: subscriber connected"); n != 1 {
		t.Fatalf("subscriber connected logged %d times, want 1", n)
	}
}
