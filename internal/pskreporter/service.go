package pskreporter

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/logging"
	"github.com/ColonelBlimp/station-manager/internal/safego"
)

// PSK Reporter upload service: buffers FT8 reception spots, dedups, and flushes
// them as IPFIX datagrams over a single long-lived UDP socket (so the source port
// stays constant, as the protocol requires). Best-effort by contract — a send
// failure is logged and dropped; nothing here ever blocks the FT8 decode path.
//
// The UDP collector is report.pskreporter.info (NOT pskreporter.info — that's the
// Cloudflare-fronted website and silently drops UDP). Port 4739 is production (the
// default); port 14739 on the SAME host is the test server, which accepts + parses
// datagrams without writing the live database (verify at /cgi-bin/psk-analysis.pl).

const (
	// DefaultHost/DefaultPort are the production collector — the fallback when the
	// operator leaves Host/Port unset. The test endpoint (same host, port 14739) is
	// not a constant here: it's just another Host/Port the operator (or the
	// ft8-psk-probe CLI) supplies via Config.
	DefaultHost = "report.pskreporter.info"
	DefaultPort = 4739

	flushInterval   = 5 * time.Minute  // protocol floor: a datagram at most every 5 min
	maxJitter       = 30 * time.Second // de-synchronise reporters (spec)
	templateBurst   = 3                // send descriptors in the first N datagrams (UDP is lossy)
	templateRefresh = time.Hour        // and at least hourly thereafter
	maxBufferedRows = 80               // flush early when a datagram would fill (~80–90 max)
)

// Config is the operator-set transport config (the receiver identity is passed
// separately — it comes from the station config, not here).
type Config struct {
	Enabled bool
	Host    string // "" → DefaultHost
	Port    int    // 0 → DefaultPort
}

// Service owns the spot buffer + UDP transport. Construct with New, then
// Start(ctx) → Stop. Start is single-use — calling it twice would open a second
// socket + flush loop without closing the first; the daemon calls it once.
type Service struct {
	cfg Config
	log logging.Logger

	mu      sync.Mutex
	recv    Receiver        // us (callsign/grid/software/antenna) — settable via SetReceiver
	buf     map[string]Spot // call → strongest spot this window (dedup)
	seq     uint32          // cumulative count of reports sent in prior datagrams (IPFIX)
	id      uint32          // constant per-session random identifier (header)
	sent    int             // datagrams sent (drives the first-N-templates rule)
	lastTpl time.Time       // last time descriptors were included
	conn    *net.UDPConn
	stopped bool // set by Stop after its final flush — further AddSpot drops

	cancel   context.CancelFunc
	wg       sync.WaitGroup
	flushNow chan struct{} // wakes the flush loop when the buffer fills (off the caller's goroutine)
}

// New builds the service. recv is the receiver identity (from the station config);
// a nil logger becomes a no-op. Host/Port default to the production server.
func New(cfg Config, recv Receiver, log logging.Logger) *Service {
	if log == nil {
		log = logging.Noop()
	}
	if cfg.Host == "" {
		cfg.Host = DefaultHost
	}
	if cfg.Port == 0 {
		cfg.Port = DefaultPort
	}
	return &Service{cfg: cfg, log: log, recv: recv, buf: make(map[string]Spot), flushNow: make(chan struct{}, 1)}
}

// SetReceiver updates the receiver identity (e.g. after a config change).
func (s *Service) SetReceiver(r Receiver) {
	s.mu.Lock()
	s.recv = r
	s.mu.Unlock()
}

// AddSpot buffers a reception report. No-op when disabled, the call is blank, or the
// service isn't live (not started / start failed / stopped — so the buffer can't
// grow unbounded with nowhere to flush). Dedups within the flush window — the
// strongest SNR per call wins (the spec wants each call at most once per ~5 min).
// When the buffer fills it WAKES the flush loop rather than sending inline: this can
// be called on the FT8 decode goroutine, and UDP I/O must never block decoding.
func (s *Service) AddSpot(spot Spot) {
	if !s.cfg.Enabled || strings.TrimSpace(spot.Call) == "" {
		return
	}
	s.mu.Lock()
	if s.conn == nil || s.stopped { // not live / stopped — drop (best-effort)
		s.mu.Unlock()
		return
	}
	if cur, ok := s.buf[spot.Call]; !ok || spot.SNR > cur.SNR {
		s.buf[spot.Call] = spot
	}
	full := len(s.buf) >= maxBufferedRows
	s.mu.Unlock()
	if full {
		// Non-blocking wake; a pending signal already covers this.
		select {
		case s.flushNow <- struct{}{}:
		default:
		}
	}
}

