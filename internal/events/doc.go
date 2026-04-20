// Package events is the daemon's in-memory pub/sub hub for
// surfacing QSO lifecycle and forwarding-outcome transitions to
// subscribers (the HTTP `GET /v1/events` SSE handler, and any
// future in-process listeners).
//
// Scope and scale assumptions:
//
//   - Personal-operator daemon: expect 1–3 concurrent subscribers,
//     not hundreds.
//   - Events are ephemeral notifications, not an audit log. The
//     authoritative state lives in the SQLite database; a client
//     reconnecting re-queries state via `GET /v1/qsos` (etc.) and
//     then resumes the stream.
//   - No backlog, no replay. A subscriber connecting 1 hour after
//     daemon start sees events from the moment of Subscribe, not
//     from daemon start.
//
// Slow-reader policy: each subscriber has a bounded channel buffer.
// When Publish finds a subscriber's channel full, the subscriber is
// disconnected (channel closed, removed from the hub). The handler
// on the other side sees the closed channel, returns from its loop,
// and the HTTP connection drops; the client reconnects and
// reconciles via a state fetch. Silent event-dropping was rejected —
// a client that can't tell it's out of sync is worse than a clean
// disconnect.
//
// See docs/v2-design/api.md §4.5 for the settled event vocabulary
// and docs/session-handoff.md for the design conversation that
// settled the topology.
package events
