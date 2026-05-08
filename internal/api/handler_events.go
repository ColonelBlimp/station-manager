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

	// Long-running stream: remove the server's WriteTimeout so an idle
	// but still-connected client isn't force-disconnected every
	// WriteTimeoutSec. ResponseController is the std-lib-blessed way
	// (Go 1.21+).
	//
	// Deadline removal is sticky for the connection lifetime — net.Conn
	// does not auto-rearm after each write, so a single
	// SetWriteDeadline(time.Time{}) covers the whole stream. If the
	// underlying conn doesn't support deadline control we surface the
	// failure at info level (the connection still works, just under the
	// regular WriteTimeout) so a future regression that breaks all
	// long-lived streams is at least visible in logs.
	rc := http.NewResponseController(w)
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		s.logger.InfoWith().Err(err).Msg("SSE write-deadline clear failed; stream subject to WriteTimeout")
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Suppress buffering for deployments behind nginx or similar; a
	// no-op for the unix-socket default but keeps TCP mode clean.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch, unsub := s.hub.Subscribe()
	defer unsub()

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
			if err := s.writeSSEEvent(w, evt); err != nil {
				// Client gone (broken pipe / reset). Nothing useful
				// to log at info level; the defer unsubs us.
				return
			}
			flusher.Flush()

		case <-keepalive.C:
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
