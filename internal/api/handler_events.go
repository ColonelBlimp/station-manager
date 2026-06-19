package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ColonelBlimp/station-manager/internal/events"
)

// sseKeepAliveInterval is how often the handler emits an SSE comment
// line to keep idle connections warm and detect dead-peer TCPs faster.
// Package-level var so tests can dial it down without waiting 30 s.
var sseKeepAliveInterval = 30 * time.Second

// sseWriteTimeout bounds a single SSE write so a wedged peer (socket open but
// not reading) can't hang the handler goroutine forever. Re-armed before every
// frame/keepalive; the idle gap between keepalives never trips it. Matches the
// bridge / FT8 SSE handlers. Package var so tests can dial it down.
var sseWriteTimeout = 10 * time.Second

// handleEvents serves the daemon's live event stream as Server-Sent
// Events (text/event-stream). The wire vocabulary is fixed in
// docs/v2-design/api.md §4.5 — qso.stored/updated/deleted and
// forward.succeeded/failed, with payloads defined in internal/events.
//
// Scope: firehose, no server-side filtering. Personal-operator scale
// (1–3 clients) makes topic subscriptions unnecessary; clients select
// events client-side by the `event:` field.
//
// Reconnect contract: the handler keeps no backlog. A client that
// connects (or reconnects) receives events from that moment forward
// only. The baseline state is reconciled via ordinary GET endpoints;
// to avoid a race, clients should open the stream first, then fetch
// current state — events for rows already in the fetch are idempotent.
//
// Slow-reader policy: if the subscriber's in-hub channel fills, the
// hub disconnects the subscriber (closes its channel). The handler
// sees the closed channel and returns; the HTTP response ends; the
// client reconnects and re-syncs. Silent event-dropping was rejected.
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// Long-running stream: rather than clearing the WriteDeadline outright
	// (which lets a wedged peer — socket open, not reading — hang this goroutine
	// forever, unable to observe ctx/shutdown/eviction; review internal-api M1),
	// bound EACH write with a fresh deadline. armWrite resets it to
	// now+sseWriteTimeout immediately before every frame/keepalive, so the long
	// idle gap between keepalives never trips it but a blocked write fails within
	// the bound and the goroutine can exit. Probe once: a writer chain with no
	// underlying net.Conn (some test recorders) can't carry a deadline — fall
	// back to no deadline + a single log; the stream still works under the
	// server's WriteTimeout. Mirrors the bridge / FT8 SSE handlers.
	rc := http.NewResponseController(w)
	deadlineSupported := true
	armWrite := func() {
		if !deadlineSupported {
			return
		}
		if err := rc.SetWriteDeadline(time.Now().Add(sseWriteTimeout)); err != nil {
			deadlineSupported = false
			s.logger.InfoWith().Err(err).Msg("SSE per-write deadline unsupported; stream subject to WriteTimeout")
		}
	}

	// Subscribe BEFORE the 200 OK is observable. The hub keeps no backlog, so a
	// client that opens the stream and then immediately fetches state (or
	// triggers a write) must not lose an event published in the gap between the
	// flush and Subscribe(). Subscribing first closes that window — the
	// "open the stream first, then fetch" contract is only sound if the
	// subscription exists by the time the client sees the stream open (review
	// 2026-06-19 M1). Subscribe doesn't touch w, so it's safe before WriteHeader.
	ch, unsub := s.hub.Subscribe()
	defer unsub()

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Suppress buffering for deployments behind nginx or similar; a
	// no-op for the unix-socket default but keeps TCP mode clean.
	h.Set("X-Accel-Buffering", "no")
	armWrite()
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	keepalive := time.NewTicker(sseKeepAliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return

		case <-s.shutdownCh:
			// Daemon is shutting down. r.Context() does not fire on
			// http.Server.Shutdown — only on connection close — so
			// without this case an idle SSE subscriber would keep
			// shutdown blocked until the graceful-shutdown timeout.
			return

		case evt, ok := <-ch:
			if !ok {
				// Hub closed or subscriber evicted for being slow.
				// Either way, the stream ends cleanly — client sees
				// EOF and reconnects.
				return
			}
			armWrite()
			if err := s.writeSSEEvent(w, evt); err != nil {
				// Client gone (broken pipe / reset) or a wedged write hit the
				// per-write deadline. Either way, the defer unsubs us.
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
}

// writeSSEEvent emits one event frame in the standard wire format:
//
//	id: <n>
//	event: <name>
//	data: <json>
//	<blank line>
//
// Payloads are JSON-marshalled. Our payload structs never produce
// embedded newlines (json.Marshal without SetIndent is single-line),
// so the single `data:` line is always well-formed. A marshal
// failure for a known typed payload would be a programmer error;
// treated as a non-fatal skip so one bad event doesn't kill the
// stream for unrelated good events. The skip is logged at warn so a
// future regression that breaks one payload type doesn't disappear
// silently.
func (s *Server) writeSSEEvent(w io.Writer, evt events.Event) error {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		s.logger.WarnWith().
			Err(err).
			Str("event", evt.Name).
			Int64("event_id", evt.ID).
			Msg("SSE payload marshal failed; skipping event")
		return nil
	}
	_, err = fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", evt.ID, evt.Name, data)
	return err
}
