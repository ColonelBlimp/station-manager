package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/ColonelBlimp/station-manager/internal/bridge"
	"github.com/ColonelBlimp/station-manager/internal/errors"
	"github.com/ColonelBlimp/station-manager/internal/events"
)

// WebSocket event streams (ADR 0099 W1, "WebSocket event delivery").
//
// WHY. Every SSE stream holds one of the browser's six HTTP/1.1 connections to
// the daemon, so four SPA windows starve ordinary requests. Open WebSockets do
// not draw from that pool (ADR 0099, WebSocket pool probe), so the shell and rig
// streams move to WebSocket twins of their SSE handlers. FT8 stays on SSE: its
// subscription is the FT8 presence signal and its lifetime must not change.
//
// The SSE endpoints stay for non-browser clients (cmd/loadgen) and the
// documented API. These handlers are transport twins: same subscription, same
// ": connected first" contract, same per-write bound, same slow-reader eviction.
//
// What a hijacked connection changes, and how each is handled here:
//   - Origin: a WebSocket is not covered by the browser's same-origin read
//     protection, so admission requires the EXACT same origin (wsOriginExact),
//     checked by SMD before Accept; the library's own check (which admits an
//     absent Origin and ignores the scheme) is disabled so one rule applies.
//   - Contexts: r.Context() is not a disconnect signal after Hijack (Go cancels
//     it only when ServeHTTP returns), so the handler owns stopCtx (shutdown) and
//     workCtx (shutdown OR the reader has exited).
//   - Shutdown: http.Server.Shutdown neither closes nor waits for hijacked
//     connections, so handlers register in s.streams BEFORE the upgrade and
//     Server.Shutdown waits for them.
//   - Reads: an SMD-owned reader on Conn.Reader, never CloseRead — in v1.8.15
//     CloseRead's goroutine calls Close(1008) from inside itself and Close then
//     waits on that goroutine's own done-channel until a 15 s timer.
//   - Close: one coordinator per connection; Close() runs in its own goroutine
//     with a grace (wsCloseGrace, capped by the shutdown deadline). At grace
//     expiry the CAPTURED transport is closed directly — CloseNow after Close
//     only waits for the first close, it cannot interrupt it.

// wsCloseGrace is the most a close handshake may take before the transport is
// closed directly (operator ruling 2026-11-05: 1 s maximum, capped by the
// remaining shutdown budget). Package var so tests can widen it.
var wsCloseGrace = time.Second

// streamRegistry tracks upgraded stream handlers so Shutdown can wait for them.
// register happens before the connection leaves HTTP ownership; beginShutdown
// sets closing under the same mutex, so no handler can register (and upgrade)
// after shutdown has begun.
type streamRegistry struct {
	mu       sync.Mutex
	closing  bool
	deadline time.Time // the shutdown context's deadline; zero = none
	n        int
	wg       sync.WaitGroup
}

func (r *streamRegistry) register() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closing {
		return false
	}
	r.n++
	r.wg.Add(1)
	return true
}

func (r *streamRegistry) done() {
	r.mu.Lock()
	r.n--
	r.mu.Unlock()
	r.wg.Done()
}

func (r *streamRegistry) active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

func (r *streamRegistry) beginShutdown(deadline time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closing = true
	r.deadline = deadline
}

func (r *streamRegistry) shutdownDeadline() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.deadline
}

