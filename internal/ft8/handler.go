package ft8

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// SSE event-type names on the /v1/ft8/events stream. Extending this set is a
// wire-protocol change that requires updating the SPA's EventSource consumer.
const (
	// EventOccupancy carries a per-slot OccupancyReport (TX-offset picker).
	EventOccupancy = "ft8-occupancy"
	// EventDecode carries a per-slot DecodeReport (live Band Activity feed).
	EventDecode = "ft8-decode"
	// EventTx carries a TxState — the FT8 transmit arm/disarm + in-flight
	// status (ADR 0030 step e1). Cached for late-subscriber replay so a tab
	// connecting mid-session sees the current arm state immediately.
	EventTx = "ft8-tx"
	// EventQso carries a QsoStatus — the manual sequencer's active-contact state
	// (ADR 0031 step e3). Cached for late-subscriber replay.
	EventQso = "ft8-qso"
	// EventLogged carries a LoggedQso — a one-shot "this completed exchange was
	// logged" notification (ADR 0029 step e4) so the SPA can add it to its
	// session list. Deliberately NOT cached for replay (see hub.publish): a late
	// subscriber re-receiving an old logged event would duplicate the session row.
	EventLogged = "ft8-logged"
	// EventAudioLevel carries an AudioLevel — the RX capture level meter
	// (~4 Hz while capture is live; audiolevel.go). NOT cached for replay:
	// the next window lands within 250 ms, and a replayed stale level could
	// masquerade as a live one to exactly the staleness check the SPA runs.
	EventAudioLevel = "ft8-audio-level"
)

// sseKeepAliveInterval matches the /v1/events and rig-events handlers — 30 s of
// idle keeps NAT/proxy paths warm without wasting bandwidth. Package-level var
// so tests can dial it down.
var sseKeepAliveInterval = 30 * time.Second

// sseWriteTimeout bounds a single SSE frame/keepalive write+flush so a wedged
// peer (socket held open, never reading) can't hang the handler goroutine
// indefinitely and hold graceful shutdown open. Re-armed before every write, so
// the long idle gap between keepalives never trips it. Mirrors the bridge
// handler's per-write deadline. Package-level var so tests can dial it down.
var sseWriteTimeout = 10 * time.Second

// Subscribe registers a new ft8-events subscriber, returning a receive-only
// channel and an idempotent unsubscribe. The latest cached events (if any) are
// replayed as the first messages.
//
// Subscriber count drives the capture device: the first subscriber acquires it,
// the last (after a linger) releases it. The returned unsubscribe decrements
// exactly once even if called repeatedly, and the SSE handler always calls it
// on exit — including after a hub-side slow-reader eviction — so the count
// stays balanced.
func (s *Service) Subscribe() (<-chan hubEvent, func()) {
	ch, unsub := s.hub.subscribe()
	s.onSubscriberAdded()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			s.onSubscriberRemoved()
			unsub()
		})
	}
}

// HTTPHandler returns the http.Handler serving `GET /v1/ft8/events`: the live
// per-slot occupancy stream feeding the clear-offset picker. The api package
// registers it on the daemon's mux when the FT8 subsystem is enabled (mirroring
// the bridge's `/v1/rig/events`).
//
// shutdownCh is the daemon-wide shutdown signal — the stream must observe both
// r.Context() (per-request cancel) and shutdownCh (daemon Shutdown), else an
// idle subscriber holds graceful shutdown open until the timeout.
//
// Unlike the bridge handler there is no bootstrap poll: the hub's one-slot
// replay cache already hands a freshly-connected tab the current occupancy, and
// the next slot (≤15 s away) refreshes it.
func (s *Service) HTTPHandler(shutdownCh <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Bound each write with a fresh deadline rather than clearing it
		// outright, so a wedged peer fails within the bound and the goroutine
		// can exit. Probe once: a writer chain with no underlying net.Conn
		// (some test recorders) can't carry a deadline — fall back to none +
		// a single log; the stream still works under the server WriteTimeout.
		rc := http.NewResponseController(w)
		deadlineSupported := true
		armWrite := func() {
			if !deadlineSupported {
				return
			}
			if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
				deadlineSupported = false
				s.log.InfoWith().Err(err).Msg("ft8 SSE per-write deadline unsupported; stream subject to WriteTimeout")
			}
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		armWrite()
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch, unsub := s.Subscribe()
		defer unsub()

		keepalive := time.NewTicker(sseKeepAliveInterval)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return

			case <-shutdownCh:
				return

			case evt, ok := <-ch:
				if !ok {
					// Hub closed (Service.Stop) or subscriber evicted (slow
					// reader). Stream ends cleanly; the SPA's EventSource
					// auto-reconnects.
					return
				}
				armWrite()
				if err := s.writeEvent(w, evt); err != nil {
					return
				}
				flusher.Flush()

			case <-keepalive.C:
				armWrite()
				if _, err := io.WriteString(w, ": keepalive\n\n"); err != nil {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// writeEvent emits one event as an SSE frame:
//
//	event: <name>
//	data: <json>
//	<blank line>
//
// A marshal failure (a programmer error for a known typed payload) is a
// non-fatal skip logged at warn, so one bad event can't kill the stream.
func (s *Service) writeEvent(w io.Writer, evt hubEvent) error {
	data, err := json.Marshal(evt.payload)
	if err != nil {
		s.log.WarnWith().Err(err).Str("event", evt.name).Msg("ft8 SSE payload marshal failed; skipping event")
		return nil
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.name, data)
	return err
}
