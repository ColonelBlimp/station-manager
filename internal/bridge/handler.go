package bridge

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// sseKeepAliveInterval matches the existing /v1/events handler — 30s
// of idle is the practical sweet spot for keeping NAT/proxy paths
// warm without wasting bandwidth. Package-level var so tests can
// dial it down.
var sseKeepAliveInterval = 30 * time.Second

// HTTPHandler returns the http.Handler that serves
// `GET /v1/rig/events`. Caller registers it on the daemon's mux when
// `bridge.enabled: true`; the api package's wiring layer is the
// canonical caller (see internal/api/server.go).
//
// shutdownCh is the daemon-wide shutdown signal — long-lived SSE
// streams must observe both r.Context() (per-request cancel) and
// shutdownCh (daemon Shutdown), otherwise an idle subscriber holds
// graceful shutdown open until the timeout fires.
//
// The handler logs the rare write-deadline-clear failure via
// s.logger (the Service's own logger, set in New) — no separate
// logger parameter; there's no scenario where a different logger
// would legitimately be passed.
func (s *Service) HTTPHandler(shutdownCh <-chan struct{}) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming unsupported", http.StatusInternalServerError)
			return
		}

		// Long-running stream: clear the WriteDeadline so an idle
		// (but still-connected) client isn't disconnected every
		// WriteTimeoutSec. Same pattern as /v1/events.
		rc := http.NewResponseController(w)
		if err := rc.SetWriteDeadline(time.Time{}); err != nil {
			s.logger.InfoWith().Err(err).Msg("rig SSE write-deadline clear failed; stream subject to WriteTimeout")
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		// Prevent intermediary buffering (nginx, etc.). No-op on
		// direct loopback but keeps the deployment-flexible default
		// honest.
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch, unsub := s.Subscribe()
		defer unsub()

		// Bootstrap poll: ask the rig for an identity + state
		// snapshot so this freshly-connected SPA tab populates
		// catState immediately rather than waiting for the
		// operator to wiggle the dial (per ADR 0019). The poll's
		// responses flow back through readLoop and fan out to
		// every subscriber — existing subscribers see a redundant
		// state refresh ($state proxy merges idempotently). Errors
		// here are logged but don't break the SSE stream — the SPA
		// just sees defaults until the rig pushes naturally.
		if err := s.TriggerBootstrap(r.Context()); err != nil {
			s.logger.WarnWith().Err(err).Msg("bridge: bootstrap poll failed; SSE stream continues")
		}

		keepalive := time.NewTicker(sseKeepAliveInterval)
		defer keepalive.Stop()

		for {
			select {
			case <-r.Context().Done():
				return

			case <-shutdownCh:
				// Daemon shutdown — return promptly so http.Server.Shutdown
				// doesn't have to wait for the graceful timeout.
				return

			case evt, ok := <-ch:
				if !ok {
					// Hub closed (Service.Stop) or subscriber evicted
					// (slow reader). Stream ends cleanly; the SPA's
					// EventSource auto-reconnects.
					return
				}
				if err := s.writeSSEEvent(w, evt); err != nil {
					// Client gone (broken pipe). Defer unsub fires.
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
	})
}

// writeSSEEvent emits one event frame in the standard wire format.
// Mirrors the /v1/events handler's writeSSEEvent but typed for
// bridge.Event. Marshal failure for a known typed payload would be
// a programmer error; treated as a non-fatal skip so one bad event
// doesn't kill the stream for unrelated good events. The skip is
// logged at warn so a future regression that breaks one payload
// type doesn't disappear silently — same pattern as the /v1/events
// writeSSEEvent.
func (s *Service) writeSSEEvent(w io.Writer, evt Event) error {
	data, err := json.Marshal(evt.Payload)
	if err != nil {
		s.logger.WarnWith().
			Err(err).
			Str("event", string(evt.Name)).
			Msg("rig SSE payload marshal failed; skipping event")
		return nil
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", evt.Name, data)
	return err
}