// wait returns nil only once every registered handler has ended; an expired
// context returns its error and promises nothing.
func (r *streamRegistry) wait(ctx context.Context) error {
	all := make(chan struct{})
	go func() {
		r.wg.Wait()
		close(all)
	}()
	select {
	case <-all:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// completion is a result that stays observable: the result is stored first,
// then the channel is CLOSED (never sent on), so the coordinator can watch it
// and the cleanup can join it afterwards without consuming a one-shot value.
type completion struct {
	ch  chan struct{}
	err error
	// unexpectedData: the reader's exit was a data message (a policy close),
	// not a transport failure or a peer close.
	unexpectedData bool
}

func newCompletion() *completion { return &completion{ch: make(chan struct{})} }

func (c *completion) finish(err error, unexpected bool) {
	c.err, c.unexpectedData = err, unexpected
	close(c.ch)
}

// wsOriginExact reports whether r's Origin is exactly this server's own origin:
// present, an absolute http(s) URL with no userinfo, path, query or fragment;
// scheme equal to the daemon's (http unless r.TLS is set); host:port equal to
// r.Host, the host compared case-insensitively. r.Host has already passed
// hostAllowed (requireSameOrigin runs for every method).
func wsOriginExact(r *http.Request) bool {
	o := r.Header.Get("Origin")
	if o == "" || o == "null" {
		return false
	}
	u, err := url.Parse(o)
	if err != nil || u.Opaque != "" || u.User != nil || u.Host == "" ||
		u.Path != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	want := "http"
	if r.TLS != nil {
		want = "https"
	}
	return u.Scheme == want && strings.EqualFold(u.Host, r.Host)
}

// requireStreamOrigin applies wsOriginExact BEFORE subscriber admission: it
// wraps limitEventSubscribers on the WebSocket routes, so a refused upgrade
// never takes a subscriber slot or logs a subscriber transition (reviewer,
// 2026-11-05). serveWSStream keeps the same check as a second gate for any
// caller that mounts it directly.
func (s *Server) requireStreamOrigin(next http.Handler) http.Handler {
	const op errors.Op = "api.requireStreamOrigin"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !wsOriginExact(r) {
			s.logOriginRefused(r, r.Header.Get("Origin"))
			s.writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected", op)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hijackCapture records the net.Conn the library hijacks, so the coordinator
// can close the transport directly at grace expiry. The library finds its
// Hijacker by type before any Unwrap (coder/websocket hijack.go), so this
// wrapper is the one it uses; WriteHeader(101) still reaches the access-log
// recorder underneath.
type hijackCapture struct {
	http.ResponseWriter
	conn net.Conn
}

func (h *hijackCapture) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	c, brw, err := http.NewResponseController(h.ResponseWriter).Hijack()
	if err == nil {
		h.conn = c
	}
	return c, brw, err
}

func (h *hijackCapture) Unwrap() http.ResponseWriter { return h.ResponseWriter }

// wsStreamSpec is what differs between the shell and rig streams.
type wsStreamSpec[E any] struct {
	// subscribe is called BEFORE the upgrade, so a client that sees the 101 is
	// genuinely subscribed (the open-then-fetch contract).
	subscribe func() (<-chan E, func())
	// encode names an event and returns its payload (marshalled into "data").
	encode func(E) (string, any)
	// afterConnected runs synchronously after the "connected" frame (the rig
	// bootstrap). Its context is workCtx: cancelled by shutdown or by the
	// reader's exit (disconnect, peer close, unexpected data).
	afterConnected func(ctx context.Context)
}

type wsFrame struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data,omitempty"`
}

// serveWSStream serves one stream connection end to end. Cleanup order on
// every path: transport closed -> Close joined -> reader joined -> unsubscribe
// -> registry done -> return (which releases the admission slot).
func serveWSStream[E any](s *Server, w http.ResponseWriter, r *http.Request, spec wsStreamSpec[E]) {
	const op errors.Op = "api.serveWSStream"
	if !wsOriginExact(r) {
		s.logOriginRefused(r, r.Header.Get("Origin"))
		s.writeError(w, http.StatusForbidden, "cross_origin", "cross-origin request rejected", op)
		return
	}
	if !s.streams.register() {
		s.writeError(w, http.StatusServiceUnavailable, "shutting_down", "daemon is shutting down", op)
		return
	}
	defer s.streams.done() // registered first, so it runs last

	ch, unsub := spec.subscribe()
	defer unsub() // runs before done

	hc := &hijackCapture{ResponseWriter: w}
	c, err := websocket.Accept(hc, r, &websocket.AcceptOptions{InsecureSkipVerify: true}) // SMD checked Origin above
	if err != nil {
		return // Accept has written the HTTP error response
	}
	if hc.conn == nil { // defensive: the library did not hijack through our wrapper
		_ = c.CloseNow()
		return
	}
	st := &wsStream{s: s, c: c, raw: hc.conn, reader: newCompletion()}
	go st.read()
	// Unconditional cleanup. Every explicit path below closes and joins; a panic
	// (in the rig bootstrap or an encoder) skips them. Deferred after Accept, so
	// it runs BEFORE unsub and registry done, and before recoverPanic sees the
	// panic: the transport is closed first (unblocking any close in progress),
	// then the library is closed and the reader joined. Without it a panic left
	// a live socket and reader that Shutdown would no longer count.
	defer func() {
		if !st.ended {
			_ = st.raw.Close()
			_ = st.c.CloseNow()
			<-st.reader.ch
		}
	}()

	stopCtx, stopCancel := context.WithCancel(context.Background())
	defer stopCancel()
	go func() {
		select {
		case <-s.shutdownCh:
			stopCancel()
		case <-stopCtx.Done():
		}
	}()
	workCtx, workCancel := context.WithCancel(stopCtx)
	defer workCancel()
	go func() {
		select {
		case <-st.reader.ch:
			workCancel()
		case <-workCtx.Done():
		}
	}()

	write := func(name string, payload any) error {
		f := wsFrame{Event: name}
		if payload != nil {
			b, err := json.Marshal(payload)
			if err != nil {
				s.logger.WarnWith().Err(err).Str("event", name).Msg("stream payload marshal failed; skipping event")
				return nil
			}
			f.Data = b
		}
		b, err := json.Marshal(f)
		if err != nil {
			return nil
		}
		wctx, cancel := context.WithTimeout(stopCtx, sseWriteTimeout)
		defer cancel()
		err = c.Write(wctx, websocket.MessageText, b)
		if s.wsWriteDone != nil {
			s.wsWriteDone(name, err)
		}
		return err
	}

	if err := write("connected", nil); err != nil {
		st.closeNow()
		return
	}
	if spec.afterConnected != nil {
		spec.afterConnected(workCtx)
	}

	keepalive := time.NewTicker(sseKeepAliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case <-st.reader.ch:
			if st.reader.unexpectedData {
				st.closeWithGrace(websocket.StatusPolicyViolation, "unexpected data message", stopCtx)
			} else {
				st.closeNow() // peer close or transport failure: clean up directly
			}
			return
		case <-stopCtx.Done():
			st.closeWithGrace(websocket.StatusGoingAway, "shutdown", nil)
			return
		case e, ok := <-ch:
			if !ok { // hub closed or this subscriber evicted as a slow reader
				st.closeWithGrace(websocket.StatusGoingAway, "resync", stopCtx)
				return
			}
			name, payload := spec.encode(e)
			if err := write(name, payload); err != nil {
				st.closeNow() // write timeout or shutdown aborted the write
				return
			}
		case <-keepalive.C:
			if err := write("keepalive", nil); err != nil {
				st.closeNow()
				return
			}
		}
	}
}

type wsStream struct {
	s      *Server
	c      *websocket.Conn
	raw    net.Conn
	reader *completion
	// ended: an explicit close path has closed the transport and joined the
	// reader. Touched only by the handler goroutine.
	ended bool
}

// read is the SMD-owned reader: the library handles ping/pong/close inside
// Reader; a data message is reported and the reader exits WITHOUT closing (the
// coordinator owns every close). Background context: a cancelled read context
// would make the library close the connection without a handshake.
func (st *wsStream) read() {
	_, _, err := st.c.Reader(context.Background())
	st.reader.finish(err, err == nil)
}

// closeNow is the direct cleanup: no close is in progress, so CloseNow does not
// wait on another close; then join the reader.
func (st *wsStream) closeNow() {
	_ = st.c.CloseNow()
	<-st.reader.ch
	st.ended = true
}

// closeWithGrace runs exactly one close handshake, bounded by the grace (capped
// by the shutdown deadline). shortenOn, when non-nil, is the shutdown signal:
// if it fires during this close, only the deadline moves (no second close).
// Completion is closeDone or grace expiry — never the reader alone: on a policy
// close the reader has already finished, and on a shutdown close its exit only
// records progress.
func (st *wsStream) closeWithGrace(code websocket.StatusCode, reason string, shortenOn context.Context) {
	closeDone := newCompletion()
	go func() { closeDone.finish(st.c.Close(code, reason), false) }()

	deadline := capByShutdown(time.Now().Add(wsCloseGrace), st.s.streams.shutdownDeadline())
	timer := time.NewTimer(time.Until(deadline))
	defer timer.Stop()
	var shorten <-chan struct{}
	if shortenOn != nil {
		shorten = shortenOn.Done()
	}
	for waiting := true; waiting; {
		select {
		case <-closeDone.ch:
			waiting = false
		case <-shorten:
			shorten = nil
			if d := capByShutdown(deadline, st.s.streams.shutdownDeadline()); d.Before(deadline) {
				deadline = d
				timer.Reset(time.Until(deadline))
			}
		case <-timer.C:
			// Grace expired: close the transport itself. Blocked reads and writes
			// in the library's close (and in the reader) return errors, so Close
			// reaches Conn.close() and returns.
			_ = st.raw.Close()
			waiting = false
		}
	}
	<-closeDone.ch
	<-st.reader.ch
	st.ended = true
}

func capByShutdown(t, shutdown time.Time) time.Time {
	if !shutdown.IsZero() && shutdown.Before(t) {
		return shutdown
	}
	return t
}

// handleEventsWS is the WebSocket twin of handleEvents (GET /v1/events/ws).
func (s *Server) handleEventsWS(w http.ResponseWriter, r *http.Request) {
	serveWSStream(s, w, r, wsStreamSpec[events.Event]{
		subscribe: s.hub.Subscribe,
		encode:    func(e events.Event) (string, any) { return e.Name, e.Payload },
	})
}

// rigEventsWSHandler is the WebSocket twin of the bridge's SSE handler (GET
// /v1/rig/events/ws). The bridge keeps owning its bootstrap: it is called
// synchronously, as the SSE handler does, with workCtx in place of the request
// context. On CI-V the bootstrap may wait on cmdMu, which observes no context;
// once granted, a cancelled workCtx writes no further frame (ADR 0099
// responsibility 8 — no total bound on that wait is claimed).
func (s *Server) rigEventsWSHandler(br *bridge.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		serveWSStream(s, w, r, wsStreamSpec[bridge.Event]{
			subscribe: br.Subscribe,
			encode:    func(e bridge.Event) (string, any) { return string(e.Name), e.Payload },
			afterConnected: func(ctx context.Context) {
				if err := br.TriggerBootstrap(ctx); err != nil {
					s.logger.WarnWith().Err(err).Msg("bridge: bootstrap poll failed; stream continues")
				}
			},
		})
	}
}

// isStreamPath reports whether p is one of the long-lived stream endpoints —
// SSE or WebSocket. All are exempt from limitConcurrent and bounded instead by
// limitEventSubscribers (see isSSEPath's history).
func isStreamPath(p string) bool {
	switch p {
	case wsEventsRoute, wsRigEventsRoute:
		return true
	}
	return isSSEPath(p)
}

const (
	wsEventsRoute    = "/v1/events/ws"
	wsRigEventsRoute = "/v1/rig/events/ws"
)
