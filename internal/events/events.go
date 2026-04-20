package events

// ServiceName is the DI bean ID under which the daemon's *Hub is
// registered (via iocdi.RegisterInstance). Consumers inject the
// hub with `di.inject:"eventhub"`.
const ServiceName = "eventhub"

// Event names. Use these constants when calling Hub.Publish so the
// emit sites and the SSE handler agree on the wire vocabulary.
// Vocabulary is settled in docs/v2-design/api.md §4.5.
const (
	NameQsoStored        = "qso.stored"
	NameQsoUpdated       = "qso.updated"
	NameQsoDeleted       = "qso.deleted"
	NameForwardSucceeded = "forward.succeeded"
	NameForwardFailed    = "forward.failed"
)

// Event is the envelope delivered to every subscriber.
//
// ID is a per-hub monotonic counter assigned at Publish time. It is
// useful for debug-tracing and client-side dedup, but is NOT a
// resume cursor — the hub keeps no backlog, so a client cannot
// "rewind" to a past ID.
//
// Payload is an event-name-specific struct (QsoStoredPayload,
// ForwardSucceededPayload, etc.). The SSE handler marshals it to
// JSON; in-process consumers can type-assert on it directly.
type Event struct {
	ID      int64
	Name    string
	Payload any
}

// QsoStoredPayload is the payload for NameQsoStored.
// Minimal shape — clients re-query for QSO details if they need them.
type QsoStoredPayload struct {
	QsoID     int64 `json:"qso_id"`
	LogbookID int64 `json:"logbook_id"`
}

// QsoUpdatedPayload is the payload for NameQsoUpdated.
type QsoUpdatedPayload struct {
	QsoID     int64 `json:"qso_id"`
	LogbookID int64 `json:"logbook_id"`
}

// QsoDeletedPayload is the payload for NameQsoDeleted.
type QsoDeletedPayload struct {
	QsoID     int64 `json:"qso_id"`
	LogbookID int64 `json:"logbook_id"`
}

// ForwardSucceededPayload is the payload for NameForwardSucceeded,
// emitted when the worker marks a qso_upload row as succeeded. Action
// is "insert", "update", or "delete" (from internal/enums/upload/action).
// UpstreamID is the remote service's identifier for the stored record
// (e.g. QRZ LOGID). Empty for forwarders that don't produce one.
type ForwardSucceededPayload struct {
	QsoID         int64  `json:"qso_id"`
	ForwarderName string `json:"forwarder_name"`
	Action        string `json:"action"`
	UpstreamID    string `json:"upstream_id,omitempty"`
	Attempts      int    `json:"attempts"`
}

// ForwardFailedPayload is the payload for NameForwardFailed, emitted
// when the worker gives up on a qso_upload row (terminal outcome or
// retries exhausted). Reason is a short human-readable summary; the
// full error history lives in qso_upload.last_error.
type ForwardFailedPayload struct {
	QsoID         int64  `json:"qso_id"`
	ForwarderName string `json:"forwarder_name"`
	Action        string `json:"action"`
	Attempts      int    `json:"attempts"`
	Reason        string `json:"reason"`
}