// Start opens the UDP socket and launches the flush loop. No-op (and no socket)
// when disabled. The socket is long-lived so the source port stays constant.
func (s *Service) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.log.InfoWith().Msg("pskreporter: disabled (no FT8 spots uploaded)")
		return nil
	}
	addr, err := net.ResolveUDPAddr("udp", fmt.Sprintf("%s:%d", s.cfg.Host, s.cfg.Port))
	if err != nil {
		return fmt.Errorf("pskreporter: resolve %s:%d: %w", s.cfg.Host, s.cfg.Port, err)
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("pskreporter: dial %s: %w", addr, err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.conn = conn
	s.id = rand.Uint32() // constant for this session
	s.cancel = cancel
	s.mu.Unlock()

	s.log.InfoWith().Str("host", s.cfg.Host).Int("port", s.cfg.Port).
		Msg("pskreporter: uploading FT8 reception spots")
	// respawn=true: the flush loop is a long-lived worker driving off shared
	// buffer/socket state, so a recovered panic must NOT leave spots buffering
	// with no background flusher (review 2026-06-19 M2). Re-entering flushLoop is
	// safe — it re-reads the same s.buf/s.conn. Stop cancels runCtx, which ends
	// the respawn loop, so shutdown still drains.
	safego.GoTracked(runCtx, "pskreporter.flush", s.onPanic, func() { s.flushLoop(runCtx) }, true, &s.wg)
	return nil
}

// Stop cancels the flush loop, then performs the authoritative final flush and
// closes the socket. Stop owns the final flush (not just the loop's ctx.Done
// flush): the daemon cancels the shared worker context BEFORE draining FT8, so a
// last decode can buffer a spot after the loop has already exited — marking
// stopped here (so further AddSpot drops) and flushing once more sends it rather
// than losing it silently (review 2026-06-19 M1).
func (s *Service) Stop() error {
	s.mu.Lock()
	cancel := s.cancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.wg.Wait() // the loop has exited (and did its own best-effort ctx.Done flush)

	// Mark stopped first so any concurrent late AddSpot drops, then flush
	// whatever was buffered up to this cutoff, then close the socket.
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.flush()
	s.mu.Lock()
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
	s.mu.Unlock()
	return nil
}

// flushLoop sends buffered spots on a ~5 min cadence, or early when AddSpot signals
// a full buffer (the "unless the packet becomes full" exception). The interval is
// timer-based (program-relative, NOT synced to the system clock) plus jitter, per
// the spec, so many stations don't all report on the same second. All UDP I/O
// happens here (or in Stop's final flush), never on a caller's goroutine.
func (s *Service) flushLoop(ctx context.Context) {
	// A stoppable timer (not time.After per iteration): a full-buffer flushNow
	// wake must reset the timer, not leave the old 5-min-plus-jitter timer
	// allocated until its deadline — avoidable retention in a long-running
	// daemon (review 2026-06-19 L1).
	timer := time.NewTimer(flushInterval + jitter())
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			s.flush() // best-effort final flush on shutdown
			return
		case <-s.flushNow:
			s.flush() // buffer filled — drain it now (off the decode goroutine)
			resetTimer(timer)
		case <-timer.C:
			s.flush()
			resetTimer(timer)
		}
	}
}

// jitter is the spec's per-reporter de-synchronisation offset (0..maxJitter).
func jitter() time.Duration { return time.Duration(rand.Int64N(int64(maxJitter))) }

// resetTimer stops the timer (draining a pending fire non-blockingly) and rearms
// it for the next interval+jitter, so an early flushNow wake doesn't leak the
// prior timer.
func resetTimer(t *time.Timer) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(flushInterval + jitter())
}

// flush snapshots + clears the buffer under the lock, then encodes and sends
// OUTSIDE the lock (UDP I/O never blocks AddSpot). Descriptors ride the first few
// datagrams and hourly thereafter.
func (s *Service) flush() {
	s.mu.Lock()
	if len(s.buf) == 0 || s.conn == nil {
		s.mu.Unlock()
		return
	}
	spots := make([]Spot, 0, len(s.buf))
	for _, sp := range s.buf {
		spots = append(spots, sp)
	}
	s.buf = make(map[string]Spot)
	recv, id, conn := s.recv, s.id, s.conn

	// Split into datagrams of at most maxBufferedRows spots each. The early-flush
	// wake is only a hint, not a hard cap, so a bursty caller can leave the buffer
	// well past one datagram's worth; encoding all of it into a single packet
	// risks an oversized UDP write or a wrapped IPFIX length field (review M2).
	// Per-datagram header state (seq counts reports; templates ride the first
	// burst + hourly) is computed up-front under the one lock.
	type plan struct {
		seq           uint32
		withTemplates bool
		spots         []Spot
	}
	var plans []plan
	for start := 0; start < len(spots); start += maxBufferedRows {
		end := min(start+maxBufferedRows, len(spots))
		chunk := spots[start:end]
		withTemplates := s.sent < templateBurst || time.Since(s.lastTpl) >= templateRefresh
		plans = append(plans, plan{seq: s.seq, withTemplates: withTemplates, spots: chunk})
		s.seq += uint32(len(chunk)) // IPFIX: seq counts reports, not packets
		s.sent++
		if withTemplates {
			s.lastTpl = time.Now()
		}
	}
	s.mu.Unlock()

	for _, p := range plans {
		dg := encodeDatagram(uint32(time.Now().Unix()), p.seq, id, p.withTemplates, recv, p.spots)
		if _, err := conn.Write(dg); err != nil {
			s.log.WarnWith().Err(err).Int("spots", len(p.spots)).
				Msg("pskreporter: datagram send failed (dropped)")
			continue
		}
		s.log.InfoWith().Int("spots", len(p.spots)).Bool("templates", p.withTemplates).
			Msg("pskreporter: uploaded spots")
	}
}

// Flush sends any buffered spots immediately as the next datagram. The 5-minute
// loop normally drives this; exposed for the ft8-psk-probe CLI (and a forced
// send). No-op when nothing is buffered or the socket is closed.
func (s *Service) Flush() { s.flush() }

func (s *Service) onPanic(name string, panicValue any, stack []byte) {
	s.log.ErrorWith().
		Str("goroutine", name).
		Interface("panic", panicValue).
		Bytes("stack", stack).
		Msg("pskreporter: goroutine panicked (recovered)")
}
